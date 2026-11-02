package claude

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestNormalizeClaudeRequest(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-opus-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"stream": true,
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if norm.Standard.ResolvedModel == "" {
		t.Fatalf("expected resolved model")
	}
	if !norm.Standard.Stream {
		t.Fatalf("expected stream=true")
	}
	if len(norm.Standard.ToolNames) == 0 {
		t.Fatalf("expected tool names")
	}
	if norm.Standard.ToolsRaw == nil {
		t.Fatalf("expected ToolsRaw preserved for downstream normalization")
	}
	if norm.Standard.FinalPrompt == "" {
		t.Fatalf("expected non-empty final prompt")
	}
}

func TestNormalizeClaudeRequestSupportsCamelCaseInputSchemaPromptInjection(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{
				"name":        "todowrite",
				"description": "Write todos",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"todos": map[string]any{"type": "array"}}},
			},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if !containsStr(norm.Standard.FinalPrompt, `"type":"array"`) {
		t.Fatalf("expected inputSchema to be injected into prompt, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestMovesFormatSpecToEndBeforeAssistant(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model":  "claude-sonnet-4-5",
		"system": "top-level system",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !strings.HasSuffix(norm.Standard.FinalPrompt, "<Assistant>:") {
		t.Fatalf("expected tool format spec right before the final <Assistant> marker, got=%q", norm.Standard.FinalPrompt)
	}
	if !strings.Contains(norm.Standard.FinalPrompt, "<System>:工具调用格式规范") {
		t.Fatalf("expected tool format spec rendered as a system-role message with <System>: prefix, got=%q", norm.Standard.FinalPrompt)
	}
	if !strings.Contains(norm.Standard.FinalPrompt, "top-level system") {
		t.Fatalf("expected top-level system preserved, got=%q", norm.Standard.FinalPrompt)
	}
	if !strings.Contains(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool descriptions injected, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoExistingSystemMessage(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "system", "content": "baseline rule"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected into final prompt, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "baseline rule") {
		t.Fatalf("expected existing system message preserved, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoTopLevelSystem(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model":  "claude-sonnet-4-5",
		"system": "top-level system",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "top-level system") {
		t.Fatalf("expected top-level system preserved, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected, got=%q", norm.Standard.FinalPrompt)
	}
}
