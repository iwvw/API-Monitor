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
	{Route: manifest.Route{Prefix: "/api/openai/health-check-all", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI full health check", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/export", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/import", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI endpoint import", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/summary", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics summary", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/charts", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics charts", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/analytics/logs", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI analytics logs", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/relay-errors", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI recent relay failure details", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/personas", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI persona list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/personas/{id}", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI persona update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/sessions", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI chat session list/create/clear", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/openai/sessions/{id}", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI chat session update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/sessions/{id}/messages", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI session message list/create/clear", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/openai/sessions/{id}/messages/{messageId}", Module: "openai", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "OpenAI session message delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/cron/preview", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler cron preview", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/tasks", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler task list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/nodes", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler node list/update", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/nodes/{id}", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler node update", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/export", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow export", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/import", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow import", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflows/{id}/run", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflow-runs/{id}/retry", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run retry", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/scheduler/workflow-runs/{id}/cancel", Module: "scheduler", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Scheduler workflow run cancel", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/cron/tasks", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron task list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/cron/tasks/{id}", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron task update/delete", MatchMode: manifest.MatchPattern}},
	{Route: manifest.Route{Prefix: "/api/cron/logs", Module: "cron", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Cron log list/cleanup", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/configs", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup config list/create", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/run", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup job run", MatchMode: manifest.MatchExact}},
	{Route: manifest.Route{Prefix: "/api/backup/restore", Module: "backup", Owner: manifest.OwnerGo, Auth: manifest.AuthSession, ResponseMode: manifest.ResponseJSON, Description: "Backup restore", MatchMode: manifest.MatchExact}},
}

func supplementalRoutes() []manifest.Route {
	routes := make([]manifest.Route, 0, len(apiDocSeeds))
	for _, seed := range apiDocSeeds {
		routes = append(routes, seed.Route)
	}
	return routes
}

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
	for _, seed := range apiDocSeeds {
		if seed.Route.Prefix != route.Prefix {
			continue
		}
		docs = mergeRouteDocs(docs, seed.Docs)
		break
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
