package gemini

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/textclean"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

var writeJSON = util.WriteJSON

type Handler struct {
	Store       ConfigReader
	Auth        AuthResolver
	DS          DeepSeekCaller
	OpenAI      OpenAIChatRunner
	ChatHistory *chathistory.Store
}

//nolint:unused // used by native Gemini stream/non-stream runtime helpers.
func stripReferenceMarkersEnabled() bool {
	return textclean.StripReferenceMarkersEnabled()
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/v1beta/models/{model}:generateContent", h.GenerateContent)
	r.Post("/v1beta/models/{model}:streamGenerateContent", h.StreamGenerateContent)
	r.Post("/v1/models/{model}:generateContent", h.GenerateContent)
	r.Post("/v1/models/{model}:streamGenerateContent", h.StreamGenerateContent)
}

func (h *Handler) GenerateContent(w http.ResponseWriter, r *http.Request) {
	h.handleGenerateContent(w, r, false)
}

func (h *Handler) StreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	h.handleGenerateContent(w, r, true)
}
