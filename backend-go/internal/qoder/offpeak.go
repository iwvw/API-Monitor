package qoder

// 本文件实现 Qoder 的「错峰折扣」（峰谷计费）规则与当前生效倍率计算。
//
// 官方规则（docs.qoder.com/events/offpeakrate）：
//   - 错峰窗口以 **UTC** 为准：每日 14:00–00:00 UTC（本地北京时间 22:00–08:00）；
//     常规时段 00:00–14:00 UTC。
//   - 仅以下模型享错峰折扣，其余模型全程标准倍率：
//       Qwen3.8-Max (qmodel_38max)：标准 0.5x，错峰 0.2x（6 折）
//       Qwen3.7-Plus (qmodel)     ：标准 0.1x，错峰 0.04x（6 折）
//   - 折扣自动生效，无需开关；服务账号错峰窗口为 17:00–23:00 UTC。
//
// 注意：静态表里的 CreditsMultiplier 是**标准倍率**；实际计费随窗口变化，
// 故这里额外计算「当前生效倍率」供前端展示。

import "time"

// offPeakWindow 描述一个错峰折扣项。
type offPeakWindow struct {
	// Standard 是标准倍率。
	Standard float64
	// OffPeak 是错峰倍率。
	OffPeak float64
}

// offPeakDiscounts 是享错峰折扣的模型（key = 上游模型 id）。
var offPeakDiscounts = map[string]offPeakWindow{
	"qmodel_38max": {Standard: 0.5, OffPeak: 0.2},
	"qmodel":       {Standard: 0.1, OffPeak: 0.04},
}

// offPeakWindowStartHourUTC / offPeakWindowEndHourUTC 是错峰窗口（UTC 小时，[start,end)）。
const (
	offPeakWindowStartHourUTC = 14
	offPeakWindowEndHourUTC   = 24
)

// isOffPeak 报告给定时刻是否处于错峰窗口（UTC 14:00–00:00）。
func isOffPeak(t time.Time) bool {
	h := t.UTC().Hour()
	return h >= offPeakWindowStartHourUTC && h < offPeakWindowEndHourUTC
}

// effectiveMultiplier 返回模型在当前时刻的生效倍率：
// 享错峰折扣的模型在窗口内返回折扣倍率，否则返回标准倍率。
// 返回 (倍率, 是否处于折扣中)。
func effectiveMultiplier(modelID string, standard float64, now time.Time) (float64, bool) {
	w, ok := offPeakDiscounts[modelID]
	if !ok {
		return standard, false
	}
	if isOffPeak(now) {
		return w.OffPeak, true
	}
	return w.Standard, false
}
