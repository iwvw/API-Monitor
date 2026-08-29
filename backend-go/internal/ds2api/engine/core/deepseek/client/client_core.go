package client

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	trans "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/transport"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/devcapture"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

// intFrom is a package-internal alias for the shared util version.
var intFrom = util.IntFrom

type Client struct {
	Store      *config.Store
	Auth       *auth.Resolver
	capture    *devcapture.Store
	regular    trans.Doer
	stream     trans.Doer
	fallback   *http.Client
	fallbackS  *http.Client
	maxRetries int

	powCache *powChallengeCache
	cookies  *cookieJar

	proxyClientsMu sync.RWMutex
	proxyClients   map[string]cachedProxyClients

	// nodeReporter 在每次经代理的上游请求完成后回调“代理 ID + 成败”，
	// 供 mihomo 代理桥把真实流量结果闭环反馈到节点健康。
	nodeReporterMu sync.RWMutex
	nodeReporter   func(proxyID string, success bool)

	// accountPoolChanged 在账号启用/禁用状态变化（含弹性号池补位）后触发，
	// 供 mihomo 代理桥立即按已有测速结果为新启用账号分配节点。
	poolChangedMu sync.RWMutex
	poolChanged   func()
}

func NewClient(store *config.Store, resolver *auth.Resolver) *Client {
	client := &Client{
		Store:        store,
		Auth:         resolver,
		capture:      devcapture.Global(),
		regular:      trans.New(60 * time.Second),
		stream:       trans.New(0),
		fallback:     &http.Client{Timeout: 60 * time.Second},
		fallbackS:    &http.Client{Timeout: 0},
		maxRetries:   3,
		proxyClients: map[string]cachedProxyClients{},
		powCache:     newPowChallengeCache(),
		cookies:      newCookieJar(),
	}
	if resolver != nil {
		resolver.PostLogin = func(ctx context.Context, a *auth.RequestAuth) {
			client.reportClientSettingsAfterLogin(ctx, a, "")
		}
	}
	return client
}

// PreloadPow 保留兼容接口，纯 Go 实现无需预加载。
func (c *Client) PreloadPow(_ context.Context) error {
	return nil
}

// SetAccountPoolChanged 挂接账号池启用/禁用变化回调（mihomo 桥侧实现，
// 用于新启用账号立即获得节点绑定）。
func (c *Client) SetAccountPoolChanged(fn func()) {
	if c == nil {
		return
	}
	c.poolChangedMu.Lock()
	c.poolChanged = fn
	c.poolChangedMu.Unlock()
}

func (c *Client) notifyAccountPoolChanged() {
	if c == nil {
		return
	}
	c.poolChangedMu.RLock()
	fn := c.poolChanged
	c.poolChangedMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// Close releases pooled upstream connections owned by this client.
func (c *Client) Close() {
	if c == nil {
		return
	}
	closeRequestClients(requestClients{
		regular:   c.regular,
		stream:    c.stream,
		fallback:  c.fallback,
		fallbackS: c.fallbackS,
	})
	c.proxyClientsMu.Lock()
	cached := make([]requestClients, 0, len(c.proxyClients))
	for key, entry := range c.proxyClients {
		cached = append(cached, entry.clients)
		delete(c.proxyClients, key)
	}
	c.proxyClientsMu.Unlock()
	for _, bundle := range cached {
		closeRequestClients(bundle)
	}
}
