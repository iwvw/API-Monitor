package bookmarks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestMigrationAgainstProductionDBCopy 在「生产库的副本」上跑迁移。
// 这是真实升级路径：用户的 data.db 里已经有 bookmark_groups/bookmarks 表，
// 列可能缺失、历史行可能有 NULL。测试必须只读副本，绝不触碰原库。
func TestMigrationAgainstProductionDBCopy(t *testing.T) {
	src := `E:\Code\API-Monitor\.tmp\data-prodtest.db`
	if _, err := os.Stat(src); err != nil {
		t.Skipf("production DB copy not present: %v", err)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "data.db")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read DB copy: %v", err)
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		t.Fatalf("write DB copy: %v", err)
	}
	t.Logf("testing against a %d byte copy of the production database", len(raw))

	// 迁移前统计旧表状态
	before := newTestServiceAt(t, dir)
	ctx := t.Context()
	pre, err := before.open(ctx)
	if err != nil {
		t.Skipf("cannot open production copy (may not have bookmarks tables yet): %v", err)
	}
	for _, tbl := range []string{"bookmark_groups", "bookmarks"} {
		var n int
		if err := pre.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(1) FROM %s", tbl)).Scan(&n); err != nil {
			t.Logf("BEFORE %s: not present (%v)", tbl, err)
			continue
		}
		cols := []string{}
		rows, _ := pre.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tbl))
		for rows.Next() {
			var cid, notNull, pk int
			var name, typ string
			var dflt any
			rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk)
			cols = append(cols, name)
		}
		rows.Close()
		t.Logf("BEFORE %-16s rows=%d cols=%v", tbl, n, cols)
	}
	pre.Close()

	// 触发迁移（再次 open 会执行 ensureSchema + 回填）
	service := newTestServiceAt(t, dir)
	post, err := service.open(ctx)
	if err != nil {
		t.Fatalf("migration on production copy failed: %v", err)
	}
	defer post.Close()

	// 迁移后：所有列表读路径必须 200，不能因 NULL/缺列而 500
	for _, path := range []string{"/api/bookmarks/groups", "/api/bookmarks/items"} {
		rec, payload := doJSON(service, "GET", path, "")
		if rec.Code != 200 {
			body := rec.Body.String()
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			t.Errorf("after migration %s -> %d: %s", path, rec.Code, body)
			continue
		}
		if path == "/api/bookmarks/groups" {
			groups := payload["groups"].([]interface{})
			items := 0
			for _, g := range groups {
				items += len(g.(map[string]interface{})["items"].([]interface{}))
			}
			t.Logf("AFTER  groups=%d nestedItems=%d", len(groups), items)
		}
	}

	// 迁移必须幂等
	again, err := service.open(ctx)
	if err != nil {
		t.Fatalf("second migration run must be idempotent: %v", err)
	}
	again.Close()

	// 关键新列必须存在
	for _, tc := range []struct{ table, col string }{
		{"bookmark_groups", "public"}, {"bookmark_groups", "slug"}, {"bookmark_groups", "domain"},
		{"bookmark_groups", "cache_seconds"}, {"bookmark_groups", "config_json"},
		{"bookmarks", "icon_bg_color"}, {"bookmarks", "open_method"}, {"bookmarks", "icon_text"},
	} {
		ok, err := hasColumn(ctx, post, tc.table, tc.col)
		if err != nil || !ok {
			t.Errorf("missing column %s.%s (err=%v)", tc.table, tc.col, err)
		}
	}
	t.Log("production-copy migration verified: all reads 200, schema complete, idempotent")
}

// newTestServiceAt 在指定目录构造 service（不新建临时目录）。
func newTestServiceAt(t *testing.T, dir string) *Service {
	t.Helper()
	return New(config.Config{DataDir: dir, DBName: "data.db"})
}
