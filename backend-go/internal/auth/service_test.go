package auth

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func Test2FAManagementFlow(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	initialCookie := findCookie(res.Result().Cookies(), "sid")
	if initialCookie == nil {
		t.Fatal("expected login cookie")
	}

	res = performAuthRequest(service, http.MethodGet, "/api/auth/2fa/status", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("initial 2fa status = %d body=%s", res.Code, res.Body.String())
	}
	var statusPayload struct {
		Success bool `json:"success"`
		Enabled bool `json:"enabled"`
	}
	mustDecodeAuth(t, res, &statusPayload)
	if !statusPayload.Success || statusPayload.Enabled {
		t.Fatalf("unexpected initial status: %#v", statusPayload)
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/2fa/setup", `{}`, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("setup without session status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/2fa/setup", `{}`, initialCookie)
	if res.Code != http.StatusOK {
		t.Fatalf("setup status = %d body=%s", res.Code, res.Body.String())
	}
	var setupPayload struct {
		Success bool   `json:"success"`
		Secret  string `json:"secret"`
		QRCode  string `json:"qrCode"`
	}
	mustDecodeAuth(t, res, &setupPayload)
	if !setupPayload.Success || setupPayload.Secret == "" || !strings.HasPrefix(setupPayload.QRCode, "data:image/png;base64,") {
		t.Fatalf("unexpected setup payload: %#v", setupPayload)
	}

	token := totpForTest(t, setupPayload.Secret, time.Now().UTC())
	enableBody := `{"secret":"` + setupPayload.Secret + `","token":"` + token + `"}`
	res = performAuthRequest(service, http.MethodPost, "/api/auth/2fa/enable", enableBody, initialCookie)
	if res.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", res.Code, res.Body.String())
	}

	db, err := service.openDB(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encryptedSecret, err := service.getConfig(context.Background(), db, system2FASecretKey)
	if err != nil {
		t.Fatal(err)
	}
	decryptedSecret, err := decryptNodeGCM(encryptedSecret)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if encryptedSecret == setupPayload.Secret || decryptedSecret != setupPayload.Secret {
		t.Fatalf("2fa secret storage mismatch encrypted=%q decrypted=%q secret=%q", encryptedSecret, decryptedSecret, setupPayload.Secret)
	}

	res = performAuthRequest(service, http.MethodGet, "/api/auth/2fa/status", "", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("enabled status code = %d body=%s", res.Code, res.Body.String())
	}
	mustDecodeAuth(t, res, &statusPayload)
	if !statusPayload.Enabled {
		t.Fatalf("expected 2fa enabled, got %#v", statusPayload)
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/logout", "", initialCookie)
	if res.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", res.Code, res.Body.String())
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login without 2fa status = %d body=%s", res.Code, res.Body.String())
	}
	var requirePayload struct {
		Success    bool `json:"success"`
		Require2FA bool `json:"require2FA"`
	}
	mustDecodeAuth(t, res, &requirePayload)
	if requirePayload.Success || !requirePayload.Require2FA {
		t.Fatalf("expected 2fa challenge, got %#v", requirePayload)
	}

	loginBody := `{"password":"secret123","totpToken":"` + token + `"}`
	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", loginBody, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login with 2fa status = %d body=%s", res.Code, res.Body.String())
	}
	totpCookie := findCookie(res.Result().Cookies(), "sid")
	if totpCookie == nil {
		t.Fatal("expected 2fa login cookie")
	}

	res = performAuthRequest(service, http.MethodPost, "/api/auth/2fa/disable", `{"password":"secret123"}`, totpCookie)
	if res.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", res.Code, res.Body.String())
	}

	db, err = service.openDB(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := service.is2FAEnabled(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	secretAfterDisable, err := service.getConfig(context.Background(), db, system2FASecretKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if enabled || secretAfterDisable != "" {
		t.Fatalf("expected disabled 2fa with deleted secret, enabled=%v secret=%q", enabled, secretAfterDisable)
	}
}

func performAuthRequest(service *Service, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.20:4567"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func mustDecodeAuth(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func totpForTest(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalizeBase32Secret(secret))
	if err != nil {
		t.Fatal(err)
	}
	return hotp(key, uint64(now.Unix()/30))
}
