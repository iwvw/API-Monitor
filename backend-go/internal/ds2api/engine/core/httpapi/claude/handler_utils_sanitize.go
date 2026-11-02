package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxClaudeRawPromptChars = 1024
	omittedBinaryMarker     = "[omitted_binary_payload]"
)

func formatClaudeUnknownBlockForPrompt(block map[string]any) string {
	if block == nil {
		return ""
	}
	safe := sanitizeClaudeBlockForPrompt(block)
	raw := strings.TrimSpace(formatClaudeBlockRaw(safe))
	if raw == "" {
		return ""
	}
	if len(raw) > maxClaudeRawPromptChars {
		return raw[:maxClaudeRawPromptChars] + "...(truncated)"
	}
	return raw
}

func sanitizeClaudeBlockForPrompt(block map[string]any) map[string]any {
	out := cloneMap(block)
	for k, v := range out {
		if looksLikeBinaryFieldName(k) {
			out[k] = omittedBinaryMarker
			continue
		}
		switch inner := v.(type) {
		case map[string]any:
			out[k] = sanitizeClaudeBlockForPrompt(inner)
		case []any:
			out[k] = sanitizeClaudeArrayForPrompt(inner)
		case string:
			out[k] = sanitizeClaudeStringForPrompt(k, inner)
		}
	}
	return out
}

func sanitizeClaudeArrayForPrompt(items []any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case map[string]any:
			out = append(out, sanitizeClaudeBlockForPrompt(v))
		case []any:
			out = append(out, sanitizeClaudeArrayForPrompt(v))
		default:
			out = append(out, v)
		}
	}
	return out
}

func sanitizeClaudeStringForPrompt(key, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if looksLikeBinaryFieldName(key) || looksLikeBase64Payload(trimmed) {
		return omittedBinaryMarker
	}
	if len(trimmed) > maxClaudeRawPromptChars {
		return trimmed[:maxClaudeRawPromptChars] + "...(truncated)"
	}
	return trimmed
}

func looksLikeBinaryFieldName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "data" || n == "bytes" || n == "base64" || n == "inline_data" || n == "inlinedata"
}

func looksLikeBase64Payload(v string) bool {
	if len(v) < 512 {
		return false
	}
	compact := strings.TrimRight(v, "=")
	if compact == "" {
		return false
	}
	for _, ch := range compact {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '+' || ch == '/' || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

//nolint:unused // helper kept for compatibility with upcoming sanitize pipeline.
func marshalCompactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return string(b)
}
