package openai

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"strings"
	"time"
)

func BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText string, toolNames []string, toolsRaw any) map[string]any {
	detected := toolcall.ParseAssistantToolCallsDetailed(finalText, finalThinking, toolNames)
	return BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, finalText, detected.Calls, toolsRaw)
}

func BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, finalText string, detected []toolcall.ParsedToolCall, toolsRaw any) map[string]any {
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if strings.TrimSpace(finalThinking) != "" {
		messageObj["reasoning_content"] = finalThinking
	}
	if len(detected) > 0 {
		finishReason = "tool_calls"
		messageObj["tool_calls"] = toolcall.FormatOpenAIToolCalls(detected, toolsRaw)
		messageObj["content"] = nil
	}

	return map[string]any{
		"id":      completionID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": messageObj, "finish_reason": finishReason}},
		"usage":   BuildChatUsageForModel(model, finalPrompt, finalThinking, finalText, 0),
	}
}

func BuildChatStreamDeltaChoice(index int, delta map[string]any) map[string]any {
	return map[string]any{
		"delta": delta,
		"index": index,
	}
}

func BuildChatStreamFinishChoice(index int, finishReason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"index":         index,
		"finish_reason": finishReason,
	}
}

func BuildChatStreamChunk(completionID string, created int64, model string, choices []map[string]any, usage map[string]any) map[string]any {
	out := map[string]any{
		"id":      completionID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": choices,
	}
	if len(usage) > 0 {
		out["usage"] = usage
	}
	return out
}
