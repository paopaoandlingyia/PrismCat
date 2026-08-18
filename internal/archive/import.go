package archive

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

const (
	maxArchiveEntryBytes = int64(64 << 30)
	maxArchiveTotalBytes = int64(256 << 30)
	maxArchiveEntries    = 2_000_000
)

func (m *Manager) ImportFromS3(ctx context.Context, key string) (*storage.ArchiveImport, error) {
	if !m.begin() {
		return nil, ErrArchiveBusy
	}
	defer m.end()
	cfg := m.cfg.ArchiveSnapshot()
	store, err := m.store(cfg.S3)
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "prismcat-import-download-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, "archive.tar.zst")
	if _, err := downloadVerifiedArchive(ctx, store, key, archivePath); err != nil {
		return nil, err
	}
	return m.importFileLocked(ctx, archivePath, key)
}

func (m *Manager) ImportDate(ctx context.Context, date string) ([]storage.ArchiveImport, error) {
	if !m.begin() {
		return nil, ErrArchiveBusy
	}
	defer m.end()
	cfg := m.cfg.ArchiveSnapshot()
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, err
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), loc)
	if err != nil {
		return nil, fmt.Errorf("invalid backup date: %w", err)
	}
	store, err := m.store(cfg.S3)
	if err != nil {
		return nil, err
	}
	objects, err := store.list(ctx, objectDatePrefix(cfg.KeyPrefix, day))
	if err != nil {
		return nil, err
	}
	imports := make([]storage.ArchiveImport, 0, len(objects))
	for _, object := range objects {
		tempDir, err := os.MkdirTemp("", "prismcat-import-date-*")
		if err != nil {
			return imports, err
		}
		archivePath := filepath.Join(tempDir, "archive.tar.zst")
		if _, err := downloadVerifiedArchive(ctx, store, object.Key, archivePath); err != nil {
			_ = os.RemoveAll(tempDir)
			return imports, err
		}
		batch, err := m.importFileLocked(ctx, archivePath, object.Key)
		_ = os.RemoveAll(tempDir)
		if err != nil {
			return imports, err
		}
		imports = append(imports, *batch)
	}
	return imports, nil
}

func downloadVerifiedArchive(ctx context.Context, store objectStore, key, archivePath string) (SidecarManifest, error) {
	sidecarData, err := store.readBytes(ctx, key+".manifest.json", 1<<20)
	if err != nil {
		return SidecarManifest{}, fmt.Errorf("read backup manifest: %w", err)
	}
	if len(sidecarData) > 1<<20 {
		return SidecarManifest{}, errors.New("backup manifest is too large")
	}
	var sidecar SidecarManifest
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil || sidecar.ObjectKey != key ||
		sidecar.BatchID == "" || sidecar.PackageSHA256 == "" || sidecar.FormatVersion != archiveFormatVersion {
		return SidecarManifest{}, errors.New("invalid backup manifest")
	}
	if err := store.download(ctx, key, archivePath); err != nil {
		return SidecarManifest{}, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return SidecarManifest{}, err
	}
	if sidecar.CompressedBytes > 0 && info.Size() != sidecar.CompressedBytes {
		return SidecarManifest{}, fmt.Errorf("backup package size mismatch: got %d, want %d", info.Size(), sidecar.CompressedBytes)
	}
	packageSHA, err := fileSHA256(archivePath)
	if err != nil {
		return SidecarManifest{}, err
	}
	if !strings.EqualFold(packageSHA, sidecar.PackageSHA256) {
		return SidecarManifest{}, errors.New("backup package SHA-256 mismatch")
	}
	return sidecar, nil
}

func (m *Manager) ImportFile(ctx context.Context, archivePath, source string) (*storage.ArchiveImport, error) {
	if !m.begin() {
		return nil, ErrArchiveBusy
	}
	defer m.end()
	return m.importFileLocked(ctx, archivePath, source)
}

func (m *Manager) importFileLocked(ctx context.Context, archivePath, source string) (*storage.ArchiveImport, error) {
	cfg := m.cfg.ArchiveSnapshot()
	batch := storage.ArchiveImport{
		ID: uuid.NewString(), SourceKey: source, Status: "importing", CreatedAt: time.Now().UTC(),
	}
	if cfg.ImportRetentionHours > 0 {
		expires := batch.CreatedAt.Add(time.Duration(cfg.ImportRetentionHours) * time.Hour)
		batch.ExpiresAt = &expires
	}
	if err := m.archive.CreateArchiveImport(batch); err != nil {
		return nil, err
	}
	fail := func(err error) (*storage.ArchiveImport, error) {
		batch.Status = "failed"
		batch.Error = err.Error()
		_ = m.archive.UpdateArchiveImport(batch)
		return &batch, err
	}

	extractDir, manifest, err := extractArchive(archivePath)
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(extractDir)
	if manifest.FormatVersion != archiveFormatVersion {
		return fail(fmt.Errorf("unsupported archive format version %d", manifest.FormatVersion))
	}
	logsFile, err := os.Open(filepath.Join(extractDir, "logs.jsonl"))
	if err != nil {
		return fail(err)
	}
	defer logsFile.Close()
	scanner := bufio.NewScanner(logsFile)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		var logEntry storage.RequestLog
		if err := json.Unmarshal(scanner.Bytes(), &logEntry); err != nil {
			return fail(fmt.Errorf("decode logs.jsonl: %w", err))
		}
		exists, err := m.archive.LogExists(logEntry.ID)
		if err != nil {
			return fail(err)
		}
		if exists {
			continue
		}
		logEntry.Origin = "archive_import"
		logEntry.ImportBatchID = batch.ID
		logEntry.BackupVerifiedAt = nil
		logEntry.BackupBatchID = ""
		logEntry.DeleteGraceStartedAt = nil
		logEntry.RequestBody = ""
		logEntry.RequestBodyOriginal = ""
		logEntry.RequestBodyFinal = ""
		logEntry.ResponseBody = ""
		for i := range logEntry.Bodies {
			body := &logEntry.Bodies[i]
			hexRef, err := blobHex(body.BlobRef)
			if err != nil {
				return fail(err)
			}
			bodyPath := filepath.Join(extractDir, "bodies", hexRef)
			data, err := os.ReadFile(bodyPath)
			if err != nil {
				return fail(fmt.Errorf("read archived body %s: %w", body.BlobRef, err))
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != hexRef {
				return fail(fmt.Errorf("archived body %s checksum mismatch", body.BlobRef))
			}
			ref, err := m.blobs.Put(ctx, data)
			if err != nil {
				return fail(err)
			}
			if ref != body.BlobRef {
				return fail(fmt.Errorf("imported body ref mismatch: got %s, want %s", ref, body.BlobRef))
			}
			if err := m.archive.StageArchiveBlobRef(batch.ID, ref); err != nil {
				return fail(err)
			}
			body.LogID = logEntry.ID
			body.Recoverable = body.BlobRef != "" && !body.Truncated
		}
		annotation := logEntry.Annotation
		logEntry.Annotation = storage.LogAnnotation{}
		if err := m.archive.SaveImportedLog(&logEntry); err != nil {
			return fail(err)
		}
		if annotation.Saved || annotation.Status != "" || annotation.Note != "" || len(annotation.Labels) > 0 {
			if _, err := m.repo.SaveLogAnnotation(logEntry.ID, annotation); err != nil {
				return fail(err)
			}
		}
		batch.LogCount++
	}
	if err := scanner.Err(); err != nil {
		return fail(err)
	}
	batch.Status = "complete"
	batch.Error = ""
	if err := m.archive.UpdateArchiveImport(batch); err != nil {
		return fail(err)
	}
	if err := m.archive.ClearArchiveBlobRefs(batch.ID); err != nil {
		return fail(err)
	}
	return &batch, nil
}

func (m *Manager) DeleteImport(batchID string) (int64, error) {
	n, err := m.archive.DeleteArchiveImport(strings.TrimSpace(batchID))
	if err == nil {
		m.reclaimBlobs()
		_ = m.repo.WALCheckpoint()
		_ = m.repo.Vacuum()
	}
	return n, err
}

func (m *Manager) DeleteExpiredImports(now time.Time) (int64, error) {
	n, err := m.archive.DeleteExpiredArchiveImports(now)
	if err == nil && n > 0 {
		m.reclaimBlobs()
		_ = m.repo.WALCheckpoint()
		_ = m.repo.Vacuum()
	}
	return n, err
}

func extractArchive(archivePath string) (string, Manifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", Manifest{}, err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f, zstd.WithDecoderMaxMemory(uint64(maxArchiveEntryBytes)))
	if err != nil {
		return "", Manifest{}, fmt.Errorf("open zstd archive: %w", err)
	}
	defer zr.Close()
	tempDir, err := os.MkdirTemp("", "prismcat-import-*")
	if err != nil {
		return "", Manifest{}, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tempDir)
		}
	}()
	tr := tar.NewReader(zr)
	var total int64
	var entries int
	seen := make(map[string]struct{})
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", Manifest{}, err
		}
		entries++
		if entries > maxArchiveEntries {
			return "", Manifest{}, errors.New("archive contains too many entries")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return "", Manifest{}, fmt.Errorf("archive entry %q is not a regular file", header.Name)
		}
		name, err := validateArchiveEntryName(header.Name)
		if err != nil {
			return "", Manifest{}, err
		}
		if _, exists := seen[name]; exists {
			return "", Manifest{}, fmt.Errorf("duplicate archive entry %q", name)
		}
		seen[name] = struct{}{}
		if header.Size < 0 || header.Size > maxArchiveEntryBytes || total > maxArchiveTotalBytes-header.Size {
			return "", Manifest{}, fmt.Errorf("archive entry %q exceeds size limit", name)
		}
		total += header.Size
		dest := filepath.Join(tempDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return "", Manifest{}, err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			return "", Manifest{}, err
		}
		_, copyErr := io.CopyN(out, tr, header.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return "", Manifest{}, copyErr
		}
		if closeErr != nil {
			return "", Manifest{}, closeErr
		}
	}
	if _, exists := seen["manifest.json"]; !exists {
		return "", Manifest{}, errors.New("archive is missing manifest.json")
	}
	if _, exists := seen["logs.jsonl"]; !exists {
		return "", Manifest{}, errors.New("archive is missing logs.jsonl")
	}
	manifestData, err := os.ReadFile(filepath.Join(tempDir, "manifest.json"))
	if err != nil {
		return "", Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateExtractedContent(tempDir, manifest); err != nil {
		return "", Manifest{}, err
	}
	if info, err := os.Stat(archivePath); err == nil && manifest.CompressedBytes > 0 && info.Size() != manifest.CompressedBytes {
		return "", Manifest{}, fmt.Errorf("archive compressed size mismatch: got %d, want %d", info.Size(), manifest.CompressedBytes)
	}
	ok = true
	return tempDir, manifest, nil
}

func validateExtractedContent(tempDir string, manifest Manifest) error {
	logsPath := filepath.Join(tempDir, "logs.jsonl")
	logsFile, err := os.Open(logsPath)
	if err != nil {
		return err
	}
	defer logsFile.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(logsFile)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	refs := make([]string, 0)
	seenRefs := make(map[string]struct{})
	var logCount int64
	for scanner.Scan() {
		line := append(append([]byte(nil), scanner.Bytes()...), '\n')
		_, _ = hash.Write(line)
		var logEntry storage.RequestLog
		if err := json.Unmarshal(scanner.Bytes(), &logEntry); err != nil {
			return fmt.Errorf("validate logs.jsonl: %w", err)
		}
		logCount++
		for _, body := range logEntry.Bodies {
			if _, ok := seenRefs[body.BlobRef]; ok {
				continue
			}
			if _, err := blobHex(body.BlobRef); err != nil {
				return err
			}
			seenRefs[body.BlobRef] = struct{}{}
			refs = append(refs, body.BlobRef)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	sort.Strings(refs)
	logsInfo, err := os.Stat(logsPath)
	if err != nil {
		return err
	}
	logicalBytes := logsInfo.Size()
	for _, ref := range refs {
		hexRef, _ := blobHex(ref)
		data, err := os.ReadFile(filepath.Join(tempDir, "bodies", hexRef))
		if err != nil {
			return fmt.Errorf("archive is missing body %s: %w", ref, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hexRef {
			return fmt.Errorf("body %s checksum mismatch", ref)
		}
		_, _ = hash.Write([]byte(ref))
		_, _ = hash.Write(data)
		logicalBytes += int64(len(data))
	}
	if logCount != manifest.LogCount {
		return fmt.Errorf("manifest log count mismatch: got %d, want %d", logCount, manifest.LogCount)
	}
	if int64(len(refs)) != manifest.BodyCount {
		return fmt.Errorf("manifest body count mismatch: got %d, want %d", len(refs), manifest.BodyCount)
	}
	if logicalBytes != manifest.LogicalBytes {
		return fmt.Errorf("manifest logical size mismatch: got %d, want %d", logicalBytes, manifest.LogicalBytes)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, manifest.ContentSHA256) {
		return fmt.Errorf("manifest content SHA-256 mismatch: got %s, want %s", got, manifest.ContentSHA256)
	}
	return nil
}

func validateArchiveEntryName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	if name == "manifest.json" || name == "logs.jsonl" {
		return name, nil
	}
	if strings.HasPrefix(name, "bodies/") {
		hexRef := strings.TrimPrefix(name, "bodies/")
		decoded, err := hex.DecodeString(hexRef)
		if err == nil && len(decoded) == sha256.Size && !strings.Contains(hexRef, "/") {
			return "bodies/" + strings.ToLower(hexRef), nil
		}
	}
	return "", fmt.Errorf("unsupported archive entry %q", name)
}
