package bookmarks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

const sunPanelFixture = `C:\Users\DSUK\AppData\Local\Temp\CB_82YW9_OPENED_ITEMS.tmp\SunPanel-Data202609252205.sun-panel.json`

func loadFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(sunPanelFixture)
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	return raw
}

// 用真实导出文件走一遍完整导入：单事务、全量入库、逐项跳过原因可查。
func TestImportSunPanelRealFixture(t *testing.T) {
	raw := loadFixture(t)
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	body := map[string]interface{}{"payload": string(raw), "group_mode": "new"}
	encoded, _ := json.Marshal(body)
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(encoded))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	report := payload["data"].(map[string]interface{})["report"].(map[string]interface{})
	t.Logf("report: created=%v used=%v imported=%v skipped=%v",
		report["groups_created"], report["groups_used"], report["items_imported"], report["items_skipped"])

	if report["groups_created"].(float64) != 2 {
		t.Errorf("expected 2 groups, got %v", report["groups_created"])
	}
	// 47 条中 1 条是无网址系统卡片 -> 46 条入库、1 条跳过
	if report["items_imported"].(float64) != 46 {
		t.Errorf("expected 46 items imported, got %v", report["items_imported"])
	}
	if report["items_skipped"].(float64) != 1 {
		t.Errorf("expected 1 skipped (system card), got %v", report["items_skipped"])
	}

	// 校验跳过原因是「系统卡片」
	skipped := report["skipped"].([]interface{})
	if len(skipped) == 0 {
		t.Fatalf("expected a skip record")
	}
	first := skipped[0].(map[string]interface{})
	if !strings.Contains(first["reason"].(string), "系统卡片") {
		t.Errorf("unexpected skip reason: %v", first["reason"])
	}

	// 落库校验
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	total := 0
	for _, g := range groups {
		m := g.(map[string]interface{})
		items := m["items"].([]interface{})
		total += len(items)
		t.Logf("group %q slug=%q sort=%v items=%d", m["title"], m["slug"], m["sort_order"], len(items))
	}
	if total != 46 {
		t.Errorf("expected 46 items in DB, got %d", total)
	}
}

// 导入必须是原子的：非法 payload 不得留下任何半成品分组。
func TestImportSunPanelIsAtomic(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 先塞一个不合法分组（single 模式目标不存在）触发事务回滚
	payload := `{"icons":[{"title":"网站","sort":1,"children":[
	  {"title":"A","url":"https://a.com","sort":1,"icon":{"itemType":2,"src":"https://a.com/i.png"}},
	  {"title":"B","url":"https://b.com","sort":2,"icon":{"itemType":2,"src":""}}
	]}]}`
	body, _ := json.Marshal(map[string]interface{}{
		"payload": payload, "group_mode": "single", "target_group_id": 12345,
	})
	rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code == http.StatusOK {
		t.Fatalf("expected failure for missing target group, got 200")
	}

	// 事务回滚：不应有任何分组或条目残留
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	if groups := listing["groups"].([]interface{}); len(groups) != 0 {
		t.Errorf("rollback failed: %d groups left behind", len(groups))
	}
	_, items := doJSON(service, http.MethodGet, "/api/bookmarks/items", "")
	if list := items["items"].([]interface{}); len(list) != 0 {
		t.Errorf("rollback failed: %d items left behind", len(list))
	}
	t.Log("atomic import verified: failed import left zero rows")
}

// dry-run 不得写库，且要如实预告结果。
func TestImportSunPanelDryRun(t *testing.T) {
	raw := loadFixture(t)
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "dry_run": true})
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if data["dry_run"] != true {
		t.Errorf("expected dry_run flag")
	}
	report := data["report"].(map[string]interface{})
	if report["items_imported"].(float64) != 46 {
		t.Errorf("dry-run should preview 46 items, got %v", report["items_imported"])
	}

	// 库里必须还是空的
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	if groups := listing["groups"].([]interface{}); len(groups) != 0 {
		t.Errorf("dry-run must not write, found %d groups", len(groups))
	}
}

// SunPanel 语义映射：图标集名不能被当 Emoji 渲染。
func TestMapItemIconSemantics(t *testing.T) {
	opts := defaultImportOptions()

	// itemType=3 + 图标集名 -> 降级为文字首字（不是 icon_type=3 整串）
	iconSet := sunPanelItem{Title: "Gemini", URL: "https://gemini.google.com/app", Icon: struct {
		ItemType        int    `json:"itemType"`
		Src             string `json:"src"`
		Text            string `json:"text"`
		BackgroundColor string `json:"backgroundColor"`
	}{ItemType: 3, Text: "vscode-icons:file-type-gemini"}}

	_, _, _, iconType, _, iconText, _, _, ok, _ := mapItem(iconSet, opts)
	if !ok {
		t.Fatal("expected item to import")
	}
	if iconType != 1 {
		t.Errorf("icon-set name should downgrade to icon_type=1, got %d", iconType)
	}
	if iconText != "G" {
		t.Errorf("expected first letter of title, got %q", iconText)
	}
	if strings.Contains(iconText, ":") {
		t.Errorf("icon-set name leaked into icon_text: %q", iconText)
	}

	// /uploads/ 相对路径 -> 降级为文字图标
	local := sunPanelItem{Title: "网盘", URL: "https://pan.dsuk.top", Icon: struct {
		ItemType        int    `json:"itemType"`
		Src             string `json:"src"`
		Text            string `json:"text"`
		BackgroundColor string `json:"backgroundColor"`
	}{ItemType: 2, Src: "/uploads/2025/11/14/abc.svg"}}
	_, _, _, iconType2, iconSrc2, iconText2, _, _, ok2, reason := mapItem(local, opts)
	if !ok2 {
		t.Fatal("expected local-icon item to import")
	}
	if iconType2 != 1 || iconSrc2 != "" || iconText2 != "网" {
		t.Errorf("local icon should downgrade to text, got type=%d src=%q text=%q", iconType2, iconSrc2, iconText2)
	}
	if reason == "" {
		t.Errorf("expected a skip reason explaining the downgrade")
	}

	// 远端 https 图标保留
	remote := sunPanelItem{Title: "pan-service", URL: "https://salen.eu.org/", Icon: struct {
		ItemType        int    `json:"itemType"`
		Src             string `json:"src"`
		Text            string `json:"text"`
		BackgroundColor string `json:"backgroundColor"`
	}{ItemType: 2, Src: "https://cdn.example/i.png"}}
	_, _, _, iconType3, iconSrc3, _, _, _, ok3, _ := mapItem(remote, opts)
	if !ok3 || iconType3 != 2 || iconSrc3 != "https://cdn.example/i.png" {
		t.Errorf("remote icon must be preserved, got type=%d src=%q", iconType3, iconSrc3)
	}
}

// open_method 与背景色规范化。
func TestMapItemNormalization(t *testing.T) {
	opts := defaultImportOptions()

	mk := func(openMethod int, bg string) sunPanelItem {
		it := sunPanelItem{Title: "x", URL: "https://x.com", OpenMethod: openMethod}
		it.Icon.BackgroundColor = bg
		return it
	}
	// openMethod=0 视为新窗口
	if _, _, _, _, _, _, _, om, _, _ := mapItem(mk(0, ""), opts); om != 2 {
		t.Errorf("openMethod 0 should map to 2, got %d", om)
	}
	if _, _, _, _, _, _, _, om, _, _ := mapItem(mk(1, ""), opts); om != 1 {
		t.Errorf("openMethod 1 should stay 1, got %d", om)
	}
	// 无 # 的十六进制要补 #（否则 style.backgroundColor 被忽略）
	if _, _, _, _, _, _, bg, _, _, _ := mapItem(mk(2, "2a2a2a6b"), opts); bg != "#2a2a2a6b" {
		t.Errorf("expected #-prefixed color, got %q", bg)
	}
	// 非颜色值必须清空，而不是原样塞进 style
	if _, _, _, _, _, _, bg, _, _, _ := mapItem(mk(2, "rgba(1,2,3,.5)"), opts); bg != "" {
		t.Errorf("expected unparsable color to be dropped, got %q", bg)
	}
	// 非法链接（javascript:）必须被拒绝
	bad := sunPanelItem{Title: "evil", URL: "javascript:alert(1)"}
	if _, _, _, _, _, _, _, _, ok, reason := mapItem(bad, opts); ok {
		t.Errorf("javascript: URL must be rejected")
	} else {
		t.Logf("rejected as expected: %s", reason)
	}
}

// 合并模式：同名分组复用而不是重复创建。
func TestImportMergeMode(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 预置一个同名分组
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站"}`)

	payload := `{"icons":[{"title":"网站","sort":1,"children":[{"title":"A","url":"https://a.com","sort":1,"icon":{"itemType":2,"src":""}}]}]}`
	body, _ := json.Marshal(map[string]interface{}{"payload": payload, "group_mode": "merge"})
	rec, resp := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("merge import status=%d body=%s", rec.Code, rec.Body.String())
	}
	report := resp["data"].(map[string]interface{})["report"].(map[string]interface{})
	if report["groups_created"].(float64) != 0 || report["groups_used"].(float64) != 1 {
		t.Errorf("merge should reuse existing group, got created=%v used=%v", report["groups_created"], report["groups_used"])
	}

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after merge, got %d", len(groups))
	}
	items := groups[0].(map[string]interface{})["items"].([]interface{})
	if len(items) != 1 {
		t.Errorf("expected 1 merged item, got %d", len(items))
	}
}

// 重复导入同一文件不应产生重复分组（slug 唯一 + 计数校验）。
func TestImportTwiceDoesNotCollide(t *testing.T) {
	raw := loadFixture(t)
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	for i := 0; i < 2; i++ {
		rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("import #%d failed status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	if len(groups) != 4 {
		t.Errorf("expected 4 groups after two imports, got %d", len(groups))
	}
	seen := map[string]bool{}
	for _, g := range groups {
		slug := g.(map[string]interface{})["slug"].(string)
		if slug == "" {
			t.Errorf("import produced empty slug")
		}
		if seen[slug] {
			t.Errorf("duplicate slug produced: %q", slug)
		}
		seen[slug] = true
	}
	t.Logf("slugs after two imports: %v", seen)
}

func TestImportRejectsNonSunPanelPayload(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	for _, payload := range []string{`{"foo":1}`, `[1,2,3]`, `not json`} {
		body, _ := json.Marshal(map[string]interface{}{"payload": payload})
		rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("payload %q should be rejected with 400, got %d", payload, rec.Code)
		}
	}
}

func Example_importSunPanel() {
	fmt.Println("POST /api/bookmarks/import {\"payload\": <sun-panel.json>, \"dry_run\": true}")
	// Output: POST /api/bookmarks/import {"payload": <sun-panel.json>, "dry_run": true}
}
