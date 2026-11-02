package shared

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

type ModelsHandler struct {
	Store ConfigReader
}

func (h *ModelsHandler) ListModels(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, config.OpenAIModelsResponse())
}

func (h *ModelsHandler) GetModel(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimSpace(chi.URLParam(r, "model_id"))
	model, ok := config.OpenAIModelByID(h.Store, modelID)
	if !ok {
		WriteOpenAIError(w, http.StatusNotFound, "Model not found.")
		return
	}
	WriteJSON(w, http.StatusOK, model)
}
