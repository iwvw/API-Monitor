// Package opencode 实现模型网关「插件中心」的 OpenCode 插件：把 OpenCode Zen
// 的免费模型（mimo / longcat / muse-spark 等）封装成 OpenAI 兼容上游。
//
// 原理（对齐开源参考 opencode-proxy）：
// Zen 免费层只对「看起来像 OpenCode 客户端」的请求放行——需要携带真实会话身份头
// （User-Agent + x-opencode-* / x-session-*）+ agentic 载荷里的 read/shell 工具
// 定义。本插件在每个请求上重放这套握手，再把标准 OpenAI 请求转发到上游，并把
// muse-spark 系列经 /zen/v1/responses 的翻译以 OpenAI chat 协议回给调用方。
//
// 管理面挂在 /api/opencode，中继面挂在 /api/opencode/v1（仅本机回环，供网关转发）。
// 结构与同类插件（geminicli/workbuddy/ds2api）保持一致。
package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// internalKey 是插件内部固定调用密钥：仅在本机回环转发时注入，用于通过
// /api/opencode/v1 的内部校验，用户无需为插件配置任何访问密钥。
const internalKey = "sk-opencode-internal"

// providerName 是中继面模型列表里的 owned_by 标识。
const providerName = "opencode"

// upstreamBase 是 OpenCode Zen 上游基址（固定，与参考实现一致）。
const upstreamBase = "https://opencode.ai"

// freeKey 是免费模型接受的字面 key，与全新安装且未配置 key 的 opencode 一致。
const freeKey = "public"

// Settings 是 OpenCode 插件的持久化配置。
type Settings struct {
	// Enabled 总开关；关闭时上游调用与网关接入都拒绝服务。
	Enabled bool `json:"enabled"`
	// APIKey 是可选的 Zen 付费 key；留空时免费模型使用字面 key "public"。
	APIKey string `json:"apiKey,omitempty"`
	// Session 是重放给上游的 OpenCode 客户端身份（来自真实 opencode 会话）。
	Session SessionIdentity `json:"session"`
	// ModelPrefix 对外模型名统一前缀（如 "oc-"）：模型列表暴露的名字带此前缀，
	// 转发时剥掉前缀再交给上游；空串表示不加前缀。
	ModelPrefix string `json:"modelPrefix,omitempty"`
	// ProxyPoolID 引用独立代理池插件的池；为空表示直连。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
	// DisabledModels 被停用的模型（不在 /v1/models 对外提供）。
	DisabledModels []string `json:"disabledModels"`
}

// SessionIdentity 是要重放给 Zen 上游的 OpenCode 会话身份。字段命名与
// opencode-proxy 的 session.json 一致，便于用户直接粘贴参考实现抓到的身份。
type SessionIdentity struct {
	UserAgent        string `json:"userAgent,omitempty"`
	XOpencodeClient  string `json:"xOpencodeClient,omitempty"`
	XOpencodeOrgID   string `json:"xOpencodeOrgId,omitempty"`
	XOpencodeProject string `json:"xOpencodeProject,omitempty"`
	XOpencodeSession string `json:"xOpencodeSession,omitempty"`
	XSessionID       string `json:"xSessionId,omitempty"`
	XSessionAffinity string `json:"xSessionAffinity,omitempty"`
}

func defaultSettings() Settings {
	return Settings{
		DisabledModels: []string{},
		Session: SessionIdentity{
			UserAgent:       "opencode/latest/2.0.12/cli",
			XOpencodeClient: "cli",
		},
	}
}

// Service 是 OpenCode 插件后端。
type Service struct {
	cfg   config.Config
	store *database.Store

	mu       sync.RWMutex
	settings Settings

	externalPool ProxyPoolSelector
}

// ProxyPoolSelector 复用独立代理池选择器（由 server 注入）。
// 与 openai 网关保持同构：SelectProxy 选出口，ReportResult 回写健康，让插件转发
// 也能冻结坏出口（403/429/5xx），避免坏 slot 被反复轮询命中。
type ProxyPoolSelector interface {
	SelectProxy(ctx context.Context, poolID, sessionKey string) (string, error)
	ReportResult(ctx context.Context, poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) error
}

// SetProxyPoolSelector 注入独立代理池选择器。
func (s *Service) SetProxyPoolSelector(sel ProxyPoolSelector) {
	s.externalPool = sel
}

// New 构造服务并加载持久化设置。
func New(cfg config.Config) *Service {
	s := &Service{
		cfg:   cfg,
		store: database.New(cfg),
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS opencode_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("opencode ensure schema: %w", err)
	}
	return db, nil
}

func (s *Service) loadSettings(ctx context.Context, db *sql.DB) {
	row := db.QueryRowContext(ctx, `SELECT data FROM opencode_settings WHERE id = 1`)
	var raw string
	if err := row.Scan(&raw); err != nil {
		cfg := defaultSettings()
		s.mu.Lock()
		s.settings = cfg
		s.mu.Unlock()
		data, _ := secure.EncryptJSON(cfg)
		_, _ = db.ExecContext(ctx, `INSERT INTO opencode_settings (id, data) VALUES (1, ?)`, data)
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
		if enc, err := secure.EncryptJSON(cfg); err == nil {
			_, _ = db.ExecContext(ctx, `UPDATE opencode_settings SET data = ? WHERE id = 1`, enc)
		}
	}
	if cfg.DisabledModels == nil {
		cfg.DisabledModels = []string{}
	}
	s.mu.Lock()
	s.settings = cfg
	s.mu.Unlock()
}

// Settings 返回当前设置的只读副本。
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.DisabledModels = append([]string(nil), s.settings.DisabledModels...)
	return out
}

// SaveSettings 校验并持久化设置。前缀变化时同步已接入网关端点的模型名单。
func (s *Service) SaveSettings(ctx context.Context, next Settings) error {
	if next.DisabledModels == nil {
		next.DisabledModels = []string{}
	}
	next.APIKey = strings.TrimSpace(next.APIKey)
	session := strings.TrimSpace(next.Session.XSessionID)
	if session != "" {
		next.Session.XSessionID = session
	}
	s.mu.RLock()
	oldPrefix := s.settings.ModelPrefix
	s.mu.RUnlock()

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	enc, err := secure.EncryptJSON(next)
	if err != nil {
		return fmt.Errorf("opencode settings encrypt: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO opencode_settings (id, data) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`, enc); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()

	if next.ModelPrefix != oldPrefix {
		s.writeLinkedEndpointModels(ctx, s.allModelIDs())
	}
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
}

// -----------------------------------------------------------------------------
// HTTP 入口
// -----------------------------------------------------------------------------

// ServeHTTP 是 OpenCode 插件总入口：
//   - /api/opencode/*：插件管理接口（会话鉴权）
//   - /api/opencode/v1/*：OpenAI 兼容中继（仅本机回环）
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/api/opencode/settings":
		s.handleSettings(w, r)
	case path == "/api/opencode/status":
		s.handleStatus(w, r)
	case path == "/api/opencode/test":
		s.handleTest(w, r)
	case path == "/api/opencode/models":
		s.handleModels(w, r)
	case path == "/api/opencode/models/toggle-batch":
		s.handleBatchToggleModels(w, r)
	case strings.HasPrefix(path, "/api/opencode/models/toggle/"):
		s.handleToggleModel(w, r, strings.TrimPrefix(path, "/api/opencode/models/toggle/"))
	case path == "/api/opencode/link":
		s.handleLink(w, r)
	default:
		// 中继面：/api/opencode/v1/...
		stripped := strings.TrimPrefix(path, "/api/opencode")
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
		// 管理面不回传其他字段的凭据；DisabledModels 由专用启停接口持有。
		current := s.Settings()
		body.DisabledModels = current.DisabledModels
		if body.Session.UserAgent == "" {
			body.Session.UserAgent = current.Session.UserAgent
		}
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

// publicSettings 返回可下发前端的设置（APIKey 脱敏为是否存在标记）。
func (s *Service) publicSettings() map[string]interface{} {
	st := s.Settings()
	return map[string]interface{}{
		"enabled":        st.Enabled,
		"hasApiKey":      st.APIKey != "",
		"session":        st.Session,
		"modelPrefix":    st.ModelPrefix,
		"proxyPoolId":    st.ProxyPoolID,
		"disabledModels": st.DisabledModels,
	}
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings()
	sessionOK := strings.TrimSpace(st.Session.XSessionID) != ""
	responseJSON(w, map[string]interface{}{
		"enabled":       st.Enabled,
		"upstreamReady": upstreamImplemented,
		"sessionReady":  sessionOK,
		"modelCount":    len(s.catalog()),
		"linkBaseUrl":   s.linkBaseURL(),
	})
}

func (s *Service) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "modelCount": len(s.catalog())})
}

// -----------------------------------------------------------------------------
// 小工具
// -----------------------------------------------------------------------------

// upstreamImplemented 标记上游协议层已实现（骨架期用于前端展示）。
const upstreamImplemented = true

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
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/opencode/v1"
}

// -----------------------------------------------------------------------------
// HTTP 客户端
// -----------------------------------------------------------------------------

var (
	ocHTTPClientOnce sync.Once
	ocSharedClient   *http.Client
)

// sharedHTTPClient 返回不带代理池的共享客户端。
// Transport 显式走 ProxyFromEnvironment：Go 不读 Windows 系统代理，但认
// HTTPS_PROXY / HTTP_PROXY / NO_PROXY 环境变量，这对本机开发（全局代理）很关键。
func sharedHTTPClient() *http.Client {
	ocHTTPClientOnce.Do(func() {
		ocSharedClient = &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				MaxIdleConnsPerHost: 5,
			},
		}
	})
	return ocSharedClient
}

// selectProxyAndClient 选择本次出网出口：配置了代理池且已注入选择器时经池选出口，
// 返回该出口 URL（直连为空串）与对应客户端；选路失败静默回退直连。
func (s *Service) selectProxyAndClient(ctx context.Context, poolID, sessionKey string) (string, *http.Client) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" || s.externalPool == nil {
		return "", sharedHTTPClient()
	}
	raw, err := s.externalPool.SelectProxy(ctx, poolID, sessionKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", sharedHTTPClient()
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", sharedHTTPClient()
	}
	return strings.TrimSpace(raw), &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(u),
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 5,
		},
	}
}

// reportPoolResult 把一次出口使用结果回写给独立代理池：失败（403/429/5xx/网络错）
// 会按 ratelimit 分别冻结为 429/cool，成功清除健康状态。fire-and-forget，不阻塞请求。
func (s *Service) reportPoolResult(poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) {
	poolID = strings.TrimSpace(poolID)
	proxy = strings.TrimSpace(proxy)
	if poolID == "" || proxy == "" || s.externalPool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.externalPool.ReportResult(ctx, poolID, proxy, ok, ratelimit, retryAfter)
}
