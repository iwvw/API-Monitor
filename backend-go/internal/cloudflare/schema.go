package cloudflare

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cf_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			api_token TEXT NOT NULL,
			email TEXT,
			user_email TEXT,
			cf_account_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			is_active INTEGER DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS cf_dns_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			records TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS cf_zones (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			name TEXT NOT NULL,
			status TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES cf_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS cf_dns_records (
			id TEXT PRIMARY KEY,
			zone_id TEXT NOT NULL,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			content TEXT NOT NULL,
			ttl INTEGER DEFAULT 1,
			proxied INTEGER DEFAULT 0,
			priority INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (zone_id) REFERENCES cf_zones(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_zones_account ON cf_zones(account_id)`,
		`CREATE TABLE IF NOT EXISTS cf_email_inboxes (
			zone_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			zone_name TEXT,
			worker_name TEXT NOT NULL,
			panel_base_url TEXT NOT NULL,
			forward_to TEXT,
			strategy TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_dns_records_zone ON cf_dns_records(zone_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure cloudflare schema: %w", err)
		}
	}
	if err := ensureColumn(ctx, db, "cf_accounts", "cf_account_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "cf_accounts", "user_email", "TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "cf_email_inboxes", "strategy", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, name, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition)
	return err
}
