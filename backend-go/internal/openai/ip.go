package openai

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// resolveClientIP 返回调用网关的客户端 IP：
// 仅当直连地址属于受信代理网关时才会采信 X-Forwarded-For / X-Real-IP，
// 防止伪造代理头。逻辑与 apikeys.requestIP 保持一致。
func (s *Service) resolveClientIP(r *http.Request) string {
	direct := ""
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		direct = host
	} else if r.RemoteAddr != "" {
		direct = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	if direct == "" {
		return ""
	}
	ip := net.ParseIP(direct)
	trusted := false
	if ip != nil {
		if isTrustedProxy(ip, s.cfg.TrustedProxyCIDRs) {
			trusted = true
		} else if !s.cfg.IsProduction() && ip.IsLoopback() {
			trusted = true
		}
	}
	if trusted {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			candidate := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
		if candidate := strings.TrimSpace(r.Header.Get("X-Real-IP")); candidate != "" {
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
	}
	return direct
}

func isTrustedProxy(ip net.IP, entries []string) bool {
	if ip == nil {
		return false
	}
	for _, entry := range entries {
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

type egressEntry struct {
	ip        string
	expiresAt time.Time
}

// egressIPCache 缓存本机出口 IP（含探测失败的负缓存），一段时间内不重复探测，
// 避免热路径反复出网。成功结果缓存 egressIPCacheTTL；探测失败（含本地网卡
// 回退也为空）只缓存 egressFailCacheTTL，避免外部回显服务故障时每请求重复出网。
var egressIPCache = struct {
	sync.Mutex
	entry egressEntry
}{}

// egressProbeFlight 手写 singleflight：保证同一时刻只有一个探测在执行，
// 缓存过期瞬间的并发请求共享同一次探测结果，而不是各自同步出网阻塞
// （此前持全局锁探测会把缓存到期后的全部网关请求串行阻塞最长约 6s）。
var egressProbeFlight struct {
	sync.Mutex
	inflight chan struct{}
}

// egressProbeTimeout 是外部回显探测的拨号/读取超时，避免慢网络阻塞请求热路径。
const egressProbeTimeout = 3 * time.Second

// egressIPCacheTTL 是出口 IP 缓存时长。公网出口 IP 极少变化，用较长缓存避免
// 每次请求都发外部探测；本地网卡回退值同理适用该 TTL。
const egressIPCacheTTL = 10 * time.Minute

// egressFailCacheTTL 是探测失败（公网与本地网卡均拿不到）的负缓存时长：
// 短缓存即可，既避免故障期每请求重复探测，又不至于恢复过慢。
const egressFailCacheTTL = 60 * time.Second

// egressIPEchoURL 是用于探测公网出口 IP 的外部回显服务（纯文本返回 IP）。
const egressIPEchoURL = "https://api.ipify.org"

// egressOutboundIP 返回本机可用的公网出口 IP（用于直连场景下的出口标识）。
// 通过外部回显服务探测真实公网出口地址（而非本地网卡的私有地址）；
// 探测失败时回退到本地网卡地址。探测一次本地缓存，避免热路径反复出网。
func egressOutboundIP() string {
	return egressOutboundIPOnce(func() string {
		ip := probePublicEgressIP()
		if ip == "" {
			ip = localInterfaceIP()
		}
		return ip
	})
}

// egressOutboundIPOnce 执行「读缓存 → 过期则 singleflight 探测 → 写缓存」。
// probe 注入探测函数（公网回显 + 本地网卡回退），便于测试替换。
func egressOutboundIPOnce(probe func() string) string {
	egressIPCache.Lock()
	if time.Now().Before(egressIPCache.entry.expiresAt) {
		ip := egressIPCache.entry.ip
		egressIPCache.Unlock()
		return ip
	}
	egressIPCache.Unlock()

	// singleflight：已有探测在执行时等待其完成并直接采用其写入的缓存结果。
	egressProbeFlight.Lock()
	if ch := egressProbeFlight.inflight; ch != nil {
		egressProbeFlight.Unlock()
		<-ch
		egressIPCache.Lock()
		ip := egressIPCache.entry.ip
		egressIPCache.Unlock()
		return ip
	}
	done := make(chan struct{})
	egressProbeFlight.inflight = done
	egressProbeFlight.Unlock()

	ip := probe()
	ttl := egressIPCacheTTL
	if ip == "" {
		ttl = egressFailCacheTTL
	}
	egressIPCache.Lock()
	egressIPCache.entry = egressEntry{ip: ip, expiresAt: time.Now().Add(ttl)}
	egressIPCache.Unlock()

	// 先写缓存再关闭 done：等待方经由 channel 的 happens-before 读到新结果。
	egressProbeFlight.Lock()
	egressProbeFlight.inflight = nil
	egressProbeFlight.Unlock()
	close(done)
	return ip
}

// probePublicEgressIP 通过外部回显服务获取公网出口 IP；超时/失败返回空串。
func probePublicEgressIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), egressProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, egressIPEchoURL, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// localInterfaceIP 返回本机出网使用的网卡地址（私有地址，仅作回退）。
func localInterfaceIP() string {
	if conn, err := net.DialTimeout("udp", "8.8.8.8:80", egressProbeTimeout); err == nil {
		if local, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			conn.Close()
			return local.IP.String()
		}
		conn.Close()
	}
	return ""
}

// egressOutbound 返回本机出口 IP（Service 便捷封装）。
func (s *Service) egressOutbound() string {
	return egressOutboundIP()
}

// proxyEndpointAddr 从代理字符串中提取出外连接地址（host:port），用于在网关日志
// 中标识"本次请求实际走了哪个代理出口"。兼容 socks://user:pass@host:port 形式。
func proxyEndpointAddr(proxy string) string {
	if proxy == "" {
		return ""
	}
	u, err := url.Parse(proxy)
	if err != nil {
		return proxy
	}
	if u.Host == "" {
		return proxy
	}
	return u.Host
}
