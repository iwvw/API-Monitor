package notification

import (
	"strings"
	"testing"
	"time"
)

// 告警聚合的核心语义：一次宿主机宕机不该刷出 20 条同组通知。
func TestAggregatorSendsFirstThenMerges(t *testing.T) {
	agg := NewAggregator(30 * time.Second)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	first := agg.Offer("rule-1", "server-a", "critical", "宿主机离线", now)
	if !first.Send || first.Reason != "first" {
		t.Fatalf("首条应立即发送, got %+v", first)
	}

	for i := 0; i < 5; i++ {
		decision := agg.Offer("rule-1", "server-a", "critical", "容器离线", now.Add(time.Duration(i)*time.Second))
		if decision.Send {
			t.Fatalf("同组同级别第 %d 条应被合并, got %+v", i+2, decision)
		}
		if decision.Reason != "merged" {
			t.Fatalf("reason = %q, want merged", decision.Reason)
		}
	}
}

// 高级别出现时要立刻冲破合并，不能被先到的低级别挡住。
func TestAggregatorUpgradesWhenSeverityRises(t *testing.T) {
	agg := NewAggregator(30 * time.Second)
	now := time.Now()

	if d := agg.Offer("rule-1", "server-a", "warning", "磁盘 85%", now); !d.Send {
		t.Fatalf("首条应发送, got %+v", d)
	}
	// 低级别后续：合并
	if d := agg.Offer("rule-1", "server-a", "info", "磁盘 86%", now.Add(time.Second)); d.Send {
		t.Fatalf("低级别应被合并, got %+v", d)
	}
	// 升级为 critical：必须发
	d := agg.Offer("rule-1", "server-a", "critical", "磁盘 98%", now.Add(2*time.Second))
	if !d.Send || d.Reason != "upgraded" {
		t.Fatalf("高级别应升级发送, got %+v", d)
	}
	// 升级后再来低级别：仍合并
	if d := agg.Offer("rule-1", "server-a", "warning", "磁盘 99%", now.Add(3*time.Second)); d.Send {
		t.Fatalf("升级后的低级别应被合并, got %+v", d)
	}
}

// 不同分组互不影响：A 主机宕机不该压掉 B 主机的告警。
func TestAggregatorGroupsAreIndependent(t *testing.T) {
	agg := NewAggregator(30 * time.Second)
	now := time.Now()

	if d := agg.Offer("rule-1", "server-a", "critical", "A 离线", now); !d.Send {
		t.Fatalf("A 首条应发送")
	}
	if d := agg.Offer("rule-1", "server-b", "critical", "B 离线", now); !d.Send {
		t.Fatalf("不同分组应各自发送, got %+v", d)
	}
}

// 窗口过期后重新计数：下一轮故障仍要发第一条。
func TestAggregatorWindowExpiryResetsBucket(t *testing.T) {
	agg := NewAggregator(10 * time.Second)
	start := time.Now()

	if d := agg.Offer("rule-1", "server-a", "critical", "A 离线", start); !d.Send {
		t.Fatal("首条应发送")
	}
	// 窗口外
	later := start.Add(11 * time.Second)
	d := agg.Offer("rule-1", "server-a", "critical", "A 又离线了", later)
	if !d.Send || d.Reason != "first" {
		t.Fatalf("新窗口应重新发送, got %+v", d)
	}
}

// Flush 只对「确实合并过内容」的窗口生成汇总。
func TestAggregatorFlushOnlyForMergedBuckets(t *testing.T) {
	agg := NewAggregator(10 * time.Second)
	start := time.Now()

	// 只有一条，没有合并 → 不该产生汇总
	agg.Offer("rule-1", "only-one", "critical", "单条告警", start)

	// 有合并 → 应产生汇总
	agg.Offer("rule-2", "multi", "critical", "容器离线", start)
	agg.Offer("rule-2", "multi", "critical", "容器离线", start.Add(time.Second))
	agg.Offer("rule-2", "multi", "critical", "另一容器离线", start.Add(2*time.Second))

	summaries := agg.Flush(start.Add(11 * time.Second))
	if len(summaries) != 1 {
		t.Fatalf("应只有 1 条汇总, got %d: %+v", len(summaries), summaries)
	}
	s := summaries[0]
	if s.Key != "multi" {
		t.Fatalf("汇总 key = %q, want multi", s.Key)
	}
	if s.Count != 2 {
		t.Fatalf("汇总条数 = %d, want 2", s.Count)
	}
	// 标题里应能看到被合并的具体标题
	if !strings.Contains(s.Body, "容器离线 x1") {
		t.Fatalf("汇总正文缺少明细: %q", s.Body)
	}
	if !strings.Contains(s.Body, "另一容器离线 x1") {
		t.Fatalf("汇总正文缺少明细: %q", s.Body)
	}
}

// 窗口未到期时 Flush 不得提前吐出。
func TestAggregatorFlushBeforeWindowEndIsEmpty(t *testing.T) {
	agg := NewAggregator(30 * time.Second)
	start := time.Now()
	agg.Offer("rule-1", "k", "critical", "a", start)
	agg.Offer("rule-1", "k", "critical", "b", start.Add(time.Second))

	if got := agg.Flush(start.Add(5 * time.Second)); len(got) != 0 {
		t.Fatalf("窗口未结束不应汇总, got %+v", got)
	}
}

// 空 key 表示不参与聚合，必须直接放行。
func TestAggregatorEmptyKeyAlwaysSends(t *testing.T) {
	agg := NewAggregator(30 * time.Second)
	now := time.Now()
	for i := 0; i < 3; i++ {
		d := agg.Offer("rule-1", "", "critical", "无分组", now)
		if !d.Send {
			t.Fatalf("空 key 应直接发送, got %+v", d)
		}
	}
}

// 分组 key 只取规则配置的字段；取不到值时返回空（宁可多发也不误合并）。
func TestGroupKeyOf(t *testing.T) {
	data := map[string]interface{}{
		"server_id": "srv-1",
		"container": map[string]interface{}{"name": "redis"},
	}

	if got := groupKeyOf([]string{"server_id"}, data); got != "srv-1" {
		t.Fatalf("got %q, want srv-1", got)
	}
	if got := groupKeyOf([]string{"server_id", "container.name"}, data); got != "srv-1|redis" {
		t.Fatalf("多字段拼接 got %q", got)
	}
	if got := groupKeyOf([]string{"missing"}, data); got != "" {
		t.Fatalf("取不到值应返回空串, got %q", got)
	}
	if got := groupKeyOf(nil, data); got != "" {
		t.Fatalf("未配置 group_by 应返回空串, got %q", got)
	}
}

// 规则里 group_by 的解析要兼容前端可能写入的几种形态。
func TestRuleGroupByParsing(t *testing.T) {
	cases := []struct {
		name        string
		suppression map[string]interface{}
		want        int
	}{
		{"nil", nil, 0},
		{"empty", map[string]interface{}{}, 0},
		{"string slice", map[string]interface{}{"group_by": []interface{}{"server_id"}}, 1},
		{"go slice", map[string]interface{}{"group_by": []string{"server_id", "container"}}, 2},
		{"single string", map[string]interface{}{"group_by": "server_id"}, 1},
		{"blank string", map[string]interface{}{"group_by": "  "}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := Rule{Suppression: tc.suppression}
			if got := len(ruleGroupBy(rule)); got != tc.want {
				t.Fatalf("group_by 长度 = %d, want %d", got, tc.want)
			}
		})
	}
}

// 未配置 group_by 时聚合器整体不介入（Service 层判断依赖这个前提）。
func TestRuleGroupByAbsentKeepsLegacyBehavior(t *testing.T) {
	rule := Rule{Suppression: map[string]interface{}{"repeat_count": 2, "silence_minutes": 30}}
	if got := ruleGroupBy(rule); len(got) != 0 {
		t.Fatalf("老规则不应被聚合, got %v", got)
	}
}
