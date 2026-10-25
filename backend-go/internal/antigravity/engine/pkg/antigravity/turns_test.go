package antigravity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDropTrailingThoughtOnlyTurns(t *testing.T) {
	// 尾部只剩 thinking 的 model turn 是历史重放时的典型形态；上游把它当成
	// 「没有可回复内容」直接 400（CLIProxyAPI #6530）。
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "hi"}}},
		{Role: "model", Parts: []GeminiPart{{Text: "done"}}},
		{Role: "model", Parts: []GeminiPart{{Text: "still thinking", Thought: true}}},
	}

	got := dropTrailingThoughtOnlyTurns(contents)
	if len(got) != 2 {
		t.Fatalf("expected trailing thought-only turn dropped, got %d turns", len(got))
	}
	if got[1].Role != "model" || got[1].Parts[0].Text != "done" {
		t.Fatalf("unexpected surviving tail: %+v", got[1])
	}

	// 至少要保留一条，不能把 contents 清空。
	only := []GeminiContent{{Role: "model", Parts: []GeminiPart{{Text: "t", Thought: true}}}}
	if got := dropTrailingThoughtOnlyTurns(only); len(got) != 1 {
		t.Fatalf("must keep at least one turn, got %d", len(got))
	}
}

func TestDropTrailingThoughtOnlyTurnsKeepsToolTurns(t *testing.T) {
	// 带 functionCall 的 part 即使标了 thought 也不能丢（会破坏工具配对）。
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "go"}}},
		{Role: "model", Parts: []GeminiPart{{
			Thought:      true,
			FunctionCall: &GeminiFunctionCall{Name: "tool", ID: "call_1"},
		}}},
	}
	if got := dropTrailingThoughtOnlyTurns(contents); len(got) != 2 {
		t.Fatalf("turn with functionCall must be kept, got %d turns", len(got))
	}
}

func TestNormalizeContentsBoundaryGeminiAppendsUserTurn(t *testing.T) {
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "q"}}},
		{Role: "model", Parts: []GeminiPart{{Text: "a"}}},
	}
	got := normalizeContentsBoundary(contents, true)

	if len(got) != 3 {
		t.Fatalf("expected synthetic trailing user turn, got %d turns", len(got))
	}
	if got[2].Role != "user" {
		t.Fatalf("trailing role = %q, want user", got[2].Role)
	}
	if !got[2].Parts[0].forceEmptyText {
		t.Fatal("synthetic turn must force-emit an empty text part")
	}

	// 序列化后必须是 {"text":""}，不能因为 omitempty 变成空 part。
	raw, err := json.Marshal(got[2].Parts[0])
	if err != nil {
		t.Fatalf("marshal part 失败: %v", err)
	}
	if string(raw) != `{"text":""}` {
		t.Fatalf("synthetic part JSON = %s, want {\"text\":\"\"}", raw)
	}
}

func TestNormalizeContentsBoundaryGeminiPrependsUserTurn(t *testing.T) {
	contents := []GeminiContent{
		{Role: "model", Parts: []GeminiPart{{Text: "a"}}},
	}
	got := normalizeContentsBoundary(contents, true)
	if len(got) != 3 {
		t.Fatalf("expected leading+synthetic trailing user turns, got %d", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "model" || got[2].Role != "user" {
		t.Fatalf("unexpected roles: %s/%s/%s", got[0].Role, got[1].Role, got[2].Role)
	}
}

func TestNormalizeContentsBoundaryClaudeSkipsSyntheticTurn(t *testing.T) {
	// Claude 系上游适配层拒绝空文本 part，因此只丢弃尾部纯思考 turn，不补空 turn。
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "q"}}},
		{Role: "model", Parts: []GeminiPart{{Text: "a"}}},
		{Role: "model", Parts: []GeminiPart{{Text: "t", Thought: true}}},
	}
	got := normalizeContentsBoundary(contents, false)

	if len(got) != 2 {
		t.Fatalf("expected only trailing thought turn dropped, got %d turns", len(got))
	}
	for _, c := range got {
		for _, p := range c.Parts {
			if p.forceEmptyText {
				t.Fatal("Claude targets must not receive synthetic empty text turns")
			}
		}
	}
}

func TestNormalizeContentsBoundaryKeepsFunctionResponseTail(t *testing.T) {
	// 尾轮是带 functionResponse 的 model turn 时上游要靠它继续生成，不能补空 turn。
	contents := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "q"}}},
		{Role: "user", Parts: []GeminiPart{{
			FunctionResponse: &GeminiFunctionResponse{
				Name:     "tool",
				Response: map[string]any{"result": "ok"},
				ID:       "call_1",
			},
		}}},
	}
	got := normalizeContentsBoundary(contents, true)
	if len(got) != 2 {
		t.Fatalf("functionResponse tail must be preserved, got %d turns", len(got))
	}

	// 但尾轮 role 是 model 且 functionResponse 带嵌套媒体时，上游会 400，需补空 turn。
	withMedia := []GeminiContent{
		{Role: "user", Parts: []GeminiPart{{Text: "q"}}},
		{Role: "model", Parts: []GeminiPart{{
			FunctionResponse: &GeminiFunctionResponse{
				Name: "tool",
				Response: map[string]any{
					"parts": []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "x"}}},
				},
				ID: "call_1",
			},
		}}},
	}
	gotMedia := normalizeContentsBoundary(withMedia, true)
	if len(gotMedia) != 3 {
		t.Fatalf("media functionResponse tail should get a synthetic user turn, got %d", len(gotMedia))
	}
	if gotMedia[2].Role != "user" {
		t.Fatalf("trailing role = %q, want user", gotMedia[2].Role)
	}
}

func TestTransformDropsTrailingThoughtTurnForGemini(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model: "gemini-2.5-flash",
		Messages: []ClaudeMessage{
			{Role: "user", Content: []byte(`"hi"`)},
			{Role: "assistant", Content: []byte(`[{"type":"thinking","thinking":"pondering"}]`)},
		},
	}

	body, err := TransformClaudeToGemini(claudeReq, "proj", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("TransformClaudeToGemini 失败: %v", err)
	}

	var req V1InternalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("解析转换结果失败: %v", err)
	}

	// 纯 thinking 的 assistant turn 应被丢掉，并补一个空 user turn。
	last := req.Request.Contents[len(req.Request.Contents)-1]
	if last.Role != "user" {
		t.Fatalf("last role = %q, want user", last.Role)
	}
	for _, content := range req.Request.Contents {
		if content.Role != "model" {
			continue
		}
		for _, part := range content.Parts {
			if part.Thought && strings.TrimSpace(part.Text) != "" {
				t.Fatalf("trailing thought-only model turn should be dropped, found %q", part.Text)
			}
		}
	}
}
