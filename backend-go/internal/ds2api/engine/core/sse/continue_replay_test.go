package sse

import (
	"encoding/json"
	"testing"
)

// These tests regress the continue/resume snapshot-envelope replay bug. When a
// stream is interrupted and resumed through the upstream continue endpoint, the
// opening envelope replays every already-accumulated fragment (including the
// full THINK fragment). That replay is historical content, not a live stream
// event, so it must never rewind the sticky segment cursor back to "thinking"
// once the message has already entered the content (text) segment. Otherwise
// every pathless {"v":"..."} body block that follows the envelope gets
// misclassified as reasoning_content and the real body disappears from content.
//
// The synthetic SSE below mirrors the continue round of the real sample
// tests/raw_stream_samples/continue-thinking-snapshot-replay-20260405.

func TestParseSSEChunkForContentReplayEnvelopeThinkMovesToThinking(t *testing.T) {
	// A replayed THINK fragment is an explicit reasoning marker. While the
	// message is still reasoning it moves the sticky cursor to thinking
	// (RESPONSE is the only way back to text). But once the body has already
	// begun (cursor text) the one-size-fits-all rule applies: the replay is
	// dropped wholesale and never rewinds the cursor.
	chunk := map[string]any{
		"v": map[string]any{
			"response": map[string]any{
				"fragments": []any{
					map[string]any{
						"type":    "THINK",
						"content": "完整的历史思考片段，长度远超三十二个字符以便触发续写重叠去重。",
					},
				},
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected envelope not to finish the stream")
	}
	if nextType != "text" {
		t.Fatalf("replayed THINK fragment must not rewind a text cursor, got %q", nextType)
	}
	if len(parts) != 0 {
		t.Fatalf("replayed THINK content after body start should be dropped, got %#v", parts)
	}

	// The RESPONSE fragment (as seen in real continue rounds) keeps text; the
	// pathless body blocks that follow must stay text.
	chunk2 := map[string]any{
		"p": "response/fragments",
		"o": "APPEND",
		"v": []any{
			map[string]any{"type": "RESPONSE", "content": "```html\n"},
		},
	}
	parts2, _, nextType2 := ParseSSEChunkForContent(chunk2, true, nextType)
	if nextType2 != "text" {
		t.Fatalf("RESPONSE fragment must keep cursor text, got %q", nextType2)
	}
	if len(parts2) != 1 || parts2[0].Type != "text" || parts2[0].Text != "```html\n" {
		t.Fatalf("expected RESPONSE content as text, got %#v", parts2)
	}

	parts3, _, nextType3 := ParseSSEChunkForContent(map[string]any{"v": "<div>正文</div>"}, true, nextType2)
	if nextType3 != "text" {
		t.Fatalf("pathless body must keep text cursor, got %q", nextType3)
	}
	if len(parts3) != 1 || parts3[0].Type != "text" || parts3[0].Text != "<div>正文</div>" {
		t.Fatalf("expected pathless body classified as text, got %#v", parts3)
	}
}

func TestParseSSEChunkForContentReplayEnvelopeThinkKeepsThinkingCursor(t *testing.T) {
	// A message still in the thinking segment resumes with a "thinking" cursor:
	// the replayed THINK fragment keeps it thinking.
	chunk := map[string]any{
		"v": map[string]any{
			"fragments": []any{
				map[string]any{
					"type":    "THINK",
					"content": "还在思考之中，尚未翻到正文。",
				},
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "thinking")
	if finished {
		t.Fatal("expected envelope not to finish the stream")
	}
	if nextType != "thinking" {
		t.Fatalf("replayed THINK fragment must keep thinking cursor, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "thinking" {
		t.Fatalf("expected thinking part from replay, got %#v", parts)
	}
}

func TestParseSSEChunkForContentReplayEnvelopeResponseStillFlipsToText(t *testing.T) {
	// A replay envelope ending in a RESPONSE fragment keeps a text cursor. The
	// replayed THINK fragment is dropped wholesale (one-size-fits-all rule:
	// body already begun) and only the RESPONSE body survives.
	chunk := map[string]any{
		"v": map[string]any{
			"response": map[string]any{
				"fragments": []any{
					map[string]any{
						"type":    "THINK",
						"content": "完整的历史思考片段，长度远超三十二个字符以便触发续写重叠去重。",
					},
					map[string]any{
						"type":    "RESPONSE",
						"content": "```html\n",
					},
				},
			},
		},
	}
	parts, finished, nextType := ParseSSEChunkForContent(chunk, true, "text")
	if finished {
		t.Fatal("expected envelope not to finish the stream")
	}
	if nextType != "text" {
		t.Fatalf("expected envelope to settle on text cursor, got %q", nextType)
	}
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "```html\n" {
		t.Fatalf("expected only the RESPONSE body parts, got %#v", parts)
	}
}

// TestParseDeepSeekContentLineContinueReplayBodyStaysText feeds a synthetic
// continue round through ParseDeepSeekContentLine mirroring real DeepSeek
// streams: the opening envelope replays the accumulated THINK fragment, the
// content segment begins with a RESPONSE fragment, and the pathless body
// blocks that follow must all stay classified as text so the final RawText
// carries the body instead of reasoning_content.
func TestParseDeepSeekContentLineContinueReplayBodyStaysText(t *testing.T) {
	thinkingReplay := "完整的历史思考片段，模型思考了很久才决定翻到正文，这足够长以触发去重。"
	envelope := map[string]any{
		"v": map[string]any{
			"response": map[string]any{
				"fragments": []any{
					map[string]any{"type": "THINK", "content": thinkingReplay},
				},
			},
		},
	}
	envBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope failed: %v", err)
	}

	lines := [][]byte{
		append([]byte("data: "), envBytes...),
		// Real continue rounds announce the content segment with a RESPONSE
		// fragment before the pathless body blocks (observed in every captured
		// stream, including inside response BATCH).
		[]byte(`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":"<div>正文内容</div>"}]}`),
		[]byte(`data: {"v":"<p>更多正文内容</p>"}`),
		[]byte(`data: {"p":"response/status","v":"FINISHED"}`),
		[]byte(`data: [DONE]`),
	}

	cursor := "text" // message already in the content segment before interruption
	var rawText, rawThinking []byte
	finished := false
	for _, line := range lines {
		res := ParseDeepSeekContentLine(line, true, cursor)
		cursor = res.NextType
		for _, p := range res.Parts {
			if p.Type == "thinking" {
				rawThinking = append(rawThinking, []byte(p.Text)...)
			} else {
				rawText = append(rawText, []byte(p.Text)...)
			}
		}
		if res.Stop {
			finished = true
		}
	}
	if !finished {
		t.Fatal("expected stream to reach the finished status")
	}
	if got := string(rawText); got != "<div>正文内容</div><p>更多正文内容</p>" {
		t.Fatalf("expected body blocks to land in text, got %q", got)
	}
	// 一刀切: the message was already in the content segment (cursor text), so
	// the replayed THINK fragment is dropped wholesale — no thinking is
	// surfaced once the body has begun.
	if got := string(rawThinking); got != "" {
		t.Fatalf("expected replayed THINK to be dropped after body start, got %q", got)
	}
}
