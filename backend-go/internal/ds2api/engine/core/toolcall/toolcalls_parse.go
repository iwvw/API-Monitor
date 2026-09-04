package toolcall

import (
	"strings"
)

type ParsedToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ToolCallParseResult struct {
	Calls             []ParsedToolCall
	SawToolCallSyntax bool
	SawToolCallIntent bool
	// ResidualIntentText carries the residual (pre-normalization, fences
	// stripped, successful-wrapper-deducted) text that triggered
	// SawToolCallIntent. It is the "bad tool-call code" the LLM repair layer
	// (plan/tool-call-fallback-design-phase3.md) operates on. It is only
	// populated when SawToolCallIntent is true.
	ResidualIntentText string
	RejectedByPolicy   bool
	RejectedToolNames  []string
}

func ParseToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseToolCallsDetailed(text, availableToolNames).Calls
}

func ParseToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	return parseToolCallsDetailedXMLOnly(text)
}

func ParseStandaloneToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseStandaloneToolCallsDetailed(text, availableToolNames).Calls
}

func ParseStandaloneToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	return parseToolCallsDetailedXMLOnly(text)
}

func ParseAssistantToolCallsDetailed(text, thinking string, availableToolNames []string) ToolCallParseResult {
	textParsed := ParseStandaloneToolCallsDetailed(text, availableToolNames)
	if len(textParsed.Calls) > 0 {
		return textParsed
	}
	if strings.TrimSpace(text) != "" {
		return textParsed
	}
	thinkingParsed := ParseStandaloneToolCallsDetailed(thinking, availableToolNames)
	if len(thinkingParsed.Calls) > 0 {
		return thinkingParsed
	}
	return textParsed
}

func parseToolCallsDetailedXMLOnly(text string) ToolCallParseResult {
	result := ToolCallParseResult{}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return result
	}
	// original is the pre-normalization coordinate system: fences stripped but
	// EPSE prefixes intact. Intent detection and successful-wrapper deduction
	// must run on this text, never on the normalized text.
	original := stripFencedCodeBlocks(trimmed)
	original = strings.TrimSpace(original)
	if original == "" {
		return result
	}
	// Recover a balanced native DeepSeek tool-call frame (tool▁calls▁begin …)
	// by translating it into the canonical XML markup the parser consumes. This
	// runs before EPSE normalization so a recovered native frame is parsed into
	// a legitimate tool call instead of being stripped or regenerated. Partial /
	// unclosed frames are left unchanged and keep flowing to the strip/retry
	// path.
	original = RewriteNativeToolCallFrames(original)

	normalized, ok := normalizeEPSEToolCallMarkup(original)
	if !ok {
		return result
	}
	result.SawToolCallSyntax = looksLikeToolCallSyntax(normalized) || hasRepairableXMLToolCallsWrapper(normalized)
	// parseSource is the text that was actually parsed for tool calls; it is
	// used to decide which wrappers parsed successfully (by ordinal index).
	parseSource := normalized
	parsed := parseXMLToolCalls(normalized)
	if len(parsed) == 0 && indexToolCDATAOpen(normalized, 0) >= 0 {
		recovered := SanitizeLooseCDATA(normalized)
		if recovered != normalized {
			if recoveredParsed := parseXMLToolCalls(recovered); len(recoveredParsed) > 0 {
				parsed = recoveredParsed
				parseSource = recovered
			}
		}
	}

	// Residual intent detection: deduct the source regions of fully-successful
	// wrappers from the original, then probe the remainder for a paired EPSE
	// shell. This must run before any len(parsed)==0 early return — the
	// all-failed (0 calls) case is exactly the fallback-critical scenario.
	residual := deductSuccessfulToolCallWrappers(original, parseSource)
	result.SawToolCallIntent = DetectToolCallIntent(residual)
	if result.SawToolCallIntent {
		result.ResidualIntentText = residual
	}

	if len(parsed) == 0 {
		return result
	}

	result.SawToolCallSyntax = true
	calls, rejectedNames := filterToolCallsDetailed(parsed)
	result.Calls = calls
	result.RejectedToolNames = rejectedNames
	result.RejectedByPolicy = len(rejectedNames) > 0 && len(calls) == 0
	return result
}

func filterToolCallsDetailed(parsed []ParsedToolCall) ([]ParsedToolCall, []string) {
	out := make([]ParsedToolCall, 0, len(parsed))
	for _, tc := range parsed {
		if tc.Name == "" {
			continue
		}
		if tc.Input == nil {
			tc.Input = map[string]any{}
		}
		out = append(out, tc)
	}
	return out, nil
}

func looksLikeToolCallSyntax(text string) bool {
	hasEPSE, hasCanonical := ContainsToolCallWrapperSyntaxOutsideIgnored(text)
	return hasEPSE || hasCanonical
}

func stripFencedCodeBlocks(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))

	lines := strings.SplitAfter(text, "\n")
	inFence := false
	fenceMarker := ""
	inCDATA := false
	cdataFenceMarker := ""
	// Track builder length when a fence opens so we can preserve content
	// collected before the unclosed fence.
	beforeFenceLen := 0
	// Track the byte offset in the original text where the current unclosed
	// fence began, so the raw tail can be recovered when the fence turns out to
	// be a false positive (see the inFence block below).
	fenceStartOffset := -1
	lineStart := 0
	for _, line := range lines {
		curLineStart := lineStart
		lineStart += len(line)
		if inCDATA || cdataStartsBeforeFence(line) {
			b.WriteString(line)
			inCDATA, cdataFenceMarker = updateCDATAStateForStrip(inCDATA, cdataFenceMarker, line)
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if !inFence {
			if marker, ok := parseFenceOpen(trimmed); ok {
				inFence = true
				fenceMarker = marker
				beforeFenceLen = b.Len()
				fenceStartOffset = curLineStart
				continue
			}
			b.WriteString(line)
			continue
		}

		if isFenceClose(trimmed, fenceMarker) {
			inFence = false
			fenceMarker = ""
		}
	}

	if inFence {
		// Unclosed fence: normally we drop the fenced region and keep only the
		// content collected before it. But tool-call parameters routinely carry
		// an odd number of markdown fences (```), which makes a real tool call
		// look like an unclosed fence and swallows the trailing tool close
		// tags. If the dropped tail still carries tool-structural closing tags,
		// treat the fence as a false positive and preserve the raw tail so the
		// close tags survive for intent detection and parsing.
		result := b.String()
		head := ""
		if beforeFenceLen > 0 && beforeFenceLen <= len(result) {
			head = result[:beforeFenceLen]
		}
		if fenceStartOffset >= 0 && fenceStartOffset <= len(text) {
			if tail := text[fenceStartOffset:]; textContainsToolMarkupCloseTag(tail) {
				return head + tail
			}
		}
		return head
	}
	return b.String()
}

// textContainsToolMarkupCloseTag reports whether text carries a closing tool
// markup tag (EPSE or canonical) for tool_calls / invoke / parameter. Unlike the
// fence-aware scanners it deliberately does NOT skip markdown fences: it is used
// to decide whether an apparently-unclosed fence actually straddles a real tool
// call whose closing tags must not be discarded.
func textContainsToolMarkupCloseTag(text string) bool {
	canonicalCloses := []string{"</tool_calls", "</tool-calls", "</toolcalls", "</invoke", "</parameter"}
	for i := 0; i < len(text); i++ {
		if _, closing, ok := detectEPSETagAt(text, i); ok && closing {
			return true
		}
		for _, name := range canonicalCloses {
			if hasASCIIPrefixFoldAt(text, i, name) && hasXMLTagBoundary(text, i+len(name)) {
				return true
			}
		}
	}
	return false
}

func markdownCodeSpanEnd(text string, start int) (int, bool) {
	if start < 0 || start >= len(text) || text[start] != '`' {
		return start, false
	}
	count := countLeadingFenceChars(text[start:], '`')
	if count == 0 {
		return start, false
	}
	search := start + count
	for search < len(text) {
		if text[search] != '`' {
			search++
			continue
		}
		run := countLeadingFenceChars(text[search:], '`')
		if run == count {
			return search + run, true
		}
		search += run
	}
	return start, false
}

func cdataStartsBeforeFence(line string) bool {
	cdataIdx := indexToolCDATAOpen(line, 0)
	if cdataIdx < 0 {
		return false
	}
	fenceIdx := firstFenceMarkerIndex(line)
	return fenceIdx < 0 || cdataIdx < fenceIdx
}

func firstFenceMarkerIndex(line string) int {
	idxBacktick := strings.Index(line, "```")
	idxTilde := strings.Index(line, "~~~")
	switch {
	case idxBacktick < 0:
		return idxTilde
	case idxTilde < 0:
		return idxBacktick
	case idxBacktick < idxTilde:
		return idxBacktick
	default:
		return idxTilde
	}
}

func updateCDATAStateForStrip(inCDATA bool, cdataFenceMarker, line string) (bool, string) {
	pos := 0
	state := inCDATA
	fenceMarker := cdataFenceMarker
	lineForFence := line
	if !state {
		start := indexToolCDATAOpen(line, pos)
		if start < 0 {
			return false, ""
		}
		pos = start + toolCDATAOpenLenAt(line, start)
		if pos > len(line) {
			pos = len(line)
		}
		state = true
		lineForFence = line[pos:]
	}
	if !state {
		return false, ""
	}

	trimmed := strings.TrimLeft(lineForFence, " \t")
	if fenceMarker == "" {
		if marker, ok := parseFenceOpen(trimmed); ok {
			fenceMarker = marker
		}
	} else if isFenceClose(trimmed, fenceMarker) {
		fenceMarker = ""
	}

	for pos < len(line) {
		endPos := -1
		closeLen := 0
		for search := pos; search < len(line); search++ {
			if foundLen := toolCDATACloseLenAt(line, search); foundLen > 0 {
				endPos = search
				closeLen = foundLen
				break
			}
		}
		if endPos < 0 {
			return true, fenceMarker
		}
		pos = endPos + closeLen
		if pos > len(line) {
			pos = len(line)
		}
		if fenceMarker != "" {
			continue
		}
		if cdataEndLooksStructural(line, pos) || strings.TrimSpace(line[pos:]) == "" {
			state = false
			for pos < len(line) {
				start := indexToolCDATAOpen(line, pos)
				if start < 0 {
					return false, ""
				}
				pos = start + toolCDATAOpenLenAt(line, start)
				if pos > len(line) {
					pos = len(line)
				}
				state = true
				trimmedTail := strings.TrimLeft(line[pos:], " \t")
				if marker, ok := parseFenceOpen(trimmedTail); ok {
					fenceMarker = marker
				} else {
					fenceMarker = ""
				}
				break
			}
			continue
		}
	}
	return state, fenceMarker
}

func parseFenceOpen(line string) (string, bool) {
	if len(line) < 3 {
		return "", false
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return "", false
	}
	count := countLeadingFenceChars(line, ch)
	if count < 3 {
		return "", false
	}
	return strings.Repeat(string(ch), count), true
}

func isFenceClose(line, marker string) bool {
	if marker == "" {
		return false
	}
	ch := marker[0]
	if line == "" || line[0] != ch {
		return false
	}
	count := countLeadingFenceChars(line, ch)
	if count < len(marker) {
		return false
	}
	rest := strings.TrimSpace(line[count:])
	return rest == ""
}

func countLeadingFenceChars(line string, ch byte) int {
	count := 0
	for count < len(line) && line[count] == ch {
		count++
	}
	return count
}
