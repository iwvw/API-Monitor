package workbuddy

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsageCreditDistinguishesZeroFromAbsent(t *testing.T) {
	if _, ok := usageCredit(map[string]any{"prompt_tokens": float64(10)}); ok {
		t.Error("上游没给 credit 字段时 ok 应为 false")
	}
	v, ok := usageCredit(map[string]any{"credit": float64(0)})
	if !ok || v != 0 {
		t.Errorf("免费模型上报 credit=0 是有效值，应 ok=true v=0，得到 %v/%v", v, ok)
	}
	if v, ok := usageCredit(map[string]any{"credit": 0.03}); !ok || v != 0.03 {
		t.Errorf("credit 解析错误: %v/%v", v, ok)
	}
	// 字符串数字也要认。
	if v, ok := usageCredit(map[string]any{"credit": "0.5"}); !ok || v != 0.5 {
		t.Errorf("字符串 credit 解析错误: %v/%v", v, ok)
	}
}

func TestUsageCachedTokensPrefersNormalizedField(t *testing.T) {
	// 归一后的标准字段优先
	u := map[string]any{
		"prompt_tokens_details":   map[string]any{"cached_tokens": float64(11)},
		"prompt_cache_hit_tokens": float64(99),
	}
	if got := usageCachedTokens(u); got != 11 {
		t.Errorf("应优先取 prompt_tokens_details.cached_tokens，得到 %d", got)
	}
	// 只有别名时回退
	u2 := map[string]any{"cache_read_tokens": float64(42)}
	if got := usageCachedTokens(u2); got != 42 {
		t.Errorf("别名回退失败，得到 %d", got)
	}
	if got := usageCachedTokens(map[string]any{"prompt_tokens": float64(5)}); got != 0 {
		t.Errorf("无缓存字段应为 0，得到 %d", got)
	}
}

// 日期桶必须按传入的站点时区归属，而不是 UTC。
func TestUsageDayUsesSiteLocation(t *testing.T) {
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区库不可用: %v", err)
	}
	// UTC 2026-09-10 23:30 = 北京时间 2026-09-11 07:30
	instant := time.Date(2026, 9, 10, 23, 30, 0, 0, time.UTC)
	if got := usageDay(instant, sh); got != "2026-09-11" {
		t.Errorf("站点时区应为 2026-09-11，得到 %s", got)
	}
	if got := usageDay(instant, time.UTC); got != "2026-09-10" {
		t.Errorf("UTC 应为 2026-09-10，得到 %s", got)
	}
}

// 完全没有数字的 usage 不该虚增调用数。
func TestRecordUsageIgnoresJunk(t *testing.T) {
	s := newTestService(t)
	s.recordUsage(context.Background(), "u1", "hy3", map[string]any{})
	s.recordUsage(context.Background(), "u1", "hy3", map[string]any{"choices": []any{}})
	if keys := s.sortedUsageKeys(); len(keys) != 0 {
		t.Errorf("空 usage 不应入账，得到 %+v", keys)
	}
}

// 账号/模型为空时用占位符，保证有消耗的请求不会被静默丢掉。
func TestRecordUsagePlaceholders(t *testing.T) {
	s := newTestService(t)
	s.recordUsage(context.Background(), "", "", map[string]any{"prompt_tokens": float64(1)})
	keys := s.sortedUsageKeys()
	if len(keys) != 1 {
		t.Fatalf("应入账 1 条，得到 %d", len(keys))
	}
	if keys[0].accountID == "" || keys[0].model == "" {
		t.Errorf("空账号/模型应使用占位符: %+v", keys[0])
	}
}

// 同一「日期 × 账号 × 模型」的多次上报要累加到一行，并能从接口按账号/模型聚合出来。
func TestUsageEndpointAggregates(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	s.recordUsage(ctx, "u1", "hy3", map[string]any{
		"prompt_tokens": float64(100), "completion_tokens": float64(5),
		"prompt_cache_hit_tokens": float64(80), "credit": 0.03,
	})
	s.recordUsage(ctx, "u1", "hy3", map[string]any{
		"prompt_tokens": float64(200), "completion_tokens": float64(10),
		"prompt_cache_hit_tokens": float64(0), "credit": 0.5,
	})
	s.recordUsage(ctx, "u2", "glm-5.3", map[string]any{
		"prompt_tokens": float64(50), "credit": 0.1,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workbuddy/usage?days=7", nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Days           int              `json:"days"`
		Totals         usageTotals      `json:"totals"`
		ByAccount      []map[string]any `json:"byAccount"`
		ByModel        []map[string]any `json:"byModel"`
		Daily          []map[string]any `json:"daily"`
		CreditReported bool             `json:"creditReported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	if payload.Days != 7 {
		t.Errorf("days=%d want=7", payload.Days)
	}
	if payload.Totals.Requests != 3 {
		t.Errorf("requests=%d want=3", payload.Totals.Requests)
	}
	if payload.Totals.PromptTokens != 350 || payload.Totals.CompletionTokens != 15 {
		t.Errorf("tokens 合计不对: %+v", payload.Totals)
	}
	if payload.Totals.CachedTokens != 80 {
		t.Errorf("cachedTokens=%d want=80", payload.Totals.CachedTokens)
	}
	if math.Abs(payload.Totals.Credit-0.63) > 1e-9 {
		t.Errorf("credit 合计=%.6f want=0.63", payload.Totals.Credit)
	}
	if math.Abs(payload.Totals.CacheHitRate-80.0/350.0) > 1e-9 {
		t.Errorf("cacheHitRate=%.6f", payload.Totals.CacheHitRate)
	}
	if !payload.CreditReported {
		t.Error("上游上报过 credit，creditReported 应为 true")
	}

	// 按扣费降序：u1(0.53) 在 u2(0.1) 前。
	if len(payload.ByAccount) != 2 {
		t.Fatalf("byAccount 应有 2 组，得到 %d", len(payload.ByAccount))
	}
	if payload.ByAccount[0]["accountId"] != "u1" {
		t.Errorf("byAccount 未按扣费降序: %+v", payload.ByAccount)
	}
	if len(payload.ByModel) != 2 || payload.ByModel[0]["model"] != "hy3" {
		t.Errorf("byModel 异常: %+v", payload.ByModel)
	}
	if len(payload.Daily) != 1 {
		t.Errorf("daily 应有 1 天（同一天），得到 %d", len(payload.Daily))
	}
	// 累加后 u1 的 hy3 是 2 次调用（同一行 UPSERT 累加，不是 2 行）。
	if got := payload.ByAccount[0]["requests"]; got != float64(2) {
		t.Errorf("u1 requests=%v want=2", got)
	}
}

// days 参数要被钳制，避免异常范围打到库上。
func TestUsageEndpointClampsDays(t *testing.T) {
	s := newTestService(t)
	for _, tc := range []struct {
		raw  string
		want int
	}{{"0", 1}, {"-5", 1}, {"999", 90}, {"abc", 7}, {"", 7}, {"30", 30}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/workbuddy/usage?days="+tc.raw, nil)
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("days=%q 应返回 200，得到 %d", tc.raw, rec.Code)
		}
		var payload struct {
			Days int `json:"days"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Days != tc.want {
			t.Errorf("days=%q 归一化后=%d want=%d", tc.raw, payload.Days, tc.want)
		}
	}
}
