package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ModelInfo 是模型目录里的一项。
type ModelInfo struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	ContextLength     int64  `json:"contextLength,omitempty"`
	MaxOutputTokens   int64  `json:"maxOutputTokens,omitempty"`
	SupportsImages    bool   `json:"supportsImages,omitempty"`
	SupportsReasoning bool   `json:"supportsReasoning,omitempty"`
	SupportsToolCall  bool   `json:"supportsToolCall,omitempty"`
	Description       string `json:"description,omitempty"`
}

// freeModelIDs 是免费层实测可用的模型（与参考实现 opencode-proxy 的
// FREE_MODEL_IDS 对齐；免费 ID 会轮换，/v1/models 始终显示可用的当前集合）。
var freeModelIDs = []string{
	"muse-spark-1.3-contributor-free",
	"muse-spark-1.2-contributor-free",
	"mimo-v2.6-flash-free",
	"longcat-2.5-preview-free",
}

// staticModels 是静态模型目录。muse-spark-* 走 /zen/v1/responses（原生推理），
// 其余走 /zen/v1/chat/completions。上下文/输出上限无可靠权威值时不编造。
var staticModels = func() []ModelInfo {
	out := make([]ModelInfo, 0, len(freeModelIDs))
	for _, id := range freeModelIDs {
		m := ModelInfo{
			ID:               id,
			SupportsToolCall: true,
			Description:      "OpenCode Zen 免费层模型（转 OpenAI 兼容 API）",
		}
		if strings.HasPrefix(id, "muse-spark-") {
			m.SupportsReasoning = true
		}
		out = append(out, m)
	}
	return out
}()

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

// modelPrefix 返回归一化的对外模型前缀（空串表示不加前缀）。
func (s *Service) modelPrefix() string {
	return strings.TrimSpace(s.Settings().ModelPrefix)
}

// prefixModel 给内部模型 ID 套上对外前缀。
func (s *Service) prefixModel(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return p + id
}

// stripModelPrefix 剥掉请求模型名上的本插件前缀（不命中或空前缀时原样返回）。
func (s *Service) stripModelPrefix(id string) string {
	p := s.modelPrefix()
	if p == "" {
		return id
	}
	return strings.TrimPrefix(id, p)
}