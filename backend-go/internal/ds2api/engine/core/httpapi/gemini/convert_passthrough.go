package gemini

import (
	"encoding/json"
	"strings"
)

//nolint:unused // compatibility hook for native Gemini request normalization path.
func collectGeminiPassThrough(req map[string]any) map[string]any {
	cfg, _ := req["generationConfig"].(map[string]any)
	if len(cfg) == 0 {
		return nil
	}
	out := map[string]any{}
	if v, ok := cfg["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := cfg["topP"]; ok {
		out["top_p"] = v
	}
	if v, ok := cfg["maxOutputTokens"]; ok {
		out["max_tokens"] = v
	}
	if v, ok := cfg["stopSequences"]; ok {
		out["stop"] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func stringifyJSON(v any) string {
	switch x := v.(type) {
	case nil:
		return "{}"
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "{}"
		}
		return s
	default:
		b, err := json.Marshal(x)
		if err != nil || len(b) == 0 {
			return "{}"
		}
		return string(b)
	}
}
