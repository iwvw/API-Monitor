package toolcall

import (
	"fmt"
	"strconv"
	"strings"
)

// cdataSlotToken describes the placeholder shape used to replace CDATA content
// while the repair layer works on the skeleton. The default form is
// DS_SLOT_{idx} (see plan/tool-call-fallback-cdata-slots.md §2.2). When a slot
// content already contains the chosen prefix, the anti-collision guard switches
// to a control-character-wrapped variant that still embeds the DS_SLOT_ literal
// so a repair model can recognize it verbatim.
type cdataSlotToken struct {
	prefix string
	suffix string
}

// format renders the placeholder for the idx-th slot, e.g. "DS_SLOT_0".
func (t cdataSlotToken) format(idx int) string {
	return t.prefix + "DS_SLOT_" + strconv.Itoa(idx) + t.suffix
}

// literalPrefix is the substring used for collision scanning and for detecting
// stray placeholders that leaked outside a CDATA region.
func (t cdataSlotToken) literalPrefix() string {
	return t.prefix + "DS_SLOT_"
}

// cdataSlotCandidates lists placeholder shapes in order of preference. The first
// candidate is the plain DS_SLOT_{idx} form; the fallbacks wrap it with the
// C1 record-separator control byte (0x1e), whose appearance in real content is
// vanishingly unlikely, so collisions are effectively impossible.
var cdataSlotCandidates = []cdataSlotToken{
	{prefix: "", suffix: ""},
	{prefix: "\x1e", suffix: "\x1e"},
	{prefix: "\x1e\x1e", suffix: "\x1e\x1e"},
}

// cdataSlotResult carries the skeleton produced by slotCDATAContent together
// with everything restoreCDATASlots needs to reverse the substitution.
type cdataSlotResult struct {
	skeleton   string
	slots      []string
	cdataSpans [][2]int
	token      cdataSlotToken
	// slotted reports whether any CDATA content was actually replaced. When
	// false the caller should keep using the original text (no-slot path).
	slotted bool
}

// slotCDATAContent replaces the inner bytes of every well-closed CDATA block in
// text with a short placeholder, leaving the CDATA shell intact so the repair
// layer sees only "skeleton + short placeholders". It returns the skeleton, the
// original contents in appearance order, the absolute CDATA outer-region spans
// in skeleton coordinates, and the chosen placeholder token.
//
// Hard constraints (plan §1, §2):
//   - CDATA content is copied byte-for-byte; nothing is unescaped, re-escaped,
//     trimmed, reordered, or dropped.
//   - Unclosed CDATA blocks are skipped (the opener is stepped over and scanning
//     continues) rather than aborting the whole scan the way
//     skipXMLIgnoredSection's blocked=true would.
//   - Before substituting, every candidate content is scanned for the token
//     prefix; on collision the guard switches to the next delimiter and rescans.
//
// The returned result has slotted=false (skeleton == text) when there is no
// closed CDATA to slot, or when every delimiter candidate collides with the
// content (in which case the caller must fall back to the no-slot path).
func slotCDATAContent(text string) cdataSlotResult {
	if text == "" {
		return cdataSlotResult{skeleton: text}
	}

	ranges := scanClosedCDATARanges(text)
	if len(ranges) == 0 {
		return cdataSlotResult{skeleton: text}
	}

	contents := make([]string, len(ranges))
	for i, r := range ranges {
		contents[i] = text[r.contentStart:r.contentEnd]
	}

	token, ok := chooseCDATASlotToken(contents)
	if !ok {
		// Every candidate delimiter collides with the content; refuse to slot
		// so the caller keeps the original text intact.
		return cdataSlotResult{skeleton: text}
	}

	var b strings.Builder
	b.Grow(len(text))
	slots := make([]string, len(ranges))
	spans := make([][2]int, len(ranges))
	prev := 0
	for i, r := range ranges {
		outerStart := b.Len() + (r.openStart - prev)
		// Copy verbatim up to and including the CDATA open marker.
		b.WriteString(text[prev:r.contentStart])
		slots[i] = text[r.contentStart:r.contentEnd]
		b.WriteString(token.format(i))
		// Copy the close marker verbatim.
		b.WriteString(text[r.contentEnd : r.contentEnd+r.closeLen])
		spans[i] = [2]int{outerStart, b.Len()}
		prev = r.contentEnd + r.closeLen
	}
	b.WriteString(text[prev:])

	return cdataSlotResult{
		skeleton:   b.String(),
		slots:      slots,
		cdataSpans: spans,
		token:      token,
		slotted:    true,
	}
}

type cdataRange struct {
	openStart    int
	contentStart int
	contentEnd   int
	closeLen     int
}

// scanClosedCDATARanges walks text and records every well-closed CDATA block.
// It reuses the same primitives as skipXMLIgnoredSection (indexToolCDATAOpen /
// findToolCDATAEnd) so the slot view of "what is a CDATA region" matches the
// parser's, but unlike skipXMLIgnoredSection it skips an unclosed opener and
// keeps scanning instead of blocking the remainder.
func scanClosedCDATARanges(text string) []cdataRange {
	var ranges []cdataRange
	pos := 0
	for pos < len(text) {
		start := indexToolCDATAOpen(text, pos)
		if start < 0 {
			break
		}
		openLen := toolCDATAOpenLenAt(text, start)
		contentStart := start + openLen
		endRel := findToolCDATAEnd(text, contentStart)
		if endRel < 0 {
			// Unclosed CDATA: skip this opener, continue scanning after it so a
			// single unclosed block cannot swallow later well-closed ones.
			pos = contentStart
			continue
		}
		closeLen := toolCDATACloseLenAt(text, endRel)
		ranges = append(ranges, cdataRange{
			openStart:    start,
			contentStart: contentStart,
			contentEnd:   endRel,
			closeLen:     closeLen,
		})
		pos = endRel + closeLen
	}
	return ranges
}

// chooseCDATASlotToken returns the first candidate delimiter whose literal
// prefix does not appear in any slot content, rescanning after each switch. It
// reports ok=false when every candidate collides.
func chooseCDATASlotToken(contents []string) (cdataSlotToken, bool) {
	for _, candidate := range cdataSlotCandidates {
		if !slotTokenPrefixCollides(contents, candidate.literalPrefix()) {
			return candidate, true
		}
	}
	return cdataSlotToken{}, false
}

func slotTokenPrefixCollides(contents []string, prefix string) bool {
	for _, c := range contents {
		if strings.Contains(c, prefix) {
			return true
		}
	}
	return false
}

// restoreCDATASlots reverses slotCDATAContent on the repaired text. It rescans
// repaired for CDATA regions (this is the slot mechanism's own scan, distinct
// from the repair layer, which must never re-judge CDATA boundaries) and, for
// each region whose content is exactly a placeholder, substitutes the original
// bytes back.
//
// It returns an error (so the caller can drop the whole slot path and fall back
// to the raw, no-slot recovery) when:
//   - a placeholder token appears outside any CDATA region (repair moved it),
//   - a CDATA region carries a stray placeholder prefix without matching a slot,
//   - a placeholder is matched more than once, or
//   - some slot placeholder is missing after repair.
//
// Because matched content is substituted with the exact original bytes, the
// restored CDATA is byte-identical to the input by construction; a mismatch can
// only surface as one of the error conditions above.
func restoreCDATASlots(repaired string, slots []string, token cdataSlotToken) (string, error) {
	if len(slots) == 0 {
		return repaired, nil
	}
	prefix := token.literalPrefix()
	used := make([]bool, len(slots))

	var b strings.Builder
	b.Grow(len(repaired))
	pos := 0
	for pos < len(repaired) {
		start := indexToolCDATAOpen(repaired, pos)
		if start < 0 {
			if strings.Contains(repaired[pos:], prefix) {
				return "", fmt.Errorf("cdata slot: placeholder token leaked outside CDATA region")
			}
			b.WriteString(repaired[pos:])
			break
		}
		outside := repaired[pos:start]
		if strings.Contains(outside, prefix) {
			return "", fmt.Errorf("cdata slot: placeholder token leaked outside CDATA region")
		}
		b.WriteString(outside)

		openLen := toolCDATAOpenLenAt(repaired, start)
		contentStart := start + openLen
		endRel := findToolCDATAEnd(repaired, contentStart)
		if endRel < 0 {
			// Unclosed CDATA in repaired output: not a region we slotted. Emit
			// the opener and keep scanning; any real placeholder would have been
			// inside a closed region.
			b.WriteString(repaired[start:contentStart])
			pos = contentStart
			continue
		}
		closeLen := toolCDATACloseLenAt(repaired, endRel)
		content := repaired[contentStart:endRel]
		restored, err := matchCDATASlotContent(content, slots, used, token)
		if err != nil {
			return "", err
		}
		b.WriteString(repaired[start:contentStart])
		b.WriteString(restored)
		b.WriteString(repaired[endRel : endRel+closeLen])
		pos = endRel + closeLen
	}

	for i, u := range used {
		if !u {
			return "", fmt.Errorf("cdata slot: placeholder %d missing after repair", i)
		}
	}
	return b.String(), nil
}

// matchCDATASlotContent maps a CDATA region's content back to its original
// bytes. A region whose content is exactly a placeholder restores that slot; a
// region that merely embeds the prefix (without being a clean placeholder) is an
// anomaly and errors out; anything else is genuine content passed through
// untouched.
func matchCDATASlotContent(content string, slots []string, used []bool, token cdataSlotToken) (string, error) {
	for i := range slots {
		if content == token.format(i) {
			if used[i] {
				return "", fmt.Errorf("cdata slot: placeholder %d matched more than once", i)
			}
			used[i] = true
			return slots[i], nil
		}
	}
	if strings.Contains(content, token.literalPrefix()) {
		return "", fmt.Errorf("cdata slot: unexpected placeholder token inside CDATA content")
	}
	return content, nil
}
