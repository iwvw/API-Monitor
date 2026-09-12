// Package workbuddy 实现模型网关「插件中心」的 WorkBuddy 插件：把腾讯 CodeBuddy
// （copilot.tencent.com）封装成 OpenAI 兼容上游，管理面挂在 /api/workbuddy，
// 中继面挂在 /api/workbuddy/v1（仅本机回环，供网关转发）。
//
// 设计来源：CLIProxyAPI 的 c-shared 插件 workbuddy-cliproxy
// （https://github.com/LiuJiaCheng11/workbuddy-cliproxy，原始设计归属 Sliverkiss）。
// 本项目是内嵌 Go 进程架构，不加载 .so/.dll，故按其记录的上游协议事实原生实现。
// 上游协议细节集中在 upstream.go，其余文件只做本地编排。
//
// 目录结构、表命名、link/前缀语义与同类的 DS2API 插件保持一致，便于统一维护与卸载。
package workbuddy

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
// /api/workbuddy/v1 的内部校验，用户无需为插件配置任何访问密钥。
const internalKey = "sk-workbuddy-internal"

// Settings 是 WorkBuddy 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// ModelPrefix 对外模型名统一前缀（如 "wb-"）：模型列表暴露的名字带此前缀，
	// 转发时剥掉前缀再交给上游；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// ProxyPoolID 引用独立代理池插件的池；为空表示直连。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
	// DisabledModels 被停用的模型（不在 /v1/models 对外提供）。
	DisabledModels []string `json:"disabledModels"`
	// Accounts 扫码登录产生的账号凭据。含 token，仅服务端可见，
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

// Service 是 WorkBuddy 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	// 模型目录缓存（目录来自上游 /v3/config，缓存内不区分账号）。
	modelMu      sync.Mutex
	modelCache   []ModelInfo
	modelCacheAt time.Time

	// 进行中的扫码登录会话，key 为上游 state。
	loginMu     sync.Mutex
	loginStates map[string]*loginState

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	// 最近一次上游 usage 的字段快照，用于诊断"上游到底上报了哪些计费/缓存字段"。
	usageMu           sync.Mutex
	usageKeys         []string
	usageSample       string
	usageReportsCache bool
	usageTypes        map[string]string
	usageAt           string
	emittedUsage      string
	emittedUsageAt    string

	// 实际用量与扣费：内存增量按「站点时区日期 × 账号 × 模型」累加，定期落盘。
	usagePending map[usageKey]*usageDelta
	creditSeen   bool

	// 选号权重：站点时区「今天」各账号已消耗的 credit。
	// 读路径只读内存（每个请求都要选号，不能查库）；写路径 = 后台 ticker 的快照刷新
	// + recordUsage 的即时累加。用 RWMutex：读多写少，读时不让 map 逃逸以避免竞态。
	creditDayMu   sync.RWMutex
	creditDay     string
	creditDayUsed map[string]float64
	creditDayAt   time.Time

	// 站点时区缓存（避免每个请求都开库读设置）。
	locMu      sync.Mutex
	locCache   *time.Location
	locCacheAt time.Time

	// 账号失败冷却：accountID → 冷却截止时刻。上游返回可重试错误（429/5xx/网络/流中断）
	// 后被写入，选号时跳过。纯内存态：重启即清空，避免把瞬时故障持久化。
	//
	// 注意与 modelLimit* 的分工：这里是**瞬时故障**的通用退避（封整个账号），
	// 而下面是上游点名的**模型级频率限制**（只封「账号 × 模型」）。详见 ratelimit.go。
	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	// 模型级限流簿：modelLimitKey(accountID, model) → 恢复时刻与原文。
	// 纯内存态（与账号冷却同理由）；rateLimitSample 只留最近一次原文供诊断。
	modelLimitMu      sync.Mutex
	modelLimits       map[string]modelLimit
	rateLimitSample   string
	rateLimitSampleAt string

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
		creditDayUsed: map[string]float64{},
		cooldownUntil: map[string]time.Time{},
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workbuddy_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("workbuddy ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workbuddy_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("workbuddy ensure call stats schema: %w", err)
	}
	// 实际用量与扣费：按「站点时区日期 × 账号 × 模型」聚合（行数 = 天数 × 账号数 × 模型数，有界）。
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workbuddy_usage_daily (
		day TEXT NOT NULL,
		account_id TEXT NOT NULL,
		model TEXT NOT NULL,
		requests INTEGER NOT NULL DEFAULT 0,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		credit REAL NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (day, account_id, model)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("workbuddy ensure usage schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workbuddy_diagnostics (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("workbuddy ensure diagnostics schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM workbuddy_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO workbuddy_settings (id, data) VALUES (1, ?)`, string(data))
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
//  1. 把传入的停用模型名单归一化到新前缀命名空间（前端会把「旧前缀的名单」和新前缀
//     一起提交，因为它是整对象 PUT，不归一化就会留下旧命名空间的残留）；
//  2. 迁移已接入网关端点上的 model_mappings key 与 models 名单。
//
// 最后无论前缀是否变化，都把停用名单权威地写回端点 disabled_models ——
// 网关正是按那一列拦请求的，漏同步会让模型开关形同虚设。
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
		prevPrefix := oldPrefix
		next.DisabledModels = remapNamespaceList(next.DisabledModels, next.ModelPrefix, prevPrefix, next.ModelPrefix)
	}

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO workbuddy_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	if prefixChanged {
		s.refreshLinkedEndpointModels(ctx, oldPrefix)
	}
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
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

// upsertAccount 按账号 ID 新增或覆盖。
func (s *Service) upsertAccount(ctx context.Context, acc Account) error {
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
	st := s.Settings()
	out := st.Accounts[:0]
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

// prefixModelNames 批量套前缀。
func (s *Service) prefixModelNames(ids []string) []string {
	p := s.modelPrefix()
	if p == "" {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, p+id)
	}
	return out
}

// remapPrefixedName 把模型名从旧前缀命名空间迁移到新前缀命名空间。
func remapPrefixedName(name, oldPrefix, newPrefix string) string {
	if oldPrefix != "" {
		name = strings.TrimPrefix(name, oldPrefix)
	}
	if newPrefix != "" {
		name = newPrefix + name
	}
	return name
}

// stripAnyPrefix 剥掉名字上命中的任一前缀（优先最长匹配）。
//
// 前缀变更时需要**双向**归一化：前端是整对象 PUT，会把「旧前缀的名单」和新前缀
// 一起提交；外部调用方也可能直接给新前缀的名字。只认旧前缀会在前一种情况留下旧
// 命名空间残留，只认新前缀又处理不了前一种情况；两者都试、按最长匹配优先，
// 才能保证两种输入都归一化到同一份裸模型名，且重复提交是幂等的。
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

// -----------------------------------------------------------------------------
// 调用计数
// -----------------------------------------------------------------------------

func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM workbuddy_call_stats`)
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
// 逐条 UPSERT 累加，避免先读后写在并发下丢增量。
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
		// 落库失败时把增量放回，等待下轮重试，避免计数丢失。
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
			INSERT INTO workbuddy_call_stats (identifier, count) VALUES (?, ?)
			ON CONFLICT(identifier) DO UPDATE SET count = count + excluded.count`, id, delta)
	}
	s.callMu.Lock()
	for k, v := range pend {
		s.callBase[k] += v
	}
	s.callMu.Unlock()
}

// callStatsFlushInterval 是调用次数定期落盘的周期。
const callStatsFlushInterval = time.Minute

// StartCallStatsFlush 启动调用次数定期落盘；ctx 取消时做最后一次落盘后退出。
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
					s.refreshCreditDaySnapshot(context.Background())
					s.persistDiagnostics(context.Background())
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

// ServeHTTP 是 WorkBuddy 插件总入口：
//   - /api/workbuddy/*：插件管理接口（会话鉴权）
//   - /api/workbuddy/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/workbuddy/settings":
		s.handleSettings(w, r)
	case path == "/api/workbuddy/status":
		s.handleStatus(w, r)
	case path == "/api/workbuddy/test":
		s.handleTest(w, r)
	case path == "/api/workbuddy/usage":
		s.handleUsage(w, r)
	case path == "/api/workbuddy/models":
		s.handleModels(w, r)
	case path == "/api/workbuddy/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/workbuddy/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/workbuddy/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/workbuddy/models/toggle/"))
	case path == "/api/workbuddy/login/start":
		s.handleLoginStart(w, r)
	case path == "/api/workbuddy/login/poll":
		s.handleLoginPoll(w, r)
	case path == "/api/workbuddy/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/workbuddy/accounts/import":
		s.handleImportAccounts(w, r)
	case path == "/api/workbuddy/accounts":
		s.handleAccounts(w, r)
	case strings.HasPrefix(path, "/api/workbuddy/accounts/"):
		rest := strings.TrimPrefix(path, "/api/workbuddy/accounts/")
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
	case path == "/api/workbuddy/link":
		s.handleLink(w, r)
	case path == "/api/workbuddy/v1/_diag":
		s.handleDiag(w, r)
	default:
		// 中继面：/api/workbuddy/v1/...
		stripped := strings.TrimPrefix(path, "/api/workbuddy")
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
		// 管理面不接受前端回传的两类字段，避免整对象 PUT 把它们盖回旧值：
		//   - Accounts：token 不下发前端，回传的必然是脱敏数据，写回会抹掉凭据；
		//   - DisabledModels：由专用接口（models/toggle、toggle-batch）持有。
		//     前端「保存设置」是整对象 PUT，禁用名单在本地可能还是切换前的快照，
		//     若接受回传值会把刚做的模型启停覆盖掉（现象：开关存不住）。
		current := s.Settings()
		body.Accounts = current.Accounts
		body.DisabledModels = current.DisabledModels
		if err := s.SaveSettings(r.Context(), body); err != nil {
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
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
	// 用 catalog 而不是缓存快照：首次访问时顺带拉一次目录（有 10 分钟缓存，
	// 轮询不会持续打上游），让前端立刻能看到模型数。
	responseJSON(w, map[string]interface{}{
		"enabled":        st.Enabled,
		"upstreamReady":  s.upstreamReady(),
		"accountCount":   len(st.Accounts),
		"availableCount": available,
		"modelCount":     len(s.catalog(r.Context())),
		"linkBaseUrl":    s.linkBaseURL(),
		// 最近一次上游 usage 的字段快照：用来判断上游有没有上报缓存命中
		// （网关的缓存统计只认 usage.prompt_tokens_details.cached_tokens）。
		"upstreamUsage": s.upstreamUsageInfo(),
	})
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	models, err := s.fetchCatalog(ctx)
	if err != nil {
		responseJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(models)})
}

// upstreamReady 报告上游协议层是否已实现（骨架阶段恒为 false）。
func (s *Service) upstreamReady() bool { return upstreamImplemented }

// -----------------------------------------------------------------------------
// 小工具
// -----------------------------------------------------------------------------

// responseJSON 是本包轻量 JSON 输出。
func responseJSON(w http.ResponseWriter, statusOrPayload interface{}, payload ...interface{}) {
	status := http.StatusOK
	var body interface{}
	switch v := statusOrPayload.(type) {
	case int:
		status = v
		if len(payload) > 0 {
			body = payload[0]
		}
	default:
		body = statusOrPayload
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if body == nil {
		body = map[string]interface{}{}
	}
	b, _ := json.Marshal(body)
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeOpenAIError 以 OpenAI 兼容错误体输出（供中继面使用）。
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
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/workbuddy/v1"
}
