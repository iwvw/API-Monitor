package subscription

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// 生产库已跑过「删除模板表/列」的迁移。恢复模板功能后必须能重新加回列，
// 否则所有订阅/模板查询都会 no such column。
func TestLegacyDBWithoutTemplateColumnsCanBeUpgraded(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 模拟已删除模板列/表的存量库
	legacy := []string{
		`CREATE TABLE subscription_settings (id INTEGER PRIMARY KEY CHECK (id=1), default_rate_limit_enabled INTEGER DEFAULT 1, default_rate_limit_per_minute INTEGER DEFAULT 30, default_refresh_hours INTEGER DEFAULT 24, geoip_enabled INTEGER DEFAULT 1, updated_at TEXT)`,
		`INSERT INTO subscription_settings(id) VALUES(1)`,
		`CREATE TABLE subscription_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, remark TEXT, enabled INTEGER DEFAULT 1, traffic_source TEXT DEFAULT 'manual', traffic_server_id TEXT, total_bytes INTEGER DEFAULT 0, manual_upload_bytes INTEGER DEFAULT 0, manual_download_bytes INTEGER DEFAULT 0, expire_at TEXT, cycle_type TEXT DEFAULT 'none', cycle_day INTEGER DEFAULT 1, cycle_start TEXT, cycle_end TEXT, baseline_upload_bytes INTEGER DEFAULT 0, baseline_download_bytes INTEGER DEFAULT 0, rate_limit_enabled INTEGER DEFAULT 1, rate_limit_per_minute INTEGER DEFAULT 30, node_filter_tags TEXT DEFAULT '', sort_order INTEGER DEFAULT 0, created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now')))`,
		`INSERT INTO subscription_profiles(id,name) VALUES('p1','库')`,
		`CREATE TABLE subscription_subscriptions (id TEXT PRIMARY KEY, profile_id TEXT, plan_id TEXT DEFAULT '', name TEXT NOT NULL, remark TEXT, enabled INTEGER DEFAULT 1, public_token TEXT NOT NULL UNIQUE, vless_uuid TEXT NOT NULL DEFAULT '', hysteria2_password TEXT NOT NULL DEFAULT '', traffic_source TEXT DEFAULT 'manual', traffic_server_id TEXT, upstream_url TEXT, upstream_enabled INTEGER DEFAULT 0, upstream_refresh_hours INTEGER DEFAULT 24, upstream_status TEXT, upstream_last_error TEXT, upstream_last_refresh_at TEXT, upstream_userinfo TEXT, total_bytes INTEGER DEFAULT 0, manual_upload_bytes INTEGER DEFAULT 0, manual_download_bytes INTEGER DEFAULT 0, expire_at TEXT, cycle_type TEXT DEFAULT 'none', cycle_day INTEGER DEFAULT 1, cycle_start TEXT, cycle_end TEXT, baseline_upload_bytes INTEGER DEFAULT 0, baseline_download_bytes INTEGER DEFAULT 0, rate_limit_enabled INTEGER DEFAULT 1, rate_limit_per_minute INTEGER DEFAULT 30, node_filter_ids TEXT DEFAULT '', include_internal_nodes INTEGER DEFAULT 1, include_external_nodes INTEGER DEFAULT 0, created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now')))`,
		`INSERT INTO subscription_subscriptions(id,profile_id,name,public_token) VALUES('s1','p1','订阅','tok')`,
	}
	for _, q := range legacy {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensureSchema on legacy DB without template columns: %v", err)
	}
	if err := ensureBuiltins(ctx, db, false); err != nil {
		t.Fatalf("ensureBuiltins: %v", err)
	}

	// 关键：恢复后必须能正常读取订阅与模板
	if _, err := loadSubscriptions(ctx, db, "s1"); err != nil {
		t.Fatalf("loadSubscriptions after upgrade: %v", err)
	}
	if _, err := loadProfiles(ctx, db, ""); err != nil {
		t.Fatalf("loadProfiles after upgrade: %v", err)
	}
	tpls, err := loadTemplates(ctx, db)
	if err != nil {
		t.Fatalf("loadTemplates after upgrade: %v", err)
	}
	if len(tpls) == 0 {
		t.Fatal("builtin templates missing after upgrade")
	}
	if _, err := loadSettings(ctx, db); err != nil {
		t.Fatalf("loadSettings after upgrade: %v", err)
	}
}
