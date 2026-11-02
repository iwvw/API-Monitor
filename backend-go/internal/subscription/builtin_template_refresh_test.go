package subscription

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// 存量库里可能残留旧版内置模板（含「😇 稳定」组）。启动时必须替换掉，
// 否则订阅输出会重新出现稳定组，违背本次「模板里删掉稳定组」的要求。
func TestStaleBuiltinTemplateWithStableGroupIsRefreshed(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	stale := "proxies:\n{{ proxies_yaml }}\nproxy-groups:\n  - name: 😇 稳定\n    type: select\n    proxies:\n{{ stable_proxy_names_yaml | indent 6 }}\n"
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_templates(id,name,format,content,builtin,is_default) VALUES(?,?,?,?,1,1)`, defaultTemplateID, "旧内置模板", "clash", stale); err != nil {
		t.Fatal(err)
	}
	if err := ensureBuiltins(ctx, db, false); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := db.QueryRowContext(ctx, `SELECT content FROM subscription_templates WHERE id=?`, defaultTemplateID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "稳定") || strings.Contains(content, "stable_proxy_names_yaml") {
		t.Fatalf("stale builtin template still contains the stable group:\n%s", content)
	}
}
