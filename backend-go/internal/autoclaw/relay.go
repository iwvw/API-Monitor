package autoclaw

// 本文件中继面：把网关转发来的 OpenAI 请求打到 AutoClaw 上游。
//
// ── 四道上游闸（转发路径必须逐条满足）───────────────────────
//  1. `max_tokens` 不能是小正整数（<10000）→ 406 空响应体。这里统一抬到至少 10000。
//  2. system 首句必须是 OpenClaw 身份句 + `## Tooling` 段，且不能出现外来 harness
//     的身份句（`You are ZCode…` / `You are Claude Code…`）→ 否则 406 / 403。
//  3. **不发** `X-Harness-Type: zcode`（会 403 pay-view / 406）。注意 userapi 域
//     （刷新/余额/签到/目录）**要发**，两处口径不同。
//  4. 请求体不能带 `metadata`（→ 500 `parse response failed`），出站前删掉整个键。

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

// minMaxTokens 是上游要求的 max_tokens 下限（小于它会被 406）。
const minMaxTokens = 10000

// identityPrefix 是上游要求的 system 身份句 + Tooling 段（照抄客户端内置提示词开头）。
//
// 实测：只有身份句、没有 `## Tooling` 段 → 403 pay-view；中性句（You are a
// helpful assistant.）或完全没有 system → 406 空响应体。
const identityPrefix = `You are OpenClaw, an interactive coding agent.

## Tooling

`

// blockedIdentities 是上游黑名单里的外来 harness 身份句（字面、大小写不敏感）。
var blockedIdentities = []string{
	"You are ZCode",
	"You are Claude Code",
	"You are Codex",
	"You are an AI agent powered by DeepSeek Harness",
}

// serveRelay 处理 /api/autoclaw/v1/* 的中继请求。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "AutoClaw 插件未启用", "service_unavailable")
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
		resp, err := s.sendChat(ctx, acc, normalized)
		if err != nil {
			s.setCooldown(acc.ID, cooldownDuration)
			lastErr = err
			continue
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
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			msg := readUpstreamError(resp)
			s.markAccountError(ctx, acc.ID, "上游返回 "+fmt.Sprint(resp.StatusCode))
			lastErr = fmt.Errorf("上游返回 %d：%s", resp.StatusCode, msg)
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

// buildUpstreamBody 把客户端请求收敛成上游认识的形状（四道闸在这里落实）。
func buildUpstreamBody(payload map[string]interface{}, upstreamModel string) ([]byte, error) {
	out := map[string]interface{}{"model": upstreamModel}

	// 闸 1：max_tokens 不能是小正整数。
	maxTokens := int64(minMaxTokens)
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		if v, ok := payload[key]; ok {
			if n := jsonIntValue(v); n > 0 {
				maxTokens = n
				break
			}
		}
	}
	if maxTokens < minMaxTokens {
		maxTokens = minMaxTokens
	}
	out["max_tokens"] = maxTokens

	// 闸 2：system 首句改写。
	messages := normalizeSystemPrompt(payload["messages"])
	if messages != nil {
		out["messages"] = messages
	}
	// 保留流式意图与常用参数。
	if v, ok := payload["stream"]; ok {
		out["stream"] = v
	}
	for _, key := range []string{"temperature", "top_p", "stop", "tools", "tool_choice"} {
		if v, ok := payload[key]; ok {
			out[key] = v
		}
	}
	// 闸 4：删掉整个 metadata。
	delete(out, "metadata")

	return json.Marshal(out)
}

// normalizeSystemPrompt 落实闸 2：首条 system 前置身份句，并改写外来 harness 身份句。
//
// 边界：**只看 system / developer 消息** —— 同一句放进 user 消息不触发闸门，
// 所以改写只做在 system 上（改 user 会破坏用户内容）。
func normalizeSystemPrompt(raw interface{}) []interface{} {
	list, ok := raw.([]interface{})
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(list))
	prefixed := false
	for _, item := range list {
		msg, ok := item.(map[string]interface{})
		if !ok {
			out = append(out, item)
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" && role != "developer" {
			out = append(out, item)
			continue
		}
		content, _ := msg["content"].(string)
		content = rewriteBlockedIdentities(content)
		if !prefixed {
			content = identityPrefix + content
			prefixed = true
		}
		clone := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			clone[k] = v
		}
		clone["content"] = content
		out = append(out, clone)
	}
	if !prefixed {
		// 没有任何 system 消息：补一条身份句（否则 406 空响应体）。
		out = append([]interface{}{map[string]interface{}{
			"role":    "system",
			"content": identityPrefix,
		}}, out...)
	}
	return out
}

// rewriteBlockedIdentities 把外来 harness 身份句改成不命中的写法（大小写不敏感）。
func rewriteBlockedIdentities(content string) string {
	for _, blocked := range blockedIdentities {
		content = replaceFold(content, blocked, "You are an assistant")
	}
	return content
}

// replaceFold 大小写不敏感地替换所有出现（保留替换文本原样）。
func replaceFold(s, old, replacement string) string {
	if old == "" {
		return s
	}
	lowerS := strings.ToLower(s)
	lowerOld := strings.ToLower(old)
	var b strings.Builder
	i := 0
	for {
		idx := strings.Index(lowerS[i:], lowerOld)
		if idx < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + idx
		b.WriteString(s[i:start])
		b.WriteString(replacement)
		i = start + len(old)
	}
	return b.String()
}

// jsonIntValue 把任意 JSON 数值转成 int64（0 表示无法解析）。
func jsonIntValue(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// sendChat 转发一次聊天请求（**不发** X-Harness-Type，见闸 3）。
func (s *Service) sendChat(ctx context.Context, acc Account, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		llmBaseFor(acc.Region)+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	applyChatHeaders(req, acc)
	return s.httpClientFor(acc.ID).Do(req)
}

// applyChatHeaders 施加对话请求头（品牌头 + 签名，但**不含** X-Harness-Type）。
func applyChatHeaders(req *http.Request, acc Account) {
	headers := signedHeaders(acc)
	delete(headers, "X-Harness-Type")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "text/event-stream")
}

func readUpstreamError(resp *http.Response) string {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorB))
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
				applog.Warn(ctx, "autoclaw", "upstream stream interrupted", "account", accountID, "error", err.Error())
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
