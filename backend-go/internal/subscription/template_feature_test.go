package subscription

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// 端到端：模板功能必须可用，且内置模板渲染结果不含「稳定」组。
func TestTemplateFeatureRestoredWithoutStableGroup(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ensureBuiltins(ctx, db, false); err != nil {
		t.Fatalf("ensureBuiltins: %v", err)
	}

	// 1) 内置模板必须存在
	tpls, err := loadTemplates(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) < 3 {
		t.Fatalf("builtin templates = %d, want >= 3", len(tpls))
	}
	ids := map[string]bool{}
	for _, tpl := range tpls {
		ids[tpl.ID] = true
	}

	// 2) 自定义模板 CRUD 可用
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_templates(id,name,format,content) VALUES('custom','自定义','clash','proxies:\n{{ proxies_yaml }}\nproxy-groups:\n  - name: 手动\n    type: select\n    proxies:\n{{ proxy_names_yaml | indent 6 }}')`); err != nil {
		t.Fatal(err)
	}
	after, _ := loadTemplates(ctx, db)
	if len(after) != len(tpls)+1 {
		t.Fatalf("custom template not listed: %d -> %d", len(tpls), len(after))
	}

	// 3) 内置模板渲染结果不能出现「稳定」组 / 占位符
	nodes := []Node{{Name: "🇭🇰 香港", Type: "vless", Server: "n.example.com", Port: 443, ConfigJSON: `{"name":"🇭🇰 香港","type":"vless","server":"n.example.com","port":443,"uuid":"u"}`, Enabled: true}}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, nodes, "clash", false)
	if err != nil {
		t.Fatalf("renderOutput: %v", err)
	}
	if strings.Contains(body, "稳定") {
		t.Errorf("builtin template must not contain the stable group:\n%s", body)
	}
	if strings.Contains(body, "{{") {
		t.Errorf("unresolved placeholder remains:\n%s", body)
	}
	// YAML 会把 emoji 转义为 \U0001F1ED，这里断言节点名的主体部分。
	if !strings.Contains(body, "香港") {
		t.Error("node missing from rendered output")
	}
	if err := validateMihomoOutput(body); err != nil {
		t.Fatalf("rendered output invalid: %v", err)
	}
}
