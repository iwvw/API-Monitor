package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAPIRoutesRemainRegistered(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"u@example.com","password":"p"}]}`)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")

	app, err := NewApp()
	if err != nil {
		t.Fatalf("NewApp() error: %v", err)
	}
	routes, ok := app.Router.(chi.Routes)
	if !ok {
		t.Fatalf("app router does not expose chi routes: %T", app.Router)
	}

	got := map[string]bool{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[fmt.Sprintf("%s %s", method, route)] = true
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	for _, want := range []string{
		"GET /v1/models",
		"GET /v1/models/{model_id}",
		"POST /v1/chat/completions",
		"POST /v1/responses",
		"GET /v1/responses/{response_id}",
		"POST /v1/files",
		"GET /v1/files/{file_id}",
		"POST /v1/embeddings",
		"GET /models",
		"GET /models/{model_id}",
		"POST /chat/completions",
		"POST /responses",
		"GET /responses/{response_id}",
		"POST /files",
		"GET /files/{file_id}",
		"POST /embeddings",
		"GET /anthropic/v1/models",
		"POST /anthropic/v1/messages",
		"POST /anthropic/v1/messages/count_tokens",
		"POST /v1/messages",
		"POST /messages",
		"POST /v1/messages/count_tokens",
		"POST /messages/count_tokens",
		"POST /v1beta/models/{model}:generateContent",
		"POST /v1beta/models/{model}:streamGenerateContent",
		"POST /v1/models/{model}:generateContent",
		"POST /v1/models/{model}:streamGenerateContent",
		"POST /admin/login",
		"GET /admin/verify",
		"GET /admin/config",
		"POST /admin/config",
		"GET /admin/settings",
		"PUT /admin/settings",
		"POST /admin/settings/password",
		"POST /admin/config/import",
		"GET /admin/config/export",
		"POST /admin/keys",
		"PUT /admin/keys/{key}",
		"DELETE /admin/keys/{key}",
		"GET /admin/proxies",
		"POST /admin/proxies",
		"PUT /admin/proxies/{proxyID}",
		"DELETE /admin/proxies/{proxyID}",
		"POST /admin/proxies/test",
		"GET /admin/accounts",
		"POST /admin/accounts",
		"PUT /admin/accounts/{identifier}",
		"DELETE /admin/accounts/{identifier}",
		"PUT /admin/accounts/{identifier}/proxy",
		"PUT /admin/accounts/{identifier}/enabled",
		"POST /admin/accounts/enabled/batch",
		"GET /admin/queue/status",
		"POST /admin/accounts/test",
		"POST /admin/accounts/test-all",
		"POST /admin/accounts/sessions/delete-all",
		"POST /admin/import",
		"POST /admin/test",
		"POST /admin/dev/raw-samples/capture",
		"GET /admin/dev/raw-samples/query",
		"POST /admin/dev/raw-samples/save",
		"POST /admin/vercel/sync",
		"GET /admin/vercel/status",
		"POST /admin/vercel/status",
		"GET /admin/export",
		"GET /admin/dev/captures",
		"DELETE /admin/dev/captures",
		"GET /admin/chat-history",
		"GET /admin/chat-history/{id}",
		"DELETE /admin/chat-history",
		"DELETE /admin/chat-history/{id}",
		"PUT /admin/chat-history/settings",
		"GET /admin/version",
	} {
		if !got[want] {
			t.Fatalf("expected route %s to be registered", want)
		}
	}
}
