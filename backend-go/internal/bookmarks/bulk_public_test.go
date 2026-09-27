package bookmarks

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 批量公开：一次把全部分组设为公开，并自动补齐缺失的 slug。
// 这正是用户「设了 APP 和网站两个分组，全部网址里却看不到 APP」的场景。
func TestBulkSetGroupsPublic(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 两个私有的中文分组，且没有 slug（默认就是空）
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"APP"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站"}`)

	// 公开前：聚合页为空
	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if total := int(payload["data"].(map[string]interface{})["total"].(float64)); total != 0 {
		t.Fatalf("expected 0 public groups before, got %d", total)
	}

	// 批量公开全部
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups/public", `{"public":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if int(data["updated"].(float64)) != 2 {
		t.Errorf("updated=%v want 2", data["updated"])
	}
	// 新建分组总是带 slug（createGroup 会 allocateSlug），所以这里没有需要补的；
	// slugs_fixed 是给历史库（slug 功能上线前的分组）兜底用的。
	if int(data["slugs_fixed"].(float64)) != 0 {
		t.Errorf("slugs_fixed=%v want 0 (new groups already have slugs)", data["slugs_fixed"])
	}

	// 公开后：聚合页应包含两个分组
	_, payload = doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	agg := payload["data"].(map[string]interface{})
	if int(agg["total"].(float64)) != 2 {
		t.Fatalf("expected 2 public groups after bulk publish, got %v", agg["total"])
	}
	titles := map[string]bool{}
	for _, g := range agg["groups"].([]interface{}) {
		m := g.(map[string]interface{})
		titles[m["title"].(string)] = true
		if m["slug"].(string) == "" {
			t.Errorf("group %q public but has no slug (would be unreachable)", m["title"])
		}
	}
	if !titles["APP"] || !titles["网站"] {
		t.Errorf("both groups should be public now, got %v", titles)
	}
	if int(agg["hidden_groups"].(float64)) != 0 {
		t.Errorf("hidden_groups=%v want 0", agg["hidden_groups"])
	}
}

// 批量取消公开：聚合页应重新变空，且 hidden_groups 反映未公开数量。
func TestBulkUnpublishAndHiddenCount(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"A"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"B"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups/public", `{"public":true}`)

	// 只取消其中一个
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	firstID := int64Value(groups[0].(map[string]interface{})["id"])

	rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/groups/public",
		fmt.Sprintf(`{"public":false,"ids":[%d]}`, firstID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	agg := payload["data"].(map[string]interface{})
	if int(agg["total"].(float64)) != 1 {
		t.Errorf("total=%v want 1", agg["total"])
	}
	// 关键：让公开页能说明「还有 1 个分组未公开」
	if int(agg["hidden_groups"].(float64)) != 1 {
		t.Errorf("hidden_groups=%v want 1 (public page should explain what is hidden)", agg["hidden_groups"])
	}
}

// 批量接口只按给定 ids 生效，不误伤其他分组。
func TestBulkSetPublicScopedToIDs(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"keep-private"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"make-public"}`)

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groups := listing["groups"].([]interface{})
	var targetID int64
	for _, g := range groups {
		m := g.(map[string]interface{})
		if m["title"] == "make-public" {
			targetID = int64Value(m["id"])
		}
	}

	doJSON(service, http.MethodPost, "/api/bookmarks/groups/public",
		fmt.Sprintf(`{"public":true,"ids":[%d]}`, targetID))

	_, aggPayload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	agg := aggPayload["data"].(map[string]interface{})
	if int(agg["total"].(float64)) != 1 {
		t.Fatalf("total=%v want 1", agg["total"])
	}
	title := agg["groups"].([]interface{})[0].(map[string]interface{})["title"].(string)
	if title != "make-public" {
		t.Errorf("wrong group published: %q", title)
	}
}

// 不存在的 id 要如实回报，而不是静默成功。
func TestBulkSetPublicReportsMissingIDs(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"real"}`)

	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups/public",
		`{"public":true,"ids":[1,999999]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	missing, ok := data["missing"].([]interface{})
	if !ok || len(missing) != 1 {
		t.Errorf("expected 1 missing id reported, got %v", data["missing"])
	}
}

// 批量公开后，每个分组都必须能通过自己的 slug 访问（slug 自动补齐的作用）。
func TestBulkPublishMakesGroupsReachable(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"APP"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"网站"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups/public", `{"public":true}`)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	for _, g := range payload["data"].(map[string]interface{})["groups"].([]interface{}) {
		m := g.(map[string]interface{})
		slug := m["slug"].(string)
		rec, got := doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/"+slug, "")
		if rec.Code != http.StatusOK {
			t.Errorf("group %q slug %q unreachable: %d", m["title"], slug, rec.Code)
			continue
		}
		gotTitle := got["data"].(map[string]interface{})["group"].(map[string]interface{})["title"].(string)
		if gotTitle != m["title"] {
			t.Errorf("slug %q resolved to %q, want %q", slug, gotTitle, m["title"])
		}
	}
}

// 保留 slug 在批量补齐时也不能被占用。
func TestBulkPublishAvoidsReservedSlug(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"all"}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups/public", `{"public":true}`)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	slug := payload["data"].(map[string]interface{})["groups"].([]interface{})[0].(map[string]interface{})["slug"].(string)
	if isReservedSlug(slug) {
		t.Errorf("auto-filled slug must not be reserved, got %q", slug)
	}
	// 聚合路由仍然可用
	rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Errorf("aggregate route broken after bulk publish: %d", rec.Code)
	}
}

// 历史库兜底：slug 为空的分组被公开时应自动补一个可访问的 slug。
// （新建分组总会带 slug，所以这条只能直接改库来构造。）
func TestBulkPublishBackfillsLegacyEmptySlug(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	ctx := t.Context()

	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"历史分组"}`)
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 模拟 slug 功能上线前创建的旧行
	if _, err := db.ExecContext(ctx, `UPDATE bookmark_groups SET slug = NULL WHERE title = '历史分组'`); err != nil {
		db.Close()
		t.Fatalf("simulate legacy row: %v", err)
	}
	db.Close()

	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups/public", `{"public":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if int(data["slugs_fixed"].(float64)) != 1 {
		t.Errorf("slugs_fixed=%v want 1 (legacy row needs a slug)", data["slugs_fixed"])
	}

	// 补齐后必须真的可访问
	_, aggPayload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	groups := aggPayload["data"].(map[string]interface{})["groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 public group, got %d", len(groups))
	}
	slug := groups[0].(map[string]interface{})["slug"].(string)
	if slug == "" {
		t.Fatalf("legacy group still has empty slug after publish")
	}
	rec, _ = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/"+slug, "")
	if rec.Code != http.StatusOK {
		t.Errorf("backfilled slug %q not reachable: %d", slug, rec.Code)
	}
	t.Logf("legacy group got slug %q", slug)
}
