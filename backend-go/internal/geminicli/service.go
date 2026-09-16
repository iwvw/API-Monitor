// Package geminicli 实现模型网关「插件中心」的 Gemini CLI 插件：把 Google
// Gemini CLI（Cloud Code Assist，cloudcode-pa.googleapis.com 的 v1internal
// 协议）封装成 OpenAI 兼容上游。管理面挂在 /api/geminicli，中继面挂在
// /api/geminicli/v1（仅本机回环，供网关转发）。
//
// 协议来源：CLIProxyAPI 的 gemini-cli 实现（internal/auth/gemini、
// internal/runtime/executor/gemini_cli_executor.go，commit dd49a520）。
// 本插件使用 gemini-cli 自己的 OAuth client，额度与账号体系与 antigravity
// 插件相互独立。
//
// 目录结构、表命名、link 语义与同类插件（workbuddy/ds2api）保持一致。
package geminicli

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

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

// internalKey 是插件内部固定调用密钥：仅在本机回环转发时注入，用于通过
// /api/geminicli/v1 的内部校验，用户无需为插件配置任何访问密钥。
const internalKey = "sk-geminicli-internal"

// providerName 是中继面模型列表里的 owned_by 标识。
const providerName = "geminicli"

// Settings 是 Gemini CLI 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// ModelPrefix 对外模型名统一前缀（如 "gcli-"）：模型列表暴露的名字带此前缀，
	// 转发时剥掉前缀再交给上游；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// ProxyPoolID 引用独立代理池插件的池；为空表示直连。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
	// DisabledModels 被停用的模型（不在 /v1/models 对外提供）。
	DisabledModels []string `json:"disabledModels"`
	// Accounts OAuth 登录产生的账号凭据。含 token，仅服务端可见，
	// 下发前端前必须经 toAccountView() 脱敏。
	Accounts []Account `json:"accounts"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:        false,
		DisabledModels: []string{},
		Accounts:       []Account{},
	}
}

// Service 是 Gemini CLI 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	// accountsMu 串行化「读取整份设置 → 改账号列表 → 写回」的读改写序列：
	// 登录回调、后台 token 刷新、删除账号可能并发发生，不加锁会互相覆盖丢账号。
	// 锁序固定为 accountsMu → mu，勿反向获取。
	accountsMu sync.Mutex

	// 进行中的 OAuth 登录会话，key 为 state。
	loginMu     sync.Mutex
	loginStates map[string]*loginState

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	// 账号失败冷却：accountID → 冷却截止时刻。上游返回可重试错误（429/5xx/网络）
	// 后被写入，选号时跳过。纯内存态：重启即清空。
	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	// 实际用量：内存增量按「站点时区日期 × 账号 × 模型」累加，定期落盘。
	usagePending map[usageKey]*usageDelta
	usageMu      sync.Mutex

	// 站点时区缓存（避免每个请求都开库读设置）。
	locMu      sync.Mutex
	locCache   *time.Location
	locCacheAt time.Time

	// 配额快照缓存（账号 ID → 快照），短 TTL，避免前端刷新时频繁打上游。
	quotaMu    sync.Mutex
	quotaCache map[string]quotaCacheEntry

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
		loginStates:   map[string]*loginState{},
		callBase:      map[string]int64{},
		callPending:   map[string]int64{},
		cooldownUntil: map[string]time.Time{},
		usagePending:  map[usageKey]*usageDelta{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		s.loadSettings(ctx, db)
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS geminicli_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("geminicli ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS geminicli_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("geminicli ensure call stats schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS geminicli_usage_daily (
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
		return nil, fmt.Errorf("geminicli ensure usage schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM geminicli_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO geminicli_settings (id, data) VALUES (1, ?)`, string(data))
		return
	}
	cfg := defaultSettings()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		cfg = defaultSettings()
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	if cfg.Accounts == nil {
		cfg.Accounts = []Account{}
	}
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
	s.loadCallStats(ctx, db)
}

// Settings 返回当前设置的只读副本（含凭据，仅限包内使用）。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.DisabledModels = append([]string(nil), s.settings.DisabledModels...)
	out.Accounts = append([]Account(nil), s.settings.Accounts...)
	return out
}

// SaveSettings 持久化设置。前缀变更时做两件事：
//  1. 把传入的停用模型名单归一化到新前缀命名空间（前端是整对象 PUT，会把「旧前缀的
//     名单」和新前缀一起提交，不归一化就会留下旧命名空间的残留，模型开关随后失效）；
//  2. 迁移已接入网关端点上的 models 名单。
//
// 最后无论前缀是否变化，都把停用名单权威地写回端点 disabled_models —— 网关正是按那一
// 列拦请求的，漏同步会让插件里的模型开关形同虚设。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.DisabledModels == nil {
		next.DisabledModels = []string{}
	}
	if next.Accounts == nil {
		next.Accounts = []Account{}
	}
	s.mu.RLock()
	oldPrefix := s.settings.ModelPrefix
	s.mu.RUnlock()
	prefixChanged := next.ModelPrefix != oldPrefix
	if prefixChanged {
		// 同时试旧前缀与新前缀，兼容「提交旧命名空间名单」与「提交新命名空间名单」两种输入。
		next.DisabledModels = remapNamespaceList(next.DisabledModels, next.ModelPrefix, oldPrefix, next.ModelPrefix)
	}

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO geminicli_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	if prefixChanged {
		s.writeLinkedEndpointModels(ctx, s.allModelIDs())
	}
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
}

// stripAnyPrefix 剥掉名字上命中的任一前缀（优先最长匹配）。
//
// 前缀变更时需要**双向**归一化：前端整对象 PUT，可能提交旧前缀名单，也可能提交新前缀
// 名单。只认旧前缀会在后一种情况留下新命名空间残留，只认新前缀又处理不了前一种情况；
// 两者都试、按最长匹配优先，才能把两种输入归一化到同一份裸模型名，且重复提交幂等。
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

// remapNamespace 把名字归一化到 newPrefix 命名空间。
func remapNamespace(name, newPrefix string, prefixes ...string) string {
	return newPrefix + stripAnyPrefix(name, prefixes...)
}

// remapNamespaceList 批量 remapNamespace。
func remapNamespaceList(names []string, newPrefix string, prefixes ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, remapNamespace(n, newPrefix, prefixes...))
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

// upsertAccount 按账号 ID 新增或覆盖。整段读改写加 accountsMu，避免与并发
// 的登录/删除互相覆盖。
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

// removeAccount 按账号 ID 删除。整段读改写加 accountsMu，避免并发覆盖。
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

// modelPrefix 返回归一化的对外模型前缀（空串表示不加前缀）。
func (s *Service) modelPrefix() string {
	return strings.TrimSpace(s.Settings().ModelPrefix)
}

// prefixModel 给内部模型 ID 套上对外前缀。
func (s *Service) prefixModel(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return p + id
}

// stripModelPrefix 剥掉请求模型名上的本插件前缀（不命中或空前缀时原样返回）。
func (s *Service) stripModelPrefix(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return strings.TrimPrefix(id, p)
}

// -----------------------------------------------------------------------------
// 调用计数
// -----------------------------------------------------------------------------

func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM geminicli_call_stats`)
	if err != nil {
		return
	}
	defer rows.Close()
	base := map[string]int64{}
	for rows.Next() {
		var id string
		var c int64
		if rows.Scan(&id, &c) == nil {
			base[id] = c
		}
	}
	s.callMu.Lock()
	s.callBase = base
	s.callMu.Unlock()
}

// recordCall 把一次成功转发记入未落盘增量。
func (s *Service) recordCall(identifier string) {
	if identifier == "" {
		return
	}
	s.callMu.Lock()
	if s.callPending == nil {
		s.callPending = map[string]int64{}
	}
	s.callPending[identifier]++
	s.callMu.Unlock()
}

// callDisplay 返回账号累计调用次数 = 已落盘基线 + 未落盘增量。
func (s *Service) callDisplay(identifier string) int64 {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.callBase[identifier] + s.callPending[identifier]
}

// flushCallStats 把未落盘增量合并进 DB，并同步进内存基线。
func (s *Service) flushCallStats(ctx context.Context) {
	s.callMu.Lock()
	if len(s.callPending) == 0 {
		s.callMu.Unlock()
		return
	}
	pend := s.callPending
	s.callPending = map[string]int64{}
	s.callMu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		s.callMu.Lock()
		for k, v := range pend {
			s.callPending[k] += v
		}
		s.callMu.Unlock()
		return
	}
	defer db.Close()
	for id, delta := range pend {
		_, _ = db.ExecContext(ctx, `
			INSERT INTO geminicli_call_stats (identifier, count) VALUES (?, ?)
			ON CONFLICT(identifier) DO UPDATE SET count = count + excluded.count`, id, delta)
	}
	s.callMu.Lock()
	for k, v := range pend {
		s.callBase[k] += v
	}
	s.callMu.Unlock()
}

// callStatsFlushInterval 是调用次数与用量定期落盘的周期。
const callStatsFlushInterval = time.Minute

// StartCallStatsFlush 启动调用次数与用量定期落盘；ctx 取消时做最后一次落盘后退出。
func (s *Service) StartCallStatsFlush(ctx context.Context) {
	s.callFlush.Do(func() {
		go func() {
			ticker := time.NewTicker(callStatsFlushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.flushCallStats(context.Background())
					s.flushUsage(context.Background())
				case <-ctx.Done():
					s.flushCallStats(context.Background())
					s.flushUsage(context.Background())
					return
				}
			}
		}()
	})
}

// -----------------------------------------------------------------------------
// HTTP 入口
// -----------------------------------------------------------------------------

// ServeHTTP 是 Gemini CLI 插件总入口：
//   - /api/geminicli/*：插件管理接口（会话鉴权）
//   - /api/geminicli/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/geminicli/settings":
		s.handleSettings(w, r)
	case path == "/api/geminicli/status":
		s.handleStatus(w, r)
	case path == "/api/geminicli/test":
		s.handleTest(w, r)
	case path == "/api/geminicli/usage":
		s.handleUsage(w, r)
	case path == "/api/geminicli/models":
		s.handleModels(w, r)
	case path == "/api/geminicli/quota":
		s.handleQuota(w, r)
	case path == "/api/geminicli/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/geminicli/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/geminicli/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/geminicli/models/toggle/"))
	case path == "/api/geminicli/login/start":
		s.handleLoginStart(w, r)
	case path == "/api/geminicli/login/poll":
		s.handleLoginPoll(w, r)
	case path == "/api/geminicli/login/callback":
		s.handleLoginCallback(w, r)
	case path == "/api/geminicli/accounts":
		s.handleAccounts(w, r)
	case path == "/api/geminicli/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/geminicli/accounts/import":
		s.handleImportAccounts(w, r)
	case strings.HasPrefix(path, "/api/geminicli/accounts/"):
		rest := strings.TrimPrefix(path, "/api/geminicli/accounts/")
		switch {
		case strings.HasSuffix(rest, "/refresh"):
			s.handleRefreshAccount(w, r, strings.TrimSuffix(rest, "/refresh"))
		case strings.HasSuffix(rest, "/toggle"):
			s.handleToggleAccount(w, r, strings.TrimSuffix(rest, "/toggle"))
		case strings.HasSuffix(rest, "/test"):
			s.handleTestAccount(w, r, strings.TrimSuffix(rest, "/test"))
		case r.Method == http.MethodPut:
			s.handleUpdateAccount(w, r, rest)
		default:
			s.handleDeleteAccount(w, r, rest)
		}
	case path == "/api/geminicli/link":
		s.handleLink(w, r)
	default:
		// 中继面：/api/geminicli/v1/...
		stripped := strings.TrimPrefix(path, "/api/geminicli")
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
		// 管理面不接受前端回传的两类字段：Accounts 含 token 不下发，回传会抹掉凭据；
		// DisabledModels 由专用启停接口持有，避免整对象 PUT 覆盖刚做的切换。
		current := s.Settings()
		body.Accounts = current.Accounts
		body.DisabledModels = current.DisabledModels
		if err := s.SaveSettings(r.Context(), body); err != nil {
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		// 关闭插件时同步移除已接入的网关端点行，避免端点仍暴露在列表。
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
		"enabled":        st.Enabled,
		"modelPrefix":    st.ModelPrefix,
		"proxyPoolId":    st.ProxyPoolID,
		"disabledModels": st.DisabledModels,
		"accounts":       views,
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	available := 0
	for _, a := range st.Accounts {
		if accountAvailable(a) {
			available++
		}
	}
	responseJSON(w, map[string]interface{}{
		"enabled":        st.Enabled,
		"upstreamReady":  s.upstreamReady(),
		"accountCount":   len(st.Accounts),
		"availableCount": available,
		"modelCount":     len(s.catalog()),
		"linkBaseUrl":    s.linkBaseURL(),
	})
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(s.catalog())})
}

// upstreamReady 报告上游协议层是否已实现。
func (s *Service) upstreamReady() bool { return upstreamImplemented }

// -----------------------------------------------------------------------------
// 小工具
// -----------------------------------------------------------------------------

// responseJSON 是本包轻量 JSON 输出。
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

// writeOpenAIError 输出 OpenAI 风格错误信封。
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
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/geminicli/v1"
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
