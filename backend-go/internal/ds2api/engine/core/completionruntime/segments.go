package completionruntime

import (
	"context"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

func StartCompletionWithSegments(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, opts Options, segments []string) (StartResult, *assistantturn.OutputError) {
	if len(segments) <= 1 {
		return startCompletionOnce(ctx, ds, a, stdReq, opts)
	}

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

	finalPow, finalPayload, outErr := fireSegmentPayloads(ctx, ds, a, stdReq, sessionID, segments, maxAttempts)
	if outErr != nil {
		return StartResult{SessionID: sessionID, Request: stdReq}, outErr
	}
	resp, err := ds.CallCompletion(ctx, a, finalPayload, finalPow, maxAttempts)
	if err != nil {
		if dsclient.IsMutedError(err) {
			return StartResult{SessionID: sessionID, Payload: finalPayload, Pow: finalPow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusForbidden, Message: "Account is muted by upstream.", Code: "account_muted"}
		}
		return StartResult{SessionID: sessionID, Payload: finalPayload, Pow: finalPow, Request: stdReq}, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to get completion.", Code: "error"}
	}
	return StartResult{SessionID: sessionID, Payload: finalPayload, Pow: finalPow, Response: resp, Request: stdReq}, nil
}

// fireSegmentPayloads sends all but the last prompt segment via
// FireCompletionAndStop and returns the PoW and payload for the final segment.
// The returned payload still carries the parent_message_id chain so the caller
// can either stream it directly or continue it with a retry payload.
func fireSegmentPayloads(ctx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, stdReq promptcompat.StandardRequest, sessionID string, segments []string, maxAttempts int) (string, map[string]any, *assistantturn.OutputError) {
	parentMessageID := 0
	for i := 0; i < len(segments)-1; i++ {
		segPow, err := ds.GetPow(ctx, a, maxAttempts)
		if err != nil {
			return "", nil, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
		}
		segPayload := stdReq.CompletionPayloadWithParentAndPrompt(sessionID, parentMessageID, segments[i])
		logSegmentPayload("fire-stop", i, len(segments), sessionID, parentMessageID, segments[i])
		respID, err := ds.FireCompletionAndStop(ctx, a, segPayload, segPow)
		if err != nil {
			if dsclient.IsMutedError(err) {
				return "", nil, &assistantturn.OutputError{Status: http.StatusForbidden, Message: "Account is muted by upstream.", Code: "account_muted"}
			}
			config.Logger.Warn("[start_completion_with_segments] segment fire-and-stop failed", "segment_index", i, "session_id", sessionID, "parent_message_id", parentMessageID, "error", err)
			return "", nil, &assistantturn.OutputError{Status: http.StatusInternalServerError, Message: "Failed to send segment before stop: " + err.Error(), Code: "error"}
		}
		parentMessageID = respID
	}

	finalPow, err := ds.GetPow(ctx, a, maxAttempts)
	if err != nil {
		return "", nil, &assistantturn.OutputError{Status: http.StatusUnauthorized, Message: "Failed to get PoW (invalid token or unknown error).", Code: "error"}
	}
	finalPayload := stdReq.CompletionPayloadWithParentAndPrompt(sessionID, parentMessageID, segments[len(segments)-1])
	logSegmentPayload("final", len(segments)-1, len(segments), sessionID, parentMessageID, segments[len(segments)-1])
	return finalPow, finalPayload, nil
}

func logSegmentPayload(kind string, index int, total int, sessionID string, parentMessageID int, prompt string) {
	config.Logger.Info("[start_completion_with_segments] sending segment",
		"kind", kind,
		"segment_index", index,
		"segment_total", total,
		"session_id", sessionID,
		"parent_message_id", parentMessageID,
		"prompt_runes", len([]rune(prompt)),
	)
}
