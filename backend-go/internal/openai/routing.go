package openai

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// channelAffinityTTL 是会话→端点亲和记录的存活时长。超过该时长后记录过期，
// 允许请求重新自由地选择端点（防止端点长期粘死导致上下文缓存漂移）。
const channelAffinityTTL = 15 * time.Minute

// channelAffinityEntry 记录某个会话键最近一次成功使用的端点。
type channelAffinityEntry struct {
	endpointID string
	updatedAt  time.Time
}

// recordChannelAffinity 记录会话键最近一次成功使用的端点。
// sessionKey 为空（无会话标识）时忽略；同一会话后续请求优先复用该端点。
func (s *Service) recordChannelAffinity(sessionKey, endpointID string) {
	if sessionKey == "" || endpointID == "" {
		return
	}
	now := time.Now()
	s.affinityMu.Lock()
	if len(s.channelAffinity) >= 1024 {
		// 防止无限增长：清空过期条目后仍超限则整体清空（低概率冷启动场景）。
		for k, v := range s.channelAffinity {
			if now.Sub(v.updatedAt) > channelAffinityTTL {
				delete(s.channelAffinity, k)
			}
		}
		if len(s.channelAffinity) >= 1024 {
			s.channelAffinity = make(map[string]channelAffinityEntry)
		}
	}
	s.channelAffinity[sessionKey] = channelAffinityEntry{endpointID: endpointID, updatedAt: now}
	s.affinityMu.Unlock()
}

// preferredAffinityEndpoint 返回会话键绑定的端点 ID（未过期时），否则返回空串。
func (s *Service) preferredAffinityEndpoint(sessionKey string) string {
	if sessionKey == "" {
		return ""
	}
	now := time.Now()
	s.affinityMu.Lock()
	defer s.affinityMu.Unlock()
	entry, ok := s.channelAffinity[sessionKey]
	if !ok || now.Sub(entry.updatedAt) > channelAffinityTTL {
		return ""
	}
	return entry.endpointID
}

// affinityEndpointIndex 从能力倒排索引中解析出会话偏好端点对应的下标（-1 表示无）。
// 供 selectEndpointCandidates 在候选端点上优先命中该端点。
func affinityEndpointIndex(endpointID string, candidates []Endpoint) int {
	if endpointID == "" {
		return -1
	}
	for i, ep := range candidates {
		if ep.ID == endpointID {
			return i
		}
	}
	return -1
}

// routeModelIndexes 从端点配置列表构建「模型名 → 候选端点下标」内存倒排索引。
// 键同时收录端点自身声明的模型与 modelMappings 的别名（对外可用的模型名）。
// 返回的映射为全新实例，不持有对端点切片的引用，调用方可直接缓存。
func buildRouteModelIndex(endpoints []Endpoint) map[string][]int {
	index := make(map[string][]int, 64)
	for i, ep := range endpoints {
		seen := map[string]bool{}
		for _, m := range ep.Models {
			m = trimModelName(m)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			index[m] = append(index[m], i)
		}
		for real, alias := range ep.ModelMappings {
			alias = trimModelName(alias)
			if alias == "" || seen[alias] {
				continue
			}
			seen[alias] = true
			index[alias] = append(index[alias], i)
			// 别名指向的真实模型也可被该端点服务（防止模型名写反的情况）。
			real = trimModelName(real)
			if real != "" && !seen[real] {
				seen[real] = true
				index[real] = append(index[real], i)
			}
		}
	}
	return index
}

// trimModelName 去除模型名首尾空白。
func trimModelName(m string) string {
	for len(m) > 0 && (m[0] == ' ' || m[0] == '\t' || m[0] == '\r' || m[0] == '\n') {
		m = m[1:]
	}
	for len(m) > 0 && (m[len(m)-1] == ' ' || m[len(m)-1] == '\t' || m[len(m)-1] == '\r' || m[len(m)-1] == '\n') {
		m = m[:len(m)-1]
	}
	return m
}

// endpointWeight 返回端点在候选选择中的加权因子：优先使用管理员配置的 weight，
// 未配置（默认 100）或非正值时退化为 1，避免配置缺失导致权重归零饿死。
// 优先级档位（priority 越大越优先）按每档 50 叠加进权重，让高优先级端点
// 在加权随机选路中显著占优（同时保留低优先级端点的后备可用性）。
func endpointWeight(ep Endpoint) int64 {
	w := int64(1)
	if ep.Weight > 0 {
		w = int64(ep.Weight)
	}
	if ep.Priority > 0 {
		w += int64(ep.Priority) * 50
	}
	return w
}

// weightedEndpointPickWeighted 在全部已知延迟的候选端点中按「延迟 + 权重」综合加权选择。
// 权重 = endpointWeight × (1 + (maxLatency-effectiveLatency)/200)。
// 同时兼顾管理员配置的 weight（越大越可能被选中）与延迟优劣（快的端点权重更高）。
func weightedEndpointPickWeighted(latencies []int64, known []bool, weights []int64) int {
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
	weighted := make([]int64, len(latencies))
	for i, latency := range latencies {
		effective := latency
		if !known[i] {
			effective = maxLatency
		}
		w := int64(1) + (maxLatency-effective)/200
		if w < 1 {
			w = 1
		}
		if weights != nil && i < len(weights) && weights[i] > 0 {
			w *= weights[i]
		}
		weighted[i] = w
		total += w
	}
	if total <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weighted {
		acc += w
		if n.Int64() < acc {
			return i
		}
	}
	return len(latencies) - 1
}

// keyHealthEntry 记录单 API Key 的独立健康状态。key 永不冻结，但保留
// 连续失败计数与最近失败原因/时间，供排障接口与前端展示。
type keyHealthEntry struct {
	failCount   int       // 连续失败次数（成功转发后清零）
	lastStatus  int       // 最近一次失败状态码（0=尚无失败）
	lastError   string    // 最近一次失败原因（脱敏）
	lastFailAt  time.Time // 最近一次失败时间
	lastSuccess time.Time // 最近一次成功转发时间
}

// keyHealthByEndpoint 返回指定端点全部 API Key 的健康快照（key → 健康记录）。
// 仅供排障接口展示，不影响请求路由（key 永不冻结）。
func (s *Service) keyHealthByEndpoint(endpointID string) map[string]keyHealthEntry {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		return nil
	}
	if len(state.health) == 0 {
		return nil
	}
	out := make(map[string]keyHealthEntry, len(state.health))
	for k, v := range state.health {
		out[k] = *v
	}
	return out
}

// markKeyFailure 记录某个 key 的一次失败（原因与状态码）。key 不冻结，
// 仅累计健康统计。任一 key 的失败不会影响其他 key 的可用性。
func (s *Service) markKeyFailure(endpointID, key string, status int, reason string) {
	if key == "" {
		return
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointKeyState()
		s.keyStateByEndpoint[endpointID] = state
	}
	if state.health == nil {
		state.health = make(map[string]*keyHealthEntry)
	}
	entry, ok := state.health[key]
	if !ok {
		entry = &keyHealthEntry{}
		state.health[key] = entry
	}
	entry.failCount++
	entry.lastStatus = status
	entry.lastError = reason
	entry.lastFailAt = time.Now()
}

// markKeySuccess 记录某个 key 的一次成功转发，清零连续失败计数。
func (s *Service) markKeySuccess(endpointID, key string) {
	if key == "" {
		return
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		return
	}
	if entry, ok := state.health[key]; ok {
		entry.failCount = 0
		entry.lastSuccess = time.Now()
	}
}

// proxySinkThreshold 是代理在探活/转发中被判定为「坏代理」的连续失败阈值。
// 达到后该代理进入长期沉淀（不参与选择），避免坏代理反复拖累转发。
const proxySinkThreshold = 3

// proxySinkDuration 是坏代理沉淀时长。沉淀期内不参与候选选择；到期自动放回，
// 让暂态故障（如上游维护）恢复后有机会重新加入。
const proxySinkDuration = 6 * time.Hour

// sinkProxy 将某端点下的代理标记为长期沉淀（坏代理）。沉淀仅影响选择，
// 不修改配置；代理 URL 只存脱敏 host:port 于日志。
func (s *Service) sinkProxy(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.sunk[proxy] = time.Now().Add(proxySinkDuration)
	// 沉淀即视为一次失败，同步累计失败计数便于前端展示。
	delete(state.failures, proxy)
	state.failures[proxy] = proxySinkThreshold
	delete(state.cooldown, proxy)
	s.persistProxyState(endpointID, proxy, "sunk", state.sunk[proxy])
	s.persistProxyState(endpointID, proxy, "cooldown", time.Time{})
	applog.Warn(context.Background(), "openai", "proxy sunk as bad", "endpoint_id", endpointID, "proxy", hostFromProxyURL(proxy), "duration", proxySinkDuration.String())
}

// unsinkProxy 清除代理的沉淀标记（探活/转发成功时调用），使其立即恢复可选。
func (s *Service) unsinkProxy(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		delete(state.sunk, proxy)
		delete(state.failures, proxy)
		s.persistProxyState(endpointID, proxy, "sunk", time.Time{})
	}
}

// proxySunk 判断代理是否处于沉淀期（沉淀中不可被选中）。
func (s *Service) proxySunk(endpointID, proxy string, now time.Time) bool {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return false
	}
	until, sunk := state.sunk[proxy]
	return sunk && now.Before(until)
}

// ssePingInterval 是流式响应期间的 SSE ping 保活间隔。上游长时间不吐流时，
// 定时向客户端写 `: ping\n\n` 注释行，穿透 NAT / TCP 空闲超时。
const ssePingInterval = 15 * time.Second

// sseStreamWriter 为流式写回加写锁，并周期性发送 SSE ping 注释行保活。
// SSE 注释行以 `:` 开头，会被标准 SSE 客户端忽略，安全且不污染数据流。
// ping 只在「流空闲超过一个间隔」且「当前处于事件边界」时才发送：避免上游
// 单条 data: 帧被分多次写回时，ping 注释被插进 JSON 中间导致客户端解析失败。
type sseStreamWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	// lastWrite 记录最后一次写数据的时刻；lastBoundary 表示最后一次写是否以
	// 换行结尾（SSE 事件边界）。两者在写锁保护下更新，ping goroutine 据此判断。
	lastWrite    time.Time
	lastBoundary bool
}

func newSSEStreamWriter(w http.ResponseWriter) *sseStreamWriter {
	sw := &sseStreamWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		sw.flusher = f
	}
	return sw
}

// write 在写锁保护下写入并刷新（若响应支持 flush）。同步更新空闲时间与事件边界。
func (sw *sseStreamWriter) write(data []byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	_, _ = sw.w.Write(data)
	sw.lastWrite = time.Now()
	sw.lastBoundary = len(data) > 0 && data[len(data)-1] == '\n'
	if sw.flusher != nil {
		sw.flusher.Flush()
	}
}

// startPing 启动 ping goroutine，返回停止函数。stop 后 goroutine 在下一次 tick 退出。
// 每次 tick 检查：距上次写数据已超过一个间隔（流空闲），且上一段数据结束在
// 事件边界（lastBoundary）或从未写过数据，才发送 ping，否则跳过本次 tick。
func (sw *sseStreamWriter) startPing(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(ssePingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				sw.mu.Lock()
				// 空闲：从未写过数据（首块前的保活）或距上次写已超过一个间隔。
				idle := sw.lastWrite.IsZero() || time.Since(sw.lastWrite) >= ssePingInterval
				// 边界：从未写过数据（无任何已发送内容）或上一段以换行结尾。
				boundary := sw.lastWrite.IsZero() || sw.lastBoundary
				if idle && boundary {
					_, _ = sw.w.Write([]byte(": ping\n\n"))
					sw.lastWrite = time.Now()
					if sw.flusher != nil {
						sw.flusher.Flush()
					}
				}
				sw.mu.Unlock()
			}
		}
	}()
	return func() { close(done) }
}
