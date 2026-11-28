package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

func TestAPIKeyRequiresSessionCoversDatabaseImportSubpaths(t *testing.T) {
	for _, path := range []string{
		"/api/settings/database/import",
		"/api/settings/database/import/preview",
		"/api/settings/database/import/commit",
		"/api/settings/export-database",
		"/api/settings/cleanup-deprecated-tables",
	} {
		if !apiKeyRequiresSession(path) {
			t.Fatalf("apiKeyRequiresSession(%q) = false, want true", path)
		}
	}
	for _, path := range []string{
		"/api/settings",
		"/api/settings/database-stats",
		"/api/settings/export-databases",
	} {
		if apiKeyRequiresSession(path) {
			t.Fatalf("apiKeyRequiresSession(%q) = true, want false", path)
		}
	}
}

func TestAPIKeyRequiresSessionCoversCredentialExports(t *testing.T) {
	// 主机凭据/Agent 控制面和云厂商账号会解密返回明文密码、私钥、AK/SK,
	// 只读 API key 不得访问。
	for _, path := range []string{
		"/api/server/accounts",
		"/api/server/accounts/export",
		"/api/server/accounts/42",
		"/api/server/credentials",
		"/api/server/credentials/42",
		"/api/server/agent/command/42",
		"/api/server/agent/install-script/42",
		"/api/aliyun/accounts/export",
		"/api/tencent/accounts",
		"/api/oracle/accounts",
		"/api/m365/accounts/export",
		"/api/huawei/export/accounts",
		"/api/gcp/accounts",
		"/api/cloudflare/accounts/export",
	} {
		if !apiKeyRequiresSession(path) {
			t.Fatalf("apiKeyRequiresSession(%q) = false, want true", path)
		}
	}
	// 边界:同前缀但不同资源的路径不能被误伤。
	for _, path := range []string{
		"/api/server/accounts-export-preview",
		"/api/server/credential-templates",
		"/api/aliyun/dns",
		"/api/tencent/dns",
	} {
		if apiKeyRequiresSession(path) {
			t.Fatalf("apiKeyRequiresSession(%q) = true, want false", path)
		}
	}
}

func TestOriginCheckHostIgnoresInjectedForwardedHost(t *testing.T) {
	server := &Server{cfg: config.Config{
		Environment:       "production",
		TrustedProxyCIDRs: []string{"127.0.0.1"},
	}}

	// 受信代理来源但 Host 已是公网域名:注入的 X-Forwarded-Host 必须被忽略。
	injected := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	injected.Host = "panel.example.com"
	injected.RemoteAddr = "127.0.0.1:12345"
	injected.Header.Set("X-Forwarded-Host", "evil.example.com")
	if got := server.originCheckHost(injected); got != "panel.example.com" {
		t.Fatalf("originCheckHost with injected forwarded host = %q, want panel.example.com", got)
	}

	// 后端看到的 Host 本身是内部地址(反代未回写公网 Host)时才采信 X-Forwarded-Host。
	internal := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	internal.Host = "127.0.0.1:3000"
	internal.RemoteAddr = "127.0.0.1:12345"
	internal.Header.Set("X-Forwarded-Host", "localhost:5173")
	if got := server.originCheckHost(internal); got != "localhost:5173" {
		t.Fatalf("originCheckHost with internal host = %q, want localhost:5173", got)
	}

	// 非受信来源:即使带 X-Forwarded-Host 也只用 Host。
	untrusted := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	untrusted.Host = "panel.example.com"
	untrusted.RemoteAddr = "203.0.113.9:12345"
	untrusted.Header.Set("X-Forwarded-Host", "evil.example.com")
	if got := server.originCheckHost(untrusted); got != "panel.example.com" {
		t.Fatalf("originCheckHost with untrusted peer = %q, want panel.example.com", got)
	}
}

// selfAuthorizedModules 是显式豁免清单:这些 Go 模块的路由不是 AuthSession,
// 要么本就公开,要么在模块 handler 内部完成令牌/密钥/回环校验。authorizeGoRoute
// 对非 Session 路由直接放行的前提是清单完整;新增非 Session 路由若未登记会断言失败。
var selfAuthorizedModules = map[string]string{
	"health":                    "公开健康检查",
	"prometheus-metrics":        "公开指标端点(可选 Bearer token,模块内校验)",
	"migration":                 "公开迁移状态",
	"auth":                      "登录/会话/密码兼容路由,模块内自行判定",
	"auth-2fa-status":           "公开 2FA 登录状态",
	"auth-login-options":        "公开登录方式可用性",
	"auth-plugin-pairing":       "一次性插件配对凭据交换",
	"auth-github-login":         "GitHub OAuth 登录流程",
	"auth-webauthn-login":       "WebAuthn 登录流程",
	"filebox":                   "公开与模块级会话校验混合",
	"uptime":                    "公开状态页与模块级会话校验混合",
	"aiagent":                   "模块内 resolveAuth 处理模块用户 token 与管理会话",
	"aiagent-gateway":           "一次性 stream token 校验",
	"ai-access":                 "AuthAgent 令牌校验",
	"m365-public-register":      "公开注册/邀请描述符",
	"github-webhook":            "Webhook 签名校验",
	"github-public-pages":       "公开页面",
	"prompts-public":            "公开提示词直链",
	"bookmarks-favicon":         "公开非枚举图标",
	"bookmarks-public":          "公开书签页",
	"emailcode-worker-callback": "共享密钥校验",
	"subscription-public":       "公开订阅页(无凭据)",
	"server-status-pages":       "公开状态页",
	"server-terminal-agent":     "WebSocket 握手内校验",
	"server-websocket":          "Agent 握手内校验",
	"server-agent":              "AuthAgent/AuthAPIKey/公开安装脚本混合,模块内校验",
	"server-agent-proxy":        "AuthAgent 校验",
	"openai-compatible":         "modelhub.AuthorizeGatewayRequest",
	"anthropic-compatible":      "modelhub.AuthorizeGatewayRequest",
	"antigravity-compatible":    "AuthInternal + 回环校验",
	"workbuddy-compatible":      "AuthInternal + 回环校验",
	"qoder-compatible":          "AuthInternal + 回环校验",
	"raccoon-compatible":        "AuthInternal + 回环校验",
	"cline-compatible":          "AuthInternal + 回环校验",
}

func TestManifestNonSessionRoutesAreSelfAuthorized(t *testing.T) {
	seen := map[string]bool{}
	for _, route := range manifest.Routes() {
		if route.Owner != manifest.OwnerGo || route.Auth == manifest.AuthSession {
			continue
		}
		if _, ok := selfAuthorizedModules[route.Module]; !ok {
			t.Errorf("非 Session 路由 %s(module=%q, auth=%q)既无模块自校验也未登记豁免;请在模块内自校验或补入 selfAuthorizedModules", route.Prefix, route.Module, route.Auth)
			continue
		}
		seen[route.Module] = true
	}
	for module := range selfAuthorizedModules {
		if !seen[module] {
			t.Errorf("豁免清单中的模块 %q 在 manifest 中不存在非 Session 路由,请移除", module)
		}
	}
}
