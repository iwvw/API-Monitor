package accounts

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/accounts", h.listAccounts)
	r.Post("/accounts", h.addAccount)
	r.Put("/accounts/{identifier}", h.updateAccount)
	r.Delete("/accounts/{identifier}", h.deleteAccount)
	r.Put("/accounts/{identifier}/enabled", h.toggleAccountEnabled)
	r.Post("/accounts/enabled/batch", h.batchToggleAccountEnabled)
	r.Put("/accounts/elastic-pool", h.updateElasticPool)
	r.Get("/queue/status", h.queueStatus)
	r.Post("/accounts/test", h.testSingleAccount)
	r.Post("/accounts/test-all", h.testAllAccounts)
	r.Post("/accounts/sessions/delete-all", h.deleteAllSessions)
	r.Post("/test", h.testAPI)
}

func RunAccountTestsConcurrently(accounts []config.Account, maxConcurrency int, testFn func(int, config.Account) map[string]any) []map[string]any {
	return runAccountTestsConcurrently(accounts, maxConcurrency, testFn)
}

func (h *Handler) TestAccount(ctx context.Context, acc config.Account, model, message string) map[string]any {
	return h.testAccount(ctx, acc, model, message)
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request)  { h.listAccounts(w, r) }
func (h *Handler) AddAccount(w http.ResponseWriter, r *http.Request)    { h.addAccount(w, r) }
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) { h.updateAccount(w, r) }
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) { h.deleteAccount(w, r) }
func (h *Handler) ToggleAccountEnabled(w http.ResponseWriter, r *http.Request) {
	h.toggleAccountEnabled(w, r)
}
func (h *Handler) BatchToggleAccountEnabled(w http.ResponseWriter, r *http.Request) {
	h.batchToggleAccountEnabled(w, r)
}
func (h *Handler) DeleteAllSessions(w http.ResponseWriter, r *http.Request) {
	h.deleteAllSessions(w, r)
}
