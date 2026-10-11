package serveragent

import (
	"testing"
	"time"
)

func TestTrafficCycleWindowCalendarMonth(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	now := time.Date(2026, 9, 15, 10, 30, 0, 0, loc)
	start, end, ok := trafficCycleWindow("calendar_month", 1, loc, now)
	if !ok {
		t.Fatalf("calendar_month should be auto-derived")
	}
	wantStart := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

func TestTrafficCycleWindowMonthlyRollsOver(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// 每月 10 号结算：今天 9 号，应落在上一周期 8-10 到 9-10
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, loc)
	start, end, ok := trafficCycleWindow("monthly", 10, loc, now)
	if !ok {
		t.Fatalf("monthly should be auto-derived")
	}
	wantStart := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 9, 10, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
	// 今天 10 号之后：应落在 9-10 到 10-10
	now2 := time.Date(2026, 9, 10, 1, 0, 0, 0, loc)
	start2, end2, ok2 := trafficCycleWindow("monthly", 10, loc, now2)
	if !ok2 {
		t.Fatalf("monthly should be auto-derived")
	}
	wantStart2 := time.Date(2026, 9, 10, 0, 0, 0, 0, loc)
	wantEnd2 := time.Date(2026, 10, 10, 0, 0, 0, 0, loc)
	if !start2.Equal(wantStart2) {
		t.Errorf("start2 = %v, want %v", start2, wantStart2)
	}
	if !end2.Equal(wantEnd2) {
		t.Errorf("end2 = %v, want %v", end2, wantEnd2)
	}
}

func TestTrafficCycleWindowNoneAndCustom(t *testing.T) {
	loc := time.UTC
	if _, _, ok := trafficCycleWindow("none", 1, loc, time.Now()); ok {
		t.Fatalf("none should not auto-derive")
	}
	if _, _, ok := trafficCycleWindow("custom", 1, loc, time.Now()); ok {
		t.Fatalf("custom should not auto-derive")
	}
}

func TestTrafficUsedForCycle(t *testing.T) {
	// 未启用周期：原样返回累计
	if got := trafficUsedForCycle(100, "none", 50); got != 100 {
		t.Errorf("none: got %d want 100", got)
	}
	// 基线未初始化：原样返回累计
	if got := trafficUsedForCycle(100, "calendar_month", 0); got != 100 {
		t.Errorf("baseline 0: got %d want 100", got)
	}
	// 正常：累计 - 基线
	if got := trafficUsedForCycle(100, "calendar_month", 60); got != 40 {
		t.Errorf("used: got %d want 40", got)
	}
	// 累计小于基线（重置瞬间）：钳到 0
	if got := trafficUsedForCycle(50, "calendar_month", 60); got != 0 {
		t.Errorf("clamped: got %d want 0", got)
	}
}

func TestTrafficTotalsFromMetricsReadsBothDirections(t *testing.T) {
	// network.rx_total_bytes/tx_total_bytes 优先
	rx, tx := trafficTotalsFromMetrics(map[string]interface{}{
		"network": map[string]interface{}{"rx_total_bytes": 1500, "tx_total_bytes": 900},
	})
	if rx != 1500 || tx != 900 {
		t.Fatalf("network totals = %d/%d, want 1500/900", rx, tx)
	}
	// 回退到顶层 net_in_transfer/net_out_transfer
	rx2, tx2 := trafficTotalsFromMetrics(map[string]interface{}{
		"net_in_transfer":  2000,
		"net_out_transfer": 1000,
	})
	if rx2 != 2000 || tx2 != 1000 {
		t.Fatalf("flat totals = %d/%d, want 2000/1000", rx2, tx2)
	}
	// 负值钳到 0
	rx3, tx3 := trafficTotalsFromMetrics(map[string]interface{}{
		"net_in_transfer":  -5,
		"net_out_transfer": 0,
	})
	if rx3 != 0 || tx3 != 0 {
		t.Fatalf("clamped totals = %d/%d, want 0/0", rx3, tx3)
	}
}
