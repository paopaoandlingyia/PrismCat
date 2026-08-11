package storage

import (
	"context"
	"log"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/usage"
)

// PrepareLogForPersistence converts raw captured bodies into bounded previews
// and stores recoverable raw bodies in the blob store before persistence.
func PrepareLogForPersistence(logEntry *RequestLog, cfg *config.Config, blobs ...BlobStore) {
	if logEntry == nil || cfg == nil {
		return
	}

	loggingCfg := cfg.LoggingSnapshot()
	blobStore := firstBlobStore(blobs)

	if len(logEntry.ResponseBodyRaw) > 0 {
		usageResult := usage.Extract(
			cfg.UsageExtractionSnapshotForTarget(logEntry.Upstream, logEntry.UpstreamTarget),
			logEntry.Upstream,
			FirstHeaderValue(logEntry.ResponseHeaders, "Content-Type"),
			FirstHeaderValue(logEntry.ResponseHeaders, "Content-Encoding"),
			logEntry.ResponseBodyRaw,
		)
		if usageResult.Source != "" {
			logEntry.UsageInputTokens = usageResult.InputTokens
			logEntry.UsageOutputTokens = usageResult.OutputTokens
			logEntry.UsageTotalTokens = usageResult.TotalTokens
			logEntry.UsageRaw = usageResult.Raw
			logEntry.UsageSource = usageResult.Source
		}
	}

	var requestUnrecoverablyTruncated bool
	if len(logEntry.RequestBodyRaw) > 0 {
		formatted := formatCapturedBodyForPersistence(
			logEntry.RequestBodyRaw,
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			loggingCfg.BodyPreviewBytes,
			loggingCfg.DetachBodyOverBytes,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBody = formatted.Text
		requestUnrecoverablyTruncated = formatted.Truncated
		if shouldStoreRawBody(logEntry.RequestBodyRaw, formatted.Truncated, formatted.Binary, logEntry.RequestBodyRef, loggingCfg.DetachBodyOverBytes) {
			logEntry.RequestBodyRef = putBodyBlob(blobStore, "request", logEntry.RequestBodyRaw)
		}
		if logEntry.RequestBodyRef != "" {
			requestUnrecoverablyTruncated = false
		}
	}
	if len(logEntry.RequestBodyOriginalRaw) > 0 {
		formatted := formatCapturedBodyForPersistence(
			logEntry.RequestBodyOriginalRaw,
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			loggingCfg.BodyPreviewBytes,
			loggingCfg.DetachBodyOverBytes,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBodyOriginal = formatted.Text
		requestUnrecoverablyTruncated = requestUnrecoverablyTruncated || formatted.Truncated
	}
	if len(logEntry.RequestBodyFinalRaw) > 0 {
		formatted := formatCapturedBodyForPersistence(
			logEntry.RequestBodyFinalRaw,
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Type"),
			FirstHeaderValue(logEntry.RequestHeaders, "Content-Encoding"),
			loggingCfg.MaxRequestBody,
			loggingCfg.BodyPreviewBytes,
			loggingCfg.DetachBodyOverBytes,
			!loggingCfg.StoreBase64,
		)
		logEntry.RequestBodyFinal = formatted.Text
		if formatted.Truncated && logEntry.RequestBodyRef == "" {
			requestUnrecoverablyTruncated = true
		}
	}

	var responseUnrecoverablyTruncated bool
	if len(logEntry.ResponseBodyRaw) > 0 {
		formatted := formatCapturedBodyForPersistence(
			logEntry.ResponseBodyRaw,
			FirstHeaderValue(logEntry.ResponseHeaders, "Content-Type"),
			FirstHeaderValue(logEntry.ResponseHeaders, "Content-Encoding"),
			loggingCfg.MaxResponseBody,
			loggingCfg.BodyPreviewBytes,
			loggingCfg.DetachBodyOverBytes,
			!loggingCfg.StoreBase64,
		)
		logEntry.ResponseBody = formatted.Text
		responseUnrecoverablyTruncated = formatted.Truncated
		if shouldStoreRawBody(logEntry.ResponseBodyRaw, formatted.Truncated, formatted.Binary, logEntry.ResponseBodyRef, loggingCfg.DetachBodyOverBytes) {
			logEntry.ResponseBodyRef = putBodyBlob(blobStore, "response", logEntry.ResponseBodyRaw)
		}
		if logEntry.ResponseBodyRef != "" {
			responseUnrecoverablyTruncated = false
		}
	}

	logEntry.Truncated = logEntry.Truncated ||
		logEntry.RequestBodyCaptureTruncated ||
		logEntry.ResponseBodyCaptureTruncated ||
		requestUnrecoverablyTruncated ||
		responseUnrecoverablyTruncated

	logEntry.RequestBodyRaw = nil
	logEntry.RequestBodyOriginalRaw = nil
	logEntry.RequestBodyFinalRaw = nil
	logEntry.ResponseBodyRaw = nil
	logEntry.RequestBodyCaptureTruncated = false
	logEntry.ResponseBodyCaptureTruncated = false
}

func formatCapturedBodyForPersistence(body []byte, contentType string, contentEncoding string, maxOutputBytes int64, previewBytes int64, detachOverBytes int64, trimLargeBase64 bool) httpbody.FormatResult {
	if len(body) == 0 {
		return httpbody.FormatResult{}
	}

	if detachOverBytes > 0 {
		return httpbody.FormatPreviewForDisplay(contentType, contentEncoding, body, httpbody.FormatOptions{
			MaxOutputBytes:  previewBytes,
			TrimLargeBase64: trimLargeBase64,
		})
	}

	return httpbody.FormatForDisplay(contentType, contentEncoding, body, httpbody.FormatOptions{
		MaxOutputBytes:  maxOutputBytes,
		TrimLargeBase64: trimLargeBase64,
	})
}

func shouldStoreRawBody(body []byte, previewTruncated bool, binary bool, existingRef string, detachOverBytes int64) bool {
	if len(body) == 0 || existingRef != "" {
		return false
	}
	if binary {
		return true
	}
	if detachOverBytes <= 0 {
		return false
	}
	return previewTruncated || int64(len(body)) > detachOverBytes
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
