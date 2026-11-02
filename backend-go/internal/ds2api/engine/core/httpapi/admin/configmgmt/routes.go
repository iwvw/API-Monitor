package configmgmt

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/config", h.getConfig)
	r.Post("/config", h.updateConfig)
	r.Post("/config/import", h.configImport)
	r.Get("/config/export", h.configExport)
	r.Get("/export", h.exportConfig)
	r.Post("/keys", h.addKey)
	r.Put("/keys/{key}", h.updateKey)
	r.Delete("/keys/{key}", h.deleteKey)
	r.Post("/import", h.batchImport)
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request)    { h.getConfig(w, r) }
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) { h.updateConfig(w, r) }
func (h *Handler) ConfigImport(w http.ResponseWriter, r *http.Request) { h.configImport(w, r) }
func (h *Handler) BatchImport(w http.ResponseWriter, r *http.Request)  { h.batchImport(w, r) }
func (h *Handler) AddKey(w http.ResponseWriter, r *http.Request)       { h.addKey(w, r) }
func (h *Handler) UpdateKey(w http.ResponseWriter, r *http.Request)    { h.updateKey(w, r) }
func (h *Handler) DeleteKey(w http.ResponseWriter, r *http.Request)    { h.deleteKey(w, r) }
