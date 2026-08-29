package devcapture

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/dev/captures", h.getDevCaptures)
	r.Delete("/dev/captures", h.clearDevCaptures)
}
