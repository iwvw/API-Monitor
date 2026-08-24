package settings

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

func newSwapTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

// 数据库导入/替换/恢复持有换库互斥时，压缩任务不得启动：
// VACUUM 重写库文件会截断正被写入/替换的文件导致库损坏。
func TestEnqueueVacuumBlockedWhileSwapHeld(t *testing.T) {
	service := newSwapTestService(t)
	swapMu := database.SwapMutex()
	swapMu.Lock()
	queued, err := service.enqueueVacuumTask(context.Background())
	swapMu.Unlock()
	if queued {
		t.Fatal("vacuum must not start while swap mutex is held")
	}
	if !errors.Is(err, errDatabaseSwapInProgress) {
		t.Fatalf("enqueueVacuumTask error = %v, want errDatabaseSwapInProgress", err)
	}
	if service.vacuumSnapshot()["running"] != false {
		t.Fatal("vacuum running flag must stay false after blocked enqueue")
	}
}

// 压缩任务必须全程持有换库互斥并在结束后释放；runVacuum 收尾先释放互斥
// 再清 running 标志，因此 running=false 蕴含互斥已释放。
func TestVacuumHoldsSwapMutexUntilDone(t *testing.T) {
	service := newSwapTestService(t)
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	queued, err := service.enqueueVacuumTask(ctx)
	if err != nil || !queued {
		t.Fatalf("enqueueVacuumTask queued=%v err=%v", queued, err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for service.vacuumSnapshot()["running"] == true {
		if time.Now().After(deadline) {
			t.Fatal("vacuum did not finish within 15s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !database.SwapMutex().TryLock() {
		t.Fatal("swap mutex must be released after vacuum finished")
	}
	database.SwapMutex().Unlock()
}

// 非法文件导入必须被拒且不影响现有库；合法库导入必须真实生效。
// 常驻逻辑句柄（held）保持 refs>0：连接池在失败后不被销毁，这是
// schema 失败粘池缺陷的触发条件。
func TestReplaceDatabaseRollbackRecoversPool(t *testing.T) {
	service := newSwapTestService(t)
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO system_config (key, value) VALUES ('rollback-marker', 'before')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	held, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	badImport := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(badImport, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.replaceDatabase(ctx, badImport); err == nil {
		t.Fatal("expected replaceDatabase to fail for a non-sqlite import file")
	}

	after, err := service.store.Open(ctx)
	if err != nil {
		t.Fatalf("open after failed import: %v", err)
	}
	var value string
	if err := after.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = 'rollback-marker'`).Scan(&value); err != nil {
		t.Fatalf("read marker after failed import: %v", err)
	}
	if value != "before" {
		t.Fatalf("marker after failed import = %q, want before", value)
	}
	if err := after.Close(); err != nil {
		t.Fatal(err)
	}

	// 合法库导入成功路径：替换生效且新库数据可读。
	goodImport := filepath.Join(t.TempDir(), "good.db")
	good, err := sql.Open("sqlite", goodImport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := good.ExecContext(ctx, `CREATE TABLE system_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := good.ExecContext(ctx, `INSERT INTO system_config (key, value) VALUES ('rollback-marker', 'imported')`); err != nil {
		t.Fatal(err)
	}
	if err := good.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.replaceDatabase(ctx, goodImport); err != nil {
		t.Fatalf("replaceDatabase with valid import: %v", err)
	}
	replaced, err := service.store.Open(ctx)
	if err != nil {
		t.Fatalf("open after successful import: %v", err)
	}
	defer replaced.Close()
	value = ""
	if err := replaced.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = 'rollback-marker'`).Scan(&value); err != nil {
		t.Fatalf("read marker after successful import: %v", err)
	}
	if value != "imported" {
		t.Fatalf("marker after successful import = %q, want imported", value)
	}
}

// cutoff 必须与表内存储格式同构：RFC3339 的 'T' 与 CURRENT_TIMESTAMP 的
// ' ' 字符序错位会把保留期首日整天的数据误删。
func TestEnforceLogLimitsCutoffMatchesStoredTimeFormat(t *testing.T) {
	service := newSwapTestService(t)
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE boundary_logs (id INTEGER PRIMARY KEY, created_at TEXT)`); err != nil {
		t.Fatal(err)
	}

	days := 30
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	keepRow := cutoff.Add(15 * time.Minute)
	if keepRow.Day() != cutoff.Day() {
		// 极少数运行时刻（cutoff 落在 23:45 后）无法构造同日更晚的行，
		// 退化为恰在边界：与 cutoff 相等的行属于保留区间（删除为开区间）。
		keepRow = cutoff
	}
	rows := []struct {
		id   int
		at   time.Time
		keep bool
	}{
		{1, cutoff.AddDate(0, 0, -3), false},      // 远超保留期，必删
		{2, cutoff.Add(-15 * time.Minute), false}, // 首日早于 cutoff 时刻，必删
		{3, keepRow, true},                        // 首日不早于 cutoff 时刻，必留（旧实现因 'T' > ' ' 误删）
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO boundary_logs (id, created_at) VALUES (?, ?)`, row.id, row.at.Format("2006-01-02 15:04:05")); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := enforceLogTableLimits(ctx, db, service.store.DatabasePath(), days, 0, 0, false); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM boundary_logs WHERE id = ?`, row.id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if row.keep && count != 1 {
			t.Fatalf("row %d (boundary keeper %s) was deleted, want kept", row.id, row.at.Format("2006-01-02 15:04:05"))
		}
		if !row.keep && count != 0 {
			t.Fatalf("row %d (%s) was kept, want deleted", row.id, row.at.Format("2006-01-02 15:04:05"))
		}
	}
}

// 统计表独立长保留按 date 列（仅日期）比较：边界日当天必须保留，
// 不得因与带时刻的 cutoff 做字符串比较而多删一天。
func TestStatisticsRetentionCutoffKeepsBoundaryDay(t *testing.T) {
	service := newSwapTestService(t)
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	today := time.Now().UTC()
	dates := map[string]bool{ // value: 是否应保留
		today.AddDate(0, 0, -statisticsRetentionDays).Format("2006-01-02"):       true,  // 边界日：保留
		today.AddDate(0, 0, -(statisticsRetentionDays + 1)).Format("2006-01-02"): false, // 超期一天：删除
		today.AddDate(0, 0, -1).Format("2006-01-02"):                             true,  // 近期：保留
	}
	for date := range dates {
		if _, err := db.ExecContext(ctx, `INSERT INTO system_api_stats (date, audit_count, ops_count) VALUES (?, 1, 1)`, date); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := enforceLogTableLimits(ctx, db, service.store.DatabasePath(), 1, 0, 0, false); err != nil {
		t.Fatal(err)
	}
	for date, keep := range dates {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_api_stats WHERE date = ?`, date).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if keep && count != 1 {
			t.Fatalf("system_api_stats boundary date %s was deleted, want kept", date)
		}
		if !keep && count != 0 {
			t.Fatalf("system_api_stats expired date %s was kept, want deleted", date)
		}
	}
}

// HTTP 层面：换库互斥被持有时压缩请求应返回 409 而非 500。
func TestVacuumEndpointReturnsConflictWhileSwapHeld(t *testing.T) {
	service := newSwapTestService(t)
	swapMu := database.SwapMutex()
	swapMu.Lock()
	defer swapMu.Unlock()
	res := performSettingsRequest(service, http.MethodPost, "/api/settings/vacuum-database", "")
	if res.Code != http.StatusConflict {
		t.Fatalf("vacuum status = %d body=%s, want 409", res.Code, res.Body.String())
	}
}
