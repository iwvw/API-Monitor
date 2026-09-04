package openai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVertexNormalizeUpstreamType(t *testing.T) {
	cases := map[string]string{
		"vertex":   "vertex",
		"VertexAI": "vertex",
		"vertex-ai": "vertex",
		"aiplatform": "vertex",
		"google-cloud": "vertex",
		"":         "openai",
		"openai":   "openai",
		"gemini":   "gemini",
	}
	for in, want := range cases {
		if got := normalizeUpstreamType(in); got != want {
			t.Errorf("normalizeUpstreamType(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestVertexGenerateURL(t *testing.T) {
	base := "https://us-central1-aiplatform.googleapis.com/v1/publishers/google"
	if got := vertexGenerateURL(base, "gemini-3.7-flash", false); got != base+"/models/gemini-3.7-flash:generateContent" {
		t.Errorf("non-stream url = %q", got)
	}
	if got := vertexGenerateURL(base, "gemini-3.7-flash", true); got != base+"/models/gemini-3.7-flash:streamGenerateContent?alt=sse" {
		t.Errorf("stream url = %q", got)
	}
}

func TestOpenAIChatToVertexBasic(t *testing.T) {
	body := map[string]interface{}{
		"model":       "gemini-3.7-flash",
		"temperature": 0.5,
		"max_tokens":  float64(64),
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "Be terse"},
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Hi"},
			map[string]interface{}{"role": "user", "content": "How?"},
		},
	}
	g, err := openAIChatToVertex(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	si := g["systemInstruction"].(map[string]interface{})
	siParts := si["parts"].([]interface{})
	if siParts[0].(map[string]interface{})["text"] != "Be terse" {
		t.Errorf("systemInstruction = %#v", si)
	}
	contents := g["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("contents len = %d; want 3", len(contents))
	}
	if contents[0].(map[string]interface{})["role"] != "user" {
		t.Errorf("contents[0].role = %v", contents[0].(map[string]interface{})["role"])
	}
	if contents[1].(map[string]interface{})["role"] != "model" {
		t.Errorf("contents[1].role = %v", contents[1].(map[string]interface{})["role"])
	}
	cfg := g["generationConfig"].(map[string]interface{})
	if cfg["temperature"] != 0.5 || cfg["maxOutputTokens"] != 64 {
		t.Errorf("generationConfig = %#v", cfg)
	}
}

func TestOpenAIChatToVertexTools(t *testing.T) {
	body := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "weather?"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Gets weather",
					"parameters": map[string]interface{}{"type": "object"},
				},
			},
		},
		"tool_choice": map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}},
	}
	g, err := openAIChatToVertex(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	tools := g["tools"].([]interface{})
	decls := tools[0].(map[string]interface{})["functionDeclarations"].([]interface{})
	if decls[0].(map[string]interface{})["name"] != "get_weather" {
		t.Errorf("decl = %#v", decls[0])
	}
	tc := g["toolConfig"].(map[string]interface{})
	fcc := tc["functionCallingConfig"].(map[string]interface{})
	if fcc["mode"] != "ANY" {
		t.Errorf("mode = %v", fcc["mode"])
	}
}

func TestOpenAIChatToVertexToolHistory(t *testing.T) {
	body := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "weather?"},
			map[string]interface{}{
				"role": "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{"name": "get_weather", "arguments": `{"location":"London"}`},
					},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "22c"},
		},
	}
	g, err := openAIChatToVertex(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	contents := g["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("contents len = %d; want 3", len(contents))
	}
	assistant := contents[1].(map[string]interface{})
	fc := assistant["parts"].([]interface{})[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	if fc["name"] != "get_weather" {
		t.Errorf("functionCall = %#v", fc)
	}
	tool := contents[2].(map[string]interface{})
	fr := tool["parts"].([]interface{})[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
	if fr["name"] != "get_weather" {
		t.Errorf("functionResponse = %#v", fr)
	}
}

func TestVertexToOpenAIChat(t *testing.T) {
	raw := `{
		"candidates":[{
			"content":{"role":"model","parts":[{"text":"Hello"},{"text":" world"}]},
			"finishReason":"STOP","index":0
		}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
	}`
	out, err := vertexToOpenAIChat([]byte(raw), "gemini-3.7-flash")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	var parsed map[string]interface{}
	_ = json.Unmarshal(out, &parsed)
	choices := parsed["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hello world" {
		t.Errorf("content = %v", msg["content"])
	}
	usage := parsed["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(10) || usage["completion_tokens"] != float64(5) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestVertexSSETransformer(t *testing.T) {
	tr := newVertexSSETransformer("gemini-3.7-flash")
	lines := []string{
		`{"candidates":[{"content":{"parts":[{"text":"4"}],"role":"model"},"index":0}],"usageMetadata":null}`,
		`{"candidates":[{"content":{"parts":[{"text":"5"}],"role":"model"},"index":0}],"usageMetadata":null}`,
		`{"candidates":[{"content":{"parts":[{"text":"6"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3,"totalTokenCount":11}}`,
	}
	var chunks [][]byte
	for _, l := range lines {
		chunks = append(chunks, tr.consume([]byte(l))...)
	}
	chunks = append(chunks, tr.finish()...)

	var text string
	var usage map[string]interface{}
	var sawDone bool
	for _, c := range chunks {
		s := string(c)
		if strings.HasPrefix(s, "data: [DONE]") {
			sawDone = true
			continue
		}
		if !strings.HasPrefix(s, "data: ") {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(s[len("data: "):])), &chunk); err != nil {
			continue
		}
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			usage = u
			continue
		}
		choices, _ := chunk["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		delta, _ := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
		if d, ok := delta["content"].(string); ok {
			text += d
		}
	}
	if text != "456" {
		t.Errorf("streamed text = %q; want %q", text, "456")
	}
	if usage == nil || usage["prompt_tokens"] != float64(8) || usage["completion_tokens"] != float64(3) {
		t.Errorf("usage = %#v", usage)
	}
	if !sawDone {
		t.Error("missing [DONE]")
	}
}

func TestVertexUsageAccounting(t *testing.T) {
	u := &vertexUsage{
		PromptTokenCount:        10,
		ToolUsePromptTokenCount: 3,
		CandidatesTokenCount:    5,
		ThoughtsTokenCount:      7,
		TotalTokenCount:         30,
		CachedContentTokenCount: 4,
	}
	usage := vertexOpenAIUsage(u)
	if usage["prompt_tokens"] != 13 {
		t.Errorf("prompt_tokens = %v; want 13 (prompt+toolUse)", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != 12 {
		t.Errorf("completion_tokens = %v; want 12 (candidates+thoughts)", usage["completion_tokens"])
	}
	if usage["total_tokens"] != 30 {
		t.Errorf("total_tokens = %v; want 30", usage["total_tokens"])
	}
	pd, _ := usage["prompt_tokens_details"].(map[string]interface{})
	if pd["cached_tokens"] != 4 {
		t.Errorf("cached_tokens = %v", pd["cached_tokens"])
	}
	cd, _ := usage["completion_tokens_details"].(map[string]interface{})
	if cd["reasoning_tokens"] != 7 {
		t.Errorf("reasoning_tokens = %v", cd["reasoning_tokens"])
	}
	// nil 或空值回退 0，不 panic。
	if got := vertexOpenAIUsage(nil)["prompt_tokens"]; got != 0 {
		t.Errorf("nil usage prompt = %v", got)
	}
}

func TestVertexThoughtToReasoningContent(t *testing.T) {
	raw := `{
		"candidates":[{
			"content":{"role":"model","parts":[
				{"text":"let me think","thought":true},
				{"text":" answer"},
				{"text":" thinking too","thought":true}
			]},
			"finishReason":"STOP","index":0
		}],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"thoughtsTokenCount":7,"totalTokenCount":22}
	}`
	out, err := vertexToOpenAIChat([]byte(raw), "m")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	var parsed map[string]interface{}
	_ = json.Unmarshal(out, &parsed)
	msg := parsed["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"] != " answer" {
		t.Errorf("content = %v; want ' answer'", msg["content"])
	}
	if msg["reasoning_content"] != "let me think thinking too" {
		t.Errorf("reasoning_content = %v", msg["reasoning_content"])
	}
	usage := parsed["usage"].(map[string]interface{})
	if usage["completion_tokens"] != float64(12) {
		t.Errorf("completion_tokens = %v; want 12 (5+7)", usage["completion_tokens"])
	}
}

func TestVertexBlockReasonToOpenAIError(t *testing.T) {
	raw := `{"promptFeedback":{"blockReason":"SAFETY"},"candidates":[]}`
	out, err := vertexToOpenAIChat([]byte(raw), "m")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	var parsed map[string]interface{}
	_ = json.Unmarshal(out, &parsed)
	errObj, _ := parsed["error"].(map[string]interface{})
	if errObj["type"] != "prompt_blocked" {
		t.Errorf("error.type = %v; want prompt_blocked", errObj["type"])
	}
	if s, _ := errObj["message"].(string); s == "" {
		t.Error("blocked error message empty")
	}
}

func TestVertexFinishReasonMapping(t *testing.T) {
	cases := map[string]string{
		"":                        "stop",
		"STOP":                    "stop",
		"MAX_TOKENS":              "length",
		"MALFORMED_FUNCTION_CALL": "tool_calls",
		"SAFETY":                  "content_filter",
		"RECITATION":              "content_filter",
		"PROHIBITED_CONTENT":      "content_filter",
		"UNKNOWN_XYZ":             "content_filter",
	}
	for in, want := range cases {
		if got := vertexFinishReason(in); got != want {
			t.Errorf("vertexFinishReason(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestVertexStreamThoughtAndUsage(t *testing.T) {
	tr := newVertexSSETransformer("m")
	lines := []string{
		`{"candidates":[{"content":{"parts":[{"text":"think a","thought":true}],"role":"model"},"index":0}],"usageMetadata":null}`,
		`{"candidates":[{"content":{"parts":[{"text":"out"}],"role":"model"},"index":0}],"usageMetadata":null}`,
		`{"candidates":[{"content":{"parts":[{"text":" think b","thought":true}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"thoughtsTokenCount":3,"totalTokenCount":10}}`,
	}
	var reasoning strings.Builder
	var text strings.Builder
	for _, l := range lines {
		for _, c := range tr.consume([]byte(l)) {
			var chunk map[string]interface{}
			_ = json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(c, []byte("data: "))), &chunk)
			choices, _ := chunk["choices"].([]interface{})
			if len(choices) == 0 {
				continue
			}
			delta, _ := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
			if d, ok := delta["reasoning_content"].(string); ok {
				reasoning.WriteString(d)
			}
			if d, ok := delta["content"].(string); ok {
				text.WriteString(d)
			}
		}
	}
	for _, c := range tr.finish() {
		var chunk map[string]interface{}
		_ = json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(c, []byte("data: "))), &chunk)
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if usage["completion_tokens"] != float64(5) {
				t.Errorf("stream completion_tokens = %v; want 5 (2+3)", usage["completion_tokens"])
			}
			if usage["prompt_tokens"] != float64(5) {
				t.Errorf("stream prompt_tokens = %v; want 5", usage["prompt_tokens"])
			}
		}
	}
	if reasoning.String() != "think a think b" {
		t.Errorf("streamed reasoning = %q", reasoning.String())
	}
	if text.String() != "out" {
		t.Errorf("streamed text = %q", text.String())
	}
}

func TestVertexPartialArgsReassembly(t *testing.T) {
	tr := newVertexSSETransformer("m")
	lines := []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","id":"fc1","partialArgs":[{"jsonPath":"$.location","stringValue":"Lon"}],"willContinue":true}}],"role":"model"},"index":0}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","id":"fc1","partialArgs":[{"jsonPath":"$.location","stringValue":"don"},{"jsonPath":"$.units","value":"metric"}],"willContinue":true}}],"role":"model"},"index":0}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","id":"fc1","partialArgs":[{"jsonPath":"$.units","stringValue":"c"}],"willContinue":false}}],"role":"model"},"index":0}],"usageMetadata":{"promptTokenCount":4,"totalTokenCount":4}}`,
	}
	var gotArgs string
	var sawTc bool
	for _, l := range lines {
		for _, c := range tr.consume([]byte(l)) {
			var chunk map[string]interface{}
			if err := json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(c, []byte("data: "))), &chunk); err != nil {
				continue
			}
			choices, _ := chunk["choices"].([]interface{})
			if len(choices) == 0 {
				continue
			}
			delta, _ := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
			tcs, _ := delta["tool_calls"].([]interface{})
			if len(tcs) == 0 {
				continue
			}
			sawTc = true
			fn, _ := tcs[0].(map[string]interface{})["function"].(map[string]interface{})
			gotArgs, _ = fn["arguments"].(string)
			if fn["name"] != "get_weather" {
				t.Errorf("tool name = %v", fn["name"])
			}
		}
	}
	if !sawTc {
		t.Fatal("no tool_calls emitted")
	}
	var argsParsed map[string]interface{}
	if err := json.Unmarshal([]byte(gotArgs), &argsParsed); err != nil {
		t.Fatalf("bad args json %q: %v", gotArgs, err)
	}
	if argsParsed["location"] != "London" {
		t.Errorf("location = %v; want 'London' (split+append: Lon+don)", argsParsed["location"])
	}
	if argsParsed["units"] != "metricc" {
		t.Errorf("units = %v; want 'metricc' (metric+c)", argsParsed["units"])
	}
}

func TestVertexMergeMultipleToolResponses(t *testing.T) {
	req := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "run two tools"},
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "tool_a",
							"arguments": "{}",
						},
					},
					map[string]interface{}{
						"id":   "call_2",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "tool_b",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "res_a",
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": "call_2",
				"content":      "res_b",
			},
		},
	}
	out, err := openAIChatToVertex(req)
	if err != nil {
		t.Fatalf("openAIChatToVertex error: %v", err)
	}
	contents, ok := out["contents"].([]interface{})
	if !ok {
		t.Fatalf("contents not []interface{}")
	}
	// 应生成 3 个 turn：
	// 1: user "run two tools"
	// 2: model with 2 functionCalls
	// 3: user with 2 functionResponses merged into a single turn
	if len(contents) != 3 {
		t.Fatalf("len(contents) = %d; want 3 (tool responses must merge into one user turn)", len(contents))
	}
	modelTurn, _ := contents[1].(map[string]interface{})
	if modelTurn["role"] != "model" {
		t.Errorf("turn 1 role = %v; want model", modelTurn["role"])
	}
	modelParts, _ := modelTurn["parts"].([]interface{})
	if len(modelParts) != 2 {
		t.Errorf("model turn parts = %d; want 2", len(modelParts))
	}

	userToolTurn, _ := contents[2].(map[string]interface{})
	if userToolTurn["role"] != "user" {
		t.Errorf("turn 2 role = %v; want user", userToolTurn["role"])
	}
	toolParts, _ := userToolTurn["parts"].([]interface{})
	if len(toolParts) != 2 {
		t.Fatalf("merged tool turn parts = %d; want 2 (equal to function calls)", len(toolParts))
	}
	p1, _ := toolParts[0].(map[string]interface{})
	fr1, _ := p1["functionResponse"].(map[string]interface{})
	if fr1["name"] != "tool_a" {
		t.Errorf("fr1 name = %v; want tool_a", fr1["name"])
	}
	p2, _ := toolParts[1].(map[string]interface{})
	fr2, _ := p2["functionResponse"].(map[string]interface{})
	if fr2["name"] != "tool_b" {
		t.Errorf("fr2 name = %v; want tool_b", fr2["name"])
	}
}

func TestVertexSanitizeArrayItemsInAnyOf(t *testing.T) {
	req := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "list"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": "read_files",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"files": map[string]interface{}{
								"anyOf": []interface{}{
									map[string]interface{}{"type": "array"},
									map[string]interface{}{"type": "null"},
								},
							},
						},
					},
				},
			},
		},
	}
	out, err := openAIChatToVertex(req)
	if err != nil {
		t.Fatalf("openAIChatToVertex error: %v", err)
	}
	tools, ok := out["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("tools missing")
	}
	fDecls, _ := tools[0].(map[string]interface{})["functionDeclarations"].([]interface{})
	if len(fDecls) == 0 {
		t.Fatalf("functionDeclarations missing")
	}
	params, _ := fDecls[0].(map[string]interface{})["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	files, _ := props["files"].(map[string]interface{})
	anyOf, _ := files["anyOf"].([]interface{})
	if len(anyOf) != 2 {
		t.Fatalf("anyOf len = %d; want 2", len(anyOf))
	}
	arrBranch, _ := anyOf[0].(map[string]interface{})
	if arrBranch["type"] != "array" {
		t.Errorf("arrBranch type = %v; want array", arrBranch["type"])
	}
	if arrBranch["items"] == nil {
		t.Errorf("arrBranch.items is nil; want defaulted empty object map")
	}
}

func TestGoogleRetryAfterFromBody(t *testing.T) {
	// RetryInfo.retryDelay（标准 protobuf Duration 文本）。
	body := `{"error":{"code":429,"message":"Quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1.5s"}]}}`
	if d := googleRetryAfterFromBody([]byte(body)); d == nil || *d != 1500*time.Millisecond {
		t.Fatalf("RetryInfo retryDelay parsed = %v; want 1.5s", d)
	}

	// ErrorInfo.metadata.quotaResetDelay。
	body = `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","metadata":{"quotaResetDelay":"30s"}}]}}`
	if d := googleRetryAfterFromBody([]byte(body)); d == nil || *d != 30*time.Second {
		t.Fatalf("ErrorInfo quotaResetDelay parsed = %v; want 30s", d)
	}

	// 空/无延迟/无法解析 → nil。
	if d := googleRetryAfterFromBody(nil); d != nil {
		t.Fatalf("nil body parsed = %v; want nil", d)
	}
	if d := googleRetryAfterFromBody([]byte(`{"error":{"details":[]}}`)); d != nil {
		t.Fatalf("empty details parsed = %v; want nil", d)
	}
	if d := googleRetryAfterFromBody([]byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"oops"}]}}`)); d != nil {
		t.Fatalf("bad duration parsed = %v; want nil", d)
	}
	if d := googleRetryAfterFromBody([]byte(`not json`)); d != nil {
		t.Fatalf("non-json parsed = %v; want nil", d)
	}
	// 零延迟（retryDelay:"0s"）不算可等待窗口。
	if d := googleRetryAfterFromBody([]byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"0s"}]}}`)); d != nil {
		t.Fatalf("zero retryDelay parsed = %v; want nil", d)
	}
}

func TestSafeTokenSumGuards(t *testing.T) {
	if got := safeTokenSum(10, 3); got != 13 {
		t.Fatalf("10+3 = %d; want 13", got)
	}
	if got := safeTokenSum(-5, 3); got != 3 {
		t.Fatalf("-5+3 clamped = %d; want 3", got)
	}
	if got := safeTokenSum(5, -7); got != 5 {
		t.Fatalf("5+-7 clamped = %d; want 5", got)
	}
	if got := safeTokenSum(-1, -1); got != 0 {
		t.Fatalf("-1+-1 clamped = %d; want 0", got)
	}
}

func TestVertexUsageNegativeClamped(t *testing.T) {
	u := &vertexUsage{PromptTokenCount: -100, CandidatesTokenCount: -1, TotalTokenCount: 0}
	usage := vertexOpenAIUsage(u)
	if usage["prompt_tokens"] != 0 {
		t.Errorf("negative prompt_tokens = %v; want 0", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != 0 {
		t.Errorf("negative completion_tokens = %v; want 0", usage["completion_tokens"])
	}
	if usage["total_tokens"] != 0 {
		t.Errorf("total_tokens with negative parts = %v; want 0", usage["total_tokens"])
	}
}

func TestVertexGenerationConfigCandidateCount(t *testing.T) {
	body := map[string]interface{}{"model": "gemini-3.7-flash", "n": float64(3)}
	cfg := vertexGenerationConfig(body)
	if cfg["candidateCount"] != 3 {
		t.Errorf("candidateCount = %v; want 3", cfg["candidateCount"])
	}
	// n=1 或缺失时不下发 candidateCount（保持默认单候选）。
	body2 := map[string]interface{}{"model": "gemini-3.7-flash", "n": float64(1)}
	if _, ok := vertexGenerationConfig(body2)["candidateCount"]; ok {
		t.Error("n=1 should not set candidateCount")
	}
}

func TestVertexGenerationConfigThinkingLevelFor3X(t *testing.T) {
	body := map[string]interface{}{"model": "gemini-3-pro", "reasoning_effort": "high"}
	cfg := vertexGenerationConfig(body)
	tc, _ := cfg["thinkingConfig"].(map[string]interface{})
	if tc["thinkingLevel"] != "high" {
		t.Errorf("gemini-3 thinkingLevel = %v; want high", tc["thinkingLevel"])
	}
	if _, ok := tc["thinkingBudget"]; ok {
		t.Error("gemini-3 should not set thinkingBudget")
	}
	// 2.5 系仍用 thinkingBudget。
	body2 := map[string]interface{}{"model": "gemini-2.5-flash", "reasoning_effort": "low"}
	cfg2 := vertexGenerationConfig(body2)
	tc2, _ := cfg2["thinkingConfig"].(map[string]interface{})
	if tc2["thinkingBudget"] != 1024 {
		t.Errorf("gemini-2.5 thinkingBudget = %v; want 1024", tc2["thinkingBudget"])
	}
	// thinking_budget 显式覆盖：3.x 也走 budget（用户明确指定）。
	body3 := map[string]interface{}{"model": "gemini-3-pro", "thinking_budget": float64(4096)}
	cfg3 := vertexGenerationConfig(body3)
	tc3, _ := cfg3["thinkingConfig"].(map[string]interface{})
	if tc3["thinkingBudget"] != 4096 {
		t.Errorf("explicit thinking_budget = %v; want 4096", tc3["thinkingBudget"])
	}
}

func TestGemini3XModel(t *testing.T) {
	for _, m := range []string{"gemini-3-pro", "gemini-3-flash", "gemini-3.5-flash", "gemini-3.1-pro-preview"} {
		if !gemini3XModel(m) {
			t.Errorf("gemini3XModel(%q) = false; want true", m)
		}
	}
	for _, m := range []string{"gemini-2.5-flash", "gemini-2.0-flash", "claude-3", "gpt-4o"} {
		if gemini3XModel(m) {
			t.Errorf("gemini3XModel(%q) = true; want false", m)
		}
	}
}

func TestVertexResponseFormatJsonSchema(t *testing.T) {
	rf := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "table",
			"strict": true,
			"schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}},
		},
	}
	got := vertexResponseFormat(rf)
	if got["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType = %v", got["responseMimeType"])
	}
	rs, ok := got["responseJsonSchema"].(map[string]interface{})
	if !ok || rs["type"] != "object" {
		t.Errorf("responseJsonSchema = %#v; want schema passthrough", got["responseJsonSchema"])
	}
	// json_object 只设 mimeType，不传 schema。
	got2 := vertexResponseFormat(map[string]interface{}{"type": "json_object"})
	if got2["responseMimeType"] != "application/json" {
		t.Errorf("json_object mime = %v", got2["responseMimeType"])
	}
	if _, ok := got2["responseJsonSchema"]; ok {
		t.Error("json_object should not set responseJsonSchema")
	}
	// 未知类型不下发。
	if got3 := vertexResponseFormat(map[string]interface{}{"type": "text"}); len(got3) != 0 {
		t.Errorf("text response_format should map to nothing, got %#v", got3)
	}
}

func TestVertexLeadingModelTurnInsertedEmptyUser(t *testing.T) {
	// 多轮重放以 assistant 开头：Vertex 拒绝以 model 开头的 contents，
	// 转换时应前插空 user turn。
	body := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "assistant", "content": "previous answer"},
			map[string]interface{}{"role": "user", "content": "next"},
		},
	}
	g, err := openAIChatToVertex(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	contents := g["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("contents len = %d; want 3 (empty user + model + user)", len(contents))
	}
	if contents[0].(map[string]interface{})["role"] != "user" {
		t.Errorf("contents[0].role = %v; want user", contents[0].(map[string]interface{})["role"])
	}
	if contents[1].(map[string]interface{})["role"] != "model" {
		t.Errorf("contents[1].role = %v; want model", contents[1].(map[string]interface{})["role"])
	}
	// 正常以 user 开头不受影响。
	body2 := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}
	g2, err := openAIChatToVertex(body2)
	if err != nil {
		t.Fatalf("error2: %v", err)
	}
	c2 := g2["contents"].([]interface{})
	if len(c2) != 1 || c2[0].(map[string]interface{})["role"] != "user" {
		t.Fatalf("normal contents should be unchanged, got %#v", c2)
	}
}
