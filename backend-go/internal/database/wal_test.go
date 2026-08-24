package database

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func walFileSize(path string) int64 {
	st, err := os.Stat(path + "-wal")
	if err != nil {
		return 0
	}
	return st.Size()
}

func TestWALCheckpointTruncateShrinksWal(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "wal-truncate.db"}
	store := New(cfg)
	db, err := store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatal(err)
	}
	// 关闭自动 checkpoint，让 WAL 先累积到可见大小。
	if _, err := db.ExecContext(context.Background(), `PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20000; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO t(payload) VALUES (?)`, strings.Repeat("x", 200)); err != nil {
			t.Fatal(err)
		}
	}

	if walFileSize(store.DatabasePath()) <= 0 {
		t.Fatal("expected a non-empty WAL file before checkpoint")
	}

	busy, _, _, err := WALCheckpointTruncate(context.Background(), store.DatabasePath())
	if err != nil {
		t.Fatalf("WALCheckpointTruncate: %v", err)
	}
	if busy {
		t.Fatal("WALCheckpointTruncate reported busy on an idle connection")
	}
	if after := walFileSize(store.DatabasePath()); after > 0 {
		t.Fatalf("expected WAL truncated to zero, got %d bytes", after)
	}
}

// WALMaintenance 只做 PASSIVE：在写满 WAL 后应能正常完成一轮（不报错），
// 且在有活跃读者时同样不被阻塞（PASSIVE 不要求"零读者"，这是结构选型的核心）。
func TestWALMaintenancePassiveCompactsWithReaders(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "wal-maintenance.db"}
	store := New(cfg)
	db, err := store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30000; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO t(payload) VALUES (?)`, strings.Repeat("y", 200)); err != nil {
			t.Fatal(err)
		}
	}

	if walFileSize(store.DatabasePath()) <= 0 {
		t.Fatal("expected a non-empty WAL file before maintenance")
	}

	// 活跃读者存在时 PASSIVE 也必须能完成（不得阻塞/超时）。
	reader, err := sql.Open("sqlite", store.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Query(`SELECT COUNT(*) FROM t`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := WALMaintenance(context.Background(), store.DatabasePath())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WALMaintenance: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WALMaintenance PASSIVE blocked or timed out with an active reader")
	}
}
