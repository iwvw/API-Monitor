package vercel

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
}

var writeJSON = adminshared.WriteJSON
var intFrom = adminshared.IntFrom

func nilIfZero(v int64) any     { return adminshared.NilIfZero(v) }
func statusOr(v int, d int) int { return adminshared.StatusOr(v, d) }

func (h *Handler) computeSyncHash() string {
	return adminshared.ComputeSyncHash(h.Store)
}
