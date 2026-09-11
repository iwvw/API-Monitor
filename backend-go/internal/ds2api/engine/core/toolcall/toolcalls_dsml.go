package toolcall

import (
	"regexp"
	"strings"
)

func normalizeEPSEToolCallMarkup(text string) (string, bool) {
	if text == "" {
		return "", true
	}
	canonicalized := canonicalizeToolCallCandidateSpans(text)
	hasEPSELikeMarkup, hasCanonicalMarkup := ContainsToolMarkupSyntaxOutsideIgnored(canonicalized)
	if !hasEPSELikeMarkup && !hasCanonicalMarkup {
		return canonicalized, true
	}
	return rewriteEPSEToolMarkupOutsideIgnored(canonicalized), true
}

func rewriteEPSEToolMarkupOutsideIgnored(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(text, i)
		if blocked {
			b.WriteString(text[i:])
			break
		}
		if advanced {
			b.WriteString(text[i:next])
			i = next
			continue
		}
		if end, ok := markdownCodeSpanEnd(text, i); ok {
			b.WriteString(text[i:end])
			i = end
			continue
		}
		tag, ok := scanToolMarkupTagAt(text, i)
		if !ok {
			b.WriteByte(text[i])
			i++
			continue
		}
		b.WriteByte('<')
		if tag.Closing {
			b.WriteByte('/')
		}
		b.WriteString(tag.Name)
		if delimLen := xmlTagEndDelimiterLenEndingAt(text, tag.End); delimLen > 0 {
			b.WriteString(text[tag.NameEnd : tag.End+1-delimLen])
			b.WriteByte('>')
		} else {
			b.WriteString(text[tag.NameEnd : tag.End+1])
			b.WriteByte('>')
		}
		i = tag.End + 1
	}
	return b.String()
}

// dsmlWrapperTagRe matches the tag-open prefix of a leaked DeepSeek web-agent
// DSML tool frame. The model emits these with the fullwidth pipes doubled
// around the literal DSML segment (<｜｜DSML｜｜calls>), and the engine's EPSE
// whitelist does not cover them. Only the prefix through the local name is
// matched so any attributes after the name survive the rewrite verbatim.
var dsmlWrapperTagRe = regexp.MustCompile(`(?i)<(/?)\s*[\|\x{FF5C}]+\s*DSML\s*[\|\x{FF5C}]+\s*(calls|invoke|parameter)`)

// RewriteDSMLWrapperFrames rewrites leaked DeepSeek web-agent DSML tool-frame
// tags into the canonical XML the standard parser consumes:
//
//	<｜｜DSML｜｜ calls>            →  <tool_calls>
//	</｜｜DSML｜｜ calls>           →  </tool_calls>
//	<｜｜DSML｜｜ invoke name="…">   →  <invoke name="…">
//	<｜｜DSML｜｜ parameter name="…"> →  <parameter name="…">
//
// Without this step a leaked frame is neither parsed into tool calls nor
// stripped by the EPSE whitelist, so it reaches the client as visible text
// (and the phase-3 repair pass is bypassed because no tool-call syntax is
// seen). Only tags whose prefix actually carries the DSML segment are
// rewritten, so ordinary <calls>/<invoke> prose is never touched.
func RewriteDSMLWrapperFrames(text string) string {
	if text == "" || !strings.Contains(text, "DSML") {
		return text
	}
	return dsmlWrapperTagRe.ReplaceAllStringFunc(text, func(m string) string {
		groups := dsmlWrapperTagRe.FindStringSubmatch(m)
		if len(groups) < 3 {
			return m
		}
		name := strings.ToLower(groups[2])
		switch name {
		case "calls":
			name = "tool_calls"
		case "invoke", "parameter":
		default:
			return m
		}
		return "<" + groups[1] + name
	})
}
