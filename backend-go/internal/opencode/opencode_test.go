package opencode

import (
	"strings"
	"testing"
)

func TestEnsureChatToolsInjectsDisguise(t *testing.T) {
	body := map[string]interface{}{
		"model":       "mimo-v2.6-flash-free",
		"messages":    []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"tools":       []interface{}{map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "edit", "parameters": map[string]interface{}{}}}},
		"tool_choice": "required",
		"stream":      false,
	}
	ensureChatTools(body)
	tools := bodyTools(body)
	names := map[string]bool{}
	for _, tt := range tools {
		n := toolName(tt)
		if n == "" {
			t.Fatalf("tool without name")
		}
		names[n] = true
	}
	if !names["edit"] || !names["read"] || !names["shell"] {
		t.Fatalf("expected edit/read/shell, got %v", names)
	}
	if v := body["stream"]; v != true {
		t.Fatalf("stream should be forced true, got %v", v)
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("tool_choice %q should have been stripped", body["tool_choice"])
	}
	so, ok := body["stream_options"].(map[string]interface{})
	if !ok || so["include_usage"] != true {
		t.Fatalf("stream_options must include usage, got %v", body["stream_options"])
	}
}

func TestEnsureChatToolsEmptyToolsUsesDisguise(t *testing.T) {
	body := map[string]interface{}{"model": "m", "messages": []interface{}{}}
	ensureChatTools(body)
	if len(bodyTools(body)) != 2 {
		t.Fatalf("expected 2 disguise tools, got %d", len(bodyTools(body)))
	}
}

func TestTranslateChatToResponses(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "you are terse"},
		map[string]interface{}{"role": "user", "content": "explain"},
	}
	body := map[string]interface{}{
		"model":            "muse-spark-1.3-contributor-free",
		"messages":         messages,
		"max_tokens":       128,
		"reasoning_effort": "high",
	}
	rb := translateChatToResponses(body)
	if rb["model"] != "muse-spark-1.3-contributor-free" {
		t.Fatalf("model not preserved: %v", rb["model"])
	}
	if rb["stream"] != true || rb["store"] != false {
		t.Fatalf("stream/store bad: %v %v", rb["stream"], rb["store"])
	}
	input, ok := rb["input"].([]map[string]interface{})
	if !ok || len(input) != 2 {
		t.Fatalf("input conversion wrong: %#v", rb["input"])
	}
	if input[0]["role"] != "developer" {
		t.Fatalf("system role should map to developer: %v", input[0]["role"])
	}
	reasoning, ok := rb["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning not normalized: %#v", rb["reasoning"])
	}
	tools := bodyTools(rb)
	names := map[string]bool{}
	for _, t := range tools {
		names[toolName(t)] = true
	}
	if !names["read"] || !names["shell"] {
		t.Fatalf("disguise tools missing in responses body: %v", names)
	}
}

func TestTranslateChatToResponsesDefaultsMedium(t *testing.T) {
	body := map[string]interface{}{
		"model":    "muse-spark-1.3-contributor-free",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	rb := translateChatToResponses(body)
	reasoning, ok := rb["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("muse-spark should default reasoning medium, got %#v", rb["reasoning"])
	}
}

func TestChatToResponsesToolLoop(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "list files"},
		map[string]interface{}{
			"role": "assistant",
			"content": "",
			"tool_calls": []interface{}{map[string]interface{}{
				"id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "shell", "arguments": `{"command":"ls"}`},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "a.txt\nb.txt"},
		map[string]interface{}{"role": "user", "content": "thanks"},
	}
	input := chatToResponsesInput(messages)
	var kinds []string
	for _, item := range input {
		if typ, ok := item["type"].(string); ok {
			kinds = append(kinds, typ)
		} else {
			kinds = append(kinds, "plain")
		}
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "function_call") || !strings.Contains(joined, "function_call_output") {
		t.Fatalf("tool loop conversion wrong: %v", kinds)
	}
}

func TestResponsesSSEToChatCompletion(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hel"}`,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		`data: {"type":"response.completed","response":{"model":"muse-spark-1.3-contributor-free","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12},"output":[]}}`,
		`data: [DONE]`,
	}, "\n")
	out := responsesSSEToChatCompletion(sse, "muse-spark-1.3-contributor-free")
	choices := out["choices"].([]map[string]interface{})
	msg := choices[0]["message"].(map[string]interface{})
	if msg["content"] != "Hello" {
		t.Fatalf("content assembly wrong: %v", msg["content"])
	}
	if choices[0]["finish_reason"] != "stop" {
		t.Fatalf("finish reason wrong: %v", choices[0]["finish_reason"])
	}
	u := out["usage"].(*openAIUsage)
	if u.PromptTokens != 5 || u.CompletionTokens != 7 || u.TotalTokens != 12 {
		t.Fatalf("usage wrong: %+v", u)
	}
}

func TestResponsesSSEToChatCompletionToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_x","name":"default.read","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"filePath\":\""}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"a.txt\"}"}`,
		`data: {"type":"response.completed","response":{"model":"m","usage":{"input_tokens":2,"output_tokens":3},"output":[{"type":"function_call","call_id":"call_x","name":"default.read","arguments":"{\"filePath\":\"a.txt\"}"}]}}`,
		`data: [DONE]`,
	}, "\n")
	out := responsesSSEToChatCompletion(sse, "m")
	choices := out["choices"].([]map[string]interface{})
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %v", choices[0]["finish_reason"])
	}
	msg := choices[0]["message"].(map[string]interface{})
	tcs := msg["tool_calls"].([]map[string]interface{})
	fn := tcs[0]["function"].(map[string]interface{})
	if fn["name"] != "read" {
		t.Fatalf("default. prefix should be stripped: %v", fn["name"])
	}
}

func TestResponsesChatStreamIncremental(t *testing.T) {
	tr := newResponsesChatStream("muse-spark-1.3-contributor-free")
	var chunks [][]byte
	chunks = append(chunks, tr.Consume([]byte(`{"type":"response.reasoning_summary_text.delta","delta":"think"}`))...)
	chunks = append(chunks, tr.Consume([]byte(`{"type":"response.output_text.delta","delta":"hi"}`))...)
	chunks = append(chunks, tr.Consume([]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2},"output":[]}}`))...)
	if fin := tr.Finish(); fin != nil {
		chunks = append(chunks, fin)
	}
	var all string
	for _, c := range chunks {
		all += string(c)
	}
	if !strings.Contains(all, "reasoning_content") {
		t.Fatalf("reasoning delta missing: %s", all)
	}
	if !strings.Contains(all, `"content":"hi"`) {
		t.Fatalf("content delta missing: %s", all)
	}
	if !strings.Contains(all, "[DONE]") {
		t.Fatalf("done missing: %s", all)
	}
	if !strings.Contains(all, "prompt_tokens") {
		t.Fatalf("usage chunk missing: %s", all)
	}
}

func TestChatSSEToChatCompletion(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"x","model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"x","model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"content":"hey"},"finish_reason":null}]}`,
		`data: {"id":"x","model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"x","model":"mimo-v2.6-flash-free","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		`data: [DONE]`,
	}, "\n")
	out := chatSSEToChatCompletion(sse, "mimo-v2.6-flash-free")
	choices := out["choices"].([]map[string]interface{})
	msg := choices[0]["message"].(map[string]interface{})
	if msg["content"] != "hey" {
		t.Fatalf("content wrong: %v", msg["content"])
	}
	if choices[0]["finish_reason"] != "stop" {
		t.Fatalf("finish wrong: %v", choices[0]["finish_reason"])
	}
	u := out["usage"].(map[string]interface{})
	if u["prompt_tokens"] != float64(3) {
		t.Fatalf("usage wrong: %v", u)
	}
}

func TestNormalizeUpstreamErrorFreeTier(t *testing.T) {
	o := normalizeUpstreamError(401, `{"type":"FreeTierError","error":{"message":"OpenCode's free tier can only be used from within OpenCode"}}`)
	e, _ := o["error"].(map[string]interface{})
	msg, _ := e["message"].(string)
	if !strings.Contains(msg, "会话") {
		t.Fatalf("FreeTierError should produce a session hint: %v", msg)
	}
}

func TestNormalizeUpstreamErrorHTML(t *testing.T) {
	o := normalizeUpstreamError(502, "<html>Bad Gateway</html>")
	e, _ := o["error"].(map[string]interface{})
	if _, ok := e["message"].(string); !ok {
		t.Fatalf("HTML error should wrap message: %#v", o)
	}
}

func TestIdentityHeaders(t *testing.T) {
	h := identityHeaders(SessionIdentity{
		UserAgent:        "opencode/1.2.3 (linux x64)",
		XOpencodeClient:  "cli",
		XOpencodeProject: "prj_x",
		XOpencodeSession: "ses_y",
		XSessionID:       "ses_y",
	})
	if h.Get("User-Agent") == "" || h.Get("x-opencode-client") != "cli" {
		t.Fatalf("base identity missing: %v", h)
	}
	if h.Get("x-opencode-session-id") != "ses_y" {
		t.Fatalf("opencode-session-id should mirror session: %v", h.Get("x-opencode-session-id"))
	}
	if !strings.HasPrefix(h.Get("x-opencode-request"), "msg_") {
		t.Fatalf("request id should be msg_ prefixed: %v", h.Get("x-opencode-request"))
	}
}

func TestFreshMsgIDShape(t *testing.T) {
	id := freshMsgID()
	if !strings.HasPrefix(id, "msg_") || len(id) != 4+12+14 {
		t.Fatalf("msg id shape bad: %q", id)
	}
}

func TestStripModelPrefix(t *testing.T) {
	s := &Service{}
	s.settings = Settings{ModelPrefix: "oc-"}
	if got := s.stripModelPrefix("oc-mimo-v2.6-flash-free"); got != "mimo-v2.6-flash-free" {
		t.Fatalf("prefix strip failed: %v", got)
	}
	if got := s.stripModelPrefix("mimo-v2.6-flash-free"); got != "mimo-v2.6-flash-free" {
		t.Fatalf("no-prefix passthrough failed: %v", got)
	}
	if got := s.prefixModel("mimo-v2.6-flash-free"); got != "oc-mimo-v2.6-flash-free" {
		t.Fatalf("prefixModel failed: %v", got)
	}
}