package geminicli

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// staticModels 是 Gemini CLI 的静态模型目录（来自 CLIProxyAPI models.json
// 的 gemini-cli 段）。CLI 的可用模型面稳定，不做动态回源。
//
// 全部模型 inputTokenLimit = 1048576、outputTokenLimit = 65536。
var staticModels = []ModelInfo{
	{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, Description: "Stable release (June 17th, 2025) of Gemini 2.5 Pro"},
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, Description: "Stable version of Gemini 2.5 Flash"},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, Description: "Smallest and most cost effective model"},
	{ID: "gemini-3-pro-preview", DisplayName: "Gemini 3 Pro Preview", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, SupportsReasoning: true, Description: "Most intelligent model with SOTA reasoning and multimodal understanding"},
	{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, SupportsReasoning: true, Description: "Gemini 3.1 Pro Preview"},
	{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, SupportsReasoning: true, Description: "Most intelligent model built for speed"},
	{ID: "gemini-3.1-flash-lite-preview", DisplayName: "Gemini 3.1 Flash Lite Preview", ContextLength: 1048576, MaxOutputTokens: 65536, SupportsImages: true, SupportsToolCall: true, SupportsReasoning: true, Description: "Smallest and most cost effective model"},
}

// catalog 返回静态模型目录快照。
func (s *Service) catalog() []ModelInfo {
	return append([]ModelInfo(nil), staticModels...)
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

// modelIDsFromCache 返回缓存里的原始（未加前缀）模型 ID。
func (s *Service) modelIDsFromCache() []string {
	models := s.catalog()
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
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
	if disabled[s.prefixModel(model)] {
		return false
	}
	return true
}

// handleModels 返回模型目录（带对外前缀）与逐项启停状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	models := s.catalog()
	disabled := s.disabledSet()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		out = append(out, map[string]interface{}{
			"id":                id,
			"displayName":       m.DisplayName,
			"contextLength":     m.ContextLength,
			"maxOutputTokens":   m.MaxOutputTokens,
			"supportsImages":    m.SupportsImages,
			"supportsReasoning": m.SupportsReasoning,
			"supportsToolCall":  m.SupportsToolCall,
			"description":       m.Description,
			"enabled":           !disabled[id],
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
