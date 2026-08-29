package chat

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/files"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/history"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/textclean"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolstream"
)

const openAIGeneralMaxSize = shared.GeneralMaxSize

var writeJSON = shared.WriteJSON

type Handler struct {
	Store        shared.ConfigReader
	Auth         shared.AuthResolver
	DS           shared.DeepSeekCaller
	ChatHistory  *chathistory.Store
	ContentStore files.ContentStore

	leaseMu      sync.Mutex
	streamLeases map[string]streamLease
}

type streamLease struct {
	Auth      *auth.RequestAuth
	Standard  promptcompat.StandardRequest
	SessionID string
	ExpiresAt time.Time
}

func stripReferenceMarkersEnabled() bool {
	return textclean.StripReferenceMarkersEnabled()
}

func (h *Handler) applyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	if h == nil {
		return stdReq, nil
	}
	stdReq = shared.ApplyThinkingInjection(h.Store, stdReq)
	svc := history.Service{Store: h.Store, DS: h.DS}
	out, err := svc.ApplyCurrentInputFile(ctx, a, stdReq)
	if err != nil || out.CurrentInputFileApplied {
		return out, err
	}
	return out, nil
}

func (h *Handler) preprocessInlineFileInputs(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	if h == nil {
		return nil
	}
	return (&files.Handler{Store: h.Store, Auth: h.Auth, DS: h.DS, ChatHistory: h.ChatHistory, ContentStore: h.ContentStore}).PreprocessInlineFileInputs(ctx, a, req)
}

func (h *Handler) preprocessInlineTextFilesForExpert(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	if h == nil {
		return nil
	}
	return (&files.Handler{Store: h.Store, Auth: h.Auth, DS: h.DS, ChatHistory: h.ChatHistory, ContentStore: h.ContentStore}).PreprocessInlineTextFilesForExpert(ctx, a, req)
}

func (h *Handler) toolcallFeatureMatchEnabled() bool {
	if h == nil {
		return shared.ToolcallFeatureMatchEnabled(nil)
	}
	return shared.ToolcallFeatureMatchEnabled(h.Store)
}

func (h *Handler) toolcallEarlyEmitHighConfidence() bool {
	if h == nil {
		return shared.ToolcallEarlyEmitHighConfidence(nil)
	}
	return shared.ToolcallEarlyEmitHighConfidence(h.Store)
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	shared.WriteOpenAIError(w, status, message)
}

func writeOpenAIErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	shared.WriteOpenAIErrorWithCode(w, status, message, code)
}

func openAIErrorType(status int) string {
	return shared.OpenAIErrorType(status)
}

func writeOpenAIInlineFileError(w http.ResponseWriter, err error) {
	files.WriteInlineFileError(w, err)
}

func mapCurrentInputFileError(err error) (int, string) {
	return history.MapError(err)
}

func requestTraceID(r *http.Request) string {
	return shared.RequestTraceID(r)
}

func asString(v any) string {
	return shared.AsString(v)
}

func cleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	return shared.CleanVisibleOutput(text, stripReferenceMarkers)
}

func emptyOutputRetryEnabled() bool {
	return shared.EmptyOutputRetryEnabled()
}

func emptyOutputRetryMaxAttempts() int {
	return shared.EmptyOutputRetryMaxAttempts()
}

func formatIncrementalStreamToolCallDeltas(deltas []toolstream.ToolCallDelta, ids map[int]string) []map[string]any {
	return shared.FormatIncrementalStreamToolCallDeltas(deltas, ids)
}

func filterIncrementalToolCallDeltasByAllowed(deltas []toolstream.ToolCallDelta, seenNames map[int]string) []toolstream.ToolCallDelta {
	return shared.FilterIncrementalToolCallDeltasByAllowed(deltas, seenNames)
}

func formatFinalStreamToolCallsWithStableIDs(calls []toolcall.ParsedToolCall, ids map[int]string, toolsRaw any) []map[string]any {
	return shared.FormatFinalStreamToolCallsWithStableIDs(calls, ids, toolsRaw)
}
