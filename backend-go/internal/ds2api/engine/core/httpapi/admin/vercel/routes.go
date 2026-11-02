package vercel

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/vercel/sync", h.syncVercel)
	r.Get("/vercel/status", h.vercelStatus)
	r.Post("/vercel/status", h.vercelStatus)
}
