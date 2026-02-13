package storage

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/prismcat/prismcat/internal/config"
)

// DetachingRepository detaches large bodies into a BlobStore before persisting logs.
// It is best-effort: on blob failures it falls back to storing inline bodies.
//
// IMPORTANT: Wrap the *inner* repository (e.g. SQLiteRepository) and then wrap with
// AsyncRepository, so the detaching work happens off the proxy hot path.
type DetachingRepository struct {
	inner Repository
	blobs BlobStore
	cfg   *config.Config
}

func NewDetachingRepository(inner Repository, blobs BlobStore, cfg *config.Config) *DetachingRepository {
	return &DetachingRepository{
		inner: inner,
		blobs: blobs,
		cfg:   cfg,
	}
}

func (r *DetachingRepository) SaveLog(logEntry *RequestLog) error {
	if r.blobs == nil || r.cfg == nil {
		return r.inner.SaveLog(logEntry)
	}
	if logEntry == nil {
		return r.inner.SaveLog(logEntry)
	}

	logging := r.cfg.LoggingSnapshot()
	detachOver := logging.DetachBodyOverBytes
	if detachOver <= 0 {
		return r.inner.SaveLog(logEntry)
	}
	previewBytes := logging.BodyPreviewBytes

	ctx := context.Background()

	if logEntry.RequestBodyRef == "" && int64(len(logEntry.RequestBody)) > detachOver {
		ref, err := r.blobs.Put(ctx, stringBytes(logEntry.RequestBody))
		if err != nil {
			log.Printf("blob put (request) failed: %v", err)
		} else {
			log.Printf("Detached request body: %d bytes -> %s", len(logEntry.RequestBody), ref)
			logEntry.RequestBodyRef = ref
			logEntry.RequestBody = truncateUTF8(logEntry.RequestBody, previewBytes)
		}
	}

	if logEntry.ResponseBodyRef == "" && int64(len(logEntry.ResponseBody)) > detachOver {
		ref, err := r.blobs.Put(ctx, stringBytes(logEntry.ResponseBody))
		if err != nil {
			log.Printf("blob put (response) failed: %v", err)
		} else {
			log.Printf("Detached response body: %d bytes -> %s", len(logEntry.ResponseBody), ref)
			logEntry.ResponseBodyRef = ref
			logEntry.ResponseBody = truncateUTF8(logEntry.ResponseBody, previewBytes)
		}
	}

	return r.inner.SaveLog(logEntry)
}

func truncateUTF8(s string, maxBytes int64) string {
	if maxBytes <= 0 {
		return ""
	}
	if int64(len(s)) <= maxBytes {
		return s
	}

	// Avoid allocating a full []byte copy for large bodies. We only need to find a
	// safe UTF-8 boundary <= maxBytes.
	cut := int(maxBytes)
	if cut > len(s) {
		cut = len(s)
	}
	// If we cut in the middle of a multi-byte rune, step back to the rune start.
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	// Best-effort validation (the input is expected to be valid UTF-8 already).
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
	}
	// Copy the preview so we don't retain the full original body in memory.
	return strings.Clone(s[:cut])
}

func stringBytes(s string) []byte {
	if s == "" {
		return nil
	}
	// Read-only view over the string data to avoid an extra allocation for large bodies.
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func (r *DetachingRepository) GetLog(id string) (*RequestLog, error) {
	return r.inner.GetLog(id)
}

func (r *DetachingRepository) ListLogs(filter LogFilter) ([]*RequestLog, int64, error) {
	return r.inner.ListLogs(filter)
}

func (r *DetachingRepository) DeleteLogsBefore(beforeTime time.Time) (int64, error) {
	return r.inner.DeleteLogsBefore(beforeTime)
}

func (r *DetachingRepository) GetStats(since *time.Time) (*LogStats, error) {
	return r.inner.GetStats(since)
}

func (r *DetachingRepository) Close() error {
	return r.inner.Close()
}
