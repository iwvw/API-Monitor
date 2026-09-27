package bookmarks

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 建几个分组 + 网址，返回公开/私有分组的 id。
func seedPublicGroups(t *testing.T, service *Service) (publicID, secondPublicID, privateID int64) {
	t.Helper()

	_, p1 := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
		`{"title":"公开甲","public":true,"slug":"alpha","sort_order":0,"cache_seconds":120}`)
	publicID = int64Value(p1["group"].(map[string]interface{})["id"])

	_, p2 := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
		`{"title":"公开乙","public":true,"slug":"beta","sort_order":1}`)
	secondPublicID = int64Value(p2["group"].(map[string]interface{})["id"])

	_, p3 := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"私有丙"}`)
	privateID = int64Value(p3["group"].(map[string]interface{})["id"])

	add := func(gid int64, title string) {
		rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/items",
			fmt.Sprintf(`{"group_id":%d,"title":%q,"url":"https://example.com/%s"}`, gid, title, title))
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed item %q status=%d body=%s", title, rec.Code, rec.Body.String())
		}
	}
	add(publicID, "甲一")
	add(publicID, "甲二")
	add(secondPublicID, "乙一")
	add(privateID, "丙一")
	return publicID, secondPublicID, privateID
}

// 聚合端点必须返回全部公开分组，且绝不泄漏私有分组。
func TestPublicAllGroupsAggregatesPublicOnly(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedPublicGroups(t, service)

	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	groups := data["groups"].([]interface{})

	if len(groups) != 2 {
		t.Fatalf("expected 2 public groups, got %d", len(groups))
	}
	if int(data["total"].(float64)) != 2 {
		t.Errorf("total=%v want 2", data["total"])
	}
	if int(data["total_items"].(float64)) != 3 {
		t.Errorf("total_items=%v want 3", data["total_items"])
	}

	titles := map[string]int{}
	for _, g := range groups {
		m := g.(map[string]interface{})
		title := m["title"].(string)
		titles[title] = len(m["items"].([]interface{}))
		if title == "私有丙" {
			t.Errorf("private group leaked into aggregate response")
		}
	}
	if titles["公开甲"] != 2 || titles["公开乙"] != 1 {
		t.Errorf("unexpected item counts: %v", titles)
	}
}

// 没有任何公开分组时返回空数组（而非 404），便于前端显示空状态。
func TestPublicAllGroupsEmpty(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"私有"}`)

	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty aggregate should be 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if len(data["groups"].([]interface{})) != 0 {
		t.Errorf("expected empty groups")
	}
}

// 缓存时长取最保守（最小）的公开分组值，避免刚改完看不到更新。
func TestPublicAllGroupsUsesConservativeCache(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"短缓存","public":true,"cache_seconds":60}`)
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"长缓存","public":true,"cache_seconds":3600}`)

	rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=60" {
		t.Errorf("expected conservative max-age=60, got %q", cc)
	}
}

// 排序：name / items / updated，升降序均可，非法值回退到默认。
func TestPublicAllGroupsSorting(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedPublicGroups(t, service)

	first := func(path string) string {
		rec, payload := doJSON(service, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, rec.Code)
		}
		groups := payload["data"].(map[string]interface{})["groups"].([]interface{})
		if len(groups) == 0 {
			return ""
		}
		return groups[0].(map[string]interface{})["title"].(string)
	}

	if got := first("/api/bookmarks/public/all/name-asc"); got != "公开乙" {
		t.Errorf("name-asc first=%q want 公开乙 (pinyin order not guaranteed, check locale)", got)
	}
	if got := first("/api/bookmarks/public/all/name-desc"); got != "公开甲" {
		t.Errorf("name-desc first=%q", got)
	}
	// 甲有 2 条、乙有 1 条
	if got := first("/api/bookmarks/public/all/items-desc"); got != "公开甲" {
		t.Errorf("items-desc first=%q want 公开甲", got)
	}
	if got := first("/api/bookmarks/public/all/items-asc"); got != "公开乙" {
		t.Errorf("items-asc first=%q want 公开乙", got)
	}
	// 非法描述回退到默认顺序（sort_order），不报错
	if got := first("/api/bookmarks/public/all/bogus-xyz"); got != "公开甲" {
		t.Errorf("bogus sort should fall back to default order, got %q", got)
	}
	// 默认与显式 order-asc 一致
	if first("/api/bookmarks/public/all") != first("/api/bookmarks/public/all/order-asc") {
		t.Errorf("default order should equal order-asc")
	}
}

// "all" 是保留 slug：用户不能创建同名 slug，否则聚合页被劫持。
func TestReservedSlugCannotBeAllocated(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	cases := []struct{ title, requested string }{
		{"全部", "all"},
		{"ALL 大写", "ALL"},
		{"带空格", "  all  "},
		{"index", "index"},
		{"public", "public"},
	}
	for _, tc := range cases {
		_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups",
			fmt.Sprintf(`{"title":%q,"slug":%q,"public":true}`, tc.title, tc.requested))
		group, ok := payload["group"].(map[string]interface{})
		if !ok {
			t.Fatalf("create %q failed: %v", tc.title, payload)
		}
		slug := group["slug"].(string)
		if isReservedSlug(slug) {
			t.Errorf("requested %q produced reserved slug %q", tc.requested, slug)
		}
		t.Logf("requested %-10q -> allocated %q", tc.requested, slug)
	}

	// 聚合端点仍应正常工作，说明没有被同名分组劫持
	rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Errorf("aggregate route must stay reachable, got %d", rec.Code)
	}

	// 标题为「all」时（未指定 slug）同样不能占用保留 slug
	_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"all","public":true}`)
	slug := payload["group"].(map[string]interface{})["slug"].(string)
	if isReservedSlug(slug) {
		t.Errorf("title-derived slug must also avoid reserved names, got %q", slug)
	}
	t.Logf("title \"all\" -> allocated %q", slug)
}

// 聚合端点必须对未认证访问开放，否则公开页无法加载。
func TestPublicAllGroupsIsUnauthenticated(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"公开","public":true}`)

	// ServeHTTP 的公开分支不经过会话校验，直接 GET 即应成功
	rec, _ := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Errorf("public aggregate must be reachable without auth, got %d", rec.Code)
	}
	// 写方法必须被拒绝（只有 GET 是公开路由）
	writeRec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/public/all", `{}`)
	if writeRec.Code == http.StatusOK {
		t.Errorf("POST to public aggregate must not succeed")
	}
}

// 聚合结果只包含公开分组的网址；私有分组的网址即使 id 相邻也不得混入。
func TestPublicAllGroupsExcludesPrivateItems(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, _, privateID := seedPublicGroups(t, service)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	groups := payload["data"].(map[string]interface{})["groups"].([]interface{})
	for _, g := range groups {
		for _, it := range g.(map[string]interface{})["items"].([]interface{}) {
			item := it.(map[string]interface{})
			if int64Value(item["group_id"]) == privateID || item["title"] == "丙一" {
				t.Errorf("private item leaked: %v", item["title"])
			}
		}
	}
}
