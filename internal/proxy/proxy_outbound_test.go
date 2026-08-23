package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type preparingProxyTestRepo struct {
	*proxyTestRepo
	cfg *config.Config
}

type ignoredPathProxyTestRepo struct {
	*proxyTestRepo
	mu      sync.Mutex
	ignored []storage.IgnoredPathObservation
}

func (r *ignoredPathProxyTestRepo) RecordIgnoredPath(upstream, requestPath string, seenAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ignored = append(r.ignored, storage.IgnoredPathObservation{Upstream: upstream, Path: requestPath, Count: 1, LastSeen: seenAt})
	return nil
}

func (r *ignoredPathProxyTestRepo) ListIgnoredPaths(storage.IgnoredPathFilter) (storage.IgnoredPathListResult, error) {
	return storage.IgnoredPathListResult{}, nil
}

func (r *ignoredPathProxyTestRepo) DeleteIgnoredPaths(string, string) (int64, error) {
	return 0, nil
}

func (r *ignoredPathProxyTestRepo) DeleteIgnoredPathsBefore(time.Time) (int64, error) {
	return 0, nil
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

func TestProxyDoesNotExposeInvalidUpstreamTarget(t *testing.T) {
	const invalidTarget = "http://user:secret@[::1"
	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {
				Target:        invalidTarget,
				OutboundProxy: "env",
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://openai.localhost:8080/v1/models", nil)
	req.Host = "openai.localhost:8080"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "invalid upstream config\n" {
		t.Fatalf("body = %q, want generic error without target details", got)
	}
}

func TestClientCanceledResponseIsNotForwardError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if !isClientCanceledResponse(ctx, context.Canceled) {
		t.Fatal("client-canceled response was not recognized")
	}
	if isClientCanceledResponse(context.Background(), context.Canceled) {
		t.Fatal("context.Canceled without a canceled client context should remain an error")
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

func TestProxyCanDisableLoggingPerUpstream(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {
				Target:          target.URL,
				LoggingDisabled: true,
			},
		},
		Logging: config.LoggingConfig{
			MaxRequestBody:   1024,
			MaxResponseBody:  1024,
			BodyPreviewBytes: 1024,
			StoreBase64:      true,
		},
	}

	repo := &ignoredPathProxyTestRepo{proxyTestRepo: newProxyTestRepo()}
	p := New(cfg, repo, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://openai.localhost:8080/v1/responses", nil)
	req.Host = "openai.localhost:8080"
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q, want upstream response", rr.Body.String())
	}
	if got := repo.count(); got != 0 {
		t.Fatalf("saved logs = %d, want 0", got)
	}
	if len(repo.ignored) != 0 {
		t.Fatalf("ignored observations = %#v, want none when logging is disabled", repo.ignored)
	}
}

func TestProxyFiltersLoggingByPathAndAuditsExcludedRequests(t *testing.T) {
	filteredBody := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/assets/app.js" {
			body, _ := io.ReadAll(r.Body)
			filteredBody <- string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `"}`))
	}))
	defer target.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {
				Target: target.URL,
				LoggingPathFilter: &config.LoggingPathFilterConfig{
					Mode:  config.LoggingModeAllowlist,
					Rules: []config.LoggingPathRule{{Matcher: config.PathMatcherAnt, Pattern: "/v1/**"}},
				},
			},
		},
		Logging: config.LoggingConfig{MaxRequestBody: 1024, MaxResponseBody: 1024, BodyPreviewBytes: 1024, StoreBase64: true},
		Overrides: config.RequestOverridesConfig{
			Enabled:      true,
			MaxBodyBytes: 1024,
			Upstreams: map[string]config.RequestOverrideUpstreamBinding{
				"openai": {Enabled: true, RuleNames: []string{"mark filtered request"}},
			},
			Rules: []config.RequestOverrideRule{{
				Name:    "mark filtered request",
				Enabled: true,
				Match:   config.RequestOverrideMatch{PathPrefixes: []string{"/assets/"}},
				Patch: []config.RequestOverridePatch{{
					Op:    "set",
					Path:  "filtered",
					Value: true,
				}},
			}},
		},
	}
	repo := &ignoredPathProxyTestRepo{proxyTestRepo: newProxyTestRepo()}
	p := New(cfg, repo, nil, nil)

	for _, requestPath := range []string{"/assets/app.js", "/v1/responses?debug=1"} {
		method := http.MethodGet
		var body io.Reader
		if strings.HasPrefix(requestPath, "/assets/") {
			method = http.MethodPost
			body = strings.NewReader(`{}`)
		}
		req := httptest.NewRequest(method, "http://openai.localhost:8080"+requestPath, body)
		req.Host = "openai.localhost:8080"
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %s status = %d", requestPath, rr.Code)
		}
	}

	if repo.count() == 0 {
		t.Fatal("allowlisted request was not logged")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.ignored) != 1 || repo.ignored[0].Path != "/assets/app.js" {
		t.Fatalf("ignored observations = %#v", repo.ignored)
	}
	if body := <-filteredBody; body != `{"filtered":true}` {
		t.Fatalf("filtered request body = %q, want request overrides to remain active", body)
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
