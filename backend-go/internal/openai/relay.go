package openai

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) resolveEndpointModel(ep Endpoint, requested string) (string, bool) {
	first := ""
	for real, alias := range ep.ModelMappings {
		if alias != requested {
			continue
		}
		if first == "" {
			first = real
		}
		if !isModelDisabled(ep.DisabledModels, real) {
			return real, true
		}
	}
	if first != "" {
		return "", false
	}
	return requested, !isModelDisabled(ep.DisabledModels, requested)
}

// reasoningVendorHints 是「要求带工具调用的 assistant 历史回合携带推理内容」的
// 厂商标识。DeepSeek/Kimi/Moonshot/MiMo 等兼容端点的 thinking 模式下，历史回合
// 若只带 tool_calls 而没有 reasoning 内容（客户端丢弃了非标准字段），下一轮
// 请求会被上游 400 拒绝。命中方式：模型名或端点地址含这些标识，或请求显式开启
// 推理（对齐 opencode2api 的 normalizeToolReasoningHistory 兼容策略）。
var reasoningVendorHints = []string{"moonshot", "kimi", "deepseek", "mimo", "xiaomimimo"}

// toolReasoningPlaceholder 是给「只带工具调用、缺推理文本」的历史回合补的
// 推理占位文本（上游仅校验存在性，内容不解读）。
const toolReasoningPlaceholder = "tool call"

// requestEnablesReasoning 判断请求是否显式启用了推理（reasoning_effort /
// reasoning / thinking / effort 非 absent、非 none/disabled）。
func requestEnablesReasoning(body map[string]interface{}) bool {
	for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "effort"} {
		raw, exists := body[key]
		if !exists || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			mode := strings.ToLower(strings.TrimSpace(v))
			if mode != "" && mode != "none" && mode != "disabled" {
				return true
			}
		case bool:
			if v {
				return true
			}
		case map[string]interface{}:
			mode := ""
			if t, ok := v["type"].(string); ok {
				mode = t
			} else if e, ok := v["effort"].(string); ok {
				mode = e
			}
			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode == "none" || mode == "disabled" {
				continue
			}
			return true
		default:
			return true
		}
	}
	return false
}

// shouldNormalizeToolReasoning 判定请求是否需要做工具历史推理兼容：
// 模型名 / 端点地址命中 reasoningVendorHints，或请求显式启用了推理。
func shouldNormalizeToolReasoning(model, baseURL string, body map[string]interface{}) bool {
	haystack := strings.ToLower(model + " " + baseURL)
	for _, hint := range reasoningVendorHints {
		if strings.Contains(haystack, hint) {
			return true
		}
	}
	return requestEnablesReasoning(body)
}

// normalizeChatToolReasoningHistory 给所有「带 tool_calls 的 assistant 历史回合」
// 补齐 reasoning_content：客户端常丢弃该非标准字段却保留 tool_calls，使下一次
// thinking 模式请求在需要重放推理的厂商端点（DeepSeek/Kimi/MiMo 等）上被 400
// 拒绝。优先提升已有的 reasoning 字符串，否则用 toolReasoningPlaceholder。
func normalizeChatToolReasoningHistory(body map[string]interface{}) bool {
	messages, ok := body["messages"].([]interface{})
	if !ok {
		return false
	}
	changed := false
	for _, raw := range messages {
		m, ok := raw.(map[string]interface{})
		if !ok || m["role"] != "assistant" {
			continue
		}
		tools, ok := m["tool_calls"].([]interface{})
		if !ok || len(tools) == 0 {
			continue
		}
		if rc, ok := m["reasoning_content"].(string); ok && strings.TrimSpace(rc) != "" {
			continue
		}
		reasoning, _ := m["reasoning"].(string)
		if strings.TrimSpace(reasoning) == "" {
			reasoning = toolReasoningPlaceholder
		}
		m["reasoning_content"] = reasoning
		changed = true
	}
	return changed
}

// normalizeReasoningEffort 将 OpenAI 标准枚举之外的 reasoning_effort 值归一到
// 兼容值，避免 failover 到枚举更窄的上游（如部分仅接受 low/medium/high 的
// 服务）时被 400 拒绝。当前仅收敛 max -> high；其余值保持透传，最小侵入。
// 同时兼容 chat.completions（reasoning_effort 顶层字段）与 responses
// （reasoning.effort 嵌套字段）两种请求形态。
func normalizeReasoningEffort(body map[string]interface{}) {
	normalize := func(raw interface{}) interface{} {
		if s, ok := raw.(string); ok && s == "max" {
			return "high"
		}
		return raw
	}
	if raw, ok := body["reasoning_effort"]; ok {
		body["reasoning_effort"] = normalize(raw)
	}
	if reasoning, ok := body["reasoning"].(map[string]interface{}); ok {
		if raw, ok := reasoning["effort"]; ok {
			reasoning["effort"] = normalize(raw)
		}
	}
}

// recordRelayError 记录一次推理转发失败事件：写入内存环形缓冲（供 relay-errors 接口读取），
// 并按严重度输出结构化日志。Proxy 字段必须传脱敏后的 host:port，禁止传完整代理 URL，
// 避免把代理凭据写进日志文件或接口响应。
func (s *Service) recordRelayError(rec RelayErrorRecord) {
	rec.Time = time.Now().UTC()

	s.relayErrMu.Lock()
	s.relayErrors = append(s.relayErrors, rec)
	if len(s.relayErrors) > relayErrorBufferSize {
		s.relayErrors = s.relayErrors[len(s.relayErrors)-relayErrorBufferSize:]
	}
	s.relayErrMu.Unlock()

	logAttrs := []any{
		"route", rec.Route,
		"kind", rec.Kind,
		"endpoint", rec.Endpoint,
		"endpoint_id", rec.EndpointID,
		"key_index", rec.KeyIndex,
		"model", rec.Model,
		"stream", rec.Stream,
		"proxy", rec.Proxy,
		"client_ip", rec.ClientIP,
		"attempts", rec.Attempts,
		"elapsed_ms", rec.ElapsedMs,
		"err", rec.Error,
	}
	if rec.StatusCode > 0 {
		logAttrs = append(logAttrs, "upstream_status", rec.StatusCode)
	}
	if rec.Upstream != "" {
		logAttrs = append(logAttrs, "upstream_body", rec.Upstream)
	}
	switch rec.Kind {
	case "no_endpoint", "config", "gateway", "bad_gateway":
		applog.Error(context.Background(), "openai", "openai relay failed", logAttrs...)
	default:
		applog.Warn(context.Background(), "openai", "openai relay degraded", logAttrs...)
	}
}

// handleRelayErrors 返回最近发生的推理转发失败明细（最新在前），供管理界面与 AI 排障调用。
// limit 参数默认 50，上限与环形缓冲一致。
func (s *Service) handleRelayErrors(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= relayErrorBufferSize {
			limit = n
		}
	}

	s.relayErrMu.Lock()
	defer s.relayErrMu.Unlock()
	total := len(s.relayErrors)
	out := make([]RelayErrorRecord, 0, min(limit, total))
	for i := total - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.relayErrors[i])
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   total,
		"records": out,
	})
}

// truncateForLog 把任意字符串截断到指定字符数，避免上游错误体把日志或缓冲撑爆。
func truncateForLog(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + " ...(truncated)"
}

// errorResponseForLog 决定错误响应体（报错 JSON）是否写入调用日志：
// 仅当请求失败（状态码 >= 400）时返回截断后的报错 JSON，成功请求返回空串。
func errorResponseForLog(body []byte, statusCode int) string {
	if statusCode >= 200 && statusCode < 400 {
		return ""
	}
	return truncateForLog(string(body), relayErrorResponseLimit)
}

// gatewayBodyReadStatus 判定请求体读取失败的类型：MaxBytesReader 超限
// （*http.MaxBytesError）返回 413（客户端违约），其余读取失败返回 502。
func gatewayBodyReadStatus(err error) (int, string) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge, "body_too_large"
	}
	return http.StatusBadGateway, "gateway"
}

// trimErrorDetailRetention 清空超出保留上限（relayErrorResponseRetention）的错误详情：
// 只更新 error_kind/error_message/response_body 列，保留调用日志行（统计不受影响）。
// 最新记录按 timestamp DESC, id DESC 判定（同秒多记录时 id 越新越靠前）。
func (s *Service) trimErrorDetailRetention(ctx context.Context, db *sql.DB) {
	if _, err := db.ExecContext(ctx, `
		UPDATE openai_gateway_analytics
		SET error_kind = '', error_message = '', response_body = ''
		WHERE error_kind IS NOT NULL AND error_kind != ''
		  AND id NOT IN (
			SELECT id FROM (
				SELECT id FROM openai_gateway_analytics
				WHERE error_kind IS NOT NULL AND error_kind != ''
				ORDER BY timestamp DESC, id DESC
				LIMIT ?
			)
		  )
	`, relayErrorResponseRetention); err != nil {
		applog.Error(ctx, "openai", "Failed to trim error detail retention", "error", err.Error())
	}
}

func (s *Service) endpointHasModel(ep Endpoint, requested string) bool {
	for _, m := range ep.Models {
		if m == requested {
			return true
		}
	}
	for _, alias := range ep.ModelMappings {
		if alias == requested {
			return true
		}
	}
	return false
}

func effectiveProxyAttempts(ep Endpoint) int {
	if ep.AutoSwitch && ep.ProxyEnabled {
		if n := len(cleanProxyPool(ep.ProxyPool)); n > 0 {
			if n > proxyAttemptCap {
				return proxyAttemptCap
			}
			return n
		}
	}
	return 1
}

func cleanProxyPool(pool []string) []string {
	out := make([]string, 0, len(pool))
	seen := make(map[string]bool, len(pool))
	for _, raw := range pool {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	return out
}

// cleanProxyBatches 清洗代理批次：剔除无 ID / 无名称 / 无代理的批次，并清洗每条代理 URL。
func cleanProxyBatches(batches []ProxyBatch) []ProxyBatch {
	out := make([]ProxyBatch, 0, len(batches))
	for _, b := range batches {
		if strings.TrimSpace(b.ID) == "" || strings.TrimSpace(b.Name) == "" {
			continue
		}
		cleaned := cleanProxyPool(b.Proxies)
		if len(cleaned) == 0 {
			continue
		}
		b.Proxies = cleaned
		out = append(out, b)
	}
	return out
}

// mergeProxyPoolWithBatches 返回「手动代理 ∪ 全部批次代理」的去重并集，
// 保证运行时 proxy_pool 始终包含批次成员（客户端可能只提交其一）。
func mergeProxyPoolWithBatches(pool []string, batches []ProxyBatch) []string {
	merged := cleanProxyPool(pool)
	for _, b := range cleanProxyBatches(batches) {
		merged = cleanProxyPool(append(merged, b.Proxies...))
	}
	return merged
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// proxyCooldown 是一个代理被标记失败后的基准冷却间隔；连续失败按指数退避放大，
// 避免瞬时抖动把坏节点快速洗回池内。达到封顶后由预热探活（成功）恢复。
const (
	proxyCooldown      = 60 * time.Second
	proxyCooldownMax   = 30 * time.Minute
	proxyCooldownShift = 5
)

// proxy429Cooldown 是 429 出口的冷却时长：每次上游 429 都立即按出口 IP 组冻结
// 该出口 1 小时（opencode 免密钥、按出口 IP 限流：429 即该 IP 已被上游限死，
// 1 小时内不会再次被选为候选，随机换代理也不会抽回刚 429 过的 IP）。
const proxy429Cooldown = time.Hour

// proxyRateLimitPicks 是一请求内「不同出口 IP」连续 429 的提前收尾阈值。
// 首个 429 后从可用候选（未冷却/未沉淀/本请求未试过）中随机抽新出口继续尝试；
// 已有 5 个不同出口 IP 都返回 429，说明上游限流已扩散到整池，提前以 429 收尾，
// 交给端点级聚合判断（全部限流时快速返回，不再浪费尝试轮）。
const proxyRateLimitPicks = 5

// stickyTTFBMax 是池级粘性代理的延迟上限：成功转发且首字耗时低于该值才记录为
// 粘性出口（一个出口有效就持续用，直到下一个 429）。过慢的出口不值得粘住，
// 继续交给池内择优逻辑。
const stickyTTFBMax = 10 * time.Second

// proxyAllFrozenRetryInterval 是「全部出口禁用时自动解冻全体代理」的节流间隔：
// 距上次自动解冻不足该间隔时仍回退直连，避免上游 IP 级限流未恢复时反复
// 「解冻→又全部冻结→又解冻」打满全池的限流风暴。
const proxyAllFrozenRetryInterval = 10 * time.Minute

// proxyAttemptCap 是单次转发最多尝试的代理数量上限。代理池可容纳数千条
// （文件批量导入），但一次请求串行扫完整池会拖死请求：限流时每个出口都要
// 等一次完整往返。封顶后大池只轮询前 cap 个出口，配合 429 累计冻结，
// 被限死的出口会逐渐退出候选，剩余流量自动向健康出口集中。
// 与 firstTokenTimeout 配合构成单次转发最坏耗时预算：10s × cap，避免
// 池过大 / 出口过慢时把请求拖到客户端超时断开（回 502）。
const proxyAttemptCap = 8

// endpointVerifyTimeout 是端点保存时「验证 Key + 拉取模型列表」的总超时上限。
// 代理池很大或所选出口挂死时，验证请求不应把保存动作拖成「等超时」。
const endpointVerifyTimeout = 8 * time.Second

// endpointRetryRounds 是全部候选端点均失败后，网关在内部重试整轮候选的
// 最大次数。对齐 New API 的 RetryTimes 语义：让客户端保持等待状态，网关
// 内部有耐心地反复重试所有候选，期间上游可能恢复。
// 0 = 不重试（试完即返回），3 = 试完候选后等待 500ms 再试，最多 3 轮。
const endpointRetryRounds = 3

var endpointRetryDelay = 500 * time.Millisecond

// attemptHeaderTimeout 是单次转发在「仍有可切换出口」时的响应头等待上限。
// 可切换时，代理在期限内不返回响应头即视为该出口链路不可用，提前切下一个
// （非流式此前只能等 transport 的 ResponseHeaderTimeout=180s，一个挂死的
// 代理会把整条回退链拖住三分钟，「第一次访问等很久」的主要成因之一）。
// 不可切换的终局尝试仍放行到 180s，避免误杀「排队很久但最终成功」的上游。
var attemptHeaderTimeout = 20 * time.Second

// sessionProxyRequestLimit 是同一会话在同一个出口 IP 上的请求数上限：
// 达到上限后主动轮换到下一个代理。opencode 等上游按出口 IP 限额时，
// 提前轮换可避免被限额后再重试（限额前主动换 IP）。
// 用 var 而非 const 以便测试注入更小的值。
var sessionProxyRequestLimit = 50

func (s *Service) relayLoop(p relayLoopParams) *relayLoopResult {
	res := &relayLoopResult{
		statusCode: http.StatusBadGateway,
		egressIP:   s.egressOutbound(),
		startTime:  time.Now(),
	}
	ctx := p.ctx
	selected := p.selected
	stream := p.stream

	// 代理池选择 + 限流自动切换：最多尝试 len(pool) 个代理。
	// 代理开关未开启或池为空时只尝试一次（重试只是对同一链路的重复请求，
	// 首字超时重发反而放大慢响应，见 effectiveProxyAttempts）。
	maxProxyAttempts := effectiveProxyAttempts(selected)
	// 多 key 时保证每个 key 至少有一次尝试机会（覆盖 401 冻结后自动切 key 的场景）。
	if keyCount := len(cleanKeyList(selected.AllKeys())); keyCount > 1 && maxProxyAttempts < keyCount {
		maxProxyAttempts = keyCount
	}

	var resp *http.Response
	var lastErr error
	var attempt int
	// lastProxy 保存最终成功使用的代理（用于 TTFB 择优记录与日志）。
	lastProxy := ""
	// lastKeyIndex 保存最终成功后使用的 API Key 序号（用于日志 key pill）。
	lastKeyIndex := -1
	// firstChunk 保存流式首字等待阶段读到的首个数据块；无切换机会时首字在循环内读取。
	var firstChunk []byte
	var ttfbMs int64
	firstWritten := false
	// triedKeys 记录本轮请求内已尝试失败的 key（key 永不冻结，仅请求内去重），
	// 避免单 key 场景 401 后对同一 key 无限重试。
	triedKeys := map[string]bool{}
	// 不同出口 IP 的 429 计数：达到 proxyRateLimitPicks 视为上游限流已扩散到
	// 整池，提前收尾（单 IP 被限时组冻结已让候选自动跳到其他 IP，不在此计数）。
	observed429IPs := map[string]bool{}
	bump429 := func(proxy string) bool {
		// 优先用探测到的出口公网 IP 区分；未探测（冷启动）时退化为按 slot 计。
		key := s.proxyExitIPOf(selected.ID, proxy)
		if key == "" {
			key = proxy
		}
		observed429IPs[key] = true
		return len(observed429IPs) >= proxyRateLimitPicks
	}
	// triedProxies 记录本轮请求内已尝试过（含 429）的代理，随机换出口时绝不会
	// 重复抽到已试出口；retryProxy 由 429 分支随机选定后强制作为下一跳。
	triedProxies := map[string]bool{}
	retryProxy := ""

	for attempt = 0; attempt < maxProxyAttempts; attempt++ {
		// 客户端已断开（ctx 取消/超时）：立即结束尝试循环，不再发起新的
		// 网络连接。无显式检查时，连接失败路径虽也会因 attemptCtx 取消而快速
		// 返回，但在 clientForEndpoint 选择阶段仍可能空转；这里在每轮最前面
		// 提前终止，杜绝客户端断开后的无效重试（对应网关 502 的常见成因）。
		if err := ctx.Err(); err != nil {
			if lastErr == nil {
				lastErr = err
			}
			res.attempt = attempt
			break
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		res.cancel = cancel
		var client *http.Client
		var currentProxy string
		var clientErr error
		if retryProxy != "" {
			// 429 后的随机换出口：强制使用随机选定的下一跳，不再走择优（已试出口
			// 与已冻结出口都会被跳过，绝不再打同一个 IP）。
			client, clientErr = s.proxyClient(retryProxy)
			currentProxy = retryProxy
		} else {
			client, currentProxy, clientErr = s.clientForEndpoint(selected.ID, selected.ProxyPool, selected.ProxyEnabled, selected.ForceProxy, p.sessionKey, selected.Protocol)
		}
		if clientErr != nil {
			cancel()
			lastErr = clientErr
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "config",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     clientErr.Error(),
			})
			break
		}
		if currentProxy != "" {
			lastProxy = currentProxy
			res.egressIP = proxyEndpointAddr(currentProxy)
		}
		triedProxies[currentProxy] = true

		httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, p.fullURL, bytes.NewReader(p.body))
		if err != nil {
			cancel()
			res.statusCode = http.StatusInternalServerError
			res.lastErr = err
			res.attempt = attempt
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "gateway",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(p.requestStarted).Milliseconds(),
				Error:     "build upstream request failed: " + err.Error(),
			})
			errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
			s.recordAnalyticsKey(ctx, p.route, selected.ID, p.model, http.StatusInternalServerError, time.Since(p.requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), boolToInt(res.lastProxy != ""), p.clientIP, res.egressIP, res.lastKeyIndex, "", &AnalyticsError{
				Kind:     "gateway",
				Message:  "build upstream request failed: " + err.Error(),
				Response: errorResponseForLog(errBody, http.StatusInternalServerError),
			})
			return res
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		} else {
			httpReq.Header.Set("Accept", "application/json")
		}
		applyCustomHeaders(httpReq, selected.Headers)

		// 多 API Key 选择：轮询选一个 key（key 永不冻结，本轮已尝试失败的 key 会被跳过）。
		keys := selected.AllKeys()
		currentKey, currentKeyIndex := s.pickKey(selected.ID, keys, triedKeys)
		if currentKey == "" {
			// 本轮全部 key 均已尝试失败：本端点不可用，标记该端点后尝试下一个候选端点。
			cancel()
			s.markProxyFailed(selected.ID, currentProxy)
			res.endpointExhausted = true
			lastErr = fmt.Errorf("端点 %s 本轮全部 API Key 均尝试失败", selected.Name)
			lastProxy = currentProxy
			break
		}
		httpReq.Header.Set("Authorization", "Bearer "+currentKey)

		// 逐跳响应头等待上限：仍有可切换出口时，代理在 attemptHeaderTimeout 内
		// 不返回响应头即视为该出口链路不可用，立即切下一个，避免挂死代理拖住
		// 整条回退链（非流式 transport 的 ResponseHeaderTimeout 高达 180s）。
		canSwitch := selected.AutoSwitch && attempt+1 < maxProxyAttempts && currentProxy != ""
		if canSwitch {
			type doRes struct {
				resp *http.Response
				err  error
			}
			ch := make(chan doRes, 1)
			go func() {
				r, e := client.Do(httpReq)
				ch <- doRes{r, e}
			}()
			select {
			case d := <-ch:
				resp, lastErr = d.resp, d.err
			case <-time.After(attemptHeaderTimeout):
				cancel()
				// 吞掉晚到的 Do 结果：若其已成功打开响应体，ctx 取消会关闭连接。
				select {
				case d := <-ch:
					if d.resp != nil {
						d.resp.Body.Close()
					}
				default:
				}
				s.markProxyFailed(selected.ID, currentProxy)
				res.retryableUpstream = true
				lastErr = fmt.Errorf("上游响应头超时（超过 %s）", attemptHeaderTimeout)
				resp = nil
				if attempt+1 < maxProxyAttempts {
					continue
				}
				break
			}
		} else {
			resp, lastErr = client.Do(httpReq)
		}
		if lastErr != nil {
			// 连接失败（例如该代理不可用）：key 不冻结，只标记代理失败，若有池则切下一个。
			s.markProxyFailed(selected.ID, currentProxy)
			cancel()
			if currentProxy != "" && attempt+1 < maxProxyAttempts {
				continue
			}
			// 直连（代理池全部冻结回退）没有可切换的出口，重试只是重复打同一条链路；
			// 但端点级还有别的候选可用，连接层失败同样交给 failover 尝试下一个端点，
			// 而不是直接把 502 返回给客户端。
			res.retryableUpstream = true
			break
		}

		// 401/403 鉴权失败：key 本身失效，但不冻结；本轮请求内已尝试的 key 记入
		// triedKeys，避免无限重试，继续尝试下一个 key（或由 pickKey 耗尽后切换端点）。
		// 不消耗代理切换次数，因为 key 问题是凭据级非代理级。
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			triedKeys[currentKey] = true
			resp.Body.Close()
			cancel()
			continue
		}

		// 非流式：读取正文判断限流或 5xx，失败时在循环内重试。
		// 上游限流/5xx 不是代理的错：只切换出口重试，不惩罚代理（不冷却），
		// 避免上游故障污染整个代理池。但限流（429）会累计计数，
		// 达到阈值后临时禁用该代理（IP 级限流下继续选择只会反复 429）。
		if !stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts {
			bodyBytesRead, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			if readErr != nil || isRetryableUpstreamResponse(resp, bodyBytesRead) {
				is429 := isRateLimitResponse(resp, bodyBytesRead)
				if is429 {
					s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
					// 随机换出口：已试出口与组冻结出口都会跳过，绝不重复打同一 IP。
					if bump429(currentProxy) {
						// 5 个不同出口 IP 均 429：限流已扩散到整池，提前收尾
						// （保留已读正文原样交给统一判定记录，以 429 退出）。
						resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
						res.retryableUpstream = true
						lastErr = fmt.Errorf("上游按出口限流（连续 %d 个出口 429）", proxyRateLimitPicks)
						break
					}
					if currentProxy != "" {
						next := s.pickRandomAvailableProxy(selected.ID, cleanProxyPool(selected.ProxyPool), triedProxies, currentProxy)
						if next == "" {
							// 全部出口已试/已冻结：无可换出口，提前收尾。
							resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
							res.retryableUpstream = true
							lastErr = fmt.Errorf("上游按出口限流（出口已耗尽，%d 个出口均 429）", proxyRateLimitPicks)
							break
						}
						lastProxy = next
						res.egressIP = proxyEndpointAddr(next)
						retryProxy = next
						s.clearSessionBinding(selected.ID, p.sessionKey)
						continue
					}
				}
				s.clearSessionBinding(selected.ID, p.sessionKey)
				if currentProxy != "" {
					// 直连（池全部冻结回退）无处可切：保留已读正文原样返回给客户端。
					continue
				}
				res.retryableUpstream = true
				lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，重试耗尽）", resp.StatusCode)
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
				break
			}
			// 不是限流：重建带正文的响应继续处理。
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
			lastKeyIndex = currentKeyIndex
			break
		}

		// 流式：仅按状态码判断，限流或 5xx 时切换代理重试一次（429 立即组冻结出口）。
		if stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts && isRetryableUpstreamResponse(resp, nil) {
			is429 := isRateLimitResponse(resp, nil)
			if is429 {
				s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
			}
			resp.Body.Close()
			cancel()
			if is429 && currentProxy != "" {
				// 随机换出口：已试出口与组冻结出口都会跳过，绝不重复打同一 IP。
				if bump429(currentProxy) {
					// 5 个不同出口 IP 均 429：上游限流已扩散到整池，提前收尾。
					res.retryableUpstream = true
					lastErr = fmt.Errorf("上游按出口限流（连续 %d 个出口 429）", proxyRateLimitPicks)
					break
				}
				next := s.pickRandomAvailableProxy(selected.ID, cleanProxyPool(selected.ProxyPool), triedProxies, currentProxy)
				if next == "" {
					// 全部出口已试/已冻结：无可换出口，提前收尾。
					res.retryableUpstream = true
					lastErr = fmt.Errorf("上游按出口限流（出口已耗尽，%d 个出口均 429）", proxyRateLimitPicks)
					break
				}
				lastProxy = next
				res.egressIP = proxyEndpointAddr(next)
				retryProxy = next
				s.clearSessionBinding(selected.ID, p.sessionKey)
				continue
			}
			s.clearSessionBinding(selected.ID, p.sessionKey)
			if currentProxy != "" {
				// 非 429 的可重试错误（5xx/首字超时外的连接类）：交给择优换下一个出口。
				continue
			}
			// 直连（代理池全部冻结回退）无处可切：标记上游可重试错误，交给端点级 failover。
			res.retryableUpstream = true
			lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，重试耗尽）", resp.StatusCode)
			break
		}

		if stream {
			// 首字等待：若还有可切换的代理，则带超时等待首个字节，
			// 超时或上游提前断流时标记该代理失败并切换下一个。
			// 直连（池全部冻结回退）没有可切换出口，直接阻塞读首块。
			waitForFirst := selected.AutoSwitch && attempt+1 < maxProxyAttempts && currentProxy != ""
			if waitForFirst {
				type readRes struct {
					n   int
					err error
				}
				ch := make(chan readRes, 1)
				tmp := make([]byte, 4096)
				go func() {
					n, err := resp.Body.Read(tmp)
					ch <- readRes{n, err}
				}()
				var r readRes
				select {
				case r = <-ch:
				case <-time.After(firstTokenTimeout):
					cancel()
					resp.Body.Close()
					s.markProxyFailed(selected.ID, currentProxy)
					if currentProxy != "" && attempt+1 < maxProxyAttempts {
						continue
					}
					lastErr = fmt.Errorf("上游首字超时（超过 %s）", firstTokenTimeout)
					// 首字超时属上游问题：代理重试已耗尽，交给端点级 failover 尝试下一个候选端点。
					res.retryableUpstream = true
					break
				}
				if r.n > 0 {
					firstChunk = append([]byte(nil), tmp[:r.n]...)
					firstWritten = true
					ttfbMs = time.Since(res.startTime).Milliseconds()
					lastKeyIndex = currentKeyIndex
					break
				}
				cancel()
				resp.Body.Close()
				s.markProxyFailed(selected.ID, currentProxy)
				if currentProxy != "" && attempt+1 < maxProxyAttempts {
					continue
				}
				if lastErr == nil {
					lastErr = r.err
					if lastErr == nil {
						lastErr = io.EOF
					}
				}
				break
			}

			// 无切换机会：直接阻塞读首块，读取结果留给下方流式循环继续消费。
			// 但若上游返回的是限流/5xx 错误（非真正 SSE 数据），不应标记
			// firstWritten（否则末尾统一判定跳过，retryableUpstream 不被设置，
			// 导致 failover 循环直接把 429 透传）。
			tmp := make([]byte, 4096)
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				// 429/5xx 错误体不是 SSE 首块，不设 firstWritten。
				if isRetryableUpstreamResponse(resp, nil) {
					res.retryableUpstream = true
					if lastErr == nil {
						lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，无切换机会）", resp.StatusCode)
					}
					break
				}
				firstChunk = append([]byte(nil), tmp[:n]...)
				firstWritten = true
				ttfbMs = time.Since(res.startTime).Milliseconds()
				lastKeyIndex = currentKeyIndex
				break
			}
			cancel()
			lastErr = err
			if lastErr == nil {
				lastErr = io.EOF
			}
			break
		}
		break
	}

	res.resp = resp
	res.lastProxy = lastProxy
	res.lastKeyIndex = lastKeyIndex
	res.firstChunk = firstChunk
	res.firstWritten = firstWritten
	res.ttfbMs = ttfbMs
	res.attempt = attempt

	if lastErr != nil && resp == nil {
		res.lastErr = lastErr
		if !res.retryableUpstream {
			// 不可重试的终局失败（配置/网关侧错误）在此记录并直接返回。
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "bad_gateway",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(lastProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     lastErr.Error(),
			})
			errBody, _ := json.Marshal(map[string]string{"error": lastErr.Error()})
			s.recordAnalyticsKey(ctx, p.route, selected.ID, p.model, http.StatusBadGateway, time.Since(res.startTime).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), boolToInt(res.lastProxy != ""), p.clientIP, res.egressIP, res.lastKeyIndex, "", &AnalyticsError{
				Kind:     "bad_gateway",
				Message:  lastErr.Error(),
				Response: errorResponseForLog(errBody, http.StatusBadGateway),
			})
		} else {
			// 可重试失败（429/5xx/首字或响应头超时/连接耗尽）：循环内不逐次记日志，
			// 也不在此记账——端点级 failover 聚合会按「最终结果」记一条（含尝试次数
			// 与出口代理），保证整条回退链只落一条终局日志。
		}
		return res
	}
	// 防御：正常退出循环但未拿到响应（理论上只会在极端路径发生），兜底为 502，
	// 避免调用方对 nil resp / nil lastErr 做解引用。
	if resp == nil {
		res.lastErr = lastErr
		if res.lastErr == nil {
			res.lastErr = fmt.Errorf("上游转发未返回响应（重试耗尽）")
		}
		res.statusCode = http.StatusBadGateway
		res.endpointExhausted = true
		s.recordRelayError(RelayErrorRecord{
			Route: p.route, Kind: "bad_gateway",
			Endpoint: selected.Name, EndpointID: selected.ID,
			Model: p.model, Stream: stream, Proxy: hostFromProxyURL(lastProxy),
			ClientIP: p.clientIP, Attempts: attempt + 1,
			ElapsedMs: time.Since(res.startTime).Milliseconds(),
			Error:     res.lastErr.Error(),
		})
		return res
	}
	// 最后一次尝试（无重试机会）返回限流：同样累计计数，供 429 熔断使用。
	if resp != nil && isRateLimitResponse(resp, nil) {
		s.markProxy429(selected.ID, lastProxy, retryAfterFromHeader(resp))
	}
	// 统一判定「上游可重试错误」：无论是否启用 AutoSwitch / 是否有代理池，
	// 只要最终响应是限流或 5xx（且流式尚未写出首字节），都交给端点级 failover
	// 尝试下一个候选端点，尽最大可能提供可用渠道。成功（2xx）或客户端 4xx 不触发。
	// 若最后一次尝试的失败事件尚未写入明细（非流式循环内未逐次记录的最后一跳、
	// 或直连无切换机会），在此补齐一条，保证「最终导致失败的那一跳」也能排障追溯。
	if resp != nil && !firstWritten && isRetryableUpstreamResponse(resp, nil) {
		res.retryableUpstream = true
		if lastErr == nil {
			lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误）", resp.StatusCode)
		}
		// 不在此记账：失败按「最终结果」由端点级 failover 聚合为一条，
		// 保证整条回退链在 relay-errors 中只出现一次（Attempts 体现尝试次数）。
	}
	res.lastErr = lastErr
	// 单 key 健康统计：成功（2xx）清连续失败；发送了 key 但上游返回限流/错误不冻结，
	// 仅记录失败（供前端展示 key 健康状态）。401/403 记失败便于观察 key 是否失效。
	keys := cleanKeyList(selected.AllKeys())
	if resp != nil && lastKeyIndex >= 0 && lastKeyIndex < len(keys) {
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			s.markKeySuccess(selected.ID, keys[lastKeyIndex])
		} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			s.markKeyFailure(selected.ID, keys[lastKeyIndex], resp.StatusCode, "401/403 auth failed")
		}
	}
	_, _ = p.db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), selected.ID)
	res.statusCode = resp.StatusCode
	// 成功转发：记录池级粘性出口（健康且 TTFB<10s 的代理持续复用，直到被冷却/冻结才换）。
	if resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 400 && res.lastProxy != "" {
		stickyTTFB := res.ttfbMs
		if stickyTTFB <= 0 {
			stickyTTFB = time.Since(res.startTime).Milliseconds()
		}
		s.recordActiveProxy(selected.ID, res.lastProxy, stickyTTFB)
	}
	return res
}

// relayCancelOnCloseBody 在正文关闭时连带释放 attempt context，供正文由调用方
// 消费的入口（/v1/messages）使用：避免在正文未读完时提前 cancel 掐断响应。
type relayCancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (s *Service) proxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestStarted := time.Now()
	clientIP := s.resolveClientIP(r)
	// 请求体上限（小内存主机防瞬时尖峰）：超限经 MaxBytesReader 截断读取，
	// 由下方 err 分支返回 413，不会把超大 body 全量读入内存。
	r.Body = http.MaxBytesReader(w, r.Body, s.gatewayBodyLimitBytes())
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// 请求体超限（MaxBytesReader 截断）应是客户端违约，返回 413；
		// 其他读取失败（如客户端上传超时中断）是网关侧问题，用 502 表达。
		status, kind := gatewayBodyReadStatus(err)
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: kind,
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body read failed: " + err.Error(),
		})
		// 网关拦截（未到达上游）不写入调用日志。
		response.JSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		// 网关拦截（未到达上游）也写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		s.recordAnalyticsKey(ctx, "chat.completions", "", "", http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "bad_request",
			Message:  "request body is not valid JSON: " + err.Error(),
			Response: errorResponseForLog(errBody, http.StatusBadRequest),
		})
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model, _ := parsedBody["model"].(string)
	stream, _ := parsedBody["stream"].(bool)
	targetEndpointID := s.resolveTargetEndpoint(r)
	sessionKey := resolveSessionKey(r, parsedBody)

	db, err := s.open(ctx)
	if err != nil {
		// 网关侧数据库故障，未进入转发；不写入调用日志。
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	endpointCandidates, selected, chosenIndex, _, found := s.selectEndpointCandidates(ctx, db, model, targetEndpointID, sessionKey)
	if !found {
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "no_endpoint",
			Model: model, Stream: stream, ClientIP: clientIP,
			ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
		})
		// 候选池为空属网关自身状态，仍写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		s.recordAnalyticsKey(ctx, "chat.completions", "", model, http.StatusServiceUnavailable, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "no_endpoint",
			Message:  fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
			Response: errorResponseForLog(errBody, http.StatusServiceUnavailable),
		})
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		return
	}

	// 记录是否经由本端点配置的代理池出网。先于网关密钥限制等分支计算，
	// 使这些早退路径的调用日志也能正确标注代理。
	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	// 网关密钥限制：模型白名单 / 端点白名单 / token 配额。
	if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, keyIdentity, model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: "chat.completions", Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, ClientIP: clientIP,
				ElapsedMs: time.Since(requestStarted).Milliseconds(),
				Error:     limitErr,
			})
			errBody, _ := json.Marshal(map[string]interface{}{
				"error": map[string]string{"message": limitErr, "type": "forbidden"},
			})
			s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, http.StatusForbidden, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
				Kind:     "blocked",
				Message:  limitErr,
				Response: errorResponseForLog(errBody, http.StatusForbidden),
			})
			response.JSON(w, http.StatusForbidden, map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			return
		}
	}

	// 本地端点判断：只在前端填入的 base_url 上判定（首个候选），
	// 决定是否启用 /uploads/ 相对路径的本地图片内联。
	primaryURL := strings.TrimSuffix(selected.BaseURL, "/")
	if !strings.HasSuffix(strings.ToLower(primaryURL), "/v1") && !strings.Contains(strings.ToLower(primaryURL), "/v1/") {
		primaryURL += "/v1"
	}
	primaryURL += "/chat/completions"
	isLocal := localURLRegex.MatchString(primaryURL)

	if !isLocal {
		if messages, ok := parsedBody["messages"].([]interface{}); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					if contentArr, ok := msgMap["content"].([]interface{}); ok {
						for _, part := range contentArr {
							if partMap, ok := part.(map[string]interface{}); ok {
								if partMap["type"] == "image_url" {
									if imgURLMap, ok := partMap["image_url"].(map[string]interface{}); ok {
										s.inlineLocalUploadImage(imgURLMap, s.cfg.DataDir)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 归一化 Anthropic/Claude 风格的消息 content 数组为 OpenAI 标准格式。
	// PI / 部分 agent 客户端以 openai-completions 协议发请求时，assistant 历史
	// 消息的 content 可能是 content blocks 数组（[{type:"thinking",...},
	// {type:"text",...},{type:"toolCall",...}] 或 Claude 的 tool_use/tool_result），
	// 而 zen 等上游的 chat.completions 只接受字符串或 OpenAI 标准 parts。
	// 这里把 thinking block 提取为顶层 reasoning_content、text 合并为字符串、
	// toolCall/tool_use 转为标准 tool_calls，否则上游直接 400 break（见本地网关
	// 透传后 opencode.ai/zen 的 "Input should be a valid string" 错误）。
	normalizeChatContentBlocks(parsedBody)

	// 多端点 failover：按侧栏 sort_order 顺序逐个尝试候选端点。
	// 端点「不可用」时切换到下一个候选，保证单端点故障不影响可用性，包括：
	//   - endpointExhausted：本轮全部 API Key 尝试失败（key 问题）
	//   - retryableUpstream：上游 429/5xx/首字超时且代理重试耗尽（上游问题）
	// 2xx/4xx（含流式已写首字节）视为该端点已给出最终响应，直接 break。
	// 注意：模型名改写（将对外别名还原为真实模型名）必须在循环内对每个候选
	// 独立执行，因为各候选的 modelMappings 可能不同，复用同一 body 会导致
	// 错误的模型名被发送到不匹配的端点（如 opencode 的内部名发到日日新）。
	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	// failoverSteps 记录本轮请求逐个尝试过的端点与状态码，前端据此展示迁移趋势。
	var failoverSteps []map[string]interface{}
	// 从加权选中的端点起拼：让每一次请求的第一次尝试就是最优端点（会话亲和优先）。
	startIdx := s.failoverStartIndex(chosenIndex, endpointCandidates, sessionKey)
	// lastTried 记录最后一次真实转发的端点：整链失败时调用日志以此展示真实端点，
	// 而不是「unknown」（切换过程本身不落日志，只落最终结果）。
	var lastTried *Endpoint
	for retryRound := 0; retryRound <= endpointRetryRounds; retryRound++ {
		// 每轮独立收集失败码；上一轮的失败响应体需关闭，避免连接泄漏。
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
			lastRes = nil
		}
		failCodes = failCodes[:0]
		retryRoundCancelled := false
		candCount := len(endpointCandidates)
		for k := 0; k < candCount; k++ {
			ci := (startIdx + k) % candCount
			cand := endpointCandidates[ci]
			// 每个候选独立解析模型映射，避免加权选中的端点映射污染其他候选。
			candModel, _ := s.resolveEndpointModel(cand, model)
			// 需要独立副本的情形：模型映射改写（写 model 字段）或 failover
			// 候选归一化（写 reasoning_effort）。首个候选不复制、保持原样透传；
			// 后续候选复制后再归一化，避免把 max 这类非标准值发给枚举更窄的上游。
			candBody := parsedBody
			needCopy := k > 0 || (candModel != model && candModel != "")
			if needCopy {
				cp := make(map[string]interface{}, len(parsedBody))
				for k2, v := range parsedBody {
					cp[k2] = v
				}
				candBody = cp
			}
			if candModel != model && candModel != "" {
				candBody["model"] = candModel
			}
			if k > 0 {
				normalizeReasoningEffort(candBody)
				if shouldNormalizeToolReasoning(candModel, cand.BaseURL, candBody) {
					normalizeChatToolReasoningHistory(candBody)
				}
			}
			upstreamBodyBytes, _ := json.Marshal(candBody)

			fullURL := strings.TrimSuffix(cand.BaseURL, "/")
			if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
				fullURL += "/v1"
			}
			fullURL += "/chat/completions"
			res = s.relayLoop(relayLoopParams{
				route:          "chat.completions",
				ctx:            ctx,
				db:             db,
				selected:       cand,
				endpoints:      endpointCandidates,
				model:          model,
				fullURL:        fullURL,
				body:           upstreamBodyBytes,
				stream:         stream,
				sessionKey:     sessionKey,
				clientIP:       clientIP,
				requestStarted: requestStarted,
			})
			// 记录该候选的尝试结果（端点名 + 状态码），供前端展示迁移趋势。
			lastTried = &cand
			stepStatus := res.statusCode
			if stepStatus == 0 && res.resp != nil {
				stepStatus = res.resp.StatusCode
			}
			failoverSteps = append(failoverSteps, map[string]interface{}{"endpoint": cand.Name, "status": stepStatus})
			if res.resp != nil && !res.retryableUpstream && !res.endpointExhausted {
				selected = cand
				// 会话亲和：仅当上游返回 2xx/3xx（真正成功）时记录该会话最近使用的端点，
				// 后续同会话请求优先复用；4xx 客户端错误不记录，避免把会话钉死在
				// 无法服务该请求的端点上。
				if res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 {
					s.recordChannelAffinity(sessionKey, cand.ID)
				}
				retryRoundFinished = true
				break
			}
			// 端点不可用（key 耗尽或上游可重试错误）：收集失败码后尝试下一个候选端点。
			if res.statusCode > 0 {
				failCodes = append(failCodes, res.statusCode)
			}
			if k+1 < candCount {
				selected = endpointCandidates[(k+1)%candCount]
			}
		}
		if retryRoundFinished {
			break
		}
		// 限流风暴快速收尾：本轮全部候选都返回限流（429/439，而非 5xx 等瞬时故障）
		// 时，重试整轮只会继续打同一批被限流的出口，且每轮都串行吃掉端点×代理的
		// 全部耗时（此前单端点 8 代理 × 4 轮可拖到 30s+ 才把 429 还给客户端）。
		// 直接聚合返回，不再等待/重试；代理池内的短冷却会自行让被限流出口陆续
		// 退场，下个请求自然分散到健康出口。
		allRateLimited := len(failCodes) == candCount
		if allRateLimited {
			for _, c := range failCodes {
				if c != http.StatusTooManyRequests && c != 439 {
					allRateLimited = false
					break
				}
			}
		}
		if allRateLimited {
			break
		}
		// 全部候选均已失败（本轮）。继续下一轮前，等待间隔并检查客户端是否断开。
		lastRes = res
		if retryRound < endpointRetryRounds {
			select {
			case <-ctx.Done():
				retryRoundCancelled = true
			case <-time.After(endpointRetryDelay):
			}
		}
		if retryRoundCancelled {
			break
		}
	}
	// 全部候选端点均已失败（重试轮耗尽或客户端断开）：聚合错误决定返回给客户端的状态码。
	if len(endpointCandidates) > 0 && len(failCodes) == len(endpointCandidates) {
		failStatus := http.StatusServiceUnavailable
		allSame := true
		first := failCodes[0]
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		msg := fmt.Sprintf("网关无可用渠道（模型 %s）", model)
		if allSame && first >= 400 && first < 600 {
			failStatus = first
			msg = fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
		}
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{"message": msg, "type": "service_unavailable"},
		})
		// 整链失败：切换过程不落日志，这里按「最终结果」聚合为一条——
		// 端点取最后一次真实转发的候选（而非 unknown），模型与状态码齐备。
		lastEpID, lastEpName := "", ""
		if lastTried != nil {
			lastEpID = lastTried.ID
			lastEpName = lastTried.Name
		}
		attempts := 0
		lastProxy := ""
		if res != nil {
			attempts = res.attempt + 1
			lastProxy = hostFromProxyURL(res.lastProxy)
		}
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "failover",
			Endpoint: lastEpName, EndpointID: lastEpID, Model: model,
			Stream: stream, Proxy: lastProxy, ClientIP: clientIP,
			Attempts:   attempts,
			ElapsedMs:  time.Since(requestStarted).Milliseconds(),
			StatusCode: failStatus,
			Error:      msg,
		})
		s.recordAnalyticsKey(ctx, "chat.completions", lastEpID, model, failStatus, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "upstream",
			Message:  msg,
			Response: errorResponseForLog(errBody, failStatus),
		})
		writeRelayUnavailable(w, model, failCodes)
		return
	}
	if lastRes != nil && lastRes.resp != nil {
		_ = lastRes.resp.Body.Close()
	}
	if res.lastErr != nil && res.resp == nil {
		// 失败原因与统计已在 relayLoop 内记录，这里仅按状态码写回响应。
		if res.statusCode == http.StatusInternalServerError {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": res.lastErr.Error()})
		} else {
			response.JSON(w, res.statusCode, map[string]interface{}{"error": map[string]string{"message": res.lastErr.Error(), "type": "proxy_error"}})
		}
		return
	}
	// 正文处理完/关闭后再释放 attempt context（defer 逆序：先关 Body 再 cancel）。
	if res.cancel != nil {
		defer res.cancel()
	}
	defer res.resp.Body.Close()

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(res.resp.StatusCode)

		sw := newSSEStreamWriter(w)
		buf := make([]byte, 4096)
		tail := make([]byte, 0, usageTailLimit)

		// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
		extendStreamDeadline := func() {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		}

		// SSE ping 保活：上游长时间不吐流时向客户端发送注释行，穿透 NAT 空闲超时。
		stopPing := sw.startPing(ctx)
		defer stopPing()

		// 首字等待阶段已读到的数据块，直接作为流式响应的首批内容写回。
		if res.firstWritten && len(res.firstChunk) > 0 {
			extendStreamDeadline()
			sw.write(res.firstChunk)
			tail = append(tail, res.firstChunk...)
		}

		for {
			// 上游流中段停滞保护：idle 内无数据则终止流，防止请求无限挂死。
			n, err := readWithIdleTimeout(ctx, res.resp.Body, buf, streamIdleTimeout)
			if n > 0 {
				extendStreamDeadline()
				sw.write(buf[:n])
				tail = append(tail, buf[:n]...)
				if len(tail) > usageTailLimit {
					tail = tail[len(tail)-usageTailLimit:]
				}
			}
			if err != nil {
				break
			}
		}
		// 对齐 new-api：首字节后的流式中断也静默收尾，绝不向客户端报错。
		// 若上游未发送结束标记（[DONE]），补发收尾，保证前端对话正常结束。
		if !bytes.Contains(tail, []byte("[DONE]")) {
			extendStreamDeadline()
			sw.write([]byte("data: [DONE]\n\n"))
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		cachedTokens := 0

		accumulatedStr := string(tail)
		if matches := promptTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			promptTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := completionTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			completionTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := totalTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			totalTokens, _ = strconv.Atoi(matches[1])
		} else if promptTokens > 0 || completionTokens > 0 {
			totalTokens = promptTokens + completionTokens
		}
		if matches := cachedTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			cachedTokens, _ = strconv.Atoi(matches[1])
		}

		s.recordProxyTTFB(selected.ID, res.lastProxy, res.ttfbMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:    "upstream",
				Message: fmt.Sprintf("upstream returned HTTP %d (stream)", res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, _ := io.ReadAll(res.resp.Body)
		latencyMs := time.Since(res.startTime).Milliseconds()

		var usageInfo struct {
			Usage struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBodyBytes, &usageInfo)

		s.recordProxyTTFB(selected.ID, res.lastProxy, latencyMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:     "upstream",
				Message:  upstreamErrorMessage(respBodyBytes),
				Response: errorResponseForLog(respBodyBytes, res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.PromptTokens, usageInfo.Usage.CompletionTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.PromptTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}

// normalizeResponsesTools 为缺失 name 的工具补充 name（取值等于 type）。
// 上游 zen 用 serde flatten 解析 tools，要求每个工具都带顶层 name；
// OpenAI 官方的 web_search 等工具本身没有 name 字段，补齐避免反序列化失败。
func normalizeResponsesTools(body map[string]interface{}) {
	tools, ok := body["tools"].([]interface{})
	if !ok {
		return
	}
	for _, item := range tools {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := tool["name"]; has {
			continue
		}
		if t, ok := tool["type"].(string); ok && t != "" {
			tool["name"] = t
		}
	}
}

// normalizeResponsesInput 规范化 Responses 请求的 input 列表，兼容 zen 的转换缺陷：
//  1. assistant 消息的 content 数组（output_text 块）在 zen 转 chat 时不被识别，
//     需提取文本为字符串，否则报 "Invalid assistant message: content or tool_calls must be set"。
//  2. input 以 function_call_output 结尾时，zen 转成 chat 的 tool 消息后无后续 user，
//     报 "reasoning_content in the thinking mode must be passed back"，
//     末尾补一条空 user 消息即可通过。
//  3. 独立 function_call items 归并到相邻 assistant 消息的 tool_calls（chat 风格）。
//     zen 对独立 function_call item 的归并不稳定（同样的请求时而 200 时而 400
//     "An assistant message with 'tool_calls' must be followed by tool messages responding
//     to each 'tool_call_id'"），显式归并后可稳定通过。
//  4. assistant 已自带 tool_calls 但后续 function_call_output 不足时（codex 多轮
//     并行工具分步回传：历史 tool_calls 仍含全部 call_id，但部分工具结果尚未返回），
//     zen 转 chat 会报 "insufficient tool messages following tool_calls message"。
//     对未被任何 function_call_output 回应的 tool_call 做防御性剔除，让校验通过。
func normalizeResponsesInput(body map[string]interface{}) {
	input, ok := body["input"].([]interface{})
	if !ok {
		return
	}
	// responded 记录已被 function_call_output 回应的 call_id。归并时从独立
	// function_call item 取 call_id（call_id 优先，回退 id）；assistant 自带
	// tool_calls 的 call_id 也在最终校验阶段核对。
	responded := map[string]bool{}
	normalized := make([]interface{}, 0, len(input))
	var lastAssistant map[string]interface{}
	for _, item := range input {
		msg, ok := item.(map[string]interface{})
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		switch msg["type"] {
		case "function_call":
			// 归并到相邻 assistant 消息的 tool_calls，并丢弃独立 item。
			if lastAssistant == nil {
				// 防御：无前驱 assistant 时原样透传，避免静默丢弃。
				normalized = append(normalized, item)
				continue
			}
			name, _ := msg["name"].(string)
			args, _ := msg["arguments"].(string)
			callID, _ := msg["call_id"].(string)
			if callID == "" {
				callID, _ = msg["id"].(string)
			}
			if name != "" {
				toolCalls, _ := lastAssistant["tool_calls"].([]interface{})
				lastAssistant["tool_calls"] = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": args,
					},
				})
			}
			continue
		case "function_call_output":
			if callID, _ := msg["call_id"].(string); callID != "" {
				responded[callID] = true
			}
		}
		normalized = append(normalized, item)
		if msg["type"] == "message" {
			if role, _ := msg["role"].(string); role == "assistant" {
				lastAssistant = msg
			} else {
				lastAssistant = nil
			}
		}
	}

	// assistant 消息的 content 数组提取为字符串。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		var text strings.Builder
		hasText := false
		for _, part := range contentArr {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if partMap["type"] != "output_text" && partMap["type"] != "input_text" {
				continue
			}
			if t, ok := partMap["text"].(string); ok {
				if hasText {
					text.WriteString("\n")
				}
				text.WriteString(t)
				hasText = true
			}
		}
		if hasText {
			msg["content"] = text.String()
		}
	}

	// 防御性剔除：assistant 已声明但未被任何 function_call_output 回应的 tool_call
	// 会触发 zen 的 "insufficient tool messages following tool_calls message"。codex
	// 多轮并行工具分步回传时历史 tool_calls 含全部 call_id，但部分 output 尚未返回，
	// 这类未回应的调用本轮无法执行，剔除后既满足 zen 校验也不改变对话语义。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}
		kept := toolCalls[:0]
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				kept = append(kept, tc)
				continue
			}
			callID, _ := tcMap["id"].(string)
			if callID != "" && !responded[callID] {
				// 无对应 function_call_output：剔除。
				continue
			}
			kept = append(kept, tc)
		}
		if len(kept) == 0 {
			delete(msg, "tool_calls")
		} else {
			msg["tool_calls"] = kept
		}
	}

	// 末尾补齐：若最后一条是 function_call_output，追加空 user 消息。
	if len(normalized) > 0 {
		if last, ok := normalized[len(normalized)-1].(map[string]interface{}); ok {
			if t, _ := last["type"].(string); t == "function_call_output" {
				normalized = append(normalized, map[string]interface{}{
					"type":    "message",
					"role":    "user",
					"content": "",
				})
			}
		}
	}
	body["input"] = normalized
}

// normalizeChatContentBlocks 把 Anthropic/Claude 或 agent 客户端发送的 content
// blocks 数组归一化为 OpenAI chat.completions 标准格式。上游 zen 的 chat.completions
// 只接受 content 为字符串或 OpenAI 标准 parts，若传入含 {type:"thinking",
// signature:"reasoning_content"} / {type:"toolCall"} / {type:"tool_use"} 等块会直接
// 400。归一化规则：
//   - thinking / reasoning / redacted_thinking：提取 thinking 文本累积到消息顶层
//     reasoning_content，并丢弃该块（避免把 Anthropic signature 传给 zen）。
//   - toolCall / tool-call / tool_use block：转化为标准 tool_calls（id/type/function）。
//     arguments 优先（PI 用对象或字符串），其次 input（Anthropic 用结构化对象）。
//   - text：合并为 content 字符串。
//   - image / image_url：保留为 OpenAI 图片 parts。
//   - tool_result：随 keptParts 原样保留（对应消息已是 role=tool 时由 zen 直接处理）。
//
// 仅当 content 为非空数组且含可识别块时才改写；纯普通图片数组（image_url）不动。
func normalizeChatContentBlocks(body map[string]interface{}) {
	messages, ok := body["messages"].([]interface{})
	if !ok {
		return
	}
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		role, _ := msg["role"].(string)
		var text strings.Builder
		hasText := false
		var reasoning strings.Builder
		hasReasoning := false
		var toolCalls []interface{}
		var keptParts []interface{}
		needsRewrite := false
		for _, part := range contentArr {
			pm, ok := part.(map[string]interface{})
			if !ok {
				keptParts = append(keptParts, part)
				continue
			}
			ptype, _ := pm["type"].(string)
			switch ptype {
			case "text":
				if t, ok := pm["text"].(string); ok {
					if hasText {
						text.WriteString("\n")
					}
					text.WriteString(t)
					hasText = true
				}
				needsRewrite = true
			case "thinking", "reasoning", "redacted_thinking":
				if t := chatContentThinkingText(pm); t != "" {
					if hasReasoning {
						reasoning.WriteString("\n")
					}
					reasoning.WriteString(t)
					hasReasoning = true
				}
				// 丢弃 thinking 块，reasoning 转顶层字段。
				needsRewrite = true
			case "toolCall", "tool-call", "tool_use":
				name, _ := pm["name"].(string)
				callID, _ := pm["id"].(string)
				argsStr := chatContentToolArguments(pm)
				if name != "" {
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   callID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": argsStr,
						},
					})
				}
				needsRewrite = true
			case "image", "image_url":
				// 保持 OpenAI 图片 part 原样。
				keptParts = append(keptParts, part)
			default:
				keptParts = append(keptParts, part)
			}
		}

		if !needsRewrite {
			continue
		}

		var content interface{}
		switch {
		case hasText:
			// 文本合并为首个或唯一 part；若同时含图片/其余 part，则文本作为
			// ContentTextPart 后接其余 part，保证 zen 接受的 OpenAI parts 结构。
			var merged []interface{}
			if len(keptParts) == 0 {
				content = text.String()
			} else {
				merged = append(merged, map[string]interface{}{
					"type": "text",
					"text": text.String(),
				})
				content = append(merged, keptParts...)
			}
		case len(keptParts) > 0:
			content = keptParts
		default:
			if role == "assistant" && len(toolCalls) > 0 {
				content = ""
			} else {
				content = text.String()
			}
		}

		msg["content"] = content
		if hasReasoning {
			msg["reasoning_content"] = reasoning.String()
		}
		if len(toolCalls) > 0 && role == "assistant" {
			msg["tool_calls"] = toolCalls
		}
	}

	// zen 的 thinking 模式下，assistant 消息一旦在 tool 循环中开启思考，之后每轮
	// toolCall 轮次的 assistant 消息都必须携带 reasoning_content（可为空串），否则
	// 上游返回 400 "The `reasoning_content` in the thinking mode must be passed back
	// to the API"。PI 等 agent 客户端在多轮工具调用时可能漏发 thinking 块，这里做
	// 兜底：记录 thinking 是否已开启，对后续缺失 reasoning_content 的 assistant
	// toolCall 消息补空串，满足 zen 的连续传回要求。
	thinkingActive := false
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if _, hasRC := msg["reasoning_content"]; hasRC {
			thinkingActive = true
		}
		if role == "user" || role == "system" {
			// 新一轮用户请求重置思考状态：新的对话轮不要求续传上一轮 reasoning。
			thinkingActive = false
		}
		if thinkingActive && role == "assistant" {
			if _, hasRC := msg["reasoning_content"]; !hasRC {
				if _, hasTC := msg["tool_calls"]; hasTC {
					msg["reasoning_content"] = ""
				}
			}
		}
	}
}

// chatContentThinkingText 提取 thinking/reasoning block 中的文本。PI 用
// {type:"thinking", thinking, signature:"reasoning_content"}，部分 agent 用
// {type:"reasoning", text}；统一兼容 thinking/reasoning_content/text/content。
func chatContentThinkingText(pm map[string]interface{}) string {
	for _, key := range []string{"thinking", "reasoning_content", "text", "content"} {
		if t, ok := pm[key].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// chatContentToolArguments 提取 toolCall block 的参数并序列化为 JSON 字符串。
// PI 用 arguments（对象或字符串），Anthropic 用 input（结构化对象）。
func chatContentToolArguments(pm map[string]interface{}) string {
	if v, ok := pm["arguments"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	if v, ok := pm["input"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return "{}"
}

// sseDataJSON 提取 SSE 事件块中 data: 行的内容。
func sseDataJSON(block []byte) (string, bool) {
	for _, line := range strings.Split(string(block), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
		}
	}
	return "", false
}

// responsesStreamNormalizer 把上游精简的 Responses 流式事件补全为 Codex 可解析的标准事件序列。
// 部分上游（如 zen）直接发 output_text.delta / function_call_arguments.delta，缺少
// output_item.added（message 前导）与 output_item.done（完整 item）事件；Codex 在
// active item 缺失时收到文本 delta 会直接报错（OutputTextDelta without active item），
// 且工具调用的完整 arguments 只从 output_item.done 中读取，缺失会导致调用永远不执行。
// 网关在此补全：文本 delta 前注入 message item 的 added，item 切换与 completed 前注入 done。
type responsesStreamNormalizer struct {
	model       string
	respID      string
	createdSent bool
	msgOpen     bool
	msgID       string
	msgText     strings.Builder
	fnOpen      bool
	fnID        string
	fnName      string
	fnCallID    string
	fnArgs      strings.Builder
}

func newResponsesStreamNormalizer(model string) *responsesStreamNormalizer {
	return &responsesStreamNormalizer{model: model}
}

// sseEventType 解析 SSE 事件块中的 data JSON 的 type 字段。
func sseEventType(block []byte) (string, map[string]interface{}) {
	dataJSON, ok := sseDataJSON(block)
	if !ok {
		return "", nil
	}
	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &ev); err != nil {
		return "", nil
	}
	t, _ := ev["type"].(string)
	return t, ev
}

func sseEventBlock(eventType string, payload interface{}) []byte {
	payloadJSON, _ := json.Marshal(payload)
	out := append([]byte("event: "+eventType+"\ndata: "), payloadJSON...)
	return append(out, []byte("\n\n")...)
}

// transform 处理一个上游事件块，返回需要写出的一个或多个事件块。
func (n *responsesStreamNormalizer) transform(block []byte) [][]byte {
	eventType, ev := sseEventType(block)
	if eventType == "" {
		return [][]byte{block}
	}

	var outs [][]byte

	// 首事件前注入 response.created（若上游未发）。
	if !n.createdSent {
		n.createdSent = true
		if eventType != "response.created" {
			respID := n.respID
			if respID == "" {
				respID = uuid.NewString()
			}
			n.respID = respID
			outs = append(outs, sseEventBlock("response.created", map[string]interface{}{
				"type": "response.created",
				"response": map[string]interface{}{
					"id":         respID,
					"object":     "response",
					"created_at": time.Now().Unix(),
					"status":     "in_progress",
					"model":      n.model,
					"output":     []interface{}{},
					"usage":      nil,
				},
			}))
		}
	}

	switch eventType {
	case "response.created":
		if n.respID == "" {
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				if id, ok := resp["id"].(string); ok {
					n.respID = id
				}
			}
		}
	case "response.output_text.delta":
		// Codex 需要 message item 先建立（active item）才能挂文本 delta。
		if !n.msgOpen {
			n.msgOpen = true
			n.msgID = "msg_" + uuid.NewString()
			outs = append(outs, sseEventBlock("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": 0,
				"item": map[string]interface{}{
					"id":      n.msgID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []interface{}{},
				},
			}))
		}
		if delta, ok := ev["delta"].(string); ok {
			n.msgText.WriteString(delta)
		}
	case "response.output_item.added":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "message":
				n.msgOpen = true
				if id, ok := item["id"].(string); ok {
					n.msgID = id
				}
			case "function_call":
				// 切换 item 前先关闭未完成的 message 与上一个 function_call
				// （上游并行工具调用时会连续发多个 function_call 的 added，
				// 不关闭会导致参数拼进同一个 arguments 变成非法 JSON）。
				outs = append(outs, n.closeMessageIfOpen()...)
				outs = append(outs, n.closeFunctionIfOpen()...)
				n.fnOpen = true
				if id, ok := item["id"].(string); ok {
					n.fnID = id
				}
				if name, ok := item["name"].(string); ok {
					n.fnName = name
				}
				if callID, ok := item["call_id"].(string); ok {
					n.fnCallID = callID
				}
			}
		}
	case "response.function_call_arguments.delta":
		if delta, ok := ev["delta"].(string); ok {
			n.fnArgs.WriteString(delta)
		}
	case "response.output_item.done":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			if itemType, _ := item["type"].(string); itemType == "message" {
				n.msgOpen = false
				n.msgText.Reset()
			} else if itemType == "function_call" {
				n.fnOpen = false
				n.fnArgs.Reset()
			}
		}
	case "response.completed":
		outs = append(outs, n.closeFunctionIfOpen()...)
		outs = append(outs, n.closeMessageIfOpen()...)
	}

	outs = append(outs, block)
	return outs
}

// closeMessageIfOpen 关闭未完成的 message item（补齐 output_item.done）。
func (n *responsesStreamNormalizer) closeMessageIfOpen() [][]byte {
	if !n.msgOpen {
		return nil
	}
	n.msgOpen = false
	content := []interface{}{}
	if n.msgText.Len() > 0 {
		content = append(content, map[string]interface{}{
			"type": "output_text",
			"text": n.msgText.String(),
		})
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":      n.msgID,
			"type":    "message",
			"status":  "completed",
			"role":    "assistant",
			"content": content,
		},
	})
	n.msgText.Reset()
	return [][]byte{done}
}

// closeFunctionIfOpen 关闭未完成的 function_call item（补齐 output_item.done 与完整 arguments）。
func (n *responsesStreamNormalizer) closeFunctionIfOpen() [][]byte {
	if !n.fnOpen {
		return nil
	}
	n.fnOpen = false
	callID := n.fnCallID
	if callID == "" {
		callID = n.fnID
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":        n.fnID,
			"type":      "function_call",
			"status":    "completed",
			"name":      n.fnName,
			"arguments": n.fnArgs.String(),
			"call_id":   callID,
		},
	})
	n.fnArgs.Reset()
	return [][]byte{done}
}

// readSSEBlock 从流中读取一个完整 SSE 事件块（到空行结束，含结尾空行）。
func readSSEBlock(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				if buf.Len() > 0 {
					buf.WriteString(line)
					return buf.Bytes(), nil
				}
				if err != nil {
					return nil, err
				}
				continue
			}
			buf.WriteString(line)
		}
		if err != nil {
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
	}
}

// proxyResponses 代理 OpenAI Responses API（POST /v1/responses）。
// 请求体按不透明 JSON 透传（Responses 的 input/instructions 结构与 chat 不同，
// 网关不做改写），仅复用端点的模型路由、代理池与首字超时切换能力。
func (s *Service) proxyResponses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestStarted := time.Now()
	clientIP := s.resolveClientIP(r)
	// 请求体上限（小内存主机防瞬时尖峰）：超限经 MaxBytesReader 截断读取，
	// 由下方 err 分支返回 413，不会把超大 body 全量读入内存。
	r.Body = http.MaxBytesReader(w, r.Body, s.gatewayBodyLimitBytes())
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// 请求体超限（MaxBytesReader 截断）应是客户端违约，返回 413；
		// 其他读取失败（如客户端上传超时中断）是网关侧问题，用 502 表达。
		status, kind := gatewayBodyReadStatus(err)
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: kind,
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body read failed: " + err.Error(),
		})
		// 网关拦截（未到达上游）不写入调用日志。
		response.JSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		// 网关拦截（未到达上游）也写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		s.recordAnalyticsKey(ctx, "responses", "", "", http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "bad_request",
			Message:  "request body is not valid JSON: " + err.Error(),
			Response: errorResponseForLog(errBody, http.StatusBadRequest),
		})
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model, _ := parsedBody["model"].(string)
	stream, _ := parsedBody["stream"].(bool)
	targetEndpointID := s.resolveTargetEndpoint(r)
	sessionKey := resolveSessionKey(r, parsedBody)

	db, err := s.open(ctx)
	if err != nil {
		// 网关侧数据库故障，未进入转发；不写入调用日志。
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	endpointCandidates, selected, chosenIndex, _, found := s.selectEndpointCandidates(ctx, db, model, targetEndpointID, sessionKey)
	if !found {
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "no_endpoint",
			Model: model, Stream: stream, ClientIP: clientIP,
			ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
		})
		// 候选池为空属网关自身状态，仍写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		s.recordAnalyticsKey(ctx, "responses", "", model, http.StatusServiceUnavailable, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "no_endpoint",
			Message:  fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
			Response: errorResponseForLog(errBody, http.StatusServiceUnavailable),
		})
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		return
	}

	// 记录是否经由本端点配置的代理池出网。先于网关密钥限制等分支计算，
	// 使这些早退路径的调用日志也能正确标注代理。
	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	// 网关密钥限制：模型白名单 / 端点白名单 / token 配额。
	if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, keyIdentity, model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: "responses", Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, ClientIP: clientIP,
				ElapsedMs: time.Since(requestStarted).Milliseconds(),
				Error:     limitErr,
			})
			errBody, _ := json.Marshal(map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			s.recordAnalyticsKey(ctx, "responses", selected.ID, model, http.StatusForbidden, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
				Kind:     "blocked",
				Message:  limitErr,
				Response: errorResponseForLog(errBody, http.StatusForbidden),
			})
			response.JSON(w, http.StatusForbidden, map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			return
		}
	}

	// Responses API 的路径为 /responses。
	fullURL := strings.TrimSuffix(selected.BaseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/responses"

	// 若请求模型名是对外别名，转发到上游时还原为真实模型名。
	// 注意：必须在循环内对每个候选独立执行，因为各候选的 modelMappings 可能不同。
	normalizeResponsesTools(parsedBody)
	normalizeResponsesInput(parsedBody)

	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	var failoverSteps []map[string]interface{}
	// 从加权选中的端点起拼：让每一次请求的第一次尝试就是最优端点（会话亲和优先）。
	startIdx := s.failoverStartIndex(chosenIndex, endpointCandidates, sessionKey)
	// lastTried 记录最后一次真实转发的端点：整链失败时调用日志以此展示真实端点，
	// 而不是「unknown」（切换过程本身不落日志，只落最终结果）。
	var lastTried *Endpoint
	for retryRound := 0; retryRound <= endpointRetryRounds; retryRound++ {
		// 每轮独立收集失败码；上一轮的失败响应体需关闭，避免连接泄漏。
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
			lastRes = nil
		}
		failCodes = failCodes[:0]
		retryRoundCancelled := false
		candCount := len(endpointCandidates)
		for k := 0; k < candCount; k++ {
			ci := (startIdx + k) % candCount
			cand := endpointCandidates[ci]
			// 每个候选独立解析模型映射，避免加权选中的端点映射污染其他候选。
			candModel, _ := s.resolveEndpointModel(cand, model)
			// 需要独立副本的情形：模型映射改写（写 model 字段）或 failover
			// 候选归一化（写 reasoning.effort）。首个候选不复制、保持原样透传；
			// 后续候选复制后再归一化，避免把 max 这类非标准值发给枚举更窄的上游。
			candBody := parsedBody
			needCopy := k > 0 || (candModel != model && candModel != "")
			if needCopy {
				cp := make(map[string]interface{}, len(parsedBody))
				for k2, v := range parsedBody {
					cp[k2] = v
				}
				candBody = cp
			}
			if candModel != model && candModel != "" {
				candBody["model"] = candModel
			}
			if k > 0 {
				normalizeReasoningEffort(candBody)
				if shouldNormalizeToolReasoning(candModel, cand.BaseURL, candBody) {
					normalizeChatToolReasoningHistory(candBody)
				}
			}
			upstreamBodyBytes, _ := json.Marshal(candBody)

			fullURL := strings.TrimSuffix(cand.BaseURL, "/")
			if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
				fullURL += "/v1"
			}
			fullURL += "/responses"
			res = s.relayLoop(relayLoopParams{
				route:          "responses",
				ctx:            ctx,
				db:             db,
				selected:       cand,
				endpoints:      endpointCandidates,
				model:          model,
				fullURL:        fullURL,
				body:           upstreamBodyBytes,
				stream:         stream,
				sessionKey:     sessionKey,
				clientIP:       clientIP,
				requestStarted: requestStarted,
			})
			// 记录该候选的尝试结果（端点名 + 状态码），供前端展示迁移趋势。
			lastTried = &cand
			stepStatus := res.statusCode
			if stepStatus == 0 && res.resp != nil {
				stepStatus = res.resp.StatusCode
			}
			failoverSteps = append(failoverSteps, map[string]interface{}{"endpoint": cand.Name, "status": stepStatus})
			if res.resp != nil && !res.retryableUpstream && !res.endpointExhausted {
				selected = cand
				// 会话亲和：仅当上游返回 2xx/3xx（真正成功）时记录该会话最近使用的端点，
				// 4xx 客户端错误不记录，避免把会话钉死在无法服务该请求的端点上。
				if res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 {
					s.recordChannelAffinity(sessionKey, cand.ID)
				}
				retryRoundFinished = true
				break
			}
			// 端点不可用（key 耗尽或上游可重试错误）：收集失败码后尝试下一个候选端点。
			if res.statusCode > 0 {
				failCodes = append(failCodes, res.statusCode)
			}
			if k+1 < candCount {
				selected = endpointCandidates[k+1]
			}
		}
		if retryRoundFinished {
			break
		}
		// 限流风暴快速收尾：本轮全部候选都返回限流（429/439）时，重试整轮只会继续
		// 打同一批被限流的出口并串行吃掉全部耗时，直接聚合返回。
		allRateLimited := len(failCodes) == candCount
		if allRateLimited {
			for _, c := range failCodes {
				if c != http.StatusTooManyRequests && c != 439 {
					allRateLimited = false
					break
				}
			}
		}
		if allRateLimited {
			break
		}
		// 全部候选均已失败（本轮）。继续下一轮前，等待间隔并检查客户端是否断开。
		lastRes = res
		if retryRound < endpointRetryRounds {
			select {
			case <-ctx.Done():
				retryRoundCancelled = true
			case <-time.After(endpointRetryDelay):
			}
		}
		if retryRoundCancelled {
			break
		}
	}
	// 全部候选端点均已失败（重试轮耗尽或客户端断开）：聚合错误决定返回给客户端的状态码。
	if len(endpointCandidates) > 0 && len(failCodes) == len(endpointCandidates) {
		failStatus := http.StatusServiceUnavailable
		allSame := true
		first := failCodes[0]
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		msg := fmt.Sprintf("网关无可用渠道（模型 %s）", model)
		if allSame && first >= 400 && first < 600 {
			failStatus = first
			msg = fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
		}
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{"message": msg, "type": "service_unavailable"},
		})
		// 整链失败：切换过程不落日志，这里按「最终结果」聚合为一条，
		// 端点取最后一次真实转发的候选（而非 unknown），模型与状态码齐备。
		lastEpID, lastEpName := "", ""
		if lastTried != nil {
			lastEpID = lastTried.ID
			lastEpName = lastTried.Name
		}
		attempts := 0
		lastProxy := ""
		if res != nil {
			attempts = res.attempt + 1
			lastProxy = hostFromProxyURL(res.lastProxy)
		}
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "failover",
			Endpoint: lastEpName, EndpointID: lastEpID, Model: model,
			Stream: stream, Proxy: lastProxy, ClientIP: clientIP,
			Attempts:   attempts,
			ElapsedMs:  time.Since(requestStarted).Milliseconds(),
			StatusCode: failStatus,
			Error:      msg,
		})
		s.recordAnalyticsKey(ctx, "responses", lastEpID, model, failStatus, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "upstream",
			Message:  msg,
			Response: errorResponseForLog(errBody, failStatus),
		})
		writeRelayUnavailable(w, model, failCodes)
		return
	}
	if lastRes != nil && lastRes.resp != nil {
		_ = lastRes.resp.Body.Close()
	}
	if res.lastErr != nil && res.resp == nil {
		// 失败原因与统计已在 relayLoop 内记录，这里仅按状态码写回响应。
		if res.statusCode == http.StatusInternalServerError {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": res.lastErr.Error()})
		} else {
			response.JSON(w, res.statusCode, map[string]interface{}{"error": map[string]string{"message": res.lastErr.Error(), "type": "proxy_error"}})
		}
		return
	}
	// 正文处理完/关闭后再释放 attempt context（defer 逆序：先关 Body 再 cancel）。
	if res.cancel != nil {
		defer res.cancel()
	}
	defer res.resp.Body.Close()

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(res.resp.StatusCode)

		sw := newSSEStreamWriter(w)
		// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
		extendStreamDeadline := func() {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		}
		// SSE ping 保活：上游长时间不吐流时向客户端发送注释行，穿透 NAT 空闲超时。
		stopPing := sw.startPing(ctx)
		defer stopPing()
		// 部分上游（如 zen）的 Responses 流缺少 response.created / output_item 容器事件，
		// Codex 等 SDK 依赖它们初始化响应与挂载文本/工具参数，缺失会导致空白回。
		// 用状态机逐事件补全后转发。
		normalizer := newResponsesStreamNormalizer(model)
		streamReader := bufio.NewReader(res.resp.Body)
		if res.firstWritten && len(res.firstChunk) > 0 {
			streamReader = bufio.NewReader(io.MultiReader(bytes.NewReader(res.firstChunk), res.resp.Body))
		}
		// usage 信息总在最后的 response.completed 事件里，只保留流尾部即可，
		// 避免长对话把整个流式响应累积在内存中。
		tail := make([]byte, 0, usageTailLimit)
		for {
			block, readErr := readSSEBlock(streamReader)
			if len(block) > 0 {
				tail = append(tail, block...)
				if len(tail) > usageTailLimit {
					tail = tail[len(tail)-usageTailLimit:]
				}
				for _, out := range normalizer.transform(block) {
					extendStreamDeadline()
					sw.write(out)
				}
			}
			if readErr != nil {
				break
			}
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		// 从尾部 response.completed 事件解析 usage（Responses 用 input/output_tokens）。
		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		cachedTokens := 0
		accumulatedStr := string(tail)
		if matches := inputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			promptTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := outputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			completionTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := totalTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			totalTokens, _ = strconv.Atoi(matches[1])
		} else if promptTokens > 0 || completionTokens > 0 {
			totalTokens = promptTokens + completionTokens
		}
		if matches := cachedTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			cachedTokens, _ = strconv.Atoi(matches[1])
		}

		s.recordProxyTTFB(selected.ID, res.lastProxy, res.ttfbMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:    "upstream",
				Message: fmt.Sprintf("upstream returned HTTP %d (stream)", res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, _ := io.ReadAll(res.resp.Body)
		latencyMs := time.Since(res.startTime).Milliseconds()

		var usageInfo struct {
			Usage struct {
				InputTokens        int `json:"input_tokens"`
				OutputTokens       int `json:"output_tokens"`
				TotalTokens        int `json:"total_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBodyBytes, &usageInfo)

		s.recordProxyTTFB(selected.ID, res.lastProxy, latencyMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:     "upstream",
				Message:  upstreamErrorMessage(respBodyBytes),
				Response: errorResponseForLog(respBodyBytes, res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.InputTokens, usageInfo.Usage.OutputTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.InputTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}

func (s *Service) GetModelsList(ctx context.Context, anonymizeOwner bool) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, enabled, status, models, disabled_models, model_mappings FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]map[string]interface{})
	for rows.Next() {
		var name, status, modelsRaw string
		var enabledInt int
		var disabledRaw, mappingsRaw sql.NullString
		if err := rows.Scan(&name, &enabledInt, &status, &modelsRaw, &disabledRaw, &mappingsRaw); err == nil {
			var models []string
			if modelsRaw != "" {
				_ = json.Unmarshal([]byte(modelsRaw), &models)
			}
			disabled := []string{}
			if disabledRaw.Valid && disabledRaw.String != "" {
				_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
			}
			mappings := map[string]string{}
			if mappingsRaw.Valid && mappingsRaw.String != "" {
				_ = json.Unmarshal([]byte(mappingsRaw.String), &mappings)
			}
			for _, mID := range models {
				if isModelDisabled(disabled, mID) {
					continue
				}
				// 对外名称：存在映射时使用别名。
				externalID := mID
				if alias := mappings[mID]; alias != "" {
					externalID = alias
				}
				if _, ok := modelMap[externalID]; !ok {
					owner := name
					if anonymizeOwner {
						// 外部统一出口不泄漏内部端点名。
						owner = "api-monitor-gateway"
					}
					modelMap[externalID] = map[string]interface{}{
						"id":       externalID,
						"object":   "model",
						"created":  time.Now().Unix(),
						"owned_by": owner,
					}
				}
			}
		}
	}

	modelList := []map[string]interface{}{}
	for _, m := range modelMap {
		modelList = append(modelList, m)
	}
	return modelList, nil
}

func (s *Service) proxyModels(w http.ResponseWriter, r *http.Request) {
	// 外部统一出口（/v1）匿名化 owned_by，不泄漏内部端点名；
	// 管理面板入口（/api/openai）保留端点归属用于按端点筛选模型。
	anonymize := !strings.HasPrefix(r.URL.Path, "/api/openai")
	modelList, err := s.GetModelsList(r.Context(), anonymize)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 网关密钥白名单：不在白名单里的模型不暴露给该密钥。
	if keyIdentity := gatewayKeyFromContext(r.Context()); keyIdentity.ID != "" {
		modelList = filterModelsByKey(keyIdentity, modelList)
	}

	sort.Slice(modelList, func(i, j int) bool {
		idI, _ := modelList[i]["id"].(string)
		idJ, _ := modelList[j]["id"].(string)
		return idI < idJ
	})

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   modelList,
	})
}

// ==================== Helper methods ====================

func (s *Service) normalizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")

	stripSuffixes := []string{"/chat/completions", "/completions", "/models", "/embeddings"}
	for _, suffix := range stripSuffixes {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			u = u[:len(u)-len(suffix)]
			u = strings.TrimSuffix(u, "/")
		}
	}

	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}

	// Append version path if missing
	hasVersion := false
	if reg := versionPathRegex; reg.MatchString(u) {
		hasVersion = true
	}
	if !hasVersion {
		u += "/v1"
	}

	return u
}

// verifyAPIKeyRaw 校验上游 API Key（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（辅助请求也参与 429 熔断，避免半死出口无人标记）。
