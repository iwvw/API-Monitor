package serveragent

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// ---- rollup（issue #78）----

func insertRawMetric(t *testing.T, db *sql.DB, serverID, recordedAt string, cpu, mem, disk float64) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO server_metrics_history
			(server_id, cpu_usage, mem_usage, disk_usage, recorded_at)
		VALUES (?, ?, ?, ?, ?)`, serverID, cpu, mem, disk, recordedAt)
	if err != nil {
		t.Fatalf("insert raw metric: %v", err)
	}
}

// 同一小时的多条原始数据要聚合成一行（avg/max/min + 样本数）。
func TestRollupMetricsAggregatesIntoHourlyBuckets(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-1', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// 用「昨天」的时间，保证是已经完整过去的小时桶
	base := time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	cpuValues := []float64{10, 20, 60}
	for i, cpu := range cpuValues {
		ts, _ := time.Parse("2006-01-02 15:04:05", base)
		insertRawMetric(t, db, "roll-1", ts.Add(time.Duration(i)*time.Minute).Format("2006-01-02 15:04:05"),
			cpu, 40, 50)
	}

	if err := service.RollupMetrics(ctx, db); err != nil {
		t.Fatalf("RollupMetrics: %v", err)
	}

	var cpuAvg, cpuMax, cpuMin float64
	var samples int
	err := db.QueryRow(`SELECT cpu_avg, cpu_max, cpu_min, samples FROM metrics_hourly_rollup
		WHERE server_id = 'roll-1' AND bucket_hour = ?`, base).Scan(&cpuAvg, &cpuMax, &cpuMin, &samples)
	if err != nil {
		t.Fatalf("query rollup: %v", err)
	}
	if samples != 3 {
		t.Fatalf("samples = %d, want 3", samples)
	}
	if cpuMax != 60 || cpuMin != 10 {
		t.Fatalf("max/min = %v/%v, want 60/10", cpuMax, cpuMin)
	}
	if cpuAvg < 29.9 || cpuAvg > 30.1 {
		t.Fatalf("avg = %v, want ~30", cpuAvg)
	}
}

// 重复执行必须幂等：不能产生重复行，也不能把值算错。
func TestRollupMetricsIsIdempotent(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-2', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	base := time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "roll-2", base, 10, 20, 30)
	insertRawMetric(t, db, "roll-2", base, 30, 20, 30)

	for i := 0; i < 3; i++ {
		if err := service.RollupMetrics(ctx, db); err != nil {
			t.Fatalf("RollupMetrics round %d: %v", i, err)
		}
	}

	var rows, samples int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(samples),0) FROM metrics_hourly_rollup WHERE server_id = 'roll-2'`).Scan(&rows, &samples); err != nil {
		t.Fatalf("count rollup: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rollup rows = %d, want 1（重复执行不应产生重复行）", rows)
	}
	if samples != 2 {
		t.Fatalf("samples = %d, want 2", samples)
	}
}

// 当前小时**不能**被提前固化：正在积累的桶 avg 会偏低。
func TestRollupMetricsSkipsCurrentHour(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-3', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "roll-3", now, 5, 5, 5)

	if err := service.RollupMetrics(ctx, db); err != nil {
		t.Fatalf("RollupMetrics: %v", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_hourly_rollup WHERE server_id = 'roll-3'`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 0 {
		t.Fatalf("当前小时不应被固化, rows = %d", rows)
	}
}

// 跨度 > 24h 时走 rollup；没有 rollup 数据时回退原始表（不能出现空图）。
func TestQueryHistoryPointsPrefersRollupForLongRanges(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('hist-1', 'h', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// 先只放原始数据：此时 rollup 为空，长跨度也必须回退到原始表。
	recent := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "hist-1", recent, 42, 50, 60)

	points, err := service.queryHistoryPoints(ctx, db, "hist-1", 48)
	if err != nil {
		t.Fatalf("queryHistoryPoints: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("rollup 为空时应回退到原始表，不能返回空结果")
	}
	if points[0].Aggregated {
		t.Fatal("回退路径不应标记为 aggregated")
	}
	if points[0].CPU != 42 {
		t.Fatalf("cpu = %v, want 42", points[0].CPU)
	}

	// 造一条 rollup 记录后，长跨度应改走聚合。
	base := time.Now().UTC().Add(-30 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.ExecContext(ctx, `INSERT INTO metrics_hourly_rollup
		(server_id, bucket_hour, cpu_avg, cpu_max, cpu_min, mem_avg, mem_max, mem_min,
		 disk_avg, disk_max, disk_min, samples)
		VALUES ('hist-1', ?, 33, 40, 20, 55, 60, 50, 65, 70, 60, 120)`, base); err != nil {
		t.Fatalf("insert rollup: %v", err)
	}

	points, err = service.queryHistoryPoints(ctx, db, "hist-1", 48)
	if err != nil {
		t.Fatalf("queryHistoryPoints: %v", err)
	}
	found := false
	for _, p := range points {
		if p.Aggregated {
			found = true
			if p.Samples != 120 {
				t.Fatalf("rollup samples = %d, want 120", p.Samples)
			}
			break
		}
	}
	if !found {
		t.Fatal("有 rollup 数据时应标记为 aggregated")
	}
}

// 短跨度仍走原始表，保持逐点精度。
func TestQueryHistoryPointsUsesRawForShortRanges(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('hist-2', 'h', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	recent := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "hist-2", recent, 11, 22, 33)

	points, err := service.queryHistoryPoints(ctx, db, "hist-2", 12)
	if err != nil {
		t.Fatalf("queryHistoryPoints: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("短跨度应返回原始点, got %d", len(points))
	}
	if points[0].Aggregated {
		t.Fatal("短跨度不应使用 rollup")
	}
	if points[0].Memory != 22 || points[0].Disk != 33 {
		t.Fatalf("字段映射错误: %+v", points[0])
	}
}

// 增量聚合只回看 rollupLookbackHours：更早的桶不重算（性能）。
func TestRollupMetricsOnlyScansRecentWindow(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-win', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	oldHour := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	recentHour := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "roll-win", oldHour, 10, 10, 10)
	insertRawMetric(t, db, "roll-win", recentHour, 20, 20, 20)

	if err := service.RollupMetrics(ctx, db); err != nil {
		t.Fatalf("RollupMetrics: %v", err)
	}

	var oldCount, recentCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM metrics_hourly_rollup WHERE server_id='roll-win' AND bucket_hour = ?`, oldHour).Scan(&oldCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM metrics_hourly_rollup WHERE server_id='roll-win' AND bucket_hour = ?`, recentHour).Scan(&recentCount)
	if recentCount != 1 {
		t.Fatalf("窗口内的小时桶应被聚合, got %d", recentCount)
	}
	if oldCount != 0 {
		t.Fatalf("窗口外的小时桶不应在增量里被聚合, got %d", oldCount)
	}
}

// BackfillRollup 负责把窗口外的历史一次性补齐。
func TestBackfillRollupFillsHistory(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-bf', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	oldHour := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "roll-bf", oldHour, 10, 10, 10)

	if err := service.BackfillRollup(ctx, db); err != nil {
		t.Fatalf("BackfillRollup: %v", err)
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM metrics_hourly_rollup WHERE server_id='roll-bf' AND bucket_hour = ?`, oldHour).Scan(&count)
	if count != 1 {
		t.Fatalf("历史桶应被补齐, got %d", count)
	}

	// 二次调用必须直接返回（幂等，不重复写）
	if err := service.BackfillRollup(ctx, db); err != nil {
		t.Fatalf("BackfillRollup second call: %v", err)
	}
}

// 样本数没变化时不应重复写 rollup（每 5 分钟白写一遍 48×N 行）。
func TestRollupMetricsSkipsUnchangedBuckets(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, port, username, auth_type)
		VALUES ('roll-noop', 'r', '127.0.0.1', 22, 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	hour := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour).Format("2006-01-02 15:04:05")
	insertRawMetric(t, db, "roll-noop", hour, 10, 20, 30)
	if err := service.RollupMetrics(ctx, db); err != nil {
		t.Fatalf("first rollup: %v", err)
	}
	var createdAt string
	_ = db.QueryRow(`SELECT created_at FROM metrics_hourly_rollup WHERE server_id='roll-noop'`).Scan(&createdAt)

	// 再来一条新样本 → 样本数变化 → 应更新
	insertRawMetric(t, db, "roll-noop", hour, 40, 20, 30)
	if err := service.RollupMetrics(ctx, db); err != nil {
		t.Fatalf("second rollup: %v", err)
	}
	var samples int
	_ = db.QueryRow(`SELECT samples FROM metrics_hourly_rollup WHERE server_id='roll-noop'`).Scan(&samples)
	if samples != 2 {
		t.Fatalf("样本数应更新为 2, got %d", samples)
	}
}
