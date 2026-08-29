package antigravity

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

func newScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	return sc
}

// openAIMessage 是 OpenAI chat.completions 请求的消息项。
type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// handleOpenAIModels 实现 OpenAI 兼容 GET /v1/models：返回已启用模型列表。
// 模型网关 verify / 健康检查会请求该端点拉取可用模型。
func (s *Service) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	models, err := s.FetchModels(ctx, "")
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"object": "list",
			"data":   []interface{}{},
		})
		return
	}
	disabled := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		disabled[d] = true
	}
	data := make([]map[string]interface{}, 0, len(models))
	for id, mi := range models {
		if disabled[id] {
			continue
		}
		outID := id
		if alias := s.aliasForUpstream(id); alias != "" {
			outID = alias
		}
		data = append(data, map[string]interface{}{
			"id":          outID,
			"object":      "model",
			"created":     0,
			"owned_by":    "antigravity",
			"displayName": mi.DisplayName,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"object": "list", "data": data})
}

// openAIChatRequest 是 OpenAI chat.completions 请求的精简结构。
type openAIChatRequest struct {
	Model       string           `json:"model"`
	Messages    []openAIMessage  `json:"messages"`
	MaxTokens   *int             `json:"max_tokens"`
	Temperature *float64         `json:"temperature"`
	TopP        *float64         `json:"top_p"`
	Stream      bool             `json:"stream"`
}

// buildClaudeRequest 把 OpenAI chat.completions 请求转为 Claude Messages 请求。
func buildClaudeRequest(oai *openAIChatRequest) (*engineag.ClaudeRequest, error) {
	claude := &engineag.ClaudeRequest{
		Model:       oai.Model,
		MaxTokens:   4096,
		Stream:      oai.Stream,
		Temperature: oai.Temperature,
		TopP:        oai.TopP,
	}
	if oai.MaxTokens != nil && *oai.MaxTokens > 0 {
		claude.MaxTokens = *oai.MaxTokens
	}

	var systemParts []string
	for _, m := range oai.Messages {
		role := m.Role
		switch role {
		case "system":
			systemParts = append(systemParts, messageText(m.Content))
		case "user":
			claude.Messages = append(claude.Messages, engineag.ClaudeMessage{Role: "user", Content: buildClaudeContent(m.Content)})
		case "assistant":
			claude.Messages = append(claude.Messages, engineag.ClaudeMessage{Role: "assistant", Content: buildClaudeContent(m.Content)})
		case "tool", "function":
			systemParts = append(systemParts, fmt.Sprintf("[tool/function result] %s", messageText(m.Content)))
		}
	}
	if len(systemParts) > 0 {
		b, _ := json.Marshal(strings.Join(systemParts, "\n\n"))
		claude.System = b
	}
	return claude, nil
}

// messageText 提取消息文本（支持字符串或 parts 数组）。
func messageText(content any) string {
	s, ok := content.(string)
	if ok {
		return s
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch pm["type"] {
		case "text":
			if t, ok := pm["text"].(string); ok {
				sb.WriteString(t)
			}
		case "image_url":
			if im, ok := pm["image_url"].(map[string]any); ok {
				if u, ok := im["url"].(string); ok && strings.HasPrefix(u, "data:") {
					sb.WriteString("[图片]")
				}
			}
		}
	}
	return sb.String()
}

// buildClaudeContent 把 OpenAI content 转为 Claude content（JSON raw）。
func buildClaudeContent(content any) json.RawMessage {
	// 字符串直接序列化为字符串。
	if s, ok := content.(string); ok {
		b, _ := json.Marshal(s)
		return b
	}
	// parts 数组 → Claude content blocks（text / image base64）。
	parts, ok := content.([]any)
	if !ok {
		b, _ := json.Marshal("")
		return b
	}
	blocks := []map[string]any{}
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch pm["type"] {
		case "text":
			if t, ok := pm["text"].(string); ok {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
		case "image_url":
			im, ok := pm["image_url"].(map[string]any)
			if !ok {
				continue
			}
			u, _ := im["url"].(string)
			if !strings.HasPrefix(u, "data:") {
				continue
			}
			// data:image/png;base64,xxx
			comma := strings.Index(u, ",")
			if comma < 0 {
				continue
			}
			meta := u[5:comma]
			mediaType := "image/png"
			if semi := strings.Index(meta, ";"); semi > 0 {
				mediaType = meta[:semi]
			}
			raw := u[comma+1:]
			if _, err := base64.StdEncoding.DecodeString(raw); err != nil {
				continue
			}
			blocks = append(blocks, map[string]any{
				"type": "image", "source": map[string]any{
					"type": "base64", "media_type": mediaType, "data": raw,
				},
			})
		}
	}
	if len(blocks) == 0 {
		b, _ := json.Marshal("")
		return b
	}
	b, _ := json.Marshal(blocks)
	return b
}

// chatCompletionsResponse 是返回给客户端的 OpenAI 格式响应（非流式）。
type chatCompletionsResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// forwardOpenAIChat 用 Claude 转发链路处理 OpenAI chat.completions 请求，输出 OpenAI 格式。
func (s *Service) forwardOpenAIChat(ctx context.Context, w http.ResponseWriter, oaiReq *openAIChatRequest) error {
	claudeReq, err := buildClaudeRequest(oaiReq)
	if err != nil {
		return err
	}
	claudeBody, err := json.Marshal(claudeReq)
	if err != nil {
		return fmt.Errorf("marshal claude request: %w", err)
	}
	claudeReq.Stream = oaiReq.Stream

	if !oaiReq.Stream {
		return s.forwardOpenAINonStream(ctx, w, oaiReq, claudeBody)
	}
	return s.forwardOpenAIStream(ctx, w, oaiReq, claudeBody)
}

// forwardOpenAINonStream 非流式：转 Claude → 上游 → 转回 OpenAI JSON。
func (s *Service) forwardOpenAINonStream(ctx context.Context, w http.ResponseWriter, oaiReq *openAIChatRequest, claudeBody []byte) error {
	acc := s.pickAccount()
	if acc == nil {
		return fmt.Errorf("尚无可用账号")
	}
	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return fmt.Errorf("构造客户端失败: %w", err)
	}
	opts := engineag.DefaultTransformOptions()
	opts.EnableIdentityPatch = true

	var claudeReq engineag.ClaudeRequest
	_ = json.Unmarshal(claudeBody, &claudeReq)
	geminiBody, err := engineag.TransformClaudeToGeminiWithOptions(&claudeReq, acc.ProjectID, s.resolveUpstreamModel(claudeReq.Model), opts)
	if err != nil {
		return fmt.Errorf("请求转换失败: %w", err)
	}
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return fmt.Errorf("获取访问凭证失败: %w", err)
	}
	req, err := engineag.NewAPIRequestWithURL(ctx, forwardBaseURL(acc), "generateContent", freshToken, geminiBody)
	if err != nil {
		return fmt.Errorf("构造上游请求失败: %w", err)
	}
	resp, err := agClient.Do(req)
	if err != nil {
		return fmt.Errorf("上游请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	full, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取上游响应失败: %w", err)
	}
	claudeOut, usage, err := engineag.TransformGeminiToClaude(full, claudeReq.Model)
	if err != nil {
		return fmt.Errorf("响应转换失败: %w", err)
	}
	var claudeResp engineag.ClaudeResponse
	if err := json.Unmarshal(claudeOut, &claudeResp); err != nil {
		return fmt.Errorf("claude 响应解析失败: %w", err)
	}

	out := chatCompletionsResponse{
		ID:      "chatcmpl-" + claudeResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   oaiReq.Model,
	}
	var content string
	for _, c := range claudeResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}
	if content == "" {
		content = " "
	}
	out.Choices = append(out.Choices, struct {
		Index        int    `json:"index"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{
		Index: 0, FinishReason: claudeStopReasonToOpenAI(claudeResp.StopReason),
	})
	out.Choices[0].Message.Role = "assistant"
	out.Choices[0].Message.Content = content
	if usage != nil {
		out.Usage.PromptTokens = usage.InputTokens
		out.Usage.CompletionTokens = usage.OutputTokens
		out.Usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(out)
}

// forwardOpenAIStream 流式：转 Claude SSE → OpenAI SSE chunk。
func (s *Service) forwardOpenAIStream(ctx context.Context, w http.ResponseWriter, oaiReq *openAIChatRequest, claudeBody []byte) error {
	acc := s.pickAccount()
	if acc == nil {
		return fmt.Errorf("尚无可用账号")
	}
	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return fmt.Errorf("构造客户端失败: %w", err)
	}
	opts := engineag.DefaultTransformOptions()
	opts.EnableIdentityPatch = true

	var claudeReq engineag.ClaudeRequest
	_ = json.Unmarshal(claudeBody, &claudeReq)
	geminiBody, err := engineag.TransformClaudeToGeminiWithOptions(&claudeReq, acc.ProjectID, s.resolveUpstreamModel(claudeReq.Model), opts)
	if err != nil {
		return fmt.Errorf("请求转换失败: %w", err)
	}
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return fmt.Errorf("获取访问凭证失败: %w", err)
	}
	req, err := engineag.NewAPIRequestWithURL(ctx, forwardBaseURL(acc), "streamGenerateContent", freshToken, geminiBody)
	if err != nil {
		return fmt.Errorf("构造上游请求失败: %w", err)
	}
	resp, err := agClient.Do(req)
	if err != nil {
		return fmt.Errorf("上游请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	processor := engineag.NewStreamingProcessor(claudeReq.Model)
	scanner := newScanner(resp.Body)
	delivered := false
	for scanner.Scan() {
		line := scanner.Text()
		event := processor.ProcessLine(line)
		if len(event) == 0 {
			continue
		}
		// event 是 Claude SSE 的 "event:...\ndata:{...}\n\n"，提取 data 里的 delta。
		text := extractClaudeStreamText(event)
		if text == "" {
			continue
		}
		delivered = true
		chunk := map[string]any{
			"id": "chatcmpl-" + reqID24(), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": oaiReq.Model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
		}
		b, _ := json.Marshal(chunk)
		if _, err := w.Write(append([]byte("data: "), append(b, '\n', '\n')...)); err != nil {
			return nil
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	// 收尾 chunk。
	end := map[string]any{
		"id": "chatcmpl-" + reqID24(), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": oaiReq.Model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	}
	b, _ := json.Marshal(end)
	_, _ = w.Write(append([]byte("data: "), append(b, '\n', '\n')...))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取上游响应流失败: %w", err)
	}
	if !delivered {
		return nil
	}
	return nil
}

// claudeStopReasonToOpenAI 映射 Claude stop_reason → OpenAI finish_reason。
func claudeStopReasonToOpenAI(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

// extractClaudeStreamText 从 Claude SSE 事件中提取增量文本。
// event 可能包含多个 "event:...\ndata:{...}\n\n" 块，需逐个扫描所有 data 行，
// 只取 content_block_delta + text_delta 的增量文本（首个匹配）。
func extractClaudeStreamText(event []byte) string {
	s := string(event)
	for {
		idx := strings.Index(s, "data: ")
		if idx < 0 {
			return ""
		}
		rest := s[idx+len("data: "):]
		// 该 data 行到下一个换行为止（SSE 单行 data）。
		lineEnd := strings.IndexByte(rest, '\n')
		var dataStr string
		if lineEnd < 0 {
			dataStr = strings.TrimSpace(rest)
			s = ""
		} else {
			dataStr = strings.TrimSpace(rest[:lineEnd])
			s = rest[lineEnd:]
		}
		var payload struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
			continue
		}
		if payload.Type != "content_block_delta" || payload.Delta.Type != "text_delta" {
			continue
		}
		return payload.Delta.Text
	}
}

// reqID24 生成本请求 ID（简单实现）。
func reqID24() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
