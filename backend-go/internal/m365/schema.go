package m365

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS m365_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			client_id TEXT NOT NULL,
			client_secret TEXT NOT NULL,
			description TEXT,
			default_domain TEXT,
			verified_domains TEXT,
			organization_name TEXT,
			enabled INTEGER DEFAULT 1,
			last_verified_at DATETIME,
			last_verified_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_m365_accounts_tenant_client ON m365_accounts(tenant_id, client_id)`,
		`CREATE TABLE IF NOT EXISTS m365_registration_invites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			account_ids TEXT,
			domain TEXT NOT NULL,
			domains TEXT,
			usage_location TEXT,
			sku_ids TEXT,
			max_uses INTEGER DEFAULT 1,
			used_count INTEGER DEFAULT 0,
			enabled INTEGER DEFAULT 1,
			force_change_password_next_sign_in INTEGER DEFAULT 0,
			batch_id TEXT,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_registration_invites_account ON m365_registration_invites(account_id)`,
		`CREATE TABLE IF NOT EXISTS m365_registration_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			invite_id INTEGER NOT NULL,
			account_id INTEGER NOT NULL,
			display_name TEXT NOT NULL,
			user_principal_name TEXT NOT NULL,
			graph_user_id TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (invite_id) REFERENCES m365_registration_invites(id) ON DELETE CASCADE,
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_registration_records_invite ON m365_registration_records(invite_id)`,
		`CREATE TABLE IF NOT EXISTS m365_sync_state (
			account_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			last_synced_at DATETIME,
			last_error TEXT,
			cursor_value TEXT,
			PRIMARY KEY (account_id, resource_type),
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure m365 schema: %w", err)
		}
	}
	if err := ensureTableColumn(ctx, db, "m365_accounts", "verified_domains", "TEXT"); err != nil {
		return err
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "account_ids", definition: "TEXT"},
		{name: "domains", definition: "TEXT"},
		{name: "batch_id", definition: "TEXT"},
	} {
		if err := ensureTableColumn(ctx, db, "m365_registration_invites", column.name, column.definition); err != nil {
			return err
		}
	}
	return ensurePublicPageSchema(ctx, db)
}

func ensureTableColumn(ctx context.Context, db *sql.DB, tableName, columnName, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", tableName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, columnName) {
			return nil
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE `+tableName+` ADD COLUMN `+columnName+` `+definition); err != nil {
		return fmt.Errorf("alter %s add %s: %w", tableName, columnName, err)
	}
	return nil
}
