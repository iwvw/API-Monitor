package auth

import (
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
	// WebUIFallback, when set, lets the admin auth middleware serve the SPA
	// index.html for browser navigation requests (GET without Authorization,
	// Accept: text/html) instead of returning 401. This allows users to refresh
	// SPA routes that collide with protected admin API GET endpoints
	// (e.g. /admin/settings, /admin/proxies).
	WebUIFallback func(http.ResponseWriter, *http.Request) bool
}

var writeJSON = adminshared.WriteJSON
var intFrom = adminshared.IntFrom
var maskSecretPreview = adminshared.MaskSecretPreview

func nilIfEmpty(s string) any { return adminshared.NilIfEmpty(s) }
