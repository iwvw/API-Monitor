package gemini

import "strings"

//nolint:unused // kept for native Gemini adapter route compatibility.
func convertGeminiTools(raw any) []any {
	tools, _ := raw.([]any)
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if fnDecls, ok := tool["functionDeclarations"].([]any); ok && len(fnDecls) > 0 {
			for _, declRaw := range fnDecls {
				decl, ok := declRaw.(map[string]any)
				if !ok {
					continue
				}
				name := strings.TrimSpace(asString(decl["name"]))
				if name == "" {
					continue
				}
				function := map[string]any{
					"name": name,
				}
				if desc := strings.TrimSpace(asString(decl["description"])); desc != "" {
					function["description"] = desc
				}
				if params, ok := decl["parameters"].(map[string]any); ok {
					function["parameters"] = params
				}
				out = append(out, map[string]any{
					"type":     "function",
					"function": function,
				})
			}
			continue
		}

		// OpenAI-style passthrough fallback.
		if _, ok := tool["function"].(map[string]any); ok {
			out = append(out, tool)
			continue
		}

		// Loose fallback for flattened function schema objects.
		name := strings.TrimSpace(asString(tool["name"]))
		if name == "" {
			continue
		}
		fn := map[string]any{"name": name}
		if desc := strings.TrimSpace(asString(tool["description"])); desc != "" {
			fn["description"] = desc
		}
		if params, ok := tool["parameters"].(map[string]any); ok {
			fn["parameters"] = params
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
