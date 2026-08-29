package responses

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/completionruntime"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/responsehistory"
	streamengine "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/stream"
)

func (h *Handler) handleResponsesStreamWithRetry(w http.ResponseWriter, r *http.Request, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, owner, responseID string, stdReq promptcompat.StandardRequest, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string, historySession *responsehistory.Session) {
	streamRuntime, initialType, ok := h.prepareResponsesStreamRuntime(w, resp, owner, responseID, model, finalPrompt, refFileTokens, thinkingEnabled, searchEnabled, toolNames, toolsRaw, toolChoice, traceID, historySession)
	if !ok {
		return
	}
	// M4: wire the phase-3 finalize-only LLM repair into the stream finalize.
	streamRuntime.toolCallRepair = completionruntime.NewToolCallRepairInvoker(h.DS, a)
	streamRuntime.toolCallRepairCtx = r.Context()
	completionruntime.ExecuteStreamWithRetry(r.Context(), h.DS, a, resp, payload, pow, completionruntime.StreamRetryOptions{
		Surface:             "responses",
		Stream:              true,
		RetryEnabled:        emptyOutputRetryEnabled(),
		RetryMaxAttempts:    emptyOutputRetryMaxAttempts(),
		MaxAttempts:         3,
		UsagePrompt:         finalPrompt,
		Request:             stdReq,
		CurrentInputFile:    h.Store,
		ExpertPromptSegment: h.Store,
	}, completionruntime.StreamRetryHooks{
		ConsumeAttempt: func(currentResp *http.Response, allowDeferEmpty bool) completionruntime.ConsumeAttemptResult {
			return h.consumeResponsesStreamAttempt(r, currentResp, streamRuntime, initialType, thinkingEnabled, allowDeferEmpty)
		},
		Finalize: func(attempts int) {
			streamRuntime.finalize("stop", false)
			config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
		},
		ParentMessageID: func() int {
			return streamRuntime.responseMessageID
		},
		OnRetryPrompt: func(prompt string) {
			streamRuntime.finalPrompt = prompt
		},
		OnRetryFailure: func(status int, message, code string) {
			streamRuntime.failResponse(status, strings.TrimSpace(message), code)
		},
		OnTerminal: func(attempts int) {
			logResponsesStreamTerminal(streamRuntime, attempts)
		},
		EmptyOutputError: func() *assistantturn.OutputError {
			if streamRuntime.finalErrorCode == "" {
				return nil
			}
			return &assistantturn.OutputError{Status: streamRuntime.finalErrorStatus, Message: streamRuntime.finalErrorMessage, Code: streamRuntime.finalErrorCode}
		},
	})
}

func (h *Handler) prepareResponsesStreamRuntime(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string, historySession *responsehistory.Session) (*responsesStreamRuntime, string, bool) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_responses_stream] captcha challenge detected on initial response", "detail", detail)
		}
		if historySession != nil {
			historySession.Error(resp.StatusCode, strings.TrimSpace(string(body)), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return nil, "", false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamRuntime := newResponsesStreamRuntime(
		w, rc, canFlush, responseID, model, finalPrompt, thinkingEnabled, searchEnabled,
		stripReferenceMarkersEnabled(), toolNames, toolsRaw,
		// 与 responses_handler.handleResponsesStream 保持一致，无论客户端是否传 tools
		// 都启用 toolSieve 流式拦截：模型在提示词中被要求使用 <|EPSE|tool_calls> 格式
		// 输出工具调用，若客户端未传 tools（如“继续会话”请求）而 bufferToolContent 为
		// false，会导致 EPSE 原文作为正文透传，产生乱码。toolSieve 解析不依赖 toolNames。
		true,
		h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence(),
		toolChoice, traceID, func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		}, historySession,
	)
	streamRuntime.refFileTokens = refFileTokens
	streamRuntime.sendCreated()
	return streamRuntime, initialType, true
}

func (h *Handler) consumeResponsesStreamAttempt(r *http.Request, resp *http.Response, streamRuntime *responsesStreamRuntime, initialType string, thinkingEnabled bool, allowDeferEmpty bool) completionruntime.ConsumeAttemptResult {
	defer func() { _ = resp.Body.Close() }()
	// On a continue/resume (or retry) round the stream has already consumed
	// part of the message. Never blindly re-seed the segment cursor to
	// "thinking" in that case: if the message already entered the content
	// segment, restart the pump as "text" so the replayed envelope and the
	// pathless body blocks that follow keep classifying as content instead of
	// being swallowed into reasoning_content.
	attemptInitialType := initialType
	if streamRuntime.accumulator.RawText.Len() > 0 {
		attemptInitialType = "text"
	} else if streamRuntime.accumulator.RawThinking.Len() > 0 {
		attemptInitialType = "thinking"
	}
	finalReason := "stop"
	var scannerErr error
	var stopReason streamengine.StopReason
	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         attemptInitialType,
		KeepAliveInterval:   time.Duration(dsprotocol.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(dsprotocol.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: dsprotocol.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, err error) {
			stopReason = reason
			scannerErr = err
			if string(reason) == "content_filter" {
				finalReason = "content_filter"
			}
		},
		OnContextDone: func() {
			streamRuntime.markContextCancelled()
		},
	})
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}

	// An upstream stream that died mid-body or stalled past the idle /
	// no-content window after producing partial output must not be silently
	// finalized as a normal "stop": schedule a continue-resume so the same
	// message is completed, or surface an explicit interruption error.
	interrupted := scannerErr != nil || stopReason == streamengine.StopReasonIdleTimeout || stopReason == streamengine.StopReasonNoContentTimeout
	if interrupted && streamRuntime.responseMessageID > 0 && streamRuntime.accumulator.HasPartialOutput() {
		config.Logger.Warn("[openai_responses_stream] upstream stream interrupted; scheduling continue-resume",
			"surface", "responses", "stream", true, "reason", stopReason, "error", fmt.Sprint(scannerErr), "response_message_id", streamRuntime.responseMessageID)
		return completionruntime.ConsumeAttemptResult{Retryable: true, ResumeContinue: true}
	}
	if interrupted && streamRuntime.accumulator.HasPartialOutput() {
		// Partial output but no resumable message id: surface the truncation.
		streamRuntime.failResponse(http.StatusBadGateway, "Upstream stream interrupted before completion and cannot be resumed.", "upstream_interrupted")
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}
	terminalWritten := streamRuntime.finalize(finalReason, allowDeferEmpty && finalReason != "content_filter")
	if terminalWritten {
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}
	return completionruntime.ConsumeAttemptResult{Retryable: true}
}

func logResponsesStreamTerminal(streamRuntime *responsesStreamRuntime, attempts int) {
	source := "first_attempt"
	if attempts > 0 {
		source = "synthetic_retry"
	}
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		config.Logger.Info("[openai_empty_retry] terminal cancelled", "surface", "responses", "stream", true, "retry_attempts", attempts, "error_code", streamRuntime.finalErrorCode)
		return
	}
	if streamRuntime.failed {
		config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
		return
	}
	config.Logger.Info("[openai_empty_retry] completed", "surface", "responses", "stream", true, "retry_attempts", attempts, "success_source", source)
}
