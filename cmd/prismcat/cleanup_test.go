package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storageusage"
)

func TestCleanupStorageLimitReclaimsBeforeDeletingLogs(t *testing.T) {
	usageBytes := int64(1200)
	repo := &fakeCleanupRepo{deletable: 100}
	gc := &fakeBlobGC{
		onGC: func() {
			usageBytes = 900
		},
	}

	result, err := cleanupStorageLimit(
		context.Background(),
		config.StorageConfig{MaxStorageBytes: 1000},
		repo,
		fakeBlobRefLister{},
		gc,
		func(config.StorageConfig) (storageusage.Usage, error) {
			return storageusage.Usage{TotalBytes: usageBytes}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("cleanupStorageLimit returned error: %v", err)
	}
	if result.DeletedLogs != 0 {
		t.Fatalf("DeletedLogs = %d, want 0", result.DeletedLogs)
	}
	if !result.RanBlobGC || gc.calls != 1 {
		t.Fatalf("blob GC calls = %d, RanBlobGC = %v; want one GC before delete", gc.calls, result.RanBlobGC)
	}
	if len(repo.deleteBatches) != 0 {
		t.Fatalf("delete batches = %v, want none", repo.deleteBatches)
	}
	if repo.checkpoints != 1 || repo.vacuums != 1 {
		t.Fatalf("checkpoints=%d vacuums=%d, want 1 and 1", repo.checkpoints, repo.vacuums)
	}
}

func TestCleanupStorageLimitSmallExcessDoesNotDeleteMinimumHundred(t *testing.T) {
	usageBytes := int64(1010)
	repo := &fakeCleanupRepo{
		deletable: 1000,
		onDelete: func(int) {
			usageBytes = 990
		},
	}

	result, err := cleanupStorageLimit(
		context.Background(),
		config.StorageConfig{MaxStorageBytes: 1000},
		repo,
		nil,
		nil,
		func(config.StorageConfig) (storageusage.Usage, error) {
			return storageusage.Usage{TotalBytes: usageBytes}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("cleanupStorageLimit returned error: %v", err)
	}
	if result.DeletedLogs != 12 {
		t.Fatalf("DeletedLogs = %d, want 12", result.DeletedLogs)
	}
	if got := repo.deleteBatches; len(got) != 1 || got[0] != 12 {
		t.Fatalf("delete batches = %v, want [12]", got)
	}
}

func TestCleanupStorageLimitDoesNotDeleteWhenBlobReclaimFailsAndDBIsUnderLimit(t *testing.T) {
	repo := &fakeCleanupRepo{deletable: 100}

	_, err := cleanupStorageLimit(
		context.Background(),
		config.StorageConfig{MaxStorageBytes: 1000},
		repo,
		errorBlobRefLister{},
		&fakeBlobGC{},
		func(config.StorageConfig) (storageusage.Usage, error) {
			return storageusage.Usage{
				DatabaseBytes: 900,
				BlobBytes:     300,
				TotalBytes:    1200,
			}, nil
		},
		nil,
	)
	if err == nil {
		t.Fatal("cleanupStorageLimit returned nil error, want blob reclaim failure")
	}
	if len(repo.deleteBatches) != 0 {
		t.Fatalf("delete batches = %v, want none", repo.deleteBatches)
	}
}

func TestStorageLimitDeleteBatchIsCapped(t *testing.T) {
	if got := storageLimitDeleteBatch(2000, 1000, 100000); got != storageLimitDeleteMaxBatch {
		t.Fatalf("batch = %d, want max batch %d", got, storageLimitDeleteMaxBatch)
	}
	if got := storageLimitDeleteBatch(1000, 1000, 100); got != 0 {
		t.Fatalf("batch = %d, want 0 when already under limit", got)
	}
}

type fakeCleanupRepo struct {
	deletable     int64
	deleteBatches []int
	checkpoints   int
	vacuums       int
	onDelete      func(int)
}

func (r *fakeCleanupRepo) DeleteOldestLogs(count int) (int64, error) {
	r.deleteBatches = append(r.deleteBatches, count)
	deleted := int64(count)
	if deleted > r.deletable {
		deleted = r.deletable
	}
	r.deletable -= deleted
	if r.onDelete != nil {
		r.onDelete(count)
	}
	return deleted, nil
}

func (r *fakeCleanupRepo) CountDeletableLogs() (int64, error) {
	return r.deletable, nil
}

func (r *fakeCleanupRepo) WALCheckpoint() error {
	r.checkpoints++
	return nil
}

func (r *fakeCleanupRepo) Vacuum() error {
	r.vacuums++
	return nil
}

type fakeBlobRefLister struct{}

func (fakeBlobRefLister) ListBlobRefs() ([]string, error) {
	return []string{"sha256:abc"}, nil
}

type errorBlobRefLister struct{}

func (errorBlobRefLister) ListBlobRefs() ([]string, error) {
	return nil, errors.New("list refs failed")
}

type fakeBlobGC struct {
	calls int
	onGC  func()
}

func (g *fakeBlobGC) GarbageCollect(context.Context, []string, time.Duration) (int, error) {
	g.calls++
	if g.onGC != nil {
		g.onGC()
	}
	return 1, nil
}
