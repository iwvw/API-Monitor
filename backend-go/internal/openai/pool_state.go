package openai

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

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

// recordActiveProxy 记录该端点最近一次成功转发的代理（池级粘性出口）。
// 不带会话 ID 的请求据此优先复用同一代理：一个出口有效就持续用，直到它被
// 冷却/429 冻结/沉淀（选择时自然跳过）才换下一个，减少每请求换 IP 带来的
// 冷启动与随机撞限。仅当成功且首字耗时低于 stickyTTFBMax（10s）时才记录：
// 过慢的出口不值得粘住，继续交给池内择优逻辑。
func (s *Service) recordActiveProxy(endpointID, proxy string, ttfbMs int64) {
	if proxy == "" {
		return
	}
	if ttfbMs <= 0 || time.Duration(ttfbMs)*time.Millisecond > stickyTTFBMax {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.activeProxy = proxy
}

// activeProxyHonored 返回池级粘性出口是否仍可复用：在池内、未冷却（含同 IP 组的
// 冷却）、未被 429 冻结（含同 IP 组冻结）、未被沉淀。调用方需持有 proxyMu。
func activeProxyHonored(state *endpointProxyState, activeProxy string, cleaned []string, now time.Time) bool {
	if activeProxy == "" {
		return false
	}
	if _, inPool := poolIndex(activeProxy, cleaned); !inPool {
		return false
	}
	if proxyGroupCooled(state, activeProxy, now) {
		return false
	}
	if proxyRateLimited(state, activeProxy, now) {
		return false
	}
	if until, sunk := state.sunk[activeProxy]; sunk && now.Before(until) {
		return false
	}
	return true
}

// trimSessionBindings 在会话绑定达到容量上限时做轻量清理：先剔除空闲过期的
// 条目，仍达上限则整体重建（低概率冷启动场景，与会话亲和的清理策略一致）。
// 调用方必须持有 proxyMu。
func trimSessionBindings(state *endpointProxyState, now time.Time) {
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
// 429 是上游按出口 IP 的限流，单次不惩罚代理的连接质量（不累计连接失败计数）。
// 每次 429 都立即按出口 IP 组冻结该出口 proxy429Cooldown（1 小时）：限流是把
// 该 IP 限死，不是偶发抖动，继续选择它只会反复 429；冻结期内随机换代理也不会
// 抽回该 IP。到期自动释放回池。
// retryAfter 非空且短于默认时长时优先采用上游给出的恢复窗口。
// rate429 保留累计计数仅用于前端展示（该出口被限流的次数），不再驱动禁用。
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
	duration := proxy429Cooldown
	if retryAfter != nil && *retryAfter > 0 && *retryAfter < duration {
		duration = *retryAfter
	}
	// 组聚合冻结：同一出口 IP 的所有 slot 一并冻结，随机换代理也不会再抽回该 IP。
	s.banProxyGroupLocked(endpointID, state, proxy, time.Now().Add(duration))
	applog.Warn(context.Background(), "openai",
		"proxy frozen after upstream 429",
		"endpoint_id", endpointID,
		"proxy", hostFromProxyURL(proxy),
		"exit_ip", state.lastExitIP[proxy],
		"duration", duration.String(),
	)
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
		"until", now.Add(proxy429Cooldown).Format(time.RFC3339),
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
// 按出口 IP 聚合：同一出口 IP 的任一个 slot 被长冻结后，该 IP 的全部 slot 都视为
// 禁用（被限死的 IP 不再占用候选位，把尝试预算留给健康 IP）。
func proxyRateLimited(state *endpointProxyState, proxy string, now time.Time) bool {
	if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
		return true
	}
	exitIP := state.lastExitIP[proxy]
	if exitIP == "" {
		return false
	}
	for p, ip := range state.lastExitIP {
		if ip != exitIP {
			continue
		}
		if until, banned := state.rateLimited[p]; banned && now.Before(until) {
			return true
		}
	}
	return false
}

// proxyGroupCooled 判断代理是否处于短冷却中。按出口 IP 聚合：同 IP 的任一个 slot
// 冷却即视为整组冷却（被限 IP 的槽位全部让出候选）；出口 IP 未知（尚未探测）时
// 只看当前 slot 自身。
func proxyGroupCooled(state *endpointProxyState, proxy string, now time.Time) bool {
	if until, cooled := state.cooldown[proxy]; cooled && !now.After(until) {
		return true
	}
	exitIP := state.lastExitIP[proxy]
	if exitIP == "" {
		return false
	}
	for p, ip := range state.lastExitIP {
		if ip != exitIP {
			continue
		}
		if until, cooled := state.cooldown[p]; cooled && !now.After(until) {
			return true
		}
	}
	return false
}

// coolProxyGroupLocked 给代理设置短冷却，并扩大到同一出口 IP 的全部 slot
// （由 lastExitIP 反查）。出口 IP 未知时退化为只冷却当前 slot。
// 调用方需持有 proxyMu。
func (s *Service) coolProxyGroupLocked(endpointID string, state *endpointProxyState, proxy string, until time.Time) {
	exitIP := state.lastExitIP[proxy]
	if exitIP == "" {
		state.cooldown[proxy] = until
		s.persistProxyState(endpointID, proxy, "cooldown", until)
		return
	}
	for p := range state.lastExitIP {
		if state.lastExitIP[p] != exitIP {
			continue
		}
		state.cooldown[p] = until
		s.persistProxyState(endpointID, p, "cooldown", until)
	}
	state.cooldown[proxy] = until
	s.persistProxyState(endpointID, proxy, "cooldown", until)
}

// banProxyGroupLocked 把代理长冻结（429 累计达阈值），并扩大到同一出口 IP 的
// 全部 slot，同时清零组内累计计数。出口 IP 未知时退化为只冻结当前 slot。
// 调用方需持有 proxyMu。
func (s *Service) banProxyGroupLocked(endpointID string, state *endpointProxyState, proxy string, until time.Time) {
	ban := func(p string) {
		state.rateLimited[p] = until
		// rate429 是展示用历史计数（该出口累计 429 次数），冻结不清零；
		// 仅自动解冻（autoUnfreezeAllLocked）时才整体复位。
		s.persistProxyState(endpointID, p, "rate_limited", until)
	}
	exitIP := state.lastExitIP[proxy]
	if exitIP == "" {
		ban(proxy)
		return
	}
	for p := range state.lastExitIP {
		if state.lastExitIP[p] != exitIP {
			continue
		}
		ban(p)
	}
	ban(proxy)
}

// proxyExitIPOf 返回代理最近探测到的出口公网 IP；未探测过返回空串。
func (s *Service) proxyExitIPOf(endpointID, proxy string) string {
	if proxy == "" {
		return ""
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		return state.lastExitIP[proxy]
	}
	return ""
}
