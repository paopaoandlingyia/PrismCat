package storage

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/prismcat/prismcat/internal/config"
)

type memBlobStore struct {
	puts int
	data [][]byte
}

func (m *memBlobStore) Put(ctx context.Context, b []byte) (string, error) {
	_ = ctx
	m.puts++
	m.data = append(m.data, append([]byte(nil), b...))
	// Deterministic ref for tests.
	return "sha256:" + strings.Repeat("0", 64), nil
}

func (m *memBlobStore) Get(ctx context.Context, ref string) ([]byte, error) {
	return nil, ErrBlobNotFound
}
func (m *memBlobStore) Exists(ctx context.Context, ref string) (bool, error) { return false, nil }

func TestDetachingRepositoryDetachesLargeBodies(t *testing.T) {
	inner := &memRepo{}
	blobs := &memBlobStore{}

	cfg := &config.Config{}
	cfg.Logging.DetachBodyOverBytes = 8
	cfg.Logging.BodyPreviewBytes = 4

	repo := NewDetachingRepository(inner, blobs, cfg)

	entry := &RequestLog{
		ID:           "id",
		RequestBody:  "0123456789", // 10 bytes
		ResponseBody: "abcd",       // 4 bytes
	}

	if err := repo.SaveLog(entry); err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}

	if blobs.puts != 1 {
		t.Fatalf("blob puts = %d, want 1", blobs.puts)
	}
	if got := string(blobs.data[0]); got != "0123456789" {
		t.Fatalf("stored blob = %q, want %q", got, "0123456789")
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]
	if saved.RequestBodyRef == "" {
		t.Fatalf("RequestBodyRef is empty")
	}
	if saved.RequestBody != "0123" {
		t.Fatalf("RequestBody preview = %q, want %q", saved.RequestBody, "0123")
	}
	if saved.ResponseBodyRef != "" {
		t.Fatalf("ResponseBodyRef = %q, want empty", saved.ResponseBodyRef)
	}
	if saved.ResponseBody != "abcd" {
		t.Fatalf("ResponseBody = %q, want %q", saved.ResponseBody, "abcd")
	}
}

func TestDetachingRepositoryTruncateUTF8DoesNotSplitRunes(t *testing.T) {
	inner := &memRepo{}
	blobs := &memBlobStore{}

	cfg := &config.Config{}
	cfg.Logging.DetachBodyOverBytes = 1
	cfg.Logging.BodyPreviewBytes = 4 // 4 bytes would cut into the 2nd rune; should back off to a rune boundary.

	repo := NewDetachingRepository(inner, blobs, cfg)

	full := "\u4f60\u597d\u4e16\u754c" // "你好世界"
	entry := &RequestLog{
		ID:          "id",
		RequestBody: full,
	}

	if err := repo.SaveLog(entry); err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}

	if blobs.puts != 1 {
		t.Fatalf("blob puts = %d, want 1", blobs.puts)
	}
	if got := string(blobs.data[0]); got != full {
		t.Fatalf("stored blob = %q, want %q", got, full)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]

	wantPreview := "\u4f60" // "你"
	if saved.RequestBody != wantPreview {
		t.Fatalf("RequestBody preview = %q, want %q", saved.RequestBody, wantPreview)
	}
	if !utf8.ValidString(saved.RequestBody) {
		t.Fatalf("RequestBody preview is not valid UTF-8: %q", saved.RequestBody)
	}
	if len(saved.RequestBody) > int(cfg.Logging.BodyPreviewBytes) {
		t.Fatalf("RequestBody preview length = %d, want <= %d", len(saved.RequestBody), cfg.Logging.BodyPreviewBytes)
	}
}
