package promptcompat

import "strings"

// RequestBodyHasTools reports whether a raw request body carries a tool
// definition. It is the single source of truth used to decide which account
// pool a request should be routed to: a request that declares tools is treated
// as a "tools" request, one that does not is treated as a "no-tools" request.
//
// The check runs on the normalized request map after any protocol adapter has
// mapped its own shape onto the shared `tools` field, so OpenAI Chat / OpenAI
// Responses / Claude / Gemini all funnel through the same detection. The result
// also governs whether tool-call prompt injection happens for the request; it
// reflects the tools that the caller actually sent (the request body is not
// rewritten to strip `tools`).
//
// Two shapes count as carrying tools:
//
//  1. The body's top-level `tools` field is a non-empty []any (the canonical
//     shape all adapters normalize onto).
//  2. Some clients (e.g. the RikkaHub workspace app) do not send a `tools`
//     array at all; instead they embed the tool catalog inside a system message
//     (e.g. "Available tools: workspace_read_file ..."). To route those to the
//     tools pool we also scan `role=system` message text for the keywords
//     "tools", "tool", and "functions" (case-insensitive).
func RequestBodyHasTools(req map[string]any) bool {
	if len(req) == 0 {
		return false
	}
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		return true
	}
	return systemMessagesMentionTools(req)
}

// toolMentionKeywords are the case-insensitive substrings that, when present in
// a system message, indicate the caller is exposing tools to the model even
// though no top-level `tools` array was supplied.
var toolMentionKeywords = []string{"tools", "tool", "functions"}

// systemMessagesMentionTools scans the request body's `messages` array for any
// `role=system` message whose text content mentions a tool-related keyword.
func systemMessagesMentionTools(req map[string]any) bool {
	messages, ok := req["messages"].([]any)
	if !ok {
		return false
	}
	return MessagesDeclareToolsInSystemText(messages)
}

// MessagesDeclareToolsInSystemText reports whether any `role=system` message in
// the given messages slice mentions a tool-related keyword ("tools" / "tool" /
// "functions", case-insensitive). It is the message-level counterpart to
// RequestBodyHasTools' system scan and is the single source of truth used by the
// prompt builder to decide whether to inject the tool-call format spec for
// requests that expose tools via system text rather than a top-level `tools`
// array.
func MessagesDeclareToolsInSystemText(messages []any) bool {
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "system") {
			continue
		}
		if textMentionsTools(systemMessageText(msg["content"])) {
			return true
		}
	}
	return false
}

// systemMessageText flattens a system message `content` field into plain text.
// The value may be a plain string or a list of content parts (each either a
// string or a map carrying a "text" field), covering the OpenAI / Claude /
// Gemini shapes that funnel through here.
func systemMessageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
				b.WriteByte('\n')
			case map[string]any:
				if text, ok := p["text"].(string); ok {
					b.WriteString(text)
					b.WriteByte('\n')
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func textMentionsTools(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, kw := range toolMentionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
