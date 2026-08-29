package responses

// addRefFileTokensToUsage adds inline-uploaded file token estimates to an existing
// usage map inside a response object. This keeps the token accounting aware of file
// content that the upstream model processes but that is not part of the prompt text.
func addRefFileTokensToUsage(obj map[string]any, refFileTokens int) {
	if refFileTokens <= 0 || obj == nil {
		return
	}
	usage, ok := obj["usage"].(map[string]any)
	if !ok || usage == nil {
		return
	}
	for _, key := range []string{"input_tokens", "prompt_tokens"} {
		if v, ok := usage[key]; ok {
			if n, ok := v.(int); ok {
				usage[key] = n + refFileTokens
			}
		}
	}
	if v, ok := usage["total_tokens"]; ok {
		if n, ok := v.(int); ok {
			usage["total_tokens"] = n + refFileTokens
		}
	}
}
