package config

import (
	"strings"
	"time"
)

type ModelInfo struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []any  `json:"permission,omitempty"`
}
type OllamaModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}
type OllamaCapabilitiesModelInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

type ModelAliasReader interface {
	ModelAliases() map[string]string
}

const noThinkingModelSuffix = "-nothinking"

// noSearchModelSuffix 是 DeepSeek 模型家族的「关联网搜索」负向变体后缀。
// 默认模型 deepseek-flash 自带联网搜索（search_enabled=true），需要关闭时
// 使用 -nosearch / -nothinking / -nosearch-nothinking 变体。
const noSearchModelSuffix = "-nosearch"

// deepSeekBaseModels 是当前唯一真实存在的模型档位：上游（chat.deepseek.com 网页端）
// 自 V4.1-Flash 起已将「快速 / 专家 / 识图」三档合并为单一模型（model_type=default，
// 原生多模态 + 默认联网搜索），expert/vision 的请求会被上游静默降级为 default。
// 因此这里只保留默认档位，再由 appendDeepSeekVariantModels 展开四象限变体
// （±nosearch × ±nothinking）；旧名 deepseek-v4-* / pro / vision 与旧正向
// -search 后缀全部降级为别名（见 DefaultModelAliases），保证存量请求仍可解析。
var deepSeekBaseModels = []ModelInfo{
	{ID: "deepseek-flash", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
}

var OllamaCapabilitiesModels = []OllamaCapabilitiesModelInfo{
	{ID: "deepseek-flash", Capabilities: []string{"tools", "thinking", "search", "vision"}},
	{ID: "deepseek-flash-nosearch", Capabilities: []string{"tools", "thinking", "vision"}},
	{ID: "deepseek-flash-nothinking", Capabilities: []string{"tools", "search", "vision"}},
	{ID: "deepseek-flash-nosearch-nothinking", Capabilities: []string{"tools", "vision"}},
}

var DeepSeekModels = appendDeepSeekVariantModels(deepSeekBaseModels)
var OllamaModels = mapToOllamaModels(DeepSeekModels)
var claudeBaseModels = []ModelInfo{
	// Current aliases
	{ID: "claude-opus-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 4.x snapshots and prior aliases kept for compatibility
	{ID: "claude-sonnet-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1-20250805", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5-20250929", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5-20251001", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 3.x (legacy/deprecated snapshots and aliases)
	{ID: "claude-3-7-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-7-sonnet-20250219", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20240620", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-opus-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-haiku-20240307", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
}

var ClaudeModels = appendNoThinkingVariants(claudeBaseModels)

func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	baseModel, noSearch, noThinking := splitVariantModel(model)
	if baseModel == "" {
		return false, false, false
	}
	switch baseModel {
	case "deepseek-flash":
		return !noThinking, !noSearch, true
	default:
		return false, false, false
	}
}

func GetModelType(model string) (modelType string, ok bool) {
	baseModel, _, _ := splitVariantModel(model)
	switch baseModel {
	case "deepseek-flash":
		return "default", true
	default:
		return "", false
	}
}

func IsSupportedDeepSeekModel(model string) bool {
	_, _, ok := GetModelConfig(model)
	return ok
}

func IsNoThinkingModel(model string) bool {
	_, noThinking := splitNoThinkingModel(model)
	return noThinking
}

func DefaultModelAliases() map[string]string {
	return map[string]string{
		// 官网现行名：V4.1-Flash（deepseek-flash），默认自带联网搜索与思考，
		// 需要关闭时使用 -nosearch / -nothinking 变体。
		"deepseek-flash": "deepseek-flash",
		// 旧正向 -search 后缀名（V4.1 过渡期曾用）收编为默认：搜索现在默认开启。
		"deepseek-flash-search":            "deepseek-flash",
		"deepseek-flash-search-nothinking": "deepseek-flash-nothinking",
		"deepseek-v4-flash-vision-exp":     "deepseek-flash",

		// 旧名（deepseek-v4-*）与已退役档位（pro / vision）统一收编为 flash：
		// 上游请求会被静默降级到 default，保留别名仅为兼容存量调用。
		"deepseek-v4-flash":                    "deepseek-flash",
		"deepseek-v4-flash-search":             "deepseek-flash",
		"deepseek-v4-pro":                      "deepseek-flash",
		"deepseek-v4-pro-search":               "deepseek-flash",
		"deepseek-v4-vision":                   "deepseek-flash",
		"deepseek-v4-vision-search":            "deepseek-flash",
		"deepseek-v4-flash-nothinking":         "deepseek-flash-nothinking",
		"deepseek-v4-flash-search-nothinking":  "deepseek-flash-nothinking",
		"deepseek-v4-pro-nothinking":           "deepseek-flash-nothinking",
		"deepseek-v4-pro-search-nothinking":    "deepseek-flash-nothinking",
		"deepseek-v4-vision-nothinking":        "deepseek-flash-nothinking",
		"deepseek-v4-vision-search-nothinking": "deepseek-flash-nothinking",

		// DeepSeek 旧接口名（deepseek-chat / deepseek-reasoner 已退役，故意不接收）
		"chatgpt-4o":          "deepseek-flash",
		"gpt-4":               "deepseek-flash",
		"gpt-4-turbo":         "deepseek-flash",
		"gpt-4-turbo-preview": "deepseek-flash",
		"gpt-4.5-preview":     "deepseek-flash",
		"gpt-4o":              "deepseek-flash",
		"gpt-4o-mini":         "deepseek-flash",
		"gpt-4.1":             "deepseek-flash",
		"gpt-4.1-mini":        "deepseek-flash",
		"gpt-4.1-nano":        "deepseek-flash",
		"gpt-5":               "deepseek-flash",
		"gpt-5-chat":          "deepseek-flash",
		"gpt-5.1":             "deepseek-flash",
		"gpt-5.1-chat":        "deepseek-flash",
		"gpt-5.2":             "deepseek-flash",
		"gpt-5.2-chat":        "deepseek-flash",
		"gpt-5.3-chat":        "deepseek-flash",
		"gpt-5.4":             "deepseek-flash",
		"gpt-5.5":             "deepseek-flash",
		"gpt-5-mini":          "deepseek-flash",
		"gpt-5-nano":          "deepseek-flash",
		"gpt-5.4-mini":        "deepseek-flash",
		"gpt-5.4-nano":        "deepseek-flash",
		"gpt-5-pro":           "deepseek-flash",
		"gpt-5.2-pro":         "deepseek-flash",
		"gpt-5.4-pro":         "deepseek-flash",
		"gpt-5.5-pro":         "deepseek-flash",
		"gpt-5-codex":         "deepseek-flash",
		"gpt-5.1-codex":       "deepseek-flash",
		"gpt-5.1-codex-mini":  "deepseek-flash",
		"gpt-5.1-codex-max":   "deepseek-flash",
		"gpt-5.2-codex":       "deepseek-flash",
		"gpt-5.3-codex":       "deepseek-flash",
		"codex-mini-latest":   "deepseek-flash",

		// OpenAI reasoning / research families
		"o1":                    "deepseek-flash",
		"o1-preview":            "deepseek-flash",
		"o1-mini":               "deepseek-flash",
		"o1-pro":                "deepseek-flash",
		"o3":                    "deepseek-flash",
		"o3-mini":               "deepseek-flash",
		"o3-pro":                "deepseek-flash",
		"o3-deep-research":      "deepseek-flash",
		"o4-mini":               "deepseek-flash",
		"o4-mini-deep-research": "deepseek-flash",

		// Claude current and historical aliases
		"claude-opus-4-6":            "deepseek-flash",
		"claude-opus-4-1":            "deepseek-flash",
		"claude-opus-4-1-20250805":   "deepseek-flash",
		"claude-opus-4-0":            "deepseek-flash",
		"claude-opus-4-20250514":     "deepseek-flash",
		"claude-sonnet-4-6":          "deepseek-flash",
		"claude-sonnet-4-5":          "deepseek-flash",
		"claude-sonnet-4-5-20250929": "deepseek-flash",
		"claude-sonnet-4-0":          "deepseek-flash",
		"claude-sonnet-4-20250514":   "deepseek-flash",
		"claude-haiku-4-5":           "deepseek-flash",
		"claude-haiku-4-5-20251001":  "deepseek-flash",
		"claude-3-7-sonnet":          "deepseek-flash",
		"claude-3-7-sonnet-latest":   "deepseek-flash",
		"claude-3-7-sonnet-20250219": "deepseek-flash",
		"claude-3-5-sonnet":          "deepseek-flash",
		"claude-3-5-sonnet-latest":   "deepseek-flash",
		"claude-3-5-sonnet-20240620": "deepseek-flash",
		"claude-3-5-sonnet-20241022": "deepseek-flash",
		"claude-3-5-haiku":           "deepseek-flash",
		"claude-3-5-haiku-latest":    "deepseek-flash",
		"claude-3-5-haiku-20241022":  "deepseek-flash",
		"claude-3-opus":              "deepseek-flash",
		"claude-3-opus-20240229":     "deepseek-flash",
		"claude-3-sonnet":            "deepseek-flash",
		"claude-3-sonnet-20240229":   "deepseek-flash",
		"claude-3-haiku":             "deepseek-flash",
		"claude-3-haiku-20240307":    "deepseek-flash",

		// Gemini current and historical text / multimodal models
		"gemini-pro":            "deepseek-flash",
		"gemini-pro-vision":     "deepseek-flash",
		"gemini-pro-latest":     "deepseek-flash",
		"gemini-flash-latest":   "deepseek-flash",
		"gemini-1.5-pro":        "deepseek-flash",
		"gemini-1.5-flash":      "deepseek-flash",
		"gemini-1.5-flash-8b":   "deepseek-flash",
		"gemini-2.0-flash":      "deepseek-flash",
		"gemini-2.0-flash-lite": "deepseek-flash",
		"gemini-2.5-pro":        "deepseek-flash",
		"gemini-2.5-flash":      "deepseek-flash",
		"gemini-2.5-flash-lite": "deepseek-flash",
		"gemini-3.1-pro":        "deepseek-flash",
		"gemini-3-pro":          "deepseek-flash",
		"gemini-3-flash":        "deepseek-flash",
		"gemini-3.1-flash":      "deepseek-flash",
		"gemini-3.1-flash-lite": "deepseek-flash",

		"llama-3.1-70b-instruct": "deepseek-flash",
		"qwen-max":               "deepseek-flash",
	}
}

func ResolveModel(store ModelAliasReader, requested string) (string, bool) {
	model := lower(strings.TrimSpace(requested))
	if model == "" {
		return "", false
	}
	aliases := loadModelAliases(store)
	if IsSupportedDeepSeekModel(model) {
		return model, true
	}
	// 先按完整名字查别名；命中值可能仍是旧名（如 deepseek-v4-vision），
	// 继续沿别名链收敛到合并后的现行模型。
	if mapped, ok := followAliasChain(aliases, model); ok && IsSupportedDeepSeekModel(mapped) {
		return mapped, true
	}
	baseModel, noSearch, noThinking := splitVariantModel(model)
	if mapped, ok := followAliasChain(aliases, baseModel); ok && IsSupportedDeepSeekModel(mapped) {
		return withVariantModel(mapped, noSearch, noThinking), true
	}
	return "", false
}

// followAliasChain 沿别名链查找（限深，防环）：deepseek-v4-vision →
// deepseek-flash 这类「旧名→现行名」的收编映射也能被自定义别名命中。
func followAliasChain(aliases map[string]string, name string) (string, bool) {
	cur := lower(strings.TrimSpace(name))
	for depth := 0; depth < 4; depth++ {
		next, ok := aliases[cur]
		if !ok {
			return cur, false
		}
		cur = lower(strings.TrimSpace(next))
		if IsSupportedDeepSeekModel(cur) {
			return cur, true
		}
	}
	return "", false
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": DeepSeekModels}
}

func OpenAIModelByID(store ModelAliasReader, id string) (ModelInfo, bool) {
	canonical, ok := ResolveModel(store, id)
	if !ok {
		return ModelInfo{}, false
	}
	for _, model := range DeepSeekModels {
		if model.ID == canonical {
			return model, true
		}
	}
	return ModelInfo{}, false
}

func OllamaModelsResponse() map[string]any {
	return map[string]any{"models": OllamaModels}
}

func OllamaModelByID(store ModelAliasReader, id string) (OllamaCapabilitiesModelInfo, bool) {
	canonical, ok := ResolveModel(store, id)
	if !ok {
		return OllamaCapabilitiesModelInfo{}, false
	}
	for _, model := range OllamaCapabilitiesModels {
		if model.ID == canonical {
			return model, true
		}
	}
	return OllamaCapabilitiesModelInfo{}, false
}

func ClaudeModelsResponse() map[string]any {
	resp := map[string]any{"object": "list", "data": ClaudeModels}
	if len(ClaudeModels) > 0 {
		resp["first_id"] = ClaudeModels[0].ID
		resp["last_id"] = ClaudeModels[len(ClaudeModels)-1].ID
	} else {
		resp["first_id"] = nil
		resp["last_id"] = nil
	}
	resp["has_more"] = false
	return resp
}

func appendNoThinkingVariants(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models)*2)
	for _, model := range models {
		out = append(out, model)
		variant := model
		variant.ID = withNoThinkingVariant(model.ID, true)
		out = append(out, variant)
	}
	return out
}
func mapToOllamaModels(models []ModelInfo) []OllamaModelInfo {
	out := make([]OllamaModelInfo, 0, len(models))
	for _, model := range models {
		var modifiedAt string
		if model.Created > 0 {
			modifiedAt = time.Unix(model.Created, 0).Format(time.RFC3339)
		}
		ollamaModel := OllamaModelInfo{
			Name:       model.ID,
			Model:      model.ID,
			Size:       0,
			ModifiedAt: modifiedAt,
		}
		out = append(out, ollamaModel)
	}
	return out
}

func splitNoThinkingModel(model string) (string, bool) {
	model = lower(strings.TrimSpace(model))
	if strings.HasSuffix(model, noThinkingModelSuffix) {
		return strings.TrimSuffix(model, noThinkingModelSuffix), true
	}
	return model, false
}

func withNoThinkingVariant(model string, enabled bool) string {
	baseModel, _ := splitNoThinkingModel(model)
	if !enabled {
		return baseModel
	}
	if baseModel == "" {
		return ""
	}
	return baseModel + noThinkingModelSuffix
}

// appendDeepSeekVariantModels expands the default model into the four-quadrant
// variant matrix used by the DeepSeek family. The default model already carries
// search + thinking; variants disable them via explicit negative suffixes:
//
//	deepseek-flash                        (search on,  thinking on)
//	deepseek-flash-nosearch               (search off, thinking on)
//	deepseek-flash-nothinking             (search on,  thinking off)
//	deepseek-flash-nosearch-nothinking    (search off, thinking off)
func appendDeepSeekVariantModels(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models)*4)
	for _, model := range models {
		out = append(out, model)
		noSearch := model
		noSearch.ID = model.ID + noSearchModelSuffix
		out = append(out, noSearch)
		noThinking := model
		noThinking.ID = model.ID + noThinkingModelSuffix
		out = append(out, noThinking)
		both := model
		both.ID = model.ID + noSearchModelSuffix + noThinkingModelSuffix
		out = append(out, both)
	}
	return out
}

// splitVariantModel parses the DeepSeek negative-variant suffixes off a model
// name. The suffix order is fixed: -nosearch before -nothinking
// (deepseek-flash-nosearch-nothinking). It returns the base model name plus the
// two disable flags.
func splitVariantModel(model string) (base string, noSearch, noThinking bool) {
	m := lower(strings.TrimSpace(model))
	noThinking = strings.HasSuffix(m, noThinkingModelSuffix)
	if noThinking {
		m = strings.TrimSuffix(m, noThinkingModelSuffix)
	}
	noSearch = strings.HasSuffix(m, noSearchModelSuffix)
	if noSearch {
		m = strings.TrimSuffix(m, noSearchModelSuffix)
	}
	return m, noSearch, noThinking
}

// withVariantModel applies the two negative-variant flags to a model name,
// normalizing any flags already present on the base name first.
func withVariantModel(model string, noSearch, noThinking bool) string {
	base, _, _ := splitVariantModel(model)
	if base == "" {
		return ""
	}
	if noSearch {
		base += noSearchModelSuffix
	}
	if noThinking {
		base += noThinkingModelSuffix
	}
	return base
}

func loadModelAliases(store ModelAliasReader) map[string]string {
	aliases := DefaultModelAliases()
	if store != nil {
		for k, v := range store.ModelAliases() {
			aliases[lower(strings.TrimSpace(k))] = lower(strings.TrimSpace(v))
		}
	}
	return aliases
}
