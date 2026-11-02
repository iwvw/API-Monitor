package responses

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
)

func asString(v any) string {
	return shared.AsString(v)
}

func decodeJSONBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode json failed: %v, body=%s", err, body)
	}
	return out
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/v1/responses", h.Responses)
	r.Get("/v1/responses/{response_id}", h.GetResponseByID)
}
