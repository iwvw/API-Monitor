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
			slug TEXT,
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
		// 公开页的全局设置（单行）。
		// 背景属于「整个公开页」而不是某个分组 —— 之前误放在分组的 config_json 里，
		// 语义上说不通（同一页面上不同分组会有不同背景），因此改为全局单行配置。
		`CREATE TABLE IF NOT EXISTS bookmark_public_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			config_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
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
		// bookmarks 表此前完全没有迁移项：旧库若缺这些列，listItems/createItem
		// 会直接报 no such column。
		{"bookmarks", "icon_bg_color", "ALTER TABLE bookmarks ADD COLUMN icon_bg_color TEXT DEFAULT ''"},
		{"bookmarks", "open_method", "ALTER TABLE bookmarks ADD COLUMN open_method INTEGER DEFAULT 2"},
		{"bookmarks", "icon_text", "ALTER TABLE bookmarks ADD COLUMN icon_text TEXT DEFAULT ''"},
		{"bookmarks", "icon_type", "ALTER TABLE bookmarks ADD COLUMN icon_type INTEGER DEFAULT 2"},
		{"bookmarks", "icon_src", "ALTER TABLE bookmarks ADD COLUMN icon_src TEXT DEFAULT ''"},
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

	// 第二批补丁：回填旧行中的 NULL。
	// ALTER TABLE ADD COLUMN 只给「此后插入」的行填默认值，既有行仍为 NULL；
	// 而 listGroups/listItems 用非空类型 Scan 这些列，NULL 会直接报
	// 「Failed to scan group」。旧库升级后列表页 500 的原因就在这里。
	backfills := []string{
		`UPDATE bookmark_groups SET public = 0 WHERE public IS NULL`,
		// slug 故意不回填空串：所有读路径都用 sql.NullString，
		// NULL 与 '' 对上层没有区别，但 NULL 被唯一索引显式排除。
		// 旧库可能是「CREATE TABLE 里写了 slug TEXT UNIQUE」建出来的，
		// 里面能躺着多行 NULL；统一写成 '' 会撞 UNIQUE 把迁移拖挂，
		// 而且下一次迁移又会再撞一次（NULL 越来越少、'' 越来越多）。
		`UPDATE bookmark_groups SET domain = '' WHERE domain IS NULL`,
		`UPDATE bookmark_groups SET cache_seconds = 300 WHERE cache_seconds IS NULL`,
		`UPDATE bookmark_groups SET description = '' WHERE description IS NULL`,
		`UPDATE bookmark_groups SET icon = '' WHERE icon IS NULL`,
		`UPDATE bookmark_groups SET sort_order = 0 WHERE sort_order IS NULL`,
		`UPDATE bookmark_groups SET created_at = datetime('now') WHERE created_at IS NULL`,
		`UPDATE bookmark_groups SET updated_at = datetime('now') WHERE updated_at IS NULL`,
		`UPDATE bookmarks SET description = '' WHERE description IS NULL`,
		`UPDATE bookmarks SET icon_src = '' WHERE icon_src IS NULL`,
		`UPDATE bookmarks SET icon_text = '' WHERE icon_text IS NULL`,
		`UPDATE bookmarks SET icon_bg_color = '' WHERE icon_bg_color IS NULL`,
		`UPDATE bookmarks SET icon_type = 2 WHERE icon_type IS NULL`,
		`UPDATE bookmarks SET open_method = 2 WHERE open_method IS NULL`,
		`UPDATE bookmarks SET sort_order = 0 WHERE sort_order IS NULL`,
		`UPDATE bookmarks SET created_at = datetime('now') WHERE created_at IS NULL`,
		`UPDATE bookmarks SET updated_at = datetime('now') WHERE updated_at IS NULL`,
	}
	for _, stmt := range backfills {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("backfill (%s): %w", stmt, err)
		}
	}

	// 旧库若存在 src 列（历史命名），把值迁到 icon_src，避免图标整体丢失。
	if legacySrc, err := hasColumn(ctx, db, "bookmarks", "src"); err != nil {
		return err
	} else if legacySrc {
		if _, err := db.ExecContext(ctx,
			`UPDATE bookmarks SET icon_src = src WHERE (icon_src IS NULL OR icon_src = '') AND src IS NOT NULL AND src != ''`); err != nil {
			return fmt.Errorf("migrate legacy src -> icon_src: %w", err)
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
	// slug 的唯一性此前只写在 CREATE TABLE 里：旧库经 ALTER TABLE 补列时
	// 无法携带 UNIQUE，于是 allocateSlug 的「先查后插」没有任何兜底，
	// 并发建组会写出重复 slug（公开页随即指向错误分组）。
	return ensureUniqueSlugIndex(ctx, db)
}

const uniqueSlugIndexStmt = `CREATE UNIQUE INDEX IF NOT EXISTS idx_bookmark_groups_slug_unique ON bookmark_groups(slug) WHERE slug IS NOT NULL AND slug != ''`

// ensureUniqueSlugIndex 建立 slug 唯一索引，遇到历史重复 slug 时先消解再重试。
//
// 为什么不直接失败返回：ensureSchema 每次 open 都会跑，一旦索引建不出来，
// 整个网址导航模块会持续 500，直到人工进 SQLite 改数据 —— 重复 slug 本身
// 却是「先查后插」在并发下就能产生的正常历史产物，不该由用户买单。
func ensureUniqueSlugIndex(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, uniqueSlugIndexStmt); err != nil {
		if dedupeErr := dedupeGroupSlugs(ctx, db); dedupeErr != nil {
			return fmt.Errorf("dedupe bookmark group slugs: %w", dedupeErr)
		}
		if _, retryErr := db.ExecContext(ctx, uniqueSlugIndexStmt); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

// dedupeGroupSlugs 消解历史库里的重复 slug。
//
// 策略：同一个 slug 保留 id 最小的一行（公开页此前也只会命中其中某一条），
// 其余按 slug-2、slug-3 递增改写成未占用的地址。不用「清空 slug」是因为
// 重复行里可能有已公开的分组，清空会让它的公开地址当场失效。
func dedupeGroupSlugs(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT slug FROM bookmark_groups WHERE slug IS NOT NULL AND slug != ''
		 GROUP BY slug HAVING COUNT(1) > 1`)
	if err != nil {
		return err
	}
	var duplicates []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return err
		}
		duplicates = append(duplicates, slug)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, slug := range duplicates {
		ids, err := groupIDsBySlug(ctx, db, slug)
		if err != nil {
			return err
		}
		if len(ids) <= 1 {
			continue
		}
		for _, id := range ids[1:] {
			next, err := freeSlugVariant(ctx, db, slug)
			if err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `UPDATE bookmark_groups SET slug = ? WHERE id = ?`, next, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func groupIDsBySlug(ctx context.Context, db *sql.DB, slug string) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM bookmark_groups WHERE slug = ? ORDER BY id ASC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// freeSlugVariant 找第一个未被占用的 slug-N（N 从 2 起）。
func freeSlugVariant(ctx context.Context, db *sql.DB, slug string) (string, error) {
	for suffix := 2; suffix < 1000; suffix++ {
		candidate := fmt.Sprintf("%s-%d", slug, suffix)
		rows, err := db.QueryContext(ctx, `SELECT id FROM bookmark_groups WHERE slug = ? LIMIT 1`, candidate)
		if err != nil {
			return "", err
		}
		taken := rows.Next()
		rows.Close()
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free slug variant for %q", slug)
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
