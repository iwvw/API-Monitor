package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	trans "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/transport"
)

type requestClients struct {
	regular   trans.Doer
	stream    trans.Doer
	fallback  trans.Doer
	fallbackS trans.Doer
}

type cachedProxyClients struct {
	proxy   config.Proxy
	clients requestClients
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

func closeRequestClients(bundle requestClients) {
	for _, doer := range []trans.Doer{bundle.regular, bundle.stream, bundle.fallback, bundle.fallbackS} {
		if closer, ok := doer.(idleConnectionCloser); ok {
			closer.CloseIdleConnections()
		}
	}
}

type hostLookupFunc func(ctx context.Context, network, host string) ([]string, error)

var proxyConnectivityTestURL = "https://chat.deepseek.com/"

var defaultHostLookup hostLookupFunc = func(ctx context.Context, _ string, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// proxyDialAddress returns the address to hand to the proxy dialer.
//
// The hostname is passed through untouched so the SOCKS5 server resolves it at
// the exit node (SOCKS5 ATYP=3, which golang.org/x/net/proxy speaks natively).
// Resolving locally first would leak DNS queries for chat.deepseek.com from the
// host running this proxy, and could pin an account's traffic to an edge IP
// that does not match the geography of its exit node.
func proxyDialAddress(_ context.Context, _, address string, _ hostLookupFunc) (string, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return "", err
	}
	return address, nil
}

func proxyCacheKey(proxyCfg config.Proxy) string {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	return strings.Join([]string{
		proxyCfg.ID,
		proxyCfg.Type,
		strings.ToLower(proxyCfg.Host),
		strconv.Itoa(proxyCfg.Port),
		proxyCfg.Username,
		proxyCfg.Password,
	}, "|")
}

func proxyDialContext(proxyCfg config.Proxy) (trans.DialContextFunc, error) {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	var authCfg *proxy.Auth
	if proxyCfg.Username != "" || proxyCfg.Password != "" {
		authCfg = &proxy.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}
	forward := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(proxyCfg.Host, strconv.Itoa(proxyCfg.Port)), authCfg, forward)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := proxyDialAddress(ctx, proxyCfg.Type, address, defaultHostLookup)
		if err != nil {
			return nil, err
		}
		if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, network, target)
		}
		return dialer.Dial(network, target)
	}, nil
}

func httpCloakProxyURL(proxyCfg config.Proxy) string {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	scheme := strings.ToLower(strings.TrimSpace(proxyCfg.Type))
	if scheme == "socks5h" {
		scheme = "socks5"
	}
	if scheme == "" {
		scheme = "socks5"
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(proxyCfg.Host, strconv.Itoa(proxyCfg.Port)),
	}
	if proxyCfg.Username != "" {
		u.User = url.UserPassword(proxyCfg.Username, proxyCfg.Password)
	}
	return u.String()
}

func (c *Client) defaultRequestClients() requestClients {
	return c.decorate(requestClients{
		regular:   c.regular,
		stream:    c.stream,
		fallback:  c.fallback,
		fallbackS: c.fallbackS,
	})
}

// decorate layers per-account cookie replay and response decompression over a
// bundle. Both have to apply on every path, including the std-transport
// fallbacks, so responses look identical to callers regardless of which
// transport served them.
func (c *Client) decorate(bundle requestClients) requestClients {
	return requestClients{
		regular:   newWireDoer(bundle.regular, c.cookies),
		stream:    newWireDoer(bundle.stream, c.cookies),
		fallback:  newWireDoer(bundle.fallback, c.cookies),
		fallbackS: newWireDoer(bundle.fallbackS, c.cookies),
	}
}

func (c *Client) resolveProxyForAccount(acc config.Account) (config.Proxy, bool) {
	if c == nil || c.Store == nil {
		return config.Proxy{}, false
	}
	proxyID := strings.TrimSpace(acc.ProxyID)
	if proxyID == "" {
		return config.Proxy{}, false
	}
	snap := c.Store.Snapshot()
	for _, proxyCfg := range snap.Proxies {
		proxyCfg = config.NormalizeProxy(proxyCfg)
		if proxyCfg.ID == proxyID {
			return proxyCfg, true
		}
	}
	return config.Proxy{}, false
}

func (c *Client) requestClientsFromContext(ctx context.Context) requestClients {
	if a, ok := auth.FromContext(ctx); ok {
		return c.requestClientsForAccount(a.Account)
	}
	return c.defaultRequestClients()
}

func (c *Client) requestClientsForAuth(ctx context.Context, a *auth.RequestAuth) requestClients {
	if a != nil {
		return c.requestClientsForAccount(a.Account)
	}
	return c.requestClientsFromContext(ctx)
}

func (c *Client) requestClientsForAccount(acc config.Account) requestClients {
	proxyCfg, ok := c.resolveProxyForAccount(acc)
	if !ok {
		return c.defaultRequestClients()
	}

	key := strings.TrimSpace(proxyCfg.ID)
	if key == "" {
		key = proxyCacheKey(proxyCfg)
	}
	c.proxyClientsMu.RLock()
	cached, ok := c.proxyClients[key]
	c.proxyClientsMu.RUnlock()
	if ok && proxyCacheKey(cached.proxy) == proxyCacheKey(proxyCfg) {
		return cached.clients
	}

	dialContext, err := proxyDialContext(proxyCfg)
	if err != nil {
		config.Logger.Warn("[proxy] build dialer failed", "proxy_id", proxyCfg.ID, "error", err)
		return c.defaultRequestClients()
	}

	proxyURL := httpCloakProxyURL(proxyCfg)
	bundle := c.reportingBundle(proxyCfg.ID, c.decorate(requestClients{
		regular:   trans.NewWithProxy(60*time.Second, proxyURL),
		stream:    trans.NewWithProxy(0, proxyURL),
		fallback:  trans.NewFallbackClient(60*time.Second, dialContext),
		fallbackS: trans.NewFallbackClient(0, dialContext),
	}))

	c.proxyClientsMu.Lock()
	if c.proxyClients == nil {
		c.proxyClients = make(map[string]cachedProxyClients)
	}
	if current, exists := c.proxyClients[key]; exists {
		if proxyCacheKey(current.proxy) == proxyCacheKey(proxyCfg) {
			c.proxyClientsMu.Unlock()
			closeRequestClients(bundle)
			return current.clients
		}
		delete(c.proxyClients, key)
		closeRequestClients(current.clients)
	}
	c.proxyClients[key] = cachedProxyClients{proxy: proxyCfg, clients: bundle}
	c.proxyClientsMu.Unlock()
	return bundle
}

// ResetProxyClients 丢弃已缓存的按账号代理客户端并关闭其空闲连接。
// 代理配置（host/port/凭据）变化后必须调用，否则旧 httpcloak/H2 连接池
// 与 fallback http.Transport 会持续泄漏，且配置改回原值时还会复用失效拨号器。
func (c *Client) ResetProxyClients() {
	if c == nil {
		return
	}
	c.proxyClientsMu.Lock()
	old := c.proxyClients
	c.proxyClients = map[string]cachedProxyClients{}
	c.proxyClientsMu.Unlock()
	for _, entry := range old {
		closeRequestClientsIdle(entry.clients)
	}
}

// closeRequestClientsIdle 关闭一组代理客户端底层 transport 的空闲连接。
func closeRequestClientsIdle(bundle requestClients) {
	type idleCloser interface{ CloseIdleConnections() }
	for _, doer := range []trans.Doer{bundle.regular, bundle.stream, bundle.fallback, bundle.fallbackS} {
		if closer, ok := doer.(idleCloser); ok {
			closer.CloseIdleConnections()
		}
	}
}

// reportDoer 把单次上游请求的传输层成败回调给节点健康反馈（mihomo 桥）。
// Do 返回错误视为该出口真实失败；成功（含主传输失败后 fallback 成功）视为
// 节点可达，由桥侧清零该节点的真实失败计数。
type reportDoer struct {
	inner   trans.Doer
	proxyID string
	report  func(proxyID string, success bool)
}

func (d reportDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.inner.Do(req)
	if d.report != nil && d.proxyID != "" {
		d.report(d.proxyID, err == nil)
	}
	return resp, err
}

func (d reportDoer) CloseIdleConnections() {
	type idleCloser interface{ CloseIdleConnections() }
	if closer, ok := d.inner.(idleCloser); ok {
		closer.CloseIdleConnections()
	}
}

// reportingBundle 为已装饰的代理客户端包一层成功/失败上报。
// 无上报回调或非代理路径（proxyID 为空）时原样返回。
func (c *Client) reportingBundle(proxyID string, bundle requestClients) requestClients {
	if proxyID == "" {
		return bundle
	}
	c.nodeReporterMu.RLock()
	fn := c.nodeReporter
	c.nodeReporterMu.RUnlock()
	if fn == nil {
		return bundle
	}
	return requestClients{
		regular:   reportDoer{inner: bundle.regular, proxyID: proxyID, report: fn},
		stream:    reportDoer{inner: bundle.stream, proxyID: proxyID, report: fn},
		fallback:  reportDoer{inner: bundle.fallback, proxyID: proxyID, report: fn},
		fallbackS: reportDoer{inner: bundle.fallbackS, proxyID: proxyID, report: fn},
	}
}

// SetNodeFailureReporter 挂接真实上游请求结果回调（mihomo 桥侧实现，
// 用于把请求成败闭环反馈到节点健康）。
func (c *Client) SetNodeFailureReporter(fn func(proxyID string, success bool)) {
	if c == nil {
		return
	}
	c.nodeReporterMu.Lock()
	c.nodeReporter = fn
	c.nodeReporterMu.Unlock()
}

func applyProxyConnectivityHeaders(req *http.Request) {
	if req == nil {
		return
	}
	for key, value := range dsprotocol.BaseHeaders {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func proxyConnectivityStatus(statusCode int) (bool, string) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return true, fmt.Sprintf("代理可达，目标返回 HTTP %d", statusCode)
	case statusCode >= 300 && statusCode < 500:
		return true, fmt.Sprintf("代理可达，但目标返回 HTTP %d（可能是风控或挑战）", statusCode)
	default:
		return false, fmt.Sprintf("目标返回 HTTP %d", statusCode)
	}
}

func TestProxyConnectivity(ctx context.Context, proxyCfg config.Proxy) map[string]any {
	start := time.Now()
	proxyCfg = config.NormalizeProxy(proxyCfg)
	result := map[string]any{
		"success":       false,
		"proxy_id":      proxyCfg.ID,
		"proxy_type":    proxyCfg.Type,
		"response_time": 0,
	}

	if err := config.ValidateProxyConfig([]config.Proxy{proxyCfg}); err != nil {
		result["message"] = "代理配置无效: " + err.Error()
		return result
	}
	dialContext, err := proxyDialContext(proxyCfg)
	if err != nil {
		result["message"] = "代理拨号器初始化失败: " + err.Error()
		return result
	}

	client := trans.NewFallbackClient(15*time.Second, dialContext)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyConnectivityTestURL, nil)
	if err != nil {
		result["message"] = err.Error()
		return result
	}
	applyProxyConnectivityHeaders(req)

	resp, err := client.Do(req)
	result["response_time"] = int(time.Since(start).Milliseconds())
	if err != nil {
		result["message"] = err.Error()
		return result
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			config.Logger.Warn("[proxy] close response body failed", "proxy_id", proxyCfg.ID, "error", closeErr)
		}
	}()

	result["status_code"] = resp.StatusCode
	result["success"], result["message"] = proxyConnectivityStatus(resp.StatusCode)
	return result
}
