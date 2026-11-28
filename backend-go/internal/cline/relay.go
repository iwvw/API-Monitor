package cline

// 本文件中继面：把网关转发来的 OpenAI 请求打到 Cline 上游，失败换号重试，
// 并透传/聚合上游响应。
//
// ── 请求体整形（照抄 cline-proxy 口径）───────────────────────
//   - `model` **原样保留**（含池前缀，那是计费通道选择器）；
//   - `max_tokens`：客户端 `max_tokens` → `max_completion_tokens` → 默认 128000；
//   - `session_id`：每请求生成 `sess_<毫秒>`，与 `X-Task-ID` 头同值；
//   - `reasoning_effort`：客户端显式值优先，否则默认 `high`；
//   - 其余键按白名单透传。
//
// ── 上游恒为流式 ────────────────────────────────────────────
// 无论客户端要什么，发给上游的 body 都带 `stream:true`；客户端要非流式时把
// SSE 聚合成一个 chat.completion 再回。

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// maxChatBodyBytes 是对话请求体的大小上限（64MB）。
const maxChatBodyBytes int64 = 64 << 20

// defaultMaxTokens 是上游缺省 max_tokens（与 cline-proxy 一致）。
const defaultMaxTokens = 128000

// defaultReasoningEffort 是上游缺省思考档位。
const defaultReasoningEffort = "high"

// passThroughKeys 是允许原样透传给上游的额外键。
var passThroughKeys = []string{
	"temperature", "top_p", "top_k", "stop", "tools", "tool_choice",
	"presence_penalty", "frequency_penalty", "seed", "response_format",
}

// serveRelay 处理 /api/cline/v1/* 的中继请求。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "Cline 插件未启用", "service_unavailable")
		return
	}
	switch {
	case r.Method == http.MethodGet && (stripped == "/v1/models" || stripped == "/v1/models/"):
		s.serveRelayModels(w, r)
	case r.Method == http.MethodPost && stripped == "/v1/chat/completions":
		s.serveChatCompletions(w, r)
	default:
		writeOpenAIError(w, http.StatusNotFound, "未支持的中继路径: "+stripped, "invalid_request_error")
	}
}

// serveRelayModels 输出带前缀、且过滤掉停用项的模型列表。
func (s *Service) serveRelayModels(w http.ResponseWriter, r *http.Request) {
	models := s.catalog()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		if !s.isModelEnabled(m.ID) {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":       s.prefixModel(m.ID),
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": providerName,
		})
	}
	responseJSON(w, map[string]interface{}{"object": "list", "data": out})
}

// serveChatCompletions 转发一次对话请求。
func (s *Service) serveChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxChatBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体不是合法 JSON", "invalid_request_error")
		return
	}
	clientModel, _ := payload["model"].(string)
	clientModel = strings.TrimSpace(clientModel)
	if clientModel == "" {
		writeOpenAIError(w, http.StatusBadRequest, "缺少 model", "invalid_request_error")
		return
	}
	if !s.isModelEnabled(clientModel) {
		writeOpenAIError(w, http.StatusBadRequest, "未知模型 "+clientModel+"，可用模型见 /v1/models", "invalid_request_error")
		return
	}
	// 剥掉**我们自己的**前缀得到上游 id；池前缀（cline-free/ 等）必须保留。
	upstreamModel := strings.TrimSpace(s.stripModelPrefix(clientModel))

	stream := false
	if v, ok := payload["stream"].(bool); ok {
		stream = v
	}
	normalized, err := buildUpstreamBody(payload, upstreamModel)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求体整形失败: "+err.Error(), "invalid_request_error")
		return
	}

	ctx := r.Context()
	maxAttempts := len(s.Settings().Accounts) + 1
	tried := map[string]bool{}
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		acc, ok := s.pickAccount(tried)
		if !ok {
			break
		}
		tried[acc.ID] = true
		if err := s.ensureToken(ctx, &acc); err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			s.markAccountError(ctx, acc.ID, err.Error())
			lastErr = err
			continue
		}
		resp, err := s.sendChat(ctx, acc, normalized)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
		}
		// 401 → 强制刷新同账号并重试一次。
		if resp.StatusCode == http.StatusUnauthorized && strings.TrimSpace(acc.RefreshToken) != "" {
			_ = resp.Body.Close()
			if _, rerr := s.refreshTokenLocked(ctx, &acc, true); rerr != nil {
				s.markAccountError(ctx, acc.ID, rerr.Error())
				lastErr = errors.New("账号 " + acc.ID + " 强制刷新失败：" + rerr.Error())
				continue
			}
			resp, err = s.sendChat(ctx, acc, normalized)
			if err != nil {
				s.setCooldown(acc.ID, cooldownDuration)
				lastErr = err
				continue
			}
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			s.clearCooldown(acc.ID)
			s.recordCall(acc.ID)
			if stream {
				s.forwardStream(ctx, w, resp, acc.ID, clientModel)
			} else {
				s.forwardJSON(w, resp, acc.ID, clientModel)
			}
			return
		case resp.StatusCode == http.StatusUnauthorized:
			msg := readUpstreamError(resp)
			s.markAccountError(ctx, acc.ID, "上游返回 401")
			lastErr = errors.New("上游返回 401：" + msg)
			continue
		case resp.StatusCode == http.StatusTooManyRequests:
			_ = resp.Body.Close()
			s.setCooldown(acc.ID, cooldownDuration)
			s.markAccountError(ctx, acc.ID, "上游返回 429，已冷却")
			lastErr = errors.New("上游返回 429，账号已冷却")
			continue
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			s.markAccountError(ctx, acc.ID, fmt.Sprintf("上游返回 %d", resp.StatusCode))
			lastErr = fmt.Errorf("上游返回 %d", resp.StatusCode)
			continue
		default:
			// 其它 4xx：不换号，原样透传状态码与上游 message。
			msg := readUpstreamError(resp)
			writeOpenAIError(w, resp.StatusCode, msg, "upstream_error")
			return
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用账号")
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// buildUpstreamBody 把客户端请求收敛成上游认识的形状。
func buildUpstreamBody(payload map[string]interface{}, upstreamModel string) ([]byte, error) {
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixMilli())
	out := map[string]interface{}{
		"model":      upstreamModel,
		"stream":     true,
		"session_id": sessionID,
	}
	if v, ok := payload["max_tokens"]; ok {
		if n, ok := toFloat(v); ok && n > 0 {
			out["max_tokens"] = int64(n)
		}
	} else if v, ok := payload["max_completion_tokens"]; ok {
		if n, ok := toFloat(v); ok && n > 0 {
			out["max_tokens"] = int64(n)
		}
	}
	if _, ok := out["max_tokens"]; !ok {
		out["max_tokens"] = defaultMaxTokens
	}
	effort := defaultReasoningEffort
	for _, key := range []string{"reasoning_effort", "reasoningEffort"} {
		if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
			effort = strings.TrimSpace(v)
			break
		}
	}
	out["reasoning_effort"] = effort
	if msgs, ok := payload["messages"]; ok {
		out["messages"] = msgs
	}
	for _, key := range passThroughKeys {
		if v, ok := payload[key]; ok {
			out[key] = v
		}
	}
	return json.Marshal(out)
}

// sendChat 转发一次聊天请求（共享客户端，无总超时避免截断长回答）。
func (s *Service) sendChat(ctx context.Context, acc Account, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	applyChatHeaders(req, acc, body)
	return s.httpClientFor(acc.ID).Do(req)
}

// applyChatHeaders 施加对话请求头。
//
// 三处**必须**保留的：`Authorization: Bearer workos:…`（前缀不能丢）、
// `X-CLIENT-TYPE: cline-sdk`（免费池门槛）、`X-Task-ID`（与 body 的 session_id 同值）。
func applyChatHeaders(req *http.Request, acc Account, body []byte) {
	var payload struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(body, &payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("User-Agent", "Cline/3.0.62")
	req.Header.Set("HTTP-Referer", "https://cline.bot")
	req.Header.Set("X-Title", "Cline")
	req.Header.Set("X-IS-MULTIROOT", "false")
	req.Header.Set("X-CLIENT-TYPE", clientType)
	req.Header.Set("X-CLIENT-VERSION", "3.0.62")
	req.Header.Set("X-PLATFORM", "terminal")
	req.Header.Set("X-PLATFORM-VERSION", "3.0.62")
	req.Header.Set("X-CORE-VERSION", "0.0.70")
	if payload.SessionID != "" {
		req.Header.Set("X-Task-ID", payload.SessionID)
	}
}

func readUpstreamError(resp *http.Response) string {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	msg := jsonMessage(data)
	if msg == "" {
		return http.StatusText(resp.StatusCode)
	}
	return truncate(msg, 300)
}

// forwardStream 逐行透传上游 SSE，并把每帧的 model 改写为客户端请求名。
func (s *Service) forwardStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, accountID, clientModel string) {
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	reader := bufio.NewReader(resp.Body)
	var lastUsage map[string]interface{}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			out, usage := rewriteSSELine(line, clientModel)
			if usage != nil {
				lastUsage = usage
			}
			_, _ = io.WriteString(w, out)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				applog.Warn(ctx, "cline", "upstream stream interrupted", "account", accountID, "error", err.Error())
			}
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	if lastUsage != nil {
		s.recordUsageFromOpenAI(accountID, clientModel, lastUsage)
	}
}

// rewriteSSELine 重写一行 SSE 的 model 字段，返回改写后的行与原样 usage。
//
// 上游 SSE 里的 model 是**上游内部名**（如 `deepseek/deepseek-v4.1-flash`），
// 必须改写回客户端请求的那个，否则客户端会认为模型不匹配。
func rewriteSSELine(line, clientModel string) (string, map[string]interface{}) {
	trimmed := strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(trimmed, "data:") {
		return line, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return line, nil
	}
	frame := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return line, nil
	}
	var usage map[string]interface{}
	if raw, ok := frame["usage"]; ok {
		_ = json.Unmarshal(raw, &usage)
	}
	if modelJSON, err := json.Marshal(clientModel); err == nil {
		frame["model"] = json.RawMessage(modelJSON)
	}
	rewritten, err := json.Marshal(frame)
	if err != nil {
		return line, usage
	}
	return "data: " + string(rewritten) + "\n", usage
}

// forwardJSON 聚合上游 SSE 成一个完整响应后下发，并提取 usage 记账。
func (s *Service) forwardJSON(w http.ResponseWriter, resp *http.Response, accountID, clientModel string) {
	defer resp.Body.Close()
	agg, err := aggregateSSE(resp.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "读取上游响应失败: "+err.Error(), "upstream_error")
		return
	}
	agg["model"] = clientModel
	if usage, ok := agg["usage"].(map[string]interface{}); ok {
		s.recordUsageFromOpenAI(accountID, clientModel, usage)
	}
	out, err := json.Marshal(agg)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "响应序列化失败: "+err.Error(), "upstream_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// aggregateSSE 读取完整 SSE 流，聚合成单个 chat.completion 响应。
func aggregateSSE(r io.Reader) (map[string]interface{}, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var (
		id, model    string
		created      float64
		content      strings.Builder
		reasoning    strings.Builder
		role         = "assistant"
		finishReason = "stop"
		usage        map[string]interface{}
	)
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload != "" && payload != "[DONE]" {
				var chunk map[string]interface{}
				if json.Unmarshal([]byte(payload), &chunk) == nil {
					if v, ok := chunk["id"].(string); ok && id == "" {
						id = v
					}
					if v, ok := chunk["model"].(string); ok && model == "" {
						model = v
					}
					if v, ok := chunk["created"].(float64); ok && created == 0 {
						created = v
					}
					if u, ok := chunk["usage"].(map[string]interface{}); ok {
						usage = u
					}
					consumeChoices(chunk, &content, &reasoning, &role, &finishReason)
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = float64(time.Now().Unix())
	}
	message := map[string]interface{}{"role": role, "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	resp := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": int64(created),
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "message": message, "finish_reason": finishReason,
		}},
	}
	if usage != nil {
		resp["usage"] = usage
	}
	return resp, nil
}

// consumeChoices 从一帧 chunk 里累积正文/思考内容与结束原因。
func consumeChoices(chunk map[string]interface{}, content, reasoning *strings.Builder, role, finishReason *string) {
	choices, ok := chunk["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return
	}
	if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
		*finishReason = fr
	}
	delta, ok := choice["delta"].(map[string]interface{})
	if !ok {
		delta, ok = choice["message"].(map[string]interface{})
		if !ok {
			return
		}
	}
	if r, ok := delta["role"].(string); ok && r != "" {
		*role = r
	}
	if c, ok := delta["content"].(string); ok {
		content.WriteString(c)
	}
	if r, ok := delta["reasoning_content"].(string); ok {
		reasoning.WriteString(r)
	}
}

// recordUsageFromOpenAI 从 usage 对象里取用量记账。
func (s *Service) recordUsageFromOpenAI(accountID, model string, usage map[string]interface{}) {
	if usage == nil {
		return
	}
	prompt := jsonInt(usage["prompt_tokens"])
	completion := jsonInt(usage["completion_tokens"])
	cached := cachedTokens(usage)
	s.recordUsage(accountID, model, prompt, completion, cached)
}
