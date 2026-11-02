package system

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// module 被细分成大量子名（onepanel / onepanel-config / onepanel-spec …），
// AI 只能靠猜子名才能查全。domain 过滤必须把同域子模块一并返回。
func TestRouteDomainGroupsSubModules(t *testing.T) {
	cases := map[string]string{
		"onepanel":           "onepanel",
		"onepanel-config":    "onepanel",
		"onepanel-spec":      "onepanel",
		"cloudflare-dns":     "cloudflare",
		"cloudflare-workers": "cloudflare",
		"prompts-drafts":     "prompts",
		"server-agent-proxy": "server",
		"settings":           "settings",
		"":                   "",
	}
	for module, want := range cases {
		if got := routeDomain(module); got != want {
			t.Errorf("routeDomain(%q) = %q, want %q", module, got, want)
		}
	}
}

func TestListAPIsDomainFilterReturnsWholeBusiness(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db"})
	defer s.Shutdown()

	res, err := s.aiRouteCatalog(map[string]interface{}{"domain": "onepanel", "limit": float64(100)})
	if err != nil {
		t.Fatal(err)
	}
	payload := res.(map[string]interface{})
	routes := payload["routes"].([]map[string]interface{})
	if len(routes) == 0 {
		t.Fatal("domain=onepanel 没有返回任何路由")
	}

	modules := map[string]bool{}
	for _, r := range routes {
		if r["domain"] != "onepanel" {
			t.Errorf("返回了非 onepanel 域的路由: %v", r["path"])
		}
		modules[r["module"].(string)] = true
	}
	// 必须同时覆盖被拆分出去的 config / spec 子模块。
	for _, want := range []string{"onepanel", "onepanel-config", "onepanel-spec"} {
		if !modules[want] {
			t.Errorf("domain=onepanel 未包含子模块 %s（实际：%s）", want, strings.Join(keysOf(modules), ","))
		}
	}

	// module 精确过滤仍然可用，且不因 domain 改动而放宽。
	res, _ = s.aiRouteCatalog(map[string]interface{}{"module": "onepanel-spec"})
	routes = res.(map[string]interface{})["routes"].([]map[string]interface{})
	if len(routes) != 1 {
		t.Fatalf("module=onepanel-spec 应只返回 1 条，got %d", len(routes))
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
