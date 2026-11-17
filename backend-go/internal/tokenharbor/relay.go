package tokenharbor

// 本文件中继面:把网关转发来的 OpenAI 请求打到 TokenHarbor 上游(direct-chat
// SSE 协议),失败换号重试,并把上游 SSE 桥接成 OpenAI 响应。
//
// ── 协议桥接 ────────────────────────────────────────────────
// 上游是站点内部的 SSE(thinking / chunk / tool_use / citation / done / error),
// 不是 OpenAI 形态;本文件把它翻译成 chat.completion(.chunk):
//   - thinking → delta.reasoning_content
//   - chunk    → delta.content
//   - tool_use → delta.tool_calls
//   - citation → delta.annotations
//   - done     → finish_reason: stop + [DONE]
//
// ── 会话 ────────────────────────────────────────────────────
// 上游按 session 保留多轮历史,请求体只发「最后一条 user 消息」。线程键优先用
// 客户端的 user 字段;缺失时用「模型 + 首条消息」的哈希锚定,保证同一会话复用。

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// serveRelay 处理 /api/tokenharbor/v1/* 的中继请求。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "TokenHarbor 插件未启用", "service_unavailable")
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
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
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
		writeOpenAIError(w, http.StatusBadRequest, "未知模型 "+clientModel+",可用模型见 /v1/models", "invalid_request_error")
		return
	}
	upstreamModel := resolveModel(s.stripModelPrefix(clientModel))

	stream := false
	if v, ok := payload["stream"].(bool); ok {
		stream = v
	}

	messages, _ := payload["messages"].([]interface{})
	content := lastUserText(messages)
	attachments := extractAttachments(messages)
	if strings.TrimSpace(content) == "" && len(attachments) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "消息内容为空", "invalid_request_error")
		return
	}
	threadKey := deriveThreadKey(payload, upstreamModel, messages)

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
		resp, err := s.attemptStream(ctx, threadKey, upstreamModel, content, attachments, acc, false)
		if err != nil {
			if upstreamStatus(err) == http.StatusTooManyRequests {
				// 上游对单会话有速率限制:换新会话重试一次绕过。
				s.sessions.remove(threadKey)
				resp, err = s.attemptStream(ctx, threadKey, upstreamModel, content, attachments, acc, true)
			}
		}
		if err != nil {
			status := upstreamStatus(err)
			switch {
			case status == http.StatusUnauthorized || status == http.StatusForbidden:
				s.setCooldown(acc.ID, cooldownDuration)
				s.markAccountError(ctx, acc.ID, err.Error())
				lastErr = err
				continue
			case status == http.StatusTooManyRequests:
				s.setCooldown(acc.ID, cooldownDuration)
				s.markAccountError(ctx, acc.ID, "上游返回 429,已冷却")
				lastErr = err
				continue
			case status >= 500 || status == 0:
				s.setCooldown(acc.ID, cooldownDuration)
				s.markAccountError(ctx, acc.ID, err.Error())
				lastErr = err
				continue
			default:
				// 其它 4xx:不换号,原样透传上游状态码。
				writeOpenAIError(w, status, err.Error(), "upstream_error")
				return
			}
		}

		s.clearCooldown(acc.ID)
		s.recordCall(acc.ID)
		if stream {
			s.forwardStream(ctx, w, resp, acc.ID, clientModel)
		} else {
			s.forwardJSON(w, resp, acc.ID, clientModel)
		}
		return
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用账号")
	}
	writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
}

// attemptStream 组装请求并打一次上游;retryNewSession 表示已在上层清过会话。
func (s *Service) attemptStream(ctx context.Context, threadKey, model, content string, attachments []interface{}, acc Account, retryNewSession bool) (*http.Response, error) {
	sessionID, err := s.ensureSession(ctx, threadKey, model, acc.Cookie)
	if err != nil {
		return nil, err
	}
	req := map[string]interface{}{
		"sessionId": sessionID,
		"content":   content,
		"model":     model,
		"webSearch": "auto",
		"tz":        siteTimezone(),
	}
	if len(attachments) > 0 {
		req["attachments"] = attachments
	}
	resp, err := s.streamChat(ctx, acc.Cookie, req)
	if err != nil && upstreamStatus(err) == http.StatusNotFound && !retryNewSession {
		// 会话失效:重建一次。
		s.sessions.remove(threadKey)
		sessionID, cerr := s.ensureSession(ctx, threadKey, model, acc.Cookie)
		if cerr != nil {
			return nil, cerr
		}
		req["sessionId"] = sessionID
		return s.streamChat(ctx, acc.Cookie, req)
	}
	return resp, err
}

// siteTimezone 返回站点时区字符串(上游用于时间感知)。
func siteTimezone() string {
	zone, offset := time.Now().Zone()
	if zone != "" && strings.ContainsAny(zone, "/") {
		return zone
	}
	return fmt.Sprintf("UTC%s", formatOffset(offset))
}

func formatOffset(offset int) string {
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
}

// -----------------------------------------------------------------------------
// 请求体整形
// -----------------------------------------------------------------------------

// lastUserText 取最后一条 user 消息的文本。
func lastUserText(messages []interface{}) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m, ok := messages[i].(map[string]interface{})
		if !ok || m["role"] != "user" {
			continue
		}
		return messageText(m["content"])
	}
	return ""
}

// messageText 把消息 content 归一化成纯文本(string 或 parts 数组)。
func messageText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, part := range v {
			p, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := p["text"].(string); ok {
				sb.WriteString(text)
			}
		}
		return sb.String()
	}
	return ""
}

// extractAttachments 从消息里提取图片 data URL 作为上游附件。
func extractAttachments(messages []interface{}) []interface{} {
	out := make([]interface{}, 0)
	for _, raw := range messages {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		parts, ok := m["content"].([]interface{})
		if !ok {
			continue
		}
		for _, part := range parts {
			p, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if p["type"] != "image_url" {
				continue
			}
			url := ""
			switch u := p["image_url"].(type) {
			case string:
				url = u
			case map[string]interface{}:
				url, _ = u["url"].(string)
			}
			if !strings.HasPrefix(url, "data:") {
				continue
			}
			mime, b64 := splitDataURL(url)
			if b64 == "" {
				continue
			}
			out = append(out, map[string]interface{}{
				"kind":     "image",
				"mime":     mime,
				"name":     "image." + mimeExt(mime),
				"bytes":    len(b64) * 3 / 4,
				"data_url": url,
			})
		}
	}
	return out
}

func splitDataURL(url string) (string, string) {
	rest := strings.TrimPrefix(url, "data:")
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return "", ""
	}
	header := rest[:idx]
	mime := strings.TrimSuffix(header, ";base64")
	if mime == "" {
		mime = "image/png"
	}
	return mime, rest[idx+1:]
}

func mimeExt(mime string) string {
	switch mime {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}

// deriveThreadKey 生成线程键:优先 user 字段,否则用模型 + 首条消息哈希锚定。
func deriveThreadKey(payload map[string]interface{}, model string, messages []interface{}) string {
	if u, ok := payload["user"].(string); ok && strings.TrimSpace(u) != "" {
		return "user:" + strings.TrimSpace(u)
	}
	h := sha256.New()
	h.Write([]byte(model))
	for i, raw := range messages {
		if i >= 2 {
			break
		}
		if m, ok := raw.(map[string]interface{}); ok {
			role, _ := m["role"].(string)
			h.Write([]byte(role))
			h.Write([]byte(messageText(m["content"])))
		}
	}
	return "hash:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// -----------------------------------------------------------------------------
// SSE 桥接
// -----------------------------------------------------------------------------

// forwardStream 逐事件把上游 SSE 桥接成 OpenAI SSE,并把每帧 model 改写为客户端请求名。
func (s *Service) forwardStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, accountID, clientModel string) {
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	flush()

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	sawContent := false
	finished := false
	var usage map[string]interface{}

	readUpstream(ctx, resp.Body, func(event string, data map[string]interface{}) bool {
		switch event {
		case "thinking":
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{
				"role": "assistant", "reasoning_content": jsonString(data["delta"]),
			}, nil))
		case "chunk":
			delta := jsonString(data["delta"])
			if delta != "" {
				sawContent = true
			}
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"content": delta}, nil))
		case "tool_use":
			tool := map[string]interface{}{
				"index": jsonInt(data["index"]),
				"type":  "function",
				"function": map[string]interface{}{
					"name":      firstNonEmpty(jsonString(data["name"]), "web_search"),
					"arguments": jsonStringify(data["arguments"]),
				},
			}
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"tool_calls": []interface{}{tool}}, nil))
		case "citation":
			if url := jsonString(data["url"]); url != "" {
				annotation := map[string]interface{}{"type": "url_citation", "url": url, "title": jsonString(data["title"])}
				writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"annotations": []interface{}{annotation}}, nil))
			}
		case "error":
			msg := firstNonEmpty(jsonString(data["message"]), "上游流错误")
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"content": "\n\n[上游错误: " + msg + "]"}, strPtr("stop")))
			writeDone(w, flush, clientModel, created, nil)
			finished = true
			return false
		case "done":
			if u, ok := data["usage"].(map[string]interface{}); ok {
				usage = u
			}
			if sawContent {
				writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{}, strPtr("stop")))
			} else {
				writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"content": "\n\n[上游未返回内容]"}, strPtr("stop")))
			}
			writeDone(w, flush, clientModel, created, upstreamUsage(usage))
			finished = true
			return false
		}
		return true
	})

	if !finished {
		// 流在未收到 done 的情况下结束:补一个结束帧。
		if !sawContent {
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{"content": "\n\n[上游未返回内容]"}, strPtr("stop")))
		} else {
			writeSSE(w, flush, chunkFrame(id, clientModel, created, map[string]interface{}{}, strPtr("stop")))
		}
		writeDone(w, flush, clientModel, created, upstreamUsage(usage))
	}
	if usage != nil {
		s.recordUsageFromUpstream(accountID, clientModel, usage)
	}
}

// forwardJSON 聚合上游 SSE 成一个完整响应后下发,并提取 usage 记账。
func (s *Service) forwardJSON(w http.ResponseWriter, resp *http.Response, accountID, clientModel string) {
	defer resp.Body.Close()
	var (
		content   strings.Builder
		reasoning strings.Builder
		toolCalls []interface{}
		usage     map[string]interface{}
	)
	readUpstream(context.Background(), resp.Body, func(event string, data map[string]interface{}) bool {
		switch event {
		case "thinking":
			reasoning.WriteString(jsonString(data["delta"]))
		case "chunk":
			content.WriteString(jsonString(data["delta"]))
		case "tool_use":
			toolCalls = append(toolCalls, map[string]interface{}{
				"index": jsonInt(data["index"]),
				"type":  "function",
				"function": map[string]interface{}{
					"name":      firstNonEmpty(jsonString(data["name"]), "web_search"),
					"arguments": jsonStringify(data["arguments"]),
				},
			})
		case "done":
			if u, ok := data["usage"].(map[string]interface{}); ok {
				usage = u
			}
			return false
		case "error":
			msg := firstNonEmpty(jsonString(data["message"]), "上游流错误")
			content.WriteString("\n\n[上游错误: " + msg + "]")
			return false
		}
		return true
	})
	message := map[string]interface{}{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	out := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   clientModel,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"message":       message,
			"finish_reason": "stop",
		}},
	}
	if usage != nil {
		out["usage"] = upstreamUsage(usage)
	}
	data, err := json.Marshal(out)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "响应序列化失败: "+err.Error(), "upstream_error")
		return
	}
	if usage != nil {
		s.recordUsageFromUpstream(accountID, clientModel, usage)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// readUpstream 逐行读取上游 SSE,解析出事件名与 data 后交给 handle。
// handle 返回 false 时立即停止读取。
func readUpstream(ctx context.Context, body io.Reader, handle func(event string, data map[string]interface{}) bool) {
	reader := bufio.NewReaderSize(body, 64*1024)
	pendingEvent := ""
	for {
		if ctx.Err() != nil {
			return
		}
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "event:") {
			pendingEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		} else if strings.HasPrefix(trimmed, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload != "" {
				var data map[string]interface{}
				if json.Unmarshal([]byte(payload), &data) == nil {
					event := pendingEvent
					if event == "" {
						event, _ = data["event"].(string)
					}
					if !handle(event, data) {
						return
					}
				}
			}
			pendingEvent = ""
		}
		if err != nil {
			if err != io.EOF {
				applog.Warn(ctx, "tokenharbor", "upstream stream interrupted", "error", err.Error())
			}
			return
		}
	}
}

// chunkFrame 构造一帧 OpenAI chat.completion.chunk。
func chunkFrame(id, model string, created int64, delta map[string]interface{}, finish interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []interface{}{map[string]interface{}{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
}

// writeSSE 写一帧 SSE data 并 flush。
func writeSSE(w io.Writer, flush func(), frame map[string]interface{}) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
	flush()
}

// writeDone 写结束帧与 [DONE]。
func writeDone(w io.Writer, flush func(), model string, created int64, usage map[string]interface{}) {
	frame := chunkFrame(fmt.Sprintf("chatcmpl-%d", created), model, created, map[string]interface{}{}, "stop")
	if usage != nil {
		frame["usage"] = usage
	}
	writeSSE(w, flush, frame)
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flush()
}

// upstreamUsage 把上游 usage(inputTokens/outputTokens)映射成 OpenAI usage。
func upstreamUsage(usage map[string]interface{}) map[string]interface{} {
	prompt := jsonInt(usage["inputTokens"])
	completion := jsonInt(usage["outputTokens"])
	return map[string]interface{}{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
}

// recordUsageFromUpstream 从上游 usage 对象里取用量记账。
func (s *Service) recordUsageFromUpstream(accountID, model string, usage map[string]interface{}) {
	if usage == nil {
		return
	}
	prompt := jsonInt(usage["inputTokens"])
	completion := jsonInt(usage["outputTokens"])
	if prompt == 0 && completion == 0 {
		prompt = jsonInt(usage["prompt_tokens"])
		completion = jsonInt(usage["completion_tokens"])
	}
	s.recordUsage(accountID, model, prompt, completion, 0)
}

func jsonString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func jsonStringify(v interface{}) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func strPtr(s string) *string {
	return &s
}
