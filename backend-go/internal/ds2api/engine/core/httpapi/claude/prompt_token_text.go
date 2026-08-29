package claude

import "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"

func buildClaudePromptTokenText(messages []any, thinkingEnabled bool) string {
	return prompt.MessagesPrepareWithThinking(toMessageMaps(messages), thinkingEnabled)
}
