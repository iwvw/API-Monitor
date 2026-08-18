package system

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// TestOpenAPIRoutesCoveredByManifest 保证 OpenAPI 文档中的每一条路径都能被
// manifest 前缀匹配命中（AI 通道 call_api 依赖 manifest.Match 放行）：
// 文档里存在而 manifest 覆盖不到的路径 = AI 无法调用的"微小接口遗漏"。
func TestOpenAPIRoutesCoveredByManifest(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db"})
	doc := s.apiDocs()
	rawItems, ok := doc["routes"].([]apiDocRoute)
	if !ok {
		t.Fatalf("apiDocs routes type = %T", doc["routes"])
	}
	t.Logf("openapi doc routes: %d", len(rawItems))

	var uncovered []string
	for _, item := range rawItems {
		if item.Prefix == "" {
			continue
		}
		if _, ok := manifest.Match(item.Prefix); !ok {
			uncovered = append(uncovered, item.Prefix)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("OpenAPI 文档中存在 %d 条 manifest 无法识别的路径（AI 通道不可达）：\n  %s",
			len(uncovered), strings.Join(uncovered, "\n  "))
	}
}
