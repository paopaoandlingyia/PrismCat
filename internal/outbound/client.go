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
	normalizedProxy, err := config.NormalizeOutboundProxy(outboundProxy)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[normalizedProxy]; ok {
		return client, nil
	}

	client, err := c.newClient(normalizedProxy)
	if err != nil {
		return nil, err
	}
	c.clients[normalizedProxy] = client
	return client, nil
}

func (c *ClientCache) newClient(outboundProxy string) (*http.Client, error) {
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
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: transport,
	}, nil
}
