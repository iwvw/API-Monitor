package assistantturn

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

type StreamEventType string

const (
	StreamEventTextDelta     StreamEventType = "text_delta"
	StreamEventThinkingDelta StreamEventType = "thinking_delta"
	StreamEventToolCall      StreamEventType = "tool_call"
	StreamEventDone          StreamEventType = "done"
	StreamEventError         StreamEventType = "error"
	StreamEventPing          StreamEventType = "ping"
)

type StreamEvent struct {
	Type     StreamEventType
	Text     string
	Thinking string
	ToolCall any
	Error    *OutputError
	Usage    *Usage
}

type Accumulator struct {
	inner shared.StreamAccumulator
}

type AccumulatorOptions struct {
	ThinkingEnabled       bool
	SearchEnabled         bool
	StripReferenceMarkers bool
}

func NewAccumulator(opts AccumulatorOptions) *Accumulator {
	return &Accumulator{
		inner: shared.StreamAccumulator{
			ThinkingEnabled:       opts.ThinkingEnabled,
			SearchEnabled:         opts.SearchEnabled,
			StripReferenceMarkers: opts.StripReferenceMarkers,
		},
	}
}

func (a *Accumulator) Apply(parsed sse.LineResult) shared.StreamAccumulatorResult {
	if a == nil {
		return shared.StreamAccumulatorResult{}
	}
	return a.inner.Apply(parsed)
}

func (a *Accumulator) Snapshot() (rawText, text, rawThinking, thinking, detectionThinking string) {
	if a == nil {
		return "", "", "", "", ""
	}
	return a.inner.RawText.String(),
		a.inner.Text.String(),
		a.inner.RawThinking.String(),
		a.inner.Thinking.String(),
		a.inner.ToolDetectionThinking.String()
}
