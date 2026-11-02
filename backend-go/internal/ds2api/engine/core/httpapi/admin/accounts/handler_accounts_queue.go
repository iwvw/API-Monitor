package accounts

import "net/http"

func (h *Handler) queueStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.Pool.Status())
}
