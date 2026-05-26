package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storageusage"
)

const (
	storageLimitCleanupMaxRounds = 5
	storageLimitDeleteMaxBatch   = 2000
	storageLimitDeleteOvershoot  = 1.15
	storageLimitBlobGCMinAge     = time.Hour
)

type storageLimitRepository interface {
	DeleteOldestLogs(count int) (int64, error)
	CountDeletableLogs() (int64, error)
	WALCheckpoint() error
	Vacuum() error
}

type blobRefLister interface {
	ListBlobRefs() ([]string, error)
}

type blobGarbageCollector interface {
	GarbageCollect(context.Context, []string, time.Duration) (int, error)
}

type storageUsageCalculator func(config.StorageConfig) (storageusage.Usage, error)

type storageLimitCleanupResult struct {
	DeletedLogs int64
	RanBlobGC   bool
}

func cleanupStorageLimit(
	ctx context.Context,
	storageCfg config.StorageConfig,
	repo storageLimitRepository,
	refLister blobRefLister,
	blobGC blobGarbageCollector,
	calculate storageUsageCalculator,
	logf func(string, ...interface{}),
) (storageLimitCleanupResult, error) {
	var result storageLimitCleanupResult
	if storageCfg.MaxStorageBytes <= 0 {
		return result, nil
	}
	if calculate == nil {
		calculate = storageusage.Calculate
	}
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}

	usage, err := calculate(storageCfg)
	if err != nil {
		return result, err
	}
	if usage.TotalBytes <= storageCfg.MaxStorageBytes {
		return result, nil
	}

	reclaimedBlobGC, reclaimErr := reclaimStorageSpace(ctx, repo, refLister, blobGC, logf)
	result.RanBlobGC = result.RanBlobGC || reclaimedBlobGC
	if reclaimErr != nil {
		logf("storage reclaim before size cleanup had errors: %v", reclaimErr)
	}

	usage, err = calculate(storageCfg)
	if err != nil {
		return result, err
	}
	if usage.TotalBytes <= storageCfg.MaxStorageBytes {
		logf("storage cleanup reclaimed space without deleting logs (%d MB / %d MB)",
			usage.TotalBytes>>20, storageCfg.MaxStorageBytes>>20)
		return result, nil
	}
	if reclaimErr != nil && usage.BlobBytes > 0 && usage.DatabaseBytes <= storageCfg.MaxStorageBytes {
		return result, fmt.Errorf("blob reclaim failed while blob files keep storage over limit: %w", reclaimErr)
	}

	for round := 1; round <= storageLimitCleanupMaxRounds && usage.TotalBytes > storageCfg.MaxStorageBytes; round++ {
		deletableCount, err := repo.CountDeletableLogs()
		if err != nil {
			return result, err
		}
		if deletableCount <= 0 {
			logf("size-based cleanup: no deletable logs (all remaining are saved)")
			return result, nil
		}

		toDelete := storageLimitDeleteBatch(usage.TotalBytes, storageCfg.MaxStorageBytes, deletableCount)
		logf("storage over limit (%d MB / %d MB), deleting %d oldest unsaved logs...",
			usage.TotalBytes>>20, storageCfg.MaxStorageBytes>>20, toDelete)

		deleted, err := repo.DeleteOldestLogs(toDelete)
		if err != nil {
			return result, err
		}
		result.DeletedLogs += deleted
		if deleted <= 0 {
			logf("size-based cleanup stopped: delete returned 0 rows")
			return result, nil
		}

		reclaimedBlobGC, reclaimErr = reclaimStorageSpace(ctx, repo, refLister, blobGC, logf)
		result.RanBlobGC = result.RanBlobGC || reclaimedBlobGC
		if reclaimErr != nil {
			logf("storage reclaim after size cleanup had errors: %v", reclaimErr)
		}

		nextUsage, err := calculate(storageCfg)
		if err != nil {
			return result, err
		}
		logf("size-based cleanup round %d done: deleted %d logs (%d MB -> %d MB, limit %d MB)",
			round, deleted, usage.TotalBytes>>20, nextUsage.TotalBytes>>20, storageCfg.MaxStorageBytes>>20)
		usage = nextUsage
	}

	if usage.TotalBytes > storageCfg.MaxStorageBytes {
		logf("size-based cleanup stopped after %d rounds (%d MB / %d MB)",
			storageLimitCleanupMaxRounds, usage.TotalBytes>>20, storageCfg.MaxStorageBytes>>20)
	}
	return result, nil
}

func reclaimStorageSpace(
	ctx context.Context,
	repo storageLimitRepository,
	refLister blobRefLister,
	blobGC blobGarbageCollector,
	logf func(string, ...interface{}),
) (bool, error) {
	var firstErr error
	ranBlobGC := false

	if refLister != nil && blobGC != nil {
		ranBlobGC = true
		refs, err := refLister.ListBlobRefs()
		if err != nil {
			firstErr = err
			logf("blob GC list refs failed: %v", err)
		} else if n, err := blobGC.GarbageCollect(ctx, refs, storageLimitBlobGCMinAge); err != nil {
			firstErr = err
			logf("blob GC failed: %v", err)
		} else if n > 0 {
			logf("deleted %d unreferenced blobs", n)
		}
	}

	if err := repo.WALCheckpoint(); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		logf("WAL checkpoint failed: %v", err)
	}
	if err := repo.Vacuum(); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		logf("VACUUM failed: %v", err)
	}

	return ranBlobGC, firstErr
}

func storageLimitDeleteBatch(totalBytes, maxBytes, deletableCount int64) int {
	if totalBytes <= 0 || maxBytes <= 0 || totalBytes <= maxBytes || deletableCount <= 0 {
		return 0
	}

	excessRatio := float64(totalBytes-maxBytes) / float64(totalBytes)
	estimated := math.Ceil(float64(deletableCount) * excessRatio * storageLimitDeleteOvershoot)
	if estimated < 1 {
		estimated = 1
	}
	if estimated > storageLimitDeleteMaxBatch {
		estimated = storageLimitDeleteMaxBatch
	}
	if estimated > float64(deletableCount) {
		estimated = float64(deletableCount)
	}
	return int(estimated)
}
