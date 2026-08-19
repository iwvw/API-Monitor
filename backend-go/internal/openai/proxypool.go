package openai

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"golang.org/x/net/proxy"
)

func (s *Service) trimSessionBindings(state *endpointProxyState, now time.Time) {
	if len(state.sessionBindings) < sessionBindingMax {
		return
	}
	for k, v := range state.sessionBindings {
		if now.Sub(v.updatedAt) > sessionBindingTTL {
			delete(state.sessionBindings, k)
		}
	}
	if len(state.sessionBindings) >= sessionBindingMax {
		state.sessionBindings = make(map[string]*sessionBinding)
	}
}

// endpointPickOverride 是测试专用确定性选路钩子（生产恒为 nil）：
// 覆盖延迟加权随机，供依赖「端点 A 先被选中」的 failover 测试消除 flake。
var endpointPickOverride func(candidates []Endpoint) int

// recordEndpointLatency 记录端点最近一次转发延迟（毫秒），供延迟加权分流使用。
func (s *Service) recordEndpointLatency(endpointID string, latencyMs int64) {
	if endpointID == "" || latencyMs <= 0 {
		return
	}
	s.latencyMu.Lock()
	s.endpointLatency[endpointID] = latencyMs
	s.endpointLatencyOK[endpointID] = true
	s.latencyMu.Unlock()
}

// getEndpointLatency 读取端点最近转发延迟；无记录时返回 (0, false)。
func (s *Service) getEndpointLatency(endpointID string) (int64, bool) {
	s.latencyMu.RLock()
	defer s.latencyMu.RUnlock()
	ok := s.endpointLatencyOK[endpointID]
	return s.endpointLatency[endpointID], ok
}

// weightedEndpointPick 在可服务同一模型的端点中按延迟加权随机选择：
// 权重 = 1 + (maxLatency - latency) / 200，延迟越低的端点权重越高，
// 健康快的端点被选中概率更高；尚无延迟记录的端点按中等延迟（maxLatency）
// 参与，保证首次使用也有机会被选中。返回选中下标。
func weightedEndpointPick(latencies []int64, known []bool) int {
	maxLatency := int64(0)
	for i, latency := range latencies {
		if known[i] && latency > maxLatency {
			maxLatency = latency
		}
	}
	if maxLatency == 0 {
		maxLatency = 1000
	}

	total := int64(0)
	weights := make([]int64, len(latencies))
	for i, latency := range latencies {
		effective := latency
		if !known[i] {
			// 无记录端点视为中等延迟，避免被饿死。
			effective = maxLatency
		}
		weight := int64(1) + (maxLatency-effective)/200
		if weight < 1 {
			weight = 1
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weights {
		acc += w
		if n.Int64() < acc {
			return i
		}
	}
	return len(latencies) - 1
}

// randIntN 返回 [0, n) 内的安全随机整数；n <= 0 时返回 0。
// 用于并发请求需要打散到不同出口的场景（如全池解冻后分散起点）。
func randIntN(n int) int {
	if n <= 0 {
		return 0
	}
	bigN, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(bigN.Int64())
}

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

// newEndpointProxyState 创建端点的代理池运行时状态。
func newEndpointProxyState() *endpointProxyState {
	return &endpointProxyState{
		cursor:          0,
		cooldown:        make(map[string]time.Time),
		failures:        make(map[string]int),
		sessionBindings: make(map[string]*sessionBinding),
		lastTTFB:        make(map[string]int64),
		rate429:         make(map[string]int),
		rateLimited:     make(map[string]time.Time),
		sunk:            make(map[string]time.Time),
		lastExitIP:      make(map[string]string),
		lastProbeAt:     make(map[string]time.Time),
	}
}

// clientForEndpoint 按端点代理池选择下一个可用代理，返回绑定该代理的 http.Client。
// 规则：
//   - proxyEnabled 关闭：忽略代理池，返回按端点 protocol 配置的直连客户端
//   - proxyEnabled 开启且池为空：forceProxy 开启时报错（禁止直连），否则回退直连
//   - proxyEnabled 开启且有池：按池选择代理；非空 sessionKey 时优先复用
//     会话粘性绑定的代理（同一会话固定出口，请求数达 sessionProxyRequestLimit 后
//     主动轮换下一个出口，规避上游按出口 IP 的限额）
func (s *Service) clientForEndpoint(endpointID string, pool []string, proxyEnabled, forceProxy bool, sessionKey, protocol string) (*http.Client, string, error) {
	if !proxyEnabled {
		return s.clientForProtocol(protocol), "", nil
	}
	cleaned := cleanProxyPool(pool)
	if len(cleaned) == 0 {
		if forceProxy {
			return nil, "", fmt.Errorf("端点配置为强制走代理，但代理池为空")
		}
		return s.client, "", nil
	}
	now := time.Now()

	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	s.trimSessionBindings(state, now)
	if state.cursor >= len(cleaned) || state.cursor < 0 {
		state.cursor = 0
	}

	// 会话粘性：绑定代理仍在池内、未冷却、未处于 429 禁用期、未被沉淀为坏代理、
	// 计数未达上限时直接复用，保持同一会话的出口 IP 稳定（配额感知轮换的前提）。
	if sessionKey != "" {
		if binding, bound := state.sessionBindings[sessionKey]; bound {
			bindingOK := false
			if until, cooled := state.cooldown[binding.proxy]; !cooled || now.After(until) {
				if !proxyRateLimited(state, binding.proxy, now) {
					if _, sunk := state.sunk[binding.proxy]; !sunk || now.After(state.sunk[binding.proxy]) {
						if binding.count < sessionProxyRequestLimit {
							if _, inPool := poolIndex(binding.proxy, cleaned); inPool {
								binding.count++
								binding.updatedAt = now
								bindingOK = true
							}
						}
					}
				}
			}
			if bindingOK {
				selectedProxy := binding.proxy
				state.cursor = (indexOfProxy(binding.proxy, cleaned) + 1) % len(cleaned)
				s.proxyMu.Unlock()
				client, err := s.proxyClient(selectedProxy)
				if err != nil {
					return s.client, selectedProxy, err
				}
				return client, selectedProxy, nil
			}
			// 绑定失效（冷却/429 禁用/沉淀/计数满/移出池）：解除并走择优换新出口。
			delete(state.sessionBindings, sessionKey)
		}
	}

	// 可用代理：未在冷却中、且未处于 429 禁用期、未被沉淀为坏代理的代理集合。
	candidates := []proxyCandidate{}
	for i := range cleaned {
		if until, cooled := state.cooldown[cleaned[i]]; cooled && !now.After(until) {
			continue
		}
		if proxyRateLimited(state, cleaned[i], now) {
			continue
		}
		if until, sunk := state.sunk[cleaned[i]]; sunk && now.Before(until) {
			continue
		}
		ttfb, known := state.lastTTFB[cleaned[i]]
		candidates = append(candidates, proxyCandidate{idx: i, ttfb: ttfb, known: known})
	}

	// 从候选集中选一个：优先让没有延迟记录的代理进入轮询（探索），
	// 全部已知后按延迟加权选择，避免单个最快代理独占全部流量。
	selectedIdx := -1
	switch {
	case len(candidates) == 0:
		// 全部冷却/禁用：退化为 cursor 轮询，先满足请求再说
		// （跳过 429 禁用中的代理，避免反复打同一个被限死的 IP）。
		for i := 0; i < len(cleaned); i++ {
			idx := (state.cursor + i) % len(cleaned)
			if proxyRateLimited(state, cleaned[idx], now) {
				continue
			}
			if until, sunk := state.sunk[cleaned[idx]]; sunk && now.Before(until) {
				continue
			}
			selectedIdx = idx
			break
		}
		if selectedIdx == -1 {
			// 全部出口都处于 429 冻结/坏代理沉淀：IP 级限流已把整个池锁死，硬选冻结代理只会
			// 反复 429（老行为：selectedIdx = cursor % len(pool) 直接选回被冻 IP，
			// 形成「全部冻结 → 每请求全池扫一遍 → 又全部冻结」的限流风暴）。
			// 自动解冻全体代理：清除全部冷却/429 冻结/沉淀状态，让池子重新获得
			// 出网机会（可能刚解冻即恢复）；但带节流，避免上游未恢复时反复解冻
			// 导致每请求都全池扫一遍。节流窗口内仍回退直连兜底。
			if s.autoUnfreezeAllLocked(endpointID, cleaned, now) {
				// 解冻成功：本次请求直接复用解冻后的池子，重新构建候选再择优。
				// 解冻后所有代理均可用（冷却/冻结/沉淀已清空），从 cursor 起取一个。
				// 解冻时刻会有大量并发请求同时涌入，若都从 cursor 起点开始会全部
				// 命中同一批代理，形成「刚解冻就被再次打爆」的雪崩。从 cursor 起加
				// 一个随机偏移，让并发请求散开到池内不同出口，避免集中重打。
				offset := 0
				if len(cleaned) > 1 {
					offset = randIntN(len(cleaned))
				}
				selectedIdx = (state.cursor + offset) % len(cleaned)
				break
			}
			s.logProxyPoolFrozen(endpointID, cleaned, now)
			s.proxyMu.Unlock()
			return s.client, "", nil
		}
	case len(candidates) == 1:
		selectedIdx = candidates[0].idx
	default:
		unknownAny := false
		for _, c := range candidates {
			if !c.known {
				unknownAny = true
				break
			}
		}
		if unknownAny {
			// 探索：在候选（未冷却）集合内按 cursor 轮询，绝不选中冷却代理。
			cursorPos := state.cursor % len(candidates)
			selectedIdx = candidates[cursorPos].idx
		} else {
			// 全部已知：延迟加权随机。延迟越低权重越高，但保留次优代理出现的机会，
			// 兼顾「选快代理」与「多代理分摊流量」。
			selectedIdx = weightedProxyPick(candidates)
		}
	}
	state.cursor = (selectedIdx + 1) % len(cleaned)
	selectedProxy := cleaned[selectedIdx]

	// 新出口绑定到会话（从 1 次计数开始），后续请求在此计数内保持同一出口。
	if sessionKey != "" {
		state.sessionBindings[sessionKey] = &sessionBinding{proxy: selectedProxy, count: 1, updatedAt: now}
	}
	s.proxyMu.Unlock()

	client, err := s.proxyClient(selectedProxy)
	if err != nil {
		return s.client, selectedProxy, err
	}
	return client, selectedProxy, nil
}

// poolIndex 返回 proxy 在 cleaned 池中的下标；不在池中返回 -1。
func poolIndex(proxy string, cleaned []string) (int, bool) {
	for i, p := range cleaned {
		if p == proxy {
			return i, true
		}
	}
	return -1, false
}

// indexOfProxy 返回 proxy 在池中的下标；不在池中时回退到 0（仅用于游标推进，无害）。
func indexOfProxy(proxy string, cleaned []string) int {
	if i, ok := poolIndex(proxy, cleaned); ok {
		return i
	}
	return 0
}

// clearSessionBinding 解除某端点下会话与出口 IP 的粘性绑定。
// 收到上游 429/5xx 切换出口时调用，使下一次请求重新绑定新出口（配额感知轮换）。
func (s *Service) clearSessionBinding(endpointID, sessionKey string) {
	if sessionKey == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		delete(state.sessionBindings, sessionKey)
	}
}

// resolveSessionKey 从请求头或请求体中提取会话标识：
// 依次取 X-OpenCode-Session-ID / X-Opencode-Session-ID / X-Relay-Session-ID / X-Session-ID，
// 再回退到请求体 user 字段；都没有时返回空串（退化为池内轮询）。
func resolveSessionKey(r *http.Request, parsedBody map[string]interface{}) string {
	for _, h := range []string{"X-OpenCode-Session-ID", "X-Opencode-Session-ID", "X-Relay-Session-ID", "X-Session-ID"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if user, ok := parsedBody["user"].(string); ok {
		if user = strings.TrimSpace(user); user != "" {
			return "user:" + user
		}
	}
	return ""
}

// auxClientForPool 为辅助请求（验证、模型列表、健康检测等）选择代理 client。
// 与 clientForEndpoint 的区别：不推进端点的游标、不写 TTFB、不写冷却，
// 只读取冷却状态做跳过，避免辅助请求污染真实转发的择优状态。
func (s *Service) auxClientForPool(endpointID string, pool []string) (*http.Client, string) {
	if len(pool) == 0 {
		return s.client, ""
	}
	cleaned := cleanProxyPool(pool)
	if len(cleaned) == 0 {
		return s.client, ""
	}

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	selectedProxy := ""
	for _, candidate := range cleaned {
		if ok && state.cooldown[candidate] != (time.Time{}) && !now.After(state.cooldown[candidate]) {
			continue
		}
		if ok && proxyRateLimited(state, candidate, now) {
			continue
		}
		if ok {
			if until, sunk := state.sunk[candidate]; sunk && now.Before(until) {
				continue
			}
		}
		selectedProxy = candidate
		break
	}
	if selectedProxy == "" {
		// 全部冷却/禁用：退化为池内第一个，先满足请求再说。
		selectedProxy = cleaned[0]
	}
	s.proxyMu.Unlock()

	client, err := s.proxyClient(selectedProxy)
	if err != nil {
		return s.client, selectedProxy
	}
	return client, selectedProxy
}

// proxyCandidate 是择优时的候选代理：idx 为 cleaned 池中的下标，ttfb 为最近一次
// 首字耗时（毫秒），known 表示是否已产生过 TTFB 记录。
type proxyCandidate struct {
	idx   int
	ttfb  int64
	known bool
}

// weightedProxyPick 在全部已知延迟的候选代理中做加权选择：
// 权重 = 1 + (maxTTFB - ttfb) / 200，延迟越低的代理权重越高。
// 权重差按 200ms 为一档，既能让几百毫秒的快慢差异被感知，又不会让
// 极端慢代理彻底失去机会，从而兼顾「优先选快代理」与「多代理分摊流量」。
func weightedProxyPick(candidates []proxyCandidate) int {
	maxTTFB := int64(0)
	for _, c := range candidates {
		if c.ttfb > maxTTFB {
			maxTTFB = c.ttfb
		}
	}
	total := int64(0)
	weights := make([]int64, len(candidates))
	for i, c := range candidates {
		weight := int64(1) + (maxTTFB-c.ttfb)/200
		if weight < 1 {
			weight = 1
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return candidates[0].idx
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weights {
		acc += w
		if n.Int64() < acc {
			return candidates[i].idx
		}
	}
	return candidates[len(candidates)-1].idx
}

// recordProxyTTFB 记录某端点下某代理的一次首字耗时，供后续请求择优。
func (s *Service) recordProxyTTFB(endpointID, proxy string, ttfbMs int64) {
	if endpointID == "" || proxy == "" || ttfbMs <= 0 {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.lastTTFB[proxy] = ttfbMs
}

// markProxyFailed 将某个代理标记为冷却，后续选择会跳过它。
// 冷却时长按连续失败次数指数退避：1min << min(failures-1, 5)，封顶 30min。
// 只应在「传输层/链路」失败时调用；上游 429/5xx 不是代理的错，不应惩罚代理
// （否则上游故障会污染整个代理池）。
func (s *Service) markProxyFailed(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return
	}
	state.failures[proxy]++
	fails := state.failures[proxy]
	shift := fails - 1
	if shift > proxyCooldownShift {
		shift = proxyCooldownShift
	}
	cooldown := proxyCooldown << shift
	if cooldown > proxyCooldownMax {
		cooldown = proxyCooldownMax
	}
	state.cooldown[proxy] = time.Now().Add(cooldown)
	s.persistProxyState(endpointID, proxy, "cooldown", state.cooldown[proxy])
}

// markProxySuccess 清除代理的失败计数与冷却（探活/预热成功时调用），使之立即恢复可选。
func (s *Service) markProxySuccess(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return
	}
	delete(state.failures, proxy)
	delete(state.cooldown, proxy)
	s.persistProxyState(endpointID, proxy, "cooldown", time.Time{})
	s.persistProxyState(endpointID, proxy, "rate_limited", time.Time{})
	s.persistProxyState(endpointID, proxy, "sunk", time.Time{})
}

// markProxy429 记录代理的一次上游 429。与 markProxyFailed 的区别：
// 429 是上游按出口 IP 的限流，单次不惩罚代理（避免上游故障污染整个池）；
// 但同一代理累计 proxy429BanThreshold 次 429 说明该 IP 已被上游限死，
// 继续把它留在候选池只会让重试反复打同一个 IP，故临时禁用 proxy429BanDuration，
// 到期自动释放回池。成功转发不解除禁用；触发禁用时清零累计计数（重新累计下一轮）。
// retryAfter 非 nil 时优先用上游给出的 Retry-After 时长作为禁用期（封顶
// proxy429BanDuration），更贴合上游的配额恢复窗口；nil 时退回默认禁用期。
// 触发禁用时打 WARN 日志（此前冻结完全静默，难以确认熔断是否生效）。
func (s *Service) markProxy429(endpointID, proxy string, retryAfter *time.Duration) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		// 辅助请求（健康检测/验证/模型列表）也会累计 429，首次出现时补建状态。
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.rate429[proxy]++
	if state.rate429[proxy] >= proxy429BanThreshold {
		duration := proxy429BanDuration
		if retryAfter != nil && *retryAfter > 0 {
			if *retryAfter < duration {
				duration = *retryAfter
			}
		}
		state.rateLimited[proxy] = time.Now().Add(duration)
		delete(state.rate429, proxy)
		applog.Warn(context.Background(), "openai",
			"proxy frozen after repeated upstream 429s",
			"endpoint_id", endpointID,
			"proxy", hostFromProxyURL(proxy),
			"duration", duration.String(),
		)
		s.persistProxyState(endpointID, proxy, "rate_limited", state.rateLimited[proxy])
	}
}

// loadProxyState 启动时从 openai_proxy_state 表恢复代理池的持久化状态
// （429 冻结 / 连接失败冷却 / 坏代理沉淀）。只恢复尚未过期的记录；
// 过期记录在恢复时顺手清理，避免表无限增长。
// 幂等：重复调用只是再次把未过期状态写回内存（各 map 均为覆盖语义）。
func (s *Service) loadProxyState(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `
		SELECT endpoint_id, proxy, kind, until FROM openai_proxy_state`)
	if err != nil {
		return
	}
	defer rows.Close()
	now := time.Now()
	var stale [][3]string
	for rows.Next() {
		var endpointID, proxy, kind, untilRaw string
		if err := rows.Scan(&endpointID, &proxy, &kind, &untilRaw); err != nil {
			continue
		}
		until, err := time.Parse(time.RFC3339, untilRaw)
		if err != nil {
			continue
		}
		if !until.After(now) {
			stale = append(stale, [3]string{endpointID, proxy, kind})
			continue
		}
		s.proxyMu.Lock()
		state, ok := s.proxyStateByEndpoint[endpointID]
		if !ok {
			state = newEndpointProxyState()
			s.proxyStateByEndpoint[endpointID] = state
		}
		switch kind {
		case "rate_limited":
			state.rateLimited[proxy] = until
		case "cooldown":
			state.cooldown[proxy] = until
		case "sunk":
			state.sunk[proxy] = until
		}
		s.proxyMu.Unlock()
	}
	if len(stale) > 0 {
		for _, key := range stale {
			_, _ = db.ExecContext(ctx,
				"DELETE FROM openai_proxy_state WHERE endpoint_id=? AND proxy=? AND kind=?",
				key[0], key[1], key[2])
		}
	}
}

// proxyStateWriteDedup 是代理池状态持久化的写入去重表：
// 同一 (endpoint, proxy, kind) 在 proxyStateWriteDedupWindow 内只触发一次实际写库，
// 避免连接失败等高频事件把 DB 写入打爆（期间状态的最终值由补写时的 latest 决定，
// 慢一点覆盖没关系，只关心当前是否该恢复/清除）。
var proxyStateWriteDedup sync.Map

// proxyStateWriteWG 追踪代理池状态持久化的在途 goroutine，供测试在 TempDir
// 清理前等待落盘完成，避免 RemoveAll 竞态失败（Windows 下目录非空）。
var proxyStateWriteWG sync.WaitGroup

// proxyStateWriteDedupWindow 是同一键持久化去重的窗口时长。
const proxyStateWriteDedupWindow = 30 * time.Second

// persistProxyState 把代理池的一条运行时状态异步持久化到 openai_proxy_state：
// until 为零值时表示清除该条记录（代理已恢复）。
// 使用独立短连接与 goroutine，避免阻塞转发热路径；同一键的并发写由
// SQLite 的 UPSERT 语义自然收敛为最终值。写入带去重窗口，低频高频均安全。
func (s *Service) persistProxyState(endpointID, proxy, kind string, until time.Time) {
	if endpointID == "" || proxy == "" || kind == "" {
		return
	}
	key := endpointID + "\x00" + proxy + "\x00" + kind
	now := time.Now()
	if v, ok := proxyStateWriteDedup.Load(key); ok {
		if last, _ := v.(time.Time); now.Sub(last) < proxyStateWriteDedupWindow {
			return
		}
	}
	proxyStateWriteDedup.Store(key, now)
	proxyStateWriteWG.Add(1)
	go func() {
		defer proxyStateWriteWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := s.open(ctx)
		if err != nil {
			return
		}
		defer db.Close()
		if until.IsZero() {
			_, _ = db.ExecContext(ctx,
				"DELETE FROM openai_proxy_state WHERE endpoint_id=? AND proxy=? AND kind=?",
				endpointID, proxy, kind)
			return
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO openai_proxy_state(endpoint_id, proxy, kind, until, created_at)
			VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(endpoint_id, proxy, kind) DO UPDATE SET until=excluded.until`,
			endpointID, proxy, kind, until.UTC().Format(time.RFC3339))
	}()
}

// retryAfterFromHeader 解析上游响应的 Retry-After 头为时长。
// 仅支持秒数形式（RFC 7231 的 HTTP-date 形式较少见，且与配额窗口语义不符）；
// 头缺失或解析失败返回 nil。禁用期上限由调用方与 proxy429BanDuration 封顶。
func retryAfterFromHeader(resp *http.Response) *time.Duration {
	if resp == nil {
		return nil
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return nil
	}
	d := time.Duration(seconds) * time.Second
	return &d
}

// logProxyPoolFrozen 在全部出口因 429 冻结、回退直连时记录 WARN。
// 同一端点 10 分钟内只记一次，避免并发请求刷屏。调用方需持有 proxyMu。
func (s *Service) logProxyPoolFrozen(endpointID string, pool []string, now time.Time) {
	state, ok := s.proxyStateByEndpoint[endpointID]
	if ok && now.Sub(state.lastAllFrozenLog) < 10*time.Minute {
		return
	}
	if ok {
		state.lastAllFrozenLog = now
	}
	sample := ""
	if len(pool) > 0 {
		sample = hostFromProxyURL(pool[0])
	}
	applog.Warn(context.Background(), "openai",
		"proxy pool fully frozen by upstream 429s, falling back to direct connection",
		"endpoint_id", endpointID,
		"pool_size", len(pool),
		"sample_proxy", sample,
		"until", now.Add(proxy429BanDuration).Format(time.RFC3339),
	)
}

// autoUnfreezeAllLocked 在全部出口被禁用（429 冻结/坏代理沉淀）时自动解冻全体代理：
// 清除池内全部出口的冷却、429 冻结与沉淀状态，使池子重新可选。带节流：
// 距上次自动解冻不足 proxyAllFrozenRetryInterval 时不执行（返回 false，调用方回退直连）。
// 调用方需持有 proxyMu。
func (s *Service) autoUnfreezeAllLocked(endpointID string, pool []string, now time.Time) bool {
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return false
	}
	if !state.lastAllUnfrozen.IsZero() && now.Sub(state.lastAllUnfrozen) < proxyAllFrozenRetryInterval {
		return false
	}
	state.lastAllUnfrozen = now
	for _, proxy := range pool {
		delete(state.cooldown, proxy)
		delete(state.rateLimited, proxy)
		delete(state.rate429, proxy)
		delete(state.sunk, proxy)
		delete(state.failures, proxy)
		s.persistProxyState(endpointID, proxy, "cooldown", time.Time{})
		s.persistProxyState(endpointID, proxy, "rate_limited", time.Time{})
		s.persistProxyState(endpointID, proxy, "sunk", time.Time{})
	}
	applog.Warn(context.Background(), "openai",
		"proxy pool fully disabled, auto-unfroze all proxies",
		"endpoint_id", endpointID,
		"pool_size", len(pool),
	)
	return true
}

// proxyRateLimited 判断代理是否处于 429 累计触发的禁用期（禁用中不可被选中）。
func proxyRateLimited(state *endpointProxyState, proxy string, now time.Time) bool {
	until, banned := state.rateLimited[proxy]
	return banned && now.Before(until)
}

// pickKey 从端点全部 key 中按轮询选出一个 key，返回 (key, index)。
// key 永不冻结：triedKeys 记录本次请求内已尝试失败的 key，跳过它们避免无限重试；
// 全部 key 均已在本轮尝试失败时返回 ("", -1)，由调用方触发端点级切换。
// 429 绝不冻结 key，只靠轮询天然分散 RPM 压力。
func (s *Service) pickKey(endpointID string, keys []string, triedKeys map[string]bool) (string, int) {
	cleaned := cleanKeyList(keys)
	if len(cleaned) == 0 {
		return "", -1
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointKeyState()
		s.keyStateByEndpoint[endpointID] = state
	}
	if state.cursor < 0 || state.cursor >= len(cleaned) {
		state.cursor = 0
	}
	start := state.cursor
	for i := 0; i < len(cleaned); i++ {
		idx := (start + i) % len(cleaned)
		if triedKeys != nil && triedKeys[cleaned[idx]] {
			continue
		}
		state.cursor = (idx + 1) % len(cleaned)
		return cleaned[idx], idx
	}
	return "", -1
}

// cleanKeyList 清洗并去重 API Key 列表（保留顺序，剔除空串）。
func cleanKeyList(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
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
// 客户端断开（ctx 取消）时同样立即返回，避免断连后继续拉流浪费上游配额。
// 超时后遗留的读取 goroutine 会在上游数据到达或连接关闭后自行退出。
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
		return 0, ctx.Err()
	case <-time.After(idle):
		return 0, errStreamIdleTimeout
	}
}

var errStreamIdleTimeout = errors.New("upstream stream idle timeout")

// isRateLimitResponse 判断上游响应是否命中限流（429/439 或正文含限流关键词）。
// 注意：503/529 是上游过载/停机信号，不属于客户端限流，不应计入「连续 429 冻结
// 代理」的累计（否则瞬时过载会把一个健康代理冻结 30 分钟）；正文关键词仍能覆盖
// 携带过载语义的 503 响应。
func isRateLimitResponse(resp *http.Response, body []byte) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests, 439:
		return true
	}
	if len(body) > 0 {
		lower := strings.ToLower(string(body))
		for _, keyword := range []string{"rate limit", "rate_limit", "too many requests", "overloaded", "throttled"} {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}
	return false
}

// unavailableStatusCode 在所有候选端点均失败后，根据各端点失败码聚合决定返回给客户端的
// 状态码：全一致（如全部 429）→ 透传该码；不一致 → 503。供调用方在自己写响应时使用。
func unavailableStatusCode(model string, failCodes []int) int {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			return first
		}
	}
	return http.StatusServiceUnavailable
}

// writeRelayUnavailable 在所有候选端点均失败后，聚合各端点失败状态码决定返回给客户端的错误：
//   - 所有端点失败码一致（如全部 429）→ 透传该码，并说明网关无可用渠道。
//   - 失败码不一致或不在 4xx/5xx 内 → 返回 503 网关无可用渠道。
//
// 这类「所有渠道耗尽」属于网关自身状态，不额外写入调用日志（各尝试已在 relayLoop 内记录）。
func writeRelayUnavailable(w http.ResponseWriter, model string, failCodes []int) {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			msg := fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
			response.JSON(w, first, map[string]interface{}{
				"error": map[string]string{"message": msg, "type": "service_unavailable"},
			})
			return
		}
	}
	response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]string{
			"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
			"type":    "service_unavailable",
		},
	})
}

// isRetryableUpstreamResponse 判断上游响应是否值得切换到下一个代理重试：
// 限流（429/439/503/529 或限流关键词）与常见 5xx 服务器错误（500/502/504/599）。
// 501/505 等表示请求本身语义问题，重试无意义，不纳入。
func isRetryableUpstreamResponse(resp *http.Response, body []byte) bool {
	if isRateLimitResponse(resp, body) {
		return true
	}
	switch resp.StatusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout,
		http.StatusServiceUnavailable, 529, 599:
		return true
	}
	return false
}

// cleanHeaders 过滤掉空名称的条目，其余原样保留。
func cleanHeaders(headers []HeaderItem) []HeaderItem {
	out := make([]HeaderItem, 0, len(headers))
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		out = append(out, HeaderItem{Name: name, Value: h.Value})
	}
	return out
}

// isModelDisabled 判断给定模型是否在端点禁用的模型列表中。
func isModelDisabled(disabled []string, model string) bool {
	for _, m := range disabled {
		if m == model {
			return true
		}
	}
	return false
}

// decodeEndpointHeaders 从数据库的 headers JSON 列还原自定义请求头。
func decodeEndpointHeaders(raw sql.NullString) []HeaderItem {
	headers := []HeaderItem{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &headers)
	}
	return cleanHeaders(headers)
}

// decodeProxyPool 从数据库读取端点代理池（JSON 字符串数组）。
func decodeProxyPool(raw sql.NullString) []string {
	pool := []string{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &pool)
	}
	return cleanProxyPool(pool)
}

// applyCustomHeaders 把端点配置的自定义请求头写入待发请求。
// 网关自身的鉴权（Authorization 等）在调用方设置，自定义头允许覆盖非鉴权头。
func applyCustomHeaders(req *http.Request, headers []HeaderItem) {
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		req.Header.Set(name, h.Value)
	}
}

func (s *Service) getEndpointProxyStateRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	items := make([]proxyRuntimeStateItem, 0, len(pool))
	for _, proxy := range pool {
		item := proxyRuntimeStateItem{Proxy: proxy}
		if ok {
			item.Failures = state.failures[proxy]
			item.Rate429 = state.rate429[proxy]
			item.LastTTFB = state.lastTTFB[proxy]
			item.LastExitIP = state.lastExitIP[proxy]
			if probeAt, probed := state.lastProbeAt[proxy]; probed && !probeAt.IsZero() {
				item.LastProbeAt = probeAt.Format(time.RFC3339)
			}
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				item.CooldownUntil = until.Format(time.RFC3339)
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				item.RateLimitedUntil = until.Format(time.RFC3339)
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				item.SunkUntil = until.Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}
	s.proxyMu.Unlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": items})
}

// unbanEndpointProxies 一键解封端点代理池全部出口：清除冷却、429 冻结与坏代理沉淀
// 状态，使被临时/长期禁用的代理立即恢复可选。代理池的禁用都是运行时内存状态，
// 不修改配置，故解封无需写库。
func (s *Service) unbanEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	cleared := 0
	if ok {
		for _, proxy := range pool {
			clearedFrom := false
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				delete(state.cooldown, proxy)
				clearedFrom = true
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				delete(state.rateLimited, proxy)
				delete(state.rate429, proxy)
				clearedFrom = true
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				delete(state.sunk, proxy)
				delete(state.failures, proxy)
				clearedFrom = true
			}
			if clearedFrom {
				cleared++
				s.persistProxyState(id, proxy, "cooldown", time.Time{})
				s.persistProxyState(id, proxy, "rate_limited", time.Time{})
				s.persistProxyState(id, proxy, "sunk", time.Time{})
			}
		}
	}
	s.proxyMu.Unlock()

	applog.Info(ctx, "openai", "proxy pool unbanned",
		"endpoint_id", id,
		"cleared", cleared,
		"pool_size", len(pool),
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "cleared": cleared})
}

// probeEndpointProxies 立即对端点代理池全体出口做一次手动探活：
//  1. 经代理向端点 /models 发起请求，判定链路连通性（成功清冷却/沉淀，失败按
//     失败计数指数冷却且连续失败达阈值沉淀为坏代理）
//  2. 经代理访问 ipify 记录出口公网 IP
//
// 并发执行（上限 20），响应返回每个代理的探测结果（成功后记入运行时状态）。
// 用于前端「批量测试」：探活结果随后通过 /proxy-state 读取。
func (s *Service) probeEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)
	if len(pool) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": 0, "reachable": 0})
		return
	}

	sem := make(chan struct{}, 20)
	var probe sync.WaitGroup
	var okMu sync.Mutex
	reachable := 0
	for _, proxyURL := range pool {
		proxyURL := proxyURL
		probe.Add(1)
		go func() {
			defer probe.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s.probeEndpointProxyOnce(ctx, id, baseURL, apiKey, proxyURL) {
				okMu.Lock()
				reachable++
				okMu.Unlock()
			}
		}()
	}
	probe.Wait()

	applog.Info(ctx, "openai", "proxy pool manually probed",
		"endpoint_id", id,
		"pool_size", len(pool),
		"reachable", reachable,
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": len(pool), "reachable": reachable})
}

// probeEndpointProxyOnce 对单个代理做一次手动探活，返回是否链路可达。
// 链路可达：清冷却/沉淀，记录出口 IP；不可达：指数冷却 + 连续失败沉淀。
func (s *Service) probeEndpointProxyOnce(ctx context.Context, endpointID, baseURL, apiKey, proxyURL string) bool {
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return false
	}
	fullURL := strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/models"

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return false
	}
	if apiKey != "" && apiKey != "public" {
		req.Header.Set("Authorization", "Bearer "+secure.SecureDecrypt(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		s.markProxyFailed(endpointID, proxyURL)
		if s.proxyFailCount(endpointID, proxyURL) >= proxySinkThreshold {
			s.sinkProxy(endpointID, proxyURL)
		}
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	resp.Body.Close()
	if resp.StatusCode != 0 && resp.StatusCode >= 300 {
		// 上游 4xx/5xx：链路可达但上游拒绝；不沉淀代理（可能是 key/额度问题）。
		s.markProxySuccess(endpointID, proxyURL)
		s.unsinkProxy(endpointID, proxyURL)
		s.probeProxyExitIP(endpointID, proxyURL)
		return true
	}
	s.markProxySuccess(endpointID, proxyURL)
	s.unsinkProxy(endpointID, proxyURL)
	s.probeProxyExitIP(endpointID, proxyURL)
	return true
}
