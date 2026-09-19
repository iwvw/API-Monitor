package aiagent

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureSchema 幂等创建模块全部表与索引。表名统一 aiagent_ 前缀；时间列一律
// RFC3339 UTC 字符串；不建跨模块外键（server_id 只存字符串，存在性由服务层校验）。
//
// 实例是平台资源，不再归属单个用户：用户与实例通过 aiagent_instance_grants
// 建立多对多授权。旧库的 aiagent_instances.user_id 会在 migrateInstanceOwnership
// 中转为授权记录后删除。
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
			server_id  TEXT NOT NULL,
			provider   TEXT NOT NULL,
			label      TEXT NOT NULL,
			port       INTEGER NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_instances_server ON aiagent_instances(server_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_aiagent_instances_unique ON aiagent_instances(server_id, provider)`,
		// 用户 ↔ 实例授权：默认不授权，由管理员按用户勾选可用的实例。
		`CREATE TABLE IF NOT EXISTS aiagent_instance_grants (
			instance_id TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			PRIMARY KEY (instance_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_aiagent_instance_grants_user ON aiagent_instance_grants(user_id)`,
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
	if err := s.migrateInstanceOwnership(ctx, db); err != nil {
		return fmt.Errorf("aiagent migrateInstanceOwnership: %w", err)
	}
	return nil
}

// migrateInstanceOwnership 把旧库的实例归属列转为授权记录。
//
// 历史模型是「实例归属一个用户」，现在改为平台实例 + 多对多授权。迁移分两步：
// 先把 user_id 复制成授权行（保持这些用户原本可见的实例不变），再删除该列
// 及其遗留索引，最后重建以 (server_id, provider) 为键的唯一索引。
//
// 幂等：user_id 列不存在时直接返回，重复启动不会重复执行。
func (s *Service) migrateInstanceOwnership(ctx context.Context, db *sql.DB) error {
	hasColumn, err := columnExists(ctx, db, "aiagent_instances", "user_id")
	if err != nil {
		return err
	}
	if !hasColumn {
		return nil
	}
	// 旧归属转授权。空 user_id 是历史脏数据，跳过即可（新模型下实例不要求归属）。
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO aiagent_instance_grants (instance_id, user_id, created_at)
		SELECT id, user_id, created_at FROM aiagent_instances
		WHERE user_id IS NOT NULL AND user_id != ''`); err != nil {
		return err
	}
	// user_id 参与旧索引，必须先删索引才能删列。
	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_aiagent_instances_user`,
		`DROP INDEX IF EXISTS idx_aiagent_instances_unique`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE aiagent_instances DROP COLUMN user_id`); err != nil {
		return err
	}
	// 实例改为平台资源后，同一主机同一 Provider 只应登记一次。
	_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_aiagent_instances_unique
		ON aiagent_instances(server_id, provider)`)
	return err
}

// columnExists 通过 PRAGMA 判断表是否已有指定列（SQLite 无 IF EXISTS 的加列语法）。
func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
