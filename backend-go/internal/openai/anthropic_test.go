package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicModelToOpenAI(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-1":   "deepseek-v4-pro",
		"claude-opus":       "deepseek-v4-pro",
		"claude-sonnet-4-5": "deepseek-v4-flash",
		"claude-haiku-3-5":  "deepseek-v4-flash",
		"deepseek-v4-flash": "deepseek-v4-flash",
		"gpt-5":             "gpt-5",
	}
	for in, want := range cases {
		if got := anthropicModelToOpenAI(in); got != want {
			t.Errorf("anthropicModelToOpenAI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnthropicToOpenAI(t *testing.T) {
	body := map[string]interface{}{
		"model":          "claude-sonnet-4-5",
		"max_tokens":     1024,
		"temperature":    0.7,
		"stream":         true,
		"system":         "You are helpful.",
		"stop_sequences": []interface{}{"END"},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "ok"},
				map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "get_goal", "input": map[string]interface{}{"a": 1}},
			}},
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": "done"},
			}},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "get_goal", "description": "d", "input_schema": map[string]interface{}{"type": "object"}},
		},
		"tool_choice": "any",
		"metadata":    map[string]interface{}{"user_id": "u1"},
	}

	out, err := anthropicToOpenAI(body)
	if err != nil {
		t.Fatal(err)
	}
	if out["model"] != "deepseek-v4-flash" {
		t.Errorf("model = %v, want deepseek-v4-flash", out["model"])
	}
	if out["max_tokens"] != 1024 {
		t.Errorf("max_tokens = %v, want 1024", out["max_tokens"])
	}
	if out["temperature"] != 0.7 || out["stream"] != true {
		t.Errorf("params lost: %v", out)
	}
	// system + user + assistant(text+tool_calls 合并一条) + tool = 4 条。
	if len(out["messages"].([]interface{})) != 4 {
		t.Errorf("messages = %d, want 4 (system/user/assistant+tool_calls/tool)", len(out["messages"].([]interface{})))
	}
	if out["user"] != "u1" {
		t.Errorf("user_id not mapped: %v", out["user"])
	}
	if stop, ok := out["stop"].([]interface{}); !ok || len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop_sequences not mapped: %v", out["stop"])
	}

	msgs := out["messages"].([]interface{})
	if msgs[0].(map[string]interface{})["role"] != "system" {
		t.Errorf("system not first message")
	}
	assistant := msgs[2].(map[string]interface{})
	if assistant["role"] != "assistant" {
		t.Errorf("msg[2] should be assistant")
	}
	if assistant["content"] != "ok" {
		t.Errorf("assistant text lost: %v", assistant["content"])
	}
	toolCalls, ok := assistant["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant should have 1 tool_calls, got %#v", assistant["tool_calls"])
	}
	tc := toolCalls[0].(map[string]interface{})
	if tc["id"] != "tu_1" {
		t.Errorf("tool_call id = %v", tc["id"])
	}
	fn := tc["function"].(map[string]interface{})
	if fn["name"] != "get_goal" || fn["arguments"] != `{"a":1}` {
		t.Errorf("tool_call function = %#v", fn)
	}
	toolMsg := msgs[3].(map[string]interface{})
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "tu_1" || toolMsg["content"] != "done" {
		t.Errorf("tool message malformed: %#v", toolMsg)
	}

	tools := out["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
	tfn := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	if tfn["name"] != "get_goal" || tfn["parameters"] == nil {
		t.Errorf("tool function malformed: %#v", tfn)
	}
	if out["tool_choice"] != "required" {
		t.Errorf("tool_choice any should map to required, got %v", out["tool_choice"])
	}
}

func TestAnthropicToOpenAIStringContentAndNoToolChoice(t *testing.T) {
	t.Run("user text array preserved", func(t *testing.T) {
		// 回归：user 消息 content 为 text 数组时不丢弃。
		body := map[string]interface{}{
			"model":      "deepseek-v4-flash",
			"max_tokens": 100,
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": []interface{}{
					map[string]interface{}{"type": "text", "text": "part1"},
					map[string]interface{}{"type": "text", "text": "part2"},
				}},
				map[string]interface{}{"role": "assistant", "content": "ok"},
			},
		}
		out, err := anthropicToOpenAI(body)
		if err != nil {
			t.Fatal(err)
		}
		msgs := out["messages"].([]interface{})
		if len(msgs) != 2 {
			t.Fatalf("messages = %d, want 2", len(msgs))
		}
		first := msgs[0].(map[string]interface{})
		if first["role"] != "user" || first["content"] != "part1part2" {
			t.Errorf("user array message dropped/mangled: %#v", first)
		}
	})
	t.Run("mixed tool_calls with text single message", func(t *testing.T) {
		// assistant 同时带 text 与 tool_use 时只输出一条 assistant 消息。
		body := map[string]interface{}{
			"model":      "deepseek-v4-flash",
			"max_tokens": 100,
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "hi"},
				map[string]interface{}{"role": "assistant", "content": []interface{}{
					map[string]interface{}{"type": "text", "text": "let me"},
					map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "get_goal", "input": map[string]interface{}{}},
				}},
				map[string]interface{}{"role": "user", "content": []interface{}{
					map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": "done"},
				}},
			},
		}
		out, err := anthropicToOpenAI(body)
		if err != nil {
			t.Fatal(err)
		}
		msgs := out["messages"].([]interface{})
		if len(msgs) != 3 {
			t.Fatalf("messages = %d, want 3 (user/assistant+tool_calls/tool)", len(msgs))
		}
		assistant := msgs[1].(map[string]interface{})
		if assistant["content"] != "let me" {
			t.Errorf("assistant text lost: %v", assistant["content"])
		}
		if _, ok := assistant["tool_calls"].([]interface{}); !ok {
			t.Errorf("assistant tool_calls missing: %#v", assistant)
		}
	})
	t.Run("plain string content", func(t *testing.T) {
		body := map[string]interface{}{
			"model":      "deepseek-v4-flash",
			"max_tokens": 100,
			"messages": []interface{}{
				map[string]interface{}{"role": "user", "content": "plain"},
			},
		}
		out, err := anthropicToOpenAI(body)
		if err != nil {
			t.Fatal(err)
		}
		msgs := out["messages"].([]interface{})
		if len(msgs) != 1 {
			t.Fatalf("messages = %d", len(msgs))
		}
		if msgs[0].(map[string]interface{})["content"] != "plain" {
			t.Errorf("string content lost")
		}
		if _, ok := out["tool_choice"]; ok {
			t.Errorf("tool_choice should be absent when not provided")
		}
	})
}

func TestOpenAIResponseToAnthropic(t *testing.T) {
	oai := `{
		"id": "chatcmpl-abc",
		"model": "deepseek-v4-flash",
		"choices": [{
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": "let me check",
				"tool_calls": [{
					"id": "call_1",
					"function": {"name": "get_goal", "arguments": "{\"x\":1}"}
				}]
			}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5}
	}`
	converted, err := openAIResponseToAnthropic([]byte(oai), "")
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(converted, &msg); err != nil {
		t.Fatal(err)
	}
	if msg["type"] != "message" || msg["role"] != "assistant" {
		t.Errorf("type/role wrong: %v", msg)
	}
	if msg["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", msg["stop_reason"])
	}
	content := msg["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("content blocks = %d, want 2", len(content))
	}
	text := content[0].(map[string]interface{})
	if text["type"] != "text" || text["text"] != "let me check" {
		t.Errorf("text block wrong: %#v", text)
	}
	toolUse := content[1].(map[string]interface{})
	if toolUse["type"] != "tool_use" || toolUse["name"] != "get_goal" {
		t.Errorf("tool_use block wrong: %#v", toolUse)
	}
	usage := msg["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) {
		t.Errorf("usage wrong: %#v", usage)
	}
}

func TestAnthropicSSETransformerParallelTools(t *testing.T) {
	tr := newAnthropicSSETransformer("m1")
	join := func(evs [][]byte) string {
		var b strings.Builder
		for _, e := range evs {
			b.Write(e)
		}
		return b.String()
	}

	// chunk1: tool index 0 开始（id/name 首次出现）。
	_ = join(tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"a_tool","arguments":""}}]}}]}`)))
	// chunk2: tool index 1 开始。
	_ = join(tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"b_tool","arguments":""}}]}}]}`)))
	// chunk3: index 0 的参数增量。
	events0 := tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":"}}]}}]}`))
	s0 := join(events0)
	if !strings.Contains(s0, `"index":0`) || !strings.Contains(s0, `{\"a\":`) {
		t.Errorf("index0 delta should map to block 0, got: %s", s0)
	}
	// chunk4: index 1 的参数增量。
	events1 := tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":"}}]}}]}`))
	s1 := join(events1)
	if !strings.Contains(s1, `"index":1`) || !strings.Contains(s1, `{\"b\":`) {
		t.Errorf("index1 delta should map to block 1, got: %s", s1)
	}
	// chunk5: index 0 续段——必须仍指向块 0。
	events0b := tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`))
	s0b := join(events0b)
	if !strings.Contains(s0b, `"index":0`) {
		t.Errorf("index0 continuation should stay at block 0, got: %s", s0b)
	}

	finished := join(tr.finish())
	if strings.Count(finished, "event: content_block_stop") != 2 {
		t.Errorf("should stop 2 tool blocks, got: %s", finished)
	}
	if !strings.Contains(finished, "message_delta") || !strings.Contains(finished, "message_stop") {
		t.Errorf("finish missing events: %s", finished)
	}
}

func TestAnthropicSSETransformer(t *testing.T) {
	tr := newAnthropicSSETransformer("m1")

	// 首个 chunk：文本。
	events := tr.consume([]byte(`{"choices":[{"delta":{"content":"He"}}]}`))
	joined := joinEvents(events)
	if !strings.Contains(joined, "message_start") {
		t.Errorf("missing message_start")
	}
	if !strings.Contains(joined, "content_block_start") || !strings.Contains(joined, "text_delta") {
		t.Errorf("missing text block start/delta")
	}

	// 第二个文本 chunk：不应重复 start。
	events = tr.consume([]byte(`{"choices":[{"delta":{"content":"llo"}}]}`))
	joined = joinEvents(events)
	if strings.Contains(joined, "content_block_start") {
		t.Errorf("should not start new block for continuing text: %s", joined)
	}
	if !strings.Contains(joined, "llo") {
		t.Errorf("delta content missing")
	}

	// 工具调用 chunk。
	events = tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_goal","arguments":"{\"a\":"}}]}}]}`))
	joined = joinEvents(events)
	if !strings.Contains(joined, "content_block_start") || !strings.Contains(joined, "tool_use") {
		t.Errorf("missing tool_use block start: %s", joined)
	}
	if !strings.Contains(joined, "input_json_delta") {
		t.Errorf("missing input_json_delta: %s", joined)
	}

	// 工具参数续段。
	events = tr.consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`))
	joined = joinEvents(events)
	if !strings.Contains(joined, "1}") {
		t.Errorf("args continuation lost: %s", joined)
	}

	// 结束 chunk：只触发 finish_reason 记录，收尾事件由 finish() 发出。
	events = tr.consume([]byte(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`))
	joined = joinEvents(events)
	if strings.Contains(joined, "message_stop") {
		t.Errorf("finish events should come from finish(), got %s", joined)
	}

	finished := joinEvents(tr.finish())
	if !strings.Contains(finished, "content_block_stop") {
		t.Errorf("missing content_block_stop")
	}
	if !strings.Contains(finished, "tool_use") || !strings.Contains(finished, "message_delta") {
		t.Errorf("stop_reason should be tool_use: %s", finished)
	}
	if !strings.Contains(finished, "message_stop") {
		t.Errorf("missing message_stop")
	}
}

func joinEvents(events [][]byte) string {
	var b strings.Builder
	for _, e := range events {
		b.Write(e)
	}
	return b.String()
}
