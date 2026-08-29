package rawsamples

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
}

var writeJSON = adminshared.WriteJSON

func intFromQuery(r *http.Request, key string, d int) int {
	return adminshared.IntFromQuery(r, key, d)
}
func nilIfEmpty(s string) any              { return adminshared.NilIfEmpty(s) }
func toStringSlice(v any) ([]string, bool) { return adminshared.ToStringSlice(v) }
func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
