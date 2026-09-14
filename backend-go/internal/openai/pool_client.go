package openai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// normalizeProtocol 规范化端点连接协议设置：
//   - "" / auto：自动协商（HTTP/2 优先，服务端不支持时回退 HTTP/1.1），即默认行为
//   - http1：强制 HTTP/1.1（对齐主流 AI SDK / 官方客户端的传输层）
//   - h2：偏好 HTTP/2（标准库仅做 ALPN 协商，服务端不支持时仍回退 HTTP/1.1）
//
// 未知值一律回退 auto，避免旧配置 / 脏数据导致转发失败。
func normalizeProtocol(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "http1", "http/1.1", "http1.1", "h1":
		return "http1"
	case "h2", "http2", "http/2":
		return "h2"
	default:
		return "auto"
	}
}

// clientForProtocol 返回绑定指定连接协议的直连客户端。
// 客户端按协议名缓存，同一协议共享连接池；auto 与 h2 使用同一传输层配置
// （ForceAttemptHTTP2 开启、ALPN 协商），http1 关闭 HTTP/2 升级。
func (s *Service) clientForProtocol(protocol string) *http.Client {
	key := normalizeProtocol(protocol)
	s.protocolMu.Lock()
	defer s.protocolMu.Unlock()
	if c, ok := s.protocolClients[key]; ok {
		return c
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 兜底限制「等待响应头」的时间；快速切换由 headerTimeoutPerAttempt 在转发循环内控制，
		// 故此处放宽到 180s，避免误杀「慢但最终成功」的非流式请求（推理模型思考阶段可能超过 60s），
		// 也不限制流式响应体时长。
		ResponseHeaderTimeout: 180 * time.Second,
	}
	if key == "http1" {
		// 关闭 HTTP/2 升级：既不尝试 h2 也不在 ALPN 中声明 h2，
		// 与 node fetch / curl 等 HTTP/1.1 客户端的传输行为一致。
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	c := &http.Client{Transport: tr}
	s.protocolClients[key] = c
	return c
}

// normalizeProxyURL 校验并规范化代理 URL：
//   - socks://socks5://socks5h:// 与裸 host:port 统一为 socks5（远端解析域名）
//   - 仅接受 socks5 与 http/https 代理，其余协议报错
func normalizeProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks5", "socks", "socks5h":
		// socks:// 是常见订阅节点前缀，socks5h 表示远端解析域名，均按 SOCKS5 处理。
		u.Scheme = "socks5"
	case "":
		// 裸地址（host:port）默认按 socks5 处理，便于直接粘贴节点地址。
		u = &url.URL{Scheme: "socks5", Host: strings.TrimSpace(raw)}
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

// configureProxyTransport 把代理绑定到 transport（复刻 New API 的渠道代理隔离做法）：
//   - socks5：用 x/net/proxy 构造支持 context 取消的拨号器，替代标准库不支持的 http.ProxyURL
//   - http/https：直接使用 http.ProxyURL
//
// 返回的 transport 在启用代理后不依赖环境变量（HTTP_PROXY/HTTPS_PROXY），
// 保证出口严格落在显式配置的代理上，避免「代理池外 IP」出现。
func configureProxyTransport(tr *http.Transport, u *url.URL) error {
	switch u.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
		return nil
	case "socks5":
		tr.Proxy = nil
		forwardDialer := &net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		dialer, err := proxy.FromURL(u, forwardDialer)
		if err != nil {
			return err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS5 代理拨号器不支持 context 取消")
		}
		tr.DialContext = contextDialer.DialContext
		return nil
	default:
		return fmt.Errorf("不支持的代理协议: %s", u.Scheme)
	}
}

// proxyClients 按代理 URL 缓存 http.Client，避免每次请求重建 transport。
var proxyClients = struct {
	sync.Mutex
	m map[string]*http.Client
}{m: make(map[string]*http.Client)}

func (s *Service) proxyClient(proxyURL string) (*http.Client, error) {
	u, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	proxyClients.Lock()
	defer proxyClients.Unlock()
	if c, ok := proxyClients.m[proxyURL]; ok {
		return c, nil
	}
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 兜底限制「等待响应头」的时间；排队的上游（免费模型高峰）可能 30s+ 才
		// 返回响应头，固定 30s 会误杀「慢但最终成功」的请求，故放宽到 180s。
		// 首字失败切换由转发循环的 firstTokenTimeout（收到响应头后等首块）控制。
		ResponseHeaderTimeout: 180 * time.Second,
	}
	if err := configureProxyTransport(tr, u); err != nil {
		return nil, err
	}
	c := &http.Client{Transport: tr}
	proxyClients.m[proxyURL] = c
	return c, nil
}

// readWithIdleTimeout 为阻塞式上游读加中段空闲超时：idle 内无任何字节到达
// 则返回 errStreamIdleTimeout，避免上游流中途停滞时请求无限挂死。
// 超时时主动关闭底层 reader（若是 io.Closer），让阻塞在 Read 上的 goroutine
// 立即返回并退出，避免上游停滞时累积僵尸读取 goroutine。
func readWithIdleTimeout(ctx context.Context, r io.Reader, p []byte, idle time.Duration) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := r.Read(p)
		select {
		case ch <- readResult{n: n, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-ctx.Done():
		// 请求被取消（客户端断连/超时）：立即关闭底层连接让阻塞读返回，
		// 停止上游继续生成（不浪费 token），并释放读 goroutine。
		if c, ok := r.(io.Closer); ok {
			_ = c.Close()
		}
		return 0, ctx.Err()
	case <-time.After(idle):
		// 超时即放弃这条上游流：关闭底层连接让阻塞读返回，释放 goroutine
		// 与连接缓冲区。调用方后续无需再读该 body（重复 Close 幂等）。
		if c, ok := r.(io.Closer); ok {
			_ = c.Close()
		}
		return 0, errStreamIdleTimeout
	}
}

var errStreamIdleTimeout = errors.New("upstream stream idle timeout")
