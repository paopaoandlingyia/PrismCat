package storage

import (
	"context"
	"log"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
)

// PrepareLogForPersistence converts raw captured bodies into their persisted
// display form before the async worker writes them to storage.
func PrepareLogForPersistence(logEntry *RequestLog, cfg *config.Config, blobs ...BlobStore) {
	if logEntry == nil || cfg == nil {
		return
	}

	loggingCfg := cfg.LoggingSnapshot()
	blobStore := firstBlobStore(blobs)

	var requestFormattedTruncated bool
	if len(logEntry.RequestBodyRaw) > 0 {
		formatted := formatCapturedBody(
			logEntry.RequestBodyRaw,
			firstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			firstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBody = formatted.Text
		requestFormattedTruncated = formatted.Truncated
		if formatted.Binary && logEntry.RequestBodyRef == "" {
			logEntry.RequestBodyRef = putBodyBlob(blobStore, "request", logEntry.RequestBodyRaw)
		}
	}
	if len(logEntry.RequestBodyOriginalRaw) > 0 {
		formatted := formatCapturedBody(
			logEntry.RequestBodyOriginalRaw,
			firstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			firstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBodyOriginal = formatted.Text
		requestFormattedTruncated = requestFormattedTruncated || formatted.Truncated
	}
	if len(logEntry.RequestBodyFinalRaw) > 0 {
		formatted := formatCapturedBody(
			logEntry.RequestBodyFinalRaw,
			firstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			firstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBodyFinal = formatted.Text
		requestFormattedTruncated = requestFormattedTruncated || formatted.Truncated
	}

	var responseFormattedTruncated bool
	if len(logEntry.ResponseBodyRaw) > 0 {
		formatted := formatCapturedBody(
			logEntry.ResponseBodyRaw,
			firstHeaderValue(logEntry.ResponseHeaders, "Content-Type"),
			firstHeaderValue(logEntry.ResponseHeaders, "Content-Encoding"),
			loggingCfg.MaxResponseBody,
			!loggingCfg.StoreBase64,
		)
		logEntry.ResponseBody = formatted.Text
		responseFormattedTruncated = formatted.Truncated
		if formatted.Binary && logEntry.ResponseBodyRef == "" {
			logEntry.ResponseBodyRef = putBodyBlob(blobStore, "response", logEntry.ResponseBodyRaw)
		}
	}

	logEntry.Truncated = logEntry.Truncated ||
		logEntry.RequestBodyCaptureTruncated ||
		logEntry.ResponseBodyCaptureTruncated ||
		requestFormattedTruncated ||
		responseFormattedTruncated

	logEntry.RequestBodyRaw = nil
	logEntry.RequestBodyOriginalRaw = nil
	logEntry.RequestBodyFinalRaw = nil
	logEntry.ResponseBodyRaw = nil
	logEntry.RequestBodyCaptureTruncated = false
	logEntry.ResponseBodyCaptureTruncated = false
}

func formatCapturedBody(body []byte, contentType string, contentEncoding string, maxOutputBytes int64, trimLargeBase64 bool) httpbody.FormatResult {
	if len(body) == 0 {
		return httpbody.FormatResult{}
	}

	return httpbody.FormatForDisplay(contentType, contentEncoding, body, httpbody.FormatOptions{
		MaxOutputBytes:  maxOutputBytes,
		TrimLargeBase64: trimLargeBase64,
	})
}

func firstBlobStore(blobs []BlobStore) BlobStore {
	if len(blobs) == 0 {
		return nil
	}
	return blobs[0]
}

func putBodyBlob(blobs BlobStore, kind string, body []byte) string {
	if blobs == nil || len(body) == 0 {
		return ""
	}
	ref, err := blobs.Put(context.Background(), body)
	if err != nil {
		log.Printf("blob put (%s binary) failed: %v", kind, err)
		return ""
	}
	return ref
}

func firstHeaderValue(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	if vv, ok := headers[key]; ok && len(vv) > 0 {
		return vv[0]
	}
	for k, vv := range headers {
		if strings.EqualFold(k, key) && len(vv) > 0 {
			return vv[0]
		}
	}
	return ""
}
