package responses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/stream"
)

func makeResponsesOpenAISSEHTTPResponse(lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestConsumeResponsesStreamAttemptMarksContextCancelledState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	streamRuntime := newResponsesStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"resp-cancelled",
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		false,
		false,
		promptcompat.DefaultToolChoicePolicy(),
		"",
		nil,
		nil,
	)
	resp := makeResponsesOpenAISSEHTTPResponse(
		`data: {"p":"response/content","v":"hello"}`,
		`data: [DONE]`,
	)

	h := &Handler{}
	res := h.consumeResponsesStreamAttempt(req, resp, streamRuntime, "text", false, true)
	if !res.Terminal || res.Retryable {
		t.Fatalf("expected cancelled attempt to terminate without retry, got result=%+v", res)
	}
	if !streamRuntime.failed {
		t.Fatalf("expected cancelled response stream to be marked failed")
	}
	if got, want := streamRuntime.finalErrorCode, string(stream.StopReasonContextCancelled); got != want {
		t.Fatalf("expected cancelled final error code %q, got %q", want, got)
	}
	if streamRuntime.finalErrorMessage == "" {
		t.Fatalf("expected cancelled final error message to be preserved")
	}
}

type erroringResponsesSSEBody struct {
	chunks []string
	err    error
	idx    int
}

func (b *erroringResponsesSSEBody) Read(p []byte) (int, error) {
	if b.idx < len(b.chunks) {
		chunk := b.chunks[b.idx]
		b.idx++
		return copy(p, chunk), nil
	}
	return 0, b.err
}

func (b *erroringResponsesSSEBody) Close() error { return nil }

func TestConsumeResponsesStreamAttemptSchedulesResumeOnInterruptedPartialStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	streamRuntime := newResponsesStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"resp-interrupted",
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		false,
		false,
		promptcompat.DefaultToolChoicePolicy(),
		"",
		nil,
		nil,
	)
	body := "data: {\"response_message_id\":88,\"p\":\"response/content\",\"v\":\"partial\"}\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       &erroringResponsesSSEBody{chunks: []string{body}, err: errors.New("connection reset by peer")},
	}

	h := &Handler{}
	res := h.consumeResponsesStreamAttempt(req, resp, streamRuntime, "text", false, false)
	if res.Terminal || !res.Retryable || !res.ResumeContinue {
		t.Fatalf("expected interrupted partial stream to schedule continue-resume, got result=%+v", res)
	}
	if got := streamRuntime.responseMessageID; got != 88 {
		t.Fatalf("expected runtime to track response_message_id 88, got %d", got)
	}
}
