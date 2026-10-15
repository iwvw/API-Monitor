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
//
// ID 与 Key 是两个不同命名空间，**不要混用**：
//   - ID 是官方模型名（如 "Qwen3.8-Max"），作为对外模型名（客户端唯一需要填的值，
//     与官方桌面版选择器一致）；网关端点 models 列、停用名单、中继 /v1/models 都用它。
//   - Key 是上游短 key（如 "qmodel_38max"），只在构造上游请求时写入
//     `model_config.key` / `x-model-key`。转发时把 ID 解析回 Key。
type ModelInfo struct {
	// ID 是官方模型名（对外 id）。
	ID string `json:"id"`
	// Key 是上游短 key（model_config.key / x-model-key 用）。
	Key string `json:"key,omitempty"`
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
// ID=官方模型名、Key=上游短 key；CreditsMultiplier 是**标准倍率**（错峰折扣前的
// price_factor），与 offpeak.go 的 Standard 一致。
var cnStaticModels = []ModelInfo{
	{ID: "Auto", Key: "auto", Provider: "qoder", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Qwen3.8-Max", Key: "qmodel_38max", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Qwen3.8-Flash", Key: "qfmodel", Provider: "qwen", CreditsMultiplier: 0, CreditsParsed: true},
	{ID: "Qwen3.7-Max", Key: "qmodel_latest", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Qwen3.7-Plus", Key: "qmodel", Provider: "qwen", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "Qwen3.7-Flash", Key: "q37fmodel", Provider: "qwen", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "DeepSeek-V4-Pro", Key: "dmodel", Provider: "deepseek", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "DeepSeek-Flash", Key: "dfmodel", Provider: "deepseek", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "GLM-5.3", Key: "gmodel", Provider: "zhipu", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "GLM-5.3-Flash", Key: "gfmodel", Provider: "zhipu", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "GLM-5.2", Key: "gm51model", Provider: "zhipu", CreditsMultiplier: 0.6, CreditsParsed: true},
	{ID: "Kimi-K3", Key: "kmodel_latest", Provider: "moonshot", CreditsMultiplier: 1.4, CreditsParsed: true},
	{ID: "Kimi-K2.8-Preview", Key: "kmodel", Provider: "moonshot", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "MiniMax-M2.7", Key: "mmodel", Provider: "minimax", CreditsMultiplier: 0.2, CreditsParsed: true},
}

// intlStaticModels 是国际版目录（取自本机 .qoder catalog-v6，仅 chat 相关档位）。
var intlStaticModels = []ModelInfo{
	{ID: "Auto", Key: "auto", Provider: "qoder", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Ultimate", Key: "ultimate", Provider: "qoder", CreditsMultiplier: 2.0, CreditsParsed: true},
	{ID: "Performance", Key: "performance", Provider: "qoder", CreditsMultiplier: 1.1, CreditsParsed: true},
	{ID: "Efficient", Key: "efficient", Provider: "qoder", CreditsMultiplier: 0.3, CreditsParsed: true},
	{ID: "Sonus", Key: "smodel", Provider: "qoder", CreditsMultiplier: 8.0, CreditsParsed: true},
	{ID: "Cantus", Key: "cmodel", Provider: "qoder", CreditsMultiplier: 4.0, CreditsParsed: true},
	{ID: "Qwen3.8-Max", Key: "qmodel_38max", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Qwen3.8-Flash", Key: "qfmodel", Provider: "qwen", CreditsMultiplier: 0, CreditsParsed: true},
	{ID: "Qwen3.7-Max", Key: "qmodel_latest", Provider: "qwen", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "Qwen3.7-Plus", Key: "qmodel", Provider: "qwen", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "Kimi-K3", Key: "kmodel_latest", Provider: "moonshot", CreditsMultiplier: 1.4, CreditsParsed: true},
	{ID: "Kimi-K2.8-Preview", Key: "kmodel", Provider: "moonshot", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "GLM-5.3", Key: "gmodel", Provider: "zhipu", CreditsMultiplier: 0.8, CreditsParsed: true},
	{ID: "GLM-5.3-Flash", Key: "gfmodel", Provider: "zhipu", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "DeepSeek-V4-Pro", Key: "dmodel", Provider: "deepseek", CreditsMultiplier: 0.5, CreditsParsed: true},
	{ID: "DeepSeek-Flash", Key: "dfmodel", Provider: "deepseek", CreditsMultiplier: 0.1, CreditsParsed: true},
	{ID: "MiniMax-M3", Key: "mmodel", Provider: "minimax", CreditsMultiplier: 0.2, CreditsParsed: true},
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

// mergeCatalogs 把两区域目录合并成一份对外视图（按对外 id 去重）：
//   - 国内版为基，保留其元数据（倍率）；
//   - 仅国际版有的 id 追加进来（元数据取国际版）；
//   - 每个模型标记型号级可用区域 Regions 与该 id 级 IDRegions。
//
// 对外 id 就是官方模型名，两区域对同一型号用同一官方名，故按 ID 直接合并即可。
func mergeCatalogs(byRegion map[string][]ModelInfo) []ModelInfo {
	cn := byRegion[regionCN]
	intl := byRegion[regionIntl]

	nameRegions := map[string]map[string]bool{}
	mark := func(models []ModelInfo, region string) {
		for _, m := range models {
			key := normalizeModelName(m.ID)
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
		set := nameRegions[normalizeModelName(m.ID)]
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

// keyToNames 是「上游短 key → 官方模型名集合」映射（忽略大小写）。
// 同一短 key 跨区域可能对应不同型号（如 mmodel = MiniMax-M2.7 国内 / MiniMax-M3
// 国际），故一个 key 可能映射到多个官方名；迁移时全部保留，避免任一侧被误开。
var keyToNames = func() map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, m := range append(append([]ModelInfo{}, cnStaticModels...), intlStaticModels...) {
		if m.Key == "" || m.ID == "" {
			continue
		}
		k := strings.ToLower(m.Key)
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		if seen[k][m.ID] {
			continue
		}
		seen[k][m.ID] = true
		out[k] = append(out[k], m.ID)
	}
	return out
}()

// migrateDisabledModels 把旧停用名单里的**上游短 key** 写法迁移为**官方模型名**写法：
// 模型对外 id 由短 key 改为官方名后，旧名单若原样保留会让用户已停用的模型「复活」。
// 入参带当前前缀，迁移保持前缀不变；短 key 同时命中多区域型号时全部写入。
// 未知条目（含已是官方名或自定义）原样保留；结果去重并排序。
func migrateDisabledModels(names []string, prefix string) []string {
	if len(names) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	appendName := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		bare := strings.TrimPrefix(name, prefix)
		if official, ok := keyToNames[strings.ToLower(bare)]; ok {
			for _, o := range official {
				appendName(prefix + o)
			}
			continue
		}
		appendName(name)
	}
	sort.Strings(out)
	return out
}

// regionModelSetFor 返回某区域的模型名集合（供选号过滤）。
// 同时收录对外官方名（ID）与上游短 key（Key），客户端用任一种写法都能命中。
func (s *Service) regionModelSetFor(region string) map[string]bool {
	set := map[string]bool{}
	for _, m := range staticModelsFor(region) {
		set[m.ID] = true
		if m.Key != "" {
			set[m.Key] = true
		}
	}
	return set
}

// accountServesModel 报告账号所属区域是否提供该模型（选号过滤）。
// model 为空时放行；目录按静态表判定（始终已知）。接受官方名或上游短 key。
func (s *Service) accountServesModel(acc Account, model string) bool {
	if model == "" {
		return true
	}
	return s.regionModelSetFor(acc.Region)[model]
}

// allModelIDs 返回对外（带前缀）的模型名列表（官方模型名 ID）。
func (s *Service) allModelIDs() []string {
	models := s.catalog()
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, s.prefixModel(m.ID))
	}
	return out
}

// lookupModelKey 把对外模型名（官方名或上游短 key，可带前缀）解析为上游短 key。
// 未命中时原样返回，保证自定义/直通模型仍然可用。
func (s *Service) lookupModelKey(model string) string {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return ""
	}
	for _, m := range s.catalog() {
		if strings.EqualFold(m.ID, name) || strings.EqualFold(m.Key, name) {
			if m.Key != "" {
				return m.Key
			}
			return m.ID
		}
	}
	return name
}

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表，
// 作为已接入网关端点 models 列的权威值（与中继面 /v1/models 口径一致）。
func (s *Service) enabledModelIDs() []string {
	disabled := s.disabledSet()
	all := s.allModelIDs()
	out := make([]string, 0, len(all))
	for _, id := range all {
		if !disabled[id] {
			out = append(out, id)
		}
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

// isModelEnabled 判断给定模型名（对外名或上游短 key，可带前缀）是否启用。
// 停用名单以对外名为准；兼容旧名单里的上游短 key 写法。
func (s *Service) isModelEnabled(model string) bool {
	disabled := s.disabledSet()
	model = strings.TrimSpace(model)
	if disabled[model] || disabled[s.prefixModel(model)] {
		return false
	}
	if key := s.lookupModelKey(model); key != "" && key != model {
		if disabled[key] || disabled[s.prefixModel(key)] {
			return false
		}
	}
	return true
}

// handleModels 返回模型目录（带对外前缀、区域标记与倍率）与逐项启停状态。
// id 是官方模型名（对外 id），key 是上游短 key（副标识）。
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
		eff, offPeak := effectiveMultiplier(m.Key, m.CreditsMultiplier, now)
		_, hasOffPeak := offPeakDiscounts[m.Key]
		out = append(out, map[string]interface{}{
			"id":                  id,
			"key":                 m.Key,
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
	// SaveSettings 内部已把启用名单与停用名单同步到已接入端点，无需重复同步。
	return s.SaveSettings(ctx, st)
}
