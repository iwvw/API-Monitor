package aiagent

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 是模块运行期的轻量计数，用于观测后台收敛与进程管理的健康度。
//
// 只保留「出问题时需要看」的计数，不做完整指标体系：
//   - 收敛轮次与其中的启停动作数 → 判断后台循环是否在正常工作；
//   - 收敛失败数 → 判断是否有实例长期无法达成期望状态；
//   - 生命周期操作计数 → 判断用户操作量级与失败率。
//
// 全部用原子操作，读侧无锁，避免在 HTTP 路径上引入争用。
type Metrics struct {
	// 收敛循环
	convergenceRounds   atomic.Int64
	convergenceStarts   atomic.Int64
	convergenceStops    atomic.Int64
	convergenceFailures atomic.Int64
	// 最近一次收敛完成时刻（Unix 秒）
	lastConvergenceAt atomic.Int64

	// 生命周期操作（含单实例与批量）
	lifecycleStarts   atomic.Int64
	lifecycleStops    atomic.Int64
	lifecycleFailures atomic.Int64

	mu         sync.Mutex
	startedAt  time.Time
	lastErrors []string
}

// maxRecentErrors 是保留的最近错误条数，避免无界增长。
const maxRecentErrors = 10

func newMetrics() *Metrics {
	return &Metrics{startedAt: time.Now()}
}

// MetricsSnapshot 是对外暴露的只读快照。
type MetricsSnapshot struct {
	UptimeSeconds       int64    `json:"uptimeSeconds"`
	ConvergenceRounds   int64    `json:"convergenceRounds"`
	ConvergenceStarts   int64    `json:"convergenceStarts"`
	ConvergenceStops    int64    `json:"convergenceStops"`
	ConvergenceFailures int64    `json:"convergenceFailures"`
	LastConvergenceAt   int64    `json:"lastConvergenceAt,omitempty"`
	LifecycleStarts     int64    `json:"lifecycleStarts"`
	LifecycleStops      int64    `json:"lifecycleStops"`
	LifecycleFailures   int64    `json:"lifecycleFailures"`
	RecentErrors        []string `json:"recentErrors,omitempty"`
}

// Snapshot 返回当前计数快照。
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	lastConvergence := m.lastConvergenceAt.Load()
	m.mu.Lock()
	errors := append([]string(nil), m.lastErrors...)
	m.mu.Unlock()

	return MetricsSnapshot{
		UptimeSeconds:       int64(time.Since(m.startedAt).Seconds()),
		ConvergenceRounds:   m.convergenceRounds.Load(),
		ConvergenceStarts:   m.convergenceStarts.Load(),
		ConvergenceStops:    m.convergenceStops.Load(),
		ConvergenceFailures: m.convergenceFailures.Load(),
		LastConvergenceAt:   lastConvergence,
		LifecycleStarts:     m.lifecycleStarts.Load(),
		LifecycleStops:      m.lifecycleStops.Load(),
		LifecycleFailures:   m.lifecycleFailures.Load(),
		RecentErrors:        errors,
	}
}

// recordError 记录一条最近错误（带时间戳），供排查使用。
func (m *Metrics) recordError(message string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErrors = append(m.lastErrors, time.Now().UTC().Format(time.RFC3339)+" "+message)
	if len(m.lastErrors) > maxRecentErrors {
		m.lastErrors = m.lastErrors[len(m.lastErrors)-maxRecentErrors:]
	}
}

// Metrics 返回模块运行期计数的快照。
func (s *Service) Metrics() MetricsSnapshot {
	return s.metrics.Snapshot()
}
