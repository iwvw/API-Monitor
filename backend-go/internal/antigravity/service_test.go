package antigravity

import (
	"context"
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

	// 无可用账号（上游不可达）时，/v1/models 应返回非 2xx 而不是 200 + 空列表：
	// 网关侧会把「验证成功但空列表」当成真实空并清空已获取的模型。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 from /v1/models with no account, got %d", rec.Code)
	}
}

// TestRefreshLinkedEndpointModelsKeepsModelsOnUpstreamFailure 前缀变更触发
// refreshLinkedEndpointModels 时，若上游不可达（无可用账号），必须保留库中已有
// 的真实模型列表（迁移到新前缀），而不是覆盖成硬编码兜底或清空。
func TestRefreshLinkedEndpointModelsKeepsModelsOnUpstreamFailure(t *testing.T) {
	s := newTestAntigravityService(t)

	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 真实场景里 openai 模块创建完整表（含 model_mappings / disabled_models）；
	// antigravity 的 ensureOpenAIEndpointsTable 只是最小化兜底，这里建全字段表。
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS openai_endpoints (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key TEXT NOT NULL,
		headers TEXT, disabled_models TEXT, proxy_pool TEXT, proxy_batches TEXT,
		auto_switch INTEGER DEFAULT 0, proxy_enabled INTEGER DEFAULT 0, force_proxy INTEGER DEFAULT 0,
		rate_limit_retry_enabled INTEGER DEFAULT 1, rate_limit_retry_wait_seconds INTEGER DEFAULT 10,
		status TEXT DEFAULT 'unknown', enabled INTEGER DEFAULT 1, models TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, last_used DATETIME, last_checked DATETIME,
		sort_order INTEGER DEFAULT 0, priority INTEGER DEFAULT 0, weight INTEGER DEFAULT 100,
		models_url TEXT, pricing TEXT, proxy_pool_id TEXT, plugin_id TEXT, model_mappings TEXT
	)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	// 旧前缀 agy-：库中已有 agy-model1/agy-model2。
	_, err = db.ExecContext(ctx, `INSERT INTO openai_endpoints
		(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
		 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
		 rate_limit_retry_wait_seconds, status, enabled, models, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, 'Antigravity', 'http://127.0.0.1/v1', 'k', '[]', '[]', '[]', '[]',
			0, 0, 0, 1, 10, 'unknown', 1, ?, datetime('now'), NULL, 100, 'antigravity')`,
		linkedEndpointID, `["agy-model1","agy-model2"]`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// 无可用账号 → 上游拉取失败。改前缀 agy- → new-。
	s.mu.Lock()
	s.settings = Settings{
		Enabled:     true,
		ModelPrefix: "new-",
		ModelAliases: map[string]string{},
		DisabledModels: []string{},
	}
	s.mu.Unlock()

	// 直接调用此前缀变化触发的刷新逻辑（与 SaveSettings 路径一致）。
	s.refreshLinkedEndpointModels(ctx, "agy-")

	db, err = s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var modelsRaw string
	if err := db.QueryRowContext(ctx, "SELECT models FROM openai_endpoints WHERE id = ?", linkedEndpointID).Scan(&modelsRaw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(modelsRaw, "new-model1") || !strings.Contains(modelsRaw, "new-model2") {
		t.Fatalf("prefix change with upstream down must keep and migrate existing models, got %q", modelsRaw)
	}
	if strings.Contains(modelsRaw, "agy-") || strings.Contains(modelsRaw, "claude-sonnet") {
		t.Fatalf("models should be migrated off old prefix and not fall back to defaults, got %q", modelsRaw)
	}
}
