package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

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
		"configs": map[string]interface{}{
			"codex": map[string]interface{}{
				"mcpServers": map[string]interface{}{
					"api-monitor": map[string]interface{}{
						"url": mcpURL,
						"headers": map[string]string{
							"Authorization": "Bearer " + key,
						},
					},
				},
			},
			"claudeDesktop": map[string]interface{}{
				"mcpServers": map[string]interface{}{
					"api-monitor": map[string]interface{}{
						"command": "npx",
						"args":    []string{"mcp-remote", mcpURL, "--header", "Authorization: Bearer " + key},
					},
				},
			},
			"curl": fmt.Sprintf("curl -H \"Authorization: Bearer %s\" %s", key, manifestURL),
		},
		"tools": tools,
		"policy": map[string]interface{}{
			"allowedMethods": []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
			"blockedPaths":   []string{"/api/ai/*", "/api/system/ai-access/key/*"},
			"blockedModes":   []string{string(manifest.ResponseStream), string(manifest.ResponseWebSocket)},
			"bodyLimitBytes": 1024 * 1024,
			"auth":           "Agent Key 作为系统级接入密钥使用；调用会写入审计记录。",
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
	_ = s.insertAIAudit(r.Context(), db, "admin", "rotate_key", "ai_agent_key", "success", 0, "AI 接入密钥已轮换", clientIP(r), r.UserAgent())
	return s.aiAccessOverview(r)
}

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

func (s *Service) validateAIAgent(r *http.Request, db *sql.DB) bool {
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
		_ = s.insertAIAudit(r.Context(), db, "external", "manifest", "/api/ai/manifest", "denied", time.Since(start).Milliseconds(), "invalid key", clientIP(r), r.UserAgent())
		return nil, errUnauthorizedAI
	}
	_ = s.insertAIAudit(r.Context(), db, "external", "manifest", "/api/ai/manifest", "success", time.Since(start).Milliseconds(), "manifest read", clientIP(r), r.UserAgent())
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
		_ = s.insertAIAudit(r.Context(), db, "external", "mcp", "/api/ai/mcp", "denied", time.Since(start).Milliseconds(), "invalid key", clientIP(r), r.UserAgent())
		return nil, http.StatusUnauthorized, errUnauthorizedAI
	}
	if r.Method == http.MethodGet {
		_ = s.insertAIAudit(r.Context(), db, "external", "mcp.describe", "/api/ai/mcp", "success", time.Since(start).Milliseconds(), "mcp metadata read", clientIP(r), r.UserAgent())
		return map[string]interface{}{
			"server":    "api-monitor",
			"protocol":  "mcp-json-rpc",
			"tools":     s.aiTools(),
			"resources": []map[string]string{{"uri": "api-monitor://routes", "name": "接口清单"}, {"uri": "api-monitor://openapi", "name": "OpenAPI"}},
		}, http.StatusOK, nil
	}
	var req aiMCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, http.StatusBadRequest, err
	}
	result, callErr := s.dispatchMCPTool(r, req)
	status := "success"
	if callErr != nil {
		status = "error"
	}
	_ = s.insertAIAudit(r.Context(), db, "external", req.Method, "/api/ai/mcp", status, time.Since(start).Milliseconds(), truncate(fmt.Sprint(req.Params), 500), clientIP(r), r.UserAgent())
	if callErr != nil {
		return mcpError(req.ID, -32000, callErr.Error()), http.StatusOK, nil
	}
	return map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result}, http.StatusOK, nil
}

func (s *Service) dispatchMCPTool(r *http.Request, req aiMCPRequest) (interface{}, error) {
	switch req.Method {
	case "initialize":
		return map[string]interface{}{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "api-monitor", "version": s.cfg.Version}, "capabilities": map[string]interface{}{"tools": map[string]bool{"listChanged": true}}}, nil
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
		return s.callAITool(r, params.Name, params.Arguments)
	default:
		return nil, fmt.Errorf("unsupported MCP method: %s", req.Method)
	}
}

func (s *Service) callAITool(r *http.Request, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "list_apis":
		return s.apiDocs(), nil
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
		{"name": "list_apis", "description": "读取系统自动生成的接口清单", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_openapi", "description": "读取 OpenAPI 3.1 文档", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_ai_manifest", "description": "读取 AI 接入能力清单", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_system_status", "description": "读取本机系统运行状态", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "call_api", "description": "调用 API Monitor 内部接口，支持 GET/POST/PUT/PATCH/DELETE、请求头和 JSON 请求体", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"method": map[string]interface{}{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}}, "path": map[string]interface{}{"type": "string", "description": "以 / 开头的系统接口路径"}, "headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]string{"type": "string"}}, "body": map[string]interface{}{"type": "object", "description": "JSON 请求体"}}, "required": []string{"path"}}},
	}
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
	_ = s.insertAIAudit(r.Context(), db, "admin", "save_mcp_server", id, "success", 0, req.Name, clientIP(r), r.UserAgent())
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
	_ = s.insertAIAudit(r.Context(), db, "admin", "delete_mcp_server", id, "success", 0, id, clientIP(r), r.UserAgent())
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
	_ = s.insertAIAudit(r.Context(), db, "admin", "save_skill", id, "success", 0, req.Name, clientIP(r), r.UserAgent())
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
	_ = s.insertAIAudit(r.Context(), db, "admin", "delete_skill", id, "success", 0, id, clientIP(r), r.UserAgent())
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

func clientIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
		return strings.TrimSpace(strings.Split(value, ",")[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > -1 {
		return host[:idx]
	}
	return host
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
