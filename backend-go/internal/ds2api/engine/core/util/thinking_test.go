package util

import "testing"

func TestResolveThinkingEnabledPriority(t *testing.T) {
	req := map[string]any{
		"thinking": map[string]any{"type": "disabled"},
		"extra_body": map[string]any{
			"thinking": map[string]any{"type": "enabled"},
		},
		"reasoning_effort": "high",
	}
	if got := ResolveThinkingEnabled(req, true); got {
		t.Fatalf("expected top-level thinking to win, got enabled=%v", got)
	}
}

func TestResolveThinkingEnabledUsesExtraBodyFallback(t *testing.T) {
	req := map[string]any{
		"extra_body": map[string]any{
			"thinking": map[string]any{"type": "disabled"},
		},
	}
	if got := ResolveThinkingEnabled(req, true); got {
		t.Fatalf("expected extra_body thinking to disable, got enabled=%v", got)
	}
}

func TestResolveThinkingEnabledMapsReasoningEffortToEnabled(t *testing.T) {
	for _, effort := range []string{"minimal", "low", "medium", "high", "xhigh"} {
		if got := ResolveThinkingEnabled(map[string]any{"reasoning_effort": effort}, false); !got {
			t.Fatalf("expected reasoning_effort=%s to enable thinking", effort)
		}
	}
}

func TestResolveThinkingEnabledMapsReasoningObject(t *testing.T) {
	req := map[string]any{"reasoning": map[string]any{"effort": "none"}}
	if got := ResolveThinkingEnabled(req, true); got {
		t.Fatalf("expected reasoning.effort=none to disable thinking")
	}
	req = map[string]any{"reasoning": map[string]any{"effort": "medium"}}
	if got := ResolveThinkingEnabled(req, false); !got {
		t.Fatalf("expected reasoning.effort=medium to enable thinking")
	}
}

func TestResolveThinkingEnabledDefaultsWhenUnset(t *testing.T) {
	if !ResolveThinkingEnabled(nil, true) {
		t.Fatal("expected default thinking=true when unset")
	}
	if ResolveThinkingEnabled(nil, false) {
		t.Fatal("expected default thinking=false when unset")
	}
}
