package ds2api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	engineserver "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/server"
)

// newURLFrom 返回一个把 path 换成 stripped 的新 URL（保留其它字段）。
func newURLFrom(u *url.URL, stripped string) *url.URL {
	cp := *u
	cp.Path = stripped
	cp.RawPath = ""
	return &cp
}

// internalKey 是插件内部固定调用密钥：仅在 loopback 内部转发时注入，用于通过
// 引擎自身的 API Key 校验，无需用户为插件配置任何 key。
const internalKey = "sk-ds2api-internal"

// isLoopback 判断请求来源是否为本机回环地址。
func isLoopback(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Settings 是 DS2API 插件持久化配置。
// 引擎核心配置（账号池 / API keys / 模型别名 / 代理）以 config JSON 形式随
// 插件设置落库；启用时写入 data 目录的 config.json 并实例化引擎 App。
type Settings struct {
	// Enabled 总开关；关闭时引擎不实例化，/v1/* 返回 404。
	Enabled bool `json:"enabled"`
	// ConfigJSON 引擎 config.json 的完整内容（ds2api 原生格式）。
	ConfigJSON string `json:"configJson"`
	// ProxyPoolID 引用独立代理池插件的池；启用时写入引擎 config 的 proxy 相关字段。
	ProxyPoolID string `json:"proxyPoolId"`
	// DisabledModels 被停用的模型（引擎列表中不对外提供）。
	DisabledModels []string `json:"disabledModels"`
	// ModelPrefix 对外模型名统一前缀（如 "ds2-"）：模型列表暴露的名字带此前缀，
	// 转发时剥掉前缀再交给引擎；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
}

func defaultSettings() Settings {
	return Settings{
		Enabled:        false,
		ConfigJSON:     `{"keys":[],"accounts":[],"models":{}}`,
		DisabledModels: []string{},
	}
}

// Service 是 DS2API 插件后端：持有内嵌引擎 App，暴露 OpenAI 兼容端点。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings
	app      *engineserver.App
	engineMu sync.Mutex

	// 调用次数持久化：callBase 为已落盘累计基线，callPending 为尚未落盘的增量。
	callMu      sync.Mutex
	callBase    map[string]int64
	callPending map[string]int64
	callFlush   sync.Once

	externalPool ProxyPoolSelector
}

// ProxyPoolSelector 复用独立代理池选择器（server 注入）。
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
		callBase:   map[string]int64{},
		callPending: map[string]int64{},
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ds2api_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("ds2api ensure schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ds2api_call_stats (
		identifier TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("ds2api ensure call stats schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM ds2api_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := json.Marshal(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO ds2api_settings (id, data) VALUES (1, ?)`, string(data))
		return
	}
	var cfg Settings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		cfg = defaultSettings()
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	// 启动时优先复用已有落盘 config.json：引擎运行期把账号冻结/禁用/禁言/冷却等
	// 状态直接写进 config.json，不回写插件设置 DB 的 ConfigJSON。若用 DB 里可能
	// 过期的 ConfigJSON 覆盖，会丢失这些运行期状态（重启后账号误显示为启用）。
	// config.json 缺失或不合法时才退回 DB ConfigJSON。此处仅在进程启动加载时
	// 生效，用户后续通过设置/配置对话框保存走 SaveSettings 仍以 DB 为准。
	if content, err := os.ReadFile(s.configPath()); err == nil && len(bytes.TrimSpace(content)) > 0 {
		var probe map[string]interface{}
		if json.Unmarshal(content, &probe) == nil {
			cfg.ConfigJSON = string(content)
		}
	}
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
	s.loadCallStats(ctx, db)
	if cfg.Enabled {
		_ = s.startEngineLocked()
	}
}

// Settings 返回当前设置的只读副本。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.DisabledModels = append([]string(nil), s.settings.DisabledModels...)
	return out
}

// SaveSettings 校验并持久化设置，随后按 Enabled 启停引擎。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.ConfigJSON == "" {
		next.ConfigJSON = defaultSettings().ConfigJSON
	}
	if next.DisabledModels == nil {
		next.DisabledModels = []string{}
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	data, _ := json.Marshal(next)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ds2api_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, string(data)); err != nil {
		return err
	}
	s.mu.Lock()
	oldPrefix := s.settings.ModelPrefix
	oldCfgJSON := s.settings.ConfigJSON
	s.settings = next
	s.mu.Unlock()
	if next.ModelPrefix != oldPrefix {
		// 前缀变更后同步已链接网关端点的模型名单、映射与禁用状态。
		s.refreshLinkedEndpointModels(ctx, oldPrefix)
	}
	s.engineMu.Lock()
	defer s.engineMu.Unlock()
	if next.Enabled {
		// 引擎配置未变且引擎已在运行：仅改前缀/禁用模型等设置时无需重建引擎，
		// 避免账号池 token 缓存、内存内容缓存被反复重置，也避免反复拉起 mihomo 子进程。
		if next.ConfigJSON == oldCfgJSON && s.app != nil {
			return nil
		}
		return s.startEngineLocked()
	}
	s.app = nil
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
	models := s.prefixModelNames(s.engineModelNames(ctx))
	modelsJSON, _ := json.Marshal(models)

	// 读取当前模型映射与禁用列表，做前缀迁移（别名 value 不受前缀影响，保留不动）。
	var mappingsRaw, disabledRaw sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT model_mappings, disabled_models FROM openai_endpoints WHERE id = ?`,
		linkedEndpointID).Scan(&mappingsRaw, &disabledRaw)

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

// modelPrefix 返回归一化的对外模型前缀（去空白；空串表示不加前缀）。
func (s *Service) modelPrefix() string {
	return strings.TrimSpace(s.Settings().ModelPrefix)
}

// prefixModel 把内部模型 ID 加上对外前缀，空前缀时原样返回。
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

// prefixModelNames 批量对模型清单加前缀。
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

// loadCallStats 从 DB 恢复已落盘的调用累计基线。
func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM ds2api_call_stats`)
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

// recordCall 是 Resolver 注入的账号调用回调：把本次调用记入未落盘增量。
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

// callDisplay 返回账号已持久化的累计调用次数 = 已落盘基线 + 未落盘增量。
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
			INSERT INTO ds2api_call_stats (identifier, count) VALUES (?, ?)
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
				case <-ctx.Done():
					s.flushCallStats(context.Background())
					return
				}
			}
		}()
	})
}

// configPath 返回引擎 config.json 落盘路径（data 目录下）。
func (s *Service) configPath() string {
	return filepath.Join(s.cfg.DataDir, "ds2api", "config.json")
}

// startEngineLocked 写入配置并实例化引擎 App。须持有 engineMu。
func (s *Service) startEngineLocked() error {
	// 重建前先释放旧引擎的后台资源（mihomo 子进程、内容缓存清理循环），
	// 避免重复拉起子进程与清理 goroutine 造成进程/goroutine/内存泄漏。
	if old := s.app; old != nil {
		old.Stop()
	}
	st := s.Settings()
	dir := filepath.Join(s.cfg.DataDir, "ds2api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建 ds2api 目录失败: %w", err)
	}
	cfgPath := s.configPath()
	cfgJSON, err := ensureInternalKey(st.ConfigJSON)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		return fmt.Errorf("写入引擎配置失败: %w", err)
	}
	_ = os.Setenv("DS2API_CONFIG_PATH", cfgPath)
	app, err := engineserver.NewApp()
	if err != nil {
		return fmt.Errorf("初始化引擎失败: %w", err)
	}
	if app.Resolver != nil {
		app.Resolver.SetCallObserver(s.recordCall)
	}
	s.app = app
	if app != nil {
		go s.syncEndpointModels()
	}
	return nil
}

// ensureInternalKey 保证引擎 config 的 keys 列表包含插件内部密钥，
// 使 loopback 内部调用免外部 key 也能通过引擎校验。
func ensureInternalKey(cfgJSON string) (string, error) {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return cfgJSON, nil
	}
	keys, _ := cfg["keys"].([]interface{})
	has := false
	for _, k := range keys {
		if str, ok := k.(string); ok && str == internalKey {
			has = true
			break
		}
	}
	if !has {
		cfg["keys"] = append(keys, internalKey)
		data, err := json.Marshal(cfg)
		if err != nil {
			return cfgJSON, fmt.Errorf("注入内部密钥失败: %w", err)
		}
		cfgJSON = string(data)
	}
	return cfgJSON, nil
}

// ServeHTTP 是 DS2API 插件总入口：
//   - /v1/*、/admin/*：内嵌引擎路由（OpenAI 兼容 + 引擎管理页）
//   - /api/ds2api/*：插件管理接口
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/ds2api/settings":
		s.handleSettings(w, r)
	case path == "/api/ds2api/status":
		s.handleStatus(w, r)
	case path == "/api/ds2api/test":
		s.handleTest(w, r)
	case path == "/api/ds2api/models":
		s.handleModels(w, r)
	case path == "/api/ds2api/models/toggle":
		s.handleToggleModel(w, r, "")
	case path == "/api/ds2api/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/ds2api/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/ds2api/models/toggle/"))
	case path == "/api/ds2api/link":
		s.handleLink(w, r)
	case path == "/api/ds2api/accounts/export":
		s.handleExportAccounts(w, r)
	case path == "/api/ds2api/accounts/import":
		s.handleImportAccounts(w, r)
	case path == "/api/ds2api/accounts":
		s.handleAccounts(w, r)
	case strings.HasPrefix(path, "/api/ds2api/accounts/"):
		rest := strings.TrimPrefix(path, "/api/ds2api/accounts/")
		switch {
		case strings.HasSuffix(rest, "/test"):
			s.handleTestAccount(w, r, strings.TrimSuffix(rest, "/test"))
		case strings.HasSuffix(rest, "/toggle"):
			s.handleToggleAccount(w, r, strings.TrimSuffix(rest, "/toggle"))
		case r.Method == http.MethodPut:
			s.handleUpdateAccount(w, r, rest)
		default:
			s.handleDeleteAccount(w, r, rest)
		}
	default:
		s.mu.RLock()
		app := s.app
		enabled := s.settings.Enabled
		s.mu.RUnlock()
		if !enabled || app == nil || app.Router == nil {
			http.NotFound(w, r)
			return
		}
		// 引擎 Router 只认识 /v1/... 等相对路径，需剥掉 /api/ds2api 前缀。
		stripped := strings.TrimPrefix(path, "/api/ds2api")
		if stripped == "" {
			stripped = "/"
		}
		rr := r.Clone(r.Context())
		rr.URL = newURLFrom(r.URL, stripped)
		// 本机内部调用（模型网关经 loopback 转发）免 key：注入插件内部密钥
		// 通过引擎校验；外部请求已被 server 层 AuthInternal 拒绝。
		// 注意：net/http 会 trim 头值尾随空格，`Bearer ` 到达时已是 `Bearer`，
		// 因此解析时用 "Bearer" 前缀剥离（不依赖尾随空格）。
		if isLoopback(r.RemoteAddr) && strings.HasPrefix(stripped, "/v1/") {
			auth := rr.Header.Get("Authorization")
			caller := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer"))
			if caller == "" {
				caller = strings.TrimSpace(rr.Header.Get("X-API-Key"))
			}
			if caller == "" {
				rr.Header.Set("Authorization", "Bearer "+internalKey)
			}
		}
		// /v1/models GET：网关验证/刷新端点模型时调用，带前缀输出。
		if r.Method == http.MethodGet && (stripped == "/v1/models" || stripped == "/v1/models/") {
			s.servePrefixedModels(w)
			return
		}
		// POST 请求：模型名剥本插件前缀后转交引擎（模型在 JSON body 里）。
		// 无前缀时直接穿透，不读 body。
		if s.modelPrefix() != "" && r.Method == http.MethodPost && strings.HasPrefix(stripped, "/v1/") {
			rr = s.stripModelPrefixInRequest(rr)
		}
		app.Router.ServeHTTP(w, rr)
	}
}

// servePrefixedModels 给网关验证 / 外部客户端展示带前缀的模型列表。
func (s *Service) servePrefixedModels(w http.ResponseWriter) {
	names := s.engineModelNames(context.Background())
	out := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]interface{}{
			"id":       s.prefixModel(n),
			"object":   "model",
			"created":  0,
			"owned_by": "ds2api",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": out})
}

// stripModelPrefixInRequest 读取请求体，剥掉 model 字段的前缀后复构建请求。
func (s *Service) stripModelPrefixInRequest(r *http.Request) *http.Request {
	if r.Body == nil {
		return r
	}
	// 模型 JSON body 体积限额（16MB），超出安全直接：剥掉 *可选* 前缀
	// 仅从 JSON object 顶层读 model 字段处理，其余字段保留不动。
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024+512))
	_ = r.Body.Close()
	if err != nil || len(body) == 0 {
		r.Body = io.NopCloser(rbodyReset(body))
		return r
	}
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return r
	}
	if m, ok := payload["model"].(string); ok {
		m = strings.TrimSpace(m)
		if strings.HasPrefix(m, s.modelPrefix()) {
			payload["model"] = s.stripModelPrefix(m)
			out, _ := json.Marshal(payload)
			r.Body = io.NopCloser(bytes.NewReader(out))
			r.ContentLength = int64(len(out))
			return r
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return r
}

// rbodyReset 用一个空 reader 替代已读尽的 body（避免 body 已被读到 EOF 而无法重读）。
func rbodyReset(b []byte) io.Reader { return bytes.NewReader(b) }

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	s.mu.RLock()
	engineUp := s.app != nil
	s.mu.RUnlock()
	responseJSON(w, map[string]interface{}{
		"enabled":      st.Enabled,
		"engineUp":     engineUp,
		"proxyPoolId":  st.ProxyPoolID,
		"configBytes":  len(st.ConfigJSON),
	})
}

func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.Settings()})
	case http.MethodPut, http.MethodPost:
		var body Settings
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
			return
		}
		if err := s.SaveSettings(r.Context(), body); err != nil {
			responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		responseJSON(w, map[string]interface{}{"success": true, "settings": s.Settings()})
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	s.mu.RLock()
	engineUp := s.app != nil
	s.mu.RUnlock()
	responseJSON(w, map[string]interface{}{"success": engineUp})
}

// helper：本包轻量 JSON 输出（避免与引擎的 httpapi 冲突）。
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

// syncEndpointModels 在引擎启动后异步同步模型列表到网关端点表（只更新 models 列）。
func (s *Service) syncEndpointModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	// 获取引擎模型列表并加前缀
	names := s.engineModelNames(ctx)
	prefixed := s.prefixModelNames(names)
	modelsJSON, _ := json.Marshal(prefixed)
	// 只更新 models 列，不动映射/禁用
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, last_checked = ? WHERE id = ?`,
		string(modelsJSON), time.Now().UTC().Format(time.RFC3339), linkedEndpointID)
}
