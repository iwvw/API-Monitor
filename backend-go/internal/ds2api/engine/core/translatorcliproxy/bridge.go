package translatorcliproxy

import (
	"context"
	"encoding/json"
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	_ "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator/builtin"
)

func ToOpenAI(from sdktranslator.Format, model string, raw []byte, stream bool) []byte {
	return sdktranslator.TranslateRequest(from, sdktranslator.FormatOpenAI, model, raw, stream)
}

func FromOpenAINonStream(to sdktranslator.Format, model string, originalReq, translatedReq, raw []byte) []byte {
	var param any
	converted := sdktranslator.TranslateNonStream(context.Background(), sdktranslator.FormatOpenAI, to, model, originalReq, translatedReq, raw, &param)
	usage, ok := extractOpenAIUsage(raw)
	if !ok {
		return converted
	}
	return injectNonStreamUsageMetadata(converted, to, usage)
}

func ParseFormat(name string) sdktranslator.Format {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai", "openai-chat", "chat", "chat-completions":
		return sdktranslator.FormatOpenAI
	case "openai-response", "responses", "openai-responses":
		return sdktranslator.FormatOpenAIResponse
	case "claude", "anthropic":
		return sdktranslator.FormatClaude
	case "gemini", "google":
		return sdktranslator.FormatGemini
	case "gemini-cli", "geminicli":
		return sdktranslator.FormatGeminiCLI
	case "codex", "openai-codex":
		return sdktranslator.FormatCodex
	case "antigravity":
		return sdktranslator.FormatAntigravity
	default:
		return sdktranslator.FromString(name)
	}
}

func injectNonStreamUsageMetadata(converted []byte, target sdktranslator.Format, usage openAIUsage) []byte {
	obj := map[string]any{}
	if err := json.Unmarshal(converted, &obj); err != nil {
		return converted
	}
	switch target {
	case sdktranslator.FormatClaude:
		obj["usage"] = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	case sdktranslator.FormatGemini:
		obj["usageMetadata"] = map[string]any{
			"promptTokenCount":     usage.PromptTokens,
			"candidatesTokenCount": usage.CompletionTokens,
			"totalTokenCount":      usage.TotalTokens,
		}
	default:
		return converted
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return converted
	}
	return out
}
