package adminai

import (
	"context"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/system"
)

// catalogTestRoutes 是最小 api-docs 载荷：覆盖精确命中与 {占位} 模板匹配两类。
func catalogTestRoutes() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"prefix": "/api/cloudflare/templates", "auth": "session",
			"responseMode": "json", "methods": []interface{}{"GET", "POST"},
			"detail": "列出或新增 DNS 模板",
		},
		map[string]interface{}{
			"prefix": "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", "auth": "session",
			"responseMode": "json", "methods": []interface{}{"GET", "POST"},
			"detail": "列出或新增 DNS 记录",
		},
	}
}

func TestCatalogBuildAndLookup(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(_ context.Context, req system.AICallRequest) (system.AICallResponse, error) {
		return system.AICallResponse{
			StatusCode: 200,
			Body:       map[string]interface{}{"routes": catalogTestRoutes()},
		}, nil
	}

	text := s.apiCatalogText(context.Background())
	if text == "" {
		t.Fatal("清单构建失败（返回空）")
	}
	if !strings.Contains(text, "列出或新增 DNS 模板") {
		t.Fatalf("清单缺少 DNS 模板描述:\n%s", text)
	}

	// 精确命中
	if got := s.lookupCatalogDesc("/api/cloudflare/templates"); got != "列出或新增 DNS 模板" {
		t.Fatalf("精确命中失败: %q", got)
	}
	// 模板路径逐段匹配（{id} 通配）
	if got := s.lookupCatalogDesc("/api/cloudflare/accounts/acc1/zones/zone2/records"); got != "列出或新增 DNS 记录" {
		t.Fatalf("模板匹配失败: %q", got)
	}
	// 清单外路径不得误命中
	if got := s.lookupCatalogDesc("/api/cloudflare/nonexistent"); got != "" {
		t.Fatalf("应返回空: %q", got)
	}

	// toolDesc：清单命中返回中文描述；未命中不返回 方法+路径 兜底（语义视图不展示路径）
	if got := s.toolDesc("call_api", `{"method":"GET","path":"/api/cloudflare/templates"}`); got != "列出或新增 DNS 模板" {
		t.Fatalf("toolDesc 命中错误: %q", got)
	}
	if got := s.toolDesc("call_api", `{"method":"GET","path":"/api/cloudflare/nonexistent"}`); got != "" {
		t.Fatalf("toolDesc 兜底不应出现路径: %q", got)
	}
	if got := s.toolDesc("get_route", `{"path":"/api/cloudflare/templates"}`); got != "查询接口契约" {
		t.Fatalf("get_route 语义名错误: %q", got)
	}
}