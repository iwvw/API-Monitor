package raccoon

// 本文件实现模型目录的对外视图与逐模型启停。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// ModelInfo 是模型目录里的一项：上游模型名（即对外模型名，无别名映射）
// 与实时倍率。能力标签、描述等元数据不再保留。
type ModelInfo struct {
	ID string `json:"id"`
	// BillingMultiplier 是标准倍率（billing_multiplier）。
	BillingMultiplier *float64 `json:"billingMultiplier,omitempty"`
	// BillingEffectiveMultiplier 是实时生效倍率（billing_effective_multiplier）。
	// 用指针区分「上游明确给 0（免费）」与「上游未提供」。
	BillingEffectiveMultiplier *float64 `json:"billingEffectiveMultiplier,omitempty"`
}

// effectiveMultiplier 返回实时倍率；上游未给时回落到标准倍率，均无则 nil。
func (m ModelInfo) effectiveMultiplier() *float64 {
	if m.BillingEffectiveMultiplier != nil {
		return m.BillingEffectiveMultiplier
	}
	return m.BillingMultiplier
}

// isModelEnabled 判断给定模型名（可带前缀）是否启用。**白名单语义**：
// 不在目录内的模型名一律视为停用，防止上游回落后默认模型被误用。
func (s *Service) isModelEnabled(model string) bool {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return false
	}
	found := false
	for _, m := range s.catalog() {
		if strings.EqualFold(m.ID, name) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	disabled := s.disabledMatchTokens()
	return !disabled[strings.ToLower(name)]
}

// disabledMatchTokens 返回停用名单归一化的 token 集合（小写、去前缀）。
func (s *Service) disabledMatchTokens() map[string]bool {
	set := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(d)))
		if name != "" {
			set[strings.ToLower(name)] = true
		}
	}
	return set
}

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表，作为已接入网关
// 端点 models 列的权威值。
func (s *Service) enabledModelIDs() []string {
	models := s.catalog()
	disabled := s.disabledMatchTokens()
	out := make([]string, 0, len(models))
	for _, m := range models {
		if disabled[strings.ToLower(m.ID)] {
			continue
		}
		out = append(out, s.prefixModel(m.ID))
	}
	return out
}

func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	s.refreshCatalogAsync()
	models := s.catalog()
	disabled := s.disabledMatchTokens()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		entry := map[string]interface{}{
			"id":      s.prefixModel(m.ID),
			"enabled": !disabled[strings.ToLower(m.ID)],
		}
		if eff := m.effectiveMultiplier(); eff != nil {
			entry["multiplier"] = *eff
		}
		out = append(out, entry)
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": s.upstreamReady(),
		"models":        out,
	})
}

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
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
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
		prefixed := s.prefixModel(strings.TrimSpace(s.stripModelPrefix(id)))
		if enabled {
			// 启用：把停用名单里指向同一模型（含旧前缀残留）一并移除。
			aliases := modelAliasSet(s.catalog(), s.stripModelPrefix(id))
			for existing := range disabled {
				if aliases[strings.ToLower(strings.TrimSpace(s.stripModelPrefix(existing)))] {
					delete(disabled, existing)
				}
			}
		} else {
			disabled[prefixed] = true
		}
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	sort.Strings(st.DisabledModels)
	return s.SaveSettings(ctx, st)
}

// modelAliasSet 返回目录中同名模型（大小写不敏感）的 token 集合。
func modelAliasSet(models []ModelInfo, name string) map[string]bool {
	n := strings.ToLower(strings.TrimSpace(name))
	set := map[string]bool{}
	for _, m := range models {
		if strings.ToLower(m.ID) == n {
			set[n] = true
			break
		}
	}
	return set
}
