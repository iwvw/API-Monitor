package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// EnvelopeError 检查标准响应信封：body 为 {"success":false,"error":...} 时返回错误文本，
// 用于 AI 工具调用层识别 HTTP 2xx 但业务失败的情况，防止把失败误判为成功。
// 非信封结构（成功响应或裸 JSON）返回空字符串。
func EnvelopeError(payload interface{}) string {
	obj, ok := payload.(map[string]interface{})
	if !ok {
		return ""
	}
	success, ok := obj["success"].(bool)
	if !ok || success {
		return ""
	}
	msg, _ := obj["error"].(string)
	if msg == "" {
		msg = "未知业务错误"
	}
	return msg
}

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
	accessPolicy, err := s.getAIAgentAccessPolicy(r.Context(), db)
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
			"accessPolicy":   accessPolicy,
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

// AI 接入权限模式：
// minimal  - 只读（写方法一律拒绝）
// standard - 默认：写操作需显式开关，管理 AI 路由（admin-ai）不可达
// full     - 单用户自用最高权限：放开全部管理面与写操作，
//
//	仅保留防自毁的两条拦截（AI 递归调用、密钥轮换）
const (
	aiAgentAccessPolicyKey = "ai_agent_access_policy"
	AIAccessPolicyMinimal  = "minimal"
	AIAccessPolicyStandard = "standard"
	AIAccessPolicyFull     = "full"
	AIAccessPolicyDefault  = AIAccessPolicyStandard
)

// AIAgentAccessPolicy 读取当前 AI 接入权限模式，缺省 standard。
func (s *Service) AIAgentAccessPolicy(ctx context.Context) (string, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return AIAccessPolicyDefault, err
	}
	defer db.Close()
	return s.getAIAgentAccessPolicy(ctx, db)
}

func (s *Service) getAIAgentAccessPolicy(ctx context.Context, db *sql.DB) (string, error) {
	var value string
	_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", aiAgentAccessPolicyKey).Scan(&value)
	switch strings.TrimSpace(value) {
	case AIAccessPolicyMinimal, AIAccessPolicyStandard, AIAccessPolicyFull:
		return strings.TrimSpace(value), nil
	default:
		return AIAccessPolicyDefault, nil
	}
}

func (s *Service) setAIAgentAccessPolicy(r *http.Request) (map[string]interface{}, error) {
	var payload struct {
		Policy string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	switch payload.Policy {
	case AIAccessPolicyMinimal, AIAccessPolicyStandard, AIAccessPolicyFull:
	default:
		return nil, fmt.Errorf("policy 必须是 minimal / standard / full 之一")
	}
	db, err := s.store.Open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(r.Context(), `INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES (?, ?, ?, ?)`,
		aiAgentAccessPolicyKey, payload.Policy, "AI agent access policy", now); err != nil {
		return nil, err
	}
	_ = s.insertAIAudit(r.Context(), db, "admin", "set_policy", aiAgentAccessPolicyKey, payload.Policy, 0, "AI 接入权限模式已设置为 "+payload.Policy, s.clientIP(r), r.UserAgent())
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
	provided := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	// 与主机 Agent Key 校验对齐：长度不等快速返回 0，等长密钥走
	// 常量时间比较，避免普通 == 的逐字节时序差泄露密钥前缀信息。
	if len(expected) == 0 || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
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
		return map[string]interface{}{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]string{"name": "api-monitor", "version": s.cfg.Version},
			"capabilities": map[string]interface{}{
				"tools":     map[string]bool{"listChanged": true},
				"resources": map[string]bool{"listChanged": false},
			},
			"instructions": "这是 API Monitor 面板的 MCP 服务，可管理主机、DNS、模型网关、备份、定时任务等 400+ 内部接口。" +
				"找接口：先用 find_api 用自然语言描述意图（如「给 flyio 应用更新镜像」），必要时用 list_apis 按 group（主机实例/Cloudflare/模型网关…）缩小；" +
				"调用前必须用 get_route 确认该路径的真实可用方法与请求体结构，路径参数要用真实 ID 替换（先调用对应 list 接口获取）；" +
				"prefixRoute=true 或描述含「聚合前缀」的条目不可直接调用。",
		}, nil
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
	case "find_api":
		return s.aiFindAPIs(args)
	case "get_route":
		return s.getRouteContract(args)
	case "get_openapi":
		return s.openapiCompactDocument(r), nil
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
	case "run_batch":
		return s.aiRunBatch(r, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// aiRouteCatalog 返回紧凑的接口目录，支持 group / module / search 过滤与分页，用于渐进式披露。
func (s *Service) aiRouteCatalog(args map[string]interface{}) (interface{}, error) {
	group, _ := args["group"].(string)
	module, _ := args["module"].(string)
	search, _ := args["search"].(string)
	group = strings.TrimSpace(group)
	module = strings.TrimSpace(module)
	search = strings.ToLower(strings.TrimSpace(search))

	limit := 30
	if rawLimit, ok := args["limit"].(float64); ok && rawLimit > 0 && rawLimit <= 1000 {
		limit = int(rawLimit)
		if limit > 100 {
			limit = 100
		}
	}
	offset := 0
	if rawOffset, ok := args["offset"].(float64); ok && rawOffset > 0 && rawOffset <= 1e6 {
		offset = int(rawOffset)
	}

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
		// 聚合前缀路由（模块根/总入口）不是可调用端点：标注给 AI，
		// 提示应从其子路由中选择具体接口。
		prefixRoute := item.MatchMode == manifest.MatchPrefix &&
			strings.HasPrefix(item.Prefix, "/api/") &&
			!strings.HasPrefix(item.Prefix, "/sub") && !strings.HasPrefix(item.Prefix, "/v1")
		entry := map[string]interface{}{
			"path":    item.Prefix,
			"methods": item.Methods,
			"group":   item.Group,
			"module":  item.Module,
			"auth":    string(item.Auth),
			"desc":    desc,
			"hasBody": hasBody,
		}
		if prefixRoute {
			entry["prefixRoute"] = true
			entry["desc"] = desc + "（聚合前缀，不可直接调用；请用其子路由）"
		}
		result = append(result, entry)
	}
	total := len(result)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := result[offset:end]
	return map[string]interface{}{"count": total, "returned": len(page), "limit": limit, "offset": offset, "routes": page}, nil
}

// aiFindAPIs 根据自然语言意图粗召回匹配的路由，返回 top-k 与契约摘要。
// 采用宽松召回 + 低置信度标注：命中数少或分低时在 confidence 字段提示，
// 由 AI 决定是否转用 list_apis/get_route 兜底。
func (s *Service) aiFindAPIs(args map[string]interface{}) (interface{}, error) {
	intent, _ := args["intent"].(string)
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return nil, fmt.Errorf("intent is required")
	}
	limit := 5
	if rawLimit, ok := args["limit"].(float64); ok && rawLimit > 0 && rawLimit <= 1000 {
		limit = int(rawLimit)
		if limit > 10 {
			limit = 10
		}
	}
	group, _ := args["group"].(string)
	group = strings.TrimSpace(group)

	items := s.apiDocs()["routes"].([]apiDocRoute)
	filtered := items
	if group != "" {
		filtered = make([]apiDocRoute, 0, len(items))
		for _, item := range items {
			if item.Group == group {
				filtered = append(filtered, item)
			}
		}
	}

	matches := findTopRoutes(filtered, intent, limit)
	confidence := "low"
	if len(matches) > 0 {
		// 以最高命中分界：≥5 视为别名/片段级强命中，≥2 视为描述级命中。
		maxScore := matches[0].Score
		switch {
		case maxScore >= 5:
			confidence = "high"
		case maxScore >= 2:
			confidence = "medium"
		}
	}

	result := make([]map[string]interface{}, 0, len(matches))
	for _, match := range matches {
		route := match.Route
		desc := route.Detail
		if desc == "" {
			desc = route.Description
		}
		reasons := make([]map[string]interface{}, 0, len(match.Reasons))
		for _, r := range match.Reasons {
			reasons = append(reasons, map[string]interface{}{
				"term":   r.Term,
				"level":  r.Level,
				"weight": r.Weight,
			})
		}
		contract := map[string]interface{}{
			"path":               route.Prefix,
			"methods":            route.Methods,
			"group":              route.Group,
			"module":             route.Module,
			"auth":               string(route.Auth),
			"desc":               desc,
			"score":              match.Score,
			"matchReason":        reasons,
			"matchMode":          string(route.MatchMode),
			"responseMode":       string(route.ResponseMode),
			"status":             route.Status,
			"pathParams":         route.PathParams,
			"queryParams":        route.QueryParams,
			"requestContentType": route.RequestType,
			"requestSchema":      route.RequestSchema,
			"requestExample":     route.RequestBody,
		}
		if route.MatchMode == manifest.MatchPrefix && strings.HasPrefix(route.Prefix, "/api/") &&
			!strings.HasPrefix(route.Prefix, "/sub") && !strings.HasPrefix(route.Prefix, "/v1") {
			contract["prefixRoute"] = true
			contract["desc"] = desc + "（聚合前缀，不可直接调用；请用其子路由）"
		}
		result = append(result, contract)
	}

	suggestions := s.findAPISuggestions(intent, group, matches)
	return map[string]interface{}{
		"intent":      intent,
		"confidence":  confidence,
		"count":       len(result),
		"suggestions": suggestions,
		"note":        "find_api 是粗召回匹配，命中不代表一定正确；每条的 score/matchReason 说明命中原因，低置信度或无结果时参考 suggestions 改用关键词。",
		"routes":      result,
	}, nil
}

// findAPISuggestions 在无命中或低置信度时，为 AI 提供替代关键词与可读分组，帮助纠偏。
func (s *Service) findAPISuggestions(intent string, group string, matches []routeMatch) []string {
	items := s.apiDocs()["routes"].([]apiDocRoute)
	suggestions := make([]string, 0, 4)
	allGroups := map[string]bool{}
	for _, item := range items {
		allGroups[item.Group] = true
	}
	if group != "" {
		if !allGroups[group] {
			return []string{fmt.Sprintf("分组 %q 不存在；可用分组：%s", group, sortedGroupList(allGroups))}
		}
	}
	if len(matches) == 0 {
		// 提示可用分组面，引导 AI 换一个维度检索。
		if code, ok := providerCodeInIntent(intent); ok {
			suggestions = append(suggestions, fmt.Sprintf("意图 %q 未命中路由；试试用接口关键词（如 %s）或 list_apis 列表检索", intent, code))
		} else {
			suggestions = append(suggestions, fmt.Sprintf("意图 %q 未命中路由；试试 list_apis 按模块/关键词检索，或检查是否属于这些分组：%s", intent, sortedGroupList(allGroups)))
		}
		return append(suggestions, "低置信度 fallback：先 get_route 核对候选路由的路径参数再 call_api")
	}
	// 低置信度（匹配到但分低）：提示可用 group 过滤精确命中。
	suggestions = append(suggestions, fmt.Sprintf("当前匹配分较低；可按分组缩小范围：%s", sortedGroupList(allGroups)))
	return suggestions
}

func sortedGroupList(groups map[string]bool) []string {
	list := make([]string, 0, len(groups))
	for g := range groups {
		list = append(list, g)
	}
	sort.Strings(list)
	return list
}

// providerCodeInIntent 探测意图中的服务商标识（cf/aliyun/flyio 等英文 token），
// 供纠偏提示中使用真实存在的模块代码。
func providerCodeInIntent(intent string) (string, bool) {
	lower := strings.ToLower(intent)
	for _, code := range []string{"cf", "cloudflare", "aliyun", "tencent", "flyio", "koyeb", "github", "openai", "oracle", "m365", "onepanel"} {
		if strings.Contains(lower, code) {
			return code, true
		}
	}
	return "", false
}

// getRouteContract 返回单个接口的完整契约；入参 path 可以是具体路径（会匹配到模式路由）。
// 与 admin-ai 侧 get_route 对齐：匹配前剥离 query；命中聚合前缀（模块总入口）直接
// 拒绝并给子路由提示，绝不返回「GET 可用」的假契约（实测 GET 聚合根 404）。
func (s *Service) getRouteContract(args map[string]interface{}) (interface{}, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	matchPath := path
	if i := strings.IndexByte(matchPath, '?'); i >= 0 {
		matchPath = matchPath[:i]
	}
	if len(matchPath) > 1 {
		matchPath = strings.TrimRight(matchPath, "/")
	}

	items := s.apiDocs()["routes"].([]apiDocRoute)
	best := (*apiDocRoute)(nil)
	var prefixHit *apiDocRoute // 命中的聚合前缀（MatchPrefix）
	for i := range items {
		item := &items[i]
		if item.MatchMode == manifest.MatchPrefix && (item.Prefix == matchPath || strings.HasPrefix(matchPath, item.Prefix+"/")) {
			if prefixHit == nil || len(item.Prefix) > len(prefixHit.Prefix) {
				prefixHit = item
			}
			continue
		}
		if routePrefixMatches(item.Prefix, item.MatchMode, matchPath) {
			if best == nil || manifest.CompareRouteSpecificity(item.Prefix, best.Prefix) > 0 {
				best = item
			}
		}
	}
	if best != nil {
		// 具体路由胜出（pattern/exact 字面量更具体者）；即使父前缀存在也优先具体路由
		_ = prefixHit
	} else if prefixHit != nil {
		children := make([]string, 0, 5)
		for i := range items {
			p := items[i].Prefix
			if strings.HasPrefix(p, prefixHit.Prefix+"/") && items[i].MatchMode != manifest.MatchPrefix {
				children = append(children, p)
				if len(children) >= 5 {
					break
				}
			}
		}
		hint := fmt.Sprintf("路径 %s 是聚合前缀（模块总入口%s），不可直接调用；请改用其具体子路由", matchPath, explainPrefixDesc(prefixHit.Detail, prefixHit.Description))
		if len(children) > 0 {
			hint += "，例如：" + strings.Join(children, "、")
		} else {
			hint += "（可用 list_apis 按 group/module 浏览具体接口）"
		}
		return nil, fmt.Errorf("%s", hint)
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
		"matchedPath":        matchPath,
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

func explainPrefixDesc(detail, description string) string {
	text := detail
	if text == "" {
		text = description
	}
	if text == "" {
		return ""
	}
	if len([]rune(text)) > 40 {
		text = string([]rune(text)[:40]) + "…"
	}
	return "：" + text
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

// batchOp 表示 run_batch 中的单个操作。
type batchOp struct {
	name   string
	path   string
	method string
	full   map[string]interface{}
}

// batchResult 表示 run_batch 中单个操作的执行结果。
type batchResult struct {
	index    int
	name     string
	path     string
	method   string
	err      error
	response interface{}
}

// aiRunBatch 一次提交多个接口调用（串行或并行），聚合返回每个操作的结果。
// 每个操作复用 callAPIFromAI 的完整约束（路由匹配、写权限门槛、递归/密钥阻断），
// 无独立绕过路径；单个操作失败不会中断整批（除非 stopOnError）。
// 每个子操作都会写入 AI 访问审计（run_batch.<index>）。
func (s *Service) aiRunBatch(r *http.Request, args map[string]interface{}) (interface{}, error) {
	rawOps, ok := args["operations"].([]interface{})
	if !ok || len(rawOps) == 0 {
		return nil, fmt.Errorf("operations is required and must be a non-empty array")
	}
	if len(rawOps) > 20 {
		return nil, fmt.Errorf("operations exceed 20 items per batch")
	}
	mode := "serial"
	if rawMode, ok := args["mode"].(string); ok {
		if rawMode == "parallel" {
			mode = "parallel"
		}
	}
	stopOnError := false
	if rawStop, ok := args["stopOnError"].(bool); ok {
		stopOnError = rawStop
	}

	ops := make([]batchOp, 0, len(rawOps))
	for i, raw := range rawOps {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("operations[%d] is not an object", i)
		}
		path, _ := m["path"].(string)
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("operations[%d].path is required", i)
		}
		method, _ := m["method"].(string)
		if method == "" {
			method = http.MethodGet
		}
		ops = append(ops, batchOp{
			name:   opLabel(m, i),
			path:   path,
			method: strings.ToUpper(method),
			full:   m,
		})
	}

	results := make([]batchResult, len(ops))
	if mode == "parallel" {
		var wg sync.WaitGroup
		for i := range ops {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i] = s.runSingleBatchOp(r.Context(), ops[i], i)
			}(i)
		}
		wg.Wait()
	} else {
		for i := range ops {
			results[i] = s.runSingleBatchOp(r.Context(), ops[i], i)
			if stopOnError && results[i].err != nil {
				// 保留后续操作状态为未执行。
				for j := i + 1; j < len(ops); j++ {
					results[j].index = j
					results[j].name = ops[j].name
					results[j].path = ops[j].path
					results[j].method = ops[j].method
					results[j].err = fmt.Errorf("batch stopped after previous error")
				}
				break
			}
		}
	}

	items := make([]map[string]interface{}, 0, len(results))
	failed := 0
	for _, r := range results {
		item := map[string]interface{}{
			"index":  r.index,
			"name":   r.name,
			"path":   r.path,
			"method": r.method,
		}
		if r.err != nil {
			failed++
			item["ok"] = false
			item["error"] = r.err.Error()
		} else {
			item["ok"] = true
			item["data"] = r.response
		}
		items = append(items, item)
	}
	s.auditRunBatchSubOps(r, ops, results)
	return map[string]interface{}{
		"mode":   mode,
		"total":  len(items),
		"ok":     len(items) - failed,
		"failed": failed,
		"note":   "run_batch 逐个复用 call_api 的鉴权与只读约束；写操作需全局开启「允许写入」。",
		"items":  items,
	}, nil
}

// auditRunBatchSubOps 将 run_batch 的子操作写入 AI 访问审计，便于追溯批量调用。
// 审计失败不影响批量结果本身。
func (s *Service) auditRunBatchSubOps(r *http.Request, ops []batchOp, results []batchResult) {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return
	}
	defer db.Close()
	if err := s.ensureAIAccessSchema(r.Context(), db); err != nil {
		return
	}
	ip := s.clientIP(r)
	ua := r.UserAgent()
	for i, res := range results {
		status := "success"
		detail := ""
		if res.err != nil {
			status = "error"
			detail = truncate(res.err.Error(), 200)
		}
		action := fmt.Sprintf("run_batch.%d", res.index)
		_ = s.insertAIAudit(r.Context(), db, "external", action, res.path, status, 0, detail, ip, ua)
		if i >= 400 {
			break
		}
	}
}

func (s *Service) runSingleBatchOp(ctx context.Context, op batchOp, index int) batchResult {
	result, err := s.callAPIFromAI(ctx, op.full)
	if err != nil {
		return batchResult{index: index, name: op.name, path: op.path, method: op.method, err: err}
	}
	if resp, ok := result.(AICallResponse); ok {
		if resp.StatusCode >= 400 {
			msg := fmt.Sprintf("non-2xx status %d", resp.StatusCode)
			if resp.Body != nil {
				msg = fmt.Sprintf("non-2xx status %d: %v", resp.StatusCode, resp.Body)
			}
			return batchResult{index: index, name: op.name, path: op.path, method: op.method, err: fmt.Errorf("%s", msg)}
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if businessErr := EnvelopeError(resp.Body); businessErr != "" {
				return batchResult{index: index, name: op.name, path: op.path, method: op.method, err: fmt.Errorf("business failure: %s", businessErr)}
			}
		}
		return batchResult{index: index, name: op.name, path: op.path, method: op.method, response: resp.Body}
	}
	return batchResult{index: index, name: op.name, path: op.path, method: op.method, response: result}
}

func opLabel(m map[string]interface{}, index int) string {
	if name, ok := m["name"].(string); ok && strings.TrimSpace(name) != "" {
		return name
	}
	return fmt.Sprintf("op-%d", index)
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
		{"name": "list_apis", "description": "接口目录浏览：支持按 group（如 主机实例 / Cloudflare / 模型网关）或模块过滤、关键词 search、分页 offset/limit。返回的 prefixRoute=true 条目是聚合前缀（模块总入口），不可直接调用，请改用其子路由。先按 group 缩到 100 条以内再挑，效率最高；配合 find_api 定位更省 token。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"group": map[string]interface{}{"type": "string", "description": "按分组过滤（如 模型网关 / Cloudflare / 主机实例）"}, "module": map[string]interface{}{"type": "string", "description": "按模块过滤（如 flyio / cloudflare-dns）"}, "search": map[string]interface{}{"type": "string", "description": "按路径或描述关键词过滤"}, "limit": map[string]interface{}{"type": "number", "description": "返回条数上限（默认 30，最大 100）"}, "offset": map[string]interface{}{"type": "number", "description": "分页偏移（默认 0）"}}}},
		{"name": "find_api", "description": "自然语言意图定位接口（top-k 召回）。适用：知道想做什么但不确定接口路径/名称时，例如 intent=“给 flyio 应用更新镜像”。每个命中会给出 path/methods/desc 与命中原因；优先取 score 高且非 prefixRoute 的条目。不要用来列全量目录（那是 list_apis 的职责）；召回不满或低置信度时，结合 list_apis 的 group/search 兜底。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"intent": map[string]interface{}{"type": "string", "description": "意图描述，如“列出所有 DNS 解析记录”“给 flyio 应用更新镜像”“查看主机监控状态”"}, "limit": map[string]interface{}{"type": "number", "description": "返回条数上限（默认 5，最大 10）"}, "group": map[string]interface{}{"type": "string", "description": "按分组过滤（可选）"}}, "required": []string{"intent"}}},
		{"name": "get_route", "description": "读取单个接口的完整契约：真实可用方法（methods，已按 handler 校准）、鉴权、路径参数（含示例值）、查询参数、请求体 schema 与示例。调用 call_api 前必须先用本工具确认方法与请求体结构；路径参数要用实际资源 ID 替换占位符（先调用对应 list 接口获取真实 ID）。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string", "description": "接口路径，如 /api/flyio/apps/{appName}/update-image 或具体路径"}}, "required": []string{"path"}}},
		{"name": "get_openapi", "description": "读取 OpenAPI 3.1 文档", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_ai_manifest", "description": "读取 AI 接入能力清单", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "get_system_status", "description": "读取本机系统运行状态（CPU/内存/磁盘）；displayTime/serverTime 为站点当前时间（本地时区），回答时间/换算 cron 必须用 displayTime 或 serverTime.local，禁止用 timestamp（UTC）", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{"name": "call_api", "description": "调用 API Monitor 内部接口，支持 GET/POST/PUT/PATCH/DELETE、请求头和 JSON 请求体；请求体结构先用 get_route 获取。强制规则：写操作（POST/PUT/PATCH/DELETE）返回后必须立即用 GET 回读验证真实生效（如列表/详情确认状态、next_run 等），且必须检查响应中的 success/error 字段，发现 success=false 或 error 非空即视为失败，绝不向用户宣称完成", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"method": map[string]interface{}{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}}, "path": map[string]interface{}{"type": "string", "description": "以 / 开头的系统接口路径"}, "headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]string{"type": "string"}}, "body": map[string]interface{}{"type": "object", "additionalProperties": true, "description": "JSON 请求体，字段以 get_route 返回的 requestSchema/requestExample 为准"}}, "required": []string{"path"}}},
		{"name": "run_batch", "description": "一次提交 1-20 个接口调用并聚合返回结果（串行或并行），减少多轮往返；每个操作复用 call_api 的鉴权与写权限约束。强制规则：含写操作（POST/PUT/PATCH/DELETE）的批次，完成后必须回读验证真实生效并检查每个子项的 ok/error 字段，任一子项 ok=false 或业务失败即视为整体未完成，绝不宣称完成", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"operations": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string", "description": "操作名（便于阅读结果）"}, "method": map[string]interface{}{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE"}}, "path": map[string]interface{}{"type": "string", "description": "以 / 开头的系统接口路径"}, "headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]string{"type": "string"}}, "body": map[string]interface{}{"type": "object", "additionalProperties": true}}, "required": []string{"path"}}, "description": "要执行的接口调用数组"}, "mode": map[string]interface{}{"type": "string", "enum": []string{"serial", "parallel"}, "description": "执行模式（默认 serial）"}, "stopOnError": map[string]interface{}{"type": "boolean", "description": "serial 模式下遇到失败是否停止后续（默认 false）"}}, "required": []string{"operations"}}},
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
- api-monitor://route-index：紧凑路由索引（仅 path/methods/group/desc/auth，适合预加载）
- api-monitor://route-index/{group}：按分组读取紧凑路由索引（如 api-monitor://route-index/Cloudflare）
- api-monitor://openapi：OpenAPI 3.1 文档

## 接入步骤（渐进式披露）
1. 用 find_api <意图> 直接定位目标接口（返回 top-k 匹配与契约摘要，命中不确信时再用 list_apis / get_route 核对）。
2. 用 list_apis 扫目录（可按 group / module / search 过滤，先只看目标模块，省 token）。
3. 确定要调用的接口后，用 get_route <path> 获取该接口的完整契约（路径参数、请求体 schema、示例、鉴权）。
4. 按契约用 call_api 调用；请求体字段以 get_route 返回的 requestSchema / requestExample 为准，不要猜。
5. 默认只读；写操作（POST/PUT/PATCH/DELETE）会返回「写入未启用」提示，需管理员在「API 文档 → AI 接入」开启「允许写入」。
6. 密钥可随时在「API 文档 → AI 接入」页面轮换；请勿将密钥写入公开仓库。

## 强制验证规则（不可省略）
1. 写操作（POST/PUT/PATCH/DELETE）调用返回后，必须立即回读验证真实生效：用 GET 列表/详情接口确认目标资源已存在且状态正确（如 enabled、next_run 等），仅凭创建接口自身返回 2xx 不算完成。
2. 每次调用都必须检查响应：HTTP 非 2xx、或 body 中 success=false、或 error 字段非空，均视为失败；任一失败出现时，禁止向用户宣称任务完成，必须如实报告错误。
3. 宁可多一次回读调用，也不要在未验证生效前宣告成功；无法验证时如实说明「未能验证」。
`, s.cfg.Version, key, key, manifestURL, mcpURL, openAPIURL, mcpURL, key, mcpURL, key, toolsText)
}

func (s *Service) mcpResources() []map[string]interface{} {
	return []map[string]interface{}{
		{"uri": "api-monitor://routes", "name": "接口清单", "mimeType": "application/json"},
		{"uri": "api-monitor://route-index", "name": "紧凑路由索引", "mimeType": "application/json"},
		{"uri": "api-monitor://route-index/{group}", "name": "紧凑路由索引（按分组）", "mimeType": "application/json"},
		{"uri": "api-monitor://openapi", "name": "OpenAPI", "mimeType": "application/json"},
	}
}

func (s *Service) mcpReadResource(r *http.Request, uri string) (interface{}, error) {
	var content string
	switch {
	case uri == "api-monitor://routes":
		encoded, err := json.Marshal(s.apiDocs())
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	case uri == "api-monitor://route-index":
		encoded, err := json.Marshal(s.routeIndexPayload(""))
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	case strings.HasPrefix(uri, "api-monitor://route-index/"):
		group := strings.TrimPrefix(uri, "api-monitor://route-index/")
		if group == "" {
			return nil, fmt.Errorf("missing group in template resource uri")
		}
		encoded, err := json.Marshal(s.routeIndexPayload(group))
		if err != nil {
			return nil, err
		}
		content = string(encoded)
	case uri == "api-monitor://openapi":
		encoded, err := json.Marshal(s.openapiCompactDocument(r))
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

// routeIndexPayload 生成紧凑路由索引：仅 path / methods / group / desc / auth，
// 供客户端预加载路由目录，避免每次用 list_apis 全量扫描。
// group 非空时只返回该分组的路由（分片读取）。
func (s *Service) routeIndexPayload(group string) map[string]interface{} {
	items := s.apiDocs()["routes"].([]apiDocRoute)
	routes := make([]map[string]interface{}, 0, len(items))
	groups := map[string]int{}
	for _, item := range items {
		if group != "" && item.Group != group {
			continue
		}
		desc := item.Detail
		if desc == "" {
			desc = item.Description
		}
		groups[item.Group]++
		routes = append(routes, map[string]interface{}{
			"path":    item.Prefix,
			"methods": item.Methods,
			"group":   item.Group,
			"auth":    string(item.Auth),
			"desc":    desc,
		})
	}
	payload := map[string]interface{}{
		"count":  len(routes),
		"routes": routes,
	}
	if group != "" {
		payload["group"] = group
	} else {
		payload["groups"] = groups
	}
	return payload
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
