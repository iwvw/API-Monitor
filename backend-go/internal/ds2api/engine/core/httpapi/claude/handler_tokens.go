package claude

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) CountTokens(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClaudeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	model, _ := req["model"].(string)
	messages, _ := req["messages"].([]any)
	if model == "" || len(messages) == 0 {
		writeClaudeError(w, http.StatusBadRequest, "Request must include 'model' and 'messages'.")
		return
	}
	normalized, err := normalizeClaudeRequest(h.Store, req)
	if err != nil {
		writeClaudeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inputTokens := countClaudeInputTokens(normalized.Standard)
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": inputTokens})
}
