package system

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

// TestFindAPIRecallQuality 用真实 AI 常用意图评估检索质量：
// top-1 是否语义正确；得到的 methods 是否含该接口真实方法。
// 这是评估工具而非断言测试：输出统计供人工判断。
func TestFindAPIRecallQuality(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db", Host: "127.0.0.1", Port: 0})

	intents := []struct {
		intent string
		want   string // 期望 top 命中（可空 = 仅观察）
	}{
		{"列出所有 DNS 解析记录", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records"},
		{"给 flyio 应用更新镜像", "/api/flyio/apps/{appName}/update-image"},
		{"查看主机监控状态", "/api/server/monitor/status"},
		{"重启服务器", "/api/server/action"},
		{"刷新 github 仓库数据", "/api/github/repositories/{id}/refresh"},
		{"创建一个 totp 账号", "/api/totp/accounts"},
		{"查看备份列表", "/api/backup/configs"},
		{"更新 DNS 记录", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}"},
		{"查看 openai 网关日志", "/api/openai/analytics/logs"},
		{"删除 cf 账号", "/api/cloudflare/accounts/{id}"},
		{"查看 sftp 文件列表", "/api/server/sftp/list"},
		{"查看定时任务列表", "/api/scheduler/tasks"},
		{"获取 m365 注册记录", "/api/m365/registrations"},
		{"测试服务器连接", "/api/server/test-connection"},
		{"查看文件分享列表", "/api/filebox/shares"},
		{"获取 API 密钥列表", "/api/api-keys"},
		{"查看系统日志", "/api/settings/sys-logs"},
		{"给 onepanel 应用安装", "/api/onepanel/{serverId}/apps/install"},
		{"获取 github 仓库列表", "/api/github/repositories"},
		{"查看 2fa 状态", "/api/auth/2fa/status"},
	}

	hitTop, hitAny, total := 0, 0, len(intents)
	for _, tc := range intents {
		res, err := s.aiFindAPIs(map[string]interface{}{"intent": tc.intent, "limit": 5})
		if err != nil {
			t.Errorf("intent %q -> error %v", tc.intent, err)
			continue
		}
		payload := res.(map[string]interface{})
		routes, _ := payload["routes"].([]map[string]interface{})
		if len(routes) == 0 {
			t.Logf("NO-HIT  %-40s want %s", tc.intent, tc.want)
			continue
		}
		top := routes[0]["path"].(string)
		methods, _ := routes[0]["methods"].([]string)
		mark := "MISS"
		if strings.Contains(top, strings.TrimSuffix(strings.TrimSuffix(tc.want, "/{recordId}"), "")) || top == tc.want {
			hitTop++
			mark = "top1"
		} else if anyHit(routes, tc.want) {
			hitAny++
			mark = "topN"
		}
		t.Logf("[%s] %-40s -> %s %v", mark, tc.intent, top, methods)
		if mark == "MISS" {
			for i, r := range routes {
				t.Logf("       #%d %s", i+1, r["path"])
			}
		}
	}
	t.Logf("RECALL top1=%d/%d topAny=%d/%d", hitTop, total, hitTop+hitAny, total)
}

func anyHit(routes []map[string]interface{}, want string) bool {
	for _, r := range routes {
		if r["path"] == want {
			return true
		}
	}
	return false
}

var _ = manifest.OwnerGo