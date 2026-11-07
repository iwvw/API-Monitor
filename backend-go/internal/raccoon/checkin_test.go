package raccoon

import (
	"testing"
	"time"
)

// TestBillDateParses 验证账单日期归属：上游字段形态不统一，且必须按 UTC+8 归属。
func TestBillDateParses(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
		want string
	}{
		{"bill_date", map[string]any{"bill_date": "2026-11-07"}, "2026-11-07"},
		{"带时间", map[string]any{"bill_date": "2026-11-07 12:34:56"}, "2026-11-07"},
		{"date", map[string]any{"date": "2026-11-06"}, "2026-11-06"},
		{"毫秒时间戳按 UTC+8", map[string]any{"created_at": float64(1794032825000)}, "2026-11-07"},
		{"缺失", map[string]any{}, ""},
	}
	for _, c := range cases {
		if got := billDate(c.item); got != c.want {
			t.Errorf("%s: billDate = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestParseGrantFilters 验证只有「今天 + daily 类 + 正积分」才算签到发放。
func TestParseGrantFilters(t *testing.T) {
	today := "2026-11-07"
	cases := []struct {
		name     string
		item     map[string]any
		wantHit  bool
		wantStop bool
	}{
		{"今日 daily 发放", map[string]any{"bill_date": today, "event_name": "daily_grant", "points": float64(300)}, true, false},
		{"今日补签不算", map[string]any{"bill_date": today, "event_name": "daily_makeup:123:free:300", "points": float64(300)}, false, false},
		{"今日消费不算", map[string]any{"bill_date": today, "event_name": "consume", "points": float64(-10)}, false, false},
		{"今日过期不算", map[string]any{"bill_date": today, "event_name": "daily_expire", "points": float64(0)}, false, false},
		{"更早日期停止翻页", map[string]any{"bill_date": "2026-11-06", "event_name": "daily_grant", "points": float64(300)}, false, true},
	}
	for _, c := range cases {
		_, hit, stop := parseGrant(c.item, today)
		if hit != c.wantHit || stop != c.wantStop {
			t.Errorf("%s: (hit=%v,stop=%v), want (%v,%v)", c.name, hit, stop, c.wantHit, c.wantStop)
		}
	}
}

// TestIsDailyGrantEvent 验证事件名过滤的词根口径。
func TestIsDailyGrantEvent(t *testing.T) {
	yes := []string{"daily_grant", "daily_signin", "DAILY_QUESTION_REWARD"}
	no := []string{"", "consume", "daily_makeup:123", "topup", "refund", "expire"}
	for _, e := range yes {
		if !isDailyGrantEvent(e) {
			t.Errorf("%q 应判为每日发放", e)
		}
	}
	for _, e := range no {
		if isDailyGrantEvent(e) {
			t.Errorf("%q 不应判为每日发放", e)
		}
	}
}

// TestCheckinZoneIsUTC8 守住「上游自然日 = UTC+8」这条口径。
func TestCheckinZoneIsUTC8(t *testing.T) {
	if _, offset := time.Now().In(checkinZone).Zone(); offset != 8*3600 {
		t.Errorf("checkinZone 偏移 = %d, want 28800", offset)
	}
}
