package uptime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS uptime_monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'http',
			url TEXT,
			hostname TEXT,
			port INTEGER,
			interval INTEGER DEFAULT 60,
			timeout INTEGER DEFAULT 30,
			confirm_count INTEGER DEFAULT 3,
			active INTEGER DEFAULT 1,
			method TEXT DEFAULT 'GET',
			headers TEXT,
			body TEXT,
			ignore_tls INTEGER DEFAULT 0,
			accepted_status_codes TEXT,
			keyword TEXT,
			dns_resolve_type TEXT DEFAULT 'A',
			dns_resolve_server TEXT,
			retry_interval INTEGER DEFAULT 30,
			resend_interval INTEGER DEFAULT 0,
			up_confirm_count INTEGER,
			down_confirm_count INTEGER,
			config_json TEXT,
			auth_json_encrypted TEXT,
			push_token TEXT UNIQUE,
			push_grace_seconds INTEGER DEFAULT 120,
			last_checked_at DATETIME,
			next_check_at DATETIME,
			expiry_notification INTEGER DEFAULT 7,
			notification_channels TEXT DEFAULT '[]',
			tags TEXT DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_heartbeats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			status INTEGER NOT NULL,
			state TEXT,
			ping INTEGER DEFAULT 0,
			duration_ms INTEGER,
			status_code INTEGER,
			error_code TEXT,
			details_json TEXT,
			maintenance INTEGER DEFAULT 0,
			probe_id TEXT,
			msg TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_incidents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			started_at DATETIME NOT NULL,
			resolved_at DATETIME,
			duration_ms INTEGER,
			cause TEXT,
			status TEXT DEFAULT 'open',
			severity TEXT,
			acknowledged_at DATETIME,
			acknowledged_by TEXT,
			maintenance_id INTEGER,
			resolved_reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_monitor_states (
			monitor_id INTEGER PRIMARY KEY,
			state TEXT DEFAULT 'up',
			fail_count INTEGER DEFAULT 0,
			recover_count INTEGER DEFAULT 0,
			active_incident_id INTEGER,
			last_transition_at DATETIME,
			last_error TEXT,
			last_ping INTEGER DEFAULT 0,
			ssl_expiry DATETIME,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			domain TEXT,
			title TEXT NOT NULL,
			description TEXT,
			theme TEXT DEFAULT 'auto',
			public INTEGER DEFAULT 1,
			cache_seconds INTEGER DEFAULT 300,
			config_json TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_page_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status_page_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			order_index INTEGER DEFAULT 0,
			FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_page_monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status_page_id INTEGER NOT NULL,
			group_id INTEGER,
			monitor_id INTEGER NOT NULL,
			order_index INTEGER DEFAULT 0,
			display_name TEXT,
			FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_maintenance_windows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT,
			strategy TEXT DEFAULT 'manual',
			timezone TEXT DEFAULT 'UTC',
			start_at DATETIME,
			end_at DATETIME,
			cron TEXT,
			recurrence_json TEXT,
			active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_maintenance_targets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			maintenance_id INTEGER NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT,
			FOREIGN KEY (maintenance_id) REFERENCES uptime_maintenance_windows(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_heartbeats_monitor_time ON uptime_heartbeats(monitor_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_heartbeats_created ON uptime_heartbeats(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_monitor ON uptime_incidents(monitor_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_states_state ON uptime_monitor_states(state, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_status_pages_slug ON uptime_status_pages(slug, public)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_status_pages_domain ON uptime_status_pages(domain, public)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_maintenance_active ON uptime_maintenance_windows(active, start_at, end_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure uptime schema: %w", err)
		}
	}
	columns := []struct {
		table string
		name  string
		sql   string
	}{
		{"uptime_monitors", "created_at", "ALTER TABLE uptime_monitors ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP"},
		{"uptime_monitors", "updated_at", "ALTER TABLE uptime_monitors ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP"},
		{"uptime_monitors", "keyword", "ALTER TABLE uptime_monitors ADD COLUMN keyword TEXT"},
		{"uptime_monitors", "dns_resolve_type", "ALTER TABLE uptime_monitors ADD COLUMN dns_resolve_type TEXT DEFAULT 'A'"},
		{"uptime_monitors", "dns_resolve_server", "ALTER TABLE uptime_monitors ADD COLUMN dns_resolve_server TEXT"},
		{"uptime_monitors", "retry_interval", "ALTER TABLE uptime_monitors ADD COLUMN retry_interval INTEGER DEFAULT 30"},
		{"uptime_monitors", "resend_interval", "ALTER TABLE uptime_monitors ADD COLUMN resend_interval INTEGER DEFAULT 0"},
		{"uptime_monitors", "up_confirm_count", "ALTER TABLE uptime_monitors ADD COLUMN up_confirm_count INTEGER"},
		{"uptime_monitors", "down_confirm_count", "ALTER TABLE uptime_monitors ADD COLUMN down_confirm_count INTEGER"},
		{"uptime_monitors", "config_json", "ALTER TABLE uptime_monitors ADD COLUMN config_json TEXT"},
		{"uptime_monitors", "auth_json_encrypted", "ALTER TABLE uptime_monitors ADD COLUMN auth_json_encrypted TEXT"},
		{"uptime_monitors", "push_token", "ALTER TABLE uptime_monitors ADD COLUMN push_token TEXT"},
		{"uptime_monitors", "push_grace_seconds", "ALTER TABLE uptime_monitors ADD COLUMN push_grace_seconds INTEGER DEFAULT 120"},
		{"uptime_monitors", "last_checked_at", "ALTER TABLE uptime_monitors ADD COLUMN last_checked_at DATETIME"},
		{"uptime_monitors", "next_check_at", "ALTER TABLE uptime_monitors ADD COLUMN next_check_at DATETIME"},
		{"uptime_heartbeats", "state", "ALTER TABLE uptime_heartbeats ADD COLUMN state TEXT"},
		{"uptime_heartbeats", "duration_ms", "ALTER TABLE uptime_heartbeats ADD COLUMN duration_ms INTEGER"},
		{"uptime_heartbeats", "status_code", "ALTER TABLE uptime_heartbeats ADD COLUMN status_code INTEGER"},
		{"uptime_heartbeats", "error_code", "ALTER TABLE uptime_heartbeats ADD COLUMN error_code TEXT"},
		{"uptime_heartbeats", "details_json", "ALTER TABLE uptime_heartbeats ADD COLUMN details_json TEXT"},
		{"uptime_heartbeats", "maintenance", "ALTER TABLE uptime_heartbeats ADD COLUMN maintenance INTEGER DEFAULT 0"},
		{"uptime_heartbeats", "probe_id", "ALTER TABLE uptime_heartbeats ADD COLUMN probe_id TEXT"},
		{"uptime_incidents", "status", "ALTER TABLE uptime_incidents ADD COLUMN status TEXT DEFAULT 'open'"},
		{"uptime_incidents", "severity", "ALTER TABLE uptime_incidents ADD COLUMN severity TEXT"},
		{"uptime_incidents", "acknowledged_at", "ALTER TABLE uptime_incidents ADD COLUMN acknowledged_at DATETIME"},
		{"uptime_incidents", "acknowledged_by", "ALTER TABLE uptime_incidents ADD COLUMN acknowledged_by TEXT"},
		{"uptime_incidents", "maintenance_id", "ALTER TABLE uptime_incidents ADD COLUMN maintenance_id INTEGER"},
		{"uptime_incidents", "resolved_reason", "ALTER TABLE uptime_incidents ADD COLUMN resolved_reason TEXT"},
		{"uptime_monitor_states", "ssl_expiry", "ALTER TABLE uptime_monitor_states ADD COLUMN ssl_expiry DATETIME"},
	}
	for _, column := range columns {
		exists, err := hasColumn(ctx, db, column.table, column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add %s.%s: %w", column.table, column.name, err)
			}
		}
	}
	return nil
}

func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Service) migrateLegacyMonitors(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM uptime_monitors`).Scan(&count); err != nil || count > 0 {
		return err
	}
	var raw sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = 'uptime_monitors_json'`).Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	var monitors []map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &monitors); err != nil {
		return nil
	}
	for _, monitor := range monitors {
		if _, err := s.createMonitor(ctx, db, monitor); err != nil {
			continue
		}
	}
	return nil
}

