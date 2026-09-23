package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
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


// 选号：三种策略各自行为正确，且都跳过不可用、冷却与缺 ProjectID 的账号。
func TestPickForRelayStrategies(t *testing.T) {
	now := time.Now().Unix()
	newSvc := func(strategy string) *Service {
		s := &Service{
			cooldownUntil: map[string]time.Time{},
			quotaSnap:     accountpick.NewSnapshot(),
		}
		s.settings = Settings{
			AccountStrategy: strategy,
			Accounts: []Account{
				{Email: "a@x", AccessToken: "t", ProjectID: "p1", ExpiresAt: now + 3600},
				{Email: "b@x", AccessToken: "t", ProjectID: "p2", ExpiresAt: now + 3600},
			},
		}
		return s
	}

	// first：固定首个。
	s := newSvc(strategyFirst)
	if acc, ok := s.pickForRelay(nil); !ok || acc.Email != "a@x" {
		t.Fatalf("first 应取 a@x，得到 %v/%v", acc.Email, ok)
	}

	// round-robin：依次轮换。
	s = newSvc(strategyRoundRobin)
	seq := []string{}
	for i := 0; i < 4; i++ {
		a, ok := s.pickForRelay(nil)
		if !ok {
			t.Fatal("round-robin 应能选中")
		}
		seq = append(seq, a.Email)
	}
	want := []string{"a@x", "b@x", "a@x", "b@x"}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("round-robin 序列错误：%v want %v", seq, want)
		}
	}

	// least-used：选额度更高的。
	s = newSvc(strategyLeastUsed)
	s.quotaSnap.Set("a@x", 1)
	s.quotaSnap.Set("b@x", 9)
	if acc, _ := s.pickForRelay(nil); acc.Email != "b@x" {
		t.Fatalf("least-used 应选 b@x，得到 %v", acc.Email)
	}

	// 冷却账号被跳过；全冷却时回退最早恢复者。
	s = newSvc(strategyFirst)
	s.markCooldown("a@x", "boom")
	if acc, _ := s.pickForRelay(nil); acc.Email != "b@x" {
		t.Fatalf("冷却账号应被跳过，得到 %v", acc.Email)
	}
	if acc, ok := s.pickForRelay(map[string]bool{"b@x": true}); !ok || acc.Email != "a@x" {
		t.Fatalf("候选全冷却时应回退 a@x，得到 %v/%v", acc.Email, ok)
	}

	// 缺 ProjectID / 过期 / 停用一律不可选。
	s.mu.Lock()
	s.settings.Accounts = []Account{
		{Email: "noproj@x", AccessToken: "t", ExpiresAt: now + 3600},
		{Email: "expired@x", AccessToken: "t", ProjectID: "p", ExpiresAt: now - 10},
		{Email: "off@x", AccessToken: "t", ProjectID: "p", ExpiresAt: now + 3600, Disabled: true},
	}
	s.mu.Unlock()
	if _, ok := s.pickForRelay(nil); ok {
		t.Fatal("无可用账号应返回 false")
	}
}

// TestTokenStateAndAvailability 校验凭据状态判据与「可用」综合判据的一致性。
// 前端状态列与后端选号必须用同一判据，否则会出现「界面绿色可用但实际不被选中」。
func TestTokenStateAndAvailability(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name      string
		acc       Account
		wantState string
		wantAvail bool
	}{
		{"无 access token", Account{Email: "a@x"}, "unknown", false},
		{"无过期时刻", Account{Email: "a@x", AccessToken: "t"}, "unknown", true},
		{"有效", Account{Email: "a@x", AccessToken: "t", ExpiresAt: now + 2*3600}, "valid", true},
		{"即将过期", Account{Email: "a@x", AccessToken: "t", ExpiresAt: now + 600}, "expiring", true},
		{"已过期", Account{Email: "a@x", AccessToken: "t", ExpiresAt: now - 10}, "expired", false},
		{"已停用", Account{Email: "a@x", AccessToken: "t", ExpiresAt: now + 2*3600, Disabled: true}, "valid", false},
	}
	for _, c := range cases {
		if got := tokenState(c.acc); got != c.wantState {
			t.Errorf("%s: tokenState = %q, want %q", c.name, got, c.wantState)
		}
		if got := accountAvailable(c.acc); got != c.wantAvail {
			t.Errorf("%s: accountAvailable = %v, want %v", c.name, got, c.wantAvail)
		}
	}
}

// TestHandleStatusExcludesExpiredAccounts 过期凭据不计入 enabledCount，
// 否则前端会显示「已授权」但实际无法转发。
func TestHandleStatusExcludesExpiredAccounts(t *testing.T) {
	s := newTestAntigravityService(t)
	now := time.Now().Unix()
	s.mu.Lock()
	s.settings = Settings{
		Enabled: true,
		Accounts: []Account{
			{Email: "ok@x", AccessToken: "t", ProjectID: "p1", ExpiresAt: now + 2*3600},
			{Email: "stale@x", AccessToken: "t", ProjectID: "p2", ExpiresAt: now - 10},
			{Email: "disabled@x", AccessToken: "t", ProjectID: "p3", ExpiresAt: now + 2*3600, Disabled: true},
			{Email: "noproj@x", AccessToken: "t", ExpiresAt: now + 2*3600},
		},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/antigravity/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Authorized   bool `json:"authorized"`
		AccountCount int  `json:"accountCount"`
		EnabledCount int  `json:"enabledCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AccountCount != 4 {
		t.Fatalf("accountCount = %d, want 4", got.AccountCount)
	}
	if got.EnabledCount != 1 {
		t.Fatalf("enabledCount = %d, want 1（仅未过期且有 project 的账号）", got.EnabledCount)
	}
}

// TestHandleAccountsExposesTokenState 账号列表必须下发 tokenState/available/lastError，
// 前端据此显示「已过期」而非绿色「可用」。
func TestHandleAccountsExposesTokenState(t *testing.T) {
	s := newTestAntigravityService(t)
	now := time.Now().Unix()
	s.mu.Lock()
	s.settings = Settings{
		Accounts: []Account{{
			Email:       "stale@x",
			AccessToken: "t",
			RefreshToken: "r",
			ProjectID:   "p1",
			ExpiresAt:   now - 10,
			LastError:   "刷新 token 失败: invalid_grant",
		}},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	s.handleAccounts(rec, httptest.NewRequest(http.MethodGet, "/api/antigravity/accounts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts = %d, want 200", rec.Code)
	}
	var got struct {
		Accounts []accountView `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(got.Accounts))
	}
	a := got.Accounts[0]
	if a.TokenState != "expired" {
		t.Errorf("tokenState = %q, want expired", a.TokenState)
	}
	if a.Available {
		t.Error("available should be false for expired token")
	}
	if a.ExpiresInSeconds != 0 {
		t.Errorf("expiresInSeconds = %d, want 0（已过期不下发倒计时）", a.ExpiresInSeconds)
	}
	if !strings.Contains(a.LastError, "invalid_grant") {
		t.Errorf("lastError should surface refresh failure, got %q", a.LastError)
	}
}

// TestRefreshStaleAccountsSkipsValidAndDisabled 自动续期只处理
// 「未停用 + 有 refresh token + 非 valid」的账号；valid 与停用账号不该被触碰。
// 这里用不可达的 refresh token 触发失败路径，验证错误被写回 LastError。
func TestRefreshStaleAccountsSkipsValidAndDisabled(t *testing.T) {
	s := newTestAntigravityService(t)
	ctx := context.Background()
	now := time.Now().Unix()
	s.mu.Lock()
	s.settings = Settings{
		Accounts: []Account{
			{Email: "valid@x", AccessToken: "t", RefreshToken: "r", ExpiresAt: now + 2*3600},
			{Email: "disabled@x", AccessToken: "t", RefreshToken: "r", ExpiresAt: now - 10, Disabled: true},
			{Email: "norefresh@x", AccessToken: "t", ExpiresAt: now - 10},
		},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
	s.mu.Unlock()

	s.RefreshStaleAccounts(ctx)

	after := s.Settings()
	for _, a := range after.Accounts {
		if a.LastError != "" {
			t.Errorf("%s 不应被自动续期触碰，却写入了 LastError: %q", a.Email, a.LastError)
		}
		if a.ExpiresAt != now-10 && a.Email != "valid@x" {
			t.Errorf("%s 的过期时刻不应被改动", a.Email)
		}
	}
}



// TestPersistAccountMergesCredentialFieldsOnly 刷新落库不得回退用户对账号
// 其它字段的并发修改：persistAccount 只覆盖凭据与 LastError 字段。
func TestPersistAccountMergesCredentialFieldsOnly(t *testing.T) {
	s := newTestAntigravityService(t)
	ctx := context.Background()
	st := Settings{
		Accounts: []Account{{
			Email:        "a@x",
			Name:         "旧名",
			PlanType:     "Pro",
			ProjectID:    "p1",
			Disabled:     true,
			AccessToken:  "old",
			RefreshToken: "oldr",
			ExpiresAt:    100,
			LastError:    "旧错误",
		}},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}

	// 模拟刷新成功：只更新凭据 + 清空错误。调用方传入的 acc 是快照（Name/Disabled 为旧值）。
	if err := s.persistAccount(ctx, "a@x", &credentialUpdate{
		AccessToken:  "new",
		RefreshToken: "newr",
		ExpiresAt:    999,
		LastError:    "",
	}); err != nil {
		t.Fatal(err)
	}

	after := s.Settings().Accounts[0]
	if after.AccessToken != "new" || after.RefreshToken != "newr" || after.ExpiresAt != 999 {
		t.Fatalf("凭据字段应被更新，实际 %+v", after)
	}
	if after.LastError != "" {
		t.Errorf("LastError 应被清空，实际 %q", after.LastError)
	}
	// 非凭据字段必须原样保留。
	if after.Name != "旧名" || after.PlanType != "Pro" || after.ProjectID != "p1" || !after.Disabled {
		t.Errorf("非凭据字段不应被改动，实际 %+v", after)
	}
}

// TestPersistAccountFailureKeepsCredentials 刷新失败只写 LastError，
// 不得把已有凭据覆盖成空值。
func TestPersistAccountFailureKeepsCredentials(t *testing.T) {
	s := newTestAntigravityService(t)
	ctx := context.Background()
	st := Settings{
		Accounts: []Account{{
			Email:        "a@x",
			AccessToken:  "keep-me",
			RefreshToken: "keep-r",
			ExpiresAt:    4242,
		}},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := s.persistAccount(ctx, "a@x", &credentialUpdate{LastError: "invalid_grant"}); err != nil {
		t.Fatal(err)
	}

	after := s.Settings().Accounts[0]
	if after.AccessToken != "keep-me" || after.RefreshToken != "keep-r" || after.ExpiresAt != 4242 {
		t.Fatalf("失败路径不应改动凭据，实际 %+v", after)
	}
	if after.LastError != "invalid_grant" {
		t.Errorf("LastError = %q, want invalid_grant", after.LastError)
	}
}

// TestPersistAccountMissingEmail 账号不存在时返回错误而非静默成功。
func TestPersistAccountMissingEmail(t *testing.T) {
	s := newTestAntigravityService(t)
	if err := s.persistAccount(context.Background(), "nope@x", &credentialUpdate{LastError: "x"}); err == nil {
		t.Fatal("expected error for missing account")
	}
}
