package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func traceIDViaMiddleware(req *http.Request) string {
	if req == nil {
		return requestTraceID(nil)
	}
	var got string
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = requestTraceID(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestRequestTraceIDPriority(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions?__trace_id=query-trace", nil)
	req.Header.Set("X-Ds2-Test-Trace", "header-trace")
	got := traceIDViaMiddleware(req)
	if got != "query-trace" {
		t.Fatalf("expected query trace id to win, got %q", got)
	}
}

func TestRequestTraceIDHeaderFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("X-Ds2-Test-Trace", "header-trace")
	got := traceIDViaMiddleware(req)
	if got != "header-trace" {
		t.Fatalf("expected header trace id to win when query missing, got %q", got)
	}
}

func TestRequestTraceIDReqIDFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	got := traceIDViaMiddleware(req)
	if got == "" {
		t.Fatal("expected middleware request id fallback to be non-empty")
	}
}
