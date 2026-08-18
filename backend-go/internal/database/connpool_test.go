package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestResetPoolInvalidatesAndRebuilds(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "pool.db")

	store := New(config.Config{DataDir: dataDir, DBName: filepath.Base(dbPath), Version: "test"})

	ctx := context.Background()
	db, err := store.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS probe (v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	ResetPool(dbPath)
	poolsMu.Lock()
	_, exists := pools[dbPath]
	poolsMu.Unlock()
	if exists {
		t.Fatal("pool should be removed after ResetPool")
	}

	db2, err := store.Open(ctx)
	if err != nil {
		t.Fatalf("reopen after reset: %v", err)
	}
	if _, err := db2.ExecContext(ctx, `INSERT INTO probe (v) VALUES ('ok')`); err != nil {
		t.Fatalf("write after reset: %v", err)
	}
	var v string
	if err := db2.QueryRowContext(ctx, `SELECT v FROM probe`).Scan(&v); err != nil || v != "ok" {
		t.Fatalf("read after reset: v=%q err=%v", v, err)
	}
	db2.Close()

	ResetPool(dbPath)
	ResetPool(dbPath)
	_ = os.Remove(dbPath)
}

func TestResetPoolIdempotentOnMissingPath(t *testing.T) {
	ResetPool(filepath.Join(t.TempDir(), "never-created.db"))
}