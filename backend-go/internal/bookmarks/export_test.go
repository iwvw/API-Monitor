package bookmarks

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 导出 -> 导入往返：数据不丢。
func TestNativeExportImportRoundTrip(t *testing.T) {
	src := New(config.Config{DataDir: t.TempDir(), DBName: "src.db"})
	doJSON(src, http.MethodPost, "/api/bookmarks/groups",
		`{"title":"开发工具","description":"日常","public":true,"slug":"dev","domain":"dev.example.com","cache_seconds":600}`)
	_, gp := doJSON(src, http.MethodGet, "/api/bookmarks/groups", "")
	gid := int64Value(gp["groups"].([]interface{})[0].(map[string]interface{})["id"])
	doJSON(src, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(gid)+`,"title":"GitHub","url":"https://github.com","description":"代码托管","icon_type":2,"icon_src":"/api/bookmarks/favicons/a.png","open_method":1}`)
	doJSON(src, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"bgColor":"#123456","pageWidth":"narrow"}}`)

	// 导出
	_, exp := doJSON(src, http.MethodGet, "/api/bookmarks/export", "")
	if exp["success"] == false {
		t.Fatalf("export failed: %v", exp)
	}
	data := exp["data"].(map[string]interface{})
	if data["format"] != exportFormatName {
		t.Fatalf("format=%v, want %v", data["format"], exportFormatName)
	}
	groups := data["groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g0 := groups[0].(map[string]interface{})
	if g0["title"] != "开发工具" || g0["slug"] != "dev" || g0["public"] != true {
		t.Errorf("group fields wrong: %v", g0)
	}
	if items := g0["items"].([]interface{}); len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	// 导出不应带库内自增 id（换库后无意义）
	if _, hasID := g0["id"]; hasID {
		t.Errorf("export should not carry database ids, got %v", g0["id"])
	}

	// 导入到另一个空库
	dst := New(config.Config{DataDir: t.TempDir(), DBName: "dst.db"})
	raw, _ := json.Marshal(data)
	_, rep := doJSON(dst, http.MethodPost, "/api/bookmarks/import-native",
		`{"payload":`+string(raw)+`}`)
	r := rep["data"].(map[string]interface{})
	if r["groups_created"].(float64) != 1 || r["items_imported"].(float64) != 1 {
		t.Fatalf("import report wrong: %v", r)
	}

	// 回读校验
	_, check := doJSON(dst, http.MethodGet, "/api/bookmarks/groups", "")
	cg := check["groups"].([]interface{})[0].(map[string]interface{})
	if cg["title"] != "开发工具" || cg["slug"] != "dev" || cg["public"] != true {
		t.Errorf("imported group wrong: %v", cg)
	}
	ci := cg["items"].([]interface{})[0].(map[string]interface{})
	if ci["url"] != "https://github.com" || ci["description"] != "代码托管" {
		t.Errorf("imported item wrong: %v", ci)
	}
	if ci["open_method"].(float64) != 1 {
		t.Errorf("open_method should round-trip, got %v", ci["open_method"])
	}
	// 公开设置也要带上
	_, ps := doJSON(dst, http.MethodGet, "/api/bookmarks/public-settings", "")
	cfg := ps["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["bgColor"] != "#123456" || cfg["pageWidth"] != "narrow" {
		t.Errorf("public settings should round-trip, got %v", cfg)
	}
}

// merge 模式：同名分组只追加，重复网址跳过。
func TestNativeImportMergeSkipsDuplicates(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"已有分组"}`)
	_, gp := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	gid := int64Value(gp["groups"].([]interface{})[0].(map[string]interface{})["id"])
	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(gid)+`,"title":"A","url":"https://a.com"}`)

	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"已有分组","items":[
			{"title":"A","url":"https://a.com"},
			{"title":"B","url":"https://b.com"}
		]}
	]}`
	_, rep := doJSON(service, http.MethodPost, "/api/bookmarks/import-native", `{"payload":`+payload+`}`)
	r := rep["data"].(map[string]interface{})
	if r["groups_merged"].(float64) != 1 || r["groups_created"].(float64) != 0 {
		t.Errorf("should merge into existing group, got %v", r)
	}
	if r["items_imported"].(float64) != 1 {
		t.Errorf("only the new url should be imported, got %v", r["items_imported"])
	}
	if r["items_skipped"].(float64) != 1 {
		t.Errorf("duplicate url should be skipped, got %v", r["items_skipped"])
	}
}

// replace 模式：清空后整份导入。
func TestNativeImportReplaceClearsExisting(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"旧分组"}`)

	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"新分组","items":[{"title":"X","url":"https://x.com"}]}
	]}`
	doJSON(service, http.MethodPost, "/api/bookmarks/import-native",
		`{"payload":`+payload+`,"mode":"replace"}`)

	_, check := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := check["groups"].([]interface{})
	if len(groups) != 1 || groups[0].(map[string]interface{})["title"] != "新分组" {
		t.Fatalf("replace should leave only the imported group, got %v", groups)
	}
}

// dry_run 不写库。
func TestNativeImportDryRunDoesNotWrite(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"G","items":[{"title":"X","url":"https://x.com"}]}
	]}`
	_, rep := doJSON(service, http.MethodPost, "/api/bookmarks/import-native",
		`{"payload":`+payload+`,"dry_run":true}`)
	r := rep["data"].(map[string]interface{})
	if r["dry_run"] != true || r["groups_created"].(float64) != 1 {
		t.Fatalf("dry-run report wrong: %v", r)
	}

	_, check := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	if groups := check["groups"].([]interface{}); len(groups) != 0 {
		t.Errorf("dry-run must not write, got %d groups", len(groups))
	}
}

// 非原生格式必须被拒绝（避免把 SunPanel 的文件误当原生格式导入）。
func TestNativeImportRejectsForeignFormat(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	cases := []string{
		`{"payload":{"icons":[]}}`,                                                   // SunPanel 结构
		`{"payload":{"format":"other","version":1,"groups":[]}}`,                     // 格式名不对
		`{"payload":{"format":"` + exportFormatName + `"}}`,                          // 缺 groups
		`{"payload":"not json at all"}`,                                              // 非 JSON
		`{"payload":{"format":"` + exportFormatName + `","version":99,"groups":[]}}`, // 版本过高
	}
	for _, body := range cases {
		rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/import-native", body)
		if rec.Code == http.StatusOK {
			t.Errorf("body %s should be rejected", body)
		}
	}
}

// 无名称的分组、缺标题/网址的条目要跳过并给出警告，而不是整批失败。
func TestNativeImportSkipsInvalidEntries(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"","items":[{"title":"X","url":"https://x.com"}]},
		{"title":"有效分组","items":[
			{"title":"","url":"https://a.com"},
			{"title":"无网址","url":""},
			{"title":"正常","url":"https://ok.com"}
		]}
	]}`
	_, rep := doJSON(service, http.MethodPost, "/api/bookmarks/import-native", `{"payload":`+payload+`}`)
	r := rep["data"].(map[string]interface{})
	if r["groups_created"].(float64) != 1 {
		t.Errorf("nameless group should be skipped, got %v", r["groups_created"])
	}
	if r["items_imported"].(float64) != 1 {
		t.Errorf("only the valid item should be imported, got %v", r["items_imported"])
	}
	if r["items_skipped"].(float64) != 2 {
		t.Errorf("two invalid items should be skipped, got %v", r["items_skipped"])
	}
	if _, ok := r["warnings"]; !ok {
		t.Errorf("expected a warning for the nameless group, got %v", r)
	}
}

// slug 冲突时清空并告警，而不是让整批导入失败（slug 有唯一约束）。
func TestNativeImportHandlesSlugConflict(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"占用者","slug":"taken","public":true}`)

	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"新分组","slug":"taken","items":[]}
	]}`
	rec, rep := doJSON(service, http.MethodPost, "/api/bookmarks/import-native", `{"payload":`+payload+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("slug conflict must not fail the whole import, got %d", rec.Code)
	}
	r := rep["data"].(map[string]interface{})
	if r["groups_created"].(float64) != 1 {
		t.Errorf("group should still be created, got %v", r)
	}
	if _, ok := r["warnings"]; !ok {
		t.Errorf("expected a warning about the slug conflict, got %v", r)
	}
}

// merge 进已有分组时，新网址必须排在原有条目之后。
// 直接用组内序号会与已有 sort_order 撞车，排序（sort_order, id）就变成新旧交错。
func TestNativeImportMergeAppendsAfterExistingItems(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"已有分组"}`)
	_, gp := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	gid := int64Value(gp["groups"].([]interface{})[0].(map[string]interface{})["id"])
	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(gid)+`,"title":"原有","url":"https://old.example"}`)

	payload := `{"format":"` + exportFormatName + `","version":1,"groups":[
		{"title":"已有分组","items":[
			{"title":"新增甲","url":"https://new-a.example"},
			{"title":"新增乙","url":"https://new-b.example"}
		]}
	]}`
	if rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/import-native", `{"payload":`+payload+`}`); rec.Code != http.StatusOK {
		t.Fatalf("merge import failed: %d", rec.Code)
	}

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/items?group_id="+itoa(gid), "")
	items, _ := listing["items"].([]interface{})
	var order []string
	for _, it := range items {
		order = append(order, it.(map[string]interface{})["title"].(string))
	}
	want := []string{"原有", "新增甲", "新增乙"}
	if len(order) != len(want) {
		t.Fatalf("unexpected item count: %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("merged items must be appended, got %v want %v", order, want)
		}
	}
}
