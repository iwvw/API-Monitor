package proxies

import (
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
	// ResetProxyClients 在代理配置（CRUD / 账号改绑）变化后清理
	// DeepSeek client 侧缓存的代理连接池；nil 时跳过。
	ResetProxyClients func()
}

var writeJSON = adminshared.WriteJSON

func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
func accountMatchesIdentifier(acc config.Account, identifier string) bool {
	return adminshared.AccountMatchesIdentifier(acc, identifier)
}
func toProxy(m map[string]any) config.Proxy { return adminshared.ToProxy(m) }
func findProxyByID(c config.Config, proxyID string) (config.Proxy, bool) {
	return adminshared.FindProxyByID(c, proxyID)
}
func newRequestError(detail string) error { return adminshared.NewRequestError(detail) }
func requestErrorDetail(err error) (string, bool) {
	return adminshared.RequestErrorDetail(err)
}
