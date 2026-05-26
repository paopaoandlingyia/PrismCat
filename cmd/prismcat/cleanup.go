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
	storageLimitTargetRatio      = 0.9
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
	targetBytes := storageLimitCleanupTarget(storageCfg.MaxStorageBytes)

	reclaimedBlobGC, reclaimErr := reclaimStorageSpace(ctx, repo, refLister, blobGC, logf)
	result.RanBlobGC = result.RanBlobGC || reclaimedBlobGC
	if reclaimErr != nil {
		logf("storage reclaim before size cleanup had errors: %v", reclaimErr)
	}

	usage, err = calculate(storageCfg)
	if err != nil {
		return result, err
	}
	if usage.TotalBytes <= targetBytes {
		logf("storage cleanup reclaimed space without deleting logs (%d MB / %d MB target, %d MB limit)",
			usage.TotalBytes>>20, targetBytes>>20, storageCfg.MaxStorageBytes>>20)
		return result, nil
	}
	if reclaimErr != nil && usage.BlobBytes > 0 && usage.DatabaseBytes <= targetBytes {
		return result, fmt.Errorf("blob reclaim failed while blob files keep storage over limit: %w", reclaimErr)
	}

	for round := 1; round <= storageLimitCleanupMaxRounds && usage.TotalBytes > targetBytes; round++ {
		deletableCount, err := repo.CountDeletableLogs()
		if err != nil {
			return result, err
		}
		if deletableCount <= 0 {
			logf("size-based cleanup: no deletable logs (all remaining are saved)")
			return result, nil
		}

		toDelete := storageLimitDeleteBatch(usage.TotalBytes, targetBytes, deletableCount)
		logf("storage above cleanup target (%d MB / %d MB target, %d MB limit), deleting %d oldest unsaved logs...",
			usage.TotalBytes>>20, targetBytes>>20, storageCfg.MaxStorageBytes>>20, toDelete)

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
		logf("size-based cleanup round %d done: deleted %d logs (%d MB -> %d MB, target %d MB, limit %d MB)",
			round, deleted, usage.TotalBytes>>20, nextUsage.TotalBytes>>20, targetBytes>>20, storageCfg.MaxStorageBytes>>20)
		usage = nextUsage
	}

	if usage.TotalBytes > targetBytes {
		logf("size-based cleanup stopped after %d rounds (%d MB / %d MB target, %d MB limit)",
			storageLimitCleanupMaxRounds, usage.TotalBytes>>20, targetBytes>>20, storageCfg.MaxStorageBytes>>20)
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

func storageLimitDeleteBatch(totalBytes, targetBytes, deletableCount int64) int {
	if totalBytes <= 0 || targetBytes <= 0 || totalBytes <= targetBytes || deletableCount <= 0 {
		return 0
	}

	excessRatio := float64(totalBytes-targetBytes) / float64(totalBytes)
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

func storageLimitCleanupTarget(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return 0
	}
	target := int64(math.Floor(float64(maxBytes) * storageLimitTargetRatio))
	if target < 1 {
		return 1
	}
	return target
}
