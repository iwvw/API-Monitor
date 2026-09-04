package assistantturn

import (
	"context"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

const truncatedEPSEToolBlock = `<|EPSE|tool_calls> <|EPSE|invoke name="Bash"> <|EPSE|parameter name="command"></|EPSE|parameter> <|EPSE|parameter name="description"></|EPSE|parameter> </|EPSE|invoke> </|EPSE>`

// A turn whose only output is a truncated tool-call block must not deliver the
// raw EPSE fragment as visible text. It must instead become an empty-output
// turn so the caller's retry regenerates a complete response (self-heal).
func TestBuildTurnFromCollectedPureTruncatedToolBlockBecomesEmptyOutput(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text: truncatedEPSEToolBlock,
	}, BuildOptions{})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "" {
		t.Fatalf("expected visible text cleared, got %q", turn.Text)
	}
	if turn.Error == nil || turn.Error.Code != "upstream_empty_output" {
		t.Fatalf("expected empty-output error to trigger retry, got %#v", turn.Error)
	}
}

// Prose that precedes a truncated tool block must survive; only the raw
// fragment is stripped so no EPSE markup leaks to the client.
func TestBuildTurnFromCollectedStripsTruncatedToolFragmentKeepsProse(t *testing.T) {
	input := "最后一块拼图是 relay.go 里的 resolveEndpointModel，我读一下确认映射 key。\n" + truncatedEPSEToolBlock
	turn := BuildTurnFromCollected(sse.CollectResult{Text: input}, BuildOptions{})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "最后一块拼图是 relay.go 里的 resolveEndpointModel，我读一下确认映射 key。\n" {
		t.Fatalf("expected prose kept and fragment stripped, got %q", turn.Text)
	}
	if turn.Error != nil {
		t.Fatalf("prose-only turn must not error, got %#v", turn.Error)
	}
}

// A complete tool call still parses normally (no false positive).
func TestBuildTurnFromCollectedCompleteToolBlockUnchanged(t *testing.T) {
	complete := `<|EPSE|tool_calls>
<|EPSE|invoke name="Bash">
<|EPSE|parameter name="command"><![CDATA[git status]]></|EPSE|parameter>
</|EPSE|invoke>
</|EPSE|tool_calls>`
	turn := BuildTurnFromCollected(sse.CollectResult{Text: complete}, BuildOptions{})
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected complete tool call to parse, got %d calls", len(turn.ToolCalls))
	}
}

// The stream-snapshot builder (used by finalize) applies the same rule so a
// truncated fragment is never flushed as visible content.
func TestBuildTurnFromStreamSnapshotPureTruncatedToolBlockBecomesEmptyOutput(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{
		RawText:     truncatedEPSEToolBlock,
		VisibleText: truncatedEPSEToolBlock,
	}, BuildOptions{})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "" {
		t.Fatalf("expected visible text cleared, got %q", turn.Text)
	}
	if turn.Error == nil || turn.Error.Code != "upstream_empty_output" {
		t.Fatalf("expected empty-output error, got %#v", turn.Error)
	}
}

func TestFinalizeTurnPureTruncatedToolBlockShouldFail(t *testing.T) {
	turn := BuildTurnFromStreamSnapshot(StreamSnapshot{
		RawText:     truncatedEPSEToolBlock,
		VisibleText: truncatedEPSEToolBlock,
	}, BuildOptions{})
	outcome := FinalizeTurn(turn, FinalizeOptions{})
	if !outcome.ShouldFail {
		t.Fatalf("expected truncated tool block to fail (trigger retry), got %#v", outcome)
	}
	if outcome.HasVisibleText {
		t.Fatalf("expected no visible text after clearing, got %#v", outcome)
	}
}

func TestValidateTurnProseOnlyOK(t *testing.T) {
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text: "这是正常的回答。",
	}, BuildOptions{})
	if turn.Error != nil {
		t.Fatalf("normal prose must not error, got %#v", turn.Error)
	}
}

func TestBuildTurnFromCollectedRepairFiresOnTruncatedBlock(t *testing.T) {
	invoke := func(_ context.Context, _ string) (string, error) {
		return "", nil
	}
	turn := BuildTurnFromCollected(sse.CollectResult{
		Text: truncatedEPSEToolBlock,
	}, BuildOptions{
		ToolNames:         []string{"Bash"},
		ToolCallRepair:    invoke,
		ToolCallRepairCtx: t.Context(),
		ToolChoice:        promptcompat.DefaultToolChoicePolicy(),
	})
	// Repair returned empty: residual must still be cleared, not leaked.
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls after failed repair, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "" {
		t.Fatalf("expected residual fragment cleared after failed repair, got %q", turn.Text)
	}
}

// A pure native DeepSeek tool-call frame that begins but never closes must not
// deliver the raw DSML fragment as visible text; it becomes an empty-output
// turn so the caller's retry regenerates a complete response.
func TestBuildTurnFromCollectedPureNativeTruncatedFrameBecomesEmptyOutput(t *testing.T) {
	frag := `<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view","path":"/workspace/example.md"}`
	turn := BuildTurnFromCollected(sse.CollectResult{Text: frag}, BuildOptions{})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "" {
		t.Fatalf("expected visible text cleared, got %q", turn.Text)
	}
	if turn.Error == nil || turn.Error.Code != "upstream_empty_output" {
		t.Fatalf("expected empty-output error to trigger retry, got %#v", turn.Error)
	}
}

// Prose that precedes a native truncated frame must survive; only the raw
// frame markers are stripped so no DSML token leaks to the client.
func TestBuildTurnFromCollectedStripsNativeFragmentKeepsProse(t *testing.T) {
	input := `我先看一下目录结构。<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}`
	turn := BuildTurnFromCollected(sse.CollectResult{Text: input}, BuildOptions{})
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != "我先看一下目录结构。" {
		t.Fatalf("expected prose kept and native markers stripped, got %q", turn.Text)
	}
}