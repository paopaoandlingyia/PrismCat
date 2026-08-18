package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestFileBlobStoreZstdEnvelopeRoundTripAndDedup(t *testing.T) {
	store, err := NewFileBlobStoreWithCompression(t.TempDir(), "zstd", 3)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte(`{"message":"compress me"}`), 4096)
	ref, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if ref2 != ref {
		t.Fatalf("dedup refs differ: %s != %s", ref, ref2)
	}
	_, hexRef, _ := parseBlobRef(ref)
	stored, err := os.ReadFile(store.pathFor(hexRef))
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) >= len(body) {
		t.Fatalf("stored bytes = %d, want less than %d", len(stored), len(body))
	}
	if len(stored) < blobEnvelopeSize || !bytes.Equal(stored[:4], blobEnvelopeMagic[:]) || stored[5] != blobCodecZstd {
		t.Fatalf("blob is not a zstd envelope: %x", stored[:min(len(stored), 16)])
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("round trip body mismatch")
	}
}

func TestFileBlobStoreFallsBackToRawEnvelopeAndReadsLegacy(t *testing.T) {
	store, err := NewFileBlobStoreWithCompression(t.TempDir(), "zstd", 19)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0x13, 0x37, 0x42}
	ref, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	_, hexRef, _ := parseBlobRef(ref)
	stored, err := os.ReadFile(store.pathFor(hexRef))
	if err != nil {
		t.Fatal(err)
	}
	if stored[5] != blobCodecNone {
		t.Fatalf("codec = %d, want raw fallback", stored[5])
	}

	legacy := []byte("legacy raw blob")
	sum := newSHA256RefForTest(legacy)
	_, legacyHex, _ := parseBlobRef(sum)
	if err := os.MkdirAll(store.baseDir+string(os.PathSeparator)+legacyHex[:2], 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.pathFor(legacyHex), legacy, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), sum)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, legacy) {
		t.Fatal("legacy body mismatch")
	}
}

func newSHA256RefForTest(data []byte) string {
	return "sha256:" + fmt.Sprintf("%x", sha256.Sum256(data))
}

// 引用列表为空但仓库非空,曾经会把整个 blob 仓库删空 —— 只要进程被指向另一个
// 数据库(还原备份、换库测试、配置写错),ListBlobRefs 就返回空列表,而 GC 照
// 字面执行。blob 目录的路径不是从数据库路径推导的,两者只是约定配对。
func TestGarbageCollectRefusesEmptyRefSet(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	ref, err := store.Put(ctx, []byte("payload that belongs to another database"))
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(ctx, nil, 0)
	if !errors.Is(err, ErrRefSetEmpty) {
		t.Fatalf("want ErrRefSetEmpty, got %v", err)
	}
	if deleted != 0 {
		t.Fatalf("want 0 deleted, got %d", deleted)
	}

	// 数据必须还在
	if _, err := store.Get(ctx, ref); err != nil {
		t.Fatalf("blob was deleted despite the guard: %v", err)
	}
}

// 仓库本来就是空的时候不该报错 —— 那是全新安装的正常状态。
func TestGarbageCollectEmptyRefSetOnEmptyStore(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("want 0 deleted, got %d", deleted)
	}
}

// 守卫不能挡住正常回收:引用列表非空时,未被引用的 blob 照删。
func TestGarbageCollectStillCollectsUnreferenced(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	keep, err := store.Put(ctx, []byte("still referenced"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put(ctx, []byte("no longer referenced"))
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := store.GarbageCollect(ctx, []string{keep}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("want 1 deleted, got %d", deleted)
	}
	if _, err := store.Get(ctx, keep); err != nil {
		t.Fatalf("referenced blob was deleted: %v", err)
	}
	if _, err := store.Get(ctx, orphan); err == nil {
		t.Fatal("orphan blob should have been deleted")
	}
}
