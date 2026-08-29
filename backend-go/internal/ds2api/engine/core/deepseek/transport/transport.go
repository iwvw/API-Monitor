package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Client dials upstream with a browser-profiled (Chrome) TLS ClientHello and
// HTTP/2 fingerprint via httpcloak.
type Client struct {
	cloak *httpCloakDoer
}

func New(timeout time.Duration) *Client {
	return newHTTPCloakTransport(timeout, "", timeout == 0)
}

// NewWithProxy creates a browser-profiled client using httpcloak's TCP proxy
// support. The proxy URL is kept at the transport boundary so account-level
// proxy pools do not need to know about httpcloak types.
func NewWithProxy(timeout time.Duration, proxyURL string) *Client {
	return newHTTPCloakTransport(timeout, proxyURL, timeout == 0)
}

func newHTTPCloakTransport(timeout time.Duration, proxyURL string, streaming bool) *Client {
	return &Client{
		cloak: newHTTPCloakDoer(timeout, proxyURL, streaming),
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if c == nil || c.cloak == nil {
		return nil, fmt.Errorf("transport client is not initialised")
	}
	return c.cloak.Do(req)
}

// CloseIdleConnections 释放 httpcloak 底层连接池的空闲连接，
// 供代理配置变更后清理缓存客户端使用。
func (c *Client) CloseIdleConnections() {
	if c == nil || c.cloak == nil || c.cloak.client == nil {
		return
	}
	c.cloak.client.Close()
}

func NewFallbackClient(timeout time.Duration, dialContext DialContextFunc) *http.Client {
	useEnvProxy := dialContext == nil
	if dialContext == nil {
		dialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	base := &http.Transport{
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         dialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if useEnvProxy {
		base.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{Timeout: timeout, Transport: base}
}
