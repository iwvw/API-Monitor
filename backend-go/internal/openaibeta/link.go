package openaibeta

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/config"
)

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
// 端点 base_url 指向本机 loopback 的 /api/openaibeta/v1：网关转发到该路径时，
// 因来源是 loopback，AuthorizeGatewayRequest 以 internal 身份放行，最终落到内嵌引擎。
// 该端点不配代理池（出网 Google 的代理由插件自身 ManualProxies/ProxyEndpointID 控制）。
const linkedEndpointID = "openaibeta-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "Vertex to API（免费 Gemini 中继）"

// linkBaseURL 构造 loopback 基址。端口取自进程监听端口（默认 3000，Fly 上同为 3000）。
func (s *Service) linkBaseURL() string {
	port := s.cfg.Port
	if port <= 0 {
		port = 3000
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/openaibeta/v1"
}

// handleLink 处理插件与模型网关端点的接入/断开/状态：
//   - GET  ：返回当前是否已接入（端点是否存在、是否启用）
//   - POST ：创建/更新 openai_endpoints 记录（幂等），模型同步插件启用列表
//   - DELETE：删除该端点记录
func (s *Service) handleLink(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.linkStatus(w, r)
	case http.MethodPost:
		s.linkCreate(w, r)
	case http.MethodDelete:
		s.linkDelete(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) linkStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	status := s.readLinkedEndpoint(ctx, db)
	if status == nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"linked": false,
			"baseUrl": s.linkBaseURL(),
			"endpointId": linkedEndpointID,
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"linked":     status.enabled == 1,
		"baseUrl":    status.baseURL,
		"endpointId": status.id,
		"name":       status.name,
		"models":     status.models,
	})
}

type linkedEndpointInfo struct {
	id      string
	name    string
	baseURL string
	enabled int
	models  []string
}

func (s *Service) readLinkedEndpoint(ctx context.Context, db *sql.DB) *linkedEndpointInfo {
	var id, name, baseURL string
	var enabled int
	var modelsRaw sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, name, base_url, enabled, COALESCE(models,'')
		FROM openai_endpoints WHERE id = ?`, linkedEndpointID).Scan(&id, &name, &baseURL, &enabled, &modelsRaw)
	if err != nil {
		return nil
	}
	info := &linkedEndpointInfo{id: id, name: name, baseURL: baseURL, enabled: enabled}
	if modelsRaw.Valid && modelsRaw.String != "" {
		_ = json.Unmarshal([]byte(modelsRaw.String), &info.models)
	}
	return info
}

func (s *Service) linkCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	// openai_endpoints 表由 openai 模块在启动时创建；此处幂等确保表存在，
	// 让插件可独立工作（openai 已建则 IF NOT EXISTS 直接跳过）。
	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	models := enabledModelIDs(s.Settings())
	modelsJSON, _ := json.Marshal(models)
	now := time.Now().UTC().Format(time.RFC3339)

	// 幂等 upsert：存在则更新模型/启用/基址，不存在则插入。
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, created_at, last_checked, sort_order)
		VALUES (?, ?, ?, '', '[]', '[]', '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, ?, ?, 100)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			models = excluded.models,
			enabled = 1`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), string(modelsJSON), now, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"linked":  true,
		"endpointId": linkedEndpointID,
		"baseUrl":    s.linkBaseURL(),
		"models":     models,
	})
}

func (s *Service) linkDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `DELETE FROM openai_endpoints WHERE id = ?`, linkedEndpointID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "linked": false})
}

// enabledModelIDs 返回当前启用模型的 ID 列表。
func enabledModelIDs(st Settings) []string {
	if len(st.Models) == 0 {
		return engineconfig.BaseModels()
	}
	out := make([]string, 0, len(st.Models))
	for _, m := range st.Models {
		if m.Enabled {
			out = append(out, m.ID)
		}
	}
	return out
}

// ensureOpenAIEndpointsTable 幂等确保 openai_endpoints 表存在（列定义与 openai 模块一致）。
func ensureOpenAIEndpointsTable(ctx context.Context, db *sql.DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS openai_endpoints (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key TEXT NOT NULL,
		headers TEXT,
		disabled_models TEXT,
		proxy_pool TEXT,
		proxy_batches TEXT,
		auto_switch INTEGER DEFAULT 0,
		proxy_enabled INTEGER DEFAULT 0,
		force_proxy INTEGER DEFAULT 0,
		rate_limit_retry_enabled INTEGER DEFAULT 1,
		rate_limit_retry_wait_seconds INTEGER DEFAULT 10,
		status TEXT DEFAULT 'unknown',
		enabled INTEGER DEFAULT 1,
		models TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used DATETIME,
		last_checked DATETIME,
		sort_order INTEGER DEFAULT 0,
		priority INTEGER DEFAULT 0,
		weight INTEGER DEFAULT 100,
		model_mappings TEXT,
		protocol TEXT,
		api_keys TEXT
	)`
	_, err := db.ExecContext(ctx, stmt)
	return err
}
