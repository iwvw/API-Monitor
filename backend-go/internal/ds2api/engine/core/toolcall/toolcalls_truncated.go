package toolcall

import "strings"

// HasUnclosedToolCallMarkup reports whether text contains an opening
// tool-call wrapper/invoke tag (EPSE or canonical) that is never closed —
// the signature of an upstream stream truncated mid-tool-call. It skips
// CDATA / XML comments / markdown code spans (via the shared scanner) so
// prose that merely mentions a tool tag is not misread as a truncation.
func HasUnclosedToolCallMarkup(text string) bool {
	if text == "" {
		return false
	}
	for pos := 0; pos < len(text); {
		tag, ok := FindToolMarkupTagOutsideIgnored(text, pos)
		if !ok {
			break
		}
		if !tag.Closing && !tag.SelfClosing && (tag.Name == "tool_calls" || tag.Name == "invoke") {
			if _, ok := FindMatchingToolMarkupClose(text, tag); !ok {
				return true
			}
		}
		pos = tag.End + 1
	}
	return false
}

// IsPureTruncatedToolCallText reports whether text is dominated by a single
// truncated tool-call block — it starts with a tool-call opener (EPSE or
// canonical) and that opener never closes — with no meaningful prose before
// it. Such turns must not be delivered as visible text; the caller clears
// them so the empty-output retry regenerates a complete response instead of
// leaking the raw fragment.
func IsPureTruncatedToolCallText(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// A pure native DeepSeek tool-call frame that begins but never closes is the
	// same truncated-fragment signature in the native DSML dialect (which the
	// EPSE/XML scanner below does not recognize). Clear it so the empty-output
	// retry regenerates a complete response instead of leaking the raw frame.
	if IsPureNativeTruncatedToolFrame(t) {
		return true
	}
	if !strings.HasPrefix(t, "<|EPSE|tool_calls") &&
		!strings.HasPrefix(t, "<|EPSE|invoke") &&
		!strings.HasPrefix(t, "<tool_calls") &&
		!strings.HasPrefix(t, "<invoke") {
		return false
	}
	return HasUnclosedToolCallMarkup(t)
}

// StripTruncatedToolFragment removes an unclosed tool-call block (the
// signature of an upstream stream truncated mid-tool-call) and everything
// after it, keeping any leading prose. It returns "" when the whole text is
// just the truncated fragment. This lets the caller drop the leaked EPSE
// markup while retaining real prose that preceded it.
func StripTruncatedToolFragment(text string) string {
	if text == "" || !HasUnclosedToolCallMarkup(text) {
		return text
	}
	var b strings.Builder
	pos := 0
	for pos < len(text) {
		tag, ok := FindToolMarkupTagOutsideIgnored(text, pos)
		if !ok {
			b.WriteString(text[pos:])
			break
		}
		if tag.Start > pos {
			b.WriteString(text[pos:tag.Start])
		}
		if !tag.Closing && !tag.SelfClosing && (tag.Name == "tool_calls" || tag.Name == "invoke") {
			if _, ok := FindMatchingToolMarkupClose(text, tag); !ok {
				return b.String()
			}
		}
		b.WriteString(text[tag.Start : tag.End+1])
		pos = tag.End + 1
	}
	return b.String()
}