package stream

import (
	"context"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

func TestConsumeSSEPrefersContextCancellationOverReadyParsedLines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var finalized bool
	var contextDone bool
	var parsedCalled bool

	ConsumeSSE(ConsumeConfig{
		Context:           ctx,
		Body:              strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"hello\"}\n\ndata: [DONE]\n"),
		ThinkingEnabled:   false,
		InitialType:       "text",
		KeepAliveInterval: 0,
	}, ConsumeHooks{
		OnParsed: func(_ sse.LineResult) ParsedDecision {
			parsedCalled = true
			return ParsedDecision{}
		},
		OnFinalize: func(_ StopReason, _ error) {
			finalized = true
		},
		OnContextDone: func() {
			contextDone = true
		},
	})

	if !contextDone {
		t.Fatal("expected OnContextDone to run for an already-cancelled context")
	}
	if finalized {
		t.Fatal("expected OnFinalize not to run after context cancellation wins")
	}
	if parsedCalled {
		t.Fatal("expected parsed lines not to be processed after context cancellation wins")
	}
}
