package manifest

import (
	"net/http"
	"strings"
)

type AuthMode string
type Owner string
type ResponseMode string
type MatchMode string

const (
	AuthPublic  AuthMode = "public"
	AuthSession AuthMode = "session"
	AuthAPIKey  AuthMode = "api_key"
	AuthAgent   AuthMode = "agent_key"

	OwnerGo      Owner = "go"
	OwnerNode    Owner = "node"
	OwnerRetired Owner = "retired"

	ResponseJSON      ResponseMode = "json"
	ResponseStatic    ResponseMode = "static"
	ResponseStream    ResponseMode = "stream"
	ResponseWebSocket ResponseMode = "websocket"
	ResponseProxy     ResponseMode = "proxy"

	MatchPrefix  MatchMode = "prefix"
	MatchExact   MatchMode = "exact"
	MatchPattern MatchMode = "pattern"
)

type Route struct {
	Prefix       string       `json:"prefix"`
	Module       string       `json:"module"`
	Owner        Owner        `json:"owner"`
	Auth         AuthMode     `json:"auth"`
	ResponseMode ResponseMode `json:"responseMode"`
	Description  string       `json:"description"`
	MatchMode    MatchMode    `json:"matchMode,omitempty"`
}

func Routes() []Route {
	return []Route{
		{Prefix: "/health", Module: "health", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Go shell health check"},
		{Prefix: "/api/migration/status", Module: "migration", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Go migration status and route ownership"},
		{Prefix: "/api/auth/2fa/status", Module: "auth-2fa-status", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Public 2FA login status route"},
		{Prefix: "/api/auth/2fa", Module: "auth-2fa-management", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "2FA setup and management routes"},
		{Prefix: "/api/auth", Module: "auth", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Authentication, password, and session compatibility routes"},
		{Prefix: "/api/settings/operation-logs", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Operation log query"},
		{Prefix: "/api/settings/sys-logs", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Recent app log entries from the Go-visible log file"},
		{Prefix: "/api/settings/app-log-file", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Raw app log file tail"},
		{Prefix: "/api/settings/log-settings", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Log retention settings"},
		{Prefix: "/api/settings/enforce-log-limits", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Log retention enforcement"},
		{Prefix: "/api/settings/clear-logs", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Database log cleanup"},
		{Prefix: "/api/settings/clear-app-logs", Module: "settings-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "App log truncation"},
		{Prefix: "/api/settings/database-stats", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Database table counts and file size"},
		{Prefix: "/api/settings/database-analysis", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Database table row counts and size estimates"},
		{Prefix: "/api/settings/deprecated-tables", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Deprecated database table cleanup preview"},
		{Prefix: "/api/settings/cleanup-deprecated-tables", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Backed-up deprecated database table cleanup"},
		{Prefix: "/api/settings/database/import", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Database import preview and commit"},
		{Prefix: "/api/settings/export-database", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SQLite database export"},
		{Prefix: "/api/settings/import-database", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Legacy database import compatibility route"},
		{Prefix: "/api/settings/migration-self-check", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Database migration table and column self-check"},
		{Prefix: "/api/settings/vacuum-database", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SQLite WAL checkpoint and VACUUM action"},
		{Prefix: "/api/settings/clear-chat-messages", Module: "settings-database", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Legacy chat table cleanup"},
		{Prefix: "/api/settings", Module: "settings", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "User settings read/update", MatchMode: MatchExact},
		{Prefix: "/api/system/host-metrics", Module: "system-host-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Local host CPU, memory, disk, and process metrics"},
		{Prefix: "/api/system/api-stats", Module: "system-api-stats", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "System API call stats (audit & ops)"},
		{Prefix: "/api/system/api-docs", Module: "system-api-docs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Auto-generated API documentation index", MatchMode: MatchExact},
		{Prefix: "/api/system/openapi.json", Module: "system-api-docs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Auto-generated OpenAPI document", MatchMode: MatchExact},
		{Prefix: "/api/openapi.json", Module: "system-api-docs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Auto-generated OpenAPI document", MatchMode: MatchExact},
		{Prefix: "/api/system/ai-access/key/rotate", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Rotate AI access agent key", MatchMode: MatchExact},
		{Prefix: "/api/system/ai-access/mcp-servers/{id}", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI MCP server update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/system/ai-access/mcp-servers", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI MCP server list/create", MatchMode: MatchExact},
		{Prefix: "/api/system/ai-access/skills/{id}", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI Skill update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/system/ai-access/skills", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI Skill list/create", MatchMode: MatchExact},
		{Prefix: "/api/system/ai-access/audit/clear", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Clear AI access audit entries", MatchMode: MatchExact},
		{Prefix: "/api/system/ai-access", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI access management", MatchMode: MatchExact},
		{Prefix: "/api/ai-access/key/rotate", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Rotate AI access agent key", MatchMode: MatchExact},
		{Prefix: "/api/ai-access/mcp-servers/{id}", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI MCP server update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/ai-access/mcp-servers", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI MCP server list/create", MatchMode: MatchExact},
		{Prefix: "/api/ai-access/skills/{id}", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI Skill update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/ai-access/skills", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI Skill list/create", MatchMode: MatchExact},
		{Prefix: "/api/ai-access/audit/clear", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Clear AI access audit entries", MatchMode: MatchExact},
		{Prefix: "/api/ai-access", Module: "system-ai-access", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "AI access management", MatchMode: MatchExact},
		{Prefix: "/api/ai/manifest", Module: "ai-access", Owner: OwnerGo, Auth: AuthAgent, ResponseMode: ResponseJSON, Description: "AI access manifest", MatchMode: MatchExact},
		{Prefix: "/api/ai/mcp", Module: "ai-access", Owner: OwnerGo, Auth: AuthAgent, ResponseMode: ResponseJSON, Description: "AI MCP JSON-RPC endpoint", MatchMode: MatchExact},
		{Prefix: "/api/system/logs/stream", Module: "system-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Tail and filter app logs", MatchMode: MatchExact},
		{Prefix: "/api/system/logs/download", Module: "system-logs", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Download app log file", MatchMode: MatchExact},
		{Prefix: "/api/totp", Module: "totp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "TOTP and HOTP module"},
		{Prefix: "/api/filebox", Module: "filebox", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Filebox public and authenticated routes"},
		{Prefix: "/api/uptime", Module: "uptime", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Uptime monitors, public status, push, badge"},
		{Prefix: "/api/notification", Module: "notification", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Notification channels, rules, and history"},
		{Prefix: "/api/scheduler", Module: "scheduler", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Workflow scheduler, DAG, runs, and distributed nodes"},
		{Prefix: "/api/cron", Module: "cron", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cron tasks, scheduler, and logs"},
		{Prefix: "/api/backup", Module: "backup", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Local backup center configs, records, and runner"},
		{Prefix: "/api/cloudflare/accounts/export", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account export", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/export/accounts", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare legacy account export", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/import/accounts", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account import", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/templates/{templateId}/apply", Module: "cloudflare-templates", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS template apply", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/templates/{id}", Module: "cloudflare-templates", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS template update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/templates", Module: "cloudflare-templates", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS template list/create", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/import/templates", Module: "cloudflare-templates", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS template import", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/accounts/{id}/verify", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account token verification", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/token", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account token reveal", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/cf-account-id", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account ID lookup for Workers", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages project list", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages/{projectName}", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages project delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages deployment list", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments/{deploymentId}", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages deployment delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages/{projectName}/domains", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages custom domain list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/pages/{projectName}/domains/{domain}", Module: "cloudflare-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Pages custom domain delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker script list", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker script get/update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}/toggle", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker subdomain toggle", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}/analytics", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker analytics", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker custom domains list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains/{domainId}", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker custom domain delete", MatchMode: MatchPattern},

		{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets", Module: "cloudflare-r2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare R2 bucket list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}", Module: "cloudflare-r2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare R2 bucket delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects", Module: "cloudflare-r2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare R2 object list", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}", Module: "cloudflare-r2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare R2 object delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download-info", Module: "cloudflare-r2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare R2 object download info", MatchMode: MatchPattern},

		{Prefix: "/api/cloudflare/accounts/{id}/tunnels", Module: "cloudflare-tunnels", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Tunnel list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}", Module: "cloudflare-tunnels", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Tunnel details/delete/update", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/configuration", Module: "cloudflare-tunnels", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Tunnel configuration read/update", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/token", Module: "cloudflare-tunnels", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Tunnel token", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/connections", Module: "cloudflare-tunnels", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Tunnel connections", MatchMode: MatchPattern},

		{Prefix: "/api/cloudflare/accounts/{id}", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts", Module: "cloudflare-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare account list/create", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/record-types", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS record type list", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/zones", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare aggregate zone list", MatchMode: MatchExact},
		{Prefix: "/api/cloudflare/accounts/{id}/zones", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare zone list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare zone delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker route list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes/{routeId}", Module: "cloudflare-workers", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare Worker route update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS record list/create", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS record update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge", Module: "cloudflare-zone-resources", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare zone cache purge", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl", Module: "cloudflare-zone-resources", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare zone SSL settings", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/analytics", Module: "cloudflare-zone-resources", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare zone analytics", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/switch", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS quick content switch", MatchMode: MatchPattern},
		{Prefix: "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/batch", Module: "cloudflare-dns", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Cloudflare DNS batch create", MatchMode: MatchPattern},
		{Prefix: "/api/aliyun", Module: "aliyun", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Aliyun DNS and compute"},
		{Prefix: "/api/tencent", Module: "tencent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Tencent DNS and compute"},
		{Prefix: "/api/koyeb", Module: "koyeb", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Koyeb accounts and services"},
		{Prefix: "/api/flyio", Module: "flyio", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Fly.io accounts, apps, and machines"},

		{Prefix: "/api/openai", Module: "openai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "OpenAI endpoint manager and proxy"},
		{Prefix: "/v1", Module: "openai-compatible", Owner: OwnerGo, Auth: AuthAPIKey, ResponseMode: ResponseStream, Description: "OpenAI-compatible API"},

		// Server Agent routes (Wave 5b)
		{Prefix: "/api/server/info", Module: "server-operations", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server info", MatchMode: MatchExact},
		{Prefix: "/api/server/test-connection", Module: "server-operations", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Test server connection", MatchMode: MatchExact},
		{Prefix: "/api/server/action", Module: "server-operations", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server actions (reboot, shutdown)", MatchMode: MatchExact},
		{Prefix: "/api/server/check-all", Module: "server-operations", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Check all servers", MatchMode: MatchExact},

		{Prefix: "/api/server/agent/quick-install", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent quick install", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/regenerate-key", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Regenerate agent key", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/command/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent install command", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/install/win/{id}/{key}", Module: "server-agent", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Windows Agent install script", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/install/linux/{id}/{key}", Module: "server-agent", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Linux Agent install script", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/install/linux/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseJSON, Description: "Linux Agent install script", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/install-script/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent install script", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/heartbeat", Module: "server-agent", Owner: OwnerGo, Auth: AuthAPIKey, ResponseMode: ResponseJSON, Description: "Agent heartbeat", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/status/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent status", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/key", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent key management", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/key/generate", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Generate new agent key", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/auto-install/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent auto install via SSH/upgrade", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/batch-install", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Create Agent batch install task", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/batch-upgrade", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Create Agent batch upgrade task", MatchMode: MatchExact},
		{Prefix: "/api/server/agent/batch/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent batch task status", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/connection-info/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent connection status info", MatchMode: MatchPattern},
		{Prefix: "/api/server/agent/uninstall/{id}", Module: "server-agent", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Agent uninstall", MatchMode: MatchPattern},

		{Prefix: "/api/server/monitor/status", Module: "server-monitor", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Monitor status", MatchMode: MatchExact},
		{Prefix: "/api/server/monitor/collect", Module: "server-monitor", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Trigger metrics collection", MatchMode: MatchExact},

		{Prefix: "/api/server/docker/check-update", Module: "server-docker", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Docker update check", MatchMode: MatchExact},

		{Prefix: "/api/server/sftp/list", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP list directory", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/read", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP read file", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/write", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP write file", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/mkdir", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP create directory", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/rename", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP rename", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/delete", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP delete file", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/rmdir", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP remove directory", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/chmod", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP change permissions", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/upload", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP upload file", MatchMode: MatchExact},
		{Prefix: "/api/server/sftp/download/{serverId}", Module: "server-sftp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "SFTP download file", MatchMode: MatchPattern},

		{Prefix: "/api/server/v2/tasks/stream", Module: "server-tasks-v2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseStream, Description: "Server tasks v2 stream", MatchMode: MatchExact},
		{Prefix: "/api/server/v2/tasks", Module: "server-tasks-v2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server tasks v2", MatchMode: MatchExact},
		{Prefix: "/api/server/v2/docker/overview", Module: "server-docker-v2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Docker overview v2", MatchMode: MatchExact},
		{Prefix: "/api/server/v2/docker", Module: "server-docker-v2", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Docker proxy v2"},

		{Prefix: "/api/server/accounts/export", Module: "server-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server account export", MatchMode: MatchExact},
		{Prefix: "/api/server/accounts/import", Module: "server-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server account import", MatchMode: MatchExact},
		{Prefix: "/api/server/accounts/reorder", Module: "server-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server account order update", MatchMode: MatchExact},
		{Prefix: "/api/server/accounts/{id}/test-traffic-alert", Module: "server-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Test server traffic alert", MatchMode: MatchPattern},
		{Prefix: "/api/server/accounts/{id}", Module: "server-accounts", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server account read/update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/server/accounts", Module: "server-api", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server account list", MatchMode: MatchExact},
		{Prefix: "/api/server/status-pages/{id}", Module: "server-status-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server status page update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/server/status-pages", Module: "server-status-pages", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server status page list/create", MatchMode: MatchExact},

		// REST API for server list and details
		{Prefix: "/api/server/s/{id}/history", Module: "server-api", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server metrics history", MatchMode: MatchPattern},
		{Prefix: "/api/server/s/{id}", Module: "server-api", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server detail", MatchMode: MatchPattern},
		{Prefix: "/api/server/s", Module: "server-api", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server list", MatchMode: MatchExact},

		{Prefix: "/api/server/credentials/default", Module: "server-credentials", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Default server credential", MatchMode: MatchExact},
		{Prefix: "/api/server/credentials/{id}/default", Module: "server-credentials", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Set default server credential", MatchMode: MatchPattern},

		{Prefix: "/api/server/metrics/history", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Metrics history query and cleanup", MatchMode: MatchExact},
		{Prefix: "/api/server/metrics/history/{id}", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Metrics history by server", MatchMode: MatchPattern},
		{Prefix: "/api/server/metrics/latest/{id}", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Latest metrics", MatchMode: MatchPattern},
		{Prefix: "/api/server/network-quality/targets/{id}", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Manage specific network quality targets", MatchMode: MatchPattern},
		{Prefix: "/api/server/network-quality/targets", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Manage all network quality targets", MatchMode: MatchExact},
		{Prefix: "/api/server/network-quality/{id}/collect", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Collect network quality", MatchMode: MatchPattern},
		{Prefix: "/api/server/network-quality/{id}", Module: "server-metrics", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Network quality", MatchMode: MatchPattern},

		{Prefix: "/api/server/tasks/{id}/stream", Module: "server-tasks", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseStream, Description: "Task SSE stream", MatchMode: MatchPattern},
		{Prefix: "/api/server/tasks", Module: "server-tasks", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Task management", MatchMode: MatchExact},

		// Terminal and Socket.IO / Engine.IO routes
		{Prefix: "/ws/ssh", Module: "server-terminal", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseWebSocket, Description: "SSH terminal WebSocket", MatchMode: MatchExact},
		{Prefix: "/socket.io/", Module: "server-websocket", Owner: OwnerGo, Auth: AuthPublic, ResponseMode: ResponseWebSocket, Description: "Agent WebSocket", MatchMode: MatchPrefix},
		{Prefix: "/api/server/credentials/{id}", Module: "server-credentials", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server credential delete", MatchMode: MatchPattern},
		{Prefix: "/api/server/credentials", Module: "server-credentials", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server credential list/create", MatchMode: MatchExact},
		{Prefix: "/api/server/snippets/preview", Module: "server-snippets", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server command snippet preview", MatchMode: MatchExact},
		{Prefix: "/api/server/snippets/history", Module: "server-snippets", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server command history", MatchMode: MatchExact},
		{Prefix: "/api/server/snippets/{id}", Module: "server-snippets", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server command snippet update/delete", MatchMode: MatchPattern},
		{Prefix: "/api/server/snippets", Module: "server-snippets", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server command snippet list/create", MatchMode: MatchExact},
		{Prefix: "/api/server/monitor/config", Module: "server-monitor", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server monitor configuration", MatchMode: MatchExact},
		{Prefix: "/api/server/monitor/logs", Module: "server-monitor", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Server monitor logs", MatchMode: MatchExact},
	}
}

func Match(path string) (Route, bool) {
	var best Route
	found := false
	for _, route := range Routes() {
		if matchesRoute(path, route) && (!found || routeScore(route) > routeScore(best)) {
			best = route
			found = true
		}
	}
	return best, found
}

func Summary() map[string]int {
	summary := map[string]int{
		string(OwnerGo):      0,
		string(OwnerNode):    0,
		string(OwnerRetired): 0,
	}
	for _, route := range Routes() {
		summary[string(route.Owner)]++
	}
	return summary
}

func KnownPrefixes() []string {
	routes := Routes()
	prefixes := make([]string, 0, len(routes))
	for _, route := range routes {
		prefixes = append(prefixes, route.Prefix)
	}
	return prefixes
}

func matchesPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, strings.TrimRight(prefix, "/")+"/")
}

func matchesRoute(path string, route Route) bool {
	switch route.MatchMode {
	case MatchExact:
		return path == route.Prefix
	case MatchPattern:
		return matchesPattern(path, route.Prefix)
	default:
		return matchesPrefix(path, route.Prefix)
	}
}

func matchesPattern(path, pattern string) bool {
	pathParts := splitPath(path)
	patternParts := splitPath(pattern)
	if len(pathParts) != len(patternParts) {
		return false
	}
	for i, part := range patternParts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if pathParts[i] != part {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func routeScore(route Route) int {
	score := len(route.Prefix)
	switch route.MatchMode {
	case MatchExact:
		score += 2000
	case MatchPattern:
		score += 1000
	}
	return score
}

func IsUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Upgrade") != ""
}
