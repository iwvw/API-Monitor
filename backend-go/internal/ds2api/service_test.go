package ds2api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestDS2APIService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Config{
		DataDir: t.TempDir(),
		DBName:  "test.db",
	}
	return New(cfg)
}

func TestModelPrefixAndRemap(t *testing.T) {
	s := &Service{}
	s.settings = Settings{
		ModelPrefix: "ds2-",
	}

	if p := s.modelPrefix(); p != "ds2-" {
		t.Fatalf("expected ds2-, got %q", p)
	}

	prefixed := s.prefixModel("deepseek-chat")
	if prefixed != "ds2-deepseek-chat" {
		t.Fatalf("expected ds2-deepseek-chat, got %q", prefixed)
	}

	stripped := s.stripModelPrefix("ds2-deepseek-chat")
	if stripped != "deepseek-chat" {
		t.Fatalf("expected deepseek-chat, got %q", stripped)
	}

	unprefixed := s.stripModelPrefix("deepseek-chat")
	if unprefixed != "deepseek-chat" {
		t.Fatalf("expected deepseek-chat, got %q", unprefixed)
	}

	list := s.prefixModelNames([]string{"m1", "m2"})
	if len(list) != 2 || list[0] != "ds2-m1" || list[1] != "ds2-m2" {
		t.Fatalf("unexpected prefixed model names: %v", list)
	}

	remapped := remapPrefixedName("old-model", "old-", "new-")
	if remapped != "new-model" {
		t.Fatalf("expected new-model, got %q", remapped)
	}
}

func TestCallStatsTracking(t *testing.T) {
	s := &Service{
		callBase:    map[string]int64{"acc-1": 10},
		callPending: map[string]int64{},
	}

	if count := s.callDisplay("acc-1"); count != 10 {
		t.Fatalf("expected 10, got %d", count)
	}

	s.recordCall("acc-1")
	s.recordCall("acc-1")
	s.recordCall("acc-2")

	if count := s.callDisplay("acc-1"); count != 12 {
		t.Fatalf("expected 12, got %d", count)
	}
	if count := s.callDisplay("acc-2"); count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}

func TestServeHTTP_StatusAndSettings(t *testing.T) {
	s := newTestDS2APIService(t)

	// GET /api/ds2api/status
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ds2api/status", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// GET /api/ds2api/settings
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/ds2api/settings", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// PUT /api/ds2api/settings
	body := `{"enabled":false,"modelPrefix":"test-"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/ds2api/settings", strings.NewReader(body))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	st := s.Settings()
	if st.ModelPrefix != "test-" {
		t.Fatalf("expected modelPrefix test-, got %s", st.ModelPrefix)
	}
}

func TestIsLoopback(t *testing.T) {
	if !isLoopback("127.0.0.1:8080") {
		t.Fatalf("expected loopback true for 127.0.0.1:8080")
	}
	if !isLoopback("::1") {
		t.Fatalf("expected loopback true for ::1")
	}
	if isLoopback("192.168.1.1:80") {
		t.Fatalf("expected loopback false for 192.168.1.1:80")
	}
}
