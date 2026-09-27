package bookmarks

import (
	"fmt"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestMigrationOnExistingDatabase 验证 ensureSchema 对「已存在的旧库」是安全幂等的：
// 这是生产环境首次升级时真正会走的路径，比全新库更容易出问题。
func TestMigrationOnExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, DBName: "legacy.db"}

	// 用旧结构手工建库：bookmark_groups 缺少 public/slug/domain/cache_seconds/config_json，
	// bookmarks 缺少若干图标列 —— 模拟加入这些功能之前的库。
	store := newTestService(t)
	ctx := t.Context()
	db, err := store.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = cfg
	db.Close()

	// 用一个隔离目录，重建「旧库」
	legacy := newTestService(t)
	ldb, err := legacy.open(ctx)
	if err != nil {
		t.Fatalf("legacy open: %v", err)
	}
	if _, err := ldb.ExecContext(ctx, `DROP TABLE IF EXISTS bookmarks`); err != nil {
		t.Fatal(err)
	}
	if _, err := ldb.ExecContext(ctx, `DROP TABLE IF EXISTS bookmark_groups`); err != nil {
		t.Fatal(err)
	}
	// 旧版 schema：无 public/slug/domain/cache_seconds/config_json，无 icon_bg_color/open_method
	legacySQL := []string{
		`CREATE TABLE bookmark_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE bookmarks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			url TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon_type INTEGER DEFAULT 2,
			src TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			created_at TEXT, updated_at TEXT)`,
	}
	for _, stmt := range legacySQL {
		if _, err := ldb.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}
	// 塞入旧数据，迁移不能丢
	if _, err := ldb.ExecContext(ctx, `INSERT INTO bookmark_groups (title) VALUES ('历史分组')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ldb.ExecContext(ctx, `INSERT INTO bookmarks (group_id, title, url) VALUES (1, '历史网址', 'https://legacy.example')`); err != nil {
		t.Fatal(err)
	}
	ldb.Close()

	// 重新打开：应自动补齐所有列
	db2, err := legacy.open(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	defer db2.Close()

	for _, tc := range []struct {
		table  string
		column string
	}{
		{"bookmark_groups", "public"},
		{"bookmark_groups", "slug"},
		{"bookmark_groups", "domain"},
		{"bookmark_groups", "cache_seconds"},
		{"bookmark_groups", "config_json"},
		{"bookmarks", "icon_bg_color"},
		{"bookmarks", "open_method"},
		{"bookmarks", "icon_text"},
	} {
		ok, err := hasColumn(ctx, db2, tc.table, tc.column)
		if err != nil {
			t.Fatalf("hasColumn(%s.%s): %v", tc.table, tc.column, err)
		}
		if !ok {
			t.Errorf("migration missing column %s.%s", tc.table, tc.column)
		}
	}

	// 历史数据必须保留
	var title, url string
	if err := db2.QueryRowContext(ctx, `SELECT title FROM bookmark_groups WHERE id = 1`).Scan(&title); err != nil {
		t.Fatalf("legacy group lost: %v", err)
	}
	if err := db2.QueryRowContext(ctx, `SELECT url FROM bookmarks WHERE id = 1`).Scan(&url); err != nil {
		t.Fatalf("legacy item lost: %v", err)
	}
	if title != "历史分组" || url != "https://legacy.example" {
		t.Errorf("legacy data altered: %q / %q", title, url)
	}
	t.Logf("migration preserved legacy data: group=%q item=%q", title, url)

	// 迁移后所有读路径都必须可用（旧实现会因缺列报 no such column）
	for _, path := range []string{"/api/bookmarks/groups", "/api/bookmarks/items"} {
		rec, _ := doJSON(legacy, "GET", path, "")
		if rec.Code != 200 {
			t.Errorf("after migration %s returned %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	// 幂等：再次打开不应报错
	db3, err := legacy.open(ctx)
	if err != nil {
		t.Fatalf("second migration run must be idempotent: %v", err)
	}
	db3.Close()

	fmt.Println("legacy migration verified")
}
