package ds2api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
const linkedEndpointID = "ds2api-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "DS2API"

// handleLink 处理插件与模型网关端点的接入/断开/状态。
func (s *Service) handleLink(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.linkStatus(w, r)
	case http.MethodPost:
		s.linkCreate(w, r)
	case http.MethodDelete:
		s.linkDelete(w, r)
	default:
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
	}
}

// linkBaseURL 构造 loopback 基址。
func (s *Service) linkBaseURL() string {
	port := s.cfg.Port
	if port <= 0 {
		port = 3000
	}
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/api/ds2api/v1"
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

func (s *Service) linkStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()

	status := s.readLinkedEndpoint(ctx, db)
	if status == nil {
		responseJSON(w, map[string]interface{}{
			"linked":     false,
			"baseUrl":    s.linkBaseURL(),
			"endpointId": linkedEndpointID,
		})
		return
	}
	responseJSON(w, map[string]interface{}{
		"linked":     status.enabled == 1,
		"baseUrl":    status.baseURL,
		"endpointId": status.id,
		"name":       status.name,
		"models":     status.models,
	})
}

func (s *Service) linkCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()

	if err := ensureOpenAIEndpointsTable(ctx, db); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	models := s.prefixModelNames(s.engineModelNames(ctx))
	modelsJSON, _ := json.Marshal(models)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, ?, ?, ?, '[]', '[]', '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, ?, ?, 100, 'ds2api')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			models = excluded.models,
			enabled = 1,
			plugin_id = excluded.plugin_id`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey, string(modelsJSON), now, now)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	responseJSON(w, map[string]interface{}{
		"success":    true,
		"linked":     true,
		"endpointId": linkedEndpointID,
		"baseUrl":    s.linkBaseURL(),
		"models":     models,
	})
}

func (s *Service) linkDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `DELETE FROM openai_endpoints WHERE id = ?`, linkedEndpointID); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "linked": false})
}

// engineModelNames 从引擎配置解析支持的模型名列表。
func (s *Service) engineModelNames(ctx context.Context) []string {
	store, err := s.loadEngineStore()
	if err != nil {
		return []string{"deepseek-v4-flash", "deepseek-v4-pro"}
	}
	return engineSupportedModels(store)
}

// ensureOpenAIEndpointsTable 幂等确保 openai_endpoints 表存在。
func ensureOpenAIEndpointsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS openai_endpoints (
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
			models_url TEXT,
			pricing TEXT,
			proxy_pool_id TEXT,
			plugin_id TEXT
		)`)
	return err
}
