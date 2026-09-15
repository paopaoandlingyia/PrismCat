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
	"strconv"
	"strings"
	"time"
)

const (
	maxSSERequestDetectionBytes = 1 << 20
	sseHeartbeatWriteTimeout    = 10 * time.Second
)

var sseHeartbeatPayload = []byte(": ping\n\n")

type earlySSEKeepaliveResult struct {
	committed bool
	err       error
}

type earlySSEKeepalive struct {
	stop   context.CancelFunc
	done   <-chan struct{}
	result *earlySSEKeepaliveResult
}

func startEarlySSEKeepalive(
	requestContext context.Context,
	w http.ResponseWriter,
	interval time.Duration,
	cancelUpstream context.CancelFunc,
) *earlySSEKeepalive {
	ctx, stop := context.WithCancel(requestContext)
	done := make(chan struct{})
	result := &earlySSEKeepaliveResult{}

	go func() {
		defer close(done)
		timer := time.NewTimer(interval)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if !result.committed {
					prepareEarlySSEHeaders(w.Header())
					w.WriteHeader(http.StatusOK)
					result.committed = true
				}
				if err := writeSSEHeartbeat(ctx, w); err != nil {
					if ctx.Err() != nil {
						return
					}
					result.err = err
					cancelUpstream()
					return
				}
				timer.Reset(interval)
			}
		}
	}()

	return &earlySSEKeepalive{stop: stop, done: done, result: result}
}

func (k *earlySSEKeepalive) stopAndWait() earlySSEKeepaliveResult {
	if k == nil {
		return earlySSEKeepaliveResult{}
	}
	k.stop()
	<-k.done
	return *k.result
}

func prepareEarlySSEHeaders(header http.Header) {
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	header.Del("Content-Length")
}

func writeSSEHeartbeat(ctx context.Context, w http.ResponseWriter) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	controller := http.NewResponseController(w)
	deadlineSet := false
	writeDeadline := time.Now().Add(sseHeartbeatWriteTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(writeDeadline) {
		writeDeadline = contextDeadline
	}
	if err := controller.SetWriteDeadline(writeDeadline); err == nil {
		deadlineSet = true
	} else if !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("set SSE heartbeat write deadline: %w", err)
	}
	if deadlineSet {
		defer func() {
			_ = controller.SetWriteDeadline(time.Time{})
		}()
	}
	if _, err := w.Write(sseHeartbeatPayload); err != nil {
		return fmt.Errorf("write SSE heartbeat: %w", err)
	}
	if err := controller.Flush(); err != nil {
		return fmt.Errorf("flush SSE heartbeat: %w", err)
	}
	return nil
}

func isSSEContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}

func canInjectSSEHeartbeat(resp *http.Response) bool {
	if resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	if !isSSEContentType(resp.Header.Get("Content-Type")) {
		return false
	}
	encoding := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	return encoding == "" || strings.EqualFold(encoding, "identity")
}

// detectSSERequest recognizes common LLM streaming requests without changing
// the forwarded body. A bounded prefix is consumed and then prepended again.
// If a large JSON body does not expose its top-level stream flag inside the
// prefix, full keepalive deliberately stays off and the caller logs the reason.
func detectSSERequest(r *http.Request, body io.ReadCloser) (bool, io.ReadCloser, string) {
	if acceptsEventStream(r.Header.Get("Accept")) || strings.EqualFold(r.URL.Query().Get("alt"), "sse") {
		return true, body, ""
	}
	if body == nil || body == http.NoBody || !isJSONContentType(r.Header.Get("Content-Type")) {
		return false, body, ""
	}
	encoding := strings.TrimSpace(r.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return false, body, "request body is encoded"
	}

	prefix, err := io.ReadAll(io.LimitReader(body, maxSSERequestDetectionBytes+1))
	restored := &teeReadCloser{r: io.MultiReader(bytes.NewReader(prefix), body), c: body}
	if err != nil {
		return false, restored, fmt.Sprintf("read request prefix: %v", err)
	}
	enabled, decided, parseErr := topLevelStreamFlag(prefix)
	if parseErr == nil && decided {
		return enabled, restored, ""
	}
	if len(prefix) > maxSSERequestDetectionBytes {
		return false, restored, fmt.Sprintf("top-level stream flag not found within %d bytes", maxSSERequestDetectionBytes)
	}
	if parseErr != nil {
		return false, restored, fmt.Sprintf("inspect request JSON: %v", parseErr)
	}
	return false, restored, ""
}

func acceptsEventStream(accept string) bool {
	for _, value := range strings.Split(accept, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err != nil || !strings.EqualFold(mediaType, "text/event-stream") {
			continue
		}
		quality := 1.0
		if rawQuality, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(strings.TrimSpace(rawQuality), 64)
			if err != nil {
				continue
			}
		}
		if quality > 0 && quality <= 1 {
			return true
		}
	}
	return false
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func topLevelStreamFlag(data []byte) (enabled bool, decided bool, err error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return false, false, err
	}
	if delim, ok := first.(json.Delim); !ok || delim != '{' {
		return false, true, nil
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, false, err
		}
		name, ok := key.(string)
		if !ok {
			return false, false, fmt.Errorf("top-level object key is not a string")
		}
		if name == "stream" {
			var value bool
			if err := decoder.Decode(&value); err != nil {
				return false, false, fmt.Errorf("stream flag is not a boolean: %w", err)
			}
			return value, true, nil
		}
		if err := skipJSONValue(decoder); err != nil {
			return false, false, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return false, false, err
	}
	return false, true, nil
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		if nested, ok := token.(json.Delim); ok {
			switch nested {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

type sseReadResult struct {
	data []byte
	err  error
}

type responseBodyStageTimeoutReader struct {
	body             io.ReadCloser
	firstByteTimeout time.Duration
	idleTimeout      time.Duration
	active           bool
}

func (r *responseBodyStageTimeoutReader) Read(buf []byte) (int, error) {
	timeout := r.idleTimeout
	timeoutErr := error(&responseBodyIdleTimeoutError{timeout: timeout})
	if !r.active {
		timeout = r.firstByteTimeout
		timeoutErr = &responseBodyFirstByteTimeoutError{timeout: timeout}
	}
	if timeout <= 0 {
		n, err := r.body.Read(buf)
		if n > 0 {
			r.active = true
		}
		return n, err
	}
	n, err := readResponseBodyChunkWithTimeout(r.body, buf, timeout, timeoutErr)
	if n > 0 {
		r.active = true
	}
	return n, err
}

func copyWithSSEKeepalive(
	ctx context.Context,
	dst http.ResponseWriter,
	src io.Reader,
	srcCloser io.Closer,
	capture io.Writer,
	onChunk func([]byte),
	interval time.Duration,
) (int64, error) {
	readCtx, cancelRead := context.WithCancel(ctx)
	results := make(chan sseReadResult)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			buf := make([]byte, copyBufferSize)
			n, err := src.Read(buf)
			if n > 0 {
				select {
				case results <- sseReadResult{data: buf[:n]}:
				case <-readCtx.Done():
					return
				}
			}
			if err != nil {
				select {
				case results <- sseReadResult{err: err}:
				case <-readCtx.Done():
				}
				return
			}
		}
	}()
	defer func() {
		cancelRead()
		if srcCloser != nil {
			_ = srcCloser.Close()
		}
		<-readerDone
	}()

	timer := time.NewTimer(interval)
	defer timer.Stop()
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)
	}

	var total int64
	atEventBoundary := true
	var sseTail []byte
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-timer.C:
			if atEventBoundary {
				if err := writeSSEHeartbeat(ctx, dst); err != nil {
					if ctx.Err() != nil {
						return total, ctx.Err()
					}
					return total, err
				}
			}
			timer.Reset(interval)
		case result := <-results:
			if len(result.data) > 0 {
				writers := []io.Writer{dst}
				if capture != nil {
					writers = append(writers, capture)
				}
				written, err := io.MultiWriter(writers...).Write(result.data)
				total += int64(written)
				if onChunk != nil && written > 0 {
					onChunk(result.data[:written])
				}
				if err != nil {
					return total, err
				}
				atEventBoundary, sseTail = updateSSEEventBoundary(sseTail, result.data[:written])
				if err := http.NewResponseController(dst).Flush(); err != nil {
					return total, fmt.Errorf("flush SSE response: %w", err)
				}
				resetTimer()
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return total, nil
				}
				return total, result.err
			}
		}
	}
}

func updateSSEEventBoundary(tail, chunk []byte) (bool, []byte) {
	combined := make([]byte, 0, len(tail)+len(chunk))
	combined = append(combined, tail...)
	combined = append(combined, chunk...)
	boundary := bytes.HasSuffix(combined, []byte("\n\n")) ||
		bytes.HasSuffix(combined, []byte("\r\n\r\n")) ||
		bytes.HasSuffix(combined, []byte("\r\r"))
	if len(combined) > 3 {
		combined = combined[len(combined)-3:]
	}
	return boundary, combined
}

func logSSEDetectionFallback(upstream, path, reason string) {
	if reason == "" {
		return
	}
	log.Printf("SSE full keepalive not started for upstream %q path %q: %s", upstream, path, reason)
}
