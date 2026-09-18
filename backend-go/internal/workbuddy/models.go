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

// catalog 返回可用的**合并**模型目录：缓存新鲜时直接用缓存；过期时按区域回源刷新。
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

	merged, sets := s.refreshCatalogs(ctx)
	if merged == nil {
		return s.modelsInternal()
	}
	s.modelMu.Lock()
	s.modelCache = merged
	s.modelCacheAt = time.Now()
	s.modelCacheByRegion = sets
	s.regionModelSet = buildRegionModelSet(sets)
	s.modelMu.Unlock()
	// 目录刷新后同步到已接入网关端点的 models 列。目录是动态的，
	// 只靠「接入那一刻」写一次的话，先接入后拉目录的场景会长期停在空列表。
	go s.writeLinkedEndpointModels(context.Background(), s.prefixModelNames(modelIDsOf(merged)))
	return merged
}

// regionModelSetFor 返回某区域的模型集合（无则 nil）。仅测试与诊断用。
func (s *Service) regionModelSetFor(region string) map[string]bool {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	return s.regionModelSet[normalizeRegion(region)]
}

// refreshCatalogs 拉取各区域目录并合并。
// 返回 (合并视图, 区域→原始目录)。两个区域都拉不到时返回 (nil, nil)，调用方保留旧快照。
func (s *Service) refreshCatalogs(ctx context.Context) ([]ModelInfo, map[string][]ModelInfo) {
	byRegion := map[string][]ModelInfo{}
	anyOK := false
	for _, region := range []string{regionCN, regionIntl} {
		models, err := s.fetchCatalog(ctx, region)
		if err != nil {
			// 单区域失败：沿用该区域上一份快照（若有），不阻塞另一区域。
			if prev := s.regionCatalog(region); len(prev) > 0 {
				byRegion[region] = prev
				anyOK = true
			}
			continue
		}
		byRegion[region] = models
		anyOK = true
	}
	if !anyOK {
		return nil, nil
	}
	return mergeCatalogs(byRegion), byRegion
}

// regionCatalog 返回某区域上一份目录快照。
func (s *Service) regionCatalog(region string) []ModelInfo {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.modelCacheByRegion == nil {
		return nil
	}
	return s.modelCacheByRegion[normalizeRegion(region)]
}

// normalizeModelName 归一化型号名用于跨区域对齐：小写、去空白。
// 上游对同一型号在两区域可能给不同 id，但显示名一致（如 Kimi-K3）。
func normalizeModelName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// mergeCatalogs 把各区域目录合并成一份对外视图（按模型 id 去重）：
//   - 国内版（cn）为基，保留其元数据（显示名/倍率）；
//   - 仅国际版有的 id 追加进来（元数据取国际版）；
//   - 每个模型标记**型号级**可用区域（Regions，按 displayName 判定：同一型号
//     两版都有即为共用，即使两版 id 不同）、**该 id 级**区域（IDRegions）与
//     型号级 InternationalOnly。
//
// 为什么要按型号名而不是 id 判定：上游对同一型号在不同区域用不同 id
// （Kimi-K3 = kimi-k3-1 / kimi-k3；Hy3 = hy3 / hy3-x / hy3），只按 id 会把
// 同一型号错拆成「国内独有 + 国际独有」两行。路由仍按 id（在 regionModelSet 里），
// 展示标签按型号名，两者语义分离。
func mergeCatalogs(byRegion map[string][]ModelInfo) []ModelInfo {
	cn := byRegion[regionCN]
	intl := byRegion[regionIntl]

	// 型号名（归一化）→ 出现过的区域集合。
	nameRegions := map[string]map[string]bool{}
	mark := func(models []ModelInfo, region string) {
		for _, m := range models {
			key := normalizeModelName(firstNonEmpty(m.DisplayName, m.ID))
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
		set := nameRegions[normalizeModelName(firstNonEmpty(m.DisplayName, m.ID))]
		out := make([]string, 0, len(set))
		// 固定顺序，便于前端稳定展示。
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

	// 国内版打底。
	for _, m := range cn {
		add(m, regionCN)
	}
	// 国际版：同 id 合并 id 区域标记；独有 id 追加。
	for _, m := range intl {
		if i, ok := index[m.ID]; ok {
			merged[i].IDRegions = append(merged[i].IDRegions, regionIntl)
			continue
		}
		add(m, regionIntl)
	}
	return merged
}

// buildRegionModelSet 由「区域→目录」构建「区域→模型 id 集合」，供选号过滤。
func buildRegionModelSet(byRegion map[string][]ModelInfo) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(byRegion))
	for region, models := range byRegion {
		set := make(map[string]bool, len(models))
		for _, m := range models {
			set[m.ID] = true
		}
		out[normalizeRegion(region)] = set
	}
	return out
}

// accountServesModel 报告账号所属区域是否提供该模型（选号过滤的唯一新增条件）。
//   - model 为空（未指定）→ 放行；
//   - 该区域目录尚未就绪 → 保守放行，避免目录未拉到就让整池不可用；
//   - 其余按区域模型集合判定。
func (s *Service) accountServesModel(acc Account, model string) bool {
	if model == "" {
		return true
	}
	s.modelMu.Lock()
	set := s.regionModelSet[normalizeRegion(acc.Region)]
	known := len(set) > 0
	ok := set[model]
	s.modelMu.Unlock()
	if !known {
		return true
	}
	return ok
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
			"regions":           m.Regions,
			"idRegions":         m.IDRegions,
			"internationalOnly": m.InternationalOnly,
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
	// 关键：同步到已接入网关端点的 models/disabled_models 列。网关是按 disabled_models
	// 拦截请求的（openai/relay.go 的 resolveEndpointModel → isModelDisabled），只写插件
	// 自己的设置不会让网关侧停止把该模型路由过来；models 列则收敛为启用项，
	// 与中继面 /v1/models 口径一致，避免首次加载显示全量目录。
	s.syncLinkedEndpointModels(ctx)
	return nil
}

// refreshLinkedEndpointModels 把 model_mappings 的 key 从旧前缀迁移到新前缀，
// 保持与 models/disabled_models 的命名空间一致。未接入或不存在时静默跳过；
// 失败不影响设置保存。
//
// models 与 disabled_models 不在这里写：SaveSettings 在前缀变更后会调用
// syncLinkedEndpointModels 权威写入两列（启用项与停用项）。此处重复写会互相覆盖，
// 且会短暂把全量目录写回 models 列。
func (s *Service) refreshLinkedEndpointModels(ctx context.Context, oldPrefix string) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	newPrefix := s.modelPrefix()

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

	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints SET model_mappings = ? WHERE id = ?`,
		string(mappingsJSON), linkedEndpointID)
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
