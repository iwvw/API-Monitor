package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/aliyun"
	"github.com/iwvw/api-monitor/backend-go/internal/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/backup"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/cronjobs"
	drawiomodule "github.com/iwvw/api-monitor/backend-go/internal/drawio"
	"github.com/iwvw/api-monitor/backend-go/internal/filebox"
	"github.com/iwvw/api-monitor/backend-go/internal/flyio"
	githubmodule "github.com/iwvw/api-monitor/backend-go/internal/github"
	"github.com/iwvw/api-monitor/backend-go/internal/koyeb"
	"github.com/iwvw/api-monitor/backend-go/internal/m365"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	"github.com/iwvw/api-monitor/backend-go/internal/notification"
	"github.com/iwvw/api-monitor/backend-go/internal/onepanel"
	"github.com/iwvw/api-monitor/backend-go/internal/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/oracle"
	promptsmodule "github.com/iwvw/api-monitor/backend-go/internal/prompts"
	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/serveragent"
	"github.com/iwvw/api-monitor/backend-go/internal/settings"
	"github.com/iwvw/api-monitor/backend-go/internal/subscription"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
	"github.com/iwvw/api-monitor/backend-go/internal/systemlogs"
	"github.com/iwvw/api-monitor/backend-go/internal/tencent"
	"github.com/iwvw/api-monitor/backend-go/internal/totp"
	"github.com/iwvw/api-monitor/backend-go/internal/uptime"
)

type Server struct {
	cfg      config.Config
	auth     *auth.Service
	settings *settings.Service
	system   *systemmetrics.Service
	totp     *totp.Service
	cron     *cronjobs.Service
	filebox  *filebox.Service
	notify   *notification.Service
	uptime   *uptime.Service
	koyeb    *koyeb.Service
	flyio    *flyio.Service
	onepanel *onepanel.Service
	github   *githubmodule.Service
	aliyun   *aliyun.Service
	tencent  *tencent.Service
	oracle   *oracle.Service
	cf       *cloudflare.Service
	m365     *m365.Service
	openai   *openai.Service
	server   *serveragent.Service
	backup   *backup.Service
	logs     *systemlogs.Service
	sub      *subscription.Service
	drawio   *drawiomodule.Service
	prompts  *promptsmodule.Service

	// warmupCancel 在 Shutdown 时取消代理池预热 goroutine，避免后台任务
	// 在 Gate 结束后继续访问数据目录（测试 teardown 也会受影响）。
	warmupCancel context.CancelFunc
}

func New(cfg config.Config) http.Handler {
	return NewServer(cfg)
}

func NewServer(cfg config.Config) *Server {
	server, err := newServer(cfg)
	if err != nil {
		panic(err)
	}
	return server
}

// NewChecked is the production constructor. Schema initialization failures
// are fatal because serving a partially migrated subscription API only turns a
// deterministic startup problem into repeated HTTP 500 responses.
func NewChecked(cfg config.Config) (*Server, error) {
	return newServer(cfg)
}

func newServer(cfg config.Config) (*Server, error) {
	if err := cfg.ValidateSecurity(); err != nil {
		return nil, err
	}
	subscriptionService := subscription.New(cfg)
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := subscriptionService.Initialize(initCtx)
	initCancel()
	if err != nil {
		return nil, fmt.Errorf("initialize subscription schema: %w", err)
	}
	authService := auth.New(cfg)
	notifyService := notification.New(cfg)
	serverAgentService := serveragent.New(cfg)
	if err := serverAgentService.StartupError(); err != nil {
		serverAgentService.Stop()
		return nil, fmt.Errorf("initialize server agent schema: %w", err)
	}
	serverAgentService.SetNotifier(notifyService)
	cloudflareService := cloudflare.New(cfg)
	serverAgentService.SetCloudflareTunnelManager(cloudflareService)
	cronService := cronjobs.New(cfg)
	cronService.SetAgentRunner(serverAgentService)
	uptimeService := uptime.New(cfg, authService, notifyService)
	uptimeService.SetHeartbeatBroadcaster(serverAgentService.BroadcastUptimeHeartbeat)
	githubService := githubmodule.New(cfg)
	githubService.SetNotifier(notifyService)
	drawioService := drawiomodule.New(cfg)
	promptsService := promptsmodule.New(cfg)
	systemService := systemmetrics.New(cfg)
	systemService.SetNotifier(notifyService)
	backupService := backup.New(cfg)
	backupService.SetNotifier(notifyService)
	settingsService := settings.New(cfg)
	settingsService.StartBackgroundCleanup()
	server := &Server{
		cfg:      cfg,
		auth:     authService,
		settings: settingsService,
		system:   systemService,
		totp:     totp.New(cfg),
		cron:     cronService,
		filebox:  filebox.New(cfg, authService),
		notify:   notifyService,
		uptime:   uptimeService,
		koyeb:    koyeb.New(cfg),
		flyio:    flyio.New(cfg),
		onepanel: onepanel.New(cfg),
		github:   githubService,
		aliyun:   aliyun.New(cfg),
		tencent:  tencent.New(cfg),
		oracle:   oracle.New(cfg),
		cf:       cloudflareService,
		m365:     m365.New(cfg),
		openai:   openai.New(cfg),
		server:   serverAgentService,
		backup:   backupService,
		logs:     systemlogs.New(cfg),
		sub:      subscriptionService,
		drawio:   drawioService,
		prompts:  promptsService,
	}
	server.onepanel.SetAgentRunner(serverAgentService)
	systemService.SetAICaller(server.callAPIFromAI)
	// 启动代理池预热：预建立各代理到上游的连接，缓解首次请求冷启动握手延迟。
	warmupCtx, warmupCancel := context.WithCancel(context.Background())
	server.warmupCancel = warmupCancel
	server.openai.StartWarmup(warmupCtx)
	return server, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.warmupCancel != nil {
		s.warmupCancel()
	}
	if s.server != nil {
		s.server.Stop()
	}
	if s.settings != nil {
		s.settings.Stop()
	}
	if s.uptime != nil {
		s.uptime.Stop()
	}
	if s.system != nil {
		s.system.Shutdown()
	}
	if s.github != nil {
		s.github.Stop()
	}
	if s.drawio != nil {
		s.drawio.Stop()
	}
	if s.cron == nil {
		return nil
	}
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.applySecurityHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if s.serveSystemControlRoute(w, r) {
		return
	}

	if strings.HasPrefix(r.URL.Path, "/sub/") {
		s.system.RecordAPICall(r.Method, r.URL.Path)
		s.sub.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/server/public/") {
		s.system.RecordAPICall(r.Method, r.URL.Path)
		s.server.ServeHTTP(w, r)
		return
	}

	if route, ok := manifest.Match(r.URL.EscapedPath()); ok {
		switch route.Owner {
		case manifest.OwnerGo:
			if !s.authorizeGoRoute(w, r, route) {
				return
			}
			s.system.RecordAPICall(r.Method, r.URL.Path)
			s.serveGoRoute(w, r, route)
		case manifest.OwnerRetired:
			// 友好的错误信息，区分已实现和未实现的功能
			if strings.HasPrefix(route.Prefix, "/api/server/metrics/") ||
				strings.HasPrefix(route.Prefix, "/api/server/tasks") ||
				strings.HasPrefix(route.Prefix, "/api/server/network-quality/") {
				response.JSON(w, http.StatusNotImplemented, map[string]interface{}{
					"success": false,
					"error":   "功能开发中，即将上线",
					"module":  route.Module,
				})
			} else {
				response.Error(w, http.StatusGone, "模块已停用: "+route.Module)
			}
		default:
			response.Error(w, http.StatusNotFound, "路由未找到")
		}
		return
	}

	// 未匹配的 API 路由返回 404
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
		response.Error(w, http.StatusNotFound, "API 路由不存在")
		return
	}

	s.serveStatic(w, r)
}

func (s *Server) serveSystemControlRoute(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	isSessionSystemRoute := path == "/api/system/api-docs" ||
		path == "/api/openapi.json" ||
		path == "/api/system/openapi.json" ||
		strings.HasPrefix(path, "/api/api-keys") ||
		strings.HasPrefix(path, "/api/system/api-keys") ||
		path == "/api/ai-access" ||
		path == "/api/ai-access/key/rotate" ||
		path == "/api/ai-access/write" ||
		path == "/api/ai-access/audit" ||
		path == "/api/ai-access/audit/clear" ||
		path == "/api/ai-access/mcp-servers" ||
		path == "/api/ai-access/skills" ||
		strings.HasPrefix(path, "/api/ai-access/mcp-servers/") ||
		strings.HasPrefix(path, "/api/ai-access/skills/") ||
		path == "/api/system/ai-access" ||
		path == "/api/system/ai-access/key/rotate" ||
		path == "/api/system/ai-access/write" ||
		path == "/api/system/ai-access/audit" ||
		path == "/api/system/ai-access/audit/clear" ||
		path == "/api/system/ai-access/mcp-servers" ||
		path == "/api/system/ai-access/skills" ||
		strings.HasPrefix(path, "/api/system/ai-access/mcp-servers/") ||
		strings.HasPrefix(path, "/api/system/ai-access/skills/")
	if isSessionSystemRoute {
		route := manifest.Route{Prefix: path, Module: "system-control", Owner: manifest.OwnerGo, Auth: manifest.AuthSession}
		if !s.authorizeGoRoute(w, r, route) {
			return true
		}
		s.system.RecordAPICall(r.Method, r.URL.Path)
		s.system.ServeHTTP(w, r)
		return true
	}
	if path == "/api/ai/manifest" || path == "/api/ai/mcp" {
		s.system.RecordAPICall(r.Method, r.URL.Path)
		s.system.ServeHTTP(w, r)
		return true
	}
	return false
}

func (s *Server) authorizeGoRoute(w http.ResponseWriter, r *http.Request, route manifest.Route) bool {
	if route.Auth == manifest.AuthAPIKey && (route.Module == "openai-compatible" || route.Module == "anthropic-compatible") {
		authorizedRequest, err := s.openai.AuthorizeGatewayRequest(r)
		if err != nil {
			response.JSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": map[string]string{
					"message": err.Error(),
					"type":    "authentication_error",
				},
			})
			return false
		}
		*r = *authorizedRequest
		return true
	}
	if route.Auth != manifest.AuthSession {
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/totp/accounts" {
		pluginAuthorized, err := s.auth.IsPluginToken(r.Context(), r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return false
		}
		if pluginAuthorized {
			return true
		}
	}
	if hasAPIKeyCredential(r) && !apiKeyRequiresSession(r.URL.Path) {
		write := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		keyAuthorized, err := s.auth.IsAPIKeyAuthorized(r.Context(), r, write)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return false
		}
		if keyAuthorized {
			return true
		}
	}
	if _, err := r.Cookie("sid"); err == nil && !s.sameOriginRequest(r) {
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "跨来源会话请求已拒绝"})
		return false
	}
	ok, err := s.auth.IsAuthenticated(r.Context(), r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "请先登录"})
		return false
	}
	return true
}

func apiKeyRequiresSession(path string) bool {
	protectedPrefixes := []string{
		"/api/api-keys",
		"/api/system/api-keys",
		"/api/ai-access",
		"/api/system/ai-access",
		"/api/backup",
		"/api/totp",
		"/api/cron",
		"/api/scheduler",
	}
	for _, prefix := range protectedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	protectedSettings := map[string]bool{
		"/api/settings/database/import":           true,
		"/api/settings/import-database":           true,
		"/api/settings/export-database":           true,
		"/api/settings/cleanup-deprecated-tables": true,
	}
	return protectedSettings[path]
}

func hasAPIKeyCredential(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("Authorization")) != "" || strings.TrimSpace(r.Header.Get("X-API-Key")) != ""
}

func (s *Server) serveGoRoute(w http.ResponseWriter, r *http.Request, route manifest.Route) {
	if strings.HasPrefix(route.Prefix, "/api/auth") {
		s.auth.ServeHTTP(w, r)
		return
	}

	switch route.Prefix {
	case "/health":
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"status":    "ok",
			"service":   "api-monitor-go",
			"version":   s.cfg.Version,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	case "/api/migration/status":
		response.OK(w, map[string]interface{}{
			"version":       s.cfg.Version,
			"databasePath":  s.cfg.DatabasePath(),
			"legacyEnabled": false,
			"routeSummary":  manifest.Summary(),
			"routes":        manifest.Routes(),
			"retired":       []string{},
		})
	case "/api/settings", "/api/settings/site-brand/icons", "/api/settings/site-brand/icons/{id}", "/api/settings/database-stats", "/api/settings/migration-self-check", "/api/settings/database-analysis", "/api/settings/deprecated-tables", "/api/settings/cleanup-deprecated-tables", "/api/settings/export-database", "/api/settings/database/import", "/api/settings/import-database", "/api/settings/operation-logs", "/api/settings/sys-logs", "/api/settings/app-log-file", "/api/settings/log-settings", "/api/settings/clear-app-logs", "/api/settings/vacuum-database", "/api/settings/clear-logs", "/api/settings/enforce-log-limits", "/api/settings/clear-chat-messages":
		s.settings.ServeHTTP(w, r)
	case "/api/system/host-metrics", "/api/system/api-stats", "/api/system/api-docs", "/api/system/openapi.json", "/api/api-keys", "/api/system/api-keys", "/api/system/ai-access/key/rotate", "/api/system/ai-access/write", "/api/system/ai-access/audit", "/api/system/ai-access/mcp-servers/{id}", "/api/system/ai-access/mcp-servers", "/api/system/ai-access/skills/{id}", "/api/system/ai-access/skills", "/api/system/ai-access/audit/clear", "/api/system/ai-access", "/api/ai-access/key/rotate", "/api/ai-access/write", "/api/ai-access/audit", "/api/ai-access/mcp-servers/{id}", "/api/ai-access/mcp-servers", "/api/ai-access/skills/{id}", "/api/ai-access/skills", "/api/ai-access/audit/clear", "/api/ai-access", "/api/ai/manifest", "/api/ai/mcp":
		s.system.ServeHTTP(w, r)
	case "/api/system/logs/stream", "/api/system/logs/download":
		s.logs.ServeHTTP(w, r)
	case "/api/backup":
		s.backup.ServeHTTP(w, r)
	case "/api/totp":
		s.totp.ServeHTTP(w, r)
	case "/api/cron", "/api/scheduler":
		s.cron.ServeHTTP(w, r)
	case "/api/filebox":
		s.filebox.ServeHTTP(w, r)
	case "/api/uptime":
		s.uptime.ServeHTTP(w, r)
	case "/api/notification":
		s.notify.ServeHTTP(w, r)
	case "/api/koyeb":
		s.koyeb.ServeHTTP(w, r)
	case "/api/flyio":
		s.flyio.ServeHTTP(w, r)
	case "/api/onepanel", "/api/onepanel/config", "/api/onepanel/spec":
		s.onepanel.ServeHTTP(w, r)
	case "/api/github", "/api/github/webhook/{repositoryId}", "/api/github/webhook", "/api/github/events/stream":
		s.github.ServeHTTP(w, r)
	case "/api/drawio", "/api/drawio/documents", "/api/drawio/documents/{id}", "/api/drawio/documents/{id}/clone", "/api/drawio/documents/{id}/draft", "/api/drawio/documents/{id}/export", "/api/drawio/documents/{id}/versions", "/api/drawio/documents/{id}/versions/{versionId}", "/api/drawio/documents/{id}/versions/{versionId}/restore", "/api/drawio/documents/{id}/thumbnails/rebuild", "/api/drawio/import", "/api/drawio/thumbnails/rebuild", "/api/drawio/render-jobs", "/api/drawio/settings":
		s.drawio.ServeHTTP(w, r)
	case "/api/prompts", "/api/prompts/collections", "/api/prompts/collections/{id}", "/api/prompts/entries", "/api/prompts/entries/{id}", "/api/prompts/entries/{id}/duplicate", "/api/prompts/entries/{id}/draft", "/api/prompts/entries/{id}/publish", "/api/prompts/entries/{id}/versions", "/api/prompts/entries/{id}/versions/{versionId}", "/api/prompts/entries/{id}/versions/{versionId}/restore", "/api/prompts/entries/{id}/public/regenerate", "/api/prompts/settings":
		s.prompts.ServeHTTP(w, r)
	case "/api/prompts/public/{publicId}", "/api/prompts/d/{publicId}", "/api/prompts/d/{publicId}/versions/{versionNo}":
		s.prompts.ServePublic(w, r)
	case "/api/aliyun":
		s.aliyun.ServeHTTP(w, r)
	case "/api/tencent":
		s.tencent.ServeHTTP(w, r)
	case "/api/oracle":
		s.oracle.ServeHTTP(w, r)
	case "/api/m365":
		s.m365.ServeHTTP(w, r)
	case "/api/cloudflare/accounts", "/api/cloudflare/accounts/export", "/api/cloudflare/export/accounts", "/api/cloudflare/import/accounts", "/api/cloudflare/templates", "/api/cloudflare/templates/{id}", "/api/cloudflare/templates/{templateId}/apply", "/api/cloudflare/import/templates", "/api/cloudflare/accounts/{id}", "/api/cloudflare/accounts/{id}/verify", "/api/cloudflare/accounts/{id}/token", "/api/cloudflare/accounts/{id}/cf-account-id", "/api/cloudflare/accounts/{id}/pages", "/api/cloudflare/accounts/{id}/pages/{projectName}", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments/{deploymentId}", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains/{domain}", "/api/cloudflare/accounts/{id}/workers", "/api/cloudflare/accounts/{id}/workers/{scriptName}", "/api/cloudflare/accounts/{id}/workers/{scriptName}/toggle", "/api/cloudflare/accounts/{id}/workers/{scriptName}/analytics", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains/{domainId}", "/api/cloudflare/accounts/{accountId}/r2/buckets", "/api/cloudflare/accounts/{accountId}/r2/metrics", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download-info", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/folder-download", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/preview", "/api/cloudflare/accounts/{id}/tunnels", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/configuration", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/token", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/connections", "/api/cloudflare/record-types", "/api/cloudflare/zones", "/api/cloudflare/accounts/{id}/zones", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes/{routeId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/analytics", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/switch", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/batch":
		s.cf.ServeHTTP(w, r)
	case "/api/openai":
		s.openai.ServeHTTP(w, r)
	case "/api/subscription":
		s.sub.ServeHTTP(w, r)
	case "/sub/{token}":
		s.sub.ServeHTTP(w, r)
	case "/v1":
		s.serveV1Route(w, r)
	case "/v1/messages":
		s.serveV1Route(w, r)
	case "/ws/ssh", "/ws/agent-terminal":
		s.server.ServeHTTP(w, r)
	case "/socket.io/":
		s.server.ServeHTTP(w, r)
	default:
		if strings.HasPrefix(route.Prefix, "/sub/") || strings.HasPrefix(r.URL.Path, "/sub/") {
			s.sub.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/m365") {
			s.m365.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/server/") {
			s.server.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/github") {
			s.github.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/drawio") {
			s.drawio.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/prompts") {
			s.prompts.ServeHTTP(w, r)
			return
		}
		response.Error(w, http.StatusNotFound, "go route not implemented: "+route.Prefix)
	}
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.settings != nil && s.settings.ServePublicSiteBrandIconAsset(w, r) {
		return
	}

	if s.servePublicPageFavicon(w, r) {
		return
	}

	// 尝试从 dist 和 public 查找文件
	foundInDist := s.tryServeFile(w, r, s.cfg.DistDir)
	if foundInDist {
		return
	}

	foundInPublic := s.tryServeFile(w, r, s.cfg.PublicDir)
	if foundInPublic {
		return
	}

	if s.tryServeAssetFallback(w, r) {
		return
	}

	// 如果是根路径或没有文件扩展名（可能是 SPA 路由），返回 index.html
	cleanPath, ok := cleanStaticRequestPath(r.URL.EscapedPath())
	if !ok {
		response.Error(w, http.StatusNotFound, "static asset not found")
		return
	}
	ext := filepath.Ext(cleanPath)
	if cleanPath == "." || cleanPath == "" || ext == "" {
		indexPath := filepath.Join(s.cfg.DistDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			setStaticCacheHeaders(w, "index.html")
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	response.Error(w, http.StatusNotFound, "static asset not found")
}

// servePublicPageFavicon 处理公开页 favicon 解析端点：
//
//	/public-page-favicon/{kind}/{slug}   kind 为 uptime/server/github
//	/public-page-favicon/domain/{host}   按域名探测三类公开页
//
// 浏览器首次拉取 favicon 时就能拿到正确的图标，避免「默认图标 → 自定义图标」的闪变。
// 自定义图标 302 到 /site-brand-icons/{id}（已有不可变缓存）；未自定义直接返回
// 该类型默认 glyph；页面不存在或解析失败时回退到站点默认 logo。
func (s *Server) servePublicPageFavicon(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	// 直接基于 r.URL.Path 解析分段；不走 cleanStaticRequestPath，
	// 因为 filepath.Clean 在 Windows 上会引入反斜杠破坏前缀匹配。
	cleanPath := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasPrefix(cleanPath, "public-page-favicon/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(cleanPath, "public-page-favicon/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	kind, lookup := parts[0], parts[1]

	var iconID, resolvedKind string
	var found bool
	var err error
	ctx := r.Context()
	switch kind {
	case publicpageicon.KindUptime:
		iconID, found, err = s.uptime.PublicPageIconID(ctx, lookup, false)
	case publicpageicon.KindServer:
		iconID, found, err = s.server.PublicPageIconID(ctx, lookup, false)
	case publicpageicon.KindGitHub:
		iconID, found, err = s.github.PublicPageIconID(ctx, lookup, false)
	case "domain":
		resolvedKind, iconID, found, err = s.publicPageFaviconByDomain(ctx, lookup)
	default:
		return false
	}
	if err != nil || !found {
		http.Redirect(w, r, "/logo-default.svg", http.StatusTemporaryRedirect)
		return true
	}
	// 仅当图标 ID 是安全路径格式时才 302 到品牌图标资产；
	// 否则视为未配置，直接返回默认 glyph。
	if iconID != "" && publicpageicon.ValidIconID(iconID) {
		w.Header().Set("Cache-Control", "public, max-age=600")
		http.Redirect(w, r, "/site-brand-icons/"+iconID, http.StatusTemporaryRedirect)
		return true
	}
	if resolvedKind == "" {
		resolvedKind = kind
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(publicpageicon.DefaultGlyphSVG(resolvedKind)))
	return true
}

// publicPageFaviconByDomain 与前端 DomainPublicStatusResolver 的探测顺序一致：
// uptime → server → github。
func (s *Server) publicPageFaviconByDomain(ctx context.Context, host string) (kind, iconID string, found bool, err error) {
	lookups := []struct {
		kind string
		svc  func(context.Context, string, bool) (string, bool, error)
	}{
		{publicpageicon.KindUptime, s.uptime.PublicPageIconID},
		{publicpageicon.KindServer, s.server.PublicPageIconID},
		{publicpageicon.KindGitHub, s.github.PublicPageIconID},
	}
	for _, lookup := range lookups {
		iconID, ok, lookupErr := lookup.svc(ctx, host, true)
		if lookupErr != nil {
			return "", "", false, lookupErr
		}
		if ok {
			return lookup.kind, iconID, true, nil
		}
	}
	return "", "", false, nil
}

func (s *Server) tryServeFile(w http.ResponseWriter, r *http.Request, dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	cleanPath, ok := cleanStaticRequestPath(r.URL.EscapedPath())
	if !ok {
		return false
	}
	candidate, ok := joinStaticPath(dir, cleanPath)
	if !ok {
		return false
	}

	fileInfo, err := os.Stat(candidate)
	if err != nil {
		return false
	}
	if !fileInfo.IsDir() {
		setStaticCacheHeaders(w, cleanPath)
		http.ServeFile(w, r, candidate)
		return true
	}

	indexPath := filepath.Join(candidate, "index.html")
	indexInfo, err := os.Stat(indexPath)
	if err != nil || indexInfo.IsDir() {
		return false
	}
	if !strings.HasSuffix(r.URL.Path, "/") {
		redirectURL := *r.URL
		redirectURL.Path += "/"
		redirectURL.RawPath = ""
		http.Redirect(w, r, redirectURL.String(), http.StatusPermanentRedirect)
		return true
	}
	setStaticCacheHeaders(w, filepath.Join(cleanPath, "index.html"))
	http.ServeFile(w, r, indexPath)
	return true

}

func setStaticCacheHeaders(w http.ResponseWriter, cleanPath string) {
	cleanPath = filepath.ToSlash(cleanPath)
	if filepath.Base(cleanPath) == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}
	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
}

func (s *Server) tryServeAssetFallback(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(s.cfg.DistDir) == "" {
		return false
	}

	cleanPath, ok := cleanStaticRequestPath(r.URL.EscapedPath())
	if !ok {
		return false
	}
	switch cleanPath {
	case "logo-default.svg", "logo.svg":
		matches, err := filepath.Glob(filepath.Join(s.cfg.DistDir, "assets", "logo-*.svg"))
		if err != nil || len(matches) == 0 {
			return false
		}
		http.ServeFile(w, r, matches[0])
		return true
	case "favicon.ico":
		matches, err := filepath.Glob(filepath.Join(s.cfg.DistDir, "assets", "logo-*.svg"))
		if err != nil || len(matches) == 0 {
			return false
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.Redirect(w, r, fmt.Sprintf("/%s", filepath.ToSlash(filepath.Join("assets", filepath.Base(matches[0])))), http.StatusTemporaryRedirect)
		return true
	default:
		return false
	}
}

func cleanStaticRequestPath(escapedPath string) (string, bool) {
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", false
	}
	normalized := strings.ReplaceAll(decoded, "\\", "/")
	normalized = strings.TrimPrefix(normalized, "/")
	if normalized == "" || normalized == "." {
		return "index.html", true
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return "", false
		}
	}
	cleanPath := filepath.Clean(filepath.FromSlash(normalized))
	if cleanPath == "." || cleanPath == "" {
		return "index.html", true
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) || filepath.IsAbs(cleanPath) || filepath.VolumeName(cleanPath) != "" {
		return "", false
	}
	return cleanPath, true
}

func joinStaticPath(rootDir, relPath string) (string, bool) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(root, relPath))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return candidate, true
}

func (s *Server) applySecurityHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(r.URL.Path, "/vendor/drawio/") {
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	} else {
		w.Header().Set("X-Frame-Options", "DENY")
	}
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s.cfg.IsProduction() {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	for _, allowed := range s.cfg.CORSAllowedOrigins {
		if origin != "" && origin == allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-API-Key,X-Agent-Key,X-Server-ID,X-Filebox-Password")
			w.Header().Add("Vary", "Origin")
			break
		}
	}
}

func (s *Server) sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(parsed.Host, s.originCheckHost(r)) {
		return true
	}
	if !s.cfg.IsProduction() && s.trustForwardedHost(r) && isDevelopmentOriginHost(parsed.Hostname()) {
		return true
	}
	return false
}

func (s *Server) originCheckHost(r *http.Request) string {
	if s.trustForwardedHost(r) {
		if forwarded := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			return forwarded
		}
	}
	return r.Host
}

func (s *Server) trustForwardedHost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if !s.cfg.IsProduction() && ip.IsLoopback() {
		return true
	}
	return s.isTrustedProxy(ip)
}

func (s *Server) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, entry := range s.cfg.TrustedProxyCIDRs {
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func (s *Server) gatewayClientIP(r *http.Request) string {
	direct := ""
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		direct = host
	} else if r.RemoteAddr != "" {
		direct = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	if direct == "" {
		return ""
	}
	ip := net.ParseIP(direct)
	trusted := false
	if ip != nil {
		if s.isTrustedProxy(ip) {
			trusted = true
		} else if !s.cfg.IsProduction() && ip.IsLoopback() {
			trusted = true
		}
	}
	if trusted {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			candidate := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
		if candidate := strings.TrimSpace(r.Header.Get("X-Real-IP")); candidate != "" {
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
	}
	return direct
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func isDevelopmentOriginHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func (s *Server) serveV1Route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method
	startedAt := time.Now()
	clientIP := s.gatewayClientIP(r)

	// 1. Models endpoint
	if method == http.MethodGet && (path == "/v1/models" || path == "/v1/model") {
		s.serveV1Models(w, r)
		return
	}

	// 2. Completions endpoint
	if method == http.MethodPost && path == "/v1/chat/completions" {
		s.openai.ServeHTTP(w, r)
		return
	}

	// 3. Responses endpoint（OpenAI Responses API，/v1/responses）
	if method == http.MethodPost && path == "/v1/responses" {
		s.openai.ServeHTTP(w, r)
		return
	}

	// 4. Anthropic Messages endpoint（/v1/messages）
	if method == http.MethodPost && path == "/v1/messages" {
		s.openai.ServeHTTP(w, r)
		return
	}

	response.Error(w, http.StatusNotFound, "v1 endpoint not found")
	s.openai.RecordAnalytics(r.Context(), strings.TrimPrefix(path, "/v1/"), "", "", http.StatusNotFound, time.Since(startedAt).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "")
}

func (s *Server) serveV1Models(w http.ResponseWriter, r *http.Request) int {
	ctx := r.Context()
	var mergedModels []map[string]interface{}

	if oaiModels, err := s.openai.GetModelsList(ctx); err == nil {
		mergedModels = append(mergedModels, oaiModels...)
	} else {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return http.StatusInternalServerError
	}

	sort.Slice(mergedModels, func(i, j int) bool {
		idI, _ := mergedModels[i]["id"].(string)
		idJ, _ := mergedModels[j]["id"].(string)
		return idI < idJ
	})

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   mergedModels,
	})
	return http.StatusOK
}
