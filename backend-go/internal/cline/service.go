// Package cline 实现模型网关「插件中心」的 Cline 插件：把 Cline（cline.bot）
// 账号封装成 OpenAI 兼容上游。
//
// ── 上游是什么 ──────────────────────────────────────────────
// Cline 的登录态是 **WorkOS OAuth2** 令牌，落到 `~/.cline/data/settings/providers.json`。
// 调模型用 `POST https://api.cline.bot/api/v1/chat/completions`（上游是
// OpenAI 兼容网关，`createOpenAICompatible` 把 baseURL 当根拼，路径里只有一个 v1）。
//
// ── 三个实测踩出来的硬约束（改之前先读）───────────────────────
//  1. **`workos:` 前缀必须原样带上**：`Authorization: Bearer workos:eyJ...`。
//     去掉前缀直接 401 —— 这个前缀是 Cline 用来区分 WorkOS 令牌与旧版令牌的标记。
//  2. **`X-CLIENT-TYPE: cline-sdk` 是免费池的硬门槛**：不带这个头时免费池模型
//     一律 403（`only available via Cline product surfaces`）。
//  3. **模型 id 里的池前缀不能剥**：`cline-free/`、`cline-pass/` 是**计费通道
//     选择器**，剥掉转发会 404 `model not found`。对外展示可剥，转发必须原样。
//
// ── 两个额度池 = 两套模型名单 ────────────────────────────────
// 同一个账号上挂着两个额度池（免费池 / ClinePass 订阅池），模型目录按池分组下发。
// 归池一律**按响应里的分组**判，不能按 id 前缀猜：免费池里有 2 条是别家裸 id
// （`z-ai/glm-5.3-flash` 等），按前缀猜会把它们错判进订阅池。
//
// 目录结构、表命名、link 语义与同类插件（raccoon/qoder/workbuddy）保持一致。
package cline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// internalKey 是插件内部固定调用密钥：仅在本机回环转发时注入，用户无需配置。
const internalKey = "sk-cline-internal"

// providerName 是中继面模型列表里的 owned_by 标识。
const providerName = "cline"

// Settings 是 Cline 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// ModelPrefix 对外模型名统一前缀（如 "cline-"）；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// ProxyPoolID 引用独立代理池插件的池；为空表示直连。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
	// DisabledModels 被停用的模型（对外名，含前缀）。
	DisabledModels []string `json:"disabledModels"`
	// AccountStrategy 是多账号选号策略：first（默认）/ round-robin。
	AccountStrategy string `json:"accountStrategy"`
	// FreeOnly 是「启用免费」模式开关的持久化偏好：开启时前端只显示免费池模型，
	// 非免费模型由前端批量写入 DisabledModels 实现真正停用；这里仅记录偏好。
	FreeOnly bool `json:"freeOnly"`
	// Accounts 账号凭据。含 token 与 refresh token，仅服务端可见。
	Accounts []Account `json:"accounts"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:         false,
		DisabledModels:  []string{},
		AccountStrategy: accountpick.First,
		Accounts:        []Account{},
	}
}

// Service 是 Cline 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	// accountsMu 串行化「读取整份设置 → 改账号列表 → 写回」的读改写序列。
	accountsMu sync.Mutex
	// tokenMu 串行化账号 token 刷新（refresh token 会轮换）。
	tokenMu sync.Mutex

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	// 实际用量：内存增量按「站点时区日期 × 账号 × 模型」累加，定期落盘。
	usagePending map[usageKey]*usageDelta
	usageMu      sync.Mutex

	// 账号失败冷却：accountID → 冷却截止时刻。纯内存态，重启即清空。
	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	// 站点时区缓存。
	locMu      sync.Mutex
	locCache   *time.Location
	locCacheAt time.Time

	// modelMu 保护模型目录缓存。
	modelMu     sync.Mutex
	models      []ModelInfo
	cacheAt     time.Time
	fetchFailAt time.Time

	// rr 是 round-robin 策略的轮询游标。
	rr accountpick.Cursor

	// login 是设备授权登录的 pending 表（内存、带过期）。
	login *loginManager

	externalPool ProxyPoolSelector
}

// ProxyPoolSelector 复用独立代理池选择器（由 server 注入）。
type ProxyPoolSelector interface {
	SelectProxy(ctx context.Context, poolID, sessionKey string) (string, error)
}

// SetProxyPoolSelector 注入独立代理池选择器。
func (s *Service) SetProxyPoolSelector(sel ProxyPoolSelector) {
	s.externalPool = sel
}

// New 构造服务并加载持久化设置。
func New(cfg config.Config) *Service {
	s := &Service{
		cfg:           cfg,
		store:         database.New(cfg),
		callBase:      map[string]int64{},
		callPending:   map[string]int64{},
		cooldownUntil: map[string]time.Time{},
		usagePending:  map[usageKey]*usageDelta{},
		login:         newLoginManager(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		s.loadSettings(ctx, db)
		s.loadCallStats(ctx, db)
		db.Close()
	}
	return s
}

// open 打开 DB 并幂等确保本插件所需表存在。
func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cline_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("cline ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cline_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("cline ensure call stats schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cline_usage_daily (
		day TEXT NOT NULL,
		account_id TEXT NOT NULL,
		model TEXT NOT NULL,
		requests INTEGER NOT NULL DEFAULT 0,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (day, account_id, model)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("cline ensure usage schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM cline_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO cline_settings (id, data) VALUES (1, ?)`, string(data))
		return
	}
	var cfg Settings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		cfg = defaultSettings()
	}
	if cfg.Accounts == nil {
		cfg.Accounts = []Account{}
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	cfg.AccountStrategy = normalizeStrategy(cfg.AccountStrategy)
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
}

// Settings 返回当前设置的副本。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.DisabledModels = append([]string(nil), s.settings.DisabledModels...)
	out.Accounts = append([]Account(nil), s.settings.Accounts...)
	return out
}

func normalizeStrategy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case accountpick.RoundRobin:
		return accountpick.RoundRobin
	default:
		return accountpick.First
	}
}

// SaveSettings 持久化设置。前缀变更时把停用名单归一化到新前缀命名空间。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.DisabledModels == nil {
		next.DisabledModels = []string{}
	}
	if next.Accounts == nil {
		next.Accounts = []Account{}
	}
	next.AccountStrategy = normalizeStrategy(next.AccountStrategy)
	s.mu.RLock()
	oldPrefix := s.settings.ModelPrefix
	s.mu.RUnlock()
	if next.ModelPrefix != oldPrefix {
		next.DisabledModels = remapNamespaceList(next.DisabledModels, next.ModelPrefix, next.ModelPrefix, oldPrefix)
	}

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	next.DisabledModels = dedupeSorted(next.DisabledModels)
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO cline_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	s.syncLinkedEndpointModels(ctx)
	return nil
}

func dedupeSorted(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// stripAnyPrefix 剥掉名字上命中的任一前缀（优先最长匹配）。
func stripAnyPrefix(name string, prefixes ...string) string {
	sorted := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p != "" {
			sorted = append(sorted, p)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, p := range sorted {
		if strings.HasPrefix(name, p) {
			return strings.TrimPrefix(name, p)
		}
	}
	return name
}

func remapNamespaceList(names []string, newPrefix string, prefixes ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, newPrefix+stripAnyPrefix(n, prefixes...))
	}
	return out
}

// findAccount 按账号 ID 取副本。
func (s *Service) findAccount(id string) (Account, bool) {
	id = strings.TrimSpace(id)
	for _, a := range s.Settings().Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

// upsertAccount 按账号 ID 新增或覆盖。整段读改写加 accountsMu。
func (s *Service) upsertAccount(ctx context.Context, acc Account) error {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	replaced := false
	for i := range st.Accounts {
		if st.Accounts[i].ID == acc.ID {
			st.Accounts[i] = acc
			replaced = true
			break
		}
	}
	if !replaced {
		st.Accounts = append(st.Accounts, acc)
	}
	if err := s.SaveSettings(ctx, st); err != nil {
		return err
	}
	// 账号落库（导入/登录/续期）后异步回源模型目录，避免等 10 分钟定时器。
	go func() {
		ensureCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s.ensureCatalog(ensureCtx)
	}()
	return nil
}

// removeAccount 按账号 ID 删除。
func (s *Service) removeAccount(ctx context.Context, id string) error {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	st := s.Settings()
	out := make([]Account, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		if a.ID != id {
			out = append(out, a)
		}
	}
	st.Accounts = out
	return s.SaveSettings(ctx, st)
}

// -----------------------------------------------------------------------------
// 模型前缀
// -----------------------------------------------------------------------------

func (s *Service) modelPrefix() string {
	return strings.TrimSpace(s.Settings().ModelPrefix)
}

func (s *Service) prefixModel(id string) string {
	p := s.modelPrefix()
	if p == "" || strings.HasPrefix(id, p) {
		return id
	}
	return p + id
}

func (s *Service) stripModelPrefix(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return strings.TrimPrefix(id, p)
}

// -----------------------------------------------------------------------------
// 站点时区与小工具
// -----------------------------------------------------------------------------

const siteLocCacheTTL = 5 * time.Minute

func (s *Service) siteLocation(ctx context.Context) *time.Location {
	s.locMu.Lock()
	defer s.locMu.Unlock()
	if s.locCache != nil && time.Since(s.locCacheAt) < siteLocCacheTTL {
		return s.locCache
	}
	loc := timeutil.LocationFromName("")
	if db, err := s.open(ctx); err == nil {
		loc = timeutil.LocationFromSettings(ctx, db)
		db.Close()
	}
	s.locCache, s.locCacheAt = loc, time.Now()
	return loc
}

func responseJSON(w http.ResponseWriter, statusOrPayload interface{}, payload ...interface{}) {
	status := http.StatusOK
	var body interface{} = statusOrPayload
	if len(payload) > 0 {
		if st, ok := statusOrPayload.(int); ok {
			status = st
			body = payload[0]
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeOpenAIError(w http.ResponseWriter, status int, message, errType string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	responseJSON(w, status, map[string]interface{}{
		"error": map[string]string{"message": message, "type": errType},
	})
}

// linkBaseURL 构造本插件中继的 loopback 基址。
func (s *Service) linkBaseURL() string {
	port := s.cfg.Port
	if port <= 0 {
		port = 3000
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/cline/v1"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// ServeHTTP 是 Cline 插件总入口：
//   - /api/cline/*：插件管理接口（会话鉴权）
//   - /api/cline/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/cline/settings":
		s.handleSettings(w, r)
	case path == "/api/cline/status":
		s.handleStatus(w, r)
	case path == "/api/cline/usage":
		s.handleUsage(w, r)
	case path == "/api/cline/models":
		s.handleModels(w, r)
	case path == "/api/cline/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/cline/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/cline/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/cline/models/toggle/"))
	case path == "/api/cline/accounts":
		s.handleAccounts(w, r)
	case path == "/api/cline/login/start":
		s.handleLoginStart(w, r)
	case path == "/api/cline/login/poll":
		s.handleLoginPoll(w, r)
	case path == "/api/cline/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/cline/accounts/import":
		s.handleImportAccounts(w, r)
	case strings.HasPrefix(path, "/api/cline/accounts/"):
		rest := strings.TrimPrefix(path, "/api/cline/accounts/")
		switch {
		case strings.HasSuffix(rest, "/toggle"):
			s.handleToggleAccount(w, r, strings.TrimSuffix(rest, "/toggle"))
		case strings.HasSuffix(rest, "/test"):
			s.handleTestAccount(w, r, strings.TrimSuffix(rest, "/test"))
		case strings.HasSuffix(rest, "/balance"):
			s.handleAccountBalance(w, r, strings.TrimSuffix(rest, "/balance"))
		case r.Method == http.MethodPut:
			s.handleUpdateAccount(w, r, rest)
		default:
			s.handleDeleteAccount(w, r, rest)
		}
	case path == "/api/cline/link":
		s.handleLink(w, r)
	default:
		stripped := strings.TrimPrefix(path, "/api/cline")
		if stripped == path || !strings.HasPrefix(stripped, "/v1") {
			http.NotFound(w, r)
			return
		}
		s.serveRelay(w, r, stripped)
	}
}

func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.publicSettings()})
	case http.MethodPut, http.MethodPost:
		var body Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
			return
		}
		// Accounts 含 token 不下发，回传会抹掉凭据；DisabledModels 由专用启停接口持有。
		s.accountsMu.Lock()
		defer s.accountsMu.Unlock()
		current := s.Settings()
		body.Accounts = current.Accounts
		body.DisabledModels = current.DisabledModels
		if err := s.SaveSettings(r.Context(), body); err != nil {
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		if !body.Enabled {
			s.unlinkIfDisabled(r.Context())
		}
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.publicSettings()})
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// publicSettings 返回可下发前端的设置（账号已脱敏）。
func (s *Service) publicSettings() map[string]interface{} {
	st := s.Settings()
	views := make([]AccountView, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		views = append(views, s.toAccountView(a))
	}
	return map[string]interface{}{
		"enabled":         st.Enabled,
		"modelPrefix":     st.ModelPrefix,
		"proxyPoolId":     st.ProxyPoolID,
		"disabledModels":  st.DisabledModels,
		"accountStrategy": normalizeStrategy(st.AccountStrategy),
		"freeOnly":        st.FreeOnly,
		"accounts":        views,
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	st := s.Settings()
	available, cooling := 0, 0
	now := time.Now()
	for _, a := range st.Accounts {
		if accountAvailable(a) {
			available++
		}
		if s.inCooldown(a.ID, now) {
			cooling++
		}
	}
	responseJSON(w, map[string]interface{}{
		"enabled":        st.Enabled,
		"upstreamReady":  true,
		"accountCount":   len(st.Accounts),
		"availableCount": available,
		"coolingCount":   cooling,
		"modelCount":     len(s.catalog()),
		"linkBaseUrl":    s.linkBaseURL(),
	})
}
