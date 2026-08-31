package stream

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

// contentRepeatGuard detects output loops in accumulated content. A naive
// "is the previous block identical" check misses incremental delta streams,
// where adjacent SSE blocks are almost never byte-identical even when the model
// is looping. Instead this guard keeps a bounded tail window of accumulated
// text and reports when the tail ends in a repeating period (three identical
// trailing substrings). This covers both consecutive repeats (AAAA...) and
// periodic/alternating loops (ABAB...).
//
// Text and thinking are accumulated separately so one stream's loop does not
// corrupt the other's period detection. The heavy lifting lives in sse.RepeatTail
// so the non-streaming path (sse.CollectStream) reuses the same detector.
type contentRepeatGuard struct {
	tail sse.RepeatTail
}

func newContentRepeatGuard(limit int) *contentRepeatGuard {
	if limit <= 0 {
		return nil
	}
	return &contentRepeatGuard{tail: sse.NewRepeatTail(limit)}
}

func (g *contentRepeatGuard) observe(parts []sse.ContentPart) bool {
	if g == nil {
		return false
	}
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if g.tail.Observe(p.Text, p.Type == "thinking") {
			return true
		}
	}
	return false
}
