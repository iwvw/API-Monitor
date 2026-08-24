package onepanel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const requestTimeout = 30 * time.Second
const panelDefaultBaseURL = "https://127.0.0.1:8888"

type AgentRunner interface {
	RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error)
}

type Service struct {
	cfg    config.Config
	store  *database.Store
	schema database.SchemaEnsurer
	runner AgentRunner
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:    cfg,
		store:  database.New(cfg),
		schema: database.SchemaEnsurer{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		db.Close()
	}
	return service
}

func (s *Service) SetAgentRunner(runner AgentRunner) {
	s.runner = runner
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS onepanel_connections (
			server_id TEXT PRIMARY KEY,
			api_key TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT 'https://127.0.0.1:8888',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure onepanel schema: %w", err)
		}
	}
	return nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/onepanel")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	// === 配置管理（全局，不按服务器）===
	case len(parts) == 1 && parts[0] == "config" && r.Method == http.MethodGet:
		s.listConfigs(w, r)
		return
	case len(parts) == 1 && parts[0] == "config" && r.Method == http.MethodPost:
		s.createConfig(w, r)
		return
	case len(parts) == 2 && parts[0] == "config" && r.Method == http.MethodPut:
		s.updateConfig(w, r, parts[1])
		return
	case len(parts) == 2 && parts[0] == "config" && r.Method == http.MethodDelete:
		s.deleteConfig(w, r, parts[1])
		return
	}

	if len(parts) == 1 && parts[0] == "spec" && r.Method == http.MethodGet {
		s.serveCatalog(w, r)
		return
	}

	if len(parts) < 2 || parts[0] == "config" {
		response.Error(w, http.StatusNotFound, "onepanel route not implemented")
		return
	}
	serverID := parts[0]

	switch {
	// === 面板与总览 ===
	case len(parts) == 2 && parts[1] == "health" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/dashboard/base/no/no")
	case len(parts) == 2 && parts[1] == "overview" && r.Method == http.MethodGet:
		s.overview(w, r, serverID)
	case len(parts) == 3 && parts[1] == "dashboard" && parts[2] == "current" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/dashboard/current/no/no")

	// === 升级 ===
	case len(parts) == 3 && parts[1] == "upgrade" && parts[2] == "check" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/core/settings/upgrade")
	case len(parts) == 2 && parts[1] == "upgrade" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/core/settings/upgrade")

	// === 网站管理 ===
	case len(parts) == 2 && parts[1] == "websites" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/websites/list")
	case len(parts) == 2 && parts[1] == "websites" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites")
	case len(parts) == 3 && parts[1] == "websites" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/websites/"+parts[2])
	case len(parts) == 4 && parts[1] == "websites" && parts[3] == "operate" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/operate")
	case len(parts) == 4 && parts[1] == "websites" && parts[3] == "proxy" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/proxies/update")
	case len(parts) == 4 && parts[1] == "websites" && parts[3] == "https" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/"+parts[2]+"/https")
	case len(parts) == 4 && parts[1] == "websites" && parts[3] == "nginx" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/nginx/update")
	case len(parts) == 3 && parts[1] == "websites" && r.Method == http.MethodDelete:
		s.proxyPost(w, r, serverID, "/websites/del")

	// === 应用管理 ===
	case len(parts) == 2 && parts[1] == "apps" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/apps/search", `{"page":1,"pageSize":50}`)
	case len(parts) == 3 && parts[1] == "apps" && parts[2] == "install" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/apps/install")
	case len(parts) == 3 && parts[1] == "apps" && parts[2] == "installed" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/apps/installed/list")
	case len(parts) == 5 && parts[1] == "apps" && parts[2] == "installed" && parts[4] == "op" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/apps/installed/op")

	// === 容器管理 ===
	case len(parts) == 2 && parts[1] == "containers" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/containers/search", `{"page":1,"pageSize":50,"state":"all","orderBy":"name","order":"ascending"}`)
	case len(parts) == 3 && parts[1] == "containers" && parts[2] == "operate" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/containers/operate")
	case len(parts) == 4 && parts[1] == "containers" && parts[3] == "logs" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/containers/search/log?name="+parts[2])
	case len(parts) == 3 && parts[1] == "containers" && parts[2] == "compose" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/containers/compose")

	// === SSL ===
	case len(parts) == 2 && parts[1] == "ssl" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/websites/ssl/search", `{"page":1,"pageSize":50}`)
	case len(parts) == 3 && parts[1] == "ssl" && parts[2] == "obtain" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/ssl/obtain")
	case len(parts) == 2 && parts[1] == "acme" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/websites/acme")

	// === OpenResty ===
	case len(parts) == 3 && parts[1] == "openresty" && parts[2] == "status" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/openresty/status")
	case len(parts) == 3 && parts[1] == "openresty" && parts[2] == "reload" && r.Method == http.MethodPost:
		s.reloadOpenResty(w, r, serverID)

	// === 备份 ===
	case len(parts) == 2 && parts[1] == "backup" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/backups/backup")
	case len(parts) == 3 && parts[1] == "backups" && parts[2] == "records" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/backups/record/search", `{"page":1,"pageSize":50}`)
	case len(parts) == 3 && parts[1] == "backups" && parts[2] == "options" && r.Method == http.MethodGet:
		s.proxyGet(w, r, serverID, "/backups/options")

	// === 数据库 ===
	case len(parts) == 2 && parts[1] == "databases" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/databases/db/search", `{"page":1,"pageSize":50,"type":"mysql"}`)
	case len(parts) == 2 && parts[1] == "databases" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/databases/db")
	case len(parts) == 4 && parts[1] == "databases" && parts[3] == "password" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/databases/change/password")
	case len(parts) == 3 && parts[1] == "databases" && r.Method == http.MethodDelete:
		s.proxyPost(w, r, serverID, "/databases/db/del")

		// === 运行环境 ===
	case len(parts) == 2 && parts[1] == "runtimes" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/runtimes/search", `{"page":1,"pageSize":50}`)
	case len(parts) == 2 && parts[1] == "runtimes" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/runtimes")

		// === 定时任务 ===
	case len(parts) == 2 && parts[1] == "cronjobs" && r.Method == http.MethodGet:
		s.proxyList(w, r, serverID, "/cronjobs/search", `{"page":1,"pageSize":50}`)
	case len(parts) == 2 && parts[1] == "cronjobs" && r.Method == http.MethodPost:
		s.proxyPost(w, r, serverID, "/cronjobs")

	// === 通用代理 ===
	case len(parts) == 3 && parts[1] == "proxy" && parts[2] == "catalog" && r.Method == http.MethodGet:
		s.serveCatalog(w, r)
	case len(parts) == 2 && parts[1] == "proxy" && r.Method == http.MethodPost:
		s.proxyGeneric(w, r, serverID)

	default:
		response.Error(w, http.StatusNotFound, "onepanel route not implemented")
	}
}

// ========== 配置管理 ==========

func (s *Service) listConfigs(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(r.Context(),
		`SELECT server_id, api_key, base_url, created_at, updated_at FROM onepanel_connections ORDER BY created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type conn struct {
		ServerID  string `json:"serverId"`
		HasKey    bool   `json:"hasKey"`
		BaseURL   string `json:"baseUrl"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	var list []conn
	for rows.Next() {
		var c conn
		var apiKey, createdAt, updatedAt string
		if err := rows.Scan(&c.ServerID, &apiKey, &c.BaseURL, &createdAt, &updatedAt); err != nil {
			continue
		}
		c.HasKey = secure.IsEncrypted(apiKey)
		c.CreatedAt = createdAt
		c.UpdatedAt = updatedAt
		list = append(list, c)
	}
	response.OK(w, list)
}

func (s *Service) createConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID string `json:"serverId"`
		APIKey   string `json:"apiKey"`
		BaseURL  string `json:"baseUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" || req.APIKey == "" {
		response.Error(w, http.StatusBadRequest, "serverId and apiKey required")
		return
	}
	if req.BaseURL == "" {
		req.BaseURL = panelDefaultBaseURL
	}

	encrypted, err := secure.SecureEncrypt(req.APIKey)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "encrypt failed")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	_, err = db.ExecContext(r.Context(),
		`INSERT INTO onepanel_connections (server_id, api_key, base_url, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(server_id) DO UPDATE SET api_key=excluded.api_key, base_url=excluded.base_url, updated_at=datetime('now')`,
		req.ServerID, encrypted, req.BaseURL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]string{"serverId": req.ServerID})
}

func (s *Service) updateConfig(w http.ResponseWriter, r *http.Request, serverID string) {
	var req struct {
		APIKey  string `json:"apiKey"`
		BaseURL string `json:"baseUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var currentKey, currentBase string
	err = db.QueryRowContext(r.Context(),
		`SELECT api_key, base_url FROM onepanel_connections WHERE server_id=?`, serverID).
		Scan(&currentKey, &currentBase)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "onepanel config not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 空字段视为「保持不变」：部分更新（只改 apiKey 或只改 baseUrl）不应清空另一项。
	nextKey := currentKey
	if req.APIKey != "" {
		encrypted, err := secure.SecureEncrypt(req.APIKey)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "encrypt failed")
			return
		}
		nextKey = encrypted
	}
	nextBase := currentBase
	if req.BaseURL != "" {
		nextBase = req.BaseURL
	}

	_, err = db.ExecContext(r.Context(),
		`UPDATE onepanel_connections SET api_key=?, base_url=?, updated_at=datetime('now') WHERE server_id=?`,
		nextKey, nextBase, serverID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]string{"serverId": serverID})
}

func (s *Service) deleteConfig(w http.ResponseWriter, r *http.Request, serverID string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	_, err = db.ExecContext(r.Context(), `DELETE FROM onepanel_connections WHERE server_id=?`, serverID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]string{"serverId": serverID})
}

// ========== 概览聚合 ==========

func (s *Service) overview(w http.ResponseWriter, r *http.Request, serverID string) {
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	base := s.execPanelCmd(r.Context(), serverID, cfg, "GET", "/dashboard/base/no/no", "")
	if base.err != nil {
		response.Error(w, http.StatusBadGateway, "panel unreachable")
		return
	}

	var dashboardData struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal([]byte(base.output), &dashboardData)

	result := map[string]interface{}{
		"dashboard": dashboardData.Data,
	}

	ws := s.execPanelCmd(r.Context(), serverID, cfg, "GET", "/websites/list", "")
	if ws.err == nil {
		var wsData struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal([]byte(ws.output), &wsData)
		result["websites"] = wsData.Data
	}

	os := s.execPanelCmd(r.Context(), serverID, cfg, "GET", "/openresty/status", "")
	if os.err == nil {
		var osData struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal([]byte(os.output), &osData)
		result["openresty"] = osData.Data
	}

	cs := s.execPanelCmd(r.Context(), serverID, cfg, "POST", "/containers/search", `{"page":1,"pageSize":50,"state":"all","orderBy":"name","order":"ascending"}`)
	if cs.err == nil {
		var csData struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal([]byte(cs.output), &csData)
		result["containers"] = csData.Data
	}

	response.OK(w, result)
}

// ========== OpenResty 重载 ==========

func (s *Service) reloadOpenResty(w http.ResponseWriter, r *http.Request, serverID string) {
	if s.runner == nil {
		response.Error(w, http.StatusServiceUnavailable, "agent runner not available")
		return
	}
	// 服务器不存在时直接 404，避免 agent 任务阶段以 502 报错
	if _, err := s.getConfig(r.Context(), serverID); err != nil {
		if errors.Is(err, errPanelConfigNotFound) {
			response.Error(w, http.StatusNotFound, "server not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 容错：1Panel 默认的 OpenResty 容器名为 `openresty`。若节点重命名了容器，
	// 会报错，需改用通用 proxy 或直接调整脚本。见 docs/onepanel接口文档.md。
	out, err := s.runner.RunCommandTaskAndWait(serverID, "docker exec openresty nginx -t && docker exec openresty nginx -s reload", 15*time.Second)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]string{"output": out})
}

// ========== 通用代理 GET ==========

func (s *Service) proxyGet(w http.ResponseWriter, r *http.Request, serverID, panelPath string) {
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	result := s.execPanelCmd(r.Context(), serverID, cfg, "GET", panelPath, "")
	if result.err != nil {
		response.Error(w, http.StatusBadGateway, result.err.Error())
		return
	}
	var payload interface{}
	_ = json.Unmarshal([]byte(result.output), &payload)
	response.JSON(w, http.StatusOK, payload)
}

// ========== 通用代理 POST ==========

func (s *Service) proxyPost(w http.ResponseWriter, r *http.Request, serverID, panelPath string) {
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	bodyBytes := readBody(r)
	result := s.execPanelCmd(r.Context(), serverID, cfg, "POST", panelPath, string(bodyBytes))
	if result.err != nil {
		response.Error(w, http.StatusBadGateway, result.err.Error())
		return
	}
	var payload interface{}
	_ = json.Unmarshal([]byte(result.output), &payload)
	response.JSON(w, http.StatusOK, payload)
}

// ========== 列表查询（POST search，带默认分页体） ==========

// proxyList 向后端 search 类接口发送 POST。若前端带了请求体则原样透传，
// 否则使用 defaultBody 作为搜索条件，保证 1Panel 必填字段不缺省。
func (s *Service) proxyList(w http.ResponseWriter, r *http.Request, serverID, panelPath, defaultBody string) {
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}
	body := defaultBody
	if reqBody := readBody(r); len(reqBody) > 0 {
		body = string(reqBody)
	}
	result := s.execPanelCmd(r.Context(), serverID, cfg, "POST", panelPath, body)
	if result.err != nil {
		response.Error(w, http.StatusBadGateway, result.err.Error())
		return
	}
	var payload interface{}
	_ = json.Unmarshal([]byte(result.output), &payload)
	response.JSON(w, http.StatusOK, payload)
}

// ========== 通用代理（任意 Method/Path） ==========

func (s *Service) proxyGeneric(w http.ResponseWriter, r *http.Request, serverID string) {
	var req struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Method == "" || req.Path == "" {
		response.Error(w, http.StatusBadRequest, "method and path required")
		return
	}

	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	bodyStr := ""
	if len(req.Body) > 0 {
		bodyStr = string(req.Body)
	}
	result := s.execPanelCmd(r.Context(), serverID, cfg, req.Method, req.Path, bodyStr)
	if result.err != nil {
		response.Error(w, http.StatusBadGateway, result.err.Error())
		return
	}
	var payload interface{}
	_ = json.Unmarshal([]byte(result.output), &payload)
	response.JSON(w, http.StatusOK, payload)
}

// ========== 内部 ==========

type panelConfig struct {
	APIKey  string
	BaseURL string
}

// errPanelConfigNotFound 是面板连接配置不存在的哨兵错误（handler 返回 404）。
var errPanelConfigNotFound = errors.New("onepanel config not found")

func (s *Service) getConfig(ctx context.Context, serverID string) (*panelConfig, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var apiKey, baseURL string
	err = db.QueryRowContext(ctx,
		`SELECT api_key, base_url FROM onepanel_connections WHERE server_id=?`, serverID).
		Scan(&apiKey, &baseURL)
	if err != nil {
		return nil, errPanelConfigNotFound
	}
	return &panelConfig{
		APIKey:  secure.SecureDecrypt(apiKey),
		BaseURL: baseURL,
	}, nil
}

type cmdResult struct {
	output string
	err    error
}

func (s *Service) execPanelCmd(ctx context.Context, serverID string, cfg *panelConfig, method, panelPath, body string) cmdResult {
	if s.runner == nil {
		return cmdResult{err: fmt.Errorf("agent runner not available")}
	}

	if !validMethod(method) {
		return cmdResult{err: fmt.Errorf("invalid panel method %q", method)}
	}
	if !validPanelPath(panelPath) {
		return cmdResult{err: fmt.Errorf("invalid panel path %q", panelPath)}
	}

	cmd := buildCurlCommand(cfg.BaseURL, cfg.APIKey, method, panelPath, body)
	out, err := s.runner.RunCommandTaskAndWait(serverID, cmd, requestTimeout)
	if err != nil {
		return cmdResult{err: err}
	}
	return cmdResult{output: out}
}

// validMethod 限制可代理的 HTTP 方法，避免任意字符串被拼入 curl -X。
func validMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// validPanelPath 限制 1Panel API 路径字符集，防止路径段（如容器名）逃逸出
// curl 命令的双引号 URL 引发 shell 注入。
var panelPathPattern = regexp.MustCompile(`^[A-Za-z0-9/_?&=:.%\-]+$`)

func validPanelPath(panelPath string) bool {
	if !panelPathPattern.MatchString(panelPath) {
		return false
	}
	// 阻止路径穿越：/.. 序列（如 /websites/../xxx）不合法。
	return !strings.Contains(panelPath, "..")
}

func buildCurlCommand(baseURL, apiKey, method, panelPath, body string) string {
	// 单行命令：agent 0.5.1 对多行脚本（含换行的 dashboard:task payload）解析会
	// 立即失败且不返回错误信息。这里用分号拼接成单行，规避代理协议限制。
	cmd := fmt.Sprintf(`KEY='%s'; TS=$(date +%%s); TOKEN=$(echo -n "1panel${KEY}${TS}" | md5sum | grep -oE '^[a-f0-9]+'); curl -sk -X %s "%s/api/v2%s" -H "1Panel-Timestamp: $TS" -H "1Panel-Token: $TOKEN" -H 'Content-Type: application/json'`,
		apiKey, method, baseURL, panelPath)

	if body != "" {
		escaped := strings.ReplaceAll(body, "'", "'\\''")
		cmd += fmt.Sprintf(" -d '%s'", escaped)
	}
	return cmd
}

func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil
	}
	return data
}
