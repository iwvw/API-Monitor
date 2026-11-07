package zcode

// 本文件实现模型目录（静态表）与逐模型启停。
//
// ── 为什么是静态表而不是远程目录 ────────────────────────────
// 上游没有「列模型」的公开接口：编码套餐的可用模型由**客户端内置目录**决定。
// 参考实现（Agent2Providers 的 zcode/models.rs）同样把它硬编码成常量表。
//
// 两地清单逐字相同：差异在「账号能用到哪些」（由套餐档位决定，上游按套餐判，
// 用不到的模型回 502 exceed quota limit），本机无法预先枚举 —— 把「用不到」
// 留给上游如实报错。
//
// `glm-5.3-flash` 必须广告：它是**体验套餐（周末套餐）**被领到之后实际可用的
// 那一档。不广告它，用户领到套餐后在 /v1/models 里看不到能用的模型。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// ModelInfo 是模型目录里的一项。
type ModelInfo struct {
	ID string `json:"id"`
	// Name 是展示名。
	Name string `json:"name,omitempty"`
	// MaxOutput 是上游声明的最大输出 token 数（仅供参考，不参与校验）。
	MaxOutput int64 `json:"maxOutput,omitempty"`
}

// staticModels 是编码套餐的模型表（两地共用）。
var staticModels = []ModelInfo{
	{ID: "glm-5.3", Name: "GLM-5.3", MaxOutput: 128000},
	{ID: "glm-5.3-flash", Name: "GLM-5.3 Flash", MaxOutput: 128000},
	{ID: "glm-5.2", Name: "GLM-5.2", MaxOutput: 128000},
	{ID: "glm-5.1", Name: "GLM-5.1", MaxOutput: 64000},
	{ID: "glm-5", Name: "GLM-5", MaxOutput: 64000},
	{ID: "glm-5-turbo", Name: "GLM-5 Turbo", MaxOutput: 64000},
	{ID: "glm-4.7", Name: "GLM-4.7", MaxOutput: 131072},
	{ID: "glm-4.6", Name: "GLM-4.6", MaxOutput: 131072},
	{ID: "glm-4.5-air", Name: "GLM-4.5 Air", MaxOutput: 98304},
	{ID: "glm-4.6v", Name: "GLM-4.6V", MaxOutput: 32768},
	{ID: "glm-5v-turbo", Name: "GLM-5V Turbo", MaxOutput: 131072},
}

// catalog 返回模型目录快照。
func (s *Service) catalog() []ModelInfo {
	return append([]ModelInfo(nil), staticModels...)
}

// isModelEnabled 判断给定模型名（可带前缀）是否启用。白名单语义。
func (s *Service) isModelEnabled(model string) bool {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return false
	}
	found := false
	for _, m := range staticModels {
		if strings.EqualFold(m.ID, name) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return !s.disabledMatchTokens()[strings.ToLower(name)]
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

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表。
func (s *Service) enabledModelIDs() []string {
	disabled := s.disabledMatchTokens()
	out := make([]string, 0, len(staticModels))
	for _, m := range staticModels {
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
	disabled := s.disabledMatchTokens()
	out := make([]map[string]interface{}, 0, len(staticModels))
	for _, m := range staticModels {
		entry := map[string]interface{}{
			"id":      s.prefixModel(m.ID),
			"enabled": !disabled[strings.ToLower(m.ID)],
		}
		if m.Name != "" {
			entry["name"] = m.Name
		}
		if m.MaxOutput > 0 {
			entry["maxOutput"] = m.MaxOutput
		}
		out = append(out, entry)
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": true,
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
			name := strings.ToLower(strings.TrimSpace(s.stripModelPrefix(id)))
			for existing := range disabled {
				if strings.ToLower(strings.TrimSpace(s.stripModelPrefix(existing))) == name {
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
