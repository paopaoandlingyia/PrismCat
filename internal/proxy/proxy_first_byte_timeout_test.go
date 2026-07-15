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
		t.Fatalf("readFirstResponseChunk error = %v, want stream first-byte timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("timeout elapsed = %s", elapsed)
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

	var finalLog *storage.RequestLog
	for {
		select {
		case saved := <-repo.saved:
			finalLog = saved
		default:
			if finalLog == nil {
				t.Fatal("no log saved")
			}
			if finalLog.StatusCode != 0 {
				t.Fatalf("status code = %d, want 0", finalLog.StatusCode)
			}
			if !strings.Contains(finalLog.Error, "upstream response header timeout after 1s") {
				t.Fatalf("log error = %q", finalLog.Error)
			}
			return
		}
	}
}

func TestProxyStreamingFirstByteTimeout(t *testing.T) {
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
	if !strings.Contains(rr.Body.String(), "stream first byte timeout") {
		t.Fatalf("body = %q, want timeout error", rr.Body.String())
	}

	var finalLog *storage.RequestLog
	for {
		select {
		case saved := <-repo.saved:
			finalLog = saved
		default:
			if finalLog == nil {
				t.Fatal("no log saved")
			}
			if !strings.Contains(finalLog.Error, "stream first byte timeout") {
				t.Fatalf("log error = %q", finalLog.Error)
			}
			return
		}
	}
}

func TestProxyNonStreamingResponseIgnoresFirstByteTimeout(t *testing.T) {
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
