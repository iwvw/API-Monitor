package gcp

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS gcp_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			client_email TEXT NOT NULL,
			default_project_id TEXT,
			service_account_json_encrypted TEXT NOT NULL,
			description TEXT,
			last_verified_at DATETIME,
			last_verify_status TEXT,
			last_verify_error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gcp_accounts_created_at ON gcp_accounts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_gcp_accounts_client_email ON gcp_accounts(client_email)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure gcp schema: %w", err)
		}
	}
	return nil
}