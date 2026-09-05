package openai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// responsesMode 描述一个端点如何服务 /v1/responses 请求。
type responsesMode int

const (
	// responsesModeConvert 表示端点不支持原生 /responses，需把 Responses 请求转换为
	// /chat/completions 转发，并把上游 Chat Completions 响应转回 Responses 格式。
	responsesModeConvert responsesMode = iota
	// responsesModePassthrough 表示端点原生支持 /responses（当前仅 DS2API 内部端点），
	// 直接透传请求体与响应流。
	responsesModePassthrough
)

// responsesModeForEndpoint 决定端点服务 Responses API 的方式。
// 转换模式（默认）：把 Responses 请求转成 /chat/completions 转发，适配所有 OpenAI
// 兼容端点（日日新、x666、merge 等第三方通常只实现 /chat/completions）。
// 透传模式：端点原生支持 /responses，直接透传保留完整能力。
//   - DS2API 内部端点原生实现完整 Responses（响应存储、文件检索等）；
//   - Gemini/Vertex 上游不走 OpenAI chat 协议，透传避免发送错误的请求体；
//   - Antigravity 是 Anthropic 兼容中继（仅 /v1/messages），透传避免打 chat 路径。
func (s *Service) responsesModeForEndpoint(ep Endpoint) responsesMode {
	if ep.PluginID == "ds2api" || ep.PluginID == "antigravity" {
		return responsesModePassthrough
	}
	switch normalizeUpstreamType(ep.UpstreamType) {
	case upstreamTypeGemini, upstreamTypeVertex:
		return responsesModePassthrough
	}
	return responsesModeConvert
}

// ==================== 请求转换：Responses → Chat Completions ====================

// responsesRequestToChat 把 OpenAI Responses 请求体转换为 Chat Completions 请求体。
// 覆盖：input（string/数组/items）、instructions、tools、tool_choice、
// parallel_tool_calls、max_output_tokens、reasoning、text.format 及通用采样参数。
// 上游 Chat Completions 不支持的能力（store/previous_response_id/include/web_search 等）
// 在转换时忽略，不影响对话执行。
func responsesRequestToChat(req map[string]interface{}) (map[string]interface{}, error) {
	chat := make(map[string]interface{}, len(req)+2)

	model, _ := req["model"].(string)
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("responses request must include 'model'")
	}
	chat["model"] = model

	if v, ok := req["stream"]; ok {
		chat["stream"] = v
	}

	messages := make([]interface{}, 0, 8)

	// instructions → 前置 system 消息。
	if instr, ok := req["instructions"].(string); ok && strings.TrimSpace(instr) != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": instr})
	}

	conv := responsesInputToMessages(req["input"])
	messages = append(messages, conv...)
	chat["messages"] = messages

	// tools：Responses 的 function 工具（顶层 name/description/parameters）→
	// Chat 的 {type:function, function:{...}}。内置工具（web_search 等）Chat 不支持，
	// 静默忽略，避免把不识别结构发给上游。
	if raw, ok := req["tools"]; ok && raw != nil {
		chat["tools"] = responsesToolsToChat(raw)
	}

	// tool_choice：Responses 格式 → Chat 格式。
	if raw, ok := req["tool_choice"]; ok && raw != nil {
		chat["tool_choice"] = responsesToolChoiceToChat(raw, req)
	}

	if v, ok := req["parallel_tool_calls"]; ok {
		chat["parallel_tool_calls"] = v
	}

	// max_output_tokens → max_tokens（Chat 端点通用字段）。
	if v, ok := req["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}

	// reasoning.effort → reasoning_effort。
	if v, ok := req["reasoning_effort"]; ok {
		chat["reasoning_effort"] = v
	}
	if r, ok := req["reasoning"].(map[string]interface{}); ok {
		if effort, ok := r["effort"]; ok {
			chat["reasoning_effort"] = effort
		}
	}

	// text.format → response_format。
	if text, ok := req["text"].(map[string]interface{}); ok {
		if rf, ok := responsesTextFormatToResponseFormat(text["format"]); ok {
			chat["response_format"] = rf
		}
	}

	// 通用采样参数透传。
	for _, k := range []string{
		"temperature", "top_p", "stop", "presence_penalty", "frequency_penalty",
		"seed", "user", "metadata", "logit_bias",
	} {
		if v, ok := req[k]; ok {
			chat[k] = v
		}
	}

	return chat, nil
}

// responsesInputToMessages 把 Responses 请求的 input 字段转换为 Chat messages 数组。
// input 支持 string、消息对象、标准 items 数组（message/input_text/input_image/
// function_call/function_call_output），并兼容 Chat 风格 tool_calls 消息。
func responsesInputToMessages(input interface{}) []interface{} {
	if input == nil {
		return []interface{}{}
	}

	switch v := input.(type) {
	case string:
		content := strings.TrimSpace(v)
		if content == "" {
			return []interface{}{}
		}
		return []interface{}{map[string]interface{}{"role": "user", "content": content}}
	case []interface{}:
		return normalizeResponsesItems(v)
	case map[string]interface{}:
		if msg := normalizeResponsesItem(v); msg != nil {
			return []interface{}{msg}
		}
		if txt, _ := v["text"].(string); strings.TrimSpace(txt) != "" {
			return []interface{}{map[string]interface{}{"role": "user", "content": txt}}
		}
		return []interface{}{}
	default:
		return []interface{}{}
	}
}

// normalizeResponsesItems 遍历 Responses input items，转换为 Chat messages。
// 独立 function_call items 与 function_call_output 成组缓冲，遇普通消息或结尾时
// 一起 flush：先输出一个含全部已回应 tool_calls 的 assistant 消息，再按序输出
// 各 tool 消息。避免并行工具历史（先全部 calls 再多个 outputs）因逐个 flush
// 提前剔除尚未回应的 call，导致后续 tool 消息无对应 assistant tool_calls。
func normalizeResponsesItems(items []interface{}) []interface{} {
	if len(items) == 0 {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(items))
	var pendingCalls []map[string]interface{}
	var pendingOutputs []map[string]interface{}
	responded := map[string]bool{}

	// flushTools 输出一个含已回应 pendingCalls 的 assistant 消息（Chat 风格
	// tool_calls），随后按序输出各 tool 消息；未回应的 tool_call 防御性剔除。
	flushTools := func() {
		hasCall := false
		calls := make([]interface{}, 0, len(pendingCalls))
		for _, call := range pendingCalls {
			id, _ := call["id"].(string)
			if id != "" && !responded[id] {
				continue
			}
			calls = append(calls, call)
			hasCall = true
		}
		if hasCall {
			out = append(out, map[string]interface{}{
				"role":       "assistant",
				"content":    "",
				"tool_calls": calls,
			})
		}
		for _, t := range pendingOutputs {
			out = append(out, t)
		}
		pendingCalls = pendingCalls[:0]
		pendingOutputs = pendingOutputs[:0]
	}

	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				flushTools()
				out = append(out, map[string]interface{}{"role": "user", "content": s})
			}
			continue
		}
		switch msg["type"] {
		case "function_call":
			if call := responsesFunctionCallToChatCall(msg); call != nil {
				pendingCalls = append(pendingCalls, call)
			}
			continue
		case "function_call_output":
			if callID, _ := msg["call_id"].(string); callID != "" {
				responded[callID] = true
			}
			if tool := responsesFunctionCallOutputToTool(msg); tool != nil {
				pendingOutputs = append(pendingOutputs, tool)
			}
			continue
		}
		flushTools()
		if normalized := normalizeResponsesItem(msg); normalized != nil {
			out = append(out, normalized)
		}
	}
	flushTools()
	return out
}

// normalizeResponsesItem 转换单个 Responses input item 为 Chat message。
// 支持：message（含 content parts 数组）、input_text、input_image、裸消息
// （role/content/text/tool_calls）。assistant 消息的 content 数组提取为文本。
func normalizeResponsesItem(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	role := strings.ToLower(strings.TrimSpace(asString(m["role"])))
	if role != "" {
		if role == "assistant" {
			return normalizeResponsesAssistantMessage(m)
		}
		content := responsesMessageContent(m["content"])
		if content == nil {
			if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
				content = txt
			}
		}
		if content == nil {
			return nil
		}
		out := map[string]interface{}{
			"role":    normalizeOpenAIRole(role),
			"content": content,
		}
		if role == "tool" || role == "function" {
			if callID := asString(m["tool_call_id"]); callID != "" {
				out["tool_call_id"] = callID
			} else if callID := asString(m["call_id"]); callID != "" {
				out["tool_call_id"] = callID
			}
			if name := asString(m["name"]); name != "" {
				out["name"] = name
			}
		}
		return out
	}

	switch itemType := strings.ToLower(strings.TrimSpace(asString(m["type"]))); itemType {
	case "message", "input_message":
		r := strings.ToLower(strings.TrimSpace(asString(m["role"])))
		if r == "" {
			r = "user"
		}
		if r == "assistant" {
			return normalizeResponsesAssistantMessage(m)
		}
		content := responsesMessageContent(m["content"])
		if content == nil {
			if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
				content = txt
			}
		}
		if content == nil {
			return nil
		}
		return map[string]interface{}{
			"role":    normalizeOpenAIRole(r),
			"content": content,
		}
	case "input_text":
		if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
			return map[string]interface{}{"role": "user", "content": txt}
		}
	case "input_image":
		if part := responsesInputImagePart(m); part != nil {
			return map[string]interface{}{
				"role":    "user",
				"content": []interface{}{part},
			}
		}
	case "function_call":
		if call := responsesFunctionCallToChatCall(m); call != nil {
			return map[string]interface{}{
				"role":       "assistant",
				"content":    "",
				"tool_calls": []interface{}{call},
			}
		}
	case "function_call_output":
		if tool := responsesFunctionCallOutputToTool(m); tool != nil {
			return tool
		}
	}
	return nil
}

// normalizeResponsesAssistantMessage 把 Responses 的 assistant 消息（含 tool_calls）
// 转为 Chat 风格。content 数组（output_text/input_text/refusal）提取为文本字符串；
// 已带 tool_calls 的消息原样保留。
func normalizeResponsesAssistantMessage(m map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"role": "assistant"}
	if toolCalls, ok := m["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	content := responsesMessageContent(m["content"])
	if content == nil {
		if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
			content = txt
		}
	}
	if content != nil {
		out["content"] = content
	}
	if reasoning := strings.TrimSpace(asString(m["reasoning_content"])); reasoning != "" {
		out["reasoning_content"] = m["reasoning_content"]
	}
	if _, hasCalls := out["tool_calls"]; hasCalls || out["content"] != nil || out["reasoning_content"] != nil {
		return out
	}
	return nil
}

// responsesMessageContent 把 Responses 消息的 content 转换为 Chat 消息 content。
// 字符串原样返回；parts 数组提取文本并支持图片 part（input_image/image_url）；
// 全部为纯文本时返回拼接字符串，含图片时返回标准 parts 数组。
func responsesMessageContent(content interface{}) interface{} {
	if content == nil {
		return nil
	}
	if s, ok := content.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return s
	}
	parts, ok := content.([]interface{})
	if !ok || len(parts) == 0 {
		return nil
	}
	var text strings.Builder
	hasText := false
	var chatParts []interface{}
	hasNonText := false
	for _, part := range parts {
		pm, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(asString(pm["type"]))) {
		case "text", "input_text", "output_text":
			if t, ok := pm["text"].(string); ok {
				if hasText {
					text.WriteString("\n")
				}
				text.WriteString(t)
				hasText = true
			}
		case "input_image":
			if img := responsesInputImagePart(pm); img != nil {
				chatParts = append(chatParts, img)
				hasNonText = true
			}
		case "image_url":
			if u, ok := pm["image_url"]; ok {
				chatParts = append(chatParts, map[string]interface{}{"type": "image_url", "image_url": u})
				hasNonText = true
			}
		case "refusal":
			if r, ok := pm["refusal"].(string); ok && strings.TrimSpace(r) != "" {
				if hasText {
					text.WriteString("\n")
				}
				text.WriteString(r)
				hasText = true
			}
		}
	}
	if !hasText && !hasNonText {
		return nil
	}
	if !hasNonText {
		return text.String()
	}
	if hasText {
		chatParts = append(chatParts, map[string]interface{}{"type": "text", "text": text.String()})
	}
	return chatParts
}

// responsesInputImagePart 把 Responses 的 input_image part 转换为 Chat 的 image_url part。
func responsesInputImagePart(pm map[string]interface{}) map[string]interface{} {
	if pm == nil {
		return nil
	}
	raw, ok := pm["image_url"]
	if !ok {
		return nil
	}
	// Responses 的 image_url 可能是字符串或对象。
	var img interface{} = raw
	if u, ok := raw.(string); ok {
		img = map[string]interface{}{"url": u}
	}
	if detail := asString(pm["detail"]); detail != "" {
		if m, ok := img.(map[string]interface{}); ok {
			m["detail"] = detail
		}
	}
	return map[string]interface{}{"type": "image_url", "image_url": img}
}

// responsesFunctionCallToChatCall 把 Responses 的 function_call item 转为 Chat 的
// tool_calls 数组元素。arguments 支持 string 或对象，统一序列化为 JSON 字符串。
func responsesFunctionCallToChatCall(m map[string]interface{}) map[string]interface{} {
	name := strings.TrimSpace(asString(m["name"]))
	if name == "" {
		return nil
	}
	id := strings.TrimSpace(asString(m["call_id"]))
	if id == "" {
		id = strings.TrimSpace(asString(m["id"]))
	}
	args := responsesArgumentsString(m["arguments"])
	return map[string]interface{}{
		"id":   id,
		"type": "function",
		"function": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
}

// responsesFunctionCallOutputToTool 把 Responses 的 function_call_output item 转为
// Chat 的 tool 消息。
func responsesFunctionCallOutputToTool(m map[string]interface{}) map[string]interface{} {
	callID := strings.TrimSpace(asString(m["call_id"]))
	if callID == "" {
		callID = strings.TrimSpace(asString(m["tool_call_id"]))
	}
	if callID == "" {
		return nil
	}
	content := m["output"]
	if content == nil {
		content = m["content"]
	}
	if content == nil {
		content = ""
	}
	return map[string]interface{}{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      content,
	}
}

// responsesArgumentsString 把 Responses 的工具参数统一为 JSON 字符串。
func responsesArgumentsString(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "{}"
		}
		return v
	case nil:
		return "{}"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "{}"
		}
		return string(b)
	}
}

// responsesToolsToChat 转换 Responses 工具数组为 Chat 工具数组。仅保留 function 工具，
// 内置工具（web_search 等）与无法识别的结构直接丢弃。
func responsesToolsToChat(raw interface{}) interface{} {
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		toolType := strings.ToLower(strings.TrimSpace(asString(tool["type"])))
		if toolType != "" && toolType != "function" {
			continue
		}
		name := strings.TrimSpace(asString(tool["name"]))
		fn := map[string]interface{}{}
		if name != "" {
			fn["name"] = name
		}
		if desc := strings.TrimSpace(asString(tool["description"])); desc != "" {
			fn["description"] = desc
		}
		if params, ok := tool["parameters"]; ok {
			fn["parameters"] = params
		}
		if len(fn) == 0 {
			continue
		}
		out = append(out, map[string]interface{}{"type": "function", "function": fn})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// responsesToolChoiceToChat 转换 Responses 的 tool_choice 为 Chat 格式。
// 字符串（auto/none/required）原样；{type:function,name} → {type:function,function:{name}}。
// 携带 disable_parallel_tool_calls 时写回请求体的 parallel_tool_calls=false。
func responsesToolChoiceToChat(raw interface{}, req map[string]interface{}) interface{} {
	switch v := raw.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "auto":
			return "auto"
		case "none":
			return "none"
		case "required":
			return "required"
		default:
			return "auto"
		}
	case map[string]interface{}:
		typ := strings.ToLower(strings.TrimSpace(asString(v["type"])))
		name := strings.TrimSpace(asString(v["name"]))
		if typ == "function" || name != "" {
			if dpt, ok := v["disable_parallel_tool_calls"].(bool); ok && dpt {
				req["parallel_tool_calls"] = false
			}
			return map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": name,
				},
			}
		}
		return "auto"
	default:
		return "auto"
	}
}

// responsesTextFormatToResponseFormat 把 Responses 的 text.format 转为 Chat 的
// response_format。json_schema / json_object 支持转换；text 与未知类型返回 nil
// （Chat 默认即文本，无需显式声明）。
func responsesTextFormatToResponseFormat(raw interface{}) (interface{}, bool) {
	if raw == nil {
		return nil, false
	}
	fm, ok := raw.(map[string]interface{})
	if !ok {
		return nil, false
	}
	typ := strings.ToLower(strings.TrimSpace(asString(fm["type"])))
	switch typ {
	case "json_schema":
		schema := map[string]interface{}{
			"type": "json_schema",
		}
		jsonSchema := map[string]interface{}{}
		if name := asString(fm["name"]); name != "" {
			jsonSchema["name"] = name
		}
		if s, ok := fm["schema"]; ok {
			jsonSchema["schema"] = s
		}
		if strict, ok := fm["strict"]; ok {
			jsonSchema["strict"] = strict
		}
		schema["json_schema"] = jsonSchema
		return schema, true
	case "json_object":
		return map[string]interface{}{"type": "json_object"}, true
	default:
		return nil, false
	}
}

// normalizeOpenAIRole 把 Responses 的 role 归一化为 Chat 可接受的 role。
// developer 在多数第三方端点不受支持，降级为 system。
func normalizeOpenAIRole(role string) string {
	switch role {
	case "developer":
		return "system"
	case "function":
		return "tool"
	default:
		return role
	}
}

// ==================== 响应转换（非流式） ====================

// chatResponseToResponses 把上游 Chat Completions 非流式响应转换为 Responses 响应。
// 上游错误响应（含 error 字段）原样透传，仅对成功响应做结构转换。
func chatResponseToResponses(body []byte, model, respID string) ([]byte, error) {
	var chat struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Error   *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role             string  `json:"role"`
				Content          *string `json:"content"`
				ReasoningContent string  `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, err
	}

	// 上游显式错误：直接透传（Responses 与 Chat 的错误结构一致，均为 {"error":{...}}）。
	if chat.Error != nil {
		return body, nil
	}

	// 上游 200 + 空候选（如内容安全拦截）：不能透传 chat 结构，构造空的 Responses 响应。
	if len(chat.Choices) == 0 {
		respModel := model
		if respModel == "" {
			respModel = chat.Model
		}
		if respID == "" {
			respID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		empty := map[string]interface{}{
			"id":          respID,
			"object":      "response",
			"created_at":  time.Now().Unix(),
			"status":      "incomplete",
			"model":       respModel,
			"output":      []interface{}{},
			"output_text": "",
			"usage": map[string]interface{}{
				"input_tokens":  chat.Usage.PromptTokens,
				"output_tokens": chat.Usage.CompletionTokens,
				"total_tokens":  chat.Usage.TotalTokens,
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": chat.Usage.PromptTokensDetails.CachedTokens,
				},
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": chat.Usage.CompletionTokensDetails.ReasoningTokens,
				},
			},
			"error": nil,
		}
		return json.Marshal(empty)
	}

	respModel := model
	if respModel == "" {
		respModel = chat.Model
	}
	if respID == "" {
		respID = "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	createdAt := chat.Created
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}

	msg := chat.Choices[0].Message
	output := make([]interface{}, 0, 2)
	var outputText strings.Builder

	if strings.TrimSpace(msg.ReasoningContent) != "" {
		output = append(output, buildResponsesReasoningItem(msg.ReasoningContent))
	}

	var contentText string
	if msg.Content != nil {
		contentText = *msg.Content
	}
	if contentText != "" {
		output = append(output, map[string]interface{}{
			"id":     "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []interface{}{map[string]interface{}{
				"type":        "output_text",
				"text":        contentText,
				"annotations": []interface{}{},
			}},
		})
		outputText.WriteString(contentText)
	}
	for _, tc := range msg.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			continue
		}
		callID := tc.ID
		if callID == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		output = append(output, map[string]interface{}{
			"id":        "fc_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":      "function_call",
			"status":    "completed",
			"call_id":   callID,
			"name":      name,
			"arguments": normalizeJSONStringForResponses(tc.Function.Arguments),
		})
	}
	if len(output) == 0 {
		// 上游为空响应（可能被安全策略拦截）：给出空 message，避免客户端报错。
		output = append(output, map[string]interface{}{
			"id":     "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []interface{}{map[string]interface{}{
				"type":        "output_text",
				"text":        "",
				"annotations": []interface{}{},
			}},
		})
	}

	usage := map[string]interface{}{
		"input_tokens":  chat.Usage.PromptTokens,
		"output_tokens": chat.Usage.CompletionTokens,
		"total_tokens":  chat.Usage.TotalTokens,
		"input_tokens_details": map[string]interface{}{
			"cached_tokens": chat.Usage.PromptTokensDetails.CachedTokens,
		},
		"output_tokens_details": map[string]interface{}{
			"reasoning_tokens": chat.Usage.CompletionTokensDetails.ReasoningTokens,
		},
	}

	resp := map[string]interface{}{
		"id":          respID,
		"object":      "response",
		"created_at":  createdAt,
		"status":      "completed",
		"model":       respModel,
		"output":      output,
		"output_text": outputText.String(),
		"usage":       usage,
		"error":       nil,
	}
	return json.Marshal(resp)
}

// buildResponsesReasoningItem 构造 Responses 的 reasoning output item。
func buildResponsesReasoningItem(text string) map[string]interface{} {
	return buildResponsesReasoningItemWithID("rsn_"+strings.ReplaceAll(uuid.NewString(), "-", ""), text)
}

// buildResponsesReasoningItemWithID 用指定 id 构造 reasoning output item，
// 保证流式输出中 output_item.done / completed 与 output_item.added 的 item id 一致。
func buildResponsesReasoningItemWithID(id, text string) map[string]interface{} {
	return map[string]interface{}{
		"id":      id,
		"type":    "reasoning",
		"summary": []interface{}{map[string]interface{}{"type": "summary_text", "text": text}},
		"content": []interface{}{map[string]interface{}{"type": "reasoning_text", "text": text}},
		"status":  "completed",
	}
}

// normalizeJSONStringForResponses 规范化工具参数 JSON 字符串：空串补 "{}"，
// 已是合法 JSON 的按紧凑格式重新序列化。
func normalizeJSONStringForResponses(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "{}"
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(b)
}

// ==================== 响应转换（流式） ====================

// chatToolCallDelta 描述一个流式 tool_calls delta（按 index 增量累积）。
type chatToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatToResponsesStreamer 把上游 Chat Completions 流式 SSE（chat.completion.chunk）
// 转换为 OpenAI Responses 流式事件序列。逐 chunk 处理，状态机保证：
//   - 首事件前补 response.created；
//   - 文本增量前补 message item / content part 容器事件；
//   - 工具调用按 index 累积 arguments，done 事件带完整 JSON；
//   - 结束前补 response.completed 与 [DONE]。
type chatToResponsesStreamer struct {
	model  string
	respID string

	createdSent bool
	nextOutput  int
	doneSent    bool

	msgAdded     bool
	msgID        string
	msgOutputIdx int
	msgPartAdded bool
	msgText      strings.Builder

	fnAdded   map[int]bool
	fnIDs     map[int]string
	fnCalls   map[int]string
	fnNames   map[int]string
	fnArgs    map[int]strings.Builder
	fnOutputs map[int]int

	reasoningAdded bool
	reasoningID    string
	reasoningIdx   int
	reasoningText  strings.Builder

	usage    map[string]interface{}
	hasUsage bool

	chatID       string
	chatModel    string
	finishReason string
}

// newChatToResponsesStreamer 创建流式转换器。
func newChatToResponsesStreamer(model string) *chatToResponsesStreamer {
	return &chatToResponsesStreamer{
		model:     model,
		respID:    "resp_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		fnAdded:   map[int]bool{},
		fnIDs:     map[int]string{},
		fnCalls:   map[int]string{},
		fnNames:   map[int]string{},
		fnArgs:    map[int]strings.Builder{},
		fnOutputs: map[int]int{},
	}
}

func (t *chatToResponsesStreamer) allocateOutput() int {
	idx := t.nextOutput
	t.nextOutput++
	return idx
}

// consume 处理一个上游 chat.completion.chunk 事件，返回需写出的 Responses 事件块。
// data 为 chunk 的 data JSON（不含 "data: " 前缀）。
func (t *chatToResponsesStreamer) consume(data []byte) [][]byte {
	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Role             string              `json:"role"`
				Content          string              `json:"content"`
				ReasoningContent string              `json:"reasoning_content"`
				ToolCalls        []chatToolCallDelta `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil
	}
	if chunk.ID != "" {
		t.chatID = chunk.ID
	}
	if chunk.Model != "" {
		t.chatModel = chunk.Model
	}
	if chunk.Usage != nil {
		t.usage = map[string]interface{}{
			"input_tokens":  chunk.Usage.PromptTokens,
			"output_tokens": chunk.Usage.CompletionTokens,
			"total_tokens":  chunk.Usage.TotalTokens,
			"input_tokens_details": map[string]interface{}{
				"cached_tokens": chunk.Usage.PromptTokensDetails.CachedTokens,
			},
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			},
		}
		t.hasUsage = true
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	delta := chunk.Choices[0].Delta
	if fr := chunk.Choices[0].FinishReason; fr != "" {
		t.finishReason = fr
	}

	var outs [][]byte
	outs = append(outs, t.ensureCreated()...)

	if dc := delta.ReasoningContent; dc != "" {
		outs = append(outs, t.emitReasoningDelta(dc)...)
	}
	if dc := delta.Content; dc != "" {
		outs = append(outs, t.emitTextDelta(dc)...)
	}
	for _, tc := range delta.ToolCalls {
		outs = append(outs, t.emitFunctionDelta(tc)...)
	}
	return outs
}

func (t *chatToResponsesStreamer) ensureCreated() [][]byte {
	if t.createdSent {
		return nil
	}
	t.createdSent = true
	respModel := t.model
	if respModel == "" {
		respModel = t.chatModel
	}
	return [][]byte{sseEventBlock("response.created", map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":         t.respID,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"status":     "in_progress",
			"model":      respModel,
			"output":     []interface{}{},
			"usage":      nil,
		},
	})}
}

func (t *chatToResponsesStreamer) emitReasoningDelta(delta string) [][]byte {
	if !t.reasoningAdded {
		t.reasoningAdded = true
		t.reasoningID = "rsn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		t.reasoningIdx = t.allocateOutput()
		idx := t.reasoningIdx
		t.reasoningText.WriteString(delta)
		return [][]byte{
			sseEventBlock("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": idx,
				"item": map[string]interface{}{
					"id":      t.reasoningID,
					"type":    "reasoning",
					"summary": []interface{}{},
					"status":  "in_progress",
				},
			}),
			sseEventBlock("response.content_part.added", map[string]interface{}{
				"type":          "response.content_part.added",
				"item_id":       t.reasoningID,
				"output_index":  idx,
				"content_index": 0,
				"part":          map[string]interface{}{"type": "reasoning_text", "text": ""},
			}),
			sseEventBlock("response.reasoning_text.delta", map[string]interface{}{
				"type":          "response.reasoning_text.delta",
				"item_id":       t.reasoningID,
				"output_index":  idx,
				"content_index": 0,
				"delta":         delta,
			}),
		}
	}
	t.reasoningText.WriteString(delta)
	idx := t.outputIndexOfItem(t.reasoningID)
	return [][]byte{sseEventBlock("response.reasoning_text.delta", map[string]interface{}{
		"type":          "response.reasoning_text.delta",
		"item_id":       t.reasoningID,
		"output_index":  idx,
		"content_index": 0,
		"delta":         delta,
	})}
}

func (t *chatToResponsesStreamer) emitTextDelta(delta string) [][]byte {
	if !t.msgAdded {
		t.msgAdded = true
		t.msgID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		t.msgOutputIdx = t.allocateOutput()
		outs := [][]byte{sseEventBlock("response.output_item.added", map[string]interface{}{
			"type":         "response.output_item.added",
			"output_index": t.msgOutputIdx,
			"item": map[string]interface{}{
				"id":      t.msgID,
				"type":    "message",
				"role":    "assistant",
				"status":  "in_progress",
				"content": []interface{}{},
			},
		})}
		outs = append(outs, sseEventBlock("response.content_part.added", map[string]interface{}{
			"type":          "response.content_part.added",
			"item_id":       t.msgID,
			"output_index":  t.msgOutputIdx,
			"content_index": 0,
			"part":          map[string]interface{}{"type": "output_text", "text": ""},
		}))
		t.msgPartAdded = true
		t.msgText.WriteString(delta)
		return append(outs, sseEventBlock("response.output_text.delta", map[string]interface{}{
			"type":          "response.output_text.delta",
			"item_id":       t.msgID,
			"output_index":  t.msgOutputIdx,
			"content_index": 0,
			"delta":         delta,
		}))
	}
	t.msgText.WriteString(delta)
	return [][]byte{sseEventBlock("response.output_text.delta", map[string]interface{}{
		"type":          "response.output_text.delta",
		"item_id":       t.msgID,
		"output_index":  t.msgOutputIdx,
		"content_index": 0,
		"delta":         delta,
	})}
}

func (t *chatToResponsesStreamer) emitFunctionDelta(tc chatToolCallDelta) [][]byte {
	idx := tc.Index
	var outs [][]byte
	if !t.fnAdded[idx] {
		t.fnAdded[idx] = true
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = t.fnNames[idx]
		}
		if name == "" {
			name = "function"
		}
		t.fnNames[idx] = name
		callID := strings.TrimSpace(tc.ID)
		if callID == "" {
			callID = t.fnCalls[idx]
		}
		if callID == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		t.fnCalls[idx] = callID
		t.fnIDs[idx] = "fc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		t.fnOutputs[idx] = t.allocateOutput()
		outs = append(outs, sseEventBlock("response.output_item.added", map[string]interface{}{
			"type":         "response.output_item.added",
			"output_index": t.fnOutputs[idx],
			"item": map[string]interface{}{
				"id":        t.fnIDs[idx],
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": "",
				"status":    "in_progress",
			},
		}))
	}
	if arg := tc.Function.Arguments; arg != "" {
		b := t.fnArgs[idx]
		b.WriteString(arg)
		t.fnArgs[idx] = b
		outs = append(outs, sseEventBlock("response.function_call_arguments.delta", map[string]interface{}{
			"type":         "response.function_call_arguments.delta",
			"item_id":      t.fnIDs[idx],
			"output_index": t.fnOutputs[idx],
			"call_id":      t.fnCalls[idx],
			"delta":        arg,
		}))
	}
	return outs
}

func (t *chatToResponsesStreamer) outputIndexOfItem(itemID string) int {
	if itemID == t.msgID {
		return t.msgOutputIdx
	}
	for i, id := range t.fnIDs {
		if id == itemID {
			return t.fnOutputs[i]
		}
	}
	if itemID == t.reasoningID {
		return t.reasoningIdx
	}
	return 0
}

// finish 在流结束时补全剩余事件：关闭 reasoning/message/function items，
// 输出 response.completed 与 [DONE]。
func (t *chatToResponsesStreamer) finish() [][]byte {
	if t.doneSent {
		return nil
	}
	t.doneSent = true
	var outs [][]byte
	outs = append(outs, t.closeReasoning()...)
	outs = append(outs, t.closeMessage()...)
	outs = append(outs, t.closeFunctions()...)

	output := []interface{}{}
	var outputText string
	status := "completed"
	if strings.TrimSpace(t.finishReason) == "content_filter" {
		status = "incomplete"
	}
	if t.reasoningAdded {
		output = append(output, buildResponsesReasoningItemWithID(t.reasoningID, t.reasoningText.String()))
	}
	if t.msgAdded {
		output = append(output, map[string]interface{}{
			"id":     t.msgID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []interface{}{map[string]interface{}{
				"type":        "output_text",
				"text":        t.msgText.String(),
				"annotations": []interface{}{},
			}},
		})
		outputText = t.msgText.String()
	}
	for _, i := range t.fnIndexesSorted() {
		output = append(output, map[string]interface{}{
			"id":        t.fnIDs[i],
			"type":      "function_call",
			"status":    "completed",
			"call_id":   t.fnCalls[i],
			"name":      t.fnNames[i],
			"arguments": normalizeJSONStringForResponses(fnArgsString(t.fnArgs[i])),
		})
	}

	respModel := t.model
	if respModel == "" {
		respModel = t.chatModel
	}
	var usage interface{}
	if t.hasUsage {
		usage = t.usage
	}
	resp := map[string]interface{}{
		"id":          t.respID,
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      status,
		"model":       respModel,
		"output":      output,
		"output_text": outputText,
		"usage":       usage,
		"error":       nil,
	}
	outs = append(outs, sseEventBlock("response.completed", map[string]interface{}{
		"type":     "response.completed",
		"response": resp,
	}))
	outs = append(outs, []byte("data: [DONE]\n\n"))
	return outs
}

func (t *chatToResponsesStreamer) closeReasoning() [][]byte {
	if !t.reasoningAdded {
		return nil
	}
	text := t.reasoningText.String()
	idx := t.outputIndexOfItem(t.reasoningID)
	return [][]byte{
		sseEventBlock("response.reasoning_text.done", map[string]interface{}{
			"type":          "response.reasoning_text.done",
			"item_id":       t.reasoningID,
			"output_index":  idx,
			"content_index": 0,
			"text":          text,
		}),
		sseEventBlock("response.content_part.done", map[string]interface{}{
			"type":          "response.content_part.done",
			"item_id":       t.reasoningID,
			"output_index":  idx,
			"content_index": 0,
			"part":          map[string]interface{}{"type": "reasoning_text", "text": text},
		}),
		sseEventBlock("response.output_item.done", map[string]interface{}{
			"type":         "response.output_item.done",
			"output_index": idx,
			"item":         buildResponsesReasoningItemWithID(t.reasoningID, text),
		}),
	}
}

func (t *chatToResponsesStreamer) closeMessage() [][]byte {
	if !t.msgAdded {
		return nil
	}
	text := t.msgText.String()
	outs := [][]byte{}
	if t.msgPartAdded {
		outs = append(outs, sseEventBlock("response.output_text.done", map[string]interface{}{
			"type":          "response.output_text.done",
			"item_id":       t.msgID,
			"output_index":  t.msgOutputIdx,
			"content_index": 0,
			"text":          text,
		}))
		outs = append(outs, sseEventBlock("response.content_part.done", map[string]interface{}{
			"type":          "response.content_part.done",
			"item_id":       t.msgID,
			"output_index":  t.msgOutputIdx,
			"content_index": 0,
			"part":          map[string]interface{}{"type": "output_text", "text": text},
		}))
	}
	outs = append(outs, sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": t.msgOutputIdx,
		"item": map[string]interface{}{
			"id":     t.msgID,
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []interface{}{map[string]interface{}{
				"type": "output_text",
				"text": text,
			}},
		},
	}))
	return outs
}

func (t *chatToResponsesStreamer) closeFunctions() [][]byte {
	outs := [][]byte{}
	for _, i := range t.fnIndexesSorted() {
		args := normalizeJSONStringForResponses(fnArgsString(t.fnArgs[i]))
		outs = append(outs, sseEventBlock("response.function_call_arguments.done", map[string]interface{}{
			"type":         "response.function_call_arguments.done",
			"item_id":      t.fnIDs[i],
			"output_index": t.fnOutputs[i],
			"call_id":      t.fnCalls[i],
			"name":         t.fnNames[i],
			"arguments":    args,
		}))
		outs = append(outs, sseEventBlock("response.output_item.done", map[string]interface{}{
			"type":         "response.output_item.done",
			"output_index": t.fnOutputs[i],
			"item": map[string]interface{}{
				"id":        t.fnIDs[i],
				"type":      "function_call",
				"status":    "completed",
				"call_id":   t.fnCalls[i],
				"name":      t.fnNames[i],
				"arguments": args,
			},
		}))
	}
	return outs
}

// ==================== 辅助 ====================

// NormalizeOpenAIContentForPrompt 计算 content 的非空文本，用于空内容判断。
// 与 ds2api promptcompat 同名函数行为对齐。
func NormalizeOpenAIContentForPrompt(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if arr, ok := v.([]interface{}); ok {
		var b strings.Builder
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				switch strings.ToLower(strings.TrimSpace(asString(m["type"]))) {
				case "text", "input_text", "output_text":
					if t, ok := m["text"].(string); ok {
						b.WriteString(t)
					}
				}
			} else if s, ok := item.(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	}
	return fmt.Sprintf("%v", v)
}

// asString 安全提取 map 字段为字符串。
func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.(json.Number); ok {
		return b.String()
	}
	return fmt.Sprintf("%v", v)
}

// fnArgsString 读取 map 中 strings.Builder 累积的文本（map 索引不可寻址，
// 不能直接调用指针方法，需取出后调用）。
func fnArgsString(b strings.Builder) string {
	return b.String()
}

// fnIndexesSorted 返回已出现的工具调用索引的升序切片。
// 上游并行工具调用的 index 可能非连续（如并行 2 个调用 index=0/2 时），
// 遍历按 index 而非 map 迭代，保证 output 顺序稳定。
func (t *chatToResponsesStreamer) fnIndexesSorted() []int {
	indexes := make([]int, 0, len(t.fnAdded))
	for idx, added := range t.fnAdded {
		if added {
			indexes = append(indexes, idx)
		}
	}
	sort.Ints(indexes)
	return indexes
}
