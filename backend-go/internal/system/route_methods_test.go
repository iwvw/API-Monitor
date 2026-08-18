package system

import (
	"reflect"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// 描述中显式标注的方法（全角/半角括号）优先于推断：
// 这是「获取安装命令（GET）或发送命令执行（POST）」类中文描述的正确来源。
func TestInferRouteMethodsAnnotations(t *testing.T) {
	cases := []struct {
		desc string
		want []string
	}{
		{"获取 Agent 安装命令（GET）或向 Agent 发送命令执行（POST）", []string{"GET", "POST"}},
		{"更新或删除单个 MCP 服务配置（PUT）", []string{"PUT"}},
		{"读取或更新应用配置（GET）", []string{"GET"}},
		{"没有显式标注的中文描述", nil},
	}
	for _, c := range cases {
		if got := methodsFromAnnotations(c.desc); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("methodsFromAnnotations(%q) = %v, want %v", c.desc, got, c.want)
		}
	}
}

// 中文关键词推断：列出/创建/删除/更新 与英文关键词同语义。
func TestInferRouteMethodsChinese(t *testing.T) {
	cases := []struct {
		desc  string
		mode  manifest.MatchMode
		want  []string
	}{
		{"列出或创建主机任务", manifest.MatchExact, []string{"GET", "POST"}},
		{"列出或新增 cron 任务", manifest.MatchExact, []string{"GET", "POST"}},
		{"列出全部主机", manifest.MatchExact, []string{"GET"}},
		{"新增 MCP 服务配置", manifest.MatchExact, []string{"POST"}},
		{"删除单条会话消息", manifest.MatchExact, []string{"DELETE"}},
		{"更新或删除会话", manifest.MatchPattern, []string{"PUT", "DELETE"}},
	}
	for _, c := range cases {
		got := inferRouteMethods(manifest.Route{Prefix: "/api/test", Description: c.desc, MatchMode: c.mode})
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("inferRouteMethods(%q) = %v, want %v", c.desc, got, c.want)
		}
	}
}

// 无法推断的 MatchPattern 路由保守只读（防文档宣称方法比实际大）。
func TestInferRouteMethodsConservativeFallback(t *testing.T) {
	got := inferRouteMethods(manifest.Route{
		Prefix: "/api/server/some/deep/{id}",
		Description: "某个无法从描述推断的具体操作",
		MatchMode: manifest.MatchPattern,
	})
	if !reflect.DeepEqual(got, []string{"GET"}) {
		t.Fatalf("保守兜底应只声明 GET, got %v", got)
	}
}

// 关键 server 写路由契约：POST 方法必须在 api-docs 中可见
// （AI 依赖该契约执行主机命令；此前只暴露 GET 导致命令下发被预检拒绝）。
func TestServerWriteRouteMethodsExposed(t *testing.T) {
	keyRoutes := []struct {
		prefix string
		want   []string
	}{
		{"/api/server/agent/command/{id}", []string{"GET", "POST"}},
		{"/api/server/tasks", []string{"GET", "POST"}},
		{"/api/server/v2/tasks", []string{"GET", "POST"}},
	}
	for _, kr := range keyRoutes {
		route := manifest.Route{
			Prefix:       kr.prefix,
			Owner:        manifest.OwnerGo,
			Auth:         manifest.AuthSession,
			ResponseMode: manifest.ResponseJSON,
			MatchMode:    manifest.MatchPattern,
		}
		if kr.prefix == "/api/server/tasks" || kr.prefix == "/api/server/v2/tasks" {
			route.MatchMode = manifest.MatchExact
		}
		docs := routeDocs(route)
		if !reflect.DeepEqual(docs.Methods, kr.want) {
			t.Fatalf("%s methods = %v, want %v", kr.prefix, docs.Methods, kr.want)
		}
	}
}