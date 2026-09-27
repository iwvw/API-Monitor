package bookmarks

import (
	"testing"
)

// 历史库可能出现重复 slug：slug 列是 ALTER TABLE 补上的，没有 UNIQUE 约束，
// 而分配逻辑是「先查后插」，并发建组就能写重。
// 一旦带重复值，唯一索引建不出来，而 ensureSchema 每次 open 都跑，
// 结果是整个网址导航模块持续报错 —— 回归锁住「先消解再建索引」。
func TestMigrationDedupesLegacySlugConflict(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range []string{`DROP TABLE IF EXISTS bookmarks`, `DROP TABLE IF EXISTS bookmark_groups`} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	// 旧版结构：slug 列存在但无唯一约束
	if _, err := db.ExecContext(ctx, `CREATE TABLE bookmark_groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		description TEXT DEFAULT '',
		icon TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		slug TEXT,
		domain TEXT,
		cache_seconds INTEGER DEFAULT 300,
		config_json TEXT,
		created_at TEXT, updated_at TEXT,
		public INTEGER DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"甲", "乙", "丙"} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO bookmark_groups (title, slug, public) VALUES (?, 'tools', 1)`, title); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	// 重新打开即触发迁移：必须成功，不能因重复 slug 卡死
	migrated, err := service.open(ctx)
	if err != nil {
		t.Fatalf("migration must tolerate duplicate slugs: %v", err)
	}
	defer migrated.Close()

	rows, err := migrated.QueryContext(ctx, `SELECT id, slug FROM bookmark_groups ORDER BY id ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	kept := ""
	for rows.Next() {
		var id int64
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			t.Fatal(err)
		}
		if slug == "" {
			t.Errorf("group %d lost its slug", id)
		}
		if seen[slug] {
			t.Errorf("slug %q still duplicated after migration", slug)
		}
		seen[slug] = true
		if id == 1 {
			kept = slug
		}
	}
	if kept != "tools" {
		t.Errorf("lowest id must keep the original slug, got %q", kept)
	}

	// 唯一索引必须真的生效：再写一个重复 slug 要被数据库挡下
	if _, err := migrated.ExecContext(ctx,
		`INSERT INTO bookmark_groups (title, slug) VALUES ('丁', 'tools')`); err == nil {
		t.Error("unique slug index is not enforcing uniqueness")
	}
}

// 旧库若是「CREATE TABLE 里就写了 slug TEXT UNIQUE」建出来的，
// 里面可能躺着多行 NULL slug。把它们统一回填成空串会直接撞 UNIQUE，
// 迁移必须能正常跑完。
func TestMigrationToleratesNullableSlugOnUniqueTable(t *testing.T) {
	service := newTestService(t)
	ctx := t.Context()

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range []string{`DROP TABLE IF EXISTS bookmarks`, `DROP TABLE IF EXISTS bookmark_groups`} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE bookmark_groups (
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
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"甲", "乙"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO bookmark_groups (title, slug) VALUES (?, NULL)`, title); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	migrated, err := service.open(ctx)
	if err != nil {
		t.Fatalf("migration must not break on multiple NULL slugs: %v", err)
	}
	migrated.Close()

	if rec, _ := doJSON(service, "GET", "/api/bookmarks/groups", ""); rec.Code != 200 {
		t.Errorf("bookmarks list broken after migration: status=%d", rec.Code)
	}
}
