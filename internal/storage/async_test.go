package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

type memRepo struct {
	mu     sync.Mutex
	closed bool
	logs   []*RequestLog
}

func (m *memRepo) SaveLog(log *RequestLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("closed")
	}
	if log == nil {
		return nil
	}
	m.logs = append(m.logs, log)
	return nil
}

func (m *memRepo) GetLog(id string) (*RequestLog, error) { return nil, errors.New("not implemented") }
func (m *memRepo) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	return nil, 0, errors.New("not implemented")
}
func (m *memRepo) DeleteLogsBefore(before time.Time) (int64, error) { return 0, nil }
func (m *memRepo) DeleteOldestLogs(count int) (int64, error)       { return 0, nil }
func (m *memRepo) CountDeletableLogs() (int64, error)             { return 0, nil }
func (m *memRepo) WALCheckpoint() error                           { return nil }
func (m *memRepo) Vacuum() error                                  { return nil }
func (m *memRepo) GetLogAnnotation(logID string) (LogAnnotation, error) {
	return LogAnnotation{}, errors.New("not implemented")
}
func (m *memRepo) SaveLogAnnotation(logID string, annotation LogAnnotation) (LogAnnotation, error) {
	return annotation, errors.New("not implemented")
}
func (m *memRepo) GetStats(since *time.Time) (*LogStats, error) { return &LogStats{}, nil }
func (m *memRepo) ListTraces(filter TraceFilter) ([]TraceSummary, int64, error) {
	return nil, 0, errors.New("not implemented")
}
func (m *memRepo) GetTraceRequests(traceID string) ([]*RequestLog, error) {
	return nil, errors.New("not implemented")
}
func (m *memRepo) Close() error { m.mu.Lock(); m.closed = true; m.mu.Unlock(); return nil }

type memBlobStore struct {
	puts int
	data [][]byte
}

func (m *memBlobStore) Put(ctx context.Context, b []byte) (string, error) {
	_ = ctx
	m.puts++
	m.data = append(m.data, append([]byte(nil), b...))
	return "sha256:" + strings.Repeat("0", 64), nil
}

func (m *memBlobStore) Get(ctx context.Context, ref string) ([]byte, error) {
	return nil, ErrBlobNotFound
}

func (m *memBlobStore) Exists(ctx context.Context, ref string) (bool, error) {
	return false, nil
}

func TestAsyncRepositoryCloseDrainsQueue(t *testing.T) {
	inner := &memRepo{}
	a := NewAsyncRepository(inner, nil, 64)

	const n = 10
	for i := 0; i < n; i++ {
		if err := a.SaveLog(&RequestLog{ID: "id"}); err != nil {
			t.Fatalf("SaveLog failed: %v", err)
		}
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	got := len(inner.logs)
	inner.mu.Unlock()

	if got != n {
		t.Fatalf("inner.SaveLog called %d times, want %d", got, n)
	}
}

func TestAsyncRepositoryCloseConcurrentSaveDoesNotPanic(t *testing.T) {
	inner := &memRepo{}
	a := NewAsyncRepository(inner, nil, 1024)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				err := a.SaveLog(&RequestLog{ID: "id"})
				if err == ErrAsyncClosed {
					return
				}
			}
		}()
	}

	// Let the producers run briefly, then close while they are active.
	time.Sleep(10 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	wg.Wait()
}

func TestAsyncRepositoryPreparesBodiesInWorker(t *testing.T) {
	inner := &memRepo{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 1024
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.StoreBase64 = true

	a := NewAsyncRepository(inner, cfg, 16)

	err := a.SaveLog(&RequestLog{
		ID:             "id",
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBodyRaw: []byte(`{"hello":"world"}`),
	})
	if err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]
	if !strings.Contains(saved.RequestBody, `"hello":"world"`) {
		t.Fatalf("RequestBody = %q, want formatted JSON content", saved.RequestBody)
	}
	if saved.RequestBodyRaw != nil {
		t.Fatalf("RequestBodyRaw not cleared")
	}
}

func TestAsyncRepositoryStoresBinaryBodyBlob(t *testing.T) {
	inner := &memRepo{}
	blobs := &memBlobStore{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 1024
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.StoreBase64 = true

	a := NewAsyncRepository(inner, cfg, 16, blobs)
	body := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff}

	err := a.SaveLog(&RequestLog{
		ID:              "id",
		ResponseHeaders: map[string][]string{"Content-Type": {"image/png"}},
		ResponseBodyRaw: body,
	})
	if err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]
	if !strings.HasPrefix(saved.ResponseBody, "[binary content omitted;") {
		t.Fatalf("ResponseBody = %q, want binary placeholder", saved.ResponseBody)
	}
	if saved.ResponseBodyRef == "" {
		t.Fatalf("ResponseBodyRef is empty")
	}
	if saved.Truncated {
		t.Fatalf("Truncated = true, want false for recoverable preview truncation")
	}
	if blobs.puts != 1 {
		t.Fatalf("blob puts = %d, want 1", blobs.puts)
	}
	if string(blobs.data[0]) != string(body) {
		t.Fatalf("stored blob = %v, want %v", blobs.data[0], body)
	}
	if saved.ResponseBodyRaw != nil {
		t.Fatalf("ResponseBodyRaw not cleared")
	}
}

func TestAsyncRepositoryStoresPreviewAndRawBlob(t *testing.T) {
	inner := &memRepo{}
	blobs := &memBlobStore{}
	cfg := &config.Config{}
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.BodyPreviewBytes = 8
	cfg.Logging.DetachBodyOverBytes = 1024
	cfg.Logging.StoreBase64 = true

	a := NewAsyncRepository(inner, cfg, 16, blobs)
	body := []byte(`{"message":"hello world"}`)

	err := a.SaveLog(&RequestLog{
		ID:              "id",
		ResponseHeaders: map[string][]string{"Content-Type": {"application/json"}},
		ResponseBodyRaw: body,
	})
	if err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]
	if saved.ResponseBody != `{"messag` {
		t.Fatalf("ResponseBody preview = %q, want truncated preview", saved.ResponseBody)
	}
	if saved.ResponseBodyRef == "" {
		t.Fatalf("ResponseBodyRef is empty")
	}
	if saved.Truncated {
		t.Fatalf("Truncated = true, want false for recoverable preview truncation")
	}
	if blobs.puts != 1 {
		t.Fatalf("blob puts = %d, want 1", blobs.puts)
	}
	if string(blobs.data[0]) != string(body) {
		t.Fatalf("stored blob = %q, want raw body %q", string(blobs.data[0]), string(body))
	}
}

func TestAsyncRepositoryMarksCaptureLimitAsTruncated(t *testing.T) {
	inner := &memRepo{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 8
	cfg.Logging.BodyPreviewBytes = 8
	cfg.Logging.DetachBodyOverBytes = 1024
	cfg.Logging.StoreBase64 = true

	a := NewAsyncRepository(inner, cfg, 16)

	err := a.SaveLog(&RequestLog{
		ID:                          "id",
		RequestHeaders:              map[string][]string{"Content-Type": {"application/json"}},
		RequestBodyRaw:              []byte(`{"msg":`),
		RequestBodySize:             int64(len(`{"msg":"hello world"}`)),
		RequestBodyCaptureTruncated: true,
	})
	if err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.logs) != 1 {
		t.Fatalf("inner logs = %d, want 1", len(inner.logs))
	}
	saved := inner.logs[0]
	if !saved.Truncated {
		t.Fatalf("Truncated = false, want true for capture limit truncation")
	}
}

func TestAsyncRepositoryExtractsUsageInWorker(t *testing.T) {
	inner := &memRepo{}
	cfg := &config.Config{}
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.BodyPreviewBytes = 1024
	cfg.Logging.StoreBase64 = true
	cfg.Usage = config.UsageExtractionConfig{
		Enabled: true,
		Upstreams: map[string]config.UsageExtractionUpstreamBinding{
			"openai": {Enabled: true, RuleNames: []string{"OpenAI compatible"}},
		},
		Rules: []config.UsageExtractionRule{
			{
				Name:    "OpenAI compatible",
				Enabled: true,
				Match:   config.UsageExtractionMatch{ContentTypes: []string{"application/json"}},
				Paths: config.UsageExtractionPaths{
					InputTokens:  []string{"/usage/prompt_tokens"},
					OutputTokens: []string{"/usage/completion_tokens"},
					TotalTokens:  []string{"/usage/total_tokens"},
					RawUsage:     []string{"/usage"},
				},
			},
		},
	}

	a := NewAsyncRepository(inner, cfg, 16)
	err := a.SaveLog(&RequestLog{
		ID:              "id",
		Upstream:        "openai",
		ResponseHeaders: map[string][]string{"Content-Type": {"application/json"}},
		ResponseBodyRaw: []byte(`{"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`),
	})
	if err != nil {
		t.Fatalf("SaveLog failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	saved := inner.logs[0]
	if saved.UsageTotalTokens == nil || *saved.UsageTotalTokens != 8 {
		t.Fatalf("UsageTotalTokens = %v, want 8", saved.UsageTotalTokens)
	}
	if saved.UsageRaw == "" {
		t.Fatalf("UsageRaw is empty")
	}
}
