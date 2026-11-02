package openai

import (
	"context"
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// sessionBindingMax 是单个端点代理状态中会话绑定条目的容量上限（对齐
// channelAffinity 的 1024）。session key 来自客户端可控的请求头/请求体
// （X-Session-ID 等），不做上限会让绑定 map 随会话数无限增长（内存泄漏/DoS）。
const sessionBindingMax = 1024

// sessionBindingTTL 是会话绑定条目的空闲过期时长：超过该时长未复用的绑定在
// 容量清理时被移除，避免僵尸会话永久占据绑定表。
const sessionBindingTTL = 30 * time.Minute

// clientForEndpoint 按端点代理池选择下一个可用代理，返回绑定该代理的 http.Client。
// 规则：
//   - proxyEnabled 关闭：忽略代理池，返回按端点 protocol 配置的直连客户端
//   - proxyPoolID 非空（引用独立代理池插件）：转发出口由插件选择（复用插件健康数据），
//     忽略内联 proxyPool；插件无可用代理时按 forceProxy 决定回退直连或报错
//   - proxyEnabled 开启且池为空：forceProxy 开启时报错（禁止直连），否则回退直连
//   - proxyEnabled 开启且有池：按池选择代理；非空 sessionKey 时优先复用
//     会话粘性绑定的代理（同一会话固定出口，请求数达 sessionProxyRequestLimit 后
//     主动轮换下一个出口，规避上游按出口 IP 的限额）
func (s *Service) clientForEndpoint(endpointID string, pool []string, proxyEnabled, forceProxy bool, sessionKey, protocol, proxyPoolID string) (*http.Client, string, error) {
	// 独立代理池：走插件选择器（不动内联 proxy_pool 逻辑与转发热路径）。
	if proxyPoolID != "" && s.externalPool != nil {
		proxyURL, selErr := s.externalPool.SelectProxy(context.Background(), proxyPoolID, sessionKey)
		if selErr != nil {
			return nil, "", fmt.Errorf("独立代理池选择失败: %w", selErr)
		}
		if proxyURL == "" {
			if forceProxy {
				return nil, "", fmt.Errorf("端点配置为强制走代理，但独立代理池无可用出口")
			}
			return s.client, "", nil
		}
		client, err := s.proxyClient(proxyURL)
		if err != nil {
			return s.client, proxyURL, err
		}
		return client, proxyURL, nil
	}
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
	trimSessionBindings(state, now)
	if state.cursor >= len(cleaned) || state.cursor < 0 {
		state.cursor = 0
	}

	// 会话粘性：绑定代理仍在池内、未冷却、未处于 429 禁用期、未被沉淀为坏代理、
	// 计数未达上限时直接复用，保持同一会话的出口 IP 稳定（配额感知轮换的前提）。
	if sessionKey != "" {
		if binding, bound := state.sessionBindings[sessionKey]; bound {
			bindingOK := false
			if !proxyGroupCooled(state, binding.proxy, now) {
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

	// 池级粘性：该端点最近一次成功转发的代理若仍可用，直接复用（会话粘性更高优先，
	// 已在上方处理）。一个出口有效就持续用，直到被冷却/冻结/沉淀才由下方候选逻辑
	// 自动换到下一个出口。
	if state.activeProxy != "" && activeProxyHonored(state, state.activeProxy, cleaned, now) {
		selectedProxy := state.activeProxy
		state.cursor = (indexOfProxy(selectedProxy, cleaned) + 1) % len(cleaned)
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

	// 可用代理：未在冷却中、且未处于 429 禁用期、未被沉淀为坏代理的代理集合。
	// 冷却与 429 禁用均按出口 IP 聚合：同一出口 IP 的任一个 slot 命中后，该 IP 的
	// 全部 slot 一起让出候选，让本请求的尝试预算花在不同的出口 IP 上（代理池换 IP
	// 规避上游按 IP 限流的核心价值）。
	candidates := []proxyCandidate{}
	for i := range cleaned {
		if proxyGroupCooled(state, cleaned[i], now) {
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

// externalPoolInUse 判断端点是否绑定独立代理池插件。
func (s *Service) externalPoolInUse(poolID string) bool {
	return poolID != "" && s.externalPool != nil
}

// reportExternalPoolResult 将一次出口使用结果反馈给独立代理池插件。
// ok=false 且 ratelimit 时 429 冻结；ok=false 非限流时冷却；ok=true 清除失败状态。
// 反馈失败仅记录日志，不阻塞转发。
func (s *Service) reportExternalPoolResult(poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) {
	if !s.externalPoolInUse(poolID) || proxy == "" {
		return
	}
	fbCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.externalPool.ReportResult(fbCtx, poolID, proxy, ok, ratelimit, retryAfter); err != nil {
		applog.Error(fbCtx, "openai", "report external proxy pool result failed", "poolID", poolID, "proxy", proxy, "error", err.Error())
	}
}

// externalPoolNextProxy 在独立代理池场景下：反馈 429 冻结当前出口后，重选池内下一个可用出口。
// 返回 "" 表示池内已无可用出口（调用方按出口耗尽收尾）。
func (s *Service) externalPoolNextProxy(ctx context.Context, poolID, currentProxy string, retryAfter *time.Duration) string {
	if !s.externalPoolInUse(poolID) || currentProxy == "" {
		return ""
	}
	s.reportExternalPoolResult(poolID, currentProxy, false, true, retryAfter)
	selCtx := ctx
	if selCtx == nil {
		selCtx = context.Background()
	}
	next, err := s.externalPool.SelectProxy(selCtx, poolID, "")
	if err != nil || next == "" || next == currentProxy {
		return ""
	}
	return next
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
// 再回退到请求体 user 字段，最后用首条 user 消息内容生成稳定种子（对齐
// opencode2api 的 conversationSeed：多轮对话的历史增长不改变首条消息，
// 同一会话的后续请求据此保持同一出口亲和，规避上游按出口 IP 的限额）。
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
	if seed := conversationSeedFromBody(parsedBody); seed != "" {
		return seed
	}
	return ""
}

// conversationSeedFromBody 用首条 user 消息的文字内容生成稳定会话种子：
// FNV-1a 摘要避免把用户消息全文作为会话键（日志/内存不落明文）。
// content 为 string 或 text block 数组（Claude 风格）时均可提取。
func conversationSeedFromBody(parsedBody map[string]interface{}) string {
	messages, ok := parsedBody["messages"].([]interface{})
	if !ok {
		return ""
	}
	for _, raw := range messages {
		m, ok := raw.(map[string]interface{})
		if !ok || m["role"] != "user" {
			continue
		}
		content := ""
		switch c := m["content"].(type) {
		case string:
			content = c
		case []interface{}:
			for _, part := range c {
				if pm, ok := part.(map[string]interface{}); ok && pm["type"] == "text" {
					if s, ok := pm["text"].(string); ok {
						content += s
					}
				}
			}
		}
		if strings.TrimSpace(content) != "" {
			h := fnv.New32a()
			_, _ = h.Write([]byte(content))
			return fmt.Sprintf("seed:%x", h.Sum32())
		}
	}
	return ""
}

// pickRandomAvailableProxy 从代理池中随机抽取一个「未在本请求试过、未冷却、
// 未被 429 冻结、未被沉淀」的出口，用于 429 后的随机换出口。返回空串表示
// 池内已无可用出口（全部试过/冷却/冻结）。exclude 之外的已试出口由调用方在
// triedProxies 中维护；currentProxy 用于把本次 429 出口也排除掉。
func (s *Service) pickRandomAvailableProxy(endpointID string, cleaned []string, triedProxies map[string]bool, currentProxy string) string {
	if len(cleaned) == 0 {
		return ""
	}
	now := time.Now()
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	avail := make([]string, 0, len(cleaned))
	for _, p := range cleaned {
		if triedProxies[p] {
			continue
		}
		if p == currentProxy {
			continue
		}
		if ok {
			if proxyGroupCooled(state, p, now) {
				continue
			}
			if proxyRateLimited(state, p, now) {
				continue
			}
			if until, sunk := state.sunk[p]; sunk && now.Before(until) {
				continue
			}
		}
		avail = append(avail, p)
	}
	if len(avail) == 0 {
		return ""
	}
	return avail[randIntN(len(avail))]
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
