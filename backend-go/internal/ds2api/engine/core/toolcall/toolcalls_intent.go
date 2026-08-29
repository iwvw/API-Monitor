package toolcall

import (
	"strings"
)

// DetectToolCallIntent reports whether the given text contains a *paired* EPSE
// tool-call intent: an EPSE opening tag (`<|EPSE...`) followed later by an EPSE
// closing tag (`</|EPSE...`), where both live outside CDATA, XML comments, and
// markdown code fences / inline code spans.
//
// The caller is expected to pass the residual text that remains after deducting
// the source regions of successfully parsed tool calls (see
// parseToolCallsDetailedXMLOnly). DetectToolCallIntent itself performs no
// parsing, no deduction and no name-scoped depth pairing: "paired" here is an
// existence test only — is there at least one EPSE open tag with at least one
// EPSE close tag somewhere after it. The local names of the open and close tags
// may differ or be empty.
//
// It must run on the pre-normalization coordinate system (the `original` text),
// never on the normalized text: normalization rewrites valid-local-name EPSE
// shells into canonical `<tool_calls>`/`<invoke>` and strips the `|EPSE|`
// prefix, which would make EPSE detection always fail.
func DetectToolCallIntent(text string) bool {
	if text == "" {
		return false
	}
	// Strip markdown code fences so EPSE literals inside *fully closed* fenced
	// blocks do not participate in detection. stripFencedCodeBlocks preserves
	// the raw tail of an unclosed (or odd-count) fence when that tail carries
	// tool-structural closing tags, so a real tool call whose parameters embed
	// an odd number of ``` fences is not decapitated here: its closing EPSE
	// tags survive and intent detection stays true. This is idempotent when the
	// caller already stripped fences.
	stripped := stripFencedCodeBlocks(text)
	if stripped == "" {
		return false
	}

	openEnd, ok := findEPSETag(stripped, 0, false)
	if !ok {
		return false
	}
	_, ok = findEPSETag(stripped, openEnd, true)
	return ok
}

// findEPSETag scans text from start for the first EPSE-prefixed markup tag whose
// closing-ness matches wantClosing, skipping CDATA / comments (via
// skipXMLIgnoredSection) and inline code spans (via markdownCodeSpanEnd). It
// returns the byte offset just past the consumed `epse` literal and true on a
// match.
func findEPSETag(text string, start int, wantClosing bool) (int, bool) {
	for i := maxInt(start, 0); i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(text, i)
		if blocked {
			return 0, false
		}
		if advanced {
			i = next
			continue
		}
		if end, ok := markdownCodeSpanEnd(text, i); ok {
			i = end
			continue
		}
		if afterEPSE, closing, ok := detectEPSETagAt(text, i); ok {
			if closing == wantClosing {
				return afterEPSE, true
			}
			i = afterEPSE
			continue
		}
		i++
	}
	return 0, false
}

// detectEPSETagAt reports whether an EPSE-prefixed markup tag begins at start.
// It walks the same low-level primitives as scanToolMarkupTagAt but only cares
// about the EPSE prefix, not the local name — so `<|EPSE|call>`, `<|EPSE|invoke>`,
// the local-name-less shorthand `</|EPSE>` and `<EPSE ...>` variants all match.
// closing is true when a closing slash sits between `<` and the EPSE prefix.
// The returned offset points just past the consumed `epse` literal.
func detectEPSETagAt(text string, start int) (afterEPSE int, closing bool, ok bool) {
	i, ok := consumeToolMarkupLessThan(text, start)
	if !ok {
		return start, false, false
	}
	for {
		next, ok := consumeToolMarkupLessThan(text, i)
		if !ok {
			break
		}
		i = next
	}
	// A closing tag carries its slash between `<` and the EPSE prefix; it must
	// be consumed via the closing-slash primitive, not the opening path.
	if next, ok := consumeToolMarkupClosingSlash(text, i); ok {
		closing = true
		i = next
	}
	// Consume any pipe/separator runes that precede the EPSE literal (e.g. the
	// leading `|` in `<|EPSE`). Whitespace and `/` are excluded by
	// consumeToolMarkupSeparator, so this only eats the pipe-family separators.
	for {
		next, ok := consumeToolMarkupSeparator(text, i)
		if !ok {
			break
		}
		i = next
	}
	afterEPSE, matched := consumeToolKeyword(text, i, "epse")
	if !matched {
		return start, false, false
	}
	// Guard against words that merely start with "epse" (e.g. "epsentence"):
	// the character immediately after the epse literal must be a tag boundary
	// (separator, whitespace, `>`, `/`, or end of input), never alphanumeric.
	if b, size := normalizedASCIIAt(text, afterEPSE); size > 0 {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			return start, false, false
		}
	}
	return afterEPSE, closing, true
}

// deductSuccessfulToolCallWrappers removes, from the pre-normalization original
// text, the source regions of every successfully-parsed tool call so the
// remainder can be probed for residual intent. Success is determined on
// parseSource (the normalized / CDATA-recovered text that was actually parsed)
// and mapped back to the original by ordinal index, because normalization
// neither adds, removes nor reorders wrappers/invokes.
//
// Two granularities are used, mirroring parseXMLToolCalls:
//   - wrapper granularity when an explicit `<|EPSE|tool_calls>` wrapper exists:
//     a wrapper whose invokes all parsed is deducted shell-and-all (so no empty
//     `<|EPSE|tool_calls></|EPSE|tool_calls>` false positive remains); a wrapper
//     with any failed invoke is left intact.
//   - invoke granularity for bare invokes (§5.1 caveat): when parseSource has no
//     opening tool_calls tag, parseXMLToolCalls synthesizes a wrapper via
//     repairMissingXMLToolCallsOpeningWrapper. The original carries no wrapper
//     shell, so the deduction target degrades to each successful invoke's own
//     region in the original.
func deductSuccessfulToolCallWrappers(original, parseSource string) string {
	sourceWrappers := findToolMarkupElementBlocksByName(parseSource, "tool_calls")
	if len(sourceWrappers) > 0 {
		return deductSuccessfulByWrapper(original, sourceWrappers)
	}
	return deductSuccessfulBareInvokes(original, parseSource)
}

// deductSuccessfulByWrapper handles the explicit-wrapper case with ordinal
// mapping between parseSource wrappers and original wrappers.
func deductSuccessfulByWrapper(original string, sourceWrappers []xmlElementBlock) string {
	origWrappers := findToolMarkupElementBlocksByName(original, "tool_calls")
	n := len(sourceWrappers)
	if len(origWrappers) < n {
		n = len(origWrappers)
	}
	if n == 0 {
		return original
	}

	var b strings.Builder
	b.Grow(len(original))
	prev := 0
	for idx := 0; idx < n; idx++ {
		invokes := findXMLElementBlocks(sourceWrappers[idx].Body, "invoke")
		if len(invokes) == 0 {
			continue
		}
		allOK := true
		for _, inv := range invokes {
			if _, ok := parseSingleXMLToolCall(inv); !ok {
				allOK = false
				break
			}
		}
		if !allOK {
			continue
		}
		ow := origWrappers[idx]
		if ow.Start < prev || ow.Start > ow.End || ow.End > len(original) {
			continue
		}
		b.WriteString(original[prev:ow.Start])
		prev = ow.End
	}
	b.WriteString(original[prev:])
	return b.String()
}

// deductSuccessfulBareInvokes handles the repaired bare-invoke case: parseSource
// gets a synthetic <tool_calls> wrapper (same as parseXMLToolCalls), success is
// evaluated per invoke, and successful invokes are deducted from the original by
// ordinal correspondence at invoke granularity.
func deductSuccessfulBareInvokes(original, parseSource string) string {
	repaired := repairMissingXMLToolCallsOpeningWrapper(parseSource)
	wrappers := findToolMarkupElementBlocksByName(repaired, "tool_calls")
	if len(wrappers) == 0 {
		return original
	}
	var successFlags []bool
	for _, w := range wrappers {
		for _, inv := range findXMLElementBlocks(w.Body, "invoke") {
			_, ok := parseSingleXMLToolCall(inv)
			successFlags = append(successFlags, ok)
		}
	}
	origInvokes := findToolMarkupElementBlocksByName(original, "invoke")
	n := len(successFlags)
	if len(origInvokes) < n {
		n = len(origInvokes)
	}
	if n == 0 {
		return original
	}

	var b strings.Builder
	b.Grow(len(original))
	prev := 0
	for idx := 0; idx < n; idx++ {
		if !successFlags[idx] {
			continue
		}
		oi := origInvokes[idx]
		if oi.Start < prev || oi.Start > oi.End || oi.End > len(original) {
			continue
		}
		b.WriteString(original[prev:oi.Start])
		prev = oi.End
	}
	b.WriteString(original[prev:])
	return b.String()
}
