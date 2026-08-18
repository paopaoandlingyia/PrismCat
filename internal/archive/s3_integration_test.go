package archive

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func TestS3ArchiveAndImportIntegration(t *testing.T) {
	credentialsFile := os.Getenv("PRISMCAT_S3_TEST_CREDENTIALS")
	if credentialsFile == "" {
		t.Skip("PRISMCAT_S3_TEST_CREDENTIALS is not set")
	}
	s3Config, err := readTestS3Config(credentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	repo, blobs := newArchiveTestStorage(t, "s3-source")
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().In(loc).Add(-time.Minute)
	body := []byte(`{"s3_archive_smoke":"ok"}`)
	ref, err := blobs.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	logEntry := &storage.RequestLog{
		ID:        "s3-smoke-" + time.Now().UTC().Format("20060102T150405.000000000"),
		CreatedAt: createdAt, Upstream: "smoke", TargetURL: "https://example.invalid",
		Method: "POST", Path: "/archive-smoke", RequestBodySize: int64(len(body)), StatusCode: 200,
		Bodies: []storage.LogBody{{Part: storage.BodyPartRequest, BlobRef: ref, CapturedBytes: int64(len(body)), TotalBytes: int64(len(body)), Representation: "wire", Recoverable: true}},
	}
	if err := repo.SaveLog(logEntry); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveLogAnnotation(logEntry.ID, storage.LogAnnotation{Saved: true, Note: "S3 restore annotation", Labels: []string{"s3-smoke"}}); err != nil {
		t.Fatal(err)
	}
	ordinary := &storage.RequestLog{
		ID: "s3-ordinary-" + time.Now().UTC().Format("20060102T150405.000000000"), CreatedAt: createdAt.Add(time.Second),
		Upstream: "smoke", TargetURL: "https://example.invalid", Method: "GET", Path: "/archive-cleanup-smoke", StatusCode: 204,
	}
	if err := repo.SaveLog(ordinary); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Archive: config.ArchiveConfig{
		Enabled: true, S3: s3Config, KeyPrefix: "backups/prismcat/${yyyy}/${MM}-${dd}",
		ScheduleTime: "02:00", Timezone: "Asia/Shanghai", ZstdLevel: 10, LocalRetentionHours: 1, ImportRetentionHours: 24,
	}}
	manager, err := NewManager(cfg, repo, blobs)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.RunBlocking(context.Background(), "manual", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Status != "complete" || job.PackageCount != 1 || job.LogCount != 2 {
		t.Fatalf("job = %#v", job)
	}
	batches, err := repo.ListArchiveBatches(1)
	if err != nil || len(batches) != 1 {
		t.Fatalf("batches = %#v, err=%v", batches, err)
	}
	batch := batches[0]
	if batch.Status != "verified" || batch.ObjectKey == "" || batch.ManifestKey == "" {
		t.Fatalf("batch = %#v", batch)
	}
	store, err := newS3Store(s3Config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, key := range []string{batch.ObjectKey, batch.ManifestKey} {
			_, _ = store.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)})
		}
	})
	objects, err := store.list(context.Background(), objectDatePrefix(cfg.Archive.KeyPrefix, createdAt))
	if err != nil || len(objects) != 1 || objects[0].Key != batch.ObjectKey {
		t.Fatalf("date listing = %#v, err=%v", objects, err)
	}
	deleted, err := manager.CleanupEligible(time.Now().Add(2 * time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("delayed cleanup = %d, %v", deleted, err)
	}
	if _, err := repo.GetLog(logEntry.ID); err != nil {
		t.Fatalf("saved log was deleted: %v", err)
	}
	if _, err := repo.GetLog(ordinary.ID); err == nil {
		t.Fatal("ordinary verified log was not deleted after grace")
	}
	targetRepo, targetBlobs := newArchiveTestStorage(t, "s3-target")
	targetManager, err := NewManager(cfg, targetRepo, targetBlobs)
	if err != nil {
		t.Fatal(err)
	}
	imports, err := targetManager.ImportDate(context.Background(), createdAt.Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 1 || imports[0].Status != "complete" || imports[0].LogCount != 2 {
		t.Fatalf("imports = %#v", imports)
	}
	got, err := targetRepo.GetLog(logEntry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != "archive_import" || !got.Annotation.Saved || got.Annotation.Note != "S3 restore annotation" {
		t.Fatalf("restored log = %#v", got)
	}
	if got, err := targetRepo.GetLog(ordinary.ID); err != nil || got.Origin != "archive_import" {
		t.Fatalf("restored ordinary log = %#v, %v", got, err)
	}
}

func readTestS3Config(path string) (config.ArchiveS3Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return config.ArchiveS3Config{}, err
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return config.ArchiveS3Config{}, err
	}
	forcePathStyle, _ := strconv.ParseBool(firstNonEmpty(values["force_path_style"], values["path_style"]))
	return config.ArchiveS3Config{
		Endpoint: firstNonEmpty(values["endpoint"], values["s3_endpoint"]),
		Region:   values["region"], Bucket: values["bucket"],
		AccessKeyID:     firstNonEmpty(values["access_key_id"], values["access_key"]),
		SecretAccessKey: firstNonEmpty(values["secret_access_key"], values["secret_key"]),
		ForcePathStyle:  forcePathStyle,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
