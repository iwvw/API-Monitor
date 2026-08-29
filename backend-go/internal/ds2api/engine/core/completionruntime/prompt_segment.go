package completionruntime

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

// ExpertPromptSegmentConfigReader is the minimal interface needed to decide
// whether expert prompt segmentation should run.
type ExpertPromptSegmentConfigReader interface {
	ExpertPromptSegmentEnabled() bool
	ExpertPromptSegmentMaxChars() int
}

// shouldSegmentExpertPrompt returns max-rune segmented prompts when the
// resolved model is expert, segmentation is enabled, and the finalized prompt
// exceeds the configured rune threshold. Returns nil when segmentation is not
// applicable.
func shouldSegmentExpertPrompt(stdReq promptcompat.StandardRequest, opts Options) []string {
	segmentReader := opts.ExpertPromptSegment
	if segmentReader == nil {
		return nil
	}
	if !segmentReader.ExpertPromptSegmentEnabled() {
		return nil
	}
	modelType, ok := config.GetModelType(stdReq.ResolvedModel)
	if !ok || modelType != "expert" {
		return nil
	}
	maxChars := segmentReader.ExpertPromptSegmentMaxChars()
	if maxChars <= 0 {
		return nil
	}
	promptText := stdReq.FinalPrompt
	if len([]rune(promptText)) <= maxChars {
		return nil
	}
	segments := prompt.SplitByMaxRunes(promptText, maxChars)
	if len(segments) <= 1 {
		return nil
	}
	return segments
}
