package translatorcliproxy

import (
	"net/http/httptest"
	"strings"
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestOpenAIStreamTranslatorWriterClaude(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("expected claude message_start event, got: %s", body)
	}
	if !strings.Contains(body, `"output_tokens":29`) {
		t.Fatalf("expected claude stream usage to preserve output tokens, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterGemini(t *testing.T) {
	original := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	translated := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "candidates") {
		t.Fatalf("expected gemini stream payload, got: %s", body)
	}
	if !strings.Contains(body, `"promptTokenCount":11`) || !strings.Contains(body, `"candidatesTokenCount":29`) {
		t.Fatalf("expected gemini stream usageMetadata to preserve usage, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterPreservesKeepAliveComment(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(": keep-alive\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive comment passthrough, got %q", body)
	}
}

func TestInjectStreamUsageMetadataPreservesSSEFrameTerminator(t *testing.T) {
	chunk := []byte("data: {\"candidates\":[{\"index\":0}],\"model\":\"gemini-2.5-pro\"}\n\n")
	usage := openAIUsage{PromptTokens: 11, CompletionTokens: 29, TotalTokens: 40}
	got := injectStreamUsageMetadata(chunk, sdktranslator.FormatGemini, usage)
	if !strings.HasSuffix(string(got), "\n\n") {
		t.Fatalf("expected injected chunk to preserve \\n\\n frame terminator, got %q", string(got))
	}
	if !strings.Contains(string(got), `"usageMetadata"`) {
		t.Fatalf("expected usageMetadata injected, got %q", string(got))
	}
}

func TestExtractOpenAIUsageSupportsResponsesUsageFields(t *testing.T) {
	line := []byte(`data: {"usage":{"input_tokens":"11","output_tokens":"29","total_tokens":"40"}}`)
	got, ok := extractOpenAIUsage(line)
	if !ok {
		t.Fatal("expected usage extracted from input/output usage fields")
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 29 || got.TotalTokens != 40 {
		t.Fatalf("unexpected usage extracted: %#v", got)
	}
}
