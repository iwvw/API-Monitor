package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

func newHTTPAdminHarness(t *testing.T, rawConfig string, ds adminshared.DeepSeekCaller) http.Handler {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", rawConfig)
	store := config.LoadStore()
	h := &Handler{
		Store: store,
		Pool:  account.NewPool(store),
		DS:    ds,
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return r
}

func adminReq(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestConfigImportIgnoresTokenFieldInPayload(t *testing.T) {
	t.Setenv("DS2API_ADMIN_KEY", "admin")
	ds := &testingDSMock{}
	router := newHTTPAdminHarness(t, `{"accounts":[]}`, ds)

	payload := []byte(`{
		"mode":"replace",
		"config":{
			"accounts":[{"email":"u@example.com","password":"pwd","token":"expired-token"}]
		}
	}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPost, "/config/import", payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}

	readRec := httptest.NewRecorder()
	router.ServeHTTP(readRec, adminReq(http.MethodGet, "/config", nil))
	if readRec.Code != http.StatusOK {
		t.Fatalf("get config status=%d body=%s", readRec.Code, readRec.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(readRec.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	accounts, _ := data["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(accounts))
	}
	accountMap, _ := accounts[0].(map[string]any)
	if hasToken, _ := accountMap["has_token"].(bool); hasToken {
		t.Fatalf("expected imported token to be ignored, account=%#v", accountMap)
	}
}

func TestAccountTestRefreshesRuntimeTokenButExportOmitsToken(t *testing.T) {
	t.Setenv("DS2API_ADMIN_KEY", "admin")
	ds := &testingDSMock{}
	router := newHTTPAdminHarness(t, `{
		"accounts":[{"email":"batch@example.com","password":"pwd","token":"stale-token"}]
	}`, ds)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPost, "/accounts/test", []byte(`{"identifier":"batch@example.com"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("test account status=%d body=%s", rec.Code, rec.Body.String())
	}
	var testResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if ok, _ := testResp["success"].(bool); !ok {
		t.Fatalf("expected test success, got %#v", testResp)
	}
	if ds.loginCalls < 1 {
		t.Fatalf("expected login to be called at least once, got %d", ds.loginCalls)
	}

	exportRec := httptest.NewRecorder()
	router.ServeHTTP(exportRec, adminReq(http.MethodGet, "/config/export", nil))
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRec.Code, exportRec.Body.String())
	}
	var exportResp map[string]any
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportResp); err != nil {
		t.Fatalf("decode export response: %v", err)
	}
	exportJSON, _ := exportResp["json"].(string)
	if strings.Contains(exportJSON, `"token"`) {
		t.Fatalf("expected export json to omit tokens, got %s", exportJSON)
	}
}
