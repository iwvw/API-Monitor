package qoder

// 本文件实现 Qoder 的倍率模型：上游实时 price_factor + 促销（错峰）规则。
//
// 上游事实（GET /algo/api/v2/model/list 的 chat 分组，2026-10-20 实测）：
//   - price_factor：当前标价倍率。**0 是合法值**（免费模型），只有字段缺失
//     才表示「上游未提供」，因此必须用指针解析。
//   - original_price_factor：原价（限时免费模型给出，如 0.0 配原价 0.1）。
//   - is_free：上游标记的免费。
//   - promotion：促销规则，含 active / rule_id / discount_factor /
//     before_promotion_price_factor / window_start / window_end / timezone /
//     badge / link_url。
//
// **关键坑：price_factor 与 promotion 可能互相矛盾。** 实测错峰窗口内
// `qmodel_38max` 的 price_factor 已是折后值 0.2（= 0.5×0.4），而 `qmodel` 的
// price_factor 仍是原价 0.1（官方文档与 promo badge 都写明折后应为 0.04）。
// 因此生效倍率既不能直接取 price_factor，也不能无条件再乘一次折扣：
//
//   - 无促销规则：取 price_factor（上游没给则用静态标准倍率）。
//   - 有规则且不在窗口：取标准倍率（promotion.before_promotion_price_factor）。
//   - 有规则且在窗口：price_factor 已等于「标准×折扣」时直接用它，否则按
//     「标准×折扣」计算（覆盖 qmodel 那种上游没把折扣写进 price_factor 的情况）。
//
// 静态规则（staticPromotions）只在上游没下发 promotion 时兜底，保证首次拉取
// 成功前展示与历史行为一致：Qwen3.8-Max 0.5→0.2、Qwen3.7-Plus 0.1→0.04，
// 窗口 Asia/Shanghai 22:00–08:00（等价于原先写死的 UTC 14:00–24:00）。

import (
	"math"
	"strings"
	"time"
	// 促销窗口时区由上游指定（如 Asia/Shanghai），不能依赖宿主机是否装了
	// tzdata；显式内嵌时区库保证窗口判定在任何镜像里都一致。
	_ "time/tzdata"
)

// 倍率来源标记，下发前端用于区分「实时值」与「内置兜底值」。
const (
	priceLive   = "live"
	priceStatic = "static"
)

// multiplierEpsilon 是倍率比较精度：上游最多给到 6 位小数（如 0.000613272），
// 判断「price_factor 是否已经是折后值」时用 1e-9 足够。
const multiplierEpsilon = 1e-9

// Promotion 是上游下发的一条促销规则（当前只有错峰折扣
// idle_time_model_credit_discount）。
type Promotion struct {
	// RuleID 是上游规则标识（如 idle_time_model_credit_discount）。
	RuleID string `json:"ruleId,omitempty"`
	// Standard 是促销前标准倍率。
	Standard float64 `json:"standard,omitempty"`
	// Discount 是折扣系数：0.4 表示按 4 折计费。
	Discount float64 `json:"discount,omitempty"`
	// WindowStart / WindowEnd 是窗口边界（HH:MM）；跨午夜时 start > end。
	WindowStart string `json:"windowStart,omitempty"`
	WindowEnd   string `json:"windowEnd,omitempty"`
	// Timezone 是窗口所属 IANA 时区（如 Asia/Shanghai）。
	Timezone string `json:"timezone,omitempty"`
	// Badge 是上游给的短标（如「错峰 4 折」）。
	Badge string `json:"badge,omitempty"`
	// LinkURL 是官方说明页。
	LinkURL string `json:"linkUrl,omitempty"`
	// Active 是上游对该规则的生效标记，仅作展示；窗口判定以 Window* 为准。
	Active bool `json:"active,omitempty"`
}

// clone 复制一份规则，避免跨模型共享同一指针后被就地修改。
func (p *Promotion) clone() *Promotion {
	if p == nil {
		return nil
	}
	out := *p
	return &out
}

// Window 返回窗口的人类可读形式（如 "22:00–08:00"）；边界缺失时返回空串。
func (p *Promotion) Window() string {
	if p == nil {
		return ""
	}
	start, ok1 := parseClock(p.WindowStart)
	end, ok2 := parseClock(p.WindowEnd)
	if !ok1 || !ok2 || start == end {
		return ""
	}
	return p.WindowStart + "–" + p.WindowEnd
}

// OffPeakMultiplier 返回窗口内的折后倍率（标准×折扣）。无规则或折扣非法时返回 0。
func (p *Promotion) OffPeakMultiplier() float64 {
	if p == nil || p.Discount <= 0 {
		return 0
	}
	return p.Standard * p.Discount
}

// location 返回窗口时区；上游没给或宿主无法解析时退回 UTC。
func (p *Promotion) location() *time.Location {
	if p != nil {
		if name := strings.TrimSpace(p.Timezone); name != "" {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
	}
	return time.UTC
}

// InWindow 报告 now 是否落在促销窗口内（支持跨午夜窗口）。
func (p *Promotion) InWindow(now time.Time) bool {
	if p == nil || p.Discount <= 0 {
		return false
	}
	start, ok1 := parseClock(p.WindowStart)
	end, ok2 := parseClock(p.WindowEnd)
	if !ok1 || !ok2 || start == end {
		return false
	}
	t := now.In(p.location())
	cur := t.Hour()*60 + t.Minute()
	if start < end {
		return cur >= start && cur < end
	}
	// 跨午夜：22:00–08:00 表示 [22:00, 24:00) ∪ [00:00, 08:00)。
	return cur >= start || cur < end
}

// parseClock 把 "HH:MM" 解析为「当日第几分钟」。
func parseClock(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	parts := strings.SplitN(v, ":", 3)
	if len(parts) < 2 {
		return 0, false
	}
	hour, ok1 := atoiBounded(parts[0], 0, 23)
	minute, ok2 := atoiBounded(parts[1], 0, 59)
	if !ok1 || !ok2 {
		return 0, false
	}
	return hour*60 + minute, true
}

// atoiBounded 解析十进制整数并限制范围（拒绝空串、符号与非法字符）。
func atoiBounded(v string, lo, hi int) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 2 {
		return 0, false
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < lo || n > hi {
		return 0, false
	}
	return n, true
}

// nearlyEqual 比较两个倍率是否等价（容忍浮点误差）。
func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) < multiplierEpsilon
}

// MultiplierAt 返回模型在 now 时刻的生效倍率，以及是否处于促销窗口。
//
// 裁决顺序见文件头，核心是判断上游有没有把折扣算进 price_factor：
//
//  1. 实时值已等于「标准×折扣」→ 上游已折价，直接用它。
//  2. 窗口内且规则仍生效 → 按 标准×折扣 计算（覆盖上游没把折扣写进
//     price_factor 的情况，如实测 qmodel 0.1 → 0.04）。
//  3. 其余情况取实时值；上游没给实时值时才退回静态标准倍率。
//
// Active 是上游的规则生效标记：实测窗口内为 true；窗口外行为不确定，因此只在
// 「有实时值、等于标准价、且 Active=false」时才认为上游确实没在打折。
func (m ModelInfo) MultiplierAt(now time.Time) (float64, bool) {
	var live float64
	hasLive := m.LiveMultiplier != nil
	if hasLive {
		live = *m.LiveMultiplier
	}
	p := m.Promotion
	if p == nil || p.Discount <= 0 {
		if hasLive {
			return live, false
		}
		return m.CreditsMultiplier, false
	}
	inWindow := p.InWindow(now)
	discounted := p.Standard * p.Discount
	// 上游已经把折扣写进 price_factor（实测 qmodel_38max 窗口内 0.5→0.2）时，
	// 该值只对应窗口期；窗口外仍应按标准价展示，不能把陈旧折后价当标准价。
	liveIsDiscounted := hasLive && nearlyEqual(live, discounted)
	if inWindow {
		if liveIsDiscounted {
			return live, true
		}
		if p.Standard > 0 {
			// 促销已结束（active=false）且实时值回到标准价 → 不再打折。
			if hasLive && !p.Active && nearlyEqual(live, p.Standard) {
				return live, false
			}
			// 上游没把折扣写进 price_factor（实测 qmodel 0.1 而非 0.04）→ 自行计算。
			return discounted, true
		}
		if hasLive {
			return live, true
		}
		return m.CreditsMultiplier, false
	}
	if hasLive && !liveIsDiscounted {
		return live, false
	}
	if p.Standard > 0 {
		return p.Standard, false
	}
	if hasLive {
		return live, false
	}
	return m.CreditsMultiplier, false
}

// staticPromotions 是静态兜底规则（仅在动态目录未返回 promotion 时使用）。
// 阈值与上游 2026-10 实测一致：错峰窗口 Asia/Shanghai 22:00–08:00，折扣 0.4。
// Active 置 true 表示「内置规则本身是生效的」；上游下发的规则以它自己的
// Active 为准（上游可能把已结束的促销标记为 false）。
var staticPromotions = map[string]*Promotion{
	"qmodel_38max": {
		RuleID:      "idle_time_model_credit_discount",
		Standard:    0.5,
		Discount:    0.4,
		WindowStart: "22:00",
		WindowEnd:   "08:00",
		Timezone:    "Asia/Shanghai",
		Badge:       "错峰 4 折",
		Active:      true,
	},
	"qmodel": {
		RuleID:      "idle_time_model_credit_discount",
		Standard:    0.1,
		Discount:    0.4,
		WindowStart: "22:00",
		WindowEnd:   "08:00",
		Timezone:    "Asia/Shanghai",
		Badge:       "错峰 4 折",
		Active:      true,
	},
}

// staticPromotionFor 按上游短 key 返回静态兜底规则（副本）。
func staticPromotionFor(key string) *Promotion {
	return staticPromotions[strings.ToLower(strings.TrimSpace(key))].clone()
}

// anyOffPeak 报告当前是否有任一模型处于促销窗口（供面板顶部状态使用）。
func anyOffPeak(models []ModelInfo, now time.Time) bool {
	for _, m := range models {
		if _, offPeak := m.MultiplierAt(now); offPeak {
			return true
		}
	}
	return false
}
