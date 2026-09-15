package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// shouldNormalizeToolReasoningForCandidates 让任意候选命中推理厂商标识即启用
// 工具历史归一化（body 在候选循环前统一归一化，候选级模型映射不影响判定）。
func (s *Service) shouldNormalizeToolReasoningForCandidates(candidates []Endpoint, model string, body map[string]interface{}) bool {
	if requestEnablesReasoning(body) {
		return true
	}
	for _, cand := range candidates {
		candModel, _ := s.resolveEndpointModel(cand, model)
		if shouldNormalizeToolReasoning(candModel, cand.BaseURL, body) {
			return true
		}
	}
	return false
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

	admission := s.admitGatewayRequest(ctx, db, admissionRequest{
		Route:            "chat.completions",
		Model:            model,
		Stream:           stream,
		TargetEndpointID: targetEndpointID,
		SessionKey:       sessionKey,
		ClientIP:         clientIP,
		Started:          requestStarted,
	})
	if admission.Rejected {
		writeAdmissionRejection(w, admission)
		return
	}
	endpointCandidates := admission.Candidates
	selected := admission.Selected
	chosenIndex := admission.ChosenIndex
	viaProxy := admission.ViaProxy

	// 本地端点判断：只在前端填入的 base_url 上判定（首个候选），
	// 决定是否启用 /uploads/ 相对路径的本地图片内联。
	primaryURL := ensureVersionPath(selected.BaseURL)
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

	// 请求体归一化在循环前统一执行一次（reasoning_effort max→high、带 tool_calls
	// 的 assistant 历史补推理内容）。不能只在 failover 副本（k>0）里做：会话亲和
	// 会把某候选端点提升为首选（k=0），亲和路径的首选请求会拿到未归一化请求体
	// 而被枚举更窄的上游 400。high 属 OpenAI 标准枚举，对全部端点安全。
	normalizeReasoningEffort(parsedBody)
	if s.shouldNormalizeToolReasoningForCandidates(endpointCandidates, model, parsedBody) {
		normalizeChatToolReasoningHistory(parsedBody)
	}

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
	// rateLimitBudget 是「429 等待重试」的本请求剩余预算；任一候选开启等待重试
	// 时启用整请求预算。rateLimitRoundsUsed 统计已发生的等待重试轮次（预算兜底）。
	rateLimitBudget := time.Duration(0)
	if rateLimitRetryEnabledAny(endpointCandidates) {
		rateLimitBudget = rateLimitRetryBudget
	}
	rateLimitRoundsUsed := 0
	// failoverSteps 记录本轮请求逐个尝试过的端点与状态码，前端据此展示迁移趋势。
	var failoverSteps []map[string]interface{}
	// clientCancelled 标记本轮请求期间客户端已断开：断开后不再尝试其他候选，仅静默收尾。
	clientCancelled := false
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
			// 需要独立副本的情形：模型映射改写（写 model 字段）。归一化在循环前
			// 已对 parsedBody 统一执行（见调用处），副本不再承担归一化职责。
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

			// Gemini 上游：OpenAI chat 请求体 → Interactions API 请求体，
			// 目标地址指向 {base}/v1beta/interactions；Vertex 上游：→ generateContent
			// 请求体，地址指向 {base}/models/{model}:generateContent（或流式
			// :streamGenerateContent?alt=sse）；其余候选走 OpenAI 协议。
			var fullURL string
			var upstreamBodyBytes []byte
			if isGeminiUpstream(cand) {
				geminiBody, gErr := openAIChatToGemini(candBody)
				if gErr != nil {
					response.JSON(w, http.StatusBadRequest, map[string]string{"error": gErr.Error()})
					return
				}
				fullURL = geminiInteractionsURL(cand.BaseURL)
				upstreamBodyBytes, _ = json.Marshal(geminiBody)
			} else if isVertexUpstream(cand) {
				vertexBody, vErr := openAIChatToVertex(candBody)
				if vErr != nil {
					response.JSON(w, http.StatusBadRequest, map[string]string{"error": vErr.Error()})
					return
				}
				vertexModel, _ := candBody["model"].(string)
				if stream {
					fullURL = vertexGenerateURL(cand.BaseURL, vertexModel, true)
				} else {
					fullURL = vertexGenerateURL(cand.BaseURL, vertexModel, false)
				}
				upstreamBodyBytes, _ = json.Marshal(vertexBody)
			} else {
				fullURL = ensureVersionPath(cand.BaseURL)
				fullURL += "/chat/completions"
				upstreamBodyBytes, _ = json.Marshal(candBody)
			}
			res = s.relayLoop(relayLoopParams{
				route:          "chat.completions",
				ctx:            ctx,
				db:             db,
				selected:       cand,
				endpoints:      endpointCandidates,
				model:          model,
				realModel:      candModel,
				fullURL:        fullURL,
				body:           upstreamBodyBytes,
				stream:         stream,
				sessionKey:     sessionKey,
				clientIP:       clientIP,
				requestStarted: requestStarted,
			})
			// 记录该候选的尝试结果（端点名 + 状态码 + 最终使用的 key 序号），供前端展示迁移趋势。
			lastTried = &cand
			stepStatus := res.statusCode
			if stepStatus == 0 && res.resp != nil {
				stepStatus = res.resp.StatusCode
			}
			failoverSteps = append(failoverSteps, map[string]interface{}{"endpoint": cand.Name, "status": stepStatus, "keyIndex": res.stepKeyIndex})
			// 客户端已断开（点击停止）：不再尝试其他候选端点，静默收尾。
			if res.clientCancelled {
				clientCancelled = true
				break
			}
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
			// 任一候选端点开启「429 等待重试」且预算未耗尽：不立即 429 收尾，
			// 等待配额窗口（优先 Retry-After，缺省端点配置秒数）后重跑整轮候选。
			// 适用于 RPM 很低、429 后等数十秒配额才恢复的端点；预算耗尽仍走快速收尾。
			if rateLimitRetryEnabledAny(endpointCandidates) && rateLimitBudget > 0 && rateLimitRoundsUsed < rateLimitRetryRoundsCap {
				wait := rateLimitRetryWaitFor(res, endpointCandidates, rateLimitBudget)
				if wait > 0 {
					rateLimitBudget -= wait
					if !waitForRateLimitRetry(ctx, wait) {
						retryRoundCancelled = true
					} else {
						rateLimitRoundsUsed++
						lastRes = res
						// 重置轮次计数：等待重试注入额外轮次（普通 5xx 路径仍受
						// endpointRetryRounds 约束，不受此影响）。
						retryRound = -1
						continue
					}
					if retryRoundCancelled {
						break
					}
				}
			}
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
	// 客户端已断开（请求上下文被取消，如点击停止）：不聚合错误、不记录故障、不回写响应。
	if clientCancelled || (ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)) {
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
		}
		return
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
		}, res.realModel)
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
		// 上游若返回非 2xx（如 400 INVALID_ARGUMENT、403），不能作为 SSE 流直接写回
		// （否则客户端收到 400 + 空 SSE chunk）。应按普通错误响应读取错误 body，
		// 转换后以 application/json 写回。
		if res.resp.StatusCode >= 400 {
			errBytes, _ := readUpstreamBodyLimited(res.resp.Body)
			if len(res.firstChunk) > 0 {
				errBytes = append(res.firstChunk, errBytes...)
			}
			if isGeminiUpstream(selected) || isVertexUpstream(selected) {
				errBytes = geminiErrorToOpenAI(errBytes)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(res.resp.StatusCode)
			_, _ = w.Write(errBytes)
			return
		}

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

		emit := func(data []byte) {
			extendStreamDeadline()
			sw.write(data)
			tail = append(tail, data...)
			if len(tail) > usageTailLimit {
				tail = tail[len(tail)-usageTailLimit:]
			}
		}

		if isGeminiUpstream(selected) {
			// Gemini 上游：Interactions 流式 SSE → OpenAI chat.completions 流式 chunk。
			// firstChunk 作为 pending 前缀接入，保证跨块的部分行完整拼回。
			transformer := newGeminiInteractionSSETransformer(model)
			lr := newGeminiLineReader(res.resp.Body, res.firstChunk)
			for {
				line, lErr := lr.readLine(ctx, streamIdleTimeout)
				if lErr != nil {
					break
				}
				line = bytes.TrimSpace(line)
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				data := line[len("data: "):]
				if bytes.Equal(data, []byte("[DONE]")) {
					break
				}
				for _, c := range transformer.consume(data) {
					emit(c)
				}
			}
			for _, c := range transformer.finish() {
				emit(c)
			}
		} else if isVertexUpstream(selected) {
			// Vertex 上游：streamGenerateContent SSE（增量 chunk）→ OpenAI 流式 chunk。
			transformer := newVertexSSETransformer(model)
			lr := newGeminiLineReader(res.resp.Body, res.firstChunk)
			for {
				line, lErr := lr.readLine(ctx, streamIdleTimeout)
				if lErr != nil {
					break
				}
				line = bytes.TrimSpace(line)
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				data := line[len("data: "):]
				if bytes.Equal(data, []byte("[DONE]")) {
					break
				}
				for _, c := range transformer.consume(data) {
					emit(c)
				}
			}
			for _, c := range transformer.finish() {
				emit(c)
			}
		} else {
			// 首字等待阶段已读到的数据块，直接作为流式响应的首批内容写回。
			if res.firstWritten && len(res.firstChunk) > 0 {
				emit(res.firstChunk)
			}

			for {
				// 上游流中段停滞保护：idle 内无数据则终止流，防止请求无限挂死。
				n, err := readWithIdleTimeout(ctx, res.resp.Body, buf, streamIdleTimeout)
				if n > 0 {
					emit(buf[:n])
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
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo, res.realModel)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, readErr := readUpstreamBodyLimited(res.resp.Body)
		if readErr != nil {
			// 上游响应体读取失败/超限：按网关侧 502 回错，不把截断数据写回客户端。
			s.recordRelayError(RelayErrorRecord{
				Route: "chat.completions", Kind: "gateway",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, Proxy: hostFromProxyURL(res.lastProxy),
				ClientIP: clientIP, Attempts: res.attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     "read upstream body failed: " + readErr.Error(),
			})
			response.JSON(w, http.StatusBadGateway, map[string]string{"error": readErr.Error()})
			return
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		// Gemini / Vertex 上游：Interactions / generateContent 响应（JSON）→
		// OpenAI chat.completions 格式；错误响应也转换为 OpenAI 错误格式。
		// 统计按转换后的 OpenAI 结构解析。
		if isGeminiUpstream(selected) {
			if res.resp.StatusCode >= 400 {
				respBodyBytes = geminiErrorToOpenAI(respBodyBytes)
			} else {
				if conv, cErr := geminiToOpenAIChat(respBodyBytes, model); cErr == nil {
					respBodyBytes = conv
				}
			}
		} else if isVertexUpstream(selected) {
			if res.resp.StatusCode >= 400 {
				respBodyBytes = geminiErrorToOpenAI(respBodyBytes)
			} else {
				if conv, cErr := vertexToOpenAIChat(respBodyBytes, model); cErr == nil {
					respBodyBytes = conv
				}
			}
			// 安全拦截（prompt_blocked）：上游返回 200+空候选，语义上是对请求的
			// 拒绝，写回前提升为 400（对齐 new-api 的 prompt_blocked 语义）。
			if res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 && vertexIsBlockedResponse(respBodyBytes) {
				res.resp.StatusCode = http.StatusBadRequest
			}
		}

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
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.PromptTokens, usageInfo.Usage.CompletionTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.PromptTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo, res.realModel)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}
