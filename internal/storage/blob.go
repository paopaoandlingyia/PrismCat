package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrBlobNotFound    = errors.New("blob not found")
	ErrInvalidBlobRef  = errors.New("invalid blob ref")
	ErrUnsupportedAlgo = errors.New("unsupported blob hash algorithm")
)

// BlobStore stores detached request/response bodies by content address.
//
// The canonical ref format is: "sha256:<hex>".
type BlobStore interface {
	Put(ctx context.Context, data []byte) (ref string, err error)
	Get(ctx context.Context, ref string) ([]byte, error)
	Exists(ctx context.Context, ref string) (bool, error)
}

func newSHA256Ref(sum [sha256.Size]byte) string {
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parseBlobRef(ref string) (algo string, hexHash string, err error) {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "blob://") // tolerate UI-ish refs
	if ref == "" {
		return "", "", ErrInvalidBlobRef
	}

	algo = "sha256"
	hexHash = ref
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		algo = strings.ToLower(strings.TrimSpace(ref[:i]))
		hexHash = strings.TrimSpace(ref[i+1:])
	}

	if algo != "sha256" {
		return "", "", ErrUnsupportedAlgo
	}
	hexHash = strings.ToLower(hexHash)
	if len(hexHash) != sha256.Size*2 {
		return "", "", ErrInvalidBlobRef
	}
	// Validate hex.
	if _, err := hex.DecodeString(hexHash); err != nil {
		return "", "", ErrInvalidBlobRef
	}
	return algo, hexHash, nil
}
