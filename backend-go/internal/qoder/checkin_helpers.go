package qoder

import "strings"

// 本文件是签到相关的「纯逻辑」层：请求头构造、活动筛选、窗口日期与本地
// 连签统计计算。协议事实来自 qoder2api（Zhengyuuuui/qoder2api）的
// checkin.go / checkin_antirisk.go 与社区 A/B 实验：
//
//   - 国内版（openapi.qoder.com.cn）：**不要**发送自造指纹头，发了反而拿不到
//     CLAIM_BENEFIT 活动；只发必要头即可。
//   - 国际版（openapi.qoder.sh）：必须携带官方客户端真实指纹
//     （cosy-machinetoken / cosy-machinetype / cosy-machineid / cosy-version 等），
//     否则服务端按设备身份过滤，静默不下发每日签到活动（表现为 no_campaign）。
//   - 领取窗口：每日 10:00 → 次日 09:59（UTC+8），按活动 startAt 记窗口日期，
//     避免 10:00 前把「昨日窗口」误记成今天。
//   - 上游 legacy daily-check-in 在国内已 DISABLED（统计恒 0），海外直接 404；
//     因此连续天数一律以本地记录为准，legacy 仅作只读补充。

// checkinHeaders 构造签到请求头。ident 为账号设备身份：
//   - 国内版：只发必要头，不带任何机器头（自造指纹会被拒）。
//   - 国际版：必须带真实指纹（Real=true 时视为可用）；无真实指纹时不带机器头，
//     由调用方给出「需配置指纹」的明确提示，而不是静默失败。
func checkinHeaders(acc Account, ident deviceIdentity, fullDevice bool) map[string]string {
	h := map[string]string{
		"authorization":   "Bearer " + acc.AccessToken,
		"accept":          "application/json",
		"accept-language": "zh-CN",
		"user-agent":      "Qoder",
		"cosy-clienttype": newClientType,
	}
	if !ident.Real {
		return h
	}
	h["cosy-version"] = firstNonEmpty(ident.Version, newCosyVersion)
	h["cosy-machineid"] = ident.MachineID
	if !fullDevice {
		return h
	}
	h["cosy-machinetoken"] = ident.MachineToken
	h["cosy-machinetype"] = ident.MachineType
	if ident.MachineCode != "" {
		h["cosy-machinecode"] = ident.MachineCode
	}
	h["cosy-machinehostname"] = firstNonEmpty(ident.Hostname, "localhost")
	h["cosy-machineos"] = firstNonEmpty(ident.MachineOS, defaultMachineOS)
	return h
}

// checkinNeedsFingerprint 报告该账号签到前是否必须先配置真实指纹。
// 国际版首次领取前必须带真实指纹；国内版不需要。
func checkinNeedsFingerprint(acc Account, ident deviceIdentity) bool {
	return normalizeRegion(acc.Region) == regionIntl && !ident.Real
}

// pickClaimableCampaigns 从活动列表挑出可领取的 CLAIM_BENEFIT 活动，
// 并回报是否存在「已领取」的同类活动（用于区分 already 与 none）。
func pickClaimableCampaigns(list []campaign) (targets []campaign, alreadyClaimed bool) {
	for _, c := range list {
		if !strings.EqualFold(c.ActionType, "CLAIM_BENEFIT") {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(c.ClaimStatus)) {
		case "CLAIMABLE":
			targets = append(targets, c)
		case "CLAIMED":
			alreadyClaimed = true
		}
	}
	return targets, alreadyClaimed
}

// campaignWindowDate 返回该活动窗口的开始日期（YYYY-MM-DD，本地时区）。
// 上游 startAt 是毫秒/秒两种形态都可能出现，按量级判断。
func campaignWindowDate(c campaign) string {
	return unixToDate(c.StartAt)
}

// campaignWindowHint 生成更准确的「已领取」提示：若当前窗口是昨日开的
// （10:00 前点击），明确告诉用户新窗口开放时间，避免误以为没领到。
func campaignWindowHint(windowDate, fallback string) string {
	if windowDate == "" {
		return fallback
	}
	if windowDate == todayIn(cstZone) {
		return fallback
	}
	next := addDays(windowDate, 1)
	if next == "" {
		return fallback
	}
	return "已领取 " + windowDate + " 窗口额度（新窗口 " + monthDay(next) + " 10:00 开放）"
}

// calcLocalCheckinStats 按签到历史计算连续天数 / 累计天数 / 累计积分。
// 连续天数从「今天已领则今天起，否则从昨天起」往前数，当天未领不算断链。
func calcLocalCheckinStats(recs []checkinRecord) (streak, totalDays int, totalCredits float64) {
	if len(recs) == 0 {
		return 0, 0, 0
	}
	seen := make(map[string]bool, len(recs))
	for _, r := range recs {
		seen[r.Date] = true
		totalCredits += r.Amount
	}
	totalDays = len(recs)
	cursor := todayIn(cstZone)
	if !seen[cursor] {
		cursor = shiftDay(cursor, -1)
	}
	for seen[cursor] {
		streak++
		cursor = shiftDay(cursor, -1)
	}
	return streak, totalDays, totalCredits
}

// streakSuffix 生成连签统计后缀（无统计时返回空串）。
func streakSuffix(res *CheckinResult) string {
	if res.StreakDays <= 0 && res.TotalDays <= 0 {
		return ""
	}
	msg := "（连续 " + itoa(res.StreakDays) + " 天 · 累计 " + itoa(res.TotalDays) + " 天"
	if res.TotalCredits > 0 {
		msg += " · 共 " + trimFloat(res.TotalCredits) + " 积分"
	}
	return msg + "）"
}

// normalizeCheckinTimes 规范化 HH:MM 列表：去空白、校验范围、去重、升序。
// 非法项直接丢弃（配置容错，不因一条写错而整天不签到）。
func normalizeCheckinTimes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		h, m, ok := parseHHMM(t)
		if !ok {
			continue
		}
		key := pad2(h) + ":" + pad2(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sortStrings(out)
	return out
}
