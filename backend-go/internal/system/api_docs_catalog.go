package system

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
)

type apiDocParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
}

type apiRouteDocs struct {
	Detail             string            `json:"detail,omitempty"`
	Methods            []string          `json:"methods,omitempty"`
	PathParams         []apiDocParameter `json:"pathParams,omitempty"`
	QueryParams        []apiDocParameter `json:"queryParams,omitempty"`
	Headers            []apiDocParameter `json:"headers,omitempty"`
	RequestContentType string            `json:"requestContentType,omitempty"`
	RequestExample     interface{}       `json:"requestExample,omitempty"`
	ResponseExample    interface{}       `json:"responseExample,omitempty"`
	Notes              []string          `json:"notes,omitempty"`
}

type apiDocSeed struct {
	Route manifest.Route
	Docs  apiRouteDocs
}

var routeParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

var apiDocSeeds = []apiDocSeed{
	{
		Route: manifest.Route{Prefix: "/api/auth/check-password", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "Password status check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:  "检查系统是否已设置管理员密码，以及当前是否运行在演示模式。",
			Methods: []string{"GET"},
			ResponseExample: map[string]interface{}{
				"hasPassword": true,
				"isDemoMode":  false,
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/session", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "Session status check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:  "校验当前浏览器会话是否已登录，适合页面初始化时做权限探测。",
			Methods: []string{"GET"},
			ResponseExample: map[string]interface{}{
				"authenticated": true,
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/login", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "Admin login", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "使用管理员密码登录后台；如果系统已启用 2FA，可在同一次请求里附带一次性验证码。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"password":  "<ADMIN_PASSWORD>",
				"totpToken": "123456",
			},
			ResponseExample: map[string]interface{}{
				"success": true,
				"message": "登录成功",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/logout", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin logout", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:  "销毁当前登录会话并清理认证状态。",
			Methods: []string{"POST"},
			ResponseExample: map[string]interface{}{
				"success": true,
				"message": "已安全登出",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/set-password", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "Initial password setup", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "首次初始化系统管理员密码。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"password": "<NEW_PASSWORD>",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/verify-password", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Verify admin password", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "在执行敏感操作前再次验证管理员密码。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"password": "<ADMIN_PASSWORD>",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/change-password", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Change admin password", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "修改管理员密码，需要同时提交旧密码和新密码。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"oldPassword": "<OLD_PASSWORD>",
				"newPassword": "<NEW_PASSWORD>",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/2fa/setup", Module: "auth-2fa-management", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Generate 2FA secret", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:  "生成 2FA 初始化所需的密钥和二维码数据。",
			Methods: []string{"POST"},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/2fa/enable", Module: "auth-2fa-management", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Enable 2FA", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "使用 setup 阶段返回的密钥和验证码正式启用 2FA。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"secret": "<TOTP_SECRET>",
				"token":  "123456",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/auth/2fa/disable", Module: "auth-2fa-management", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Disable 2FA", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "关闭 2FA 保护，需要再次提交管理员密码确认。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"password": "<ADMIN_PASSWORD>",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/settings/database/import/preview", Module: "settings-database", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Database import preview", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "上传 SQLite 数据库文件并生成导入分析结果，不会立即覆盖当前库。",
			Methods:            []string{"POST"},
			RequestContentType: "multipart/form-data",
			Notes:              []string{"请求体需包含一个数据库文件字段，适合先做结构和完整性预检。"},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/settings/database/import/commit", Module: "settings-database", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Database import commit", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "根据 preview 返回的导入令牌正式提交数据库替换操作。",
			Methods:            []string{"POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"importToken": "<PREVIEW_TOKEN>",
			},
		},
	},
	{
		Route: manifest.Route{Prefix: "/api/aliyun/accounts", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun account list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Detail:             "列出已保存的阿里云账号，或新增一个可用于 DNS 与云资源管理的账号。",
			Methods:            []string{"GET", "POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"name":            "prod",
				"accessKeyId":     "<ACCESS_KEY_ID>",
				"accessKeySecret": "<ACCESS_KEY_SECRET>",
				"regionId":        "cn-hangzhou",
			},
		},
	},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun account update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/metrics", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun instance metrics query", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/domains", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun domain list/create", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/domains/{domainName}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun domain delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/domains/{domainName}/records", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun DNS record list/create", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/records/{recordId}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun DNS record update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/records/{recordId}/status", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun DNS record status update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/instances", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun ECS instance list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/instances/{instanceId}/{action}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun ECS instance lifecycle action", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/swas", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun SWAS instance list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/swas/{instanceId}/firewall", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun SWAS firewall list/create", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/swas/{instanceId}/firewall/{ruleId}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun SWAS firewall rule delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/{id}/swas/{instanceId}/{action}", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun SWAS instance lifecycle action", MatchMode: manifest.MatchPattern}},
	{
		Route: manifest.Route{Prefix: "/api/tencent/accounts", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent account list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{
			Methods:            []string{"GET", "POST"},
			RequestContentType: "application/json",
			RequestExample: map[string]interface{}{
				"name":      "prod",
				"secretId":  "<SECRET_ID>",
				"secretKey": "<SECRET_KEY>",
				"regionId":  "ap-guangzhou",
			},
		},
	},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent account update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/domains", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent domain list/create", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/domains/{domain}/records", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent DNS record list/create", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/domains/{domain}/records/{recordId}", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent DNS record update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/domains/{domain}/records/{recordId}/status", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent DNS record status update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/cvm", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent CVM instance list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/cvm/{instanceId}/control", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent CVM lifecycle control", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/lighthouse", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent Lighthouse instance list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/lighthouse/{instanceId}/control", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent Lighthouse lifecycle control", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/accounts/export", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io account export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/flyio/accounts", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io account list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/flyio/accounts/{id}", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io account delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/accounts/{id}/update-all-images", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io account bulk image update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/proxy/apps", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app list proxy", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/rename", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app rename", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/redeploy", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app redeploy", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/update-image", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app image update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io machine list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/events", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app events", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/config", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app config", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/logs", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly.io app logs", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/accounts/export", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb account export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/koyeb/accounts", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb account list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/koyeb/accounts/{id}", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb account delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/accounts/{id}/refresh", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb account refresh", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/data", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb aggregated app/service data", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/pause", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service pause", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/restart", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service restart", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/redeploy", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service redeploy", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/rename", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service rename", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/logs", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service logs", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/instances", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service instances", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/metrics", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service metrics", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/apps/{appId}", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb app delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/apps/{appId}/rename", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb app rename", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/koyeb/usage", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb usage summary", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/toggle", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint toggle", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/verify", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint verify", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/models", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint model list", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/models/toggle", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint model enable/disable toggle", MatchMode: manifest.MatchPattern}},
	{
		Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/model-mappings", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint model mapping update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}},
	},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/routing", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint routing priority/weight update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/test", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint chat test", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/health-check", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint model health check", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/health-check-all", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint full health check", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/key-check", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint API key validity check", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/health", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint health summary", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/proxy-state", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint proxy pool runtime state", MatchMode: manifest.MatchPattern}},
	{
		Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/proxy-state/unban", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint proxy pool one-click unban", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}},
	},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/refresh", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint refresh all", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/refresh-all", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint refresh all", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/keys", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI gateway key list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/keys/{id}", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI gateway key update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/keys/{id}/toggle", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI gateway key enable/disable toggle", MatchMode: manifest.MatchPattern}},
	{
		Route: manifest.Route{Prefix: "/api/openai/keys/{id}/rotate", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI gateway key rotate", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/openai/keys/{id}/default", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI gateway key set default", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}},
	},
	{Route: manifest.Route{Prefix: "/api/openai/proxies/subscription-nodes", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI subscription socks proxy node list", MatchMode: manifest.MatchExact}},
	{
		Route: manifest.Route{Prefix: "/api/openai/proxies/resolve-subscription", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI resolve subscription proxy nodes", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/openai/analytics/clear", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics log clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/openai/analytics/clear-history", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics dashboard history clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}},
	},
	{Route: manifest.Route{Prefix: "/api/openai/health-check-all", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI full health check", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/export", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/import", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint import", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/summary", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics summary", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/charts", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics charts", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/logs", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics logs", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/relay-errors", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI recent relay failure details", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/cron/preview", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler cron preview", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/tasks", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler task list/create", MatchMode: manifest.MatchExact}},
	{
		Route: manifest.Route{Prefix: "/api/scheduler/tasks/{id}", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler task read/update/delete", MatchMode: manifest.MatchPattern},
		Docs:  apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/scheduler/tasks/{id}/run", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler task manual run", MatchMode: manifest.MatchPattern},
		Docs:  apiRouteDocs{Methods: []string{"POST"}},
	},
	{Route: manifest.Route{Prefix: "/api/scheduler/nodes", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler node list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/scheduler/nodes/{id}", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler node update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow list/create", MatchMode: manifest.MatchExact}},
	{
		Route: manifest.Route{Prefix: "/api/scheduler/workflows/{id}", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow read/update/delete", MatchMode: manifest.MatchPattern},
		Docs:  apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}},
	},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/export", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/import", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow import", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/{id}/run", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflow-runs/{id}/retry", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run retry", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflow-runs/{id}/cancel", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run cancel", MatchMode: manifest.MatchPattern}},
	{
		Route: manifest.Route{Prefix: "/api/scheduler/runs", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run list/clear", MatchMode: manifest.MatchExact},
		Docs:  apiRouteDocs{Methods: []string{"GET", "DELETE"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/scheduler/runs/{id}", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run detail", MatchMode: manifest.MatchPattern},
		Docs:  apiRouteDocs{Methods: []string{"GET"}},
	},
	{Route: manifest.Route{Prefix: "/api/cron/tasks", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron task list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/cron/tasks/{id}", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron task update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/cron/logs", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron log list/cleanup", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/configs", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup config list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/run", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup job run", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/restore", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup restore", MatchMode: manifest.MatchExact}},
	// 主机 Agent 操作型子路由：GET/POST 双方法无法从中文描述推断
	// （「发送命令执行（POST）」标注可覆盖，此处显式登记双保险；
	// 修复前契约只暴露 GET，导致 AI 无法向 Agent 下发命令执行）。
	{
		Route: manifest.Route{Prefix: "/api/server/agent/command/{id}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Get agent install command or send command execution to agent", MatchMode: manifest.MatchPattern},
		Docs:  apiRouteDocs{Methods: []string{"GET", "POST"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/server/tasks", Module: "server-tasks", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "List or create host task", MatchMode: manifest.MatchExact},
		Docs:  apiRouteDocs{Methods: []string{"GET", "POST"}},
	},
	{
		Route: manifest.Route{Prefix: "/api/server/v2/tasks", Module: "server-tasks-v2", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "List or create v2 host task", MatchMode: manifest.MatchExact},
		Docs:  apiRouteDocs{Methods: []string{"GET", "POST"}},
	},
	// ============ 契约覆盖审计修复（2026-08-19） ============
	// 此前中文描述操作型路由大多被保守推断为 GET-only，或子路由漏登记，
	// 导致 AI 按契约调用必 405/404。以下按 handler 真实能力显式登记。

	// ---- server-agent 批量/密钥/卸载（真实 POST，曾误为 GET）----
	{Route: manifest.Route{Prefix: "/api/server/agent/quick-install", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent quick install", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/regenerate-key", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent key regenerate", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/key/generate", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent key generate", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/batch-install", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent batch install", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/batch-upgrade", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent batch upgrade", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/uninstall/{id}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent uninstall", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/install/win/{id}/{key}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Windows agent install script", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/install/linux/{id}/{key}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Linux agent install script", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/status/{id}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent status detail", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/connection-info/{id}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Agent connection info", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- server-agent 托管代理池（nodes/tunnels 真实方法与契约一致）----
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/nodes", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed proxy node list/reconcile", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/nodes/{id}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed proxy node update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/nodes/{id}/reconcile", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed proxy node reconcile", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/tunnels/preflight", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed tunnel preflight check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/tunnels/{serverId}", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed tunnel read/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/tunnels/{serverId}/deploy", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed tunnel deploy", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	// proxy/runtimes：历史契约声明 POST 但 handler 仅 GET（POST 404），对齐现状
	{Route: manifest.Route{Prefix: "/api/server/agent/proxy/runtimes", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Managed proxy runtime list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- server 运维动作（真实 POST，曾误为 GET）----
	{Route: manifest.Route{Prefix: "/api/server/info", Module: "server-operations", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server info probe", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/test-connection", Module: "server-operations", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server connection test", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/check-all", Module: "server-operations", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server batch status check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/docker/check-update", Module: "server-operations", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker update check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/accounts/refresh-locations", Module: "server-accounts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server account locations refresh", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/accounts/reorder", Module: "server-accounts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server account order update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/accounts/{id}/test-traffic-alert", Module: "server-accounts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server traffic alert test", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/accounts/{id}", Module: "server-accounts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server account read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/credentials/{id}", Module: "server-credentials", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server credential update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/credentials/{id}/default", Module: "server-credentials", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server credential set default", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/snippets/history", Module: "server-agent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server snippets history read/collect", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/metrics/history", Module: "server-metrics", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server metrics history read/clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/tasks/{id}/stream", Module: "server-tasks", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Server task output stream（SSE）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- 网络质量 / 远程桌面（真实方法覆盖）----
	{Route: manifest.Route{Prefix: "/api/server/network-quality/targets", Module: "server-network-quality", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Network quality targets list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/network-quality/targets/{id}", Module: "server-network-quality", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Network quality target update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/network-quality/{id}/collect", Module: "server-network-quality", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Network quality collect", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/remote-desktop/sessions", Module: "server-remote-desktop", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Remote desktop session create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/remote-desktop/sessions/{id}", Module: "server-remote-desktop", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Remote desktop session read/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/remote-desktop/sessions/{id}/signals", Module: "server-remote-desktop", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Remote desktop input signals", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},

	// ---- scheduler / cron 补充修正 ----
	{Route: manifest.Route{Prefix: "/api/cron/tasks/{id}/run", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron task manual run", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- settings 清理/维护（真实 POST，曾误为 DELETE/GET）----
	{Route: manifest.Route{Prefix: "/api/settings/cleanup-deprecated-tables", Module: "settings-database", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cleanup deprecated tables", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/clear-logs", Module: "settings-logs", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Clear system logs", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/clear-app-logs", Module: "settings-logs", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Clear app logs", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/enforce-log-limits", Module: "settings-logs", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Enforce log size limits now", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/log-settings", Module: "settings-logs", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Log retention settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/site-brand/icons", Module: "settings-brand", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Site brand icons list/add", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/settings/site-brand/icons/{id}", Module: "settings-brand", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Site brand icon read/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	// database/import 为模块子前缀：实际入口为 import/preview 与 import/commit
	{Route: manifest.Route{Prefix: "/api/settings/database/import", Module: "settings-database", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Database import module（实际入口：import/preview 预览、import/commit 提交）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- admin-ai 管理接口（修正误登方法 + 补缺失写路由）----
	{Route: manifest.Route{Prefix: "/api/admin-ai/sessions/{id}", Module: "admin-ai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin AI session update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/admin-ai/messages", Module: "admin-ai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin AI message send（历史读取经 /sessions/{id}/messages）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/admin-ai/cancel", Module: "admin-ai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin AI run cancel", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/admin-ai/settings", Module: "admin-ai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin AI settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},
	{Route: manifest.Route{Prefix: "/api/admin-ai/approvals/{id}/resolve", Module: "admin-ai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Admin AI approval resolve", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- system/ai-access（修正方法 + 补密钥管理）----
	{Route: manifest.Route{Prefix: "/api/system/ai-access/audit/clear", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Clear AI access audit records", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/system/ai-access/write", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Toggle AI agent write permission", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/system/ai-access/policy", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Set AI agent access policy", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/system/ai-access/mcp-servers", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Add/update AI MCP server config（列表见 overview）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/system/ai-access/skills", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Add/update AI skill config（列表见 overview）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/system/api-keys", Module: "system-api-keys", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Central API keys list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/system/api-keys/{id}", Module: "system-api-keys", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Central API key update/revoke", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/ai/manifest", Module: "ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "AI access capability manifest", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ai-access 别名路径（/api/ai-access/*，与 /api/system/ai-access/* 同 handler 不同前缀）
	{Route: manifest.Route{Prefix: "/api/ai-access/audit/clear", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Clear AI access audit records", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/ai-access/write", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Toggle AI agent write permission", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/ai-access/policy", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Set AI agent access policy", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/ai-access/mcp-servers", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Add/update AI MCP server config（列表见 overview）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/ai-access/skills", Module: "system-ai-access", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Add/update AI skill config（列表见 overview）", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- notification 模块（整模块此前仅登记了入口）----
	{Route: manifest.Route{Prefix: "/api/notification/channels", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification channels list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/channels/{id}", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification channel read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/notification/channels/{id}/test", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification channel test send", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/rules", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification rules list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/rules/{id}", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification rule read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/notification/rules/{id}/dry-run", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification rule dry run", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/rules/{id}/enable", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification rule enable", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/rules/{id}/disable", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification rule disable", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/templates/preview", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification template preview", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/history", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification history list/clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/notification/config", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification global config read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},
	{Route: manifest.Route{Prefix: "/api/notification/trigger", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification manual trigger", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/notification/event-catalog", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification event types catalog", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/notification/events/catalog", Module: "notification", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Notification event types catalog (alias)", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- backup 记录 ---- 
	{Route: manifest.Route{Prefix: "/api/backup/records", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup records list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/backup/records/{id}", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup record delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/backup/records/{id}/download", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup record download", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- GitHub 模块（此前仅登记公开页，业务路由全部缺失）----
	{Route: manifest.Route{Prefix: "/api/github/tokens", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub tokens list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/tokens/{id}", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub token update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/github/tokens/{id}/test", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub token test", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/tokens/{id}/rotate", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub token rotate", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repositories list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/parse-url", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository URL parse", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/reorder", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repositories reorder", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/refresh", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository refresh", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/summary", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository summary", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/trends", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository trends", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/runs", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions runs list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/runs/{runId}/jobs", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions run jobs", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/runs/{runId}/rerun", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions run rerun", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/runs/{runId}/rerun-failed-jobs", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions failed jobs rerun", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/runs/{runId}/cancel", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions run cancel", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/refresh", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub Actions data refresh", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/workflows", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub workflow files list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/actions/workflows/{workflowId}/dispatch", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub workflow dispatch", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/branches", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub branches list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/webhook/configure", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub webhook configure", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/traffic", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository traffic", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/contributors", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub contributors list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/repositories/{id}/events", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub repository events", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/settings", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub collector settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "PATCH"}}},
	{Route: manifest.Route{Prefix: "/api/github/collector/run", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub collector run now", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/collector/status", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub collector status", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/history", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub history clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/github/history/compact", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub history compact", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/events", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub events list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- v2/docker 代理子树（Docker REST 代理，AI 运维主机容器用）----
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/containers/json", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker containers list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/containers/stats", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker containers stats", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/containers/{containerId}/logs", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker container logs", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/containers/{containerId}/{action}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker container action（start/stop/restart/pause/unpause）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/containers/{containerId}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker container delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/images/json", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker images list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/images/prune", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker images prune", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/images/{imageRef}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker image delete（非多段引用可用 images?image= 查询参数）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/networks", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker networks list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/networks/prune", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker networks prune", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/networks/{name}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker network delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/volumes", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker volumes list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/volumes/prune", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker volumes prune", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/volumes/{name}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker volume delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/compose/projects", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker compose projects list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/compose/{project}/{action}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker compose action（up/down/start/stop/restart/pull/update）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/stacks", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker stacks snapshot list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/stacks/sync", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker stacks sync", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/stacks/{project}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker stack record delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/server/v2/docker/{serverId}/stacks/{project}/{action}", Module: "server-docker", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Docker stack action（up/down/start/stop/restart/pull/update）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	// ============ 全模块契约补齐（2026-08-19 第二批） ============

	// ---- Cloudflare 补缺（R2 对象子路由 + 真实写方法）----
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}", Module: "cloudflare-r2", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "R2 object upload/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download", Module: "cloudflare-r2", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "R2 object download", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/preview", Module: "cloudflare-r2", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "R2 object preview", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/folder-download", Module: "cloudflare-r2", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "R2 folder download zip", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}", Module: "cloudflare-tunnels", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tunnel read/rename/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl", Module: "cloudflare-zones", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Zone SSL mode read/update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PATCH"}}},
	{Route: manifest.Route{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}", Module: "cloudflare-workers", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Worker script read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},

	// ---- Aliyun / Tencent 补缺 ----
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/export", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun accounts export", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/aliyun/accounts/import", Module: "aliyun", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Aliyun accounts import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/export", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent accounts export", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/import", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent accounts import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/domains/{domain}", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Tencent domain read/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/cvm/{instanceId}/control", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "CVM instance control（start/stop/reboot 等）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/tencent/accounts/{id}/lighthouse/{instanceId}/control", Module: "tencent", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Lighthouse instance control（start/stop/reboot 等）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- Fly.io 补缺（apps 创建 + machines 整族）----
	{Route: manifest.Route{Prefix: "/api/flyio/apps", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly apps list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/config", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly app config read", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machines list/create", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/wait", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine state wait", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/events", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine events", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/memory", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine memory read/update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/restart", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine restart", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/start", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine start", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/stop", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine stop", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/lease", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine lease read/create/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/flyio/apps/{appName}/machines/{machineId}/metadata", Module: "flyio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Fly machine metadata read/update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "PATCH"}}},

	// ---- Koyeb 修正（rename 真实为 POST）----
	{Route: manifest.Route{Prefix: "/api/koyeb/apps/{appId}/rename", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb app rename", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/koyeb/services/{serviceId}/rename", Module: "koyeb", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Koyeb service rename", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- Oracle 全模块（此前仅模块前缀，子路由全部缺失）----
	{Route: manifest.Route{Prefix: "/api/oracle/accounts", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle accounts list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/export/accounts", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle accounts export", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/import/accounts", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle accounts import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle account update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/verify", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle account verify", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/compartments", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle compartments list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/availability-domains", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle availability domains", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/instances", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle instances list/create", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/instances/{iid}", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle instance read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/instances/{iid}/actions", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle instance action", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/oracle/accounts/{id}/shapes", Module: "oracle", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Oracle shapes list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- M365 全模块（此前仅模块前缀，子路由全部缺失）----
	{Route: manifest.Route{Prefix: "/api/m365/accounts", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 accounts list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/export/accounts", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 accounts export", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/m365/import/accounts", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 accounts import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 account update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/verify", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 account verify", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/organization", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 organization info", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/permissions", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 permissions list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/users", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 users list/create", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/users/{uid}", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 user read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/users/{uid}/license-details", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 user license details", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/users/{uid}/assign-license", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 user assign license", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/licenses/skus", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 license SKUs list", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/groups", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 groups list/create", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/groups/{gid}/members", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 group members list/add", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/groups/{gid}/members/{mid}", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 group member delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/accounts/{id}/groups/{gid}/assign-license", Module: "m365", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 group assign license", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/public-pages", Module: "m365-public", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 public pages list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/m365/public-pages/{id}", Module: "m365-public", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 public page update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/invite-codes", Module: "m365-public", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 invite codes list/create/clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/invite-codes/{id}", Module: "m365-public", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 invite code update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/m365/registrations", Module: "m365-public", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "M365 registrations list/clear", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},

	// ---- OpenAI 网关修正与补缺 ----
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/key-check", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint API key check", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/routing", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint routing update", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/test", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint chat test", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/reorder", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint order update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/models/toggle-batch", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint models batch toggle", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/openai/endpoints/{id}/proxy-state/probe", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Endpoint proxy state probe", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/openai/proxies/import-list", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Proxy nodes import from list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- OnePanel（代理路由整块补齐）----
	{Route: manifest.Route{Prefix: "/api/onepanel/config", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel configs list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/config/{id}", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel config update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/overview", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel overview", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/dashboard/current", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel dashboard current", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Websites list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites/{id}", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Website delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites/{id}/operate", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Website operate（start/stop/restart）", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites/{id}/proxy", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Website reverse proxy config", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites/{id}/https", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Website HTTPS config", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/websites/{id}/nginx", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Website nginx config", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/apps/install", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Apps install", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/apps/installed/{app}/op", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Installed app operation", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/containers/operate", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Containers operate", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/containers/{name}/logs", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Container logs", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/ssl/obtain", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "SSL obtain", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/acme", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "ACME account manage", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/openresty/reload", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenResty reload", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/backup", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel backup create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/databases", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Databases list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/databases/{id}", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Database delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/databases/{id}/password", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Database password reset", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/runtimes", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Runtimes list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/cronjobs", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cronjobs list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/onepanel/upgrade", Module: "onepanel", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OnePanel upgrade", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- Uptime 全模块 ----
	{Route: manifest.Route{Prefix: "/api/uptime/monitors", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitors list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/monitors/{id}", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitor read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/monitors/batch-delete", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitors batch delete", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/monitors/{id}/test", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitor test", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/monitors/{id}/check-now", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitor check now", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/monitors/{id}/toggle", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitor enable/disable", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/batch", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime batch operations", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/import", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime monitors import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/import/preview", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime import preview", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/status-pages", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime status pages list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/uptime/maintenance", Module: "uptime", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Uptime maintenance windows", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},

	// ---- Subscription 全模块（此前仅前缀）----
	{Route: manifest.Route{Prefix: "/api/subscription/profiles", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription profiles list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/profiles/{id}", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription profile read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/profiles/{id}/refresh-upstream", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription profile refresh upstream", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscriptions list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions/{id}", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions/{id}/reset-token", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription token reset", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions/{id}/rotate-address", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription address rotate", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions/{id}/refresh-upstream", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription refresh upstream", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/subscriptions/{id}/usage", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription usage detail (cycle totals + hourly daily/hourly breakdown)", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}, QueryParams: []apiDocParameter{
			{Name: "granularity", Description: "day|hour，默认 day"},
			{Name: "days", Description: "窗口天数，默认 30、上限 90"},
		}}},
	{Route: manifest.Route{Prefix: "/api/subscription/nodes/reorder", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription nodes reorder", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/nodes/{id}", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription node update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/import/preview", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription import preview", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/import/commit", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription import commit", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/templates", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription templates list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/templates/{id}", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription template update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/templates/{id}/default", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription template set default", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/templates/restore-builtins", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription templates restore builtins", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/subscription/settings", Module: "subscription", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Subscription settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},

	// ---- Drawio 修正与补缺 ----
	{Route: manifest.Route{Prefix: "/api/drawio/documents/{id}/clone", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio document clone", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/documents/{id}/draft", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio document draft save", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/documents/{id}/thumbnail", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio document thumbnail save", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/documents/{id}/thumbnails/rebuild", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio thumbnails rebuild", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/thumbnails/rebuild", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio all thumbnails rebuild", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/documents/{id}/versions", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio document version create", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/drawio/settings", Module: "drawio", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Drawio settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},

	// ---- Filebox 全模块 ----
	{Route: manifest.Route{Prefix: "/api/filebox/rooms", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox rooms list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/void/rooms", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox void rooms create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/void/rooms/{id}", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox void room delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/void/rooms/{id}/participants", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox void room participants add", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/void/rooms/{id}/signals", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox void room signals", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/shares", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox shares list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/shares/{id}", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox share read/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/settings", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},
	{Route: manifest.Route{Prefix: "/api/filebox/jobs/cleanup", Module: "filebox", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Filebox cleanup job run", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},

	// ---- TOTP 全模块 ----
	{Route: manifest.Route{Prefix: "/api/totp/accounts", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP accounts list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/accounts/{id}", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP account update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/totp/accounts/{id}/increment", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP account counter increment", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/verify", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP code verify", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/groups", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP groups list/create", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/groups/{id}", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP group update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/totp/export", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP accounts export", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/import", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP accounts import", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/generate-secret", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP secret generate", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/totp/order", Module: "totp", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "TOTP accounts order update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},

	// ---- Prompts 修正与补缺 ----
	{Route: manifest.Route{Prefix: "/api/prompts/entries/{id}", Module: "prompts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Prompt entry read/update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/prompts/entries/{id}/duplicate", Module: "prompts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Prompt entry duplicate", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/prompts/entries/{id}/draft", Module: "prompts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Prompt entry draft save", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT"}}},
	{Route: manifest.Route{Prefix: "/api/prompts/entries/{id}/public/regenerate", Module: "prompts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Prompt public link regenerate", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/prompts/settings", Module: "prompts", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Prompts settings read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "PUT"}}},

	// ---- Auth 修正与补缺 ----
	{Route: manifest.Route{Prefix: "/api/auth/github/config", Module: "auth-github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub OAuth config read/update", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/register/begin", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn registration begin", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/register/finish", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn registration finish", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/credentials", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn credentials list", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/credentials/{id}/delete", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn credential delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/login/begin", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn login begin", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/webauthn/login/finish", Module: "auth-webauthn", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "WebAuthn login finish", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/plugin-token", Module: "auth-plugin", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Plugin token create/delete", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/auth/sessions", Module: "auth-sessions", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Auth sessions list/cleanup", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/auth/sessions/{id}", Module: "auth-sessions", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Auth session revoke", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/auth/sessions/revoke-all", Module: "auth-sessions", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Auth all sessions revoke", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/auth/check-password", Module: "auth", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "Password status check", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},

	// ---- GitHub 补缺 / System 补缺 ----
	{Route: manifest.Route{Prefix: "/api/github/public-pages/{id}", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub public page update/delete", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"PUT", "PATCH", "DELETE"}}},
	{Route: manifest.Route{Prefix: "/api/github/public/pages/{slug}", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub public page by slug", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/public/page-by-domain", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "GitHub public page by domain", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
	{Route: manifest.Route{Prefix: "/api/github/webhook", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "GitHub webhook receiver", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/github/webhook/{repositoryId}", Module: "github", Owner: manifest.OwnerGo, Auth: manifest.AuthPublic, ResponseMode: manifest.ResponseJSON, Description: "GitHub webhook receiver (repository bound)", MatchMode: manifest.MatchPattern},
		Docs: apiRouteDocs{Methods: []string{"POST"}}},
	{Route: manifest.Route{Prefix: "/api/system/status/stream", Module: "system", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "System status SSE stream", MatchMode: manifest.MatchExact},
		Docs: apiRouteDocs{Methods: []string{"GET"}}},
}

func supplementalRoutes() []manifest.Route {
	routes := make([]manifest.Route, 0, len(apiDocSeeds))
	for _, seed := range apiDocSeeds {
		routes = append(routes, seed.Route)
	}
	return routes
}

// SupplementalRoutes 返回 apiDocSeeds 补充登记的路由（供 api-docs 输出
// 与契约覆盖审计程序使用）。
func SupplementalRoutes() []manifest.Route {
	return supplementalRoutes()
}

// RouteDocsForAudit 导出单条路由的文档视图（真实 methods 与契约），
// 供契约覆盖审计程序与工具脚本核对「后端 handler 真实能力 vs 文档声明」。
func RouteDocsForAudit(route manifest.Route) apiRouteDocs {
	return routeDocs(route)
}

// routeDocs 返回给定路由的 api-docs 文档视图（方法/参数/请求体等）。
func routeDocs(route manifest.Route) apiRouteDocs {
	if route.MatchMode == "" {
		route.MatchMode = manifest.MatchPrefix
	}
	docs := apiRouteDocs{
		Detail:     defaultRouteDetail(route),
		PathParams: extractRoutePathParams(route.Prefix),
		Headers:    authHeaders(route.Auth),
		Notes:      defaultRouteNotes(route),
	}
	// 合并同 Prefix 的全部种子：数组早期存在「无 Methods 的占位条目」
	// （方法靠 infer 兜底），修复契约的补充条目追加在数组后部；
	// 依次 merge、后者覆盖前者，确保带显式 Methods 的修正生效。
	for _, seed := range apiDocSeeds {
		if seed.Route.Prefix != route.Prefix {
			continue
		}
		docs = mergeRouteDocs(docs, seed.Docs)
	}
	if len(docs.Methods) == 0 {
		docs.Methods = inferRouteMethods(route)
	}
	if strings.TrimSpace(docs.Detail) == "" {
		docs.Detail = routeDescription(route)
	}
	return docs
}

func mergeRouteDocs(base, override apiRouteDocs) apiRouteDocs {
	if strings.TrimSpace(override.Detail) != "" {
		base.Detail = override.Detail
	}
	if len(override.Methods) > 0 {
		base.Methods = slices.Clone(override.Methods)
	}
	if len(override.PathParams) > 0 {
		base.PathParams = slices.Clone(override.PathParams)
	}
	if len(override.QueryParams) > 0 {
		base.QueryParams = slices.Clone(override.QueryParams)
	}
	if len(override.Headers) > 0 {
		base.Headers = slices.Clone(override.Headers)
	}
	if strings.TrimSpace(override.RequestContentType) != "" {
		base.RequestContentType = override.RequestContentType
	}
	if override.RequestExample != nil {
		base.RequestExample = override.RequestExample
	}
	if override.ResponseExample != nil {
		base.ResponseExample = override.ResponseExample
	}
	if len(override.Notes) > 0 {
		base.Notes = append(base.Notes, override.Notes...)
	}
	return base
}

func defaultRouteDetail(route manifest.Route) string {
	detail := routeDescription(route)
	switch {
	case route.MatchMode == manifest.MatchPrefix:
		return fmt.Sprintf("%s 这是一个模块级前缀路由，用来归档同一业务域下的子接口。", detail)
	case route.MatchMode == manifest.MatchPattern:
		return fmt.Sprintf("%s 路径中包含资源标识参数，调用前请先替换占位符。", detail)
	default:
		return detail
	}
}

func defaultRouteNotes(route manifest.Route) []string {
	notes := []string{}
	switch route.ResponseMode {
	case manifest.ResponseStream:
		notes = append(notes, "该接口会返回流式内容，客户端需要持续读取响应体。")
	case manifest.ResponseWebSocket:
		notes = append(notes, "该接口使用 WebSocket 协议，不能按普通 JSON 请求方式调用。")
	case manifest.ResponseProxy:
		notes = append(notes, "该接口返回代理内容，实际响应头与内容类型可能由上游资源决定。")
	}
	if route.Owner == manifest.OwnerRetired {
		notes = append(notes, "该接口已标记为停用，仅保留历史索引和兼容性提示。")
	}
	if route.MatchMode == manifest.MatchPrefix {
		notes = append(notes, "如果你想查看更细的接口，请优先选择同前缀下的具体子路由。")
	}
	return notes
}

func extractRoutePathParams(prefix string) []apiDocParameter {
	matches := routeParamPattern.FindAllStringSubmatch(prefix, -1)
	if len(matches) == 0 {
		return nil
	}
	params := make([]apiDocParameter, 0, len(matches))
	for _, match := range matches {
		name := match[1]
		params = append(params, apiDocParameter{
			Name:        name,
			In:          "path",
			Required:    true,
			Description: describePathParam(name),
			Example:     examplePathParam(name),
		})
	}
	return params
}

func authHeaders(mode manifest.AuthMode) []apiDocParameter {
	switch mode {
	case manifest.AuthSession:
		return []apiDocParameter{
			{Name: "sid", In: "cookie", Required: true, Description: "已登录后台会话 Cookie", Example: "<SESSION_ID>"},
		}
	case manifest.AuthAPIKey:
		return []apiDocParameter{
			{Name: "Authorization", In: "header", Required: true, Description: "OpenAI 兼容 API Key，格式为 Bearer Token", Example: "Bearer sk-xxx"},
		}
	case manifest.AuthAgent:
		return []apiDocParameter{
			{Name: "Authorization", In: "header", Required: true, Description: "AI Agent Key，格式为 Bearer Token", Example: "Bearer am-xxx"},
		}
	default:
		return nil
	}
}

func describePathParam(name string) string {
	switch name {
	case "id":
		return "资源主键 ID"
	case "accountId":
		return "云账号或平台账号 ID"
	case "zoneId":
		return "Zone 标识"
	case "recordId":
		return "DNS 记录 ID"
	case "templateId":
		return "模板 ID"
	case "deploymentId":
		return "部署记录 ID"
	case "projectName":
		return "项目名称"
	case "scriptName":
		return "脚本名称"
	case "domain", "domainName":
		return "域名"
	case "serverId":
		return "服务器 ID"
	case "instanceId":
		return "实例 ID"
	case "ruleId":
		return "规则 ID"
	case "serviceId":
		return "服务 ID"
	case "appId":
		return "应用 ID"
	case "appName":
		return "应用名称"
	case "messageId":
		return "消息 ID"
	case "slug":
		return "公开页面 slug"
	case "token":
		return "访问令牌"
	case "key":
		return "安装密钥或一次性校验密钥"
	case "action":
		return "动作名称，例如 start、stop、reboot"
	default:
		return "路径参数"
	}
}

func examplePathParam(name string) string {
	switch name {
	case "id", "accountId":
		return "1"
	case "zoneId":
		return "zone-001"
	case "recordId":
		return "record-001"
	case "templateId":
		return "template-001"
	case "deploymentId":
		return "deployment-001"
	case "projectName":
		return "my-project"
	case "scriptName":
		return "worker-demo"
	case "domain", "domainName":
		return "example.com"
	case "serverId":
		return "server-001"
	case "instanceId":
		return "instance-001"
	case "ruleId":
		return "rule-001"
	case "serviceId":
		return "service-001"
	case "appId":
		return "app-001"
	case "appName":
		return "demo-app"
	case "messageId":
		return "msg-001"
	case "slug":
		return "public-status"
	case "token":
		return "token-001"
	case "key":
		return "install-key"
	case "action":
		return "start"
	default:
		return "value"
	}
}
