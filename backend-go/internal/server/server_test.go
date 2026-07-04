package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	return newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

func newTestServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	if cfg.DBName == "" {
		cfg.DBName = "data.db"
	}
	handler := NewServer(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := handler.Shutdown(ctx); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})
	return handler
}

func loginServerForTest(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/set-password", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "sid" {
			return cookie
		}
	}
	t.Fatal("expected sid cookie")
	return nil
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status payload = %v", payload["status"])
	}
}

func TestAIMCPCallAPIUsesInternalRoutes(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	keyReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	keyReq.AddCookie(cookie)
	keyRes := httptest.NewRecorder()
	handler.ServeHTTP(keyRes, keyReq)
	if keyRes.Code != http.StatusOK {
		t.Fatalf("ai access status = %d, body=%s", keyRes.Code, keyRes.Body.String())
	}
	var keyPayload map[string]interface{}
	if err := json.Unmarshal(keyRes.Body.Bytes(), &keyPayload); err != nil {
		t.Fatal(err)
	}
	overview := keyPayload["data"].(map[string]interface{})
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call_api","arguments":{"method":"GET","path":"/api/migration/status"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	result := payload["result"].(map[string]interface{})
	apiResponse := result["content"]
	if apiResponse != nil {
		t.Fatalf("unexpected MCP content wrapper: %#v", apiResponse)
	}
	callResult := result
	if callResult["statusCode"].(float64) != 200 {
		t.Fatalf("expected proxied status 200, got %#v", callResult)
	}
	bodyPayload := callResult["body"].(map[string]interface{})
	if bodyPayload["success"] != true {
		t.Fatalf("expected envelope success, got %#v", bodyPayload)
	}
}

func TestStaticSpaRouteServesDistIndex(t *testing.T) {
	distDir := t.TempDir()
	indexHTML := "<!doctype html><div id=\"root\"></div>"
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DistDir: distDir,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/server", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), indexHTML) {
		t.Fatalf("expected dist index body, got %q", res.Body.String())
	}
}

func TestStaticRouteServesPublicAssets(t *testing.T) {
	distDir := t.TempDir()
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "logo.svg"), []byte("<svg></svg>"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newTestServer(t, config.Config{
		Version:   "test",
		Host:      "127.0.0.1",
		Port:      0,
		DistDir:   distDir,
		PublicDir: publicDir,
		DataDir:   t.TempDir(),
		DBName:    "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "<svg></svg>" {
		t.Fatalf("expected public logo body, got %q", res.Body.String())
	}
}

func TestStaticRouteFallsBackToHashedDistLogo(t *testing.T) {
	distDir := t.TempDir()
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "logo-hash.svg"), []byte("<svg>hashed</svg>"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DistDir: distDir,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "<svg>hashed</svg>" {
		t.Fatalf("expected hashed dist logo body, got %q", res.Body.String())
	}
}

func TestStaticRouteRejectsEncodedPathTraversal(t *testing.T) {
	root := t.TempDir()
	distDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("do not serve"), 0o600); err != nil {
		t.Fatal(err)
	}

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DistDir: distDir,
		DataDir: filepath.Join(root, "data"),
		DBName:  "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/%2e%2e/secret.txt", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "do not serve") {
		t.Fatalf("path traversal response leaked file body: %q", res.Body.String())
	}
}

func TestMigrationStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/migration/status", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			LegacyEnabled bool     `json:"legacyEnabled"`
			Retired       []string `json:"retired"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success {
		t.Fatal("expected success envelope")
	}
	if payload.Data.LegacyEnabled {
		t.Fatal("legacy adapter should be disabled in test config")
	}
	if len(payload.Data.Retired) != 0 {
		t.Fatalf("retired modules = %#v", payload.Data.Retired)
	}
}

func TestRetiredMusicRouteIsRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/music/search", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestRetiredCloudflareFallbackIsRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts/cf_smoke/unimplemented-deep-path", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestRemovedCloudflareFallbackDoesNotProxyToLegacyAdapter(t *testing.T) {
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("removed route should not be proxied to legacy: %s", r.URL.Path)
	}))
	defer legacy.Close()

	handler := newTestServer(t, config.Config{
		Version:       "test",
		Host:          "127.0.0.1",
		Port:          0,
		LegacyBaseURL: legacy.URL,
		DataDir:       t.TempDir(),
		DBName:        "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts/cf_smoke/unimplemented-deep-path?page=1", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestCloudflareDeepFallbackIsRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts/cf_smoke/unimplemented-deep-path", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("deep cloudflare route status = %d, want %d; body=%s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestServerInventoryRoutesRequireSession(t *testing.T) {
	handler := testServer(t)
	for _, path := range []string{
		"/api/server/s",
		"/api/server/s/server-1",
		"/api/server/s/server-1/history",
		"/api/server/accounts",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d; body=%s", path, res.Code, http.StatusUnauthorized, res.Body.String())
		}
	}
}

func TestUptimeRoutesAreGoOwnedWithInternalAuthSplit(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated monitors status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/uptime/push/not-a-token", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("public push status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/uptime/public/status-pages/missing", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("public status page status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/uptime/public/badge/999", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("public badge status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/uptime/monitors", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated monitors status = %d body=%s", res.Code, res.Body.String())
	}
	var monitors []map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &monitors); err != nil {
		t.Fatalf("decode monitors: %v body=%s", err, res.Body.String())
	}
}

func TestKoyebRoutesAreGoOwnedAndRequireSession(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/koyeb/accounts", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated koyeb accounts status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/koyeb/accounts", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated koyeb accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success  bool                     `json:"success"`
		Accounts []map[string]interface{} `json:"accounts"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode koyeb accounts: %v body=%s", err, res.Body.String())
	}
	if !payload.Success || len(payload.Accounts) != 0 {
		t.Fatalf("unexpected koyeb accounts payload: %#v", payload)
	}
}

func TestFlyioRoutesAreGoOwnedAndRequireSession(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/flyio/accounts", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated flyio accounts status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/flyio/accounts", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated flyio accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode flyio accounts: %v body=%s", err, res.Body.String())
	}
	if !payload.Success || len(payload.Data) != 0 {
		t.Fatalf("unexpected flyio accounts payload: %#v", payload)
	}
}

func TestAliyunRoutesAreGoOwnedAndRequireSession(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/aliyun/accounts", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated aliyun accounts status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/aliyun/accounts", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated aliyun accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var payload []map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode aliyun accounts: %v body=%s", err, res.Body.String())
	}
	if len(payload) != 0 {
		t.Fatalf("unexpected aliyun accounts payload: %#v", payload)
	}
}

func TestTencentRoutesAreGoOwnedAndRequireSession(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/tencent/accounts", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated tencent accounts status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/tencent/accounts", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated tencent accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var payload []map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tencent accounts: %v body=%s", err, res.Body.String())
	}
	if len(payload) != 0 {
		t.Fatalf("unexpected tencent accounts payload: %#v", payload)
	}
}

func TestCloudflareAccountRoutesAreGoOwnedAndRequireSession(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated cloudflare accounts status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/accounts", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated cloudflare accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var payload []map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cloudflare accounts: %v body=%s", err, res.Body.String())
	}
	if len(payload) != 0 {
		t.Fatalf("unexpected cloudflare accounts payload: %#v", payload)
	}
}

func TestAuthPasswordSessionLogoutFlow(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/check-password", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("check-password status = %d", res.Code)
	}
	var checkPayload struct {
		HasPassword bool `json:"hasPassword"`
		IsDemoMode  bool `json:"isDemoMode"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &checkPayload); err != nil {
		t.Fatal(err)
	}
	if checkPayload.HasPassword || checkPayload.IsDemoMode {
		t.Fatalf("unexpected initial check payload: %#v", checkPayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/set-password", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:1234"
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:1234"
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	cookies := res.Result().Cookies()
	var sidCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "sid" {
			sidCookie = cookie
			break
		}
	}
	if sidCookie == nil || sidCookie.Value == "" || !sidCookie.HttpOnly {
		t.Fatalf("expected httpOnly sid cookie, got %#v", cookies)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("session status = %d body=%s", res.Code, res.Body.String())
	}
	var sessionPayload struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatal(err)
	}
	if !sessionPayload.Authenticated {
		t.Fatal("expected authenticated session")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("post-logout session status = %d body=%s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatal(err)
	}
	if sessionPayload.Authenticated {
		t.Fatal("expected logout to invalidate session")
	}
}

func TestAuth2FAManagementRoutesAreServedByGo(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/2fa/status", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Enabled {
		t.Fatalf("unexpected 2fa status payload: %#v", payload)
	}
}

func TestCoreSettingsRequireSessionAndAreServedByGo(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated settings status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/set-password", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	var sidCookie *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "sid" {
			sidCookie = cookie
			break
		}
	}
	if sidCookie == nil {
		t.Fatal("expected sid cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated settings status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			ModuleVisibility map[string]bool `json:"moduleVisibility"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.ModuleVisibility["scheduler"] {
		t.Fatalf("unexpected settings payload: %#v", payload)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/database-stats", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated database-stats status = %d body=%s", res.Code, res.Body.String())
	}
	var statsPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Tables map[string]int64 `json:"tables"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &statsPayload); err != nil {
		t.Fatal(err)
	}
	if !statsPayload.Success || statsPayload.Data.Tables["user_settings"] != 1 {
		t.Fatalf("unexpected database stats payload: %#v", statsPayload)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/log-settings", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated log-settings status = %d body=%s", res.Code, res.Body.String())
	}
	var logSettingsPayload struct {
		Success bool `json:"success"`
		Data    struct {
			LogFileSizeMB int `json:"logFileSizeMB"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &logSettingsPayload); err != nil {
		t.Fatal(err)
	}
	if !logSettingsPayload.Success || logSettingsPayload.Data.LogFileSizeMB != 10 {
		t.Fatalf("unexpected log settings payload: %#v", logSettingsPayload)
	}
}

func TestSettingsDatabaseReadRoutesRequireSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/settings/database-stats", nil)
	res := httptest.NewRecorder()

	testServer(t).ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusUnauthorized, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/operation-logs", nil)
	res = httptest.NewRecorder()
	testServer(t).ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("operation-logs status = %d, want %d; body=%s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
}

func TestCronRoutesRequireSessionAndAreServedByGo(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cron/tasks", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated cron tasks status = %d body=%s", res.Code, res.Body.String())
	}

	sidCookie := loginServerForTest(t, handler)

	req = httptest.NewRequest(http.MethodGet, "/api/cron/tasks", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated cron tasks status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool          `json:"success"`
		Data    []interface{} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data == nil {
		t.Fatalf("unexpected cron tasks payload: %#v", payload)
	}
}

func TestNotificationRoutesRequireSessionAndAreServedByGo(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/notification/channels", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated notification channels status = %d body=%s", res.Code, res.Body.String())
	}

	sidCookie := loginServerForTest(t, handler)

	req = httptest.NewRequest(http.MethodPost, "/api/notification/channels", strings.NewReader(`{
		"name":"Ops Telegram",
		"type":"telegram",
		"enabled":true,
		"config":{"bot_token":"123456:test-token","chat_id":"10001"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("create notification channel status = %d body=%s", res.Code, res.Body.String())
	}
	var channelPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ID     string                 `json:"id"`
			Config map[string]interface{} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &channelPayload); err != nil {
		t.Fatal(err)
	}
	if !channelPayload.Success || channelPayload.Data.ID == "" || channelPayload.Data.Config["chat_id"] != "10001" {
		t.Fatalf("unexpected channel payload: %#v", channelPayload)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/notification/event-catalog", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("event catalog status = %d body=%s", res.Code, res.Body.String())
	}
	var catalogPayload struct {
		Success bool `json:"success"`
		Data    []struct {
			Module string `json:"module"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &catalogPayload); err != nil {
		t.Fatal(err)
	}
	for _, item := range catalogPayload.Data {
		if item.Module == "music" || item.Module == "openlist" {
			t.Fatalf("retired module leaked into notification event catalog: %#v", catalogPayload.Data)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/notification/rules", strings.NewReader(`{
		"name":"Uptime Down",
		"source_module":"uptime",
		"event_type":"down",
		"severity":"critical",
		"channels":["`+channelPayload.Data.ID+`"],
		"title_template":"[{{severity}}] {{monitorName}}",
		"message_template":"{{monitorName}} failed: {{error}}"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("create notification rule status = %d body=%s", res.Code, res.Body.String())
	}
	var rulePayload struct {
		Success bool `json:"success"`
		Data    struct {
			ID       string   `json:"id"`
			Channels []string `json:"channels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &rulePayload); err != nil {
		t.Fatal(err)
	}
	if !rulePayload.Success || rulePayload.Data.ID == "" || rulePayload.Data.Channels[0] != channelPayload.Data.ID {
		t.Fatalf("unexpected rule payload: %#v", rulePayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/notification/rules/"+rulePayload.Data.ID+"/dry-run", strings.NewReader(`{
		"data":{"severity":"critical","monitorId":"monitor-1","monitorName":"API Gateway","error":"timeout"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("notification dry-run status = %d body=%s", res.Code, res.Body.String())
	}
	var dryRunPayload struct {
		Success bool `json:"success"`
		Data    struct {
			WouldNotify bool   `json:"wouldNotify"`
			Title       string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &dryRunPayload); err != nil {
		t.Fatal(err)
	}
	if !dryRunPayload.Success || !dryRunPayload.Data.WouldNotify || dryRunPayload.Data.Title != "[critical] API Gateway" {
		t.Fatalf("unexpected dry-run payload: %#v", dryRunPayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/notification/rules", strings.NewReader(`{
		"name":"OpenList Rule",
		"source_module":"openlist",
		"event_type":"down",
		"channels":["`+channelPayload.Data.ID+`"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("retired OpenList rule status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestSystemHostMetricsRequireSessionAndAreServedByGo(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/system/host-metrics", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated host metrics status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/set-password", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	var sidCookie *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "sid" {
			sidCookie = cookie
			break
		}
	}
	if sidCookie == nil {
		t.Fatal("expected sid cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/system/host-metrics", nil)
	req.AddCookie(sidCookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("host metrics status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Platform string `json:"platform"`
			CPU      struct {
				Cores int `json:"cores"`
			} `json:"cpu"`
			Memory struct {
				Total uint64 `json:"total"`
			} `json:"memory"`
			Disk struct {
				Root string `json:"root"`
			} `json:"disk"`
			Process struct {
				MemoryRSS uint64 `json:"memoryRss"`
			} `json:"process"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Platform == "" || payload.Data.CPU.Cores < 1 || payload.Data.Memory.Total == 0 || payload.Data.Disk.Root == "" || payload.Data.Process.MemoryRSS == 0 {
		t.Fatalf("unexpected host metrics payload: %#v", payload)
	}
}

func TestOpenAIServerRouting(t *testing.T) {
	handler := testServer(t)

	// 1. OpenAI endpoint CRUD requires session
	req := httptest.NewRequest(http.MethodGet, "/api/openai/endpoints", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated openai endpoints status = %d", res.Code)
	}

	cookie := loginServerForTest(t, handler)
	req = httptest.NewRequest(http.MethodGet, "/api/openai/endpoints", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authenticated openai endpoints status = %d body=%s", res.Code, res.Body.String())
	}

	// 2. GET /v1/models is public
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("v1 models status = %d body=%s", res.Code, res.Body.String())
	}
}
