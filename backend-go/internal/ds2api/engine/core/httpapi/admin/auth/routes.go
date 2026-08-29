package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return h.requireAdmin(next)
}

func RegisterPublicRoutes(r chi.Router, h *Handler) {
	r.Post("/login", h.login)
	r.Get("/verify", h.verify)
}

func RegisterProtectedRoutes(r chi.Router, h *Handler) {
	r.Get("/vercel/config", h.getVercelConfig)
}
