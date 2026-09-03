package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 端点上游协议类型：openai（OpenAI 兼容，默认）、gemini（Google AI Studio /
// Generative Language API 的 Interactions API）与 vertex（Google Vertex AI 的
// generateContent / streamGenerateContent）。gemini/vertex 上游把 OpenAI chat
// 请求体转换为对应 Google 协议（Interactions 用 snake_case，Vertex 用 camelCase
// generateContent 格式），并把响应转回 OpenAI 格式。
const (
	upstreamTypeOpenAI = "openai"
	upstreamTypeGemini = "gemini"
	upstreamTypeVertex = "vertex"
)

// normalizeUpstreamType 规范化端点上游协议类型：空串/未知值回退 openai，
// 保持旧配置与脏数据兼容。
func normalizeUpstreamType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "gemini", "google", "aistudio", "generativelanguage", "interactions":
		return upstreamTypeGemini
	case "vertex", "vertex-ai", "vertexai", "aiplatform", "google-cloud", "google-vertex":
		return upstreamTypeVertex
	default:
		return upstreamTypeOpenAI
	}
}

// isGeminiUpstream 判断端点是否 Gemini 上游。
func isGeminiUpstream(ep Endpoint) bool {
	return normalizeUpstreamType(ep.UpstreamType) == upstreamTypeGemini
}

// isVertexUpstream 判断端点是否 Vertex AI 上游。
func isVertexUpstream(ep Endpoint) bool {
	return normalizeUpstreamType(ep.UpstreamType) == upstreamTypeVertex
}

// isGoogleAPIKeyUpstream 判断端点的鉴权是否走 X-Goog-Api-Key（Gemini / Vertex 共用）。
func isGoogleAPIKeyUpstream(ep Endpoint) bool {
	t := normalizeUpstreamType(ep.UpstreamType)
	return t == upstreamTypeGemini || t == upstreamTypeVertex
}

// geminiModelsURL 返回 Gemini 上游模型列表地址（GET {base}/v1beta/models）。
func geminiModelsURL(baseURL string) string {
	return geminiAPIPath(baseURL, "/models")
}

// geminiInteractionsURL 返回 Gemini 上游 Interactions API 地址。
func geminiInteractionsURL(baseURL string) string {
	return geminiAPIPath(baseURL, "/interactions")
}

// geminiAPIPath 拼接 Gemini API 路径：baseURL 已含 /v1beta 时直接追加，
// 否则补 /v1beta 前缀（兼容两种填法）。
func geminiAPIPath(baseURL, path string) string {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1beta") {
		return base + path
	}
	return base + "/v1beta" + path
}

// normalizeGeminiBaseURL 归一化 Gemini 上游 baseURL：只补 scheme、去尾部斜杠，
// 不追加 OpenAI 风格的 /v1 版本路径（Gemini 是 /v1beta/interactions 与
// /v1beta/models）。保留用户填写的 v1beta 前缀。
func normalizeGeminiBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

// openAIChatToGemini 将 OpenAI chat.completions 请求体转换为 Gemini
// Interactions API 请求体。映射：
//   - model → model
//   - system 消息 → system_instruction（snake_case 顶层参数）
//   - user/assistant 消息 → input 的 step_list（user_input / model_output，
//     content 为 Gemini Part 数组）
//   - assistant 的 tool_calls → 独立 function_call step；tool 消息 →
//     function_result step
//   - tools → 顶层 tools（function 声明，参数 JSON Schema 直通）
//   - temperature/top_p/top_k/max_tokens/stop → generation_config
//   - response_format json_object → response_format（结构化输出）
//
// Interactions API 无状态（store=false）：历史以 step_list 打包进 input，
// 不依赖服务端会话，与 OpenAI 语义对齐。
func openAIChatToGemini(body map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}

	model, _ := body["model"].(string)
	out["model"] = model

	var steps []interface{}
	var systemText strings.Builder
	// tool_call_id → 函数名映射：function_result 需要 name 与对应的 function_call
	// 一致，OpenAI tool 消息只有 tool_call_id，须从 assistant 消息收集。
	callName := map[string]string{}
	if rawMessages, ok := body["messages"].([]interface{}); ok {
		for _, raw := range rawMessages {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
				for _, tc := range toolCalls {
					tcMap, ok := tc.(map[string]interface{})
					if !ok {
						continue
					}
					fn, _ := tcMap["function"].(map[string]interface{})
					name, _ := fn["name"].(string)
					id, _ := tcMap["id"].(string)
					if name != "" && id != "" {
						callName[id] = name
					}
				}
			}
		}
	}
	if rawMessages, ok := body["messages"].([]interface{}); ok {
		for _, raw := range rawMessages {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			switch role {
			case "system":
				if t := contentToPlainText(msg["content"]); t != "" {
					if systemText.Len() > 0 {
						systemText.WriteString("\n")
					}
					systemText.WriteString(t)
				}
			case "assistant":
				if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]interface{})
						if !ok {
							continue
						}
						fn, _ := tcMap["function"].(map[string]interface{})
						name, _ := fn["name"].(string)
						args, _ := fn["arguments"].(string)
						if name == "" {
							continue
						}
						fc := map[string]interface{}{"type": "function_call", "name": name}
						if id, _ := tcMap["id"].(string); id != "" {
							fc["id"] = id
						}
						if args != "" {
							fc["arguments"] = args
						}
						steps = append(steps, fc)
					}
				}
				if parts := openAIContentToGeminiParts(msg["content"]); len(parts) > 0 {
					steps = append(steps, map[string]interface{}{
						"type":    "model_output",
						"content": parts,
					})
				}
			case "tool":
				if step := openAIToolResultToGeminiStep(msg, callName); step != nil {
					steps = append(steps, step)
				}
			default:
				parts := openAIMessageToGeminiParts(msg)
				if len(parts) == 0 {
					continue
				}
				steps = append(steps, map[string]interface{}{
					"type":    "user_input",
					"content": parts,
				})
			}
		}
	}

	if systemText.Len() > 0 {
		out["system_instruction"] = systemText.String()
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("gemini upstream: no user messages")
	}
	out["input"] = steps

	if tools := openAIToolsToGemini(body["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}
	if tc := openAIToolChoiceToGemini(body["tool_choice"]); tc != nil {
		out["tool_config"] = tc
	}

	if cfg := geminiGenerationConfig(body); len(cfg) > 0 {
		out["generation_config"] = cfg
	}
	if rf := geminiResponseFormat(body["response_format"]); rf != nil {
		out["response_format"] = rf
	}
	// 无状态：历史由网关管理，不落服务端会话。
	out["store"] = false
	if stream, ok := body["stream"].(bool); ok && stream {
		out["stream"] = true
	}
	return out, nil
}

// openAIMessageToGeminiParts 将 OpenAI 消息 content 转换为 Gemini parts 数组。
// 支持：纯文本、文本块、image_url（data URL 内联 / http URL 透传）、
// input_audio 内联、assistant 的 tool_calls（function_call parts）。
func openAIMessageToGeminiParts(msg map[string]interface{}) []interface{} {
	var parts []interface{}

	if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tcMap["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			args, _ := fn["arguments"].(string)
			if name == "" {
				continue
			}
			callID, _ := tcMap["id"].(string)
			fc := map[string]interface{}{"type": "function_call", "name": name}
			if callID != "" {
				fc["id"] = callID
			}
			if args != "" {
				fc["arguments"] = args
			}
			parts = append(parts, fc)
		}
	}

	parts = append(parts, openAIContentToGeminiParts(msg["content"])...)
	return parts
}

// openAIContentToGeminiParts 仅将 OpenAI 消息 content 转换为 Gemini parts
// （不含 tool_calls）。用于 assistant 历史：tool_calls 已拆为独立 function_call
// step，文本 content 单独转为 model_output step。
func openAIContentToGeminiParts(content interface{}) []interface{} {
	var parts []interface{}
	switch c := content.(type) {
	case string:
		if strings.TrimSpace(c) != "" {
			parts = append(parts, map[string]interface{}{"type": "text", "text": c})
		}
	case []interface{}:
		for _, part := range c {
			pm, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			switch pm["type"] {
			case "text":
				if t, ok := pm["text"].(string); ok && t != "" {
					parts = append(parts, map[string]interface{}{"type": "text", "text": t})
				}
			case "image_url":
				if g := openAIImageURLToGeminiPart(pm["image_url"]); g != nil {
					parts = append(parts, g)
				}
			case "input_audio":
				if g := openAIAudioToGeminiPart(pm["input_audio"]); g != nil {
					parts = append(parts, g)
				}
			case "file":
				if g := openAIFileToGeminiPart(pm); g != nil {
					parts = append(parts, g)
				}
			}
		}
	}
	return parts
}

// contentToPlainText 提取消息 content（字符串或文本块数组）的纯文本，用于 system。
func contentToPlainText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var b strings.Builder
		for _, part := range c {
			if pm, ok := part.(map[string]interface{}); ok {
				if t, ok := pm["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	return ""
}

// openAIToolResultToGeminiStep 将 OpenAI tool 消息转换为 Gemini function_result
// step。name 通过 callName 映射（call_id → 函数名），确保与对应 function_call 匹配。
func openAIToolResultToGeminiStep(msg map[string]interface{}, callName map[string]string) map[string]interface{} {
	callID, _ := msg["tool_call_id"].(string)
	text := contentToPlainText(msg["content"])
	if text == "" {
		text = "ok"
	}
	name := callName[callID]
	if name == "" {
		name = callID
	}
	step := map[string]interface{}{
		"type":   "function_result",
		"name":   name,
		"result": []interface{}{map[string]interface{}{"type": "text", "text": text}},
	}
	if callID != "" {
		step["call_id"] = callID
	}
	return step
}

// openAIImageURLToGeminiPart 将 OpenAI image_url 转换为 Gemini inline_data / file_data part。
// data URL 内联 base64；http(s) URL 透传为 file_data。
func openAIImageURLToGeminiPart(imageURL interface{}) map[string]interface{} {
	u := ""
	switch v := imageURL.(type) {
	case string:
		u = v
	case map[string]interface{}:
		if s, ok := v["url"].(string); ok {
			u = s
		}
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return nil
	}
	if strings.HasPrefix(u, "data:") {
		mime, data, ok := parseDataURL(u)
		if !ok {
			return nil
		}
		return map[string]interface{}{
			"type": "inline_data",
			"data": map[string]interface{}{"mime_type": mime, "data": data},
		}
	}
	return map[string]interface{}{
		"type": "file_data",
		"file_data": map[string]interface{}{
			"mime_type": "image/png",
			"uri":       u,
		},
	}
}

// openAIAudioToGeminiPart 将 OpenAI input_audio（base64）转换为 Gemini inline_data。
func openAIAudioToGeminiPart(inputAudio interface{}) map[string]interface{} {
	am, ok := inputAudio.(map[string]interface{})
	if !ok {
		return nil
	}
	data, _ := am["data"].(string)
	format, _ := am["format"].(string)
	if data == "" {
		return nil
	}
	mime := "audio/wav"
	switch format {
	case "mp3":
		mime = "audio/mpeg"
	case "wav":
		mime = "audio/wav"
	case "aac":
		mime = "audio/aac"
	case "flac":
		mime = "audio/flac"
	case "pcm":
		mime = "audio/L16"
	case "opus":
		mime = "audio/opus"
	}
	return map[string]interface{}{
		"type": "inline_data",
		"data": map[string]interface{}{"mime_type": mime, "data": data},
	}
}

// openAIFileToGeminiPart 将 OpenAI file part（url）转换为 Gemini file_data。
func openAIFileToGeminiPart(pm map[string]interface{}) map[string]interface{} {
	fm, ok := pm["file"].(map[string]interface{})
	if !ok {
		return nil
	}
	uri, _ := fm["file_id"].(string)
	mime, _ := fm["mime_type"].(string)
	if uri == "" {
		return nil
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	return map[string]interface{}{
		"type": "file_data",
		"file_data": map[string]interface{}{
			"mime_type": mime,
			"uri":       uri,
		},
	}
}

// parseDataURL 解析 data:<mime>;base64,<data> URL。
func parseDataURL(raw string) (mime, data string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(raw, prefix) {
		return "", "", false
	}
	rest := raw[len(prefix):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", "", false
	}
	meta := rest[:comma]
	payload := rest[comma+1:]
	mime = "application/octet-stream"
	for _, seg := range strings.Split(meta, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if strings.EqualFold(seg, "base64") {
			continue
		}
		mime = seg
	}
	if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
		return "", "", false
	}
	return mime, payload, true
}

// openAIToolsToGemini 将 OpenAI tools 转换为 Gemini tools（function 声明）。
// OpenAI 的 {type:"function", function:{name,description,parameters}} 转为
// {type:"function", name, description, parameters}（参数 JSON Schema 直通）。
func openAIToolsToGemini(tools interface{}) []interface{} {
	rawTools, ok := tools.([]interface{})
	if !ok {
		return nil
	}
	var out []interface{}
	for _, raw := range rawTools {
		tm, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		gTool := map[string]interface{}{
			"type": "function",
			"name": name,
		}
		if desc, ok := fn["description"].(string); ok && desc != "" {
			gTool["description"] = desc
		}
		if params, ok := fn["parameters"]; ok && params != nil {
			gTool["parameters"] = params
		}
		out = append(out, gTool)
	}
	return out
}

// openAIToolChoiceToGemini 将 OpenAI tool_choice 映射为 Gemini function_calling_config。
// 未显式指定 tool_choice 时返回 nil（不附加 tool_config），避免 Gemini 拒绝
// 未知参数。
func openAIToolChoiceToGemini(toolChoice interface{}) map[string]interface{} {
	if toolChoice == nil {
		return nil
	}
	switch tc := toolChoice.(type) {
	case string:
		switch tc {
		case "none":
			return map[string]interface{}{
				"function_calling_config": map[string]interface{}{"mode": "NONE"},
			}
		case "required":
			return map[string]interface{}{
				"function_calling_config": map[string]interface{}{"mode": "ANY"},
			}
		case "auto":
			return map[string]interface{}{
				"function_calling_config": map[string]interface{}{"mode": "AUTO"},
			}
		default:
			return nil
		}
	case map[string]interface{}:
		if t, ok := tc["type"].(string); ok && t == "function" {
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok && name != "" {
					return map[string]interface{}{
						"function_calling_config": map[string]interface{}{
							"mode":                   "ANY",
							"allowed_function_names": []interface{}{name},
						},
					}
				}
			}
		}
	}
	return nil
}

// geminiGenerationConfig 从 OpenAI 请求体提取 Gemini generation_config（snake_case）。
func geminiGenerationConfig(body map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	if v, ok := body["temperature"].(float64); ok {
		cfg["temperature"] = v
	}
	if v, ok := body["top_p"].(float64); ok {
		cfg["top_p"] = v
	}
	if v, ok := body["top_k"].(float64); ok {
		cfg["top_k"] = v
	}
	if v, ok := body["max_tokens"].(float64); ok {
		cfg["max_output_tokens"] = int(v)
	}
	if v, ok := body["max_completion_tokens"].(float64); ok {
		cfg["max_output_tokens"] = int(v)
	}
	if v, ok := body["stop"].([]interface{}); ok && len(v) > 0 {
		var stops []string
		for _, s := range v {
			if str, ok := s.(string); ok && str != "" {
				stops = append(stops, str)
			}
		}
		if len(stops) > 0 {
			cfg["stop_sequences"] = stops
		}
	} else if v, ok := body["stop"].(string); ok && v != "" {
		cfg["stop_sequences"] = []string{v}
	}
	if len(cfg) == 0 {
		return nil
	}
	return cfg
}

// geminiResponseFormat 将 OpenAI response_format（json_object / json_schema）映射为
// Gemini 结构化输出 response_format。
func geminiResponseFormat(rf interface{}) map[string]interface{} {
	rfm, ok := rf.(map[string]interface{})
	if !ok {
		return nil
	}
	rfType, _ := rfm["type"].(string)
	switch rfType {
	case "json_object":
		return map[string]interface{}{
			"type":      "text",
			"mime_type": "application/json",
		}
	case "json_schema":
		schema, ok := rfm["json_schema"].(map[string]interface{})
		if !ok {
			return map[string]interface{}{
				"type":      "text",
				"mime_type": "application/json",
			}
		}
		if s, ok := schema["schema"].(map[string]interface{}); ok {
			return map[string]interface{}{
				"type":      "text",
				"mime_type": "application/json",
				"schema":    s,
			}
		}
	}
	return nil
}

// geminiToOpenAIChat 将 Gemini Interactions API 非流式响应转换为 OpenAI
// chat.completions 响应。steps 中的 model_output 文本拼接为 content，
// function_call 步骤归并为 tool_calls。
func geminiToOpenAIChat(body []byte, fallbackModel string) ([]byte, error) {
	var resp struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Status  string          `json:"status"`
		Created string          `json:"created"`
		Usage   geminiUsage     `json:"usage"`
		Steps   []geminiStep    `json:"steps"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	model := resp.Model
	if model == "" {
		model = fallbackModel
	}
	created := time.Now().Unix()
	if resp.Created != "" {
		if t, err := time.Parse(time.RFC3339, resp.Created); err == nil {
			created = t.Unix()
		}
	}

	message := map[string]interface{}{
		"role":    "assistant",
		"content": "",
	}
	var toolCalls []interface{}
	finishReason := "stop"
	if resp.Status == "requires_action" {
		finishReason = "tool_calls"
	}
	for _, step := range resp.Steps {
		switch step.Type {
		case "model_output":
			var b strings.Builder
			for _, part := range step.Content {
				if part.Type == "text" && part.Text != "" {
					b.WriteString(part.Text)
				}
			}
			if b.Len() > 0 {
				if message["content"].(string) != "" {
					message["content"] = message["content"].(string) + "\n" + b.String()
				} else {
					message["content"] = b.String()
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   step.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      step.Name,
					"arguments": step.Arguments,
				},
			})
		}
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	usage := map[string]interface{}{
		"prompt_tokens":     resp.Usage.TotalInputTokens,
		"completion_tokens": resp.Usage.TotalOutputTokens,
		"total_tokens":      resp.Usage.TotalTokens,
	}
	if resp.Usage.TotalTokens == 0 {
		usage["total_tokens"] = resp.Usage.TotalInputTokens + resp.Usage.TotalOutputTokens
	}
	if resp.Usage.TotalCachedTokens > 0 {
		usage["prompt_tokens_details"] = map[string]interface{}{"cached_tokens": resp.Usage.TotalCachedTokens}
	}

	out := map[string]interface{}{
		"id":      openAICompletionID(resp.ID),
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
	return json.Marshal(out)
}

type geminiUsage struct {
	TotalTokens        int `json:"total_tokens"`
	TotalInputTokens   int `json:"total_input_tokens"`
	TotalOutputTokens  int `json:"total_output_tokens"`
	TotalCachedTokens  int `json:"total_cached_tokens"`
	TotalThoughtTokens int `json:"total_thought_tokens"`
}

type geminiStep struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// openAICompletionID 生成稳定的 OpenAI 风格响应 ID。
func openAICompletionID(base string) string {
	if base == "" {
		return "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "chatcmpl-" + base
}

// geminiErrorToOpenAI 将 Gemini 上游错误响应（JSON）转换为 OpenAI 错误格式。
func geminiErrorToOpenAI(body []byte) []byte {
	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		out, _ := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"message": parsed.Error.Message,
				"type":    parsed.Error.Status,
				"code":    parsed.Error.Code,
			},
		})
		return out
	}
	return body
}

// geminiInteractionSSETransformer 将 Gemini Interactions API 流式 SSE 事件
// 转换为 OpenAI chat.completions 流式 chunk。事件流：
// interaction.created / status_update / step.start / step.delta / step.stop /
// interaction.completed / done。step.delta 为增量文本；interaction.completed
// 携带 usage，转换为最终 chunk（finish_reason + usage）。
type geminiInteractionSSETransformer struct {
	model    string
	id       string
	created  int64
	started  bool
	finished bool
	doneSent bool
	sawTool  bool
	usage    *geminiUsage
}

func newGeminiInteractionSSETransformer(model string) *geminiInteractionSSETransformer {
	return &geminiInteractionSSETransformer{
		model:   model,
		id:      openAICompletionID(""),
		created: time.Now().Unix(),
	}
}

func (t *geminiInteractionSSETransformer) chunk(delta map[string]interface{}, finish string) []byte {
	payload := map[string]interface{}{
		"id":      t.id,
		"object":  "chat.completion.chunk",
		"created": t.created,
		"model":   t.model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}
	if finish != "" {
		payload["choices"] = []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			},
		}
	}
	data, _ := json.Marshal(payload)
	return append([]byte("data: "), append(data, '\n', '\n')...)
}

// consume 处理一个 Gemini SSE data 行，返回需要写出的 OpenAI chunk 字节。
func (t *geminiInteractionSSETransformer) consume(data []byte) [][]byte {
	if t.finished {
		return nil
	}
	var ev struct {
		EventType string `json:"event_type"`
		Step      struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"step"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Interaction struct {
			ID      string      `json:"id"`
			Status  string      `json:"status"`
			Usage   *geminiUsage `json:"usage"`
			Model   string      `json:"model"`
		} `json:"interaction"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil
	}

	var out [][]byte
	switch ev.EventType {
	case "interaction.created":
		if ev.Interaction.ID != "" {
			t.id = openAICompletionID(ev.Interaction.ID)
		}
		if ev.Interaction.Model != "" {
			t.model = ev.Interaction.Model
		}
		// 立即输出 role chunk：确认流已建立，避免思考模型长时间无输出时
		// 客户端误以为连接挂死（TTFB 提前到首事件）。
		if !t.started {
			t.started = true
			out = append(out, t.chunk(map[string]interface{}{"role": "assistant"}, ""))
		}
	case "step.delta":
		if ev.Delta.Type == "text" && ev.Delta.Text != "" {
			out = append(out, t.chunk(map[string]interface{}{"content": ev.Delta.Text}, ""))
		}
	case "step.complete":
		// 工具调用步骤：Interaction API 在步骤结束时下发完整 function_call
		// step（id/name/arguments）。每次下发都输出对应 tool_calls delta，
		// 支持并行工具调用（可能多次 step.complete）；sawTool 仅用于标记
		// interaction.completed 的 requires_action 是否以 tool_calls 收尾。
		if ev.Step.Type == "function_call" && ev.Step.Name != "" {
			t.sawTool = true
			args := ev.Step.Arguments
			if args == "" {
				args = "{}"
			}
			toolID := ev.Step.ID
			if toolID == "" {
				toolID = "call_" + strconv.FormatInt(time.Now().UnixNano(), 10)
			}
			out = append(out, t.chunk(map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"index": 0,
						"id":    toolID,
						"type":  "function",
						"function": map[string]interface{}{
							"name":      ev.Step.Name,
							"arguments": args,
						},
					},
				},
			}, ""))
		}
	case "interaction.completed":
		if ev.Interaction.Usage != nil {
			t.usage = ev.Interaction.Usage
		}
		usage := map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		}
		if t.usage != nil {
			usage["prompt_tokens"] = t.usage.TotalInputTokens
			usage["completion_tokens"] = t.usage.TotalOutputTokens
			if t.usage.TotalTokens > 0 {
				usage["total_tokens"] = t.usage.TotalTokens
			} else {
				usage["total_tokens"] = t.usage.TotalInputTokens + t.usage.TotalOutputTokens
			}
			if t.usage.TotalCachedTokens > 0 {
				usage["prompt_tokens_details"] = map[string]interface{}{"cached_tokens": t.usage.TotalCachedTokens}
			}
		}
		finish := "stop"
		if ev.Interaction.Status == "requires_action" && t.sawTool {
			finish = "tool_calls"
		}
		out = append(out, t.finalChunk(usage, finish))
		t.finished = true
	}
	return out
}

func (t *geminiInteractionSSETransformer) finalChunk(usage map[string]interface{}, finish string) []byte {
	payload := map[string]interface{}{
		"id":      t.id,
		"object":  "chat.completion.chunk",
		"created": t.created,
		"model":   t.model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finish,
			},
		},
		"usage": usage,
	}
	data, _ := json.Marshal(payload)
	return append([]byte("data: "), append(data, '\n', '\n')...)
}

// finish 在流结束（上游 done 或读中断）时补发收尾，保证 OpenAI 流式
// 有 finish_reason 与 usage，并总是以 [DONE] 收尾（与 OpenAI 协议一致）。
// interaction.completed 已输出 final chunk 时仅补 [DONE]。
func (t *geminiInteractionSSETransformer) finish() [][]byte {
	var out [][]byte
	if !t.finished {
		t.finished = true
		usage := map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		}
		if t.usage != nil {
			usage["prompt_tokens"] = t.usage.TotalInputTokens
			usage["completion_tokens"] = t.usage.TotalOutputTokens
			if t.usage.TotalTokens > 0 {
				usage["total_tokens"] = t.usage.TotalTokens
			} else {
				usage["total_tokens"] = t.usage.TotalInputTokens + t.usage.TotalOutputTokens
			}
		}
		out = append(out, t.finalChunk(usage, "stop"))
	}
	if !t.doneSent {
		t.doneSent = true
		out = append(out, []byte("data: [DONE]\n\n"))
	}
	return out
}

// geminiModelsList 从 GET /v1beta/models 响应中提取模型 id 列表（剥离 models/ 前缀）。
// 仅保留支持内容生成的模型（supportedGenerationMethods 含 generateContent 系列），
// 并排除语音合成/转录/实时/Agent 等非文本对话类模型，避免客户端选到调用即 404 的模型。
func geminiModelsList(body []byte) []string {
	var parsed struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	out := []string{}
	for _, m := range parsed.Models {
		if !geminiGenerativeModel(m.SupportedGenerationMethods) {
			continue
		}
		name := strings.TrimPrefix(m.Name, "models/")
		if name == "" || geminiExcludedModel(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// geminiGenerativeModel 判断模型是否支持内容生成（可经 Interactions API 对话）。
func geminiGenerativeModel(methods []string) bool {
	for _, m := range methods {
		switch m {
		case "generateContent", "streamGenerateContent", "bidiGenerateContent", "predict":
			return true
		}
	}
	return false
}

// geminiExcludedModel 排除非文本对话类模型：语音合成/转录、实时 Live、
// 计算机使用/机器人、音乐生成、Deep Research Agent（需 background 语义）。
func geminiExcludedModel(name string) bool {
	lower := strings.ToLower(name)
	for _, seg := range []string{"-tts", "-transcribe", "-live", "-computer-use", "-robotics"} {
		if strings.Contains(lower, seg) {
			return true
		}
	}
	if strings.HasPrefix(lower, "lyria-") || strings.HasPrefix(lower, "deep-research-") {
		return true
	}
	return false
}

// validateGeminiURL 校验 Gemini baseURL 为 http(s)。
func validateGeminiURL(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("gemini upstream base url must be http(s)")
	}
	return nil
}

// geminiLineReader 从 SSE 字节流中按行切分，支持跨 Read 的部分行缓冲。
// 每行读取复用 readWithIdleTimeout（中段停滞超时并关闭底层连接），
// 防止上游流中途挂死时请求无限阻塞。首块（firstChunk）以 pending 前缀接入，
// 保证首字等待阶段读到的半个 SSE 行也能被完整拼回。
type geminiLineReader struct {
	r       io.Reader
	pending []byte
}

func newGeminiLineReader(r io.Reader, firstChunk []byte) *geminiLineReader {
	g := &geminiLineReader{r: r}
	if len(firstChunk) > 0 {
		g.pending = append(g.pending, firstChunk...)
	}
	return g
}

// readLine 返回下一个完整行（不含换行符）；流结束返回 io.EOF。
func (g *geminiLineReader) readLine(ctx context.Context, idle time.Duration) ([]byte, error) {
	for {
		if i := bytes.IndexByte(g.pending, '\n'); i >= 0 {
			line := g.pending[:i]
			g.pending = g.pending[i+1:]
			return line, nil
		}
		buf := make([]byte, 8192)
		n, err := readWithIdleTimeout(ctx, g.r, buf, idle)
		if n > 0 {
			g.pending = append(g.pending, buf[:n]...)
			continue
		}
		if err != nil {
			if len(g.pending) > 0 {
				line := g.pending
				g.pending = nil
				return line, nil
			}
			return nil, io.EOF
		}
	}
}
