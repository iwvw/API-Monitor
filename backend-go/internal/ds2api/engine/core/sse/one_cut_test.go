package sse

import (
	"strings"
	"testing"
)

// These tests freeze the one-size-fits-all (一刀切) rule: once a request has
// switched from reasoning to the visible answer — i.e. content output has
// already begun (the sticky segment cursor reached "text") — every residual
// think / thinking / reasoning increment that later arrives is dropped
// wholesale. It is neither surfaced as a thinking/reasoning_content delta nor
// folded back into the body content. The upstream DeepSeek bug is that
// reasoning increments keep arriving after the model has moved on to the body;
// this is the whole-request guarantee that no thinking reaches the client once
// the body has started.

// TestOneCutRuleDropsThinkingAfterBodyStarted feeds a synthetic stream that
// first goes through a normal thinking segment, then begins the visible answer,
// and then (bug) keeps delivering reasoning increments mixed in with the body.
// Downstream must only ever receive the body text.
func TestOneCutRuleDropsThinkingAfterBodyStarted(t *testing.T) {
	lines := []string{
		`{"p":"response/fragments","o":"APPEND","v":[{"type":"THINK","content":"第一段思考"}]}`,
		`{"p":"response/fragments/-1/content","v":"第二段思考"}`,
		`{"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":"正文开始"}]}`,
		`{"p":"response/fragments/-1/content","v":"正文继续"}`,
		`{"p":"response/content","v":"正文结尾"}`,
		// The bug: upstream keeps sending reasoning increments after the body
		// began — as response/thinking_content patches and THINK fragments. All
		// of them must be dropped.
		`{"p":"response/thinking_content","v":"残余思考不该出现"}`,
		`{"p":"response/fragments","o":"APPEND","v":[{"type":"THINK","content":"更不该出现"}]}`,
		`{"p":"response/status","v":"FINISHED"}`,
	}

	cursor := "thinking"
	var body, thinking strings.Builder
	for _, raw := range lines {
		res := ParseDeepSeekContentLine([]byte("data: "+raw), true, cursor)
		cursor = res.NextType
		for _, p := range res.Parts {
			if p.Type == "thinking" {
				thinking.WriteString(p.Text)
			} else {
				body.WriteString(p.Text)
			}
		}
	}
	if got := body.String(); got != "正文开始正文继续正文结尾" {
		t.Fatalf("unexpected downstream body: %q", got)
	}
	if got := thinking.String(); got != "第一段思考第二段思考" {
		t.Fatalf("thinking after body start must be dropped, got %q", got)
	}
}

// TestOneCutRuleThinkingContentAfterBodyNotRecoveredAsText asserts that a
// response/thinking_content increment that arrives after the body has begun is
// dropped entirely — it must NOT be recovered into the body as text (the old
// behavior that leaked reasoning into the visible answer).
func TestOneCutRuleThinkingContentAfterBodyNotRecoveredAsText(t *testing.T) {
	parts, finished, nextType := ParseSSEChunkForContent(map[string]any{
		"p": "response/thinking_content",
		"v": "leaked reasoning",
	}, true, "text")
	if finished {
		t.Fatal("expected not finished")
	}
	if nextType != "text" {
		t.Fatalf("expected cursor to stay text, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("expected residual reasoning dropped, got %#v", parts)
	}
}

// TestOneCutRuleThinkFragmentAfterBodyDropped asserts a THINK fragment arriving
// after the body has begun is dropped wholesale and does not rewind the sticky
// cursor, so following body blocks stay classified as body text.
func TestOneCutRuleThinkFragmentAfterBodyDropped(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "THINK", "content": "replayed reasoning"},
		},
	}
	parts, _, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if nextType != "text" {
		t.Fatalf("THINK after body must not rewind cursor, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("expected THINK content dropped, got %#v", parts)
	}

	// The body block that follows stays text.
	parts2, _, nextType2 := ParseSSEChunkForContent(map[string]any{"v": "正文继续"}, true, nextType)
	if nextType2 != "text" {
		t.Fatalf("body block must stay text, got %q", nextType2)
	}
	if len(parts2) != 1 || parts2[0].Type != "text" || parts2[0].Text != "正文继续" {
		t.Fatalf("expected body text after dropped THINK, got %#v", parts2)
	}
}

// TestOneCutRuleThinkingDisabledStillHidesReasoning guards against the rule
// breaking the thinking-disabled path: when reasoning is hidden the sticky
// cursor must still track the upstream THINK segment so the hidden reasoning
// blocks are not leaked into the visible body.
func TestOneCutRuleThinkingDisabledStillHidesReasoning(t *testing.T) {
	chunk1 := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "THINK", "content": "我们"},
		},
	}
	parts1, _, nextType1 := ParseSSEChunkForContent(chunk1, false, "text")
	if nextType1 != "thinking" {
		t.Fatalf("thinking-disabled stream must keep hidden THINK cursor, got %q", nextType1)
	}
	if len(parts1) != 0 {
		t.Fatalf("hidden thinking must be dropped, got %#v", parts1)
	}
	chunk2 := map[string]any{"v": "要求"}
	parts2, _, nextType2 := ParseSSEChunkForContent(chunk2, false, nextType1)
	if nextType2 != "thinking" {
		t.Fatalf("hidden continuation must stay hidden, got %q", nextType2)
	}
	if len(parts2) != 0 {
		t.Fatalf("hidden continuation must be dropped, got %#v", parts2)
	}
	chunk3 := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "RESPONSE", "content": "答"},
		},
	}
	parts3, _, nextType3 := ParseSSEChunkForContent(chunk3, false, nextType2)
	if nextType3 != "text" {
		t.Fatalf("RESPONSE must flip cursor to text, got %q", nextType3)
	}
	if len(parts3) != 1 || parts3[0].Type != "text" || parts3[0].Text != "答" {
		t.Fatalf("expected visible response text, got %#v", parts3)
	}
}

// TestOneCutRuleSameChunkThinkBeforeResponseKept verifies that within a single
// chunk where the message is still reasoning, a THINK fragment that precedes
// the RESPONSE fragment is genuine reasoning and must still be surfaced; the
// rule only kicks in once the body has already begun.
func TestOneCutRuleSameChunkThinkBeforeResponseKept(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "THINK", "content": "deep thought"},
			map[string]any{"type": "RESPONSE", "content": "answer"},
		},
	}
	parts, _, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if nextType != "text" {
		t.Fatalf("expected cursor text after RESPONSE, got %q", nextType)
	}
	if len(parts) != 2 || parts[0].Type != "thinking" || parts[1].Type != "text" {
		t.Fatalf("expected thinking then text, got %#v", parts)
	}
}

// TestOneCutRuleLeakedThinkCloseInSingleChunkDropped asserts that a thinking
// increment carrying a stray </think> is still dropped wholesale once the body
// has already begun (it is not resurrected as body text).
func TestOneCutRuleLeakedThinkCloseInSingleChunkDropped(t *testing.T) {
	parts, _, nextType := ParseSSEChunkForContent(map[string]any{
		"p": "response/thinking_content",
		"v": "leaked</think>still leaked",
	}, true, "text")
	if nextType != "text" {
		t.Fatalf("expected cursor to stay text, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("expected leaked thinking dropped wholesale, got %#v", parts)
	}
}

// TestOneCutRuleKeepsUpstreamThinkingDetection asserts that even though the
// residual thinking is dropped from the visible parts once the body has begun,
// it is still reported through ToolDetectionThinkingParts so the caller can
// tell the upstream stream was still producing activity (stop-stream detection,
// no-content watchdog, etc.).
func TestOneCutRuleKeepsUpstreamThinkingDetection(t *testing.T) {
	chunk := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "THINK", "content": "upstream still thinking"},
		},
	}
	parts, detection, _, nextType := ParseSSEChunkForContentDetailed(chunk, true, "text")
	if nextType != "text" {
		t.Fatalf("expected cursor to stay text, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("expected visible thinking dropped, got %#v", parts)
	}
	if len(detection) != 1 || detection[0].Type != "thinking" || detection[0].Text != "upstream still thinking" {
		t.Fatalf("expected upstream thinking detected, got %#v", detection)
	}
}
