package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// escapeLike 转义 LIKE 模式中的通配符（配合 ESCAPE '\' 使用），
// 让用户输入的 % _ \ 按字面匹配而非被当作模式字符。
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

type aiMCPServer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Transport   string `json:"transport"`
	Command     string `json:"command"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	EnvJSON     string `json:"envJson"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type aiSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Entrypoint  string   `json:"entrypoint"`
	Version     string   `json:"version"`
	Enabled     bool     `json:"enabled"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

type aiAuditEntry struct {
	ID        int64  `json:"id"`
	AgentName string `json:"agentName"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Details   string `json:"details"`
	IPAddress string `json:"ipAddress"`
	UserAgent string `json:"userAgent"`
	CreatedAt string `json:"createdAt"`
}

type AICallRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

type AICallResponse struct {
	StatusCode int                 `json:"statusCode"`
	Headers    map[string][]string `json:"headers"`
	Body       interface{}         `json:"body,omitempty"`
	Raw        string              `json:"raw,omitempty"`
}

type AICaller func(context.Context, AICallRequest) (AICallResponse, error)

type aiMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Service) ensureAIAccessSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ai_mcp_servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			transport TEXT NOT NULL DEFAULT 'stdio',
			command TEXT,
			url TEXT,
			description TEXT,
			enabled INTEGER DEFAULT 1,
			env_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			entrypoint TEXT,
			version TEXT,
			enabled INTEGER DEFAULT 1,
			permissions_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_access_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_name TEXT,
			action TEXT NOT NULL,
			target TEXT,
			status TEXT NOT NULL,
			latency_ms INTEGER DEFAULT 0,
			details TEXT,
			ip_address TEXT,
			user_agent TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_access_audit_created ON ai_access_audit(created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure ai access schema: %w", err)
		}
	}
	return nil
}

func (s *Service) aiAccessOverview(r *http.Request) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	key, createdAt, err := s.getOrCreateAIAgentKey(r.Context(), db)
	if err != nil {
		return nil, err
	}
	mcpServers, err := s.listMCPServers(r.Context(), db)
	if err != nil {
		return nil, err
	}
	skills, err := s.listSkills(r.Context(), db)
	if err != nil {
		return nil, err
	}
	audit, err := s.listAIAudit(r.Context(), db, 20)
	if err != nil {
		return nil, err
	}

	baseURL := requestBaseURL(r)
	mcpURL := baseURL + "/api/ai/mcp"
	manifestURL := baseURL + "/api/ai/manifest"
	openAPIURL := baseURL + "/api/openapi.json"
	tools := s.aiTools()
	writeEnabled, err := s.getAIAgentWriteEnabled(r.Context(), db)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"agentKey": map[string]interface{}{
			"value":     key,
			"masked":    maskSecret(key),
			"createdAt": createdAt,
			"header":    "Authorization: Bearer " + key,
		},
		"endpoints": map[string]string{
			"manifest": manifestURL,
			"mcp":      mcpURL,
			"openapi":  openAPIURL,
		},
		"guide": s.aiAccessGuide(mcpURL, manifestURL, openAPIURL, key, tools),
		"tools": tools,
		"policy": map[string]interface{}{
			"allowedMethods": []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
			"blockedPaths":   []string{"/api/ai/*", "/api/system/ai-access/key/*", "/api/ai-access/key/*"},
			"blockedModes":   []string{string(manifest.ResponseStream), string(manifest.ResponseWebSocket)},
			"bodyLimitBytes": 1024 * 1024,
			"writeEnabled":   writeEnabled,
			"auth":           "Agent Key 作为系统级接入密钥使用；默认只读，写入需在设置中开启，所有调用都会写入审计记录。",
		},
		"mcpServers": mcpServers,
		"skills":     skills,
		"audit":      audit,
		"summary": map[string]int{
			"tools":      len(tools),
			"mcpServers": len(mcpServers),
			"skills":     len(skills),
			"audit":      len(audit),
		},
	}, nil
}

func (s *Service) rotateAIAgentKey(r *http.Request) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	key, err := randomToken("aik_", 32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(r.Context(), `INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES
		('ai_agent_key', ?, 'AI access bearer key', ?),
		('ai_agent_key_created_at', ?, 'AI access bearer key creation time', ?)`, key, now, now, now); err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "rotate_key", "ai_agent_key", "success", 0, "AI 接入密钥已轮换", s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

const aiAgentWriteEnabledKey = "ai_agent_write_enabled"

func (s *Service) getOrCreateAIAgentKey(ctx context.Context, db *sql.DB) (string, string, error) {
	var key string
	if err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'ai_agent_key'").Scan(&key); err == nil && key != "" {
		var createdAt string
		_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'ai_agent_key_created_at'").Scan(&createdAt)
		return key, createdAt, nil
	}
	key, err := randomToken("aik_", 32)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES
		('ai_agent_key', ?, 'AI access bearer key', ?),
		('ai_agent_key_created_at', ?, 'AI access bearer key creation time', ?)`, key, now, now, now)
	return key, now, err
}

func (s *Service) getAIAgentWriteEnabled(ctx context.Context, db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", aiAgentWriteEnabledKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1" || strings.EqualFold(value, "true"), nil
}

func (s *Service) AIAgentWriteAllowed(ctx context.Context) (bool, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	return s.getAIAgentWriteEnabled(ctx, db)
}

func (s *Service) setAIAgentWriteEnabled(r *http.Request) (map[string]interface{}, error) {
	var payload struct {
		WriteEnabled bool `json:"writeEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	value := "0"
	if payload.WriteEnabled {
		value = "1"
	}
	if _, err := db.ExecContext(r.Context(), `INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES (?, ?, ?, ?)`,
		aiAgentWriteEnabledKey, value, "AI agent write access toggle", now); err != nil {
		return nil, err
	}
	status := "disabled"
	detail := "AI 写入已关闭"
	if payload.WriteEnabled {
		status = "enabled"
		detail = "AI 写入已开启"
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "toggle_write", aiAgentWriteEnabledKey, status, 0, detail, s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

func (s *Service) validateAIAgent(r *http.Request, db *sql.DB) bool {
	if _, err := s.apiKeys.Authorize(r.Context(), r, apikeys.ScopeAIMCP); err == nil {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	expected, _, err := s.getOrCreateAIAgentKey(r.Context(), db)
	if err != nil {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")) == expected
}

func (s *Service) aiManifest(r *http.Request) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	start := time.Now()
	if !s.validateAIAgent(r, db) {
		_ = s.insertAIAudit(r.Context(), db, "external", "manifest", "/api/ai/manifest", "denied", time.Since(start).Milliseconds(), "invalid key", s.clientIP(r), r.UserAgent())
		return nil, errUnauthorizedAI
	}
	_ = s.insertAIAudit(r.Context(), db, "external", "manifest", "/api/ai/manifest", "success", time.Since(start).Milliseconds(), "manifest read", s.clientIP(r), r.UserAgent())
	return s.aiManifestPayload(r), nil
}

func (s *Service) aiManifestPayload(r *http.Request) map[string]interface{} {
	baseURL := requestBaseURL(r)
	return map[string]interface{}{
		"name":        "API Monitor",
		"version":     s.cfg.Version,
		"description": "API Monitor AI access surface",
		"endpoints": map[string]string{
			"mcp":     baseURL + "/api/ai/mcp",
			"openapi": baseURL + "/api/openapi.json",
			"docs":    baseURL + "/api/system/api-docs",
		},
		"tools":  s.aiTools(),
		"routes": s.apiDocs()["summary"],
	}
}

func (s *Service) handleMCP(r *http.Request) (interface{}, int, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	start := time.Now()
	if !s.validateAIAgent(r, db) {
		_ = s.insertAIAudit(r.Context(), db, "external", "mcp", "/api/ai/mcp", "denied", time.Since(start).Milliseconds(), "invalid key", s.clientIP(r), r.UserAgent())
		return nil, http.StatusUnauthorized, errUnauthorizedAI
	}
	if r.Method == http.MethodGet {
		_ = s.insertAIAudit(r.Context(), db, "external", "mcp.describe", "/api/ai/mcp", "success", time.Since(start).Milliseconds(), "mcp metadata read", s.clientIP(r), r.UserAgent())
		return map[string]interface{}{
			"server":    "api-monitor",
			"protocol":  "mcp-json-rpc",
			"tools":     s.aiTools(),
			"resources": s.mcpResources(),
		}, http.StatusOK, nil
	}
	var req aiMCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		_ = s.insertAIAudit(r.Context(), db, "external", req.Method, "/api/ai/mcp", "success", time.Since(start).Milliseconds(), truncate(string(req.Params), 500), s.clientIP(r), r.UserAgent())
		return nil, http.StatusAccepted, nil
	}
	result, callErr := s.dispatchMCPTool(r, req)
	status := "success"
	if callErr != nil {
		status = "error"
	}
	_ = s.insertAIAudit(r.Context(), db, "external", req.Method, "/api/ai/mcp", status, time.Since(start).Milliseconds(), truncate(string(req.Params), 500), s.clientIP(r), r.UserAgent())
	if callErr != nil {
		return mcpError(req.ID, -32000, callErr.Error()), http.StatusOK, nil
	}
	return map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result}, http.StatusOK, nil
}

func (s *Service) dispatchMCPTool(r *http.Request, req aiMCPRequest) (interface{}, error) {
	switch req.Method {
	case "initialize":
		return map[string]interface{}{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "api-monitor", "version": s.cfg.Version}, "capabilities": map[string]interface{}{"tools": map[string]bool{"listChanged": true}, "resources": map[string]bool{"listChanged": false}}}, nil
	case "ping":
		return map[string]interface{}{}, nil
	case "resources/list":
		return map[string]interface{}{"resources": s.mcpResources()}, nil
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		return s.mcpReadResource(r, params.URI)
	case "tools/list":
		return map[string]interface{}{"tools": s.aiTools()}, nil
	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		toolResult, err := s.callAITool(r, params.Name, params.Arguments)
		if err != nil {
			return nil, err
		}
		// MCP 规范要求 tools/call 结果包含 content 数组，否则多数客户端
		// （Claude、opencode 等）会把工具输出渲染为空。structuredContent
		// 同时保留结构化数据，供支持该字段的客户端使用。
		return mcpToolResult(toolResult), nil
	default:
		return nil, fmt.Errorf("unsupported MCP method: %s", req.Method)
	}
}

func mcpToolResult(result interface{}) map[string]interface{} {
	text := ""
	if encoded, err := json.Marshal(result); err == nil {
		text = string(encoded)
	} else {
		text = fmt.Sprint(result)
	}
	return map[string]interface{}{
		"content":           []map[string]interface{}{{"type": "text", "text": text}},
		"structuredContent": result,
	}
}

func (s *Service) callAITool(r *http.Request, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "list_apis":
		return s.aiRouteCatalog(args)
	case "get_route":
		return s.getRouteContract(args)
	case "get_openapi":
		return s.openapiDocument(r), nil
	case "get_ai_manifest":
		return s.aiManifestPayload(r), nil
	case "get_system_status":
		metrics, err := s.hostMetrics()
		if err != nil {
			return nil, err
		}
		return metrics, nil
	case "call_api":
		return s.callAPIFromAI(r.Context(), args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// aiRouteCatalog 返回紧凑的接口目录，支持 group / module / search 过滤，用于渐进式披露。
func (s *Service) aiRouteCatalog(args map[string]interface{}) (interface{}, error) {
	group, _ := args["group"].(string)
	module, _ := args["module"].(string)
	search, _ := args["search"].(string)
	group = strings.TrimSpace(group)
	module = strings.TrimSpace(module)
	search = strings.ToLower(strings.TrimSpace(search))

	items := s.apiDocs()["routes"].([]apiDocRoute)
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if group != "" && item.Group != group {
			continue
		}
		if module != "" && item.Module != module {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(item.Prefix + " " + item.Detail + " " + item.Description)
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		hasBody := item.RequestSchema != nil || item.RequestBody != nil
		if !hasBody {
			for _, method := range item.Methods {
				if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
					hasBody = true
					break
				}
			}
		}
		desc := item.Detail
		if desc == "" {
			desc = item.Description
		}
		result = append(result, map[string]interface{}{
			"path":    item.Prefix,
			"methods": item.Methods,
			"group":   item.Group,
			"module":  item.Module,
			"auth":    string(item.Auth),
			"desc":    desc,
			"hasBody": hasBody,
		})
	}
	return map[string]interface{}{"count": len(result), "routes": result}, nil
}

// getRouteContract 返回单个接口的完整契约；入参 path 可以是具体路径（会匹配到模式路由）。
func (s *Service) getRouteContract(args map[string]interface{}) (interface{}, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	items := s.apiDocs()["routes"].([]apiDocRoute)
	best := (*apiDocRoute)(nil)
	for i := range items {
		item := &items[i]
		if routePrefixMatches(item.Prefix, item.MatchMode, path) {
			if best == nil || len(item.Prefix) > len(best.Prefix) {
				best = item
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("API 路由不存在: %s", path)
	}
	desc := best.Detail
	if desc == "" {
		desc = best.Description
	}
	return map[string]interface{}{
		"path":               best.Prefix,
		"matchedPath":        path,
		"methods":            best.Methods,
		"group":              best.Group,
		"module":             best.Module,
		"auth":               string(best.Auth),
		"responseMode":       string(best.ResponseMode),
		"matchMode":          string(best.MatchMode),
		"status":             best.Status,
		"description":        desc,
		"pathParams":         best.PathParams,
		"queryParams":        best.QueryParams,
		"headers":            best.Headers,
		"requestContentType": best.RequestType,
		"requestSchema":      best.RequestSchema,
		"requestExample":     best.RequestBody,
		"responseExample":    best.ResponseBody,
		"notes":              best.Notes,
	}, nil
}

func routePrefixMatches(prefix string, mode manifest.MatchMode, path string) bool {
	switch mode {
	case manifest.MatchExact:
		return path == prefix
	case manifest.MatchPattern:
		return routePatternMatches(prefix, path)
	default:
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
}

func routePatternMatches(prefix, path string) bool {
	parts := strings.Split(prefix, "/")
	target := strings.Split(path, "/")
	if len(parts) != len(target) {
		return false
	}
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		if part != target[i] {
			return false
		}
	}
	return true
}

func (s *Service) callAPIFromAI(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	method, _ := args["method"].(string)
	if method == "" {
		method = http.MethodGet
	}
	headers := map[string]string{}
	if rawHeaders, ok := args["headers"].(map[string]interface{}); ok {
		for key, value := range rawHeaders {
			headers[key] = fmt.Sprint(value)
		}
	}
	var body json.RawMessage
	if rawBody, ok := args["body"]; ok && rawBody != nil {
		encoded, err := json.Marshal(rawBody)
		if err != nil {
			return nil, err
		}
		body = encoded
	}
	req := AICallRequest{
		Method:  strings.ToUpper(method),
		Path:    path,
		Headers: headers,
		Body:    body,
	}
	if s.aiCaller != nil {
		return s.aiCaller(ctx, req)
	}
	return s.readOnlyAICall(req)
}

func (s *Service) readOnlyAICall(req AICallRequest) (interface{}, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("AI caller is not configured; only GET fallback is available")
	}
	switch req.Path {
	case "/health":
		return map[string]interface{}{"status": "ok", "service": "api-monitor-go", "version": s.cfg.Version, "timestamp": time.Now().UTC().Format(time.RFC3339)}, nil
	case "/api/migration/status":
		routes := manifest.Routes()
		retiredModules := make([]string, 0)
		for _, route := range routes {
			if route.Owner == manifest.OwnerRetired {
				retiredModules = append(retiredModules, route.Module)
			}
		}
		return map[string]interface{}{"version": s.cfg.Version, "databasePath": s.cfg.DatabasePath(), "legacyEnabled": false, "routeSummary": manifest.Summary(), "routes": routes, "retired": retiredModules}, nil
	case "/api/system/api-docs":
		return s.apiDocs(), nil
	case "/api/system/openapi.json":
		httpReq, _ := http.NewRequest(http.MethodGet, req.Path, nil)
		return s.openapiDocument(httpReq), nil
	default:
		return nil, fmt.Errorf("AI caller is not configured for this path")
	}
}

func (s *Service) aiTools() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "list_apis", "description": "读取接口目录（紧凑版，支持 group/module/search 过滤），先扫目录再按需取详情", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"group": map[string]interface{}{"type": "string", "description": "按分组过滤（如 模型网关 / Cloudflare / 主机实例）"}, "module": map[string]interface{}{"type": "string", "description": "按模块过滤（如 flyio / cloudflare-dns）"}, "search": map[string]interface{}{"type": "string", "description": "按路径或描述关键词过滤"}}}},
		{"name": "get_route", "description": "读取单个接口的完整契约（参数、请求体 schema、示例、鉴权）；调用 call_api 前必须先用本工具确认请求体结构", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string", "description": "接口路径，如 /api/flyio/apps/{appName}/update-image 或具体路径"}}, "required": []string{"path"}}},
		{"name": "get_openapi", "description": "读取 OpenAPI 3.1 文档", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_ai_manifest", "description": "读取 AI 接入能力清单", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_system_status", "description": "读取本机系统运行状态", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "call_api", "description": "调用 API Monitor 内部接口，支持 GET/POST/PUT/PATCH/DELETE、请求头和 JSON 请求体；请求体结构先用 get_route 获取", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"method": map[string]interface{}{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}}, "path": map[string]interface{}{"type": "string", "description": "以 / 开头的系统接口路径"}, "headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]string{"type": "string"}}, "body": map[string]interface{}{"type": "object", "additionalProperties": true, "description": "JSON 请求体，字段以 get_route 返回的 requestSchema/requestExample 为准"}}, "required": []string{"path"}}},
	}
}

func (s *Service) aiAccessGuide(mcpURL, manifestURL, openAPIURL, key string, tools []map[string]interface{}) string {
	toolLines := make([]string, 0, len(tools))
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		desc, _ := tool["description"].(string)
		toolLines = append(toolLines, "- "+name+"："+desc)
	}
	toolsText := strings.Join(toolLines, "\n")
	return fmt.Sprintf(`# API Monitor AI 接入指南

你是 API Monitor 的 AI 访问面接入说明。阅读本指南即可完成自我配置与连接。

## 服务信息
- 服务名称：api-monitor
- 协议：MCP（Streamable HTTP / JSON-RPC 2.0）
- 版本：%s
- 接入密钥（Agent Key）：%s
- 鉴权头：Authorization: Bearer %s
- 权限：默认只读；写操作需管理员在「API 文档 → AI 接入」开启「允许写入」

## 端点
- 能力清单（manifest，GET）：%s
- MCP 服务：%s
- OpenAPI 文档：%s（需登录会话，也可用 MCP 工具 get_openapi 获取）

## 连接流程
1. 复制 Agent Key，客户端以 Bearer 鉴权接入。
2. 在 AI 客户端注册 MCP 接入地址（支持 MCP Streamable HTTP 的客户端，如 Claude、Cursor、VS Code、Cline、Roo）：
~~~json
{
  "mcpServers": {
    "api-monitor": {
      "url": "%s",
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}
~~~
客户端仅支持 stdio 时，可用 mcp-remote 桥接：npx mcp-remote %s --header "Authorization: Bearer %s"
3. 连接成功后即可调用系统接口；全部调用都会写入审计记录。

## 可用工具
%s

## 可用资源
- api-monitor://routes：完整接口清单
- api-monitor://openapi：OpenAPI 3.1 文档

## 接入步骤（渐进式披露）
1. 用 list_apis 扫目录（可按 group / module / search 过滤，先只看目标模块，省 token）。
2. 确定要调用的接口后，用 get_route <path> 获取该接口的完整契约（路径参数、请求体 schema、示例、鉴权）。
3. 按契约用 call_api 调用；请求体字段以 get_route 返回的 requestSchema / requestExample 为准，不要猜。
4. 默认只读；写操作（POST/PUT/PATCH/DELETE）会返回「写入未启用」提示，需管理员在「API 文档 → AI 接入」开启「允许写入」。
5. 密钥可随时在「API 文档 → AI 接入」页面轮换；请勿将密钥写入公开仓库。
`, s.cfg.Version, key, key, manifestURL, mcpURL, openAPIURL, mcpURL, key, mcpURL, key, toolsText)
}

func (s *Service) mcpResources() []map[string]interface{} {
	return []map[string]interface{}{
		{"uri": "api-monitor://routes", "name": "接口清单", "mimeType": "application/json"},
		{"uri": "api-monitor://openapi", "name": "OpenAPI", "mimeType": "application/json"},
	}
}

func (s *Service) mcpReadResource(r *http.Request, uri string) (interface{}, error) {
	var content string
	switch uri {
	case "api-monitor://routes":
		encoded, err := json.Marshal(s.apiDocs())
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	case "api-monitor://openapi":
		encoded, err := json.Marshal(s.openapiDocument(r))
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	default:
		return nil, fmt.Errorf("unknown resource: %s", uri)
	}
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{"uri": uri, "mimeType": "application/json", "text": content},
		},
	}, nil
}

func (s *Service) listMCPServers(ctx context.Context, db *sql.DB) ([]aiMCPServer, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, transport, COALESCE(command,''), COALESCE(url,''), COALESCE(description,''), enabled, COALESCE(env_json,''), created_at, updated_at FROM ai_mcp_servers ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []aiMCPServer{}
	for rows.Next() {
		var item aiMCPServer
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Transport, &item.Command, &item.URL, &item.Description, &enabled, &item.EnvJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) saveMCPServer(r *http.Request, id string) (map[string]interface{}, error) {
	var req aiMCPServer
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if id == "" {
		var err error
		id, err = randomToken("mcp_", 8)
		if err != nil {
			return nil, err
		}
	}
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(), `INSERT INTO ai_mcp_servers (id, name, transport, command, url, description, enabled, env_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, transport=excluded.transport, command=excluded.command, url=excluded.url, description=excluded.description, enabled=excluded.enabled, env_json=excluded.env_json, updated_at=excluded.updated_at`,
		id, req.Name, req.Transport, req.Command, req.URL, req.Description, boolInt(req.Enabled), req.EnvJSON, now, now)
	if err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "save_mcp_server", id, "success", 0, req.Name, s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

func (s *Service) deleteMCPServer(r *http.Request, id string) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	_, err = db.ExecContext(r.Context(), "DELETE FROM ai_mcp_servers WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "delete_mcp_server", id, "success", 0, id, s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

func (s *Service) listSkills(ctx context.Context, db *sql.DB) ([]aiSkill, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, COALESCE(description,''), COALESCE(entrypoint,''), COALESCE(version,''), enabled, COALESCE(permissions_json,'[]'), created_at, updated_at FROM ai_skills ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []aiSkill{}
	for rows.Next() {
		var item aiSkill
		var enabled int
		var permissionsRaw string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Entrypoint, &item.Version, &enabled, &permissionsRaw, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		_ = json.Unmarshal([]byte(permissionsRaw), &item.Permissions)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) saveSkill(r *http.Request, id string) (map[string]interface{}, error) {
	var req aiSkill
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}
	if id == "" {
		var err error
		id, err = randomToken("sk_", 8)
		if err != nil {
			return nil, err
		}
	}
	permissions, _ := json.Marshal(req.Permissions)
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(), `INSERT INTO ai_skills (id, name, description, entrypoint, version, enabled, permissions_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, entrypoint=excluded.entrypoint, version=excluded.version, enabled=excluded.enabled, permissions_json=excluded.permissions_json, updated_at=excluded.updated_at`,
		id, req.Name, req.Description, req.Entrypoint, req.Version, boolInt(req.Enabled), string(permissions), now, now)
	if err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "save_skill", id, "success", 0, req.Name, s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

func (s *Service) deleteSkill(r *http.Request, id string) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	_, err = db.ExecContext(r.Context(), "DELETE FROM ai_skills WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "delete_skill", id, "success", 0, id, s.clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

func (s *Service) listAIAudit(ctx context.Context, db *sql.DB, limit int) ([]aiAuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(agent_name,''), action, COALESCE(target,''), status, latency_ms, COALESCE(details,''), COALESCE(ip_address,''), COALESCE(user_agent,''), created_at FROM ai_access_audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []aiAuditEntry{}
	for rows.Next() {
		var item aiAuditEntry
		if err := rows.Scan(&item.ID, &item.AgentName, &item.Action, &item.Target, &item.Status, &item.LatencyMS, &item.Details, &item.IPAddress, &item.UserAgent, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// listAIAuditPage returns paginated AI access audit entries filtered by recent days.
// Supports days (default 7), page (default 1), pageSize (default 20, max 100),
// action (exact match) and search (LIKE across action, target, agent_name, details, ip_address, status).
func (s *Service) listAIAuditPage(r *http.Request) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}

	query := r.URL.Query()
	page, pageSize, days := 1, 20, 7
	if p, err := strconv.Atoi(query.Get("page")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(query.Get("pageSize")); err == nil && ps > 0 {
		pageSize = ps
		if pageSize > 100 {
			pageSize = 100
		}
	}
	if d, err := strconv.Atoi(query.Get("days")); err == nil && d > 0 {
		days = d
	}

	actionFilter := strings.TrimSpace(query.Get("action"))
	searchText := strings.TrimSpace(query.Get("search"))

	offset := (page - 1) * pageSize
	timeFilter := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)

	whereClause := "WHERE created_at >= ?"
	args := []interface{}{timeFilter}

	if actionFilter != "" {
		whereClause += " AND action = ?"
		args = append(args, actionFilter)
	}
	if searchText != "" {
		whereClause += " AND (action LIKE ? ESCAPE '\\' OR target LIKE ? ESCAPE '\\' OR agent_name LIKE ? ESCAPE '\\' OR details LIKE ? ESCAPE '\\' OR ip_address LIKE ? ESCAPE '\\' OR status LIKE ? ESCAPE '\\')"
		escaped := escapeLike(searchText)
		like := "%" + escaped + "%"
		args = append(args, like, like, like, like, like, like)
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM ai_access_audit " + whereClause
	if err := db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	dataQuery := "SELECT id, COALESCE(agent_name,''), action, COALESCE(target,''), status, COALESCE(latency_ms,0), COALESCE(details,''), COALESCE(ip_address,''), COALESCE(user_agent,''), created_at FROM ai_access_audit " + whereClause + " ORDER BY id DESC LIMIT ? OFFSET ?"
	dataArgs := append(args, pageSize, offset)
	rows, err := db.QueryContext(r.Context(), dataQuery, dataArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []aiAuditEntry{}
	for rows.Next() {
		var item aiAuditEntry
		if err := rows.Scan(&item.ID, &item.AgentName, &item.Action, &item.Target, &item.Status, &item.LatencyMS, &item.Details, &item.IPAddress, &item.UserAgent, &item.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":   total,
		"records": records,
	}, nil
}

func (s *Service) clearAIAudit(r *http.Request) (map[string]interface{}, error) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return nil, err
	}
	_, err = db.ExecContext(r.Context(), "DELETE FROM ai_access_audit")
	if err != nil {
		return nil, err
	}
	return s.aiAccessOverview(r)
}

func (s *Service) insertAIAudit(ctx context.Context, db *sql.DB, agentName, action, target, status string, latencyMS int64, details, ip, userAgent string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO ai_access_audit (agent_name, action, target, status, latency_ms, details, ip_address, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agentName, action, target, status, latencyMS, details, ip, userAgent, time.Now().UTC().Format(time.RFC3339))
	return err
}

func mcpError(id interface{}, code int, message string) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": message}}
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func randomToken(prefix string, byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}

func maskSecret(secret string) string {
	if len(secret) <= 12 {
		return "********"
	}
	return secret[:8] + strings.Repeat("*", 10) + secret[len(secret)-6:]
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Service) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	if isTrustedProxy(net.ParseIP(host), s.cfg.TrustedProxyCIDRs) {
		if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
			candidate := strings.TrimSpace(strings.Split(value, ",")[0])
			if ip := net.ParseIP(candidate); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}

func isTrustedProxy(ip net.IP, entries []string) bool {
	if ip == nil {
		return false
	}
	for _, entry := range entries {
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	var buf bytes.Buffer
	buf.WriteString(value[:limit])
	buf.WriteString("...")
	return buf.String()
}

var errUnauthorizedAI = fmt.Errorf("invalid AI agent key")
