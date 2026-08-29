package claude

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"fmt"
	"strings"
)

func hasSystemMessage(messages []any) bool {
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if ok && msg["role"] == "system" {
			return true
		}
	}
	return false
}

func extractClaudeToolNames(tools []any) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := extractClaudeToolMeta(m)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func extractClaudeToolMeta(m map[string]any) (string, string, any) {
	name, desc, schemaObj := toolcall.ExtractToolMeta(m)
	if strings.TrimSpace(desc) == "" {
		desc = "No description available"
	}
	return strings.TrimSpace(name), strings.TrimSpace(desc), schemaObj
}

func toMessageMaps(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func extractMessageContent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, it := range x {
			parts = append(parts, fmt.Sprintf("%v", it))
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprintf("%v", x)
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
