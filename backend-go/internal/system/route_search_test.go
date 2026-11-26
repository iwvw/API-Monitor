package system

import (
	"os"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newSearchTestService(t *testing.T) *Service {
	t.Helper()
	dir, err := os.MkdirTemp("", "api_monitor_search_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	svc := New(config.Config{DataDir: dir, DBName: "data.db", Version: "test"})
	t.Cleanup(svc.Shutdown)
	return svc
}

// TestListAPIsSearchByBodyField 验证 list_apis 的 search 现在覆盖请求体字段语义:
// 按“备注”应能召回以 description 承载备注的证书更新接口。
func TestListAPIsSearchByBodyField(t *testing.T) {
	svc := newSearchTestService(t)

	res, err := svc.aiRouteCatalog(map[string]interface{}{"domain": "onepanel", "search": "备注", "limit": float64(100)})
	if err != nil {
		t.Fatalf("aiRouteCatalog: %v", err)
	}
	payload, _ := res.(map[string]interface{})
	routes, _ := payload["routes"].([]map[string]interface{})
	found := false
	for _, r := range routes {
		if r["path"] == "/api/onepanel/{serverId}/ssl/{id}/update" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list_apis search=备注 should surface the ssl update route, got %d routes", len(routes))
	}
}

// TestFindAPIsRecallsByBodyField 验证 find_api 的自然语言召回能把“备注”映射到
// 请求体字段描述(body-field),从而定位证书更新接口。
func TestFindAPIsRecallsByBodyField(t *testing.T) {
	svc := newSearchTestService(t)

	items := svc.apiDocs()["routes"].([]apiDocRoute)
	matches := findTopRoutes(items, "更新证书备注", 10)
	if len(matches) == 0 {
		t.Fatal("findTopRoutes returned no matches")
	}
	found := false
	for _, m := range matches {
		if m.Route.Prefix != "/api/onepanel/{serverId}/ssl/{id}/update" {
			continue
		}
		found = true
		hasBodyField := false
		for _, reason := range m.Reasons {
			if reason.Level == "body-field" {
				hasBodyField = true
			}
		}
		if !hasBodyField {
			t.Fatalf("ssl update matched without a body-field reason: %#v", m.Reasons)
		}
	}
	if !found {
		t.Fatalf("find_api should recall ssl update by body field, matches=%d", len(matches))
	}
}

// TestOnepanelIDPathParamSemantics 验证证书/网站接口的路径参数描述携带取 id 指引,
// 并带有“不要跨主机复用 id”的 note(Agent 最常见的踩坑点)。
func TestOnepanelIDPathParamSemantics(t *testing.T) {
	svc := newSearchTestService(t)
	items := svc.apiDocs()["routes"].([]apiDocRoute)

	sslOK, notesOK := false, false
	for _, item := range items {
		if item.Prefix != "/api/onepanel/{serverId}/ssl/{id}/update" {
			continue
		}
		for _, p := range item.PathParams {
			if p.Name == "id" && strings.Contains(p.Description, "/ssl/search") {
				sslOK = true
			}
		}
		for _, n := range item.Notes {
			if strings.Contains(n, "不要跨主机复用") {
				notesOK = true
			}
		}
	}
	if !sslOK {
		t.Fatal("ssl {id} path param should describe where to obtain the id")
	}
	if !notesOK {
		t.Fatal("ssl {id} route should carry the cross-host id note")
	}
}
