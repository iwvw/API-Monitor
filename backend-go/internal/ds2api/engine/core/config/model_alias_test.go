package config

import "testing"

type mockModelAliasReader map[string]string

func (m mockModelAliasReader) ModelAliases() map[string]string { return m }

func TestResolveModelDirectDeepSeek(t *testing.T) {
	got, ok := ResolveModel(nil, "deepseek-flash")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelDirectDeepSeekNoThinking(t *testing.T) {
	got, ok := ResolveModel(nil, "deepseek-flash-nothinking")
	if !ok || got != "deepseek-flash-nothinking" {
		t.Fatalf("expected deepseek-flash-nothinking, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelAlias(t *testing.T) {
	got, ok := ResolveModel(nil, "gpt-4.1")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected alias gpt-4.1 -> deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveLatestOpenAIAlias(t *testing.T) {
	got, ok := ResolveModel(nil, "gpt-5.5")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected alias gpt-5.5 -> deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveLatestClaudeAlias(t *testing.T) {
	got, ok := ResolveModel(nil, "claude-sonnet-4-6")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected alias claude-sonnet-4-6 -> deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestResolveLatestClaudeAliasNoThinking(t *testing.T) {
	got, ok := ResolveModel(nil, "claude-sonnet-4-6-nothinking")
	if !ok || got != "deepseek-flash-nothinking" {
		t.Fatalf("expected alias claude-sonnet-4-6-nothinking -> deepseek-flash-nothinking, got ok=%v model=%q", ok, got)
	}
}

func TestResolveExpandedHistoricalAliases(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  string
	}{
		{name: "openai old chatgpt", model: "chatgpt-4o", want: "deepseek-flash"},
		{name: "openai codex max", model: "gpt-5.1-codex-max", want: "deepseek-flash"},
		{name: "openai deep research", model: "o3-deep-research", want: "deepseek-flash-search"},
		{name: "openai historical reasoning", model: "o1-preview", want: "deepseek-flash"},
		{name: "claude latest historical", model: "claude-3-5-sonnet-latest", want: "deepseek-flash"},
		{name: "claude historical opus", model: "claude-3-opus-20240229", want: "deepseek-flash"},
		{name: "claude historical haiku", model: "claude-3-haiku-20240307", want: "deepseek-flash"},
		{name: "gemini latest alias", model: "gemini-flash-latest", want: "deepseek-flash"},
		{name: "gemini historical pro", model: "gemini-1.5-pro", want: "deepseek-flash"},
		{name: "gemini vision legacy", model: "gemini-pro-vision", want: "deepseek-flash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveModel(nil, tc.model)
			if !ok || got != tc.want {
				t.Fatalf("expected alias %s -> %s, got ok=%v model=%q", tc.model, tc.want, ok, got)
			}
		})
	}
}

func TestResolveModelUnknown(t *testing.T) {
	_, ok := ResolveModel(nil, "totally-custom-model")
	if ok {
		t.Fatal("expected unknown model to fail resolve")
	}
}

func TestResolveModelUnknownKnownFamilyName(t *testing.T) {
	_, ok := ResolveModel(nil, "gpt-5.5-pro-search")
	if ok {
		t.Fatal("expected unknown known-family model to fail resolve without alias")
	}
}

func TestResolveModelRejectsLegacyDeepSeekIDs(t *testing.T) {
	legacyModels := []string{
		"deepseek-chat",
		"deepseek-reasoner",
		"deepseek-chat-search",
		"deepseek-reasoner-search",
		"deepseek-expert-chat",
		"deepseek-expert-reasoner",
		"deepseek-vision-chat",
	}
	for _, model := range legacyModels {
		if got, ok := ResolveModel(nil, model); ok {
			t.Fatalf("expected legacy model %q to be rejected, got %q", model, got)
		}
	}
}

func TestResolveModelRejectsRetiredHistoricalModels(t *testing.T) {
	retiredModels := []string{
		"claude-2.1",
		"claude-instant-1.2",
		"gpt-3.5-turbo",
	}
	for _, model := range retiredModels {
		if got, ok := ResolveModel(nil, model); ok {
			t.Fatalf("expected retired model %q to be rejected, got %q", model, got)
		}
	}
}

// 退役档位（pro / vision）统一收编为 flash，仍可解析但落到合并模型。
func TestResolveModelRetiredTierCollapsesToFlash(t *testing.T) {
	cases := map[string]string{
		"deepseek-v4-pro":               "deepseek-flash",
		"deepseek-v4-pro-nothinking":    "deepseek-flash-nothinking",
		"deepseek-v4-pro-search":        "deepseek-flash-search",
		"deepseek-v4-vision":            "deepseek-flash",
		"deepseek-v4-vision-nothinking": "deepseek-flash-nothinking",
		"deepseek-v4-vision-search":     "deepseek-flash-search",
	}
	for model, want := range cases {
		got, ok := ResolveModel(nil, model)
		if !ok || got != want {
			t.Fatalf("expected %q -> %q, got ok=%v model=%q", model, want, ok, got)
		}
	}
}

func TestResolveModelCustomAliasToFlash(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"my-flash-model": "deepseek-flash-search",
	}, "my-flash-model")
	if !ok || got != "deepseek-flash-search" {
		t.Fatalf("expected alias -> deepseek-flash-search, got ok=%v model=%q", ok, got)
	}
}

// 自定义别名指向已退役档位时落到合并后的 flash。
func TestResolveModelCustomAliasToRetiredTierCollapsesToFlash(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"my-vision-model": "deepseek-v4-vision",
	}, "my-vision-model")
	if !ok || got != "deepseek-flash" {
		t.Fatalf("expected alias -> deepseek-flash, got ok=%v model=%q", ok, got)
	}
}

func TestClaudeModelsResponsePaginationFields(t *testing.T) {
	resp := ClaudeModelsResponse()
	if _, ok := resp["first_id"]; !ok {
		t.Fatalf("expected first_id in response: %#v", resp)
	}
	if _, ok := resp["last_id"]; !ok {
		t.Fatalf("expected last_id in response: %#v", resp)
	}
	if _, ok := resp["has_more"]; !ok {
		t.Fatalf("expected has_more in response: %#v", resp)
	}
}
