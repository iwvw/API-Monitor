package posthogcode

import (
	"strconv"
	"strings"
)

// 倍率基准：Claude Sonnet 5（输入 $2 / 输出 $10 每 100 万 token）。
// 与 PostHog Desktop 的展示口径一致（packages/core/src/billing/modelPricing.ts
// 的 MODEL_COST_BASELINE_NAME / BASELINE）。
const (
	baselineInputPerMtok  = 2.0
	baselineOutputPerMtok = 10.0
)

// parseRate 把上游的 USD/token 字符串转成 USD/百万 token。
// 空值或非法值返回 0。
func parseRate(v string) float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f * 1_000_000
}

// PriceInfo 是模型的价格与相对倍率。
type PriceInfo struct {
	// InputPerMtok / OutputPerMtok 单位为 USD/百万 token。
	InputPerMtok  float64 `json:"inputPerMtok"`
	OutputPerMtok float64 `json:"outputPerMtok"`
	// Multiplier 是相对基准模型的混合倍率（输入/输出比值的平均）。
	Multiplier float64 `json:"multiplier"`
	// Approximate 为真表示输入与输出比值差异超过 10%，倍率只是近似。
	Approximate bool `json:"approximate"`
}

// priceInfoFor 由上游计价表算出价格与倍率；无计价数据时返回 nil。
func priceInfoFor(p *ModelPricing) *PriceInfo {
	if p == nil {
		return nil
	}
	in := parseRate(p.Prompt)
	out := parseRate(p.Completion)
	if in <= 0 && out <= 0 {
		return nil
	}
	info := &PriceInfo{InputPerMtok: in, OutputPerMtok: out}
	// 两侧都有效才谈倍率；缺一侧时用另一侧单独比。
	switch {
	case in > 0 && out > 0:
		inRatio := in / baselineInputPerMtok
		outRatio := out / baselineOutputPerMtok
		info.Multiplier = (inRatio + outRatio) / 2
		denom := inRatio
		if outRatio > denom {
			denom = outRatio
		}
		if denom > 0 {
			info.Approximate = abs(inRatio-outRatio)/denom > 0.1
		}
	case in > 0:
		info.Multiplier = in / baselineInputPerMtok
	default:
		info.Multiplier = out / baselineOutputPerMtok
	}
	return info
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
