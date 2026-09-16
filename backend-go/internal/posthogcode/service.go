// Package posthogcode 实现模型网关「插件中心」的 PostHog Code 插件：把 PostHog
// LLM Gateway（gateway.us.posthog.com / gateway.eu.posthog.com）的 posthog_code 产品
// 封装成 OpenAI 兼容上游，管理面挂在 /api/posthogcode，中继面挂在 /api/posthogcode/v1
// （仅本机回环，供网关转发）。
//
// 鉴权走 PostHog OAuth 2.0 PKCE：PostHog 的 llm_gateway:read 是 privileged scope，
// 自建个人 API Key 无法获得，只有 OAuth 授权（Desktop 同款应用白名单）能拿到，
// 故本插件内嵌授权码流程与 refresh token 自动轮换。
//
// 上游协议细节集中在 upstream.go，OAuth 流程在 oauth.go，其余文件只做本地编排。
// 目录结构、表命名、link/前缀语义与同类插件（antigravity / workbuddy）保持一致。
package posthogcode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// internalKey 是插件内部固定调用密钥：仅在本机回环转发时注入，用于通过
// /api/posthogcode/v1 的内部校验，用户无需为插件配置任何访问密钥。
const internalKey = "sk-posthogcode-internal"

// callStatsFlushInterval 是调用次数落盘周期。
const callStatsFlushInterval = time.Minute

// Settings 是 PostHog Code 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// Region 是 PostHog Cloud 区域：us / eu。
	Region string `json:"region"`
	// Product 是网关产品路径段。posthog_code 是唯一对 Desktop OAuth 应用开放的产品。
	Product string `json:"product"`
	// ModelPrefix 对外模型名统一前缀（如 "phc-"）：模型列表暴露的名字带此前缀，
	// 转发时剥掉前缀再交给上游；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// DisabledModels 被停用的模型（不在 /v1/models 对外提供）。
	DisabledModels []string `json:"disabledModels"`
	// Accounts OAuth 授权产生的账号凭据。含 token，仅服务端可见，
	// 下发前端前必须经 toAccountView() 脱敏。
	Accounts []Account `json:"accounts"`
	// FreeTierOnly 为真时只暴露 PostHog 免费层模型（未绑定付款方式的组织可用）。
	FreeTierOnly bool `json:"freeTierOnly"`
	// ModelsInitialized 标记首次模型目录已初始化：首次拉取时自动停用受限模型，
	// 之后不再干预用户的启停选择。
	ModelsInitialized bool `json:"modelsInitialized"`
	// AccountStrategy 是多账号选号策略：
	//   first        —— 固定用列表首个可用账号（主备，默认，行为最可预期）
	//   round-robin  —— 依次轮询，请求均匀分摊
	//   least-used   —— 选剩余额度最多的账号，按实际额度拉平消耗
	AccountStrategy string `json:"accountStrategy"`
}

// 选号策略取值。
const (
	strategyFirst      = "first"
	strategyRoundRobin = "round-robin"
	strategyLeastUsed  = "least-used"
)

// normalizeStrategy 归一化策略值，未知值回落到默认。
func normalizeStrategy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case strategyRoundRobin:
		return strategyRoundRobin
	case strategyLeastUsed:
		return strategyLeastUsed
	default:
		return strategyFirst
	}
}

func defaultSettings() Settings {
	return Settings{
		Enabled:         false,
		Region:          "us",
		Product:         "posthog_code",
		DisabledModels:  []string{},
		Accounts:        []Account{},
		FreeTierOnly:    true,
		AccountStrategy: strategyFirst,
	}
}

// Account 是一个 PostHog 账号的 OAuth 凭证。
type Account struct {
	// ID 是账号标识（用 PostHog 用户 uuid 或 email）。
	ID string `json:"id"`
	// Email 是授权账号邮箱（用于展示）。
	Email string `json:"email"`
	// Region 是该账号所属的 PostHog Cloud 区域（us / eu）。
	// 账号级存储，使 us 与 eu 的账号可以共存并各自走对的网关。
	// 为空表示旧数据，回落到 Settings.Region。
	Region string `json:"region,omitempty"`
	// AccessToken / RefreshToken 是 OAuth 凭据（加密落库）。
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	// ExpiresAt 是 access token 过期时刻（Unix 秒）。
	ExpiresAt int64 `json:"expiresAt"`
	// OrganizationID / ProjectID 是授权时解析到的归属。
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	// Disabled 由用户手动停用。
	Disabled bool `json:"disabled"`
	// Scope 是授权时实际获得的 scope 列表，用于判断能否查询组织额度。
	Scope string `json:"scope,omitempty"`
	// LastError 记录最近一次刷新/调用失败原因，供前端排障。
	LastError string `json:"lastError"`
	// CreatedAt 是账号加入时间。
	CreatedAt string `json:"createdAt"`
}

// AccountView 是下发前端的脱敏账号视图（不含任何 token）。
type AccountView struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Region         string `json:"region"`
	OrganizationID string `json:"organizationId"`
	ProjectID      string `json:"projectId"`
	Disabled       bool   `json:"disabled"`
	TokenState     string `json:"tokenState"`
	ExpiresAt      int64  `json:"expiresAt"`
	ExpiresIn      int64  `json:"expiresInSeconds"`
	Available      bool   `json:"available"`
	CallCount      int64  `json:"callCount"`
	// ScopeReady 为假表示该凭据缺少 project:read，查不到组织额度，需要重新授权。
	ScopeReady bool   `json:"scopeReady"`
	LastError  string `json:"lastError,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

// Service 是 PostHog Code 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	// accountsMu 串行化「读取整份设置 → 改账号列表 → 写回」的读改写序列：
	// OAuth 回调、后台 token 刷新（含轮换）、导入与删除可能并发发生，
	// 不加锁会互相覆盖丢失账号或已轮换的 refresh token。
	// 锁序固定为 accountsMu → mu，勿反向获取。
	accountsMu sync.Mutex

	// 模型目录缓存（来自上游 /v1/models）。
	modelMu      sync.Mutex
	modelCache   []ModelInfo
	modelCacheAt time.Time

	// 进行中的 OAuth 授权会话，key 为 state。
	oauthMu     sync.Mutex
	oauthStates map[string]*oauthState

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	// 账号失败冷却：accountID → 冷却截止时刻（纯内存态，重启即清空）。
	cooldownMu    sync.Mutex
	cooldownUntil map[string]time.Time

	// tokenRefreshMu 串行化所有账号的 token 刷新。
	// PostHog 的 refresh token 是单次轮换的：若同一账号并发现刷新，
	// 多个请求会拿同一个 refresh token 去换，一个成功、其余因旧 token
	// 已失效而报 Invalid access token，把账号刷成「永久失效」。
	// 全局串行化成本可忽略（刷新仅发生在临近过期时），换来换码安全。
	tokenRefreshMu sync.Mutex

	// 额度快照：accountID → 剩余额度（credit）。由用量查询与转发后刷新，
	// 供 least-used 策略选号。读路径不查库，可承受每请求一次。
	quotaMu   sync.RWMutex
	quotaSnap map[string]float64
	quotaAt   time.Time
	// quotaRefreshedAt 记录各账号上次快照刷新时刻，用于转发后刷新的节流。
	quotaRefreshedAt map[string]time.Time

	// 轮询游标：round-robin 策略用，记录上次选中的账号在完整列表中的下标。
	// 用原始下标而非候选集下标锚定，避免候选集长度变化时游标回绕导致分配不均。
	// 初值 -1 使首次选取落在列表第一个候选上。
	rrMu      sync.Mutex
	rrLastIdx int
	rrInit    bool

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
		cfg:              cfg,
		store:            database.New(cfg),
		oauthStates:      map[string]*oauthState{},
		callBase:         map[string]int64{},
		callPending:      map[string]int64{},
		cooldownUntil:    map[string]time.Time{},
		quotaSnap:        map[string]float64{},
		quotaRefreshedAt: map[string]time.Time{},
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS posthogcode_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("posthogcode ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS posthogcode_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("posthogcode ensure call stats schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM posthogcode_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := secure.EncryptJSON(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO posthogcode_settings (id, data) VALUES (1, ?)`, data)
		return
	}
	var cfg Settings
	if secure.IsEncrypted(raw) {
		if err := secure.DecryptJSON(raw, &cfg); err != nil {
			cfg = defaultSettings()
		}
	} else {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			cfg = defaultSettings()
		}
		if enc, err := secure.EncryptJSON(cfg); err == nil {
			_, _ = db.ExecContext(ctx, `UPDATE posthogcode_settings SET data = ? WHERE id = 1`, enc)
		}
	}
	cfg = normalizeSettings(cfg)
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
	s.loadCallStats(ctx, db)
}

// normalizeSettings 补齐缺省字段，避免旧行留下 nil 切片。
func normalizeSettings(cfg Settings) Settings {
	if cfg.Region == "" {
		cfg.Region = "us"
	}
	if cfg.Product == "" {
		cfg.Product = "posthog_code"
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	if cfg.Accounts == nil {
		cfg.Accounts = []Account{}
	}
	cfg.AccountStrategy = normalizeStrategy(cfg.AccountStrategy)
	return cfg
}

// accountRegion 返回账号所属区域：优先账号级记录，为空时回落全局设置。
// 账号级区域让 us 与 eu 的账号可以共存，各自走对的网关与主站。
func (s *Service) accountRegion(acc Account) string {
	if r := normalizeRegion(acc.Region); r != "" {
		return r
	}
	return normalizeRegion(s.Settings().Region)
}

// normalizeRegion 归一化区域值，只接受 us / eu，其余返回空串。
func normalizeRegion(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "us":
		return "us"
	case "eu":
		return "eu"
	default:
		return ""
	}
}

// accountProduct 返回账号使用的网关产品段。目前所有账号都是 posthog_code。
func (s *Service) accountProduct() string {
	return productSlug(s.Settings().Product)
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

// SaveSettings 持久化设置。前缀变更时同步已接入网关端点的模型名单。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	next = normalizeSettings(next)
	s.mu.RLock()
	oldPrefix := s.settings.ModelPrefix
	s.mu.RUnlock()
	prefixChanged := next.ModelPrefix != oldPrefix

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	enc, err := secure.EncryptJSON(next)
	if err != nil {
		return fmt.Errorf("posthogcode settings encrypt: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO posthogcode_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, enc); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	if prefixChanged {
		s.refreshLinkedEndpointModels(ctx)
	}
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
}

// modelPrefix 读取当前模型前缀。
func (s *Service) modelPrefix() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings.ModelPrefix
}

// prefixModel 给单个模型名套前缀。
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

// disabledSet 返回停用模型的集合。
func (s *Service) disabledSet() map[string]bool {
	out := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		out[d] = true
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

// upsertAccount 按账号 ID 新增或覆盖。
func (s *Service) upsertAccount(ctx context.Context, acc Account) error {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	return s.upsertAccountLocked(ctx, acc)
}

// upsertAccountLocked 在调用方已持有 accountsMu 时执行账号写入。
func (s *Service) upsertAccountLocked(ctx context.Context, acc Account) error {
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
// 账号状态
// -----------------------------------------------------------------------------

// tokenExpiringWindow 是判定 access token「即将过期」的阈值。
const tokenExpiringWindow = 24 * time.Hour

// tokenState 依据过期时刻给出 token 状态。
func tokenState(a Account) string {
	if a.AccessToken == "" {
		return "unknown"
	}
	if a.ExpiresAt <= 0 {
		return "unknown"
	}
	left := time.Until(time.Unix(a.ExpiresAt, 0))
	if left <= 0 {
		return "expired"
	}
	if left < tokenExpiringWindow {
		return "expiring"
	}
	return "valid"
}

// accountAvailable 表示账号当前是否可用于转发。
// 无凭据、被停用或 access token 已过期都视为不可用；
// ExpiresAt 缺失（tokenState = unknown）仍视为可用，避免误伤未记录过期时刻的凭据。
func accountAvailable(a Account) bool {
	if a.Disabled || a.ID == "" || a.AccessToken == "" {
		return false
	}
	return tokenState(a) != "expired"
}

// toAccountView 把账号脱敏成可下发前端的视图（不下发任何 token）。
func (s *Service) toAccountView(a Account) AccountView {
	view := AccountView{
		ID:             a.ID,
		Email:          a.Email,
		Region:         s.accountRegion(a),
		OrganizationID: a.OrganizationID,
		ProjectID:      a.ProjectID,
		Disabled:       a.Disabled,
		TokenState:     tokenState(a),
		ExpiresAt:      a.ExpiresAt,
		Available:      accountAvailable(a),
		CallCount:      s.callDisplay(a.ID),
		ScopeReady:     hasProjectRead(a.Scope),
		LastError:      a.LastError,
		CreatedAt:      a.CreatedAt,
	}
	if a.ExpiresAt > 0 {
		if left := a.ExpiresAt - time.Now().Unix(); left > 0 {
			view.ExpiresIn = left
		}
	}
	return view
}

// hasProjectRead 判断凭据的 scope 是否含 project:read（查询组织额度的前提）。
// scope 为空表示旧凭据（该字段引入前授权），一律视为不满足。
func hasProjectRead(scope string) bool {
	for _, sc := range strings.Fields(scope) {
		if sc == "project:read" {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// 调用次数
// -----------------------------------------------------------------------------

func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM posthogcode_call_stats`)
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
			INSERT INTO posthogcode_call_stats (identifier, count) VALUES (?, ?)
			ON CONFLICT(identifier) DO UPDATE SET count = count + excluded.count`, id, delta)
	}
	s.callMu.Lock()
	for k, v := range pend {
		s.callBase[k] += v
	}
	s.callMu.Unlock()
}

// StartCallStatsFlush 启动调用次数定期落盘。
func (s *Service) StartCallStatsFlush(ctx context.Context) {
	s.callFlush.Do(func() {
		go func() {
			ticker := time.NewTicker(callStatsFlushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.flushCallStats(context.Background())
				case <-ctx.Done():
					s.flushCallStats(context.Background())
					return
				}
			}
		}()
	})
}

// -----------------------------------------------------------------------------
// 账号冷却
// -----------------------------------------------------------------------------

// accountCooldown 是账号遇到可重试上游失败后的冷却时长。
const accountCooldown = 5 * time.Minute

// inCooldown 返回账号是否处于失败冷却期。
func (s *Service) inCooldown(id string) bool {
	_, ok := s.cooldownUntilOf(id)
	return ok
}

// cooldownUntilOf 读取账号当前冷却的截止时刻；未冷却或已过冷却期返回 ok=false。
func (s *Service) cooldownUntilOf(id string) (time.Time, bool) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[id]
	if !ok || !time.Now().Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// markCooldown 记录账号一次可重试的上游失败，进入冷却期。
func (s *Service) markCooldown(id, reason string) {
	until := time.Now().Add(accountCooldown)
	s.cooldownMu.Lock()
	if s.cooldownUntil == nil {
		s.cooldownUntil = map[string]time.Time{}
	}
	s.cooldownUntil[id] = until
	s.cooldownMu.Unlock()
}

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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
		},
	})
	_, _ = w.Write(b)
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// atoiSafe 容错解析整数。
func atoiSafe(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}
