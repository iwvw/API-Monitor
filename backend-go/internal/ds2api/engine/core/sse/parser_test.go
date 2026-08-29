package sse

import "testing"

func TestParseDeepSeekSSELine(t *testing.T) {
	chunk, done, ok := ParseDeepSeekSSELine([]byte(`data: {"v":"你好"}`))
	if !ok || done {
		t.Fatalf("expected parsed chunk")
	}
	if chunk["v"] != "你好" {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
}

func TestParseDeepSeekSSELineDone(t *testing.T) {
	_, done, ok := ParseDeepSeekSSELine([]byte(`data: [DONE]`))
	if !ok || !done {
		t.Fatalf("expected done signal")
	}
}

func TestParseSSEChunkForContentSimple(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{"v": "hello"}, false, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Text != "hello" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentThinking(t *testing.T) {
	parts, finished, _ := ParseSSEChunkForContent(map[string]any{"p": "response/thinking_content", "v": "think"}, true, "thinking")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestIsCitation(t *testing.T) {
	if !IsCitation("[citation:1] abc") {
		t.Fatal("expected citation true")
	}
	if IsCitation("normal text") {
		t.Fatal("expected citation false")
	}
}

func TestParseSSEChunkForContentFragmentsAppendSwitchToResponse(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{
				"type":    "RESPONSE",
				"content": "你好",
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if finished {
		t.Fatal("expected unfinished")
	}
	if nextType != "text" {
		t.Fatalf("expected next type text, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "你好" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentAfterAppendUsesUpdatedType(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments/-1/content",
		"v": "！",
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if nextType != "text" {
		t.Fatalf("expected next type text, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "！" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentThinkingDisabledKeepsHiddenFragmentState(t *testing.T) {
	chunk1 := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "THINK", "content": "我们"},
		},
	}
	parts1, finished1, nextType1 := ParseSSEChunkForContent(chunk1, false, "text")
	if finished1 {
		t.Fatal("expected first chunk unfinished")
	}
	if nextType1 != "thinking" {
		t.Fatalf("expected hidden THINK fragment to keep next type thinking, got %q", nextType1)
	}
	if len(parts1) != 0 {
		t.Fatalf("expected hidden thinking to be dropped, got %#v", parts1)
	}

	chunk2 := map[string]any{
		"p": "response/fragments/-1/content",
		"v": "被",
	}
	parts2, finished2, nextType2 := ParseSSEChunkForContent(chunk2, false, nextType1)
	if finished2 {
		t.Fatal("expected second chunk unfinished")
	}
	if nextType2 != "thinking" {
		t.Fatalf("expected hidden continuation to keep next type thinking, got %q", nextType2)
	}
	if len(parts2) != 0 {
		t.Fatalf("expected hidden continuation to be dropped, got %#v", parts2)
	}

	chunk3 := map[string]any{"v": "要求"}
	parts3, finished3, nextType3 := ParseSSEChunkForContent(chunk3, false, nextType2)
	if finished3 {
		t.Fatal("expected third chunk unfinished")
	}
	if nextType3 != "thinking" {
		t.Fatalf("expected pathless hidden continuation to keep next type thinking, got %q", nextType3)
	}
	if len(parts3) != 0 {
		t.Fatalf("expected pathless hidden continuation to be dropped, got %#v", parts3)
	}

	chunk4 := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "RESPONSE", "content": "答"},
		},
	}
	parts4, finished4, nextType4 := ParseSSEChunkForContent(chunk4, false, nextType3)
	if finished4 {
		t.Fatal("expected fourth chunk unfinished")
	}
	if nextType4 != "text" {
		t.Fatalf("expected RESPONSE fragment to switch next type text, got %q", nextType4)
	}
	if len(parts4) != 1 || parts4[0].Type != "text" || parts4[0].Text != "答" {
		t.Fatalf("expected visible response text, got %#v", parts4)
	}
}

func TestParseSSEChunkForContentAutoTransitionsThinkClose(t *testing.T) {
	chunk := map[string]any{
		"p": "response/thinking_content",
		"v": "deep thoughts</think>actual answer",
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, true, "thinking")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts from split, got %d: %#v", len(parts), parts)
	}
	if parts[0].Type != "thinking" || parts[0].Text != "deep thoughts" {
		t.Fatalf("first part should be thinking: %#v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text != "actual answer" {
		t.Fatalf("second part should be text: %#v", parts[1])
	}
}

func TestParseSSEChunkForContentStripsLeakedThinkTags(t *testing.T) {
	chunk := map[string]any{
		"p": "response/thinking_content",
		"v": "<think>more thoughts</think>  answer",
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, true, "thinking")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
	}
	if parts[0].Type != "thinking" || parts[0].Text != "<think>more thoughts" {
		// note: the open tag is before the split, so it remains in the thinking part.
		// that's fine, the output sanitization handles the final string.
		t.Fatalf("first part mismatch: %#v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text != "  answer" {
		t.Fatalf("second part mismatch: %#v", parts[1])
	}
}

func TestParseSSEChunkForContentAutoTransitionsState(t *testing.T) {
	chunk1 := map[string]any{
		"p": "response/thinking_content",
		"v": "end of thought</think>start of text",
	}
	parts1, _, nextType1 := ParseSSEChunkForContent(chunk1, true, "thinking")
	if len(parts1) != 2 || parts1[1].Type != "text" {
		t.Fatalf("expected split parts, got %#v", parts1)
	}
	if nextType1 != "text" {
		t.Fatalf("expected nextType to transition to text, got %q", nextType1)
	}

	chunk2 := map[string]any{
		"p": "response/thinking_content",
		"v": "more actual text sent to thinking path",
	}
	parts2, _, nextType2 := ParseSSEChunkForContent(chunk2, true, nextType1)
	// 一刀切: the body has already begun, so this residual reasoning increment
	// is dropped wholesale — it is neither routed to thinking nor recovered
	// into the body as text.
	if len(parts2) != 0 {
		t.Fatalf("expected residual reasoning to be dropped, got %#v", parts2)
	}
	if nextType2 != "text" {
		t.Fatalf("expected nextType2 to remain text, got %q", nextType2)
	}
}

func TestParseSSEChunkForContentStripsLeakedThinkTagsFromText(t *testing.T) {
	chunk := map[string]any{
		"p": "response/content", // This makes the part type "text"
		"v": "normal text <think>leaked</think> end",
	}
	parts, _, _ := ParseSSEChunkForContent(chunk, true, "text")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d: %#v", len(parts), parts)
	}
	if parts[0].Type != "text" || parts[0].Text != "normal text leaked end" {
		t.Fatalf("expected leaked think tag to be stripped, got %#v", parts[0])
	}
}

func TestParseSSEChunkForContentResponseContentObjectShape(t *testing.T) {
	chunk := map[string]any{
		"p": "response/content",
		"v": map[string]any{"text": "对象内容"},
	}
	parts, finished, _ := ParseSSEChunkForContent(chunk, false, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Text != "对象内容" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForThinkingContentObjectShape(t *testing.T) {
	chunk := map[string]any{
		"p": "response/thinking_content",
		"v": map[string]any{"content": "对象思考"},
	}
	parts, finished, _ := ParseSSEChunkForContent(chunk, true, "thinking")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Text != "对象思考" || parts[0].Type != "thinking" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}

func TestParseSSEChunkForContentObjectShapeWithoutPath(t *testing.T) {
	chunk := map[string]any{
		"v": map[string]any{"text": "无路径对象内容"},
	}
	parts, finished, _ := ParseSSEChunkForContent(chunk, false, "text")
	if finished {
		t.Fatal("expected unfinished")
	}
	if len(parts) != 1 || parts[0].Text != "无路径对象内容" || parts[0].Type != "text" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}
