package completionruntime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/history"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
)

type DeepSeekCaller interface {
	CreateSession(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	GetPow(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error)
	UploadFile(ctx context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int) (*dsclient.UploadFileResult, error)
	CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error)
	StopStream(ctx context.Context, a *auth.RequestAuth, sessionID string, messageID int) error
	FireCompletionAndStop(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string) (int, error)
}

type Options struct {
	StripReferenceMarkers bool
	MaxAttempts           int
	RetryEnabled          bool
	RetryMaxAttempts      int
	CurrentInputFile      history.CurrentInputConfigReader
	ExpertPromptSegment   ExpertPromptSegmentConfigReader
	// ToolCallRepairEnabled turns on the phase-3 finalize-only LLM tool-call
	// repair pass. When enabled, the runtime builds a repair invoker bound to
	// the request's account (expert mode, thinking off, new session, 10s) and
	// hands it to the finalize turn builder so residual bad tool-call code can
	// be repaired. It is never wired into the streaming sieve.
	ToolCallRepairEnabled bool
	// toolCallRepair / toolCallRepairCtx are populated internally by the
	// Execute* entrypoints (where ds and auth are available) and threaded into
	// the finalize turn build options.
	toolCallRepair    toolcall.ToolCallRepairInvoker
	toolCallRepairCtx context.Context
}

type NonStreamResult struct {
	SessionID string
	Payload   map[string]any
	Turn      assistantturn.Turn
	Attempts  int
}

type StartResult struct {
	SessionID string
	Payload   map[string]any
	Pow       string
	Response  *http.Response
	Request   promptcompat.StandardRequest
}

func StartCompletion(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (StartResult, *assistantturn.OutputError) {
	if segments := shouldSegmentExpertPrompt(stdReq, opts); segments != nil {
		return StartCompletionWithSegments(ctx, ds, a, stdReq, opts, segments)
	}
	return startCompletionOnce(ctx, ds, a, stdReq, opts)
}

// PrepareCompletionPayload builds the session-aware completion payload for a
// request without calling the completion endpoint itself. Oversized expert
// prompts are split into segments first (all but the last are sent via
// FireCompletionAndStop), so the caller can stream the returned final payload
// directly. This is the payload-side equivalent of StartCompletion for
// surfaces that stream upstream responses themselves (e.g. the Vercel Node
// stream layer). On success the caller must stream the returned payload with
// the returned PoW on the same account and session.
func PrepareCompletionPayload(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options, maxAttempts int) (sessionID, pow string, payload map[string]any, outErr *assistantturn.OutputError) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var prepErr *assistantturn.OutputError
	stdReq, prepErr = prepareCurrentInputFile(ctx, ds, a, stdReq, opts)
	if prepErr != nil {
		return "", "", nil, prepErr
	}
	var err error
	sessionID, err = ds.CreateSession(ctx, a, maxAttempts)
	if err != nil {
		return "", "", nil, authOutputError(a)
	}
	if segments := shouldSegmentExpertPrompt(stdReq, opts); len(segments) > 1 {
		finalPow, finalPayload, segErr := fireSegmentPayloads(ctx, ds, a, stdReq, sessionID, segments, maxAttempts)
		if segErr != nil {
			return sessionID, "", nil, segErr
		}
		return sessionID, finalPow, finalPayload, nil
	}
	pow, err = ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return sessionID, "", nil, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	return sessionID, pow, stdReq.CompletionPayload(sessionID), nil
}

func startCompletionOnce(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (StartResult, *assistantturn.OutputError) {
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var prepErr *assistantturn.OutputError
	stdReq, prepErr = prepareCurrentInputFile(ctx, ds, a, stdReq, opts)
	if prepErr != nil {
		return StartResult{Request: stdReq}, prepErr
	}
	sessionID, err := ds.CreateSession(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{Request: stdReq}, authOutputError(a)
	}
	pow, err := ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{SessionID: sessionID, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := ds.CallCompletion(ctx, a, payload, pow, maxAttempts)
	if err != nil {
		if dsclient.IsMutedError(err) {
			return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusForbidden, Message: "Account is muted by upstream.", Code: "account_muted"}
		}
		return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
	}
	return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Response: resp, Request: stdReq}, nil
}

func prepareCurrentInputFile(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (promptcompat.StandardRequest, *assistantturn.OutputError) {
	if opts.CurrentInputFile == nil || stdReq.CurrentInputFileApplied {
		return stdReq, nil
	}
	out, err := (history.Service{Store: opts.CurrentInputFile, DS: ds}).ApplyCurrentInputFile(ctx, a, stdReq)
	if err != nil {
		status, message := history.MapError(err)
		return out, &assistantturn.OutputError{Status: status, Message: message, Code: "error"}
	}
	return out, nil
}

func ExecuteNonStreamWithRetry(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (NonStreamResult, *assistantturn.OutputError) {
	opts = withToolCallRepair(ctx, ds, a, opts)
	start, startErr := StartCompletion(ctx, ds, a, stdReq, opts)
	if startErr != nil {
		return NonStreamResult{SessionID: start.SessionID, Payload: start.Payload}, startErr
	}
	return ExecuteNonStreamStartedWithRetry(ctx, ds, a, start, opts)
}

// withToolCallRepair lazily builds the phase-3 repair invoker (bound to the
// request account) and scopes it to ctx, but only when repair is enabled and
// not already configured. The repair pass is finalize-only; the streaming sieve
// must never receive an invoker (plan §5.1).
func withToolCallRepair(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, opts Options) Options {
	if !opts.ToolCallRepairEnabled || opts.toolCallRepair != nil {
		return opts
	}
	opts.toolCallRepair = NewToolCallRepairInvoker(ds, a)
	opts.toolCallRepairCtx = ctx
	return opts
}

func ExecuteNonStreamStartedWithRetry(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, start StartResult, opts Options) (NonStreamResult, *assistantturn.OutputError) {
	opts = withToolCallRepair(ctx, ds, a, opts)
	stdReq := start.Request
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	sessionID := start.SessionID
	payload := start.Payload
	pow := start.Pow

	attempts := 0
	accountSwitchAttempted := false
	currentResp := start.Response
	usagePrompt := stdReq.PromptTokenText
	accumulatedThinking := ""
	accumulatedRawThinking := ""
	accumulatedToolDetectionThinking := ""
	for {
		turn, outErr := collectAttempt(currentResp, stdReq, usagePrompt, opts)
		if outErr != nil {
			if canRetryOnAlternateAccount(ctx, a, outErr, opts.RetryEnabled, &accountSwitchAttempted) {
				switched, switchErr := startStandardCompletionOnAlternateAccount(ctx, ds, a, stdReq, opts, maxAttempts)
				if switchErr != nil {
					return NonStreamResult{SessionID: sessionID, Payload: payload, Attempts: attempts}, switchErr
				}
				if switched.Response != nil {
					config.Logger.Info("[completion_runtime_account_switch_retry] retrying after 429", "surface", stdReq.Surface, "stream", false, "account", a.AccountID)
					sessionID = switched.SessionID
					payload = switched.Payload
					pow = switched.Pow
					currentResp = switched.Response
					usagePrompt = stdReq.PromptTokenText
					accumulatedThinking = ""
					accumulatedRawThinking = ""
					accumulatedToolDetectionThinking = ""
					continue
				}
			}
			return NonStreamResult{SessionID: sessionID, Payload: payload, Attempts: attempts}, outErr
		}
		accumulatedThinking += sse.TrimContinuationOverlap(accumulatedThinking, turn.Thinking)
		accumulatedRawThinking += sse.TrimContinuationOverlap(accumulatedRawThinking, turn.RawThinking)
		accumulatedToolDetectionThinking += sse.TrimContinuationOverlap(accumulatedToolDetectionThinking, turn.DetectionThinking)
		turn.Thinking = accumulatedThinking
		turn.RawThinking = accumulatedRawThinking
		turn.DetectionThinking = accumulatedToolDetectionThinking
		turn = assistantturn.BuildTurnFromCollected(sse.CollectResult{
			Text:                  turn.RawText,
			Thinking:              turn.RawThinking,
			ToolDetectionThinking: turn.DetectionThinking,
			ContentFilter:         turn.ContentFilter,
			CitationLinks:         turn.CitationLinks,
			ResponseMessageID:     turn.ResponseMessageID,
		}, buildOptions(stdReq, usagePrompt, opts))

		retryMax := opts.RetryMaxAttempts
		if retryMax <= 0 {
			retryMax = shared.EmptyOutputRetryMaxAttempts()
		}
		if !opts.RetryEnabled || !assistantturn.ShouldRetryEmptyOutput(turn, attempts, retryMax) {
			if canRetryOnAlternateAccount(ctx, a, turn.Error, opts.RetryEnabled, &accountSwitchAttempted) {
				switched, switchErr := startStandardCompletionOnAlternateAccount(ctx, ds, a, stdReq, opts, maxAttempts)
				if switchErr != nil {
					return NonStreamResult{SessionID: sessionID, Payload: payload, Turn: turn, Attempts: attempts}, switchErr
				}
				if switched.Response != nil {
					config.Logger.Info("[completion_runtime_account_switch_retry] retrying after 429", "surface", stdReq.Surface, "stream", false, "account", a.AccountID)
					sessionID = switched.SessionID
					payload = switched.Payload
					pow = switched.Pow
					currentResp = switched.Response
					usagePrompt = stdReq.PromptTokenText
					accumulatedThinking = ""
					accumulatedRawThinking = ""
					accumulatedToolDetectionThinking = ""
					continue
				}
			}
			return NonStreamResult{SessionID: sessionID, Payload: payload, Turn: turn, Attempts: attempts}, turn.Error
		}

		attempts++
		parentMessageID := retryParentMessageID(turn.ResponseMessageID, payload)
		config.Logger.Info("[completion_runtime_empty_retry] attempting synthetic retry", "surface", stdReq.Surface, "stream", false, "retry_attempt", attempts, "parent_message_id", parentMessageID)
		retryPow, powErr := ds.GetPow(ctx, a, maxAttempts)
		if powErr != nil {
			config.Logger.Warn("[completion_runtime_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", stdReq.Surface, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		retryPayload := shared.ClonePayloadForEmptyOutputRetry(payload, parentMessageID)
		nextResp, err := ds.CallCompletion(ctx, a, retryPayload, retryPow, maxAttempts)
		if err != nil {
			return NonStreamResult{SessionID: sessionID, Payload: payload, Turn: turn, Attempts: attempts}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
		}
		payload = retryPayload
		usagePrompt = shared.UsagePromptWithEmptyOutputRetry(usagePrompt, attempts)
		currentResp = nextResp
	}
}

func retryParentMessageID(observed int, payload map[string]any) int {
	if observed > 0 {
		return observed
	}
	return payloadParentMessageID(payload)
}

func payloadParentMessageID(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	switch v := payload["parent_message_id"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func canRetryOnAlternateAccount(ctx context.Context, a *auth.RequestAuth, outErr *assistantturn.OutputError, retryEnabled bool, attempted *bool) bool {
	if outErr == nil || !retryEnabled || a == nil || !a.UseConfigToken {
		return false
	}
	if isAccountMuted(outErr) {
		return a.SwitchAccount(ctx)
	}
	if isUpstreamUnavailable(outErr) {
		return a.SwitchAccount(ctx)
	}
	if outErr.Status != http.StatusTooManyRequests {
		return false
	}
	if attempted == nil || *attempted {
		return false
	}
	*attempted = true
	return a.SwitchAccount(ctx)
}

func isUpstreamUnavailable(outErr *assistantturn.OutputError) bool {
	return outErr != nil && outErr.Code == "upstream_unavailable"
}

func isAccountMuted(outErr *assistantturn.OutputError) bool {
	return outErr != nil && outErr.Code == "account_muted"
}

func startStandardCompletionOnAlternateAccount(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options, maxAttempts int) (StartResult, *assistantturn.OutputError) {
	if segments := shouldSegmentExpertPrompt(stdReq, opts); segments != nil {
		return StartCompletionWithSegments(ctx, ds, a, stdReq, opts, segments)
	}
	var prepErr *assistantturn.OutputError
	stdReq, prepErr = reuploadCurrentInputFileForAccount(ctx, ds, a, stdReq, opts)
	if prepErr != nil {
		return StartResult{Request: stdReq}, prepErr
	}
	sessionID, err := ds.CreateSession(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{}, authOutputError(a)
	}
	pow, err := ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{SessionID: sessionID}, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := ds.CallCompletion(ctx, a, payload, pow, maxAttempts)
	if err != nil {
		if dsclient.IsMutedError(err) {
			return StartResult{SessionID: sessionID, Payload: payload, Pow: pow}, &assistantturn.OutputError{Status: http.StatusForbidden, Message: "Account is muted by upstream.", Code: "account_muted"}
		}
		return StartResult{SessionID: sessionID, Payload: payload, Pow: pow}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
	}
	return StartResult{SessionID: sessionID, Payload: payload, Pow: pow, Response: resp, Request: stdReq}, nil
}

func reuploadCurrentInputFileForAccount(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options) (promptcompat.StandardRequest, *assistantturn.OutputError) {
	if opts.CurrentInputFile == nil || !stdReq.CurrentInputFileApplied {
		return stdReq, nil
	}
	out, err := (history.Service{Store: opts.CurrentInputFile, DS: ds}).ReuploadAppliedCurrentInputFile(ctx, a, stdReq)
	if err != nil {
		status, message := history.MapError(err)
		return out, &assistantturn.OutputError{Status: status, Message: message, Code: "error"}
	}
	return out, nil
}

func collectAttempt(resp *http.Response, stdReq promptcompat.StandardRequest, usagePrompt string, opts Options) (assistantturn.Turn, *assistantturn.OutputError) {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			config.Logger.Warn("[completion_runtime] response body close failed", "surface", stdReq.Surface, "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		if captchaBody := tryDetectCaptchaFromBody(body); captchaBody != "" {
			config.Logger.Warn("[completion_runtime] captcha challenge detected, mapping to 429 for account switch", "surface", stdReq.Surface, "detail", captchaBody)
			return assistantturn.Turn{}, &assistantturn.OutputError{Status: http.StatusTooManyRequests, Message: "Captcha challenge detected, account may be rate-limited.", Code: "captcha_required"}
		}
		// 限流提示（如「消息发送过于频繁，请稍后重试」）：映射 429 触发账号
		// 切换重试，同一条消息换号发送通常能成功。
		if sse.IsRateLimitMessage(message) {
			config.Logger.Warn("[completion_runtime] upstream rate limited, mapping to 429 for account switch", "surface", stdReq.Surface, "status", resp.StatusCode)
			return assistantturn.Turn{}, &assistantturn.OutputError{Status: http.StatusTooManyRequests, Message: message, Code: "upstream_rate_limited"}
		}
		return assistantturn.Turn{}, &assistantturn.OutputError{Status: resp.StatusCode, Message: message, Code: "error"}
	}
	result := sse.CollectStream(resp, stdReq.Thinking, false)
	// collectAttempt only accumulates raw text/thinking across retries; the
	// terminal turn is rebuilt once from turn.RawText by the retry loop (see
	// ExecuteNonStreamStartedWithRetry). Running LLM repair here would repair
	// the same output twice (H1) — one repair session/completion is created in
	// this build and another in the terminal build, doubling cost, latency and
	// leaked sessions. Build without the repair invoker so repair runs only in
	// the single terminal build.
	return assistantturn.BuildTurnFromCollected(result, buildOptionsWithoutRepair(stdReq, usagePrompt, opts)), nil
}

func buildOptions(stdReq promptcompat.StandardRequest, prompt string, opts Options) assistantturn.BuildOptions {
	built := buildOptionsWithoutRepair(stdReq, prompt, opts)
	built.ToolCallRepair = opts.toolCallRepair
	built.ToolCallRepairCtx = opts.toolCallRepairCtx
	return built
}

// buildOptionsWithoutRepair mirrors buildOptions but never attaches the LLM
// repair invoker. Intermediate builds (per-attempt collection) must use this so
// a single upstream output triggers at most one LLM repair pass.
func buildOptionsWithoutRepair(stdReq promptcompat.StandardRequest, prompt string, opts Options) assistantturn.BuildOptions {
	return assistantturn.BuildOptions{
		Model:                 stdReq.ResponseModel,
		Prompt:                prompt,
		RefFileTokens:         stdReq.RefFileTokens,
		SearchEnabled:         stdReq.Search,
		StripReferenceMarkers: opts.StripReferenceMarkers,
		ToolNames:             stdReq.ToolNames,
		ToolsRaw:              stdReq.ToolsRaw,
		ToolChoice:            stdReq.ToolChoice,
	}
}

func authOutputError(a *auth.RequestAuth) *assistantturn.OutputError {
	if a != nil && a.UseConfigToken {
		return &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Account token is invalid. Please re-login the account in admin.", Code: "error"}
	}
	return &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: DirectTokenErrorMessage, Code: "error"}
}

const DirectTokenErrorMessage = "Invalid token. If this should be a DS2API key, add it to config.keys first."

func IsDirectTokenAuthError(outErr *assistantturn.OutputError) bool {
	return outErr != nil && outErr.Status == http.StatusUnauthorized && outErr.Message == DirectTokenErrorMessage
}

func Errorf(status int, format string, args ...any) *assistantturn.OutputError {
	return &assistantturn.OutputError{Status: status, Message: fmt.Sprintf(format, args...), Code: "error"}
}
