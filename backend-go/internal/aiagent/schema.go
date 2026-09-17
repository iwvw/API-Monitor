package aiagent

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureSchema 幂等创建模块全部表与索引。表名统一 aiagent_ 前缀；时间列一律
// RFC3339 UTC 字符串；不建跨模块外键（server_id 只存字符串，存在性由服务层校验）。
func (s *Service) ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS aiagent_users (
			id            TEXT PRIMARY KEY,
			username      TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			display_name  TEXT,
			disabled      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL,
			last_login_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS aiagent_tokens (
			id           TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			token_hash   TEXT NOT NULL,
			token_prefix TEXT NOT NULL,
			device_label TEXT,
			expires_at   TEXT NOT NULL,
			revoked_at   TEXT,
			last_used_at TEXT,
			last_ip      TEXT,
			created_at   TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_tokens_user ON aiagent_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_tokens_prefix ON aiagent_tokens(token_prefix)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_tokens_hash ON aiagent_tokens(token_hash)`,
		`CREATE TABLE IF NOT EXISTS aiagent_instances (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			server_id  TEXT NOT NULL,
			provider   TEXT NOT NULL,
			label      TEXT NOT NULL,
			port       INTEGER NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_instances_user ON aiagent_instances(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_instances_server ON aiagent_instances(server_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_aiagent_instances_unique ON aiagent_instances(user_id, server_id, port)`,
		`CREATE TABLE IF NOT EXISTS aiagent_instance_meta (
			instance_id   TEXT PRIMARY KEY,
			user_id       TEXT NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			updated_at    TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS aiagent_user_preferences (
			user_id    TEXT NOT NULL,
			key        TEXT NOT NULL,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (user_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS aiagent_access_logs (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id       TEXT,
			token_id      TEXT,
			instance_id   TEXT,
			action        TEXT NOT NULL,
			result        TEXT NOT NULL,
			status_code   INTEGER,
			error_summary TEXT,
			ip            TEXT,
			user_agent    TEXT,
			created_at    TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_access_logs_created ON aiagent_access_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_access_logs_user ON aiagent_access_logs(user_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("aiagent ensureSchema: %w", err)
		}
	}
	return nil
}
