// Package zcode 实现模型网关「插件中心」的 ZCode 插件：把 ZCode（智谱 / Z.AI
// 的编码代理客户端）的订阅登录态封装成 OpenAI 兼容上游。
//
// ── 上游是什么 ──────────────────────────────────────────────
// ZCode 的「编码套餐」（Coding Plan）可以用**订阅登录态**调用，不必去开放平台
// 申请 API Key。推理平面是标准 OpenAI 兼容协议：
//
//	国内  https://open.bigmodel.cn/api/coding/paas/v4/chat/completions
//	国际  https://api.z.ai/api/coding/paas/v4/chat/completions
//
// 登录、领取与余额都发生在 ZCode 自己的服务端 `https://zcode.z.ai`（**两地相同**，
// 靠 OAuth 的 provider 字段区分 bigmodel / zai）。
//
// ── 为什么用「粘贴凭证」而不是网页登录 ────────────────────────
// ZCode 的网页登录是**服务端中介的 CLI 轮询**（`/oauth/cli/init` + `/oauth/cli/poll`），
// 且授权页必须补一个「中转页」参数（国际版 `redirect_uri`、国内版 `redirect`，
// 参数名两地不同）—— 写错那一地的登录会静默停在 pending 直到超时。
// 参考实现（Agent2Providers 的 zcode/oauth.rs）明确记着这条链没有本地回调。
// 因此本插件只提供「粘贴 access_token / jwt」这一条稳定入口，网页登录留待后续。
//
// ── 两个凭证字段不能互相替代 ────────────────────────────────
//   - `accessToken`：推理平面用（打 open.bigmodel.cn / api.z.ai）；
//   - `jwt`：ZCode 套餐令牌（打 zcode.z.ai 的领取接口）。
//
// 只存 accessToken 时领取功能不可用，反之推理不可用。
//
// ── 每日签到 = 领当天那份赠送套餐 ────────────────────────────
// ZCode 没有独立的签到接口：上游每天下发一份**带日期段的新套餐**
// （`zcode-v3-start-plan-<日期>`），在客户端里点一下就领，那一下就是这一家的
// 「签到」。同一期再领回 `1003 already claimed`。
package zcode

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

// internalKey 是插件内部固定调用密钥：仅在本机回环转发时注入。
const internalKey = "sk-zcode-internal"

// providerName 是中继面模型列表里的 owned_by 标识。
const providerName = "zcode"

// 区域常量。
const (
	regionCN   = "cn"
	regionIntl = "intl"
)

// zcodeOrigin 是 zcode 平面基址（登录 / 领取 / 余额；两地相同）。
const zcodeOrigin = "https://zcode.z.ai"

// openAIBaseFor 返回区域的推理平面基址（OpenAI 兼容，不含尾斜杠）。
func openAIBaseFor(region string) string {
	if normalizeRegion(region) == regionIntl {
		return "https://api.z.ai/api/coding/paas/v4"
	}
	return "https://open.bigmodel.cn/api/coding/paas/v4"
}

// normalizeRegion 归一区域（空值按国内版，与存量账号兼容）。
func normalizeRegion(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), regionIntl) {
		return regionIntl
	}
	return regionCN
}

// regionLabel 返回区域中文名。
func regionLabel(region string) string {
	if normalizeRegion(region) == regionIntl {
		return "国际版"
	}
	return "国内版"
}

// upstreamProvider 返回 OAuth / 领取链路里的 provider 取值。
func upstreamProvider(region string) string {
	if normalizeRegion(region) == regionIntl {
		return "zai"
	}
	return "bigmodel"
}

// Settings 是 ZCode 插件的持久化配置。
type Settings struct {
	Enabled         bool      `json:"enabled"`
	ModelPrefix     string    `json:"modelPrefix,omitempty"`
	ProxyPoolID     string    `json:"proxyPoolId,omitempty"`
	DisabledModels  []string  `json:"disabledModels"`
	AccountStrategy string    `json:"accountStrategy"`
	AutoCheckin     bool      `json:"autoCheckin"`
	Accounts        []Account `json:"accounts"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:         false,
		DisabledModels:  []string{},
		AccountStrategy: accountpick.First,
		AutoCheckin:     true,
		Accounts:        []Account{},
	}
}

// Service 是 ZCode 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	accountsMu sync.Mutex
	tokenMu    sync.Mutex

	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	usagePending map[usageKey]*usageDelta
	usageMu      sync.Mutex

	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	locMu      sync.Mutex
	locCache   *time.Location
	locCacheAt time.Time

	checkinOnce sync.Once

	rr accountpick.Cursor

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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS zcode_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("zcode ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS zcode_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("zcode ensure call stats schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS zcode_usage_daily (
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
		return nil, fmt.Errorf("zcode ensure usage schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM zcode_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO zcode_settings (id, data) VALUES (1, ?)`, string(data))
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
	if strings.EqualFold(strings.TrimSpace(v), accountpick.RoundRobin) {
		return accountpick.RoundRobin
	}
	return accountpick.First
}

// SaveSettings 持久化设置。
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
		INSERT INTO zcode_settings (id, data) VALUES (1, ?)
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

func (s *Service) findAccount(id string) (Account, bool) {
	id = strings.TrimSpace(id)
	for _, a := range s.Settings().Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

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
	return s.SaveSettings(ctx, st)
}

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

func (s *Service) linkBaseURL() string {
	port := s.cfg.Port
	if port <= 0 {
		port = 3000
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/zcode/v1"
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

// ServeHTTP 是 ZCode 插件总入口。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/zcode/settings":
		s.handleSettings(w, r)
	case path == "/api/zcode/status":
		s.handleStatus(w, r)
	case path == "/api/zcode/usage":
		s.handleUsage(w, r)
	case path == "/api/zcode/models":
		s.handleModels(w, r)
	case path == "/api/zcode/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/zcode/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/zcode/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/zcode/models/toggle/"))
	case path == "/api/zcode/accounts":
		s.handleAccounts(w, r)
	case path == "/api/zcode/checkin":
		s.handleCheckin(w, r)
	case path == "/api/zcode/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/zcode/accounts/import":
		s.handleImportAccounts(w, r)
	case strings.HasPrefix(path, "/api/zcode/accounts/"):
		rest := strings.TrimPrefix(path, "/api/zcode/accounts/")
		switch {
		case strings.HasSuffix(rest, "/toggle"):
			s.handleToggleAccount(w, r, strings.TrimSuffix(rest, "/toggle"))
		case strings.HasSuffix(rest, "/test"):
			s.handleTestAccount(w, r, strings.TrimSuffix(rest, "/test"))
		case r.Method == http.MethodPut:
			s.handleUpdateAccount(w, r, rest)
		default:
			s.handleDeleteAccount(w, r, rest)
		}
	case path == "/api/zcode/link":
		s.handleLink(w, r)
	default:
		stripped := strings.TrimPrefix(path, "/api/zcode")
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
		"autoCheckin":     st.AutoCheckin,
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
		"modelCount":     len(staticModels),
		"linkBaseUrl":    s.linkBaseURL(),
	})
}
