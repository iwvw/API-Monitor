package timeutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

func newTestDB(t *testing.T) *database.Store {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "api_monitor_timeutil_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	return database.New(config.Config{DataDir: tempDir, DBName: "data.db"})
}

func TestLocationFromName(t *testing.T) {
	if loc := LocationFromName("Asia/Shanghai"); loc == time.Local {
		t.Fatal("expected named location, got time.Local")
	}
	if loc := LocationFromName("system"); loc != time.Local {
		t.Fatalf("expected time.Local for system, got %v", loc)
	}
	if loc := LocationFromName(""); loc != time.Local {
		t.Fatalf("expected time.Local for empty, got %v", loc)
	}
	if loc := LocationFromName("Invalid/Zone"); loc != time.Local {
		t.Fatalf("expected time.Local fallback for invalid, got %v", loc)
	}
}

func TestReadTimeZoneAndLocationFromSettings(t *testing.T) {
	store := newTestDB(t)
	ctx := context.Background()
	db, err := store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 未设置 -> system
	if got := ReadTimeZone(ctx, db); got != "system" {
		t.Fatalf("expected system default, got %q", got)
	}
	if loc := LocationFromSettings(ctx, db); loc != time.Local {
		t.Fatalf("expected time.Local when unset, got %v", loc)
	}

	// 显式设置时区
	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_settings (id, time_zone) VALUES (1, 'Asia/Shanghai')
		 ON CONFLICT(id) DO UPDATE SET time_zone = 'Asia/Shanghai'`); err != nil {
		t.Fatal(err)
	}
	if got := ReadTimeZone(ctx, db); got != "Asia/Shanghai" {
		t.Fatalf("expected Asia/Shanghai, got %q", got)
	}
	loc := LocationFromSettings(ctx, db)
	now := time.Now().UTC().In(loc)
	if now.Location().String() != "Asia/Shanghai" {
		t.Fatalf("expected Asia/Shanghai location, got %v", now.Location())
	}
}

func TestFormatInLocation(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	tm := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	got := FormatInLocation(tm, sh)
	if got != "2026-08-16T11:00:00+08:00" {
		t.Fatalf("unexpected format: %s", got)
	}
}