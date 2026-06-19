package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	_ "modernc.org/sqlite"
)

type Store struct {
	cfg        config.Config
	schemaOnce sync.Once
	schemaErr  error
}

func New(cfg config.Config) *Store {
	return &Store{cfg: cfg}
}

func (s *Store) DatabasePath() string {
	return s.cfg.DatabasePath()
}

func (s *Store) Open(ctx context.Context) (*sql.DB, error) {
	if err := os.MkdirAll(s.cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dbPath := s.cfg.DatabasePath()
	if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Clean(dbPath)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	s.schemaOnce.Do(func() {
		s.schemaErr = EnsureCoreSchema(ctx, db)
	})
	if s.schemaErr != nil {
		db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

func EnsureCoreSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			description TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			password TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			is_active INTEGER DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(is_active, expires_at)`,
		`CREATE TABLE IF NOT EXISTS login_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip_address TEXT NOT NULL UNIQUE,
			failed_count INTEGER DEFAULT 0,
			locked_until TEXT,
			last_attempt TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS operation_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			operation_type TEXT NOT NULL,
			table_name TEXT NOT NULL,
			record_id TEXT,
			details TEXT,
			ip_address TEXT,
			user_agent TEXT,
			trace_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operation_logs_table ON operation_logs(table_name, created_at)`,
		`CREATE TABLE IF NOT EXISTS user_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			custom_css TEXT,
			theme_mode TEXT DEFAULT 'auto',
			page_width_mode TEXT DEFAULT 'full',
			sidebar_collapsed INTEGER DEFAULT 0,
			module_visibility TEXT,
			module_order TEXT,
			channel_enabled TEXT,
			channel_model_prefix TEXT,
			load_balancing_strategy TEXT DEFAULT 'random',
			server_ip_display_mode TEXT DEFAULT 'normal',
			main_tabs_layout TEXT DEFAULT 'top',
			vibration_enabled INTEGER DEFAULT 1,
			totp_settings TEXT,
			agent_download_url TEXT,
			koyeb_refresh_interval INTEGER DEFAULT 30000,
			fly_refresh_interval INTEGER DEFAULT 30000,
			public_api_url TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS system_api_stats (
			date TEXT PRIMARY KEY,
			audit_count INTEGER DEFAULT 0,
			ops_count INTEGER DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure core schema: %w", err)
		}
	}
	if err := ensureUserSettingsColumns(ctx, db); err != nil {
		return err
	}
	return ensureDefaultUserSettings(ctx, db)
}

func ensureDefaultUserSettings(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_settings (
			id,
			custom_css,
			module_visibility,
			module_order,
			channel_enabled,
			channel_model_prefix
		) VALUES (
			1,
			'',
			'{"openai":true,"dns":true,"server":true}',
			'["openai","dns","server"]',
			'{}',
			'{}'
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure default user settings: %w", err)
	}
	return nil
}

func ensureUserSettingsColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{"theme_mode", "ALTER TABLE user_settings ADD COLUMN theme_mode TEXT DEFAULT 'auto'"},
		{"page_width_mode", "ALTER TABLE user_settings ADD COLUMN page_width_mode TEXT DEFAULT 'full'"},
		{"sidebar_collapsed", "ALTER TABLE user_settings ADD COLUMN sidebar_collapsed INTEGER DEFAULT 0"},
		{"channel_enabled", "ALTER TABLE user_settings ADD COLUMN channel_enabled TEXT"},
		{"channel_model_prefix", "ALTER TABLE user_settings ADD COLUMN channel_model_prefix TEXT"},
		{"load_balancing_strategy", "ALTER TABLE user_settings ADD COLUMN load_balancing_strategy TEXT DEFAULT 'random'"},
		{"server_ip_display_mode", "ALTER TABLE user_settings ADD COLUMN server_ip_display_mode TEXT DEFAULT 'normal'"},
		{"main_tabs_layout", "ALTER TABLE user_settings ADD COLUMN main_tabs_layout TEXT DEFAULT 'top'"},
		{"vibration_enabled", "ALTER TABLE user_settings ADD COLUMN vibration_enabled INTEGER DEFAULT 1"},
		{"totp_settings", "ALTER TABLE user_settings ADD COLUMN totp_settings TEXT"},
		{"agent_download_url", "ALTER TABLE user_settings ADD COLUMN agent_download_url TEXT"},
		{"koyeb_refresh_interval", "ALTER TABLE user_settings ADD COLUMN koyeb_refresh_interval INTEGER DEFAULT 30000"},
		{"fly_refresh_interval", "ALTER TABLE user_settings ADD COLUMN fly_refresh_interval INTEGER DEFAULT 30000"},
		{"public_api_url", "ALTER TABLE user_settings ADD COLUMN public_api_url TEXT"},
	}
	for _, column := range columns {
		exists, err := hasColumn(ctx, db, "user_settings", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add user_settings.%s: %w", column.name, err)
			}
		}
	}
	_, err := db.ExecContext(ctx, `
		UPDATE user_settings
		SET
			theme_mode = COALESCE(theme_mode, 'auto'),
			page_width_mode = COALESCE(page_width_mode, 'full'),
			sidebar_collapsed = COALESCE(sidebar_collapsed, 0),
			load_balancing_strategy = COALESCE(load_balancing_strategy, 'random'),
			server_ip_display_mode = COALESCE(server_ip_display_mode, 'normal'),
			main_tabs_layout = COALESCE(main_tabs_layout, 'top'),
			vibration_enabled = COALESCE(vibration_enabled, 1),
			koyeb_refresh_interval = COALESCE(koyeb_refresh_interval, 30000),
			fly_refresh_interval = COALESCE(fly_refresh_interval, 30000)
		WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("normalize user_settings defaults: %w", err)
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
			return false, fmt.Errorf("scan %s column: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	return false, nil
}
