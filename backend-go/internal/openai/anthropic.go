package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// anthropicModelToOpenAI 将 Anthropic 客户端常用的 claude 模型名映射为本网关的 OpenAI 模型名。
// 规则参考 DeepSeek Anthropic API 兼容层：claude-opus → deepseek-v4-pro，
// claude-haiku / claude-sonnet → deepseek-v4-flash。其余模型名原样返回，
// 由现有端点模型路由决定是否可用。
func anthropicModelToOpenAI(model string) string {
	switch {
	case strings.HasPrefix(model, "claude-opus"):
		return "deepseek-v4-pro"
	case strings.HasPrefix(model, "claude-haiku"), strings.HasPrefix(model, "claude-sonnet"):
		return "deepseek-v4-flash"
	default:
		return model
	}
}

// anthropicToOpenAI 将 Anthropic Messages API 请求体转换为 OpenAI chat.completions 请求体。
// 兼容细节参照 DeepSeek Anthropic API 兼容性表：
//   - system（字符串或数组）转为首条 system 消息；metadata.user_id 转为 user 字段
//   - content 字符串或 text 块透传；tool_use 块归并为 assistant 消息的 tool_calls；
//     tool_result 块拆出为独立的 role=tool 消息；image/thinking 等其他块忽略
//   - tools 由 {name, description, input_schema} 转为 function 工具
//   - tool_choice: auto/any/none/tool 映射为 OpenAI 的 auto/required/none/指定工具
//   - stop_sequences 转为 stop
func anthropicToOpenAI(body map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}

	model, _ := body["model"].(string)
	out["model"] = anthropicModelToOpenAI(model)

	switch v := body["max_tokens"].(type) {
	case float64:
		out["max_tokens"] = int(v)
	case int:
		out["max_tokens"] = v
	}
	if v, ok := body["temperature"].(float64); ok {
		out["temperature"] = v
	}
	if v, ok := body["top_p"].(float64); ok {
		out["top_p"] = v
	}
	if v, ok := body["stream"].(bool); ok {
		out["stream"] = v
	}
	if v, ok := body["stop_sequences"].([]interface{}); ok && len(v) > 0 {
		out["stop"] = v
	}
	if meta, ok := body["metadata"].(map[string]interface{}); ok {
		if uid, ok := meta["user_id"].(string); ok && uid != "" {
			out["user"] = uid
		}
	}

	// system → 首条 system 消息。
	var systemText string
	if sys, ok := body["system"]; ok {
		switch s := sys.(type) {
		case string:
			systemText = s
		case []interface{}:
			for _, part := range s {
				if pm, ok := part.(map[string]interface{}); ok && pm["type"] == "text" {
					if t, ok := pm["text"].(string); ok {
						systemText += t
					}
				}
			}
		}
	}

	var messages []interface{}
	if systemText != "" {
		messages = append(messages, map[string]interface{}{
			"role": "system", "content": systemText,
		})
	}

	// 消息转换：Anthropic tool_result 需要拆成独立 role=tool 消息，
	// 因此按顺序扫描，遇到 tool_result 块时生成 tool 消息。
	if rawMsgs, ok := body["messages"].([]interface{}); ok {
		for _, raw := range rawMsgs {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			content := msg["content"]

			var toolCalls []interface{}
			var textParts []string
			var toolResultParts []interface{}

			if strContent, ok := content.(string); ok {
				// 字符串 content：user/assistant 直接透传。
				if role == "user" || role == "assistant" {
					messages = append(messages, map[string]interface{}{
						"role": role, "content": strContent,
					})
				}
				continue
			}
			if contentArr, ok := content.([]interface{}); ok {
				for _, part := range contentArr {
					pm, ok := part.(map[string]interface{})
					if !ok {
						continue
					}
					switch pm["type"] {
					case "text":
						if t, ok := pm["text"].(string); ok && t != "" {
							textParts = append(textParts, t)
						}
					case "tool_use":
						id, _ := pm["id"].(string)
						name, _ := pm["name"].(string)
						input := pm["input"]
						argsBytes, err := json.Marshal(input)
						if err != nil {
							argsBytes = []byte("{}")
						}
						toolCalls = append(toolCalls, map[string]interface{}{
							"id":   id,
							"type": "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": string(argsBytes),
							},
						})
					case "tool_result":
						toolResultParts = append(toolResultParts, pm)
					}
				}
			}

			// 拼接 text 块的 helper。
			joinText := func() string {
				var b strings.Builder
				for _, tp := range textParts {
					b.WriteString(tp)
				}
				return b.String()
			}

			switch role {
			case "user":
				// user 数组消息：text 块拼接为字符串；image 等块被静默忽略。
				if len(textParts) > 0 {
					messages = append(messages, map[string]interface{}{
						"role": "user", "content": joinText(),
					})
				}
			case "assistant":
				// assistant 消息：文本与 tool_calls 合并为同一条（OpenAI 要求 tool 消息
				// 紧跟带 tool_calls 的 assistant，中间不能插入纯文本 assistant）。
				if len(textParts) > 0 || len(toolCalls) > 0 {
					msgOut := map[string]interface{}{"role": "assistant"}
					if len(textParts) > 0 {
						msgOut["content"] = joinText()
					}
					if len(toolCalls) > 0 {
						msgOut["tool_calls"] = toolCalls
					}
					messages = append(messages, msgOut)
				}
			}

			// tool_result：拆为 role=tool 消息。OpenAI 的 tool 消息 content 是字符串或数组。
			for _, tp := range toolResultParts {
				pm := tp.(map[string]interface{})
				toolUseID, _ := pm["tool_use_id"].(string)
				toolContent := "ok"
				if inner, ok := pm["content"]; ok {
					switch ic := inner.(type) {
					case string:
						toolContent = ic
					case []interface{}:
						var b strings.Builder
						for _, part := range ic {
							if ip, ok := part.(map[string]interface{}); ok && ip["type"] == "text" {
								if t, ok := ip["text"].(string); ok {
									b.WriteString(t)
								}
							}
						}
						toolContent = b.String()
					}
				}
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": toolUseID,
					"content":      toolContent,
				})
			}
		}
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages must not be empty")
	}
	out["messages"] = messages

	// tools: {name, description, input_schema} → function 风格。
	if rawTools, ok := body["tools"].([]interface{}); ok && len(rawTools) > 0 {
		var tools []interface{}
		for _, raw := range rawTools {
			tm, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			fn := map[string]interface{}{}
			if name, ok := tm["name"].(string); ok {
				fn["name"] = name
			}
			if desc, ok := tm["description"].(string); ok {
				fn["description"] = desc
			}
			if schema, ok := tm["input_schema"]; ok && schema != nil {
				fn["parameters"] = schema
			}
			tools = append(tools, map[string]interface{}{
				"type":     "function",
				"function": fn,
			})
		}
		if len(tools) > 0 {
			out["tools"] = tools
		}
	}

	// tool_choice 映射。
	if tc, ok := body["tool_choice"]; ok && tc != nil {
		switch choice := tc.(type) {
		case string:
			switch choice {
			case "auto":
				out["tool_choice"] = "auto"
			case "any":
				out["tool_choice"] = "required"
			case "none":
				out["tool_choice"] = "none"
			}
		case map[string]interface{}:
			if choice["type"] == "tool" {
				if name, ok := choice["name"].(string); ok {
					out["tool_choice"] = map[string]interface{}{
						"type":     "function",
						"function": map[string]interface{}{"name": name},
					}
				}
			}
		}
	}

	return out, nil
}

// openAIResponseToAnthropic 将 OpenAI chat.completions 非流式响应转换为 Anthropic Messages 响应。
func openAIResponseToAnthropic(oaiBody []byte, fallbackModel string) ([]byte, error) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(oaiBody, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in upstream response")
	}
	choice := resp.Choices[0]

	msg := map[string]interface{}{
		"id":            "msg_" + resp.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         resp.Model,
		"stop_reason":   anthropicStopReason(choice.FinishReason),
		"stop_sequence": nil,
		"content":       []interface{}{},
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}
	if msg["model"] == "" {
		msg["model"] = fallbackModel
	}

	var content []interface{}
	if choice.Message.Content != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": choice.Message.Content,
		})
	}
	for _, tc := range choice.Message.ToolCalls {
		var input interface{}
		var argsObj map[string]interface{}
		if json.Unmarshal([]byte(tc.Function.Arguments), &argsObj) == nil {
			input = argsObj
		} else {
			input = map[string]interface{}{}
		}
		content = append(content, map[string]interface{}{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": input,
		})
	}
	msg["content"] = content
	return json.Marshal(msg)
}

// anthropicStopReason 将 OpenAI finish_reason 映射为 Anthropic stop_reason。
func anthropicStopReason(finishReason string) string {
	switch finishReason {
	case "", "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// relayChatOpenAI 是 /v1/messages 通道的共享转发核心：对转换后的 OpenAI chat 请求体
// 执行端点路由、网关密钥限制与代理池重试，返回上游响应（body 未读取、由调用者处理）。
// 逻辑与 proxyChatCompletions 的转发循环保持一致，但不做图片本地化（Anthropic 不支持图片）。
// 统计路由固定为 "messages"。
func (s *Service) relayChatOpenAI(ctx context.Context, r *http.Request, bodyBytes []byte, model string, stream bool, clientIP string, requestStarted time.Time) (int, *http.Response, error) {
	route := "messages"

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: route, Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		s.RecordAnalytics(ctx, route, "", model, http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "")
		return http.StatusBadRequest, nil, fmt.Errorf("request body is not valid JSON: %v", err)
	}

	targetEndpointID := s.resolveTargetEndpoint(r)
	sessionKey := resolveSessionKey(r, parsedBody)

	db, err := s.open(ctx)
	if err != nil {
		s.RecordAnalytics(ctx, route, "", model, http.StatusInternalServerError, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "")
		return http.StatusInternalServerError, nil, err
	}
	defer db.Close()

	endpointCandidates, selected, _, _, found := s.selectEndpointCandidates(ctx, db, model, targetEndpointID)
	if !found {
		s.recordRelayError(RelayErrorRecord{
			Route: route, Kind: "no_endpoint",
			Model: model, Stream: stream, ClientIP: clientIP,
			ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
		})
		// 候选池为空属网关自身状态，不写入调用日志；直接在网关拦截并告知外部。
		return http.StatusServiceUnavailable, nil, fmt.Errorf("网关无可用渠道（模型 %s）", model)
	}

	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, keyIdentity, model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: route, Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, ClientIP: clientIP,
				ElapsedMs: time.Since(requestStarted).Milliseconds(),
				Error:     limitErr,
			})
			s.RecordAnalytics(ctx, route, selected.ID, model, http.StatusForbidden, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "")
			return http.StatusForbidden, nil, fmt.Errorf("%s", limitErr)
		}
	}

	// 若请求模型名是对外别名，转发到上游时还原为真实模型名。
	// 注意：必须在循环内对每个候选独立执行，因为各候选的 modelMappings 可能不同。

	// 正文由调用方读取：把 attempt context 的释放挂到 Body.Close 上，
	// 避免在正文未读完时提前 cancel 掐断响应（非流式且未启用 AutoSwitch 时
	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	for retryRound := 0; retryRound <= endpointRetryRounds; retryRound++ {
		// 每轮独立收集失败码；上一轮的失败响应体需关闭，避免连接泄漏。
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
			lastRes = nil
		}
		failCodes = failCodes[:0]
		retryRoundCancelled := false
		for ci, cand := range endpointCandidates {
			// 每个候选独立解析模型映射，避免加权选中的端点映射污染其他候选。
			candModel, _ := s.resolveEndpointModel(cand, model)
			candBody := parsedBody
			if candModel != model && candModel != "" {
				cp := make(map[string]interface{}, len(parsedBody))
				for k, v := range parsedBody {
					cp[k] = v
				}
				cp["model"] = candModel
				candBody = cp
			}
			upstreamBodyBytes, _ := json.Marshal(candBody)

			fullURL := strings.TrimSuffix(cand.BaseURL, "/")
			if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
				fullURL += "/v1"
			}
			fullURL += "/chat/completions"
			res = s.relayLoop(relayLoopParams{
				route:          route,
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
			if res.resp != nil && !res.retryableUpstream && !res.endpointExhausted {
				selected = cand
				retryRoundFinished = true
				break
			}
			// 端点不可用（key 耗尽或上游可重试错误）：收集失败码后尝试下一个候选端点。
			if res.statusCode > 0 {
				failCodes = append(failCodes, res.statusCode)
			}
			if ci+1 < len(endpointCandidates) {
				selected = endpointCandidates[ci+1]
				failReason := "上游转发失败"
				if res.lastErr != nil {
					failReason = res.lastErr.Error()
				}
				s.recordRelayError(RelayErrorRecord{
					Route: route, Kind: "endpoint_failover",
					Endpoint: cand.Name, EndpointID: cand.ID, Model: model,
					Stream: stream, ClientIP: clientIP,
					Attempts:  res.attempt + 1,
					ElapsedMs: time.Since(requestStarted).Milliseconds(),
					Error:     failReason,
				})
			}
		}
		if retryRoundFinished {
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
		return unavailableStatusCode(model, failCodes), nil, fmt.Errorf("网关无可用渠道（模型 %s）", model)
	}
	if lastRes != nil && lastRes.resp != nil {
		_ = lastRes.resp.Body.Close()
	}
	if res.lastErr != nil && res.resp == nil {
		return res.statusCode, nil, res.lastErr
	}
	res.resp.Body = &relayCancelOnCloseBody{ReadCloser: res.resp.Body, cancel: res.cancel}

	// 记录 TTFB：流式用首字耗时，非流式用整体耗时（与 chat.completions 一致，
	// 避免把 0 写入择优池导致该代理被误判为最优）。
	if res.ttfbMs > 0 || !stream {
		latency := res.ttfbMs
		if !stream {
			latency = time.Since(res.startTime).Milliseconds()
		}
		s.recordProxyTTFB(selected.ID, res.lastProxy, latency)
	}

	// 首字数据随响应体一并返回（流式首字等待阶段已读到的内容）。
	if res.firstWritten && len(res.firstChunk) > 0 {
		res.resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(res.firstChunk), res.resp.Body))
	}
	s.recordAnalyticsKey(ctx, route, selected.ID, model, res.resp.StatusCode, time.Since(res.startTime).Milliseconds(), res.ttfbMs, 0, 0, 0, 0, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex)
	return res.resp.StatusCode, res.resp, nil
}
func (s *Service) proxyAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestStarted := time.Now()
	clientIP := s.resolveClientIP(r)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "messages", Kind: "gateway",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body read failed: " + err.Error(),
		})
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var anthropicBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &anthropicBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "messages", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// 校验必填字段（Anthropic 格式）。
	model, _ := anthropicBody["model"].(string)
	if model == "" {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "model: field required")
		return
	}
	if _, ok := anthropicBody["messages"]; !ok {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "messages: field required")
		return
	}

	openAIBody, err := anthropicToOpenAI(anthropicBody)
	if err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	stream, _ := anthropicBody["stream"].(bool)
	upstreamModel, _ := openAIBody["model"].(string)
	if upstreamModel == "" {
		upstreamModel = model
	}

	// 复用 chat 转发管线，但统计路由标记为 messages。
	oaiBytes, _ := json.Marshal(openAIBody)
	statusCode, oaiResp, relayErr := s.relayChatOpenAI(ctx, r, oaiBytes, upstreamModel, stream, clientIP, requestStarted)
	if relayErr != nil {
		if statusCode == http.StatusServiceUnavailable {
			anthropicError(w, statusCode, "service_unavailable", relayErr.Error())
		} else {
			anthropicError(w, statusCode, "api_error", relayErr.Error())
		}
		return
	}
	defer oaiResp.Body.Close()

	if !stream {
		respBodyBytes, readErr := io.ReadAll(oaiResp.Body)
		if readErr != nil {
			anthropicError(w, http.StatusBadGateway, "api_error", readErr.Error())
			return
		}
		// 上游错误响应原样透传（如 429/5xx），格式转成 Anthropic 错误。
		if oaiResp.StatusCode >= 400 {
			anthropicError(w, oaiResp.StatusCode, "api_error", upstreamErrorMessage(respBodyBytes))
			return
		}
		converted, convErr := openAIResponseToAnthropic(respBodyBytes, upstreamModel)
		if convErr != nil {
			anthropicError(w, http.StatusBadGateway, "api_error", convErr.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(converted)
		return
	}

	// 流式：OpenAI SSE → Anthropic SSE。非 200 先判状态码，避免双写 header。
	if oaiResp.StatusCode != http.StatusOK {
		respBodyBytes, _ := io.ReadAll(oaiResp.Body)
		anthropicError(w, oaiResp.StatusCode, "api_error", upstreamErrorMessage(respBodyBytes))
		return
	}

	flusher, ok := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	extendDeadline := func() {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	}
	transformer := newAnthropicSSETransformer(upstreamModel)
	scanner := bufio.NewScanner(oaiResp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		events := transformer.consume([]byte(data))
		for _, ev := range events {
			extendDeadline()
			_, _ = w.Write(ev)
			if ok {
				flusher.Flush()
			}
		}
	}
	// 流结束收尾（usage 与 message_stop）。
	for _, ev := range transformer.finish() {
		extendDeadline()
		_, _ = w.Write(ev)
		if ok {
			flusher.Flush()
		}
	}
}

// upstreamErrorMessage 从上游 JSON 错误响应中提取 message。
func upstreamErrorMessage(body []byte) string {
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if errObj, ok := parsed["error"].(map[string]interface{}); ok {
			if m, ok := errObj["message"].(string); ok {
				return m
			}
		}
	}
	return string(body)
}

// anthropicError 以 Anthropic 风格错误格式写出响应。
func anthropicError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

// anthropicSSETransformer 将 OpenAI chat.completions 流式 chunk 转换为 Anthropic SSE 事件。
// 事件序列：message_start → content_block_start(text/tool_use) → content_block_delta
// → content_block_stop → message_delta(usage) → message_stop。并行工具调用按 OpenAI
// tool index 拆块，并通过 toolIndexToBlock 保持「OpenAI index → Anthropic block index」
// 的稳定映射，避免交错推送时 delta 标错块。
type anthropicSSETransformer struct {
	model            string
	messageID        string
	blockIndex       int
	textBlockIndex   int         // 文本块的 Anthropic index（-1 表示未开始）
	toolIndexToBlock map[int]int // OpenAI tool index → Anthropic block index
	inputJSONs       map[int]*strings.Builder
	started          bool
	finished         bool
	stopReason       string
	usage            *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
}

func newAnthropicSSETransformer(model string) *anthropicSSETransformer {
	return &anthropicSSETransformer{
		model:            model,
		messageID:        "msg_" + uuid.NewString(),
		textBlockIndex:   -1,
		toolIndexToBlock: map[int]int{},
		inputJSONs:       map[int]*strings.Builder{},
	}
}

func (t *anthropicSSETransformer) sse(eventType string, data interface{}) []byte {
	dataJSON, _ := json.Marshal(data)
	out := append([]byte("event: "+eventType+"\ndata: "), dataJSON...)
	return append(out, '\n', '\n')
}

// consume 处理一个 OpenAI chunk，返回需要写出的 Anthropic SSE 事件块。
func (t *anthropicSSETransformer) consume(chunk []byte) [][]byte {
	if t.finished {
		return nil
	}
	var payload struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chunk, &payload); err != nil {
		return nil
	}

	var events [][]byte

	if !t.started {
		t.started = true
		events = append(events, t.sse("message_start", map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            t.messageID,
				"type":          "message",
				"role":          "assistant",
				"model":         t.model,
				"content":       []interface{}{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]interface{}{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}))
	}

	if len(payload.Choices) == 0 {
		if payload.Usage != nil {
			t.usage = payload.Usage
		}
		return events
	}
	choice := payload.Choices[0]

	// 文本增量（块索引固定为 textBlockIndex，先于工具块或与工具块交错均不串号）。
	if choice.Delta.Content != "" {
		if t.textBlockIndex < 0 {
			t.textBlockIndex = t.blockIndex
			t.blockIndex++
			events = append(events, t.sse("content_block_start", map[string]interface{}{
				"type":  "content_block_start",
				"index": t.textBlockIndex,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			}))
		}
		events = append(events, t.sse("content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": t.textBlockIndex,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": choice.Delta.Content,
			},
		}))
	}

	// 工具调用增量（并行按 OpenAI tool index 拆块）。
	for _, tc := range choice.Delta.ToolCalls {
		blockIdx, started := t.toolIndexToBlock[tc.Index]
		if !started {
			blockIdx = t.blockIndex
			t.blockIndex++
			t.toolIndexToBlock[tc.Index] = blockIdx
			events = append(events, t.sse("content_block_start", map[string]interface{}{
				"type":  "content_block_start",
				"index": blockIdx,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": map[string]interface{}{},
				},
			}))
			if t.inputJSONs[tc.Index] == nil {
				t.inputJSONs[tc.Index] = &strings.Builder{}
			}
		}
		if tc.Function.Arguments != "" {
			if t.inputJSONs[tc.Index] == nil {
				t.inputJSONs[tc.Index] = &strings.Builder{}
			}
			t.inputJSONs[tc.Index].WriteString(tc.Function.Arguments)
			events = append(events, t.sse("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIdx,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": tc.Function.Arguments,
				},
			}))
		}
	}

	if choice.FinishReason != "" {
		t.stopReason = anthropicStopReason(choice.FinishReason)
	}
	return events
}

// finish 在流结束时写出收尾事件（关闭所有块、message_delta、message_stop）。
func (t *anthropicSSETransformer) finish() [][]byte {
	if t.finished {
		return nil
	}
	t.finished = true
	var events [][]byte

	if !t.started {
		t.started = true
		events = append(events, t.sse("message_start", map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            t.messageID,
				"type":          "message",
				"role":          "assistant",
				"model":         t.model,
				"content":       []interface{}{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]interface{}{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}))
	}

	// 关闭所有已开始的块。
	for i := 0; i < t.blockIndex; i++ {
		events = append(events, t.sse("content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": i,
		}))
	}

	stopReason := t.stopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	usage := map[string]interface{}{"input_tokens": 0, "output_tokens": 0}
	if t.usage != nil {
		usage = map[string]interface{}{
			"input_tokens":  t.usage.PromptTokens,
			"output_tokens": t.usage.CompletionTokens,
		}
	}
	events = append(events, t.sse("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usage,
	}))
	events = append(events, t.sse("message_stop", map[string]interface{}{
		"type": "message_stop",
	}))
	return events
}
