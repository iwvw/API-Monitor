package subscription

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/managedproxy"
	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			default_template_id TEXT DEFAULT 'builtin_mihomo_default',
			default_rate_limit_enabled INTEGER DEFAULT 1,
			default_rate_limit_per_minute INTEGER DEFAULT 30,
			default_refresh_hours INTEGER DEFAULT 24,
			geoip_enabled INTEGER DEFAULT 1,
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO subscription_settings (id) VALUES (1)`,
		`CREATE TABLE IF NOT EXISTS subscription_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			format TEXT NOT NULL,
			content TEXT NOT NULL,
			builtin INTEGER DEFAULT 0,
			is_default INTEGER DEFAULT 0,
			description TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			remark TEXT,
			enabled INTEGER DEFAULT 1,
			template_id TEXT DEFAULT 'builtin_mihomo_default',
			traffic_source TEXT DEFAULT 'manual',
			traffic_server_id TEXT,
			ownership TEXT DEFAULT 'external',
			management TEXT DEFAULT 'unmanaged',
			traffic_reporting TEXT DEFAULT 'unavailable',
			total_bytes INTEGER DEFAULT 0,
			manual_upload_bytes INTEGER DEFAULT 0,
			manual_download_bytes INTEGER DEFAULT 0,
			expire_at TEXT,
			cycle_type TEXT DEFAULT 'none',
			cycle_day INTEGER DEFAULT 1,
			cycle_start TEXT,
			cycle_end TEXT,
			baseline_upload_bytes INTEGER DEFAULT 0,
			baseline_download_bytes INTEGER DEFAULT 0,
			rate_limit_enabled INTEGER DEFAULT 1,
			rate_limit_per_minute INTEGER DEFAULT 30,
			node_filter_tags TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_upstreams (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			refresh_hours INTEGER DEFAULT 24,
			status TEXT,
			last_error TEXT,
			last_refresh_at TEXT,
			userinfo TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_subscriptions (
			id TEXT PRIMARY KEY,
			profile_id TEXT,
			plan_id TEXT DEFAULT '',
			name TEXT NOT NULL,
			remark TEXT,
			enabled INTEGER DEFAULT 1,
			public_token TEXT NOT NULL UNIQUE,
			vless_uuid TEXT NOT NULL DEFAULT '',
			hysteria2_password TEXT NOT NULL DEFAULT '',
			template_id TEXT DEFAULT 'builtin_mihomo_default',
			traffic_source TEXT DEFAULT 'manual',
			traffic_server_id TEXT,
			upstream_url TEXT,
			upstream_enabled INTEGER DEFAULT 0,
			upstream_refresh_hours INTEGER DEFAULT 24,
			upstream_status TEXT,
			upstream_last_error TEXT,
			upstream_last_refresh_at TEXT,
			upstream_userinfo TEXT,
			total_bytes INTEGER DEFAULT 0,
			manual_upload_bytes INTEGER DEFAULT 0,
			manual_download_bytes INTEGER DEFAULT 0,
			expire_at TEXT,
			cycle_type TEXT DEFAULT 'none',
			cycle_day INTEGER DEFAULT 1,
			cycle_start TEXT,
			cycle_end TEXT,
			baseline_upload_bytes INTEGER DEFAULT 0,
			baseline_download_bytes INTEGER DEFAULT 0,
			rate_limit_enabled INTEGER DEFAULT 1,
			rate_limit_per_minute INTEGER DEFAULT 30,
			node_filter_ids TEXT DEFAULT '',
			include_internal_nodes INTEGER DEFAULT 1,
			include_external_nodes INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_plans (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			remark TEXT DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			total_bytes INTEGER NOT NULL DEFAULT 0,
			cycle_type TEXT NOT NULL DEFAULT 'monthly',
			cycle_day INTEGER NOT NULL DEFAULT 1,
			rate_limit_enabled INTEGER NOT NULL DEFAULT 1,
			rate_limit_per_minute INTEGER NOT NULL DEFAULT 30,
			node_ids TEXT NOT NULL DEFAULT '',
			selection_mode TEXT NOT NULL DEFAULT 'explicit' CHECK(selection_mode IN ('explicit','all')),
			include_internal_nodes INTEGER NOT NULL DEFAULT 1,
			include_external_nodes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_plan_nodes (
			plan_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			source TEXT NOT NULL CHECK(source IN ('internal','external')),
			created_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY(plan_id,node_id,source)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_plan_nodes_node ON subscription_plan_nodes(node_id,source)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_subscriptions_token ON subscription_subscriptions(public_token)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_upstreams_profile ON subscription_upstreams(profile_id)`,
		`CREATE TABLE IF NOT EXISTS subscription_nodes (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL,
			profile_id TEXT,
			name TEXT NOT NULL,
			type TEXT,
			server TEXT,
			port INTEGER DEFAULT 0,
			country_code TEXT,
			location TEXT,
			tags TEXT,
			traffic_server_id TEXT,
			ownership TEXT DEFAULT 'external',
			management TEXT DEFAULT 'unmanaged',
			traffic_reporting TEXT DEFAULT 'unavailable',
			enabled INTEGER DEFAULT 1,
			stable INTEGER DEFAULT 0,
			sort_order INTEGER DEFAULT 0,
			raw_encrypted TEXT,
			config_encrypted TEXT,
			fingerprint TEXT,
			source TEXT DEFAULT 'manual',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_nodes_subscription ON subscription_nodes(subscription_id, sort_order)`,
		`CREATE TABLE IF NOT EXISTS subscription_access_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscription_id TEXT,
			public_token TEXT,
			ip_address TEXT,
			user_agent TEXT,
			format TEXT,
			success INTEGER DEFAULT 0,
			status_code INTEGER DEFAULT 200,
			error_message TEXT,
			node_count INTEGER DEFAULT 0,
			upload_bytes INTEGER DEFAULT 0,
			download_bytes INTEGER DEFAULT 0,
			total_bytes INTEGER DEFAULT 0,
			expire_at INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now'))
		)`,
		managedproxy.NodeTableDDL,
		`CREATE TABLE IF NOT EXISTS managed_proxy_preferences (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 443, enabled INTEGER NOT NULL DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT 'unknown', last_latency_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '', checked_at TEXT,
			created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_access_logs_subscription ON subscription_access_logs(subscription_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_access_logs_ip ON subscription_access_logs(subscription_id, ip_address, created_at)`,
		`CREATE TRIGGER IF NOT EXISTS trg_subscription_plan_nodes_managed_delete AFTER DELETE ON managed_proxy_nodes
			BEGIN DELETE FROM subscription_plan_nodes WHERE node_id=OLD.id AND source='internal'; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_subscription_plan_nodes_external_delete AFTER DELETE ON subscription_nodes
			BEGIN DELETE FROM subscription_plan_nodes WHERE node_id=OLD.id AND source='external'; END`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure subscription schema: %w", err)
		}
	}
	if err := reconcilequeue.EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := subscriptionledger.EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "profile_id", "ALTER TABLE subscription_subscriptions ADD COLUMN profile_id TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "vless_uuid", "ALTER TABLE subscription_subscriptions ADD COLUMN vless_uuid TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "hysteria2_password", "ALTER TABLE subscription_subscriptions ADD COLUMN hysteria2_password TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_vless_uuid ON subscription_subscriptions(vless_uuid) WHERE vless_uuid<>''`); err != nil {
		return fmt.Errorf("create subscription VLESS credential index: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_hysteria2_password ON subscription_subscriptions(hysteria2_password) WHERE hysteria2_password<>''`); err != nil {
		return fmt.Errorf("create subscription Hysteria2 credential index: %w", err)
	}
	if err := ensureColumn(ctx, db, "subscription_plans", "selection_mode", "ALTER TABLE subscription_plans ADD COLUMN selection_mode TEXT NOT NULL DEFAULT 'explicit'"); err != nil {
		return err
	}
	// profile 型订阅的内部节点记账开关（与 plan 语义一致，默认关闭兼容存量）
	if err := ensureColumn(ctx, db, "subscription_profiles", "include_internal_nodes", "ALTER TABLE subscription_profiles ADD COLUMN include_internal_nodes INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_profiles", "selection_mode", "ALTER TABLE subscription_profiles ADD COLUMN selection_mode TEXT NOT NULL DEFAULT 'explicit'"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "plan_id", "ALTER TABLE subscription_subscriptions ADD COLUMN plan_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_nodes", "profile_id", "ALTER TABLE subscription_nodes ADD COLUMN profile_id TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_nodes", "traffic_server_id", "ALTER TABLE subscription_nodes ADD COLUMN traffic_server_id TEXT"); err != nil {
		return err
	}
	for _, column := range []struct{ name, sql string }{
		{"ownership", "ALTER TABLE subscription_nodes ADD COLUMN ownership TEXT DEFAULT 'external'"},
		{"management", "ALTER TABLE subscription_nodes ADD COLUMN management TEXT DEFAULT 'unmanaged'"},
		{"traffic_reporting", "ALTER TABLE subscription_nodes ADD COLUMN traffic_reporting TEXT DEFAULT 'unavailable'"},
	} {
		if err := ensureColumn(ctx, db, "subscription_nodes", column.name, column.sql); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, sql string }{
		{"ownership", "ALTER TABLE subscription_profiles ADD COLUMN ownership TEXT DEFAULT 'external'"},
		{"management", "ALTER TABLE subscription_profiles ADD COLUMN management TEXT DEFAULT 'unmanaged'"},
		{"traffic_reporting", "ALTER TABLE subscription_profiles ADD COLUMN traffic_reporting TEXT DEFAULT 'unavailable'"},
	} {
		if err := ensureColumn(ctx, db, "subscription_profiles", column.name, column.sql); err != nil {
			return err
		}
	}
	if err := managedproxy.EnsureNodeColumns(ctx, db); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "node_filter_ids", "ALTER TABLE subscription_subscriptions ADD COLUMN node_filter_ids TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "include_external_nodes", "ALTER TABLE subscription_subscriptions ADD COLUMN include_external_nodes INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "include_internal_nodes", "ALTER TABLE subscription_subscriptions ADD COLUMN include_internal_nodes INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_subscription_subscriptions_profile ON subscription_subscriptions(profile_id)`); err != nil {
		return fmt.Errorf("create subscription profile index: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_subscriptions SET profile_id = id WHERE COALESCE(profile_id, '') = ''`); err != nil {
		return fmt.Errorf("normalize subscription profile ids: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_nodes SET profile_id = subscription_id WHERE COALESCE(profile_id, '') = ''`); err != nil {
		return fmt.Errorf("normalize node profile ids: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_profiles (
			id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, created_at, updated_at
		)
		SELECT profile_id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, created_at, updated_at
		FROM subscription_subscriptions
		WHERE COALESCE(profile_id, '') != ''`); err != nil {
		return fmt.Errorf("seed subscription profiles: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_upstreams (
			id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, userinfo, created_at, updated_at
		)
		SELECT 'up_' || profile_id, profile_id, '默认上游', upstream_url, upstream_enabled, upstream_refresh_hours,
			upstream_status, upstream_last_error, upstream_last_refresh_at, upstream_userinfo, created_at, updated_at
		FROM subscription_subscriptions
		WHERE COALESCE(profile_id, '') != '' AND COALESCE(upstream_url, '') != ''`); err != nil {
		return fmt.Errorf("seed subscription upstreams: %w", err)
	}
	if err := migratePlanNodeRelations(ctx, db); err != nil {
		return err
	}
	if err := backfillSubscriptionCredentials(ctx, db); err != nil {
		return err
	}
	return nil
}

func backfillSubscriptionCredentials(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM subscription_subscriptions WHERE COALESCE(vless_uuid,'')='' OR COALESCE(hysteria2_password,'')=''`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, `UPDATE subscription_subscriptions SET vless_uuid=CASE WHEN COALESCE(vless_uuid,'')='' THEN ? ELSE vless_uuid END,hysteria2_password=CASE WHEN COALESCE(hysteria2_password,'')='' THEN ? ELSE hysteria2_password END WHERE id=?`, randomUUID(), randomCredential(), id); err != nil {
			return fmt.Errorf("backfill subscription credentials: %w", err)
		}
	}
	return nil
}

func migratePlanNodeRelations(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id,COALESCE(node_ids,'') FROM subscription_plans WHERE COALESCE(node_ids,'')<>''`)
	if err != nil {
		return err
	}
	type legacyPlan struct{ id, nodeIDs string }
	legacy := []legacyPlan{}
	for rows.Next() {
		var item legacyPlan
		if err := rows.Scan(&item.id, &item.nodeIDs); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, plan := range legacy {
		var relationCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id=?`, plan.id).Scan(&relationCount); err != nil {
			return err
		}
		if relationCount == 0 {
			seen := map[string]struct{}{}
			for _, rawID := range decodeNodeFilterIDs(plan.nodeIDs) {
				nodeID := strings.TrimSpace(rawID)
				if nodeID == "" {
					continue
				}
				if _, exists := seen[nodeID]; exists {
					continue
				}
				seen[nodeID] = struct{}{}
				source := ""
				var exists int
				if scanErr := tx.QueryRowContext(ctx, `SELECT 1 FROM managed_proxy_nodes WHERE id=?`, nodeID).Scan(&exists); scanErr == nil {
					source = "internal"
				} else if scanErr := tx.QueryRowContext(ctx, `SELECT 1 FROM subscription_nodes WHERE id=?`, nodeID).Scan(&exists); scanErr == nil {
					source = "external"
				}
				// Legacy snapshots can outlive their node. Migration intentionally
				// drops those stale references; normal plan saves remain strict.
				if source == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_plan_nodes(plan_id,node_id,source) VALUES(?,?,?)`, plan.id, nodeID, source); err != nil {
					return fmt.Errorf("migrate plan node relations: %w", err)
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscription_plans SET node_ids='' WHERE id=?`, plan.id); err != nil {
			return fmt.Errorf("retire legacy plan node snapshot: %w", err)
		}
	}
	return tx.Commit()
}

func ensureColumn(ctx context.Context, db *sql.DB, tableName, columnName, alterSQL string) error {
	exists, err := schemaColumnExists(ctx, db, tableName, columnName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		// Another backend process may have completed the same ALTER between the
		// inspection and this statement. Re-inspect before treating it as fatal.
		if exists, inspectErr := schemaColumnExists(ctx, db, tableName, columnName); inspectErr == nil && exists {
			return nil
		}
		return fmt.Errorf("add %s.%s: %w", tableName, columnName, err)
	}
	return nil
}

func schemaColumnExists(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan %s columns: %w", tableName, err)
		}
		if name == columnName {
			rows.Close()
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close %s columns: %w", tableName, err)
	}
	return false, nil
}
