package antigravity

// Antigravity 模型目录与单模型额度。
//
// 运行时模型列表以 fetchAvailableModels 为准；这里的静态目录是上游不可达时的
// 兜底，同时为请求侧提供 per-model 的 maxOutputTokens 上限与 thinking 档位。
//
// 数据来源：CLIProxyAPI 的 internal/registry/models/models.json（antigravity 段）。
// 上游一旦调整模型清单，这里需要同步，否则会出现「目录里有、上游已下线」或者
// 「上游给了更大额度、我们按旧上限截断」两类问题。

import "strings"

// modelLimit 描述单模型的输出额度与 thinking 约束。
type modelLimit struct {
	// MaxCompletionTokens 是上游允许的 maxOutputTokens 上限。
	MaxCompletionTokens int
	// ThinkingMin / ThinkingMax 是该模型可用的 thinking budget 区间。
	// 0 表示该模型不提供 thinking 配置。
	ThinkingMin int
	ThinkingMax int
	// ThinkingLevels 非空表示该模型使用档位（low/medium/high）而非预算值。
	ThinkingLevels []string
	// SupportsWebSearch 表示模型原生支持 googleSearch 工具。
	SupportsWebSearch bool
}

// antigravityModelLimits 是模型 → 额度/能力表。
//
// 键为模型 ID 前缀匹配（按最长前缀命中），以便带后缀的变体
// （如 claude-opus-4-6-thinking-20260205）也能落到同一行。
var antigravityModelLimits = map[string]modelLimit{
	"claude-fable-5": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-opus-5-5-high": {
		MaxCompletionTokens: 128000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-sonnet-5-5-high": {
		MaxCompletionTokens: 128000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-opus-4-8": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-opus-4-7": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-opus-4-6": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-sonnet-4-6": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-opus-4-5": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"claude-sonnet-4-5": {
		MaxCompletionTokens: 64000,
		ThinkingMin:         1024,
		ThinkingMax:         64000,
	},
	"gemini-4-argon": {
		MaxCompletionTokens: 1048576,
		ThinkingMin:         1,
		ThinkingMax:         65535,
		ThinkingLevels:      []string{"low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.8-flash": {
		MaxCompletionTokens: 65536,
		ThinkingMin:         1,
		ThinkingMax:         65535,
		ThinkingLevels:      []string{"low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.6-flash": {
		MaxCompletionTokens: 65536,
		ThinkingMin:         1,
		ThinkingMax:         65535,
		ThinkingLevels:      []string{"minimal", "low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.5-flash-lite": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         1,
		ThinkingMax:         65535,
		ThinkingLevels:      []string{"minimal", "low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.1-flash-image": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         128,
		ThinkingMax:         32768,
		ThinkingLevels:      []string{"minimal", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.1-flash-lite": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         1,
		ThinkingMax:         65535,
		ThinkingLevels:      []string{"minimal", "low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-3.1-pro": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         128,
		ThinkingMax:         32768,
	},
	"gemini-3-pro": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         128,
		ThinkingMax:         32768,
		SupportsWebSearch:   true,
	},
	"gemini-3-flash": {
		MaxCompletionTokens: 65536,
		ThinkingMin:         128,
		ThinkingMax:         32768,
		ThinkingLevels:      []string{"minimal", "low", "medium", "high"},
		SupportsWebSearch:   true,
	},
	"gemini-2.5-flash": {
		MaxCompletionTokens: 65535,
		ThinkingMin:         0,
		ThinkingMax:         Gemini25FlashThinkingBudgetLimit,
	},
	"gpt-oss-120b": {
		MaxCompletionTokens: 32768,
	},
}

// LookupModelLimit 按最长前缀匹配返回模型的额度约束。
func LookupModelLimit(modelID string) (modelLimit, bool) {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	if modelID == "" {
		return modelLimit{}, false
	}
	var (
		best  modelLimit
		bestN int
		found bool
	)
	for prefix, limit := range antigravityModelLimits {
		if !strings.HasPrefix(modelID, prefix) {
			continue
		}
		if len(prefix) > bestN {
			best, bestN, found = limit, len(prefix), true
		}
	}
	return best, found
}

// exposedModelIDs 是允许对外暴露的模型 ID 集合（小写）。
//
// 上游 fetchAvailableModels 会返回大量内部实验代号（例如
// tab_jump_flash_lite_preview），直接透传会把它们当成正式模型名暴露给客户端，
// 既容易误用也会随上游灰度随时消失。因此对外模型 = 静态目录 ∩ 上游返回，
// 与 CLIProxyAPI 的 filterAntigravityModels 语义一致。
var exposedModelIDs = func() map[string]struct{} {
	ids := make(map[string]struct{})
	add := func(list []modelDef) {
		for _, m := range list {
			ids[strings.ToLower(m.ID)] = struct{}{}
		}
	}
	add(claudeModels)
	add(geminiModels)
	add(ossModels)
	return ids
}()

// IsExposedModel 判断模型 ID 是否属于对外暴露的白名单。
func IsExposedModel(modelID string) bool {
	_, ok := exposedModelIDs[strings.ToLower(strings.TrimSpace(modelID))]
	return ok
}

// FilterExposedModels 从上游返回的模型表中只保留白名单内的条目。
//
// 返回的 map 是新 map，不修改入参。上游若返回白名单之外的 ID（内部实验代号），
// 会被丢弃；白名单里但上游没返回的模型也不会凭空出现。
func FilterExposedModels(models map[string]ModelInfo) map[string]ModelInfo {
	if len(models) == 0 {
		return models
	}
	filtered := make(map[string]ModelInfo, len(models))
	for id, info := range models {
		if !IsExposedModel(id) {
			continue
		}
		filtered[id] = info
	}
	return filtered
}

// MaxCompletionTokensFor 返回模型允许的最大输出 tokens。
// 未知模型回落到历史默认值，避免改变既有行为。
func MaxCompletionTokensFor(modelID string) int {
	if limit, ok := LookupModelLimit(modelID); ok && limit.MaxCompletionTokens > 0 {
		return limit.MaxCompletionTokens
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "claude-") {
		return maxOutputTokensClaude
	}
	return maxOutputTokensUpperBound
}

// clampThinkingBudget 按模型能力收紧 thinking budget。
//
// 返回 (budget, keep)：keep=false 表示该模型的 thinking 配置应整体移除
// （budget 低于模型最小值，上游会以 400 拒绝）。
func clampThinkingBudget(modelID string, budget int) (int, bool) {
	limit, ok := LookupModelLimit(modelID)
	if !ok {
		return budget, true
	}
	// 负数表示「动态预算」，由上游决定，不做区间裁剪。
	if budget < 0 {
		return budget, true
	}
	if limit.ThinkingMin > 0 && budget > 0 && budget < limit.ThinkingMin {
		return 0, false
	}
	if limit.ThinkingMax > 0 && budget > limit.ThinkingMax {
		budget = limit.ThinkingMax
	}
	return budget, true
}
