package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
)

type routeAliasAuthStub struct{}

func (routeAliasAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return nil, auth.ErrUnauthorized
}

func (routeAliasAuthStub) Release(_ *auth.RequestAuth) {}

func (routeAliasAuthStub) SetAccountMutedUntil(_ *auth.RequestAuth, _ float64) {}

func (routeAliasAuthStub) SetAccountBanned(_ *auth.RequestAuth, _ string) {}

func TestClaudeRouteAliasesDoNot404(t *testing.T) {
	h := &Handler{
		Auth: routeAliasAuthStub{},
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)

	paths := []string{
		"/anthropic/v1/messages",
		"/v1/messages",
		"/messages",
		"/anthropic/v1/messages/count_tokens",
		"/v1/messages/count_tokens",
		"/messages/count_tokens",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("expected route %s to be registered, got 404", path)
		}
	}
}
