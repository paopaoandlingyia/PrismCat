package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

type failingBodyBlobStore struct{}

func (failingBodyBlobStore) Put(context.Context, []byte) (string, error) {
	return "", errors.New("disk full")
}
func (failingBodyBlobStore) Get(context.Context, string) ([]byte, error)  { return nil, ErrBlobNotFound }
func (failingBodyBlobStore) Exists(context.Context, string) (bool, error) { return false, nil }

func TestPrepareLogExternalizesAllBodyParts(t *testing.T) {
	store, err := NewFileBlobStoreWithCompression(t.TempDir(), "zstd", 3)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Logging: config.LoggingConfig{MaxRequestBody: 5 << 20, MaxResponseBody: 32 << 20}}
	entry := &RequestLog{
		ID: "all-parts", RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		ResponseHeaders: map[string][]string{"Content-Type": {"text/plain"}},
		RequestBodyRaw:  []byte(`{"current":true}`), RequestBodyOriginalRaw: []byte(`{"before":true}`),
		RequestBodyFinalRaw: []byte(`{"after":true}`), ResponseBodyRaw: []byte("response"),
		RequestBodySize: 16, ResponseBodySize: 8,
	}
	PrepareLogForPersistence(entry, cfg, store)
	if len(entry.Bodies) != 4 {
		t.Fatalf("body metadata count = %d, want 4", len(entry.Bodies))
	}
	for _, part := range []string{BodyPartRequest, BodyPartRequestOriginal, BodyPartRequestFinal, BodyPartResponse} {
		body, ok := entry.Body(part)
		if !ok || body.BlobRef == "" || body.Representation != "wire" {
			t.Fatalf("part %s metadata = %#v", part, body)
		}
		if _, err := store.Get(context.Background(), body.BlobRef); err != nil {
			t.Fatalf("read %s: %v", part, err)
		}
	}
	if entry.RequestBodyRaw != nil || entry.ResponseBodyRaw != nil {
		t.Fatal("raw capture buffers were not cleared")
	}
}

func TestPrepareLogRecordsBodyStorageFailure(t *testing.T) {
	cfg := &config.Config{Logging: config.LoggingConfig{MaxRequestBody: 1024, MaxResponseBody: 1024}}
	entry := &RequestLog{ID: "failed", RequestBodyRaw: []byte("body"), RequestBodySize: 4}
	PrepareLogForPersistence(entry, cfg, failingBodyBlobStore{})
	if entry.BodyStorageError == "" || !entry.Truncated {
		t.Fatalf("failure not recorded: %#v", entry)
	}
	body, ok := entry.Body(BodyPartRequest)
	if !ok || body.BlobRef != "" || body.Recoverable || !body.Truncated {
		t.Fatalf("body failure metadata = %#v", body)
	}
}

func TestSQLiteFreshSchemaHasNoBodyColumnsAndCascadesMetadata(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, column := range []string{"request_body", "request_body_original", "request_body_final", "request_body_ref", "response_body", "response_body_ref"} {
		has, err := repo.hasColumn("request_logs", column)
		if err != nil {
			t.Fatal(err)
		}
		if has {
			t.Fatalf("fresh request_logs unexpectedly has %s", column)
		}
	}
	entry := &RequestLog{ID: "cascade", Upstream: "test", TargetURL: "http://example.test", Method: "POST", Path: "/", Bodies: []LogBody{{Part: BodyPartRequest, BlobRef: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CapturedBytes: 1, TotalBytes: 1, Representation: "wire"}}}
	if err := repo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec("DELETE FROM request_logs WHERE id=?", entry.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repo.db.QueryRow("SELECT COUNT(*) FROM log_bodies WHERE log_id=?", entry.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("body rows after parent delete = %d", count)
	}
}
