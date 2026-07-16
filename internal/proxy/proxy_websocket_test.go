package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestProxyWebSocketUpgradeSurvivesRequestTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWebSocketUpgrade(r.Header) {
			http.Error(w, "missing websocket upgrade", http.StatusBadRequest)
			return
		}

		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("upstream response writer does not support hijacking")
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("upstream hijack: %v", err)
			return
		}
		defer conn.Close()

		_, _ = fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\n")
		_, _ = fmt.Fprint(rw, "Connection: Upgrade\r\n")
		_, _ = fmt.Fprint(rw, "Upgrade: websocket\r\n")
		_, _ = fmt.Fprint(rw, "Sec-WebSocket-Accept: test-accept\r\n")
		_, _ = fmt.Fprint(rw, "X-Upstream: websocket\r\n\r\n")
		if err := rw.Flush(); err != nil {
			t.Errorf("upstream flush handshake: %v", err)
			return
		}

		_, _ = io.Copy(conn, conn)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {
				Target:  upstream.URL,
				Timeout: 1,
			},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}

	const earlyPayload = "pipelined-payload"
	_, err = fmt.Fprint(conn, "GET /v1/responses HTTP/1.1\r\n"+
		"Host: openai.localhost\r\n"+
		"Connection: keep-alive, Upgrade\r\n"+
		"Upgrade: websocket\r\n"+
		"Sec-WebSocket-Key: test-key\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n"+
		earlyPayload)
	if err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Upgrade"); !strings.EqualFold(got, "websocket") {
		t.Fatalf("Upgrade = %q, want websocket", got)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != "test-accept" {
		t.Fatalf("Sec-WebSocket-Accept = %q, want test-accept", got)
	}
	if got := resp.Header.Get("X-Upstream"); got != "websocket" {
		t.Fatalf("X-Upstream = %q, want websocket", got)
	}
	earlyEcho := make([]byte, len(earlyPayload))
	if _, err := io.ReadFull(reader, earlyEcho); err != nil {
		t.Fatalf("read pipelined upgraded payload: %v", err)
	}
	if string(earlyEcho) != earlyPayload {
		t.Fatalf("pipelined payload = %q, want %q", string(earlyEcho), earlyPayload)
	}

	// The normal proxy request timeout must only bound the handshake. Once the
	// connection is upgraded, it should remain usable beyond that deadline.
	time.Sleep(1200 * time.Millisecond)
	const payload = "ping-after-timeout"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write upgraded payload: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read upgraded payload: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("upgraded payload = %q, want %q", string(got), payload)
	}
}

func TestProxyWebSocketUpgradeRejectionUsesNormalHTTPResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWebSocketUpgrade(r.Header) {
			http.Error(w, "missing websocket upgrade", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "rejected")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"rejected"}`))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{ProxyDomains: []string{"localhost"}},
		Upstreams: map[string]config.UpstreamConfig{
			"openai": {Target: upstream.URL, Timeout: 5},
		},
	}

	p := New(cfg, newProxyTestRepo(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "http://openai.localhost/v1/responses", nil)
	req.Host = "openai.localhost"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "test-key")
	req.Header.Set("Sec-WebSocket-Version", "13")
	rr := httptest.NewRecorder()

	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-Upstream"); got != "rejected" {
		t.Fatalf("X-Upstream = %q, want rejected", got)
	}
	if got := rr.Body.String(); got != `{"error":"rejected"}` {
		t.Fatalf("body = %q, want rejection JSON", got)
	}
}
