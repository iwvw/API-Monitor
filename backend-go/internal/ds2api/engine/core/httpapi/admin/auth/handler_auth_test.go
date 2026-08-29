package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestGetVercelConfigFallsBackToSavedConfig(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"vercel":{"token":"saved-token","project_id":"saved-project","team_id":"saved-team"}}`)
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	h := &Handler{Store: config.LoadStore()}

	rec := httptest.NewRecorder()
	h.getVercelConfig(rec, httptest.NewRequest(http.MethodGet, "/admin/vercel/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["has_token"] != true {
		t.Fatalf("expected saved token to be detected: %#v", payload)
	}
	if payload["token_source"] != "config" || payload["project_id"] != "saved-project" || payload["team_id"] != "saved-team" {
		t.Fatalf("unexpected preconfig payload: %#v", payload)
	}
	if payload["token_preview"] == "saved-token" {
		t.Fatal("token preview leaked the full token")
	}
}

func TestRequireAdminServesSPAForBrowserNavigation(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)

	fallbackCalled := false
	h := &Handler{
		Store: config.LoadStore(),
		WebUIFallback: func(w http.ResponseWriter, r *http.Request) bool {
			fallbackCalled = true
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("index.html"))
			return true
		},
	}
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called when auth fails")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	rec := httptest.NewRecorder()
	h.requireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !fallbackCalled {
		t.Fatal("WebUIFallback was not called")
	}
	if body := rec.Body.String(); body != "index.html" {
		t.Fatalf("body = %q, want %q", body, "index.html")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestRequireAdminReturns401ForAPIRequestWithoutAuth(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"]}`)
	h := &Handler{
		Store: config.LoadStore(),
		WebUIFallback: func(_ http.ResponseWriter, _ *http.Request) bool {
			t.Fatal("WebUIFallback should not be called for API requests")
			return false
		},
	}
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called when auth fails")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.Header.Set("Accept", "*/*")

	rec := httptest.NewRecorder()
	h.requireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["detail"] != "authentication required" {
		t.Fatalf("detail = %q, want %q", body["detail"], "authentication required")
	}
}

func TestRequireAdminReturns401ForPOSTEvenWithTextHTMLAccept(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"]}`)
	fallbackCalled := false
	h := &Handler{
		Store: config.LoadStore(),
		WebUIFallback: func(_ http.ResponseWriter, _ *http.Request) bool {
			fallbackCalled = true
			return true
		},
	}
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called when auth fails")
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/settings", nil)
	req.Header.Set("Accept", "text/html")

	rec := httptest.NewRecorder()
	h.requireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if fallbackCalled {
		t.Fatal("WebUIFallback should not be called for POST requests")
	}
}

func TestRequireAdminDoesNotFallbackWhenAuthorizationPresent(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	t.Setenv("DS2API_ADMIN_KEY", "test-admin-key")

	token, err := authn.CreateJWT(1)
	if err != nil {
		t.Fatalf("CreateJWT: %v", err)
	}

	h := &Handler{
		Store: config.LoadStore(),
		WebUIFallback: func(_ http.ResponseWriter, _ *http.Request) bool {
			t.Fatal("WebUIFallback should not be called when Authorization is present")
			return false
		},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("protected data"))
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/html")

	rec := httptest.NewRecorder()
	h.requireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "protected data" {
		t.Fatalf("body = %q, want %q", body, "protected data")
	}
}

func TestRequireAdminReturns401WhenFallbackNotSet(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"]}`)
	h := &Handler{
		Store: config.LoadStore(),
	}
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler should not be called when auth fails")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.Header.Set("Accept", "text/html")

	rec := httptest.NewRecorder()
	h.requireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["detail"] != "authentication required" {
		t.Fatalf("detail = %q, want %q", body["detail"], "authentication required")
	}
}
