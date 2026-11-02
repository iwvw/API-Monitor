package shared

import (
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
)

func DetectAssistantToolCalls(rawText, visibleText, exposedThinking, detectionThinking string, toolNames []string) toolcall.ToolCallParseResult {
	textParsed := toolcall.ParseStandaloneToolCallsDetailed(rawText, toolNames)
	if len(textParsed.Calls) > 0 {
		return textParsed
	}
	if strings.TrimSpace(visibleText) != "" {
		return textParsed
	}
	thinking := detectionThinking
	if strings.TrimSpace(thinking) == "" {
		thinking = exposedThinking
	}
	thinkingParsed := toolcall.ParseStandaloneToolCallsDetailed(thinking, toolNames)
	if len(thinkingParsed.Calls) > 0 {
		return thinkingParsed
	}
	return textParsed
}
