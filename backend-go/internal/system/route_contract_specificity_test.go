package system

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestGetRouteSpecificityOverParamSibling 回归：同段数 pattern 路由并存时，
// 字面量路由必须胜过 {param} 兄弟路由。此前按前缀字符串长度挑路由，
// {imageRef} 因占位符名更长而误胜 /images/prune，导致 prune 契约变成 DELETE。
func TestGetRouteSpecificityOverParamSibling(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	cases := []struct {
		path       string
		wantPrefix string
		wantMethod string
	}{
		{"/api/server/v2/docker/S1/images/prune", "/api/server/v2/docker/{serverId}/images/prune", "POST"},
		{"/api/server/v2/docker/S1/networks/prune", "/api/server/v2/docker/{serverId}/networks/prune", "POST"},
		{"/api/server/v2/docker/S1/volumes/prune", "/api/server/v2/docker/{serverId}/volumes/prune", "POST"},
		{"/api/server/v2/docker/S1/stacks/sync", "/api/server/v2/docker/{serverId}/stacks/sync", "POST"},
		{"/api/server/v2/docker/S1/images/nginx:latest", "/api/server/v2/docker/{serverId}/images/{imageRef}", "DELETE"},
	}
	for _, tc := range cases {
		raw, err := service.getRouteContract(map[string]interface{}{"path": tc.path})
		if err != nil {
			t.Fatalf("getRouteContract(%s) error: %v", tc.path, err)
		}
		contract := raw.(map[string]interface{})
		if got := contract["path"]; got != tc.wantPrefix {
			t.Errorf("path %s resolved to %v, want %s", tc.path, got, tc.wantPrefix)
		}
		methods, _ := contract["methods"].([]string)
		if len(methods) != 1 || methods[0] != tc.wantMethod {
			t.Errorf("path %s resolved to methods %v, want [%s]", tc.path, methods, tc.wantMethod)
		}
	}
}
