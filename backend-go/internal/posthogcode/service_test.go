package posthogcode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// newTestService 构造一个不落库的最小 Service（仅用于选号逻辑测试）。
func newTestService(accounts []Account, strategy string) *Service {
	s := &Service{
		cfg:              config.Config{},
		cooldownUntil:    map[string]time.Time{},
		quotaSnap:        map[string]float64{},
		quotaRefreshedAt: map[string]time.Time{},
	}
	s.settings = Settings{
		Enabled:         true,
		Region:          "us",
		Product:         "posthog_code",
		AccountStrategy: strategy,
		Accounts:        accounts,
		DisabledModels:  []string{},
	}
	return s
}

func acc(id string) Account {
	return Account{
		ID:          id,
		Email:       id + "@example.com",
		Region:      "us",
		AccessToken: "pha_" + id,
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
		Scope:       "llm_gateway:read project:read",
	}
}

func ptr(v float64) *float64 { return &v }

func TestPickAccountFirst(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b"), acc("c")}, strategyFirst)
	for i := 0; i < 5; i++ {
		got, ok := s.pickAccount("", nil)
		if !ok || got.ID != "a" {
			t.Fatalf("first 策略应恒选 a，得到 %q ok=%v", got.ID, ok)
		}
	}
}

func TestPickAccountRoundRobin(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b"), acc("c")}, strategyRoundRobin)
	seen := []string{}
	for i := 0; i < 6; i++ {
		got, ok := s.pickAccount("", nil)
		if !ok {
			t.Fatalf("第 %d 次应选到账号", i)
		}
		seen = append(seen, got.ID)
	}
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("轮询序列不符：得到 %v，期望 %v", seen, want)
		}
	}
}

// 候选集缩短时游标不应回绕：这是 ID 锚定相对下标取模的核心修复。
func TestPickAccountRoundRobinShrinksGracefully(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b"), acc("c")}, strategyRoundRobin)

	// 依次选中 a、b。
	if got, _ := s.pickAccount("", nil); got.ID != "a" {
		t.Fatalf("第 1 次应为 a，得到 %q", got.ID)
	}
	if got, _ := s.pickAccount("", nil); got.ID != "b" {
		t.Fatalf("第 2 次应为 b，得到 %q", got.ID)
	}

	// b 进入冷却 → 候选集变为 {a, c}。上次选中 b，应接着选 c，而不是回绕到 a。
	s.markCooldown("b", "test")
	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "c" {
		t.Fatalf("b 冷却后应接着选 c（不回绕），得到 %q ok=%v", got.ID, ok)
	}
}

// 上次选中的账号被停用时，应从头开始而不是卡住。
func TestPickAccountRoundRobinLastDisabled(t *testing.T) {
	a, b, c := acc("a"), acc("b"), acc("c")
	s := newTestService([]Account{a, b, c}, strategyRoundRobin)
	if got, _ := s.pickAccount("", nil); got.ID != "a" {
		t.Fatalf("第 1 次应为 a")
	}
	// a 停用：候选集 {b, c}，上次是 a 不在集合内，从头（b）开始。
	st := s.Settings()
	st.Accounts[0].Disabled = true
	s.settings = st
	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "b" {
		t.Fatalf("上次账号停用后应从头选 b，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickAccountLeastUsed(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b"), acc("c")}, strategyLeastUsed)
	// a 剩 10、b 剩 300、c 剩 50 → 应恒选 b
	s.recordQuotaSnapshot("a", ptr(1990.0), ptr(2000.0))
	s.recordQuotaSnapshot("b", ptr(1700.0), ptr(2000.0))
	s.recordQuotaSnapshot("c", ptr(1950.0), ptr(2000.0))

	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "b" {
		t.Fatalf("least-used 应选剩余最多的 b，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickAccountLeastUsedNoData(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b")}, strategyLeastUsed)
	// 无快照时全部按 0，取列表序首个。
	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "a" {
		t.Fatalf("无额度数据时应回落列表序，得到 %q ok=%v", got.ID, ok)
	}
}

// 快照过期后应视为无数据，退化为列表序，而不是基于陈旧值决策。
func TestPickAccountLeastUsedStaleSnapshot(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b")}, strategyLeastUsed)
	// a 快照很少（应劣后），但时间戳已过期 → 失效 → 回落列表序选 a。
	s.quotaMu.Lock()
	s.quotaSnap["a"] = 1
	s.quotaSnap["b"] = 999
	s.quotaAt = time.Now().Add(-quotaSnapshotTTL - time.Minute)
	s.quotaMu.Unlock()

	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "a" {
		t.Fatalf("快照过期应回落列表序选 a，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickAccountSkipsTried(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b")}, strategyFirst)
	got, ok := s.pickAccount("", map[string]bool{"a": true})
	if !ok || got.ID != "b" {
		t.Fatalf("应跳过已尝试的 a，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickAccountSkipsDisabled(t *testing.T) {
	disabled := acc("a")
	disabled.Disabled = true
	s := newTestService([]Account{disabled, acc("b")}, strategyFirst)
	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "b" {
		t.Fatalf("应跳过已停用的 a，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickAccountCooldownFallback(t *testing.T) {
	s := newTestService([]Account{acc("a"), acc("b")}, strategyFirst)
	s.markCooldown("a", "test")
	s.markCooldown("b", "test")
	// 全冷却时仍应选出最早恢复的（这里是 a，先标记）。
	got, ok := s.pickAccount("", nil)
	if !ok || got.ID != "a" {
		t.Fatalf("全冷却应回落最早恢复的 a，得到 %q ok=%v", got.ID, ok)
	}
}

func TestRecordQuotaSnapshotClampsNegative(t *testing.T) {
	s := newTestService(nil, strategyLeastUsed)
	// 已用超过上限：剩余记 0，不出现负值。
	s.recordQuotaSnapshot("a", ptr(2100.0), ptr(2000.0))
	s.quotaMu.RLock()
	got := s.quotaSnap["a"]
	s.quotaMu.RUnlock()
	if got != 0 {
		t.Fatalf("超额时剩余应为 0，得到 %v", got)
	}
}

func TestNormalizeStrategy(t *testing.T) {
	cases := map[string]string{
		"":             strategyFirst,
		"first":        strategyFirst,
		"round-robin":  strategyRoundRobin,
		"ROUND-ROBIN":  strategyRoundRobin,
		" least-used ": strategyLeastUsed,
		"bogus":        strategyFirst,
	}
	for in, want := range cases {
		if got := normalizeStrategy(in); got != want {
			t.Errorf("normalizeStrategy(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestNormalizeRegion(t *testing.T) {
	cases := map[string]string{
		"":     "",
		"us":   "us",
		"US":   "us",
		" eu ": "eu",
		"cn":   "",
	}
	for in, want := range cases {
		if got := normalizeRegion(in); got != want {
			t.Errorf("normalizeRegion(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 账号级区域优先于全局设置，使 us/eu 账号可共存。
func TestAccountRegionPrefersAccountLevel(t *testing.T) {
	s := newTestService([]Account{acc("a")}, strategyFirst)
	s.settings.Region = "us"

	euAcc := acc("eu-acc")
	euAcc.Region = "eu"
	if got := s.accountRegion(euAcc); got != "eu" {
		t.Fatalf("账号级区域应优先，得到 %q", got)
	}

	// 账号级为空时回落全局。
	legacy := acc("legacy")
	legacy.Region = ""
	if got := s.accountRegion(legacy); got != "us" {
		t.Fatalf("账号级为空应回落全局 us，得到 %q", got)
	}
}

// newDBService 构造带临时库的 Service，用于验证设置持久化语义。
func newDBService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

// TestHandleSettingsPreservesServerOwnedFields 整对象 PUT 必须回填服务端拥有
// 的字段，否则 settings 保存会把账号清空、用户模型启停重置、首次初始化标记清零。
func TestHandleSettingsPreservesServerOwnedFields(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	seed := s.Settings()
	seed.Accounts = []Account{{ID: "a1", Email: "a1@x", AccessToken: "t", RefreshToken: "r"}}
	seed.DisabledModels = []string{"claude-opus"}
	seed.ModelsInitialized = true
	if err := s.SaveSettings(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// 前端 PUT 只带用户可编辑字段（模拟 publicSettings 回传，不含 accounts 等）。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/posthogcode/settings",
		strings.NewReader(`{"enabled":true,"region":"us","product":"posthog_code","freeTierOnly":true,"accountStrategy":"first"}`))
	s.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings PUT = %d, body=%s", rec.Code, rec.Body.String())
	}

	after := s.Settings()
	if len(after.Accounts) != 1 || after.Accounts[0].ID != "a1" {
		t.Fatalf("账号应被保留，实际 %+v", after.Accounts)
	}
	if len(after.DisabledModels) != 1 || after.DisabledModels[0] != "claude-opus" {
		t.Fatalf("用户模型启停应被保留，实际 %+v", after.DisabledModels)
	}
	if !after.ModelsInitialized {
		t.Fatal("modelsInitialized 应被保留为 true，否则下次拉取会重置用户选择")
	}
	if !after.FreeTierOnly {
		t.Fatal("用户可编辑字段 freeTierOnly 应生效")
	}
}

// TestApplyInitialModelDefaultsRunsOnce 首次初始化只跑一次：
// 再次调用不得覆盖用户已做的启停选择。
func TestApplyInitialModelDefaultsRunsOnce(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	models := []ModelInfo{{ID: freeTierModels[0]}, {ID: "anthropic/claude-fable"}}
	s.applyInitialModelDefaults(ctx, models)

	first := s.Settings()
	if !first.ModelsInitialized {
		t.Fatal("首次调用应置 modelsInitialized = true")
	}
	if len(first.DisabledModels) != 1 || first.DisabledModels[0] != "anthropic/claude-fable" {
		t.Fatalf("首次初始化应只停用受限模型，实际 %+v", first.DisabledModels)
	}

	// 用户改为全部启用后再次触发初始化，不应被回退。
	st := s.Settings()
	st.DisabledModels = []string{}
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	s.applyInitialModelDefaults(ctx, models)
	if got := s.Settings().DisabledModels; len(got) != 0 {
		t.Fatalf("已初始化后不得重置用户选择，实际 %+v", got)
	}
}

// TestImportAccountsSkipsMissingRefreshToken 只有 access token 的条目应被跳过：
// 过期后无法续期，导入即成死账号。导入幂等（重复导入 added=0）。
func TestImportAccountsSkipsMissingRefreshToken(t *testing.T) {
	s := newDBService(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posthogcode/accounts/import",
		strings.NewReader(`{"accounts":[
			{"id":"ok","accessToken":"at","refreshToken":"rt"},
			{"id":"no-refresh","accessToken":"at"}
		]}`))
	s.handleImportAccounts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d, body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		Added int `json:"added"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 {
		t.Fatalf("added = %d, want 1（缺 refresh token 的条目应被跳过）", res.Added)
	}
	if got := len(s.Settings().Accounts); got != 1 {
		t.Fatalf("账号数 = %d, want 1", got)
	}

	// 重复导入同一账号应幂等。
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/posthogcode/accounts/import",
		strings.NewReader(`{"accounts":[{"id":"ok","accessToken":"at","refreshToken":"rt"}]}`))
	s.handleImportAccounts(rec2, req2)
	var res2 struct {
		Added int `json:"added"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if res2.Added != 0 {
		t.Fatalf("重复导入 added = %d, want 0", res2.Added)
	}
}

// TestHandleAuthURLUsesRegisteredRedirectURI 授权链接里的 redirect_uri 必须
// 是 OAuth 应用注册值，不能采用请求体传入的地址。回归「前端按面板所在源
// 推导回调地址」导致的 "Mismatching redirect URI"。
func TestHandleAuthURLUsesRegisteredRedirectURI(t *testing.T) {
	s := newDBService(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posthogcode/oauth/auth-url",
		strings.NewReader(`{"region":"us","redirectUri":"https://dsukhub.com/callback"}`))
	s.handleAuthURL(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth-url = %d, body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		URL         string `json:"url"`
		State       string `json:"state"`
		RedirectURI string `json:"redirectUri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.RedirectURI != defaultRedirectURI {
		t.Fatalf("redirectUri = %q, want %q（必须用注册值）", res.RedirectURI, defaultRedirectURI)
	}
	if strings.Contains(res.URL, "dsukhub.com") {
		t.Fatalf("授权链接不得包含前端传入的域名: %s", res.URL)
	}
	u, err := url.Parse(res.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("redirect_uri"); got != defaultRedirectURI {
		t.Fatalf("授权链接 redirect_uri = %q, want %q", got, defaultRedirectURI)
	}
	if got := u.Query().Get("state"); got != res.State {
		t.Fatalf("授权链接 state = %q, 应与返回的 state 一致", got)
	}

	// 会话里必须记下同一个 redirect_uri，供换码时使用。
	sess := s.takeOAuthState(res.State)
	if sess == nil {
		t.Fatal("授权会话应已建立")
	}
	if sess.redirectURI != defaultRedirectURI {
		t.Fatalf("会话 redirectURI = %q, want %q", sess.redirectURI, defaultRedirectURI)
	}
}

// TestCallbackRedirectURIEnvOverride 允许部署方用环境变量覆盖注册回调地址。
func TestCallbackRedirectURIEnvOverride(t *testing.T) {
	t.Setenv(envRedirectURI, "https://example.com/cb")
	if got := callbackRedirectURI(); got != "https://example.com/cb" {
		t.Fatalf("callbackRedirectURI = %q, want env value", got)
	}
	t.Setenv(envRedirectURI, "")
	if got := callbackRedirectURI(); got != defaultRedirectURI {
		t.Fatalf("空环境变量应回落默认值，得到 %q", got)
	}
}

// TestHandleAuthURLCarriesReauthAccountID 重新授权时前端带上 accountId，
// 授权会话必须记住它，换码成功后据此替换该既有账号而不是新建。
func TestHandleAuthURLCarriesReauthAccountID(t *testing.T) {
	s := newDBService(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posthogcode/oauth/auth-url",
		strings.NewReader(`{"region":"eu","accountId":"acc-123"}`))
	s.handleAuthURL(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth-url = %d, body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		State  string `json:"state"`
		Region string `json:"region"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Region != "eu" {
		t.Fatalf("region = %q, want eu（重新授权必须用该账号区域）", res.Region)
	}
	sess := s.takeOAuthState(res.State)
	if sess == nil {
		t.Fatal("授权会话应已建立")
	}
	if sess.accountID != "acc-123" {
		t.Fatalf("会话 accountID = %q, want acc-123", sess.accountID)
	}
}

// TestHandleAuthURLWithoutAccountIDCreatesNew 不带 accountId 时是新增账号，
// 会话不得锚定任何既有账号。
func TestHandleAuthURLWithoutAccountIDCreatesNew(t *testing.T) {
	s := newDBService(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/posthogcode/oauth/auth-url",
		strings.NewReader(`{"region":"us"}`))
	s.handleAuthURL(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth-url = %d, body=%s", rec.Code, rec.Body.String())
	}
	var res struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	sess := s.takeOAuthState(res.State)
	if sess == nil {
		t.Fatal("授权会话应已建立")
	}
	if sess.accountID != "" {
		t.Fatalf("新增账号会话 accountID 应为空，得到 %q", sess.accountID)
	}
}

// TestMarkRevokedOnAuthFailureDisablesAccount 刷新失败若源于凭据被 PostHog
// 吊销（authentication_error/invalid_grant 等），该账号应被自动停用并记录
// LastError，使选号策略跳过它（故障转移用正常账号），而不是短暂冷却后重试。
func TestMarkRevokedOnAuthFailureDisablesAccount(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	acc := Account{
		ID:           "a1",
		Email:        "a1@x",
		Region:       "us",
		AccessToken:  "old",
		RefreshToken: "dead",
		ExpiresAt:    time.Now().Add(2 * time.Hour).Unix(),
		Scope:        "llm_gateway:read project:read",
	}
	if err := s.upsertAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}

	err := fmt.Errorf(`token 端点返回 400: {"type":"authentication_error","code":"authentication_failed","detail":"Invalid access token."}`)
	s.markRevokedOnAuthFailure(ctx, &acc, err)

	if !acc.Disabled {
		t.Fatal("凭据被吊销后账号应被停用")
	}
	if acc.LastError == "" || !strings.Contains(acc.LastError, "重新授权") {
		t.Fatalf("LastError 应提示需重新授权，得到 %q", acc.LastError)
	}
	// 选号策略应跳过该账号（无候选可选中）。
	st := s.Settings()
	if len(st.Accounts) != 1 || !st.Accounts[0].Disabled {
		t.Fatalf("账号应被持久化为停用，实际 %+v", st.Accounts)
	}
}

// TestMarkRevokedOnAuthFailureKeepsTransient 瞬时/可重试失败（5xx、超时等）
// 不得停用账号，应走冷却重试而不是判成永久吊销。
func TestMarkRevokedOnAuthFailureKeepsTransient(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	a := Account{ID: "a1", Email: "a1@x", Region: "us", Scope: "llm_gateway:read"}
	if err := s.upsertAccount(ctx, a); err != nil {
		t.Fatal(err)
	}

	for _, msg := range []string{
		"token 端点返回 500: internal error",
		"请求 token 端点失败: connection reset",
		"token 端点返回 429: too many requests",
	} {
		acc := a
		s.markRevokedOnAuthFailure(ctx, &acc, fmt.Errorf("%s", msg))
		if acc.Disabled {
			t.Fatalf("瞬时失败 %q 不应停用账号", msg)
		}
	}
	if st := s.Settings(); len(st.Accounts) != 1 || st.Accounts[0].Disabled {
		t.Fatalf("瞬时失败不应持久化停用，实际 %+v", st.Accounts)
	}
}

// TestRefreshLinkedEndpointModelsSinglePrefix 端点 models 列必须带恰好一次前缀。
// 回归「prefixModelNames(visibleModels())」把前缀叠成 phc-phc- 的问题。
func TestRefreshLinkedEndpointModelsSinglePrefix(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	// 注入模型目录缓存（catalog 回落到 modelCache，无需真实账号）。
	s.modelMu.Lock()
	s.modelCache = []ModelInfo{{ID: "@cf/zai-org/glm-5.2"}, {ID: "moonshotai/kimi-k3"}}
	s.modelMu.Unlock()

	// 设置前缀并写库。
	st := s.Settings()
	st.ModelPrefix = "phc-"
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}

	// 播种 linked endpoint（最小化建表 + 插入）。
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO openai_endpoints
		(id, name, base_url, api_key, enabled, models, plugin_id)
		VALUES (?, 'PostHog Code', 'http://127.0.0.1:3000/api/posthogcode/v1', 'k', 1, '[]', 'posthogcode')`, linkedEndpointID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	s.refreshLinkedEndpointModels(ctx)

	db, err = s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var modelsRaw string
	if err := db.QueryRowContext(ctx, "SELECT models FROM openai_endpoints WHERE id = ?", linkedEndpointID).Scan(&modelsRaw); err != nil {
		t.Fatal(err)
	}
	var models []string
	if err := json.Unmarshal([]byte(modelsRaw), &models); err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		if strings.HasPrefix(m, "phc-phc-") {
			t.Errorf("端点模型出现双前缀: %q", m)
		}
		if !strings.HasPrefix(m, "phc-") {
			t.Errorf("端点模型应带一次前缀 phc-: %q", m)
		}
	}
}

// TestHandleSettingsDisableUnlinksEndpoint 关闭插件（enabled=false）保存后，
// 已接入的 internal 网关端点行应被自动删除，避免端点仍暴露在列表。
func TestHandleSettingsDisableUnlinksEndpoint(t *testing.T) {
	ctx := context.Background()
	s := newDBService(t)

	// 播种一个已接入的 internal 端点行（模拟先 link 过）。
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO openai_endpoints
		(id, name, base_url, api_key, enabled, models, plugin_id)
		VALUES (?, 'PostHog Code', 'http://127.0.0.1:3000/api/posthogcode/v1', 'k', 1, '[]', 'posthogcode')`, linkedEndpointID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// 前端 PUT enabled=false。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/posthogcode/settings",
		strings.NewReader(`{"enabled":false,"region":"us","product":"posthog_code"}`))
	s.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings PUT = %d, body=%s", rec.Code, rec.Body.String())
	}
	if s.Settings().Enabled {
		t.Fatal("enabled 应被保存为 false")
	}

	// 端点行应被删除。
	db, err = s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", linkedEndpointID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("关闭插件后 internal 端点应被删除，实际存在 %d 行", n)
	}
}
