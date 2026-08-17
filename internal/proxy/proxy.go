package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/live"
	"github.com/paopaoandlingyia/PrismCat/internal/outbound"
	"github.com/paopaoandlingyia/PrismCat/internal/requestoverride"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
	"github.com/paopaoandlingyia/PrismCat/internal/trace"
)

type responseBodyFirstByteTimeoutError struct {
	timeout time.Duration
}

func (e *responseBodyFirstByteTimeoutError) Error() string {
	return fmt.Sprintf("response body first byte timeout after %s", e.timeout)
}

func (e *responseBodyFirstByteTimeoutError) Timeout() bool   { return true }
func (e *responseBodyFirstByteTimeoutError) Temporary() bool { return true }

type responseBodyIdleTimeoutError struct {
	timeout time.Duration
}

func (e *responseBodyIdleTimeoutError) Error() string {
	return fmt.Sprintf("response body idle timeout after %s", e.timeout)
}

func (e *responseBodyIdleTimeoutError) Timeout() bool   { return true }
func (e *responseBodyIdleTimeoutError) Temporary() bool { return true }

func readResponseBodyChunkWithTimeout(body io.ReadCloser, buf []byte, timeout time.Duration, timeoutErr error) (int, error) {
	if timeout <= 0 {
		return body.Read(buf)
	}

	var stateMu sync.Mutex
	completed := false
	timedOut := false
	timer := time.AfterFunc(timeout, func() {
		stateMu.Lock()
		if completed {
			stateMu.Unlock()
			return
		}
		timedOut = true
		stateMu.Unlock()
		_ = body.Close()
	})

	var n int
	var err error
	for n == 0 && err == nil {
		n, err = body.Read(buf)
	}

	stateMu.Lock()
	completed = true
	didTimeout := timedOut
	stateMu.Unlock()
	timer.Stop()

	if didTimeout {
		return 0, timeoutErr
	}
	return n, err
}

func readFirstResponseBodyChunk(body io.ReadCloser, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		return nil, nil
	}

	buf := make([]byte, copyBufferSize)
	n, err := readResponseBodyChunkWithTimeout(
		body,
		buf,
		timeout,
		&responseBodyFirstByteTimeoutError{timeout: timeout},
	)
	if n > 0 {
		return buf[:n], nil
	}
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	return nil, err
}

type responseBodyIdleTimeoutReader struct {
	body    io.ReadCloser
	timeout time.Duration
	active  bool
}

func (r *responseBodyIdleTimeoutReader) Read(buf []byte) (int, error) {
	if !r.active {
		n, err := r.body.Read(buf)
		if n > 0 {
			r.active = true
		}
		return n, err
	}
	return readResponseBodyChunkWithTimeout(
		r.body,
		buf,
		r.timeout,
		&responseBodyIdleTimeoutError{timeout: r.timeout},
	)
}

// Proxy handles host-based upstream routing and request/response logging.
type Proxy struct {
	cfg      *config.Config
	repo     storage.Repository
	live     *live.Registry
	clients  *outbound.ClientCache
	traceSeq *trace.Sequencer
}

const copyBufferSize = 32 * 1024

var copyBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, copyBufferSize)
		return &buf
	},
}

// New creates a new proxy instance.
func New(cfg *config.Config, repo storage.Repository, liveRegistry *live.Registry, traceSeq *trace.Sequencer) *Proxy {
	return &Proxy{
		cfg:      cfg,
		repo:     repo,
		live:     liveRegistry,
		clients:  outbound.NewClientCache(100, 50),
		traceSeq: traceSeq,
	}
}

// ServeHTTP proxies the request to the configured upstream and logs the traffic.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	serverCfg := p.cfg.ServerSnapshot()
	loggingCfg := p.cfg.LoggingSnapshot()
	var logMu sync.Mutex

	upstreamName, requestURL := p.resolveRoute(r, serverCfg)
	if upstreamName == "" {
		http.Error(w, "invalid proxy route: missing upstream", http.StatusBadRequest)
		return
	}

	upstream, overrideCfg, ok := p.cfg.ResolveUpstreamSnapshot(upstreamName)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown upstream: %s", upstreamName), http.StatusBadGateway)
		return
	}
	loggingEnabled := !upstream.LoggingDisabled
	if loggingEnabled && !upstream.LoggingPathFilter.Allows(requestURL.Path) {
		loggingEnabled = false
		if ignored, ok := p.repo.(storage.IgnoredPathRepository); ok {
			if err := ignored.RecordIgnoredPath(upstreamName, requestURL.Path, startTime); err != nil && !errors.Is(err, storage.ErrAsyncClosed) {
				log.Printf("record ignored path failed: %v", err)
			}
		}
	}

	responseHeaderTimeout := time.Duration(upstream.ResponseHeaderTimeout) * time.Second
	client, err := p.clients.ClientWithResponseHeaderTimeout(upstream.OutboundProxy, responseHeaderTimeout)
	if err != nil {
		http.Error(w, "invalid upstream outbound proxy config", http.StatusInternalServerError)
		return
	}
	targetURL, err := url.Parse(upstream.Target)
	if err != nil {
		http.Error(w, "invalid upstream config", http.StatusInternalServerError)
		return
	}

	upstreamURL := buildUpstreamURL(targetURL, requestURL)

	// Initial log entry (best-effort). This allows the UI to show in-flight requests.
	traceID := strings.TrimSpace(r.Header.Get("X-PrismCat-Trace-ID"))
	var traceSeq int
	if loggingEnabled && traceID != "" && p.traceSeq != nil {
		traceSeq = p.traceSeq.Next(traceID)
	}

	logEntry := &storage.RequestLog{
		ID:             uuid.NewString(),
		CreatedAt:      startTime,
		Upstream:       upstreamName,
		UpstreamTarget: upstream.TargetName,
		Method:         r.Method,
		Path:           requestURL.Path,
		Query:          requestURL.RawQuery,
		TargetURL:      upstreamURL.String(),
		Tag:            r.Header.Get("X-PrismCat-Tag"),
		TraceID:        traceID,
		TraceSeq:       traceSeq,

		RequestHeaders: p.sanitizeHeaders(r.Header, loggingCfg.SensitiveHeaders),
	}
	if loggingEnabled {
		logMu.Lock()
		p.saveLogSnapshot(logEntry)
		p.publishInitialLive(logEntry)
		logMu.Unlock()
	}

	// Per-request timeout: do NOT mutate a shared http.Client timeout.
	timeoutSeconds := upstream.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = config.DefaultUpstreamTimeoutSeconds
	}
	webSocketUpgrade := isWebSocketUpgrade(r.Header)
	var ctx context.Context
	var cancel context.CancelFunc
	var totalTimeoutTimer *time.Timer
	if webSocketUpgrade {
		// For an upgrade request, the normal timeout only bounds the handshake.
		// Keeping a deadline on the request context would close a healthy upgraded
		// connection when the ordinary HTTP request timeout expires.
		ctx, cancel = context.WithCancel(r.Context())
		totalTimeoutTimer = time.AfterFunc(time.Duration(timeoutSeconds)*time.Second, cancel)
	} else {
		ctx, cancel = context.WithTimeout(r.Context(), time.Duration(timeoutSeconds)*time.Second)
	}
	defer cancel()
	if totalTimeoutTimer != nil {
		defer totalTimeoutTimer.Stop()
	}

	// Capture request body for logging while streaming it to the upstream (no truncation of forwarding).
	var reqCapture *limitedCapture
	if loggingEnabled {
		reqCapture = newLimitedCapture(loggingCfg.MaxRequestBody)
	}
	var body io.Reader
	var contentLength = r.ContentLength
	requestBodySource := r.Body

	overrideInfo := requestoverride.RequestInfo{
		Upstream:        upstreamName,
		Method:          r.Method,
		Path:            requestURL.Path,
		ContentType:     r.Header.Get("Content-Type"),
		ContentEncoding: r.Header.Get("Content-Encoding"),
	}
	if r.Body != nil && r.Body != http.NoBody && requestoverride.HasCandidate(overrideCfg, overrideInfo) {
		rawBody, readErr := readRequestBodyLimited(r.Body, overrideCfg.MaxBodyBytes)
		if readErr != nil {
			logMu.Lock()
			logEntry.Error = fmt.Sprintf("request override failed: %v", readErr)
			logEntry.RequestOverrideError = readErr.Error()
			if loggingEnabled {
				p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
				p.completeLive(logEntry)
			}
			logMu.Unlock()
			http.Error(w, "request override failed: "+readErr.Error(), http.StatusBadRequest)
			return
		}
		result, applyErr := requestoverride.Apply(overrideCfg, overrideInfo, rawBody)
		if applyErr != nil {
			if errors.Is(applyErr, requestoverride.ErrUnsupportedContent) || errors.Is(applyErr, requestoverride.ErrUnsupportedEncoding) {
				logEntry.RequestOverrideError = applyErr.Error()
				logEntry.RequestBodyOriginalRaw = append([]byte(nil), rawBody...)
				requestBodySource = io.NopCloser(bytes.NewReader(rawBody))
				contentLength = int64(len(rawBody))
			} else {
				logMu.Lock()
				logEntry.Error = fmt.Sprintf("request override failed: %v", applyErr)
				logEntry.RequestOverrideError = applyErr.Error()
				logEntry.RequestBodyOriginalRaw = append([]byte(nil), rawBody...)
				if loggingEnabled {
					p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
					p.completeLive(logEntry)
				}
				logMu.Unlock()
				http.Error(w, "request override failed: "+applyErr.Error(), http.StatusBadRequest)
				return
			}
		}
		if len(result.AppliedRuleNames) > 0 {
			logEntry.RequestOverrideApplied = true
			logEntry.RequestOverrideRules = append([]string(nil), result.AppliedRuleNames...)
			logEntry.RequestBodyOriginalRaw = append([]byte(nil), rawBody...)
			logEntry.RequestBodyFinalRaw = append([]byte(nil), result.Body...)
			requestBodySource = io.NopCloser(bytes.NewReader(result.Body))
			contentLength = int64(len(result.Body))
		} else {
			requestBodySource = io.NopCloser(bytes.NewReader(rawBody))
			contentLength = int64(len(rawBody))
		}
	}

	if requestBodySource != nil && requestBodySource != http.NoBody {
		if loggingEnabled {
			tee := io.TeeReader(requestBodySource, reqCapture)
			rc := &teeReadCloser{r: tee, c: requestBodySource}
			if p.live != nil || loggingCfg.EarlyRequestBodySnapshot {
				bodyDone := make(chan struct{})
				body = &eofNotifyReadCloser{rc: rc, done: bodyDone}

				// Publish the request body to the live detail view once it has been sent
				// upstream. Persisting this in-flight snapshot remains optional because it
				// adds an extra DB write per request.
				go func() {
					select {
					case <-bodyDone:
					case <-ctx.Done():
						// Avoid leaking goroutines if the transport never fully reads the body.
					}

					logMu.Lock()
					p.applyRequestCapture(logEntry, reqCapture)
					if loggingCfg.EarlyRequestBodySnapshot {
						p.saveLogSnapshot(logEntry)
					}
					p.publishRequestReady(logEntry)
					logMu.Unlock()
				}()
			} else {
				body = rc
			}
		} else {
			body = requestBodySource
		}
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), body)
	if err != nil {
		logMu.Lock()
		logEntry.Error = fmt.Sprintf("create upstream request: %v", err)
		if loggingEnabled {
			p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
			p.completeLive(logEntry)
		}
		logMu.Unlock()
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	p.copyHeaders(upstreamReq.Header, r.Header)
	// Host is special: set the field (Header["Host"] is ignored by net/http client).
	upstreamReq.Host = targetURL.Host
	// Preserve original length semantics if present.
	upstreamReq.ContentLength = contentLength
	upstreamReq.Header.Del("Content-Length")

	if headerChanges, headerRuleNames := requestoverride.ApplyHeaders(overrideCfg, overrideInfo, upstreamReq.Header); len(headerChanges) > 0 {
		logMu.Lock()
		logEntry.RequestHeaderOverrideApplied = true
		logEntry.RequestHeadersOriginal = logEntry.RequestHeaders
		logEntry.RequestHeaders = p.sanitizeHeaders(upstreamReq.Header, loggingCfg.SensitiveHeaders)
		if raw, err := json.Marshal(sanitizeHeaderChanges(headerChanges, loggingCfg.SensitiveHeaders)); err == nil {
			logEntry.RequestHeaderOverrideChanges = raw
		}
		if !logEntry.RequestOverrideApplied {
			logEntry.RequestOverrideApplied = true
		}
		for _, name := range headerRuleNames {
			if !containsString(logEntry.RequestOverrideRules, name) {
				logEntry.RequestOverrideRules = append(logEntry.RequestOverrideRules, name)
			}
		}
		logMu.Unlock()
	}
	if webSocketUpgrade {
		// These hop-by-hop headers are intentionally restored only for the
		// dedicated protocol-switching path.
		upstreamReq.Header.Set("Connection", "Upgrade")
		upstreamReq.Header.Set("Upgrade", r.Header.Get("Upgrade"))
	}

	resp, err := client.Do(upstreamReq)
	if err != nil {
		logMu.Lock()
		if isResponseHeaderTimeout(err) && upstream.ResponseHeaderTimeout > 0 {
			logEntry.Error = fmt.Sprintf("upstream response header timeout after %ds", upstream.ResponseHeaderTimeout)
		} else {
			logEntry.Error = fmt.Sprintf("upstream request failed: %v", err)
		}
		if loggingEnabled {
			p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
			p.completeLive(logEntry)
		}
		logMu.Unlock()
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if webSocketUpgrade && resp.StatusCode == http.StatusSwitchingProtocols {
		if totalTimeoutTimer != nil {
			totalTimeoutTimer.Stop()
		}

		onUpgraded := func() {
			logMu.Lock()
			logEntry.StatusCode = resp.StatusCode
			logEntry.ResponseHeaders = p.headerToMap(resp.Header)
			logEntry.Streaming = true
			if loggingEnabled {
				p.publishHeaders(logEntry)
			}
			logMu.Unlock()
		}
		hijacked, tunnelErr := p.handleWebSocketUpgrade(w, r, resp, onUpgraded)

		logMu.Lock()
		if tunnelErr != nil {
			logEntry.Error = fmt.Sprintf("websocket proxy failed: %v", tunnelErr)
		}
		if !hijacked {
			logEntry.StatusCode = http.StatusBadGateway
			logEntry.ResponseHeaders = p.headerToMap(resp.Header)
		}
		if loggingEnabled {
			p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
			p.completeLive(logEntry)
		}
		logMu.Unlock()

		if tunnelErr != nil && !hijacked {
			http.Error(w, fmt.Sprintf("websocket upstream error: %v", tunnelErr), http.StatusBadGateway)
		}
		return
	}

	streaming := isStreaming(resp.Header)
	var firstResponseChunk []byte
	if upstream.ResponseBodyFirstByteTimeout > 0 {
		firstResponseChunk, err = readFirstResponseBodyChunk(
			resp.Body,
			time.Duration(upstream.ResponseBodyFirstByteTimeout)*time.Second,
		)
		if err != nil {
			logMu.Lock()
			logEntry.StatusCode = resp.StatusCode
			logEntry.ResponseHeaders = p.headerToMap(resp.Header)
			logEntry.Streaming = streaming
			if isResponseBodyFirstByteTimeout(err) {
				logEntry.Error = err.Error()
			} else {
				logEntry.Error = fmt.Sprintf("read response body first byte failed: %v", err)
			}
			if loggingEnabled {
				p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
				p.completeLive(logEntry)
			}
			logMu.Unlock()
			http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
			return
		}
	}

	logMu.Lock()
	logEntry.StatusCode = resp.StatusCode
	logEntry.ResponseHeaders = p.headerToMap(resp.Header)
	logEntry.Streaming = streaming
	if loggingEnabled {
		p.publishHeaders(logEntry)
	}
	logMu.Unlock()

	// Forward response headers and status code.
	p.copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	// Forward response body while capturing a bounded preview for logging.
	var respCapture *limitedCapture
	var respCaptureWriter io.Writer
	if loggingEnabled {
		respCapture = newLimitedCapture(loggingCfg.MaxResponseBody)
		respCaptureWriter = respCapture
	}
	var responseBodyReader io.Reader = resp.Body
	if upstream.ResponseBodyIdleTimeout > 0 {
		responseBodyReader = &responseBodyIdleTimeoutReader{
			body:    resp.Body,
			timeout: time.Duration(upstream.ResponseBodyIdleTimeout) * time.Second,
			active:  len(firstResponseChunk) > 0,
		}
	}
	if len(firstResponseChunk) > 0 {
		responseBodyReader = io.MultiReader(bytes.NewReader(firstResponseChunk), responseBodyReader)
	}
	copied, copyErr := copyWithOptionalFlush(
		w,
		responseBodyReader,
		respCaptureWriter,
		logEntry.Streaming,
		p.makeLiveChunkPublisherIfEnabled(loggingEnabled, logEntry, resp.Header),
	)
	logMu.Lock()
	logEntry.ResponseBodySize = copied
	if copyErr != nil {
		// Client-side cancellation is common for streaming responses when callers
		// stop after receiving enough data. Keep the partial capture, but do not
		// classify it as an upstream/proxy failure.
		if isClientCanceledResponse(r.Context(), copyErr) {
			logEntry.Truncated = true
		} else if isResponseBodyIdleTimeout(copyErr) {
			logEntry.Error = copyErr.Error()
		} else {
			// The response may already be partially written; we can only record the error.
			logEntry.Error = fmt.Sprintf("forward response failed: %v", copyErr)
		}
	}
	if loggingEnabled {
		p.finalizeAndSaveLog(logEntry, startTime, reqCapture, respCapture)
		p.completeLive(logEntry)
	}
	logMu.Unlock()
}

func (p *Proxy) resolveRoute(r *http.Request, serverCfg config.ServerConfig) (string, *url.URL) {
	if serverCfg.EnablePathRouting && p.cfg.IsUIHost(r.Host) {
		if upstream, forwardPath, ok := config.ExtractPathUpstream(r.URL.Path, serverCfg.PathRoutingPrefix); ok {
			requestURL := *r.URL
			requestURL.Path = forwardPath
			requestURL.RawPath = ""
			return upstream, &requestURL
		}
	}

	return config.ExtractSubdomain(r.Host, serverCfg.ProxyDomains), r.URL
}

func (p *Proxy) finalizeAndSaveLog(log *storage.RequestLog, startTime time.Time, reqCap, respCap *limitedCapture) {
	p.applyRequestCapture(log, reqCap)
	p.applyResponseCapture(log, respCap)
	log.Latency = time.Since(startTime).Milliseconds()

	p.saveLogSnapshot(log)
}

func (p *Proxy) applyRequestCapture(log *storage.RequestLog, reqCap *limitedCapture) {
	if log == nil || reqCap == nil {
		return
	}
	raw, reqSize, reqTruncated := reqCap.Snapshot()
	log.RequestBodyRaw = raw
	log.RequestBodySize = reqSize
	log.RequestBodyCaptureTruncated = reqTruncated
}

func (p *Proxy) applyResponseCapture(log *storage.RequestLog, respCap *limitedCapture) {
	if log == nil || respCap == nil {
		return
	}
	raw, respSize, respTruncated := respCap.Snapshot()
	log.ResponseBodyRaw = raw
	log.ResponseBodySize = respSize
	log.ResponseBodyCaptureTruncated = respTruncated
}

func (p *Proxy) saveLogSnapshot(entry *storage.RequestLog) {
	if err := p.repo.SaveLog(entry); err != nil {
		// Best-effort: avoid crashing the request path.
		log.Printf("save log failed/dropped: %v", err)
	}
}

func (p *Proxy) publishInitialLive(logEntry *storage.RequestLog) {
	if p.live == nil || logEntry == nil {
		return
	}
	p.live.Register(logEntry)
}

func (p *Proxy) publishRequestReady(logEntry *storage.RequestLog) {
	if p.live == nil || logEntry == nil {
		return
	}

	contentType := storage.FirstHeaderValue(logEntry.RequestHeaders, "Content-Type")
	contentEncoding := storage.FirstHeaderValue(logEntry.RequestHeaders, "Content-Encoding")
	body, _ := p.formatLiveBody(contentType, contentEncoding, logEntry.RequestBodyRaw, p.cfg.LoggingSnapshot().BodyPreviewBytes)

	p.live.UpdateSnapshot(logEntry.ID, func(snapshot *storage.RequestLog) {
		snapshot.RequestBody = body
		snapshot.RequestBodySize = logEntry.RequestBodySize
		snapshot.Truncated = snapshot.Truncated || logEntry.RequestBodyCaptureTruncated
	})
}

func (p *Proxy) publishHeaders(logEntry *storage.RequestLog) {
	if p.live == nil || logEntry == nil {
		return
	}

	p.live.UpdateSnapshot(logEntry.ID, func(snapshot *storage.RequestLog) {
		snapshot.StatusCode = logEntry.StatusCode
		snapshot.ResponseHeaders = storage.CloneHeaders(logEntry.ResponseHeaders)
		snapshot.Streaming = logEntry.Streaming
	})
}

func (p *Proxy) makeLiveChunkPublisher(logEntry *storage.RequestLog, headers http.Header) func([]byte) {
	if p.live == nil || logEntry == nil {
		return nil
	}

	if !isLiveTextResponse(headers.Get("Content-Type")) {
		return nil
	}

	contentEncoding := strings.TrimSpace(headers.Get("Content-Encoding"))
	if contentEncoding != "" && !strings.EqualFold(contentEncoding, "identity") {
		return nil
	}

	return func(chunk []byte) {
		p.publishResponseChunk(logEntry.ID, headers.Get("Content-Type"), chunk)
	}
}

func (p *Proxy) makeLiveChunkPublisherIfEnabled(enabled bool, logEntry *storage.RequestLog, headers http.Header) func([]byte) {
	if !enabled {
		return nil
	}
	return p.makeLiveChunkPublisher(logEntry, headers)
}

func (p *Proxy) publishResponseChunk(id string, contentType string, chunk []byte) {
	if p.live == nil || id == "" || len(chunk) == 0 {
		return
	}

	formatted := httpbody.FormatForDisplay(contentType, "", chunk, httpbody.FormatOptions{
		MaxOutputBytes:  int64(len(chunk)),
		TrimLargeBase64: !p.cfg.LoggingSnapshot().StoreBase64,
	})
	if formatted.Text == "" || strings.HasPrefix(formatted.Text, "[binary content omitted;") {
		return
	}

	p.live.AppendResponseChunk(id, formatted.Text, int64(len(chunk)))
}

func (p *Proxy) completeLive(logEntry *storage.RequestLog) {
	if p.live == nil || logEntry == nil {
		return
	}

	finalLog := logEntry.Clone()
	loggingCfg := p.cfg.LoggingSnapshot()

	requestBody, _ := p.formatLiveBody(
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Type"),
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Encoding"),
		finalLog.RequestBodyRaw,
		loggingCfg.BodyPreviewBytes,
	)
	responseBody, _ := p.formatLiveBody(
		storage.FirstHeaderValue(finalLog.ResponseHeaders, "Content-Type"),
		storage.FirstHeaderValue(finalLog.ResponseHeaders, "Content-Encoding"),
		finalLog.ResponseBodyRaw,
		loggingCfg.BodyPreviewBytes,
	)
	requestBodyOriginal, _ := p.formatLiveBody(
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Type"),
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Encoding"),
		finalLog.RequestBodyOriginalRaw,
		loggingCfg.BodyPreviewBytes,
	)
	requestBodyFinal, _ := p.formatLiveBody(
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Type"),
		storage.FirstHeaderValue(finalLog.RequestHeaders, "Content-Encoding"),
		finalLog.RequestBodyFinalRaw,
		loggingCfg.BodyPreviewBytes,
	)

	finalLog.RequestBody = requestBody
	finalLog.ResponseBody = responseBody
	finalLog.RequestBodyOriginal = requestBodyOriginal
	finalLog.RequestBodyFinal = requestBodyFinal
	finalLog.Truncated = finalLog.Truncated ||
		finalLog.RequestBodyCaptureTruncated ||
		finalLog.ResponseBodyCaptureTruncated

	p.live.Complete(finalLog)
}

func (p *Proxy) formatLiveBody(contentType string, contentEncoding string, body []byte, maxOutputBytes int64) (string, bool) {
	if len(body) == 0 {
		return "", false
	}

	formatted := httpbody.FormatPreviewForDisplay(contentType, contentEncoding, body, httpbody.FormatOptions{
		MaxOutputBytes:  maxOutputBytes,
		TrimLargeBase64: !p.cfg.LoggingSnapshot().StoreBase64,
	})
	return formatted.Text, formatted.Truncated
}

// copyHeaders copies HTTP headers excluding hop-by-hop headers.
func (p *Proxy) copyHeaders(dst, src http.Header) {
	// RFC 7230 section 6.1: headers listed in "Connection" are hop-by-hop too.
	connectionTokens := parseConnectionHeader(src.Values("Connection"))

	for k, vv := range src {
		if isHopByHopHeader(k) || connectionTokens[textproto.CanonicalMIMEHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// copyResponseHeaders copies upstream response headers while keeping CORS policy
// under PrismCat's control.
func (p *Proxy) copyResponseHeaders(dst, src http.Header) {
	// RFC 7230 section 6.1: headers listed in "Connection" are hop-by-hop too.
	connectionTokens := parseConnectionHeader(src.Values("Connection"))

	for k, vv := range src {
		if isHopByHopHeader(k) ||
			connectionTokens[textproto.CanonicalMIMEHeaderKey(k)] ||
			isAccessControlHeader(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isWebSocketUpgrade(headers http.Header) bool {
	connectionTokens := parseConnectionHeader(headers.Values("Connection"))
	return connectionTokens["Upgrade"] && strings.EqualFold(strings.TrimSpace(headers.Get("Upgrade")), "websocket")
}

func (p *Proxy) handleWebSocketUpgrade(
	w http.ResponseWriter,
	r *http.Request,
	resp *http.Response,
	onUpgraded func(),
) (bool, error) {
	if !isWebSocketUpgrade(resp.Header) {
		return false, fmt.Errorf("upstream returned 101 without a valid websocket upgrade")
	}

	upstreamConn, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return false, fmt.Errorf("upstream 101 response body is not writable")
	}

	clientConn, clientBuf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return false, fmt.Errorf("hijack client connection: %w", err)
	}
	hijacked := true
	defer clientConn.Close()
	defer upstreamConn.Close()

	// Keep PrismCat's existing CORS policy, restore only the headers required
	// for switching protocols, and preserve WebSocket headers such as
	// Sec-WebSocket-Accept and Sec-WebSocket-Protocol.
	p.copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Upgrade", resp.Header.Get("Upgrade"))

	upgradeResp := new(http.Response)
	*upgradeResp = *resp
	upgradeResp.Header = w.Header()
	upgradeResp.Body = nil
	upgradeResp.ContentLength = 0
	upgradeResp.TransferEncoding = nil
	if err := upgradeResp.Write(clientBuf); err != nil {
		return hijacked, fmt.Errorf("write upgrade response: %w", err)
	}
	if err := clientBuf.Flush(); err != nil {
		return hijacked, fmt.Errorf("flush upgrade response: %w", err)
	}
	if onUpgraded != nil {
		onUpgraded()
	}

	type copyResult struct {
		direction string
		err       error
	}
	results := make(chan copyResult, 2)
	go func() {
		// Read through the hijacked buffer first so any bytes already received
		// after the HTTP headers are not stranded there.
		_, copyErr := io.Copy(upstreamConn, clientBuf)
		results <- copyResult{direction: "client to upstream", err: copyErr}
	}()
	go func() {
		_, copyErr := io.Copy(clientConn, upstreamConn)
		results <- copyResult{direction: "upstream to client", err: copyErr}
	}()

	first := <-results
	// Terminate the peer copy when either side closes, then wait for it so the
	// tunnel cannot leave a blocked goroutine behind.
	_ = clientConn.Close()
	_ = upstreamConn.Close()
	<-results

	if isExpectedWebSocketClose(r.Context(), first.err) {
		return hijacked, nil
	}
	return hijacked, fmt.Errorf("copy %s: %w", first.direction, first.err)
}

func isExpectedWebSocketClose(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "closed network connection") ||
		strings.Contains(lower, "connection reset by peer") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "forcibly closed")
}

func isAccessControlHeader(header string) bool {
	return strings.HasPrefix(strings.ToLower(header), "access-control-")
}

func isSensitiveHeader(name string, sensitiveHeaders []string) bool {
	for _, sensitive := range sensitiveHeaders {
		if strings.EqualFold(name, sensitive) {
			return true
		}
	}
	return false
}

func maskSensitiveHeaderValue(value string) string {
	if len(value) > 10 {
		return value[:5] + "***" + value[len(value)-3:]
	}
	return "***"
}

func sanitizeHeaderChanges(changes []requestoverride.HeaderChange, sensitiveHeaders []string) []requestoverride.HeaderChange {
	sanitized := make([]requestoverride.HeaderChange, len(changes))
	copy(sanitized, changes)
	for i := range sanitized {
		if !isSensitiveHeader(sanitized[i].Name, sensitiveHeaders) {
			continue
		}
		if sanitized[i].Value != "" {
			sanitized[i].Value = maskSensitiveHeaderValue(sanitized[i].Value)
		}
		if sanitized[i].OldValue != "" {
			sanitized[i].OldValue = maskSensitiveHeaderValue(sanitized[i].OldValue)
		}
	}
	return sanitized
}

// sanitizeHeaders masks configured sensitive headers.
func (p *Proxy) sanitizeHeaders(headers http.Header, sensitiveHeaders []string) map[string][]string {
	result := make(map[string][]string)
	for k, vv := range headers {
		if len(vv) == 0 {
			continue
		}

		newValues := make([]string, len(vv))
		for i, value := range vv {
			if isSensitiveHeader(k, sensitiveHeaders) {
				newValues[i] = maskSensitiveHeaderValue(value)
			} else {
				newValues[i] = value
			}
		}
		result[k] = newValues
	}
	return result
}

func (p *Proxy) headerToMap(headers http.Header) map[string][]string {
	result := make(map[string][]string)
	for k, vv := range headers {
		if len(vv) > 0 {
			result[k] = vv
		}
	}
	return result
}

func isHopByHopHeader(header string) bool {
	// RFC 7230, section 6.1.
	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, h := range hopByHop {
		if strings.EqualFold(header, h) {
			return true
		}
	}
	return false
}

func parseConnectionHeader(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	m := make(map[string]bool)
	for _, v := range values {
		for _, token := range strings.Split(v, ",") {
			t := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(token))
			if t != "" {
				m[t] = true
			}
		}
	}
	return m
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// isStreaming determines whether an HTTP response is a streaming response
// by inspecting Content-Type and transport-related headers.
func isStreaming(header http.Header) bool {
	// 1. Check Content-Type for known streaming media types.
	contentType := header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			// Fallback: raw substring check.
			lower := strings.ToLower(contentType)
			for _, t := range streamingMediaTypes {
				if strings.Contains(lower, t) {
					return true
				}
			}
		} else {
			for _, t := range streamingMediaTypes {
				if strings.EqualFold(mediaType, t) {
					return true
				}
			}
		}
	}

	// 2. X-Accel-Buffering: no (commonly set by Nginx or upstream proxies).
	if strings.EqualFold(header.Get("X-Accel-Buffering"), "no") {
		return true
	}

	return false
}

// streamingMediaTypes lists Content-Type values that indicate a streaming response.
var streamingMediaTypes = []string{
	"text/event-stream",
	"application/x-ndjson",
	"application/stream+json",
	"application/json-seq",
}

func buildUpstreamURL(base *url.URL, in *url.URL) *url.URL {
	u := *base // copy
	u.Path = singleJoiningSlash(base.Path, in.Path)
	u.RawQuery = mergeQuery(base.RawQuery, in.RawQuery)
	u.Fragment = ""
	return &u
}

func mergeQuery(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "&" + b
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" || b == "" {
			return a + b
		}
		return a + "/" + b
	default:
		return a + b
	}
}

// teeReadCloser turns an io.Reader + io.Closer into an io.ReadCloser.
// Used to ensure the upstream transport closes the original request body.
type teeReadCloser struct {
	r io.Reader
	c io.Closer
}

func (t *teeReadCloser) Read(p []byte) (int, error) { return t.r.Read(p) }
func (t *teeReadCloser) Close() error               { return t.c.Close() }

// eofNotifyReadCloser signals on EOF/Close. Used to snapshot request bodies as
// soon as they're fully sent to the upstream.
type eofNotifyReadCloser struct {
	rc   io.ReadCloser
	done chan struct{}
	once sync.Once
}

func (n *eofNotifyReadCloser) Read(p []byte) (int, error) {
	nn, err := n.rc.Read(p)
	if err == io.EOF {
		n.once.Do(func() {
			close(n.done)
		})
	}
	return nn, err
}

func (n *eofNotifyReadCloser) Close() error {
	err := n.rc.Close()
	n.once.Do(func() {
		close(n.done)
	})
	return err
}

type limitedCapture struct {
	max int64

	mu sync.Mutex

	buf       []byte
	total     int64
	truncated bool
}

func newLimitedCapture(max int64) *limitedCapture {
	return &limitedCapture{max: max}
}

func (c *limitedCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total += int64(len(p))
	if c.max <= 0 {
		return len(p), nil
	}

	remaining := c.max - int64(len(c.buf))
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		c.buf = append(c.buf, p[:remaining]...)
		c.truncated = true
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

func (c *limitedCapture) Snapshot() ([]byte, int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) == 0 {
		return nil, c.total, c.truncated
	}
	buf := make([]byte, len(c.buf))
	copy(buf, c.buf)
	return buf, c.total, c.truncated
}

func isLiveTextResponse(contentType string) bool {
	if contentType == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if mediaType == "application/json" ||
		mediaType == "application/xml" ||
		mediaType == "application/x-www-form-urlencoded" ||
		mediaType == "application/x-ndjson" ||
		mediaType == "application/stream+json" ||
		mediaType == "application/json-seq" {
		return true
	}
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

func readRequestBodyLimited(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, requestoverride.ErrBodyTooLarge
	}
	return data, nil
}

func copyWithOptionalFlush(dst http.ResponseWriter, src io.Reader, capture io.Writer, flush bool, onChunk func([]byte)) (int64, error) {
	var w io.Writer = dst
	if capture != nil {
		w = io.MultiWriter(dst, capture)
	}

	bufPtr := copyBufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer copyBufferPool.Put(bufPtr)

	if !flush && onChunk == nil {
		return io.CopyBuffer(w, src, buf)
	}

	flusher, ok := dst.(http.Flusher)
	if !ok && onChunk == nil {
		return io.CopyBuffer(w, src, buf)
	}

	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			wn, werr := w.Write(buf[:n])
			total += int64(wn)
			if onChunk != nil && wn > 0 {
				onChunk(buf[:wn])
			}
			if werr != nil {
				return total, werr
			}
			if ok {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

func isResponseHeaderTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	return errors.As(err, &netErr) && netErr.Timeout() && strings.Contains(strings.ToLower(err.Error()), "response headers")
}

func isResponseBodyFirstByteTimeout(err error) bool {
	var timeoutErr *responseBodyFirstByteTimeoutError
	return errors.As(err, &timeoutErr)
}

func isResponseBodyIdleTimeout(err error) bool {
	var timeoutErr *responseBodyIdleTimeoutError
	return errors.As(err, &timeoutErr)
}

func isClientCanceledResponse(ctx context.Context, err error) bool {
	return err != nil && ctx != nil && errors.Is(ctx.Err(), context.Canceled)
}
