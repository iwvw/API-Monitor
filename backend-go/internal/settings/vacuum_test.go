package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

// 构造一个 fresh 临时库（auto_vacuum=NONE），写入数据后验证：
// 1) migrateAutoVacuumIncremental 把模式迁到 INCREMENTAL 且可回收空闲页；
// 2) 迁移后连接级 PRAGMA（temp_store / cache_size）已恢复，不污染复用连接。
func TestMigrateAutoVacuumIncrementalMigratesAndReclaims(t *testing.T) {
	store := database.New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := store.Open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 0; i < 500; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO t(payload) VALUES (?)`, strings.Repeat("x", 200)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := db.ExecContext(context.Background(), `DELETE FROM t WHERE id % 2 = 0`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	mode, err := autoVacuumMode(context.Background(), db)
	if err != nil {
		t.Fatalf("autoVacuumMode before: %v", err)
	}
	if mode != 0 {
		t.Fatalf("fresh test db auto_vacuum = %d, want 0 (NONE)", mode)
	}

	if err := migrateAutoVacuumIncremental(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mode, err = autoVacuumMode(context.Background(), db)
	if err != nil {
		t.Fatalf("autoVacuumMode after: %v", err)
	}
	if mode != 2 {
		t.Fatalf("auto_vacuum after migrate = %d, want 2 (INCREMENTAL)", mode)
	}

	// 迁移后 pointer-map 已建立：新删除产生的空闲页可通过 incremental_vacuum 回收。
	if _, err := db.ExecContext(context.Background(), `DELETE FROM t WHERE id % 3 = 0`); err != nil {
		t.Fatalf("delete after migrate: %v", err)
	}
	free, err := freelistPageCount(context.Background(), db)
	if err != nil {
		t.Fatalf("freelist after delete: %v", err)
	}
	if free <= 0 {
		t.Fatalf("expected freelist pages after delete, got %d", free)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA incremental_vacuum(4096)`); err != nil {
		t.Fatalf("incremental vacuum: %v", err)
	}
	afterFree, err := freelistPageCount(context.Background(), db)
	if err != nil {
		t.Fatalf("freelist after incremental: %v", err)
	}
	if afterFree != 0 {
		t.Fatalf("freelist after incremental vacuum = %d, want 0", afterFree)
	}

	// 迁移函数必须恢复连接级 PRAGMA，避免污染池化复用的物理连接。
	// PRAGMA temp_store 返回数字：0=DEFAULT 1=FILE 2=MEMORY。
	var tempStore int
	if err := db.QueryRowContext(context.Background(), `PRAGMA temp_store`).Scan(&tempStore); err != nil {
		t.Fatalf("read temp_store: %v", err)
	}
	if tempStore != 2 {
		t.Fatalf("temp_store after migrate = %d, want 2 (MEMORY, restored)", tempStore)
	}
	var cacheSize int
	if err := db.QueryRowContext(context.Background(), `PRAGMA cache_size`).Scan(&cacheSize); err != nil {
		t.Fatalf("read cache_size: %v", err)
	}
	if cacheSize != -16000 {
		t.Fatalf("cache_size after migrate = %d, want -16000 (restored)", cacheSize)
	}
}

// enforceLogTableLimits 的 allowVacuum 路径在 NONE 模式库上应完成迁移而非失败。
func TestEnforceLogLimitsMigratesAutoVacuum(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS operation_logs (
		id INTEGER PRIMARY KEY, operation_type TEXT, table_name TEXT,
		details TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create log table: %v", err)
	}
	for i := 0; i < 20; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO operation_logs(operation_type, table_name) VALUES ('test','t')`); err != nil {
			t.Fatalf("insert log: %v", err)
		}
	}

	result, err := enforceLogTableLimits(context.Background(), db, service.store.DatabasePath(), 0, 5, 1000, true)
	if err != nil {
		t.Fatalf("enforceLogTableLimits: %v", err)
	}
	deleted, _ := result["deleted"].(int64)
	if deleted < 10 {
		t.Fatalf("expected at least 10 deleted log rows, got %d", deleted)
	}

	mode, err := autoVacuumMode(context.Background(), db)
	if err != nil {
		t.Fatalf("autoVacuumMode: %v", err)
	}
	if mode == 0 {
		t.Fatal("expected auto_vacuum to be migrated away from NONE after allowVacuum enforcement")
	}
}