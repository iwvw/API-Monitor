package promptcompat

import (
	"strings"
	"testing"
)

func TestAppendThinkingInjectionToLatestUserStringContent(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "older"},
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	latest := out[2].(map[string]any)
	content, _ := latest["content"].(string)
	if !strings.Contains(content, "latest\n\n"+ThinkingInjectionMarker) {
		t.Fatalf("expected injection after latest user text, got %q", content)
	}
	older := out[0].(map[string]any)
	if older["content"] != "older" {
		t.Fatalf("expected older user message unchanged, got %#v", older["content"])
	}
}

func TestAppendThinkingInjectionToLatestUserArrayContent(t *testing.T) {
	messages := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "latest"},
			},
		},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	content, _ := out[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected appended text block, got %#v", content)
	}
	block, _ := content[1].(map[string]any)
	if block["type"] != "text" || !strings.Contains(block["text"].(string), ThinkingInjectionMarker) {
		t.Fatalf("unexpected appended block: %#v", block)
	}
}

func TestAppendThinkingInjectionToLatestUserCustomPrompt(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionPromptToLatestUser(messages, "custom thinking format")
	if !changed {
		t.Fatal("expected custom thinking injection to be appended")
	}
	content, _ := out[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, "latest\n\ncustom thinking format") {
		t.Fatalf("expected custom injection after latest user text, got %q", content)
	}
}

func TestAppendThinkingInjectionToLatestUserSkipsDuplicate(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "latest\n\n" + DefaultThinkingInjectionPrompt},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if changed {
		t.Fatal("expected duplicate injection to be skipped")
	}
	if len(out) != 1 {
		t.Fatalf("unexpected messages: %#v", out)
	}
}
