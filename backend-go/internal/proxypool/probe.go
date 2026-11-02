package proxypool

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// proxyStateItem 是单个代理的运行时健康信息（供前端「批量测试/状态」展示）。
type proxyStateItem struct {
	Proxy            string `json:"proxy"`
	Reachable        bool   `json:"reachable"`
	LastExitIP       string `json:"lastExitIP,omitempty"`
	LastProbeMs      int64  `json:"lastProbeMs,omitempty"`
	CooldownUntil    string `json:"cooldownUntil,omitempty"`
	RateLimitedUntil string `json:"rateLimitedUntil,omitempty"`
	SunkUntil        string `json:"sunkUntil,omitempty"`
}

// runtime 是池内代理的进程内探活缓存（探活结果、出口 IP）。
type runtime struct {
	mu        sync.Mutex
	reachable map[string]bool
	exitIP    map[string]string
	probeMs   map[string]int64
}

// ProbeAll 对池内全部代理做一次并发探活（经代理访问 ipify 记录出口 IP，
// 可达性标记 + 记录耗时），并写回进程内缓存供 State 读取。上限并发 20。
func (s *Service) ProbeAll(ctx context.Context, poolID string) (total, reachable int, err error) {
	pool, err := s.Get(ctx, poolID)
	if err != nil {
		return 0, 0, err
	}
	if pool == nil {
		return 0, 0, nil
	}
	pool.Proxies = cleanProxies(pool.Proxies)
	if len(pool.Proxies) == 0 {
		return 0, 0, nil
	}
	rt := s.runtimeFor(poolID)
	sem := make(chan struct{}, 20)
	var wg sync.WaitGroup
	var okMu sync.Mutex
	for _, p := range pool.Proxies {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ok, exitIP, ms := probeOnce(ctx, p)
			rt.mu.Lock()
			rt.reachable[p] = ok
			rt.exitIP[p] = exitIP
			rt.probeMs[p] = ms
			rt.mu.Unlock()
			if ok {
				okMu.Lock()
				reachable++
				okMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return len(pool.Proxies), reachable, nil
}

// State 返回池内全部代理的运行时健康信息（合并进程内探活缓存 + 持久化禁用状态）。
func (s *Service) State(ctx context.Context, poolID string) ([]proxyStateItem, error) {
	pool, err := s.Get(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return []proxyStateItem{}, nil
	}
	pool.Proxies = cleanProxies(pool.Proxies)
	rt := s.runtimeFor(poolID)
	items := make([]proxyStateItem, 0, len(pool.Proxies))
	for _, p := range pool.Proxies {
		item := proxyStateItem{Proxy: p}
		rt.mu.Lock()
		item.Reachable = rt.reachable[p]
		item.LastExitIP = rt.exitIP[p]
		item.LastProbeMs = rt.probeMs[p]
		rt.mu.Unlock()
		items = append(items, item)
	}
	return items, nil
}

// runtimeFor 返回池的进程内探活缓存（懒创建）。
func (s *Service) runtimeFor(poolID string) *runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeByPool == nil {
		s.runtimeByPool = map[string]*runtime{}
	}
	rt, ok := s.runtimeByPool[poolID]
	if !ok {
		rt = &runtime{
			reachable: map[string]bool{},
			exitIP:    map[string]string{},
			probeMs:   map[string]int64{},
		}
		s.runtimeByPool[poolID] = rt
	}
	return rt
}

// proxyClient 构建经指定代理出网的 http.Client（http/https/socks5），
// 复用 x/net/proxy 的 context 取消能力，出口严格落在显式代理上。
func proxyClient(proxyURL string) (*http.Client, error) {
	u, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	if err := configureProxyTransport(tr, u); err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr, Timeout: 20 * time.Second}, nil
}

func normalizeProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks5", "socks", "socks5h":
		u.Scheme = "socks5"
	case "":
		u = &url.URL{Scheme: "socks5", Host: strings.TrimSpace(raw)}
	default:
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errUnsupportedScheme
		}
	}
	if u.Host == "" {
		return nil, errMissingHost
	}
	return u, nil
}

func configureProxyTransport(tr *http.Transport, u *url.URL) error {
	switch u.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
		return nil
	case "socks5":
		tr.Proxy = nil
		forward := &net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}
		dialer, err := proxy.FromURL(u, forward)
		if err != nil {
			return err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return errNotContextDialer
		}
		tr.DialContext = contextDialer.DialContext
		return nil
	}
	return errUnsupportedScheme
}

// probeOnce 经代理访问 ipify 探活，返回（可达, 出口IP, 耗时ms）。
func probeOnce(ctx context.Context, proxyURL string) (bool, string, int64) {
	client, err := proxyClient(proxyURL)
	if err != nil {
		return false, "", 0
	}
	start := time.Now()
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return false, "", 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	ms := time.Since(start).Milliseconds()
	exitIP := strings.TrimSpace(string(body))
	return resp.StatusCode < 400, exitIP, ms
}

var (
	errUnsupportedScheme = &net.OpError{Op: "proxy", Err: errScheme}
	errMissingHost       = &net.OpError{Op: "proxy", Err: errHost}
	errNotContextDialer  = &net.OpError{Op: "proxy", Err: errDialer}
)

var (
	errScheme = errString("不支持的代理协议")
	errHost   = errString("代理地址缺少 host:port")
	errDialer = errString("socks5 拨号器不支持 context")
)

type errString string

func (e errString) Error() string { return string(e) }
