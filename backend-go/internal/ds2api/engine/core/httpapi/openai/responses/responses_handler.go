package responses

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/completionruntime"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	openaifmt "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/format/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/responsehistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	streamengine "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/stream"
)

func (h *Handler) GetResponseByID(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineCaller(r)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, err.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "response_id"))
	if id == "" {
		writeOpenAIError(w, http.StatusBadRequest, "response_id is required.")
		return
	}
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st := h.getResponseStore()
	item, ok := st.get(owner, id)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Response not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, openAIGeneralMaxSize)
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// 是否注入工具提示词与号池路由均按“body 是否携带工具定义”决定，写入上下文供 Determine 读取。
	r = r.WithContext(auth.WithToolsPresent(r.Context(), promptcompat.RequestBodyHasTools(req)))

	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	originalModel, rerouted := promptcompat.MaybeAutoRouteVision(req, h.Store)
	if err := h.preprocessInlineFileInputs(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	if err := h.preprocessInlineTextFilesForExpert(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	if rerouted {
		promptcompat.StripImageBlocksFromRequest(req)
	}
	traceID := requestTraceID(r)
	stdReq, err := promptcompat.NormalizeOpenAIResponsesRequest(h.Store, req, traceID)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if rerouted && originalModel != "" {
		stdReq.RequestedModel = originalModel
		stdReq.ResponseModel = originalModel
	}
	stdReq, err = h.applyCurrentInputFile(r.Context(), a, stdReq)
	if err != nil {
		status, message := mapCurrentInputFileError(err)
		writeOpenAIError(w, status, message)
		return
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	historySession := responsehistory.Start(responsehistory.StartParams{
		Store:    h.ChatHistory,
		Request:  r,
		Auth:     a,
		Surface:  "openai.responses",
		Standard: stdReq,
	})
	if !stdReq.Stream {
		result, outErr := completionruntime.ExecuteNonStreamWithRetry(r.Context(), h.DS, a, stdReq, completionruntime.Options{
			RetryEnabled:          true,
			CurrentInputFile:      h.Store,
			ExpertPromptSegment:   h.Store,
			ToolCallRepairEnabled: true,
		})
		if outErr != nil {
			if historySession != nil {
				if completionruntime.IsDirectTokenAuthError(outErr) {
					historySession.Discard()
				} else {
					historySession.ErrorTurn(outErr.Status, outErr.Message, outErr.Code, result.Turn)
				}
			}
			writeOpenAIErrorWithCode(w, outErr.Status, outErr.Message, outErr.Code)
			return
		}
		if historySession != nil {
			historySession.SuccessTurn(http.StatusOK, result.Turn, assistantturn.OpenAIResponsesUsage(result.Turn))
		}
		responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, stdReq.ResponseModel, result.Turn.Prompt, result.Turn.Thinking, result.Turn.Text, result.Turn.ToolCalls, stdReq.ToolsRaw)
		responseObj["usage"] = assistantturn.OpenAIResponsesUsage(result.Turn)
		h.getResponseStore().put(owner, responseID, responseObj)
		writeJSON(w, http.StatusOK, responseObj)
		return
	}

	start, outErr := completionruntime.StartCompletion(r.Context(), h.DS, a, stdReq, completionruntime.Options{
		CurrentInputFile:    h.Store,
		ExpertPromptSegment: h.Store,
	})
	if outErr != nil {
		if historySession != nil {
			if completionruntime.IsDirectTokenAuthError(outErr) {
				historySession.Discard()
			} else {
				historySession.Error(outErr.Status, outErr.Message, outErr.Code, "", "")
			}
		}
		writeOpenAIErrorWithCode(w, outErr.Status, outErr.Message, outErr.Code)
		return
	}

	streamReq := start.Request
	refFileTokens := streamReq.RefFileTokens
	h.handleResponsesStreamWithRetry(w, r, a, start.Response, start.Payload, start.Pow, owner, responseID, streamReq, streamReq.ResponseModel, streamReq.PromptTokenText, refFileTokens, streamReq.Thinking, streamReq.Search, streamReq.ToolNames, streamReq.ToolsRaw, streamReq.ToolChoice, traceID, historySession)
}

func (h *Handler) handleResponsesNonStream(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_responses_nonstream] captcha challenge detected on initial response", "detail", detail)
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)

	turn := assistantturn.BuildTurnFromCollected(result, assistantturn.BuildOptions{
		Model:         model,
		Prompt:        finalPrompt,
		RefFileTokens: refFileTokens,
		SearchEnabled: searchEnabled,
		ToolNames:     toolNames,
		ToolsRaw:      toolsRaw,
		ToolChoice:    toolChoice,
	})
	logResponsesToolPolicyRejection(traceID, toolChoice, turn.ParsedToolCalls, "text")
	outcome := assistantturn.FinalizeTurn(turn, assistantturn.FinalizeOptions{})
	if outcome.ShouldFail {
		writeOpenAIErrorWithCode(w, outcome.Error.Status, outcome.Error.Message, outcome.Error.Code)
		return
	}

	responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, model, finalPrompt, turn.Thinking, turn.Text, turn.ToolCalls, toolsRaw)
	responseObj["usage"] = assistantturn.OpenAIResponsesUsage(turn)
	h.getResponseStore().put(owner, responseID, responseObj)
	writeJSON(w, http.StatusOK, responseObj)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_responses_stream] captcha challenge detected on initial response", "detail", detail)
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
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
	// 无论客户端是否传 tools，都启用 toolSieve 流式拦截：
	// 模型在提示词中被要求使用 <|EPSE|tool_calls> 格式输出工具调用，
	// 若客户端未传 tools（如"继续会话"请求），bufferToolContent 为 false
	// 会导致 EPSE 原文作为正文透传给客户端，产生乱码。
	// toolSieve 解析不依赖 toolNames 过滤，空列表也能正常拦截。
	bufferToolContent := true
	emitEarlyToolDeltas := h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence()
	stripReferenceMarkers := stripReferenceMarkersEnabled()

	streamRuntime := newResponsesStreamRuntime(
		w,
		rc,
		canFlush,
		responseID,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		stripReferenceMarkers,
		toolNames,
		toolsRaw,
		bufferToolContent,
		emitEarlyToolDeltas,
		toolChoice,
		traceID,
		func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		},
		nil,
	)
	streamRuntime.refFileTokens = refFileTokens
	streamRuntime.sendCreated()

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(dsprotocol.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(dsprotocol.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: dsprotocol.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if reason == streamengine.StopReasonRepetitionLoop {
				streamRuntime.failResponse(http.StatusInternalServerError, "identical content repeated", string(streamengine.StopReasonRepetitionLoop))
				return
			}
			if string(reason) == "content_filter" {
				streamRuntime.finalize("content_filter", false)
				return
			}
			streamRuntime.finalize("stop", false)
		},
	})
}

func logResponsesToolPolicyRejection(traceID string, policy promptcompat.ToolChoicePolicy, parsed toolcall.ToolCallParseResult, channel string) {
	rejected := filteredRejectedToolNamesForLog(parsed.RejectedToolNames)
	if !parsed.RejectedByPolicy || len(rejected) == 0 {
		return
	}
	config.Logger.Warn(
		"[responses] rejected tool calls by policy",
		"trace_id", strings.TrimSpace(traceID),
		"channel", channel,
		"tool_choice_mode", policy.Mode,
		"rejected_tool_names", strings.Join(rejected, ","),
	)
}

func filteredRejectedToolNamesForLog(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		switch strings.ToLower(trimmed) {
		case "", "tool_name":
			continue
		default:
			out = append(out, trimmed)
		}
	}
	return out
}
