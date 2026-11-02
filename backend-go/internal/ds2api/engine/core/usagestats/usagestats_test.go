package usagestats

import (
	"path/filepath"
	"testing"
)

func TestUsageStatsStore_AddAndQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	store := New(path)

	store.AddCosted("deepseek-chat", "user-1", 100, 200, 50, 350)
	store.AddCosted("deepseek-chat", "user-1", 50, 50, 0, 100)

	entries := store.Summary()
	if len(entries) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Model != "deepseek-chat" {
		t.Fatalf("unexpected model: %s", e.Model)
	}
	if e.CallerID != "user-1" {
		t.Fatalf("unexpected caller ID: %s", e.CallerID)
	}
	if e.PromptTokens != 150 {
		t.Fatalf("expected 150 prompt tokens, got %d", e.PromptTokens)
	}
	if e.CompletionTokens != 250 {
		t.Fatalf("expected 250 completion tokens, got %d", e.CompletionTokens)
	}
	if e.TotalTokens != 450 {
		t.Fatalf("expected 450 total tokens, got %d", e.TotalTokens)
	}
	if e.Calls != 2 {
		t.Fatalf("expected 2 calls, got %d", e.Calls)
	}

	// Test persistence reload
	store2 := New(path)
	entries2 := store2.Summary()
	if len(entries2) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(entries2))
	}
	if entries2[0].TotalTokens != 450 {
		t.Fatalf("expected 450 tokens after reload, got %d", entries2[0].TotalTokens)
	}
}

func TestParseUsage(t *testing.T) {
	usageMap := map[string]any{
		"prompt_tokens":     100,
		"completion_tokens": 200,
		"total_tokens":      300,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": 50,
		},
	}
	prompt, completion, reasoning, total := ParseUsage(usageMap)
	if prompt != 100 || completion != 200 || reasoning != 50 || total != 300 {
		t.Fatalf("unexpected ParseUsage result: %d, %d, %d, %d", prompt, completion, reasoning, total)
	}
}

func TestUsageSettingsValidation(t *testing.T) {
	s := DefaultSettings()
	if !s.Enabled {
		t.Fatalf("expected default settings enabled to be true")
	}
	if s.Peak.Multiplier <= 0 {
		t.Fatalf("expected positive multiplier")
	}
}
