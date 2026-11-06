package notification

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 告警聚合（group_by）与同级抑制。
//
// 为什么需要
// ----------
// 一台宿主机宕机时，它上面的 20 个容器会各自触发一次「容器离线」，
// 用户收到 21 条通知，而真正要看的只有 1 条「主机宕机」。这在带
// 容器/多实例的部署里是最主要的告警噪音来源。
//
// 设计
// ----
//   - 规则可配 `group_by: ["server_id"]`（eventData 的字段路径）。
//     同一 group 在窗口期内的重复告警**只发第一条**，其余计数；
//     窗口结束时补发一条汇总（含被合并的条数）。
//   - 窗口内如果先收到高严重级别（critical），随后同 group 的低级别
//     （warning/info）不再单独发送 —— 相当于最小可用的「抑制」：
//     宿主机 critical 已经把问题说清楚了。
//   - 未配置 group_by 时完全走原路径，零行为变化。
//
// 边界
// ----
//  - 状态只在进程内存里：进程重启后计数清零，最多多发一条汇总。可接受。
//  - 恢复（resolve）事件**永不聚合**：漏掉恢复通知会让告警永远悬着。
//  - 窗口按 group 独立计时，不是全局滑动窗口。
//  - 并发安全：Trigger 可能来自多个监控协程，全部走互斥锁。

// DefaultGroupWindow 是未显式配置时的聚合窗口。
//
// 取 30 秒：足够覆盖一次「宿主机连带其容器一起失联」的检测抖动，
// 又不至于让首条告警迟到太多（首条是立即发的，只有汇总会延后）。
const DefaultGroupWindow = 30 * time.Second

// severityRank 用于「高级别抑制低级别」的比较；未知级别按最低处理。
func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// groupKeyOf 按规则配置的 group_by 路径从 eventData 取值拼成 key。
//
// 返回空串表示「这条事件不参与聚合」——调用方据此直接走原路径。
// 任一路径取不到值也返回空串：宁可多发，也不要把不相关的告警
// 因为「都取不到 server_id」而合并成一条（那会丢掉真实信息）。
func groupKeyOf(groupBy []string, eventData map[string]interface{}) string {
	if len(groupBy) == 0 {
		return ""
	}
	parts := make([]string, 0, len(groupBy))
	for _, path := range groupBy {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		value := pathValue(eventData, path)
		if value == nil {
			return ""
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return ""
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}

// suppressedEntry 记录窗口内被合并掉的一条告警。
type suppressedEntry struct {
	Title    string
	Count    int
	Severity string
}

type groupBucket struct {
	ruleID     string
	key        string
	windowEnds time.Time
	peakRank   int
	// 被聚合掉的条目，按标题聚合计数，便于汇总时列出「容器离线 x5」。
	suppressed map[string]*suppressedEntry
}

// AggregateDecision 是 Offer 的返回：调用方据此决定要不要真的发。
type AggregateDecision struct {
	// Send 为 true 表示这条要立即发送。
	Send bool
	// Reason 仅用于日志/诊断：first / upgraded / merged。
	Reason string
}

// Aggregator 持有所有 group 的窗口状态。
type Aggregator struct {
	mu        sync.Mutex
	buckets   map[string]*groupBucket
	window    time.Duration
	nowFn     func() time.Time
	maxKeys   int
	lastSweep time.Time
}

// NewAggregator 创建聚合器；window<=0 时用 DefaultGroupWindow。
func NewAggregator(window time.Duration) *Aggregator {
	if window <= 0 {
		window = DefaultGroupWindow
	}
	return &Aggregator{
		buckets: make(map[string]*groupBucket),
		window:  window,
		nowFn:   time.Now,
		// 上限防止异常 eventData（如每条 key 都不一样的 UUID）把内存撑爆。
		maxKeys: 2000,
	}
}

func bucketID(ruleID, key string) string { return ruleID + "\x00" + key }

// Offer 处理一次触发，返回是否应立刻发送。
//
// 语义：
//   - 窗口内首条 → 立即发（Reason=first）。
//   - 同 group 已有更高 severity 且本条更低 → 合并（Send=false）。
//   - 本条 severity 更高 → 立即发（Reason=upgraded），并把之前的合并进来汇总。
//   - 同 group 已有同级 → 合并（Send=false）。
func (a *Aggregator) Offer(ruleID, key, severity, title string, now time.Time) AggregateDecision {
	if key == "" {
		return AggregateDecision{Send: true, Reason: "no-group"}
	}
	rank := severityRank(severity)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.sweepLocked(now)

	id := bucketID(ruleID, key)
	bucket, exists := a.buckets[id]
	if !exists {
		if len(a.buckets) >= a.maxKeys {
			// 超过上限时退化成「都发」，宁可吵也不要悄悄丢告警。
			return AggregateDecision{Send: true, Reason: "overflow"}
		}
		a.buckets[id] = &groupBucket{
			ruleID:     ruleID,
			key:        key,
			windowEnds: now.Add(a.window),
			peakRank:   rank,
			suppressed: make(map[string]*suppressedEntry),
		}
		return AggregateDecision{Send: true, Reason: "first"}
	}

	// 窗口已过期（sweep 之外的双保险）
	if !now.Before(bucket.windowEnds) {
		bucket.windowEnds = now.Add(a.window)
		bucket.peakRank = rank
		bucket.suppressed = make(map[string]*suppressedEntry)
		return AggregateDecision{Send: true, Reason: "first"}
	}

	if rank > bucket.peakRank {
		bucket.peakRank = rank
		return AggregateDecision{Send: true, Reason: "upgraded"}
	}

	// 同级或更低：合并计数
	entry, ok := bucket.suppressed[title]
	if !ok {
		entry = &suppressedEntry{Title: title, Severity: severity}
		bucket.suppressed[title] = entry
	}
	entry.Count++
	return AggregateDecision{Send: false, Reason: "merged"}
}

// Summary 是窗口到期后要补发的一条汇总。
type Summary struct {
	RuleID string
	Key    string
	Title  string
	Body   string
	Count  int
}

// Flush 取出所有已过期的窗口并生成汇总；没有需要补发的窗口时返回空切片。
//
// 只返回「被合并过至少一条」的窗口：没有合并就没有汇总，避免给
// 正常单条告警补发一条无意义的「汇总」。
func (a *Aggregator) Flush(now time.Time) []Summary {
	a.mu.Lock()
	defer a.mu.Unlock()

	var out []Summary
	for id, bucket := range a.buckets {
		if now.Before(bucket.windowEnds) {
			continue
		}
		delete(a.buckets, id)
		if len(bucket.suppressed) == 0 {
			continue
		}

		titles := make([]string, 0, len(bucket.suppressed))
		total := 0
		for title, entry := range bucket.suppressed {
			titles = append(titles, fmt.Sprintf("%s x%d", title, entry.Count))
			total += entry.Count
		}
		// 稳定顺序，便于阅读与测试（map 遍历顺序随机）。
		sort.Strings(titles)

		out = append(out, Summary{
			RuleID: bucket.ruleID,
			Key:    bucket.key,
			Title:  "告警汇总（窗口期内已合并）",
			Body:   fmt.Sprintf("分组 %s 在 %s 内另有 %d 条告警被合并：\n%s", bucket.key, a.window, total, strings.Join(titles, "\n")),
			Count:  total,
		})
	}
	return out
}

// sweepLocked 清掉早就过期且没有任何合并内容的窗口，避免 map 无限增长。
func (a *Aggregator) sweepLocked(now time.Time) {
	// 每 100 次 Offer 才扫一遍，避免每次 O(n)。
	if !a.lastSweep.IsZero() && now.Sub(a.lastSweep) < 10*time.Second {
		return
	}
	a.lastSweep = now
	for id, bucket := range a.buckets {
		if now.After(bucket.windowEnds) && len(bucket.suppressed) == 0 {
			delete(a.buckets, id)
		}
	}
}

// Len 返回当前活跃窗口数（测试与诊断用）。
func (a *Aggregator) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.buckets)
}
