package openai

import (
	"bufio"
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

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// normalizeResponsesTools 为缺失 name 的工具补充 name（取值等于 type）。
// 上游 zen 用 serde flatten 解析 tools，要求每个工具都带顶层 name；
// OpenAI 官方的 web_search 等工具本身没有 name 字段，补齐避免反序列化失败。
func normalizeResponsesTools(body map[string]interface{}) {
	tools, ok := body["tools"].([]interface{})
	if !ok {
		return
	}
	for _, item := range tools {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := tool["name"]; has {
			continue
		}
		if t, ok := tool["type"].(string); ok && t != "" {
			tool["name"] = t
		}
	}
}

// normalizeResponsesInput 规范化 Responses 请求的 input 列表，兼容 zen 的转换缺陷：
//  1. assistant 消息的 content 数组（output_text 块）在 zen 转 chat 时不被识别，
//     需提取文本为字符串，否则报 "Invalid assistant message: content or tool_calls must be set"。
//  2. input 以 function_call_output 结尾时，zen 转成 chat 的 tool 消息后无后续 user，
//     报 "reasoning_content in the thinking mode must be passed back"，
//     末尾补一条空 user 消息即可通过。
//  3. 独立 function_call items 归并到相邻 assistant 消息的 tool_calls（chat 风格）。
//     zen 对独立 function_call item 的归并不稳定（同样的请求时而 200 时而 400
//     "An assistant message with 'tool_calls' must be followed by tool messages responding
//     to each 'tool_call_id'"），显式归并后可稳定通过。
//  4. assistant 已自带 tool_calls 但后续 function_call_output 不足时（codex 多轮
//     并行工具分步回传：历史 tool_calls 仍含全部 call_id，但部分工具结果尚未返回），
//     zen 转 chat 会报 "insufficient tool messages following tool_calls message"。
//     对未被任何 function_call_output 回应的 tool_call 做防御性剔除，让校验通过。
func normalizeResponsesInput(body map[string]interface{}) {
	input, ok := body["input"].([]interface{})
	if !ok {
		return
	}
	// responded 记录已被 function_call_output 回应的 call_id。归并时从独立
	// function_call item 取 call_id（call_id 优先，回退 id）；assistant 自带
	// tool_calls 的 call_id 也在最终校验阶段核对。
	responded := map[string]bool{}
	normalized := make([]interface{}, 0, len(input))
	var lastAssistant map[string]interface{}
	for _, item := range input {
		msg, ok := item.(map[string]interface{})
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		switch msg["type"] {
		case "function_call":
			// 归并到相邻 assistant 消息的 tool_calls，并丢弃独立 item。
			if lastAssistant == nil {
				// 防御：无前驱 assistant 时原样透传，避免静默丢弃。
				normalized = append(normalized, item)
				continue
			}
			name, _ := msg["name"].(string)
			args, _ := msg["arguments"].(string)
			callID, _ := msg["call_id"].(string)
			if callID == "" {
				callID, _ = msg["id"].(string)
			}
			if name != "" {
				toolCalls, _ := lastAssistant["tool_calls"].([]interface{})
				lastAssistant["tool_calls"] = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": args,
					},
				})
			}
			continue
		case "function_call_output":
			if callID, _ := msg["call_id"].(string); callID != "" {
				responded[callID] = true
			}
		}
		normalized = append(normalized, item)
		if msg["type"] == "message" {
			if role, _ := msg["role"].(string); role == "assistant" {
				lastAssistant = msg
			} else {
				lastAssistant = nil
			}
		}
	}

	// assistant 消息的 content 数组提取为字符串。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		var text strings.Builder
		hasText := false
		for _, part := range contentArr {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if partMap["type"] != "output_text" && partMap["type"] != "input_text" {
				continue
			}
			if t, ok := partMap["text"].(string); ok {
				if hasText {
					text.WriteString("\n")
				}
				text.WriteString(t)
				hasText = true
			}
		}
		if hasText {
			msg["content"] = text.String()
		}
	}

	// 防御性剔除：assistant 已声明但未被任何 function_call_output 回应的 tool_call
	// 会触发 zen 的 "insufficient tool messages following tool_calls message"。codex
	// 多轮并行工具分步回传时历史 tool_calls 含全部 call_id，但部分 output 尚未返回，
	// 这类未回应的调用本轮无法执行，剔除后既满足 zen 校验也不改变对话语义。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}
		kept := toolCalls[:0]
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				kept = append(kept, tc)
				continue
			}
			callID, _ := tcMap["id"].(string)
			if callID != "" && !responded[callID] {
				// 无对应 function_call_output：剔除。
				continue
			}
			kept = append(kept, tc)
		}
		if len(kept) == 0 {
			delete(msg, "tool_calls")
		} else {
			msg["tool_calls"] = kept
		}
	}

	// 末尾补齐：若最后一条是 function_call_output，追加空 user 消息。
	if len(normalized) > 0 {
		if last, ok := normalized[len(normalized)-1].(map[string]interface{}); ok {
			if t, _ := last["type"].(string); t == "function_call_output" {
				normalized = append(normalized, map[string]interface{}{
					"type":    "message",
					"role":    "user",
					"content": "",
				})
			}
		}
	}
	body["input"] = normalized
}

// normalizeChatContentBlocks 把 Anthropic/Claude 或 agent 客户端发送的 content
// blocks 数组归一化为 OpenAI chat.completions 标准格式。上游 zen 的 chat.completions
// 只接受 content 为字符串或 OpenAI 标准 parts，若传入含 {type:"thinking",
// signature:"reasoning_content"} / {type:"toolCall"} / {type:"tool_use"} 等块会直接
// 400。归一化规则：
//   - thinking / reasoning / redacted_thinking：提取 thinking 文本累积到消息顶层
//     reasoning_content，并丢弃该块（避免把 Anthropic signature 传给 zen）。
//   - toolCall / tool-call / tool_use block：转化为标准 tool_calls（id/type/function）。
//     arguments 优先（PI 用对象或字符串），其次 input（Anthropic 用结构化对象）。
//   - text：合并为 content 字符串。
//   - image / image_url：保留为 OpenAI 图片 parts。
//   - tool_result：随 keptParts 原样保留（对应消息已是 role=tool 时由 zen 直接处理）。
//
// 仅当 content 为非空数组且含可识别块时才改写；纯普通图片数组（image_url）不动。
func normalizeChatContentBlocks(body map[string]interface{}) {
	messages, ok := body["messages"].([]interface{})
	if !ok {
		return
	}
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		role, _ := msg["role"].(string)
		var text strings.Builder
		hasText := false
		var reasoning strings.Builder
		hasReasoning := false
		var toolCalls []interface{}
		var keptParts []interface{}
		needsRewrite := false
		for _, part := range contentArr {
			pm, ok := part.(map[string]interface{})
			if !ok {
				keptParts = append(keptParts, part)
				continue
			}
			ptype, _ := pm["type"].(string)
			switch ptype {
			case "text":
				if t, ok := pm["text"].(string); ok {
					if hasText {
						text.WriteString("\n")
					}
					text.WriteString(t)
					hasText = true
				}
				needsRewrite = true
			case "thinking", "reasoning", "redacted_thinking":
				if t := chatContentThinkingText(pm); t != "" {
					if hasReasoning {
						reasoning.WriteString("\n")
					}
					reasoning.WriteString(t)
					hasReasoning = true
				}
				// 丢弃 thinking 块，reasoning 转顶层字段。
				needsRewrite = true
			case "toolCall", "tool-call", "tool_use":
				name, _ := pm["name"].(string)
				callID, _ := pm["id"].(string)
				argsStr := chatContentToolArguments(pm)
				if name != "" {
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   callID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": argsStr,
						},
					})
				}
				needsRewrite = true
			case "image", "image_url":
				// 保持 OpenAI 图片 part 原样。
				keptParts = append(keptParts, part)
			default:
				keptParts = append(keptParts, part)
			}
		}

		if !needsRewrite {
			continue
		}

		var content interface{}
		switch {
		case hasText:
			// 文本合并为首个或唯一 part；若同时含图片/其余 part，则文本作为
			// ContentTextPart 后接其余 part，保证 zen 接受的 OpenAI parts 结构。
			var merged []interface{}
			if len(keptParts) == 0 {
				content = text.String()
			} else {
				merged = append(merged, map[string]interface{}{
					"type": "text",
					"text": text.String(),
				})
				content = append(merged, keptParts...)
			}
		case len(keptParts) > 0:
			content = keptParts
		default:
			if role == "assistant" && len(toolCalls) > 0 {
				content = ""
			} else {
				content = text.String()
			}
		}

		msg["content"] = content
		if hasReasoning {
			msg["reasoning_content"] = reasoning.String()
		}
		if len(toolCalls) > 0 && role == "assistant" {
			msg["tool_calls"] = toolCalls
		}
	}

	// zen 的 thinking 模式下，assistant 消息一旦在 tool 循环中开启思考，之后每轮
	// toolCall 轮次的 assistant 消息都必须携带 reasoning_content（可为空串），否则
	// 上游返回 400 "The `reasoning_content` in the thinking mode must be passed back
	// to the API"。PI 等 agent 客户端在多轮工具调用时可能漏发 thinking 块，这里做
	// 兜底：记录 thinking 是否已开启，对后续缺失 reasoning_content 的 assistant
	// toolCall 消息补空串，满足 zen 的连续传回要求。
	thinkingActive := false
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if _, hasRC := msg["reasoning_content"]; hasRC {
			thinkingActive = true
		}
		if role == "user" || role == "system" {
			// 新一轮用户请求重置思考状态：新的对话轮不要求续传上一轮 reasoning。
			thinkingActive = false
		}
		if thinkingActive && role == "assistant" {
			if _, hasRC := msg["reasoning_content"]; !hasRC {
				if _, hasTC := msg["tool_calls"]; hasTC {
					msg["reasoning_content"] = ""
				}
			}
		}
	}
}

// chatContentThinkingText 提取 thinking/reasoning block 中的文本。PI 用
// {type:"thinking", thinking, signature:"reasoning_content"}，部分 agent 用
// {type:"reasoning", text}；统一兼容 thinking/reasoning_content/text/content。
func chatContentThinkingText(pm map[string]interface{}) string {
	for _, key := range []string{"thinking", "reasoning_content", "text", "content"} {
		if t, ok := pm[key].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// chatContentToolArguments 提取 toolCall block 的参数并序列化为 JSON 字符串。
// PI 用 arguments（对象或字符串），Anthropic 用 input（结构化对象）。
func chatContentToolArguments(pm map[string]interface{}) string {
	if v, ok := pm["arguments"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	if v, ok := pm["input"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return "{}"
}

// sseDataJSON 提取 SSE 事件块中 data: 行的内容。
func sseDataJSON(block []byte) (string, bool) {
	for _, line := range strings.Split(string(block), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
		}
	}
	return "", false
}

// responsesStreamNormalizer 把上游精简的 Responses 流式事件补全为 Codex 可解析的标准事件序列。
// 部分上游（如 zen）直接发 output_text.delta / function_call_arguments.delta，缺少
// output_item.added（message 前导）与 output_item.done（完整 item）事件；Codex 在
// active item 缺失时收到文本 delta 会直接报错（OutputTextDelta without active item），
// 且工具调用的完整 arguments 只从 output_item.done 中读取，缺失会导致调用永远不执行。
// 网关在此补全：文本 delta 前注入 message item 的 added，item 切换与 completed 前注入 done。
type responsesStreamNormalizer struct {
	model       string
	respID      string
	createdSent bool
	msgOpen     bool
	msgID       string
	msgText     strings.Builder
	fnOpen      bool
	fnID        string
	fnName      string
	fnCallID    string
	fnArgs      strings.Builder
}

func newResponsesStreamNormalizer(model string) *responsesStreamNormalizer {
	return &responsesStreamNormalizer{model: model}
}

// sseEventType 解析 SSE 事件块中的 data JSON 的 type 字段。
func sseEventType(block []byte) (string, map[string]interface{}) {
	dataJSON, ok := sseDataJSON(block)
	if !ok {
		return "", nil
	}
	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &ev); err != nil {
		return "", nil
	}
	t, _ := ev["type"].(string)
	return t, ev
}

func sseEventBlock(eventType string, payload interface{}) []byte {
	payloadJSON, _ := json.Marshal(payload)
	out := append([]byte("event: "+eventType+"\ndata: "), payloadJSON...)
	return append(out, []byte("\n\n")...)
}

// transform 处理一个上游事件块，返回需要写出的一个或多个事件块。
func (n *responsesStreamNormalizer) transform(block []byte) [][]byte {
	eventType, ev := sseEventType(block)
	if eventType == "" {
		return [][]byte{block}
	}

	var outs [][]byte

	// 首事件前注入 response.created（若上游未发）。
	if !n.createdSent {
		n.createdSent = true
		if eventType != "response.created" {
			respID := n.respID
			if respID == "" {
				respID = uuid.NewString()
			}
			n.respID = respID
			outs = append(outs, sseEventBlock("response.created", map[string]interface{}{
				"type": "response.created",
				"response": map[string]interface{}{
					"id":         respID,
					"object":     "response",
					"created_at": time.Now().Unix(),
					"status":     "in_progress",
					"model":      n.model,
					"output":     []interface{}{},
					"usage":      nil,
				},
			}))
		}
	}

	switch eventType {
	case "response.created":
		if n.respID == "" {
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				if id, ok := resp["id"].(string); ok {
					n.respID = id
				}
			}
		}
	case "response.output_text.delta":
		// Codex 需要 message item 先建立（active item）才能挂文本 delta。
		if !n.msgOpen {
			n.msgOpen = true
			n.msgID = "msg_" + uuid.NewString()
			outs = append(outs, sseEventBlock("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": 0,
				"item": map[string]interface{}{
					"id":      n.msgID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []interface{}{},
				},
			}))
		}
		if delta, ok := ev["delta"].(string); ok {
			n.msgText.WriteString(delta)
		}
	case "response.output_item.added":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "message":
				n.msgOpen = true
				if id, ok := item["id"].(string); ok {
					n.msgID = id
				}
			case "function_call":
				// 切换 item 前先关闭未完成的 message 与上一个 function_call
				// （上游并行工具调用时会连续发多个 function_call 的 added，
				// 不关闭会导致参数拼进同一个 arguments 变成非法 JSON）。
				outs = append(outs, n.closeMessageIfOpen()...)
				outs = append(outs, n.closeFunctionIfOpen()...)
				n.fnOpen = true
				if id, ok := item["id"].(string); ok {
					n.fnID = id
				}
				if name, ok := item["name"].(string); ok {
					n.fnName = name
				}
				if callID, ok := item["call_id"].(string); ok {
					n.fnCallID = callID
				}
			}
		}
	case "response.function_call_arguments.delta":
		if delta, ok := ev["delta"].(string); ok {
			n.fnArgs.WriteString(delta)
		}
	case "response.output_item.done":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			if itemType, _ := item["type"].(string); itemType == "message" {
				n.msgOpen = false
				n.msgText.Reset()
			} else if itemType == "function_call" {
				n.fnOpen = false
				n.fnArgs.Reset()
			}
		}
	case "response.completed":
		outs = append(outs, n.closeFunctionIfOpen()...)
		outs = append(outs, n.closeMessageIfOpen()...)
	}

	outs = append(outs, block)
	return outs
}

// closeMessageIfOpen 关闭未完成的 message item（补齐 output_item.done）。
func (n *responsesStreamNormalizer) closeMessageIfOpen() [][]byte {
	if !n.msgOpen {
		return nil
	}
	n.msgOpen = false
	content := []interface{}{}
	if n.msgText.Len() > 0 {
		content = append(content, map[string]interface{}{
			"type": "output_text",
			"text": n.msgText.String(),
		})
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":      n.msgID,
			"type":    "message",
			"status":  "completed",
			"role":    "assistant",
			"content": content,
		},
	})
	n.msgText.Reset()
	return [][]byte{done}
}

// closeFunctionIfOpen 关闭未完成的 function_call item（补齐 output_item.done 与完整 arguments）。
func (n *responsesStreamNormalizer) closeFunctionIfOpen() [][]byte {
	if !n.fnOpen {
		return nil
	}
	n.fnOpen = false
	callID := n.fnCallID
	if callID == "" {
		callID = n.fnID
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":        n.fnID,
			"type":      "function_call",
			"status":    "completed",
			"name":      n.fnName,
			"arguments": n.fnArgs.String(),
			"call_id":   callID,
		},
	})
	n.fnArgs.Reset()
	return [][]byte{done}
}

// readSSEBlock 从流中读取一个完整 SSE 事件块（到空行结束，含结尾空行）。
func readSSEBlock(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				if buf.Len() > 0 {
					buf.WriteString(line)
					return buf.Bytes(), nil
				}
				if err != nil {
					return nil, err
				}
				continue
			}
			buf.WriteString(line)
		}
		if err != nil {
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
	}
}

// proxyResponses 代理 OpenAI Responses API（POST /v1/responses）。
// 请求体按不透明 JSON 透传（Responses 的 input/instructions 结构与 chat 不同，
// 网关不做改写），仅复用端点的模型路由、代理池与首字超时切换能力。
func (s *Service) proxyResponses(w http.ResponseWriter, r *http.Request) {
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
			Route: "responses", Kind: kind,
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
			Route: "responses", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		// 网关拦截（未到达上游）也写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		s.recordAnalyticsKey(ctx, "responses", "", "", http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "", -1, "", &AnalyticsError{
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
		Route:            "responses",
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

	// 若请求模型名是对外别名，转发到上游时还原为真实模型名。
	// 注意：必须在循环内对每个候选独立执行，因为各候选的 modelMappings 可能不同。
	normalizeResponsesTools(parsedBody)
	normalizeResponsesInput(parsedBody)

	// 请求体归一化在循环前统一执行（reasoning_effort max→high）；responses 无
	// messages 结构，工具历史推理补齐不适用（no-op 由实现自动跳过）。
	normalizeReasoningEffort(parsedBody)

	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	// rateLimitBudget 是「429 等待重试」的本请求剩余预算；任一候选开启等待重试时启用。
	rateLimitBudget := time.Duration(0)
	if rateLimitRetryEnabledAny(endpointCandidates) {
		rateLimitBudget = rateLimitRetryBudget
	}
	rateLimitRoundsUsed := 0
	var failoverSteps []map[string]interface{}
	// clientCancelled 标记本轮请求期间客户端已断开：断开后不再尝试其他候选，仅静默收尾。
	clientCancelled := false
	// 从加权选中的端点起拼：让每一次请求的第一次尝试就是最优端点（会话亲和优先）。
	startIdx := s.failoverStartIndex(chosenIndex, endpointCandidates, sessionKey)
	// lastTried 记录最后一次真实转发的端点：整链失败时调用日志以此展示真实端点，
	// 而不是「unknown」（切换过程本身不落日志，只落最终结果）。
	var lastTried *Endpoint
	// selectedMode 记录最终成功端点的 Responses 处理模式，响应处理据此决定
	// 透传还是转换回 Responses 格式。
	selectedMode := responsesModeConvert
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
			// 需要独立副本的情形：模型映射改写（写 model 字段）或 failover
			// 候选归一化（写 reasoning.effort）。首个候选不复制、保持原样透传；
			// 后续候选复制后再归一化，避免把 max 这类非标准值发给枚举更窄的上游。
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
			// 按端点能力分流：
			//   - 转换模式（responsesModeConvert）：不支持原生 /responses 的 OpenAI
			//     兼容端点，把 Responses 请求体转换为 /chat/completions 请求转发；
			//   - 透传模式（responsesModePassthrough）：原生支持 responses 的端点
			//     （DS2API），保持原样转发到 /responses。
			upstreamMode := s.responsesModeForEndpoint(cand)
			var fullURL string
			var upstreamBodyBytes []byte
			if upstreamMode == responsesModeConvert {
				chatBody, cErr := responsesRequestToChat(candBody)
				if cErr != nil {
					s.recordRelayError(RelayErrorRecord{
						Route: "responses", Kind: "bad_request",
						Model: model, Stream: stream, ClientIP: clientIP,
						ElapsedMs: time.Since(requestStarted).Milliseconds(),
						Error:     "responses→chat conversion failed: " + cErr.Error(),
					})
					response.JSON(w, http.StatusBadRequest, map[string]string{"error": cErr.Error()})
					return
				}
				upstreamBodyBytes, _ = json.Marshal(chatBody)
				fullURL = ensureVersionPath(cand.BaseURL) + "/chat/completions"
			} else {
				upstreamBodyBytes, _ = json.Marshal(candBody)
				fullURL = ensureVersionPath(cand.BaseURL) + "/responses"
			}
			res = s.relayLoop(relayLoopParams{
				route:          "responses",
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
				selectedMode = s.responsesModeForEndpoint(cand)
				// 会话亲和：仅当上游返回 2xx/3xx（真正成功）时记录该会话最近使用的端点，
				// 4xx 客户端错误不记录，避免把会话钉死在无法服务该请求的端点上。
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
				selected = endpointCandidates[k+1]
			}
		}
		if retryRoundFinished {
			break
		}
		// 限流风暴快速收尾：本轮全部候选都返回限流（429/439）时，重试整轮只会继续
		// 打同一批被限流的出口并串行吃掉全部耗时，直接聚合返回。
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
			// 任一候选端点开启「429 等待重试」且预算未耗尽：等待配额窗口后重跑整轮候选。
			if rateLimitRetryEnabledAny(endpointCandidates) && rateLimitBudget > 0 && rateLimitRoundsUsed < rateLimitRetryRoundsCap {
				wait := rateLimitRetryWaitFor(res, endpointCandidates, rateLimitBudget)
				if wait > 0 {
					rateLimitBudget -= wait
					if !waitForRateLimitRetry(ctx, wait) {
						retryRoundCancelled = true
					} else {
						rateLimitRoundsUsed++
						lastRes = res
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
		// 整链失败：切换过程不落日志，这里按「最终结果」聚合为一条，
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
			Route: "responses", Kind: "failover",
			Endpoint: lastEpName, EndpointID: lastEpID, Model: model,
			Stream: stream, Proxy: lastProxy, ClientIP: clientIP,
			Attempts:   attempts,
			ElapsedMs:  time.Since(requestStarted).Milliseconds(),
			StatusCode: failStatus,
			Error:      msg,
		})
		s.recordAnalyticsKey(ctx, "responses", lastEpID, model, failStatus, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
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
		// 转换模式：上游返回 chat.completion 流的 4xx/5xx 错误时不能作为 SSE 透传
		// （客户端会把错误 JSON 当 SSE 解析失败）。按普通错误响应读取并写回 JSON。
		if selectedMode == responsesModeConvert && res.resp.StatusCode >= 400 {
			errBytes, _ := readUpstreamBodyLimited(res.resp.Body)
			if len(res.firstChunk) > 0 {
				errBytes = append(res.firstChunk, errBytes...)
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
		// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
		extendStreamDeadline := func() {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		}
		// SSE ping 保活：上游长时间不吐流时向客户端发送注释行，穿透 NAT 空闲超时。
		stopPing := sw.startPing(ctx)
		defer stopPing()
		// 部分上游（如 zen）的 Responses 流缺少 response.created / output_item 容器事件，
		// Codex 等 SDK 依赖它们初始化响应与挂载文本/工具参数，缺失会导致空白回。
		// 用状态机逐事件补全后转发。
		normalizer := newResponsesStreamNormalizer(model)
		// 转换模式：上游返回 chat.completion.chunk 流，逐块转换为 Responses 事件序列。
		var streamer *chatToResponsesStreamer
		if selectedMode == responsesModeConvert {
			streamer = newChatToResponsesStreamer(model)
		}
		streamReader := bufio.NewReader(res.resp.Body)
		if res.firstWritten && len(res.firstChunk) > 0 {
			streamReader = bufio.NewReader(io.MultiReader(bytes.NewReader(res.firstChunk), res.resp.Body))
		}
		// usage 信息总在流尾部，只保留流尾部即可，避免长对话把整个流式响应累积在内存中。
		tail := make([]byte, 0, usageTailLimit)
		for {
			block, readErr := readSSEBlock(streamReader)
			if len(block) > 0 {
				tail = append(tail, block...)
				if len(tail) > usageTailLimit {
					tail = tail[len(tail)-usageTailLimit:]
				}
				if streamer != nil {
					if dataJSON, ok := sseDataJSON(block); ok {
						for _, out := range streamer.consume([]byte(dataJSON)) {
							extendStreamDeadline()
							sw.write(out)
						}
					}
				} else {
					for _, out := range normalizer.transform(block) {
						extendStreamDeadline()
						sw.write(out)
					}
				}
			}
			if readErr != nil {
				break
			}
		}
		if streamer != nil {
			for _, out := range streamer.finish() {
				extendStreamDeadline()
				sw.write(out)
			}
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		// 解析 usage：转换模式下 streamer 已把 chat usage 映射为 responses 字段
		// （input/output_tokens），优先取它；透传模式从尾部 response.completed 事件解析。
		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		cachedTokens := 0
		if streamer != nil && streamer.hasUsage {
			if v, ok := streamer.usage["input_tokens"].(int); ok {
				promptTokens = v
			}
			if v, ok := streamer.usage["output_tokens"].(int); ok {
				completionTokens = v
			}
			if v, ok := streamer.usage["total_tokens"].(int); ok {
				totalTokens = v
			} else if promptTokens > 0 || completionTokens > 0 {
				totalTokens = promptTokens + completionTokens
			}
			if d, ok := streamer.usage["input_tokens_details"].(map[string]interface{}); ok {
				if v, ok := d["cached_tokens"].(int); ok {
					cachedTokens = v
				}
			}
		} else {
			accumulatedStr := string(tail)
			if matches := inputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
				promptTokens, _ = strconv.Atoi(matches[1])
			}
			if matches := outputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
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
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo, res.realModel)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, readErr := readUpstreamBodyLimited(res.resp.Body)
		if readErr != nil {
			// 上游响应体读取失败/超限：按网关侧 502 回错，不把截断数据写回客户端。
			s.recordRelayError(RelayErrorRecord{
				Route: "responses", Kind: "gateway",
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

		// 转换模式：上游返回 chat.completion JSON（prompt/completion_tokens），
		// 需转换为 Responses 响应（input/output_tokens）后再解析 usage 并返回。
		if selectedMode == responsesModeConvert && res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 {
			if conv, cErr := chatResponseToResponses(respBodyBytes, model, ""); cErr == nil {
				respBodyBytes = conv
			}
		}

		var usageInfo struct {
			Usage struct {
				InputTokens        int `json:"input_tokens"`
				OutputTokens       int `json:"output_tokens"`
				TotalTokens        int `json:"total_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
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
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.InputTokens, usageInfo.Usage.OutputTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.InputTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo, res.realModel)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}
