package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	adminaccounts "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/accounts"
	adminauth "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/auth"
	adminconfig "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/configmgmt"
	admindevcapture "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/devcapture"
	adminhistory "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/history"
	adminmihomo "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/mihomo"
	adminproxies "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/proxies"
	adminrawsamples "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/rawsamples"
	adminsettings "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/settings"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
	adminvercel "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/vercel"
	adminversion "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/version"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
	// ResetProxyClients 透传给 /admin/proxies 处理器：代理配置变化后
	// 清理 DeepSeek client 侧的代理连接池缓存。
	ResetProxyClients func()
	// Mihomo 是 Mihomo 代理桥控制器；nil 时 /admin/mihomo/* 返回 503。
	Mihomo adminmihomo.Bridge
	// WebUIFallback is forwarded to the auth sub-handler so its RequireAdmin
	// middleware can serve the SPA index.html when a browser navigation request
	// (GET, no Authorization, Accept: text/html) hits a protected admin API
	// endpoint that collides with an SPA route (e.g. /admin/settings).
	WebUIFallback func(http.ResponseWriter, *http.Request) bool
}

func RegisterRoutes(r chi.Router, h *Handler) {
	deps := adminsharedDeps(h)
	authHandler := &adminauth.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory, WebUIFallback: h.WebUIFallback}
	// 账号启用/禁用、弹性号池变更后，让 Mihomo 代理桥立即按已有测速结果
	// 为新启用账号分配节点（桥未装配或未实现时保持 nil，安全跳过）。
	var onAccountsChanged func()
	if bridge, ok := h.Mihomo.(interface{ RequestReconcile() }); ok {
		onAccountsChanged = bridge.RequestReconcile
	}
	accountsHandler := &adminaccounts.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory, OnAccountsChanged: onAccountsChanged}
	configHandler := &adminconfig.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	settingsHandler := &adminsettings.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	proxiesHandler := &adminproxies.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory, ResetProxyClients: h.ResetProxyClients}
	mihomoHandler := &adminmihomo.Handler{Store: deps.Store, Pool: deps.Pool, Bridge: h.Mihomo}
	rawSamplesHandler := &adminrawsamples.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	vercelHandler := &adminvercel.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	historyHandler := &adminhistory.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	devCaptureHandler := &admindevcapture.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}
	versionHandler := &adminversion.Handler{Store: deps.Store, Pool: deps.Pool, DS: deps.DS, OpenAI: deps.OpenAI, ChatHistory: deps.ChatHistory}

	adminauth.RegisterPublicRoutes(r, authHandler)
	r.Group(func(pr chi.Router) {
		pr.Use(authHandler.RequireAdmin)
		adminauth.RegisterProtectedRoutes(pr, authHandler)
		adminconfig.RegisterRoutes(pr, configHandler)
		adminsettings.RegisterRoutes(pr, settingsHandler)
		adminproxies.RegisterRoutes(pr, proxiesHandler)
		adminmihomo.RegisterRoutes(pr, mihomoHandler)
		adminaccounts.RegisterRoutes(pr, accountsHandler)
		adminrawsamples.RegisterRoutes(pr, rawSamplesHandler)
		adminvercel.RegisterRoutes(pr, vercelHandler)
		admindevcapture.RegisterRoutes(pr, devCaptureHandler)
		adminhistory.RegisterRoutes(pr, historyHandler)
		adminversion.RegisterRoutes(pr, versionHandler)
	})
}

func adminsharedDeps(h *Handler) adminsharedDepsValue {
	if h == nil {
		return adminsharedDepsValue{}
	}
	return adminsharedDepsValue{Store: h.Store, Pool: h.Pool, DS: h.DS, OpenAI: h.OpenAI, ChatHistory: h.ChatHistory}
}

type adminsharedDepsValue struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
}
