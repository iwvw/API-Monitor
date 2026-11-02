package aiagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// issueTokenGranted 建用户 → 授权该实例 → 发令牌。
//
// 授权是必需的：requireInstanceAccess 对非管理员按「是否被授权该实例」判定，
// 未授权一律返回 404（与 status 接口同一套语义）。
func issueTokenGranted(t *testing.T, service *Service, db *sql.DB, username, instanceID string) string {
	t.Helper()
	ctx := context.Background()
	user, err := service.createUser(ctx, db, username, "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{instanceID}); err != nil {
		t.Fatalf("setGrantsForUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "test-device", "")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	return plain
}

// boolPtr 便于在 payload 里表达「显式启用」。
func boolPtr(v bool) *bool { return &v }

// ensureMetricsTable 在测试库里建出 server_metrics_history。
//
// 生产环境该表由 serveragent 模块建在**同一个 data.db** 里（实测
// /opt/1panel/docker/compose/api-monitor/data/data.db 同时存在 server_metrics_history
// 与 aiagent_* 表），所以本模块直接读它。测试库是独立的，需要自己建。
func ensureMetricsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS server_metrics_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		cpu_usage REAL,
		-- 必须与 serveragent 的建表语句逐列一致（service_schema.go）。特别注意
		-- cpu_load / disk_used / disk_total 是 **TEXT**：写成 INTEGER/REAL 的话
		-- 测试库会「恰好」通过，而生产库每次 Scan 都失败、接口恒返回 500。
		cpu_load TEXT,
		cpu_cores INTEGER,
		cpu_threads INTEGER DEFAULT 0,
		cpu_temp REAL,
		cpu_power REAL,
		mem_used INTEGER,
		mem_total INTEGER,
		mem_usage REAL,
		disk_used TEXT,
		disk_total TEXT,
		disk_usage REAL,
		docker_installed INTEGER,
		docker_running INTEGER,
		docker_stopped INTEGER,
		gpu_usage REAL,
		gpu_mem_used INTEGER,
		gpu_mem_total INTEGER,
		gpu_temp REAL,
		gpu_power REAL,
		platform TEXT,
		net_rx REAL,
		net_tx REAL,
		recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create server_metrics_history: %v", err)
	}
}

// seedHostMetricsAt 在指定时间戳写一条整机指标（时间戳可控，便于验证「取最新」）。
//
// 写入形态与生产一致：cpu_load 是三段负载文本，disk_* 是带单位的容量串，
// recorded_at 是不带时区后缀的 UTC 字符串。
func seedHostMetricsAt(t *testing.T, db *sql.DB, serverID string, cpu, mem, disk float64, platform, recordedAt string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads,
		mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage,
		platform, recorded_at
	) VALUES (?, ?, '1.26 2.06 1.89', 8, 16, ?, ?, ?, ?, ?, ?, ?, ?)`,
		serverID, cpu, int64(4)<<30, int64(16)<<30, mem,
		"100.2 GB", "193.7 GB", disk, platform, recordedAt)
	if err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
}

// seedHostMetrics 往 server_metrics_history 写一条整机指标。
//
// 该表由 serveragent 模块维护；这里只写入本路由读取的那几列，其余留 NULL，
// 顺便验证读取侧对 NULL 的容错（Windows 主机没有 cpu_load，Linux 没有某些字段）。
func seedHostMetrics(t *testing.T, db *sql.DB, serverID string, cpu, mem, disk float64, platform string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads,
		mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage,
		platform, recorded_at
	) VALUES (?, ?, '1.26 2.06 1.89', 8, 16, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		serverID, cpu, int64(4)<<30, int64(16)<<30, mem,
		"100.2 GB", "193.7 GB", disk, platform)
	if err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
}

// TestInstanceHostMetricsReturnsLatest 验证：授权用户能读到该实例所在主机的
// 最新整机指标，字段映射正确（TEXT 列按字符串读、cpu_load 取 1 分钟段、
// recorded_at 补上 UTC 时区）。
func TestInstanceHostMetricsReturnsLatest(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-metrics-1",
		Provider: "opencode-v2",
		Label:    "metrics-host",
		Port:     4096,
		Enabled:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	ensureMetricsTable(t, db)

	// recorded_at 只到秒（serveragent 用 datetime('now') 写入）。两条记录在同一秒
	// 时单纯按时间排序结果不确定，因此查询里加了 id DESC 兜底。这里也显式给不同
	// 时间戳，让用例语义明确：验证的是「取最新」，而不是「依赖插入顺序」。
	seedHostMetricsAt(t, db, "server-metrics-1", 11.1, 22.2, 33.3, "linux", "2026-11-01 10:00:00")
	seedHostMetricsAt(t, db, "server-metrics-1", 52.3, 61.5, 70.1, "linux", "2026-11-01 10:00:30")

	plain := issueTokenGranted(t, service, db, "metrics-alice", instance.ID)

	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances/"+instance.ID+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Success bool                 `json:"success"`
		Data    *instanceHostMetrics `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if !payload.Success || payload.Data == nil {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}

	got := payload.Data
	if got.ServerID != "server-metrics-1" {
		t.Errorf("ServerID = %q, want server-metrics-1", got.ServerID)
	}
	// 必须是最新那条（52.3），不是先写的那条（11.1）。
	if got.CPUPercent == nil || *got.CPUPercent != 52.3 {
		cp := 0.0
		if got.CPUPercent != nil {
			cp = *got.CPUPercent
		}
		t.Errorf("CPUPercent = %v (recordedAt=%s), want 52.3 (latest record)", cp, got.RecordedAt)
	}
	if got.MemPercent == nil || *got.MemPercent != 61.5 {
		t.Errorf("MemPercent = %v, want 61.5", got.MemPercent)
	}
	if got.DiskPercent == nil || *got.DiskPercent != 70.1 {
		t.Errorf("DiskPercent = %v, want 70.1", got.DiskPercent)
	}
	if got.Platform != "linux" {
		t.Errorf("Platform = %q, want linux", got.Platform)
	}
	// recorded_at 源值是不带时区的 UTC，接口必须补成带 Z 的 RFC3339，
	// 否则客户端会按本地时间解析、把「新鲜度」算错好几个小时。
	if got.RecordedAt != "2026-11-01T10:00:30Z" {
		t.Errorf("RecordedAt = %q, want 2026-11-01T10:00:30Z", got.RecordedAt)
	}
	// cpu_load 是三段文本，取第一段（1 分钟）并保留原始串。
	if got.CPULoad == nil || *got.CPULoad != 1.26 {
		t.Errorf("CPULoad = %v, want 1.26", got.CPULoad)
	}
	if got.CPULoadRaw != "1.26 2.06 1.89" {
		t.Errorf("CPULoadRaw = %q, want 原始三段负载", got.CPULoadRaw)
	}
	// disk_* 是带单位的文本列，必须原样带回来，不能被当成数值解析。
	if got.DiskUsed != "100.2 GB" || got.DiskTotal != "193.7 GB" {
		t.Errorf("DiskUsed/DiskTotal = %q/%q, want 100.2 GB / 193.7 GB", got.DiskUsed, got.DiskTotal)
	}
	if got.MemTotal == nil || *got.MemTotal != int64(16)<<30 {
		t.Errorf("MemTotal = %v, want %d", got.MemTotal, int64(16)<<30)
	}
}

// TestInstanceHostMetricsTDDLFlexible 回归：TEXT 列里出现 NULL / 空串 / 非数值
// 时不能把整个接口打成 500，也不能编造 0。Windows 主机没有负载，某些主机
// 的 disk 字段可能为空。
func TestInstanceHostMetricsTDDLFlexible(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-nulls",
		Provider: "opencode-v2",
		Label:    "nulls",
		Port:     4098,
		Enabled:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	ensureMetricsTable(t, db)
	// cpu_load 为空串、disk 为 NULL —— 都不能让 Scan 失败。
	if _, err := db.ExecContext(ctx, `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads, mem_usage, platform, recorded_at
	) VALUES (?, 12.5, '', 8, 16, 40.0, 'Windows', '2026-11-01 10:00:00')`, "server-nulls"); err != nil {
		t.Fatalf("seed null-ish row: %v", err)
	}

	plain := issueTokenGranted(t, service, db, "metrics-nulls", instance.ID)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances/"+instance.ID+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool                 `json:"success"`
		Data    *instanceHostMetrics `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data == nil {
		t.Fatalf("want data, got %s", rec.Body.String())
	}
	if payload.Data.CPULoad != nil {
		t.Errorf("CPULoad = %v, want nil（空串/无负载不能变成 0）", *payload.Data.CPULoad)
	}
	if payload.Data.DiskUsed != "" || payload.Data.DiskTotal != "" {
		t.Errorf("DiskUsed/DiskTotal = %q/%q, want 空", payload.Data.DiskUsed, payload.Data.DiskTotal)
	}
}

// TestInstanceHostMetricsNoData 验证：主机还没上报过指标时返回 data:null，
// 而不是 404 或 500 —— 否则前端会把「暂无数据」显示成错误。
func TestInstanceHostMetricsNoData(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-no-metrics",
		Provider: "opencode-v2",
		Label:    "no-metrics",
		Port:     4097,
		Enabled:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	ensureMetricsTable(t, db)

	plain := issueTokenGranted(t, service, db, "metrics-bob", instance.ID)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances/"+instance.ID+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool      `json:"success"`
		Data    *struct{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Success || payload.Data != nil {
		t.Fatalf("want success with null data, got %s", rec.Body.String())
	}
}

// TestInstanceHostMetricsRequiresAuth 验证未带凭据时拒绝（与 status 接口一致）。
func TestInstanceHostMetricsRequiresAuth(t *testing.T) {
	service := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances/inst_x/metrics", nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}
