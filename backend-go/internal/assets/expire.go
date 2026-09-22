package assets

import (
	"math"
	"sort"
	"strings"
	"time"
)

// 资产状态派生：expire_at 统一为 UTC RFC3339，剩余天数按站点时区归属计算。
// 前后端口径一致：后端产出 derived_status 与 days_left，前端只做展示分级。

const (
	statusActive   = "active"
	statusExpiring = "expiring"
	statusExpired  = "expired"
	statusRetired  = "retired"
	statusOrphan   = "orphan"
	statusUnknown  = "unknown"
)

var defaultWarnDays = []int{30, 14, 7, 1}

// parseExpireAt 解析存储的 UTC RFC3339 到期时刻。
func parseExpireAt(value string) (time.Time, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// daysUntil 计算从 now 到 expire 的日历天数差（按给定时区的日界）。
// 正数表示尚未到期，0 表示今天到期，负数表示已过期。
func daysUntil(expire, now time.Time, loc *time.Location) int {
	if loc == nil {
		loc = time.Local
	}
	expireDay := expire.In(loc)
	nowDay := now.In(loc)
	expireMidnight := time.Date(expireDay.Year(), expireDay.Month(), expireDay.Day(), 0, 0, 0, 0, loc)
	nowMidnight := time.Date(nowDay.Year(), nowDay.Month(), nowDay.Day(), 0, 0, 0, 0, loc)
	return int(math.Round(expireMidnight.Sub(nowMidnight).Hours() / 24))
}

// deriveStatus 根据到期时刻、阈值与自动续费派生状态。
// autoRenew 的资产不进入 expiring，避免无意义的续费提醒。
func deriveStatus(asset Asset, now time.Time, loc *time.Location) (string, *int) {
	if asset.Status == statusRetired || asset.Status == statusOrphan {
		days := daysLeftFor(asset.ExpireAt, now, loc)
		return asset.Status, days
	}
	expire, ok := parseExpireAt(asset.ExpireAt)
	if !ok {
		return statusUnknown, nil
	}
	days := daysUntil(expire, now, loc)
	if days < 0 {
		return statusExpired, &days
	}
	if asset.AutoRenew {
		return statusActive, &days
	}
	threshold := warnThreshold(asset.WarnDays)
	if days <= threshold {
		return statusExpiring, &days
	}
	return statusActive, &days
}

func daysLeftFor(expireAt string, now time.Time, loc *time.Location) *int {
	expire, ok := parseExpireAt(expireAt)
	if !ok {
		return nil
	}
	days := daysUntil(expire, now, loc)
	return &days
}

// warnThreshold 取资产自身阈值里的最大天数；为空则用全局默认的最大值。
func warnThreshold(warnDays []int) int {
	if len(warnDays) == 0 {
		return defaultWarnDays[0]
	}
	threshold := 0
	for _, day := range warnDays {
		if day > threshold {
			threshold = day
		}
	}
	return threshold
}

// effectiveWarnDays 返回资产生效的阈值数组：自身覆盖优先，否则全局默认。
func effectiveWarnDays(assetWarn []int, globalWarn []int) []int {
	if len(assetWarn) > 0 {
		return assetWarn
	}
	if len(globalWarn) > 0 {
		return globalWarn
	}
	return defaultWarnDays
}

// applyDerived 为单个资产填充 days_left 与 derived_status。
func applyDerived(asset *Asset, now time.Time, loc *time.Location) {
	status, days := deriveStatus(*asset, now, loc)
	asset.DerivedStatus = status
	asset.DaysLeft = days
}

// bucketFor 把剩余天数归入总览分桶。
func bucketFor(asset Asset) string {
	if asset.Status == statusRetired || asset.Status == statusOrphan {
		return "no_renew"
	}
	if asset.DaysLeft == nil {
		return "normal"
	}
	days := *asset.DaysLeft
	switch {
	case days < 0:
		return "expired"
	case days <= 7:
		return "within_7"
	case days <= 30:
		return "within_30"
	default:
		return "normal"
	}
}

// sortByExpireAsc 按到期升序排列，无到期信息的排最后。
func sortByExpireAsc(items []Asset) {
	sort.SliceStable(items, func(i, j int) bool {
		left, leftOK := parseExpireAt(items[i].ExpireAt)
		right, rightOK := parseExpireAt(items[j].ExpireAt)
		if !leftOK && !rightOK {
			return items[i].Name < items[j].Name
		}
		if !leftOK {
			return false
		}
		if !rightOK {
			return true
		}
		return left.Before(right)
	})
}
