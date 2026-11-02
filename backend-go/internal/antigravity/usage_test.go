package antigravity

import (
	"testing"
	"time"
)

// 用量按「站点时区日期 × 账号 × 模型」聚合，空账号/模型用占位符兜底。
func TestRecordUsageAggregatesByKey(t *testing.T) {
	s := newTestAntigravityService(t)

	s.recordUsage("a@example.com", "claude-sonnet-4-6", 100, 20, 5)
	s.recordUsage("a@example.com", "claude-sonnet-4-6", 50, 10, 0)
	s.recordUsage("b@example.com", "gemini-3-flash", 30, 8, 0)
	s.recordUsage("", "", 1, 1, 0)

	keys := s.sortedUsageKeys()
	if len(keys) != 3 {
		t.Fatalf("应产生 3 个用量键，得到 %d", len(keys))
	}

	var first *usageDelta
	for _, k := range keys {
		if k.email == "a@example.com" && k.model == "claude-sonnet-4-6" {
			first = s.usagePending[k]
		}
	}
	if first == nil {
		t.Fatalf("a@example.com 的用量键缺失")
	}
	if first.requests != 2 || first.promptTokens != 150 || first.completionTokens != 30 || first.cachedTokens != 5 {
		t.Fatalf("聚合错误: %+v", *first)
	}
}

// 零 token 不产生记录。
func TestRecordUsageSkipsEmpty(t *testing.T) {
	s := newTestAntigravityService(t)
	s.recordUsage("a@example.com", "m", 0, 0, 0)
	if keys := s.sortedUsageKeys(); len(keys) != 0 {
		t.Fatalf("零用量不应产生记录，得到 %d 个键", len(keys))
	}
}

// 从 OpenAI 兼容聚合响应提取 usage。
func TestRecordUsageFromOpenAI(t *testing.T) {
	s := newTestAntigravityService(t)
	resp := map[string]interface{}{
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(12),
			"completion_tokens": float64(34),
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": float64(6),
			},
		},
	}
	s.recordUsageFromOpenAI("a@example.com", "claude-sonnet-4-6", resp)
	keys := s.sortedUsageKeys()
	if len(keys) != 1 {
		t.Fatalf("应产生 1 个用量键，得到 %d", len(keys))
	}
	d := s.usagePending[keys[0]]
	if d.promptTokens != 12 || d.completionTokens != 34 || d.cachedTokens != 6 {
		t.Fatalf("usage 提取错误: %+v", *d)
	}
}

// 缓存命中字段归一：cache_read_input_tokens 也应被识别。
func TestCachedTokensVariants(t *testing.T) {
	if v := cachedTokens(map[string]interface{}{"cache_read_input_tokens": float64(9)}); v != 9 {
		t.Fatalf("cache_read_input_tokens = %d", v)
	}
	if v := cachedTokens(map[string]interface{}{"completion_tokens": float64(3)}); v != 0 {
		t.Fatalf("无缓存字段应为 0，得到 %d", v)
	}
}

// 日期桶按站点时区归属。
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
