package storage

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/usage"
)

// PrepareLogForPersistence extracts usage while the response is still in
// memory, then externalizes every non-empty wire body. SQLite only receives
// body metadata through RequestLog.Bodies.
func PrepareLogForPersistence(logEntry *RequestLog, cfg *config.Config, blobs ...BlobStore) {
	if logEntry == nil || cfg == nil {
		return
	}

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

	blobStore := firstBlobStore(blobs)
	logEntry.Bodies = nil
	var storageErrors []string
	requestType := FirstHeaderValue(logEntry.RequestHeaders, "Content-Type")
	requestEncoding := FirstHeaderValue(logEntry.RequestHeaders, "Content-Encoding")
	responseType := FirstHeaderValue(logEntry.ResponseHeaders, "Content-Type")
	responseEncoding := FirstHeaderValue(logEntry.ResponseHeaders, "Content-Encoding")
	loggingCfg := cfg.LoggingSnapshot()

	formatTransient := func(raw []byte, contentType, contentEncoding string, max int64) string {
		if len(raw) == 0 {
			return ""
		}
		// Deprecated tests/embedders may still set BodyPreviewBytes in memory.
		// It never reaches YAML and SQLite never persists this formatted text.
		if loggingCfg.BodyPreviewBytes > 0 && loggingCfg.BodyPreviewBytes < max {
			return httpbody.FormatPreviewForDisplay(contentType, contentEncoding, raw, httpbody.FormatOptions{
				MaxOutputBytes:  loggingCfg.BodyPreviewBytes,
				TrimLargeBase64: !loggingCfg.StoreBase64,
			}).Text
		}
		return httpbody.FormatForDisplay(contentType, contentEncoding, raw, httpbody.FormatOptions{
			MaxOutputBytes:  max,
			TrimLargeBase64: !loggingCfg.StoreBase64,
		}).Text
	}
	logEntry.RequestBody = formatTransient(logEntry.RequestBodyRaw, requestType, requestEncoding, loggingCfg.MaxRequestBody)
	logEntry.RequestBodyOriginal = formatTransient(logEntry.RequestBodyOriginalRaw, requestType, requestEncoding, loggingCfg.MaxRequestBody)
	logEntry.RequestBodyFinal = formatTransient(logEntry.RequestBodyFinalRaw, requestType, requestEncoding, loggingCfg.MaxRequestBody)
	logEntry.ResponseBody = formatTransient(logEntry.ResponseBodyRaw, responseType, responseEncoding, loggingCfg.MaxResponseBody)

	add := func(part string, raw []byte, total int64, truncated bool, contentType, contentEncoding string) {
		if len(raw) == 0 {
			return
		}
		if total <= 0 {
			total = int64(len(raw))
		}
		body := LogBody{
			LogID:           logEntry.ID,
			Part:            part,
			CapturedBytes:   int64(len(raw)),
			TotalBytes:      total,
			Truncated:       truncated || total > int64(len(raw)),
			ContentType:     contentType,
			ContentEncoding: contentEncoding,
			Representation:  "wire",
		}
		if blobStore == nil {
			storageErrors = append(storageErrors, part+": blob store is unavailable")
			body.Truncated = true
		} else {
			ref, err := blobStore.Put(context.Background(), raw)
			if err != nil {
				storageErrors = append(storageErrors, fmt.Sprintf("%s: %v", part, err))
				body.Truncated = true
				log.Printf("blob put (%s) failed: %v", part, err)
			} else {
				body.BlobRef = ref
				body.Recoverable = !body.Truncated
			}
		}
		logEntry.Bodies = append(logEntry.Bodies, body)
	}

	add(BodyPartRequest, logEntry.RequestBodyRaw, logEntry.RequestBodySize,
		logEntry.RequestBodyCaptureTruncated, requestType, requestEncoding)
	add(BodyPartRequestOriginal, logEntry.RequestBodyOriginalRaw, int64(len(logEntry.RequestBodyOriginalRaw)),
		false, requestType, requestEncoding)
	add(BodyPartRequestFinal, logEntry.RequestBodyFinalRaw, int64(len(logEntry.RequestBodyFinalRaw)),
		false, requestType, requestEncoding)
	add(BodyPartResponse, logEntry.ResponseBodyRaw, logEntry.ResponseBodySize,
		logEntry.ResponseBodyCaptureTruncated, responseType, responseEncoding)

	logEntry.BodyStorageError = strings.Join(storageErrors, "; ")
	logEntry.Truncated = logEntry.Truncated || logEntry.RequestBodyCaptureTruncated ||
		logEntry.ResponseBodyCaptureTruncated || len(storageErrors) > 0

	// SQLite ignores formatted body fields and persists only Bodies metadata.
	populateLegacyBodyRefs(logEntry)
	logEntry.RequestBodyRaw = nil
	logEntry.RequestBodyOriginalRaw = nil
	logEntry.RequestBodyFinalRaw = nil
	logEntry.ResponseBodyRaw = nil
	logEntry.RequestBodyCaptureTruncated = false
	logEntry.ResponseBodyCaptureTruncated = false
}

func firstBlobStore(blobs []BlobStore) BlobStore {
	if len(blobs) == 0 {
		return nil
	}
	return blobs[0]
}
