package claude

import (
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

type claudeNormalizedRequest struct {
	Standard           promptcompat.StandardRequest
	NormalizedMessages []any
}

func normalizeClaudeRequest(store ConfigReader, req map[string]any) (claudeNormalizedRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return claudeNormalizedRequest{}, fmt.Errorf("request must include 'model' and 'messages'")
	}
	if _, ok := req["max_tokens"]; !ok {
		req["max_tokens"] = 8192
	}
	normalizedMessages := normalizeClaudeMessages(messagesRaw)
	payload := cloneMap(req)
	payload["messages"] = normalizedMessages
	toolsRequested, _ := req["tools"].([]any)
	payload["messages"] = injectClaudeToolPrompt(payload, normalizedMessages, toolsRequested)

	dsPayload := convertClaudeToDeepSeek(payload, store)
	dsModel, _ := dsPayload["model"].(string)
	defaultThinkingEnabled, searchEnabled, ok := config.GetModelConfig(dsModel)
	if !ok {
		searchEnabled = false
	}
	thinkingEnabled := util.ResolveThinkingEnabled(req, defaultThinkingEnabled)
	if config.IsNoThinkingModel(dsModel) {
		thinkingEnabled = false
	}
	finalPrompt := prompt.MessagesPrepareWithThinking(toMessageMaps(dsPayload["messages"]), thinkingEnabled)
	toolNames := extractClaudeToolNames(toolsRequested)
	if len(toolNames) == 0 && len(toolsRequested) > 0 {
		toolNames = []string{"__any_tool__"}
	}

	return claudeNormalizedRequest{
		Standard: promptcompat.StandardRequest{
			Surface:         "anthropic_messages",
			RequestedModel:  strings.TrimSpace(model),
			ResolvedModel:   dsModel,
			ResponseModel:   strings.TrimSpace(model),
			Messages:        normalizedMessages,
			PromptTokenText: finalPrompt,
			ToolsRaw:        toolsRequested,
			FinalPrompt:     finalPrompt,
			ToolNames:       toolNames,
			Stream:          util.ToBool(req["stream"]),
			Thinking:        thinkingEnabled,
			Search:          searchEnabled,
		},
		NormalizedMessages: normalizedMessages,
	}, nil
}

func injectClaudeToolPrompt(payload map[string]any, normalizedMessages []any, tools []any) []any {
	if len(tools) == 0 {
		return normalizedMessages
	}
	descriptions, instructions := buildClaudeToolPromptParts(tools)
	descriptions = strings.TrimSpace(descriptions)
	instructions = strings.TrimSpace(instructions)
	if descriptions == "" && instructions == "" {
		return normalizedMessages
	}

	// Tool descriptions/schemas stay in the Anthropic-style system prompt:
	// top-level payload.system when available, otherwise the first system
	// message, else a new leading system message.
	messages := normalizedMessages
	if descriptions != "" {
		if systemText, ok := payload["system"].(string); ok && strings.TrimSpace(systemText) != "" {
			payload["system"] = mergeSystemPrompt(systemText, descriptions)
		} else {
			messages = mergeClaudeToolDescriptions(normalizedMessages, descriptions)
		}
	}
	// The tool-call format spec moves to the end of the message sequence as a
	// system-role message, right before the final <Assistant> marker.
	return appendClaudeToolCallFormatSpec(messages, instructions)
}

func mergeClaudeToolDescriptions(normalizedMessages []any, descriptions string) []any {
	messages := cloneAnySlice(normalizedMessages)
	for i := range messages {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "system") {
			continue
		}
		copied := cloneMap(msg)
		copied["content"] = mergeSystemPrompt(strings.TrimSpace(fmt.Sprintf("%v", copied["content"])), descriptions)
		messages[i] = copied
		return messages
	}
	return append([]any{map[string]any{"role": "system", "content": descriptions}}, messages...)
}

// appendClaudeToolCallFormatSpec appends the tool-call format spec as a
// system-role message at the end of the message sequence, immediately before
// the final <Assistant> completion marker (or before the trailing assistant
// message when the sequence ends with one).
func appendClaudeToolCallFormatSpec(messages []any, spec string) []any {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return messages
	}
	insertAt := len(messages)
	if n := len(messages); n > 0 {
		if msg, ok := messages[n-1].(map[string]any); ok {
			if role, _ := msg["role"].(string); strings.EqualFold(strings.TrimSpace(role), "assistant") {
				insertAt = n - 1
			}
		}
	}
	block := map[string]any{"role": "system", "content": spec}
	out := make([]any, 0, len(messages)+1)
	out = append(out, messages[:insertAt]...)
	out = append(out, block)
	out = append(out, messages[insertAt:]...)
	return out
}

func mergeSystemPrompt(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "\n\n" + extra
	}
}

func cloneAnySlice(in []any) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	copy(out, in)
	return out
}
