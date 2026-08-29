package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/stream"
)

// erroringSSEBody emits a fixed set of chunks and then fails with err,
// simulating an upstream stream that is cut mid-body (RST / EOF without
// terminal status).
type erroringSSEBody struct {
	chunks []string
	err    error
	idx    int
}

func (b *erroringSSEBody) Read(p []byte) (int, error) {
	if b.idx < len(b.chunks) {
		chunk := b.chunks[b.idx]
		b.idx++
		return copy(p, chunk), nil
	}
	return 0, b.err
}

func (b *erroringSSEBody) Close() error { return nil }

func makeInterruptedOpenAISSEHTTPResponse(err error, lines ...string) *http.Response {
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       &erroringSSEBody{chunks: []string{body}, err: err},
	}
}

func TestConsumeChatStreamAttemptSchedulesResumeOnInterruptedPartialStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	streamRuntime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"cid-interrupted",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false,
		false,
	)
	resp := makeInterruptedOpenAISSEHTTPResponse(
		errors.New("connection reset by peer"),
		`data: {"response_message_id":77,"p":"response/content","v":"partial code"}`,
	)

	h := &Handler{}
	res := h.consumeChatStreamAttempt(req, resp, streamRuntime, "text", false, nil, false)
	if res.Terminal || !res.Retryable || !res.ResumeContinue {
		t.Fatalf("expected interrupted partial stream to schedule continue-resume, got result=%+v", res)
	}
	if got := streamRuntime.responseMessageID; got != 77 {
		t.Fatalf("expected runtime to track response_message_id 77, got %d", got)
	}
}

func TestConsumeChatStreamAttemptSurfacesInterruptionWithoutMessageID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	streamRuntime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"cid-no-msg",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false,
		false,
	)
	resp := makeInterruptedOpenAISSEHTTPResponse(
		errors.New("connection reset by peer"),
		`data: {"p":"response/content","v":"partial code"}`,
	)

	h := &Handler{}
	res := h.consumeChatStreamAttempt(req, resp, streamRuntime, "text", false, nil, false)
	if !res.Terminal {
		t.Fatalf("expected interrupted stream without message id to terminate, got result=%+v", res)
	}
	if got, want := streamRuntime.finalErrorCode, "upstream_interrupted"; got != want {
		t.Fatalf("expected upstream_interrupted error code, got %q", got)
	}
	body := rec.Body.String()
	if !containsStreamErrorCode(body, "upstream_interrupted") {
		t.Fatalf("expected explicit interruption error chunk in response body, got %q", body)
	}
}

func containsStreamErrorCode(body, code string) bool {
	return strings.Contains(body, `"code":"`+code+`"`)
}

func TestConsumeChatStreamAttemptMarksContextCancelledState(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	entry, err := historyStore.Start(chathistory.StartParams{
		CallerID:  "caller:test",
		Model:     "deepseek-v4-flash",
		Stream:    true,
		UserInput: "hello",
	})
	if err != nil {
		t.Fatalf("start history failed: %v", err)
	}
	session := &chatHistorySession{
		store:       historyStore,
		entryID:     entry.ID,
		startedAt:   time.Now(),
		lastPersist: time.Now(),
		finalPrompt: "prompt",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	streamRuntime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"cid-cancelled",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false,
		false,
	)
	resp := makeOpenAISSEHTTPResponse(
		`data: {"p":"response/content","v":"hello"}`,
		`data: [DONE]`,
	)

	h := &Handler{}
	res := h.consumeChatStreamAttempt(req, resp, streamRuntime, "text", false, session, true)
	if !res.Terminal || res.Retryable {
		t.Fatalf("expected cancelled attempt to terminate without retry, got result=%+v", res)
	}
	if got, want := streamRuntime.finalErrorCode, string(stream.StopReasonContextCancelled); got != want {
		t.Fatalf("expected cancelled final error code %q, got %q", want, got)
	}
	if streamRuntime.finalErrorMessage == "" {
		t.Fatalf("expected cancelled final error message to be preserved")
	}

	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	full, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get detail failed: %v", err)
	}
	if full.Status != "stopped" {
		t.Fatalf("expected stopped status, got %#v", full)
	}
}
