package qoder

// 本文件实现「池级限流」状态的记录与查询。
//
// 背景（2026-10-20 实测）：Qoder 的 code=110「Billing daily count exceeded」
// 是**账号池级**的每日次数限制，不是积分耗尽——
//   - 三个账号积分分别是 1700/1600/1600，quota/usage 里 isQuotaExceeded=false；
//   - 但三个账号 × 2 个模型 = 6/6 全部返回 code=110。
// 也就是说换号解决不了，而旧实现每次都依次试满所有账号（每个 200~600ms），
// 把一次失败放大成三倍延迟。
//
// 因此引入池级状态：全部账号都因额度失败 → 记录恢复时刻 → 冷却期内直接 429。

import (
	"strings"
	"time"
)

// dailyResetHourUTC8 是推断的每日重置小时（UTC+8 的 10:00）。
//
// **这是一个推断值，不是上游下发**：错误信封只有 4 个字段
// （body/headers/statusCode/statusCodeValue），完全没有重置时间；
// quota/usage 只有远期的 expiresAt；只有签到活动 campaigns 里带日窗口
// （实测 startAt 2026-10-19 10:00 → endAt 2026-10-20 09:59，UTC+8）。
//
// 签到窗口与计费限流是两套机制，不能确定完全一致，故：
//   - 用它作为冷却上限（避免整天弃用）；
//   - 前端明确标注「推断」，不冒充权威时间。
const dailyResetHourUTC8 = 10

// cstZone 是 UTC+8 固定时区（不依赖宿主 tzdata）。
var cstZone = time.FixedZone("UTC+8", 8*60*60)

// inferPoolResetAt 推断池级限流的恢复时刻：下一个 UTC+8 的 10:00。
func inferPoolResetAt(now time.Time) time.Time {
	local := now.In(cstZone)
	next := time.Date(local.Year(), local.Month(), local.Day(), dailyResetHourUTC8, 0, 0, 0, cstZone)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// markPoolLimited 记录「所有账号都被额度限制」及其恢复时刻。
func (s *Service) markPoolLimited(msg string, until time.Time) {
	if until.IsZero() {
		until = inferPoolResetAt(time.Now())
	}
	s.poolLimitMu.Lock()
	defer s.poolLimitMu.Unlock()
	// 多次触发时保留更晚的恢复时刻（避免被较早的覆盖而提前解除）。
	if until.After(s.poolLimitTill) {
		s.poolLimitTill = until
	}
	if strings.TrimSpace(msg) != "" {
		s.poolLimitMsg = msg
	}
}

// clearPoolLimit 清除池级限流（任一账号请求成功时调用）。
func (s *Service) clearPoolLimit() {
	s.poolLimitMu.Lock()
	defer s.poolLimitMu.Unlock()
	s.poolLimitTill = time.Time{}
	s.poolLimitMsg = ""
}

// poolLimitStatus 返回池级限流状态。
//
// limited=true 表示当前仍处于限流期，调用方应直接返回 429 而不必再试账号。
// until 是推断的恢复时刻；msg 是上游原始错误信息。
func (s *Service) poolLimitStatus(now time.Time) (limited bool, until time.Time, msg string) {
	s.poolLimitMu.Lock()
	defer s.poolLimitMu.Unlock()
	if s.poolLimitTill.IsZero() || !now.Before(s.poolLimitTill) {
		return false, time.Time{}, ""
	}
	return true, s.poolLimitTill, s.poolLimitMsg
}
