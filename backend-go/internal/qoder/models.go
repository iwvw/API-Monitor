package qoder

// 本文件实现模型目录的对外视图与逐模型启停。
//
// 目录来源分两层：
//   - **动态发现**（catalog.go）：用已登录账号的签名会话调上游
//     `GET /algo/api/v2/model/list`，按区域缓存真实模型目录。这是对外 id 与
//     路由的权威来源，能跟上上游增删/改名（参考实现 cli-proxy/qoder）。
//   - **静态兜底**（本文件 cnStaticModels/intlStaticModels）：仅在动态目录首次
//     拉取成功前使用，取自某次客户端目录缓存实测。它同时提供动态目录不带的
//     元数据（price_factor 倍率、厂商标识），按上游短 key 回填到动态条目上。
//
// ModelInfo 的两个命名空间**不要混用**：
//   - ID 是对外模型名（上游 display_name，如 "Qwen3.8-Max"），网关端点 models 列、
//     停用名单、中继 /v1/models 都用它。
//   - Key 是上游短 key（如 "qmodel_38max"），只在构造上游请求时写入
//     `model_config.key` / `x-model-key`。转发时把对外名（或短 key）解析回 Key。
//
// 停用（DisabledModels）采用**白名单**语义：不在当前目录内的模型名一律拒绝，
// 杜绝上游改名后旧名/别名绕过停用名单；停用判定同时按 ID 与 Key 归一，
// 历史短 key 写法与跨区域同 key 型号都能命中。

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
	// 倍数，不含货币单价，只能横向比较。动态目录未给出实时倍率时取静态表。
	CreditsMultiplier float64 `json:"creditsMultiplier,omitempty"`
	// CreditsParsed 表示倍率是否解析成功（false 时前端显示「—」而不是 0）。
	CreditsParsed bool `json:"creditsParsed,omitempty"`
	// LiveMultiplier 是上游**实时**下发的主倍率（price_factor）。用指针是为了
	// 区分「上游明确给了 0（免费）」与「上游没给」——0 是合法值。
	LiveMultiplier *float64 `json:"liveMultiplier,omitempty"`
	// OriginalMultiplier 是上游的原价倍率（original_price_factor），限时免费
	// 模型会给出「现价 0 / 原价 0.1」。
	OriginalMultiplier *float64 `json:"originalMultiplier,omitempty"`
	// Free 表示上游把该模型标为免费（price_factor 为 0 或 is_free=true）。
	Free bool `json:"free,omitempty"`
	// Promotion 是上游下发的促销（错峰折扣）规则；nil 表示该模型没有促销。
	Promotion *Promotion `json:"promotion,omitempty"`
	// PriceSource 标记倍率来源：live（上游实时）/ static（内置兜底）。
	PriceSource string `json:"priceSource,omitempty"`

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

// cnStaticModels 是国内版目录（取自本机 .qoder-cn catalog-v6），仅作动态目录
// 拉取前的兜底。ID=官方模型名、Key=上游短 key；CreditsMultiplier 是**标准倍率**
// （错峰折扣前的 price_factor），与 offpeak.go 的静态兜底规则一致。
// 注意：上游调价后这些值会过期，动态目录里的实时 price_factor 优先。
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

// intlStaticModels 是国际版目录（取自本机 .qoder catalog-v6，仅 chat 相关档位），
// 同样仅作动态目录拉取前的兜底。
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

// staticModelsFor 返回区域的静态兜底目录。
// 兜底条目也附上静态促销规则，保证首次回源成功前错峰倍率展示与历史一致。
func staticModelsFor(region string) []ModelInfo {
	src := cnStaticModels
	if normalizeRegion(region) == regionIntl {
		src = intlStaticModels
	}
	out := make([]ModelInfo, 0, len(src))
	for _, m := range src {
		m.Promotion = staticPromotionFor(m.Key)
		m.PriceSource = priceStatic
		out = append(out, m)
	}
	return out
}

// staticModelByKey 按上游短 key 在静态表里查条目（用于回填倍率/厂商标识）。
func staticModelByKey(key string) (ModelInfo, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return ModelInfo{}, false
	}
	for _, m := range append(append([]ModelInfo{}, cnStaticModels...), intlStaticModels...) {
		if strings.ToLower(m.Key) == k {
			return m, true
		}
	}
	return ModelInfo{}, false
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

// catalogModels 返回当前对外模型目录快照（合并视图，未加前缀）：
//   - 只有国内版账号 → 仅国内版模型；
//   - 只有国际版账号 → 仅国际版模型；
//   - 两区域都有 → 合并视图（带区域标记）；
//   - 无账号 → 国内版目录（默认区域），保证首次接入前列表非空。
//
// 各区域目录优先取动态发现缓存，未就绪时回退静态兜底（见 catalog.go regionModels）。
func (s *Service) catalogModels() []ModelInfo {
	regions := s.activeRegions()
	hasCN := regions[regionCN]
	hasIntl := regions[regionIntl]
	switch {
	case hasCN && hasIntl:
		return mergeCatalogs(map[string][]ModelInfo{
			regionCN:   s.regionModels(regionCN),
			regionIntl: s.regionModels(regionIntl),
		})
	case hasIntl:
		return s.regionModels(regionIntl)
	default:
		// 只有国内版，或无账号（默认国内版）。
		return s.regionModels(regionCN)
	}
}

// catalog 是 catalogModels 的别名，保持与同类插件一致的调用命名。
func (s *Service) catalog() []ModelInfo {
	return s.catalogModels()
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

// keyToNames 是「上游短 key → 官方模型名集合」映射（忽略大小写），取自静态表。
// 同一短 key 跨区域可能对应不同型号（如 mmodel = MiniMax-M2.7 国内 / MiniMax-M3
// 国际），故一个 key 可能映射到多个官方名；停用归一化时全部保留，避免任一侧被误开。
// 动态目录里的新别名不在静态表内，运行时由 disabledMatchTokens 按当前目录补齐。
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
// 目录取动态发现缓存，未就绪时回退静态兜底。
func (s *Service) regionModelSetFor(region string) map[string]bool {
	set := map[string]bool{}
	for _, m := range s.regionModels(region) {
		set[m.ID] = true
		if m.Key != "" {
			set[m.Key] = true
		}
	}
	return set
}

// accountServesModel 报告账号所属区域是否提供该模型（选号过滤）。
// model 为空时放行；目录按当前区域目录判定。接受官方名或上游短 key。
func (s *Service) accountServesModel(acc Account, model string) bool {
	if model == "" {
		return true
	}
	return s.regionModelSetFor(acc.Region)[model]
}

// resolveCatalogEntries 把对外模型名（官方名或上游短 key，可带前缀）解析为
// 当前目录里的全部命中条目（跨区域同 key 可能多条）。大小写不敏感。
func (s *Service) resolveCatalogEntries(model string) []ModelInfo {
	name := strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(model)))
	if name == "" {
		return nil
	}
	var out []ModelInfo
	seen := map[string]bool{}
	for _, m := range s.catalog() {
		if strings.EqualFold(m.ID, name) || (m.Key != "" && strings.EqualFold(m.Key, name)) {
			k := strings.ToLower(m.ID)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, m)
		}
	}
	return out
}

// lookupModelKey 把对外模型名（官方名或上游短 key，可带前缀）解析为上游短 key。
// 未命中时原样返回（理论上不会发生：中继面已用 isModelEnabled 白名单拦截目录外模型）。
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

// modelAliasClosure 从一个或多个种子名出发，计算「同一模型」的别名闭包（全小写）。
//
// 每个目录条目（静态表 + 当前动态目录）都被视为一条 id ↔ key 双向边；同名/同 key
// 的条目自然连通，因此在合并图上按连通分量展开即可。这样：
//   - 静态表把官方名与其短 key 连起来；
//   - 动态目录把上游新 display_name 与其（可能沿用旧短 key 的）key 连起来；
//   - 共享短 key 的旧名与新名也连通（如静态 DeepSeek-Flash 与动态
//     DeepSeek-V4.1-Flash 都用 dfmodel）。
//
// 用途：停用判定与「启用时清理别名」共用同一套连通性，保证二者语义一致。
func (s *Service) modelAliasClosure(seeds []string) map[string]bool {
	tokens := map[string]bool{}
	add := func(v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			tokens[v] = true
		}
	}
	for _, raw := range seeds {
		add(s.stripModelPrefix(strings.TrimSpace(raw)))
	}
	edges := append(append([]ModelInfo{}, cnStaticModels...), intlStaticModels...)
	edges = append(edges, s.catalog()...)
	for changed := true; changed; {
		changed = false
		for _, m := range edges {
			id := strings.ToLower(strings.TrimSpace(m.ID))
			key := strings.ToLower(strings.TrimSpace(m.Key))
			if !tokens[id] && !(key != "" && tokens[key]) {
				continue
			}
			if id != "" && !tokens[id] {
				tokens[id] = true
				changed = true
			}
			if key != "" && !tokens[key] {
				tokens[key] = true
				changed = true
			}
		}
	}
	return tokens
}

// disabledMatchTokens 返回一组「停用匹配 token」（全小写）：只要某个目录条目的
// ID 或 Key 落在集合里，就视为被停用。
//
// 为什么不能只按字面比对：停用名单是历史累积的（可能存官方名、短 key、甚至上游
// 改名前的旧名）。这里对全部停用项做一次**闭包归一**，这样「停用了 DeepSeek-Flash
// （短 key dfmodel）」在上游把它改名为 DeepSeek-V4.1-Flash 后依然生效，
// 杜绝改名绕过停用。
func (s *Service) disabledMatchTokens() map[string]bool {
	raw := s.Settings().DisabledModels
	seeds := make([]string, 0, len(raw))
	for _, d := range raw {
		seeds = append(seeds, strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(d))))
	}
	tokens := s.modelAliasClosure(seeds)
	delete(tokens, "")
	return tokens
}

// enabledModelIDs 返回对外（带前缀）且未被停用的模型 ID 列表，
// 作为已接入网关端点 models 列的权威值（与中继面 /v1/models 口径一致）。
func (s *Service) enabledModelIDs() []string {
	models := s.catalog()
	disabled := s.disabledMatchTokens()
	out := make([]string, 0, len(models))
	for _, m := range models {
		if entryEnabled([]ModelInfo{m}, disabled) {
			out = append(out, s.prefixModel(m.ID))
		}
	}
	return out
}

// isModelEnabled 判断给定模型名（对外名或上游短 key，可带前缀）是否启用。
// **白名单语义**：不在当前目录内的模型名一律视为停用（返回 false），
// 防止上游改名后旧名/任意别名绕过停用名单。停用判定按 ID 与 Key 归一。
func (s *Service) isModelEnabled(model string) bool {
	return entryEnabled(s.resolveCatalogEntries(model), s.disabledMatchTokens())
}

// entryEnabled 判断一组（同一模型跨区域命中的）目录条目是否启用：
// 任一条目的 ID 或 Key 落在停用 token 集合里即视为停用。
func entryEnabled(entries []ModelInfo, disabled map[string]bool) bool {
	if len(entries) == 0 {
		return false
	}
	for _, m := range entries {
		if disabled[strings.ToLower(m.ID)] {
			return false
		}
		if m.Key != "" && disabled[strings.ToLower(m.Key)] {
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
	// 面板读取时后台触发一次目录回源（不阻塞本次响应），目录新鲜时为空操作。
	s.refreshCatalogAsync()
	models := s.catalog()
	disabled := s.disabledMatchTokens()
	now := time.Now()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		eff, offPeak := m.MultiplierAt(now)
		out = append(out, map[string]interface{}{
			"id":                  id,
			"key":                 m.Key,
			"provider":            m.Provider,
			"creditsMultiplier":   m.CreditsMultiplier,
			"creditsParsed":       m.CreditsParsed,
			"liveMultiplier":      m.LiveMultiplier,
			"originalMultiplier":  m.OriginalMultiplier,
			"free":                m.Free,
			"priceSource":         m.PriceSource,
			"promotion":           m.Promotion,
			"effectiveMultiplier": eff,
			"offPeak":             offPeak,
			"hasOffPeak":          m.Promotion != nil && m.Promotion.Discount > 0,
			"regions":             m.Regions,
			"idRegions":           m.IDRegions,
			"internationalOnly":   m.InternationalOnly,
			"enabled":             entryEnabled([]ModelInfo{m}, disabled),
		})
	}
	responseJSON(w, map[string]interface{}{
		"success":       true,
		"upstreamReady": s.upstreamReady(),
		"offPeak":       anyOffPeak(models, now),
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
			// 启用：把停用名单里指向同一模型的所有写法一并移除（含旧名/短 key 残留）。
			aliases := s.modelAliasClosure([]string{id})
			for existing := range disabled {
				bare := strings.ToLower(strings.TrimSpace(s.stripModelPrefix(strings.TrimSpace(existing))))
				if aliases[bare] {
					delete(disabled, existing)
				}
			}
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
