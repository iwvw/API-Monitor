package aiagent

import (
	"strings"
	"testing"
)

// 计数起点为 0，且快照不 panic。
func TestMetricsInitialSnapshot(t *testing.T) {
	metrics := newMetrics()
	snapshot := metrics.Snapshot()

	if snapshot.ConvergenceRounds != 0 {
		t.Fatalf("初始收敛轮次应为 0，got %d", snapshot.ConvergenceRounds)
	}
	if snapshot.UptimeSeconds < 0 {
		t.Fatalf("运行时长不应为负，got %d", snapshot.UptimeSeconds)
	}
	if len(snapshot.RecentErrors) != 0 {
		t.Fatalf("初始不应有错误记录，got %v", snapshot.RecentErrors)
	}
}

// 计数能被累加并反映在快照里。
func TestMetricsAccumulate(t *testing.T) {
	metrics := newMetrics()
	metrics.convergenceRounds.Add(3)
	metrics.convergenceStarts.Add(2)
	metrics.convergenceStops.Add(1)
	metrics.convergenceFailures.Add(4)
	metrics.lifecycleStarts.Add(5)
	metrics.lifecycleStops.Add(6)
	metrics.lifecycleFailures.Add(7)

	snapshot := metrics.Snapshot()
	if snapshot.ConvergenceRounds != 3 {
		t.Fatalf("ConvergenceRounds = %d, want 3", snapshot.ConvergenceRounds)
	}
	if snapshot.ConvergenceStarts != 2 || snapshot.ConvergenceStops != 1 {
		t.Fatalf("收敛启停计数不对: %+v", snapshot)
	}
	if snapshot.ConvergenceFailures != 4 {
		t.Fatalf("ConvergenceFailures = %d, want 4", snapshot.ConvergenceFailures)
	}
	if snapshot.LifecycleStarts != 5 || snapshot.LifecycleStops != 6 {
		t.Fatalf("生命周期计数不对: %+v", snapshot)
	}
	if snapshot.LifecycleFailures != 7 {
		t.Fatalf("LifecycleFailures = %d, want 7", snapshot.LifecycleFailures)
	}
}

// 最近错误必须有界：否则长期运行会因错误堆积导致内存增长。
func TestMetricsRecentErrorsAreBounded(t *testing.T) {
	metrics := newMetrics()
	for index := 0; index < maxRecentErrors*3; index++ {
		metrics.recordError("boom")
	}

	snapshot := metrics.Snapshot()
	if len(snapshot.RecentErrors) != maxRecentErrors {
		t.Fatalf("错误记录应保留最近 %d 条，got %d", maxRecentErrors, len(snapshot.RecentErrors))
	}
}

// 错误记录带时间戳，便于对照日志定位。
func TestMetricsRecordErrorIncludesTimestamp(t *testing.T) {
	metrics := newMetrics()
	metrics.recordError("instance inst_1 start failed")

	snapshot := metrics.Snapshot()
	if len(snapshot.RecentErrors) != 1 {
		t.Fatalf("应记录 1 条错误，got %d", len(snapshot.RecentErrors))
	}
	entry := snapshot.RecentErrors[0]
	if !strings.Contains(entry, "inst_1") {
		t.Fatalf("错误记录应保留原始信息，got %q", entry)
	}
	// RFC3339 以 4 位年份开头，可据此判断时间戳存在。
	if len(entry) < 4 || entry[4] != '-' {
		t.Fatalf("错误记录应以 RFC3339 时间戳开头，got %q", entry)
	}
}

// nil 接收者不应 panic：Service 可能未初始化 metrics（如某些测试路径）。
func TestMetricsNilSafe(t *testing.T) {
	var metrics *Metrics
	if snapshot := metrics.Snapshot(); snapshot.ConvergenceRounds != 0 {
		t.Fatal("nil metrics 快照应为零值")
	}
	// recordError 在 nil 接收者上必须静默返回。
	metrics.recordError("ignored")
}

// 最后收敛时刻初始为 0（表示尚未收敛过），便于前端区分「从未运行」。
func TestMetricsLastConvergenceZeroBeforeFirstRound(t *testing.T) {
	metrics := newMetrics()
	if got := metrics.Snapshot().LastConvergenceAt; got != 0 {
		t.Fatalf("首次收敛前 LastConvergenceAt 应为 0，got %d", got)
	}
}

// Service.Metrics 可访问且返回快照。
func TestServiceMetricsAccessible(t *testing.T) {
	service := newTestService(t)
	snapshot := service.Metrics()
	if snapshot.ConvergenceRounds != 0 {
		t.Fatalf("新服务不应有收敛记录，got %d", snapshot.ConvergenceRounds)
	}
}
