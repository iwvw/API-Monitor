package ds2api

import (
	"testing"
	"time"
)

// 用量按「站点时区日期 × 账号 × 模型」聚合，且空账号/模型用占位符兜底。
func TestRecordUsageAggregatesByKey(t *testing.T) {
	s := newTestDS2APIService(t)

	s.recordUsage("acc-1", "deepseek-v4-flash", 100, 20, 5)
	s.recordUsage("acc-1", "deepseek-v4-flash", 50, 10, 0)
	s.recordUsage("acc-2", "deepseek-v4-pro", 30, 8, 0)
	// 空账号/模型走占位符，不应被静默丢弃。
	s.recordUsage("", "", 1, 1, 0)

	keys := s.sortedUsageKeys()
	if len(keys) != 3 {
		t.Fatalf("应产生 3 个用量键，得到 %d", len(keys))
	}

	var acc1 *usageDelta
	for _, k := range keys {
		if k.accountID == "acc-1" && k.model == "deepseek-v4-flash" {
			acc1 = s.usagePending[k]
		}
	}
	if acc1 == nil {
		t.Fatalf("acc-1 的用量键缺失")
	}
	if acc1.requests != 2 || acc1.promptTokens != 150 || acc1.completionTokens != 30 || acc1.cachedTokens != 5 {
		t.Fatalf("acc-1 聚合错误: %+v", *acc1)
	}
}

// 零 token 的请求不产生用量记录（避免虚增调用数）。
func TestRecordUsageSkipsEmpty(t *testing.T) {
	s := newTestDS2APIService(t)
	s.recordUsage("acc-1", "m", 0, 0, 0)
	if keys := s.sortedUsageKeys(); len(keys) != 0 {
		t.Fatalf("零用量不应产生记录，得到 %d 个键", len(keys))
	}
}

// 引擎回调把 reasoning 并入输出 token（对齐其它插件的 completion 口径）。
func TestRecordUsageFromEngineMergesReasoning(t *testing.T) {
	s := newTestDS2APIService(t)
	s.recordUsageFromEngine("deepseek-v4-flash", "acc-1", 100, 20, 7, 127)
	keys := s.sortedUsageKeys()
	if len(keys) != 1 {
		t.Fatalf("应产生 1 个用量键，得到 %d", len(keys))
	}
	d := s.usagePending[keys[0]]
	if d.promptTokens != 100 {
		t.Errorf("prompt = %d, want 100", d.promptTokens)
	}
	if d.completionTokens != 27 {
		t.Errorf("completion 应含 reasoning = %d, want 27", d.completionTokens)
	}
}

// 日期桶按站点时区归属（东八区跨日）。
func TestUsageDayUsesLocation(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	tm := time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC)
	if got := usageDay(tm, loc); got != "2026-01-02" {
		t.Fatalf("usageDay = %q want 2026-01-02", got)
	}
}

// 落盘失败时增量回队，不丢数。
func TestRequeueUsageRestores(t *testing.T) {
	s := newTestDS2APIService(t)
	key := usageKey{day: "2026-01-01", accountID: "a", model: "m"}
	s.requeueUsage(map[usageKey]*usageDelta{key: {requests: 1, promptTokens: 10}})
	s.requeueUsage(map[usageKey]*usageDelta{key: {requests: 1, promptTokens: 5}})
	if got := s.usagePending[key]; got == nil || got.requests != 2 || got.promptTokens != 15 {
		t.Fatalf("回队合并错误: %+v", got)
	}
}
