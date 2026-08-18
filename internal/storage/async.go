package storage

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

var (
	// ErrAsyncQueueFull indicates the log queue is full and the entry was dropped.
	ErrAsyncQueueFull = errors.New("async log queue full; dropped")
	// ErrAsyncClosed indicates the async repository has been closed.
	ErrAsyncClosed = errors.New("async repository closed")
)

// AsyncRepository wraps a Repository and makes SaveLog best-effort/async.
// Other operations are executed synchronously against the underlying repository.
//
// This is intentionally single-worker: SQLite only allows one writer at a time anyway,
// and preserving order (insert then update) matters.
type AsyncRepository struct {
	inner Repository
	cfg   *config.Config
	blobs BlobStore

	ch        chan asyncItem
	sendMu    sync.Mutex
	closeOnce sync.Once
	closed    atomic.Bool

	inflightMu   sync.Mutex
	inflightCond *sync.Cond
	inflight     int64

	wg      sync.WaitGroup
	dropped atomic.Uint64

	ignoredMu                sync.Mutex
	pendingIgnored           map[ignoredPathKey]IgnoredPathObservation
	pendingIgnoredByUpstream map[string]int
	ignoredStop              chan struct{}
	ignoredWG                sync.WaitGroup
	ignoredFlushErrMu        sync.Mutex
	ignoredFlushErr          error
}

type ignoredPathKey struct {
	upstream string
	path     string
}

type asyncItem struct {
	log     *RequestLog
	barrier chan struct{}
}

// NewAsyncRepository creates an async wrapper with a bounded queue.
func NewAsyncRepository(inner Repository, cfg *config.Config, buffer int, blobs ...BlobStore) *AsyncRepository {
	if buffer <= 0 {
		buffer = 1024
	}
	a := &AsyncRepository{
		inner:                    inner,
		cfg:                      cfg,
		blobs:                    firstBlobStore(blobs),
		ch:                       make(chan asyncItem, buffer),
		pendingIgnored:           make(map[ignoredPathKey]IgnoredPathObservation),
		pendingIgnoredByUpstream: make(map[string]int),
		ignoredStop:              make(chan struct{}),
	}
	a.inflightCond = sync.NewCond(&a.inflightMu)

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for item := range a.ch {
			if item.barrier != nil {
				close(item.barrier)
				continue
			}
			PrepareLogForPersistence(item.log, a.cfg, a.blobs)
			if err := a.inner.SaveLog(item.log); err != nil {
				// Best-effort: avoid crashing the proxy path.
				log.Printf("save log failed: %v", err)
			}
		}
	}()

	if _, ok := inner.(ignoredPathPersistence); ok {
		a.ignoredWG.Add(1)
		go func() {
			defer a.ignoredWG.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := a.flushIgnoredPaths(); err != nil {
						log.Printf("save ignored path stats failed: %v", err)
					}
				case <-a.ignoredStop:
					if err := a.flushIgnoredPaths(); err != nil {
						a.ignoredFlushErrMu.Lock()
						a.ignoredFlushErr = err
						a.ignoredFlushErrMu.Unlock()
					}
					return
				}
			}
		}()
	}

	return a
}

// Dropped returns the number of logs dropped due to a full queue.
func (a *AsyncRepository) Dropped() uint64 {
	return a.dropped.Load()
}

func (a *AsyncRepository) SaveLog(log *RequestLog) error {
	if log == nil {
		return nil
	}
	if a.closed.Load() {
		return ErrAsyncClosed
	}

	// Coordinate with Close(): prevent closing the channel while a send is in-flight.
	a.inflightMu.Lock()
	if a.closed.Load() {
		a.inflightMu.Unlock()
		return ErrAsyncClosed
	}
	a.inflight++
	a.inflightMu.Unlock()
	defer func() {
		a.inflightMu.Lock()
		a.inflight--
		if a.inflight == 0 && a.inflightCond != nil {
			a.inflightCond.Broadcast()
		}
		a.inflightMu.Unlock()
	}()

	c := log.Clone()
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	select {
	case a.ch <- asyncItem{log: c}:
		return nil
	default:
		a.dropped.Add(1)
		return ErrAsyncQueueFull
	}
}

// Flush waits until every log enqueued before the call has been persisted.
func (a *AsyncRepository) Flush(ctx context.Context) error {
	if a.closed.Load() {
		return ErrAsyncClosed
	}
	barrier := make(chan struct{})
	a.sendMu.Lock()
	select {
	case a.ch <- asyncItem{barrier: barrier}:
		a.sendMu.Unlock()
	case <-ctx.Done():
		a.sendMu.Unlock()
		return ctx.Err()
	}
	select {
	case <-barrier:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *AsyncRepository) GetLog(id string) (*RequestLog, error) {
	return a.inner.GetLog(id)
}

func (a *AsyncRepository) GetLogBody(logID, part string) (LogBody, error) {
	repo, ok := a.inner.(BodyRepository)
	if !ok {
		return LogBody{}, errors.New("body repository is unavailable")
	}
	return repo.GetLogBody(logID, part)
}

func (a *AsyncRepository) GetLogBodies(logID string) ([]LogBody, error) {
	repo, ok := a.inner.(BodyRepository)
	if !ok {
		return nil, errors.New("body repository is unavailable")
	}
	return repo.GetLogBodies(logID)
}

func (a *AsyncRepository) ListBlobRefs() ([]string, error) {
	repo, ok := a.inner.(BlobRefRepository)
	if !ok {
		return nil, errors.New("blob reference repository is unavailable")
	}
	return repo.ListBlobRefs()
}

func (a *AsyncRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	return a.inner.ListLogs(filter)
}

func (a *AsyncRepository) ExportLogs(ctx context.Context, filter LogFilter, each func(*RequestLog) error) error {
	return a.inner.ExportLogs(ctx, filter, each)
}

func (a *AsyncRepository) DeleteLogsBefore(beforeTime time.Time) (int64, error) {
	return a.inner.DeleteLogsBefore(beforeTime)
}

func (a *AsyncRepository) DeleteOldestLogs(count int) (int64, error) {
	return a.inner.DeleteOldestLogs(count)
}

func (a *AsyncRepository) CountDeletableLogs() (int64, error) {
	return a.inner.CountDeletableLogs()
}

func (a *AsyncRepository) WALCheckpoint() error {
	return a.inner.WALCheckpoint()
}

func (a *AsyncRepository) Vacuum() error {
	return a.inner.Vacuum()
}

func (a *AsyncRepository) GetLogAnnotation(logID string) (LogAnnotation, error) {
	return a.inner.GetLogAnnotation(logID)
}

func (a *AsyncRepository) SaveLogAnnotation(logID string, annotation LogAnnotation) (LogAnnotation, error) {
	return a.inner.SaveLogAnnotation(logID, annotation)
}

func (a *AsyncRepository) ListTraces(filter TraceFilter) ([]TraceSummary, int64, error) {
	return a.inner.ListTraces(filter)
}

func (a *AsyncRepository) GetTraceRequests(traceID string) ([]*RequestLog, error) {
	return a.inner.GetTraceRequests(traceID)
}

func (a *AsyncRepository) GetStats(since *time.Time) (*LogStats, error) {
	return a.inner.GetStats(since)
}

func (a *AsyncRepository) RecordIgnoredPath(upstream, requestPath string, seenAt time.Time) error {
	upstream = strings.TrimSpace(upstream)
	requestPath = strings.TrimSpace(requestPath)
	if upstream == "" || requestPath == "" {
		return nil
	}
	if a.closed.Load() {
		return ErrAsyncClosed
	}

	a.inflightMu.Lock()
	if a.closed.Load() {
		a.inflightMu.Unlock()
		return ErrAsyncClosed
	}
	a.inflight++
	a.inflightMu.Unlock()
	defer func() {
		a.inflightMu.Lock()
		a.inflight--
		if a.inflight == 0 && a.inflightCond != nil {
			a.inflightCond.Broadcast()
		}
		a.inflightMu.Unlock()
	}()

	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	key := ignoredPathKey{upstream: upstream, path: requestPath}
	a.ignoredMu.Lock()
	entry, exists := a.pendingIgnored[key]
	if !exists && a.pendingIgnoredByUpstream[upstream] >= IgnoredPathMaxEntriesPerUpstream {
		a.ignoredMu.Unlock()
		return nil
	}
	if !exists {
		entry = IgnoredPathObservation{Upstream: upstream, Path: requestPath}
		a.pendingIgnoredByUpstream[upstream]++
	}
	entry.Count++
	if seenAt.After(entry.LastSeen) {
		entry.LastSeen = seenAt
	}
	a.pendingIgnored[key] = entry
	a.ignoredMu.Unlock()
	return nil
}

func (a *AsyncRepository) flushIgnoredPaths() error {
	store, ok := a.inner.(ignoredPathPersistence)
	if !ok {
		return nil
	}
	a.ignoredMu.Lock()
	if len(a.pendingIgnored) == 0 {
		a.ignoredMu.Unlock()
		return nil
	}
	entries := make([]IgnoredPathObservation, 0, len(a.pendingIgnored))
	for _, entry := range a.pendingIgnored {
		entries = append(entries, entry)
	}
	a.pendingIgnored = make(map[ignoredPathKey]IgnoredPathObservation)
	a.pendingIgnoredByUpstream = make(map[string]int)
	a.ignoredMu.Unlock()

	if err := store.UpsertIgnoredPaths(entries, IgnoredPathMaxEntriesPerUpstream); err != nil {
		// Put observations back so a transient SQLite error does not lose audit counts.
		a.ignoredMu.Lock()
		for _, entry := range entries {
			key := ignoredPathKey{upstream: entry.Upstream, path: entry.Path}
			pending, exists := a.pendingIgnored[key]
			if !exists {
				pending = IgnoredPathObservation{Upstream: entry.Upstream, Path: entry.Path}
				a.pendingIgnoredByUpstream[entry.Upstream]++
			}
			pending.Count += entry.Count
			if entry.LastSeen.After(pending.LastSeen) {
				pending.LastSeen = entry.LastSeen
			}
			a.pendingIgnored[key] = pending
		}
		a.ignoredMu.Unlock()
		return err
	}
	return nil
}

func (a *AsyncRepository) ListIgnoredPaths(filter IgnoredPathFilter) (IgnoredPathListResult, error) {
	store, ok := a.inner.(ignoredPathPersistence)
	if !ok {
		return IgnoredPathListResult{}, errors.New("ignored path storage is unavailable")
	}
	return store.ListIgnoredPaths(filter)
}

func (a *AsyncRepository) DeleteIgnoredPaths(upstream, requestPath string) (int64, error) {
	if err := a.flushIgnoredPaths(); err != nil {
		return 0, err
	}
	store, ok := a.inner.(ignoredPathPersistence)
	if !ok {
		return 0, errors.New("ignored path storage is unavailable")
	}
	return store.DeleteIgnoredPaths(upstream, requestPath)
}

func (a *AsyncRepository) DeleteIgnoredPathsBefore(before time.Time) (int64, error) {
	if err := a.flushIgnoredPaths(); err != nil {
		return 0, err
	}
	store, ok := a.inner.(ignoredPathPersistence)
	if !ok {
		return 0, errors.New("ignored path storage is unavailable")
	}
	return store.DeleteIgnoredPathsBefore(before)
}

func (a *AsyncRepository) archiveRepository() (ArchiveRepository, error) {
	repo, ok := a.inner.(ArchiveRepository)
	if !ok {
		return nil, errors.New("archive repository is unavailable")
	}
	return repo, nil
}

func (a *AsyncRepository) RecoverInterruptedArchiveWork(now time.Time) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.RecoverInterruptedArchiveWork(now)
}

func (a *AsyncRepository) OldestUnarchivedLogTime(before time.Time) (*time.Time, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, err
	}
	return r.OldestUnarchivedLogTime(before)
}
func (a *AsyncRepository) CreateArchiveJob(v ArchiveJob) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.CreateArchiveJob(v)
}
func (a *AsyncRepository) UpdateArchiveJob(v ArchiveJob) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.UpdateArchiveJob(v)
}
func (a *AsyncRepository) ListArchiveJobs(limit int) ([]ArchiveJob, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, err
	}
	return r.ListArchiveJobs(limit)
}
func (a *AsyncRepository) ListArchiveJobsPage(offset, limit int) ([]ArchiveJob, int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, 0, err
	}
	return r.ListArchiveJobsPage(offset, limit)
}
func (a *AsyncRepository) CreateArchiveBatch(v ArchiveBatch) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.CreateArchiveBatch(v)
}
func (a *AsyncRepository) UpdateArchiveBatch(v ArchiveBatch) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.UpdateArchiveBatch(v)
}
func (a *AsyncRepository) ListArchiveBatches(limit int) ([]ArchiveBatch, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, err
	}
	return r.ListArchiveBatches(limit)
}
func (a *AsyncRepository) ListArchiveBatchesPage(filter ArchiveBatchFilter) ([]ArchiveBatch, int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, 0, err
	}
	return r.ListArchiveBatchesPage(filter)
}
func (a *AsyncRepository) ReserveArchiveBatchLogs(id string, start, end time.Time) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.ReserveArchiveBatchLogs(id, start, end)
}
func (a *AsyncRepository) ReleaseArchiveBatchLogs(id string) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.ReleaseArchiveBatchLogs(id)
}
func (a *AsyncRepository) ExportArchiveBatch(ctx context.Context, id string, each func(*RequestLog) error) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.ExportArchiveBatch(ctx, id, each)
}
func (a *AsyncRepository) MarkArchiveBatchVerified(id string, at time.Time) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.MarkArchiveBatchVerified(id, at)
}
func (a *AsyncRepository) DeleteEligibleBackedLogs(cutoff time.Time, limit int) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.DeleteEligibleBackedLogs(cutoff, limit)
}
func (a *AsyncRepository) CountEligibleBackedLogs(cutoff time.Time) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.CountEligibleBackedLogs(cutoff)
}
func (a *AsyncRepository) PendingBackedLogCleanup() (int64, *time.Time, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, nil, err
	}
	return r.PendingBackedLogCleanup()
}
func (a *AsyncRepository) LogExists(id string) (bool, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return false, err
	}
	return r.LogExists(id)
}
func (a *AsyncRepository) SaveImportedLog(log *RequestLog) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.SaveImportedLog(log)
}
func (a *AsyncRepository) CreateArchiveImport(v ArchiveImport) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.CreateArchiveImport(v)
}
func (a *AsyncRepository) UpdateArchiveImport(v ArchiveImport) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.UpdateArchiveImport(v)
}
func (a *AsyncRepository) ListArchiveImports() ([]ArchiveImport, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, err
	}
	return r.ListArchiveImports()
}
func (a *AsyncRepository) ListArchiveImportsPage(offset, limit int) ([]ArchiveImport, int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return nil, 0, err
	}
	return r.ListArchiveImportsPage(offset, limit)
}
func (a *AsyncRepository) DeleteArchiveImport(id string) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.DeleteArchiveImport(id)
}
func (a *AsyncRepository) DeleteExpiredArchiveImports(now time.Time) (int64, error) {
	r, err := a.archiveRepository()
	if err != nil {
		return 0, err
	}
	return r.DeleteExpiredArchiveImports(now)
}
func (a *AsyncRepository) StageArchiveBlobRef(id, ref string) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.StageArchiveBlobRef(id, ref)
}
func (a *AsyncRepository) ClearArchiveBlobRefs(id string) error {
	r, err := a.archiveRepository()
	if err != nil {
		return err
	}
	return r.ClearArchiveBlobRefs(id)
}

func (a *AsyncRepository) Close() error {
	a.closeOnce.Do(func() {
		if a.inflightCond == nil {
			a.inflightCond = sync.NewCond(&a.inflightMu)
		}

		a.inflightMu.Lock()
		a.closed.Store(true)
		for a.inflight > 0 {
			a.inflightCond.Wait()
		}
		close(a.ch)
		close(a.ignoredStop)
		a.inflightMu.Unlock()
	})
	a.wg.Wait()
	a.ignoredWG.Wait()
	a.ignoredFlushErrMu.Lock()
	flushErr := a.ignoredFlushErr
	a.ignoredFlushErrMu.Unlock()
	return errors.Join(flushErr, a.inner.Close())
}
