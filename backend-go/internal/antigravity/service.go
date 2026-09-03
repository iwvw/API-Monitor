package antigravity

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// Account 是一个 Google 账号（Claude 订阅）的 OAuth 凭证。
type Account struct {
	// Name 是账号显示名（可选）。
	Name string `json:"name"`
	// OAuth 凭证。
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
	Email        string `json:"email"`
	ProjectID    string `json:"projectId"`
	PlanType     string `json:"planType"`
	// Disabled 由用户手动停用。
	Disabled bool `json:"disabled"`
}

// Settings 是 Antigravity 插件持久化配置（单行 JSON）。
type Settings struct {
	// Enabled 总开关。
	Enabled bool `json:"enabled"`
	// Accounts 是已授权的 Google 账号列表。
	Accounts []Account `json:"accounts"`
	// ProxyPoolID 引用独立代理池插件（/api/proxypool）的池；空 = 直连。
	ProxyPoolID string `json:"proxyPoolId"`
	// DisabledModels 被停用的模型（上游模型列表中不对外提供）。
	DisabledModels []string `json:"disabledModels"`
	// ModelAliases 模型别名映射：key=上游模型 ID，value=对外别名。
	// 外部客户端只看到/调用别名，转发时反查回上游 ID。
	ModelAliases map[string]string `json:"modelAliases,omitempty"`
	// ModelPrefix 对外模型名统一前缀（如 "agy-"）：模型列表暴露的名字带此前缀，
	// 请求转发前先剥掉前缀再走别名/原生 ID 解析。为空表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// QuotaMonitorEnabled 配额刷新自动化检测：后台轮询各账号配额窗口的剩余比例，
	// 比例回升（窗口被重置/刷新）时触发通知。
	QuotaMonitorEnabled bool `json:"quotaMonitorEnabled"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:        false,
		Accounts:       []Account{},
		DisabledModels: []string{},
		ModelAliases:   map[string]string{},
	}
}

// modelPrefix 返回归一化的对外模型前缀（去空白；空串表示不加前缀）。
func (s *Service) modelPrefix() string {
	return strings.TrimSpace(s.Settings().ModelPrefix)
}

// prefixModel 把内部模型 ID 加上对外前缀，空前缀时原样返回。
// 用于模型列表暴露（linkCreate 写入网关端点 models、/v1/models 响应）。
func (s *Service) prefixModel(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return p + id
}

// stripModelPrefix 剥掉请求模型名上的本插件前缀（不命中或空前缀时原样返回）。
// 用于 OpenAI/Anthropic 兼容转发入口，把对外模型名还原为内部模型 ID 再进别名反查。
func (s *Service) stripModelPrefix(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return strings.TrimPrefix(id, p)
}

// prefixModelNames 批量对模型清单加前缀（跳过禁用模型由调用方处理）。
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

// remapPrefixedName 把模型名从旧前缀命名空间迁移到新前缀命名空间：
// 先剥掉旧前缀，再套上新前缀。用于前缀变更时同步 model_mappings 的 key
// 与 disabled_models 里的模型名，保持与 models 列表同一命名空间。
func remapPrefixedName(name, oldPrefix, newPrefix string) string {
	if oldPrefix != "" {
		name = strings.TrimPrefix(name, oldPrefix)
	}
	if newPrefix != "" {
		name = newPrefix + name
	}
	return name
}

// Notifier 是 antigravity 向外部通知系统上报事件的最小接口。
// 各模块自行声明同构接口避免包级循环依赖，由 server 注入实现。
type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

// Service 是 Antigravity 插件后端：OAuth 授权、多账号 token 管理、Claude→Gemini 转发。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings
	cursor   uint64

	// externalPool 是独立代理池选择器（server 注入）。
	externalPool ProxyPoolSelector

	// notifier 是外部通知系统注入（server 注入）。
	notifier Notifier

	// quotaMonitorOnce 保证配额监控后台循环只启动一次。
	quotaMonitorOnce sync.Once
	// quotaPrevMu 保护 quotaPrev；quotaPrev 记录 邮箱+窗口 → 上次剩余比例快照。
	quotaPrevMu sync.Mutex
	quotaPrev   map[string]float64

	// callMu 保护调用计数持久化状态：callCounts 是自上次落盘以来的未落盘增量，
	// callBase 是已落盘累计基线（进程启动时从 DB 恢复）。
	callMu         sync.Mutex
	callCounts     map[string]int64
	callBase       map[string]int64
	callFlushOnce  sync.Once
}

// SetNotifier 注入外部通知系统。
func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

// ProxyPoolSelector 复用独立代理池插件选择能力（server 注入）。
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
		cfg:        cfg,
		store:      database.New(cfg),
		callCounts: map[string]int64{},
		callBase:   map[string]int64{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		s.loadSettings(ctx, db)
		db.Close()
	}
	return s
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS antigravity_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("antigravity ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS antigravity_call_stats (
		email TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("antigravity ensure call stats schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM antigravity_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := secure.EncryptJSON(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO antigravity_settings (id, data) VALUES (1, ?)`, data)
		return
	}
	var cfg Settings
	if secure.IsEncrypted(raw) {
		if err := secure.DecryptJSON(raw, &cfg); err != nil {
			cfg = defaultSettings()
		}
	} else {
		// 兼容存量明文行（升级前以明文 JSON 存储）：解密失败路径由 EncryptJSON 归位明文时覆盖。
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			cfg = defaultSettings()
		}
		// 存量明文迁移为加密存储（尽力而为，失败不影响读取）。
		if enc, err := secure.EncryptJSON(cfg); err == nil {
			_, _ = db.ExecContext(ctx, `UPDATE antigravity_settings SET data = ? WHERE id = 1`, enc)
		}
	}
	if cfg.Accounts == nil {
		cfg.Accounts = []Account{}
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	if cfg.ModelAliases == nil {
		cfg.ModelAliases = map[string]string{}
	}
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
	s.loadCallStats(db)
	if cfg.Enabled {
		go s.syncEndpointModels()
	}
}

// Settings 返回当前设置的只读副本。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.Accounts = append([]Account(nil), s.settings.Accounts...)
	out.DisabledModels = append([]string(nil), s.settings.DisabledModels...)
	out.ModelAliases = make(map[string]string, len(s.settings.ModelAliases))
	for k, v := range s.settings.ModelAliases {
		out.ModelAliases[k] = v
	}
	return out
}

// resolveUpstreamModel 把外部客户端请求的模型名反查回上游 ID。
// 遍历 ModelAliases，若 value（对外别名）== requested，返回对应 key（上游 ID）；
// 未命中则原样返回 requested（未配别名或本来就是上游 ID）。
func (s *Service) resolveUpstreamModel(requested string) string {
	requested = s.stripModelPrefix(requested)
	if strings.TrimSpace(requested) == "" {
		return requested
	}
	for upstreamID, alias := range s.Settings().ModelAliases {
		if strings.TrimSpace(alias) != "" && alias == requested {
			return upstreamID
		}
	}
	return requested
}

// aliasForUpstream 返回上游模型 ID 配置的对外别名；未配置返回空串。
func (s *Service) aliasForUpstream(upstreamID string) string {
	alias := s.Settings().ModelAliases[upstreamID]
	return strings.TrimSpace(alias)
}

// SaveSettings 校验并持久化设置。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.Accounts == nil {
		next.Accounts = []Account{}
	}
	if next.DisabledModels == nil {
		next.DisabledModels = []string{}
	}
	if next.ModelAliases == nil {
		next.ModelAliases = map[string]string{}
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	enc, err := secure.EncryptJSON(next)
	if err != nil {
		return fmt.Errorf("antigravity settings encrypt: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO antigravity_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, enc); err != nil {
		return err
	}
	s.mu.Lock()
	oldPrefix := s.settings.ModelPrefix
	s.settings = next
	s.mu.Unlock()
	if next.ModelPrefix != oldPrefix {
		// 前缀变更后同步已链接网关端点的模型名单、映射与禁用状态。
		s.refreshLinkedEndpointModels(ctx, oldPrefix)
	}
	return nil
}

// refreshLinkedEndpointModels 把「前缀 + 引擎模型」后的模型名单写回已链接的网关端点，
// 并把 model_mappings 的 key 与 disabled_models 里的模型名从旧前缀迁移到新前缀，
// 保持三列命名空间一致。未链接/不存在时静默跳过；失败不影响设置保存。
func (s *Service) refreshLinkedEndpointModels(ctx context.Context, oldPrefix string) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	newPrefix := s.modelPrefix()
	// 只有上游拉取成功才用「新前缀 + 最新模型」覆盖模型名单；拉取失败（端点不稳定）
	// 时保留库中已有模型并把旧前缀迁移到新前缀，避免一次瞬时故障把真实模型列表
	// 覆盖成硬编码兜底（defaultModelNames）或空列表。
	models := s.prefixModelNames(s.fetchModelNamesNoFallback(ctx))
	modelsJSON, _ := json.Marshal(models)

	// 读取当前模型映射与禁用列表，做前缀迁移（别名 value 不受前缀影响，保留不动）。
	var modelsRaw, mappingsRaw, disabledRaw sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT models, model_mappings, disabled_models FROM openai_endpoints WHERE id = ?`,
		linkedEndpointID).Scan(&modelsRaw, &mappingsRaw, &disabledRaw)

	// 拉取失败且当前已有模型时：保留旧列表并迁移前缀，不清空也不写兜底。
	if len(models) == 0 && modelsRaw.Valid && modelsRaw.String != "" && modelsRaw.String != "[]" {
		var existing []string
		if json.Unmarshal([]byte(modelsRaw.String), &existing) == nil && len(existing) > 0 {
			migrated := make([]string, 0, len(existing))
			for _, name := range existing {
				migrated = append(migrated, remapPrefixedName(name, oldPrefix, newPrefix))
			}
			modelsJSON, _ = json.Marshal(migrated)
		}
	}

	mappings := map[string]string{}
	if mappingsRaw.Valid && mappingsRaw.String != "" {
		_ = json.Unmarshal([]byte(mappingsRaw.String), &mappings)
	}
	migratedMappings := make(map[string]string, len(mappings))
	for real, alias := range mappings {
		migratedMappings[remapPrefixedName(real, oldPrefix, newPrefix)] = alias
	}
	mappingsJSON, _ := json.Marshal(migratedMappings)

	disabled := []string{}
	if disabledRaw.Valid && disabledRaw.String != "" {
		_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
	}
	migratedDisabled := make([]string, 0, len(disabled))
	for _, name := range disabled {
		migratedDisabled = append(migratedDisabled, remapPrefixedName(name, oldPrefix, newPrefix))
	}
	disabledJSON, _ := json.Marshal(migratedDisabled)

	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, model_mappings = ?, disabled_models = ?, last_checked = ? WHERE id = ?`,
		string(modelsJSON), string(mappingsJSON), string(disabledJSON),
		time.Now().UTC().Format(time.RFC3339), linkedEndpointID)
}

// externalSelector 返回注入的独立代理池选择器。
func (s *Service) externalSelector() ProxyPoolSelector {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.externalPool
}

// resolveProxy 从设置解析出口代理；空表示直连。
func (s *Service) resolveProxy(ctx context.Context) string {
	st := s.Settings()
	if st.ProxyPoolID == "" {
		return ""
	}
	if sel := s.externalSelector(); sel != nil {
		p, _ := sel.SelectProxy(ctx, st.ProxyPoolID, "")
		return p
	}
	return ""
}

// pickAccount 轮询返回下一个启用且有 token 的账号；无可用返回 nil。
func (s *Service) pickAccount() *Account {
	st := s.Settings()
	enabled := make([]Account, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		if a.Disabled {
			continue
		}
		if strings.TrimSpace(a.AccessToken) != "" && strings.TrimSpace(a.ProjectID) != "" {
			enabled = append(enabled, a)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&s.cursor, 1) - 1
	acc := enabled[idx%uint64(len(enabled))]
	return &acc
}

// incrementCall 记录一次账号被选中处理推理请求（计入未落盘增量，定期合并落库）。
func (s *Service) incrementCall(email string) {
	if email == "" {
		return
	}
	s.callMu.Lock()
	defer s.callMu.Unlock()
	if s.callCounts == nil {
		s.callCounts = map[string]int64{}
	}
	s.callCounts[email]++
}

// callCount 返回账号持久化的累计调用次数 = 已落盘基线 + 未落盘增量。
func (s *Service) callCount(email string) int64 {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.callBase[email] + s.callCounts[email]
}

// loadCallStats 从 DB 恢复已落盘的调用累计基线。
func (s *Service) loadCallStats(db *sql.DB) {
	rows, err := db.Query(`SELECT email, count FROM antigravity_call_stats`)
	if err != nil {
		return
	}
	defer rows.Close()
	base := map[string]int64{}
	for rows.Next() {
		var email string
		var c int64
		if rows.Scan(&email, &c) == nil {
			base[email] = c
		}
	}
	s.callMu.Lock()
	s.callBase = base
	s.callMu.Unlock()
}

// flushCallStats 把未落盘增量合并进 DB，并同步进内存基线。
// 逐条 UPSERT 累加，避免先读后写在并发下丢增量。
func (s *Service) flushCallStats(ctx context.Context) {
	s.callMu.Lock()
	if len(s.callCounts) == 0 {
		s.callMu.Unlock()
		return
	}
	pend := s.callCounts
	s.callCounts = map[string]int64{}
	s.callMu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		// 落库失败时把增量放回，等待下轮重试，避免计数丢失。
		s.callMu.Lock()
		for k, v := range pend {
			s.callCounts[k] += v
		}
		s.callMu.Unlock()
		return
	}
	defer db.Close()
	for email, delta := range pend {
		_, _ = db.ExecContext(ctx, `
			INSERT INTO antigravity_call_stats (email, count) VALUES (?, ?)
			ON CONFLICT(email) DO UPDATE SET count = count + excluded.count`, email, delta)
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
	s.callFlushOnce.Do(func() {
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

// forwardBaseURL 按账号 planType 选择转发端点。
// 与 Antigravity 官方一致：付费账号（Pro/Ultra）使用 daily 端点，其余用 prod。
func forwardBaseURL(acc *Account) string {
	urls := engineag.ForwardBaseURLs()
	if len(urls) == 0 {
		return engineag.BaseURL
	}
	plan := strings.ToLower(strings.TrimSpace(acc.PlanType))
	if plan == "pro" || plan == "ultra" {
		return urls[0]
	}
	return engineag.BaseURL
}

// ensureFreshToken 返回有效 access_token；过期则用 refresh_token 刷新并落库。
func (s *Service) ensureFreshToken(ctx context.Context, acc *Account) (string, error) {
	if strings.TrimSpace(acc.AccessToken) != "" && acc.ExpiresAt > time.Now().Unix()+60 {
		return acc.AccessToken, nil
	}
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return "", fmt.Errorf("账号 %s token 已过期且无 refresh_token", acc.Email)
	}
	agClient, err := engineag.NewClient("")
	if err != nil {
		return "", err
	}
	tok, err := agClient.RefreshToken(ctx, acc.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("刷新 token 失败: %w", err)
	}
	if tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return "", fmt.Errorf("刷新 token 返回空")
	}
	newExpires := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	if tok.ExpiresIn <= 0 {
		newExpires = time.Now().Add(50 * time.Minute).Unix()
	}
	s.mu.Lock()
	for i := range s.settings.Accounts {
		if strings.EqualFold(s.settings.Accounts[i].Email, acc.Email) {
			s.settings.Accounts[i].AccessToken = tok.AccessToken
			if tok.RefreshToken != "" {
				s.settings.Accounts[i].RefreshToken = tok.RefreshToken
			}
			s.settings.Accounts[i].ExpiresAt = newExpires
		}
	}
	updated := s.settings
	s.mu.Unlock()
	if err := s.SaveSettings(ctx, updated); err != nil {
		// 落库失败不阻断本次请求。
		return tok.AccessToken, nil
	}
	return tok.AccessToken, nil
}

// hasAuthorizedAccounts 是否至少有一个可用账号。
func (s *Service) hasAuthorizedAccounts() bool {
	return s.pickAccount() != nil
}

// accountCount 返回已授权账号数量。
func (s *Service) accountCount() int {
	return len(s.Settings().Accounts)
}

// ForwardClaude 转发单次 Claude Messages 请求到 Antigravity 上游。
// stream=true 时 writes 直接输出 SSE；否则收集完整响应返回。
func (s *Service) ForwardClaude(ctx context.Context, w http.ResponseWriter, body []byte, stream bool) error {
	st := s.Settings()
	if !st.Enabled {
		return fmt.Errorf("插件未启用")
	}
	acc := s.pickAccount()
	if acc == nil {
		return fmt.Errorf("尚无可用账号，请先完成 Google 账号授权")
	}
	s.incrementCall(acc.Email)
	var claudeReq engineag.ClaudeRequest
	if err := json.Unmarshal(body, &claudeReq); err != nil {
		return fmt.Errorf("请求体解析失败: %w", err)
	}
	if strings.TrimSpace(claudeReq.Model) == "" {
		return fmt.Errorf("缺少 model")
	}
	originalModel := claudeReq.Model
	mappedModel := s.resolveUpstreamModel(claudeReq.Model)

	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return fmt.Errorf("构造客户端失败: %w", err)
	}

	opts := engineag.DefaultTransformOptions()
	opts.EnableIdentityPatch = true
	geminiBody, err := engineag.TransformClaudeToGeminiWithOptions(&claudeReq, acc.ProjectID, mappedModel, opts)
	if err != nil {
		return fmt.Errorf("请求转换失败: %w", err)
	}

	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	base := forwardBaseURL(acc)
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return fmt.Errorf("获取访问凭证失败: %w", err)
	}
	req, err := engineag.NewAPIRequestWithURL(ctx, base, action, freshToken, geminiBody)
	if err != nil {
		return fmt.Errorf("构造上游请求失败: %w", err)
	}

	resp, err := agClient.Do(req)
	if err != nil {
		return fmt.Errorf("上游请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("上游返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if !stream {
		full, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("读取上游响应失败: %w", err)
		}
		claudeOut, _, err := engineag.TransformGeminiToClaude(full, originalModel)
		if err != nil {
			return fmt.Errorf("响应转换失败: %w", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(claudeOut)
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	processor := engineag.NewStreamingProcessor(originalModel)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		event := processor.ProcessLine(line)
		if len(event) > 0 {
			if _, err := w.Write(event); err != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	// 收尾：上游流正常结束或中途被截断时，补发缺失的结束事件（message_delta/message_stop）。
	// Finish 内部对 messageStartSent/messageStopSent 有守卫，正常结束不会产生重复事件。
	if tail, _ := processor.Finish(); len(tail) > 0 {
		if _, err := w.Write(tail); err != nil {
			return nil
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取上游响应流失败: %w", err)
	}
	return nil
}

// FetchModels 用指定账号从上游拉取模型列表。
func (s *Service) FetchModels(ctx context.Context, email string) (map[string]engineag.ModelInfo, error) {
	acc := s.findAccount(email)
	if acc == nil {
		return nil, fmt.Errorf("账号不存在或未授权")
	}
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return nil, err
	}
	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return nil, err
	}
	models, _, err := agClient.FetchAvailableModels(ctx, freshToken, acc.ProjectID, 8*1024*1024)
	if err != nil {
		return nil, err
	}
	return models.Models, nil
}

// QuotaView 是账号配额摘要的归一化展示结构。
type QuotaView struct {
	Email      string               `json:"email"`
	PlanType   string               `json:"planType"`
	Credits    []engineag.AvailableCredit `json:"credits,omitempty"`
	Groups     []engineag.UserQuotaGroup `json:"groups,omitempty"`
	Raw        map[string]any       `json:"-"`
}

// FetchQuota 拉取指定账号的配额摘要：AI Credits 余额 + bucket 窗口。
func (s *Service) FetchQuota(ctx context.Context, email string) (*QuotaView, error) {
	acc := s.findAccount(email)
	if acc == nil {
		return nil, fmt.Errorf("账号不存在或未授权")
	}
	freshToken, err := s.ensureFreshToken(ctx, acc)
	if err != nil {
		return nil, err
	}
	proxyURI := s.resolveProxy(ctx)
	agClient, err := engineag.NewClient(proxyURI)
	if err != nil {
		return nil, err
	}
	view := &QuotaView{Email: acc.Email, PlanType: acc.PlanType}
	loadResp, _, err := agClient.LoadCodeAssist(ctx, freshToken)
	if err == nil && loadResp != nil && loadResp.PaidTier != nil {
		view.Credits = loadResp.GetAvailableCredits()
	} else if err != nil {
		applog.Warn(nil, "antigravity", "拉取 AI Credits 失败", "email", acc.Email, "error", err.Error())
	}
	summary, raw, err := agClient.FetchUserQuotaSummary(ctx, freshToken, acc.ProjectID)
	if err == nil && summary != nil {
		view.Groups = summary.Groups
		view.Raw = raw
	} else if err != nil {
		applog.Warn(nil, "antigravity", "拉取配额窗口失败", "email", acc.Email, "error", err.Error())
	}
	if view.Credits == nil && len(view.Groups) == 0 {
		return nil, fmt.Errorf("上游未返回配额信息")
	}
	return view, nil
}

// findAccount 按 email 查找账号。
func (s *Service) findAccount(email string) *Account {	if email == "" {
		return s.pickAccount()
	}
	st := s.Settings()
	for i := range st.Accounts {
		if strings.EqualFold(st.Accounts[i].Email, email) {
			return &st.Accounts[i]
		}
	}
	return nil
}

// quotaMonitorInterval 是配额刷新检测的轮询周期。
const quotaMonitorInterval = 15 * time.Minute

// quotaMonitorCycleTimeout 是单轮检测的整体预算，超时即中止不阻塞下一轮。
const quotaMonitorCycleTimeout = 60 * time.Second

// StartQuotaMonitor 后台周期性检测各账号配额窗口是否在消耗/刷新。
// 开关（QuotaMonitorEnabled）关闭时静默跳过；进程结束（ctx 取消）时退出。
func (s *Service) StartQuotaMonitor(ctx context.Context) {
	s.quotaMonitorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(quotaMonitorInterval)
			defer ticker.Stop()
			s.quotaMonitorOnceNow(ctx)
			for {
				select {
				case <-ticker.C:
					s.quotaMonitorOnceNow(ctx)
				case <-ctx.Done():
					return
				}
			}
		}()
	})
}

// quotaMonitorOnceNow 执行一轮配额检测：拉取各账号 bucket 剩余比例，
// 跨轮次对比；剩余比例上升（窗口已刷新/重置）时触发通知。
// 比例下降（消耗中）与持平（冻结）不通知；新出现的窗口 key 只入基线，避免误报。
func (s *Service) quotaMonitorOnceNow(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	st := s.Settings()
	if !st.QuotaMonitorEnabled {
		return
	}

	cycleCtx, cancel := context.WithTimeout(ctx, quotaMonitorCycleTimeout)
	defer cancel()

	// 首轮之前只建基线不通知，避免冷启动误报。
	s.quotaPrevMu.Lock()
	if s.quotaPrev == nil {
		s.quotaPrev = map[string]float64{}
	}
	prev := s.quotaPrev
	firstRound := len(prev) == 0
	s.quotaPrevMu.Unlock()

	now := map[string]float64{}
	eligible := 0
	failed := 0
	for _, acc := range st.Accounts {
		if acc.Disabled || strings.TrimSpace(acc.AccessToken) == "" || strings.TrimSpace(acc.ProjectID) == "" {
			continue
		}
		eligible++
		view, err := s.FetchQuota(cycleCtx, acc.Email)
		if err != nil || view == nil {
			failed++
			continue
		}
		for _, g := range view.Groups {
			for _, b := range g.Buckets {
				key := acc.Email + "\x00" + b.Window + "\x00" + b.DisplayName
				now[key] = b.RemainingFraction
			}
		}
	}
	if failed > 0 {
		applog.Warn(nil, "antigravity", "本轮配额检测存在拉取失败账号", "failed", failed, "total", eligible)
	}

	if firstRound || len(now) == 0 {
		s.quotaPrevMu.Lock()
		for k, v := range now {
			s.quotaPrev[k] = v
		}
		s.quotaPrevMu.Unlock()
		return
	}

	changed := map[string]float64{}
	s.quotaPrevMu.Lock()
	for k, cur := range now {
		last, ok := s.quotaPrev[k]
		if !ok {
			continue
		}
		// 值上升 = 窗口已刷新；值不变 = 冻结（额度未用时间浪费）；下降 = 消耗中。
		if cur-last > 0.0001 {
			changed[k] = cur
		}
	}
	s.quotaPrev = now
	s.quotaPrevMu.Unlock()

	for k, cur := range changed {
		parts := strings.Split(k, "\x00")
		if len(parts) != 3 {
			continue
		}
		email, window, displayName := parts[0], parts[1], parts[2]
		label := displayName
		if label == "" {
			label = window
		}
		if label == "" {
			label = "配额窗口"
		}
		s.notifier.Trigger(ctx, "antigravity", "quota_window_refreshed", map[string]interface{}{
			"email":             email,
			"window":            window,
			"displayName":       displayName,
			"remainingFraction": cur,
		})
		applog.Info(nil, "antigravity", "配额窗口已刷新", "email", email, "window", label, "remaining_fraction", cur)
	}
}


// syncEndpointModels 在插件启动后异步同步模型列表到网关端点表（只更新 models 列）。
func (s *Service) syncEndpointModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	// 获取一个启用账号
	acc := s.pickAccount()
	if acc == nil {
		return
	}
	// 获取模型列表
	modelsMap, err := s.FetchModels(ctx, acc.Email)
	if err != nil {
		return
	}
	// 提取模型ID并加前缀
	ids := make([]string, 0, len(modelsMap))
	for id := range modelsMap {
		ids = append(ids, id)
	}
	prefixed := s.prefixModelNames(ids)
	modelsJSON, _ := json.Marshal(prefixed)
	// 只更新 models 列，不动映射/禁用
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, last_checked = ? WHERE id = ?`,
		string(modelsJSON), time.Now().UTC().Format(time.RFC3339), linkedEndpointID)
}
