package claude

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/textclean"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

// writeJSON is a package-internal alias to avoid mass-renaming all call-sites.
var writeJSON = util.WriteJSON

type Handler struct {
	Store       ConfigReader
	Auth        AuthResolver
	DS          DeepSeekCaller
	OpenAI      OpenAIChatRunner
	ChatHistory *chathistory.Store
}

func stripReferenceMarkersEnabled() bool {
	return textclean.StripReferenceMarkersEnabled()
}

var (
	claudeStreamPingInterval    = time.Duration(dsprotocol.KeepAliveTimeout) * time.Second
	claudeStreamIdleTimeout     = time.Duration(dsprotocol.StreamIdleTimeout) * time.Second
	claudeStreamMaxKeepaliveCnt = dsprotocol.MaxKeepaliveCount
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/anthropic/v1/models", h.ListModels)
	r.Post("/anthropic/v1/messages", h.Messages)
	r.Post("/anthropic/v1/messages/count_tokens", h.CountTokens)
	r.Post("/v1/messages", h.Messages)
	r.Post("/messages", h.Messages)
	r.Post("/v1/messages/count_tokens", h.CountTokens)
	r.Post("/messages/count_tokens", h.CountTokens)
}

func (h *Handler) ListModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, config.ClaudeModelsResponse())
}
