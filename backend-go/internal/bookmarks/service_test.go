package bookmarks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

func doJSON(service *Service, method, path, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec, payload
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestGroupAndItemCRUD(t *testing.T) {
	service := newTestService(t)

	// 创建分组
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"常用工具","description":"日常入口"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", rec.Code, rec.Body.String())
	}
	groupID := int64(payload["group"].(map[string]interface{})["id"].(float64))

	// 创建网址
	rec, payload = doJSON(service, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(groupID)+`,"title":"GitHub","url":"https://github.com","open_method":2,"icon_type":2,"icon_src":"/x.png"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item status=%d body=%s", rec.Code, rec.Body.String())
	}
	itemID := int64(payload["item"].(map[string]interface{})["id"].(float64))

	// 列表嵌套返回
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	groups := payload["groups"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	items := groups[0].(map[string]interface{})["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 item nested, got %d", len(items))
	}

	// 更新网址
	rec, payload = doJSON(service, http.MethodPut, "/api/bookmarks/items/"+itoa(itemID), `{"title":"GitHub 更新","url":"https://github.com/iwvw","open_method":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update item status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 排序
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/groups/sort", `{"items":[{"id":`+itoa(groupID)+`,"sort":5}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("group sort status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/items/sort", `{"group_id":`+itoa(groupID)+`,"items":[{"id":`+itoa(itemID)+`,"sort":1}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("item sort status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 删除分组应级联删除网址
	rec, _ = doJSON(service, http.MethodDelete, "/api/bookmarks/groups/"+itoa(groupID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete group status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	if len(payload["groups"].([]interface{})) != 0 {
		t.Fatalf("expected empty groups after delete, body=%s", rec.Body.String())
	}
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/items", "")
	if len(payload["items"].([]interface{})) != 0 {
		t.Fatalf("expected cascade delete items, body=%s", rec.Body.String())
	}
}

func TestItemValidation(t *testing.T) {
	service := newTestService(t)

	// 缺少标题
	rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/items", `{"group_id":1,"url":"https://a.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing title should be 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 不存在分组
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/items", `{"group_id":999,"title":"x","url":"https://a.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing group should be 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicGroupPage(t *testing.T) {
	service := newTestService(t)

	// 公开分组
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"Public Tools","public":true,"slug":"public-tools","domain":"nav.example.com","cache_seconds":120}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create public group status=%d body=%s", rec.Code, rec.Body.String())
	}
	groupID := int64(payload["group"].(map[string]interface{})["id"].(float64))
	slug := payload["group"].(map[string]interface{})["slug"].(string)
	if slug != "public-tools" {
		t.Fatalf("expected slug public-tools, got %q", slug)
	}

	// 非公开分组
	rec, payload = doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"私有工具","slug":"private-tools"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create private group status=%d body=%s", rec.Code, rec.Body.String())
	}
	privateID := int64(payload["group"].(map[string]interface{})["id"].(float64))

	// 公开组添加网址
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(groupID)+`,"title":"GitHub","url":"https://github.com","open_method":2}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 私有组添加网址（不应出现在公开页）
	rec, _ = doJSON(service, http.MethodPost, "/api/bookmarks/items",
		`{"group_id":`+itoa(privateID)+`,"title":"Secret","url":"https://secret.example"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create secret item status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 公开读取：按 slug
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/public-tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public group status=%d body=%s", rec.Code, rec.Body.String())
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("public group missing data envelope body=%s", rec.Body.String())
	}
	group := data["group"].(map[string]interface{})
	items := group["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 public item, got %d body=%s", len(items), rec.Body.String())
	}
	first := items[0].(map[string]interface{})
	if first["title"] != "GitHub" || first["url"] != "https://github.com" {
		t.Fatalf("unexpected public item %#v", first)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=120") {
		t.Fatalf("expected cache-control max-age=120, got %q", rec.Header().Get("Cache-Control"))
	}

	// 公开读取：按域名
	rec, payload = doJSON(service, http.MethodGet, "/api/bookmarks/public/page-by-domain?domain=nav.example.com", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public by domain status=%d body=%s", rec.Code, rec.Body.String())
	}
	if data, ok := payload["data"].(map[string]interface{}); !ok || data["found"] != true {
		t.Fatalf("public by domain found mismatch body=%s", rec.Body.String())
	}

	// 私有分组不应可公开访问
	rec, _ = doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/private-tools", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("private group should 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	// favicon 解析：公开分组可命中，私有分组不可
	iconID, found, err := service.PublicPageIconID(t.Context(), "public-tools", false)
	if err != nil || !found || iconID != "" {
		t.Fatalf("PublicPageIconID public-tools: id=%q found=%v err=%v", iconID, found, err)
	}
	if _, found, err := service.PublicPageIconID(t.Context(), "private-tools", false); err != nil || found {
		t.Fatalf("PublicPageIconID private-tools should not be found, found=%v err=%v", found, err)
	}
}
