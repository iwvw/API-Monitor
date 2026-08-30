package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/completionruntime"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	openaifmt "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/format/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	streamengine "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/stream"
)

func (h *Handler) handleNonStreamWithRetry(w http.ResponseWriter, ctx context.Context, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, completionID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, historySession *chatHistorySession) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_chat] captcha challenge detected on initial response", "account", a.AccountID, "detail", detail)
		}
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	stdReq := promptcompat.StandardRequest{
		Surface:         "chat.completions",
		ResponseModel:   model,
		PromptTokenText: finalPrompt,
		FinalPrompt:     finalPrompt,
		RefFileTokens:   refFileTokens,
		Thinking:        thinkingEnabled,
		Search:          searchEnabled,
		ToolNames:       toolNames,
		ToolsRaw:        toolsRaw,
		ToolChoice:      promptcompat.DefaultToolChoicePolicy(),
	}
	retryEnabled := h != nil && h.DS != nil && emptyOutputRetryEnabled()
	result, outErr := completionruntime.ExecuteNonStreamStartedWithRetry(ctx, h.DS, a, completionruntime.StartResult{
		SessionID: completionID,
		Payload:   payload,
		Pow:       pow,
		Response:  resp,
		Request:   stdReq,
	}, completionruntime.Options{
		RetryEnabled:          retryEnabled,
		RetryMaxAttempts:      emptyOutputRetryMaxAttempts(),
		ExpertPromptSegment:   h.Store,
		ToolCallRepairEnabled: true,
	})
	if outErr != nil {
		if historySession != nil {
			historySession.error(outErr.Status, outErr.Message, outErr.Code, historyThinkingForArchive(result.Turn.RawThinking, result.Turn.DetectionThinking, result.Turn.Thinking), historyTextForArchive(result.Turn.RawText, result.Turn.Text))
		}
		writeOpenAIErrorWithCode(w, outErr.Status, outErr.Message, outErr.Code)
		return
	}
	respBody := openaifmt.BuildChatCompletionWithToolCalls(result.SessionID, model, result.Turn.Prompt, result.Turn.Thinking, result.Turn.Text, result.Turn.ToolCalls, toolsRaw)
	respBody["usage"] = assistantturn.OpenAIChatUsage(result.Turn)
	outcome := assistantturn.FinalizeTurn(result.Turn, assistantturn.FinalizeOptions{})
	if historySession != nil {
		historySession.success(http.StatusOK, historyThinkingForArchive(result.Turn.RawThinking, result.Turn.DetectionThinking, result.Turn.Thinking), historyTextForArchive(result.Turn.RawText, result.Turn.Text), outcome.FinishReason, assistantturn.OpenAIChatUsage(result.Turn))
	}
	writeJSON(w, http.StatusOK, respBody)
}

func (h *Handler) handleStreamWithRetry(w http.ResponseWriter, r *http.Request, a *auth.RequestAuth, resp *http.Response, payload map[string]any, pow, completionID string, sessionIDRef *string, stdReq promptcompat.StandardRequest, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, historySession *chatHistorySession) {
	streamRuntime, initialType, ok := h.prepareChatStreamRuntime(w, resp, completionID, model, finalPrompt, refFileTokens, thinkingEnabled, searchEnabled, toolNames, toolsRaw, toolChoice, historySession)
	if !ok {
		return
	}
	// M4: wire the phase-3 finalize-only LLM repair into the stream finalize so
	// bad tool-call text can be replaced by repaired tool_calls before the
	// final frame is delivered. The per-chunk sieve never receives the invoker.
	streamRuntime.toolCallRepair = completionruntime.NewToolCallRepairInvoker(h.DS, a)
	streamRuntime.toolCallRepairCtx = r.Context()
	completionruntime.ExecuteStreamWithRetry(r.Context(), h.DS, a, resp, payload, pow, completionruntime.StreamRetryOptions{
		Surface:             "chat.completions",
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
			return h.consumeChatStreamAttempt(r, currentResp, streamRuntime, initialType, thinkingEnabled, historySession, allowDeferEmpty)
		},
		Finalize: func(attempts int) {
			streamRuntime.finalize("stop", false)
			recordChatStreamHistory(streamRuntime, historySession)
			config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", "none")
		},
		ParentMessageID: func() int {
			return streamRuntime.responseMessageID
		},
		OnRetryPrompt: func(prompt string) {
			streamRuntime.finalPrompt = prompt
		},
		OnRetryFailure: func(status int, message, code string) {
			failChatStreamRetry(streamRuntime, historySession, status, message, code)
		},
		OnAccountSwitch: func(sessionID string) {
			if sessionIDRef != nil {
				*sessionIDRef = sessionID
			}
		},
		OnTerminal: func(attempts int) {
			logChatStreamTerminal(streamRuntime, attempts)
		},
		EmptyOutputError: func() *assistantturn.OutputError {
			if streamRuntime.finalErrorCode == "" {
				return nil
			}
			return &assistantturn.OutputError{Status: streamRuntime.finalErrorStatus, Message: streamRuntime.finalErrorMessage, Code: streamRuntime.finalErrorCode}
		},
	})
}

func (h *Handler) prepareChatStreamRuntime(w http.ResponseWriter, resp *http.Response, completionID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, historySession *chatHistorySession) (*chatStreamRuntime, string, bool) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_chat_stream] captcha challenge detected on initial response", "detail", detail)
		}
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, string(body))
		return nil, "", false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if !canFlush {
		config.Logger.Warn("[stream] response writer does not support flush; streaming may be buffered")
	}
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamRuntime := newChatStreamRuntime(
		w, rc, canFlush, completionID, time.Now().Unix(), model, finalPrompt,
		thinkingEnabled, searchEnabled, stripReferenceMarkersEnabled(), toolNames, toolsRaw,
		toolChoice,
		// 无论客户端是否传 tools，都启用 toolSieve 流式拦截：
		// 模型在提示词中被要求使用 <|EPSE|tool_calls> 格式输出工具调用，
		// 若客户端未传 tools（如 opencode/rikkahub 的“继续会话”请求），
		// bufferToolContent 为 false 会导致 EPSE 原文作为正文透传给客户端，
		// 产生乱码。toolSieve 解析不依赖 toolNames 过滤，空列表也能正常拦截。
		true, h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence(),
	)
	streamRuntime.refFileTokens = refFileTokens
	return streamRuntime, initialType, true
}

func (h *Handler) consumeChatStreamAttempt(r *http.Request, resp *http.Response, streamRuntime *chatStreamRuntime, initialType string, thinkingEnabled bool, historySession *chatHistorySession, allowDeferEmpty bool) completionruntime.ConsumeAttemptResult {
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
		OnKeepAlive: streamRuntime.sendKeepAlive,
		OnParsed: func(parsed sse.LineResult) streamengine.ParsedDecision {
			decision := streamRuntime.onParsed(parsed)
			if historySession != nil {
				historySession.progress(streamRuntime.historyThinking(), streamRuntime.historyText())
			}
			return decision
		},
		OnFinalize: func(reason streamengine.StopReason, err error) {
			stopReason = reason
			scannerErr = err
			if string(reason) == "content_filter" {
				finalReason = "content_filter"
			}
		},
		OnContextDone: func() {
			streamRuntime.markContextCancelled()
			if historySession != nil {
				historySession.stopped(streamRuntime.historyThinking(), streamRuntime.historyText(), string(streamengine.StopReasonContextCancelled))
			}
		},
	})
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}

	// An upstream stream that died mid-body (read error) or stalled past the
	// idle / no-content window after producing partial output must not be
	// silently finalized as a normal "stop": schedule a continue-resume so the
	// same message is completed, or surface an explicit interruption error.
	interrupted := scannerErr != nil || stopReason == streamengine.StopReasonIdleTimeout || stopReason == streamengine.StopReasonNoContentTimeout
	if interrupted && streamRuntime.responseMessageID > 0 && streamRuntime.accumulator.HasPartialOutput() {
		config.Logger.Warn("[openai_chat_stream] upstream stream interrupted; scheduling continue-resume",
			"surface", "chat.completions", "stream", true, "reason", stopReason, "error", fmt.Sprint(scannerErr), "response_message_id", streamRuntime.responseMessageID)
		return completionruntime.ConsumeAttemptResult{Retryable: true, ResumeContinue: true}
	}
	if interrupted && streamRuntime.accumulator.HasPartialOutput() {
		// No resumable message id. If nothing visible has been delivered to the
		// client yet (only thinking accumulated, no content/tool_calls), schedule
		// an empty-output regeneration so the turn self-heals instead of
		// surfacing a hard 502 interruption to the client.
		if streamRuntime.accumulator.Text.Len() == 0 && !streamRuntime.toolCallsEmitted && !streamRuntime.toolCallsDoneEmitted {
			config.Logger.Warn("[openai_chat_stream] upstream stream interrupted before visible output; scheduling empty-output retry",
				"surface", "chat.completions", "stream", true, "reason", stopReason, "error", fmt.Sprint(scannerErr))
			return completionruntime.ConsumeAttemptResult{Retryable: true}
		}
		// Partial output but no resumable message id: surface the truncation.
		streamRuntime.sendFailedChunk(http.StatusBadGateway, "Upstream stream interrupted before completion and cannot be resumed.", "upstream_interrupted")
		recordChatStreamHistory(streamRuntime, historySession)
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}
	terminalWritten := streamRuntime.finalize(finalReason, allowDeferEmpty && finalReason != "content_filter")
	if terminalWritten {
		recordChatStreamHistory(streamRuntime, historySession)
		return completionruntime.ConsumeAttemptResult{Terminal: true}
	}
	return completionruntime.ConsumeAttemptResult{Retryable: true}
}

func recordChatStreamHistory(streamRuntime *chatStreamRuntime, historySession *chatHistorySession) {
	if historySession == nil {
		return
	}
	if streamRuntime.finalErrorMessage != "" {
		historySession.error(streamRuntime.finalErrorStatus, streamRuntime.finalErrorMessage, streamRuntime.finalErrorCode, streamRuntime.historyThinking(), streamRuntime.historyText())
		return
	}
	historySession.success(http.StatusOK, streamRuntime.historyThinking(), streamRuntime.historyText(), streamRuntime.finalFinishReason, streamRuntime.finalUsage)
}

func failChatStreamRetry(streamRuntime *chatStreamRuntime, historySession *chatHistorySession, status int, message, code string) {
	streamRuntime.sendFailedChunk(status, message, code)
	if historySession != nil {
		historySession.error(status, message, code, streamRuntime.historyThinking(), streamRuntime.historyText())
	}
}

func logChatStreamTerminal(streamRuntime *chatStreamRuntime, attempts int) {
	source := "first_attempt"
	if attempts > 0 {
		source = "synthetic_retry"
	}
	if streamRuntime.finalErrorCode == string(streamengine.StopReasonContextCancelled) {
		config.Logger.Info("[openai_empty_retry] terminal cancelled", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "error_code", streamRuntime.finalErrorCode)
		return
	}
	if streamRuntime.finalErrorMessage != "" {
		config.Logger.Info("[openai_empty_retry] terminal empty output", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", "none", "error_code", streamRuntime.finalErrorCode)
		return
	}
	config.Logger.Info("[openai_empty_retry] completed", "surface", "chat.completions", "stream", true, "retry_attempts", attempts, "success_source", source)
}
