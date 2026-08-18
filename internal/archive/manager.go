package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

var ErrArchiveBusy = errors.New("archive operation is already running")

const archiveFormatVersion = 1

type Manifest struct {
	FormatVersion   int       `json:"format_version"`
	CreatedAt       time.Time `json:"created_at"`
	RangeStart      time.Time `json:"range_start"`
	RangeEnd        time.Time `json:"range_end"`
	Timezone        string    `json:"timezone"`
	Compression     string    `json:"compression"`
	ZstdLevel       int       `json:"zstd_level"`
	LogCount        int64     `json:"log_count"`
	BodyCount       int64     `json:"body_count"`
	LogicalBytes    int64     `json:"logical_bytes"`
	CompressedBytes int64     `json:"compressed_bytes"`
	ContentSHA256   string    `json:"content_sha256"`
}

type SidecarManifest struct {
	Manifest
	BatchID         string    `json:"batch_id"`
	ObjectKey       string    `json:"object_key"`
	PackageSHA256   string    `json:"package_sha256"`
	BackupStartedAt time.Time `json:"backup_started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	VerifiedAt      time.Time `json:"verified_at"`
}

type Status struct {
	Enabled            bool                    `json:"enabled"`
	KeyPrefix          string                  `json:"key_prefix"`
	ScheduleTime       string                  `json:"schedule_time"`
	Timezone           string                  `json:"timezone"`
	Running            bool                    `json:"running"`
	S3Reachable        bool                    `json:"s3_reachable"`
	S3Error            string                  `json:"s3_error,omitempty"`
	PendingDate        string                  `json:"pending_date,omitempty"`
	PendingDeleteCount int64                   `json:"pending_delete_count"`
	EarliestDeleteAt   *time.Time              `json:"earliest_delete_at,omitempty"`
	Jobs               []storage.ArchiveJob    `json:"jobs"`
	Batches            []storage.ArchiveBatch  `json:"batches"`
	Imports            []storage.ArchiveImport `json:"imports"`
	Objects            []S3Object              `json:"objects"`
}

type Manager struct {
	cfg     *config.Config
	repo    storage.Repository
	archive storage.ArchiveRepository
	blobs   storage.BlobStore
	store   func(config.ArchiveS3Config) (objectStore, error)
	mu      sync.Mutex
	running bool
}

type objectStore interface {
	test(context.Context, string) error
	upload(context.Context, string, string, string, string) (int64, error)
	uploadBytes(context.Context, string, []byte, string) error
	list(context.Context, string) ([]S3Object, error)
	download(context.Context, string, string) error
	readBytes(context.Context, string, int64) ([]byte, error)
}

func NewManager(cfg *config.Config, repo storage.Repository, blobs storage.BlobStore) (*Manager, error) {
	archiveRepo, ok := repo.(storage.ArchiveRepository)
	if !ok {
		return nil, errors.New("repository does not support archives")
	}
	if err := archiveRepo.RecoverInterruptedArchiveWork(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("recover interrupted archive work: %w", err)
	}
	return &Manager{cfg: cfg, repo: repo, archive: archiveRepo, blobs: blobs, store: func(cfg config.ArchiveS3Config) (objectStore, error) {
		return newS3Store(cfg)
	}}, nil
}

func (m *Manager) begin() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *Manager) end() {
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}

func (m *Manager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *Manager) TestConnection(ctx context.Context, candidate config.ArchiveConfig) error {
	if err := config.ValidateArchiveConfig(candidate); err != nil {
		return err
	}
	candidate = config.NormalizeArchiveConfig(candidate)
	store, err := m.store(candidate.S3)
	if err != nil {
		return err
	}
	return store.test(ctx, config.ResolveArchiveKeyPrefix(candidate.KeyPrefix, time.Now()))
}

func (m *Manager) Status(ctx context.Context, includeS3 bool, date string) (Status, error) {
	cfg := m.cfg.ArchiveSnapshot()
	status := Status{
		Enabled: cfg.Enabled, KeyPrefix: cfg.KeyPrefix, ScheduleTime: cfg.ScheduleTime,
		Timezone: cfg.Timezone, Running: m.isRunning(),
	}
	var err error
	status.Jobs, err = m.archive.ListArchiveJobs(1)
	if err != nil {
		return status, err
	}
	status.PendingDeleteCount, status.EarliestDeleteAt, _ = m.archive.PendingBackedLogCleanup()
	if status.EarliestDeleteAt != nil {
		value := status.EarliestDeleteAt.Add(time.Duration(cfg.LocalRetentionHours) * time.Hour)
		status.EarliestDeleteAt = &value
	}
	loc, locErr := time.LoadLocation(cfg.Timezone)
	if locErr == nil {
		today := startOfDay(time.Now().In(loc), loc)
		oldest, oldestErr := m.archive.OldestUnarchivedLogTime(today)
		if oldestErr == nil && oldest != nil {
			status.PendingDate = startOfDay(oldest.In(loc), loc).Format("2006-01-02")
		}
	}
	if includeS3 && cfg.S3.AccessKeyID != "" && cfg.S3.SecretAccessKey != "" {
		store, storeErr := m.store(cfg.S3)
		if storeErr != nil {
			status.S3Error = storeErr.Error()
		} else {
			day := time.Now().In(loc)
			if date != "" {
				parsed, parseErr := time.ParseInLocation("2006-01-02", date, loc)
				if parseErr != nil {
					status.S3Error = "invalid archive date"
					return status, nil
				}
				day = parsed
			}
			objects, listErr := store.list(ctx, objectDatePrefix(cfg.KeyPrefix, day))
			if listErr != nil {
				status.S3Error = listErr.Error()
			} else {
				status.S3Reachable = true
				status.Objects = objects
			}
		}
	}
	return status, nil
}

func (m *Manager) ListBatches(filter storage.ArchiveBatchFilter) ([]storage.ArchiveBatch, int64, error) {
	return m.archive.ListArchiveBatchesPage(filter)
}

func (m *Manager) ListJobs(offset, limit int) ([]storage.ArchiveJob, int64, error) {
	return m.archive.ListArchiveJobsPage(offset, limit)
}

func (m *Manager) ListImports(offset, limit int) ([]storage.ArchiveImport, int64, error) {
	return m.archive.ListArchiveImportsPage(offset, limit)
}

func (m *Manager) StartManual() (*storage.ArchiveJob, error) {
	return m.start("manual", time.Now())
}

func (m *Manager) StartScheduled(now time.Time) (*storage.ArchiveJob, error) {
	cfg := m.cfg.ArchiveSnapshot()
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, err
	}
	return m.start("scheduled", startOfDay(now.In(loc), loc))
}

func (m *Manager) RunBlocking(ctx context.Context, trigger string, cutoff time.Time) (*storage.ArchiveJob, error) {
	if !m.begin() {
		return nil, ErrArchiveBusy
	}
	defer m.end()
	cfg := m.cfg.ArchiveSnapshot()
	if !cfg.Enabled {
		return nil, errors.New("S3 backup is disabled")
	}
	if err := config.ValidateArchiveConfig(cfg); err != nil {
		return nil, err
	}
	job := storage.ArchiveJob{ID: uuid.NewString(), Trigger: trigger, Cutoff: cutoff.UTC(), Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := m.archive.CreateArchiveJob(job); err != nil {
		return nil, err
	}
	err := m.runJob(ctx, &job, cfg)
	return &job, err
}

func (m *Manager) start(trigger string, cutoff time.Time) (*storage.ArchiveJob, error) {
	if !m.begin() {
		return nil, ErrArchiveBusy
	}
	cfg := m.cfg.ArchiveSnapshot()
	if !cfg.Enabled {
		m.end()
		return nil, errors.New("S3 backup is disabled")
	}
	if err := config.ValidateArchiveConfig(cfg); err != nil {
		m.end()
		return nil, err
	}
	job := storage.ArchiveJob{ID: uuid.NewString(), Trigger: trigger, Cutoff: cutoff.UTC(), Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := m.archive.CreateArchiveJob(job); err != nil {
		m.end()
		return nil, err
	}
	go func() {
		defer m.end()
		_ = m.runJob(context.Background(), &job, cfg)
	}()
	return &job, nil
}

func (m *Manager) runJob(ctx context.Context, job *storage.ArchiveJob, cfg config.ArchiveConfig) error {
	job.Status = "running"
	_ = m.archive.UpdateArchiveJob(*job)
	if flusher, ok := m.repo.(interface{ Flush(context.Context) error }); ok {
		if err := flusher.Flush(ctx); err != nil {
			return m.failJob(job, err)
		}
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return m.failJob(job, err)
	}
	for {
		oldest, err := m.archive.OldestUnarchivedLogTime(job.Cutoff)
		if err != nil {
			return m.failJob(job, err)
		}
		if oldest == nil {
			break
		}
		start := startOfDay(oldest.In(loc), loc)
		end := start.AddDate(0, 0, 1)
		if end.After(job.Cutoff) {
			end = job.Cutoff.In(loc)
		}
		batch, err := m.archiveDay(ctx, cfg, job.ID, job.CreatedAt, start, end)
		if err != nil {
			return m.failJob(job, err)
		}
		if batch.LogCount == 0 {
			break
		}
		job.PackageCount++
		job.LogCount += batch.LogCount
		_ = m.archive.UpdateArchiveJob(*job)
	}
	now := time.Now().UTC()
	job.Status = "complete"
	job.CompletedAt = &now
	job.Error = ""
	return m.archive.UpdateArchiveJob(*job)
}

func (m *Manager) failJob(job *storage.ArchiveJob, err error) error {
	now := time.Now().UTC()
	job.Status = "failed"
	job.Error = err.Error()
	job.CompletedAt = &now
	_ = m.archive.UpdateArchiveJob(*job)
	return err
}

func (m *Manager) archiveDay(ctx context.Context, cfg config.ArchiveConfig, jobID string, backupStartedAt, start, end time.Time) (*storage.ArchiveBatch, error) {
	batch := storage.ArchiveBatch{
		ID: uuid.NewString(), JobID: jobID, ArchiveDate: start.Format("2006-01-02"), RangeStart: start.UTC(), RangeEnd: end.UTC(), Status: "building",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := m.archive.CreateArchiveBatch(batch); err != nil {
		return nil, err
	}
	fail := func(err error) (*storage.ArchiveBatch, error) {
		batch.Status = "failed"
		batch.Error = err.Error()
		_ = m.archive.UpdateArchiveBatch(batch)
		_ = m.archive.ReleaseArchiveBatchLogs(batch.ID)
		return &batch, err
	}
	reserved, err := m.archive.ReserveArchiveBatchLogs(batch.ID, start, end)
	if err != nil {
		return fail(err)
	}
	if reserved == 0 {
		batch.Status = "empty"
		_ = m.archive.UpdateArchiveBatch(batch)
		return &batch, nil
	}

	build, err := m.build(ctx, cfg, batch.ID, start, end)
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(build.tempDir)
	batch.LogCount = build.manifest.LogCount
	batch.BodyCount = build.manifest.BodyCount
	batch.LogicalBytes = build.manifest.LogicalBytes
	batch.CompressedBytes = build.manifest.CompressedBytes
	batch.SHA256 = build.packageSHA
	batch.ObjectKey = objectKey(cfg.KeyPrefix, start, backupStartedAt, batch.ID)
	batch.ManifestKey = batch.ObjectKey + ".manifest.json"
	batch.Status = "uploading"
	if err := m.archive.UpdateArchiveBatch(batch); err != nil {
		return fail(err)
	}

	store, err := m.store(cfg.S3)
	if err != nil {
		return fail(err)
	}
	uploadedBytes, err := store.upload(ctx, batch.ObjectKey, build.path, batch.SHA256, "application/zstd")
	if err != nil {
		return fail(err)
	}
	if uploadedBytes != batch.CompressedBytes {
		return fail(fmt.Errorf("verified size %d differs from archive size %d", uploadedBytes, batch.CompressedBytes))
	}
	now := time.Now().UTC()
	sidecar := SidecarManifest{
		Manifest: build.manifest, BatchID: batch.ID, ObjectKey: batch.ObjectKey,
		PackageSHA256: batch.SHA256, BackupStartedAt: backupStartedAt, CompletedAt: now, VerifiedAt: now,
	}
	sidecarData, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return fail(err)
	}
	sidecarData = append(sidecarData, '\n')
	if err := store.uploadBytes(ctx, batch.ManifestKey, sidecarData, "application/json"); err != nil {
		return fail(fmt.Errorf("upload backup manifest: %w", err))
	}
	if _, err := m.archive.MarkArchiveBatchVerified(batch.ID, now); err != nil {
		return fail(fmt.Errorf("mark backup verified: %w", err))
	}
	batch.Status = "verified"
	batch.VerifiedAt = &now
	return &batch, nil
}

func (m *Manager) CleanupEligible(now time.Time) (int64, error) {
	cfg := m.cfg.ArchiveSnapshot()
	cutoff := now.Add(-time.Duration(cfg.LocalRetentionHours) * time.Hour)
	var total int64
	for {
		n, err := m.archive.DeleteEligibleBackedLogs(cutoff, 2000)
		if err != nil {
			return total, err
		}
		total += n
		if n < 2000 {
			break
		}
	}
	if total > 0 {
		_ = m.repo.WALCheckpoint()
		_ = m.repo.Vacuum()
		m.reclaimBlobs()
	}
	return total, nil
}

func (m *Manager) DeletionPreview(hours int) (int64, error) {
	if hours < 1 {
		return 0, errors.New("hours must be at least 1")
	}
	return m.archive.CountEligibleBackedLogs(time.Now().Add(-time.Duration(hours) * time.Hour))
}

type archiveBuild struct {
	tempDir    string
	path       string
	manifest   Manifest
	packageSHA string
}

func (m *Manager) build(ctx context.Context, cfg config.ArchiveConfig, batchID string, start, end time.Time) (*archiveBuild, error) {
	tempDir, err := os.MkdirTemp("", "prismcat-archive-*")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempDir)
		}
	}()
	logsPath := filepath.Join(tempDir, "logs.jsonl")
	logsFile, err := os.OpenFile(logsPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(logsFile, hash))
	bodyFiles := make(map[string]string)
	manifest := Manifest{
		FormatVersion: archiveFormatVersion, CreatedAt: time.Now().UTC(), RangeStart: start.UTC(), RangeEnd: end.UTC(),
		Timezone: cfg.Timezone, Compression: "zstd", ZstdLevel: cfg.ZstdLevel,
	}
	err = m.archive.ExportArchiveBatch(ctx, batchID, func(logEntry *storage.RequestLog) error {
		logEntry.RequestBody = ""
		logEntry.RequestBodyOriginal = ""
		logEntry.RequestBodyFinal = ""
		logEntry.ResponseBody = ""
		if err := encoder.Encode(logEntry); err != nil {
			return err
		}
		manifest.LogCount++
		for _, body := range logEntry.Bodies {
			if body.BlobRef == "" {
				return fmt.Errorf("log %s body %s is not recoverable", logEntry.ID, body.Part)
			}
			if _, ok := bodyFiles[body.BlobRef]; ok {
				continue
			}
			data, err := m.blobs.Get(ctx, body.BlobRef)
			if err != nil {
				return fmt.Errorf("read log %s body %s: %w", logEntry.ID, body.Part, err)
			}
			hexRef, err := blobHex(body.BlobRef)
			if err != nil {
				return err
			}
			path := filepath.Join(tempDir, "body-"+hexRef)
			if err := os.WriteFile(path, data, 0600); err != nil {
				return err
			}
			bodyFiles[body.BlobRef] = path
			manifest.BodyCount++
			manifest.LogicalBytes += int64(len(data))
		}
		return nil
	})
	closeErr := logsFile.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if info, err := os.Stat(logsPath); err == nil {
		manifest.LogicalBytes += info.Size()
	}
	contentRefs := make([]string, 0, len(bodyFiles))
	for ref := range bodyFiles {
		contentRefs = append(contentRefs, ref)
	}
	sort.Strings(contentRefs)
	for _, ref := range contentRefs {
		data, err := os.ReadFile(bodyFiles[ref])
		if err != nil {
			return nil, err
		}
		_, _ = hash.Write([]byte(ref))
		_, _ = hash.Write(data)
	}
	manifest.ContentSHA256 = hex.EncodeToString(hash.Sum(nil))

	archivePath := filepath.Join(tempDir, "archive.tar.zst")
	if err := writeTarZstd(archivePath, manifest, logsPath, bodyFiles, cfg.ZstdLevel); err != nil {
		return nil, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	// Compressed size is self-referential inside the compressed stream. The
	// authoritative value is stored in the sidecar written after upload.
	manifest.CompressedBytes = info.Size()
	packageSHA, err := fileSHA256(archivePath)
	if err != nil {
		return nil, err
	}
	cleanup = false
	return &archiveBuild{tempDir: tempDir, path: archivePath, manifest: manifest, packageSHA: packageSHA}, nil
}

func writeTarZstd(outputPath string, manifest Manifest, logsPath string, bodyFiles map[string]string, level int) error {
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	zw, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)), zstd.WithEncoderCRC(true), zstd.WithEncoderConcurrency(1))
	if err != nil {
		_ = f.Close()
		return err
	}
	tw := tar.NewWriter(zw)
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestData = append(manifestData, '\n')
	if err := writeTarBytes(tw, "manifest.json", manifestData, manifest.CreatedAt); err != nil {
		return closeArchiveWriters(tw, zw, f, err)
	}
	if err := writeTarFile(tw, "logs.jsonl", logsPath, manifest.CreatedAt); err != nil {
		return closeArchiveWriters(tw, zw, f, err)
	}
	refs := make([]string, 0, len(bodyFiles))
	for ref := range bodyFiles {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		hexRef, _ := blobHex(ref)
		if err := writeTarFile(tw, "bodies/"+hexRef, bodyFiles[ref], manifest.CreatedAt); err != nil {
			return closeArchiveWriters(tw, zw, f, err)
		}
	}
	return closeArchiveWriters(tw, zw, f, nil)
}

func closeArchiveWriters(tw *tar.Writer, zw *zstd.Encoder, f *os.File, first error) error {
	if err := tw.Close(); first == nil {
		first = err
	}
	if err := zw.Close(); first == nil {
		first = err
	}
	if err := f.Close(); first == nil {
		first = err
	}
	return first
}

func writeTarBytes(tw *tar.Writer, name string, data []byte, modTime time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), ModTime: modTime}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func writeTarFile(tw *tar.Writer, name, path string, modTime time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size(), ModTime: modTime}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func blobHex(ref string) (string, error) {
	hexRef := strings.TrimPrefix(strings.TrimSpace(ref), "sha256:")
	decoded, err := hex.DecodeString(hexRef)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("invalid blob ref %q", ref)
	}
	return strings.ToLower(hexRef), nil
}

func objectKey(prefix string, day, startedAt time.Time, batchID string) string {
	started := startedAt.In(day.Location()).Format("20060102T150405-0700")
	return fmt.Sprintf("%s/prismcat-%s-%s-%s.tar.zst", config.ResolveArchiveKeyPrefix(prefix, day), day.Format("20060102"), started, batchID)
}

func objectDatePrefix(prefix string, day time.Time) string {
	return fmt.Sprintf("%s/prismcat-%s-", config.ResolveArchiveKeyPrefix(prefix, day), day.Format("20060102"))
}

func startOfDay(value time.Time, loc *time.Location) time.Time {
	y, m, d := value.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func (m *Manager) reclaimBlobs() {
	refRepo, refsOK := m.repo.(storage.BlobRefRepository)
	gc, gcOK := m.blobs.(interface {
		GarbageCollectConfirmed(context.Context, []string, time.Duration) (int, error)
	})
	if !refsOK || !gcOK {
		return
	}
	refs, err := refRepo.ListBlobRefs()
	if err != nil {
		return
	}
	// Leave a safety window for a body written by the async worker immediately
	// before its log_bodies transaction becomes visible.
	_, _ = gc.GarbageCollectConfirmed(context.Background(), refs, time.Hour)
}
