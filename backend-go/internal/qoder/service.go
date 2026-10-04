// Package qoder 实现模型网关「插件中心」的 Qoder 插件：把 Qoder（qoder.com）
// 封装成 OpenAI 兼容上游。管理面挂在 /api/qoder，中继面挂在 /api/qoder/v1
// （仅本机回环，供网关转发）。
//
// 协议来源：开源反代 qoder2api（jyao0708/qoder2api）记录的 Qoder 设备登录与
// 加密 SSE 协议：设备授权（nonce + PKCE）换取 securityOauthToken，随后把身份
// 信息 AES 加密 + RSA 包裹 + MD5 签名，POST 到 api3.qoder.sh 的
// agent_chat_generation 流式端点，响应为 {body: <openai chunk>} 信封。
//
// 目录结构、表命名、link 语义与同类插件（lobsterai/workbuddy/geminicli）保持一致。
package qoder

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
const internalKey = "sk-qoder-internal"

// providerName 是中继面模型列表里的 owned_by 标识。
const providerName = "qoder"

// upstreamImplemented 标记上游协议层已实现。
const upstreamImplemented = true

// Settings 是 Qoder 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// ModelPrefix 对外模型名统一前缀（如 "qoder-"）；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// ProxyPoolID 引用独立代理池插件的池；为空表示直连。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
	// DisabledModels 被停用的模型（对外名，含前缀）。
	DisabledModels []string `json:"disabledModels"`
	// AutoCheckin 每日自动签到（站点时区 9 点与 21 点各一次，领取每日 100 Credits）。
	// 指针语义：nil 视为开启（缺省开启），显式 false 才关闭。
	AutoCheckin *bool `json:"autoCheckin,omitempty"`
	// AccountStrategy 是多账号选号策略：first（默认）/ round-robin。
	AccountStrategy string `json:"accountStrategy"`
	// Accounts 设备登录产生的账号凭据。含 token，仅服务端可见，
	// 下发前端前必须经 toAccountView() 脱敏。
	Accounts []Account `json:"accounts"`
}

func defaultSettings() Settings {
	enabled := true
	return Settings{
		Enabled:         false,
		DisabledModels:  []string{},
		AutoCheckin:     &enabled,
		AccountStrategy: accountpick.First,
		Accounts:        []Account{},
	}
}

// autoCheckinEnabled 判定自动签到是否开启（缺省视为开启）。
func (s *Settings) autoCheckinEnabled() bool {
	if s.AutoCheckin == nil {
		return true
	}
	return *s.AutoCheckin
}

// Service 是 Qoder 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	// accountsMu 串行化「读取整份设置 → 改账号列表 → 写回」的读改写序列。
	// 锁序固定为 accountsMu → mu，勿反向获取。
	accountsMu sync.Mutex

	// 进行中的设备登录会话，key 为 loginID。
	loginMu     sync.Mutex
	loginStates map[string]*loginState

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	// 账号失败冷却：accountID → 冷却截止时刻。纯内存态，重启即清空。
	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	// 实际用量：内存增量按「站点时区日期 × 账号 × 模型」累加，定期落盘。
	usagePending map[usageKey]*usageDelta
	usageMu      sync.Mutex

	// 站点时区缓存。
	locMu      sync.Mutex
	locCache   *time.Location
	locCacheAt time.Time

	// rr 是 round-robin 策略的轮询游标（按原始列表下标锚定）。
	rr accountpick.Cursor

	// 每日签到调度器（cron + 站点时区 watcher）。
	schedStart sync.Once
	schedMu    sync.Mutex
	scheduler  *cronRuntime

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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS qoder_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("qoder ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS qoder_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("qoder ensure call stats schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS qoder_usage_daily (
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
		return nil, fmt.Errorf("qoder ensure usage schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM qoder_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO qoder_settings (id, data) VALUES (1, ?)`, string(data))
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
	cfg.AccountStrategy = normalizeStrategy(cfg.AccountStrategy)
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

// normalizeStrategy 归一化选号策略，未知值回落到 first。
func normalizeStrategy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case accountpick.RoundRobin:
		return accountpick.RoundRobin
	default:
		return accountpick.First
	}
}

// SaveSettings 持久化设置。前缀变更时把停用名单归一化到新前缀命名空间，
// 并把停用名单权威地写回已接入网关端点的 disabled_models 列（网关按那一列拦请求）。
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
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO qoder_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	// 无条件把当前启用模型名单与停用名单同步到已接入端点：模型目录会随账号
	// 区域变化（如只有国内版账号时是 14 个），仅在前缀变化时同步会让端点行
	// 长期停留在旧值。
	s.syncLinkedEndpointModels(ctx)
	return nil
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
	return s.SaveSettings(ctx, st)
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
	if p == "" {
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
// 调用计数
// -----------------------------------------------------------------------------

func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM qoder_call_stats`)
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

func (s *Service) callDisplay(identifier string) int64 {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.callBase[identifier] + s.callPending[identifier]
}

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
			INSERT INTO qoder_call_stats (identifier, count) VALUES (?, ?)
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

// ServeHTTP 是 Qoder 插件总入口：
//   - /api/qoder/*：插件管理接口（会话鉴权）
//   - /api/qoder/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/qoder/settings":
		s.handleSettings(w, r)
	case path == "/api/qoder/status":
		s.handleStatus(w, r)
	case path == "/api/qoder/test":
		s.handleTest(w, r)
	case path == "/api/qoder/usage":
		s.handleUsage(w, r)
	case path == "/api/qoder/models":
		s.handleModels(w, r)
	case path == "/api/qoder/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/qoder/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/qoder/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/qoder/models/toggle/"))
	case path == "/api/qoder/checkin":
		s.handleCheckinAll(w, r)
	case path == "/api/qoder/login/start":
		s.handleLoginStart(w, r)
	case path == "/api/qoder/login/poll":
		s.handleLoginPoll(w, r)
	case path == "/api/qoder/accounts":
		s.handleAccounts(w, r)
	case path == "/api/qoder/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/qoder/accounts/import":
		s.handleImportAccounts(w, r)
	case strings.HasPrefix(path, "/api/qoder/accounts/"):
		rest := strings.TrimPrefix(path, "/api/qoder/accounts/")
		switch {
		case strings.HasSuffix(rest, "/toggle"):
			s.handleToggleAccount(w, r, strings.TrimSuffix(rest, "/toggle"))
		case strings.HasSuffix(rest, "/test"):
			s.handleTestAccount(w, r, strings.TrimSuffix(rest, "/test"))
		case strings.HasSuffix(rest, "/checkin"):
			s.handleCheckinAccount(w, r, strings.TrimSuffix(rest, "/checkin"))
		case strings.HasSuffix(rest, "/quota"):
			s.handleAccountQuota(w, r, strings.TrimSuffix(rest, "/quota"))
		case r.Method == http.MethodPut:
			s.handleUpdateAccount(w, r, rest)
		default:
			s.handleDeleteAccount(w, r, rest)
		}
	case path == "/api/qoder/link":
		s.handleLink(w, r)
	default:
		stripped := strings.TrimPrefix(path, "/api/qoder")
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
		"autoCheckin":     st.autoCheckinEnabled(),
		"accountStrategy": normalizeStrategy(st.AccountStrategy),
		"accounts":        views,
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
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
		"upstreamReady":  s.upstreamReady(),
		"accountCount":   len(st.Accounts),
		"availableCount": available,
		"coolingCount":   cooling,
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

func (s *Service) upstreamReady() bool { return upstreamImplemented }

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
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/qoder/v1"
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
