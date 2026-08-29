package shared

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

func TestStreamAccumulatorAppliesThinkingAndTextDedupe(t *testing.T) {
	acc := StreamAccumulator{ThinkingEnabled: true, StripReferenceMarkers: true}
	thinkingPrefix := "this is a long thinking snapshot prefix used by DeepSeek continue replay"
	textPrefix := "this is a long visible answer snapshot prefix used by DeepSeek continue replay"
	first := acc.Apply(sse.LineResult{
		Parsed: true,
		Parts: []sse.ContentPart{
			{Type: "thinking", Text: thinkingPrefix},
			{Type: "text", Text: textPrefix},
		},
	})
	second := acc.Apply(sse.LineResult{
		Parsed: true,
		Parts: []sse.ContentPart{
			{Type: "thinking", Text: thinkingPrefix + " next"},
			{Type: "text", Text: textPrefix + " world"},
		},
	})

	if !first.ContentSeen || !second.ContentSeen {
		t.Fatalf("expected both chunks to mark content seen")
	}
	if got := acc.RawThinking.String(); got != thinkingPrefix+" next" {
		t.Fatalf("raw thinking = %q", got)
	}
	if got := acc.Thinking.String(); got != thinkingPrefix+" next" {
		t.Fatalf("thinking = %q", got)
	}
	if got := acc.RawText.String(); got != textPrefix+" world" {
		t.Fatalf("raw text = %q", got)
	}
	if got := acc.Text.String(); got != textPrefix+" world" {
		t.Fatalf("text = %q", got)
	}
	if got := second.Parts[0].VisibleText; got != " next" {
		t.Fatalf("thinking delta = %q", got)
	}
	if got := second.Parts[1].VisibleText; got != " world" {
		t.Fatalf("text delta = %q", got)
	}
}

func TestStreamAccumulatorKeepsHiddenThinkingForToolDetection(t *testing.T) {
	acc := StreamAccumulator{ThinkingEnabled: false, StripReferenceMarkers: true}
	result := acc.Apply(sse.LineResult{
		Parsed: true,
		Parts: []sse.ContentPart{
			{Type: "thinking", Text: "<tool_calls></tool_calls>"},
		},
		ToolDetectionThinkingParts: []sse.ContentPart{
			{Type: "thinking", Text: "detect"},
			{Type: "thinking", Text: " tools"},
		},
	})

	if !result.ContentSeen {
		t.Fatalf("expected hidden thinking to count as upstream content")
	}
	if got := acc.RawThinking.String(); got != "<tool_calls></tool_calls>" {
		t.Fatalf("raw thinking = %q", got)
	}
	if got := acc.Thinking.String(); got != "" {
		t.Fatalf("visible thinking = %q", got)
	}
	if got := acc.ToolDetectionThinking.String(); got != "detect tools" {
		t.Fatalf("tool detection thinking = %q", got)
	}
}

func TestStreamAccumulatorCountsThinkingOnlyAsActivity(t *testing.T) {
	// With reasoning hidden (thinking not requested by the client) the parser
	// drops thinking from Parts and only reports it via
	// ToolDetectionThinkingParts. That reasoning phase is still upstream
	// activity: it must count as ContentSeen so the stream watchdog's
	// no-content timeout cannot kill a long thinking-only generation.
	acc := StreamAccumulator{ThinkingEnabled: false, StripReferenceMarkers: true}
	result := acc.Apply(sse.LineResult{
		Parsed: true,
		ToolDetectionThinkingParts: []sse.ContentPart{
			{Type: "thinking", Text: "planning the long answer"},
		},
	})

	if !result.ContentSeen {
		t.Fatalf("expected thinking-only stream to count as upstream activity")
	}
	if got := acc.ToolDetectionThinking.String(); got != "planning the long answer" {
		t.Fatalf("tool detection thinking = %q", got)
	}
}

func TestStreamAccumulatorSuppressesCitationTextWhenSearchEnabled(t *testing.T) {
	acc := StreamAccumulator{SearchEnabled: true, StripReferenceMarkers: true}
	result := acc.Apply(sse.LineResult{
		Parsed: true,
		Parts:  []sse.ContentPart{{Type: "text", Text: "[citation:1]"}},
	})

	if !result.ContentSeen {
		t.Fatalf("expected citation chunk to mark upstream content")
	}
	if len(result.Parts) != 1 || !result.Parts[0].CitationOnly {
		t.Fatalf("expected citation-only delta, got %#v", result.Parts)
	}
	if got := acc.RawText.String(); got != "[citation:1]" {
		t.Fatalf("raw text = %q", got)
	}
	if got := acc.Text.String(); got != "" {
		t.Fatalf("visible text = %q", got)
	}
}

func TestStreamAccumulatorStripsInlineCitationAndReferenceMarkers(t *testing.T) {
	acc := StreamAccumulator{SearchEnabled: true, StripReferenceMarkers: true}
	result := acc.Apply(sse.LineResult{
		Parsed: true,
		Parts:  []sse.ContentPart{{Type: "text", Text: "广州天气[citation:1] 多云[reference:0]"}},
	})

	if !result.ContentSeen {
		t.Fatalf("expected marker chunk to mark upstream content")
	}
	if got := acc.Text.String(); got != "广州天气 多云" {
		t.Fatalf("visible text = %q", got)
	}
	if len(result.Parts) != 1 || result.Parts[0].VisibleText != "广州天气 多云" {
		t.Fatalf("unexpected parts: %#v", result.Parts)
	}
}
