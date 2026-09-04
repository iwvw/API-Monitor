package toolcall

import (
	"regexp"
	"strings"
)

// DeepSeek 网页版 agent 模式使用一套「原生工具调用帧」DSML 标记，与引擎自身
// 注入的 <|EPSE|tool_calls> 外壳不同。当模型把它们泄漏到正文文本时（而非由
// sieve 捕获成合法工具调用），上游还常对其做转义：竖线写成全角 ｜（U+FF5C）
// 且可能翻倍成 ｜｜，右尖括号写成全角 ＞（U+FF1E），名字内的分隔符写成 ▁
// （U+2581）。这套帧不在 EPSE/XML 白名单里，因此既不会被解析成工具调用，
// 也会被 leaked_output_sanitize 漏过、原样透传给客户端。
//
// 本文件提供对这些帧的归一化关键字检测与剥离：把 ▁ 折叠成 _、全角符号折叠
// 成半角后按关键字（tool_calls_begin / tool_call_begin / tool_sep /
// tool_call_end / tool_calls_end）匹配。采用「单关键字命中即判」口径：真实
// 泄漏大量是残缺帧（begin + sep + 参数、没有 end 标记），配对规则结构性失效，
// 这也是外部 epse-guard 项目 v2 之后采用的策略。
const (
	nativeSepUnderscore  = "\u2581" // ▁ U+2581 lower one eighth block
	nativeFullwidthPipe  = "\uFF5C" // ｜ U+FF5C fullwidth vertical line
	nativeFullwidthGT    = "\uFF1E" // ＞ U+FF1E fullwidth greater-than sign
	nativeFullwidthLT    = "\uFF1C" // ＜ U+FF1C fullwidth less-than sign
)

// nativeNormalizeFold 把原生帧用到的特殊分隔字符折叠成 ASCII，便于关键字匹配。
func nativeNormalizeFold(r rune) rune {
	switch r {
	case '\u2581':
		return '_'
	case '\uFF5C':
		return '|'
	case '\uFF1E':
		return '>'
	case '\uFF1C':
		return '<'
	default:
		return r
	}
}

// normalizeNativeToolFrame 归一化一段可能含原生帧标记的文本：折叠全角符号与
// ▁ 后转小写。归一化前后的字节坐标不一致，因此只用于「检测」，不用于「定位」。
func normalizeNativeToolFrame(text string) string {
	return strings.ToLower(strings.Map(nativeNormalizeFold, text))
}

// nativeFrameKeywordRe 命中任一原生工具帧关键字（归一化后）即视为泄漏信号。
// tool_calls?_(?:begin|end) 同时覆盖 tool_call_begin / tool_calls_begin /
// tool_call_end / tool_calls_end。
var nativeFrameKeywordRe = regexp.MustCompile(`tool_calls?_(?:begin|end)|tool_sep`)

// HasNativeToolFrame reports whether text contains a native DeepSeek tool-call
// frame keyword (begin / sep / end), in raw or upstream-escaped form. A single
// stray keyword in model text is already a malfunction, so no begin/end pairing
// is required (partial frames leak without any end marker).
func HasNativeToolFrame(text string) bool {
	if text == "" {
		return false
	}
	return nativeFrameKeywordRe.MatchString(normalizeNativeToolFrame(text))
}

// nativeMarkerRe 匹配单个原生帧标记（begin / end / sep），兼容全角竖线（含
// 翻倍 ｜｜）、全角右尖括号 ＞ 与 ▁ 分隔符。
var nativeMarkerRe = regexp.MustCompile(
	`<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]calls?[_\x{2581}](?:begin|end)\s*[\|\x{FF5C}]+\s*[\x{FF1E}>]` +
		`|<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]sep\s*[\|\x{FF5C}]+\s*[\x{FF1E}>]`,
)

// nativeWrapperRe 匹配一段「完整」原生工具调用包装帧：从 tool_calls_begin 到
// tool_calls_end（含其间嵌套的 call_begin … call_end 与 sep），整体剥离。
var nativeWrapperRe = regexp.MustCompile(
	`(?is)<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]calls[_\x{2581}]begin\s*[\|\x{FF5C}]+\s*[\x{FF1E}>].*?<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]calls[_\x{2581}]end\s*[\|\x{FF5C}]+\s*[\x{FF1E}>]`,
)

// StripNativeToolCallFrames removes leaked native DeepSeek tool-call frames from
// text: complete wrapper blocks and partial begin…sep…args frames are stripped
// whole, and any leftover standalone sep marker plus its JSON argument is
// removed, so the raw token bytes and tool-call payloads never reach the client.
// Prose that surrounds a leaked frame is preserved.
func StripNativeToolCallFrames(text string) string {
	if text == "" || !HasNativeToolFrame(text) {
		return text
	}
	// 1) Strip complete wrapper frames (tool_calls_begin … tool_calls_end),
	//    including the nested call_begin … call_end invokes inside them.
	out := nativeWrapperRe.ReplaceAllString(text, "")
	// 2) Strip partial frames directly on the (normalized) text via a scanner so
	//    the tool name + sep + JSON argument block is removed as one unit.
	out = stripNativePartialFrames(out)
	// 3) Any remaining lone begin/sep/end markers (e.g. a stray sep whose args
	//    did not directly follow) are removed so the token bytes do not leak.
	return nativeMarkerRe.ReplaceAllString(out, "")
}

// stripNativePartialFrames removes every native frame that starts with a begin
// marker but has no closing end marker, along with its optional tool name,
// sep marker, and JSON argument block. Prose before/after the frame survives.
func stripNativePartialFrames(text string) string {
	positions := nativeBeginRe.FindAllStringIndex(text, -1)
	if len(positions) == 0 {
		return text
	}
	var b strings.Builder
	prev := 0
	for _, loc := range positions {
		start := loc[0]
		b.WriteString(text[prev:start])
		end := start
		nameStart := nativeBeginRe.FindStringSubmatchIndex(text[start:])
		if nameStart != nil && nameStart[2] >= 0 {
			end += nameStart[2]
		}
		sepIdx := nativeSepRe.FindStringIndex(text[end:])
		if sepIdx != nil {
			argStart := end + sepIdx[1]
			argEnd := nativeJSONArgEnd(text, argStart)
			if argEnd > argStart {
				end = argEnd
			} else {
				end += sepIdx[1]
			}
		}
		prev = end
	}
	b.WriteString(text[prev:])
	return b.String()
}

// nativeBeginRe matches a native frame's begin marker, capturing the optional
// tool name that follows it (group 1/indices 2-3).
var nativeBeginRe = regexp.MustCompile(
	`<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]calls?[_\x{2581}]begin\s*[\|\x{FF5C}]+\s*[\x{FF1E}>]\s*([\p{L}\p{N}_-]*)`,
)

// nativeSepRe matches a native frame's sep marker.
var nativeSepRe = regexp.MustCompile(
	`<\s*[\|\x{FF5C}]+\s*tool[_\x{2581}]sep\s*[\|\x{FF5C}]+\s*[\x{FF1E}>]`,
)

// nativeJSONArgEnd returns the byte offset just past a JSON argument block that
// begins at start (the first non-space byte is '{' or '['), or start if the text
// at start is not a JSON value. It tracks brace/bracket depth so a complete
// `{...}` / `[...]` is consumed as the frame's argument payload. If the value is
// opened but never closed before the end of the line, it consumes to the line end
// so a truncated JSON payload still cannot leak to output.
func nativeJSONArgEnd(text string, start int) int {
	i := start
	for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r') {
		i++
	}
	if i >= len(text) || (text[i] != '{' && text[i] != '[') {
		return start
	}
	depth := 0
	inString := false
	escaped := false
	for ; i < len(text); i++ {
		c := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '\n':
			// Value was opened but never closed before line end; consume the
			// rest of the line so a truncated payload does not reach output.
			if depth > 0 {
				return i
			}
		}
	}
	return start
}

// IsPureNativeTruncatedToolFrame reports whether text is dominated by a native
// tool-call frame that starts with a begin marker but never closes (the
// upstream stream was cut mid-frame, or the model emitted begin + sep + args
// with no end). Such turns must not be delivered as visible text: the caller
// clears them so the empty-output retry regenerates a complete response.
func IsPureNativeTruncatedToolFrame(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || !strings.HasPrefix(t, "<") {
		return false
	}
	nt := normalizeNativeToolFrame(t)
	if !strings.Contains(nt, "tool_call_begin") && !strings.Contains(nt, "tool_calls_begin") {
		return false
	}
	hasEnd := strings.Contains(nt, "tool_call_end") || strings.Contains(nt, "tool_calls_end")
	return !hasEnd
}

// nativeKind identifies the role of a single native frame marker.
type nativeKind int

const (
	nativeKindSep nativeKind = iota
	nativeKindCallBegin
	nativeKindCallEnd
	nativeKindCallsBegin
	nativeKindCallsEnd
)

// nativeStructuralRe matches one native frame marker, capturing the frame word
// (group "word": "call" or "calls") and morph (group "morph": "begin" or "end")
// for begin/end markers. The sep marker has no captures. Compatible with the
// raw, fullwidth-pipe (｜, possibly doubled), fullwidth-gt (＞) and ▁/_
// separator forms.
var nativeStructuralRe = regexp.MustCompile(
	`<[<\x{FF1C}]?[ \t]*[\|\x{FF5C}]+[ \t]*tool[_\x{2581}](?P<word>calls?)[_\x{2581}](?P<morph>begin|end)[ \t]*[\|\x{FF5C}]+[ \t]*[\x{FF1E}>]` +
		`|<[<\x{FF1C}]?[ \t]*[\|\x{FF5C}]+[ \t]*tool[_\x{2581}]sep[ \t]*[\|\x{FF5C}]+[ \t]*[\x{FF1E}>]`,
)

type nativeEvent struct {
	kind      nativeKind
	start     int
	end       int
	nameStart int
	nameEnd   int
}

func collectNativeEvents(text string) []nativeEvent {
	all := nativeStructuralRe.FindAllStringSubmatchIndex(text, -1)
	if len(all) == 0 {
		return nil
	}
	events := make([]nativeEvent, 0, len(all))
	for _, m := range all {
		if len(m) < 2 {
			continue
		}
		ev := nativeEvent{start: m[0], end: m[1]}
		if m[2] >= 0 && m[3] >= 0 && m[4] >= 0 && m[5] >= 0 {
			word := text[m[2]:m[3]]
			morph := text[m[4]:m[5]]
			plural := word == "calls"
			switch {
			case morph == "begin" && plural:
				ev.kind = nativeKindCallsBegin
			case morph == "end" && plural:
				ev.kind = nativeKindCallsEnd
			case morph == "begin":
				ev.kind = nativeKindCallBegin
			default:
				ev.kind = nativeKindCallEnd
			}
		} else {
			ev.kind = nativeKindSep
		}
		events = append(events, ev)
	}
	return events
}

// nativeFramesAreBalanced reports whether the native marker sequence forms a
// well-nested, fully-closed set of frames (every calls_begin closed by
// calls_end, every call_begin closed by call_end, seps only inside invokes).
// Only such frames can be translated into well-formed XML for the parser.
func nativeFramesAreBalanced(events []nativeEvent) bool {
	var stack []nativeKind
	for _, ev := range events {
		switch ev.kind {
		case nativeKindCallsBegin:
			stack = append(stack, nativeKindCallsBegin)
		case nativeKindCallBegin:
			stack = append(stack, nativeKindCallBegin)
		case nativeKindSep:
			if len(stack) == 0 || stack[len(stack)-1] != nativeKindCallBegin {
				return false
			}
		case nativeKindCallEnd:
			if len(stack) == 0 || stack[len(stack)-1] != nativeKindCallBegin {
				return false
			}
			stack = stack[:len(stack)-1]
		case nativeKindCallsEnd:
			if len(stack) == 0 || stack[len(stack)-1] != nativeKindCallsBegin {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

// RewriteNativeToolCallFrames translates a balanced, fully-closed native
// DeepSeek tool-call frame into the canonical XML markup the existing parser
// consumes:
//
//	tool_calls_begin     -> <tool_calls>
//	tool_call_begin NAME -> <invoke name="NAME">{"input":<json>}
//	tool_sep {json}      ->   (the JSON is emitted as the invoke body)
//	tool_call_end        -> </invoke>
//	tool_calls_end       -> </tool_calls>
//
// The invoke body is the flat JSON argument object wrapped as {"input": ...}
// so the parser's JSON-body branch extracts it as the invoke input. This lets
// a recovered native frame be parsed into a legitimate tool call instead of
// being stripped or regenerated. It returns text unchanged when the frame is
// not balanced / not closed (partial frames keep flowing to the strip/retry
// path). Prose outside the frame is preserved.
func RewriteNativeToolCallFrames(text string) string {
	if text == "" || !HasNativeToolFrame(text) {
		return text
	}
	events := collectNativeEvents(text)
	if !nativeFramesAreBalanced(events) {
		return text
	}
	var b strings.Builder
	pos := 0
	for i, ev := range events {
		b.WriteString(text[pos:ev.start])
		var next *nativeEvent
		if i+1 < len(events) {
			next = &events[i+1]
		}
		switch ev.kind {
		case nativeKindCallsBegin:
			b.WriteString("<tool_calls>")
		case nativeKindCallsEnd:
			b.WriteString("</tool_calls>")
		case nativeKindCallBegin:
			name := ""
			if next != nil {
				name = strings.TrimSpace(text[ev.end:next.start])
			} else {
				name = strings.TrimSpace(text[ev.end:])
			}
			if n, _, ok := takeNativeToolName(name); ok {
				name = n
			}
			b.WriteString(`<invoke name="` + htmlEscapeNativeAttr(name) + `">`)
			if next != nil {
				pos = next.start
				continue
			}
			pos = len(text)
		case nativeKindSep:
			json := ""
			if next != nil {
				json = strings.TrimSpace(text[ev.end:next.start])
			} else {
				json = strings.TrimSpace(text[ev.end:])
			}
			b.WriteString(`{"input":` + json + `}`)
			if next != nil {
				pos = next.start
				continue
			}
			pos = len(text)
		default:
			b.WriteString("</invoke>")
		}
		pos = ev.end
	}
	b.WriteString(text[pos:])
	return b.String()
}

func htmlEscapeNativeAttr(s string) string {
	r := strings.NewReplacer(`&`, "&amp;", `"`, "&quot;", `<`, "&lt;", `>`, "&gt;")
	return r.Replace(s)
}

// takeNativeToolName extracts the leading tool-name token from s, stopping at
// the first character that is not a letter / digit / underscore / hyphen (the
// characters valid in a native tool name). It returns the token and whether one
// was found.
func takeNativeToolName(s string) (string, int, bool) {
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return "", 0, false
	}
	return s[:i], i, true
}
