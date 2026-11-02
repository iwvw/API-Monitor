package configmgmt

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
}

var writeJSON = adminshared.WriteJSON

func maskSecretPreview(secret string) string {
	return adminshared.MaskSecretPreview(secret)
}
func toStringSlice(v any) ([]string, bool) { return adminshared.ToStringSlice(v) }
func toAccount(m map[string]any) config.Account {
	return adminshared.ToAccount(m)
}
func toAPIKeys(v any) ([]config.APIKey, bool) { return adminshared.ToAPIKeys(v) }
func mergeAPIKeysPreferStructured(existing, incoming []config.APIKey) ([]config.APIKey, int) {
	return adminshared.MergeAPIKeysPreferStructured(existing, incoming)
}
func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
func fieldStringOptional(m map[string]any, key string) (string, bool) {
	return adminshared.FieldStringOptional(m, key)
}
func normalizeAccountForStorage(acc config.Account) config.Account {
	return adminshared.NormalizeAccountForStorage(acc)
}
func accountDedupeKey(acc config.Account) string { return adminshared.AccountDedupeKey(acc) }
func normalizeAndDedupeAccounts(accounts []config.Account) []config.Account {
	return adminshared.NormalizeAndDedupeAccounts(accounts)
}
func newRequestError(detail string) error { return adminshared.NewRequestError(detail) }
func requestErrorDetail(err error) (string, bool) {
	return adminshared.RequestErrorDetail(err)
}
func normalizeSettingsConfig(c *config.Config) { adminshared.NormalizeSettingsConfig(c) }
func validateSettingsConfig(c config.Config) error {
	return adminshared.ValidateSettingsConfig(c)
}
