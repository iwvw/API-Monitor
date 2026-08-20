package database

import (
	"context"
	"os"
	"strings"
	"testing"

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
