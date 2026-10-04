package qoder

// 本文件实现模型目录与逐模型启停。
//
// Qoder 没有公开的动态模型目录接口（`/algo/api/v2/model/list` 需 COSY 签名），
// 目录以静态表为准：按区域各自维护，取自官方客户端目录缓存 `catalog-v6`
// （HKDF-SHA256(uid) + AES-256-GCM 解密）实测记录，含 price_factor 倍率。
//
// 两区域目录不同：国际版有 Ultimate/Performance/Efficient/Sonus/Cantus，
// 国内版没有；国内版有 Qwen3.7-Flash/GLM-5.2，国际版没有。模型 key 直接透传给
// 上游 `model_config.key` / `x-model-key`。

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
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Provider 是厂商标识（qwen/deepseek/zhipu/...）。
	Provider string `json:"provider,omitempty"`
	// CreditsMultiplier 是标准积分倍率（price_factor），Qoder 计费体系内的相对
	// 倍数，不含货币单价，只能横向比较。
	CreditsMultiplier float64 `json:"creditsMultiplier,omitempty"`
	// CreditsParsed 表示倍率是否解析成功（false 时前端显示「—」而不是 0）。
	CreditsParsed bool `json:"creditsParsed,omitempty"`
	// EffectiveMultiplier 是当前时刻实际生效的倍率（考虑错峰折扣）。
	EffectiveMultiplier float64 `json:"effectiveMultiplier,omitempty"`
	// OffPeak 表示该模型当前是否处于错峰折扣中。
	OffPeak bool `json:"offPeak,omitempty"`
	// HasOffPeak 表示该模型是否属于错峰折扣范围（有折扣档）。
	HasOffPeak bool `json:"hasOffPeak,omitempty"`

	// Regions 是该模型**型号**可用的区域列表（cn/intl），按 displayName 判定。
	Regions []string `json:"regions,omitempty"`
	// IDRegions 是**该 id 本身**所属的区域。
	IDRegions []string `json:"idRegions,omitempty"`
	// InternationalOnly 表示该型号仅国际版提供（前端据此打「国际」标注）。
	InternationalOnly bool `json:"internationalOnly,omitempty"`
}

// cnStaticModels 是国内版目录（取自本机 .qoder-cn catalog-v6）。
var cnStaticModels = []ModelInfo{
	{ID: "auto", Name: "Auto（自动选择）", Provider: "qoder", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "qmodel_38max", Name: "Qwen3.8-Max", Provider: "qwen", CreditsMultiplier: 0.2, CreditsParsed: true},
	{ID: "qfmodel", Name: "Qwen3.8-Flash", Provider: "qwen", CreditsMultiplier: 0, CreditsParsed: true},
	{ID: "qmodel_latest", Name: "Qwen3.7-Max", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "qmodel", Name: "Qwen3.7-Plus", Provider: "qwen", CreditsMultiplier: 0.04, CreditsParsed: true},
	{ID: "q37fmodel", Name: "Qwen3.7-Flash", Provider: "qwen", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "dmodel", Name: "DeepSeek-V4-Pro", Provider: "deepseek", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "dfmodel", Name: "DeepSeek-Flash", Provider: "deepseek", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "gmodel", Name: "GLM-5.3", Provider: "zhipu", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "gfmodel", Name: "GLM-5.3-Flash", Provider: "zhipu", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "gm51model", Name: "GLM-5.2", Provider: "zhipu", CreditsMultiplier: 0.6, CreditsParsed: true},
	{ID: "kmodel_latest", Name: "Kimi-K3", Provider: "moonshot", CreditsMultiplier: 1.4, CreditsParsed: true},
	{ID: "kmodel", Name: "Kimi-K2.8-Preview", Provider: "moonshot", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "mmodel", Name: "MiniMax-M2.7", Provider: "minimax", CreditsMultiplier: 0.2, CreditsParsed: true},
}

// intlStaticModels 是国际版目录（取自本机 .qoder catalog-v6，仅 chat 相关档位）。
var intlStaticModels = []ModelInfo{
	{ID: "auto", Name: "Auto（自动选择）", Provider: "qoder", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "ultimate", Name: "Ultimate", Provider: "qoder", CreditsMultiplier: 2.0, CreditsParsed: true},
	{ID: "performance", Name: "Performance", Provider: "qoder", CreditsMultiplier: 1.1, CreditsParsed: true},
	{ID: "efficient", Name: "Efficient", Provider: "qoder", CreditsMultiplier: 0.3, CreditsParsed: true},
	{ID: "smodel", Name: "Sonus", Provider: "qoder", CreditsMultiplier: 8.0, CreditsParsed: true},
	{ID: "cmodel", Name: "Cantus", Provider: "qoder", CreditsMultiplier: 4.0, CreditsParsed: true},
	{ID: "qmodel_38max", Name: "Qwen3.8-Max", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "qfmodel", Name: "Qwen3.8-Flash", Provider: "qwen", CreditsMultiplier: 0, CreditsParsed: true},
	{ID: "qmodel_latest", Name: "Qwen3.7-Max", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "qmodel", Name: "Qwen3.7-Plus", Provider: "qwen", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "kmodel_latest", Name: "Kimi-K3", Provider: "moonshot", CreditsMultiplier: 1.4, CreditsParsed: true},
	{ID: "kmodel", Name: "Kimi-K2.8-Preview", Provider: "moonshot", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "gmodel", Name: "GLM-5.3", Provider: "zhipu", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "gfmodel", Name: "GLM-5.3-Flash", Provider: "zhipu", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "dmodel", Name: "DeepSeek-V4-Pro", Provider: "deepseek", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "dfmodel", Name: "DeepSeek-Flash", Provider: "deepseek", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "mmodel", Name: "MiniMax-M3", Provider: "minimax", CreditsMultiplier: 0.2, CreditsParsed: true},
}

// staticModelsFor 返回区域的静态目录。
func staticModelsFor(region string) []ModelInfo {
	if normalizeRegion(region) == regionIntl {
		return intlStaticModels
	}
	return cnStaticModels
}

// normalizeModelName 归一化型号名用于跨区域对齐：小写、去空白。
func normalizeModelName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// mergeCatalogs 把两区域目录合并成一份对外视图（按模型 id 去重）：
//   - 国内版为基，保留其元数据（显示名/倍率）；
//   - 仅国际版有的 id 追加进来（元数据取国际版）；
//   - 每个模型标记型号级可用区域 Regions（按 displayName 判定）与该 id 级 IDRegions。
func mergeCatalogs(byRegion map[string][]ModelInfo) []ModelInfo {
	cn := byRegion[regionCN]
	intl := byRegion[regionIntl]

	nameRegions := map[string]map[string]bool{}
	mark := func(models []ModelInfo, region string) {
		for _, m := range models {
			key := normalizeModelName(firstNonEmpty(m.Name, m.ID))
			if key == "" {
				continue
			}
			if nameRegions[key] == nil {
				nameRegions[key] = map[string]bool{}
			}
			nameRegions[key][region] = true
		}
	}
	mark(cn, regionCN)
	mark(intl, regionIntl)

	regionsFor := func(m ModelInfo) []string {
		set := nameRegions[normalizeModelName(firstNonEmpty(m.Name, m.ID))]
		out := make([]string, 0, len(set))
		if set[regionCN] {
			out = append(out, regionCN)
		}
		if set[regionIntl] {
			out = append(out, regionIntl)
		}
		return out
	}

	merged := make([]ModelInfo, 0, len(cn)+len(intl))
	index := map[string]int{}
	add := func(m ModelInfo, idRegion string) {
		m.IDRegions = []string{idRegion}
		m.Regions = regionsFor(m)
		m.InternationalOnly = len(m.Regions) == 1 && m.Regions[0] == regionIntl
		merged = append(merged, m)
		index[m.ID] = len(merged) - 1
	}

	for _, m := range cn {
		add(m, regionCN)
	}
	for _, m := range intl {
		if i, ok := index[m.ID]; ok {
			merged[i].IDRegions = append(merged[i].IDRegions, regionIntl)
			continue
		}
		add(m, regionIntl)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	return merged
}

// activeRegions 返回当前存在账号的区域集合（含已停用账号，账号存在即视为活跃）。
// 无账号时返回空集合。
func (s *Service) activeRegions() map[string]bool {
	out := map[string]bool{}
	for _, a := range s.Settings().Accounts {
		out[normalizeRegion(a.Region)] = true
	}
	return out
}

// catalog 返回模型目录快照，**只包含当前有账号的区域**的模型：
//   - 只有国内版账号 → 仅国内版模型；
//   - 只有国际版账号 → 仅国际版模型；
//   - 两区域都有 → 合并视图（带区域标记）。
//
// 无账号时返回国内版目录（默认区域），保证首次接入前列表非空。
func (s *Service) catalog() []ModelInfo {
	regions := s.activeRegions()
	hasCN := regions[regionCN]
	hasIntl := regions[regionIntl]
	switch {
	case hasCN && hasIntl:
		return mergeCatalogs(map[string][]ModelInfo{
			regionCN:   cnStaticModels,
			regionIntl: intlStaticModels,
		})
	case hasIntl:
		return withRegion(intlStaticModels, regionIntl)
	default:
		// 只有国内版，或无账号（默认国内版）。
		return withRegion(cnStaticModels, regionCN)
	}
}

// withRegion 给一组模型补上单一区域标记（供只显示单区域时使用）。
func withRegion(models []ModelInfo, region string) []ModelInfo {
	out := make([]ModelInfo, 0, len(models))
	for _, m := range models {
		m.Regions = []string{region}
		m.IDRegions = []string{region}
		m.InternationalOnly = region == regionIntl
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// regionModelSetFor 返回某区域的模型 id 集合（供选号过滤）。
func (s *Service) regionModelSetFor(region string) map[string]bool {
	set := map[string]bool{}
	for _, m := range staticModelsFor(region) {
		set[m.ID] = true
	}
	return set
}

// accountServesModel 报告账号所属区域是否提供该模型（选号过滤）。
// model 为空时放行；目录按静态表判定（始终已知）。
func (s *Service) accountServesModel(acc Account, model string) bool {
	if model == "" {
		return true
	}
	return s.regionModelSetFor(acc.Region)[model]
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

// handleModels 返回模型目录（带对外前缀、区域标记与倍率）与逐项启停状态。
func (s *Service) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	models := s.catalog()
	disabled := s.disabledSet()
	now := time.Now()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		eff, offPeak := effectiveMultiplier(m.ID, m.CreditsMultiplier, now)
		_, hasOffPeak := offPeakDiscounts[m.ID]
		out = append(out, map[string]interface{}{
			"id":                  id,
			"name":                m.Name,
			"provider":            m.Provider,
			"creditsMultiplier":   m.CreditsMultiplier,
			"creditsParsed":       m.CreditsParsed,
			"effectiveMultiplier": eff,
			"offPeak":             offPeak,
			"hasOffPeak":          hasOffPeak,
			"regions":             m.Regions,
			"idRegions":           m.IDRegions,
			"internationalOnly":   m.InternationalOnly,
			"enabled":             !disabled[id],
		})
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": s.upstreamReady(),
		"offPeak":       isOffPeak(now),
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
		UPDATE openai_endpoints SET models = ? WHERE id = ?`,
		string(payload), linkedEndpointID)
}
