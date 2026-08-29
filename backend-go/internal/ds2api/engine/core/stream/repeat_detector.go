package stream

import (
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

// contentRepeatGuard detects consecutive identical content blocks in parsed
// SSE lines and reports when the same block repeats limit times in a row.
// Counters are kept per part type so repeated visible text and repeated
// thinking text each have their own sequence.
type contentRepeatGuard struct {
	limit int
	last  map[string]string
	count map[string]int
}

func newContentRepeatGuard(limit int) *contentRepeatGuard {
	if limit <= 0 {
		return nil
	}
	return &contentRepeatGuard{
		limit: limit,
		last:  make(map[string]string),
		count: make(map[string]int),
	}
}

func (g *contentRepeatGuard) observe(parts []sse.ContentPart) bool {
	if g == nil || g.limit <= 0 {
		return false
	}

	// Build one block per part type. Consecutive parts of the same type in a
	// single SSE line are joined so the line is one observation rather than
	// several; this avoids over-counting a line that merely contains multiple
	// fragments of the same repeated chunk.
	blocks := map[string]string{}
	for _, p := range parts {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		typ := p.Type
		if typ == "" {
			typ = "text"
		}
		if blocks[typ] == "" {
			blocks[typ] = text
		} else {
			blocks[typ] += "\n" + text
		}
	}

	for typ, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// Ignore blocks composed of a single rune. Normal token coalescing can
		// legitimately produce long runs of the same character (for example a
		// stream of single-character deltas), so those are not repetition loops.
		if distinctRuneCount(block) < 2 {
			continue
		}
		if g.last[typ] == block {
			g.count[typ]++
			if g.count[typ] >= g.limit {
				return true
			}
		} else {
			g.last[typ] = block
			g.count[typ] = 1
		}
	}
	return false
}

// distinctRuneCount reports how many different runes a block contains.
func distinctRuneCount(s string) int {
	seen := make(map[rune]struct{})
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
}
