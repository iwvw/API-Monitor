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

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func newScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	return sc
}

// openAIMessage 是 OpenAI chat.completions 请求的消息项。
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// openAIToolCall 表示一条 OpenAI 工具调用信息。
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAITool 表示请求中定义的工具。
type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
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
		// 上游拉取失败时返回非 2xx，而不是 200 + 空列表：网关侧 listModelsWithPricing
		// 会把「验证成功但空列表」当成真实空并覆盖已获取的模型，导致端点不稳定时
		// 模型列表被清空（加前缀后仍需手动刷新）。返回错误让网关保留旧模型列表。
		response.JSON(w, http.StatusBadGateway, map[string]interface{}{
			"object": "list",
			"error":  err.Error(),
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
		outID = s.prefixModel(outID)
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
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   *int            `json:"max_tokens"`
	Temperature *float64        `json:"temperature"`
	TopP        *float64        `json:"top_p"`
	Stream      bool            `json:"stream"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
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

	// 转换 tools
	if len(oai.Tools) > 0 {
		claude.Tools = make([]engineag.ClaudeTool, 0, len(oai.Tools))
		for _, t := range oai.Tools {
			if t.Type != "function" && t.Type != "" {
				continue
			}
			name := strings.TrimSpace(t.Function.Name)
			if name == "" {
				continue
			}
			schema := t.Function.Parameters
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			} else {
				schema = engineag.CleanJSONSchema(schema)
			}
			claude.Tools = append(claude.Tools, engineag.ClaudeTool{
				Name:        name,
				Description: t.Function.Description,
				InputSchema: schema,
			})
		}
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
			claude.Messages = append(claude.Messages, engineag.ClaudeMessage{Role: "assistant", Content: buildAssistantClaudeContent(m)})
		case "tool", "function":
			claude.Messages = append(claude.Messages, engineag.ClaudeMessage{Role: "user", Content: buildToolResultClaudeContent(m)})
		}
	}
	if len(systemParts) > 0 {
		b, _ := json.Marshal(strings.Join(systemParts, "\n\n"))
		claude.System = b
	}
	return claude, nil
}

// buildAssistantClaudeContent 构建 assistant 消息的 Claude content（支持 text + tool_use 混合）。
func buildAssistantClaudeContent(m openAIMessage) json.RawMessage {
	if len(m.ToolCalls) == 0 {
		return buildClaudeContent(m.Content)
	}
	var blocks []map[string]any
	text := messageText(m.Content)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for _, tc := range m.ToolCalls {
		var input any
		if strings.TrimSpace(tc.Function.Arguments) != "" {
			var parsed any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
				input = parsed
			} else {
				input = tc.Function.Arguments
			}
		}
		if input == nil {
			input = map[string]any{}
		}
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%s_%d", tc.Function.Name, time.Now().UnixNano())
		}
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  tc.Function.Name,
			"input": input,
		})
	}
	if len(blocks) == 0 {
		b, _ := json.Marshal("")
		return b
	}
	b, _ := json.Marshal(blocks)
	return b
}

// buildToolResultClaudeContent 构建 tool/function 响应消息的 Claude content（tool_result）。
func buildToolResultClaudeContent(m openAIMessage) json.RawMessage {
	text := messageText(m.Content)
	toolUseID := strings.TrimSpace(m.ToolCallID)
	if toolUseID == "" {
		toolUseID = "tool_call"
	}
	blocks := []map[string]any{
		{
			"type":        "tool_result",
			"tool_use_id": toolUseID,
			"content":     text,
		},
	}
	b, _ := json.Marshal(blocks)
	return b
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
		Index        int           `json:"index"`
		Message      openAIRespMsg `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIRespMsg struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

// forwardOpenAIChat 用 Claude 转发链路处理 OpenAI chat.completions 请求，输出 OpenAI 格式。
// 失败时按选号策略换号重试（最多 maxAccountAttempts 个账号）。
func (s *Service) forwardOpenAIChat(ctx context.Context, w http.ResponseWriter, oaiReq *openAIChatRequest) error {
	claudeReq, err := buildClaudeRequest(oaiReq)
	if err != nil {
		return err
	}
	claudeReq.Stream = oaiReq.Stream

	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return fmt.Errorf("构造客户端失败: %w", err)
	}

	tried := make(map[string]bool, maxAccountAttempts)
	var lastErr error
	for attempt := 0; attempt < maxAccountAttempts; attempt++ {
		acc, ok := s.pickForRelay(tried)
		if !ok {
			break
		}
		tried[acc.Email] = true
		res := s.attemptOpenAIChat(ctx, w, agClient, &acc, oaiReq, claudeReq)
		if res.wrote {
			return res.err
		}
		if res.err == nil {
			s.clearCooldown(acc.Email)
			return nil
		}
		lastErr = res.err
		if !res.retryable {
			return res.err
		}
		s.markCooldown(acc.Email, res.err.Error())
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("尚无可用账号，请先完成 Google 账号授权")
}

// attemptOpenAIChat 用指定账号执行一次 OpenAI 转发；失败且尚未写出任何字节时
// 返回 retryable 供上层换号。wrote=true 表示已开始写响应，不可再重试。
func (s *Service) attemptOpenAIChat(ctx context.Context, w http.ResponseWriter, agClient *engineag.Client, acc *Account, oaiReq *openAIChatRequest, claudeReq *engineag.ClaudeRequest) attemptResult {
	s.incrementCall(acc.Email)
	opts := engineag.DefaultTransformOptions()
	opts.EnableIdentityPatch = true
	geminiBody, err := engineag.TransformClaudeToGeminiWithOptions(claudeReq, acc.ProjectID, s.resolveUpstreamModel(claudeReq.Model), opts)
	if err != nil {
		return attemptResult{err: fmt.Errorf("请求转换失败: %w", err)}
	}
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return attemptResult{retryable: true, err: fmt.Errorf("获取访问凭证失败: %w", err)}
	}

	if !oaiReq.Stream {
		action := "generateContent"
		req, err := engineag.NewAPIRequestWithURL(ctx, forwardBaseURL(acc), action, freshToken, geminiBody)
		if err != nil {
			return attemptResult{err: fmt.Errorf("构造上游请求失败: %w", err)}
		}
		resp, err := agClient.Do(req)
		if err != nil {
			return attemptResult{retryable: true, err: fmt.Errorf("上游请求失败: %w", err)}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			ue := classifyUpstream(resp.StatusCode, string(b))
			return attemptResult{retryable: ue.retryable, err: ue}
		}
		full, err := io.ReadAll(resp.Body)
		if err != nil {
			return attemptResult{retryable: true, err: fmt.Errorf("读取上游响应失败: %w", err)}
		}
		claudeOut, usage, err := engineag.TransformGeminiToClaude(full, claudeReq.Model)
		if err != nil {
			return attemptResult{err: fmt.Errorf("响应转换失败: %w", err)}
		}
		var claudeResp engineag.ClaudeResponse
		if err := json.Unmarshal(claudeOut, &claudeResp); err != nil {
			return attemptResult{err: fmt.Errorf("claude 响应解析失败: %w", err)}
		}

		out := chatCompletionsResponse{
			ID:      "chatcmpl-" + claudeResp.ID,
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   oaiReq.Model,
		}
		var contentStr string
		var toolCalls []openAIToolCall
		for _, c := range claudeResp.Content {
			switch c.Type {
			case "text":
				contentStr += c.Text
			case "tool_use":
				argsBytes, _ := json.Marshal(c.Input)
				tc := openAIToolCall{
					ID:   c.ID,
					Type: "function",
				}
				tc.Function.Name = c.Name
				tc.Function.Arguments = string(argsBytes)
				toolCalls = append(toolCalls, tc)
			}
		}

		respMsg := openAIRespMsg{
			Role:      "assistant",
			ToolCalls: toolCalls,
		}
		if contentStr != "" || len(toolCalls) == 0 {
			respMsg.Content = &contentStr
		}

		finishReason := claudeStopReasonToOpenAI(claudeResp.StopReason)
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}

		out.Choices = append(out.Choices, struct {
			Index        int           `json:"index"`
			Message      openAIRespMsg `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			Index:        0,
			Message:      respMsg,
			FinishReason: finishReason,
		})
		if usage != nil {
			// OpenAI 口径的 prompt_tokens 包含缓存命中部分，而 Claude 的
			// input_tokens 已减掉 cache_read，这里必须加回去（详见流式路径注释）。
			promptTotal := usage.InputTokens + usage.CacheReadInputTokens
			out.Usage.PromptTokens = promptTotal
			out.Usage.CompletionTokens = usage.OutputTokens
			out.Usage.TotalTokens = promptTotal + usage.OutputTokens
			s.recordUsage(acc.Email, claudeReq.Model, int64(promptTotal), int64(usage.OutputTokens), int64(usage.CacheReadInputTokens))
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			return attemptResult{wrote: true, err: err}
		}
		return attemptResult{}
	}

	req, err := engineag.NewAPIRequestWithURL(ctx, forwardBaseURL(acc), "streamGenerateContent", freshToken, geminiBody)
	if err != nil {
		return attemptResult{err: fmt.Errorf("构造上游请求失败: %w", err)}
	}
	resp, err := agClient.Do(req)
	if err != nil {
		return attemptResult{retryable: true, err: fmt.Errorf("上游请求失败: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		ue := classifyUpstream(resp.StatusCode, string(b))
		return attemptResult{retryable: ue.retryable, err: ue}
	}
	return s.pipeOpenAIStream(ctx, w, resp.Body, acc, oaiReq, claudeReq)
}

// pipeOpenAIStream 把上游 Claude SSE 转成 OpenAI SSE 写出。
func (s *Service) pipeOpenAIStream(ctx context.Context, w http.ResponseWriter, body io.Reader, acc *Account, oaiReq *openAIChatRequest, claudeReq *engineag.ClaudeRequest) attemptResult {
	// 必须带 X-Accel-Buffering: no —— 否则 openresty 会缓冲 SSE，
	// 长时间无字节后按 read timeout 给客户端返回 504（见 stream_writer.go）。
	setStreamHeaders(w)

	// 统一用 sseStreamWriter 写回：加写锁、延长写超时、周期性发 ping 保活。
	sw := newSSEStreamWriter(w)
	stopPing := sw.startPing(ctx)
	defer stopPing()

	processor := engineag.NewStreamingProcessor(claudeReq.Model)
	scanner := newScanner(body)
	inTok, outTok := 0, 0
	// 上下文字段：Claude 口径的 input_tokens 不含 cache_read，必须分开累计，
	// 否则记账里缓存永远是 0。
	cacheReadTok, cacheWriteTok := 0, 0
	delivered := false
	wrote := false

	// 工具调用与思考状态追踪
	sawToolCall := false
	toolCallIndex := -1
	currentToolID := ""
	currentToolName := ""
	// 上游判定的致命错误。一旦出现，本轮必须以错误结束，不能再补成功收尾。
	upstreamErr := ""

	for scanner.Scan() {
		line := scanner.Text()
		event := processor.ProcessLine(line)
		if len(event) == 0 {
			continue
		}
		// 提取 Claude SSE 里的 usage（含 cache 读/写，此前只取 input/output，
		// 导致用量面板缓存恒为 0）
		if u, ok := extractClaudeStreamUsage(event); ok {
			if u.InputTokens > 0 {
				inTok = u.InputTokens
			}
			if u.OutputTokens > 0 {
				outTok = u.OutputTokens
			}
			if u.CacheReadTokens > 0 {
				cacheReadTok = u.CacheReadTokens
			}
			if u.CacheCreationTokens > 0 {
				cacheWriteTok = u.CacheCreationTokens
			}
		}

		// 检查是否有 tool_use / tool_delta 事件
		toolChunks := extractClaudeStreamToolCalls(event, &toolCallIndex, &currentToolID, &currentToolName, oaiReq.Model)
		for _, tcChunk := range toolChunks {
			sawToolCall = true
			delivered = true
			wrote = true
			sw.write(append([]byte("data: "), append(tcChunk, '\n', '\n')...))
		}

		// 提取 data 里的 delta 文本
		text := extractClaudeStreamText(event)
		if text != "" {
			delivered = true
			wrote = true
			chunk := map[string]any{
				"id": "chatcmpl-" + reqID24(), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": oaiReq.Model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
			}
			b, _ := json.Marshal(chunk)
			sw.write(append([]byte("data: "), append(b, '\n', '\n')...))
		}

		// thinking delta 也算上游产出的内容，不能因为「只有 reasoning / 纯 thought」就判为空响应。
		if extractClaudeStreamThinking(event) {
			delivered = true
		}

		/**
		 * 上游判定的错误（如 MALFORMED_FUNCTION_CALL → error 事件）必须原样
		 * 传给调用方。
		 *
		 * 此前这里只挑 text / tool_calls / usage 三类，error 事件被静默丢弃，
		 * 于是「上游明确失败」在调用方看来只是「这一轮什么都没输出就结束了」，
		 * 既没有报错也不会重试 —— 与用户报告的「总是中断、查不出原因」一致。
		 *
		 * 以 OpenAI 的 error chunk 形状下发，让 opencode 把它记成这一轮的错误。
		 */
		if msg, ok := extractClaudeStreamError(event); ok {
			delivered = true
			wrote = true
			upstreamErr = msg
			errChunk := map[string]any{
				"error": map[string]any{
					"message": msg,
					"type":    "upstream_error",
				},
			}
			b, _ := json.Marshal(errChunk)
			sw.write(append([]byte("data: "), append(b, '\n', '\n')...))
		}
	}

	/**
	 * 上游已经给出致命错误 → 只补 [DONE]，不再发 finish_reason。
	 *
	 * 发了 finish_reason 就等于告诉调用方「这一轮正常结束了」，与前面的
	 * error chunk 自相矛盾：opencode 会把它当成功轮收尾，于是错误被吞掉，
	 * 用户看到的仍然是「莫名其妙断了」。
	 */
	if upstreamErr != "" {
		sw.write([]byte("data: [DONE]\n\n"))
		return attemptResult{wrote: true, err: fmt.Errorf("%s", upstreamErr)}
	}

	finishReason := "stop"
	if sawToolCall {
		finishReason = "tool_calls"
	}

	/**
	 * 上游一个可解析 payload 都没给 → 不能补发「成功收尾」。
	 *
	 * 真实故障形态：上游流被掐断 / 返回空 body 时，这里过去仍会写出
	 * `finish_reason:"stop"` + `[DONE]`。调用方（opencode）于是把这一轮判成
	 * 「模型正常回了个空回复」——assistant 消息 `content=[]`、`tokens=0`，
	 * 界面上什么都没发生，用户只看到「总是中断」，且没有任何错误可排查。
	 *
	 * 对照 CLIProxyAPI（internal/translator/antigravity/openai/chat-completions/
	 * antigravity_openai_response.go:69-92）的 `SawResponse` 守卫：空上游必须
	 * 保持「可判定为失败」，不能被粉饰成成功的空补全。
	 *
	 * 这里不写任何收尾事件并返回可重试错误，交给 forwardOpenAIChat 换号重试；
	 * 全部账号都失败时错误会如实抛给调用方。
	 */
	if !delivered && !wrote {
		return attemptResult{retryable: true, err: fmt.Errorf("上游未返回任何可解析内容（空响应流）")}
	}

	// 收尾 chunk：携带累计 usage 与正确的 finish_reason，外部消费方/网关可读出消耗与工具状态。
	end := map[string]any{
		"id": "chatcmpl-" + reqID24(), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": oaiReq.Model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}},
	}
	if inTok > 0 || outTok > 0 || cacheReadTok > 0 {
		/**
		 * OpenAI 口径的 prompt_tokens **包含**缓存命中部分（cached_tokens 是它的
		 * 子集），而 Claude/Gemini 口径的 input_tokens 已经把 cache_read 减掉了。
		 * 这里必须加回去，否则：
		 *   - 调用方看到的「输入词元」只有未命中部分（199065 显示成 1551）；
		 *   - 网关按 cached_tokens / prompt_tokens 算命中率会 > 100%，被 clamp 成 100%。
		 * 记账同样用回加后的总量，保证与 OpenAI 兼容插件（geminicli/qoder/workbuddy）
		 * 的「prompt_tokens 含缓存」口径一致。
		 */
		promptTotal := inTok + cacheReadTok
		usageMap := map[string]any{
			"prompt_tokens":     promptTotal,
			"completion_tokens": outTok,
			"total_tokens":      promptTotal + outTok,
		}
		// 缓存字段按 OpenAI 口径透出，调用方（含自建 UI）才能看到命中情况。
		if cacheReadTok > 0 {
			usageMap["prompt_tokens_details"] = map[string]any{"cached_tokens": cacheReadTok}
		}
		if cacheWriteTok > 0 {
			usageMap["cache_creation_input_tokens"] = cacheWriteTok
		}
		end["usage"] = usageMap
		s.recordUsage(acc.Email, claudeReq.Model, int64(promptTotal), int64(outTok), int64(cacheReadTok))
	}
	b, _ := json.Marshal(end)
	sw.write(append([]byte("data: "), append(b, '\n', '\n')...))
	sw.write([]byte("data: [DONE]\n\n"))
	if err := scanner.Err(); err != nil {
		// 流中途出错时可能已写出部分内容：不可换号重试，直接返回。
		if wrote || delivered {
			return attemptResult{wrote: true}
		}
		return attemptResult{retryable: true, err: fmt.Errorf("读取上游响应流失败: %w", err)}
	}
	return attemptResult{wrote: wrote}
}

// extractClaudeStreamToolCalls 从 Claude SSE 事件中解析工具调用并组装成 OpenAI chunk。
// 处理两种 Claude SSE 事件：
// 1. content_block_start: 当 content_block.type == "tool_use" 时，分配新的 index，输出首个 chunk (带 id, name, empty arguments)。
// 2. content_block_delta: 当 delta.type == "input_json_delta" 时，向当前 index 追加 arguments delta。
func extractClaudeStreamToolCalls(event []byte, callIdx *int, curID, curName *string, model string) [][]byte {
	s := string(event)
	var out [][]byte
	for {
		idx := strings.Index(s, "data: ")
		if idx < 0 {
			break
		}
		rest := s[idx+len("data: "):]
		lineEnd := strings.IndexByte(rest, '\n')
		var dataStr string
		if lineEnd < 0 {
			dataStr = strings.TrimSpace(rest)
			s = ""
		} else {
			dataStr = strings.TrimSpace(rest[:lineEnd])
			s = rest[lineEnd:]
		}
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}

		var payload struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
			continue
		}

		switch payload.Type {
		case "content_block_start":
			if payload.ContentBlock.Type == "tool_use" {
				*callIdx++
				*curID = payload.ContentBlock.ID
				*curName = payload.ContentBlock.Name
				chunk := map[string]any{
					"id":      "chatcmpl-" + reqID24(),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"role": "assistant",
								"tool_calls": []any{
									map[string]any{
										"index": *callIdx,
										"id":    *curID,
										"type":  "function",
										"function": map[string]any{
											"name":      *curName,
											"arguments": "",
										},
									},
								},
							},
							"finish_reason": nil,
						},
					},
				}
				b, _ := json.Marshal(chunk)
				out = append(out, b)
			}
		case "content_block_delta":
			if payload.Delta.Type == "input_json_delta" && payload.Delta.PartialJSON != "" {
				chunk := map[string]any{
					"id":      "chatcmpl-" + reqID24(),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []any{
									map[string]any{
										"index": *callIdx,
										"function": map[string]any{
											"arguments": payload.Delta.PartialJSON,
										},
									},
								},
							},
							"finish_reason": nil,
						},
					},
				}
				b, _ := json.Marshal(chunk)
				out = append(out, b)
			}
		}
	}
	return out
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

// extractClaudeStreamThinking 判断 SSE 事件里是否携带 thinking delta（reasoning 产出）。
//
// 长思考模型常以 thought-only 收尾，thinking delta 同样算「上游产出内容」，
// 不能被空响应守卫误判成空回复。
func extractClaudeStreamThinking(event []byte) bool {
	s := string(event)
	for {
		idx := strings.Index(s, "data: ")
		if idx < 0 {
			return false
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
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
			continue
		}
		if payload.Type == "content_block_delta" && payload.Delta.Type == "thinking_delta" {
			return true
		}
	}
}

// extractClaudeStreamError 从 Claude SSE 事件里取值 Anthropic error 事件的 message。
//
// 上游判定的致命收尾（如 MALFORMED_FUNCTION_CALL）会走
// `event: error / data: {"type":"error","error":{...,"message":...}}`。
// 这条事件此前被 pipeOpenAIStream 忽略，必须显式取出来交给调用方。
func extractClaudeStreamError(event []byte) (string, bool) {
	s := string(event)
	for {
		idx := strings.Index(s, "data: ")
		if idx < 0 {
			return "", false
		}
		rest := s[idx+len("data: "):]
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
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
			continue
		}
		if payload.Type != "error" {
			continue
		}
		msg := strings.TrimSpace(payload.Error.Message)
		if msg == "" {
			msg = "上游返回错误"
		}
		return msg, true
	}
}

// streamUsage 是一次 SSE 事件里携带的用量快照。
//
// 必须把 cache 两个字段一起读出来：上游 Gemini 会把「命中上下文的 token」
// 放在 cacheReadInputTokens 里，而 Claude 口径的 input_tokens **不含**这部分。
// 此前只取 input/output，cache 恒为 0 —— 表现为用量面板「缓存全是 0」，
// 看起来像完全没命中缓存，实际是没读。
type streamUsage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}

// extractClaudeStreamUsage 从 Claude SSE 事件中提取 token 用量。
// Claude 在 message_start.message.usage 带 input_tokens（流起点），
// message_delta.usage.output_tokens 累积输出。只处理这两种事件。
func extractClaudeStreamUsage(event []byte) (streamUsage, bool) {
	s := string(event)
	for {
		idx := strings.Index(s, "data: ")
		if idx < 0 {
			return streamUsage{}, false
		}
		rest := s[idx+len("data: "):]
		lineEnd := strings.IndexByte(rest, '\n')
		var dataStr string
		if lineEnd < 0 {
			dataStr = strings.TrimSpace(rest)
			s = ""
		} else {
			dataStr = strings.TrimSpace(rest[:lineEnd])
			s = rest[lineEnd:]
		}
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}
		type usagePayload struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		}
		var payload struct {
			Type    string `json:"type"`
			Message struct {
				Usage usagePayload `json:"usage"`
			} `json:"message,omitempty"`
			Usage usagePayload `json:"usage,omitempty"`
		}
		if json.Unmarshal([]byte(dataStr), &payload) != nil {
			continue
		}
		toStreamUsage := func(u usagePayload) streamUsage {
			return streamUsage{
				InputTokens:         u.InputTokens,
				OutputTokens:        u.OutputTokens,
				CacheReadTokens:     u.CacheReadTokens,
				CacheCreationTokens: u.CacheCreationTokens,
			}
		}
		switch payload.Type {
		case "message_start":
			u := toStreamUsage(payload.Message.Usage)
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
				return u, true
			}
		case "message_delta":
			u := toStreamUsage(payload.Usage)
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 {
				return u, true
			}
		}
	}
}

// reqID24 生成本请求 ID（简单实现）。
func reqID24() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
