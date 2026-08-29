package openai

import "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"

func BuildChatUsageForModel(model, finalPrompt, finalThinking, finalText string, refFileTokens int) map[string]any {
	promptTokens := util.CountPromptTokens(finalPrompt, model) + refFileTokens
	reasoningTokens := util.CountOutputTokens(finalThinking, model)
	completionTokens := util.CountOutputTokens(finalText, model)
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": reasoningTokens + completionTokens,
		"total_tokens":      promptTokens + reasoningTokens + completionTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func BuildChatUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	return BuildChatUsageForModel("", finalPrompt, finalThinking, finalText, 0)
}

func BuildResponsesUsageForModel(model, finalPrompt, finalThinking, finalText string, refFileTokens int) map[string]any {
	promptTokens := util.CountPromptTokens(finalPrompt, model) + refFileTokens
	reasoningTokens := util.CountOutputTokens(finalThinking, model)
	completionTokens := util.CountOutputTokens(finalText, model)
	return map[string]any{
		"input_tokens":  promptTokens,
		"output_tokens": reasoningTokens + completionTokens,
		"total_tokens":  promptTokens + reasoningTokens + completionTokens,
	}
}

func BuildResponsesUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	return BuildResponsesUsageForModel("", finalPrompt, finalThinking, finalText, 0)
}
