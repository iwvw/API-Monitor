package proxies

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/proxies", h.listProxies)
	r.Post("/proxies", h.addProxy)
	r.Put("/proxies/{proxyID}", h.updateProxy)
	r.Delete("/proxies/{proxyID}", h.deleteProxy)
	r.Post("/proxies/test", h.testProxy)
	r.Put("/accounts/{identifier}/proxy", h.updateAccountProxy)
}

func (h *Handler) AddProxy(w http.ResponseWriter, r *http.Request)    { h.addProxy(w, r) }
func (h *Handler) UpdateProxy(w http.ResponseWriter, r *http.Request) { h.updateProxy(w, r) }
func (h *Handler) DeleteProxy(w http.ResponseWriter, r *http.Request) { h.deleteProxy(w, r) }
func (h *Handler) TestProxy(w http.ResponseWriter, r *http.Request)   { h.testProxy(w, r) }
func (h *Handler) UpdateAccountProxy(w http.ResponseWriter, r *http.Request) {
	h.updateAccountProxy(w, r)
}
