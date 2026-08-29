package assistantturn

import (
	"context"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
)

func TestBuildTurnFromCollectedTextCitation(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text:          "See [citation:1]",
		CitationLinks: map[int]string{1: "https://example.com"},
	}, BuildOptions{Model: "deepseek-v4-flash", Prompt: "prompt", SearchEnabled: true})
	if turn.Text != "See [1](https://example.com)" {
		t.Fatalf("text mismatch: %q", turn.Text)
	}
	if turn.StopReason != StopReasonStop {
		t.Fatalf("stop reason mismatch: %q", turn.StopReason)
	}
	if turn.Error != nil {
		t.Fatalf("unexpected error: %#v", turn.Error)
	}
}

func TestBuildTurnFromCollectedKeepsNonStreamReferenceLinks(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text: "结论[reference:0]，补充[reference:1]。",
		CitationLinks: map[int]string{
			1: "https://example.com/a",
			2: "https://example.com/b",
		},
	}, BuildOptions{Model: "deepseek-v4-flash-search", Prompt: "prompt", SearchEnabled: true})
	want := "结论[0](https://example.com/a)，补充[1](https://example.com/b)。"
	if turn.Text != want {
		t.Fatalf("text mismatch: got %q want %q", turn.Text, want)
	}
}

func TestBuildTurnFromCollectedToolCall(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text: `<tool_calls><invoke name="Write"><parameter name="content">{"x":1}</parameter></invoke></tool_calls>`,
	}, BuildOptions{
		ToolNames: []string{"Write"},
		ToolsRaw: []any{map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		}},
	})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(turn.ToolCalls))
	}
	if turn.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %q", turn.StopReason)
	}
	if _, ok := turn.ToolCalls[0].Input["content"].(string); !ok {
		t.Fatalf("expected content coerced to string, got %#v", turn.ToolCalls[0].Input["content"])
	}
}

func TestBuildTurnFromCollectedThinkingOnlyIsEmptyOutput(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{Thinking: "hidden"}, BuildOptions{})
	if turn.Error == nil || turn.Error.Code != "upstream_empty_output" {
		t.Fatalf("expected empty output error, got %#v", turn.Error)
	}
}

func TestBuildTurnFromCollectedPureEmptyOutputIsUpstreamUnavailable(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{}, BuildOptions{})
	if turn.Error == nil || turn.Error.Status != http.StatusServiceUnavailable || turn.Error.Code != "upstream_unavailable" {
		t.Fatalf("expected upstream unavailable error, got %#v", turn.Error)
	}
}

func TestBuildTurnFromCollectedToolChoiceRequired(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{Text: "hello"}, BuildOptions{
		ToolChoice: promptcompat.ToolChoicePolicy{Mode: promptcompat.ToolChoiceRequired},
	})
	if turn.Error == nil || turn.Error.Code != "tool_choice_violation" {
		t.Fatalf("expected tool choice violation, got %#v", turn.Error)
	}
}

func TestBuildTurnFromStreamSnapshotUsesVisibleTextAndRawToolDetection(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{
		RawText:     `<tool_calls><invoke name="Write"><parameter name="content">{"x":1}</parameter></invoke></tool_calls>`,
		VisibleText: "",
	}, BuildOptions{
		ToolNames: []string{"Write"},
		ToolsRaw: []any{map[string]any{
			"name": "Write",
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		}},
	})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected stream snapshot tool call, got %d", len(turn.ToolCalls))
	}
	if _, ok := turn.ToolCalls[0].Input["content"].(string); !ok {
		t.Fatalf("expected stream snapshot schema coercion, got %#v", turn.ToolCalls[0].Input["content"])
	}
}

func TestBuildTurnFromStreamSnapshotAlreadyEmittedToolAvoidsEmptyError(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{AlreadyEmittedCalls: true}, BuildOptions{})
	if turn.Error != nil {
		t.Fatalf("unexpected empty-output error after emitted tool call: %#v", turn.Error)
	}
	if turn.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %q", turn.StopReason)
	}
}

func TestFinalizeTurnStopOutcome(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{Text: "hello"}, BuildOptions{})
	outcome := FinalizeTurn(turn, FinalizeOptions{})
	if outcome.ShouldFail {
		t.Fatalf("unexpected failure: %#v", outcome.Error)
	}
	if outcome.FinishReason != "stop" || !outcome.HasVisibleText || !outcome.HasVisibleOutput {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
}

func TestFinalizeTurnToolCallsOutcome(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{AlreadyEmittedCalls: true}, BuildOptions{})
	outcome := FinalizeTurn(turn, FinalizeOptions{AlreadyEmittedToolCalls: true})
	if outcome.ShouldFail || outcome.FinishReason != "tool_calls" || !outcome.HasToolCalls {
		t.Fatalf("unexpected tool outcome: %#v", outcome)
	}
}

func TestFinalizeTurnContentFilterOutcome(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{ContentFilter: true}, BuildOptions{})
	outcome := FinalizeTurn(turn, FinalizeOptions{})
	if !outcome.ShouldFail || outcome.Error == nil || outcome.Error.Code != "content_filter" {
		t.Fatalf("expected content filter failure, got %#v", outcome)
	}
}

func TestBuildTurnFromCollectedUpstreamError(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		UpstreamError: "内容超长，请删减后再试",
	}, BuildOptions{})
	if turn.Error == nil || turn.Error.Status != http.StatusBadRequest ||
		turn.Error.Code != "upstream_error" ||
		turn.Error.Message != "内容超长，请删减后再试" {
		t.Fatalf("expected upstream_error with original message, got %#v", turn.Error)
	}
}

func TestBuildTurnFromStreamSnapshotUpstreamError(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{
		UpstreamError: "内容超长，请删减后再试",
	}, BuildOptions{})
	if turn.Error == nil || turn.Error.Status != http.StatusBadRequest ||
		turn.Error.Code != "upstream_error" {
		t.Fatalf("expected upstream_error from snapshot, got %#v", turn.Error)
	}
}

func TestShouldRetryEmptyOutputSkipsUpstreamError(t *testing.T) {
	turn := Turn{UpstreamError: "内容超长，请删减后再试"}
	if ShouldRetryEmptyOutput(turn, 0, 3) {
		t.Fatalf("should not retry when upstream error is present")
	}
}

func TestBuildTurnFromCollectedLLMRepairFillsToolCall(t *testing.T) {
	// A malformed tool call (wrong local name "call" instead of "invoke") that
	// the deterministic parser cannot rescue, but the finalize LLM repair pass
	// can. The residual bad code is handed to the invoker, which returns valid
	// EPSE format; the repaired call is surfaced.
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{Text: bad}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected one repaired tool call, got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %q", turn.ToolCalls[0].Name)
	}
	if got, _ := turn.ToolCalls[0].Input["command"].(string); got != "pwd" {
		t.Fatalf("expected restored command 'pwd', got %q", got)
	}
	if turn.StopReason != StopReasonToolCalls {
		t.Fatalf("expected tool_calls stop reason, got %q", turn.StopReason)
	}
}

func TestBuildTurnFromCollectedLLMRepairNoToolLeavesText(t *testing.T) {
	bad := `<|EPSE|call name="x">just prose that is not a tool</|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		return "NO TOOL", nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{Text: bad}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected NO TOOL to leave zero tool calls, got %d", len(turn.ToolCalls))
	}
}

func TestBuildTurnFromCollectedLLMRepairDisabledWhenNoInvoker(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	turn := BuildTurnFromCollected(sse.CollectResult{Text: bad}, BuildOptions{
		ToolNames: []string{"Bash"},
	})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no repair without an invoker, got %d calls", len(turn.ToolCalls))
	}
}

// TestBuildTurnFromCollectedPartialSuccessMerges covers M3: one wrapper parses
// successfully, and a trailing bad EPSE shell survives as residual intent. The
// residual is repaired and merged with the already-parsed call rather than
// dropped.
func TestBuildTurnFromCollectedPartialSuccessMerges(t *testing.T) {
	// good wrapper (parses to Write) + trailing bad shell (wrong local name).
	good := `<|EPSE|tool_calls><|EPSE|invoke name="Write"><|EPSE|parameter name="content"><![CDATA[hi]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{Text: good + bad}, BuildOptions{
		ToolNames:         []string{"Write", "Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if len(turn.ToolCalls) != 2 {
		t.Fatalf("expected merged 2 tool calls (parsed + repaired), got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "Write" {
		t.Fatalf("expected first (parsed) call Write, got %q", turn.ToolCalls[0].Name)
	}
	if turn.ToolCalls[1].Name != "Bash" {
		t.Fatalf("expected merged repaired call Bash, got %q", turn.ToolCalls[1].Name)
	}
	if got, _ := turn.ToolCalls[1].Input["command"].(string); got != "pwd" {
		t.Fatalf("expected restored command 'pwd', got %q", got)
	}
	if turn.StopReason != StopReasonToolCalls {
		t.Fatalf("expected tool_calls stop reason, got %q", turn.StopReason)
	}
}

// TestMergeRepairedToolCallsDeduplicates verifies the merge drops repaired
// calls that duplicate an existing parsed call.
func TestMergeRepairedToolCallsDeduplicates(t *testing.T) {
	existing := []toolcall.ParsedToolCall{{Name: "Bash", Input: map[string]any{"command": "pwd"}}}
	repaired := []toolcall.ParsedToolCall{
		{Name: "Bash", Input: map[string]any{"command": "pwd"}}, // dup
		{Name: "Write", Input: map[string]any{"content": "hi"}}, // new
	}
	merged := mergeRepairedToolCalls(existing, repaired)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged calls (dedup), got %d", len(merged))
	}
	if merged[0].Name != "Bash" || merged[1].Name != "Write" {
		t.Fatalf("unexpected merge order: %+v", merged)
	}
}

// TestBuildTurnFromStreamSnapshotRepairsResidual covers M4: a streaming finalize
// with bad tool-call text (no structured tool_calls emitted) triggers the LLM
// repair and the finalize turn surfaces the repaired tool_calls instead of the
// bad visible text.
func TestBuildTurnFromStreamSnapshotRepairsResidual(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{
		RawText:     bad,
		VisibleText: "",
	}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected one repaired stream tool call, got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %q", turn.ToolCalls[0].Name)
	}
	if turn.StopReason != StopReasonToolCalls {
		t.Fatalf("expected tool_calls stop reason, got %q", turn.StopReason)
	}
}

// TestBuildTurnFromStreamSnapshotSkipsRepairAfterEmittedCalls ensures M4 does
// not attempt repair once structured tool_calls were already streamed (they
// cannot be retracted).
func TestBuildTurnFromStreamSnapshotSkipsRepairAfterEmittedCalls(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	called := false
	invoke := func(_ context.Context, _ string) (string, error) {
		called = true
		return "NO TOOL", nil
	}
	BuildTurnFromStreamSnapshot(StreamSnapshot{
		RawText:             bad,
		AlreadyEmittedCalls: true,
	}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if called {
		t.Fatalf("repair invoker must not run when tool calls were already emitted")
	}
}

// TestBuildTurnFromCollectedDefensiveRepairOnSyntaxWithoutIntent covers
// Direction 4: the residual intent probe can miss (SawToolCallIntent=false) even
// though the deterministic parser saw tool-call syntax and produced zero calls.
// Rather than leaking the bad tool-call code as visible text, the raw text is
// handed to the repair layer as a defensive fallback.
func TestBuildTurnFromCollectedDefensiveRepairOnSyntaxWithoutIntent(t *testing.T) {
	// A canonical (no EPSE prefix) wrapper whose invoke has no name: this
	// parses to zero calls and, being canonical, produces no EPSE intent
	// signal — exactly the silent-leak gap Direction 4 guards.
	bad := `<tool_calls><invoke><parameter name="command">pwd</parameter></invoke></tool_calls>`
	parsed := toolcall.ParseStandaloneToolCallsDetailed(bad, []string{"Bash"})
	if parsed.SawToolCallIntent {
		t.Fatalf("precondition: expected SawToolCallIntent=false for canonical no-name invoke")
	}
	if !parsed.SawToolCallSyntax {
		t.Fatalf("precondition: expected SawToolCallSyntax=true")
	}
	if len(parsed.Calls) != 0 {
		t.Fatalf("precondition: expected 0 parsed calls, got %d", len(parsed.Calls))
	}

	var seen string
	invoke := func(_ context.Context, prompt string) (string, error) {
		seen = prompt
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{Text: bad}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected defensive repair to yield one tool call, got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %q", turn.ToolCalls[0].Name)
	}
	if seen == "" {
		t.Fatalf("expected repair invoker to receive the raw bad code")
	}
}

// TestBuildTurnFromCollectedNoDefensiveRepairWithoutSyntax ensures the Direction
// 4 fallback does NOT fire for plain prose (no tool-call syntax at all), so
// ordinary text is never sent to the repair layer.
func TestBuildTurnFromCollectedNoDefensiveRepairWithoutSyntax(t *testing.T) {
	called := false
	invoke := func(_ context.Context, _ string) (string, error) {
		called = true
		return "NO TOOL", nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{Text: "just a normal answer, no tools here"}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: context.Background(),
	})
	if called {
		t.Fatalf("defensive repair must not fire without tool-call syntax")
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
}
