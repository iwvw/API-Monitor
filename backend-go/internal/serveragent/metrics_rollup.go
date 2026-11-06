package serveragent

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// 指标降采样（rollup）。
//
// 为什么需要
// ----------
// 历史查询（api.go 的 getMetricsHistory）一直是「按 hours 全量取原始行」，
// 没有点数上限也没有聚合。30 天保留 × 1.5 秒上报 × N 台主机意味着单台
// 主机一个月的原始行就有 170 万条；用户点开「最近 30 天」等于让 SQLite
// 扫全表并把几百万个点丢给前端画图。
//
// 方案
// ----
//   - 独立表 `metrics_hourly_rollup`，按 (server_id, hour) 存 avg/max/min
//     与样本数，主键去重（重复聚合幂等）。
//   - 查询侧：跨度 > rollupThresholdHours 时改查 rollup，短跨度仍走原始表
//     （短跨度下原始点更真实，且行数可控）。
//   - 聚合由 `RollupMetrics` 在清理循环里顺带执行，只处理「整点已过去」的
//     小时桶，避免把正在积累的当前小时提前固化。
//
// 刻意不做的事
// ------------
//   - 不引入后台常驻 ticker：复用已有的清理周期（默认 5 分钟）就够，
//     新起一个 goroutine 只会增加调度与锁竞争。
//   - 不自动删除原始数据：保留策略仍由 settings 的 log_retention_days 管，
//     两者职责分离（rollup 负责"查得快"，清理负责"占得少"）。

const rollupTable = "metrics_hourly_rollup"

// rollupThresholdHours 是「超过多少小时的历史改走 rollup」的分界。
//
// 24 小时：一天内原始点（1.5s 间隔约 5.7 万点/台）仍然太大，
// 所以实际上跨天就该用 rollup；分界放在 24 是给「看今天」留原始精度。
const rollupThresholdHours = 24

func ensureRollupSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ` + rollupTable + ` (
			server_id TEXT NOT NULL,
			bucket_hour TEXT NOT NULL,
			cpu_avg REAL, cpu_max REAL, cpu_min REAL,
			mem_avg REAL, mem_max REAL, mem_min REAL,
			disk_avg REAL, disk_max REAL, disk_min REAL,
			samples INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (server_id, bucket_hour)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_rollup_hour ON ` + rollupTable + `(bucket_hour DESC)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create metrics rollup schema: %w", err)
		}
	}
	return nil
}

// RollupMetrics 把还没汇总过的整点小时桶写入 rollup 表。
//
// 幂等：用 INSERT ... ON CONFLICT DO UPDATE 覆盖，重复执行结果一致
// （即使中途崩溃、下一轮重跑也不会产生重复行或错值）。
func (s *Service) RollupMetrics(ctx context.Context, db *sql.DB) error {
	if err := ensureRollupSchema(ctx, db); err != nil {
		return err
	}
	// 只汇总「已完整过去」的小时：当前小时还在积累，提前固化会让 avg 偏低。
	_, err := db.ExecContext(ctx, `
		INSERT INTO `+rollupTable+`
			(server_id, bucket_hour, cpu_avg, cpu_max, cpu_min,
			 mem_avg, mem_max, mem_min, disk_avg, disk_max, disk_min, samples)
		SELECT
			server_id,
			strftime('%Y-%m-%d %H:00:00', recorded_at) AS bucket_hour,
			AVG(cpu_usage), MAX(cpu_usage), MIN(cpu_usage),
			AVG(mem_usage), MAX(mem_usage), MIN(mem_usage),
			AVG(disk_usage), MAX(disk_usage), MIN(disk_usage),
			COUNT(*)
		FROM server_metrics_history
		WHERE recorded_at >= datetime('now', '-30 days')
		  AND recorded_at < strftime('%Y-%m-%d %H:00:00', 'now')
		GROUP BY server_id, bucket_hour
		ON CONFLICT(server_id, bucket_hour) DO UPDATE SET
			cpu_avg = excluded.cpu_avg, cpu_max = excluded.cpu_max, cpu_min = excluded.cpu_min,
			mem_avg = excluded.mem_avg, mem_max = excluded.mem_max, mem_min = excluded.mem_min,
			disk_avg = excluded.disk_avg, disk_max = excluded.disk_max, disk_min = excluded.disk_min,
			samples = excluded.samples
	`)
	if err != nil {
		return fmt.Errorf("rollup metrics: %w", err)
	}
	return nil
}

// RollupPoint 是查询层统一返回的一个采样点（原始行与 rollup 行都能映射成它）。
type RollupPoint struct {
	Time       string  `json:"time"`
	CPU        float64 `json:"cpu"`
	CPUMax     float64 `json:"cpuMax,omitempty"`
	Memory     float64 `json:"memory"`
	Disk       float64 `json:"disk"`
	NetRx      float64 `json:"networkRx"`
	NetTx      float64 `json:"networkTx"`
	GPUTemp    float64 `json:"gpuTemp"`
	Aggregated bool    `json:"aggregated,omitempty"`
	Samples    int     `json:"samples,omitempty"`
}

// queryHistoryPoints 在原始表与 rollup 表之间选一条路径。
//
// 选择规则：跨度 > rollupThresholdHours 且 rollup 里有数据 → 走 rollup。
// rollup 为空（刚部署、还没跑过聚合）时回退原始表，避免「图表突然空白」。
func (s *Service) queryHistoryPoints(ctx context.Context, db *sql.DB, serverID string, hours int) ([]RollupPoint, error) {
	if hours > rollupThresholdHours {
		points, err := queryRollupPoints(ctx, db, serverID, hours)
		if err == nil && len(points) > 0 {
			return points, nil
		}
		// rollup 不可用/为空时静默回退：宁可慢，也不要空图。
		if err != nil {
			applog.Warn(ctx, "serveragent", "rollup query failed, falling back to raw",
				"server_id", serverID, "hours", hours, "error", err.Error())
		}
	}
	return queryRawPoints(ctx, db, serverID, hours)
}

func queryRollupPoints(ctx context.Context, db *sql.DB, serverID string, hours int) ([]RollupPoint, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT bucket_hour, cpu_avg, cpu_max, mem_avg, disk_avg, samples
		FROM `+rollupTable+`
		WHERE server_id = ? AND bucket_hour >= datetime('now', '-' || ? || ' hours')
		ORDER BY bucket_hour ASC
	`, serverID, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]RollupPoint, 0, 64)
	for rows.Next() {
		var point RollupPoint
		var cpuAvg, cpuMax, memAvg, diskAvg sql.NullFloat64
		var samples sql.NullInt64
		if err := rows.Scan(&point.Time, &cpuAvg, &cpuMax, &memAvg, &diskAvg, &samples); err != nil {
			return nil, err
		}
		point.CPU = cpuAvg.Float64
		point.CPUMax = cpuMax.Float64
		point.Memory = memAvg.Float64
		point.Disk = diskAvg.Float64
		point.Samples = int(samples.Int64)
		point.Aggregated = true
		points = append(points, point)
	}
	return points, rows.Err()
}

func queryRawPoints(ctx context.Context, db *sql.DB, serverID string, hours int) ([]RollupPoint, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT recorded_at, cpu_usage, mem_usage, disk_usage, net_rx, net_tx, gpu_temp
		FROM server_metrics_history
		WHERE server_id = ? AND recorded_at >= datetime('now', '-' || ? || ' hours')
		ORDER BY recorded_at ASC
	`, serverID, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]RollupPoint, 0, 256)
	for rows.Next() {
		var point RollupPoint
		var cpu, mem, disk, netRx, netTx, gpuTemp sql.NullFloat64
		if err := rows.Scan(&point.Time, &cpu, &mem, &disk, &netRx, &netTx, &gpuTemp); err != nil {
			return nil, err
		}
		point.CPU = cpu.Float64
		point.Memory = mem.Float64
		point.Disk = disk.Float64
		point.NetRx = netRx.Float64
		point.NetTx = netTx.Float64
		point.GPUTemp = gpuTemp.Float64
		points = append(points, point)
	}
	return points, rows.Err()
}

// RollupRetention 返回当前 rollup 表保留多少天（与指标保留期一致）。
func RollupRetention() time.Duration {
	return 30 * 24 * time.Hour
}
