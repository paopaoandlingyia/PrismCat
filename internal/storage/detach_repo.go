package storage

import (
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

// DetachingRepository detaches large bodies into a BlobStore before persisting logs.
// It is best-effort: on blob failures it falls back to storing inline bodies.
//
// PrismCat now also detaches large bodies before they enter the async queue.
// This repository-level detacher remains as a safety net for any caller that
// persists full bodies directly.
type DetachingRepository struct {
	inner Repository
	blobs BlobStore
	cfg   *config.Config
}

func NewDetachingRepository(inner Repository, blobs BlobStore, cfg *config.Config) *DetachingRepository {
	return &DetachingRepository{
		inner: inner,
		blobs: blobs,
		cfg:   cfg,
	}
}

func (r *DetachingRepository) SaveLog(logEntry *RequestLog) error {
	DetachLargeBodies(logEntry, r.blobs, r.cfg)
	return r.inner.SaveLog(logEntry)
}

func (r *DetachingRepository) GetLog(id string) (*RequestLog, error) {
	return r.inner.GetLog(id)
}

func (r *DetachingRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	return r.inner.ListLogs(filter)
}

func (r *DetachingRepository) DeleteLogsBefore(beforeTime time.Time) (int64, error) {
	return r.inner.DeleteLogsBefore(beforeTime)
}

func (r *DetachingRepository) GetLogAnnotation(logID string) (LogAnnotation, error) {
	return r.inner.GetLogAnnotation(logID)
}

func (r *DetachingRepository) SaveLogAnnotation(logID string, annotation LogAnnotation) (LogAnnotation, error) {
	return r.inner.SaveLogAnnotation(logID, annotation)
}

func (r *DetachingRepository) GetStats(since *time.Time) (*LogStats, error) {
	return r.inner.GetStats(since)
}

func (r *DetachingRepository) Close() error {
	return r.inner.Close()
}
