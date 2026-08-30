package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/claude"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/gemini"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/ollama"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/chat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/embeddings"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/files"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/responses"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/requestbody"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/mihomo"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/webui"
)

type App struct {
	Store    *config.Store
	Pool     *account.Pool
	Resolver *auth.Resolver
	DS       *dsclient.Client
	Mihomo   *mihomo.Manager
	Router   http.Handler

	// cleanupStop 停止内容缓存的后台过期清理循环（App.Stop 时调用）。
	cleanupStop func()
}

// Stop 释放 App 持有的后台资源：mihomo 子进程、内容缓存清理循环。
// 幂等，可安全多次调用。
func (a *App) Stop() {
	if a == nil {
		return
	}
	if a.Mihomo != nil {
		a.Mihomo.Stop()
	}
	if a.cleanupStop != nil {
		a.cleanupStop()
		a.cleanupStop = nil
	}
}

func NewApp() (*App, error) {
	store, err := config.LoadStoreWithError()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	pool := account.NewPool(store)
	var dsClient *dsclient.Client
	resolver := auth.NewResolver(store, pool, func(ctx context.Context, acc config.Account) (string, error) {
		return dsClient.Login(ctx, acc)
	})
	dsClient = dsclient.NewClient(store, resolver)
	if err := dsClient.PreloadPow(context.Background()); err != nil {
		config.Logger.Warn("[PoW] init failed", "error", err)
	} else {
		config.Logger.Info("[PoW] pure Go solver ready")
	}
	chatHistoryStore := chathistory.New(config.ChatHistoryPath())
	if err := chatHistoryStore.Err(); err != nil {
		config.Logger.Warn("[chat_history] unavailable", "path", chatHistoryStore.Path(), "error", err)
	}
	usageStatsStore := usagestats.New(config.UsageStatsPath())
	usagestats.SetGlobal(usageStatsStore)
	backfillUsageStatsOnce(chatHistoryStore, usageStatsStore)

	contentStore := files.NewMemoryContentStore(16<<20, 30*time.Minute)
	// 后台定期清理过期上传内容，避免过期字节长期滞留内存（TTL 30 分钟，
	// 每 5 分钟扫一次；随 App.Stop 停止）。
	cleanupStop := contentStore.StartCleanup(5 * time.Minute)

	modelsHandler := &shared.ModelsHandler{Store: store}
	chatHandler := &chat.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore, ContentStore: contentStore}
	responsesHandler := &responses.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore, ContentStore: contentStore}
	filesHandler := &files.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore, ContentStore: contentStore}
	embeddingsHandler := &embeddings.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore}
	claudeHandler := &claude.Handler{Store: store, Auth: resolver, DS: dsClient, OpenAI: chatHandler, ChatHistory: chatHistoryStore}
	geminiHandler := &gemini.Handler{Store: store, Auth: resolver, DS: dsClient, OpenAI: chatHandler, ChatHistory: chatHistoryStore}
	ollamaHandler := &ollama.Handler{Store: store}
	webuiHandler := webui.NewHandler()
	mihomoMgr := mihomo.NewManager(store, pool)
	mihomoMgr.SetProxyReset(dsClient.ResetProxyClients)
	dsClient.SetNodeFailureReporter(mihomoMgr.ReportUpstreamResult)
	dsClient.SetAccountPoolChanged(mihomoMgr.RequestReconcile)
	adminHandler := &admin.Handler{Store: store, Pool: pool, DS: dsClient, OpenAI: chatHandler, ChatHistory: chatHistoryStore, Mihomo: mihomoMgr, ResetProxyClients: dsClient.ResetProxyClients, WebUIFallback: webuiHandler.HandleAdminFallback}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(filteredLogger())
	r.Use(middleware.Recoverer)
	r.Use(cors)
	r.Use(requestbody.ValidateJSONUTF8)
	r.Use(timeout(0))

	healthzHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
	readyzHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
	r.Get("/healthz", healthzHandler)
	r.Head("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler)
	r.Head("/readyz", readyzHandler)
	r.Get("/v1/models", modelsHandler.ListModels)
	r.Get("/v1/models/{model_id}", modelsHandler.GetModel)
	r.Post("/v1/chat/completions", chatHandler.ChatCompletions)
	r.Post("/v1/responses", responsesHandler.Responses)
	r.Get("/v1/responses/{response_id}", responsesHandler.GetResponseByID)
	r.Post("/v1/files", filesHandler.UploadFile)
	r.Get("/v1/files/{file_id}", filesHandler.RetrieveFile)
	r.Post("/v1/embeddings", embeddingsHandler.Embeddings)
	// Root OpenAI aliases support clients configured with the bare DS2API service URL.
	r.Get("/models", modelsHandler.ListModels)
	r.Get("/models/{model_id}", modelsHandler.GetModel)
	r.Post("/chat/completions", chatHandler.ChatCompletions)
	r.Post("/responses", responsesHandler.Responses)
	r.Get("/responses/{response_id}", responsesHandler.GetResponseByID)
	r.Post("/files", filesHandler.UploadFile)
	r.Get("/files/{file_id}", filesHandler.RetrieveFile)
	r.Post("/embeddings", embeddingsHandler.Embeddings)
	claude.RegisterRoutes(r, claudeHandler)
	gemini.RegisterRoutes(r, geminiHandler)
	ollama.RegisterRoutes(r, ollamaHandler)
	r.Route("/admin", func(ar chi.Router) {
		admin.RegisterRoutes(ar, adminHandler)
	})
	webui.RegisterRoutes(r, webuiHandler)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/admin/") && webuiHandler.HandleAdminFallback(w, req) {
			return
		}
		http.NotFound(w, req)
	})

	// 配置启用时后台拉起 mihomo 子进程（Vercel 等不支持子进程的环境自动跳过）。
	mihomoMgr.StartIfEnabled()

	return &App{Store: store, Pool: pool, Resolver: resolver, DS: dsClient, Mihomo: mihomoMgr, Router: r, cleanupStop: cleanupStop}, nil
}

func timeout(d time.Duration) func(http.Handler) http.Handler {
	if d <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return middleware.Timeout(d)
}

// filteredLogger 为 noop：请求日志由主程序 applog.Middleware 统一记录，
// 引擎内部不再重复输出（避免两种格式并存的重复请求日志）。
func filteredLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

var defaultCORSAllowHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-API-Key",
	"X-Ds2-Target-Account",
	"X-Ds2-Source",
	"X-Vercel-Protection-Bypass",
	"X-Goog-Api-Key",
	"Anthropic-Version",
	"Anthropic-Beta",
}

var blockedCORSRequestHeaders = map[string]struct{}{
	"x-ds2-internal-token": {},
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		addVaryHeaderToken(w.Header(), "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", buildCORSAllowHeaders(r))
	w.Header().Set("Access-Control-Max-Age", "600")
	addVaryHeaderToken(w.Header(), "Access-Control-Request-Headers")
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Access-Control-Request-Private-Network")), "true") {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		addVaryHeaderToken(w.Header(), "Access-Control-Request-Private-Network")
	}
}

func buildCORSAllowHeaders(r *http.Request) string {
	names := make([]string, 0, len(defaultCORSAllowHeaders)+4)
	seen := make(map[string]struct{}, len(defaultCORSAllowHeaders)+4)
	for _, name := range defaultCORSAllowHeaders {
		appendCORSHeaderName(&names, seen, name)
	}
	if r == nil {
		return strings.Join(names, ", ")
	}
	for _, name := range splitCORSRequestHeaders(r.Header.Get("Access-Control-Request-Headers")) {
		appendCORSHeaderName(&names, seen, name)
	}
	return strings.Join(names, ", ")
}

func splitCORSRequestHeaders(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if !isValidCORSHeaderToken(name) {
			continue
		}
		if _, blocked := blockedCORSRequestHeaders[strings.ToLower(name)]; blocked {
			continue
		}
		out = append(out, name)
	}
	return out
}

func appendCORSHeaderName(dst *[]string, seen map[string]struct{}, name string) {
	name = strings.TrimSpace(name)
	if !isValidCORSHeaderToken(name) {
		return
	}
	key := strings.ToLower(name)
	if _, blocked := blockedCORSRequestHeaders[key]; blocked {
		return
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*dst = append(*dst, name)
}

func isValidCORSHeaderToken(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func addVaryHeaderToken(h http.Header, token string) {
	if h == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	current := h.Values("Vary")
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(current)+1)
	for _, value := range current {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, name)
		}
	}
	key := strings.ToLower(token)
	if _, ok := seen[key]; !ok {
		merged = append(merged, token)
	}
	h.Set("Vary", strings.Join(merged, ", "))
}

func WriteUnhandledError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "api_error", "message": "Internal Server Error", "detail": err.Error()}})
}
