package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

// ==================== 请求转换 ====================

// ==================== 辅助 ====================

func TestResponsesModeForEndpoint(t *testing.T) {
	s := newOpenAIService(t)
	cases := []struct {
		name string
		ep   Endpoint
		want responsesMode
	}{
		{"third-party openai", Endpoint{Name: "x", BaseURL: "https://x666.me/v1", UpstreamType: "openai"}, responsesModeConvert},
		{"ds2api internal", Endpoint{Name: "ds2", BaseURL: "http://127.0.0.1:3000/api/ds2api/v1", PluginID: "ds2api", UpstreamType: "openai"}, responsesModePassthrough},
		{"antigravity", Endpoint{Name: "ag", BaseURL: "http://127.0.0.1:3000/api/antigravity/v1", PluginID: "antigravity", UpstreamType: "openai"}, responsesModePassthrough},
		{"gemini", Endpoint{Name: "g", BaseURL: "https://generativelanguage.googleapis.com", UpstreamType: "gemini"}, responsesModePassthrough},
		{"vertex", Endpoint{Name: "v", BaseURL: "https://aiplatform.googleapis.com/v1/publishers/google", UpstreamType: "vertex"}, responsesModePassthrough},
	}
	for _, tc := range cases {
		if got := s.responsesModeForEndpoint(tc.ep); got != tc.want {
			t.Errorf("%s: mode = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResponsesRequestToChatBasic(t *testing.T) {
	req := map[string]interface{}{
		"model":             "gpt-4o",
		"stream":            false,
		"instructions":      "You are helpful",
		"input":             "hello world",
		"max_output_tokens": 100,
		"reasoning":         map[string]interface{}{"effort": "high"},
	}
	chat, err := responsesRequestToChat(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat["model"] != "gpt-4o" {
		t.Errorf("model = %v", chat["model"])
	}
	if chat["max_tokens"] != 100 {
		t.Errorf("max_tokens = %v", chat["max_tokens"])
	}
	if chat["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", chat["reasoning_effort"])
	}
	messages := chat["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	sys := messages[0].(map[string]interface{})
	if sys["role"] != "system" || sys["content"] != "You are helpful" {
		t.Errorf("system msg = %v", sys)
	}
	user := messages[1].(map[string]interface{})
	if user["role"] != "user" || user["content"] != "hello world" {
		t.Errorf("user msg = %v", user)
	}
}

func TestResponsesRequestToChatInputArray(t *testing.T) {
	req := map[string]interface{}{
		"model": "m",
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "hi"},
			map[string]interface{}{"type": "input_text", "text": "second"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": "previous"},
			}},
			map[string]interface{}{"role": "user", "content": "next"},
		},
	}
	chat, err := responsesRequestToChat(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := chat["messages"].([]interface{})
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4", len(messages))
	}
	if got := messages[1].(map[string]interface{})["content"]; got != "second" {
		t.Errorf("input_text content = %v", got)
	}
	assistant := messages[2].(map[string]interface{})
	if assistant["role"] != "assistant" || assistant["content"] != "previous" {
		t.Errorf("assistant msg = %v", assistant)
	}
}

func TestResponsesRequestToChatToolCalls(t *testing.T) {
	req := map[string]interface{}{
		"model": "m",
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "name": "get_weather", "description": "get weather", "parameters": map[string]interface{}{"type": "object"}},
			map[string]interface{}{"type": "web_search"},
		},
		"tool_choice": map[string]interface{}{"type": "function", "name": "get_weather"},
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "weather?"},
			map[string]interface{}{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{\"city\":\"beijing\"}"},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "25c"},
		},
	}
	chat, err := responsesRequestToChat(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools := chat["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1 (web_search dropped)", len(tools))
	}
	toolFn := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	if toolFn["name"] != "get_weather" {
		t.Errorf("tool fn name = %v", toolFn["name"])
	}

	tc := chat["tool_choice"].(map[string]interface{})
	if tc["type"] != "function" {
		t.Errorf("tool_choice type = %v", tc["type"])
	}
	if fn := tc["function"].(map[string]interface{})["name"]; fn != "get_weather" {
		t.Errorf("tool_choice fn name = %v", fn)
	}

	messages := chat["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("messages len = %d, want 3 (user, assistant tool_calls, tool)", len(messages))
	}
	assistant := messages[1].(map[string]interface{})
	toolCalls, _ := assistant["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("assistant tool_calls len = %d", len(toolCalls))
	}
	call := toolCalls[0].(map[string]interface{})
	if call["id"] != "call_1" {
		t.Errorf("call id = %v", call["id"])
	}
	if callFn := call["function"].(map[string]interface{}); callFn["name"] != "get_weather" || callFn["arguments"] != "{\"city\":\"beijing\"}" {
		t.Errorf("call fn = %v", call["function"])
	}
	toolMsg := messages[2].(map[string]interface{})
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "25c" {
		t.Errorf("tool msg = %v", toolMsg)
	}
}

func TestResponsesRequestToChatTextFormat(t *testing.T) {
	req := map[string]interface{}{
		"model": "m",
		"input": "hi",
		"text": map[string]interface{}{
			"format": map[string]interface{}{
				"type":   "json_schema",
				"name":   "schema",
				"schema": map[string]interface{}{"type": "object"},
				"strict": true,
			},
		},
	}
	chat, err := responsesRequestToChat(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rf := chat["response_format"].(map[string]interface{})
	if rf["type"] != "json_schema" {
		t.Errorf("response_format type = %v", rf["type"])
	}
	js := rf["json_schema"].(map[string]interface{})
	if js["name"] != "schema" || js["strict"] != true {
		t.Errorf("response_format json_schema = %v", js)
	}
}

func TestResponsesRequestToChatMissingModel(t *testing.T) {
	_, err := responsesRequestToChat(map[string]interface{}{"input": "hi"})
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

// ==================== 非流式响应转换 ====================

func TestChatResponseToResponsesBasic(t *testing.T) {
	chatBody := `{
		"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-4o",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Hello!"}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`
	out, err := chatResponseToResponses([]byte(chatBody), "gpt-4o", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["object"] != "response" || resp["status"] != "completed" {
		t.Errorf("resp object/status = %v / %v", resp["object"], resp["status"])
	}
	if resp["model"] != "gpt-4o" {
		t.Errorf("model = %v", resp["model"])
	}
	output := resp["output"].([]interface{})
	if len(output) != 1 {
		t.Fatalf("output len = %d", len(output))
	}
	msg := output[0].(map[string]interface{})
	if msg["type"] != "message" || msg["role"] != "assistant" {
		t.Errorf("msg = %v", msg)
	}
	content := msg["content"].([]interface{})[0].(map[string]interface{})
	if content["type"] != "output_text" || content["text"] != "Hello!" {
		t.Errorf("content = %v", content)
	}
	if resp["output_text"] != "Hello!" {
		t.Errorf("output_text = %v", resp["output_text"])
	}
	usage := resp["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) || usage["total_tokens"] != float64(15) {
		t.Errorf("usage = %v", usage)
	}
}

func TestChatResponseToResponsesToolCalls(t *testing.T) {
	chatBody := `{
		"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4o",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{
			"role":"assistant","content":null,
			"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"beijing\"}"}},
				{"id":"call_2","type":"function","function":{"name":"get_time","arguments":""}}
			]
		}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`
	out, err := chatResponseToResponses([]byte(chatBody), "gpt-4o", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	output := resp["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("output len = %d, want 2", len(output))
	}
	fc1 := output[0].(map[string]interface{})
	if fc1["type"] != "function_call" || fc1["call_id"] != "call_1" || fc1["name"] != "get_weather" {
		t.Errorf("fc1 = %v", fc1)
	}
	if fc1["arguments"] != "{\"city\":\"beijing\"}" {
		t.Errorf("fc1 arguments = %v", fc1["arguments"])
	}
	fc2 := output[1].(map[string]interface{})
	if fc2["arguments"] != "{}" {
		t.Errorf("fc2 empty arguments should normalize to {}, got %v", fc2["arguments"])
	}
}

func TestChatResponseToResponsesErrorPassthrough(t *testing.T) {
	chatBody := `{"error":{"message":"model not found","type":"invalid_request_error"}}`
	out, err := chatResponseToResponses([]byte(chatBody), "m", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != chatBody {
		t.Errorf("error body should pass through unchanged, got %s", out)
	}
}

// ==================== 流式响应转换 ====================

func collectStreamerEvents(t *testing.T, chunks []string, model string) string {
	t.Helper()
	st := newChatToResponsesStreamer(model)
	var sb strings.Builder
	for _, c := range chunks {
		for _, ev := range st.consume([]byte(c)) {
			sb.Write(ev)
		}
	}
	for _, ev := range st.finish() {
		sb.Write(ev)
	}
	return sb.String()
}

func TestChatToResponsesStreamerText(t *testing.T) {
	chunks := []string{
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
	}
	out := collectStreamerEvents(t, chunks, "gpt-4o")
	// 必须包含关键事件序列。
	for _, want := range []string{
		"event: response.created",
		"event: response.output_item.added",
		"event: response.content_part.added",
		`"type":"message"`,
		"event: response.output_text.delta",
		`"delta":"Hello"`,
		`"delta":" world"`,
		"event: response.output_text.done",
		`"text":"Hello world"`,
		"event: response.content_part.done",
		"event: response.output_item.done",
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in stream output", want)
		}
	}
	if !strings.Contains(out, `"input_tokens":10`) ||
		!strings.Contains(out, `"output_tokens":5`) ||
		!strings.Contains(out, `"total_tokens":15`) {
		t.Errorf("usage mapping wrong: %s", out)
	}
}

func TestChatToResponsesStreamerToolCalls(t *testing.T) {
	chunks := []string{
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"beijing\"}"}}]},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	out := collectStreamerEvents(t, chunks, "gpt-4o")
	for _, want := range []string{
		`"type":"function_call"`,
		`"call_id":"call_1"`,
		`"name":"get_weather"`,
		"event: response.function_call_arguments.delta",
		`"delta":"{\"city\":"`,
		"event: response.function_call_arguments.done",
		`"arguments":"{\"city\":\"beijing\"}"`,
		"event: response.output_item.done",
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in stream output", want)
		}
	}
}

func TestChatToResponsesStreamerParallelToolCalls(t *testing.T) {
	chunks := []string{
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f_a","arguments":""}},{"index":1,"id":"call_b","type":"function","function":{"name":"f_b","arguments":""}}]},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"x\":1}"}}]},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	out := collectStreamerEvents(t, chunks, "gpt-4o")
	// 每个工具在 output_item.added 中恰出现一次。
	if strings.Count(out, `"type":"function_call"`) != 6 {
		t.Errorf("function_call occurrences = %d, want 6 (added+done+completed per tool)", strings.Count(out, `"type":"function_call"`))
	}
	if !strings.Contains(out, `"name":"f_a"`) || !strings.Contains(out, `"name":"f_b"`) {
		t.Errorf("missing parallel tool names: %s", out)
	}
	// f_a 无参数增量，应归一化为 {}；f_b 累积 {"x":1}。
	if !strings.Contains(out, `"arguments":"{\"x\":1}"`) {
		t.Errorf("f_b arguments accumulation failed: %s", out)
	}
	if strings.Contains(out, `{}{`) {
		t.Errorf("f_a empty arguments should be normalized to {}, f_b not duplicated: %s", out)
	}
	// call_id 出现在 added / arguments.done / item.done / completed；call_b 另有 arguments.delta 一次。
	if strings.Count(out, `"call_id":"call_a"`) != 4 || strings.Count(out, `"call_id":"call_b"`) != 5 {
		t.Errorf("call_id occurrences wrong: %s", out)
	}
}

func TestChatToResponsesStreamerReasoning(t *testing.T) {
	chunks := []string{
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":"think..."},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":null}]}`,
		`{"id":"x","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}
	out := collectStreamerEvents(t, chunks, "gpt-4o")
	for _, want := range []string{
		`"type":"reasoning"`,
		"event: response.reasoning_text.delta",
		`"delta":"think..."`,
		"event: response.reasoning_text.done",
		`"type":"output_text"`,
		`"text":"answer"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in stream output", want)
		}
	}
}

// TestResponsesRequestToChatParallelToolHistory 验证并行工具调用历史的 input
// （先全部 function_call，再全部 function_call_output）能正确转换为 Chat 格式：
// 全部 tool_calls 归并进同一条 assistant 消息，各 tool 消息紧随其后。
func TestResponsesRequestToChatParallelToolHistory(t *testing.T) {
	req := map[string]interface{}{
		"model": "m",
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "both?"},
			map[string]interface{}{"type": "function_call", "call_id": "call_a", "name": "f_a", "arguments": "{}"},
			map[string]interface{}{"type": "function_call", "call_id": "call_b", "name": "f_b", "arguments": "{}"},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_a", "output": "1"},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_b", "output": "2"},
		},
	}
	chat, err := responsesRequestToChat(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := chat["messages"].([]interface{})
	// user, assistant(tool_calls=a,b), tool(a), tool(b)
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4 (user, assistant tool_calls, 2 tools)", len(messages))
	}
	assistant := messages[1].(map[string]interface{})
	if assistant["role"] != "assistant" {
		t.Fatalf("messages[1] role = %v", assistant["role"])
	}
	toolCalls, _ := assistant["tool_calls"].([]interface{})
	if len(toolCalls) != 2 {
		t.Fatalf("assistant tool_calls len = %d, want 2 (both parallel calls)", len(toolCalls))
	}
	callA := toolCalls[0].(map[string]interface{})
	callB := toolCalls[1].(map[string]interface{})
	if callA["id"] != "call_a" || callB["id"] != "call_b" {
		t.Fatalf("tool_calls ids = %v / %v", callA["id"], callB["id"])
	}
	toolMsgA := messages[2].(map[string]interface{})
	toolMsgB := messages[3].(map[string]interface{})
	if toolMsgA["role"] != "tool" || toolMsgA["tool_call_id"] != "call_a" || toolMsgA["content"] != "1" {
		t.Errorf("tool msg a = %v", toolMsgA)
	}
	if toolMsgB["role"] != "tool" || toolMsgB["tool_call_id"] != "call_b" || toolMsgB["content"] != "2" {
		t.Errorf("tool msg b = %v", toolMsgB)
	}
}
