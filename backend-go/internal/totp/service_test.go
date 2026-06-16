package totp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestTOTPAccountGroupCodeImportAndExportFlow(t *testing.T) {
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	secret := "JBSWY3DPEHPK3PXP"

	groupsRes := performTOTPRequest(service, http.MethodGet, "/api/totp/groups", "")
	if groupsRes.Code != http.StatusOK {
		t.Fatalf("groups status = %d body=%s", groupsRes.Code, groupsRes.Body.String())
	}

	createGroupRes := performTOTPRequest(service, http.MethodPost, "/api/totp/groups", `{"name":"Work","color":"#4285f4"}`)
	if createGroupRes.Code != http.StatusOK {
		t.Fatalf("create group status = %d body=%s", createGroupRes.Code, createGroupRes.Body.String())
	}
	var groupPayload struct {
		Success bool  `json:"success"`
		Data    Group `json:"data"`
	}
	mustDecodeTOTP(t, createGroupRes, &groupPayload)
	if !groupPayload.Success || groupPayload.Data.ID == "" || groupPayload.Data.Name != "Work" {
		t.Fatalf("unexpected group payload: %#v", groupPayload)
	}

	createAccountRes := performTOTPRequest(service, http.MethodPost, "/api/totp/accounts", `{
		"issuer":"GitHub",
		"account":"user@example.com",
		"secret":"`+secret+`",
		"group_id":"`+groupPayload.Data.ID+`"
	}`)
	if createAccountRes.Code != http.StatusOK {
		t.Fatalf("create account status = %d body=%s", createAccountRes.Code, createAccountRes.Body.String())
	}
	var accountPayload struct {
		Success bool    `json:"success"`
		Data    Account `json:"data"`
	}
	mustDecodeTOTP(t, createAccountRes, &accountPayload)
	if !accountPayload.Success || accountPayload.Data.ID == "" || accountPayload.Data.Secret != "" || !accountPayload.Data.HasSecret {
		t.Fatalf("unexpected account payload: %#v", accountPayload)
	}

	code, err := totpCode(secret, time.Now(), 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	verifyRes := performTOTPRequest(service, http.MethodPost, "/api/totp/verify", `{"id":"`+accountPayload.Data.ID+`","token":"`+code+`"}`)
	if verifyRes.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verifyRes.Code, verifyRes.Body.String())
	}
	var verifyPayload struct {
		Success bool `json:"success"`
		Valid   bool `json:"valid"`
	}
	mustDecodeTOTP(t, verifyRes, &verifyPayload)
	if !verifyPayload.Success || !verifyPayload.Valid {
		t.Fatalf("unexpected verify payload: %#v", verifyPayload)
	}

	codesRes := performTOTPRequest(service, http.MethodGet, "/api/totp/codes", "")
	if codesRes.Code != http.StatusOK {
		t.Fatalf("codes status = %d body=%s", codesRes.Code, codesRes.Body.String())
	}
	var codesPayload struct {
		Success bool                              `json:"success"`
		Data    map[string]map[string]interface{} `json:"data"`
	}
	mustDecodeTOTP(t, codesRes, &codesPayload)
	if !codesPayload.Success || codesPayload.Data[accountPayload.Data.ID]["code"] == "" {
		t.Fatalf("unexpected codes payload: %#v", codesPayload)
	}

	revealRes := performTOTPRequest(service, http.MethodGet, "/api/totp/accounts/"+accountPayload.Data.ID+"?showSecret=true", "")
	if revealRes.Code != http.StatusOK {
		t.Fatalf("reveal status = %d body=%s", revealRes.Code, revealRes.Body.String())
	}
	mustDecodeTOTP(t, revealRes, &accountPayload)
	if accountPayload.Data.Secret != secret || accountPayload.Data.LastRevealedAt == nil {
		t.Fatalf("unexpected revealed account: %#v", accountPayload.Data)
	}

	hotpRes := performTOTPRequest(service, http.MethodPost, "/api/totp/accounts", `{
		"otp_type":"hotp",
		"issuer":"Yubi",
		"account":"token",
		"secret":"`+secret+`",
		"counter":0
	}`)
	if hotpRes.Code != http.StatusOK {
		t.Fatalf("create hotp status = %d body=%s", hotpRes.Code, hotpRes.Body.String())
	}
	var hotpPayload struct {
		Success bool    `json:"success"`
		Data    Account `json:"data"`
	}
	mustDecodeTOTP(t, hotpRes, &hotpPayload)
	incrementRes := performTOTPRequest(service, http.MethodPost, "/api/totp/accounts/"+hotpPayload.Data.ID+"/increment", "")
	if incrementRes.Code != http.StatusOK {
		t.Fatalf("increment status = %d body=%s", incrementRes.Code, incrementRes.Body.String())
	}
	var incrementPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Code    string `json:"code"`
			Counter int64  `json:"counter"`
		} `json:"data"`
	}
	mustDecodeTOTP(t, incrementRes, &incrementPayload)
	if !incrementPayload.Success || incrementPayload.Data.Code == "" || incrementPayload.Data.Counter != 1 {
		t.Fatalf("unexpected increment payload: %#v", incrementPayload)
	}

	exportRes := performTOTPRequest(service, http.MethodGet, "/api/totp/export", "")
	if exportRes.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exportRes.Code, exportRes.Body.String())
	}
	var exportPayload struct {
		Success bool   `json:"success"`
		Format  string `json:"format"`
		Data    struct {
			AccountCount int    `json:"accountCount"`
			GroupCount   int    `json:"groupCount"`
			Payload      string `json:"payload"`
		} `json:"data"`
	}
	mustDecodeTOTP(t, exportRes, &exportPayload)
	if !exportPayload.Success || exportPayload.Format != "encrypted-backup" || exportPayload.Data.AccountCount != 2 || exportPayload.Data.GroupCount != 1 || exportPayload.Data.Payload == "" {
		t.Fatalf("unexpected export payload: %#v", exportPayload)
	}

	importURI := "otpauth://totp/Example:new@example.com?secret=" + secret + "&issuer=Example"
	previewRes := performTOTPRequest(service, http.MethodPost, "/api/totp/import/preview", `{"uris":["`+importURI+`"]}`)
	if previewRes.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", previewRes.Code, previewRes.Body.String())
	}
	var previewPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Total int `json:"total"`
			Valid int `json:"valid"`
		} `json:"data"`
	}
	mustDecodeTOTP(t, previewRes, &previewPayload)
	if !previewPayload.Success || previewPayload.Data.Total != 1 || previewPayload.Data.Valid != 1 {
		t.Fatalf("unexpected preview payload: %#v", previewPayload)
	}

	importRes := performTOTPRequest(service, http.MethodPost, "/api/totp/import", `{"uris":["`+importURI+`"]}`)
	if importRes.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", importRes.Code, importRes.Body.String())
	}
	var importPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Success int `json:"success"`
			Failed  int `json:"failed"`
		} `json:"data"`
	}
	mustDecodeTOTP(t, importRes, &importPayload)
	if !importPayload.Success || importPayload.Data.Success != 1 || importPayload.Data.Failed != 0 {
		t.Fatalf("unexpected import payload: %#v", importPayload)
	}
}

func performTOTPRequest(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func mustDecodeTOTP(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
}
