package opencode

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 免费层门控只要求工具名里出现 "read"/"shell"（参考实现实测：名字不对/数量不足
// → 403 FreeTierError）。只注入这两个最小工具，避免给模型塞一堆无关工具。
// 定义与 opencode-proxy 的 worker.js DISGUISE_* 对齐。

// disguiseChatTools 是 chat 格式的伪装工具（read/shell）。
var disguiseChatTools = func() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "read",
				"description": "Read a file. Use this instead of cat. Prefer this tool over shell for reading files.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"filePath": map[string]interface{}{"type": "string", "description": "Path to the file to read."},
						"offset":   map[string]interface{}{"type": "number", "description": "Line offset to start reading from."},
						"limit":    map[string]interface{}{"type": "number", "description": "Number of lines to read."},
					},
					"required": []string{"filePath"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "shell",
				"description": "Run a shell command. Prefer the read tool for reading files.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{"type": "string", "description": "Command to run."},
						"workdir": map[string]interface{}{"type": "string", "description": "Working directory."},
						"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds."},
					},
					"required": []string{"command"},
				},
			},
		},
	}
}()

// disguiseResponsesTools 是 Responses API 格式的伪装工具。
var disguiseResponsesTools = func() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(disguiseChatTools))
	for _, t := range disguiseChatTools {
		fn, _ := t["function"].(map[string]interface{})
		out = append(out, map[string]interface{}{
			"type":        "function",
			"name":        fn["name"],
			"description": fn["description"],
			"parameters":  fn["parameters"],
		})
	}
	return out
}()

// openAITools 从请求体里取 tools 数组（兼容 chat 与 responses 两种形状）。
func bodyTools(body map[string]interface{}) []interface{} {
	if t, ok := body["tools"].([]interface{}); ok {
		return t
	}
	return nil
}

func toolName(t interface{}) string {
	m, ok := t.(map[string]interface{})
	if !ok {
		return ""
	}
	if n, ok := m["name"].(string); ok && n != "" {
		return n
	}
	if fn, ok := m["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			return n
		}
	}
	return ""
}

// sanitizeToolChoice 上游只接受 tool_choice=auto；其余取值透传会 400，直接剥掉。
func sanitizeToolChoice(body map[string]interface{}) {
	if v, ok := body["tool_choice"]; ok && v != "auto" {
		delete(body, "tool_choice")
	}
}

// cloneTools 深拷贝工具数组，避免共享底层 map 被意外修改。
func cloneTools(tools []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		raw, _ := json.Marshal(t)
		var cp map[string]interface{}
		if json.Unmarshal(raw, &cp) == nil {
			out = append(out, cp)
		}
	}
	return out
}

// ensureChatTools 注入 read/shell 伪装工具、把 tool_choice 收敛为 auto、强制流式。
func ensureChatTools(body map[string]interface{}) {
	tools := bodyTools(body)
	if len(tools) == 0 {
		body["tools"] = cloneTools(disguiseChatTools)
	} else {
		names := map[string]bool{}
		for _, t := range tools {
			if n := toolName(t); n != "" {
				names[n] = true
			}
		}
		for _, dt := range disguiseChatTools {
			if n := toolName(dt); !names[n] {
				tools = append(tools, cloneTools([]map[string]interface{}{dt})[0])
				names[n] = true
			}
		}
		body["tools"] = tools
	}
	sanitizeToolChoice(body)
	body["stream"] = true
	so, _ := body["stream_options"].(map[string]interface{})
	if so == nil {
		so = map[string]interface{}{}
	}
	so["include_usage"] = true
	body["stream_options"] = so
}

// toResponsesTool 把 chat 或 responses 形状的工具归一化为 Responses API 形状。
func toResponsesTool(t interface{}) map[string]interface{} {
	m, ok := t.(map[string]interface{})
	if !ok || m["type"] != "function" {
		return nil
	}
	if n, _ := m["name"].(string); n != "" {
		out := map[string]interface{}{
			"type":        "function",
			"name":        n,
			"description": firstNonEmpty(m["description"], ""),
			"parameters":  firstNonNil(m["parameters"], map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		}
		if strict, ok := m["strict"].(bool); ok {
			out["strict"] = strict
		}
		return out
	}
	fn, ok := m["function"].(map[string]interface{})
	if !ok {
		return nil
	}
	name, _ := fn["name"].(string)
	if name == "" {
		return nil
	}
	out := map[string]interface{}{
		"type":        "function",
		"name":        name,
		"description": firstNonEmpty(fn["description"], ""),
		"parameters":  firstNonNil(fn["parameters"], map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
	}
	if strict, ok := fn["strict"].(bool); ok {
		out["strict"] = strict
	}
	return out
}

// ensureResponsesTools 把工具归一化为 Responses 形状、补 read/shell 伪装工具并收敛 tool_choice。
func ensureResponsesTools(body map[string]interface{}) {
	app := bodyTools(body)
	merged := make([]interface{}, 0, len(app)+2)
	names := map[string]bool{}
	for _, t := range app {
		if rt := toResponsesTool(t); rt != nil {
			if n, _ := rt["name"].(string); n != "" {
				names[n] = true
			}
			merged = append(merged, rt)
		}
	}
	for _, dt := range disguiseResponsesTools {
		if n, _ := dt["name"].(string); !names[n] {
			merged = append(merged, cloneTools([]map[string]interface{}{dt})[0])
			names[n] = true
		}
	}
	body["tools"] = merged
	sanitizeToolChoice(body)
	body["stream"] = true
}

// responsesFormatToText 把 chat 的 response_format 转成 Responses 的 text.format；
// 不适用时返回 nil（默认纯文本）。
func responsesFormatToText(rf interface{}) map[string]interface{} {
	m, ok := rf.(map[string]interface{})
	if !ok {
		return nil
	}
	typ, _ := m["type"].(string)
	switch typ {
	case "json_object":
		return map[string]interface{}{"format": map[string]interface{}{"type": "json_object"}}
	case "json_schema":
		js, _ := m["json_schema"].(map[string]interface{})
		if js == nil {
			return nil
		}
		format := map[string]interface{}{
			"type":   "json_schema",
			"name":   firstNonEmpty(js["name"], "response"),
			"schema": firstNonNil(js["schema"], map[string]interface{}{}),
		}
		if strict, ok := js["strict"].(bool); ok {
			format["strict"] = strict
		}
		return map[string]interface{}{"format": format}
	}
	return nil
}

// chatContentToResponses 把 chat 消息 content（string 或 parts 数组）转成
// Responses 的 content parts（input_text / input_image）。无可用内容返回 nil。
func chatContentToResponses(content interface{}) []map[string]interface{} {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []map[string]interface{}{{"type": "input_text", "text": s}}
	}
	parts, ok := content.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(parts))
	for _, raw := range parts {
		switch p := raw.(type) {
		case string:
			if p != "" {
				out = append(out, map[string]interface{}{"type": "input_text", "text": p})
			}
		case map[string]interface{}:
			switch p["type"] {
			case "image_url":
				if img, ok := p["image_url"].(map[string]interface{}); ok {
					if u, ok := img["url"].(string); ok && u != "" {
						out = append(out, map[string]interface{}{"type": "input_image", "image_url": u})
					}
				}
			case "input_image":
				if u, ok := p["image_url"].(string); ok && u != "" {
					out = append(out, map[string]interface{}{"type": "input_image", "image_url": u})
				}
			default:
				if t, ok := p["text"].(string); ok && t != "" {
					out = append(out, map[string]interface{}{"type": "input_text", "text": t})
				}
			}
		}
	}
	return out
}

// chatToResponsesInput 把 OpenAI chat messages 转成 Responses API 的 input 数组。
// 支持多轮工具循环：assistant tool_calls → function_call，tool → function_call_output。
func chatToResponsesInput(messages []interface{}) []map[string]interface{} {
	input := make([]map[string]interface{}, 0, len(messages))
	for _, raw := range messages {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role == "tool" {
			cid, _ := m["tool_call_id"].(string)
			if strings.TrimSpace(cid) == "" {
				cid, _ = m["id"].(string)
			}
			if cid == "" {
				continue
			}
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": cid,
				"output":  stringifyContent(m["content"]),
			})
			continue
		}
		mappedRole := "user"
		switch role {
		case "assistant":
			mappedRole = "assistant"
		case "system", "developer":
			mappedRole = "developer"
		}
		contentParts := chatContentToResponses(m["content"])
		if mappedRole == "assistant" {
			var text string
			if s, ok := m["content"].(string); ok {
				text = s
			} else if contentParts != nil {
				var sb strings.Builder
				for _, p := range contentParts {
					if t, ok := p["text"].(string); ok {
						sb.WriteString(t)
					}
				}
				text = sb.String()
			}
			if text != "" {
				input = append(input, map[string]interface{}{"role": mappedRole, "content": text})
			}
		} else if contentParts != nil {
			onlyText := len(contentParts) == 1 && contentParts[0]["type"] == "input_text"
			if onlyText {
				input = append(input, map[string]interface{}{"role": mappedRole, "content": contentParts[0]["text"]})
			} else {
				input = append(input, map[string]interface{}{"role": mappedRole, "content": contentParts})
			}
		}
		if role == "assistant" {
			if tcs, ok := m["tool_calls"].([]interface{}); ok {
				for _, rawTC := range tcs {
					tc, ok := rawTC.(map[string]interface{})
					if !ok {
						continue
					}
					fn, _ := tc["function"].(map[string]interface{})
					if fn == nil {
						fn = map[string]interface{}{}
					}
					name, _ := fn["name"].(string)
					if name == "" {
						if n, ok := tc["name"].(string); ok {
							name = n
						}
					}
					if name == "" {
						continue
					}
					cid, _ := tc["id"].(string)
					if cid == "" {
						cid = fmt.Sprintf("call_chat_%d", len(input))
					}
					input = append(input, map[string]interface{}{
						"type":      "function_call",
						"call_id":   cid,
						"name":      name,
						"arguments": stringifyArgs(fn["arguments"]),
					})
				}
			}
		}
	}
	return input
}

// applySamplingParams 只复制 Responses 上游接受的采样参数（透传未知参数会 400）。
func applySamplingParams(target, src map[string]interface{}) {
	maxOut := firstNonNilAny(
		src["max_output_tokens"],
		src["max_tokens"],
		src["max_completion_tokens"],
	)
	if n, ok := maxOut.(float64); ok && n > 0 {
		target["max_output_tokens"] = int64(n)
	}
	for _, k := range []string{"temperature", "top_p", "presence_penalty", "frequency_penalty"} {
		if v, ok := src[k]; ok && v != nil {
			target[k] = v
		}
	}
}

// normalizeResponsesReasoning 把 chat 侧的推理参数（reasoning / reasoning_effort）
// 收敛成 Responses 的 {effort, summary} 形状，并给 muse-spark 补默认 medium。
func normalizeResponsesReasoning(rb, body map[string]interface{}) {
	reasoning, _ := rb["reasoning"].(map[string]interface{})
	if eff, ok := body["reasoning_effort"].(string); ok && eff != "" {
		norm := eff
		if norm == "auto" {
			norm = "medium"
		}
		if norm != "none" {
			if reasoning == nil {
				reasoning = map[string]interface{}{}
			}
			if _, has := reasoning["effort"]; !has {
				reasoning["effort"] = norm
			}
		}
	}
	model, _ := rb["model"].(string)
	if strings.HasPrefix(model, "muse-spark-") && effortIsNone(reasoning) {
		reasoning = map[string]interface{}{"effort": "medium"}
	}
	if reasoning != nil {
		if _, has := reasoning["summary"]; !has {
			reasoning["summary"] = "auto"
		}
		rb["reasoning"] = reasoning
	}
}

func effortIsNone(reasoning map[string]interface{}) bool {
	if reasoning == nil {
		return true
	}
	eff, _ := reasoning["effort"].(string)
	return eff == "" || eff == "none"
}

// translateChatToResponses 把 OpenAI chat 请求体转成 /zen/v1/responses 请求体
// （供 muse-spark 系列免费模型使用）。内部会补 read/shell 伪装工具。
func translateChatToResponses(body map[string]interface{}) map[string]interface{} {
	messages, _ := body["messages"].([]interface{})
	rb := map[string]interface{}{
		"model":  body["model"],
		"input":  chatToResponsesInput(messages),
		"tools":  bodyTools(body),
		"stream": true,
		"store":  false,
	}
	// response_format → text.format（直接透传 response_format 上游会 400）。
	if body["response_format"] != nil && body["text"] == nil {
		if t := responsesFormatToText(body["response_format"]); t != nil {
			rb["text"] = t
		}
	}
	if body["tool_choice"] != nil {
		rb["tool_choice"] = body["tool_choice"]
	}
	applySamplingParams(rb, body)
	normalizeResponsesReasoning(rb, body)
	ensureResponsesTools(rb)
	return rb
}

// unprefixToolName 剥离 Responses 偶发的 "default." 前缀。剥离仅当剥离后的名字
// 在已知工具名单里，避免误伤本名就叫 default.xxx 的工具。
func unprefixToolName(name string, known map[string]bool) string {
	if strings.HasPrefix(name, "default.") {
		stripped := strings.TrimPrefix(name, "default.")
		if known == nil || known[stripped] {
			return stripped
		}
	}
	return name
}

// collectToolNames 收集请求里的全部客户端工具名（含伪装工具）。
func collectToolNames(body map[string]interface{}) map[string]bool {
	names := map[string]bool{"read": true, "shell": true}
	for _, t := range bodyTools(body) {
		if n := toolName(t); n != "" {
			names[n] = true
		}
	}
	return names
}

// usageFromResponses 把 Responses usage 归一化为 OpenAI shape。
type openAIUsage struct {
	PromptTokens        int                    `json:"prompt_tokens"`
	CompletionTokens    int                    `json:"completion_tokens"`
	TotalTokens         int                    `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetailObj `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetailObj struct {
	CachedTokens int `json:"cached_tokens"`
}

// usageToOpenAI 从 Responses 的 usage 对象构造 OpenAI 形状（缺字段保留 0）。
func usageToOpenAI(u map[string]interface{}) *openAIUsage {
	if u == nil {
		return nil
	}
	in := intOf(u["input_tokens"])
	out := intOf(u["output_tokens"])
	total := intOf(u["total_tokens"])
	if total == 0 && (in > 0 || out > 0) {
		total = in + out
	}
	usage := &openAIUsage{PromptTokens: in, CompletionTokens: out, TotalTokens: total}
	if details, ok := u["input_tokens_details"].(map[string]interface{}); ok {
		if cached := intOf(details["cached_tokens"]); cached >= 0 {
			usage.PromptTokensDetails = &promptTokensDetailObj{CachedTokens: cached}
		}
	}
	return usage
}

func intOf(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		f, _ := n.Float64()
		return int(f)
	}
	return 0
}

func stringifyContent(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

func stringifyArgs(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

func firstNonEmpty(vals ...interface{}) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstNonNil(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// firstNonNilAny 返回第一个非 nil 的接口值。
func firstNonNilAny(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func nowUnix() int64 {
	return time.Now().Unix()
}