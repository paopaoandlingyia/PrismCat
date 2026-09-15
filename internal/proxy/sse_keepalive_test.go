package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestDetectSSERequestFromJSONPreservesBody(t *testing.T) {
	original := `{"model":"gpt-test","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(original))
	req.Header.Set("Content-Type", "application/json")

	detected, restored, reason := detectSSERequest(req, req.Body)
	if !detected || reason != "" {
		t.Fatalf("detectSSERequest() = %v, %q; want true with no fallback", detected, reason)
	}
	got, err := io.ReadAll(restored)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(got) != original {
		t.Fatalf("restored body = %q, want %q", string(got), original)
	}
}

func TestDetectSSERequestIgnoresNestedStreamFlag(t *testing.T) {
	original := `{"metadata":{"stream":true},"model":"gpt-test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(original))
	req.Header.Set("Content-Type", "application/json")

	detected, restored, reason := detectSSERequest(req, req.Body)
	if detected || reason != "" {
		t.Fatalf("detectSSERequest() = %v, %q; want false with no fallback", detected, reason)
	}
	got, _ := io.ReadAll(restored)
	if string(got) != original {
		t.Fatalf("restored body = %q, want %q", string(got), original)
	}
}

func TestAcceptsEventStreamHonorsExplicitRejection(t *testing.T) {
	if acceptsEventStream("application/json, text/event-stream;q=0") {
		t.Fatal("q=0 event stream must not enable early keepalive")
	}
	if acceptsEventStream("text/event-stream;q=0.000") {
		t.Fatal("zero event-stream quality must not enable early keepalive")
	}
	if !acceptsEventStream("application/json, text/event-stream;q=0.5") {
		t.Fatal("positive event-stream quality should enable early keepalive")
	}
}

func TestEarlySSEKeepaliveCommitsOnlyAfterInterval(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	keepalive := startEarlySSEKeepalive(ctx, recorder, 20*time.Millisecond, cancel)

	time.Sleep(60 * time.Millisecond)
	result := keepalive.stopAndWait()
	if result.err != nil || !result.committed {
		t.Fatalf("keepalive result = %#v", result)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, ": ping\n\n") {
		t.Fatalf("body = %q, want heartbeat", got)
	}
}

func TestEarlySSEKeepaliveStoppedBeforeIntervalDoesNotWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	keepalive := startEarlySSEKeepalive(ctx, recorder, time.Second, cancel)

	result := keepalive.stopAndWait()
	if result.err != nil || result.committed {
		t.Fatalf("keepalive result = %#v", result)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", recorder.Body.String())
	}
}

func TestCopyWithSSEKeepaliveDoesNotCaptureSyntheticHeartbeats(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		time.Sleep(55 * time.Millisecond)
		_, _ = writer.Write([]byte("data: chunk\n\n"))
		_ = writer.Close()
	}()

	recorder := httptest.NewRecorder()
	var capture bytes.Buffer
	copied, err := copyWithSSEKeepalive(
		context.Background(),
		recorder,
		reader,
		reader,
		&capture,
		nil,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("copyWithSSEKeepalive() error = %v", err)
	}
	if copied != int64(len("data: chunk\n\n")) {
		t.Fatalf("copied = %d, want %d", copied, len("data: chunk\n\n"))
	}
	if got := capture.String(); got != "data: chunk\n\n" {
		t.Fatalf("capture = %q, want only upstream chunk", got)
	}
	if got := recorder.Body.String(); !strings.Contains(got, ": ping\n\n") || !strings.Contains(got, "data: chunk\n\n") {
		t.Fatalf("downstream body = %q, want heartbeat and upstream chunk", got)
	}
}

func TestCopyWithSSEKeepaliveDoesNotInjectInsidePartialEvent(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte("data: par"))
		time.Sleep(55 * time.Millisecond)
		_, _ = writer.Write([]byte("tial\n\n"))
		_ = writer.Close()
	}()

	recorder := httptest.NewRecorder()
	_, err := copyWithSSEKeepalive(
		context.Background(),
		recorder,
		reader,
		reader,
		nil,
		nil,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("copyWithSSEKeepalive() error = %v", err)
	}
	if got := recorder.Body.String(); got != "data: partial\n\n" {
		t.Fatalf("downstream body = %q, want unmodified partial event", got)
	}
}

func TestCanInjectSSEHeartbeatRequiresSuccessfulUnencodedSSE(t *testing.T) {
	valid := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}
	if !canInjectSSEHeartbeat(valid) {
		t.Fatal("expected successful unencoded SSE response to allow heartbeat")
	}
	encoded := &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"Content-Type":     []string{"text/event-stream"},
		"Content-Encoding": []string{"gzip"},
	}}
	if canInjectSSEHeartbeat(encoded) {
		t.Fatal("expected encoded SSE response to reject heartbeat injection")
	}
	errorResponse := &http.Response{StatusCode: http.StatusTooManyRequests, Header: valid.Header}
	if canInjectSSEHeartbeat(errorResponse) {
		t.Fatal("expected non-2xx SSE response to reject heartbeat injection")
	}
}

func TestProxyFullSSEKeepaliveCoversResponseHeaderWait(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		time.Sleep(1200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: upstream\n\n"))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server:  config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Logging: config.LoggingConfig{MaxRequestBody: 1024, MaxResponseBody: 1024},
		Upstreams: map[string]config.UpstreamConfig{
			"waiting": {
				Target:  upstream.URL,
				Timeout: 5,
				SSEKeepalive: &config.SSEKeepaliveConfig{
					Mode:            config.SSEKeepaliveModeFull,
					IntervalSeconds: 1,
				},
			},
		},
	}
	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://waiting.localhost/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Host = "waiting.localhost"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, ": ping\n\n") || !strings.Contains(body, "data: upstream\n\n") {
		t.Fatalf("downstream body = %q, want early heartbeat and upstream data", body)
	}
	finalLog := drainLatestProxyTestLog(t, repo)
	if bytes.Contains(finalLog.ResponseBodyRaw, []byte(": ping")) {
		t.Fatalf("captured upstream body contains synthetic heartbeat: %q", string(finalLog.ResponseBodyRaw))
	}
	if string(finalLog.ResponseBodyRaw) != "data: upstream\n\n" {
		t.Fatalf("captured upstream body = %q", string(finalLog.ResponseBodyRaw))
	}
}

func TestProxyAfterHeadersSSEKeepaliveCoversSilentResponseBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("data: upstream\n\n"))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"waiting": {
				Target:  upstream.URL,
				Timeout: 5,
				SSEKeepalive: &config.SSEKeepaliveConfig{
					Mode:            config.SSEKeepaliveModeAfterHeaders,
					IntervalSeconds: 1,
				},
			},
		},
	}
	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://waiting.localhost/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Host = "waiting.localhost"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, ": ping\n\n") || !strings.Contains(body, "data: upstream\n\n") {
		t.Fatalf("downstream body = %q, want body-phase heartbeat and upstream data", body)
	}
}

func TestProxyFullSSEKeepalivePreservesFastUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"failing": {
				Target:  upstream.URL,
				Timeout: 5,
				SSEKeepalive: &config.SSEKeepaliveConfig{
					Mode:            config.SSEKeepaliveModeFull,
					IntervalSeconds: 1,
				},
			},
		},
	}
	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://failing.localhost/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Host = "failing.localhost"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), ": ping") {
		t.Fatalf("body = %q, did not expect heartbeat before fast error", recorder.Body.String())
	}
}

func TestProxyFullSSEKeepaliveRecordsDelayedUpstreamErrorAfterCommit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"failing": {
				Target:  upstream.URL,
				Timeout: 5,
				SSEKeepalive: &config.SSEKeepaliveConfig{
					Mode:            config.SSEKeepaliveModeFull,
					IntervalSeconds: 1,
				},
			},
		},
	}
	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://failing.localhost/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Host = "failing.localhost"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	p.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != ": ping\n\n" {
		t.Fatalf("status/body = %d, %q; want committed heartbeat followed by a terminated stream", recorder.Code, recorder.Body.String())
	}
	finalLog := drainLatestProxyTestLog(t, repo)
	if finalLog.StatusCode != http.StatusTooManyRequests || !strings.Contains(finalLog.Error, "after early SSE keepalive committed") {
		t.Fatalf("log status/error = %d, %q; want delayed upstream status recorded", finalLog.StatusCode, finalLog.Error)
	}
}

func TestProxyFullSSEKeepaliveDoesNotExtendTotalTimeout(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-releaseUpstream:
		}
	}))
	defer upstream.Close()
	defer close(releaseUpstream)

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"waiting": {
				Target:  upstream.URL,
				Timeout: 2,
				SSEKeepalive: &config.SSEKeepaliveConfig{
					Mode:            config.SSEKeepaliveModeFull,
					IntervalSeconds: 1,
				},
			},
		},
	}
	repo := newProxyTestRepo()
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://waiting.localhost/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Host = "waiting.localhost"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	start := time.Now()
	p.ServeHTTP(recorder, req)
	if elapsed := time.Since(start); elapsed < 1800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatalf("ServeHTTP elapsed = %s, want total timeout near 2s", elapsed)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), ": ping\n\n") {
		t.Fatalf("status/body = %d, %q; want committed SSE heartbeat", recorder.Code, recorder.Body.String())
	}
	finalLog := drainLatestProxyTestLog(t, repo)
	if !strings.Contains(finalLog.Error, "context deadline exceeded") {
		t.Fatalf("log error = %q, want total timeout", finalLog.Error)
	}
}
