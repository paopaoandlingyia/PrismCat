package storageusage

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

type Usage struct {
	CalculatedAt  time.Time `json:"calculated_at"`
	BlobStore     string    `json:"blob_store"`
	DatabaseBytes int64     `json:"database_bytes"`
	DatabaseFiles int64     `json:"database_files"`
	BlobBytes     int64     `json:"blob_bytes"`
	BlobFiles     int64     `json:"blob_files"`
	TotalBytes    int64     `json:"total_bytes"`
}

func Calculate(storageCfg config.StorageConfig) (Usage, error) {
	usage := Usage{
		CalculatedAt: time.Now(),
		BlobStore:    storageCfg.BlobStore,
	}
	if usage.BlobStore == "" {
		usage.BlobStore = "fs"
	}

	dbBytes, dbFiles, err := databaseUsage(storageCfg.Database)
	if err != nil {
		return usage, err
	}
	usage.DatabaseBytes = dbBytes
	usage.DatabaseFiles = dbFiles

	if usage.BlobStore == "fs" && strings.TrimSpace(storageCfg.BlobDir) != "" {
		blobBytes, blobFiles, err := dirUsage(storageCfg.BlobDir)
		if err != nil {
			return usage, err
		}
		usage.BlobBytes = blobBytes
		usage.BlobFiles = blobFiles
	}

	usage.TotalBytes = usage.DatabaseBytes + usage.BlobBytes
	return usage, nil
}

func databaseUsage(path string) (int64, int64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, 0, nil
	}

	paths := []string{path, path + "-wal", path + "-shm"}
	var total int64
	var files int64
	for _, item := range paths {
		info, err := os.Stat(item)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, 0, err
		}
		if info.IsDir() {
			continue
		}
		total += info.Size()
		files++
	}
	return total, files, nil
}

func dirUsage(root string) (int64, int64, error) {
	var total int64
	var files int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		total += info.Size()
		files++
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return total, files, nil
}
