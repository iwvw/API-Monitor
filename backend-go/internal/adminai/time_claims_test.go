package adminai

import (
	"testing"
	"time"
)

// 权威时间基准：2026-08-16 11:00:00 Asia/Shanghai
func testAuthoritativeNow() (time.Time, *time.Location) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 8, 16, 11, 0, 0, 0, sh)
	return now, sh
}

func TestCheckReplyTimeClaims(t *testing.T) {
	now, loc := testAuthoritativeNow()

	cases := []struct {
		name    string
		reply   string
		wantWarn bool
	}{
		{"无时间表述", "已成功删除所有定时任务。", false},
		{"时钟一致", "现在 11:00。", false},
		{"时钟偏差小", "现在 10:58。", false},
		{"时钟编造", "现在是 23:45。", true},
		{"时钟编造带秒", "此刻 08:15:30。", true},
		{"日期正确", "今天是 2026年8月16日 11:00。", false},
		{"日期编造", "今天是 2025年1月1日 09:00。", true},
		{"日期编造无时分", "今天是 2030年3月5日。", true},
		{"业务时间不校验", "任务下次执行时间为 2026-08-17 02:00（cron 0 2 * * *）。", false},
		{"历史时间不校验", "该任务创建于 2026-08-10。", false},
		{"空回复", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkReplyTimeClaims(tc.reply, now, loc)
			if tc.wantWarn && len(got) == 0 {
				t.Fatalf("expected warning for %q, got none", tc.reply)
			}
			if !tc.wantWarn && len(got) > 0 {
				t.Fatalf("expected no warning for %q, got %v", tc.reply, got)
			}
		})
	}
}

func TestCheckReplyTimeClaimsBoundary(t *testing.T) {
	now, loc := testAuthoritativeNow()

	// 10 分钟整偏差：drift == maxTimeDrift 不告警（<= 阈值）
	reply := "现在 10:50。"
	if got := checkReplyTimeClaims(reply, now, loc); len(got) != 0 {
		t.Fatalf("exact threshold should pass, got %v", got)
	}
	// 超过阈值：告警
	reply = "现在 10:49。"
	if got := checkReplyTimeClaims(reply, now, loc); len(got) == 0 {
		t.Fatal("over threshold should warn")
	}
}