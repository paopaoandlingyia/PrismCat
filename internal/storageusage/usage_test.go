package storageusage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestCalculateIncludesDatabaseSidecarsAndBlobs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "prismcat.db")
	blobDir := filepath.Join(dir, "blobs")

	writeFile(t, dbPath, 10)
	writeFile(t, dbPath+"-wal", 20)
	writeFile(t, dbPath+"-shm", 30)
	writeFile(t, filepath.Join(blobDir, "aa", "blob-one"), 40)
	writeFile(t, filepath.Join(blobDir, "bb", ".tmp-ignore"), 50)

	usage, err := Calculate(config.StorageConfig{
		Database:  dbPath,
		BlobStore: "fs",
		BlobDir:   blobDir,
	})
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if usage.DatabaseBytes != 60 {
		t.Fatalf("DatabaseBytes = %d, want 60", usage.DatabaseBytes)
	}
	if usage.DatabaseFiles != 3 {
		t.Fatalf("DatabaseFiles = %d, want 3", usage.DatabaseFiles)
	}
	if usage.BlobBytes != 40 {
		t.Fatalf("BlobBytes = %d, want 40", usage.BlobBytes)
	}
	if usage.BlobFiles != 1 {
		t.Fatalf("BlobFiles = %d, want 1", usage.BlobFiles)
	}
	if usage.TotalBytes != 100 {
		t.Fatalf("TotalBytes = %d, want 100", usage.TotalBytes)
	}
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
