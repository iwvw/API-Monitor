package posthogcode

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// resolveProxy 从设置解析注册/自动登录要用的出口代理；空串表示直连。
// 目的是绕过 PostHog 对注册的 IP 限流与人机验证。
func (s *Service) resolveProxy(ctx context.Context) string {
	return s.resolveProxyWith(ctx, "")
}

// resolveProxyWith 在设置值之上支持按次覆盖（注册对话框里临时选的池）。
func (s *Service) resolveProxyWith(ctx context.Context, override string) string {
	poolID := strings.TrimSpace(override)
	if poolID == "" {
		st := s.Settings()
		poolID = strings.TrimSpace(st.ProxyPoolID)
	}
	if poolID == "" {
		return ""
	}
	s.mu.RLock()
	sel := s.externalPool
	s.mu.RUnlock()
	if sel == nil {
		return ""
	}
	p, _ := sel.SelectProxy(ctx, poolID, "")
	return p
}

// autologinClient 包装一个带独立 cookie jar 的客户端，并记录该会话的出口代理。
// 代理一旦选定就固定给该会话复用，避免同一登录流程中途换 IP 导致 PostHog 判定异常。
func (s *Service) newAutologinClient(ctx context.Context) (*http.Client, *cookiejar.Jar, string, error) {
	return s.newAutologinClientWith(ctx, "")
}

// newAutologinClientWith 支持按次指定代理池。
func (s *Service) newAutologinClientWith(ctx context.Context, override string) (*http.Client, *cookiejar.Jar, string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, "", err
	}
	proxyURL := s.resolveProxyWith(ctx, override)
	if proxyURL == "" {
		return &http.Client{Timeout: 30 * time.Second, Jar: jar}, jar, "", nil
	}
	client, err := proxyClient(proxyURL)
	if err != nil {
		return nil, nil, "", fmt.Errorf("代理不可用：%w", err)
	}
	client.Jar = jar
	return client, jar, proxyURL, nil
}

// proxyClients 按代理 URL 缓存 transport，避免每次登录重建连接池。
var proxyClients = struct {
	sync.Mutex
	m map[string]*http.Client
}{m: map[string]*http.Client{}}

// proxyClient 构造走指定代理的 HTTP 客户端。
// socks5 用 x/net/proxy 拨号器（标准库 http.ProxyURL 不支持），http/https 直接用 ProxyURL。
func proxyClient(raw string) (*http.Client, error) {
	u, err := normalizeProxyURL(raw)
	if err != nil {
		return nil, err
	}
	key := u.String()
	proxyClients.Lock()
	defer proxyClients.Unlock()
	if c, ok := proxyClients.m[key]; ok {
		return cloneClient(c), nil
	}
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	switch u.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
	case "socks5":
		dialer, derr := proxy.FromURL(u, &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second})
		if derr != nil {
			return nil, derr
		}
		cd, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 代理不支持 context 取消")
		}
		tr.DialContext = cd.DialContext
	default:
		return nil, fmt.Errorf("不支持的代理协议: %s", u.Scheme)
	}
	c := &http.Client{Timeout: 30 * time.Second, Transport: tr}
	proxyClients.m[key] = c
	return cloneClient(c), nil
}

// cloneClient 复制客户端，让调用方可以安全地各自设置 Jar 与 CheckRedirect。
func cloneClient(c *http.Client) *http.Client {
	cp := *c
	return &cp
}

// normalizeProxyURL 校验并规范化代理地址：
// socks/socks5h 与裸 host:port 统一为 socks5，其余仅接受 http/https。
func normalizeProxyURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("代理地址为空")
	}
	// 裸 host:port 会被 url.Parse 误当作 scheme（如 "host:1080" -> scheme=host），
	// 先按「无 :// 即视为 socks5」处理，避免误判为不支持的协议。
	if !strings.Contains(trimmed, "://") {
		return &url.URL{Scheme: "socks5", Host: trimmed}, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks5", "socks", "socks5h":
		u.Scheme = "socks5"
	default:
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("不支持的代理协议: %s", u.Scheme)
		}
	}
	if u.Host == "" {
		return nil, fmt.Errorf("代理地址缺少 host:port: %s", raw)
	}
	return u, nil
}
