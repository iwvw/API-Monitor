package rawsample

import (
	"encoding/json"
	"strings"
)

//nolint:unused // retained for raw-sample processing entrypoints.
func extractProcessedVisibleText(raw []byte, kind, contentType string) string {
	if len(raw) == 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "json":
		return parseOpenAIJSONText(string(raw))
	case "stream":
		return parseOpenAIStreamText(string(raw))
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(ct, "application/json") {
		return parseOpenAIJSONText(string(raw))
	}
	return parseOpenAIStreamText(string(raw))
}

//nolint:unused // retained for raw-sample processing entrypoints.
func parseOpenAIStreamText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var out strings.Builder
	for _, block := range strings.Split(raw, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		dataLines := make([]string, 0, 2)
		for _, line := range strings.Split(block, "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if len(dataLines) == 0 {
			continue
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		if payload == "" || payload == "[DONE]" || !strings.HasPrefix(payload, "{") {
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			continue
		}
		out.WriteString(extractOpenAIVisibleTextValue(decoded))
	}
	return out.String()
}

//nolint:unused // retained for raw-sample processing entrypoints.
func parseOpenAIJSONText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return ""
	}
	return extractOpenAIVisibleTextValue(decoded)
}

//nolint:unused // retained for raw-sample processing entrypoints.
func extractOpenAIVisibleTextValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out strings.Builder
		for _, item := range x {
			out.WriteString(extractOpenAIVisibleTextValue(item))
		}
		return out.String()
	case map[string]any:
		var out strings.Builder
		if s, ok := x["output_text"].(string); ok {
			out.WriteString(s)
		}
		if arr, ok := x["output"].([]any); ok {
			for _, item := range arr {
				out.WriteString(extractOpenAIVisibleTextValue(item))
			}
		}
		if arr, ok := x["choices"].([]any); ok {
			for _, item := range arr {
				out.WriteString(extractOpenAIVisibleTextValue(item))
			}
		}
		if msg, ok := x["message"]; ok {
			out.WriteString(extractOpenAIVisibleTextValue(msg))
		}
		if delta, ok := x["delta"]; ok {
			out.WriteString(extractOpenAIVisibleTextValue(delta))
		}
		if content, ok := x["content"]; ok {
			out.WriteString(extractOpenAIVisibleTextValue(content))
		}
		if reasoning, ok := x["reasoning_content"]; ok {
			out.WriteString(extractOpenAIVisibleTextValue(reasoning))
		}
		if text, ok := x["text"]; ok {
			out.WriteString(extractOpenAIVisibleTextValue(text))
		}
		return out.String()
	default:
		return ""
	}
}
