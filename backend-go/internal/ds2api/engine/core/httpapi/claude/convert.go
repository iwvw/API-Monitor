package claude

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/claudeconv"
)

const defaultClaudeModel = "claude-sonnet-4-6"

func convertClaudeToDeepSeek(claudeReq map[string]any, store ConfigReader) map[string]any {
	return claudeconv.ConvertClaudeToDeepSeek(claudeReq, store, defaultClaudeModel)
}
