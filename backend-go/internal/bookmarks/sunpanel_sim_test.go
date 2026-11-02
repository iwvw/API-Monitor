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

// 本文件保留「朴素逐条导入」的对照实验：它证明若不走事务性导入端点，
// 会出现部分成功、无失败报告、语义错配等问题（即导入端点要解决的动机）。
// 生产路径见 sunpanel.go 与 sunpanel_import_test.go。

const simFixturePath = `C:\Users\DSUK\AppData\Local\Temp\CB_82YW9_OPENED_ITEMS.tmp\SunPanel-Data202609252205.sun-panel.json`

// TestSimulateNaiveImportFailsPartially 用逐条 POST /items 的朴素方式导入，
// 复现「无事务、无报告、系统卡片 400」的原始问题。
func TestSimulateNaiveImportFailsPartially(t *testing.T) {
	if _, err := os.Stat(simFixturePath); err != nil {
		t.Skipf("fixture not present: %v", err)
	}
	raw, err := os.ReadFile(simFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	export, err := parseSunPanelExport(raw)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	itemFailures := 0
	totalItems := 0
	for _, g := range export.Icons {
		_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
			fmt.Sprintf(`{"title":%q,"sort_order":%d}`, g.Title, g.Sort))
		gid := int64Value(payload["group"].(map[string]interface{})["id"])

		for _, it := range g.Children {
			totalItems++
			// 朴素映射：SunPanel 字段直接塞进 bookmarks 字段。
			body, _ := json.Marshal(map[string]interface{}{
				"group_id": gid, "title": it.Title, "url": it.URL,
				"description": it.Description, "icon_type": it.Icon.ItemType,
				"icon_src": it.Icon.Src, "icon_text": it.Icon.Text,
				"icon_bg_color": it.Icon.BackgroundColor,
				"open_method":   it.OpenMethod, "sort_order": it.Sort,
			})
			rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/items", string(body))
			if rec.Code != http.StatusCreated {
				itemFailures++
				t.Logf("naive import failure: title=%q cardType=%d status=%d", it.Title, it.CardType, rec.Code)
			}
		}
	}

	t.Logf("naive simulation: items=%d failures=%d", totalItems, itemFailures)
	if itemFailures == 0 {
		t.Errorf("expected the no-url system card to fail naive import")
	}

	// 结论：事务性导入端点应做到 0 失败，并把系统卡片记录为「跳过」。
	svc2 := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	rec, payload := doJSON(svc2, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("transactional import failed: %d %s", rec.Code, rec.Body.String())
	}
	report := payload["data"].(map[string]interface{})["report"].(map[string]interface{})
	if report["items_imported"].(float64)+report["items_skipped"].(float64) != float64(totalItems) {
		t.Errorf("import must account for every item: imported=%v skipped=%v total=%d",
			report["items_imported"], report["items_skipped"], totalItems)
	}
	t.Logf("transactional import: imported=%v skipped=%v (accounted for all %d)",
		report["items_imported"], report["items_skipped"], totalItems)
}

// TestNaiveImportStoresUnusableIcons 证明朴素导入会把 /uploads/ 相对路径
// 原样落库（浏览器必然 404），而事务性导入会降级为文字图标。
func TestNaiveImportStoresUnusableIcons(t *testing.T) {
	raw, err := os.ReadFile(simFixturePath)
	if err != nil {
		t.Skipf("fixture not present: %v", err)
	}

	// 朴素路径
	naive := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	export, _ := parseSunPanelExport(raw)
	_, gp := doJSON(naive, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站"}`)
	gid := int64Value(gp["group"].(map[string]interface{})["id"])
	naiveLocal := 0
	for _, g := range export.Icons {
		for _, it := range g.Children {
			if it.Icon.ItemType == 2 && isLocalIconSrc(it.Icon.Src) {
				body, _ := json.Marshal(map[string]interface{}{
					"group_id": gid, "title": it.Title, "url": it.URL,
					"icon_type": it.Icon.ItemType, "icon_src": it.Icon.Src,
				})
				doJSON(naive, http.MethodPost, "/api/bookmarks/items", string(body))
				naiveLocal++
			}
		}
	}
	_, listing := doJSON(naive, http.MethodGet, "/api/bookmarks/items?group_id="+itoa(gid), "")
	storedBad := 0
	for _, it := range listing["items"].([]interface{}) {
		if src, _ := it.(map[string]interface{})["icon_src"].(string); strings.HasPrefix(src, "/uploads/") {
			storedBad++
		}
	}
	t.Logf("naive: %d local-icon items -> %d stored with unusable /uploads/ paths", naiveLocal, storedBad)
	if storedBad == 0 {
		t.Errorf("expected naive import to store unloadable relative icon paths")
	}

	// 事务性路径：同样数据不应留下任何 /uploads/ 图标
	svc := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	doJSON(svc, http.MethodPost, "/api/bookmarks/import", string(body))
	_, all := doJSON(svc, http.MethodGet, "/api/bookmarks/items", "")
	fixedBad := 0
	for _, it := range all["items"].([]interface{}) {
		if src, _ := it.(map[string]interface{})["icon_src"].(string); strings.HasPrefix(src, "/uploads/") {
			fixedBad++
		}
	}
	if fixedBad != 0 {
		t.Errorf("transactional import must not store /uploads/ icons, found %d", fixedBad)
	}
	t.Log("transactional import converted all local-icon entries to text icons (0 unloadable)")
}

// TestNaiveImportIsNotAtomic 证明逐条导入中途失败会留下半成品。
func TestNaiveImportIsNotAtomic(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"存量分组"}`)
	gid := int64Value(payload["group"].(map[string]interface{})["id"])

	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		fmt.Sprintf(`{"group_id":%d,"title":"A","url":"https://a.com"}`, gid))
	recInvalid, _ := doJSON(service, http.MethodPost, "/api/bookmarks/items",
		fmt.Sprintf(`{"group_id":%d,"title":"B","url":""}`, gid))
	if recInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty url, got %d", recInvalid.Code)
	}
	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		fmt.Sprintf(`{"group_id":%d,"title":"C","url":"https://c.com"}`, gid))

	_, payload = doJSON(service, http.MethodGet, "/api/bookmarks/items?group_id="+itoa(gid), "")
	items := payload["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("expected partial success leaving 2 items, got %d", len(items))
	}
	t.Log("naive per-item import is non-atomic: 2/3 applied, no rollback, no report")
}
