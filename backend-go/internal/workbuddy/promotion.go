package workbuddy

// 本文件解析上游的限时促销（modelPromotions）与套餐门槛（modelTiers）。
//
// 上游事实（GET /v3/config 的 data.modelPromotions / data.modelTiers，
// 2026-10-20 实测）：
//
//	modelPromotions[] 里有两种条目：
//	  - 折扣类：{id, kind:"discount", enabled, priority, modelIds:[...],
//	    discount:{factor, discountedCredits, displayMode}, schedule:{timezone,
//	    validFrom, validUntil}, badge:{label,...}, hover:{textZh,...}}
//	  - 目录类（无折扣字段）：{id, name, tags, videoGeneration} —— 不是促销，
//	    不能当成「免费」，否则会把图片/视频模型错标成 0 成本。
//
//	modelTiers[] 用来标记需订阅的模型（如 Pro Only）：
//	  {id, tier:"standard", requiredUserType, modelIds:[...], badge, hover,
//	   enabled, priority}
//
// 关键点：
//   - **必须按 schedule 时间窗过滤**：实测 hy3 的免费活动写着
//     "Aug 6 – Oct 31"，一旦过期就不该继续标免费（否则用户以为还免费）。
//   - **必须按 enabled 过滤**：上游可能保留已停用的活动。
//   - 同一模型被多条活动命中时取 priority 最高的一条（上游用它表达优先级）。
//   - 折扣只影响「倍率展示」，不写进 openai_endpoints.pricing（那里是货币单价）。

import (
	"strings"
	"time"
)

// Promotion 是一条生效中的促销。
type Promotion struct {
	// ID 是活动标识（如 hy3-free-trial-202608），便于排障。
	ID string `json:"id,omitempty"`
	// Factor 是折扣系数：0 表示免费，0.5 表示五折，1 或不设表示无折扣。
	Factor float64 `json:"factor"`
	// Label 是上游徽标文案（如 "Free now"）。
	Label string `json:"label,omitempty"`
	// Text 是上游悬浮说明（中文优先）。
	Text string `json:"text,omitempty"`
	// ValidUntil 是活动结束时刻（RFC3339）；零值表示上游未给。
	ValidUntil string `json:"validUntil,omitempty"`
	// DisplayMode 是上游的展示方式（replace / strikethrough）。
	DisplayMode string `json:"displayMode,omitempty"`
}

// Free 报告该促销是否等价于免费。
func (p *Promotion) Free() bool {
	return p != nil && p.Factor == 0
}

// Tier 是模型的套餐门槛（如「仅 Pro」）。
type Tier struct {
	ID     string `json:"id,omitempty"`
	Tier   string `json:"tier,omitempty"`
	Label  string `json:"label,omitempty"`
	Text   string `json:"text,omitempty"`
	Action string `json:"action,omitempty"`
}

// upstreamPromotion 是上游 modelPromotions 条目的并集结构。
type upstreamPromotion struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Enabled  *bool    `json:"enabled"`
	Priority int      `json:"priority"`
	ModelIDs []string `json:"modelIds"`
	Discount struct {
		Factor            float64 `json:"factor"`
		DiscountedCredits string  `json:"discountedCredits"`
		DisplayMode       string  `json:"displayMode"`
	} `json:"discount"`
	Schedule struct {
		Timezone   string `json:"timezone"`
		ValidFrom  string `json:"validFrom"`
		ValidUntil string `json:"validUntil"`
	} `json:"schedule"`
	Badge struct {
		Label string `json:"label"`
	} `json:"badge"`
	Hover struct {
		TextZh string `json:"textZh"`
		TextEn string `json:"textEn"`
	} `json:"hover"`
}

// isDiscount 判断该条目是否真的是折扣活动（目录类条目没有 discount 内容）。
func (u *upstreamPromotion) isDiscount() bool {
	if strings.EqualFold(strings.TrimSpace(u.Kind), "discount") {
		return true
	}
	// kind 缺失时用字段特征兜底：有 modelIds 且给了折扣展示或时间窗才算。
	return len(u.ModelIDs) > 0 &&
		(u.Discount.DisplayMode != "" || u.Discount.DiscountedCredits != "" || u.Schedule.ValidUntil != "")
}

// activeAt 判断活动在 now 是否生效：enabled 未显式为 false、且落在时间窗内。
func (u *upstreamPromotion) activeAt(now time.Time) bool {
	if u.Enabled != nil && !*u.Enabled {
		return false
	}
	from, okFrom := parseUpstreamTime(u.Schedule.ValidFrom)
	until, okUntil := parseUpstreamTime(u.Schedule.ValidUntil)
	if okFrom && now.Before(from) {
		return false
	}
	if okUntil && !now.Before(until) {
		return false
	}
	return true
}

// toPromotion 转换为对外结构。
func (u *upstreamPromotion) toPromotion() *Promotion {
	p := &Promotion{
		ID:          strings.TrimSpace(u.ID),
		Factor:      u.Discount.Factor,
		Label:       strings.TrimSpace(u.Badge.Label),
		Text:        strings.TrimSpace(firstNonEmpty(u.Hover.TextZh, u.Hover.TextEn)),
		ValidUntil:  strings.TrimSpace(u.Schedule.ValidUntil),
		DisplayMode: strings.TrimSpace(u.Discount.DisplayMode),
	}
	return p
}

// parseUpstreamTime 解析上游时间（RFC3339，可能带 +08:00 偏移）；失败返回零值与 false。
func parseUpstreamTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	// 兼容无偏移的形式（部分上游字段只给 "2026-10-31T00:00:00"）。
	if t, err := time.Parse("2006-01-02T15:04:05", raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// upstreamTier 是上游 modelTiers 条目的并集结构。
type upstreamTier struct {
	ID       string   `json:"id"`
	Tier     string   `json:"tier"`
	Enabled  *bool    `json:"enabled"`
	Priority int      `json:"priority"`
	ModelIDs []string `json:"modelIds"`
	Badge    struct {
		Label string `json:"label"`
	} `json:"badge"`
	Hover struct {
		TextZh string `json:"textZh"`
		TextEn string `json:"textEn"`
		Action struct {
			LabelZh string `json:"labelZh"`
			LabelEn string `json:"labelEn"`
		} `json:"action"`
	} `json:"hover"`
}

// toTier 转换为对外结构（disabled 的条目返回 nil）。
func (u *upstreamTier) toTier() *Tier {
	if u == nil || (u.Enabled != nil && !*u.Enabled) {
		return nil
	}
	return &Tier{
		ID:     strings.TrimSpace(u.ID),
		Tier:   strings.TrimSpace(u.Tier),
		Label:  strings.TrimSpace(u.Badge.Label),
		Text:   strings.TrimSpace(firstNonEmpty(u.Hover.TextZh, u.Hover.TextEn)),
		Action: strings.TrimSpace(firstNonEmpty(u.Hover.Action.LabelZh, u.Hover.Action.LabelEn)),
	}
}

// buildPromotionIndex 把促销列表按模型 id 建索引，按 priority 取最高的一条。
//
// 只收生效中的折扣活动；目录类条目（图片/视频模型）会被 isDiscount 排除，
// 否则会被误标成免费。
func buildPromotionIndex(items []upstreamPromotion, now time.Time) map[string]*Promotion {
	out := map[string]*Promotion{}
	best := map[string]int{}
	for i := range items {
		u := &items[i]
		if !u.isDiscount() || !u.activeAt(now) {
			continue
		}
		for _, id := range u.ModelIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if prev, ok := best[id]; ok && prev >= u.Priority {
				continue
			}
			best[id] = u.Priority
			out[id] = u.toPromotion()
		}
	}
	return out
}

// buildTierIndex 把套餐门槛按模型 id 建索引，按 priority 取最高的一条。
func buildTierIndex(items []upstreamTier) map[string]*Tier {
	out := map[string]*Tier{}
	best := map[string]int{}
	for i := range items {
		u := &items[i]
		t := u.toTier()
		if t == nil {
			continue
		}
		for _, id := range u.ModelIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if prev, ok := best[id]; ok && prev >= u.Priority {
				continue
			}
			best[id] = u.Priority
			out[id] = t
		}
	}
	return out
}
