package claude

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type claudeFrame struct {
	Event   string
	Payload map[string]any
}

func makeClaudeSSEHTTPResponse(lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func makeClaudeContentLine(t *testing.T, text string) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"p": "response/content",
		"v": text,
	})
	if err != nil {
		t.Fatalf("marshal content line failed: %v", err)
	}
	return "data: " + string(line)
}

func parseClaudeFrames(t *testing.T, body string) []claudeFrame {
	t.Helper()
	chunks := strings.Split(body, "\n\n")
	frames := make([]claudeFrame, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		eventName := ""
		dataPayload := ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataPayload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if eventName == "" || dataPayload == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(dataPayload), &payload); err != nil {
			t.Fatalf("decode frame failed: %v, payload=%s", err, dataPayload)
		}
		frames = append(frames, claudeFrame{Event: eventName, Payload: payload})
	}
	return frames
}

func findClaudeFrames(frames []claudeFrame, event string) []claudeFrame {
	out := make([]claudeFrame, 0)
	for _, f := range frames {
		if f.Event == event {
			out = append(out, f)
		}
	}
	return out
}

func collectClaudeTextDeltas(frames []claudeFrame) string {
	var combined strings.Builder
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	return combined.String()
}

func TestHandleClaudeStreamRealtimeTextIncrementsWithEventHeaders(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"Hel"}`,
		`data: {"p":"response/content","v":"lo"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("missing event header: message_start, body=%s", body)
	}
	if !strings.Contains(body, "event: content_block_delta") {
		t.Fatalf("missing event header: content_block_delta, body=%s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("missing event header: message_stop, body=%s", body)
	}

	frames := parseClaudeFrames(t, body)
	deltas := findClaudeFrames(frames, "content_block_delta")
	if len(deltas) < 1 {
		t.Fatalf("expected at least 1 text delta, got=%d body=%s", len(deltas), body)
	}
	combined := strings.Builder{}
	for _, f := range deltas {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	if combined.String() != "Hello" {
		t.Fatalf("unexpected combined text: %q body=%s", combined.String(), body)
	}
}

func TestHandleClaudeStreamRealtimeToolBufferedPlainTextDoesNotRepeatFinalText(t *testing.T) {
	h := &Handler{}
	want := "明白\n\nBash\nIN\npwd\nOUT\nok"
	resp := makeClaudeSSEHTTPResponse(
		makeClaudeContentLine(t, "明"),
		makeClaudeContentLine(t, "白\n\nBash\nIN\npwd\n"),
		makeClaudeContentLine(t, "OUT\nok"),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"Bash"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	if got := collectClaudeTextDeltas(frames); got != want {
		t.Fatalf("unexpected combined text: got %q want %q body=%s", got, want, rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeTrimsContinuationReplay(t *testing.T) {
	h := &Handler{}
	prefix := strings.Repeat("A", 40)
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"`+prefix+`"}`,
		`data: {"p":"response/content","v":"`+prefix+` tail"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	combined := strings.Builder{}
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	if got, want := combined.String(), prefix+" tail"; got != want {
		t.Fatalf("unexpected combined text: got %q want %q body=%s", got, want, rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeThinkingDelta(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"思"}`,
		`data: {"p":"response/thinking_content","v":"考"}`,
		`data: {"p":"response/content","v":"ok"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, true, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundThinkingDelta := false
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "thinking_delta" {
			foundThinkingDelta = true
			break
		}
	}
	if !foundThinkingDelta {
		t.Fatalf("expected thinking_delta event, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeSkipsThinkingFallbackWhenFinalTextExists(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"{\"tool_calls\":[{\"name\":\"search\""}`,
		`data: {"p":"response/thinking_content","v":",\"input\":{\"q\":\"go\"}}]}"}`,
		`data: {"p":"response/content","v":"normal answer"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, true, false, []string{"search"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" {
			t.Fatalf("unexpected tool_use block when final text exists, body=%s", rec.Body.String())
		}
	}

	foundEndTurn := false
	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "end_turn" {
			foundEndTurn = true
			break
		}
	}
	if !foundEndTurn {
		t.Fatalf("expected stop_reason=end_turn, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeUpstreamErrorEvent(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"error":{"message":"boom"}}`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	errFrames := findClaudeFrames(frames, "error")
	if len(errFrames) == 0 {
		t.Fatalf("expected error event frame, body=%s", rec.Body.String())
	}
	if errFrames[0].Payload["type"] != "error" {
		t.Fatalf("expected error payload type, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimePingEvent(t *testing.T) {
	h := &Handler{}
	oldPing := claudeStreamPingInterval
	oldIdle := claudeStreamIdleTimeout
	oldKeepalive := claudeStreamMaxKeepaliveCnt
	claudeStreamPingInterval = 10 * time.Millisecond
	claudeStreamIdleTimeout = 300 * time.Millisecond
	claudeStreamMaxKeepaliveCnt = 50
	defer func() {
		claudeStreamPingInterval = oldPing
		claudeStreamIdleTimeout = oldIdle
		claudeStreamMaxKeepaliveCnt = oldKeepalive
	}()

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: pr}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_, _ = io.WriteString(pw, "data: {\"p\":\"response/content\",\"v\":\"hi\"}\n")
		_, _ = io.WriteString(pw, "data: [DONE]\n")
		_ = pw.Close()
	}()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	if len(findClaudeFrames(frames, "ping")) == 0 {
		t.Fatalf("expected ping event in stream, body=%s", rec.Body.String())
	}
}

func TestCollectDeepSeekRegression(t *testing.T) {
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"想"}`,
		`data: {"p":"response/content","v":"答"}`,
		`data: [DONE]`,
	)
	result := sse.CollectStream(resp, true, true)
	if result.Thinking != "想" {
		t.Fatalf("unexpected thinking: %q", result.Thinking)
	}
	if result.Text != "答" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func TestHandleClaudeStreamRealtimeToolSafetyAcrossStructuredFormats(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantToolUse bool
	}{
		{name: "invoke_parameter_wrapper", payload: `<tool_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></tool_calls>`, wantToolUse: true},
		{name: "legacy_single_tool_root", payload: `<tool><tool_name>Bash</tool_name><param><command>pwd</command></param></tool>`, wantToolUse: false},
		{name: "legacy_tool_call_json", payload: `<tool>{"tool":"Bash","params":{"command":"pwd"}}</tool>`, wantToolUse: false},
		{name: "legacy_nested_tool_tag_style", payload: `<tool><tool name="Bash"><command>pwd</command></tool_call></tool>`, wantToolUse: false},
		{name: "legacy_function_tag_style", payload: `<function_call>Bash</function_call><function parameter name="command">pwd</function parameter>`, wantToolUse: false},
		{name: "legacy_antml_argument_style", payload: `<antml:function_calls><antml:function_call id="1" name="Bash"><antml:argument name="command">pwd</antml:argument></antml:function_call></antml:function_calls>`, wantToolUse: false},
		{name: "legacy_antml_function_attr_parameters", payload: `<antml:function_calls><antml:function_call id="1" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call></antml:function_calls>`, wantToolUse: false},
		{name: "legacy_function_calls_wrapper", payload: `<function_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></function_calls>`, wantToolUse: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			resp := makeClaudeSSEHTTPResponse(
				`data: {"p":"response/content","v":"`+strings.ReplaceAll(tc.payload, `"`, `\"`)+`"}`,
				`data: [DONE]`,
			)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

			h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"Bash"}, nil)

			frames := parseClaudeFrames(t, rec.Body.String())
			foundToolUse := false
			for _, f := range findClaudeFrames(frames, "content_block_start") {
				contentBlock, _ := f.Payload["content_block"].(map[string]any)
				if contentBlock["type"] == "tool_use" {
					foundToolUse = true
					break
				}
			}
			if foundToolUse != tc.wantToolUse {
				t.Fatalf("unexpected tool_use=%v for format %s, body=%s", foundToolUse, tc.name, rec.Body.String())
			}
		})
	}
}

func TestHandleClaudeStreamRealtimeDetectsToolUseWithLeadingProse(t *testing.T) {
	h := &Handler{}
	payload := "I'll call a tool now.\\n<tool_calls><invoke name=\\\"write_file\\\"><parameter name=\\\"path\\\">/tmp/a.txt</parameter><parameter name=\\\"content\\\">abc</parameter></invoke></tool_calls>"
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"`+payload+`"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"write_file"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundToolUse := false
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" && contentBlock["name"] == "write_file" {
			foundToolUse = true
			break
		}
	}
	if !foundToolUse {
		t.Fatalf("expected tool_use block with leading prose payload, body=%s", rec.Body.String())
	}

	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "tool_use" {
			return
		}
	}
	t.Fatalf("expected stop_reason=tool_use, body=%s", rec.Body.String())
}

func TestHandleClaudeStreamRealtimeIgnoresUnclosedFencedToolExample(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		"data: {\"p\":\"response/content\",\"v\":\"Here is an example:\\n```json\\n{\\\"tool_calls\\\":[{\\\"name\\\":\\\"Bash\\\",\\\"input\\\":{\\\"command\\\":\\\"pwd\\\"}}]}\"}",
		"data: {\"p\":\"response/content\",\"v\":\"\\n```\\nDo not execute it.\"}",
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "show example only"}}, false, false, []string{"Bash"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundToolUse := false
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" {
			foundToolUse = true
			break
		}
	}
	if foundToolUse {
		t.Fatalf("expected no tool_use for fenced example, body=%s", rec.Body.String())
	}

	foundToolStop := false
	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "tool_use" {
			foundToolStop = true
			break
		}
	}
	if foundToolStop {
		t.Fatalf("expected stop_reason to remain content-only, body=%s", rec.Body.String())
	}
}

// Backward-compatible alias for historical test name used in CI logs.
func TestHandleClaudeStreamRealtimePromotesUnclosedFencedToolExample(t *testing.T) {
	TestHandleClaudeStreamRealtimeIgnoresUnclosedFencedToolExample(t)
}

func TestHandleClaudeStreamRealtimeNormalizesToolInputBySchema(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"<tool_calls><invoke name=\"Write\">{\"input\":{\"content\":{\"message\":\"hi\"},\"taskId\":1}}</invoke></tool_calls>"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	toolsRaw := []any{
		map[string]any{
			"name": "Write",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"taskId":  map[string]any{"type": "string"},
				},
			},
		},
	}

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "write"}}, false, false, []string{"Write"}, toolsRaw)

	frames := parseClaudeFrames(t, rec.Body.String())
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] != "input_json_delta" {
			continue
		}
		partial := asString(delta["partial_json"])
		var args map[string]any
		if err := json.Unmarshal([]byte(partial), &args); err != nil {
			t.Fatalf("decode partial_json failed: %v payload=%s", err, partial)
		}
		if args["content"] != `{"message":"hi"}` {
			t.Fatalf("expected content normalized to string, got %#v", args["content"])
		}
		if args["taskId"] != "1" {
			t.Fatalf("expected taskId normalized to string, got %#v", args["taskId"])
		}
		return
	}
	t.Fatalf("expected input_json_delta frame, body=%s", rec.Body.String())
}
