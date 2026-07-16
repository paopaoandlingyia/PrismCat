package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

func TestReadFirstResponseChunkTimesOut(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()

	start := time.Now()
	_, err := readFirstResponseChunk(reader, 30*time.Millisecond)
	if !isStreamFirstByteTimeout(err) {
		t.Fatalf("readFirstResponseChunk error = %v, want stream first response body byte timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("timeout elapsed = %s", elapsed)
	}
}

func TestReadFirstResponseChunkTreatsEOFAsEmptyResponse(t *testing.T) {
	chunk, err := readFirstResponseChunk(io.NopCloser(strings.NewReader("")), 30*time.Millisecond)
	if err != nil {
		t.Fatalf("readFirstResponseChunk error = %v, want nil", err)
	}
	if len(chunk) != 0 {
		t.Fatalf("chunk = %q, want empty", string(chunk))
	}
}

func TestReadFirstResponseChunkReturnsAvailableBytes(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte("first"))
		_ = writer.Close()
	}()

	chunk, err := readFirstResponseChunk(reader, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("readFirstResponseChunk error = %v", err)
	}
	if string(chunk) != "first" {
		t.Fatalf("chunk = %q, want first", string(chunk))
	}
}

func TestProxyResponseHeaderTimeout(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	defer close(release)

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"waiting": {
				Target:                upstream.URL,
				Timeout:               5,
				ResponseHeaderTimeout: 1,
			},
		},
	}

	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://waiting.localhost/v1/responses", strings.NewReader("{}"))
	req.Host = "waiting.localhost"
	rr := httptest.NewRecorder()

	start := time.Now()
	p.ServeHTTP(rr, req)
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("ServeHTTP elapsed = %s", elapsed)
	}
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}

	finalLog := drainLatestProxyTestLog(t, repo)
	if finalLog.StatusCode != 0 {
		t.Fatalf("status code = %d, want 0", finalLog.StatusCode)
	}
	if !strings.Contains(finalLog.Error, "upstream response header timeout after 1s") {
		t.Fatalf("log error = %q", finalLog.Error)
	}
}

func TestProxyTotalTimeoutStillApplies(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	defer close(release)

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"waiting": {
				Target:  upstream.URL,
				Timeout: 1,
			},
		},
	}

	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://waiting.localhost/v1/responses", nil)
	req.Host = "waiting.localhost"
	rr := httptest.NewRecorder()

	start := time.Now()
	p.ServeHTTP(rr, req)
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("ServeHTTP elapsed = %s", elapsed)
	}
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}

	finalLog := drainLatestProxyTestLog(t, repo)
	if finalLog.StatusCode != 0 {
		t.Fatalf("status code = %d, want 0", finalLog.StatusCode)
	}
	if !strings.Contains(finalLog.Error, "context deadline exceeded") {
		t.Fatalf("log error = %q, want total timeout", finalLog.Error)
	}
}

func TestProxyStreamingFirstResponseBodyByteTimeout(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
		_, _ = w.Write([]byte("data: late\n\n"))
	}))
	defer upstream.Close()
	defer close(release)

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"stream": {
				Target:                 upstream.URL,
				Timeout:                5,
				StreamFirstByteTimeout: 1,
			},
		},
		Logging: config.LoggingConfig{
			MaxRequestBody:   1024,
			MaxResponseBody:  1024,
			BodyPreviewBytes: 1024,
		},
	}

	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://stream.localhost/v1/events", nil)
	req.Host = "stream.localhost"
	rr := httptest.NewRecorder()

	start := time.Now()
	p.ServeHTTP(rr, req)
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("ServeHTTP elapsed = %s", elapsed)
	}
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "stream first response body byte timeout") {
		t.Fatalf("body = %q, want timeout error", rr.Body.String())
	}

	finalLog := drainLatestProxyTestLog(t, repo)
	if !strings.Contains(finalLog.Error, "stream first response body byte timeout") {
		t.Fatalf("log error = %q", finalLog.Error)
	}
}

func TestProxyEmptyStreamingResponsePreservesStatusAndHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream-Empty", "true")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"stream": {
				Target:                 upstream.URL,
				Timeout:                5,
				StreamFirstByteTimeout: 1,
			},
		},
	}

	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://stream.localhost/v1/events", nil)
	req.Host = "stream.localhost"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
	if rr.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get("X-Upstream-Empty") != "true" {
		t.Fatalf("X-Upstream-Empty = %q", rr.Header().Get("X-Upstream-Empty"))
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rr.Body.String())
	}

	finalLog := drainLatestProxyTestLog(t, repo)
	if finalLog.StatusCode != http.StatusAccepted {
		t.Fatalf("log status = %d, want %d", finalLog.StatusCode, http.StatusAccepted)
	}
	if finalLog.Error != "" {
		t.Fatalf("log error = %q, want empty", finalLog.Error)
	}
}

func TestProxyNoContentIsNotRewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Upstream-No-Content", "true")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"stream": {
				Target:                 upstream.URL,
				Timeout:                5,
				StreamFirstByteTimeout: 1,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://stream.localhost/v1/events", nil)
	req.Host = "stream.localhost"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if rr.Header().Get("X-Upstream-No-Content") != "true" {
		t.Fatalf("X-Upstream-No-Content = %q", rr.Header().Get("X-Upstream-No-Content"))
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rr.Body.String())
	}
}

func TestProxyStreamingFirstChunkIsForwardedExactlyOnce(t *testing.T) {
	const firstChunk = "data: first\n\n"
	const secondChunk = "data: second\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, firstChunk)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, secondChunk)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"stream": {
				Target:                 upstream.URL,
				Timeout:                5,
				StreamFirstByteTimeout: 1,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://stream.localhost/v1/events", nil)
	req.Host = "stream.localhost"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	want := firstChunk + secondChunk
	if rr.Body.String() != want {
		t.Fatalf("body = %q, want %q", rr.Body.String(), want)
	}
	if strings.Count(rr.Body.String(), firstChunk) != 1 {
		t.Fatalf("first chunk count = %d, want 1", strings.Count(rr.Body.String(), firstChunk))
	}
}

func TestProxyNonStreamingResponseIgnoresFirstResponseBodyByteTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"json": {
				Target:                 upstream.URL,
				Timeout:                5,
				StreamFirstByteTimeout: 1,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://json.localhost/v1/data", nil)
	req.Host = "json.localhost"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func drainLatestProxyTestLog(t *testing.T, repo *proxyTestRepo) *storage.RequestLog {
	t.Helper()

	var finalLog *storage.RequestLog
	for {
		select {
		case saved := <-repo.saved:
			finalLog = saved
		default:
			if finalLog == nil {
				t.Fatal("no log saved")
			}
			return finalLog
		}
	}
}
