package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
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
	dataDir := cfg.DataDir
	dbPath := filepath.Join(dataDir, cfg.DBName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := handler.Shutdown(ctx); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
		// 等待后台 goroutine（代理预热等）释放数据库文件句柄，
		// 避免 Linux CI 上 t.TempDir 清理时报 directory not empty。
		if dbPath != "" {
			for i := 0; i < 100; i++ {
				if err := os.Remove(dbPath); err == nil {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
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

func TestAuthSubroutesAreForwarded(t *testing.T) {
	handler := testServer(t)

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "login options", method: http.MethodGet, path: "/api/auth/login-options", wantStatus: http.StatusOK},
		{name: "github config", method: http.MethodGet, path: "/api/auth/github/config", wantStatus: http.StatusUnauthorized},
		{name: "webauthn credentials", method: http.MethodGet, path: "/api/auth/webauthn/credentials", wantStatus: http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			if res.Code != tc.wantStatus {
				t.Fatalf("%s status = %d, want %d; body=%s", tc.path, res.Code, tc.wantStatus, res.Body.String())
			}
			if res.Code == http.StatusNotFound {
				t.Fatalf("%s unexpectedly fell through router; body=%s", tc.path, res.Body.String())
			}
		})
	}
}

func TestAdminAIMemoriesRouteForwarded(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/memories", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code == http.StatusNotFound {
		t.Fatalf("/api/admin-ai/memories unexpectedly fell through router; body=%s", res.Body.String())
	}
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("/api/admin-ai/memories status = %d, want 401 (session required); body=%s", res.Code, res.Body.String())
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
	if result["content"] == nil || len(result["content"].([]interface{})) == 0 {
		t.Fatalf("expected MCP content block, got %#v", result)
	}
	callResult := result["structuredContent"].(map[string]interface{})
	if callResult["statusCode"].(float64) != 200 {
		t.Fatalf("expected proxied status 200, got %#v", callResult)
	}
	bodyPayload := callResult["body"].(map[string]interface{})
	if bodyPayload["success"] != true {
		t.Fatalf("expected envelope success, got %#v", bodyPayload)
	}
}

func TestAIMCPWriteGatingAndKeyRotationBlock(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	getAgentKey := func() (string, bool) {
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
		policy := overview["policy"].(map[string]interface{})
		writeEnabled := policy["writeEnabled"].(bool)
		return overview["agentKey"].(map[string]interface{})["value"].(string), writeEnabled
	}

	agentKey, writeEnabled := getAgentKey()
	if writeEnabled {
		t.Fatal("expected write access to default to disabled")
	}

	callTools := func(name string, args map[string]interface{}) map[string]interface{} {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params":  map[string]interface{}{"name": name, "arguments": args},
		}
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/ai/mcp", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("mcp status = %d, body=%s", res.Code, res.Body.String())
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	// 写入方法在未开启时被拒绝
	payload := callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/backup/run"})
	if payload["error"] == nil {
		t.Fatalf("expected write call to be rejected, got %#v", payload)
	}
	if !strings.Contains(payload["error"].(map[string]interface{})["message"].(string), "写入") {
		t.Fatalf("unexpected rejection message: %#v", payload["error"])
	}

	// 密钥轮换别名路径必须始终被屏蔽
	payload = callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/ai-access/key/rotate"})
	if payload["error"] == nil {
		t.Fatalf("expected key rotate alias to be blocked, got %#v", payload)
	}

	// 开启写入后允许写操作
	writeReq := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/write", strings.NewReader(`{"writeEnabled":true}`))
	writeReq.AddCookie(cookie)
	writeReq.Header.Set("Content-Type", "application/json")
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusOK {
		t.Fatalf("enable write status = %d, body=%s", writeRes.Code, writeRes.Body.String())
	}

	if _, writeEnabled := getAgentKey(); !writeEnabled {
		t.Fatal("expected write access to be enabled after toggle")
	}

	payload = callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/backup/run"})
	if payload["error"] != nil {
		t.Fatalf("expected write call to succeed after enabling, got %#v", payload)
	}
	result := payload["result"].(map[string]interface{})
	callResult := result["structuredContent"].(map[string]interface{})
	if callResult["statusCode"].(float64) != 200 {
		t.Fatalf("expected proxied status 200, got %#v", callResult)
	}
}

func TestAIMCPCannotSelfApproveAdminRoutes(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	keyReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	keyReq.AddCookie(cookie)
	keyRes := httptest.NewRecorder()
	handler.ServeHTTP(keyRes, keyReq)
	var keyPayload map[string]interface{}
	if err := json.Unmarshal(keyRes.Body.Bytes(), &keyPayload); err != nil {
		t.Fatal(err)
	}
	overview := keyPayload["data"].(map[string]interface{})
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)

	// 启用写入，确保 admin-ai 屏蔽不是写开关的副作用
	writeReq := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/write", strings.NewReader(`{"writeEnabled":true}`))
	writeReq.AddCookie(cookie)
	writeReq.Header.Set("Content-Type", "application/json")
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusOK {
		t.Fatalf("enable write status = %d, body=%s", writeRes.Code, writeRes.Body.String())
	}

	callTools := func(name string, args map[string]interface{}) map[string]interface{} {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params":  map[string]interface{}{"name": name, "arguments": args},
		}
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/ai/mcp", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		var payload map[string]interface{}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode mcp response: %v body=%s", err, res.Body.String())
		}
		return payload
	}

	// 枚举待批项必须被拒
	payload := callTools("call_api", map[string]interface{}{"method": "GET", "path": "/api/admin-ai/approvals"})
	if payload["error"] == nil {
		t.Fatalf("AI must not enumerate admin approvals, got %#v", payload)
	}
	if !strings.Contains(payload["error"].(map[string]interface{})["message"].(string), "不允许") {
		t.Fatalf("unexpected block message: %#v", payload["error"])
	}

	// resolve 自批必须被拒
	payload = callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/admin-ai/approvals/aaa_x/resolve", "body": map[string]interface{}{"action": "approve"}})
	if payload["error"] == nil {
		t.Fatalf("AI must not self-resolve approvals, got %#v", payload)
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

func TestStaticRouteServesDirectoryIndex(t *testing.T) {
	distDir := t.TempDir()
	drawioDir := filepath.Join(distDir, "vendor", "drawio")
	if err := os.MkdirAll(drawioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<title>API Monitor</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(drawioDir, "index.html"), []byte("<title>Draw.io</title>"), 0o644); err != nil {
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
	req := httptest.NewRequest(http.MethodGet, "/vendor/drawio/?embed=1&proto=json", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "<title>Draw.io</title>") {
		t.Fatalf("expected directory index body, got %q", res.Body.String())
	}
}

func TestSiteBrandIconRoutesRequireSessionAndPublicIconRouteServesUploadedIcon(t *testing.T) {
	distDir := t.TempDir()
	publicDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "logo.svg"), []byte("<svg>default</svg>"), 0o644); err != nil {
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

	req := httptest.NewRequest(http.MethodGet, "/api/settings/site-brand/icons", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated site brand icon list status = %d body=%s", res.Code, res.Body.String())
	}

	cookie := loginServerForTest(t, handler)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "公开页图标"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "brand.svg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><text x="1" y="12">custom-brand</text></svg>`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/settings/site-brand/icons", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload site brand icon status = %d body=%s", res.Code, res.Body.String())
	}
	var uploadPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &uploadPayload); err != nil {
		t.Fatalf("decode upload payload: %v body=%s", err, res.Body.String())
	}
	if !uploadPayload.Success || uploadPayload.Data.ID == "" || uploadPayload.Data.URL == "" {
		t.Fatalf("unexpected upload payload: %#v", uploadPayload)
	}

	req = httptest.NewRequest(http.MethodGet, uploadPayload.Data.URL, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("site brand asset route should require session, status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/site-brand-icons/"+uploadPayload.Data.ID, nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("public site brand icon status = %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("public site brand icon cache-control = %q", got)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("public site brand icon content type = %q", res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), "custom-brand") {
		t.Fatalf("expected public icon route to serve uploaded icon, got %q", res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("public logo status = %d body=%s", res.Code, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "<svg>default</svg>" {
		t.Fatalf("expected public logo body to stay default, got %q", res.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/settings/site-brand/icons/"+uploadPayload.Data.ID, nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete site brand icon status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, uploadPayload.Data.URL, nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("deleted site brand asset route status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestStaticRouteCachePolicy(t *testing.T) {
	distDir := t.TempDir()
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app-contenthash.js"), []byte("export {};"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newTestServer(t, config.Config{
		Version: "test", Host: "127.0.0.1", DistDir: distDir,
		DataDir: t.TempDir(), DBName: "data.db",
	})

	assetReq := httptest.NewRequest(http.MethodGet, "/assets/app-contenthash.js", nil)
	assetRes := httptest.NewRecorder()
	handler.ServeHTTP(assetRes, assetReq)
	if got := assetRes.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control=%q", got)
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRes := httptest.NewRecorder()
	handler.ServeHTTP(indexRes, indexReq)
	if got := indexRes.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index Cache-Control=%q", got)
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
		"/api/server/accounts/server-1/test-traffic-alert",
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

func TestPluginCompatibleRelayRoutesAreInternalOnly(t *testing.T) {
	handler := testServer(t)

	// 外部来源（非 loopback）：插件 /v1 兼容中继应被 AuthInternal 拒绝。
	for _, path := range []string{"/api/antigravity/v1/messages", "/api/ds2api/v1/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.RemoteAddr = "203.0.113.9:12345"
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s from external source = %d body=%s, want 403", path, res.Code, res.Body.String())
		}
	}

	// 本机回环来源：放行到插件（鉴权通过，落点行为与插件内部一致）。
	req := httptest.NewRequest(http.MethodPost, "/api/ds2api/v1/chat/completions", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:12345"
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code == http.StatusForbidden || res.Code == http.StatusUnauthorized {
		t.Fatalf("loopback internal relay = %d body=%s, want non-auth reject", res.Code, res.Body.String())
	}
}

func TestM365PublicRegistrationRoutesBypassSessionAuth(t *testing.T) {	handler := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/m365/public/register?code=missing", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("public register descriptor status = %d body=%s", res.Code, res.Body.String())
	}
	var descriptorPayload struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &descriptorPayload); err != nil {
		t.Fatalf("decode public register descriptor: %v body=%s", err, res.Body.String())
	}
	if !descriptorPayload.Success || descriptorPayload.Data["method"] != "POST" {
		t.Fatalf("unexpected public register descriptor payload: %#v", descriptorPayload)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/m365/public/invites/missing", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("public invite descriptor status = %d body=%s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "请先登录") {
		t.Fatalf("public invite route should bypass session auth: %s", res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/m365/public/register", strings.NewReader(`{"mailNickname":"user1"}`))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("public register post status = %d body=%s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "请先登录") {
		t.Fatalf("public register post should bypass session auth: %s", res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/m365/accounts", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("private m365 accounts status = %d body=%s", res.Code, res.Body.String())
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

func TestPluginTokenIsRestrictedToTOTPAccountReads(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	issueReq := httptest.NewRequest(http.MethodPost, "/api/auth/plugin-token", nil)
	issueReq.AddCookie(cookie)
	issueRes := httptest.NewRecorder()
	handler.ServeHTTP(issueRes, issueReq)
	if issueRes.Code != http.StatusOK {
		t.Fatalf("issue token status = %d body=%s", issueRes.Code, issueRes.Body.String())
	}
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issueRes.Body.Bytes(), &issued); err != nil || !strings.HasPrefix(issued.Token, "akp_") {
		t.Fatalf("unexpected token response: %s", issueRes.Body.String())
	}

	accountsReq := httptest.NewRequest(http.MethodGet, "/api/totp/accounts?withCodes=true", nil)
	accountsReq.Header.Set("Authorization", "Bearer "+issued.Token)
	accountsRes := httptest.NewRecorder()
	handler.ServeHTTP(accountsRes, accountsReq)
	if accountsRes.Code != http.StatusOK {
		t.Fatalf("plugin account read status = %d body=%s", accountsRes.Code, accountsRes.Body.String())
	}

	settingsReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	settingsReq.Header.Set("Authorization", "Bearer "+issued.Token)
	settingsRes := httptest.NewRecorder()
	handler.ServeHTTP(settingsRes, settingsReq)
	if settingsRes.Code != http.StatusUnauthorized {
		t.Fatalf("plugin token accessed settings: %d body=%s", settingsRes.Code, settingsRes.Body.String())
	}

	writeReq := httptest.NewRequest(http.MethodPost, "/api/totp/accounts", strings.NewReader(`{}`))
	writeReq.Header.Set("Authorization", "Bearer "+issued.Token)
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusUnauthorized {
		t.Fatalf("plugin token wrote TOTP data: %d body=%s", writeRes.Code, writeRes.Body.String())
	}
}

func TestCrossOriginCookieRequestIsRejected(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://attacker.example")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("cross-origin session status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestDevProxyCookieRequestUsesForwardedHostForSameOrigin(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(cookie)
	req.RemoteAddr = "127.0.0.1:5173"
	req.Host = "127.0.0.1:3000"
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("X-Forwarded-Host", "localhost:5173")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("dev proxy same-origin status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestDevProxyCookieRequestAllowsPrivateOriginWithoutForwardedHost(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"theme":"dark"}`))
	req.AddCookie(cookie)
	req.RemoteAddr = "127.0.0.1:5173"
	req.Host = "127.0.0.1:3000"
	req.Header.Set("Origin", "http://192.168.10.3:5173")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("dev proxy private-origin status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestDevProxyCookieRequestStillRejectsPublicOrigin(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"theme":"dark"}`))
	req.AddCookie(cookie)
	req.RemoteAddr = "127.0.0.1:5173"
	req.Host = "127.0.0.1:3000"
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("dev proxy public-origin status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestGatewayRoutesEmitWildcardCORSHeaders(t *testing.T) {
	handler := testServer(t)

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "https://chat.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("preflight ACAO = %q, want *", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("preflight ACAC must stay unset, got %q", got)
	}
	allowHeaders := strings.ToLower(res.Header().Get("Access-Control-Allow-Headers"))
	for _, want := range []string{"authorization", "content-type", "x-api-key"} {
		if !strings.Contains(allowHeaders, want) {
			t.Fatalf("preflight allow-headers missing %q, got %q", want, res.Header().Get("Access-Control-Allow-Headers"))
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "https://chat.example.com")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("response ACAO = %q, want *", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("response ACAC must stay unset, got %q", got)
	}
}

func TestGatewayRootPathEmitsWildcardCORS(t *testing.T) {
	handler := testServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/v1", nil)
	req.Header.Set("Origin", "app://obsidian.md")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("preflight ACAO = %q, want *", got)
	}
}

func TestNonGatewayRoutesDoNotEmitWildcardCORS(t *testing.T) {
	handler := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("Origin", "https://attacker.example.com")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("non-gateway route leaked ACAO %q", got)
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
			Tables      map[string]int64 `json:"tables"`
			CountsExact bool             `json:"countsExact"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &statsPayload); err != nil {
		t.Fatal(err)
	}
	if !statsPayload.Success || statsPayload.Data.CountsExact || statsPayload.Data.Tables["user_settings"] != -1 {
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

func TestInternalCronReadonlyWhitelist(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")

	handler := newTestServer(t, config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	cronReq := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-Internal-Cron", "true")
		req.RemoteAddr = "127.0.0.1:12345"
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}

	// 白名单内业务家族只读 GET 放行
	for _, path := range []string{
		"/api/system/host-metrics",
		"/api/server/s",
		"/api/cloudflare/zones",
		"/api/openai/analytics/summary",
		"/api/totp/accounts",
		"/api/notification/channels",
		"/api/drawio/documents",
		"/api/prompts/entries",
		"/api/aliyun/accounts",
		"/api/koyeb/data",
		"/api/github/tokens",
	} {
		res := cronReq(http.MethodGet, path)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s with internal cron header status = %d body=%s", path, res.Code, res.Body.String())
		}
	}

	// 白名单内路径的写操作拒绝
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/scheduler/workflows"},
		{http.MethodDelete, "/api/prompts/entries/1"},
		{http.MethodPut, "/api/totp/accounts/1"},
		{http.MethodPost, "/api/cloudflare/zones"},
	} {
		res := cronReq(tc.method, tc.path)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s with internal cron header status = %d (want 401) body=%s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}

	// 带模块级二次鉴权的模块不放行（uptime）
	res := cronReq(http.MethodGet, "/api/uptime/monitors")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/uptime/monitors with internal cron header status = %d (want 401) body=%s", res.Code, res.Body.String())
	}

	// 外部地址伪造 X-Internal-Cron 头不放行
	req := httptest.NewRequest(http.MethodGet, "/api/system/host-metrics", nil)
	req.Header.Set("X-Internal-Cron", "true")
	req.RemoteAddr = "203.0.113.5:12345"
	resFake := httptest.NewRecorder()
	handler.ServeHTTP(resFake, req)
	if resFake.Code != http.StatusUnauthorized {
		t.Fatalf("external addr with internal cron header status = %d (want 401) body=%s", resFake.Code, resFake.Body.String())
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

	// 2. OpenAI-compatible routes require a managed gateway key.
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("v1 models without key status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/openai/keys", strings.NewReader(`{"name":"test client"}`))
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create gateway key status = %d body=%s", res.Code, res.Body.String())
	}
	var keyPayload struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &keyPayload); err != nil || keyPayload.APIKey == "" {
		t.Fatalf("decode gateway key: %v body=%s", err, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/openai/keys", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list gateway keys status = %d body=%s", res.Code, res.Body.String())
	}
	var listedKeys []struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listedKeys); err != nil || len(listedKeys) != 1 || listedKeys[0].APIKey != keyPayload.APIKey {
		t.Fatalf("listed gateway key is not recoverable: %v body=%s", err, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+keyPayload.APIKey)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("v1 models with key status = %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/openai/analytics/logs?days=1&page=1&pageSize=20", nil)
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("analytics logs status = %d body=%s", res.Code, res.Body.String())
	}
	var analyticsPayload struct {
		Records []struct {
			Route string `json:"route"`
		} `json:"records"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &analyticsPayload); err != nil {
		t.Fatalf("unmarshal analytics logs: %v body=%s", err, res.Body.String())
	}
	for _, record := range analyticsPayload.Records {
		if record.Route == "models" {
			t.Fatalf("models request should not pollute analytics logs: %+v", analyticsPayload.Records)
		}
	}

	// 7. key-check 路由：端点 API Key 批量检测需要会话鉴权。
	req = httptest.NewRequest(http.MethodPost, "/api/openai/endpoints", strings.NewReader(`{
		"name":"key-check-test","baseUrl":"https://example.com/v1","apiKey":"sk-test-main"
	}`))
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK && res.Code != http.StatusCreated {
		t.Fatalf("create endpoint for key-check status = %d body=%s", res.Code, res.Body.String())
	}
	var createdEndpoint struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &createdEndpoint); err != nil || createdEndpoint.ID == "" {
		var createdEnvelope struct {
			Endpoint struct {
				ID string `json:"id"`
			} `json:"endpoint"`
		}
		if envErr := json.Unmarshal(res.Body.Bytes(), &createdEnvelope); envErr != nil || createdEnvelope.Endpoint.ID == "" {
			t.Fatalf("decode created endpoint: %v body=%s", err, res.Body.String())
		}
		createdEndpoint.ID = createdEnvelope.Endpoint.ID
	}

	req = httptest.NewRequest(http.MethodPost, "/api/openai/endpoints/"+createdEndpoint.ID+"/key-check", strings.NewReader(`{"keys":["sk-test-main","sk-test-backup"],"timeout":3000}`))
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("key-check status = %d body=%s", res.Code, res.Body.String())
	}
	var keyCheckPayload struct {
		Results []struct {
			Index  int    `json:"index"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &keyCheckPayload); err != nil {
		t.Fatalf("unmarshal key-check: %v body=%s", err, res.Body.String())
	}
	if len(keyCheckPayload.Results) != 2 {
		t.Fatalf("key-check results count = %d, want 2: %s", len(keyCheckPayload.Results), res.Body.String())
	}
}

func TestPublicPageFaviconResolver(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
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

	// 通过公开路由触发 uptime schema 初始化，然后直插测试数据。
	prime := httptest.NewRequest(http.MethodGet, "/api/uptime/public/status-pages/init-schema", nil)
	handler.ServeHTTP(httptest.NewRecorder(), prime)

	dbCfg := config.Config{Version: "test", Host: "127.0.0.1", Port: 0, DataDir: handler.cfg.DataDir, DBName: handler.cfg.DBName}
	ctx := context.Background()
	store := database.New(dbCfg)
	db, err := store.Open(ctx)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	_, err = db.Exec(`INSERT INTO uptime_status_pages (slug, domain, title, public, cache_seconds, config_json)
		VALUES ('fav-slug', 'fav.example.com', 'Fav Test', 1, 300, '{"publicIconId":"site-custom"}')`)
	db.Close()
	if err != nil {
		t.Fatalf("insert uptime status page: %v", err)
	}

	cases := []struct {
		name     string
		path     string
		wantCode int
		wantLoc  string
	}{
		{
			name:     "custom icon redirects to brand icon asset",
			path:     "/public-page-favicon/uptime/fav-slug",
			wantCode: http.StatusTemporaryRedirect,
			wantLoc:  "/site-brand-icons/site-custom",
		},
		{
			name:     "custom icon by domain probe",
			path:     "/public-page-favicon/domain/fav.example.com",
			wantCode: http.StatusTemporaryRedirect,
			wantLoc:  "/site-brand-icons/site-custom",
		},
		{
			name:     "missing page falls back to default logo",
			path:     "/public-page-favicon/uptime/no-such-slug",
			wantCode: http.StatusTemporaryRedirect,
			wantLoc:  "/logo-default.svg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, tc.wantCode, res.Body.String())
			}
			if loc := res.Header().Get("Location"); loc != tc.wantLoc {
				t.Fatalf("location = %q, want %q", loc, tc.wantLoc)
			}
		})
	}

	// 未自定义图标的公开页直接返回默认 glyph（无额外跳转）。
	db, err = store.Open(ctx)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	_, err = db.Exec(`INSERT INTO uptime_status_pages (slug, title, public, cache_seconds, config_json)
		VALUES ('plain-slug', 'Plain', 1, 300, '{}')`)
	db.Close()
	if err != nil {
		t.Fatalf("insert plain uptime status page: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/public-page-favicon/uptime/plain-slug", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("glyph status = %d body=%s", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("glyph content type = %q, want image/svg+xml", ct)
	}
	if body := res.Body.String(); !strings.HasPrefix(body, "<svg") || !strings.Contains(body, "#f48120") {
		t.Fatalf("unexpected glyph body: %q", body)
	}

	// 未知 kind 不应被解析端点吞掉，回落 SPA。
	req = httptest.NewRequest(http.MethodGet, "/public-page-favicon/unknown/whatever", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "root") {
		t.Fatalf("unknown kind should fall through to SPA, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestPublicBookmarksFaviconAndPublicPage(t *testing.T) {
	distDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><div id=\"root\"></div>"), 0o644); err != nil {
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

	// 通过公开路由触发 bookmarks schema 初始化。
	prime := httptest.NewRequest(http.MethodGet, "/api/bookmarks/public/groups/init-schema", nil)
	handler.ServeHTTP(httptest.NewRecorder(), prime)

	dbCfg := config.Config{Version: "test", Host: "127.0.0.1", Port: 0, DataDir: handler.cfg.DataDir, DBName: handler.cfg.DBName}
	ctx := context.Background()
	store := database.New(dbCfg)
	db, err := store.Open(ctx)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	_, err = db.Exec(`INSERT INTO bookmark_groups (slug, domain, title, public, cache_seconds, config_json)
		VALUES ('fav-slug', 'fav.example.com', 'Fav Group', 1, 300, '{"publicIconId":"site-custom"}'),
		       ('plain-slug', '', 'Plain Group', 1, 300, '{}')`)
	if err != nil {
		db.Close()
		t.Fatalf("insert bookmark groups: %v", err)
	}
	var groupID int64
	if err := db.QueryRow(`SELECT id FROM bookmark_groups WHERE slug = 'fav-slug'`).Scan(&groupID); err != nil {
		db.Close()
		t.Fatalf("select group id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO bookmarks (group_id, title, url, sort_order) VALUES (?, 'GitHub', 'https://github.com', 0)`, groupID)
	db.Close()
	if err != nil {
		t.Fatalf("insert bookmark item: %v", err)
	}

	// favicon：自定义图标 302 到品牌图标资产
	req := httptest.NewRequest(http.MethodGet, "/public-page-favicon/bookmarks/fav-slug", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusTemporaryRedirect || res.Header().Get("Location") != "/site-brand-icons/site-custom" {
		t.Fatalf("bookmarks favicon custom: status=%d loc=%q", res.Code, res.Header().Get("Location"))
	}

	// favicon：未自定义返回默认 glyph
	req = httptest.NewRequest(http.MethodGet, "/public-page-favicon/bookmarks/plain-slug", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("bookmarks favicon glyph: status=%d type=%q", res.Code, res.Header().Get("Content-Type"))
	}
	if body := res.Body.String(); !strings.HasPrefix(body, "<svg") || !strings.Contains(body, "#f48120") {
		t.Fatalf("unexpected bookmarks glyph body: %q", body)
	}

	// 公开分组数据：无需登录即可读取
	req = httptest.NewRequest(http.MethodGet, "/api/bookmarks/public/groups/fav-slug", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("public group status=%d body=%s", res.Code, res.Body.String())
	}
	var publicPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Group struct {
				Title string `json:"title"`
				Items []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
				} `json:"items"`
			} `json:"group"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &publicPayload); err != nil {
		t.Fatalf("unmarshal public group: %v", err)
	}
	if !publicPayload.Success || publicPayload.Data.Group.Title != "Fav Group" {
		t.Fatalf("public group payload mismatch: %s", res.Body.String())
	}
	if len(publicPayload.Data.Group.Items) != 1 || publicPayload.Data.Group.Items[0].URL != "https://github.com" {
		t.Fatalf("public group items mismatch: %s", res.Body.String())
	}

	// 域名探测
	req = httptest.NewRequest(http.MethodGet, "/api/bookmarks/public/page-by-domain?domain=fav.example.com", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"found":true`) {
		t.Fatalf("bookmarks by-domain status=%d body=%s", res.Code, res.Body.String())
	}

	// 内部管理路由仍受登录保护（AuthSession 拦截未登录请求）
	req = httptest.NewRequest(http.MethodGet, "/api/bookmarks/groups", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("internal groups without session should 401, got %d", res.Code)
	}
}
