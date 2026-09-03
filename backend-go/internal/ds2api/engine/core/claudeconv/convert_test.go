package claudeconv

import (
	"reflect"
	"testing"
)

type mockAliasReader struct {
	aliases map[string]string
}

func (m mockAliasReader) ModelAliases() map[string]string {
	return m.aliases
}

func TestConvertClaudeToDeepSeek(t *testing.T) {
	reader := mockAliasReader{
		aliases: map[string]string{
			"claude-3-5-sonnet": "deepseek-v4-flash",
		},
	}

	req := map[string]any{
		"model":       "claude-3-5-sonnet",
		"system":      "You are a helpful assistant.",
		"temperature": 0.5,
		"top_p":       0.9,
		"stream":      true,
		"stop_sequences": []string{"STOP"},
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}

	out := ConvertClaudeToDeepSeek(req, reader, "default-model")
	if out["model"] != "deepseek-v4-flash" {
		t.Fatalf("expected model deepseek-v4-flash, got %v", out["model"])
	}
	if out["temperature"] != 0.5 {
		t.Fatalf("expected temperature 0.5, got %v", out["temperature"])
	}
	if out["stream"] != true {
		t.Fatalf("expected stream true, got %v", out["stream"])
	}
	if !reflect.DeepEqual(out["stop"], []string{"STOP"}) {
		t.Fatalf("unexpected stop: %v", out["stop"])
	}
	messages, ok := out["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %v", messages)
	}
	sysMsg := messages[0].(map[string]any)
	if sysMsg["role"] != "system" || sysMsg["content"] != "You are a helpful assistant." {
		t.Fatalf("unexpected system message: %v", sysMsg)
	}
	userMsg := messages[1].(map[string]any)
	if userMsg["role"] != "user" || userMsg["content"] != "hello" {
		t.Fatalf("unexpected user message: %v", userMsg)
	}
}

func TestConvertClaudeToDeepSeek_DefaultModel(t *testing.T) {
	req := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	out := ConvertClaudeToDeepSeek(req, nil, "")
	if out["model"] != "deepseek-v4-flash" {
		t.Fatalf("expected fallback deepseek-v4-flash, got %v", out["model"])
	}
}
