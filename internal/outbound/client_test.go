package outbound

import (
	"testing"
	"time"
)

func TestClientCacheSeparatesResponseHeaderTimeouts(t *testing.T) {
	cache := NewClientCache(10, 5)

	withoutTimeout, err := cache.ClientWithResponseHeaderTimeout("direct", 0)
	if err != nil {
		t.Fatalf("client without timeout: %v", err)
	}
	withTimeout, err := cache.ClientWithResponseHeaderTimeout("direct", time.Second)
	if err != nil {
		t.Fatalf("client with timeout: %v", err)
	}
	withTimeoutAgain, err := cache.ClientWithResponseHeaderTimeout("direct", time.Second)
	if err != nil {
		t.Fatalf("client with timeout again: %v", err)
	}

	if withoutTimeout == withTimeout {
		t.Fatal("clients with different response header timeouts were shared")
	}
	if withTimeout != withTimeoutAgain {
		t.Fatal("client with identical proxy and timeout was not cached")
	}
}
