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

// 走真实 ServeHTTP 路由（而非直接调用内部函数），验证导入端点端到端可用。
func TestImportEndpointEndToEndRouting(t *testing.T) {
	raw, err := os.ReadFile(sunPanelFixture)
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// GET /api/bookmarks/import 应返回选项元信息
	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/import", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import info status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if data["format"] != "sun-panel" {
		t.Errorf("expected sun-panel format, got %v", data["format"])
	}
	if len(data["notes"].([]interface{})) == 0 {
		t.Errorf("expected documented notes")
	}

	// POST dry-run
	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "dry_run": true})
	rec, payload = doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run status=%d body=%s", rec.Code, rec.Body.String())
	}

	// POST 真实导入
	body, _ = json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	rec, payload = doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	report := payload["data"].(map[string]interface{})["report"].(map[string]interface{})
	if report["items_imported"].(float64) != 46 {
		t.Errorf("expected 46 imported, got %v", report["items_imported"])
	}

	// 公开列表能看到新分组
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// 每个导入分组都应拥有可用的 slug（非空、可被公开路由解析）
	for _, g := range groups {
		m := g.(map[string]interface{})
		slug := m["slug"].(string)
		if slug == "" {
			t.Errorf("group %q has empty slug", m["title"])
			continue
		}
		// 即使未公开，也应因 public=0 返回 404 而不是因 slug 无效而崩
		rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/"+slug, "")
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
			t.Errorf("public lookup for slug %q returned %d", slug, rec.Code)
		}
	}
}

// 导入的条目在 UI 语义下必须可渲染：icon_type 与配套字段一致。
func TestImportedItemsRenderConsistently(t *testing.T) {
	raw, err := os.ReadFile(sunPanelFixture)
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/items", "")
	items := listing["items"].([]interface{})
	if len(items) == 0 {
		t.Fatal("no items imported")
	}

	byType := map[int]int{}
	for _, it := range items {
		m := it.(map[string]interface{})
		iconType := int(m["icon_type"].(float64))
		src := m["icon_src"].(string)
		text := m["icon_text"].(string)
		byType[iconType]++

		switch iconType {
		case 2:
			// 图片类型必须有可加载的地址（http/https 或 data:）
			if src == "" {
				t.Errorf("icon_type=2 但没有 icon_src: %v", m["title"])
			}
			if strings.HasPrefix(src, "/uploads/") {
				t.Errorf("icon_type=2 指向不可加载的本地路径: %v -> %q", m["title"], src)
			}
		case 1:
			// 文字类型必须有内容，且不能是图标集名
			if text == "" {
				t.Errorf("icon_type=1 但没有 icon_text: %v", m["title"])
			}
			if strings.Contains(text, ":") {
				t.Errorf("icon_type=1 的 icon_text 疑似图标集名: %v -> %q", m["title"], text)
			}
		}

		// 背景色必须可被 CSS 解析
		if bg := m["icon_bg_color"].(string); bg != "" && !strings.HasPrefix(bg, "#") {
			t.Errorf("icon_bg_color 不是合法颜色: %v -> %q", m["title"], bg)
		}
		// open_method 只能是 1 或 2
		if om := int(m["open_method"].(float64)); om != 1 && om != 2 {
			t.Errorf("open_method 越界: %v -> %d", m["title"], om)
		}
	}
	t.Logf("icon_type distribution: %v", byType)

	// 降级项应占多数（33 个本地图标 + 6 个图标集名）
	if byType[1] == 0 {
		t.Errorf("expected text-icon downgrades, got %v", byType)
	}
	if byType[2]+byType[1] != len(items) {
		t.Errorf("unexpected icon types: %v (total %d)", byType, len(items))
	}
}

// 导入后排序必须连续、可预测（消除 SunPanel 的 9999 与重复 sort）。
func TestImportedSortIsContiguous(t *testing.T) {
	raw, err := os.ReadFile(sunPanelFixture)
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	body, _ := json.Marshal(map[string]interface{}{"payload": string(raw), "group_mode": "new"})
	doJSON(service, http.MethodPost, "/api/bookmarks/import", string(body))

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	for _, g := range listing["groups"].([]interface{}) {
		m := g.(map[string]interface{})
		items := m["items"].([]interface{})
		for idx, it := range items {
			got := int(it.(map[string]interface{})["sort_order"].(float64))
			if got != idx {
				t.Errorf("group %q item %d has sort_order %d, want %d", m["title"], idx, got, idx)
			}
		}
	}
	// 分组排序也应连续（0,1 而不是 0,9999）
	for idx, g := range listing["groups"].([]interface{}) {
		got := int(g.(map[string]interface{})["sort_order"].(float64))
		if got != idx {
			t.Errorf("group %d has sort_order %d, want %d", idx, got, idx)
		}
	}
	t.Log("imported sort values are re-numbered contiguously")
}

func Example_importEndpoint() {
	fmt.Println("POST /api/bookmarks/import with {\"payload\": <sun-panel.json>, \"dry_run\": true} first")
	// Output: POST /api/bookmarks/import with {"payload": <sun-panel.json>, "dry_run": true} first
}
