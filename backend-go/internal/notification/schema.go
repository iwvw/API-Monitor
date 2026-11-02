package notification

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('email', 'telegram')),
			enabled INTEGER DEFAULT 1,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source_module TEXT NOT NULL,
			event_type TEXT NOT NULL,
			severity TEXT DEFAULT 'warning' CHECK(severity IN ('critical', 'warning', 'info')),
			enabled INTEGER DEFAULT 1,
			channels TEXT NOT NULL,
			conditions TEXT,
			suppression TEXT,
			time_window TEXT,
			description TEXT,
			title_template TEXT DEFAULT '',
			message_template TEXT DEFAULT '',
			backup_channels TEXT DEFAULT '[]',
			quiet_until DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS maintenance_schedules (
			id TEXT PRIMARY KEY,
			target_type TEXT NOT NULL,
			target_id TEXT,
			start_at DATETIME NOT NULL,
			end_at DATETIME NOT NULL,
			reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS notification_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'failed', 'retrying')),
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			data TEXT,
			error_message TEXT,
			sent_at DATETIME,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS notification_message_state (
			channel_id TEXT NOT NULL,
			source_module TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			lifecycle_kind TEXT NOT NULL,
			chat_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			last_data TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (channel_id, source_module, resource_key, lifecycle_kind)
		)`,
		`CREATE TABLE IF NOT EXISTS alert_state_tracking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			last_triggered_at DATETIME NOT NULL,
			consecutive_failures INTEGER DEFAULT 1,
			last_notified_at DATETIME,
			metadata TEXT,
			state_history TEXT DEFAULT '[]',
			is_flapping INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(rule_id, fingerprint)
		)`,
		`CREATE TABLE IF NOT EXISTS notification_global_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			max_retry_times INTEGER DEFAULT 3,
			retry_interval_seconds INTEGER DEFAULT 60,
			history_retention_days INTEGER DEFAULT 30,
			enable_batch INTEGER DEFAULT 1,
			batch_interval_seconds INTEGER DEFAULT 30,
			default_channels TEXT,
			global_rate_limit_per_hour INTEGER DEFAULT 100,
			enable_auto_escalation INTEGER DEFAULT 0,
			base_url TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_channels_type ON notification_channels(type, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_rules_source ON alert_rules(source_module, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_rule ON notification_history(rule_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_status ON notification_history(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_created ON notification_history(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_message_resource ON notification_message_state(source_module, resource_key, lifecycle_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_rule ON alert_state_tracking(rule_id, fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_triggered ON alert_state_tracking(last_triggered_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure notification schema: %w", err)
		}
	}
	for _, column := range []struct {
		table string
		name  string
		sql   string
	}{
		{"alert_rules", "title_template", "ALTER TABLE alert_rules ADD COLUMN title_template TEXT DEFAULT ''"},
		{"alert_rules", "message_template", "ALTER TABLE alert_rules ADD COLUMN message_template TEXT DEFAULT ''"},
		{"alert_rules", "backup_channels", "ALTER TABLE alert_rules ADD COLUMN backup_channels TEXT DEFAULT '[]'"},
		{"alert_rules", "quiet_until", "ALTER TABLE alert_rules ADD COLUMN quiet_until DATETIME"},
		{"alert_state_tracking", "state_history", "ALTER TABLE alert_state_tracking ADD COLUMN state_history TEXT DEFAULT '[]'"},
		{"alert_state_tracking", "is_flapping", "ALTER TABLE alert_state_tracking ADD COLUMN is_flapping INTEGER DEFAULT 0"},
		{"notification_message_state", "last_data", "ALTER TABLE notification_message_state ADD COLUMN last_data TEXT DEFAULT '{}'"},
		{"notification_global_config", "global_rate_limit_per_hour", "ALTER TABLE notification_global_config ADD COLUMN global_rate_limit_per_hour INTEGER DEFAULT 100"},
		{"notification_global_config", "enable_auto_escalation", "ALTER TABLE notification_global_config ADD COLUMN enable_auto_escalation INTEGER DEFAULT 0"},
		{"notification_global_config", "base_url", "ALTER TABLE notification_global_config ADD COLUMN base_url TEXT"},
	} {
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
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO notification_global_config (
			id, max_retry_times, retry_interval_seconds,
			history_retention_days, enable_batch, batch_interval_seconds,
			default_channels, global_rate_limit_per_hour, enable_auto_escalation, base_url
		) VALUES (1, 3, 60, 30, 1, 30, '[]', 100, 0, '')
	`); err != nil {
		return fmt.Errorf("ensure notification default config: %w", err)
	}
	return nil
}

func hasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
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
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}
