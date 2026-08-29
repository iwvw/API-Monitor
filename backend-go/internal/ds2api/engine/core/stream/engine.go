package stream

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

type StopReason string

const (
	StopReasonNone              StopReason = ""
	StopReasonContextCancelled  StopReason = "context_cancelled"
	StopReasonNoContentTimeout  StopReason = "no_content_timeout"
	StopReasonIdleTimeout       StopReason = "idle_timeout"
	StopReasonUpstreamCompleted StopReason = "upstream_completed"
	StopReasonHandlerRequested  StopReason = "handler_requested"
	StopReasonRepetitionLoop    StopReason = "repetition_loop"
)

type ConsumeConfig struct {
	Context             context.Context
	Body                io.Reader
	ThinkingEnabled     bool
	InitialType         string
	KeepAliveInterval   time.Duration
	IdleTimeout         time.Duration
	MaxKeepAliveNoInput int

	// RepeatLimit is how many consecutive identical content blocks must be
	// observed before the stream is cut off. Zero enables the default (3);
	// a negative value disables the guard.
	RepeatLimit int
}

type ParsedDecision struct {
	Stop        bool
	StopReason  StopReason
	ContentSeen bool
}

type ConsumeHooks struct {
	OnParsed      func(parsed sse.LineResult) ParsedDecision
	OnKeepAlive   func()
	OnFinalize    func(reason StopReason, scannerErr error)
	OnContextDone func()
}

// finalize reports the end of a consume pass. reason describes how the pass
// ended; scannerErr, when non-nil, is the underlying stream read error that
// terminated the upstream body. A clean upstream EOF arrives as
// StopReasonUpstreamCompleted with a nil scannerErr; engine-generated idle and
// no-content timeouts arrive as StopReasonIdleTimeout /
// StopReasonNoContentTimeout with a descriptive scannerErr so callers can tell
// an interrupted stream apart from a cleanly completed one instead of silently
// masquerading a truncation as a normal stop.

func ConsumeSSE(cfg ConsumeConfig, hooks ConsumeHooks) {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	initialType := cfg.InitialType
	if initialType == "" {
		if cfg.ThinkingEnabled {
			initialType = "thinking"
		} else {
			initialType = "text"
		}
	}
	repeatLimit := cfg.RepeatLimit
	if repeatLimit == 0 {
		repeatLimit = 3
	}
	repeatGuard := newContentRepeatGuard(repeatLimit)
	parsedLines, done := sse.StartParsedLinePump(cfg.Context, cfg.Body, cfg.ThinkingEnabled, initialType)

	var ticker *time.Ticker
	if cfg.KeepAliveInterval > 0 {
		ticker = time.NewTicker(cfg.KeepAliveInterval)
		defer ticker.Stop()
	}

	hasContent := false
	lastContent := time.Now()
	keepaliveCount := 0

	finalize := func(reason StopReason, scannerErr error) {
		if hooks.OnFinalize != nil {
			hooks.OnFinalize(reason, scannerErr)
		}
	}
	contextDone := func() bool {
		if cfg.Context.Err() == nil {
			return false
		}
		if hooks.OnContextDone != nil {
			hooks.OnContextDone()
		}
		return true
	}

	for {
		if contextDone() {
			return
		}
		select {
		case <-cfg.Context.Done():
			if contextDone() {
				return
			}
			return
		case <-tickCh(ticker):
			if contextDone() {
				return
			}
			if !hasContent {
				keepaliveCount++
				if cfg.MaxKeepAliveNoInput > 0 && keepaliveCount >= cfg.MaxKeepAliveNoInput {
					finalize(StopReasonNoContentTimeout, fmt.Errorf("no content received from upstream within %d keepalive intervals", cfg.MaxKeepAliveNoInput))
					return
				}
			}
			if hasContent && cfg.IdleTimeout > 0 && time.Since(lastContent) > cfg.IdleTimeout {
				finalize(StopReasonIdleTimeout, fmt.Errorf("no content received from upstream for %s", cfg.IdleTimeout))
				return
			}
			if hooks.OnKeepAlive != nil {
				hooks.OnKeepAlive()
			}
		case parsed, ok := <-parsedLines:
			if contextDone() {
				return
			}
			if !ok {
				finalize(StopReasonUpstreamCompleted, <-done)
				return
			}
			if repeatGuard.observe(parsed.Parts) {
				finalize(StopReasonRepetitionLoop, fmt.Errorf("identical content block repeated %d times", repeatLimit))
				return
			}
			if hooks.OnParsed == nil {
				continue
			}
			decision := hooks.OnParsed(parsed)
			if decision.ContentSeen {
				hasContent = true
				lastContent = time.Now()
				keepaliveCount = 0
			}
			if decision.Stop {
				reason := decision.StopReason
				if reason == StopReasonNone {
					reason = StopReasonHandlerRequested
				}
				finalize(reason, nil)
				return
			}
		}
	}
}

func tickCh(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}
