package openai

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

func BuildResponseObject(responseID, model, finalPrompt, finalThinking, finalText string, toolNames []string, toolsRaw any) map[string]any {
	// Strict mode: only standalone, structured tool-call payloads are treated
	// as executable tool calls.
	detected := toolcall.ParseAssistantToolCallsDetailed(finalText, finalThinking, toolNames)
	return BuildResponseObjectWithToolCalls(responseID, model, finalPrompt, finalThinking, finalText, detected.Calls, toolsRaw)
}

func BuildResponseObjectWithToolCalls(responseID, model, finalPrompt, finalThinking, finalText string, detected []toolcall.ParsedToolCall, toolsRaw any) map[string]any {
	exposedOutputText := finalText
	output := make([]any, 0, 3)
	if strings.TrimSpace(finalThinking) != "" {
		output = append(output, buildResponsesReasoningItem(finalThinking))
	}
	if len(detected) > 0 {
		exposedOutputText = ""
		output = append(output, toResponsesFunctionCallItems(detected, toolsRaw)...)
	} else {
		if strings.TrimSpace(finalText) == "" {
			if strings.TrimSpace(finalThinking) != "" {
				exposedOutputText = finalThinking
			}
		} else {
			output = append(output, map[string]any{
				"type":    "message",
				"id":      "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
				"role":    "assistant",
				"status":  "completed",
				"content": []any{map[string]any{"type": "output_text", "text": finalText}},
			})
		}
	}
	return BuildResponseObjectFromItems(
		responseID,
		model,
		finalPrompt,
		finalThinking,
		finalText,
		output,
		exposedOutputText,
	)
}

func buildResponsesReasoningItem(finalThinking string) map[string]any {
	return map[string]any{
		"id":   "rsn_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"type": "reasoning",
		"summary": []any{map[string]any{
			"type": "summary_text",
			"text": finalThinking,
		}},
		"content": []any{map[string]any{
			"type": "reasoning_text",
			"text": finalThinking,
		}},
		"status": "completed",
	}
}

func BuildResponseObjectFromItems(responseID, model, finalPrompt, finalThinking, finalText string, output []any, outputText string) map[string]any {
	if output == nil {
		output = []any{}
	}
	return map[string]any{
		"id":          responseID,
		"type":        "response",
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       model,
		"output":      output,
		"output_text": outputText,
		"usage":       BuildResponsesUsageForModel(model, finalPrompt, finalThinking, finalText, 0),
	}
}

func toResponsesFunctionCallItems(toolCalls []toolcall.ParsedToolCall, toolsRaw any) []any {
	if len(toolCalls) == 0 {
		return nil
	}
	normalizedCalls := toolcall.NormalizeParsedToolCallsForSchemas(toolCalls, toolsRaw)
	out := make([]any, 0, len(toolCalls))
	for _, tc := range normalizedCalls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		argsBytes, _ := json.Marshal(tc.Input)
		args := normalizeJSONString(string(argsBytes))
		out = append(out, map[string]any{
			"id":        "fc_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":      "function_call",
			"call_id":   "call_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"name":      tc.Name,
			"arguments": args,
			"status":    "completed",
		})
	}
	return out
}

func normalizeJSONString(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(b)
}
