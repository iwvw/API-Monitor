package onepanel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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

// ServeHTTP 按生成的 quickRoutes 表分发：路由形状、可用方法、上游映射全部
// 来自 tools/onepanel/quick-routes.json，避免代码与文档各写一份再漂移。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/api/onepanel"))

	route, params, ok := matchQuickRoute(parts)
	if !ok {
		response.Error(w, http.StatusNotFound, "onepanel route not implemented")
		return
	}

	allowed := false
	for _, m := range route.Methods {
		if m == r.Method {
			allowed = true
			break
		}
	}
	if !allowed {
		w.Header().Set("Allow", strings.Join(route.Methods, ", "))
		response.Error(w, http.StatusMethodNotAllowed, "onepanel route does not support "+r.Method)
		return
	}

	switch route.Kind {
	case "local":
		s.serveQuickLocal(w, r, route, params)
	case "aggregate":
		s.overview(w, r, params["serverId"])
	case "proxy":
		s.serveQuickProxy(w, r, route, params)
	default:
		response.Error(w, http.StatusInternalServerError, "unknown quick route kind: "+route.Kind)
	}
}

// matchQuickRoute 按段匹配 prefix（{xxx} 为通配段），返回路由与路径参数。
// 传入的 parts 是请求路径去掉 /api/onepanel 之后的段，因此这里也比对去掉前缀的 pattern。
// 多条命中时取「静态段最多」的那条，避免 {id} 抢走固定子路径。
func matchQuickRoute(parts []string) (quickRoute, map[string]string, bool) {
	var best quickRoute
	var bestParams map[string]string
	bestStatic := -1
	for _, route := range quickRoutes {
		pattern := quickRouteTail(route.Prefix)
		if len(pattern) != len(parts) {
			continue
		}
		params := map[string]string{}
		static := 0
		matched := true
		for i, seg := range pattern {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				name := strings.Trim(seg, "{}")
				if parts[i] == "" {
					matched = false
					break
				}
				params[name] = parts[i]
				continue
			}
			if seg != parts[i] {
				matched = false
				break
			}
			static++
		}
		if !matched {
			continue
		}
		if static > bestStatic {
			best, bestParams, bestStatic = route, params, static
		}
	}
	return best, bestParams, bestStatic >= 0
}

// serveQuickLocal 处理不经过 1Panel 的本地接口（配置管理、内置目录、通用代理等）。
func (s *Service) serveQuickLocal(w http.ResponseWriter, r *http.Request, route quickRoute, params map[string]string) {
	switch route.Prefix {
	case "/api/onepanel/config":
		if r.Method == http.MethodGet {
			s.listConfigs(w, r)
			return
		}
		s.createConfig(w, r)
	case "/api/onepanel/config/{serverId}":
		if r.Method == http.MethodPut {
			s.updateConfig(w, r, params["serverId"])
			return
		}
		s.deleteConfig(w, r, params["serverId"])
	case "/api/onepanel/spec", "/api/onepanel/{serverId}/proxy/catalog":
		s.serveCatalog(w, r)
	case "/api/onepanel/{serverId}/openresty/reload":
		s.reloadOpenResty(w, r, params["serverId"])
	case "/api/onepanel/{serverId}/proxy":
		s.proxyGeneric(w, r, params["serverId"])
	default:
		response.Error(w, http.StatusNotFound, "onepanel local route not implemented: "+route.Prefix)
	}
}

// serveQuickProxy 按 upstream 映射构造 1Panel 请求并转发。
func (s *Service) serveQuickProxy(w http.ResponseWriter, r *http.Request, route quickRoute, params map[string]string) {
	// 路径参数最终会拼进上游 URL 与请求体，先做字符集校验（纵深防御）：
	// 容器名/站点 ID 这类值不会包含引号、分号、换行。
	for name, value := range params {
		if name == "serverId" {
			continue
		}
		if !quickParamPattern.MatchString(value) {
			response.Error(w, http.StatusBadGateway, "invalid path parameter "+name)
			return
		}
	}

	var up *quickUpstream
	for i := range route.Upstreams {
		if route.Upstreams[i].Method == r.Method {
			up = &route.Upstreams[i]
			break
		}
	}
	if up == nil {
		response.Error(w, http.StatusNotFound, "onepanel upstream not declared for "+r.Method)
		return
	}

	serverID := params["serverId"]
	cfg, err := s.getConfig(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusNotFound, err.Error())
		return
	}

	// 上游路径里的 {param} 用对外路径参数替换（serverId 不进上游路径）。
	upstreamPath := up.Upstream
	for name, value := range params {
		if name == "serverId" {
			continue
		}
		upstreamPath = strings.ReplaceAll(upstreamPath, "{"+name+"}", url.PathEscape(value))
	}

	body := buildQuickBody(r, up, params, route.Query)
	upstreamPath = applyQuickQuery(r, upstreamPath, up, params, body, route.Query)

	method := r.Method
	if up.As != "" {
		method = up.As
	}

	result := s.execPanelCmd(r.Context(), serverID, cfg, method, upstreamPath, body)
	if result.err != nil {
		response.Error(w, http.StatusBadGateway, result.err.Error())
		return
	}
	var payload interface{}
	_ = json.Unmarshal([]byte(result.output), &payload)
	response.JSON(w, http.StatusOK, payload)
}

// buildQuickBody 组装发往 1Panel 的 JSON 请求体：
// 以 Defaults 为基线 → 合并调用方请求体 → 注入路径参数 → 用查询参数覆盖同名基线字段。
func buildQuickBody(r *http.Request, up *quickUpstream, params map[string]string, queryKeys []string) string {
	payload := map[string]interface{}{}
	if up.Defaults != "" {
		_ = json.Unmarshal([]byte(up.Defaults), &payload)
	}

	// 调用方显式传的请求体优先于默认值。
	if raw := readBody(r); len(raw) > 0 {
		var incoming map[string]interface{}
		if err := json.Unmarshal(raw, &incoming); err == nil {
			for k, v := range incoming {
				payload[k] = v
			}
		}
	}

	// 路径参数注入：一对多时用 `outward=upstream` 语法声明上游字段名。
	if up.BodyFrom != "" {
		for _, token := range strings.Split(up.BodyFrom, ",") {
			token = strings.TrimSpace(strings.TrimPrefix(token, "path:"))
			if token == "" {
				continue
			}
			from, to := token, token
			if eq := strings.Index(token, "="); eq >= 0 {
				from, to = token[:eq], token[eq+1:]
			}
			if v, ok := params[from]; ok {
				payload[to] = coerceScalar(v)
			}
		}
	}

	// 查询参数若在基线体里已存在（如 databases 的 type），用它覆盖请求体字段。
	for _, key := range queryKeys {
		if _, inBody := payload[key]; !inBody {
			continue
		}
		if v := r.URL.Query().Get(key); v != "" {
			payload[key] = coerceScalar(v)
		}
	}

	if len(payload) == 0 {
		return ""
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// applyQuickQuery 处理上游的查询串：显式映射（如 container=path:name）与
// 调用方透传的查询参数（如 tail / since）。已在请求体里用掉的参数不再重复拼。
func applyQuickQuery(r *http.Request, upstreamPath string, up *quickUpstream, params map[string]string, body string, queryKeys []string) string {
	pairs := []string{}
	add := func(k, v string) {
		if k == "" || v == "" {
			return
		}
		pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}

	for k, spec := range up.Query {
		if strings.HasPrefix(spec, "path:") {
			add(k, params[strings.TrimPrefix(spec, "path:")])
			continue
		}
		add(k, spec)
	}

	bodyFields := map[string]bool{}
	if body != "" {
		var m map[string]interface{}
		if json.Unmarshal([]byte(body), &m) == nil {
			for k := range m {
				bodyFields[k] = true
			}
		}
	}
	for _, key := range queryKeys {
		if bodyFields[key] {
			continue
		}
		if _, mapped := up.Query[key]; mapped {
			continue
		}
		add(key, r.URL.Query().Get(key))
	}

	if len(pairs) == 0 {
		return upstreamPath
	}
	sep := "?"
	if strings.Contains(upstreamPath, "?") {
		sep = "&"
	}
	return upstreamPath + sep + strings.Join(pairs, "&")
}

// coerceScalar 把纯数字字符串转成 JSON number：1Panel 的 ID 字段是 integer，
// 从路径段拿到的永远是字符串，直接塞进 body 会触发类型校验失败。
func coerceScalar(v string) interface{} {
	if v == "" {
		return v
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return json.Number(v)
	}
	return v
}

// quickParamPattern 限制路径参数的字符集：域名/容器名/ID 都不需要引号、
// 分号、空格和控制字符。上游路径与请求体最终都会进入 shell 命令，这层
// 校验与 execPanelCmd 里的 validPanelPath 共同构成注入防线。
var quickParamPattern = regexp.MustCompile(`^[A-Za-z0-9._:@\-]{1,128}$`)

// splitPath 把 URL 路径切成非空段（保留 {param} 的原样比较）。
func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		out = append(out, seg)
	}
	return out
}

// quickRouteTail 返回前缀去掉 /api/onepanel 之后的段。
func quickRouteTail(prefix string) []string {
	segs := splitPath(prefix)
	const head = "/api/onepanel"
	h := splitPath(head)
	if len(segs) < len(h) {
		return nil
	}
	for i := range h {
		if segs[i] != h[i] {
			return nil
		}
	}
	return segs[len(h):]
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
	if !validPanelBaseURL(req.BaseURL) {
		response.Error(w, http.StatusBadRequest, "invalid baseUrl")
		return
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
		if !validPanelBaseURL(req.BaseURL) {
			response.Error(w, http.StatusBadRequest, "invalid baseUrl")
			return
		}
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

	// 面板基础信息是连通性探针：失败即整体 502，其余区块按需降级。
	base := s.execPanelCmd(r.Context(), serverID, cfg, "GET", "/dashboard/base/no/no", "")
	if base.err != nil {
		response.Error(w, http.StatusBadGateway, "panel unreachable")
		return
	}

	result := map[string]interface{}{
		"dashboard": panelData(base.output),
	}

	// 其余区块复用快捷路由的上游定义，避免这里再手写一份 1Panel 路径。
	blocks := []struct {
		key    string
		prefix string
		method string
	}{
		{"websites", "/api/onepanel/{serverId}/websites", "GET"},
		{"openresty", "/api/onepanel/{serverId}/openresty/status", "GET"},
		{"containers", "/api/onepanel/{serverId}/containers", "GET"},
	}
	for _, block := range blocks {
		route, ok := quickRouteByPrefix(block.prefix)
		if !ok {
			continue
		}
		var up *quickUpstream
		for i := range route.Upstreams {
			if route.Upstreams[i].Method == block.method {
				up = &route.Upstreams[i]
				break
			}
		}
		if up == nil {
			continue
		}
		method := block.method
		if up.As != "" {
			method = up.As
		}
		res := s.execPanelCmd(r.Context(), serverID, cfg, method, up.Upstream, up.Defaults)
		if res.err != nil {
			continue
		}
		if data := panelData(res.output); data != nil {
			result[block.key] = data
		}
	}

	response.OK(w, result)
}

// panelData 剥掉 1Panel 的 {code,message,data} 外壳，只留 data。
func panelData(output string) interface{} {
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		return nil
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	var data interface{}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil
	}
	return data
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
	// 会报错，需改用通用 proxy 或直接调整脚本。见 docs/architecture/onepanel接口文档.md。
	out, err := s.runner.RunCommandTaskAndWait(serverID, "docker exec openresty nginx -t && docker exec openresty nginx -s reload", 15*time.Second)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]string{"output": out})
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errPanelConfigNotFound
		}
		return nil, err
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
	if !validPanelBaseURL(cfg.BaseURL) {
		return cmdResult{err: fmt.Errorf("invalid panel base url")}
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

// validPanelBaseURL 校验 1Panel BaseURL:必须是 http/https 且不含 shell 元字符。
func validPanelBaseURL(baseURL string) bool {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return !strings.ContainsAny(trimmed, "'\"`$\\ \n\r\t")
}

func buildCurlCommand(baseURL, apiKey, method, panelPath, body string) string {
	// 单行命令：agent 0.5.1 对多行脚本（含换行的 dashboard:task payload）解析会
	// 立即失败且不返回错误信息。这里用分号拼接成单行，规避代理协议限制。
	safeKey := strings.ReplaceAll(apiKey, "'", "'\\''")
	safeBase := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(baseURL)
	cmd := fmt.Sprintf(`KEY='%s'; TS=$(date +%%s); TOKEN=$(echo -n "1panel${KEY}${TS}" | md5sum | grep -oE '^[a-f0-9]+'); curl -sk -X %s "%s/api/v2%s" -H "1Panel-Timestamp: $TS" -H "1Panel-Token: $TOKEN" -H 'Content-Type: application/json'`,
		safeKey, method, safeBase, panelPath)

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
	data, err := io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
	if err != nil {
		return nil
	}
	return data
}
