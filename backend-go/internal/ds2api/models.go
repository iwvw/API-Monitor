package ds2api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// handleExportAccounts 导出完整引擎配置（账号 + API keys + 模型别名等）。
// 账号含 password（便于迁移），token 因不落盘故不导出。
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
	cfg := store.Snapshot()
	cfg.ClearAccountTokens()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ds2api-config-%s.json", time.Now().Format("20060102")))
	_ = json.NewEncoder(w).Encode(cfg)
}

// handleImportAccounts 导入完整引擎配置（账号 + API keys + 模型别名等）并合并。
// 兼容两种文件：完整配置对象，或 {"accounts":[...]} 包装。
func (s *Service) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "请求体解析失败"})
		return
	}
	// 兼容 {"accounts":[...]} 包装：仅账号导入。
	if wrapped, ok := payload["accounts"].([]interface{}); ok && len(payload) <= 1 {
		s.importAccountsOnly(w, r, wrapped)
		return
	}
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "配置序列化失败"})
		return
	}
	var incoming engineconfig.Config
	if err := json.Unmarshal(rawJSON, &incoming); err != nil {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "配置解析失败: " + err.Error()})
		return
	}
	incoming.ClearAccountTokens()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	importedKeys, importedAccounts := 0, 0
	err = s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		next := cfg.Clone()
		// 合并 API keys（按 key 去重，结构化优先）。
		existing := map[string]bool{}
		for _, k := range next.APIKeys {
			if strings.TrimSpace(k.Key) != "" {
				existing[strings.TrimSpace(k.Key)] = true
			}
		}
		for _, k := range incoming.APIKeys {
			if strings.TrimSpace(k.Key) == "" || existing[strings.TrimSpace(k.Key)] {
				continue
			}
			existing[strings.TrimSpace(k.Key)] = true
			next.APIKeys = append(next.APIKeys, k)
			importedKeys++
		}
		// 合并账号（按 identifier 去重）。
		seen := map[string]bool{}
		for _, a := range next.Accounts {
			if id := a.Identifier(); id != "" {
				seen[id] = true
			}
		}
		for _, a := range incoming.Accounts {
			id := a.Identifier()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			next.Accounts = append(next.Accounts, a)
			importedAccounts++
		}
		// 合并模型别名。
		if len(incoming.ModelAliases) > 0 {
			if next.ModelAliases == nil {
				next.ModelAliases = map[string]string{}
			}
			for k, v := range incoming.ModelAliases {
				next.ModelAliases[k] = v
			}
		}
		// 其余配置项仅在本机为空时采用导入值，避免覆盖本机已有设置。
		if len(next.Keys) == 0 && len(incoming.Keys) > 0 {
			next.Keys = append([]string(nil), incoming.Keys...)
		}
		next.ReconcileCredentials(*cfg)
		next.NormalizeCredentials()
		*cfg = next
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{
		"success":           true,
		"imported_keys":     importedKeys,
		"imported_accounts": importedAccounts,
	})
}

// importAccountsOnly 兼容旧版 {"accounts":[...]} 文件：仅合并账号。
func (s *Service) importAccountsOnly(w http.ResponseWriter, r *http.Request, rawAccounts []interface{}) {
	accs := make([]engineconfig.Account, 0, len(rawAccounts))
	for _, item := range rawAccounts {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var a engineconfig.Account
		if json.Unmarshal(b, &a) == nil {
			accs = append(accs, a)
		}
	}
	if len(accs) == 0 {
		responseJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的账号"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	added := 0
	err := s.persistEngineStore(ctx, func(cfg *engineconfig.Config) error {
		seen := map[string]bool{}
		for _, a := range cfg.Accounts {
			if id := a.Identifier(); id != "" {
				seen[id] = true
			}
		}
		for _, a := range accs {
			id := a.Identifier()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			cfg.Accounts = append(cfg.Accounts, a)
			added++
		}
		return nil
	})
	if err != nil {
		responseJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "added": added})
}
