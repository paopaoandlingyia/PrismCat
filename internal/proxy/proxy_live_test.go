package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/live"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type proxyTestRepo struct {
	mu    sync.Mutex
	logs  []*storage.RequestLog
	saved chan *storage.RequestLog
}

func newProxyTestRepo() *proxyTestRepo {
	return &proxyTestRepo{
		saved: make(chan *storage.RequestLog, 8),
	}
}

func (r *proxyTestRepo) SaveLog(log *storage.RequestLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	next := cloneLog(log)
	r.logs = append(r.logs, next)
	r.saved <- next
	return nil
}

func (r *proxyTestRepo) GetLog(id string) (*storage.RequestLog, error) {
	return nil, errors.New("not implemented")
}

func (r *proxyTestRepo) ListLogs(filter storage.LogFilter) ([]*storage.RequestLog, int64, error) {
	return nil, 0, errors.New("not implemented")
}

func (r *proxyTestRepo) DeleteLogsBefore(before time.Time) (int64, error) {
	return 0, nil
}

func (r *proxyTestRepo) GetLogAnnotation(logID string) (storage.LogAnnotation, error) {
	return storage.LogAnnotation{}, errors.New("not implemented")
}

func (r *proxyTestRepo) SaveLogAnnotation(logID string, annotation storage.LogAnnotation) (storage.LogAnnotation, error) {
	return annotation, errors.New("not implemented")
}

func (r *proxyTestRepo) GetStats(since *time.Time) (*storage.LogStats, error) {
	return &storage.LogStats{}, nil
}

func (r *proxyTestRepo) Close() error {
	return nil
}

func (r *proxyTestRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.logs)
}

func TestLiveRequestBodyUpdatesWithoutEarlySnapshot(t *testing.T) {
	const requestBody = `{"hello":"world"}`

	bodyRead := make(chan struct{})
	releaseResponse := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
			return
		}
		if string(body) != requestBody {
			t.Errorf("upstream body = %q, want %q", string(body), requestBody)
		}

		close(bodyRead)
		<-releaseResponse

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{
			ProxyDomains: []string{"localhost"},
		},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {
				Target:  upstream.URL,
				Timeout: 5,
			},
		},
		Logging: config.LoggingConfig{
			MaxRequestBody:           1024,
			MaxResponseBody:          1024,
			SensitiveHeaders:         []string{"Authorization", "x-api-key", "api-key"},
			StoreBase64:              true,
			EarlyRequestBodySnapshot: false,
		},
	}
	repo := newProxyTestRepo()
	liveRegistry := live.NewRegistry(cfg.Logging.MaxResponseBody)
	proxy := New(cfg, repo, liveRegistry)

	req := httptest.NewRequest(http.MethodPost, "http://openai.localhost/v1/chat/completions", strings.NewReader(requestBody))
	req.Host = "openai.localhost"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.ServeHTTP(rr, req)
	}()

	var initialLog *storage.RequestLog
	select {
	case initialLog = <-repo.saved:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial log save")
	}
	if initialLog.ID == "" {
		t.Fatal("initial log ID is empty")
	}

	select {
	case <-bodyRead:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream to read request body")
	}

	deadline := time.Now().Add(time.Second)
	for {
		snapshot, ok := liveRegistry.Snapshot(initialLog.ID)
		if ok &&
			snapshot.RequestBodySize == int64(len(requestBody)) &&
			strings.Contains(snapshot.RequestBody, "hello") &&
			strings.Contains(snapshot.RequestBody, "world") {
			break
		}
		if time.Now().After(deadline) {
			if ok {
				t.Fatalf("live request body was not published: size=%d body=%q", snapshot.RequestBodySize, snapshot.RequestBody)
			}
			t.Fatal("live snapshot missing before response completed")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := repo.count(); got != 1 {
		t.Fatalf("repo saves before final response = %d, want 1", got)
	}

	close(releaseResponse)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for proxy response")
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := repo.count(); got != 2 {
		t.Fatalf("repo saves after final response = %d, want 2", got)
	}
}
