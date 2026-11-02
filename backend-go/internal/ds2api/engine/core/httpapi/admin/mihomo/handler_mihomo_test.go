package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

type mockBridge struct {
	status map[string]any
}

func (m *mockBridge) Supported() bool { return true }
func (m *mockBridge) Status() map[string]any {
	if m.status != nil {
		return m.status
	}
	return map[string]any{"running": true}
}
func (m *mockBridge) Apply(ctx context.Context) error { return nil }
func (m *mockBridge) BindAccount(ctx context.Context, identifier, nodeKey string) error {
	return nil
}
func (m *mockBridge) AddSubscription(ctx context.Context, name, rawURL string) (config.MihomoSubscription, error) {
	return config.MihomoSubscription{ID: "sub-1", Name: name, URL: rawURL}, nil
}
func (m *mockBridge) RefreshSubscription(ctx context.Context, subID string) (int, error) {
	return 5, nil
}
func (m *mockBridge) DeleteSubscription(ctx context.Context, subID string) error { return nil }
func (m *mockBridge) UpdateSettings(ctx context.Context, enabled bool, binaryPath string, basePort, apiPort int, autoBind bool) error {
	return nil
}
func (m *mockBridge) ListNodes() []map[string]any {
	return []map[string]any{{"name": "Node A", "key": "k1"}}
}
func (m *mockBridge) TestLatency(ctx context.Context) ([]map[string]any, error) {
	return []map[string]any{{"key": "k1", "delay": 50}}, nil
}
func (m *mockBridge) AssignAccounts(ctx context.Context, nodeKeys []string) (int, error) {
	return 1, nil
}
func (m *mockBridge) DownloadInfo() map[string]any {
	return map[string]any{"downloading": false}
}
func (m *mockBridge) StartBinaryDownload(ctx context.Context) error { return nil }

func setupRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return r
}

func TestMihomoRoutes(t *testing.T) {
	bridge := &mockBridge{
		status: map[string]any{"running": true, "nodes": 1},
	}
	h := &Handler{Bridge: bridge}
	r := setupRouter(h)

	// GET /mihomo/status
	req := httptest.NewRequest(http.MethodGet, "/mihomo/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// GET /mihomo/nodes
	req = httptest.NewRequest(http.MethodGet, "/mihomo/nodes", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// PUT /mihomo/settings
	body := `{"enabled": true, "base_port": 10000}`
	req = httptest.NewRequest(http.MethodPut, "/mihomo/settings", strings.NewReader(body))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Nil bridge should return 503
	hNil := &Handler{Bridge: nil}
	rNil := setupRouter(hNil)
	req = httptest.NewRequest(http.MethodGet, "/mihomo/status", nil)
	rec = httptest.NewRecorder()
	rNil.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
