package gemini

import "testing"

func TestNormalizeGeminiRequestNoThinkingModelForcesThinkingOff(t *testing.T) {
	req := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "hello"}},
			},
		},
		"reasoning_effort": "high",
	}
	out, err := normalizeGeminiRequest(testGeminiConfig{}, "gemini-2.5-pro-nothinking", req, false)
	if err != nil {
		t.Fatalf("normalizeGeminiRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-pro-nothinking" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if out.Thinking {
		t.Fatalf("expected nothinking model to force thinking off")
	}
	if out.Search {
		t.Fatalf("expected search=false, got=%v", out.Search)
	}
}
