package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	adminaccounts "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/accounts"
	adminconfig "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/configmgmt"
	adminsettings "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/settings"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

var intFrom = adminshared.IntFrom

func toAccount(m map[string]any) config.Account { return adminshared.ToAccount(m) }
func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}
func maskSecretPreview(secret string) string { return adminshared.MaskSecretPreview(secret) }
func boolFrom(v any) bool                    { return adminsettings.BoolFrom(v) }

func newAdminTestHandler(t *testing.T, raw string) *Handler {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", raw)
	store := config.LoadStore()
	return &Handler{
		Store: store,
		Pool:  account.NewPool(store),
	}
}

type testingDSMock struct {
	loginToken                 string
	deleteAllSessionsError     error
	deleteAllSessionsErrorOnce bool
	sessionCount               *dsclient.SessionStats
	loginCalls                 int
	deleteAllCalls             int
}

func (m *testingDSMock) Login(_ context.Context, _ config.Account) (string, error) {
	m.loginCalls++
	if m.loginToken == "" {
		return "token", nil
	}
	return m.loginToken, nil
}

func (m *testingDSMock) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (m *testingDSMock) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (m *testingDSMock) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}

func (m *testingDSMock) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	m.deleteAllCalls++
	if m.deleteAllSessionsError != nil {
		err := m.deleteAllSessionsError
		if m.deleteAllSessionsErrorOnce {
			m.deleteAllSessionsError = nil
		}
		return err
	}
	return nil
}

func (m *testingDSMock) GetSessionCountForToken(_ context.Context, _ string) (*dsclient.SessionStats, error) {
	if m.sessionCount != nil {
		return m.sessionCount, nil
	}
	return &dsclient.SessionStats{}, nil
}

func (h *Handler) configHandler() *adminconfig.Handler {
	return &adminconfig.Handler{Store: h.Store, Pool: h.Pool, DS: h.DS, OpenAI: h.OpenAI, ChatHistory: h.ChatHistory}
}

func (h *Handler) settingsHandler() *adminsettings.Handler {
	return &adminsettings.Handler{Store: h.Store, Pool: h.Pool, DS: h.DS, OpenAI: h.OpenAI, ChatHistory: h.ChatHistory}
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	h.configHandler().GetConfig(w, r)
}

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	h.configHandler().UpdateConfig(w, r)
}

func (h *Handler) configImport(w http.ResponseWriter, r *http.Request) {
	h.configHandler().ConfigImport(w, r)
}

func (h *Handler) batchImport(w http.ResponseWriter, r *http.Request) {
	h.configHandler().BatchImport(w, r)
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	h.settingsHandler().GetSettings(w, r)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	h.settingsHandler().UpdateSettings(w, r)
}

func (h *Handler) updateSettingsPassword(w http.ResponseWriter, r *http.Request) {
	h.settingsHandler().UpdateSettingsPassword(w, r)
}

func runAccountTestsConcurrently(accounts []config.Account, maxConcurrency int, testFn func(int, config.Account) map[string]any) []map[string]any {
	return adminaccounts.RunAccountTestsConcurrently(accounts, maxConcurrency, testFn)
}
