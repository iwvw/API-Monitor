package stream

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

func TestContentRepeatGuardRequiresPositiveLimit(t *testing.T) {
	if g := newContentRepeatGuard(0); g != nil {
		t.Fatal("expected nil guard for zero limit")
	}
	if g := newContentRepeatGuard(-1); g != nil {
		t.Fatal("expected nil guard for negative limit")
	}
}

func TestContentRepeatGuardTriggersAfterLimitIdenticalBlocks(t *testing.T) {
	g := newContentRepeatGuard(3)
	parts := []sse.ContentPart{{Text: "hello", Type: "text"}}

	if g.observe(parts) {
		t.Fatal("guard should not trigger on first observation")
	}
	if g.observe(parts) {
		t.Fatal("guard should not trigger on second observation")
	}
	if !g.observe(parts) {
		t.Fatal("guard should trigger on third identical observation")
	}
}

func TestContentRepeatGuardResetsOnDifferentContent(t *testing.T) {
	g := newContentRepeatGuard(3)
	a := []sse.ContentPart{{Text: "hello", Type: "text"}}
	b := []sse.ContentPart{{Text: "world", Type: "text"}}

	g.observe(a)
	g.observe(a)
	g.observe(b)
	if g.observe(a) {
		t.Fatal("guard should reset after a different block")
	}
	if g.observe(a) {
		t.Fatal("guard should not trigger after only two repeats following a reset")
	}
	if !g.observe(a) {
		t.Fatal("guard should trigger after three repeats following a reset")
	}
}

func TestContentRepeatGuardIgnoresEmptyParts(t *testing.T) {
	g := newContentRepeatGuard(2)
	if g.observe(nil) {
		t.Fatal("guard should ignore a nil parts slice")
	}
	if g.observe([]sse.ContentPart{{Text: "   ", Type: "text"}}) {
		t.Fatal("guard should ignore whitespace-only parts")
	}
	if g.observe([]sse.ContentPart{{Text: "ok", Type: "text"}}) {
		t.Fatal("guard should not trigger on first non-empty observation")
	}
	if !g.observe([]sse.ContentPart{{Text: "ok", Type: "text"}}) {
		t.Fatal("guard should trigger after limit repeats")
	}
}
