package huawei

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS huawei_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			site TEXT NOT NULL DEFAULT 'cn',
			access_key_id TEXT NOT NULL,
			secret_access_key_encrypted TEXT NOT NULL,
			domain_id TEXT,
			default_region TEXT,
			default_project_id TEXT,
			description TEXT,
			last_verified_at DATETIME,
			last_verify_status TEXT,
			last_verify_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_huawei_accounts_created_at ON huawei_accounts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_huawei_accounts_access_key_id ON huawei_accounts(access_key_id)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure huawei schema: %w", err)
		}
	}

	// 列迁移：补齐旧表缺失的 SSH 凭据列（幂等）。
	columns := []struct {
		table string
		name  string
		sql   string
	}{
		{"huawei_accounts", "ssh_user", "ALTER TABLE huawei_accounts ADD COLUMN ssh_user TEXT"},
		{"huawei_accounts", "ssh_port", "ALTER TABLE huawei_accounts ADD COLUMN ssh_port INTEGER"},
		{"huawei_accounts", "ssh_private_key_encrypted", "ALTER TABLE huawei_accounts ADD COLUMN ssh_private_key_encrypted TEXT"},
		{"huawei_accounts", "ssh_password_encrypted", "ALTER TABLE huawei_accounts ADD COLUMN ssh_password_encrypted TEXT"},
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
