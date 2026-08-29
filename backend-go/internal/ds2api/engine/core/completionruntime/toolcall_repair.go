package completionruntime

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
)

// toolCallRepairModel is the resolved model used for the LLM tool-call repair
// pass. deepseek-v4-pro resolves to model_type "expert" (config.GetModelType),
// satisfying the phase3 §3 "专家模式" hard constraint.
const toolCallRepairModel = "deepseek-v4-pro"

// toolCallRepairTimeout bounds a single repair completion to 10 seconds
// (phase3 §3). On timeout the invoker returns an error and the caller falls
// back to emitting the residual verbatim (branch ③).
const toolCallRepairTimeout = 10 * time.Second

// toolCallRepairSessionDeleteTimeout bounds the best-effort cleanup delete of a
// repair session. It runs on a context detached from the request/repair
// deadlines (mirroring the autoDeleteRemoteSession mechanism) so the delete is
// not cancelled when the repair itself times out, and does not prolong the main
// request.
const toolCallRepairSessionDeleteTimeout = 10 * time.Second

// repairSessionDeleter is the subset of the DeepSeek client used to reclaim a
// repair session. It is satisfied by *dsclient.Client via a runtime type
// assertion (mirroring the continueResumer pattern in stream_retry.go), so the
// narrow DeepSeekCaller interface stays unchanged.
type repairSessionDeleter interface {
	DeleteSession(ctx context.Context, a *auth.RequestAuth, sessionID string, maxAttempts int) (*dsclient.DeleteSessionResult, error)
}

// NewToolCallRepairInvoker builds a toolcall.ToolCallRepairInvoker backed by the
// upstream DeepSeek caller. Each invocation honors the phase3 §3 hard
// constraints:
//   - same account (the request's *auth.RequestAuth),
//   - expert mode (deepseek-v4-pro → model_type "expert"),
//   - thinking disabled (thinking_enabled=false),
//   - a brand-new session (CreateSession, no reused context),
//   - a 10-second timeout.
//
// It returns nil when ds or a is nil so callers can treat repair as disabled.
func NewToolCallRepairInvoker(ds DeepSeekCaller, a *auth.RequestAuth) toolcall.ToolCallRepairInvoker {
	if ds == nil || a == nil {
		return nil
	}
	return func(ctx context.Context, prompt string) (string, error) {
		// Detach from the parent deadline but keep cancellation semantics via a
		// fresh 10s budget dedicated to the repair pass.
		repairCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolCallRepairTimeout)
		defer cancel()

		stdReq := promptcompat.StandardRequest{
			Surface:       "toolcall.repair",
			ResolvedModel: toolCallRepairModel,
			ResponseModel: toolCallRepairModel,
			FinalPrompt:   prompt,
			Thinking:      false,
			Search:        false,
		}

		sessionID, err := ds.CreateSession(repairCtx, a, 1)
		if err != nil {
			return "", err
		}
		// The repair session is a throwaway "new conversation" (phase3 §3). It
		// must not leak on the upstream account, so schedule a best-effort
		// delete regardless of success/failure/timeout. The delete runs on a
		// context detached from ctx/repairCtx (via WithoutCancel) so a repair
		// timeout does not cancel the cleanup, and it does not prolong the main
		// request.
		defer deleteRepairSession(ctx, ds, a, sessionID)
		pow, err := ds.GetPow(repairCtx, a, 1)
		if err != nil {
			return "", err
		}
		payload := stdReq.CompletionPayload(sessionID)
		resp, err := ds.CallCompletion(repairCtx, a, payload, pow, 1)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return "", errToolCallRepairUpstream
		}
		result := sse.CollectStream(resp, false, true)
		return result.Text, nil
	}
}

// deleteRepairSession reclaims a repair session on a best-effort basis, wiring
// the phase3 repair pass into the project's session-deletion strategy (the same
// mechanism autoDeleteRemoteSession uses for the main request). It is a no-op
// when the caller cannot delete sessions or the session id is empty. Errors are
// logged, not returned: cleanup failure must never fail the repair.
func deleteRepairSession(parentCtx context.Context, ds DeepSeekCaller, a *auth.RequestAuth, sessionID string) {
	if sessionID == "" {
		return
	}
	deleter, ok := ds.(repairSessionDeleter)
	if !ok {
		return
	}
	// Detach from the parent (and the already-expired repair) context so the
	// delete is not cancelled by a repair timeout or a completed request.
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), toolCallRepairSessionDeleteTimeout)
	defer cancel()
	if _, err := deleter.DeleteSession(deleteCtx, a, sessionID, 1); err != nil {
		config.Logger.Warn("[toolcall_repair] failed to delete repair session", "session_id", sessionID, "error", err)
	}
}

var errToolCallRepairUpstream = errors.New("toolcall repair: upstream returned non-200")
