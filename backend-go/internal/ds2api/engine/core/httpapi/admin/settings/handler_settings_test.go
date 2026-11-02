package settings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func newSettingsTestHandler(t *testing.T, raw string) (*Handler, chi.Router) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", raw)
	store := config.LoadStore()
	h := &Handler{
		Store: store,
		Pool:  account.NewPool(store),
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return h, r
}

func TestSettingsRoutes(t *testing.T) {
	_, r := newSettingsTestHandler(t, `{"runtime":{"stream_timeout_seconds":60}}`)

	// GET /settings
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// PUT /settings
	body := `{"runtime":{"stream_timeout_seconds":120}}`
	req = httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBoolFrom(t *testing.T) {
	if !BoolFrom(true) {
		t.Fatalf("expected true")
	}
	if BoolFrom(false) {
		t.Fatalf("expected false")
	}
	if !BoolFrom("true") {
		t.Fatalf("expected true for 'true'")
	}
	if BoolFrom(nil) {
		t.Fatalf("expected false for nil")
	}
}
