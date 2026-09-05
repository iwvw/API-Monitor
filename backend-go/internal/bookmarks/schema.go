package bookmarks

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	// 第一批：建表（幂等）。旧库已存在同名表时跳过，表结构由下文的
	// 列迁移补齐，因此此处绝不允许任何引用新列的语句（如 CREATE INDEX
	// 引用 public/slug/domain）在迁移完成前执行。
	statements := []string{
		`CREATE TABLE IF NOT EXISTS bookmark_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			public INTEGER DEFAULT 0,
			slug TEXT UNIQUE,
			domain TEXT,
			cache_seconds INTEGER DEFAULT 300,
			config_json TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bookmark_groups_sort ON bookmark_groups(sort_order)`,

		`CREATE TABLE IF NOT EXISTS bookmarks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			url TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon_type INTEGER DEFAULT 2,
			icon_src TEXT DEFAULT '',
			icon_text TEXT DEFAULT '',
			icon_bg_color TEXT DEFAULT '',
			open_method INTEGER DEFAULT 2,
			sort_order INTEGER DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (group_id) REFERENCES bookmark_groups(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bookmarks_group_sort ON bookmarks(group_id, sort_order)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	// 第二批：列迁移，补齐旧表缺失的列。
	columns := []struct {
		table string
		name  string
		sql   string
	}{
		{"bookmark_groups", "public", "ALTER TABLE bookmark_groups ADD COLUMN public INTEGER DEFAULT 0"},
		{"bookmark_groups", "slug", "ALTER TABLE bookmark_groups ADD COLUMN slug TEXT"},
		{"bookmark_groups", "domain", "ALTER TABLE bookmark_groups ADD COLUMN domain TEXT"},
		{"bookmark_groups", "cache_seconds", "ALTER TABLE bookmark_groups ADD COLUMN cache_seconds INTEGER DEFAULT 300"},
		{"bookmark_groups", "config_json", "ALTER TABLE bookmark_groups ADD COLUMN config_json TEXT"},
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

	// 第三批：依赖新列的索引，必须在列迁移完成后创建。
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_bookmark_groups_public ON bookmark_groups(public, slug)`,
		`CREATE INDEX IF NOT EXISTS idx_bookmark_groups_domain ON bookmark_groups(domain, public)`,
	}
	for _, stmt := range indexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
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