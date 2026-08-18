package archive

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func TestBuildAndImportTarZstd(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	sourceRepo, sourceBlobs := newArchiveTestStorage(t, "source")
	body := []byte(`{"prompt":"compressible compressible compressible"}`)
	ref, err := sourceBlobs.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	entry := &storage.RequestLog{
		ID: "archive-log", CreatedAt: start.Add(time.Hour), Upstream: "openai", TargetURL: "https://example.test",
		Method: "POST", Path: "/v1/responses", RequestBodySize: int64(len(body)), StatusCode: 200,
		Bodies: []storage.LogBody{{Part: storage.BodyPartRequest, BlobRef: ref, CapturedBytes: int64(len(body)), TotalBytes: int64(len(body)), Representation: "wire", Recoverable: true}},
	}
	if err := sourceRepo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceRepo.SaveLogAnnotation(entry.ID, storage.LogAnnotation{
		Saved: true, Status: "todo", Note: "preserve this annotation", Labels: []string{"audit"},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Archive: config.ArchiveConfig{Timezone: "Asia/Shanghai", ZstdLevel: 10, ImportRetentionHours: 24}}
	manager, err := NewManager(cfg, sourceRepo, sourceBlobs)
	if err != nil {
		t.Fatal(err)
	}
	batchID := "build-test-batch"
	if err := sourceRepo.CreateArchiveBatch(storage.ArchiveBatch{ID: batchID, ArchiveDate: "2026-08-16", RangeStart: start, RangeEnd: start.AddDate(0, 0, 1), Status: "building"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceRepo.ReserveArchiveBatchLogs(batchID, start, start.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	build, err := manager.build(context.Background(), config.NormalizeArchiveConfig(cfg.Archive), batchID, start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(build.tempDir)
	if build.manifest.Compression != "zstd" || build.manifest.ZstdLevel != 10 || build.manifest.LogCount != 1 || build.manifest.BodyCount != 1 {
		t.Fatalf("manifest = %#v", build.manifest)
	}
	info, err := os.Stat(build.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != build.manifest.CompressedBytes {
		t.Fatalf("compressed size = %d, manifest = %d", info.Size(), build.manifest.CompressedBytes)
	}

	targetRepo, targetBlobs := newArchiveTestStorage(t, "target")
	targetManager, err := NewManager(cfg, targetRepo, targetBlobs)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := targetManager.ImportFile(context.Background(), build.path, "test.tar.zst")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Status != "complete" || imported.LogCount != 1 {
		t.Fatalf("import = %#v", imported)
	}
	got, err := targetRepo.GetLog(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != "archive_import" || got.ImportBatchID != imported.ID || len(got.Bodies) != 1 {
		t.Fatalf("imported log = %#v", got)
	}
	if !got.Annotation.Saved || got.Annotation.Status != "todo" || got.Annotation.Note != "preserve this annotation" ||
		len(got.Annotation.Labels) != 1 || got.Annotation.Labels[0] != "audit" {
		t.Fatalf("imported annotation = %#v", got.Annotation)
	}
	gotBody, err := targetBlobs.Get(context.Background(), got.Bodies[0].BlobRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBody) != string(body) {
		t.Fatal("imported body mismatch")
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.tar.zst")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	data := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "../outside", Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := extractArchive(archivePath); err == nil {
		t.Fatal("unsafe archive was accepted")
	}
}

func TestObjectKeyIncludesArchiveDateAndBackupStartTime(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	day := time.Date(2026, 8, 17, 0, 0, 0, 0, loc)
	started := time.Date(2026, 8, 17, 17, 10, 56, 0, time.UTC)
	key := objectKey("backups/prismcat/${yyyy}/${MM}-${dd}", day, started, "batch-id")
	want := "backups/prismcat/2026/08-17/prismcat-20260817-20260818T011056+0800-batch-id.tar.zst"
	if key != want {
		t.Fatalf("object key = %q, want %q", key, want)
	}
	prefix := objectDatePrefix("backups/prismcat/${yyyy}/${MM}-${dd}", day)
	for _, candidate := range []string{
		key,
		"backups/prismcat/2026/08-17/prismcat-20260817-old-batch-id.tar.zst",
	} {
		if !strings.HasPrefix(candidate, prefix) {
			t.Fatalf("date prefix %q does not discover %q", prefix, candidate)
		}
	}
}

func TestSidecarFailureDoesNotMarkLogsVerified(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	repo, blobs := newArchiveTestStorage(t, "sidecar-failure")
	if err := repo.SaveLog(&storage.RequestLog{ID: "not-committed", CreatedAt: start.Add(time.Hour), Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	cfg := archiveTestConfig()
	manager, err := NewManager(cfg, repo, blobs)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryObjectStore{objects: make(map[string][]byte), failSidecar: true}
	manager.store = func(config.ArchiveS3Config) (objectStore, error) { return store, nil }
	if _, err := manager.RunBlocking(context.Background(), "manual", start.Add(2*time.Hour)); err == nil {
		t.Fatal("archive unexpectedly succeeded after sidecar upload failure")
	}
	got, err := repo.GetLog("not-committed")
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupVerifiedAt != nil || got.BackupBatchID != "" {
		t.Fatalf("failed backup marked log verified: %#v", got)
	}
	batches, err := repo.ListArchiveBatches(1)
	if err != nil || len(batches) != 1 || batches[0].Status != "failed" {
		t.Fatalf("batches = %#v, err=%v", batches, err)
	}
}

func TestImportDateRestoresAllPackagesForTheDay(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	sourceRepo, sourceBlobs := newArchiveTestStorage(t, "multi-source")
	cfg := archiveTestConfig()
	store := &memoryObjectStore{objects: make(map[string][]byte)}
	sourceManager, err := NewManager(cfg, sourceRepo, sourceBlobs)
	if err != nil {
		t.Fatal(err)
	}
	sourceManager.store = func(config.ArchiveS3Config) (objectStore, error) { return store, nil }

	for index, item := range []struct {
		id      string
		created time.Time
		cutoff  time.Time
	}{{"first", start.Add(time.Hour), start.Add(2 * time.Hour)}, {"second", start.Add(3 * time.Hour), start.Add(4 * time.Hour)}} {
		if err := sourceRepo.SaveLog(&storage.RequestLog{ID: item.id, CreatedAt: item.created, Upstream: "test", TargetURL: "https://example.test", Method: "POST", Path: "/"}); err != nil {
			t.Fatal(err)
		}
		job, err := sourceManager.RunBlocking(context.Background(), "manual", item.cutoff)
		if err != nil || job.PackageCount != 1 || job.LogCount != 1 {
			t.Fatalf("backup %d = %#v, %v", index, job, err)
		}
	}
	manifestCount := 0
	for key, data := range store.objects {
		if !strings.HasSuffix(key, ".manifest.json") {
			continue
		}
		manifestCount++
		var sidecar SidecarManifest
		if err := json.Unmarshal(data, &sidecar); err != nil {
			t.Fatalf("decode sidecar %s: %v", key, err)
		}
		if sidecar.BackupStartedAt.IsZero() || sidecar.CompletedAt.IsZero() || !sidecar.CompletedAt.Equal(sidecar.VerifiedAt) {
			t.Fatalf("sidecar timestamps = %#v", sidecar)
		}
	}
	if manifestCount != 2 {
		t.Fatalf("sidecar count = %d, want 2", manifestCount)
	}

	targetRepo, targetBlobs := newArchiveTestStorage(t, "multi-target")
	targetManager, err := NewManager(cfg, targetRepo, targetBlobs)
	if err != nil {
		t.Fatal(err)
	}
	targetManager.store = func(config.ArchiveS3Config) (objectStore, error) { return store, nil }
	imports, err := targetManager.ImportDate(context.Background(), "2026-08-17")
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 2 {
		t.Fatalf("imports = %#v, want two packages", imports)
	}
	for _, id := range []string{"first", "second"} {
		got, err := targetRepo.GetLog(id)
		if err != nil || got.Origin != "archive_import" {
			t.Fatalf("restored %s = %#v, %v", id, got, err)
		}
	}
}

func TestDownloadVerifiedArchiveRejectsOversizeBeforeDownload(t *testing.T) {
	const (
		key   = "backups/prismcat/archive.tar.zst"
		limit = int64(8)
	)
	store := &memoryObjectStore{objects: map[string][]byte{
		key: []byte("0123456789"),
	}}
	store.objects[key+".manifest.json"] = testSidecarData(t, key, limit)
	archivePath := filepath.Join(t.TempDir(), "archive.tar.zst")

	_, err := downloadVerifiedArchiveWithLimit(context.Background(), store, key, archivePath, limit)
	if err == nil || !strings.Contains(err.Error(), "exceeds download limit") {
		t.Fatalf("error = %v, want download limit error", err)
	}
	if store.downloadCalls != 0 {
		t.Fatalf("download calls = %d, want 0", store.downloadCalls)
	}
}

func TestDownloadVerifiedArchiveEnforcesStreamLimit(t *testing.T) {
	const (
		key   = "backups/prismcat/archive.tar.zst"
		limit = int64(8)
	)
	store := &memoryObjectStore{
		objects:      map[string][]byte{key: []byte("0123456789")},
		sizeOverride: map[string]int64{key: limit},
	}
	store.objects[key+".manifest.json"] = testSidecarData(t, key, limit)
	archivePath := filepath.Join(t.TempDir(), "archive.tar.zst")

	_, err := downloadVerifiedArchiveWithLimit(context.Background(), store, key, archivePath, limit)
	if err == nil || !strings.Contains(err.Error(), "exceeds download limit") {
		t.Fatalf("error = %v, want download limit error", err)
	}
	if store.downloadCalls != 1 {
		t.Fatalf("download calls = %d, want 1", store.downloadCalls)
	}
}

func TestWriteLimitedDownloadRemovesPartialFile(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "archive.tar.zst")
	written, err := writeLimitedDownload(archivePath, strings.NewReader("0123456789"), 8)
	if err == nil || !strings.Contains(err.Error(), "exceeds download limit") {
		t.Fatalf("error = %v, want download limit error", err)
	}
	if written != 9 {
		t.Fatalf("written = %d, want 9", written)
	}
	if _, statErr := os.Stat(archivePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial file still exists: %v", statErr)
	}
}

func TestRunJobAbortsWhenRunningStatusUpdateFails(t *testing.T) {
	baseRepo, blobs := newArchiveTestStorage(t, "job-status-failure")
	repo := &failingArchiveUpdateRepository{
		SQLiteRepository: baseRepo,
		failJobUpdate: func(job storage.ArchiveJob) error {
			if job.Status == "running" {
				return errors.New("simulated job status failure")
			}
			return nil
		},
	}
	manager, err := NewManager(archiveTestConfig(), repo, blobs)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.RunBlocking(context.Background(), "manual", time.Now())
	if err == nil || !strings.Contains(err.Error(), "mark archive job running") {
		t.Fatalf("error = %v, want running status error", err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status = %q, want failed", job.Status)
	}
	jobs, listErr := baseRepo.ListArchiveJobs(1)
	if listErr != nil || len(jobs) != 1 || jobs[0].Status != "failed" || jobs[0].Error == "" {
		t.Fatalf("persisted jobs = %#v, error = %v", jobs, listErr)
	}
	batches, listErr := baseRepo.ListArchiveBatches(1)
	if listErr != nil || len(batches) != 0 {
		t.Fatalf("batches = %#v, error = %v", batches, listErr)
	}
}

func TestArchiveDayAbortsWhenUploadingStatusUpdateFails(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	baseRepo, blobs := newArchiveTestStorage(t, "batch-status-failure")
	if err := baseRepo.SaveLog(&storage.RequestLog{
		ID: "batch-status-log", CreatedAt: start.Add(time.Hour), Upstream: "test",
		TargetURL: "https://example.test", Method: "POST", Path: "/",
	}); err != nil {
		t.Fatal(err)
	}
	repo := &failingArchiveUpdateRepository{
		SQLiteRepository: baseRepo,
		failBatchUpdate: func(batch storage.ArchiveBatch) error {
			if batch.Status == "uploading" {
				return errors.New("simulated batch status failure")
			}
			return nil
		},
	}
	manager, err := NewManager(archiveTestConfig(), repo, blobs)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryObjectStore{objects: make(map[string][]byte)}
	manager.store = func(config.ArchiveS3Config) (objectStore, error) { return store, nil }

	job, err := manager.RunBlocking(context.Background(), "manual", start.Add(2*time.Hour))
	if err == nil || !strings.Contains(err.Error(), "simulated batch status failure") {
		t.Fatalf("error = %v, want batch status error", err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status = %q, want failed", job.Status)
	}
	if len(store.objects) != 0 {
		t.Fatalf("uploaded objects = %d, want 0", len(store.objects))
	}
	batches, listErr := baseRepo.ListArchiveBatches(1)
	if listErr != nil || len(batches) != 1 || batches[0].Status != "failed" || batches[0].Error == "" {
		t.Fatalf("batches = %#v, error = %v", batches, listErr)
	}
}

func testSidecarData(t *testing.T, key string, compressedBytes int64) []byte {
	t.Helper()
	data, err := json.Marshal(SidecarManifest{
		Manifest: Manifest{FormatVersion: archiveFormatVersion, CompressedBytes: compressedBytes},
		BatchID:  "test-batch", ObjectKey: key, PackageSHA256: strings.Repeat("0", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type failingArchiveUpdateRepository struct {
	*storage.SQLiteRepository
	failJobUpdate   func(storage.ArchiveJob) error
	failBatchUpdate func(storage.ArchiveBatch) error
}

func (r *failingArchiveUpdateRepository) UpdateArchiveJob(job storage.ArchiveJob) error {
	if r.failJobUpdate != nil {
		if err := r.failJobUpdate(job); err != nil {
			return err
		}
	}
	return r.SQLiteRepository.UpdateArchiveJob(job)
}

func (r *failingArchiveUpdateRepository) UpdateArchiveBatch(batch storage.ArchiveBatch) error {
	if r.failBatchUpdate != nil {
		if err := r.failBatchUpdate(batch); err != nil {
			return err
		}
	}
	return r.SQLiteRepository.UpdateArchiveBatch(batch)
}

func archiveTestConfig() *config.Config {
	return &config.Config{Archive: config.ArchiveConfig{
		Enabled:   true,
		S3:        config.ArchiveS3Config{Region: "test", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret"},
		KeyPrefix: "backups/prismcat/${yyyy}/${MM}-${dd}", ScheduleTime: "02:00", Timezone: "Asia/Shanghai",
		ZstdLevel: 10, LocalRetentionHours: 24, ImportRetentionHours: 24,
	}}
}

type memoryObjectStore struct {
	objects       map[string][]byte
	failSidecar   bool
	sizeOverride  map[string]int64
	downloadCalls int
}

func (s *memoryObjectStore) test(context.Context, string) error { return nil }

func (s *memoryObjectStore) upload(_ context.Context, key, filePath, _ string, _ string) (int64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	s.objects[key] = append([]byte(nil), data...)
	return int64(len(data)), nil
}

func (s *memoryObjectStore) uploadBytes(_ context.Context, key string, data []byte, _ string) error {
	if s.failSidecar && strings.HasSuffix(key, ".manifest.json") {
		return errors.New("simulated sidecar failure")
	}
	s.objects[key] = append([]byte(nil), data...)
	return nil
}

func (s *memoryObjectStore) list(_ context.Context, prefix string) ([]S3Object, error) {
	var objects []S3Object
	for key, data := range s.objects {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".tar.zst") {
			if _, committed := s.objects[key+".manifest.json"]; committed {
				objects = append(objects, S3Object{Key: key, Size: int64(len(data))})
			}
		}
	}
	return objects, nil
}

func (s *memoryObjectStore) size(_ context.Context, key string) (int64, error) {
	data, ok := s.objects[key]
	if !ok {
		return 0, os.ErrNotExist
	}
	if size, ok := s.sizeOverride[key]; ok {
		return size, nil
	}
	return int64(len(data)), nil
}

func (s *memoryObjectStore) download(_ context.Context, key, filePath string, limit int64) (int64, error) {
	s.downloadCalls++
	data, ok := s.objects[key]
	if !ok {
		return 0, os.ErrNotExist
	}
	if int64(len(data)) > limit {
		return limit + 1, fmt.Errorf("object exceeds download limit: read more than %d bytes", limit)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}

func (s *memoryObjectStore) readBytes(_ context.Context, key string, limit int64) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	if int64(len(data)) > limit {
		return append([]byte(nil), data[:limit+1]...), nil
	}
	return append([]byte(nil), data...), nil
}

func newArchiveTestStorage(t *testing.T, name string) (*storage.SQLiteRepository, *storage.FileBlobStore) {
	t.Helper()
	base := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(filepath.Join(base, "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	blobs, err := storage.NewFileBlobStoreWithCompression(filepath.Join(base, "blobs"), "zstd", 3)
	if err != nil {
		t.Fatal(err)
	}
	return repo, blobs
}
