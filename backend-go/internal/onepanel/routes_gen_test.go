package onepanel

import (
	"net/http"
	"strings"
	"testing"
)

// 快捷路由表与 ServeHTTP 的实际分发必须保持一致：
// quickRoutes 里声明的每种方法都要能被分发到，且不能出现「表里有、实际 404」。
// 这条测试是「文档说有、调用 404」那类问题的回归防线。
func TestQuickRoutesDispatchable(t *testing.T) {
	service := newTestService(t)
	runner := &recordingRunner{}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	for _, route := range quickRoutes {
		for _, method := range route.Methods {
			// config 的 DELETE 会清掉连接配置，导致后续 proxy 路由全部 404。
			// 每次调用前用 POST upsert 重建，保证被测的是路由分发而不是配置状态。
			perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"ks-1"}`)

			// 用占位符填出具体路径；serverId 固定为已注册的 srv-arm。
			path := strings.ReplaceAll(route.Prefix, "{serverId}", "srv-arm")
			path = strings.ReplaceAll(path, "{id}", "999999")
			path = strings.ReplaceAll(path, "{appInstallId}", "999999")
			path = strings.ReplaceAll(path, "{name}", "probe-container")

			res := perform(service, method, path, "{}")
			if res.Code == http.StatusNotFound {
				t.Errorf("%s %s 在路由表里声明了，但分发返回 404", method, path)
			}
			if res.Code == http.StatusMethodNotAllowed {
				t.Errorf("%s %s 在路由表里声明了，但分发返回 405", method, path)
			}
		}
	}
}

// 未声明的方法必须被显式拒绝为 405（而不是静默走错分支），
// 这样 AI 按错方法调用时能拿到准确信号。
func TestQuickRoutesRejectUndeclaredMethod(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRunner(&recordingRunner{})
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"k"}`)

	cases := []struct{ method, path string }{
		{http.MethodDelete, "/api/onepanel/srv-arm/websites"},
		{http.MethodPut, "/api/onepanel/srv-arm/overview"},
		{http.MethodGet, "/api/onepanel/srv-arm/proxy"},
	}
	for _, tc := range cases {
		res := perform(service, tc.method, tc.path, "")
		if res.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: want 405, got %d body=%s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

// 每个代理路由的上游都必须真实存在，且方法映射自洽。
func TestQuickRouteUpstreamsCoverEveryMethod(t *testing.T) {
	for _, route := range quickRoutes {
		if route.Kind != "proxy" {
			continue
		}
		declared := map[string]bool{}
		for _, up := range route.Upstreams {
			declared[up.Method] = true
			if up.Upstream == "" {
				t.Errorf("%s: upstream 路径为空", route.Prefix)
			}
			// 上游路径里的占位符必须来自对外 prefix，否则会被留在 URL 里变成字面量。
			for _, seg := range strings.Split(up.Upstream, "/") {
				if !strings.HasPrefix(seg, "{") {
					continue
				}
				name := strings.Trim(seg, "{}")
				if !strings.Contains(route.Prefix, "{"+name+"}") {
					t.Errorf("%s: 上游占位符 {%s} 在对外 prefix 中不存在", route.Prefix, name)
				}
			}
		}
		for _, m := range route.Methods {
			if !declared[m] {
				t.Errorf("%s: 声明了方法 %s 但没有对应 upstream", route.Prefix, m)
			}
		}
	}
}
