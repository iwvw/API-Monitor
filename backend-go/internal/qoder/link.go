package qoder

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
const linkedEndpointID = "qoder-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "Qoder"

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

	models := s.allModelIDs()
	modelsJSON, _ := json.Marshal(models)
	disabled := s.Settings().DisabledModels
	if disabled == nil {
		disabled = []string{}
	}
	disabledJSON, _ := json.Marshal(disabled)
	namesJSON, _ := json.Marshal(s.modelNameMap())
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, model_names, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, ?, ?, ?, '[]', ?, '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, ?, ?, ?, 100, 'qoder')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			models = excluded.models,
			disabled_models = excluded.disabled_models,
			model_names = excluded.model_names,
			enabled = 1,
			plugin_id = excluded.plugin_id`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey, string(disabledJSON), string(modelsJSON), string(namesJSON), now, now)
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

// unlinkIfDisabled 供保存设置联动：插件被关闭时移除已接入的网关端点行。
func (s *Service) unlinkIfDisabled(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `DELETE FROM openai_endpoints WHERE id = ?`, linkedEndpointID)
}

// syncLinkedEndpointModels 把插件当前启用模型名单同步到已接入网关端点的
// models 列，并同时刷新 disabled_models。未接入时静默跳过。
//
// 为什么必须做：端点 models 列只在 linkCreate 时写过一次；若插件目录后来变化
// （例如模型表从 2 个扩到 14 个），历史行会停留在旧值，前端端点模型列表随之
// 显示过期内容。启动对账与保存设置时都应调用。
func (s *Service) syncLinkedEndpointModels(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	disabled := s.Settings().DisabledModels
	if disabled == nil {
		disabled = []string{}
	}
	disabledPayload, _ := json.Marshal(disabled)
	modelsPayload, _ := json.Marshal(s.enabledModelIDs())
	// model_names：模型 id → 可读名称（如 dfmodel → DeepSeek-Flash），供网关端点
	// 面板展示，不参与路由。前缀会一并加在 key 上以对齐端点 models 列。
	namesPayload, _ := json.Marshal(s.modelNameMap())
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, disabled_models = ?, model_names = ? WHERE id = ?`,
		string(modelsPayload), string(disabledPayload), string(namesPayload), linkedEndpointID)
}

// modelNameMap 返回「对外模型名 → 可读名称」映射（带前缀，与端点 models 列口径一致）。
func (s *Service) modelNameMap() map[string]string {
	out := map[string]string{}
	for _, m := range s.catalog() {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		out[s.prefixModel(m.ID)] = m.Name
	}
	return out
}

// ReconcileLinkedEndpoint 在启动时按当前启用名单对账已接入端点的 models 列。
// 只读静态目录快照，不回源；未接入时静默跳过。
func (s *Service) ReconcileLinkedEndpoint(ctx context.Context) {
	s.syncLinkedEndpointModels(ctx)
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
			model_names TEXT,
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
