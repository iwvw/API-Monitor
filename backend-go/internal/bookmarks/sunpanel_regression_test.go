package bookmarks

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 这些测试固化此前「模拟导入」中发现的缺陷，防止回归。
// 每条用例都对应一次真实复现。

// 缺陷 S4：只改标题的 PUT 曾清空 slug/domain/public/config，导致公开页 404。
func TestRegressionPartialGroupUpdatePreservesFields(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
		`{"title":"Public Tools","public":true,"slug":"public-tools","domain":"nav.example.com","cache_seconds":120,"config":{"publicIconId":"icon-123"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	gid := int64Value(payload["group"].(map[string]interface{})["id"])

	// 只发 title
	rec, _ = doJSON(service, http.MethodPut, "/api/bookmarks/groups/"+itoa(gid), `{"title":"Renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	_, payload = doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	after := payload["groups"].([]interface{})[0].(map[string]interface{})
	if after["title"] != "Renamed" {
		t.Errorf("title not updated: %v", after["title"])
	}
	if after["slug"] != "public-tools" {
		t.Errorf("slug must be preserved, got %q", after["slug"])
	}
	if after["domain"] != "nav.example.com" {
		t.Errorf("domain must be preserved, got %q", after["domain"])
	}
	if after["public"] != true {
		t.Errorf("public must be preserved, got %v", after["public"])
	}
	if after["cache_seconds"].(float64) != 120 {
		t.Errorf("cache_seconds must be preserved, got %v", after["cache_seconds"])
	}
	cfg := after["config"].(map[string]interface{})
	if cfg["publicIconId"] != "icon-123" {
		t.Errorf("config must be preserved, got %v", cfg)
	}

	// 公开页仍可访问
	rec, _ = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/public-tools", "")
	if rec.Code != http.StatusOK {
		t.Errorf("public page must stay reachable, got %d", rec.Code)
	}

	// 显式取消公开仍应生效
	doJSON(service, http.MethodPut, "/api/bookmarks/groups/"+itoa(gid), `{"title":"Renamed","public":false}`)
	rec, _ = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/public-tools", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("explicit public=false must unpublish, got %d", rec.Code)
	}
}

// 缺陷 S3：中文标题塌缩为空 -> bookmarks / bookmarks-N。
func TestRegressionSlugSurvivesCJK(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	slugs := map[string]string{}
	for _, title := range []string{"APP", "网站", "常用工具", "我的导航"} {
		_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
			fmt.Sprintf(`{"title":%q}`, title))
		g := payload["group"].(map[string]interface{})
		slugs[title] = g["slug"].(string)
	}

	seen := map[string]string{}
	for title, slug := range slugs {
		if slug == "" || slug == "bookmarks" {
			t.Errorf("title %q produced degenerate slug %q", title, slug)
		}
		if prev, dup := seen[slug]; dup {
			t.Errorf("slug collision: %q and %q both produced %q", prev, title, slug)
		}
		seen[slug] = title
	}

	// 同一标题必须稳定得到同一 slug（可重现性）
	_, p1 := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站"}`)
	s1 := p1["group"].(map[string]interface{})["slug"].(string)
	if s1 != slugs["网站"]+"-2" {
		t.Errorf("expected deterministic suffix, got %q (base %q)", s1, slugs["网站"])
	}
	t.Logf("slugs: %v", slugs)
}

// 缺陷：createGroup 丢弃 sort_order，导入后分组顺序全为 0。
func TestRegressionCreateGroupPersistsSortOrder(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站","sort_order":9999}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"APP","sort_order":0}`)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := payload["groups"].([]interface{})
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	first := groups[0].(map[string]interface{})
	second := groups[1].(map[string]interface{})
	if first["title"] != "APP" || first["sort_order"].(float64) != 0 {
		t.Errorf("expected APP first with sort 0, got %v/%v", first["title"], first["sort_order"])
	}
	if second["title"] != "网站" || second["sort_order"].(float64) != 9999 {
		t.Errorf("expected 网站 second with sort 9999, got %v/%v", second["title"], second["sort_order"])
	}
}

// 缺陷 S10：排序接口对不存在的 id / group_id=0 静默返回成功。
func TestRegressionSortReportsMissingAndRejectsBadGroup(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 不存在的分组 id：如实回报 missing
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups/sort", `{"items":[{"id":999999,"sort":3}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sort status=%d", rec.Code)
	}
	data := payload["data"].(map[string]interface{})
	missing, ok := data["missing"].([]interface{})
	if !ok || len(missing) != 1 {
		t.Errorf("expected 1 missing id reported, got %v", data["missing"])
	}

	// group_id=0 必须被拒绝，而不是假装成功
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/items/sort", `{"group_id":0,"items":[{"id":1,"sort":5}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("group_id=0 must be 400, got %d", rec.Code)
	}

	// 负排序值必须被拒绝
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/groups/sort", `{"items":[{"id":1,"sort":-1}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("negative sort must be 400, got %d", rec.Code)
	}
}

// 回归：slug 唯一索引在迁移库上必须存在（CREATE TABLE 的 UNIQUE 不会被 ALTER 继承）。
func TestRegressionSlugUniqueIndexExists(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	ctx := t.Context()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var name string
	err = db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_bookmark_groups_slug_unique'`).Scan(&name)
	if err != nil {
		t.Fatalf("unique slug index missing: %v", err)
	}

	// 唯一性真的生效
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"a","slug":"dup"}`)
	rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"b","slug":"dup"}`)
	if rec.Code == http.StatusCreated {
		slugB := ""
		_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
		for _, g := range payload["groups"].([]interface{}) {
			m := g.(map[string]interface{})
			if m["title"] == "b" {
				slugB = m["slug"].(string)
			}
		}
		if slugB == "dup" {
			t.Errorf("duplicate slug 'dup' was accepted twice despite unique index")
		}
	}
	t.Logf("second create with duplicate slug -> status=%d (index present: %s)", rec.Code, name)
}
