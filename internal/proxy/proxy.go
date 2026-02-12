package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
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
	"unicode/utf8"

	"github.com/andybalholm/brotli"
	"github.com/google/uuid"

	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/storage"
)

// Proxy handles host-based upstream routing and request/response logging.
type Proxy struct {
	cfg    *config.Config
	repo   storage.Repository
	client *http.Client
}

// New creates a new proxy instance.
func New(cfg *config.Config, repo storage.Repository) *Proxy {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Proxy{
		cfg:  cfg,
		repo: repo,
		client: &http.Client{
			// Do not follow redirects automatically.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: transport,
		},
	}
}

// ServeHTTP proxies the request to the configured upstream and logs the traffic.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	serverCfg := p.cfg.ServerSnapshot()
	loggingCfg := p.cfg.LoggingSnapshot()

	// Extract upstream name from host (e.g. openai.localhost -> openai).
	subdomain := config.ExtractSubdomain(r.Host, serverCfg.ProxyDomains)
	if subdomain == "" {
		http.Error(w, "invalid host: missing subdomain", http.StatusBadRequest)
		return
	}

	upstream, ok := p.cfg.GetUpstream(subdomain)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown upstream: %s", subdomain), http.StatusBadGateway)
		return
	}

	targetURL, err := url.Parse(upstream.Target)
	if err != nil {
		http.Error(w, "invalid upstream config", http.StatusInternalServerError)
		return
	}

	upstreamURL := buildUpstreamURL(targetURL, r.URL)

	// Initial log entry (best-effort). This allows the UI to show in-flight requests.
	logEntry := &storage.RequestLog{
		ID:        uuid.NewString(),
		CreatedAt: startTime,
		Upstream:  subdomain,
		Method:    r.Method,
		Path:      r.URL.Path,
		Query:     r.URL.RawQuery,
		TargetURL: upstreamURL.String(),

		RequestHeaders: p.sanitizeHeaders(r.Header, loggingCfg.SensitiveHeaders),
	}
	p.saveLogSnapshot(logEntry)

	// Per-request timeout: do NOT mutate a shared http.Client timeout.
	timeoutSeconds := upstream.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Capture request body for logging while streaming it to the upstream (no truncation of forwarding).
	reqCapture := newLimitedCapture(loggingCfg.MaxRequestBody)
	var body io.Reader
	if r.Body != nil && r.Body != http.NoBody {
		tee := io.TeeReader(r.Body, reqCapture)
		body = &teeReadCloser{r: tee, c: r.Body}
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), body)
	if err != nil {
		logEntry.Error = fmt.Sprintf("create upstream request: %v", err)
		p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil, loggingCfg)
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	p.copyHeaders(upstreamReq.Header, r.Header)
	// Host is special: set the field (Header["Host"] is ignored by net/http client).
	upstreamReq.Host = targetURL.Host
	// Preserve original length semantics if present.
	upstreamReq.ContentLength = r.ContentLength

	resp, err := p.client.Do(upstreamReq)
	if err != nil {
		logEntry.Error = fmt.Sprintf("upstream request failed: %v", err)
		p.finalizeAndSaveLog(logEntry, startTime, reqCapture, nil, loggingCfg)
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	logEntry.StatusCode = resp.StatusCode
	logEntry.ResponseHeaders = p.headerToMap(resp.Header)
	logEntry.Streaming = isStreaming(resp.Header)

	// Forward response headers and status code.
	p.copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	// Forward response body while capturing a bounded preview for logging.
	respCapture := newLimitedCapture(loggingCfg.MaxResponseBody)
	copied, copyErr := copyWithOptionalFlush(w, resp.Body, respCapture, logEntry.Streaming)
	logEntry.ResponseBodySize = copied
	if copyErr != nil {
		// The response may already be partially written; we can only record the error.
		logEntry.Error = fmt.Sprintf("forward response failed: %v", copyErr)
	}

	p.finalizeAndSaveLog(logEntry, startTime, reqCapture, respCapture, loggingCfg)
}

func (p *Proxy) finalizeAndSaveLog(log *storage.RequestLog, startTime time.Time, reqCap, respCap *limitedCapture, loggingCfg config.LoggingConfig) {
	if reqCap != nil {
		log.RequestBodySize = reqCap.Total()
		contentType := firstHeaderValue(log.RequestHeaders, "Content-Type")
		contentEncoding := firstHeaderValue(log.RequestHeaders, "Content-Encoding")
		body, truncated := bodyForLog(contentType, contentEncoding, reqCap.Bytes(), loggingCfg.MaxRequestBody)
		log.RequestBody = body
		log.Truncated = log.Truncated || truncated
	}
	if respCap != nil {
		contentType := firstHeaderValue(log.ResponseHeaders, "Content-Type")
		contentEncoding := firstHeaderValue(log.ResponseHeaders, "Content-Encoding")
		body, truncated := bodyForLog(contentType, contentEncoding, respCap.Bytes(), loggingCfg.MaxResponseBody)
		log.ResponseBody = body
		log.Truncated = log.Truncated || truncated
	}

	log.Truncated = log.Truncated ||
		(reqCap != nil && reqCap.Truncated()) ||
		(respCap != nil && respCap.Truncated())
	log.Latency = time.Since(startTime).Milliseconds()

	p.saveLogSnapshot(log)
}

func firstHeaderValue(headers map[string]string, key string) string {
	if headers == nil {
		return ""
	}
	if v, ok := headers[key]; ok {
		return v
	}
	// Best-effort: tolerate different casing.
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func (p *Proxy) saveLogSnapshot(entry *storage.RequestLog) {
	if err := p.repo.SaveLog(entry); err != nil {
		// Best-effort: avoid crashing the request path.
		log.Printf("save log failed/dropped: %v", err)
	}
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

// sanitizeHeaders masks configured sensitive headers.
func (p *Proxy) sanitizeHeaders(headers http.Header, sensitiveHeaders []string) map[string]string {
	result := make(map[string]string)
	for k, vv := range headers {
		if len(vv) == 0 {
			continue
		}

		value := vv[0]
		for _, sensitive := range sensitiveHeaders {
			if strings.EqualFold(k, sensitive) {
				if len(value) > 10 {
					value = value[:5] + "***" + value[len(value)-3:]
				} else {
					value = "***"
				}
				break
			}
		}
		result[k] = value
	}
	return result
}

func (p *Proxy) headerToMap(headers http.Header) map[string]string {
	result := make(map[string]string)
	for k, vv := range headers {
		if len(vv) > 0 {
			result[k] = vv[0]
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

func (c *limitedCapture) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) == 0 {
		return nil
	}
	out := make([]byte, len(c.buf))
	copy(out, c.buf)
	return out
}

func (c *limitedCapture) Total() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func (c *limitedCapture) Truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.truncated
}

func copyWithOptionalFlush(dst http.ResponseWriter, src io.Reader, capture io.Writer, flush bool) (int64, error) {
	var w io.Writer = dst
	if capture != nil {
		w = io.MultiWriter(dst, capture)
	}

	buf := make([]byte, 32*1024)
	if !flush {
		return io.CopyBuffer(w, src, buf)
	}

	flusher, ok := dst.(http.Flusher)
	if !ok {
		return io.CopyBuffer(w, src, buf)
	}

	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			wn, werr := w.Write(buf[:n])
			total += int64(wn)
			if werr != nil {
				return total, werr
			}
			flusher.Flush()
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

// bodyForLog converts captured bytes to a UI-friendly string.
// For compressed payloads, it attempts decompression first.
// For non-textual payloads, it returns a short placeholder to avoid blowing up the UI.
func readAllLimited(r io.Reader, max int64) ([]byte, bool, error) {
	if max <= 0 {
		// No budget: return empty.
		return nil, false, nil
	}
	// Read up to max+1 so we can detect truncation.
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) <= max {
		return data, false, nil
	}
	return data[:max], true, nil
}

func bodyForLog(contentType, contentEncoding string, b []byte, maxOutputBytes int64) (string, bool) {
	if len(b) == 0 {
		return "", false
	}

	data := b
	decompressed := false
	truncated := false

	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "gzip":
		if r, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			if d, t, err := readAllLimited(r, maxOutputBytes); err == nil {
				data = d
				decompressed = true
				truncated = truncated || t
			}
			r.Close()
		}
	case "deflate":
		r := flate.NewReader(bytes.NewReader(b))
		if d, t, err := readAllLimited(r, maxOutputBytes); err == nil {
			data = d
			decompressed = true
			truncated = truncated || t
		}
		r.Close()
	case "br":
		r := brotli.NewReader(bytes.NewReader(b))
		if d, t, err := readAllLimited(r, maxOutputBytes); err == nil {
			data = d
			decompressed = true
			truncated = truncated || t
		}
	}

	if isProbablyText(contentType) && utf8.Valid(data) {
		return string(data), truncated
	}
	if utf8.Valid(data) {
		return string(data), truncated
	}

	if decompressed {
		if truncated {
			return fmt.Sprintf("[binary content omitted; %d bytes after decompression (truncated)]", len(data)), true
		}
		return fmt.Sprintf("[binary content omitted; %d bytes after decompression]", len(data)), false
	}
	return fmt.Sprintf("[binary content omitted; %d bytes captured]", len(b)), false
}

func isProbablyText(contentType string) bool {
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
		mediaType == "application/x-www-form-urlencoded" {
		return true
	}
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	return false
}
