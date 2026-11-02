package accounts

import (
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
	// OnAccountsChanged 在账号启用/禁用、弹性号池重算等操作后触发（若非 nil），
	// 供 mihomo 代理桥立即按已有测速结果为新启用账号分配节点。
	OnAccountsChanged func()
}

var writeJSON = adminshared.WriteJSON

func (h *Handler) notifyAccountsChanged() {
	if h != nil && h.OnAccountsChanged != nil {
		h.OnAccountsChanged()
	}
}

func reverseAccounts(a []config.Account) { adminshared.ReverseAccounts(a) }
func intFromQuery(r *http.Request, key string, d int) int {
	return adminshared.IntFromQuery(r, key, d)
}
func maskSecretPreview(secret string) string {
	return adminshared.MaskSecretPreview(secret)
}
func toAccount(m map[string]any) config.Account {
	return adminshared.ToAccount(m)
}
func fieldStringOptional(m map[string]any, key string) (string, bool) {
	return adminshared.FieldStringOptional(m, key)
}
func fieldBoolOptional(m map[string]any, key string) (bool, bool) {
	return adminshared.FieldBoolOptional(m, key)
}
func accountMatchesIdentifier(acc config.Account, identifier string) bool {
	return adminshared.AccountMatchesIdentifier(acc, identifier)
}
func findProxyByID(c config.Config, proxyID string) (config.Proxy, bool) {
	return adminshared.FindProxyByID(c, proxyID)
}
func findAccountByIdentifier(store adminshared.ConfigStore, identifier string) (config.Account, bool) {
	return adminshared.FindAccountByIdentifier(store, identifier)
}
func newRequestError(detail string) error { return adminshared.NewRequestError(detail) }
func requestErrorDetail(err error) (string, bool) {
	return adminshared.RequestErrorDetail(err)
}
