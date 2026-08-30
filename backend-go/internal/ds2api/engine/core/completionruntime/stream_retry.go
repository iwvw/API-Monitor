package completionruntime

import (
	"context"
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
)

type StreamRetryOptions struct {
	Surface             string
	Stream              bool
	RetryEnabled        bool
	RetryMaxAttempts    int
	MaxAttempts         int
	UsagePrompt         string
	Request             promptcompat.StandardRequest
	CurrentInputFile    history.CurrentInputConfigReader
	ExpertPromptSegment ExpertPromptSegmentConfigReader
}

// ConsumeAttemptResult describes what one stream-consuming attempt ended with.
type ConsumeAttemptResult struct {
	// Terminal indicates the attempt wrote its terminal frame(s) (finish
	// chunk, error chunk or [DONE]); the retry loop must stop.
	Terminal bool
	// Retryable indicates the attempt produced no usable output and the loop
	// may retry with a synthetic empty-output payload.
	Retryable bool
	// ResumeContinue indicates the upstream stream was interrupted after
	// producing partial output; the loop should resume the same message via
	// the continue endpoint instead of starting an empty-output retry.
	ResumeContinue bool
}

type StreamRetryHooks struct {
	ConsumeAttempt   func(resp *http.Response, allowDeferEmpty bool) ConsumeAttemptResult
	Finalize         func(attempts int)
	ParentMessageID  func() int
	OnRetry          func(attempts int)
	OnRetryPrompt    func(prompt string)
	OnRetryFailure   func(status int, message, code string)
	OnAccountSwitch  func(sessionID string)
	OnTerminal       func(attempts int)
	EmptyOutputError func() *assistantturn.OutputError
}

// continueResumer is implemented by DeepSeek callers that can resume a
// partially generated message through the upstream continue endpoint.
type continueResumer interface {
	ContinueCompletion(ctx context.Context, a *auth.RequestAuth, sessionID string, messageID int, powResp string) (*http.Response, error)
}

func ExecuteStreamWithRetry(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, initialResp *http.Response, payload map[string]any, pow string, opts StreamRetryOptions, hooks StreamRetryHooks) {
	if hooks.ConsumeAttempt == nil {
		return
	}
	surface := strings.TrimSpace(opts.Surface)
	if surface == "" {
		surface = "completion"
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	retryMax := opts.RetryMaxAttempts
	if retryMax <= 0 {
		retryMax = shared.EmptyOutputRetryMaxAttempts()
	}

	attempts := 0
	accountSwitchAttempted := false
	currentResp := initialResp
	currentPayload := clonePayload(payload)
	for {
		allowAccountSwitch := opts.RetryEnabled && attempts >= retryMax && a != nil && a.UseConfigToken
		result := hooks.ConsumeAttempt(currentResp, opts.RetryEnabled && (attempts < retryMax || allowAccountSwitch))
		if result.Terminal {
			if hooks.OnTerminal != nil {
				hooks.OnTerminal(attempts)
			}
			return
		}
		if !result.Retryable || !opts.RetryEnabled {
			if hooks.Finalize != nil {
				hooks.Finalize(attempts)
			}
			return
		}

		if attempts >= retryMax {
			if result.ResumeContinue {
				// Continue-resume budget exhausted: surface the interruption
				// instead of silently delivering a truncated response.
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(http.StatusBadGateway, "Upstream stream interrupted and continue-resume attempts are exhausted.", "upstream_interrupted")
				}
				return
			}
			emptyErr := buildEmptyOutputError(hooks)
			if canRetryOnAlternateAccount(ctx, a, emptyErr, opts.RetryEnabled, &accountSwitchAttempted) {
				switched, switchErr := startPayloadCompletionOnAlternateAccount(ctx, ds, a, payload, opts, maxAttempts)
				if switchErr != nil {
					if hooks.OnRetryFailure != nil {
						hooks.OnRetryFailure(switchErr.Status, switchErr.Message, switchErr.Code)
					}
					return
				}
				if switched.Response != nil {
					config.Logger.Info("[completion_runtime_account_switch_retry] retrying after empty output", "surface", surface, "stream", opts.Stream, "account", a.AccountID, "error_code", emptyErr.Code)
					currentResp = switched.Response
					currentPayload = switched.Payload
					pow = switched.Pow
					if hooks.OnAccountSwitch != nil {
						hooks.OnAccountSwitch(switched.SessionID)
					}
					if hooks.OnRetryPrompt != nil {
						hooks.OnRetryPrompt(opts.UsagePrompt)
					}
					continue
				}
			}
			if hooks.Finalize != nil {
				hooks.Finalize(attempts)
			}
			return
		}

		attempts++
		parentMessageID := 0
		if hooks.ParentMessageID != nil {
			parentMessageID = hooks.ParentMessageID()
		}
		parentMessageID = retryParentMessageID(parentMessageID, currentPayload)

		if result.ResumeContinue {
			// Interrupted with partial output: resume the same upstream message
			// via the continue endpoint instead of opening a synthetic retry.
			config.Logger.Info("[completion_runtime_continue_resume] resuming interrupted stream", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "parent_message_id", parentMessageID)
			retryPow, powErr := ds.GetPow(ctx, a, maxAttempts)
			if powErr != nil {
				config.Logger.Warn("[completion_runtime_continue_resume] retry PoW fetch failed, falling back to original PoW", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", powErr)
				retryPow = pow
			}
			sessionID, _ := currentPayload["chat_session_id"].(string)
			resumer, ok := ds.(continueResumer)
			if !ok {
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(http.StatusInternalServerError, "Streaming resume via continue endpoint is not supported by this caller.", "error")
				}
				return
			}
			nextResp, err := resumer.ContinueCompletion(ctx, a, sessionID, parentMessageID, retryPow)
			if err != nil {
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(http.StatusBadGateway, "Failed to resume interrupted stream: "+err.Error(), "upstream_interrupted")
				}
				config.Logger.Warn("[completion_runtime_continue_resume] continue request failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", err)
				return
			}
			if nextResp.StatusCode != http.StatusOK {
				body, readErr := io.ReadAll(io.LimitReader(nextResp.Body, 2<<20))
				if readErr != nil {
					config.Logger.Warn("[completion_runtime_continue_resume] continue error body read failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", readErr)
				}
				closeRetryBody(surface, nextResp.Body)
				msg := strings.TrimSpace(string(body))
				if msg == "" {
					msg = http.StatusText(nextResp.StatusCode)
				}
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(nextResp.StatusCode, msg, "error")
				}
				return
			}
			if hooks.OnRetry != nil {
				hooks.OnRetry(attempts)
			}
			currentResp = nextResp
			continue
		}

		config.Logger.Info("[completion_runtime_empty_retry] attempting synthetic retry", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "parent_message_id", parentMessageID)
		retryPow, powErr := ds.GetPow(ctx, a, maxAttempts)
		if powErr != nil {
			config.Logger.Warn("[completion_runtime_empty_retry] retry PoW fetch failed, falling back to original PoW", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", powErr)
			retryPow = pow
		}
		retryPayload := shared.ClonePayloadForEmptyOutputRetry(currentPayload, parentMessageID)
		nextResp, err := ds.CallCompletion(ctx, a, retryPayload, retryPow, maxAttempts)
		if err != nil {
			if dsclient.IsMutedError(err) {
				if canRetryOnAlternateAccount(ctx, a, &assistantturn.OutputError{Status: http.StatusForbidden, Code: "account_muted"}, opts.RetryEnabled, &accountSwitchAttempted) {
					switched, switchErr := startPayloadCompletionOnAlternateAccount(ctx, ds, a, payload, opts, maxAttempts)
					if switchErr != nil {
						if hooks.OnRetryFailure != nil {
							hooks.OnRetryFailure(switchErr.Status, switchErr.Message, switchErr.Code)
						}
						return
					}
					if switched.Response != nil {
						config.Logger.Info("[completion_runtime_account_switch_retry] retrying after account muted", "surface", surface, "stream", opts.Stream, "account", a.AccountID)
						currentResp = switched.Response
						currentPayload = switched.Payload
						pow = switched.Pow
						if hooks.OnAccountSwitch != nil {
							hooks.OnAccountSwitch(switched.SessionID)
						}
						if hooks.OnRetryPrompt != nil {
							hooks.OnRetryPrompt(opts.UsagePrompt)
						}
						continue
					}
				}
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(http.StatusForbidden, "Account is muted by upstream.", "account_muted")
				}
				return
			}
			if hooks.OnRetryFailure != nil {
				hooks.OnRetryFailure(http.StatusInternalServerError, "Failed to get completion.", "error")
			}
			config.Logger.Warn("[completion_runtime_empty_retry] retry request failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", err)
			return
		}
		if nextResp.StatusCode != http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(nextResp.Body, 2<<20))
			if readErr != nil {
				config.Logger.Warn("[completion_runtime_empty_retry] retry error body read failed", "surface", surface, "stream", opts.Stream, "retry_attempt", attempts, "error", readErr)
			}
			closeRetryBody(surface, nextResp.Body)
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = http.StatusText(nextResp.StatusCode)
			}
			if captchaBody := tryDetectCaptchaFromBody(body); captchaBody != "" {
				config.Logger.Warn("[completion_runtime_empty_retry] captcha challenge detected, mapping to 429 for account switch", "surface", surface, "stream", opts.Stream, "account", a.AccountID, "detail", captchaBody)
				if canRetryOnAlternateAccount(ctx, a, &assistantturn.OutputError{Status: http.StatusTooManyRequests}, opts.RetryEnabled, &accountSwitchAttempted) {
					switched, switchErr := startPayloadCompletionOnAlternateAccount(ctx, ds, a, payload, opts, maxAttempts)
					if switchErr != nil {
						if hooks.OnRetryFailure != nil {
							hooks.OnRetryFailure(switchErr.Status, switchErr.Message, switchErr.Code)
						}
						return
					}
					if switched.Response != nil {
						config.Logger.Info("[completion_runtime_account_switch_retry] retrying after captcha", "surface", surface, "stream", opts.Stream, "account", a.AccountID)
						currentResp = switched.Response
						currentPayload = clonePayload(payload)
						pow = switched.Pow
						if hooks.OnAccountSwitch != nil {
							hooks.OnAccountSwitch(switched.SessionID)
						}
						if hooks.OnRetryPrompt != nil {
							hooks.OnRetryPrompt(opts.UsagePrompt)
						}
						continue
					}
				}
				if hooks.OnRetryFailure != nil {
					hooks.OnRetryFailure(http.StatusTooManyRequests, "Captcha challenge detected, account may be rate-limited.", "captcha_required")
				}
				return
			}
			if hooks.OnRetryFailure != nil {
				hooks.OnRetryFailure(nextResp.StatusCode, msg, "error")
			}
			return
		}
		if hooks.OnRetry != nil {
			hooks.OnRetry(attempts)
		}
		if hooks.OnRetryPrompt != nil {
			hooks.OnRetryPrompt(shared.UsagePromptWithEmptyOutputRetry(opts.UsagePrompt, attempts))
		}
		currentResp = nextResp
		currentPayload = retryPayload
	}
}

func startPayloadCompletionOnAlternateAccount(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, payload map[string]any, opts StreamRetryOptions, maxAttempts int) (StartResult, *assistantturn.OutputError) {
	if segments := shouldSegmentExpertPrompt(opts.Request, Options{ExpertPromptSegment: opts.ExpertPromptSegment}); segments != nil {
		return StartCompletionWithSegments(ctx, ds, a, opts.Request, Options{
			MaxAttempts:         maxAttempts,
			RetryEnabled:        opts.RetryEnabled,
			RetryMaxAttempts:    opts.RetryMaxAttempts,
			CurrentInputFile:    opts.CurrentInputFile,
			ExpertPromptSegment: opts.ExpertPromptSegment,
		}, segments)
	}
	sessionID, err := ds.CreateSession(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{}, authOutputError(a)
	}
	pow, err := ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return StartResult{SessionID: sessionID}, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	nextPayload := clonePayload(payload)
	if opts.CurrentInputFile != nil && opts.Request.CurrentInputFileApplied {
		stdReq, prepErr := reuploadCurrentInputFileForAccount(ctx, ds, a, opts.Request, Options{CurrentInputFile: opts.CurrentInputFile})
		if prepErr != nil {
			return StartResult{SessionID: sessionID}, prepErr
		}
		nextPayload = stdReq.CompletionPayload(sessionID)
	}
	nextPayload["chat_session_id"] = sessionID
	delete(nextPayload, "parent_message_id")
	resp, err := ds.CallCompletion(ctx, a, nextPayload, pow, maxAttempts)
	if err != nil {
		if dsclient.IsMutedError(err) {
			return StartResult{SessionID: sessionID, Payload: nextPayload, Pow: pow}, &assistantturn.OutputError{Status: http.StatusForbidden, Message: "Account is muted by upstream.", Code: "account_muted"}
		}
		return StartResult{SessionID: sessionID, Payload: nextPayload, Pow: pow}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
	}
	return StartResult{SessionID: sessionID, Payload: nextPayload, Pow: pow, Response: resp}, nil
}

func buildEmptyOutputError(hooks StreamRetryHooks) *assistantturn.OutputError {
	if hooks.EmptyOutputError != nil {
		if err := hooks.EmptyOutputError(); err != nil {
			return err
		}
	}
	return &assistantturn.OutputError{Status: http.StatusTooManyRequests}
}

func clonePayload(payload map[string]any) map[string]any {
	clone := make(map[string]any, len(payload))
	for k, v := range payload {
		clone[k] = v
	}
	return clone
}

func closeRetryBody(surface string, body io.Closer) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		config.Logger.Warn("[completion_runtime_empty_retry] retry response body close failed", "surface", surface, "error", err)
	}
}
