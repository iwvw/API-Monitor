package promptcompat

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return BuildOpenAIPrompt(messagesRaw, toolsRaw, traceID, DefaultToolChoicePolicy(), thinkingEnabled)
}

func BuildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, true)
}

func BuildOpenAIPromptWithToolInstructionsOnly(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, false)
}

// BuildOpenAIPromptWithForcedFormatSpec builds a prompt and always appends the
// tool-name-independent EPSE tool-call format spec, regardless of whether a
// top-level `tools` array is present. It is used by the current-input-file
// continuation path: when the original request exposed its tool catalog inside
// a system message (no `tools` array), the catalog is uploaded as HISTORY.txt
// context while the live continuation prompt still needs the format spec so the
// model keeps emitting tool calls in the <|EPSE|tool_calls> format.
func BuildOpenAIPromptWithForcedFormatSpec(messagesRaw []any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	messages := NormalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	messages, toolNames := injectToolCallFormatSpecOnly(messages, toolPolicy)
	return prompt.MessagesPrepareWithThinking(messages, thinkingEnabled), toolNames
}

func buildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool, includeToolDescriptions bool) (string, []string) {
	messages := NormalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	toolNames := []string{}
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		if includeToolDescriptions {
			messages, toolNames = injectToolPrompt(messages, tools, toolPolicy)
		} else {
			messages, toolNames = injectToolPromptInstructionsOnly(messages, tools, toolPolicy)
		}
	} else if MessagesDeclareToolsInSystemText(messagesRaw) {
		// The caller exposed its tool catalog inside a system message rather
		// than a top-level `tools` array (e.g. the RikkaHub workspace app), so
		// there is no schema to extract. Inject the tool-name-independent
		// EPSE format spec so the model still knows how to emit tool calls.
		messages, toolNames = injectToolCallFormatSpecOnly(messages, toolPolicy)
	}
	return prompt.MessagesPrepareWithThinking(messages, thinkingEnabled), toolNames
}

// BuildOpenAIPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same tool/history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildOpenAIPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID, thinkingEnabled)
}
