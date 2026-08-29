package assistantturn

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

type StopReason string

const (
	StopReasonStop          StopReason = "stop"
	StopReasonToolCalls     StopReason = "tool_calls"
	StopReasonContentFilter StopReason = "content_filter"
	StopReasonError         StopReason = "error"
)

type Usage struct {
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	TotalTokens     int
}

type OutputError struct {
	Status  int
	Message string
	Code    string
}

type Turn struct {
	Model             string
	Prompt            string
	RawText           string
	RawThinking       string
	DetectionThinking string
	Text              string
	Thinking          string
	ToolCalls         []toolcall.ParsedToolCall
	ParsedToolCalls   toolcall.ToolCallParseResult
	CitationLinks     map[int]string
	ContentFilter     bool
	UpstreamError     string
	ResponseMessageID int
	StopReason        StopReason
	Usage             Usage
	Error             *OutputError
}

type FinalizeOptions struct {
	AlreadyEmittedToolCalls bool
}

type FinalOutcome struct {
	FinishReason     string
	Error            *OutputError
	Usage            Usage
	HasToolCalls     bool
	HasVisibleText   bool
	HasVisibleOutput bool
	ShouldFail       bool
}

type BuildOptions struct {
	Model                 string
	Prompt                string
	RefFileTokens         int
	SearchEnabled         bool
	StripReferenceMarkers bool
	ToolNames             []string
	ToolsRaw              any
	ToolChoice            promptcompat.ToolChoicePolicy
	// ToolCallRepair, when non-nil, enables the phase-3 LLM tool-call repair
	// pass in the finalize path: if the parsed result found a residual tool
	// call intent but zero calls, the residual bad code is handed to this
	// invoker for repair. It must never be wired into the per-chunk streaming
	// sieve (plan/tool-call-fallback-design-phase3.md §5.1).
	ToolCallRepair toolcall.ToolCallRepairInvoker
	// ToolCallRepairCtx scopes the repair invocation; defaults to
	// context.Background() when nil.
	ToolCallRepairCtx context.Context
}

// maybeRepairToolCalls implements the phase-3 finalize-only LLM repair pass. It
// returns repaired calls (branch ①) or nil for the fallback branches (② NO
// TOOL, ③ unparseable/timeout/error), in which case the caller leaves the
// residual bad code as visible text ("原样输出"). It is a no-op unless the parse
// found a residual intent and a repair invoker is configured.
//
// Per phase1 §5.1 / phase3, it fires whenever there is a residual tool-call
// intent, INCLUDING the partial-success case where some calls parsed but a bad
// residual fragment remains (SawToolCallIntent && ResidualIntentText != ""):
// the residual is repaired and the caller merges the repaired calls with the
// already-parsed ones. The all-failed case (zero calls) is the classic branch.
//
// It also carries a defensive fallback (Direction 4): when the residual intent
// probe missed a residual but the deterministic parser still saw tool-call
// syntax and produced zero calls (SawToolCallSyntax && len(Calls)==0), the raw
// un-stripped text is handed to the repair layer. This closes the silent-leak
// gap where any intent-detection false negative would emit bad tool-call code
// as visible text.
func maybeRepairToolCalls(parsed toolcall.ToolCallParseResult, rawText string, opts BuildOptions) []toolcall.ParsedToolCall {
	if opts.ToolCallRepair == nil {
		return nil
	}
	badCode := strings.TrimSpace(parsed.ResidualIntentText)
	if !parsed.SawToolCallIntent {
		// Direction 4 defensive path: intent detection can miss a residual
		// (e.g. odd markdown fences confusing the residual probe), but the
		// deterministic parser still saw tool-call syntax and produced zero
		// calls. Rather than silently leaking the bad tool-call code as visible
		// text, hand the raw (un-stripped) text to the repair layer. Only fires
		// when syntax was seen AND nothing parsed, so well-formed / partial
		// successes are unaffected.
		if !parsed.SawToolCallSyntax || len(parsed.Calls) > 0 {
			return nil
		}
		badCode = strings.TrimSpace(rawText)
	}
	if badCode == "" {
		return nil
	}
	ctx := opts.ToolCallRepairCtx
	if ctx == nil {
		ctx = context.Background()
	}
	calls, ok := toolcall.RepairToolCallsWithLLM(ctx, badCode, opts.ToolCallRepair)
	if !ok || len(calls) == 0 {
		return nil
	}
	return toolcall.NormalizeParsedToolCallsForSchemas(calls, opts.ToolsRaw)
}

// mergeRepairedToolCalls appends repaired residual calls to the already-parsed
// calls, preserving parse order (existing calls first, repaired residual after)
// and dropping repaired calls that duplicate an existing one. This implements
// the phase1 §5.1 partial-success merge semantics: successful calls are kept,
// the failed residual is repaired, and the two sets are combined.
func mergeRepairedToolCalls(existing, repaired []toolcall.ParsedToolCall) []toolcall.ParsedToolCall {
	if len(repaired) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return repaired
	}
	merged := make([]toolcall.ParsedToolCall, 0, len(existing)+len(repaired))
	merged = append(merged, existing...)
	for _, rc := range repaired {
		dup := false
		for _, ec := range existing {
			if toolCallsEqual(ec, rc) {
				dup = true
				break
			}
		}
		if !dup {
			merged = append(merged, rc)
		}
	}
	return merged
}

func toolCallsEqual(a, b toolcall.ParsedToolCall) bool {
	if a.Name != b.Name {
		return false
	}
	return reflect.DeepEqual(a.Input, b.Input)
}

type StreamSnapshot struct {
	RawText               string
	VisibleText           string
	RawThinking           string
	VisibleThinking       string
	DetectionThinking     string
	ContentFilter         bool
	UpstreamError         string
	CitationLinks         map[int]string
	ResponseMessageID     int
	AlreadyEmittedCalls   bool
	AdditionalToolCalls   []toolcall.ParsedToolCall
	AlreadyEmittedToolRaw bool
}

func BuildTurnFromCollected(result sse.CollectResult, opts BuildOptions) Turn {
	thinking := shared.CleanVisibleOutput(result.Thinking, opts.StripReferenceMarkers)
	text := shared.CleanVisibleOutput(result.Text, opts.StripReferenceMarkers)
	if opts.SearchEnabled {
		text = shared.ReplaceCitationMarkersWithLinks(text, result.CitationLinks)
	}

	parsed := shared.DetectAssistantToolCalls(result.Text, text, result.Thinking, result.ToolDetectionThinking, opts.ToolNames)
	calls := toolcall.NormalizeParsedToolCallsForSchemas(parsed.Calls, opts.ToolsRaw)
	if repaired := maybeRepairToolCalls(parsed, result.Text, opts); len(repaired) > 0 {
		calls = mergeRepairedToolCalls(calls, repaired)
	}
	parsed.Calls = calls

	stopReason := StopReasonStop
	if result.ContentFilter {
		stopReason = StopReasonContentFilter
	}
	if len(calls) > 0 {
		stopReason = StopReasonToolCalls
	}

	turn := Turn{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		RawText:           result.Text,
		RawThinking:       result.Thinking,
		DetectionThinking: result.ToolDetectionThinking,
		Text:              text,
		Thinking:          thinking,
		ToolCalls:         calls,
		ParsedToolCalls:   parsed,
		CitationLinks:     result.CitationLinks,
		ContentFilter:     result.ContentFilter,
		UpstreamError:     result.UpstreamError,
		ResponseMessageID: result.ResponseMessageID,
		StopReason:        stopReason,
	}
	turn.Usage = BuildUsage(opts.Model, opts.Prompt, thinking, text, opts.RefFileTokens)
	turn.Error = ValidateTurn(turn, opts.ToolChoice)
	if turn.Error != nil {
		turn.StopReason = StopReasonError
	}
	return turn
}

func BuildTurnFromStreamSnapshot(snapshot StreamSnapshot, opts BuildOptions) Turn {
	thinking := shared.CleanVisibleOutput(snapshot.VisibleThinking, opts.StripReferenceMarkers)
	text := shared.CleanVisibleOutput(snapshot.VisibleText, opts.StripReferenceMarkers)
	if opts.SearchEnabled {
		text = shared.ReplaceCitationMarkersWithLinks(text, snapshot.CitationLinks)
	}

	parsed := shared.DetectAssistantToolCalls(snapshot.RawText, text, snapshot.RawThinking, snapshot.DetectionThinking, opts.ToolNames)
	calls := parsed.Calls
	if len(calls) == 0 && len(snapshot.AdditionalToolCalls) > 0 {
		calls = snapshot.AdditionalToolCalls
	}
	calls = toolcall.NormalizeParsedToolCallsForSchemas(calls, opts.ToolsRaw)
	// M4/M3: run the finalize LLM repair before delivering the final frame,
	// unless structured tool calls were already streamed to the client (we
	// cannot retract emitted tool_calls). When only bad text was streamed, the
	// finalize replaces it with the repaired tool_calls (phase1 §5.3 / phase3
	// §5.1): with tool_calls present, the caller emits no visible text. Partial
	// success merges repaired residual calls into the parsed ones (phase1 §5.1).
	if !snapshot.AlreadyEmittedCalls && !snapshot.AlreadyEmittedToolRaw {
		if repaired := maybeRepairToolCalls(parsed, snapshot.RawText, opts); len(repaired) > 0 {
			calls = mergeRepairedToolCalls(calls, repaired)
		}
	}
	parsed.Calls = calls

	stopReason := StopReasonStop
	if snapshot.ContentFilter {
		stopReason = StopReasonContentFilter
	}
	if len(calls) > 0 || snapshot.AlreadyEmittedCalls || snapshot.AlreadyEmittedToolRaw {
		stopReason = StopReasonToolCalls
	}

	turn := Turn{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		RawText:           snapshot.RawText,
		RawThinking:       snapshot.RawThinking,
		DetectionThinking: snapshot.DetectionThinking,
		Text:              text,
		Thinking:          thinking,
		ToolCalls:         calls,
		ParsedToolCalls:   parsed,
		CitationLinks:     snapshot.CitationLinks,
		ContentFilter:     snapshot.ContentFilter,
		UpstreamError:     snapshot.UpstreamError,
		ResponseMessageID: snapshot.ResponseMessageID,
		StopReason:        stopReason,
	}
	turn.Usage = BuildUsage(opts.Model, opts.Prompt, thinking, text, opts.RefFileTokens)
	if !snapshot.AlreadyEmittedCalls && !snapshot.AlreadyEmittedToolRaw {
		turn.Error = ValidateTurn(turn, opts.ToolChoice)
	}
	if turn.Error != nil && len(calls) == 0 {
		turn.StopReason = StopReasonError
	}
	return turn
}

func BuildUsage(model, prompt, thinking, text string, refFileTokens int) Usage {
	inputTokens := util.CountPromptTokens(prompt, model) + refFileTokens
	reasoningTokens := util.CountOutputTokens(thinking, model)
	outputTokens := reasoningTokens + util.CountOutputTokens(text, model)
	return Usage{
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		ReasoningTokens: reasoningTokens,
		TotalTokens:     inputTokens + outputTokens,
	}
}

func ValidateTurn(turn Turn, policy promptcompat.ToolChoicePolicy) *OutputError {
	if policy.IsRequired() && len(turn.ToolCalls) == 0 {
		return &OutputError{
			Status:  http.StatusUnprocessableEntity,
			Message: "tool_choice requires at least one valid tool call.",
			Code:    "tool_choice_violation",
		}
	}
	if len(turn.ToolCalls) > 0 {
		return nil
	}
	if strings.TrimSpace(turn.Text) != "" {
		return nil
	}
	if strings.TrimSpace(turn.UpstreamError) != "" {
		return &OutputError{
			Status:  http.StatusBadRequest,
			Message: turn.UpstreamError,
			Code:    "upstream_error",
		}
	}
	status, message, code := UpstreamEmptyOutputDetail(turn.ContentFilter, turn.Text, turn.Thinking)
	return &OutputError{Status: status, Message: message, Code: code}
}

func UpstreamEmptyOutputDetail(contentFilter bool, text, thinking string) (int, string, string) {
	_ = text
	if contentFilter {
		return http.StatusBadRequest, "Upstream content filtered the response and returned no output.", "content_filter"
	}
	if strings.TrimSpace(thinking) != "" {
		return http.StatusTooManyRequests, "Upstream account hit a rate limit and returned reasoning without visible output.", "upstream_empty_output"
	}
	return http.StatusServiceUnavailable, "Upstream service is unavailable and returned no output.", "upstream_unavailable"
}

// ShouldRetryEmptyOutput returns true when the turn produced no visible text
// and has no tool calls or content filter. This includes thinking-only responses,
// where the model returned reasoning but no answer — a retry may yield text.
func ShouldRetryEmptyOutput(turn Turn, attempts, maxAttempts int) bool {
	return attempts < maxAttempts &&
		!turn.ContentFilter &&
		len(turn.ToolCalls) == 0 &&
		strings.TrimSpace(turn.UpstreamError) == "" &&
		strings.TrimSpace(turn.Text) == ""
}

func FinalizeTurn(turn Turn, opts FinalizeOptions) FinalOutcome {
	hasToolCalls := len(turn.ToolCalls) > 0 || opts.AlreadyEmittedToolCalls
	hasVisibleText := strings.TrimSpace(turn.Text) != ""
	hasVisibleThinking := strings.TrimSpace(turn.Thinking) != ""
	err := turn.Error
	if hasToolCalls {
		err = nil
	}
	finishReason := FinishReason(turn)
	if hasToolCalls {
		finishReason = "tool_calls"
	}
	return FinalOutcome{
		FinishReason:     finishReason,
		Error:            err,
		Usage:            turn.Usage,
		HasToolCalls:     hasToolCalls,
		HasVisibleText:   hasVisibleText,
		HasVisibleOutput: hasVisibleText || hasVisibleThinking || hasToolCalls,
		ShouldFail:       err != nil,
	}
}

func OpenAIChatUsage(turn Turn) map[string]any {
	return map[string]any{
		"prompt_tokens":     turn.Usage.InputTokens,
		"completion_tokens": turn.Usage.OutputTokens,
		"total_tokens":      turn.Usage.TotalTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": turn.Usage.ReasoningTokens,
		},
	}
}

func OpenAIResponsesUsage(turn Turn) map[string]any {
	return map[string]any{
		"input_tokens":  turn.Usage.InputTokens,
		"output_tokens": turn.Usage.OutputTokens,
		"total_tokens":  turn.Usage.TotalTokens,
	}
}

func FinishReason(turn Turn) string {
	switch turn.StopReason {
	case StopReasonToolCalls:
		return "tool_calls"
	case StopReasonContentFilter:
		return "content_filter"
	default:
		return "stop"
	}
}
