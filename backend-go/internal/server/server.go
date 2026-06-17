package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/aliyun"
	"github.com/iwvw/api-monitor/backend-go/internal/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/cronjobs"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/filebox"
	"github.com/iwvw/api-monitor/backend-go/internal/flyio"
	"github.com/iwvw/api-monitor/backend-go/internal/geminicli"
	"github.com/iwvw/api-monitor/backend-go/internal/koyeb"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	"github.com/iwvw/api-monitor/backend-go/internal/notification"
	"github.com/iwvw/api-monitor/backend-go/internal/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/qwen"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/serveragent"
	"github.com/iwvw/api-monitor/backend-go/internal/settings"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
	"github.com/iwvw/api-monitor/backend-go/internal/tencent"
	"github.com/iwvw/api-monitor/backend-go/internal/totp"
	"github.com/iwvw/api-monitor/backend-go/internal/uptime"
)

type Server struct {
	cfg       config.Config
	auth      *auth.Service
	settings  *settings.Service
	system    *systemmetrics.Service
	totp      *totp.Service
	cron      *cronjobs.Service
	filebox   *filebox.Service
	notify    *notification.Service
	uptime    *uptime.Service
	koyeb     *koyeb.Service
	flyio     *flyio.Service
	aliyun    *aliyun.Service
	tencent   *tencent.Service
	cf        *cloudflare.Service
	openai    *openai.Service
	qwen      *qwen.Service
	geminicli *geminicli.Service
	server    *serveragent.Service
}

func New(cfg config.Config) http.Handler {
	return NewServer(cfg)
}

func NewServer(cfg config.Config) *Server {
	authService := auth.New(cfg)
	notifyService := notification.New(cfg)
	serverAgentService := serveragent.New(cfg)
	uptimeService := uptime.New(cfg, authService, notifyService)
	uptimeService.SetHeartbeatBroadcaster(serverAgentService.BroadcastUptimeHeartbeat)
	return &Server{
		cfg:       cfg,
		auth:      authService,
		settings:  settings.New(cfg),
		system:    systemmetrics.New(),
		totp:      totp.New(cfg),
		cron:      cronjobs.New(cfg),
		filebox:   filebox.New(cfg, authService),
		notify:    notifyService,
		uptime:    uptimeService,
		koyeb:     koyeb.New(cfg),
		flyio:     flyio.New(cfg),
		aliyun:    aliyun.New(cfg),
		tencent:   tencent.New(cfg),
		cf:        cloudflare.New(cfg),
		openai:    openai.New(cfg),
		qwen:      qwen.New(cfg),
		geminicli: geminicli.New(cfg, authService),
		server:    serverAgentService,
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.uptime != nil {
		s.uptime.Stop()
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
	s.applySecurityHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if route, ok := manifest.Match(r.URL.EscapedPath()); ok {
		switch route.Owner {
		case manifest.OwnerGo:
			if !s.authorizeGoRoute(w, r, route) {
				return
			}
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

func (s *Server) authorizeGoRoute(w http.ResponseWriter, r *http.Request, route manifest.Route) bool {
	if route.Auth != manifest.AuthSession {
		return true
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

func (s *Server) serveGoRoute(w http.ResponseWriter, r *http.Request, route manifest.Route) {
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
			"retired":       []string{"music", "openlist"},
		})
	case "/api/auth", "/api/auth/2fa", "/api/auth/2fa/status":
		s.auth.ServeHTTP(w, r)
	case "/api/settings", "/api/settings/database-stats", "/api/settings/migration-self-check", "/api/settings/database-analysis", "/api/settings/export-database", "/api/settings/database/import", "/api/settings/import-database", "/api/settings/operation-logs", "/api/settings/sys-logs", "/api/settings/app-log-file", "/api/settings/log-settings", "/api/settings/clear-app-logs", "/api/settings/vacuum-database", "/api/settings/clear-logs", "/api/settings/enforce-log-limits", "/api/settings/clear-chat-messages":
		s.settings.ServeHTTP(w, r)
	case "/api/system/host-metrics":
		s.system.ServeHTTP(w, r)
	case "/api/totp":
		s.totp.ServeHTTP(w, r)
	case "/api/cron":
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
	case "/api/aliyun":
		s.aliyun.ServeHTTP(w, r)
	case "/api/tencent":
		s.tencent.ServeHTTP(w, r)
	case "/api/cloudflare/accounts", "/api/cloudflare/accounts/export", "/api/cloudflare/export/accounts", "/api/cloudflare/import/accounts", "/api/cloudflare/templates", "/api/cloudflare/templates/{id}", "/api/cloudflare/templates/{templateId}/apply", "/api/cloudflare/import/templates", "/api/cloudflare/accounts/{id}", "/api/cloudflare/accounts/{id}/verify", "/api/cloudflare/accounts/{id}/token", "/api/cloudflare/accounts/{id}/cf-account-id", "/api/cloudflare/accounts/{id}/pages", "/api/cloudflare/accounts/{id}/pages/{projectName}", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments", "/api/cloudflare/accounts/{id}/pages/{projectName}/deployments/{deploymentId}", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains", "/api/cloudflare/accounts/{id}/pages/{projectName}/domains/{domain}", "/api/cloudflare/accounts/{id}/workers", "/api/cloudflare/accounts/{id}/workers/{scriptName}", "/api/cloudflare/accounts/{id}/workers/{scriptName}/toggle", "/api/cloudflare/accounts/{id}/workers/{scriptName}/analytics", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains", "/api/cloudflare/accounts/{id}/workers/{scriptName}/domains/{domainId}", "/api/cloudflare/accounts/{accountId}/r2/buckets", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}", "/api/cloudflare/accounts/{accountId}/r2/buckets/{bucketName}/objects/{objectKey}/download-info", "/api/cloudflare/accounts/{id}/tunnels", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/configuration", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/token", "/api/cloudflare/accounts/{accountId}/tunnels/{tunnelId}/connections", "/api/cloudflare/record-types", "/api/cloudflare/zones", "/api/cloudflare/accounts/{id}/zones", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/workers/routes/{routeId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records/{recordId}", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/purge", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/ssl", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/analytics", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/switch", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/batch":
		s.cf.ServeHTTP(w, r)
	case "/api/openai":
		s.openai.ServeHTTP(w, r)
	case "/api/qwen":
		s.qwen.ServeHTTP(w, r)
	case "/api/gemini-cli":
		s.geminicli.ServeHTTP(w, r)
	case "/v1":
		s.serveV1Route(w, r)
	case "/socket.io/":
		s.server.ServeHTTP(w, r)
	default:
		if strings.HasPrefix(route.Prefix, "/api/server/") {
			s.server.ServeHTTP(w, r)
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

	// 尝试从 dist 和 public 查找文件
	foundInDist := s.tryServeFile(w, r, s.cfg.DistDir)
	if foundInDist {
		return
	}

	foundInPublic := s.tryServeFile(w, r, s.cfg.PublicDir)
	if foundInPublic {
		return
	}

	// 如果是根路径或没有文件扩展名（可能是 SPA 路由），返回 index.html
	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	ext := filepath.Ext(cleanPath)
	if cleanPath == "." || cleanPath == "" || ext == "" {
		indexPath := filepath.Join(s.cfg.DistDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	response.Error(w, http.StatusNotFound, "static asset not found")
}

func (s *Server) tryServeFile(w http.ResponseWriter, r *http.Request, dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if cleanPath == "." || cleanPath == "" {
		cleanPath = "index.html"
	}
	candidate := filepath.Join(dir, cleanPath)

	// 只在文件存在时返回
	if fileInfo, err := os.Stat(candidate); err == nil && !fileInfo.IsDir() {
		http.ServeFile(w, r, candidate)
		return true
	}

	return false
}

func (s *Server) applySecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Admin-Password,X-API-Key,X-Agent-Key,X-Server-ID,X-Filebox-Password")
}

func (s *Server) serveV1Route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method
	channels := s.loadV1ChannelSettings(r.Context())

	// 1. Models endpoint
	if method == http.MethodGet && (path == "/v1/models" || path == "/v1/model") {
		s.serveV1Models(w, r)
		return
	}

	// 2. Images generations endpoint
	if method == http.MethodPost && path == "/v1/images/generations" {
		if !channels["qwen"] {
			response.Error(w, http.StatusNotFound, "model channel is disabled")
			return
		}
		s.qwen.ServeHTTP(w, r)
		return
	}

	// 3. Completions endpoint
	if method == http.MethodPost && path == "/v1/chat/completions" {
		var bodyBytes []byte
		if r.Body != nil {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		var req struct {
			Model string `json:"model"`
		}
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &req)
		}

		if channels["gemini-cli"] && s.geminicli.CanHandleModel(r.Context(), req.Model) {
			s.geminicli.ServeHTTP(w, r)
		} else if channels["qwen"] && s.qwen.CanHandleModel(r.Context(), req.Model) {
			s.qwen.ServeHTTP(w, r)
		} else if (!channels["gemini-cli"] && s.geminicli.CanHandleModel(r.Context(), req.Model)) ||
			(!channels["qwen"] && s.qwen.CanHandleModel(r.Context(), req.Model)) {
			response.Error(w, http.StatusNotFound, "model channel is disabled")
		} else {
			s.openai.ServeHTTP(w, r)
		}
		return
	}

	response.Error(w, http.StatusNotFound, "v1 endpoint not found")
}

func (s *Server) serveV1Models(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var mergedModels []map[string]interface{}
	channels := s.loadV1ChannelSettings(ctx)

	if oaiModels, err := s.openai.GetModelsList(ctx); err == nil {
		mergedModels = append(mergedModels, oaiModels...)
	}

	if channels["qwen"] {
		if qwenModels, err := s.qwen.GetModelsList(ctx); err == nil {
			mergedModels = append(mergedModels, qwenModels...)
		}
	}

	if channels["gemini-cli"] {
		if geminiModels, err := s.geminicli.GetModelsList(ctx); err == nil {
			mergedModels = append(mergedModels, geminiModels...)
		}
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
}

func (s *Server) loadV1ChannelSettings(ctx context.Context) map[string]bool {
	channels := map[string]bool{
		"gemini-cli": true,
		"qwen":       false,
	}

	db, err := database.New(s.cfg).Open(ctx)
	if err != nil {
		return channels
	}
	defer db.Close()

	var raw sql.NullString
	err = db.QueryRowContext(ctx, `SELECT channel_enabled FROM user_settings WHERE id = 1`).Scan(&raw)
	if err != nil || !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return channels
	}

	var saved map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &saved); err != nil {
		return channels
	}

	for channel := range channels {
		value, ok := saved[channel]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			channels[channel] = typed
		case string:
			channels[channel] = strings.EqualFold(typed, "true") || typed == "1"
		case float64:
			channels[channel] = typed != 0
		}
	}
	return channels
}
