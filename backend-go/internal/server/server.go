package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/iwvw/api-monitor/backend-go/internal/antigravity"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api"
	"github.com/iwvw/api-monitor/backend-go/internal/proxypool"
	"github.com/iwvw/api-monitor/backend-go/internal/oracle"
	"github.com/iwvw/api-monitor/backend-go/internal/gcp"
	originpkg "github.com/iwvw/api-monitor/backend-go/internal/origin"
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

	"github.com/iwvw/api-monitor/backend-go/internal/adminai"
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
	gcp      *gcp.Service
	cf       *cloudflare.Service
	m365     *m365.Service
	openai   *openai.Service
	antigravity *antigravity.Service
	ds2api   *ds2api.Service
	proxypool *proxypool.Service
	server   *serveragent.Service
	backup   *backup.Service
	logs     *systemlogs.Service
	sub      *subscription.Service
	drawio   *drawiomodule.Service
	prompts  *promptsmodule.Service
	adminai  *adminai.Service

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
	cronService.SetNotifier(notifyService)
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
	settingsService.StartWALMaintenance()
	adminaiService := adminai.New(cfg)
	adminaiService.SetNotificationSource(notifyService)
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
		gcp:      gcp.New(cfg),
		cf:       cloudflareService,
		m365:     m365.New(cfg),
		openai:   openai.New(cfg),
		antigravity: antigravity.New(cfg),
		ds2api:   ds2api.New(cfg),
		proxypool: proxypool.New(cfg),
		server:   serverAgentService,
		backup:   backupService,
		logs:     systemlogs.New(cfg),
		sub:      subscriptionService,
		drawio:   drawioService,
		prompts:  promptsService,
		adminai:  adminaiService,
	}
	server.onepanel.SetAgentRunner(serverAgentService)
	server.filebox.SetNodeProvider(serverAgentService)
	systemService.SetAICaller(server.callAPIFromAI)
	adminaiService.SetAICaller(server.callAPIFromAI)
	// 管理 AI：启动审批超时清理 goroutine + 频道注册（PRD-03/04）
	adminaiService.StartBackground()
	adminaiService.SetupChannels()
	// 启动代理池预热：预建立各代理到上游的连接，缓解首次请求冷启动握手延迟。
	warmupCtx, warmupCancel := context.WithCancel(context.Background())
	server.warmupCancel = warmupCancel
	server.openai.SetNotifier(notifyService)
	// 注入独立代理池选择器：端点配置 proxy_pool_id 时复用插件管理的池与健康数据。
	server.openai.SetProxyPoolSelector(server.proxypool)
	// Antigravity 插件可引用独立代理池作为出网出口。
	server.antigravity.SetProxyPoolSelector(server.proxypool)
	// Antigravity 插件配额刷新检测：上报事件走统一通知中心。
	server.antigravity.SetNotifier(notifyService)
	// DS2API 插件可引用独立代理池作为出网出口。
	server.ds2api.SetProxyPoolSelector(server.proxypool)
	server.openai.StartWarmup(warmupCtx)
	// 启动网关健康告警监测（错误率过高/恢复触发通知）。
	server.openai.StartAlertMonitor(warmupCtx)
	// 启动上游模型列表每小时自动刷新（后台默认开启，无需前端展示）。
	server.openai.StartModelAutoRefresh(warmupCtx)
	// 启动 Antigravity 配额刷新检测（开关由前端控制，关闭时后台静默跳过）。
	server.antigravity.StartQuotaMonitor(warmupCtx)
	// 启动两个插件的调用次数定期落盘（重启保留，ctx 取消时补最后一次落盘）。
	server.ds2api.StartCallStatsFlush(warmupCtx)
	server.antigravity.StartCallStatsFlush(warmupCtx)
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
	if s.adminai != nil {
		// 停止审批清理 goroutine 与频道轮询（runs 通道由 RunLoop 结束时的 defer 清理）
		s.adminai.StopBackground()
		s.adminai.StopAllChannels()
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

	if strings.HasPrefix(r.URL.Path, "/share/") {
		code := strings.TrimPrefix(r.URL.Path, "/share/")
		code = strings.Trim(code, "/")
		if code != "" && !strings.Contains(code, "/") {
			if s.filebox.HandleShareRedirect(w, r, code) {
				return
			}
		}
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
	// 本机定时任务内部调用（cronjobs internal 任务）：仅放行登记的内部接口，
	// 且来源必须是本机回环地址，防止外部伪造 X-Internal-Cron 头绕过会话鉴权。
	// 方法级校验限制为只读动作（admin-ai cron 回调除外），杜绝无会话写操作。
	if r.Header.Get("X-Internal-Cron") == "true" && isLoopbackRemoteAddr(r.RemoteAddr) && isInternalCronRoute(r.URL.Path) && s.internalCronAllowsMethod(r) {
		return true
	}
	// 仅本机内部调用：网关转发等 loopback 来源免密钥放行，外部一律拒绝。
	// 适用于插件兼容中继（antigravity/ds2api /v1），避免把独立兼容端点暴露到公网。
	if route.Auth == manifest.AuthInternal {
		if isLoopbackRemoteAddr(r.RemoteAddr) {
			return true
		}
		response.JSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "error": "该接口仅限本机内部调用"})
		return false
	}
	if route.Auth == manifest.AuthAPIKey && (route.Module == "openai-compatible" || route.Module == "anthropic-compatible" || route.Module == "antigravity-compatible" || route.Module == "ds2api-compatible") {
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
		"/api/settings/export-database":           true,
		"/api/settings/cleanup-deprecated-tables": true,
	}
	return protectedSettings[path]
}

// isLoopbackRemoteAddr 判断请求来源是否为本机回环地址。
func isLoopbackRemoteAddr(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isInternalCronRoute 判断路径是否为登记的本机定时任务内部接口。
// 仅放行本机 cron 任务（经 loopback + X-Internal-Cron 防伪造）可读取的接口：
//   - 精确登记的系统只读接口与 admin-ai cron 回调；
//   - 带模块级二次鉴权的模块（uptime/filebox）不在白名单内，属预期安全边界。
//
// 该方法判定路径是否可放行；方法级校验（仅 GET 等只读动作）由调用方
// internalCronAllowsMethod 完成，避免白名单接口被用于写操作。
func isInternalCronRoute(path string) bool {
	switch path {
	case "/api/admin-ai/cron/daily-briefing", "/api/admin-ai/cron/task-run",
		"/api/system/host-metrics", "/api/system/api-stats",
		"/api/system/api-docs", "/api/system/openapi.json", "/api/openapi.json",
		"/api/migration/status", "/api/server/s":
		return true
	}
	// 只读 GET 业务家族：无模块级二次鉴权，prefix 前缀 + 调用方 GET 校验兜底。
	for _, prefix := range internalCronReadonlyPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// internalCronAllowsMethod 校验 cron 内部调用允许的方法：除 admin-ai cron 回调外，
// 业务只读家族只放行 GET/HEAD（防止 cron 无会话写操作）。
func (s *Server) internalCronAllowsMethod(r *http.Request) bool {
	path := r.URL.Path
	switch path {
	case "/api/admin-ai/cron/task-run":
		return r.Method == http.MethodPost
	case "/api/admin-ai/cron/daily-briefing":
		return r.Method == http.MethodGet
	}
	for _, prefix := range internalCronReadonlyPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return r.Method == http.MethodGet || r.Method == http.MethodHead
		}
	}
	if isInternalCronRoute(path) {
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return false
}

// internalCronReadonlyPrefixes 是 cron 内部任务可只读访问的模块家族前缀。
// 仅收录无模块级二次鉴权、GET 语义为读取/列表/统计的模块；uptime/filebox
// 自带模块级会话校验，不在此列。
var internalCronReadonlyPrefixes = []string{
	"/api/system",
	"/api/cloudflare/zones",
	"/api/openai/analytics",
	"/api/openai/endpoints",
	"/api/totp",
	"/api/notification",
	"/api/scheduler",
	"/api/backup",
	"/api/aliyun",
	"/api/tencent",
	"/api/flyio",
	"/api/koyeb",
	"/api/github",
	"/api/drawio",
	"/api/prompts",
	"/api/server",
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
			"goVersion": runtime.Version(),
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
	case "/api/settings", "/api/settings/site-brand/icons", "/api/settings/site-brand/icons/{id}", "/api/settings/database-stats", "/api/settings/migration-self-check", "/api/settings/database-analysis", "/api/settings/deprecated-tables", "/api/settings/cleanup-deprecated-tables", "/api/settings/export-database", "/api/settings/database/import", "/api/settings/operation-logs", "/api/settings/sys-logs", "/api/settings/app-log-file", "/api/settings/log-settings", "/api/settings/clear-app-logs", "/api/settings/vacuum-database", "/api/settings/clear-logs", "/api/settings/enforce-log-limits":
		s.settings.ServeHTTP(w, r)
	case "/api/system/host-metrics", "/api/system/api-stats", "/api/system/api-docs", "/api/system/openapi.json", "/api/openapi.json", "/api/system/status/stream", "/api/api-keys", "/api/system/api-keys", "/api/system/ai-access/key/rotate", "/api/system/ai-access/write", "/api/system/ai-access/policy", "/api/system/ai-access/audit", "/api/system/ai-access/mcp-servers/{id}", "/api/system/ai-access/mcp-servers", "/api/system/ai-access/skills/{id}", "/api/system/ai-access/skills", "/api/system/ai-access/audit/clear", "/api/system/ai-access", "/api/ai-access/key/rotate", "/api/ai-access/write", "/api/ai-access/policy", "/api/ai-access/audit", "/api/ai-access/mcp-servers/{id}", "/api/ai-access/mcp-servers", "/api/ai-access/skills/{id}", "/api/ai-access/skills", "/api/ai-access/audit/clear", "/api/ai-access", "/api/ai/manifest", "/api/ai/mcp":
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
	case "/api/gcp":
		s.gcp.ServeHTTP(w, r)
	case "/api/m365":
		s.m365.ServeHTTP(w, r)
	case "/api/cloudflare/accounts", "/api/cloudflare/accounts/export", "/api/cloudflare/export/accounts", "/api/cloudflare/import/accounts", "/api/cloudflare/templates", "/api/cloudflare/templates/{id}", "/api/cloudflare/templates/{templateId}/apply", "/api/cloudflare/import/templates", "/api/cloudflare/accounts/{id}", "/api/cloudflare/accounts/{id}/verify", "/api/cloudflare/accounts/{id}/token", "/api/cloudflare/accounts/{id}/cf-account-id", "/api/cloudflare/accounts/{id}/pages", "/api/cloudflare/accounts/{id}/pages/{projectName}", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments/{deploymentId}", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains/{domain}", "/api/cloudflare/accounts/{id}/workers", "/api/cloudflare/accounts/{id}/workers/{scriptName}", "/api/cloudflare/accounts/{id}/workers/{scriptName}/toggle", "/api/cloudflare/accounts/{id}/workers/{scriptName}/analytics", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains/{domainId}", "/api/cloudflare/accounts/{accountId}/r2/buckets", "/api/cloudflare/accounts/{accountId}/r2/metrics", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download-info", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/folder-download", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/preview", "/api/cloudflare/accounts/{id}/tunnels", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/configuration", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/token", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/connections", "/api/cloudflare/record-types", "/api/cloudflare/zones", "/api/cloudflare/accounts/{id}/zones", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes/{routeId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/analytics", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/switch", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/batch":
		s.cf.ServeHTTP(w, r)
	case "/api/openai":
		s.openai.ServeHTTP(w, r)
	case "/api/proxypool":
		s.proxypool.ServeHTTP(w, r)
	case "/api/antigravity", "/api/antigravity/v1":
		s.antigravity.ServeHTTP(w, r)
	case "/api/ds2api", "/api/ds2api/v1":
		s.ds2api.ServeHTTP(w, r)
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
	case "/api/admin-ai", "/api/admin-ai/cron/daily-briefing", "/api/admin-ai/cron/task-run", "/api/admin-ai/sessions", "/api/admin-ai/sessions/{id}", "/api/admin-ai/sessions/{id}/messages", "/api/admin-ai/messages", "/api/admin-ai/messages/stream", "/api/admin-ai/cancel", "/api/admin-ai/channels", "/api/admin-ai/channels/{id}", "/api/admin-ai/channels/{id}/start", "/api/admin-ai/channels/{id}/stop", "/api/admin-ai/channels/{id}/status", "/api/admin-ai/channels/{id}/wechat/qrcode", "/api/admin-ai/channels/{id}/wechat/qrcode/status", "/api/admin-ai/channel-bindings", "/api/admin-ai/channel-bindings/{id}", "/api/admin-ai/approvals", "/api/admin-ai/approvals/{id}", "/api/admin-ai/approvals/{id}/resolve", "/api/admin-ai/audit", "/api/admin-ai/settings", "/api/admin-ai/memories", "/api/admin-ai/memories/{id}":
		s.adminai.ServeHTTP(w, r)
	default:
		if strings.HasPrefix(route.Prefix, "/sub/") || strings.HasPrefix(r.URL.Path, "/sub/") {
			s.sub.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(route.Prefix, "/api/subscription") {
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
		return
	}
	if strings.HasPrefix(cleanPath, "fonts/") || cleanPath == "logo.svg" || cleanPath == "logo-default.svg" || cleanPath == "robots.txt" || cleanPath == "llms.txt" {
		w.Header().Set("Cache-Control", "public, max-age=2592000")
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
	if isGatewayRoute(r.URL.Path) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-API-Key,X-Client-Version")
		return
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

func isGatewayRoute(path string) bool {
	return strings.HasPrefix(path, "/v1/") || path == "/v1"
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
	if !s.cfg.IsProduction() && s.trustForwardedHost(r) {
		// 开发模式下本机直接放行：包装 App / 内嵌 WebView 的本地开发来源
		// （localhost、内网地址、初始化时的 5173 代理），避免本地联调受阻。
		if originpkg.IsDevelopmentOriginHost(parsed.Hostname()) {
			return true
		}
		// 包装环境来源（Origin: null、app:// 等自定义 scheme）只能由用户自己的
		// 嵌入容器产生，公网站点无法伪造，放行不构成 CSRF 风险。
		if originpkg.IsEmbeddedWrapperOrigin(origin) {
			return true
		}
	}
	return s.originAllowedByConfig(origin)
}

func (s *Server) originAllowedByConfig(origin string) bool {
	return originpkg.AllowedByConfig(s.cfg.CORSAllowedOrigins, origin)
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

	if oaiModels, err := s.openai.GetModelsList(ctx, true); err == nil {
		mergedModels = append(mergedModels, oaiModels...)
	} else {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return http.StatusInternalServerError
	}
	mergedModels = s.openai.FilterModelsListByKey(ctx, mergedModels)

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
