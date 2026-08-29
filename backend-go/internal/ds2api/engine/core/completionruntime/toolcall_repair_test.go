package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
)

// repairFakeCaller is a DeepSeekCaller that also implements repairSessionDeleter
// so the repair invoker can create and reclaim its throwaway session. It counts
// how many sessions were created, completions issued and sessions deleted.
type repairFakeCaller struct {
	createdSessions int64
	completions     int64
	deletedSessions int64
	deletedIDs      []string
	completionBody  string
}

func (f *repairFakeCaller) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	n := atomic.AddInt64(&f.createdSessions, 1)
	return "repair-session-" + string(rune('0'+n)), nil
}

func (f *repairFakeCaller) GetPow(context.Context, *auth.RequestAuth, int) (string, error) {
	return "pow", nil
}

func (f *repairFakeCaller) UploadFile(_ context.Context, _ *auth.RequestAuth, _ dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	return &dsclient.UploadFileResult{ID: "file"}, nil
}

func (f *repairFakeCaller) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	atomic.AddInt64(&f.completions, 1)
	body := f.completionBody
	if body == "" {
		body = `data: {"p":"response/content","v":"<|EPSE|tool_calls><|EPSE|invoke name=\"Bash\"><|EPSE|parameter name=\"command\"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>"}`
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (f *repairFakeCaller) StopStream(context.Context, *auth.RequestAuth, string, int) error {
	return nil
}

func (f *repairFakeCaller) FireCompletionAndStop(context.Context, *auth.RequestAuth, map[string]any, string) (int, error) {
	return 1, nil
}

func (f *repairFakeCaller) DeleteSession(_ context.Context, _ *auth.RequestAuth, sessionID string, _ int) (*dsclient.DeleteSessionResult, error) {
	atomic.AddInt64(&f.deletedSessions, 1)
	f.deletedIDs = append(f.deletedIDs, sessionID)
	return &dsclient.DeleteSessionResult{SessionID: sessionID, Success: true}, nil
}

// TestNewToolCallRepairInvokerDeletesSession covers M2: the repair session is
// created and then deleted after the repair completes, so it does not leak.
func TestNewToolCallRepairInvokerDeletesSession(t *testing.T) {
	ds := &repairFakeCaller{}
	invoke := NewToolCallRepairInvoker(ds, &auth.RequestAuth{})
	if invoke == nil {
		t.Fatal("expected non-nil invoker")
	}
	out, err := invoke(context.Background(), "some repair prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty repair output")
	}
	if got := atomic.LoadInt64(&ds.createdSessions); got != 1 {
		t.Fatalf("expected 1 created session, got %d", got)
	}
	if got := atomic.LoadInt64(&ds.deletedSessions); got != 1 {
		t.Fatalf("expected 1 deleted session, got %d", got)
	}
	if len(ds.deletedIDs) != 1 || ds.deletedIDs[0] != "repair-session-1" {
		t.Fatalf("expected repair-session-1 deleted, got %v", ds.deletedIDs)
	}
}

// TestNewToolCallRepairInvokerDeletesSessionOnUpstreamError covers M2: even when
// the completion returns non-200, the created session must still be deleted.
func TestNewToolCallRepairInvokerDeletesSessionOnUpstreamError(t *testing.T) {
	ds := &repairFakeCaller{}
	// Override completion to return a non-200 via a wrapper caller.
	failing := &repairFailingCaller{repairFakeCaller: ds}
	invoke := NewToolCallRepairInvoker(failing, &auth.RequestAuth{})
	_, err := invoke(context.Background(), "prompt")
	if err == nil {
		t.Fatalf("expected upstream error")
	}
	if got := atomic.LoadInt64(&ds.deletedSessions); got != 1 {
		t.Fatalf("expected session deleted even on upstream error, got %d", got)
	}
}

type repairFailingCaller struct {
	*repairFakeCaller
}

func (f *repairFailingCaller) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("boom"))}, nil
}

func (f *repairFailingCaller) DeleteSession(ctx context.Context, a *auth.RequestAuth, sessionID string, n int) (*dsclient.DeleteSessionResult, error) {
	return f.repairFakeCaller.DeleteSession(ctx, a, sessionID, n)
}
