package settings

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/settings", h.getSettings)
	r.Put("/settings", h.updateSettings)
	r.Post("/settings/password", h.updateSettingsPassword)
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request)    { h.getSettings(w, r) }
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) { h.updateSettings(w, r) }
func (h *Handler) UpdateSettingsPassword(w http.ResponseWriter, r *http.Request) {
	h.updateSettingsPassword(w, r)
}
func BoolFrom(v any) bool { return boolFrom(v) }
