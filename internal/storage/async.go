package storage

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
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

	ch        chan *RequestLog
	closeOnce sync.Once
	closed    atomic.Bool

	inflightMu   sync.Mutex
	inflightCond *sync.Cond
	inflight     int64

	wg      sync.WaitGroup
	dropped atomic.Uint64
}

// NewAsyncRepository creates an async wrapper with a bounded queue.
func NewAsyncRepository(inner Repository, buffer int) *AsyncRepository {
	if buffer <= 0 {
		buffer = 1024
	}
	a := &AsyncRepository{
		inner: inner,
		ch:    make(chan *RequestLog, buffer),
	}
	a.inflightCond = sync.NewCond(&a.inflightMu)

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for entry := range a.ch {
			if err := a.inner.SaveLog(entry); err != nil {
				// Best-effort: avoid crashing the proxy path.
				log.Printf("save log failed: %v", err)
			}
		}
	}()

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

	c := cloneRequestLog(log)
	select {
	case a.ch <- c:
		return nil
	default:
		a.dropped.Add(1)
		return ErrAsyncQueueFull
	}
}

func (a *AsyncRepository) GetLog(id string) (*RequestLog, error) {
	return a.inner.GetLog(id)
}

func (a *AsyncRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	return a.inner.ListLogs(filter)
}

func (a *AsyncRepository) DeleteLogsBefore(beforeTime time.Time) (int64, error) {
	return a.inner.DeleteLogsBefore(beforeTime)
}

func (a *AsyncRepository) GetStats(since *time.Time) (*LogStats, error) {
	return a.inner.GetStats(since)
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
		a.inflightMu.Unlock()
	})
	a.wg.Wait()
	return a.inner.Close()
}

func cloneRequestLog(in *RequestLog) *RequestLog {
	if in == nil {
		return nil
	}
	out := *in
	out.RequestHeaders = cloneStringMap(in.RequestHeaders)
	out.ResponseHeaders = cloneStringMap(in.ResponseHeaders)
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
