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
		cpu_load REAL,
		cpu_cores INTEGER,
		cpu_threads INTEGER,
		cpu_temp REAL,
		cpu_power REAL,
		mem_used INTEGER,
		mem_total INTEGER,
		mem_usage REAL,
		disk_used INTEGER,
		disk_total INTEGER,
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
		net_rx INTEGER,
		net_tx INTEGER,
		recorded_at TEXT
	)`)
	if err != nil {
		t.Fatalf("create server_metrics_history: %v", err)
	}
}

// seedHostMetricsAt 在指定时间戳写一条整机指标（时间戳可控，便于验证「取最新」）。
func seedHostMetricsAt(t *testing.T, db *sql.DB, serverID string, cpu, mem, disk float64, platform, recordedAt string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads,
		mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage,
		platform, recorded_at
	) VALUES (?, ?, NULL, 8, 16, ?, ?, ?, ?, ?, ?, ?, ?)`,
		serverID, cpu, int64(4)<<30, int64(16)<<30, mem,
		int64(100)<<30, int64(200)<<30, disk, platform, recordedAt)
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
	) VALUES (?, ?, NULL, 8, 16, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		serverID, cpu, int64(4)<<30, int64(16)<<30, mem,
		int64(100)<<30, int64(200)<<30, disk, platform)
	if err != nil {
		t.Fatalf("seed metrics: %v", err)
	}
}

// TestInstanceHostMetricsReturnsLatest 验证：授权用户能读到该实例所在主机的
// 最新整机指标，字段映射正确。
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
		Success bool                  `json:"success"`
		Data    *instanceHostMetrics  `json:"data"`
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
	if got.RecordedAt == "" {
		t.Error("RecordedAt 为空，前端无法判断数据新鲜度")
	}
	// cpu_load 写的是 NULL：必须留空而不是变成 0（0 会被误读成「空载」）。
	if got.CPULoad != nil {
		t.Errorf("CPULoad = %v, want nil (NULL 列不应被当成 0)", *got.CPULoad)
	}
	if got.MemTotal == nil || *got.MemTotal != int64(16)<<30 {
		t.Errorf("MemTotal = %v, want %d", got.MemTotal, int64(16)<<30)
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
		Success bool           `json:"success"`
		Data    *struct{}      `json:"data"`
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
