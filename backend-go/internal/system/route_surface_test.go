package system

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 新补的叶子路由必须进入 AI 目录（apiDocs），且 get_route 能按真实方法/查询参数命中，
// 不再回落到聚合前缀「不可直接调用」误导。
func TestRouteSurfaceLeafRoutesExposed(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	byPath := map[string]apiDocRoute{}
	for _, r := range service.apiDocs()["routes"].([]apiDocRoute) {
		byPath[r.Prefix] = r
	}

	cases := []struct {
		path       string
		wantMethod string
		wantQuery  []string
	}{
		{"/api/admin-ai/sessions/{id}/messages", "GET", []string{"cursor", "limit"}},
		{"/api/server/forward", "GET", []string{"limit", "server_id", "search"}},
		{"/api/server/forward/{id}/deploy", "POST", nil},
		{"/api/server/forward/preflight", "POST", nil},
		{"/api/server/forward/available-ports", "GET", []string{"server_id"}},
		{"/api/openai/analytics/logs", "GET", []string{"page", "pageSize", "status", "model"}},
		{"/api/openai/analytics/charts", "GET", []string{"granularity", "days"}},
		{"/api/scheduler/runs", "DELETE", []string{"all", "days"}},
		{"/api/cron/logs", "GET", []string{"task_id", "all", "days"}},
		{"/api/server/v2/docker/{serverId}/containers/{containerId}/logs", "GET", []string{"tail", "since"}},
	}
	for _, tc := range cases {
		doc, ok := byPath[tc.path]
		if !ok {
			t.Fatalf("route %s missing from AI catalog (apiDocs)", tc.path)
		}
		has := false
		for _, m := range doc.Methods {
			if m == tc.wantMethod {
				has = true
			}
		}
		if !has {
			t.Errorf("route %s methods = %v, want includes %s", tc.path, doc.Methods, tc.wantMethod)
		}
		if len(tc.wantQuery) > 0 {
			if len(doc.QueryParams) == 0 {
				t.Errorf("route %s has no queryParams, want %v", tc.path, tc.wantQuery)
				continue
			}
			names := map[string]bool{}
			for _, q := range doc.QueryParams {
				names[q.Name] = true
			}
			for _, want := range tc.wantQuery {
				if !names[want] {
					t.Errorf("route %s missing query param %q; have %v", tc.path, want, names)
				}
			}
		}
	}
}

// get_route 对具体叶子必须命中具体路由而非聚合前缀，返回正确方法与查询参数。
func TestGetRouteLeafResolvesConcrete(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	cases := []struct {
		path       string
		wantMethod string
	}{
		{"/api/admin-ai/sessions/aas_x/messages", "GET"},
		{"/api/server/forward/fwd1/deploy", "POST"},
		{"/api/server/forward/fwd1/status", "GET"},
		{"/api/server/forward/available-ports?server_id=h1", "GET"},
		{"/api/openai/analytics/logs?page=1&pageSize=20", "GET"},
		{"/api/scheduler/runs", "DELETE"},
	}
	for _, tc := range cases {
		raw, err := service.getRouteContract(map[string]interface{}{"path": tc.path})
		if err != nil {
			t.Fatalf("getRouteContract(%s) error: %v", tc.path, err)
		}
		contract := raw.(map[string]interface{})
		methods, _ := contract["methods"].([]string)
		has := false
		for _, m := range methods {
			if m == tc.wantMethod {
				has = true
			}
		}
		if !has {
			t.Errorf("get_route(%s) methods = %v, want includes %s", tc.path, methods, tc.wantMethod)
		}
		if agg, _ := contract["matchMode"].(string); agg == "prefix" {
			t.Errorf("get_route(%s) fell back to prefix mode, want concrete leaf", tc.path)
		}
	}
}

// 上下文预算断言（渐进式披露的真实边界）：
// 1) route-index 按 group 分片读取，任何单分组分片体积必须可控（≤ 32KB）；
// 2) 单条 get_route 契约体积必须小（≤ 8KB），AI 按需读取不会撑爆上下文。
func TestRouteIndexContextBudget(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	groups := map[string]bool{}
	for _, r := range service.apiDocs()["routes"].([]apiDocRoute) {
		groups[r.Group] = true
	}
	maxGroupBytes := 0
	for g := range groups {
		payload := service.routeIndexPayload(g)
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > maxGroupBytes {
			maxGroupBytes = len(raw)
		}
		if len(raw) > 32*1024 {
			t.Errorf("route-index group %q too large: %d bytes (limit 32KB)", g, len(raw))
		}
	}
	t.Logf("largest route-index group = %d bytes", maxGroupBytes)

	// 单条契约体积
	contract, err := service.getRouteContract(map[string]interface{}{"path": "/api/server/forward/{id}/deploy"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 8*1024 {
		t.Errorf("single get_route contract too large: %d bytes (limit 8KB)", len(raw))
	}
	t.Logf("single get_route contract = %d bytes", len(raw))
}

// 治理防线：聚合前缀（MatchPrefix 且以 /api/ 开头）不得成为叶子路由的唯一登记——
// 每个前缀下必须存在具体叶子，否则 AI 无法调用。抽查几个已知聚合前缀。
func TestAggregatePrefixesHaveLeafChildren(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	byPath := map[string]apiDocRoute{}
	for _, r := range service.apiDocs()["routes"].([]apiDocRoute) {
		byPath[r.Prefix] = r
	}
	prefixes := []string{"/api/admin-ai", "/api/server/forward", "/api/scheduler", "/api/cron", "/api/openai"}
	for _, p := range prefixes {
		leaf := false
		for path := range byPath {
			if strings.HasPrefix(path, p+"/") {
				leaf = true
				break
			}
		}
		if !leaf {
			t.Errorf("aggregate prefix %s has no concrete leaf routes in AI catalog", p)
		}
	}
}

// get_route 完全未命中时应返回相近候选（同前缀/同段数叶子），而非干巴巴的「路由不存在」。
func TestGetRouteNotFoundSuggestsNearby(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	cases := []string{
		"/api/nonexistent/xyz",       // 无任何前缀命中 → 404 + 邻近建议
		"/api/openai/analytics/foo",  // 前缀族存在但无叶子 → 聚合前缀提示（含子路由）
	}
	for _, p := range cases {
		_, err := service.getRouteContract(map[string]interface{}{"path": p})
		if err == nil {
			t.Errorf("getRouteContract(%s) unexpectedly succeeded, want not-found with suggestions", p)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "相近接口") && !strings.Contains(msg, "list_apis") && !strings.Contains(msg, "请改用其具体子路由") {
			t.Errorf("getRouteContract(%s) error lacks navigation hint: %s", p, msg)
		}
		t.Logf("not-found hint for %s: %s", p, msg)
	}
}
