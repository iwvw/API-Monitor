package system

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// TestOpenAPISummarySize 保证 MCP get_openapi 的精简文档远小于完整版，
// 避免 AI 上下文被 600KB 撑爆导致路径检索失效。
func TestOpenAPISummarySize(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db", Host: "127.0.0.1", Port: 0})
	req, _ := http.NewRequest(http.MethodGet, "/api/system/openapi.json", nil)

	full, _ := json.Marshal(s.openapiDocument(req))
	compact, _ := json.Marshal(s.openapiCompactDocument(req))
	fullBytes, compactBytes := len(full), len(compact)
	t.Logf("openapi full=%d bytes compact=%d bytes (%.1f%%)", fullBytes, compactBytes, 100.0*float64(compactBytes)/float64(fullBytes))
	if compactBytes > 64*1024 || compactBytes > fullBytes/6 {
		t.Errorf("compact openapi should stay readable (<=64KB and <=1/6 of full), got %d bytes (%.1f%%)", compactBytes, 100.0*float64(compactBytes)/float64(fullBytes))
	}

	// 精简文档覆盖全部路径（路径数同源）。
	// 例外：1Panel 路由是外部面板的镜像（随 1Panel 版本变动、条目多），
	// 有意不纳入紧凑文档以免挤占其它模块的上下文预算；它通过
	// list_apis(domain=onepanel) / get_route 按需检索。
	var fullDoc, compactDoc struct {
		Paths map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(full, &fullDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(compact, &compactDoc); err != nil {
		t.Fatal(err)
	}
	// 计数：compact 里保留的 1Panel 入口路由仍算已覆盖，其余 1Panel 路由计数为排除。
	excludedOnepanel := 0
	for p := range fullDoc.Paths {
		if strings.HasPrefix(p, "/api/onepanel/") {
			if _, ok := compactDoc.Paths[p]; !ok {
				excludedOnepanel++
			}
		}
	}
	if got, want := len(compactDoc.Paths)+excludedOnepanel, len(fullDoc.Paths); got != want {
		t.Fatalf("compact paths=%d (+%d onepanel excluded) full paths=%d", len(compactDoc.Paths), excludedOnepanel, len(fullDoc.Paths))
	}
	// 除 1Panel 外的每条路径都必须出现在紧凑文档里。
	for p := range fullDoc.Paths {
		if strings.HasPrefix(p, "/api/onepanel/") {
			continue
		}
		if _, ok := compactDoc.Paths[p]; !ok {
			t.Fatalf("compact document missing path %s", p)
		}
	}

	// 1Panel 入口路由必须保留，供 AI 先发现「有这个模块」。
	for _, p := range []string{"/api/onepanel/config", "/api/onepanel/spec", "/api/onepanel/probe"} {
		if _, ok := compactDoc.Paths[p]; !ok {
			t.Fatalf("compact document should keep onepanel entry route %s", p)
		}
	}
}

// TestInferRouteMethodsConservative 文档声明的方法必须保守（默认只读）：
// 无法推断的路由不得宣称全方法，否则 AI 会照文档调用而 405。
func TestInferRouteMethodsConservative(t *testing.T) {
	cases := []struct {
		route manifest.Route
		want  []string
	}{
		{manifest.Route{Prefix: "/health", Auth: manifest.AuthPublic, Description: "服务健康检查与版本状态", Owner: manifest.OwnerGo}, []string{"GET"}},
		{manifest.Route{Prefix: "/api/auth/login-options", MatchMode: manifest.MatchExact, Auth: manifest.AuthPublic, Description: "获取当前可用的登录方式（密码 / GitHub / 通行密钥）", Owner: manifest.OwnerGo}, []string{"GET"}},
		{manifest.Route{Prefix: "/api/admin-ai/cron/task-run", MatchMode: manifest.MatchExact, Description: "cron task run", Owner: manifest.OwnerGo}, []string{"POST"}},
		{manifest.Route{Prefix: "/api/xyz/unknown-op", MatchMode: manifest.MatchPattern, Description: "操作类型：refresh", Owner: manifest.OwnerGo}, []string{"POST"}},
		{manifest.Route{Prefix: "/api/xyz/unknown", MatchMode: manifest.MatchExact, Description: "未知接口", Owner: manifest.OwnerGo}, []string{"GET"}},
		{manifest.Route{Prefix: "/api/xyz/unknown-pattern", MatchMode: manifest.MatchPattern, Description: "未知接口", Owner: manifest.OwnerGo}, []string{"GET"}},
	}
	for _, tc := range cases {
		got := inferRouteMethods(tc.route)
		if len(got) != len(tc.want) || len(got) == 0 || got[0] != tc.want[0] {
			t.Errorf("inferRouteMethods(%s) = %v, want %v", tc.route.Prefix, got, tc.want)
			continue
		}
		for _, m := range tc.want {
			found := false
			for _, g := range got {
				if g == m {
					found = true
				}
			}
			if !found {
				t.Errorf("inferRouteMethods(%s) = %v, want contains %v", tc.route.Prefix, got, m)
			}
		}
	}

	// 兜底默认不得包含 4 个全方法
	fallback := inferRouteMethods(manifest.Route{Prefix: "/api/unknown-x", Description: "x", Owner: manifest.OwnerGo})
	if strings.Contains(strings.Join(fallback, ","), "POST") {
		t.Errorf("fallback must be GET-only, got %v", fallback)
	}
}