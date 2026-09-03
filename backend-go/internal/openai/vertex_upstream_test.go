package openai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
