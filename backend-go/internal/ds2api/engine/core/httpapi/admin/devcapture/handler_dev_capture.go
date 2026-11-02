package devcapture

import (
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/devcapture"
)

func (h *Handler) getDevCaptures(w http.ResponseWriter, _ *http.Request) {
	store := devcapture.Global()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        store.Enabled(),
		"limit":          store.Limit(),
		"max_body_bytes": store.MaxBodyBytes(),
		"items":          store.Snapshot(),
	})
}

func (h *Handler) clearDevCaptures(w http.ResponseWriter, _ *http.Request) {
	store := devcapture.Global()
	store.Clear()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"detail":  "capture logs cleared",
	})
}
