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

	// 精简文档仍覆盖全部路径（路径数同源）
	var fullDoc, compactDoc struct {
		Paths map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(full, &fullDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(compact, &compactDoc); err != nil {
		t.Fatal(err)
	}
	if len(compactDoc.Paths) != len(fullDoc.Paths) {
		t.Fatalf("compact paths=%d full paths=%d", len(compactDoc.Paths), len(fullDoc.Paths))
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