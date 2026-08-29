package gemini

import (
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

//nolint:unused // kept for native Gemini adapter route compatibility.
func normalizeGeminiRequest(store ConfigReader, routeModel string, req map[string]any, stream bool) (promptcompat.StandardRequest, error) {
	requestedModel := strings.TrimSpace(routeModel)
	if requestedModel == "" {
		return promptcompat.StandardRequest{}, fmt.Errorf("model is required in request path")
	}

	resolvedModel, ok := config.ResolveModel(store, requestedModel)
	if !ok {
		return promptcompat.StandardRequest{}, fmt.Errorf("model %q is not available", requestedModel)
	}
	defaultThinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)
	thinkingEnabled := util.ResolveThinkingEnabled(req, defaultThinkingEnabled)
	if config.IsNoThinkingModel(resolvedModel) {
		thinkingEnabled = false
	}

	messagesRaw := geminiMessagesFromRequest(req)
	if len(messagesRaw) == 0 {
		return promptcompat.StandardRequest{}, fmt.Errorf("request must include non-empty contents")
	}

	toolsRaw := convertGeminiTools(req["tools"])
	finalPrompt, toolNames := promptcompat.BuildOpenAIPromptForAdapter(messagesRaw, toolsRaw, "", thinkingEnabled)
	if len(toolNames) == 0 && len(toolsRaw) > 0 {
		toolNames = []string{"__any_tool__"}
	}
	passThrough := collectGeminiPassThrough(req)

	return promptcompat.StandardRequest{
		Surface:         "google_gemini",
		RequestedModel:  requestedModel,
		ResolvedModel:   resolvedModel,
		ResponseModel:   requestedModel,
		Messages:        messagesRaw,
		PromptTokenText: finalPrompt,
		ToolsRaw:        toolsRaw,
		FinalPrompt:     finalPrompt,
		ToolNames:       toolNames,
		Stream:          stream,
		Thinking:        thinkingEnabled,
		Search:          searchEnabled,
		PassThrough:     passThrough,
	}, nil
}
