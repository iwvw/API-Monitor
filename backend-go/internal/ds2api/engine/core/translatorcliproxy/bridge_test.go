package translatorcliproxy

import (
	"strings"
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestToOpenAIClaude(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	got := ToOpenAI(sdktranslator.FormatClaude, "claude-sonnet-4-5", raw, false)
	s := string(got)
	if !strings.Contains(s, `"messages"`) || !strings.Contains(s, `"model"`) {
		t.Fatalf("unexpected translated request: %s", s)
	}
}

func TestToOpenAIGeminiThinkingBudgetZeroDisablesReasoning(t *testing.T) {
	raw := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`)
	got := string(ToOpenAI(sdktranslator.FormatGemini, "gemini-2.5-flash", raw, false))
	if !strings.Contains(got, `"reasoning_effort":"none"`) {
		t.Fatalf("expected Gemini thinkingBudget=0 to translate to reasoning_effort none, got: %s", got)
	}
}

func TestFromOpenAINonStreamClaude(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	translatedReq := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	openaibody := []byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	got := FromOpenAINonStream(sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translatedReq, openaibody)
	if !strings.Contains(string(got), `"type":"message"`) {
		t.Fatalf("expected claude response format, got: %s", string(got))
	}
}

func TestFromOpenAINonStreamClaudePreservesUsageFromOpenAI(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	translatedReq := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	openaibody := []byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":29,"total_tokens":40}}`)
	got := string(FromOpenAINonStream(sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translatedReq, openaibody))
	if !strings.Contains(got, `"input_tokens":11`) || !strings.Contains(got, `"output_tokens":29`) {
		t.Fatalf("expected claude usage to preserve prompt/completion tokens, got: %s", got)
	}
}

func TestFromOpenAINonStreamGeminiPreservesUsageFromOpenAI(t *testing.T) {
	original := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	translatedReq := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	openaibody := []byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"gemini-2.5-pro","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":29,"total_tokens":40}}`)
	got := string(FromOpenAINonStream(sdktranslator.FormatGemini, "gemini-2.5-pro", original, translatedReq, openaibody))
	if !strings.Contains(got, `"promptTokenCount":11`) || !strings.Contains(got, `"candidatesTokenCount":29`) || !strings.Contains(got, `"totalTokenCount":40`) {
		t.Fatalf("expected gemini usageMetadata to preserve prompt/completion tokens, got: %s", got)
	}
}

func TestFromOpenAINonStreamPreservesResponsesUsageShape(t *testing.T) {
	original := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	translatedReq := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	openaibody := []byte(`{"id":"resp_1","object":"response","model":"gemini-2.5-pro","usage":{"input_tokens":"11","output_tokens":"29","total_tokens":"40"}}`)
	gotGemini := string(FromOpenAINonStream(sdktranslator.FormatGemini, "gemini-2.5-pro", original, translatedReq, openaibody))
	if !strings.Contains(gotGemini, `"promptTokenCount":11`) || !strings.Contains(gotGemini, `"candidatesTokenCount":29`) || !strings.Contains(gotGemini, `"totalTokenCount":40`) {
		t.Fatalf("expected gemini usageMetadata from input/output usage fields, got: %s", gotGemini)
	}

	origClaude := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	gotClaude := string(FromOpenAINonStream(sdktranslator.FormatClaude, "claude-sonnet-4-5", origClaude, origClaude, openaibody))
	if !strings.Contains(gotClaude, `"input_tokens":11`) || !strings.Contains(gotClaude, `"output_tokens":29`) {
		t.Fatalf("expected claude usage from input/output usage fields, got: %s", gotClaude)
	}
}

func TestParseFormatAliases(t *testing.T) {
	cases := map[string]sdktranslator.Format{
		"responses":        sdktranslator.FormatOpenAIResponse,
		"anthropic":        sdktranslator.FormatClaude,
		"geminicli":        sdktranslator.FormatGeminiCLI,
		"openai-codex":     sdktranslator.FormatCodex,
		"antigravity":      sdktranslator.FormatAntigravity,
		"chat-completions": sdktranslator.FormatOpenAI,
	}
	for in, want := range cases {
		if got := ParseFormat(in); got != want {
			t.Fatalf("ParseFormat(%q)=%q want %q", in, got, want)
		}
	}
}
