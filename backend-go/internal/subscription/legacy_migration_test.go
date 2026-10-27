package subscription

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

// 验证存量库（含 template_id/default_template_id/stable 与 subscription_templates）
// 能被 ensureSchema 平滑升级，且新写入路径不再引用已删除的列。
func TestLegacySchemaUpgradeDropsTemplateAndStable(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := []string{
		`CREATE TABLE subscription_settings (id INTEGER PRIMARY KEY CHECK (id=1), default_template_id TEXT DEFAULT 'builtin_mihomo_default', default_rate_limit_enabled INTEGER DEFAULT 1, default_rate_limit_per_minute INTEGER DEFAULT 30, default_refresh_hours INTEGER DEFAULT 24, geoip_enabled INTEGER DEFAULT 1, updated_at TEXT)`,
		`INSERT INTO subscription_settings(id) VALUES(1)`,
		`CREATE TABLE subscription_templates (id TEXT PRIMARY KEY, name TEXT NOT NULL, format TEXT NOT NULL, content TEXT NOT NULL, builtin INTEGER DEFAULT 0, is_default INTEGER DEFAULT 0, description TEXT, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO subscription_templates(id,name,format,content) VALUES('builtin_mihomo_default','旧','clash','x')`,
		`CREATE TABLE subscription_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, remark TEXT, enabled INTEGER DEFAULT 1, template_id TEXT DEFAULT 'builtin_mihomo_default', traffic_source TEXT DEFAULT 'manual', traffic_server_id TEXT, total_bytes INTEGER DEFAULT 0, manual_upload_bytes INTEGER DEFAULT 0, manual_download_bytes INTEGER DEFAULT 0, expire_at TEXT, cycle_type TEXT DEFAULT 'none', cycle_day INTEGER DEFAULT 1, cycle_start TEXT, cycle_end TEXT, baseline_upload_bytes INTEGER DEFAULT 0, baseline_download_bytes INTEGER DEFAULT 0, rate_limit_enabled INTEGER DEFAULT 1, rate_limit_per_minute INTEGER DEFAULT 30, node_filter_tags TEXT DEFAULT '', sort_order INTEGER DEFAULT 0, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO subscription_profiles(id,name,template_id) VALUES('p1','库','builtin_mihomo_default')`,
		`CREATE TABLE subscription_subscriptions (id TEXT PRIMARY KEY, profile_id TEXT, plan_id TEXT DEFAULT '', name TEXT NOT NULL, remark TEXT, enabled INTEGER DEFAULT 1, public_token TEXT NOT NULL UNIQUE, vless_uuid TEXT NOT NULL DEFAULT '', hysteria2_password TEXT NOT NULL DEFAULT '', template_id TEXT DEFAULT 'builtin_mihomo_default', traffic_source TEXT DEFAULT 'manual', traffic_server_id TEXT, upstream_url TEXT, upstream_enabled INTEGER DEFAULT 0, upstream_refresh_hours INTEGER DEFAULT 24, upstream_status TEXT, upstream_last_error TEXT, upstream_last_refresh_at TEXT, upstream_userinfo TEXT, total_bytes INTEGER DEFAULT 0, manual_upload_bytes INTEGER DEFAULT 0, manual_download_bytes INTEGER DEFAULT 0, expire_at TEXT, cycle_type TEXT DEFAULT 'none', cycle_day INTEGER DEFAULT 1, cycle_start TEXT, cycle_end TEXT, baseline_upload_bytes INTEGER DEFAULT 0, baseline_download_bytes INTEGER DEFAULT 0, rate_limit_enabled INTEGER DEFAULT 1, rate_limit_per_minute INTEGER DEFAULT 30, node_filter_ids TEXT DEFAULT '', include_internal_nodes INTEGER DEFAULT 1, include_external_nodes INTEGER DEFAULT 0, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO subscription_subscriptions(id,profile_id,name,public_token,template_id,created_at,updated_at) VALUES('s1','p1','订阅','tok','builtin_mihomo_default',datetime('now'),datetime('now'))`,
		`CREATE TABLE subscription_nodes (id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL, profile_id TEXT, name TEXT NOT NULL, type TEXT, server TEXT, port INTEGER DEFAULT 0, country_code TEXT, location TEXT, tags TEXT, traffic_server_id TEXT, ownership TEXT DEFAULT 'external', management TEXT DEFAULT 'unmanaged', traffic_reporting TEXT DEFAULT 'unavailable', enabled INTEGER DEFAULT 1, stable INTEGER DEFAULT 0, sort_order INTEGER DEFAULT 0, raw_encrypted TEXT, config_encrypted TEXT, fingerprint TEXT, source TEXT DEFAULT 'manual', created_at TEXT, updated_at TEXT)`,
		`INSERT INTO subscription_nodes(id,subscription_id,profile_id,name,stable) VALUES('n1','p1','p1','节点',1)`,
	}
	for _, q := range legacy {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensureSchema on legacy: %v", err)
	}

	for _, tc := range []struct{ table, col string }{
		{"subscription_nodes", "stable"},
		{"subscription_profiles", "template_id"},
		{"subscription_subscriptions", "template_id"},
		{"subscription_settings", "default_template_id"},
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, tc.table, tc.col).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("%s.%s still present after migration", tc.table, tc.col)
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='subscription_templates'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("subscription_templates table still exists")
	}

	// 存量数据仍在，且新代码可正常读取
	subs, err := loadSubscriptions(ctx, db, "s1")
	if err != nil || len(subs) != 1 || subs[0].Name != "订阅" {
		t.Fatalf("load after migration: %v %#v", err, subs)
	}
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensureSchema must be idempotent: %v", err)
	}
}
