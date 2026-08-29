package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

// Regression tests for the continue/resume cursor bug: a resume round must not
// blindly re-seed the segment cursor to "thinking" when the message has already
// entered the content (text) segment. The synthetic SSE below mirrors the
// continue round of the real sample
// tests/raw_stream_samples/continue-thinking-snapshot-replay-20260405: an
// opening snapshot envelope that replays the accumulated THINK fragment,
// followed by pathless {"v":"..."} body blocks.

func TestConsumeChatStreamAttemptResumeKeepsContentSegmentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	streamRuntime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"cid-continue",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		true,  // thinkingEnabled
		false, // searchEnabled
		true,  // stripReferenceMarkers
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false, // bufferToolContent
		false, // emitEarlyToolDeltas
	)

	// First attempt produced the code-fence shell before the stream died
	// mid-body; seed the accumulator to that state so the resume cursor must be
	// "text", not "thinking".
	streamRuntime.accumulator.RawText.WriteString("```html\n")
	streamRuntime.accumulator.Text.WriteString("```html\n")

	thinkingReplay := "完整的历史思考片段，模型思考了很久才决定翻到正文，这足够长以触发去重。"
	env, err := json.Marshal(map[string]any{
		"v": map[string]any{
			"response": map[string]any{
				"fragments": []any{
					map[string]any{"type": "THINK", "content": thinkingReplay},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal envelope failed: %v", err)
	}

	resp := makeOpenAISSEHTTPResponse(
		`data: `+string(env),
		// Real continue rounds announce the content segment with a RESPONSE
		// fragment before the pathless body blocks.
		`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":"<div>正文内容</div>"}]}`,
		`data: {"v":"<p>更多正文内容</p>"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	)

	h := &Handler{}
	// The retry loop passes initialType "thinking" because thinking is enabled;
	// consumeChatStreamAttempt must override it with the message's real segment.
	res := h.consumeChatStreamAttempt(req, resp, streamRuntime, "thinking", true, nil, false)
	if !res.Terminal {
		t.Fatalf("expected resume attempt to finish normally, got result=%+v", res)
	}

	text := streamRuntime.accumulator.Text.String()
	if !strings.Contains(text, "<div>正文内容</div>") || !strings.Contains(text, "<p>更多正文内容</p>") {
		t.Fatalf("expected pathless body blocks to land in text, got %q", text)
	}
	thinking := streamRuntime.accumulator.Thinking.String()
	if strings.Contains(thinking, "<div>") || strings.Contains(thinking, "<p>") {
		t.Fatalf("expected body not to be swallowed into reasoning_content, got %q", thinking)
	}
}

func TestConsumeChatStreamAttemptResumeThinkingSegmentStaysThinking(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	streamRuntime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"cid-continue-think",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		true,  // thinkingEnabled
		false, // searchEnabled
		true,  // stripReferenceMarkers
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false, // bufferToolContent
		false, // emitEarlyToolDeltas
	)

	// Message was interrupted while still reasoning: only thinking consumed.
	streamRuntime.accumulator.RawThinking.WriteString("历史思考前缀，还没有任何正文。")

	resp := makeOpenAISSEHTTPResponse(
		`data: {"v":{"fragments":[{"type":"THINK","content":"完整的历史思考片段，模型还在思考，正文尚未开始，足够长。"}]}}`,
		`data: {"v":"再想想细节"}`,
		`data: {"p":"response/fragments","o":"APPEND","v":[{"type":"RESPONSE","content":"开始"}]}`,
		`data: {"v":"正文内容"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	)

	h := &Handler{}
	res := h.consumeChatStreamAttempt(req, resp, streamRuntime, "thinking", true, nil, false)
	if !res.Terminal {
		t.Fatalf("expected resume attempt to finish normally, got result=%+v", res)
	}

	thinking := streamRuntime.accumulator.RawThinking.String()
	if !strings.Contains(thinking, "再想想细节") {
		t.Fatalf("expected thinking continuation to stay in thinking, got %q", thinking)
	}
	text := streamRuntime.accumulator.RawText.String()
	if !strings.Contains(text, "正文内容") {
		t.Fatalf("expected post-RESPONSE body to land in text, got %q", text)
	}
}
