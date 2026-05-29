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

	upstream, ok := p.cfg.GetUpstream(upstreamName)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown upstream: %s", upstreamName), http.StatusBadGateway)
		return
	}
	client, err := p.clients.Client(upstream.OutboundProxy)
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
	if traceID != "" && p.traceSeq != nil {
		traceSeq = p.traceSeq.Next(traceID)
	}

	logEntry := &storage.RequestLog{
		ID:        uuid.NewString(),
		CreatedAt: startTime,
		Upstream:  upstreamName,
		Method:    r.Method,
		Path:      requestURL.Path,
		Query:     requestURL.RawQuery,
		TargetURL: upstreamURL.String(),
		Tag:       r.Header.Get("X-PrismCat-Tag"),
		TraceID:   traceID,
		TraceSeq:  traceSeq,

		RequestHeaders: p.sanitizeHeaders(r.Header, loggingCfg.SensitiveHeaders),
	}
	logMu.Lock()
	p.saveLogSnapshot(logEntry)
	p.publishInitialLive(logEntry)
	logMu.Unlock()

	// Per-request timeout: do NOT mutate a shared http.Client timeout.
	timeoutSeconds := upstream.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = config.DefaultUpstreamTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Capture request body for logging while streaming it to the upstream (no truncation of forwarding).
	reqCapture := newLimitedCapture(loggingCfg.MaxRequestBody)
	var body io.Reader
	var contentLength = r.ContentLength
	requestBodySource := r.Body

	overrideCfg := p.cfg.RequestOverridesSnapshot()
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
			p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
			p.completeLive(logEntry)
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
				p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
				p.completeLive(logEntry)
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
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), body)
	if err != nil {
		logMu.Lock()
		logEntry.Error = fmt.Sprintf("create upstream request: %v", err)
		p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
		p.completeLive(logEntry)
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
		if raw, err := json.Marshal(headerChanges); err == nil {
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

	resp, err := client.Do(upstreamReq)
	if err != nil {
		logMu.Lock()
		logEntry.Error = fmt.Sprintf("upstream request failed: %v", err)
		p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil)
		p.completeLive(logEntry)
		logMu.Unlock()
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	logMu.Lock()
	logEntry.StatusCode = resp.StatusCode
	logEntry.ResponseHeaders = p.headerToMap(resp.Header)
	logEntry.Streaming = isStreaming(resp.Header)
	p.publishHeaders(logEntry)
	logMu.Unlock()

	// Forward response headers and status code.
	p.copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	// Forward response body while capturing a bounded preview for logging.
	respCapture := newLimitedCapture(loggingCfg.MaxResponseBody)
	copied, copyErr := copyWithOptionalFlush(
		w,
		resp.Body,
		respCapture,
		logEntry.Streaming,
		p.makeLiveChunkPublisher(logEntry, resp.Header),
	)
	logMu.Lock()
	logEntry.ResponseBodySize = copied
	if copyErr != nil {
		// The response may already be partially written; we can only record the error.
		logEntry.Error = fmt.Sprintf("forward response failed: %v", copyErr)
	}
	p.finalizeAndSaveLog(logEntry, startTime, reqCapture, respCapture)
	p.completeLive(logEntry)
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

func isAccessControlHeader(header string) bool {
	return strings.HasPrefix(strings.ToLower(header), "access-control-")
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
			isSensitive := false
			for _, sensitive := range sensitiveHeaders {
				if strings.EqualFold(k, sensitive) {
					isSensitive = true
					break
				}
			}

			if isSensitive {
				if len(value) > 10 {
					newValues[i] = value[:5] + "***" + value[len(value)-3:]
				} else {
					newValues[i] = "***"
				}
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
