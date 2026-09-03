package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestAntigravityService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Config{
		DataDir: t.TempDir(),
		DBName:  "test.db",
	}
	return New(cfg)
}

func TestModelPrefixHelpers(t *testing.T) {
	s := &Service{}
	s.settings = Settings{
		ModelPrefix: "agy-",
	}

	if p := s.modelPrefix(); p != "agy-" {
		t.Fatalf("expected prefix 'agy-', got %q", p)
	}

	prefixed := s.prefixModel("claude-3-5-sonnet")
	if prefixed != "agy-claude-3-5-sonnet" {
		t.Fatalf("expected agy-claude-3-5-sonnet, got %q", prefixed)
	}

	stripped := s.stripModelPrefix("agy-claude-3-5-sonnet")
	if stripped != "claude-3-5-sonnet" {
		t.Fatalf("expected claude-3-5-sonnet, got %q", stripped)
	}

	unprefixed := s.stripModelPrefix("claude-3-5-sonnet")
	if unprefixed != "claude-3-5-sonnet" {
		t.Fatalf("expected claude-3-5-sonnet, got %q", unprefixed)
	}

	list := s.prefixModelNames([]string{"m1", "m2"})
	if len(list) != 2 || list[0] != "agy-m1" || list[1] != "agy-m2" {
		t.Fatalf("unexpected prefixModelNames result: %v", list)
	}

	remapped := remapPrefixedName("old-model", "old-", "new-")
	if remapped != "new-model" {
		t.Fatalf("expected new-model, got %q", remapped)
	}
}

func TestBuildClaudeRequest(t *testing.T) {
	maxTokens := 1024
	temp := 0.7
	topP := 0.9
	req := &openAIChatRequest{
		Model:       "claude-3-5-sonnet",
		MaxTokens:   &maxTokens,
		Temperature: &temp,
		TopP:        &topP,
		Stream:      true,
		Messages: []openAIMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello world"},
		},
	}

	claude, err := buildClaudeRequest(req)
	if err != nil {
		t.Fatalf("buildClaudeRequest failed: %v", err)
	}
	if claude.Model != "claude-3-5-sonnet" {
		t.Fatalf("unexpected model: %s", claude.Model)
	}
	if claude.MaxTokens != 1024 {
		t.Fatalf("unexpected max tokens: %d", claude.MaxTokens)
	}
	if !claude.Stream {
		t.Fatalf("expected stream to be true")
	}
	if string(claude.System) != `"You are helpful."` {
		t.Fatalf("unexpected system prompt: %s", string(claude.System))
	}
	if len(claude.Messages) != 1 {
		t.Fatalf("expected 1 user message, got %d", len(claude.Messages))
	}
	if claude.Messages[0].Role != "user" {
		t.Fatalf("expected user role, got %s", claude.Messages[0].Role)
	}
}

func TestServeHTTP_StatusAndSettings(t *testing.T) {
	s := newTestAntigravityService(t)

	// GET /api/antigravity/status
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/antigravity/status", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint failed with code %d", rec.Code)
	}
	var statusMap map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusMap); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if statusMap["enabled"] != false {
		t.Fatalf("expected enabled=false initially")
	}

	// GET /api/antigravity/settings
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/antigravity/settings", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("settings endpoint failed with code %d", rec.Code)
	}

	// PUT /api/antigravity/settings
	patchBody := `{"enabled":true,"modelPrefix":"test-"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/antigravity/settings", strings.NewReader(patchBody))
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("put settings failed with code %d: %s", rec.Code, rec.Body.String())
	}
	st := s.Settings()
	if !st.Enabled || st.ModelPrefix != "test-" {
		t.Fatalf("settings not updated: %+v", st)
	}

	// Non-existent route
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/antigravity/unknown", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestServeHTTP_HandleOpenAIModels(t *testing.T) {
	s := newTestAntigravityService(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /v1/models, got %d", rec.Code)
	}
	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode models: %v", err)
	}
	if res["object"] != "list" {
		t.Fatalf("expected object=list, got %v", res["object"])
	}
}
