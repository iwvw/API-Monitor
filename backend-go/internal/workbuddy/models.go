package workbuddy

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// modelsInternal 返回当前模型目录快照（未加对外前缀）。
// 只读缓存，不回源；目录由 refreshCatalog 负责刷新。
func (s *Service) modelsInternal() []ModelInfo {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	return append([]ModelInfo(nil), s.modelCache...)
}

// catalog 返回可用的模型目录：缓存新鲜时直接用缓存；过期时回源刷新。
// 回源失败时返回上一份快照而不是空列表，避免上游抖动让 /v1/models 变空；
// 首次拉取成功前返回空列表（这是合法状态，不是错误）。
func (s *Service) catalog(ctx context.Context) []ModelInfo {
	s.modelMu.Lock()
	if len(s.modelCache) > 0 && time.Since(s.modelCacheAt) < modelCacheTTL {
		cached := append([]ModelInfo(nil), s.modelCache...)
		s.modelMu.Unlock()
		return cached
	}
	s.modelMu.Unlock()

	models, err := s.fetchCatalog(ctx)
	if err != nil {
		return s.modelsInternal()
	}
	s.modelMu.Lock()
	s.modelCache = models
	s.modelCacheAt = time.Now()
	s.modelMu.Unlock()
	// 目录刷新后同步到已接入网关端点的 models 列。目录是动态的，
	// 只靠「接入那一刻」写一次的话，先接入后拉目录的场景会长期停在空列表。
	go s.writeLinkedEndpointModels(context.Background(), s.prefixModelNames(modelIDsOf(models)))
	return models
}

// modelIDsOf 取出目录里的原始（未加前缀）模型 ID。
func modelIDsOf(models []ModelInfo) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

// writeLinkedEndpointModels 只更新已接入网关端点的 models 列（不动映射/禁用）。
// 未接入时静默跳过。
func (s *Service) writeLinkedEndpointModels(ctx context.Context, names []string) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	payload, _ := json.Marshal(names)
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, last_checked = ? WHERE id = ?`,
		string(payload), time.Now().UTC().Format(time.RFC3339), linkedEndpointID)
}

// allModelIDs 返回对外（带前缀）的模型 ID 列表。
func (s *Service) allModelIDs(ctx context.Context) []string {
	models := s.catalog(ctx)
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, s.prefixModel(m.ID))
	}
	return out
}

// disabledSet 返回被停用模型（对外名）的集合。
func (s *Service) disabledSet() map[string]bool {
	out := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		out[d] = true
	}
	return out
}

// handleModels 返回模型目录（带对外前缀）与逐项启停状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	models := s.catalog(r.Context())
	disabled := s.disabledSet()
	// 模型维度的限流汇总：账号表回答「谁被限流」，这里回答「这个模型还能不能用」。
	// 键用**未加前缀**的上游模型名 —— 那正是限流簿与转发层的命名空间
	// （转发前会剥掉插件前缀再发给上游）。
	limitStats := s.modelLimitStats(s.Settings().Accounts)
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		entry := map[string]interface{}{
			"id":                id,
			"displayName":       m.DisplayName,
			"contextLength":     m.ContextLength,
			"maxOutputTokens":   m.MaxOutputTokens,
			"maxAllowedSize":    m.MaxAllowedSize,
			"supportsImages":    m.SupportsImages,
			"supportsReasoning": m.SupportsReasoning,
			"supportsToolCall":  m.SupportsToolCall,
			"onlyReasoning":     m.OnlyReasoning,
			"vendor":            m.Vendor,
			"description":       m.Description,
			"creditsLabel":      m.CreditsLabel,
			"creditsParsed":     m.CreditsParsed,
			"creditsMultiplier": m.CreditsMultiplier,
			"enabled":           !disabled[id],
		}
		if st, ok := limitStats[m.ID]; ok {
			entry["limit"] = st
		}
		out = append(out, entry)
	}
	responseJSON(w, map[string]interface{}{
		"success": true,
		// upstreamReady=false 时列表为空是预期的（上游目录尚未接入）。
		"upstreamReady": upstreamImplemented,
		"models":        out,
	})
}

// handleToggleModel 切换单个模型启用/停用。modelID 为对外（带前缀）模型名。
func (s *Service) handleToggleModel(w http.ResponseWriter, r *http.Request, modelID string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 enabled"})
		return
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少模型 ID"})
		return
	}
	if err := s.setModelsEnabled(r.Context(), []string{modelID}, *body.Enabled); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleBatchToggleModels 批量启用/停用模型。
func (s *Service) handleBatchToggleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Enabled *bool    `json:"enabled"`
		Models  []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "缺少 enabled"})
		return
	}
	if err := s.setModelsEnabled(r.Context(), body.Models, *body.Enabled); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// setModelsEnabled 把给定模型名批量置为启用/停用并持久化。
func (s *Service) setModelsEnabled(ctx context.Context, ids []string, enabled bool) error {
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if enabled {
			delete(disabled, id)
		} else {
			disabled[id] = true
		}
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	if err := s.SaveSettings(ctx, st); err != nil {
		return err
	}
	// 关键：同步到已接入网关端点的 disabled_models 列。网关是按这一列拦截请求的
	// （openai/relay.go 的 resolveEndpointModel → isModelDisabled），只写插件自己的
	// 设置不会让网关侧停止把该模型路由过来。
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
}

// refreshLinkedEndpointModels 把「前缀 + 目录模型」后的名单写回已接入的网关端点，
// 并把 model_mappings 的 key 与 disabled_models 里的模型名从旧前缀迁移到新前缀，
// 保持三列命名空间一致。未接入或不存在时静默跳过；失败不影响设置保存。
func (s *Service) refreshLinkedEndpointModels(ctx context.Context, oldPrefix string) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	newPrefix := s.modelPrefix()
	models := s.prefixModelNames(s.modelIDsFromCache())
	modelsJSON, _ := json.Marshal(models)

	var mappingsRaw sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT model_mappings FROM openai_endpoints WHERE id = ?`,
		linkedEndpointID).Scan(&mappingsRaw)

	mappings := map[string]string{}
	if mappingsRaw.Valid && mappingsRaw.String != "" {
		_ = json.Unmarshal([]byte(mappingsRaw.String), &mappings)
	}
	migratedMappings := make(map[string]string, len(mappings))
	for real, alias := range mappings {
		migratedMappings[remapNamespace(real, newPrefix, oldPrefix, newPrefix)] = alias
	}
	mappingsJSON, _ := json.Marshal(migratedMappings)

	// disabled_models 不在这里迁移：SaveSettings 在前缀变更后会统一把插件设置里的
	// 停用名单权威地写回端点（syncLinkedEndpointDisabledModels）。两处都写会互相覆盖。
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET models = ?, model_mappings = ?, last_checked = ? WHERE id = ?`,
		string(modelsJSON), string(mappingsJSON),
		time.Now().UTC().Format(time.RFC3339), linkedEndpointID)
}

// modelIDsFromCache 返回缓存里的原始（未加前缀）模型 ID。
func (s *Service) modelIDsFromCache() []string {
	models := s.modelsInternal()
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}
