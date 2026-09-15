package lobsterai

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// staticModels 是动态模型接口失败时的回退目录（2026-09-15 从上游
// GET /api/models/available 记录，共 19 个）。动态拉取成功后以动态结果为准。
var staticModels = []ModelInfo{
	{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Provider: "deepseek"},
	{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Provider: "deepseek"},
	{ID: "MiniMax-M3", Name: "MiniMax M3", Provider: "minimax"},
	{ID: "MiniMax-M2.7", Name: "MiniMax M2.7", Provider: "minimax"},
	{ID: "qwen3.7-max", Name: "Qwen3.7 Max", Provider: "qwen"},
	{ID: "qwen3.7-plus", Name: "Qwen3.7 Plus", Provider: "qwen"},
	{ID: "qwen3.6-plus", Name: "Qwen3.6 Plus", Provider: "qwen"},
	{ID: "qwen3.5-plus-2026-04-20", Name: "Qwen3.5 Plus", Provider: "qwen"},
	{ID: "kimi-k2.7-code", Name: "Kimi K2.7 Code", Provider: "kimi"},
	{ID: "kimi-k2.7-code-highspeed", Name: "Kimi K2.7 Code Highspeed", Provider: "kimi"},
	{ID: "kimi-k2.6", Name: "Kimi K2.6", Provider: "kimi"},
	{ID: "kimi-k2.5", Name: "Kimi K2.5", Provider: "kimi"},
	{ID: "doubao-seed-2-1-pro-260628", Name: "Doubao Seed 2.1 Pro", Provider: "doubao"},
	{ID: "doubao-seed-2-1-turbo-260628", Name: "Doubao Seed 2.1 Turbo", Provider: "doubao"},
	{ID: "doubao-seed-2-0-code-preview-260215", Name: "Doubao Seed 2.0 Code Preview", Provider: "doubao"},
	{ID: "glm-5.2", Name: "GLM-5.2", Provider: "zhipu"},
	{ID: "glm-5.1", Name: "GLM-5.1", Provider: "zhipu"},
	{ID: "glm-5v-turbo", Name: "GLM-5V Turbo", Provider: "zhipu"},
	{ID: "glm-5", Name: "GLM-5", Provider: "zhipu"},
}

// modelsCacheTTL 是动态模型目录的缓存时长。
const modelsCacheTTL = time.Hour

// catalog 返回模型目录快照：优先动态缓存，其次静态表。
func (s *Service) catalog() []ModelInfo {
	s.modelsMu.Lock()
	defer s.modelsMu.Unlock()
	if len(s.modelsCache) > 0 {
		return append([]ModelInfo(nil), s.modelsCache...)
	}
	return append([]ModelInfo(nil), staticModels...)
}

// refreshCatalog 尝试从上游拉一次模型目录并写入缓存；失败返回错误（保留旧缓存）。
func (s *Service) refreshCatalog(ctx context.Context) error {
	s.modelsMu.Lock()
	if len(s.modelsCache) > 0 && time.Since(s.modelsAt) < modelsCacheTTL {
		s.modelsMu.Unlock()
		return nil
	}
	s.modelsMu.Unlock()

	models, err := s.fetchModelsFromAnyAccount(ctx)
	if err != nil {
		return err
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	s.modelsMu.Lock()
	s.modelsCache, s.modelsAt = models, time.Now()
	s.modelsMu.Unlock()
	return nil
}

// allModelIDs 返回对外（带前缀）的模型 ID 列表。
func (s *Service) allModelIDs() []string {
	models := s.catalog()
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

// isModelEnabled 判断给定模型名（对外或裸名）是否启用。
func (s *Service) isModelEnabled(model string) bool {
	disabled := s.disabledSet()
	if disabled[model] {
		return false
	}
	return !disabled[s.prefixModel(model)]
}

// handleModels 返回模型目录（带对外前缀）与逐项启停状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		_ = s.refreshCatalog(r.Context())
	}
	models := s.catalog()
	disabled := s.disabledSet()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		out = append(out, map[string]interface{}{
			"id":             id,
			"name":           m.Name,
			"provider":       m.Provider,
			"costMultiplier": m.Cost,
			"enabled":        !disabled[id],
		})
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": s.upstreamReady(),
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
	sort.Strings(st.DisabledModels)
	if err := s.SaveSettings(ctx, st); err != nil {
		return err
	}
	// 网关是按端点 disabled_models 列拦截请求的，必须同步。
	s.syncLinkedEndpointDisabledModels(ctx)
	return nil
}

// writeLinkedEndpointModels 只更新已接入网关端点的 models 列。未接入时静默跳过。
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
