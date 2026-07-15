package outbound

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

type ClientCache struct {
	maxIdleConns        int
	maxIdleConnsPerHost int

	mu      sync.Mutex
	clients map[string]*http.Client
}

func NewClientCache(maxIdleConns, maxIdleConnsPerHost int) *ClientCache {
	return &ClientCache{
		maxIdleConns:        maxIdleConns,
		maxIdleConnsPerHost: maxIdleConnsPerHost,
		clients:             make(map[string]*http.Client),
	}
}

func (c *ClientCache) Client(outboundProxy string) (*http.Client, error) {
	return c.ClientWithResponseHeaderTimeout(outboundProxy, 0)
}

func (c *ClientCache) ClientWithResponseHeaderTimeout(outboundProxy string, timeout time.Duration) (*http.Client, error) {
	normalizedProxy, err := config.NormalizeOutboundProxy(outboundProxy)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := clientCacheKey(normalizedProxy, timeout)
	if client, ok := c.clients[key]; ok {
		return client, nil
	}

	client, err := c.newClient(normalizedProxy, timeout)
	if err != nil {
		return nil, err
	}
	c.clients[key] = client
	return client, nil
}

func clientCacheKey(outboundProxy string, timeout time.Duration) string {
	return outboundProxy + "\x00" + timeout.String()
}

func (c *ClientCache) newClient(outboundProxy string, responseHeaderTimeout time.Duration) (*http.Client, error) {
	proxyFunc := http.ProxyFromEnvironment
	switch outboundProxy {
	case "direct":
		proxyFunc = nil
	case "env":
	default:
		proxyURL, err := url.Parse(outboundProxy)
		if err != nil {
			return nil, err
		}
		proxyFunc = http.ProxyURL(proxyURL)
	}

	transport := &http.Transport{
		Proxy: proxyFunc,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          c.maxIdleConns,
		MaxIdleConnsPerHost:   c.maxIdleConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: transport,
	}, nil
}
