package openai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNormalizeUpstreamType(t *testing.T) {
	cases := map[string]string{
		"":          "openai",
		"openai":    "openai",
		"OpenAI":    "openai",
		"gemini":    "gemini",
		"Gemini":    "gemini",
		"aistudio":  "gemini",
		"generativelanguage": "gemini",
		"  gemini  ": "gemini",
		"random":    "openai",
	}
	for in, want := range cases {
		if got := normalizeUpstreamType(in); got != want {
			t.Errorf("normalizeUpstreamType(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestOpenAIChatToGeminiBasic(t *testing.T) {
	body := map[string]interface{}{
		"model":       "gemini-3.7-flash",
		"temperature": 0.7,
		"max_tokens":  float64(128),
		"stream":      true,
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Hi there"},
			map[string]interface{}{"role": "user", "content": "How are you?"},
		},
	}
	g, err := openAIChatToGemini(body)
	if err != nil {
		t.Fatalf("openAIChatToGemini error: %v", err)
	}
	if g["model"] != "gemini-3.7-flash" {
		t.Errorf("model = %v", g["model"])
	}
	if g["system_instruction"] != "You are helpful." {
		t.Errorf("system_instruction = %v; want system text", g["system_instruction"])
	}
	if g["stream"] != true {
		t.Errorf("stream = %v; want true", g["stream"])
	}
	if g["store"] != false {
		t.Errorf("store = %v; want false (stateless)", g["store"])
	}
	input, ok := g["input"].([]interface{})
	if !ok {
		t.Fatalf("input missing: %#v", g)
	}
	if len(input) != 3 {
		t.Fatalf("input len = %d; want 3", len(input))
	}
	if s0 := input[0].(map[string]interface{}); s0["type"] != "user_input" {
		t.Errorf("input[0].type = %v; want user_input", s0["type"])
	}
	if s1 := input[1].(map[string]interface{}); s1["type"] != "model_output" {
		t.Errorf("input[1].type = %v; want model_output", s1["type"])
	}
	cfg, ok := g["generation_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("generation_config missing")
	}
	if cfg["temperature"] != 0.7 || cfg["max_output_tokens"] != 128 {
		t.Errorf("generation_config = %#v", cfg)
	}
}

func TestOpenAIChatToGeminiTools(t *testing.T) {
	body := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "What is the temp in London?"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Gets weather",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
		"tool_choice": map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}},
	}
	g, err := openAIChatToGemini(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	tools, ok := g["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", g["tools"])
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "get_weather" || tool["type"] != "function" {
		t.Errorf("tool = %#v", tool)
	}
	tc, ok := g["tool_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_config missing")
	}
	fcc, ok := tc["function_calling_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("function_calling_config missing")
	}
	if fcc["mode"] != "ANY" {
		t.Errorf("mode = %v", fcc["mode"])
	}
}

func TestOpenAIChatToGeminiToolHistory(t *testing.T) {
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
	g, err := openAIChatToGemini(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	input := g["input"].([]interface{})
	if len(input) != 3 {
		t.Fatalf("input len = %d; want 3", len(input))
	}
	fcStep := input[1].(map[string]interface{})
	if fcStep["type"] != "function_call" || fcStep["name"] != "get_weather" {
		t.Errorf("assistant step = %#v", fcStep)
	}
	frStep := input[2].(map[string]interface{})
	if frStep["type"] != "function_result" || frStep["name"] != "get_weather" {
		t.Errorf("tool step = %#v", frStep)
	}
}

func TestOpenAIChatToGeminiImage(t *testing.T) {
	body := map[string]interface{}{
		"model": "gemini-3.7-flash",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "what is this"},
					map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,aGVsbG8="}},
				},
			},
		},
	}
	g, err := openAIChatToGemini(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	input := g["input"].([]interface{})
	step := input[0].(map[string]interface{})
	parts := step["content"].([]interface{})
	if len(parts) != 2 {
		t.Fatalf("parts len = %d; want 2", len(parts))
	}
	img := parts[1].(map[string]interface{})
	if img["type"] != "inline_data" {
		t.Fatalf("img type = %v", img["type"])
	}
	blob := img["data"].(map[string]interface{})
	if blob["mime_type"] != "image/png" || blob["data"] != "aGVsbG8=" {
		t.Errorf("blob = %#v", blob)
	}
}

func TestOpenAIChatToGeminiJSONMode(t *testing.T) {
	body := map[string]interface{}{
		"model":           "gemini-3.7-flash",
		"response_format": map[string]interface{}{"type": "json_object"},
		"messages":        []interface{}{map[string]interface{}{"role": "user", "content": "json please"}},
	}
	g, err := openAIChatToGemini(body)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	rf := g["response_format"].(map[string]interface{})
	if rf["mime_type"] != "application/json" {
		t.Errorf("response_format = %#v", rf)
	}
}

func TestGeminiToOpenAIChat(t *testing.T) {
	raw := `{
		"id":"v1_testid",
		"status":"completed",
		"created":"2026-09-02T10:00:00Z",
		"model":"gemini-3.7-flash",
		"usage":{"total_tokens":100,"total_input_tokens":40,"total_output_tokens":60,"total_cached_tokens":10},
		"steps":[
			{"type":"thought","signature":"sig1"},
			{"type":"model_output","content":[{"type":"text","text":"Hello"},{"type":"text","text":" world"}]}
		],
		"object":"interaction"
	}`
	out, err := geminiToOpenAIChat([]byte(raw), "fallback")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
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
	if usage["prompt_tokens"] != float64(40) || usage["completion_tokens"] != float64(60) {
		t.Errorf("usage = %#v", usage)
	}
	if parsed["model"] != "gemini-3.7-flash" {
		t.Errorf("model = %v", parsed["model"])
	}
}

func TestGeminiToOpenAIChatFunctionCall(t *testing.T) {
	raw := `{
		"id":"v1_x","status":"requires_action","model":"gemini-3.7-flash",
		"steps":[
			{"type":"function_call","id":"fc1","name":"get_weather","arguments":"{\"location\":\"London\"}"}
		],
		"usage":{"total_tokens":5,"total_input_tokens":2,"total_output_tokens":3}
	}`
	out, err := geminiToOpenAIChat([]byte(raw), "m")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	var parsed map[string]interface{}
	_ = json.Unmarshal(out, &parsed)
	choices := parsed["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v; want tool_calls", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]interface{})
	toolCalls := msg["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls len = %d", len(toolCalls))
	}
	fc := toolCalls[0].(map[string]interface{})["function"].(map[string]interface{})
	if fc["name"] != "get_weather" {
		t.Errorf("fn name = %v", fc["name"])
	}
}

func TestGeminiInteractionSSETransformer(t *testing.T) {
	tr := newGeminiInteractionSSETransformer("gemini-3.6-flash")
	events := []string{
		`{"event_type":"interaction.created","interaction":{"id":"v1_abc","status":"in_progress","model":"gemini-3.6-flash"}}`,
		`{"event_type":"interaction.status_update","interaction_id":"v1_abc","status":"in_progress"}`,
		`{"event_type":"step.start","index":0,"step":{"type":"thought"}}`,
		`{"event_type":"step.start","index":1,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"text","text":"4 5"}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"text","text":" 6"}}`,
		`{"event_type":"interaction.completed","interaction":{"id":"v1_abc","status":"incomplete","usage":{"total_tokens":68,"total_input_tokens":8,"total_output_tokens":3},"model":"gemini-3.6-flash"}}`,
	}
	var chunks [][]byte
	for _, e := range events {
		chunks = append(chunks, tr.consume([]byte(e))...)
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
	if text != "4 5 6" {
		t.Errorf("streamed text = %q; want %q", text, "4 5 6")
	}
	if usage == nil {
		t.Fatal("usage chunk missing")
	}
	if usage["prompt_tokens"] != float64(8) || usage["completion_tokens"] != float64(3) {
		t.Errorf("usage = %#v", usage)
	}
	if !sawDone {
		t.Error("missing [DONE]")
	}
}

func TestGeminiModelsList(t *testing.T) {
	raw := `{"models":[
		{"name":"models/gemini-3.7-flash","supportedGenerationMethods":["generateContent"]},
		{"name":"models/gemini-embedding-2","supportedGenerationMethods":["embedContent","countTokens"]},
		{"name":"models/veo-3.1-generate-preview","supportedGenerationMethods":["predictLongRunning"]}
	]}`
	got := geminiModelsList([]byte(raw))
	if len(got) != 1 || got[0] != "gemini-3.7-flash" {
		t.Errorf("geminiModelsList = %v; want [gemini-3.7-flash] (embedding/veo filtered)", got)
	}
}

func TestGeminiLineReader(t *testing.T) {
	firstChunk := []byte("event: interaction.created\ndata: {\"x\":1}\n\nevent: step.")
	rest := strings.NewReader("start\ndata: {\"y\":2}\n\n")
	lr := newGeminiLineReader(rest, firstChunk)
	var lines []string
	for {
		line, err := lr.readLine(context.Background(), time.Second)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("readLine error: %v", err)
		}
		lines = append(lines, string(line))
	}
	want := []string{
		"event: interaction.created",
		`data: {"x":1}`,
		"",
		"event: step.start",
		`data: {"y":2}`,
		"",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v; want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line[%d] = %q; want %q", i, lines[i], want[i])
		}
	}
}

func TestGeminiURLs(t *testing.T) {
	if got := geminiModelsURL("https://generativelanguage.googleapis.com"); got != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Errorf("models url = %q", got)
	}
	if got := geminiInteractionsURL("https://generativelanguage.googleapis.com"); got != "https://generativelanguage.googleapis.com/v1beta/interactions" {
		t.Errorf("interactions url = %q", got)
	}
}
