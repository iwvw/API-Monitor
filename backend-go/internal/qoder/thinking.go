package qoder

// 本文件实现「思考强度（thinking / reasoning effort）」的目录解析与请求映射。
//
// 上游事实（GET /algo/api/v2/model/list 的 chat 分组，2026-10-20 实测）：
//   - thinking_config.disabled：存在即表示上游允许关闭思考。
//   - thinking_config.enabled.efforts：该模型发布的强度档位（对象键），如
//     {"xhigh":{}, "low":{}, "medium":{"is_default":true}}。
//   - 部分模型只有 enabled/disabled 而无 efforts（如 Qwen3.7-Max），即只能开关。
//   - 少数模型（Auto / Qwen3.7-Flash / MiniMax）完全没有 thinking_config，但
//     is_reasoning 仍可能为 true —— 此时只有「开/关」，没有强度。
//   - 请求侧：强度写进 parameters.reasoning_effort，开关写进
//     model_config.is_reasoning（以及 chat_context.extra.modelConfig.is_reasoning）。
//     依据开源反代 CLIProxyAPIPlus 的 qoder_executor.go：parameters.reasoning_effort
//     是 Qoder chat API 实际读取的槽位。
//
// 调用方可能给 OpenAI 的 reasoning_effort（minimal/low/medium/high/xhigh）或
// none/off。这里统一归一为上游发布的档位：不足时向上取最近一档，超过上限取最高档。
// 为什么向上取：模型没发布 low 而只有 high/max 时，按 low 请求若静默降到「不开思考」
// 会明显损失质量；向上取是更安全的降级方向。

import (
	"strings"
)

// effortOrder 是强度档位的强弱顺序（升序）。"none" 单独表示关闭。
var effortOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// effortRank 返回档位强弱序号；未知档位返回 -1。
func effortRank(level string) int {
	level = strings.ToLower(strings.TrimSpace(level))
	for i, name := range effortOrder {
		if name == level {
			return i
		}
	}
	return -1
}

// isDisableEffort 报告该取值是否表示「关闭思考」。
func isDisableEffort(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "none", "off", "disabled", "false":
		return true
	}
	return false
}

// ThinkingConfig 是模型的思考能力描述（由上游 thinking_config 解析而来）。
type ThinkingConfig struct {
	// CanDisable 表示上游允许关闭思考（thinking_config.disabled 存在）。
	CanDisable bool `json:"canDisable,omitempty"`
	// DefaultDisabled 表示上游默认关闭思考。
	DefaultDisabled bool `json:"defaultDisabled,omitempty"`
	// Efforts 是上游发布的强度档位，按强弱升序（如 low/medium/xhigh）。
	Efforts []string `json:"efforts,omitempty"`
	// DefaultEffort 是上游默认强度（无 efforts 时为空）。
	DefaultEffort string `json:"defaultEffort,omitempty"`
}

// Supported 报告该模型是否有任何思考能力（无能力时不应写入思考参数）。
func (t *ThinkingConfig) Supported() bool {
	if t == nil {
		return false
	}
	return t.CanDisable || len(t.Efforts) > 0
}

// Resolve 把调用方给的 OpenAI 风格取值归一为上游可接受的档位。
//
// 返回 (level, disable, hasLevel)：
//   - disable=true 表示应关闭思考（level 为空）。
//   - hasLevel=false 表示该模型没有强度档位（只有开关，或完全不支持思考），
//     调用方应只处理 is_reasoning。
//
// requested 为空时返回上游默认档位；模型不支持思考时返回 (\"\", false, false)。
func (t *ThinkingConfig) Resolve(requested string) (level string, disable bool, hasLevel bool) {
	if t == nil {
		return "", false, false
	}
	requested = strings.ToLower(strings.TrimSpace(requested))
	if isDisableEffort(requested) {
		// 上游明确允许关闭时按关闭处理；不允许则保持默认（不能假装关掉）。
		if t.CanDisable {
			return "", true, false
		}
		return "", false, false
	}
	if len(t.Efforts) == 0 {
		// 无档位：只有开关，按默认开启。
		return "", false, false
	}
	if requested == "" {
		if t.DefaultEffort != "" {
			return t.DefaultEffort, false, true
		}
		return t.Efforts[len(t.Efforts)-1], false, true
	}
	want := effortRank(requested)
	if want < 0 {
		// 未知取值（如 OpenAI 的 "minimal" 之外的私有档位）：用默认档位。
		if t.DefaultEffort != "" {
			return t.DefaultEffort, false, true
		}
		return t.Efforts[0], false, true
	}
	// 向上取最近一档；超过上限取最高档。
	for _, candidate := range t.Efforts {
		if effortRank(candidate) >= want {
			return candidate, false, true
		}
	}
	return t.Efforts[len(t.Efforts)-1], false, true
}

// DefaultLevel 返回上游默认档位（无档位时为空）。
func (t *ThinkingConfig) DefaultLevel() string {
	if t == nil {
		return ""
	}
	if t.DefaultEffort != "" {
		return t.DefaultEffort
	}
	if len(t.Efforts) > 0 {
		return t.Efforts[len(t.Efforts)-1]
	}
	return ""
}

// upstreamThinkingConfig 是上游 thinking_config 的原始结构。
type upstreamThinkingConfig struct {
	Disabled *struct {
		Description string `json:"description"`
		IsDefault   bool   `json:"is_default"`
	} `json:"disabled"`
	Enabled *struct {
		Description string `json:"description"`
		IsDefault   bool   `json:"is_default"`
		Efforts     map[string]struct {
			Description string `json:"description"`
			IsDefault   bool   `json:"is_default"`
		} `json:"efforts"`
	} `json:"enabled"`
}

// toThinkingConfig 把上游结构转成对外的 ThinkingConfig；无思考能力时返回 nil。
func (u *upstreamThinkingConfig) toThinkingConfig() *ThinkingConfig {
	if u == nil {
		return nil
	}
	out := &ThinkingConfig{CanDisable: u.Disabled != nil}
	if u.Disabled != nil && u.Disabled.IsDefault {
		out.DefaultDisabled = true
	}
	if u.Enabled != nil {
		if len(u.Enabled.Efforts) > 0 {
			// 按 effortOrder 排序，保证档位强弱有序（上游 JSON 对象无序）。
			published := make(map[string]bool, len(u.Enabled.Efforts))
			for name := range u.Enabled.Efforts {
				published[strings.ToLower(strings.TrimSpace(name))] = true
			}
			for _, name := range effortOrder {
				if published[name] {
					out.Efforts = append(out.Efforts, name)
				}
			}
			for name, item := range u.Enabled.Efforts {
				if item.IsDefault {
					out.DefaultEffort = strings.ToLower(strings.TrimSpace(name))
					break
				}
			}
			if out.DefaultEffort == "" && len(out.Efforts) > 0 {
				out.DefaultEffort = out.Efforts[len(out.Efforts)-1]
			}
		}
	}
	if !out.CanDisable && len(out.Efforts) == 0 {
		// thinking_config 存在但既不能关也没档位：视为「恒开、无强度可调」，
		// 仍返回非 nil 以便请求侧把 is_reasoning 置 true。
		if u.Enabled == nil {
			return nil
		}
	}
	return out
}
