package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyBodiesExternalizesAndDropsColumns(t *testing.T) {
	baseDir := t.TempDir()
	dbPath := filepath.Join(baseDir, "legacy.db")
	blobs, err := NewFileBlobStoreWithCompression(filepath.Join(baseDir, "blobs"), "zstd", 3)
	if err != nil {
		t.Fatal(err)
	}
	responseData := []byte(`{"result":"legacy ref"}`)
	responseRef, err := blobs.Put(context.Background(), responseData)
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE request_logs (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL,
			upstream TEXT NOT NULL,
			target_url TEXT NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			query TEXT DEFAULT '',
			request_headers TEXT,
			request_body TEXT,
			request_body_original TEXT,
			request_body_final TEXT,
			request_body_ref TEXT,
			request_body_size INTEGER DEFAULT 0,
			status_code INTEGER DEFAULT 0,
			response_headers TEXT,
			response_body TEXT,
			response_body_ref TEXT,
			response_body_size INTEGER DEFAULT 0,
			streaming INTEGER DEFAULT 0,
			latency_ms INTEGER DEFAULT 0,
			error TEXT DEFAULT '',
			truncated INTEGER DEFAULT 0
		);
		INSERT INTO request_logs (
			id, created_at, upstream, target_url, method, path,
			request_headers, request_body, request_body_original, request_body_final,
			request_body_size, status_code, response_headers, response_body,
			response_body_ref, response_body_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "legacy-body", "2026-08-16T10:00:00+08:00", "openai", "https://example.test/v1/responses",
		"POST", "/v1/responses", `{"Content-Type":["application/json"]}`, `{"prompt":"inline"}`,
		`{"prompt":"original"}`, `{"prompt":"final"}`, 19, 200,
		`{"Content-Type":["application/json"]}`, "legacy preview", responseRef, len(responseData))
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if err := repo.MigrateLegacyBodies(blobs); err != nil {
		t.Fatal(err)
	}
	if err := repo.MigrateLegacyBodies(blobs); err != nil {
		t.Fatalf("second migration should be a no-op: %v", err)
	}

	for _, column := range []string{"request_body", "request_body_original", "request_body_final", "request_body_ref", "response_body", "response_body_ref"} {
		has, err := repo.hasColumn("request_logs", column)
		if err != nil {
			t.Fatal(err)
		}
		if has {
			t.Fatalf("legacy column %s still exists", column)
		}
	}
	logEntry, err := repo.GetLog("legacy-body")
	if err != nil {
		t.Fatal(err)
	}
	if len(logEntry.Bodies) != 4 {
		t.Fatalf("body metadata count = %d, want 4", len(logEntry.Bodies))
	}
	want := map[string]string{
		BodyPartRequest:         `{"prompt":"inline"}`,
		BodyPartRequestOriginal: `{"prompt":"original"}`,
		BodyPartRequestFinal:    `{"prompt":"final"}`,
		BodyPartResponse:        string(responseData),
	}
	for _, body := range logEntry.Bodies {
		data, err := blobs.Get(context.Background(), body.BlobRef)
		if err != nil {
			t.Fatalf("load %s: %v", body.Part, err)
		}
		if string(data) != want[body.Part] {
			t.Fatalf("%s body = %q, want %q", body.Part, data, want[body.Part])
		}
		if body.Part == BodyPartResponse && body.Representation != "wire" {
			t.Fatalf("referenced response representation = %q", body.Representation)
		}
		if body.Part != BodyPartResponse && body.Representation != "display" {
			t.Fatalf("inline %s representation = %q", body.Part, body.Representation)
		}
	}
	if _, err := os.Stat(filepath.Join(baseDir, "blobs")); err != nil {
		t.Fatal(err)
	}
}
