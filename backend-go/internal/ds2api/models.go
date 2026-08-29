package ds2api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// handleModels 返回引擎支持的 DeepSeek 模型列表（含别名展开）+ 启用状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	names := engineSupportedModels(nil)
	if st, _ := s.loadEngineStore(); st != nil {
		names = engineSupportedModels(st)
	}
	disabled := map[string]bool{}
	for _, d := range s.Settings().DisabledModels {
		disabled[d] = true
	}
	out := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]interface{}{"id": n, "enabled": !disabled[n]})
	}
	responseJSON(w, map[string]interface{}{"success": true, "models": out})
}

// handleToggleModel 切换单个模型启用/停用。
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
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	if *body.Enabled {
		delete(disabled, modelID)
	} else {
		disabled[modelID] = true
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
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
	st := s.Settings()
	disabled := map[string]bool{}
	for _, d := range st.DisabledModels {
		disabled[d] = true
	}
	for _, m := range body.Models {
		if m == "" {
			continue
		}
		if *body.Enabled {
			delete(disabled, m)
		} else {
			disabled[m] = true
		}
	}
	st.DisabledModels = st.DisabledModels[:0]
	for k := range disabled {
		st.DisabledModels = append(st.DisabledModels, k)
	}
	if err := s.SaveSettings(r.Context(), st); err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// engineSupportedModels 返回引擎真实支持的 DeepSeek 模型（不含别名展开）。
func engineSupportedModels(_ *engineconfig.Store) []string {
	out := []string{}
	for _, m := range engineconfig.DeepSeekModels {
		out = append(out, m.ID)
	}
	return out
}

// handleExportAccounts 导出账号授权文件（含 token/凭证）。
func (s *Service) handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	store, err := s.loadEngineStore()
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "引擎未启用: " + err.Error()})
		return
	}
	accs := store.Snapshot().Accounts
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ds2api-accounts-%s.json", time.Now().Format("20060102")))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accs})
}

// handleImportAccounts 导入账号授权文件。
func (s *Service) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var body struct {
		Accounts []engineconfig.Account `json:"accounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	if len(body.Accounts) == 0 {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的账号"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err := s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		seen := map[string]bool{}
		for _, a := range cfg.Accounts {
			seen[a.Identifier()] = true
		}
		for _, a := range body.Accounts {
			id := a.Identifier()
			if id == "" {
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			cfg.Accounts = append(cfg.Accounts, a)
		}
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}
