package workbuddy

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// linkedEndpointID 是插件接入模型网关端点列表时的固定端点 ID。
const linkedEndpointID = "workbuddy-internal"

// linkedEndpointName 是端点在模型网关端点列表里展示的名称。
const linkedEndpointName = "WorkBuddy"

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

	models := s.enabledModelIDsFrom(s.allModelIDs(ctx))
	modelsJSON, _ := json.Marshal(models)
	disabled := s.Settings().DisabledModels
	if disabled == nil {
		disabled = []string{}
	}
	disabledJSON, _ := json.Marshal(disabled)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints
			(id, name, base_url, api_key, headers, disabled_models, proxy_pool, proxy_batches,
			 auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled,
			 rate_limit_retry_wait_seconds, protocol, status, enabled, models, created_at, last_checked, sort_order, plugin_id)
		VALUES (?, ?, ?, ?, '[]', ?, '[]', '[]', 0, 0, 0, 1, 10, 'auto', 'unknown', 1, ?, ?, ?, 100, 'workbuddy')
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			models = excluded.models,
			disabled_models = excluded.disabled_models,
			enabled = 1,
			plugin_id = excluded.plugin_id`,
		linkedEndpointID, linkedEndpointName, s.linkBaseURL(), internalKey, string(disabledJSON), string(modelsJSON), now, now)
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

// unlinkIfDisabled 供保存设置联动：插件被关闭（enabled=false）时移除
// 已接入的网关端点行，避免端点仍暴露在列表。失败静默，不影响保存结果。
func (s *Service) unlinkIfDisabled(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `DELETE FROM openai_endpoints WHERE id = ?`, linkedEndpointID)
}

// syncLinkedEndpointModels 把插件内的模型启停状态同步到已接入网关端点的
// models 与 disabled_models 两列。
//
// 为什么必须同步：网关是按 disabled_models 这一列做请求拦截的 ——
// `internal/openai/relay.go` 的 resolveEndpointModel → isModelDisabled(ep.DisabledModels, requested)。
// 因此只写插件自己的设置，网关仍会把停用模型的请求路由过来。
//
// models 列同时收敛为「启用项」，与中继面 /v1/models 的输出口径一致：
// 该列由 refreshAllModels 用上游返回覆盖，而 WorkBuddy 中继只吐启用模型。
// 若此处不写，首次加载读到的是 linkCreate 写入的全量目录，开关过的模型会
// 全部显示，直到下一次刷新才被上游结果纠正。
//
// 名称用**对外（带前缀）**模型名，与网关收到的 requested 同一命名空间；
// 未接入时静默跳过，不影响设置保存。
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

	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, disabled_models = ? WHERE id = ?`,
		string(modelsPayload), string(disabledPayload), linkedEndpointID)
}

// ReconcileLinkedEndpoint 在启动时修正已接入端点的 models 列。
//
// 背景：旧版本在这里只写 disabled_models，models 列停留在 linkCreate 写入的
// 全量目录；只有等 refreshAllModels 从上游拉一次才收敛。升级后历史行仍是旧值，
// 因此启动时按当前启用名单对账一次，避免用户仍看到「停用模型全量显示」。
//
// 目录缓存为空时回源一次拿全量；回源失败则跳过（不误清空端点列）。
// 未接入时静默跳过。
func (s *Service) ReconcileLinkedEndpoint(ctx context.Context) {
	models := s.catalog(ctx)
	if len(models) == 0 {
		return
	}
	s.modelMu.Lock()
	if len(s.modelCache) == 0 {
		s.modelCache = models
		s.modelCacheAt = time.Now()
	}
	s.modelMu.Unlock()
	s.syncLinkedEndpointModels(ctx)
}

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表。
// 与中继面 /v1/models 的输出口径一致，作为端点 models 列的权威值。
//
// 只读缓存快照（不走 catalog 回源）：调用方在 SaveSettings 路径上，
// 回源会向上游发起请求并污染测试/运行时行为，且保存设置不应依赖上游可用性。
func (s *Service) enabledModelIDs() []string {
	return s.enabledModelIDsFrom(s.prefixModelNames(s.modelIDsFromCache()))
}

// enabledModelIDsFrom 从给定（已带前缀）名单里剔除停用项。
func (s *Service) enabledModelIDsFrom(ids []string) []string {
	disabled := s.disabledSet()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !disabled[id] {
			out = append(out, id)
		}
	}
	return out
}

// ensureOpenAIEndpointsTable 幂等确保 openai_endpoints 表存在
// （插件可能先于模型网关模块初始化而被访问）。
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
