package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type preparingProxyTestRepo struct {
	*proxyTestRepo
	cfg *config.Config
}

func (r *preparingProxyTestRepo) SaveLog(log *storage.RequestLog) error {
	entry := log.Clone()
	storage.PrepareLogForPersistence(entry, r.cfg)
	return r.proxyTestRepo.SaveLog(entry)
}

func TestProxyUsesUpstreamOutboundProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("target-ok"))
	}))
	defer target.Close()

	var proxyHits int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		if !r.URL.IsAbs() {
			http.Error(w, "proxy received non-absolute URL", http.StatusBadRequest)
			return
		}

		upstreamReq := r.Clone(r.Context())
		upstreamReq.RequestURI = ""
		upstreamReq.Host = r.URL.Host
		resp, err := http.DefaultTransport.RoundTrip(upstreamReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"gemini": {
				Target:        target.URL,
				OutboundProxy: proxyServer.URL,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://gemini.localhost:8080/v1/models", nil)
	req.Host = "gemini.localhost:8080"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "target-ok" {
		t.Fatalf("body = %q, want target-ok", rr.Body.String())
	}
	if got := atomic.LoadInt32(&proxyHits); got != 1 {
		t.Fatalf("proxy hits = %d, want 1", got)
	}
}

func TestProxyDirectOutboundProxyBypassesProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("direct-ok"))
	}))
	defer target.Close()

	var proxyHits int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		http.Error(w, "should not use proxy", http.StatusBadGateway)
	}))
	defer proxyServer.Close()
	t.Setenv("HTTP_PROXY", proxyServer.URL)

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"local": {
				Target:        target.URL,
				OutboundProxy: "direct",
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://local.localhost:8080/api", nil)
	req.Host = "local.localhost:8080"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "direct-ok" {
		t.Fatalf("body = %q, want direct-ok", rr.Body.String())
	}
	if got := atomic.LoadInt32(&proxyHits); got != 0 {
		t.Fatalf("proxy hits = %d, want 0", got)
	}
}

func TestProxyFiltersUpstreamAccessControlResponseHeaders(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://upstream.example")
		w.Header().Set("Access-Control-Allow-Headers", "X-Upstream")
		w.Header().Set("Access-Control-Expose-Headers", "X-Upstream-Expose")
		w.Header().Set("X-Upstream", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cors-ok"))
	}))
	defer target.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"cors": {
				Target: target.URL,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://cors.localhost:8080/api", nil)
	req.Host = "cors.localhost:8080"
	rr := httptest.NewRecorder()
	rr.Header().Set("Access-Control-Allow-Origin", "*")
	rr.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want only [*]", got)
	}
	if got := rr.Header().Values("Access-Control-Allow-Headers"); len(got) != 0 {
		t.Fatalf("Access-Control-Allow-Headers = %q, want filtered", got)
	}
	if got := rr.Header().Values("Access-Control-Expose-Headers"); len(got) != 0 {
		t.Fatalf("Access-Control-Expose-Headers = %q, want filtered", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want PrismCat value", got)
	}
	if got := rr.Header().Get("X-Upstream"); got != "ok" {
		t.Fatalf("X-Upstream = %q, want ok", got)
	}
}

func TestProxyStillCopiesAccessControlRequestHeaders(t *testing.T) {
	seen := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Access-Control-Request-Headers")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("request-header-ok"))
	}))
	defer target.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"corsreq": {
				Target: target.URL,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://corsreq.localhost:8080/api", nil)
	req.Host = "corsreq.localhost:8080"
	req.Header.Set("Access-Control-Request-Headers", "X-Debug")
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if got := <-seen; got != "X-Debug" {
		t.Fatalf("upstream Access-Control-Request-Headers = %q, want X-Debug", got)
	}
}

func TestProxyPersistsOriginalBodyWhenRequestOverrideFails(t *testing.T) {
	const requestBody = `{"system":"not-array","messages":[]}`

	var targetHits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"claude": {
				Target: target.URL,
			},
		},
		Logging: config.LoggingConfig{
			MaxRequestBody:   1024,
			MaxResponseBody:  1024,
			BodyPreviewBytes: 1024,
			SensitiveHeaders: []string{"Authorization", "x-api-key", "api-key"},
			StoreBase64:      true,
		},
		Overrides: config.RequestOverridesConfig{
			Enabled:      true,
			MaxBodyBytes: 1024,
			Upstreams: map[string]config.RequestOverrideUpstreamBinding{
				"claude": {
					Enabled:   true,
					RuleNames: []string{"prepend system"},
				},
			},
			Rules: []config.RequestOverrideRule{
				{
					Name:    "prepend system",
					Enabled: true,
					Match: config.RequestOverrideMatch{
						Methods: []string{http.MethodPost},
					},
					Patch: []config.RequestOverridePatch{
						{
							Op:    "prepend",
							Path:  "system",
							Value: map[string]interface{}{"type": "text", "text": "injected"},
						},
					},
				},
			},
		},
	}

	baseRepo := newProxyTestRepo()
	repo := &preparingProxyTestRepo{proxyTestRepo: baseRepo, cfg: cfg}
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "http://claude.localhost:8080/v1/messages", strings.NewReader(requestBody))
	req.Host = "claude.localhost:8080"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if got := atomic.LoadInt32(&targetHits); got != 0 {
		t.Fatalf("target hits = %d, want 0", got)
	}

	var finalLog *storage.RequestLog
	for {
		select {
		case saved := <-baseRepo.saved:
			finalLog = saved
		default:
			goto drained
		}
	}

drained:
	if finalLog == nil {
		t.Fatal("no log was saved")
	}
	if finalLog.RequestOverrideError == "" {
		t.Fatal("request override error was not persisted")
	}
	if finalLog.RequestBody != "" {
		t.Fatalf("request body = %q, want empty forwarded body", finalLog.RequestBody)
	}
	if finalLog.RequestBodyOriginal != requestBody {
		t.Fatalf("request body original = %q, want %q", finalLog.RequestBodyOriginal, requestBody)
	}
}
