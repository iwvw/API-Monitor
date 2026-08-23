package database

import (
	"context"
	"os"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// SwapMutex 必须是进程级单例：settings 导入与 backup 恢复依赖它互斥。
func TestSwapMutexIsProcessWide(t *testing.T) {
	first := SwapMutex()
	if first == nil {
		t.Fatal("SwapMutex returned nil")
	}
	if !first.TryLock() {
		t.Fatal("fresh swap mutex must be lockable")
	}
	if first.TryLock() {
		t.Fatal("swap mutex must not be reentrant")
	}
	first.Unlock()
	if SwapMutex() != first {
		t.Fatal("SwapMutex must return the same instance")
	}
}

// schema 失败不得粘池：底层文件修复后，下一次 Open 必须重试 EnsureCoreSchema
// 而不是沿用缓存失败（曾导致一次导入失败后全站 store.Open 持续失败到重启）。
// pin 模拟生产常驻句柄，保持 refs>0——池在失败后不被销毁，这正是粘池缺陷
// 的触发条件。
func TestOpenRetriesSchemaAfterFailure(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "retry-schema.db"}
	dbPath := cfg.DatabasePath()
	if err := os.WriteFile(dbPath, []byte("definitely not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := New(cfg)
	pin := &poolConnector{pool: poolFor(dbPath)}
	defer pin.Close()

	if _, err := store.Open(context.Background()); err == nil {
		t.Fatal("expected Open to fail on non-sqlite file")
	}

	// 修复底层文件：空文件是合法的 SQLite 库。
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background())
	if err != nil {
		t.Fatalf("Open after fixing underlying file: %v", err)
	}
	db.Close()
}
