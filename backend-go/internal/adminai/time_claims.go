package adminai

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 输出时间校验（治理层防线）：
// AI 回复中若出现与权威时间严重不符的时间表述，在落库前附加警告，
// 防止模型凭训练记忆编造"当前时间/日期"造成用户误判。
// 只校验明确描述"当前/现在/今天"类时刻的表述，不校验历史/未来的业务时间（如 next_run、创建时间）。

var (
	// 匹配 "现在/当前/目前/此刻/今天" 后 8 个字符内的时间点（HH:mm 或 HH:mm:ss）
	nowTimeRe = regexp.MustCompile(`(?:现在|当前|目前|此刻|今天)[^\d]{0,6}(\d{1,2})[:：](\d{2})(?::(\d{2}))?`)
	// 匹配 "今天是/当前时间是/现在是" 语境下的完整日期时间（YYYY-MM-DD HH:mm）
	// 仅当日期出现在"现在/当前/今天"近邻时认定是描述"当前时刻"，避免误伤业务时间（如 next_run）。
	nowDateTimeRe = regexp.MustCompile(`(?:现在是|当前时间[是为]|今天是|当前日期[是为]|今天日期[是为])[^\d]{0,6}(\d{4})[年\-/](\d{1,2})[月\-/](\d{1,2})日?[^\d]{0,4}(\d{1,2})[:：](\d{2})(?::(\d{2}))?`)
	dateOnlyRe = regexp.MustCompile(`(?:今天是|现在是|当前日期[是为]|今天日期[是为])[^\d]{0,6}(\d{4})[年\-/](\d{1,2})[月\-/](\d{1,2})日?`)
)

const maxTimeDrift = 10 * time.Minute // 超过该偏差视为异常

// checkReplyTimeClaims 检查 AI 回复中的时间表述与权威时间（站点本地时间）的偏差。
// 返回警告文案列表；无异常返回 nil。now 与 loc 为调用方注入的权威时间与时区。
func checkReplyTimeClaims(reply string, now time.Time, loc *time.Location) []string {
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	localNow := now.In(loc)
	warnings := make([]string, 0, 2)

	// 1. "现在 HH:mm" 类表述：时钟偏差校验
	for _, m := range nowTimeRe.FindAllStringSubmatch(reply, -1) {
		hour, _ := strconv.Atoi(m[1])
		minute, _ := strconv.Atoi(m[2])
		second := 0
		if m[3] != "" {
			second, _ = strconv.Atoi(m[3])
		}
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 {
			continue
		}
		claimed := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, second, 0, loc)
		drift := claimed.Sub(localNow)
		if drift < -maxTimeDrift || drift > maxTimeDrift {
			warnings = append(warnings, fmt.Sprintf(
				"回复中「%s」表述的时间为 %02d:%02d，与当前站点时间 %s 偏差过大，请以实际时间为准",
				m[0], hour, minute, localNow.Format("15:04")))
			break // 同类表述只告警一次
		}
	}

	// 2. "现在是 XXXX-XX-XX HH:mm" 类完整日期时间：日期偏差校验
	for _, m := range nowDateTimeRe.FindAllStringSubmatch(reply, -1) {
		year, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		hour, _ := strconv.Atoi(m[4])
		minute, _ := strconv.Atoi(m[5])
		second := 0
		if m[6] != "" {
			second, _ = strconv.Atoi(m[6])
		}
		if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 {
			continue
		}
		claimed := time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
		drift := claimed.Sub(localNow)
		if drift < -maxTimeDrift || drift > maxTimeDrift {
			warnings = append(warnings, fmt.Sprintf(
				"回复中「%s」表述的日期时间与当前站点时间 %s 偏差过大，请以实际时间为准",
				m[0], localNow.Format("2006-01-02 15:04")))
			break
		}
	}

	// 3. "今天是 XXXX年XX月XX日"（无时间）：日期偏差校验
	if len(warnings) == 0 {
		for _, m := range dateOnlyRe.FindAllStringSubmatch(reply, -1) {
			year, _ := strconv.Atoi(m[1])
			month, _ := strconv.Atoi(m[2])
			day, _ := strconv.Atoi(m[3])
			if month < 1 || month > 12 || day < 1 || day > 31 {
				continue
			}
			claimed := time.Date(year, time.Month(month), day, 12, 0, 0, 0, loc)
			drift := claimed.Sub(localNow)
			if drift < -24*time.Hour || drift > 24*time.Hour {
				warnings = append(warnings, fmt.Sprintf(
					"回复中「%s」表述的日期与当前站点日期 %s 不符，请以实际日期为准",
					m[0], localNow.Format("2006-01-02")))
				break
			}
		}
	}
	return warnings
}