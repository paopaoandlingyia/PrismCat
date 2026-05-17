package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

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
