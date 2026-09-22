package assets

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS assets (
			id TEXT PRIMARY KEY,
			origin TEXT NOT NULL DEFAULT 'manual',
			category TEXT NOT NULL,
			asset_type TEXT NOT NULL,
			name TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			owner TEXT NOT NULL DEFAULT '',
			location TEXT NOT NULL DEFAULT '',
			serial_no TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			source_module TEXT NOT NULL DEFAULT '',
			source_ref_id TEXT NOT NULL DEFAULT '',
			source_synced_at DATETIME,
			acquire_date TEXT NOT NULL DEFAULT '',
			expire_at TEXT NOT NULL DEFAULT '',
			warn_days_json TEXT NOT NULL DEFAULT '[]',
			auto_renew INTEGER NOT NULL DEFAULT 0,
			cost_amount REAL NOT NULL DEFAULT 0,
			cost_currency TEXT NOT NULL DEFAULT '',
			cost_cycle TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			remark TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_category ON assets(category)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_type ON assets(asset_type)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_status ON assets(status)`,
		`CREATE INDEX IF NOT EXISTS idx_assets_expire_at ON assets(expire_at)`,
		`CREATE TABLE IF NOT EXISTS asset_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			base_currency TEXT NOT NULL DEFAULT '',
			exchange_rates_json TEXT NOT NULL DEFAULT '{}',
			warn_days_json TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS asset_links (
			id TEXT PRIMARY KEY,
			asset_id TEXT NOT NULL,
			source_module TEXT NOT NULL,
			source_ref_id TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(source_module, source_ref_id)
		)`,
		`CREATE TABLE IF NOT EXISTS asset_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			asset_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_events_asset ON asset_events(asset_id)`,
		`CREATE TABLE IF NOT EXISTS asset_alerts (
			asset_id TEXT NOT NULL,
			marker TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (asset_id, marker)
		)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure assets schema: %w", err)
		}
	}
	return nil
}
