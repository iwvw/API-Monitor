package shared

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

func RequestTraceID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if q := strings.TrimSpace(r.URL.Query().Get("__trace_id")); q != "" {
		return q
	}
	if h := strings.TrimSpace(r.Header.Get("X-Ds2-Test-Trace")); h != "" {
		return h
	}
	return strings.TrimSpace(middleware.GetReqID(r.Context()))
}
