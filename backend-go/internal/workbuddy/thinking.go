package workbuddy

// 本文件实现 CodeBuddy / WorkBuddy 的思考强度（reasoning effort）解析与归一。
//
// 上游事实（GET /v3/config 的 models[]，2026-10-20 实测三区域）：
//   reasoning 字段有三种形态：
//     1. {"effort":"medium","summary":"auto"}                    —— 只给默认档
//     2. {"canDisableThinking":true,"defaultEffort":"high",
//         "summary":"auto","supportedEfforts":["low","high","max"]} —— 完整能力
//     3. {"defaultEffort":"high","supportedEfforts":[...]}        —— 无 canDisable（视为不可关）
//   缺省（无 reasoning 字段）表示该模型不参与思考档位（多为非推理模型或补全模型）。
//
// 与 Qoder 的差异：
//   - WorkBuddy 直接给 effort 名字（low/medium/high/xhigh/max），语义与 OpenAI
//     的 reasoning_effort 基本同构，只需在同一空间内归一；
//   - 形态一没有 supportedEfforts，说明「上游只接受这个默认档」——此时不能
//     自行推算档位，只能原样用 effort。这条是早先 forceMaxThinking 的由来：
//     混元 hy3/hy4 曾只认 high，故被硬编码成 high。
//
// 实测 2026-10 的混元已经变了：wb/intl 的 hy3 给出 supportedEfforts
// ["low","high"]，hy4-preview 给出 ["high"]。所以原「无条件强制 high」的做法
// 会把用户明确要的 low 改掉，是有损的；现在改为按 supportedEfforts 归一，
// 仅当上游真的只给 high 时才落到 high（结果与旧行为一致，但不再覆盖合法档位）。

import (
	"strings"
)

// effortOrder 是档位强弱顺序（升序）。与 OpenAI 的 reasoning_effort 空间一致，
// 额外包含上游出现过的 low/medium/high/xhigh/max。
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

// ThinkingConfig 是模型的思考能力描述（由上游 reasoning 字段解析而来）。
type ThinkingConfig struct {
	// CanDisable 表示上游允许关闭思考（canDisableThinking=true）。
	CanDisable bool `json:"canDisable,omitempty"`
	// DefaultEffort 是上游默认档位。
	DefaultEffort string `json:"defaultEffort,omitempty"`
	// SupportsEfforts 是上游支持的档位（升序）；为空表示上游未声明档位列表。
	SupportsEfforts []string `json:"supportedEfforts,omitempty"`
	// Summary 是上游的思考摘要偏好（多为 "auto"）。
	Summary string `json:"summary,omitempty"`
}

// Supported 报告该模型是否有思考能力信息。
func (t *ThinkingConfig) Supported() bool {
	if t == nil {
		return false
	}
	return t.CanDisable || t.DefaultEffort != "" || len(t.SupportsEfforts) > 0
}

// Resolve 把调用方的 reasoning_effort 归一为上游可接受的值。
//
// 返回 (effort, disable)：
//   - disable=true 表示应关闭思考（effort 为空）；仅当上游 CanDisable 才生效；
//   - effort 为空表示不写入 reasoning_effort（交给上游默认）。
//
// 归一规则（按上游是否声明 supportedEfforts 分两种）：
//
//	A) 有 supportedEfforts：把请求档位映射到「不小于请求的最低可用档」；
//	   高于全部档位时取最高档；低于全部档位时取最低档。
//	B) 没有 supportedEfforts（只给 effort 默认值）：上游只接受该档，
//	   无论请求什么都用 DefaultEffort —— 这正是混合系列旧行为，
//	   但现在只在这一种情形下才覆盖，不再从前缀硬判。
func (t *ThinkingConfig) Resolve(requested string) (effort string, disable bool) {
	if t == nil {
		return "", false
	}
	requested = strings.ToLower(strings.TrimSpace(requested))
	if isDisableEffort(requested) {
		if t.CanDisable {
			return "", true
		}
		// 不能关：落到默认档（不假装关掉）。
		return t.DefaultEffort, false
	}
	if len(t.SupportsEfforts) == 0 {
		// 情形 B：上游只接受默认档。
		return t.DefaultEffort, false
	}
	if requested == "" {
		if t.DefaultEffort != "" {
			return t.DefaultEffort, false
		}
		return t.SupportsEfforts[len(t.SupportsEfforts)-1], false
	}
	want := effortRank(requested)
	if want < 0 {
		// 未知取值：用默认档，避免把无效值透传给上游。
		if t.DefaultEffort != "" {
			return t.DefaultEffort, false
		}
		return t.SupportsEfforts[0], false
	}
	for _, candidate := range t.SupportsEfforts {
		if effortRank(candidate) >= want {
			return candidate, false
		}
	}
	return t.SupportsEfforts[len(t.SupportsEfforts)-1], false
}

// upstreamReasoning 是上游 reasoning 字段的原始结构（三种形态的并集）。
type upstreamReasoning struct {
	Effort             string   `json:"effort"`
	Summary            string   `json:"summary"`
	CanDisableThinking *bool    `json:"canDisableThinking"`
	DefaultEffort      string   `json:"defaultEffort"`
	SupportedEfforts   []string `json:"supportedEfforts"`
}

// toThinkingConfig 把上游结构转成 ThinkingConfig；无思考信息时返回 nil。
func (u *upstreamReasoning) toThinkingConfig() *ThinkingConfig {
	if u == nil {
		return nil
	}
	out := &ThinkingConfig{
		DefaultEffort: strings.ToLower(strings.TrimSpace(firstNonEmpty(u.DefaultEffort, u.Effort))),
		Summary:       strings.TrimSpace(u.Summary),
	}
	if u.CanDisableThinking != nil {
		out.CanDisable = *u.CanDisableThinking
	}
	// 档位列表按强弱排序（上游顺序不可靠），并去重。
	seen := map[string]bool{}
	for _, raw := range u.SupportedEfforts {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out.SupportsEfforts = append(out.SupportsEfforts, name)
	}
	if len(out.SupportsEfforts) > 0 {
		sortEfforts(out.SupportsEfforts)
	}
	if !out.Supported() {
		return nil
	}
	// 有档位但默认档不在列表里时，用最高档兜底，避免写出上游不认的值。
	if out.DefaultEffort != "" && len(out.SupportsEfforts) > 0 && effortRank(out.DefaultEffort) < 0 {
		out.DefaultEffort = out.SupportsEfforts[len(out.SupportsEfforts)-1]
	}
	return out
}

// sortEfforts 按 effortOrder 把档位升序排列（未知档位排在末尾，保持相对顺序）。
func sortEfforts(items []string) {
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			ri, rj := effortRank(items[i]), effortRank(items[j])
			if ri < 0 {
				ri = len(effortOrder) + 1
			}
			if rj < 0 {
				rj = len(effortOrder) + 1
			}
			if rj < ri {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}
