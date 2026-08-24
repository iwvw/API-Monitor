package adminai

import (
	"context"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/system"
)

// catalogTestRoutes 是最小 api-docs 载荷：覆盖精确命中、{占位} 模板匹配与聚合前缀三类。
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
			"matchMode": "pattern", "detail": "列出或新增 DNS 记录",
		},
		map[string]interface{}{
			"prefix": "/api/cloudflare", "auth": "session",
			"responseMode": "json", "methods": []interface{}{"POST"},
			"matchMode": "prefix", "detail": "Cloudflare 管理总入口",
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

// 聚合前缀（matchMode=prefix 模块总入口）必须排除出清单与契约缓存：
// 否则模型会「有据可依」地尝试调用不存在的裸路径（审计实证：GET /api/scheduler 404）。
func TestCatalogExcludesAggregatePrefix(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(_ context.Context, req system.AICallRequest) (system.AICallResponse, error) {
		return system.AICallResponse{
			StatusCode: 200,
			Body:       map[string]interface{}{"routes": catalogTestRoutes()},
		}, nil
	}

	text := s.apiCatalogText(context.Background())
	if strings.Contains(text, "Cloudflare 管理总入口") || strings.Contains(text, "POST /api/cloudflare ——") {
		t.Fatalf("聚合前缀不应出现在清单:\n%s", text)
	}

	s.catalogMu.Lock()
	prefixDesc, inPrefixes := s.catalogPrefixes["/api/cloudflare"]
	s.catalogMu.Unlock()
	if !inPrefixes || prefixDesc == "" {
		t.Fatalf("聚合前缀应记录到 catalogPrefixes（供纠错提示）")
	}

	// get_route 命中聚合前缀 → 明确错误并给出子路由，不再返回「假契约」
	_, err := s.executeReadOnlyTool(context.Background(), "get_route", map[string]interface{}{"path": "/api/cloudflare"})
	if err == nil {
		t.Fatal("get_route 对聚合前缀应报错")
	}
	for _, want := range []string{"聚合前缀", "/api/cloudflare/templates"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误缺少 %q: %v", want, err)
		}
	}
}

// call_api 本地契约预检：清单外路径、聚合前缀、错误方法在发真实 HTTP 前拦截。
func TestValidateCallAPIPath(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(_ context.Context, req system.AICallRequest) (system.AICallResponse, error) {
		return system.AICallResponse{
			StatusCode: 200,
			Body:       map[string]interface{}{"routes": catalogTestRoutes()},
		}, nil
	}
	s.apiCatalogText(context.Background())

	// 清单内精确路径 + 正确方法 → 放行
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/templates"); !ok {
		t.Fatalf("应放行，got %q", hint)
	}
	// 模板路径真实 ID 替换 + 正确方法 → 放行
	if hint, ok := s.validateCallAPIPath("POST", "/api/cloudflare/accounts/acc1/zones/zone2/records"); !ok {
		t.Fatalf("模板路径应放行，got %q", hint)
	}
	// 聚合前缀 → 拦截并提示子路由
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare"); ok {
		t.Fatal("聚合前缀应拦截")
	} else {
		for _, want := range []string{"聚合前缀", "/api/cloudflare/templates"} {
			if !strings.Contains(hint, want) {
				t.Fatalf("聚合前缀拦截提示缺少 %q: %s", want, hint)
			}
		}
	}
	// 清单外臆造路径 → 拦截
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/nonexistent"); ok {
		t.Fatal("清单外路径应拦截")
	} else if !strings.Contains(hint, "不在可调用接口清单") {
		t.Fatalf("清单外拦截提示异常: %s", hint)
	}
	// 错误方法 → 拦截并给出真实可用方法
	if hint, ok := s.validateCallAPIPath("DELETE", "/api/cloudflare/templates"); ok {
		t.Fatal("错误方法应拦截")
	} else if !strings.Contains(hint, "GET/POST") {
		t.Fatalf("错误方法提示应含真实可用方法: %s", hint)
	}
	// 带 query 的真实路径 → 放行
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/templates?page=1"); !ok {
		t.Fatalf("带 query 的真实路径应放行，got %q", hint)
	}
}

// manifest 兜底：不在 JSON 清单但 manifest 真实存在的接口（ResponseProxy 等）
// 保持旧行为放行；manifest 也查不到的臆造路径才拦截。
func TestValidateCallAPIPathManifestFallback(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(_ context.Context, req system.AICallRequest) (system.AICallResponse, error) {
		return system.AICallResponse{
			StatusCode: 200,
			Body:       map[string]interface{}{"routes": catalogTestRoutes()},
		}, nil
	}
	s.apiCatalogText(context.Background())

	// R2 下载（ResponseProxy，不在 JSON 清单）→ manifest 兜底放行
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/accounts/a1/r2/buckets/b1/objects/k1/download"); !ok {
		t.Fatalf("manifest 存在的代理接口应放行，got %q", hint)
	}
	// 带 query 的 manifest 路径 → 放行（query 已在匹配前剥离）
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/accounts/a1/r2/buckets/b1/objects/k1/download?expires=1"); !ok {
		t.Fatalf("manifest 代理接口带 query 应放行，got %q", hint)
	}
	// manifest 不存在的臆造路径 → 拦截
	if hint, ok := s.validateCallAPIPath("GET", "/api/cloudflare/definitely/not/a/route"); ok {
		t.Fatal("manifest 之外的臆造路径应拦截")
	} else if !strings.Contains(hint, "不在可调用接口清单") {
		t.Fatalf("臆造路径拦截提示异常: %s", hint)
	}
}