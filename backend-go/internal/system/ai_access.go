package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
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
	// 进程内只执行一次：这些 DDL 幂等，但原本每次 MCP 请求都要跑 4 条语句。
	// 失败不置位，下一次请求会重试（瞬时故障可自愈）。
	// 数据库被导入/恢复整体替换后 schema 代数变化，缓存失效重建
	// （否则新库里可能根本没有这些表）。
	s.aiAccessSchemaMu.Lock()
	defer s.aiAccessSchemaMu.Unlock()
	generation := database.SchemaGeneration()
	if s.aiAccessSchemaReady && s.aiAccessSchemaGeneration == generation {
		return nil
	}
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
	s.aiAccessSchemaReady = true
	s.aiAccessSchemaGeneration = generation
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
		"guide": s.aiAccessGuide(mcpURL, manifestURL, openAPIURL, baseURL, key, tools),
		"tools": tools,
		"policy": map[string]interface{}{
			"allowedMethods": []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
			"blockedPaths":   []string{"/api/ai/*", "/api/system/ai-access/key/*", "/api/ai-access/key/*"},
			"blockedModes":   []string{string(manifest.ResponseStream), string(manifest.ResponseWebSocket)},
			"bodyLimitBytes": 1024 * 1024,
			"accessPolicy":   accessPolicy,
			"auth":           "Agent Key 作为系统级接入密钥使用；权限由上方模式决定，所有调用都会写入审计记录。",
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

// AI 接入权限模式（四档，逐级放开）：
//
//	minimal   - 只读：写方法（POST/PUT/PATCH/DELETE）一律拒绝
//	sensitive - 敏感：允许写入；危险命令软拦截，调用方显式确认后才执行
//	standard  - 标准：允许写入；危险命令硬拦截（直接拒绝）
//	full      - 全部权限：等同 SSH root，放开全部管理面与危险操作
//
//	所有模式都保留防自毁的两条拦截（AI 递归调用、密钥轮换）。
const (
	aiAgentAccessPolicyKey  = "ai_agent_access_policy"
	AIAccessPolicyMinimal   = "minimal"
	AIAccessPolicySensitive = "sensitive"
	AIAccessPolicyStandard  = "standard"
	AIAccessPolicyFull      = "full"
	AIAccessPolicyDefault   = AIAccessPolicyStandard
)

// aiAccessPolicies 是合法权限模式集合，供读取回退与写入校验共用。
var aiAccessPolicies = map[string]bool{
	AIAccessPolicyMinimal:   true,
	AIAccessPolicySensitive: true,
	AIAccessPolicyStandard:  true,
	AIAccessPolicyFull:      true,
}

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
	trimmed := strings.TrimSpace(value)
	if aiAccessPolicies[trimmed] {
		return trimmed, nil
	}
	return AIAccessPolicyDefault, nil
}

func (s *Service) setAIAgentAccessPolicy(r *http.Request) (map[string]interface{}, error) {
	var payload struct {
		Policy string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if !aiAccessPolicies[payload.Policy] {
		return nil, fmt.Errorf("policy 必须是 minimal / sensitive / standard / full 之一")
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

// ValidateAIAgentRequest 对外暴露 AI Agent 接入校验（Agent Key 或带 ai:mcp scope
// 的 API Key），供其它模块（如 serveragent 的二进制直传端点）复用，避免各处重复
// 实现密钥校验逻辑。
func (s *Service) ValidateAIAgentRequest(r *http.Request) bool {
	db, err := s.store.Open(r.Context())
	if err != nil {
		return false
	}
	defer db.Close()
	return s.validateAIAgent(r, db)
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
	// 注意：GET 已在 ServeHTTP 层按 MCP 规范统一返回 405（本服务不提供
	// server→client SSE 流），因此这里只会收到 POST。
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

// mcpToolResult 按 MCP 规范组装 tools/call 结果：content 数组为必需，
// structuredContent 为可选的结构化副本。
//
// 两者都是完整负载，大结果理论上会翻倍。这里保留双份而不是按大小丢弃
// structuredContent：部分客户端只读该字段，静默省略会直接让工具结果变空。
// 体积由上游的硬上限控制（Agent 侧 256KB 首尾保留 + 命令输出 120K rune），
// 不在这里做有兼容风险的裁剪。
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
	case "filebox_share_from_agent":
		return s.fileboxShareFromAgent(r.Context(), args)
	case "agent_run_command":
		return s.agentRunCommand(r.Context(), args)
	case "agent_task_result":
		return s.agentTaskResult(r.Context(), args)
	case "agent_fetch_url":
		return s.agentFetchURL(r.Context(), args)
	case "agent_transfer_file":
		return s.agentTransferFile(r.Context(), args)
	case "agent_upload_file":
		return s.agentUploadFile(r.Context(), args)
	case "agent_upload_chunk":
		return s.agentUploadChunk(r.Context(), args)
	case "agent_file_roots":
		return s.agentFileRoots(r.Context(), args)
	case "agent_file_stat":
		return s.agentFileStat(r.Context(), args)
	case "agent_file_chmod":
		return s.agentFileChmod(r.Context(), args)
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
		return nil, fmt.Errorf("API 路由不存在: %s（%s）", path, s.suggestNearbyRoutes(matchPath, items))
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

// suggestNearbyRoutes 在 get_route 完全未命中时，给出与目标路径最相近的可调用候选，
// 帮助 AI 从「路由不存在」的僵局里纠偏：优先同前缀的叶子，其次同段数/编辑距离相近者。
func (s *Service) suggestNearbyRoutes(matchPath string, items []apiDocRoute) string {
	candidates := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && p != matchPath && !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}
	// 1) 同前缀族：去掉尾部一段后，仍能作为前缀命中/被 pattern 匹配的叶子
	for i := range items {
		item := &items[i]
		if item.MatchMode == manifest.MatchPrefix {
			continue
		}
		// 精确同前缀（/api/x/y → /api/x/y/...）
		if strings.HasPrefix(matchPath, strings.TrimRight(item.Prefix, "/")+"/") {
			add(item.Prefix)
			continue
		}
		// 占位符化后 pattern 同段数命中（/api/x/{id} → /api/x/123）
		if routePatternMatches(item.Prefix, matchPath) {
			add(item.Prefix)
		}
	}
	// 2) 编辑距离相近（同段数，字面段不同的候选）
	if len(candidates) < 4 {
		parts := strings.Split(matchPath, "/")
		for i := range items {
			item := &items[i]
			if item.MatchMode == manifest.MatchPrefix {
				continue
			}
			ip := strings.Split(item.Prefix, "/")
			if len(ip) == len(parts) {
				add(item.Prefix)
				if len(candidates) >= 4 {
					break
				}
			}
		}
	}
	if len(candidates) == 0 {
		return "可用 list_apis 按 group/module 检索，或核对路径是否缺/多了一段"
	}
	return "相近接口：" + strings.Join(candidates, "、")
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

// fileboxShareFromAgent 是 filebox_share_from_agent 元工具的实现：把结构化参数
// 组装成对 /api/filebox/share-from-agent 的调用，复用 callAPIFromAI 的完整约束
// （路由匹配、写权限门槛、内部调用标记注入），避免出现绕过写开关的第二条路径。
func (s *Service) fileboxShareFromAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, _ := args["serverId"].(string)
	remotePath, _ := args["remotePath"].(string)
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(remotePath) == "" {
		return nil, fmt.Errorf("serverId 与 remotePath 均为必填")
	}
	body := map[string]interface{}{
		"serverId":   strings.TrimSpace(serverID),
		"remotePath": strings.TrimSpace(remotePath),
	}
	for _, key := range []string{"storageTarget", "expiry", "max_downloads", "access_password"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			body[key] = value
		}
	}
	if value, ok := args["burn_after_reading"]; ok && value != nil {
		body["burn_after_reading"] = value
	}
	return s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodPost,
		"path":   "/api/filebox/share-from-agent",
		"body":   body,
	})
}

// resolveServerID 把 AI 传来的主机标识解析成真实 serverId。
//
// AI 常见输入是主机名而不是 ID（工具描述里写「先调 /api/server/accounts 取 ID」
// 只解决了一半问题），而那个接口会返回全量账号并带解密后的凭据。这里在
// serveragent 侧按 id → 精确名称 → 主机地址 的顺序解析，命中多台时返回
// 候选列表让 AI 缩小范围，而不是静默挑一台。
func (s *Service) resolveServerID(serverID, serverName, host string) (string, error) {
	id := strings.TrimSpace(serverID)
	name := strings.TrimSpace(serverName)
	host = strings.TrimSpace(host)
	if id != "" {
		return id, nil
	}
	if name == "" && host == "" {
		return "", fmt.Errorf("serverId 必填（可由 serverName 或 host 代替）")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		return "", fmt.Errorf("解析主机失败: %w", err)
	}
	defer db.Close()

	candidates := make([]string, 0, 2)
	for _, field := range []string{"name", "host"} {
		value := name
		if field == "host" {
			value = host
		}
		if value == "" {
			continue
		}
		rows, err := db.QueryContext(ctx,
			"SELECT id FROM server_accounts WHERE "+field+" = ? LIMIT 5", value)
		if err != nil {
			return "", fmt.Errorf("解析主机失败: %w", err)
		}
		for rows.Next() {
			var candidate string
			if err := rows.Scan(&candidate); err == nil {
				candidates = append(candidates, candidate)
			}
		}
		rows.Close()
		if len(candidates) > 0 {
			break
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", fmt.Errorf("找不到主机 serverId=%q serverName=%q host=%q；请调 /api/aiagent/servers 取真实 ID（只返回 id/name/host/online）", serverID, serverName, host)
	default:
		label := name
		if label == "" {
			label = host
		}
		return "", fmt.Errorf("主机标识 %q 命中多台（%s），请直接传 serverId", label, strings.Join(candidates, ", "))
	}
}

// agentRunCommand 是 agent_run_command 元工具的实现：组装对
// /api/server/agent/command/{id} 的 POST 调用，复用 callAPIFromAI 的完整约束
// （写权限门槛、危险命令门控由下游 serveragent 处理）。
func (s *Service) agentRunCommand(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	rawServerID, _ := args["serverId"].(string)
	serverName, _ := args["serverName"].(string)
	host, _ := args["host"].(string)
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command 必填")
	}
	serverID, err := s.resolveServerID(rawServerID, serverName, host)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{
		"command": command,
	}
	if timeout, ok := args["timeout"].(float64); ok && timeout > 0 {
		body["timeout"] = int(timeout)
	}
	if confirm, ok := args["confirm"].(bool); ok && confirm {
		body["confirm"] = true
	}
	if cwd, ok := args["cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
		body["cwd"] = strings.TrimSpace(cwd)
	}
	if wait, ok := args["wait"].(bool); ok && !wait {
		body["wait"] = false
	}
	return s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodPost,
		"path":   "/api/server/agent/command/" + serverID,
		"body":   body,
	})
}

// agentTaskResult 是 agent_task_result 元工具的实现：查询异步命令任务的终态。
// 与 agent_run_command 的 wait=false 配套，让长任务不必占住一次同步请求。
func (s *Service) agentTaskResult(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, _ := args["taskId"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("taskId 必填（来自 agent_run_command 的 wait=false 返回）")
	}
	return s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodGet,
		"path":   "/api/server/tasks/" + url.PathEscape(taskID),
	})
}

// agentFetchURL 是 agent_fetch_url 元工具的实现：让目标主机直接从 URL 拉取
// 文件写入本地路径，字节不经面板中转，避免 base64 与 1MB 请求体限制。
func (s *Service) agentFetchURL(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, path, err := s.resolveFileTarget(args)
	if err != nil {
		return nil, err
	}
	url, _ := args["url"].(string)
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("url 为必填")
	}
	if err := s.requireAgentFileWrite(ctx); err != nil {
		return nil, err
	}
	result, err := s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodPost,
		"path":   "/api/server/agent/fetch-url",
		"body": map[string]interface{}{
			"serverId": serverID,
			"url":      strings.TrimSpace(url),
			"path":     path,
		},
	})
	if err != nil {
		return nil, err
	}
	// Agent 自己下载时已知道真实落盘路径；把它补出来，避免调用方拿虚拟路径去执行。
	if stat, statErr := s.agentFileStat(ctx, map[string]interface{}{
		"serverId": serverID,
		"path":     path,
	}); statErr == nil {
		if statMap, ok := stat.(map[string]interface{}); ok {
			if real, ok := statMap["realPath"].(string); ok && real != "" {
				if resp, ok := result.(AICallResponse); ok {
					if body, ok := resp.Body.(map[string]interface{}); ok {
						body["realPath"] = real
						body["note"] = agentFilePathNote(real)
					}
					return resp, nil
				}
			}
		}
	}
	return result, nil
}

// agentTransferFile 是 agent_transfer_file 元工具的实现：把一台 Agent 主机上的文件
// 搬到另一台 Agent 主机，全程字节不经 AI、也不经面板中转。步骤：
//  1. 对源主机调 /api/filebox/share-from-agent，源主机 Agent 自己 PUT 到存储节点
//     （storageTarget=auto 优先节点直传），拿到一次性直链 directUrl；
//  2. 对目标主机调 /api/server/agent/fetch-url，目标主机自己从直链拉取落盘；
//  3. 删除临时分享，避免直链残留。
//
// AI 全程只传两个路径字符串，无 base64、无 1MB 限制、无分块。适合大文件
// （5~30MB 及以上）主机间搬运；源或目标任一方离线时对应步骤会返回可读错误。
func (s *Service) agentTransferFile(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sourceServerID, _ := args["sourceServerId"].(string)
	sourcePath, _ := args["sourcePath"].(string)
	targetServerID, _ := args["targetServerId"].(string)
	targetPath, _ := args["targetPath"].(string)
	if strings.TrimSpace(sourceServerID) == "" || strings.TrimSpace(sourcePath) == "" ||
		strings.TrimSpace(targetServerID) == "" || strings.TrimSpace(targetPath) == "" {
		return nil, fmt.Errorf("sourceServerId、sourcePath、targetServerId、targetPath 均为必填")
	}
	sourceServerID = strings.TrimSpace(sourceServerID)
	sourcePath = strings.TrimSpace(sourcePath)
	targetServerID = strings.TrimSpace(targetServerID)
	targetPath = strings.TrimSpace(targetPath)

	shareBody := map[string]interface{}{
		"serverId":      sourceServerID,
		"remotePath":    sourcePath,
		"storageTarget": "auto",
	}
	if target, ok := args["storageTarget"].(string); ok && strings.TrimSpace(target) != "" {
		shareBody["storageTarget"] = strings.TrimSpace(target)
	}
	shareResult, err := s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodPost,
		"path":   "/api/filebox/share-from-agent",
		"body":   shareBody,
	})
	if err != nil {
		return nil, fmt.Errorf("源主机文件分享失败: %w", err)
	}
	shareResponse, ok := shareResult.(AICallResponse)
	if !ok {
		return nil, fmt.Errorf("源主机文件分享返回异常: %v", shareResult)
	}
	directURL, code := transferShareURL(shareResponse)
	if directURL == "" {
		return nil, fmt.Errorf("源主机文件分享未返回可下载直链: %v", shareResponse.Body)
	}

	fetchResult, fetchErr := s.callAPIFromAI(ctx, map[string]interface{}{
		"method": http.MethodPost,
		"path":   "/api/server/agent/fetch-url",
		"body": map[string]interface{}{
			"serverId": targetServerID,
			"url":      directURL,
			"path":     targetPath,
		},
	})
	// 无论成败都清理临时分享，避免直链长期可用。
	if code != "" {
		_, _ = s.callAPIFromAI(ctx, map[string]interface{}{
			"method": http.MethodDelete,
			"path":   "/api/filebox/" + code,
		})
	}
	if fetchErr != nil {
		return nil, fmt.Errorf("目标主机拉取文件失败: %w", fetchErr)
	}

	fetchBody := interface{}(nil)
	if fetchResponse, ok := fetchResult.(AICallResponse); ok {
		fetchBody = fetchResponse.Body
	}
	// 目标主机落盘后的真实路径：源/目标都在沙箱内时，虚拟路径没法直接执行。
	realPath := ""
	if stat, statErr := s.agentFileStat(ctx, map[string]interface{}{
		"serverId": targetServerID,
		"path":     targetPath,
	}); statErr == nil {
		if statMap, ok := stat.(map[string]interface{}); ok {
			realPath, _ = statMap["realPath"].(string)
		}
	}
	return map[string]interface{}{
		"success":        true,
		"transport":      "agent-to-agent",
		"sourceServerId": sourceServerID,
		"sourcePath":     sourcePath,
		"targetServerId": targetServerID,
		"targetPath":     targetPath,
		"realPath":       realPath,
		"fetch":          fetchBody,
		"note":           "字节由源主机直传存储节点、目标主机直链拉取，全程未经 AI 与面板中转。" + agentFilePathNote(realPath),
	}, nil
}

// transferShareURL 从 share-from-agent 的响应体中提取直链与分享 code。
func transferShareURL(result AICallResponse) (directURL, code string) {
	body, ok := result.Body.(map[string]interface{})
	if !ok {
		return "", ""
	}
	directURL, _ = body["directUrl"].(string)
	code, _ = body["code"].(string)
	return strings.TrimSpace(directURL), strings.TrimSpace(code)
}

// maxInlineBase64Bytes 是「字节经模型输出」这条路的上限。
//
// 原来的 88MB 是虚构的：真实瓶颈不是面板，而是模型必须把整个 base64 逐 token
// 生成出来（1024 字符 ≈ 700 token），而且长 base64 复述极易被静默改写。
// 因此这里收到 256KB（≈340K token 之前的现实操作区间，覆盖绝大多数脚本/配置），
// 超限时直接给出「不改用模型传字节」的替代路径，而不是让调用方反复失败。
const maxInlineBase64Bytes = 256 * 1024

// agentUploadFile 是 agent_upload_file 元工具的实现：把 base64 编码的文件内容
// 一次性直传到目标 Agent 主机（面板按分块经 socket 下发）。
//
// 定位：小文件兜底。大文件必须走「字节不过模型」的通道（shell + curl 直传、
// agent_fetch_url、agent_transfer_file、filebox 直传），本工具会在超限时
// 把这些命令原样写进错误信息里。
func (s *Service) agentUploadFile(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, path, err := s.resolveFileTarget(args)
	if err != nil {
		return nil, err
	}
	contentB64, _ := args["content_base64"].(string)
	if strings.TrimSpace(contentB64) == "" {
		return nil, fmt.Errorf("content_base64 为必填（文件的 base64 编码）")
	}
	if len(contentB64) > maxInlineBase64Bytes*4/3+8 {
		return nil, fmt.Errorf(
			"content_base64 过大（解码后上限 %d KB，当前约 %d KB）。本工具适合小文件；"+
				"大文件请改用字节不经模型的通道（任选其一）：\n"+
				"1) 宿主能执行 shell → curl -H \"Authorization: Bearer <agentKey>\" -F serverId=%s -F path=%s -F file=@./本地文件 %s/api/server/agent/upload\n"+
				"2) 文件已在某台 Agent 主机上 → agent_transfer_file\n"+
				"3) 文件有可访问 URL → agent_fetch_url\n"+
				"4) 客户端能自己读文件 → agent_upload_chunk 逐个 offset 上传",
			maxInlineBase64Bytes/1024, len(contentB64)*3/4/1024, serverID, path, s.publicBaseURL())
	}
	if err := s.requireAgentFileWrite(ctx); err != nil {
		return nil, err
	}
	if s.agentFileOps == nil && s.agentFiles == nil {
		return nil, fmt.Errorf("Agent 文件直传能力未配置")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(contentB64))
	if err != nil {
		return nil, fmt.Errorf("content_base64 不是合法 base64: %w", err)
	}
	if len(data) > maxInlineBase64Bytes {
		return nil, fmt.Errorf("解码后大小 %d 字节超过上限 %d 字节，请改用 agent_upload_chunk", len(data), maxInlineBase64Bytes)
	}
	// 可选校验：字节经模型复述后无法保证完整，调用方能算就比对一次。
	if want := strings.TrimSpace(stringArg(args, "sha256")); want != "" {
		got := sha256Hex(data)
		if !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("sha256 校验失败：期望 %s，实际 %s。字节很可能在传输中被改写，请改用不经模型的通道", want, got)
		}
	}

	result := AgentFileWriteResult{Size: int64(len(data))}
	if s.agentFileOps != nil {
		result, err = s.agentFileOps.WriteAgentFileBytesDetailed(ctx, serverID, path, data, 10*time.Minute)
	} else {
		var written int64
		written, err = s.agentFiles.WriteAgentFileBytes(ctx, serverID, path, data, 10*time.Minute)
		result.Size = written
	}
	if err != nil {
		return nil, fmt.Errorf("写入主机文件失败: %w", err)
	}
	if result.Size <= 0 {
		result.Size = int64(len(data))
	}
	return map[string]interface{}{
		"success":   true,
		"transport": "agent-direct",
		"serverId":  serverID,
		"path":      path,
		"realPath":  result.RealPath,
		"size":      result.Size,
		"sha256":    sha256Hex(data),
		"note":      agentFilePathNote(result.RealPath),
	}, nil
}

// agentFilePathNote 统一说明「虚拟路径 vs 真实路径」，避免上传成功但命令找不到文件。
func agentFilePathNote(realPath string) string {
	if strings.TrimSpace(realPath) == "" {
		return "已在主机落盘。注意：面板返回的 path 是文件沙箱虚拟路径，agent_run_command 执行在真实文件系统上，" +
			"请先调 agent_file_stat 取 realPath 再用它执行；旧版 Agent 不返回 realPath，可调 agent_file_roots 查沙箱根拼接真实路径。"
	}
	return "已在主机落盘。realPath 是真实绝对路径，agent_run_command 里请直接用它执行（path 字段只是面板虚拟路径）。"
}

// publicBaseURL 返回面板的公网基址，用于在错误信息里拼可复制的直传命令。
// 读不到配置时返回空串——调用方会得到「%/api/...」这种相对提示，仍可定位端点。
func (s *Service) publicBaseURL() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		return ""
	}
	defer db.Close()
	var value string
	if err := db.QueryRowContext(ctx,
		"SELECT value FROM system_config WHERE key = 'public_api_url'").Scan(&value); err != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

// sha256Hex 计算字节的 sha256 十六进制串。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// requireAgentFileWrite 复用与 call_api 一致的写门槛（minimal 只读拒绝写操作）。
func (s *Service) requireAgentFileWrite(ctx context.Context) error {
	policy, err := s.AIAgentAccessPolicy(ctx)
	if err != nil {
		return err
	}
	if policy == AIAccessPolicyMinimal {
		return fmt.Errorf("AI 接入处于只读模式（minimal），写操作已禁用")
	}
	return nil
}

// resolveFileTarget 解析文件工具的目标主机与路径，serverId 可用 serverName/host 代替。
func (s *Service) resolveFileTarget(args map[string]interface{}) (serverID, path string, err error) {
	rawServerID, _ := args["serverId"].(string)
	serverName, _ := args["serverName"].(string)
	host, _ := args["host"].(string)
	path, _ = args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("path 为必填（主机上的目标路径）")
	}
	serverID, err = s.resolveServerID(rawServerID, serverName, host)
	if err != nil {
		return "", "", err
	}
	return serverID, strings.TrimSpace(path), nil
}

// stringArg 读取字符串参数（缺省返回空串）。
func stringArg(args map[string]interface{}, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

// agentUploadChunk 是 agent_upload_chunk 元工具的实现：按 offset 逐块上传。
//
// 这是「大文件但要经过 MCP」时的正解：客户端自己读文件切片、自己算 sha256，
// 模型只传 offset/长度这类元数据，因此不受模型单次输出长度限制，也不会被
// 复述改写。offset=0 的块会截断目标文件（覆盖写）。
func (s *Service) agentUploadChunk(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, path, err := s.resolveFileTarget(args)
	if err != nil {
		return nil, err
	}
	if s.agentFileOps == nil {
		return nil, fmt.Errorf("分块上传能力未配置")
	}
	if err := s.requireAgentFileWrite(ctx); err != nil {
		return nil, err
	}
	contentB64, _ := args["content_base64"].(string)
	if strings.TrimSpace(contentB64) == "" {
		return nil, fmt.Errorf("content_base64 为必填（本块字节的 base64）")
	}
	if len(contentB64) > maxInlineBase64Bytes*4/3+8 {
		return nil, fmt.Errorf("单块 base64 过大（解码后上限 %d KB），请缩小 chunkSize", maxInlineBase64Bytes/1024)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(contentB64))
	if err != nil {
		return nil, fmt.Errorf("content_base64 不是合法 base64: %w", err)
	}
	if want := strings.TrimSpace(stringArg(args, "sha256")); want != "" {
		got := sha256Hex(data)
		if !strings.EqualFold(got, want) {
			return nil, fmt.Errorf("本块 sha256 校验失败：期望 %s，实际 %s", want, got)
		}
	}
	// offset 必须显式给出且合法：缺省按 0 处理会带 truncate=true，
	// 等于把调用方本意的「续写」静默变成「覆盖」，是真实的数据丢失路径。
	rawOffset, hasOffset := args["offset"]
	if !hasOffset {
		return nil, fmt.Errorf("offset 为必填（首块传 0，续写传目标文件当前字节数）")
	}
	offsetFloat, ok := rawOffset.(float64)
	if !ok || offsetFloat < 0 {
		return nil, fmt.Errorf("offset 必须是非负整数")
	}
	offset := int64(offsetFloat)
	if offset > 0 {
		// 续写前确认目标已有内容，避免块序错乱写进空文件。
		stat, statErr := s.agentFileOps.AgentFileStat(ctx, serverID, path)
		if statErr != nil {
			return nil, fmt.Errorf("读取目标文件状态失败（续写需要文件已存在）: %w", statErr)
		}
		existing, _ := stat["size"].(float64)
		if int64(existing) != offset {
			return nil, fmt.Errorf("offset 不连续：目标文件当前 %d 字节，本块 offset=%d。请按实际长度重传该块", int64(existing), offset)
		}
	}
	result, err := s.agentFileOps.WriteAgentFileChunk(ctx, serverID, path, data, offset, offset == 0, 2*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("写入分块失败: %w", err)
	}
	return map[string]interface{}{
		"success":   true,
		"transport": "agent-chunk",
		"serverId":  serverID,
		"path":      path,
		"realPath":  result.RealPath,
		"offset":    offset,
		"written":   len(data),
		"size":      result.Size,
		"sha256":    sha256Hex(data),
		"note":      agentFilePathNote(result.RealPath),
	}, nil
}

// agentFileRoots 是 agent_file_roots 元工具的实现：返回文件沙箱的真实根目录。
// 没有它，调用方无从知道「/opt/a.sh」最终落到哪，只能靠试错。
func (s *Service) agentFileRoots(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	rawServerID, _ := args["serverId"].(string)
	serverName, _ := args["serverName"].(string)
	host, _ := args["host"].(string)
	serverID, err := s.resolveServerID(rawServerID, serverName, host)
	if err != nil {
		return nil, err
	}
	if s.agentFileOps == nil {
		return nil, fmt.Errorf("文件沙箱查询能力未配置")
	}
	roots, err := s.agentFileOps.AgentFileRoots(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("查询文件沙箱失败: %w", err)
	}
	roots["serverId"] = serverID
	return roots, nil
}

// agentFileStat 是 agent_file_stat 元工具的实现：查文件是否存在、多大、真实路径。
func (s *Service) agentFileStat(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, path, err := s.resolveFileTarget(args)
	if err != nil {
		return nil, err
	}
	if s.agentFileOps == nil {
		return nil, fmt.Errorf("文件查询能力未配置")
	}
	stat, err := s.agentFileOps.AgentFileStat(ctx, serverID, path)
	if err != nil {
		return nil, fmt.Errorf("查询主机文件失败: %w", err)
	}
	stat["serverId"] = serverID
	return stat, nil
}

// agentFileChmod 是 agent_file_chmod 元工具的实现：让上传的脚本变成可执行。
func (s *Service) agentFileChmod(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	serverID, path, err := s.resolveFileTarget(args)
	if err != nil {
		return nil, err
	}
	if s.agentFileOps == nil {
		return nil, fmt.Errorf("修改权限能力未配置")
	}
	if err := s.requireAgentFileWrite(ctx); err != nil {
		return nil, err
	}
	mode := int64(0o755)
	if raw := stringArg(args, "mode"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 8, 32)
		if parseErr != nil {
			return nil, fmt.Errorf("mode 必须是八进制字符串，如 \"755\" 或 \"644\"")
		}
		mode = parsed
	}
	message, err := s.agentFileOps.AgentFileChmod(ctx, serverID, path, uint32(mode))
	if err != nil {
		return nil, fmt.Errorf("修改文件权限失败: %w", err)
	}
	return map[string]interface{}{
		"success":  true,
		"serverId": serverID,
		"path":     path,
		"mode":     fmt.Sprintf("%o", mode),
		"message":  message,
		"note":     "权限已修改。若该路径在沙箱内，执行时请用 realPath（见 agent_file_stat）。",
	}, nil
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
		"note":   "run_batch 逐个复用 call_api 的鉴权与权限模式约束；写操作是否允许由 AI 接入权限模式决定。",
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
		{"name": "filebox_share_from_agent", "description": "把一台 Agent 主机上的文件转成文件柜分享，返回可直接下载的分享链接与直链。这是把「某台主机上的文件给别人下载」的标准工具，无需先 list_apis/find_api。storageTarget=auto（默认）时优先把文件放到公网存储节点并让源主机直传（面板零流量），无可用节点或旧版 Agent 时自动回退为主站本地/面板中转。写操作，需 AI 接入权限模式非 minimal。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（先调 /api/server/accounts 获取真实 ID）"}, "remotePath": map[string]interface{}{"type": "string", "description": "主机上的文件路径（受 Agent 文件沙箱约束：默认根或 API_MONITOR_FILE_ROOTS 白名单内）"}, "storageTarget": map[string]interface{}{"type": "string", "description": "auto（默认，优先可用存储节点并让源主机直传）/ local（主站本地）/ 指定存储节点 serverId"}, "expiry": map[string]interface{}{"type": "string", "description": "有效期小时数，如 24；不传用文件柜默认值"}, "burn_after_reading": map[string]interface{}{"type": "boolean", "description": "阅后即焚（下载一次后删除）"}, "max_downloads": map[string]interface{}{"type": "string", "description": "最大下载次数，0 或不传为不限"}, "access_password": map[string]interface{}{"type": "string", "description": "分享访问密码（可选）"}}, "required": []string{"serverId", "remotePath"}}},
		{"name": "agent_run_command", "description": "在指定 Agent 主机上执行一条 shell 命令并返回 stdout/stderr 输出，无需登录 SSH。这是「在某台主机上跑命令」的标准工具，无需先 list_apis/find_api。主机用 serverId 指定；不确定 ID 时可用 serverName 或 host 代替（面板会自动解析，命中多台会返回候选列表）。也可调 /api/aiagent/servers 取轻量主机列表（只含 id/name/host/online，比 /api/server/accounts 省很多）。仅对已安装 Agent 且在线的主机有效。返回 success=true 表示命令已执行完成（含非零退出码，如 grep 无匹配、test/diff 非零，此时 exitCode 字段给出退出码，输出照常返回），仅 spawn/超时等硬错误才 success=false。输出上限约 256KB（保留首尾，中间省略并标注），需要更多请用 grep/head/tail 缩小或重定向到文件。长任务（apt upgrade、编译、docker pull）请传 wait=false：立即返回 task_id，再用 agent_task_result 取终态，避免同步等待被超时上限掐断。危险命令（rm -rf、dd、mkfs、shutdown/reboot、docker rm/prune、kubectl delete、DROP DATABASE/TABLE、Remove-Item -Recurse 等）会被 commandguard 拦截：standard 模式硬拦截，sensitive 模式返回 requires_confirmation 提示（确认后带 confirm=true 重新调用即可执行），full 模式直接放行。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代，按名称精确匹配）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址/IP（serverId 的替代，按 host 精确匹配）"}, "command": map[string]interface{}{"type": "string", "description": "要执行的 shell 命令（Linux 走 sh，Windows 走 cmd /C；PowerShell 会自动改写为 -EncodedCommand）"}, "cwd": map[string]interface{}{"type": "string", "description": "工作目录（可选）。省去每条命令都写 cd /x && ..."}, "timeout": map[string]interface{}{"type": "integer", "description": "超时秒数，默认 30，上限 300"}, "wait": map[string]interface{}{"type": "boolean", "description": "默认 true 同步等待结果；false 立即返回 task_id（长任务用，之后用 agent_task_result 查询）"}, "confirm": map[string]interface{}{"type": "boolean", "description": "危险命令二次确认：敏感模式下首次返回 requires_confirmation，确认后带 confirm=true 重试"}}, "required": []string{"command"}}},
		{"name": "agent_task_result", "description": "查询异步命令任务的终态。配合 agent_run_command 的 wait=false 使用：先用 wait=false 拿到 task_id，稍后用本工具查询。返回 status（running/completed/failed）、output（命令输出）与 exit_code；任务仍在执行时 status=running，稍后再查一次即可。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"taskId": map[string]interface{}{"type": "string", "description": "任务 ID（agent_run_command 传 wait=false 时的返回）"}}, "required": []string{"taskId"}}},
		{"name": "agent_fetch_url", "description": "让指定 Agent 主机直接从 URL 拉取文件并写入其本地路径（字节由主机直接下载，不经面板中转，无 base64 与 1MB 请求体限制）。适用于把已有的可访问地址（文件柜直链、GitHub release、软件包等）落到主机上，避免用 base64 经 call_api 传输大文件。写操作，需 AI 接入权限模式非 minimal；目标路径受 Agent 文件沙箱约束（默认根或 API_MONITOR_FILE_ROOTS 白名单内）。仅对已安装 Agent 且在线的主机有效（先调 /api/server/accounts 取 serverId 并确认 agent_online=true）。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（先调 /api/server/accounts 获取真实 ID 并确认在线）"}, "url": map[string]interface{}{"type": "string", "description": "文件下载地址（http/https，主机需可访问）"}, "path": map[string]interface{}{"type": "string", "description": "主机上的目标文件路径（受 Agent 文件沙箱约束）"}}, "required": []string{"serverId", "url", "path"}}},
		{"name": "agent_transfer_file", "description": "把一台 Agent 主机上的文件搬到另一台 Agent 主机（主机↔主机直传，字节由源主机直传存储节点、目标主机直链拉取，全程不经 AI、不经面板中转）。【传输大文件（如 5~30MB 脚本）到主机的首选方式，绝不要用 call_api + base64 分块】。本机若已注册为 Agent 主机（在 /api/server/accounts 中可查到），先在本机把文件放到 Agent 沙箱路径，再用本工具搬运。写操作，需 AI 接入权限模式非 minimal；源与目标路径均受 Agent 文件沙箱约束。仅对两端都已安装 Agent 且在线的主机有效。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sourceServerId": map[string]interface{}{"type": "string", "description": "源 Agent 主机 ID（文件所在主机，先调 /api/server/accounts 确认在线）"}, "sourcePath": map[string]interface{}{"type": "string", "description": "源主机上的文件路径（受 Agent 文件沙箱约束）"}, "targetServerId": map[string]interface{}{"type": "string", "description": "目标 Agent 主机 ID（先调 /api/server/accounts 确认在线）"}, "targetPath": map[string]interface{}{"type": "string", "description": "目标主机上的落盘路径（受 Agent 文件沙箱约束）"}, "storageTarget": map[string]interface{}{"type": "string", "description": "可选，中转存储目标：auto（默认，优先可用存储节点）/ local / 指定存储节点 serverId"}}, "required": []string{"sourceServerId", "sourcePath", "targetServerId", "targetPath"}}},
		{"name": "agent_upload_file", "description": "【小文件兜底，上限 256KB】把 base64 内容一次性直传到指定 Agent 主机。仅当文件很小（脚本、配置、证书等）且无法用其他通道时使用——字节必须由模型逐 token 生成，长 base64 极易被静默改写，因此超过 256KB 会被直接拒绝并给出替代命令。优先顺序：① 宿主能执行 shell → 用 curl 直传 POST /api/server/agent/upload（字节不进模型，见 get_ai_manifest 的接入指南）；② 文件已在某台 Agent 主机上 → agent_transfer_file；③ 文件有可访问 URL → agent_fetch_url；④ 客户端能自己读文件 → agent_upload_chunk（按 offset 分块，模型只传元数据）。返回 realPath 为真实绝对路径，agent_run_command 里必须用它（path 只是面板虚拟路径）。写操作，需权限模式非 minimal。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "目标 Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址（serverId 的替代）"}, "path": map[string]interface{}{"type": "string", "description": "目标主机上的落盘路径（受 Agent 文件沙箱约束；用 agent_file_roots 查真实根）"}, "content_base64": map[string]interface{}{"type": "string", "description": "文件的 base64 标准编码（含 padding），解码后不得超过 256KB"}, "sha256": map[string]interface{}{"type": "string", "description": "可选，原始字节的 sha256 十六进制；传了就会校验，不一致直接报错"}}, "required": []string{"path", "content_base64"}}},
		{"name": "agent_upload_chunk", "description": "【大文件正解】按 offset 分块把字节写到指定 Agent 主机。客户端自己读文件切片、自己算 sha256，模型只传 offset 这类元数据，因此不受模型输出长度限制，也不会被复述改写。用法：先 offset=0 传第一块（会自动截断/覆盖目标文件），之后每块 offset 必须等于目标文件当前大小；续写时面板会 stat 校验连续性，错序直接报错。返回 realPath，agent_run_command 里用它执行。写操作，需权限模式非 minimal。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "目标 Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址（serverId 的替代）"}, "path": map[string]interface{}{"type": "string", "description": "目标主机上的落盘路径"}, "offset": map[string]interface{}{"type": "integer", "description": "本块写入起始偏移，首块为 0"}, "content_base64": map[string]interface{}{"type": "string", "description": "本块字节的 base64（单块解码后不超过 256KB）"}, "sha256": map[string]interface{}{"type": "string", "description": "可选，本块原始字节的 sha256；传了就校验"}}, "required": []string{"path", "offset", "content_base64"}}},
		{"name": "agent_file_roots", "description": "查询指定 Agent 主机的文件沙箱真实根目录与白名单根（API_MONITOR_FILE_ROOTS）。上传文件前先调它，才能知道「/opt/a.sh」这类路径最终落到哪。关键语义：默认沙箱下的绝对路径会被锚定到 sandboxRoot 内（即 /opt/a.sh 实际落盘 sandboxRoot/opt/a.sh），只有 allowedRoots 内的路径才按真实绝对路径访问；而 agent_run_command 执行在真实文件系统上，必须使用文件工具返回的 realPath。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址（serverId 的替代）"}}}},
		{"name": "agent_file_stat", "description": "查询指定 Agent 主机上某个路径的信息：是否存在、大小、权限、真实绝对路径（realPath）。上传后不确定文件落在哪、或需要确认 offset 续写位置时使用。返回 realPath 可直接用于 agent_run_command。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址（serverId 的替代）"}, "path": map[string]interface{}{"type": "string", "description": "主机上的路径（沙箱虚拟路径或真实绝对路径）"}}, "required": []string{"path"}}},
		{"name": "agent_file_chmod", "description": "修改指定 Agent 主机上文件的权限（八进制，默认 755），用于让上传的脚本可直接执行。Windows 主机不支持。写操作，需权限模式非 minimal。", "inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"serverId": map[string]interface{}{"type": "string", "description": "Agent 主机 ID（可省略，改用 serverName/host）"}, "serverName": map[string]interface{}{"type": "string", "description": "主机名称（serverId 的替代）"}, "host": map[string]interface{}{"type": "string", "description": "主机地址（serverId 的替代）"}, "path": map[string]interface{}{"type": "string", "description": "主机上的文件路径"}, "mode": map[string]interface{}{"type": "string", "description": "八进制权限，如 755 / 644；默认 755"}}, "required": []string{"path"}}},
	}
}

func (s *Service) aiAccessGuide(mcpURL, manifestURL, openAPIURL, baseURL, key string, tools []map[string]interface{}) string {
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
- 权限：由「API 文档 → AI 接入」的权限模式决定（minimal 只读 / sensitive 敏感 / standard 标准 / full 全部权限）

## 端点
- 能力清单（manifest，GET）：%s
- MCP 服务：%s
- OpenAPI 文档：%s（需登录会话，也可用 MCP 工具 get_openapi 获取）

## 文件传输到主机（按「字节是否经过模型」分档，务必按顺序选）

**原则：绝不要让模型复述文件内容。** base64 由模型逐 token 生成时既慢又不可靠（长 base64
会被静默改写），因此只有小文件才允许走内容参数。按下面顺序选通道：

1. **宿主能执行 shell（首选）**：直接把字节 POST 给面板，全程不进模型。
   支持 multipart 直传、2GB 上限、Agent Key 鉴权、面板按分块下发给主机 Agent：
   ~~~
   curl -H "Authorization: Bearer <agentKey>" \
        -F serverId=<serverId> -F path=/opt/x/app.bin -F file=@./app.bin \
        %sapi/server/agent/upload
   ~~~
   该端点无 base64 与 1MB JSON 请求体限制，也不受 AI 接入权限模式（minimal 等）约束。
2. **文件已在某台 Agent 主机上**：用 MCP 工具 agent_transfer_file，字节由源主机直传
   存储节点、目标主机直链拉取，全程不经 AI 与面板中转。大文件（5~30MB 及以上）首选。
3. **文件有可访问 URL**（文件柜直链、GitHub release、软件包）：用 agent_fetch_url，
   让主机自己下载。
4. **客户端能自己读文件、但不方便跑 shell**：用 agent_upload_chunk 按 offset 分块上传，
   模型只传 offset/sha256 这类元数据，不碰字节内容。
5. **只有极小文件**（脚本、配置、证书，≤256KB）：agent_upload_file 传 base64。
   超过 256KB 会被直接拒绝并给出上面的替代命令。

**路径必须对齐（最常见的失败）**：文件工具受 Agent 沙箱约束，默认沙箱下传 /opt/a.sh
实际落在 <sandboxRoot>/opt/a.sh，而 agent_run_command 执行在真实文件系统上。
因此：上传后一律使用返回的 **realPath** 执行；不确定时先调 agent_file_roots 查
sandboxRoot/allowedRoots，或用 agent_file_stat 取 realPath。需要脚本可执行时用
agent_file_chmod（默认 755），不要靠再跑一条 chmod 命令绕路。


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
5. 写操作（POST/PUT/PATCH/DELETE）是否允许由权限模式决定：minimal 只读时写操作会被拒绝；sensitive/standard/full 允许写入。危险命令在 standard 下硬拦截、sensitive 下需二次确认、full 下放行。
6. 密钥可随时在「API 文档 → AI 接入」页面轮换；请勿将密钥写入公开仓库。

## 强制验证规则（不可省略）
1. 写操作（POST/PUT/PATCH/DELETE）调用返回后，必须立即回读验证真实生效：用 GET 列表/详情接口确认目标资源已存在且状态正确（如 enabled、next_run 等），仅凭创建接口自身返回 2xx 不算完成。
2. 每次调用都必须检查响应：HTTP 非 2xx、或 body 中 success=false、或 error 字段非空，均视为失败；任一失败出现时，禁止向用户宣称任务完成，必须如实报告错误。
3. 宁可多一次回读调用，也不要在未验证生效前宣告成功；无法验证时如实说明「未能验证」。
`, s.cfg.Version, key, key, manifestURL, mcpURL, openAPIURL, baseURL, mcpURL, key, mcpURL, key, toolsText)
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
