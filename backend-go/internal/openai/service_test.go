package openai

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	_ "modernc.org/sqlite"
)

// TestMain 固定候选选路为「第一个候选」：延迟加权选路基于 crypto/rand 随机
// （无延迟记录时候选等权），而 failover/retry 测试都按「先创建的端点先被选中」
// 构造场景，随机选路会让这批测试间歇性失败（B 被选中 → failover 断言落空）。
// 需要验证真实加权行为的测试须显式覆盖/恢复该钩子。
func TestMain(m *testing.M) {
	endpointPickOverride = func([]Endpoint) int { return 0 }
	code := m.Run()
	endpointPickOverride = nil
	os.Exit(code)
}

func TestOpenAINormalization(t *testing.T) {
	s := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	testCases := []struct {
		input    string
		expected string
	}{
		{"api.openai.com", "https://api.openai.com/v1"},
		{"http://api.openai.com/v1/chat/completions", "http://api.openai.com/v1"},
		{"https://api.openai.com/v1/models/", "https://api.openai.com/v1"},
		{"http://my-proxy.com", "http://my-proxy.com/v1"},
		{"https://my-proxy.com/v2/", "https://my-proxy.com/v2"},
	}

	for _, tc := range testCases {
		res := s.normalizeBaseURL(tc.input)
		if res != tc.expected {
			t.Errorf("normalizeBaseURL(%q) = %q; want %q", tc.input, res, tc.expected)
		}
	}
}

func TestNormalizeProtocol(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"", "auto"},
		{"auto", "auto"},
		{"AUTO", "auto"},
		{"http1", "http1"},
		{"HTTP/1.1", "http1"},
		{"h1", "http1"},
		{"h2", "h2"},
		{"HTTP2", "h2"},
		{"http/2", "h2"},
		{"quic", "auto"},
		{"  ", "auto"},
	}
	for _, tc := range testCases {
		if got := normalizeProtocol(tc.input); got != tc.expected {
			t.Errorf("normalizeProtocol(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestEffectiveProxyAttempts(t *testing.T) {
	pool := []string{"http://p1:8080", "http://p2:8080"}
	bigPool := make([]string, 0, proxyAttemptCap+5)
	for i := 1; i <= proxyAttemptCap+5; i++ {
		bigPool = append(bigPool, fmt.Sprintf("http://p%d:8080", i))
	}
	testCases := []struct {
		name     string
		ep       Endpoint
		expected int
	}{
		{
			name:     "auto switch with pool",
			ep:       Endpoint{AutoSwitch: true, ProxyEnabled: true, ProxyPool: pool},
			expected: 2,
		},
		{
			name:     "huge pool capped",
			ep:       Endpoint{AutoSwitch: true, ProxyEnabled: true, ProxyPool: bigPool},
			expected: proxyAttemptCap,
		},
		{
			name:     "proxy disabled",
			ep:       Endpoint{AutoSwitch: true, ProxyEnabled: false, ProxyPool: pool},
			expected: 1,
		},
		{
			name:     "pool empty",
			ep:       Endpoint{AutoSwitch: true, ProxyEnabled: true, ProxyPool: []string{}},
			expected: 1,
		},
		{
			name:     "pool only whitespace",
			ep:       Endpoint{AutoSwitch: true, ProxyEnabled: true, ProxyPool: []string{"  "}},
			expected: 1,
		},
		{
			name:     "auto switch off",
			ep:       Endpoint{AutoSwitch: false, ProxyEnabled: true, ProxyPool: pool},
			expected: 1,
		},
		{
			name:     "everything off",
			ep:       Endpoint{},
			expected: 1,
		},
	}
	for _, tc := range testCases {
		if got := effectiveProxyAttempts(tc.ep); got != tc.expected {
			t.Errorf("%s: effectiveProxyAttempts = %d; want %d", tc.name, got, tc.expected)
		}
	}
}

func TestClientForProtocol(t *testing.T) {
	s := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	http1 := s.clientForProtocol("http1")
	if tr, ok := http1.Transport.(*http.Transport); !ok {
		t.Fatal("clientForProtocol(http1) transport is not *http.Transport")
	} else if tr.ForceAttemptHTTP2 {
		t.Error("http1 transport must not force HTTP/2")
	} else if len(tr.TLSNextProto) != 0 {
		t.Errorf("http1 transport must disable h2 via empty TLSNextProto, got %d entries", len(tr.TLSNextProto))
	}

	h2 := s.clientForProtocol("h2")
	if tr, ok := h2.Transport.(*http.Transport); !ok {
		t.Fatal("clientForProtocol(h2) transport is not *http.Transport")
	} else if !tr.ForceAttemptHTTP2 {
		t.Error("h2 transport must force HTTP/2 attempt")
	}

	// 缓存复用：同一协议返回同一实例（连接池共享）。
	if s.clientForProtocol("http1") != http1 {
		t.Error("clientForProtocol(http1) should return the cached client")
	}
	if s.clientForProtocol("HTTP/1.1") != http1 {
		t.Error("normalized alias HTTP/1.1 should hit the same cached http1 client")
	}
	if s.clientForProtocol("") != s.clientForProtocol("auto") {
		t.Error("empty protocol should be treated as auto and cached")
	}
}

func TestEnsureSchemaMigratesGatewayAnalyticsKeyColumn(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE openai_gateway_analytics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		endpoint_id TEXT,
		model TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL,
		prompt_tokens INTEGER DEFAULT 0,
		completion_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}

	if err := ensureSchema(context.Background(), db); err != nil {
		t.Fatalf("ensureSchema failed: %v", err)
	}

	rows, err := db.Query("PRAGMA table_info(openai_gateway_analytics)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	foundGatewayKeyID := false
	foundRoute := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "gateway_key_id" {
			foundGatewayKeyID = true
		}
		if name == "route" {
			foundRoute = true
		}
	}
	if !foundGatewayKeyID {
		t.Fatal("gateway_key_id column was not added")
	}
	if !foundRoute {
		t.Fatal("route column was not added")
	}
}

func TestAnalyticsLogsRespectDaysFilter(t *testing.T) {
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO openai_gateway_analytics (endpoint_id, model, status_code, latency_ms, timestamp)
		VALUES
			('recent', 'recent-model', 200, 10, ?),
			('old', 'old-model', 200, 20, ?)
	`, time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05"), time.Now().AddDate(0, 0, -30).Format("2006-01-02 15:04:05"))
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/openai/analytics/logs?days=7&page=1&pageSize=20", nil)
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var result struct {
		Total   int `json:"total"`
		Records []struct {
			Model string `json:"model"`
		} `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Records) != 1 || result.Records[0].Model != "recent-model" {
		t.Fatalf("unexpected filtered logs: %+v", result)
	}
}

func TestRecordAnalyticsSurvivesCancelledRequestContext(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, gatewayKeyContextKey{}, gatewayKeyIdentity{ID: "key-1", Name: "client"})
	cancel()

	service.RecordAnalytics(ctx, "chat.completions", "endpoint-1", "model-1", http.StatusBadGateway, 42, 0, 0, 0, 0, 0, 0, 0, "203.0.113.9", "198.51.100.7")
	service.flushAnalyticsQueue(5 * time.Second)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var route, gatewayKeyID string
	if err := db.QueryRow("SELECT route, gateway_key_id FROM openai_gateway_analytics LIMIT 1").Scan(&route, &gatewayKeyID); err != nil {
		t.Fatal(err)
	}
	if route != "chat.completions" || gatewayKeyID != "key-1" {
		t.Fatalf("unexpected analytics identity: route=%q gateway_key_id=%q", route, gatewayKeyID)
	}
}

func TestEnsureSchemaMigratesGatewayKeyCipherColumn(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE openai_gateway_keys (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		key_suffix TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used DATETIME,
		expires_at DATETIME,
		request_count INTEGER DEFAULT 0
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureSchema(context.Background(), db); err != nil {
		t.Fatalf("ensureSchema failed: %v", err)
	}

	rows, err := db.Query("PRAGMA table_info(openai_gateway_keys)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "key_cipher" {
			found = true
		}
	}
	if !found {
		t.Fatal("key_cipher column was not added")
	}
}

func TestGatewayKeyIsStoredEncrypted(t *testing.T) {
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/openai/keys", strings.NewReader(`{"name":"desktop client"}`))
	service.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create gateway key status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		APIKey string `json:"apiKey"`
	}
	mustDecode(t, createRecorder.Body.String(), &created)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored string
	if err := db.QueryRow("SELECT key_cipher FROM openai_gateway_keys LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == created.APIKey {
		t.Fatalf("gateway key was stored as plaintext: stored=%q created=%q", stored, created.APIKey)
	}
	if !secure.IsEncrypted(stored) {
		t.Fatalf("gateway key was not encrypted: stored=%q", stored)
	}
	decrypted := secure.SecureDecrypt(stored)
	if decrypted != created.APIKey {
		t.Fatalf("decrypted key does not match original: decrypted=%q original=%q", decrypted, created.APIKey)
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/openai/keys", nil)
	service.ServeHTTP(listRecorder, listRequest)
	var listed []GatewayKey
	mustDecode(t, listRecorder.Body.String(), &listed)
	if len(listed) != 1 || listed[0].APIKey != created.APIKey {
		t.Fatalf("gateway key list did not return plaintext: %+v", listed)
	}
}

func TestGetModelsListIncludesEnabledEndpointPendingVerification(t *testing.T) {
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO openai_endpoints (id, name, base_url, api_key, status, enabled, models)
		VALUES ('pending', 'Pending endpoint', 'https://example.com/v1', 'encrypted-placeholder', 'unknown', 1, '["pending-model"]')
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	models, err := service.GetModelsList(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0]["id"] != "pending-model" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestOpenAILifecycleAndProxy(t *testing.T) {
	var activeChatRequests int32

	// Spin up a mock upstream OpenAI server
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"Invalid API Key"}}`))
			return
		}

		if r.Method == http.MethodGet && path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"object": "list",
				"data": [
					{"id": "gpt-4", "object": "model"},
					{"id": "gpt-3.5-turbo", "object": "model"}
				]
			}`))
			return
		}

		if r.Method == http.MethodPost && path == "/v1/chat/completions" {
			current := atomic.AddInt32(&activeChatRequests, 1)
			defer atomic.AddInt32(&activeChatRequests, -1)

			var body struct {
				Stream bool `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)

			if current > 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":{"message":"rate limited"}}`))
				return
			}

			time.Sleep(20 * time.Millisecond)

			if body.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
				w.Write([]byte("data: [DONE]\n\n"))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"id": "chatcmpl-123",
					"object": "chat.completion",
					"choices": [
						{"message": {"role": "assistant", "content": "Hello response"}}
					]
				}`))
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	// 1. List initially empty
	wList := httptest.NewRecorder()
	rList, _ := http.NewRequest("GET", "/api/openai/endpoints", nil)
	service.ServeHTTP(wList, rList)
	if wList.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", wList.Code, wList.Body.String())
	}
	var endpoints []Endpoint
	mustDecode(t, wList.Body.String(), &endpoints)
	if len(endpoints) != 0 {
		t.Fatalf("expected 0 endpoints, got %d", len(endpoints))
	}

	// 2. Create endpoint with validation
	createPayload := fmt.Sprintf(`{
		"name": "Test Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key"
	}`, mockUpstream.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}

	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if !createRes.Success || createRes.Endpoint.ID == "" || createRes.Endpoint.Status != "valid" {
		t.Fatalf("create failed, res: %#v", createRes)
	}

	endpointID := createRes.Endpoint.ID

	// 3. Toggle
	wToggle := httptest.NewRecorder()
	rToggle, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/toggle", strings.NewReader(`{"enabled":false}`))
	service.ServeHTTP(wToggle, rToggle)
	if wToggle.Code != http.StatusOK {
		t.Fatalf("toggle status = %d body=%s", wToggle.Code, wToggle.Body.String())
	}

	// 4. Update
	wUpdate := httptest.NewRecorder()
	rUpdate, _ := http.NewRequest("PUT", "/api/openai/endpoints/"+endpointID, strings.NewReader(`{
		"name": "Test Updated"
	}`))
	service.ServeHTTP(wUpdate, rUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", wUpdate.Code, wUpdate.Body.String())
	}

	// Toggle back on
	wToggleOn := httptest.NewRecorder()
	rToggleOn, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/toggle", strings.NewReader(`{"enabled":true}`))
	service.ServeHTTP(wToggleOn, rToggleOn)

	// 5. Test models proxy (which merges valid/enabled models)
	wModels := httptest.NewRecorder()
	rModels, _ := http.NewRequest("GET", "/v1/models", nil)
	service.ServeHTTP(wModels, rModels)
	if wModels.Code != http.StatusOK {
		t.Fatalf("models proxy status = %d body=%s", wModels.Code, wModels.Body.String())
	}
	var modelsRes struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	mustDecode(t, wModels.Body.String(), &modelsRes)
	if len(modelsRes.Data) != 2 || modelsRes.Data[0].ID != "gpt-3.5-turbo" {
		t.Fatalf("unexpected models list: %#v", modelsRes)
	}

	// 6. Test Chat completions proxy (non-stream)
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChat.Header.Set("x-endpoint-id", endpointID)
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("chat proxy status = %d body=%s", wChat.Code, wChat.Body.String())
	}

	// 7. Test Chat completions proxy (stream)
	wChatStream := httptest.NewRecorder()
	rChatStream, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	rChatStream.Header.Set("x-endpoint-id", endpointID)
	service.ServeHTTP(wChatStream, rChatStream)
	if wChatStream.Code != http.StatusOK {
		t.Fatalf("chat stream proxy status = %d body=%s", wChatStream.Code, wChatStream.Body.String())
	}
	if !strings.Contains(wChatStream.Body.String(), "Hello") {
		t.Fatalf("expected Hello in stream, got %s", wChatStream.Body.String())
	}

	// 8. Health check Single model
	wHealth := httptest.NewRecorder()
	rHealth, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/health-check", strings.NewReader(`{
		"model": "gpt-4",
		"timeout": 5000
	}`))
	service.ServeHTTP(wHealth, rHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("health check status = %d body=%s", wHealth.Code, wHealth.Body.String())
	}
	var healthRes HealthRecord
	mustDecode(t, wHealth.Body.String(), &healthRes)
	if healthRes.Status != "operational" {
		t.Fatalf("expected operational status, got %s error=%s", healthRes.Status, healthRes.Error)
	}

	// 9. Health check all models on one endpoint
	wHealthAll := httptest.NewRecorder()
	rHealthAll, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/health-check-all", strings.NewReader(`{
		"timeout": 5000,
		"concurrency": 1
	}`))
	service.ServeHTTP(wHealthAll, rHealthAll)
	if wHealthAll.Code != http.StatusOK {
		t.Fatalf("health check all status = %d body=%s", wHealthAll.Code, wHealthAll.Body.String())
	}
	var endpointHealthAll struct {
		Success bool          `json:"success"`
		Summary HealthSummary `json:"summary"`
	}
	mustDecode(t, wHealthAll.Body.String(), &endpointHealthAll)
	if !endpointHealthAll.Success || endpointHealthAll.Summary.Operational != 2 || endpointHealthAll.Summary.Failed != 0 {
		t.Fatalf("unexpected endpoint batch health summary: %#v", endpointHealthAll)
	}

	// 10. Health check all enabled endpoints
	wGlobalHealthAll := httptest.NewRecorder()
	rGlobalHealthAll, _ := http.NewRequest("POST", "/api/openai/health-check-all", strings.NewReader(`{
		"timeout": 5000,
		"concurrency": 1
	}`))
	service.ServeHTTP(wGlobalHealthAll, rGlobalHealthAll)
	if wGlobalHealthAll.Code != http.StatusOK {
		t.Fatalf("global health check all status = %d body=%s", wGlobalHealthAll.Code, wGlobalHealthAll.Body.String())
	}
	var globalHealthAll struct {
		Success   bool `json:"success"`
		Endpoints []struct {
			EndpointID  string         `json:"endpointId"`
			Operational int            `json:"operational"`
			Failed      int            `json:"failed"`
			Results     []HealthRecord `json:"results"`
		} `json:"endpoints"`
	}
	mustDecode(t, wGlobalHealthAll.Body.String(), &globalHealthAll)
	if !globalHealthAll.Success || len(globalHealthAll.Endpoints) != 1 {
		t.Fatalf("unexpected global batch health payload: %#v", globalHealthAll)
	}
	if globalHealthAll.Endpoints[0].EndpointID != endpointID || globalHealthAll.Endpoints[0].Operational != 2 || globalHealthAll.Endpoints[0].Failed != 0 {
		t.Fatalf("unexpected global batch health endpoint result: %#v", globalHealthAll.Endpoints[0])
	}

	// 11. Export
	wExport := httptest.NewRecorder()
	rExport, _ := http.NewRequest("GET", "/api/openai/export", nil)
	service.ServeHTTP(wExport, rExport)
	if wExport.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", wExport.Code, wExport.Body.String())
	}
	var exportPayload struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	mustDecode(t, wExport.Body.String(), &exportPayload)
	if len(exportPayload.Endpoints) != 1 {
		t.Fatalf("expected 1 exported endpoint, got %d", len(exportPayload.Endpoints))
	}

	// 12. Import
	wImport := httptest.NewRecorder()
	rImport, _ := http.NewRequest("POST", "/api/openai/import", strings.NewReader(fmt.Sprintf(`{
		"endpoints": [{
			"id": "%s",
			"name": "Imported Endpoint",
			"baseUrl": "%s",
			"apiKey": "test-api-key",
			"enabled": true
		}],
		"overwrite": true
	}`, endpointID, mockUpstream.URL)))
	service.ServeHTTP(wImport, rImport)
	if wImport.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", wImport.Code, wImport.Body.String())
	}

	// Delete
	wDel := httptest.NewRecorder()
	rDel, _ := http.NewRequest("DELETE", "/api/openai/endpoints/"+endpointID, nil)
	service.ServeHTTP(wDel, rDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", wDel.Code, wDel.Body.String())
	}
}

func TestHealthCheckAcceptsEmptyOutput(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"Invalid API Key"}}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch body.Model {
		case "empty-model":
			w.Write([]byte(`{
				"id": "chatcmpl-empty",
				"object": "chat.completion",
				"choices": [
					{"message": {"role": "assistant", "content": "   "}}
				]
			}`))
		case "array-model":
			w.Write([]byte(`{
				"id": "chatcmpl-array",
				"object": "chat.completion",
				"choices": [
					{"message": {"role": "assistant", "content": [{"type":"text","text":"ok"}]}}
				]
			}`))
		default:
			w.Write([]byte(`{
				"id": "chatcmpl-default",
				"object": "chat.completion",
				"choices": [
					{"message": {"role": "assistant", "content": "hello"}}
				]
			}`))
		}
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	success := service.healthCheckSingleModel(
		context.Background(),
		"",
		mockUpstream.URL+"/v1",
		"test-api-key",
		"array-model",
		5*time.Second,
		nil,
	)
	if success.Status == "failed" || success.Error != "" {
		t.Fatalf("expected valid output to pass, got status=%s error=%s", success.Status, success.Error)
	}

	failed := service.healthCheckSingleModel(
		context.Background(),
		"",
		mockUpstream.URL+"/v1",
		"test-api-key",
		"empty-model",
		5*time.Second,
		nil,
	)
	if failed.Status == "failed" {
		t.Fatalf("expected empty output to count as healthy, got status=%s error=%s", failed.Status, failed.Error)
	}
}

func TestHealthCheckRejects200WithErrorBody(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"error":{"message":"Model temporarily unavailable"}}`))
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	record := service.healthCheckSingleModel(
		context.Background(),
		"",
		mockUpstream.URL+"/v1",
		"test-api-key",
		"broken-model",
		5*time.Second,
		nil,
	)
	if record.Status != "failed" {
		t.Fatalf("expected 200-with-error-body to fail, got status=%s", record.Status)
	}
	if !strings.Contains(record.Error, "Model temporarily unavailable") {
		t.Fatalf("expected upstream error message, got error=%q", record.Error)
	}
}

func mustDecode(t *testing.T, body string, v interface{}) {
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("json decode failed: %v body=%q", err, body)
	}
}

func TestDeletedEndpointKeepsNameInAnalytics(t *testing.T) {
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO openai_endpoints (id, name, base_url, api_key, status, enabled)
		VALUES ('ep-archived', '已删除站点A', 'https://example.com/v1', 'encrypted-placeholder', 'unknown', 1)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO openai_gateway_analytics (endpoint_id, model, status_code, latency_ms, total_tokens, timestamp)
		VALUES
			('ep-archived', 'model-a', 200, 10, 1000, datetime('now', '-1 day')),
			('ep-archived', 'model-b', 200, 20, 2000, datetime('now', '-2 days'))
	`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	delRecorder := httptest.NewRecorder()
	service.ServeHTTP(delRecorder, httptest.NewRequest(http.MethodDelete, "/api/openai/endpoints/ep-archived", nil))
	if delRecorder.Code != http.StatusOK {
		t.Fatalf("delete endpoint status = %d, body = %s", delRecorder.Code, delRecorder.Body.String())
	}

	chartsRecorder := httptest.NewRecorder()
	service.ServeHTTP(chartsRecorder, httptest.NewRequest(http.MethodGet, "/api/openai/analytics/charts?days=7&granularity=day", nil))
	if chartsRecorder.Code != http.StatusOK {
		t.Fatalf("charts status = %d, body = %s", chartsRecorder.Code, chartsRecorder.Body.String())
	}
	var charts struct {
		Endpoints []struct {
			Model  string `json:"model"`
			Count  int    `json:"count"`
			Tokens int    `json:"tokens"`
		} `json:"endpoints"`
	}
	mustDecode(t, chartsRecorder.Body.String(), &charts)
	if len(charts.Endpoints) != 1 || charts.Endpoints[0].Model != "已删除站点A" {
		t.Fatalf("charts endpoints did not keep deleted name: %+v", charts.Endpoints)
	}
	if charts.Endpoints[0].Count != 2 || charts.Endpoints[0].Tokens != 3000 {
		t.Fatalf("charts endpoints aggregates wrong: %+v", charts.Endpoints)
	}

	logsRecorder := httptest.NewRecorder()
	service.ServeHTTP(logsRecorder, httptest.NewRequest(http.MethodGet, "/api/openai/analytics/logs?days=7&page=1&pageSize=20", nil))
	if logsRecorder.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", logsRecorder.Code, logsRecorder.Body.String())
	}
	var logs struct {
		Total   int `json:"total"`
		Records []struct {
			EndpointName string `json:"endpointName"`
		} `json:"records"`
	}
	mustDecode(t, logsRecorder.Body.String(), &logs)
	if logs.Total != 2 || len(logs.Records) != 2 {
		t.Fatalf("unexpected logs count: %+v", logs)
	}
	for _, rec := range logs.Records {
		if rec.EndpointName != "已删除站点A" {
			t.Fatalf("logs endpoint name not kept: %q", rec.EndpointName)
		}
	}

	// 归档名称同样可用于日志按端点筛选（输入名称而非 ID）。
	filterRecorder := httptest.NewRecorder()
	service.ServeHTTP(filterRecorder, httptest.NewRequest(http.MethodGet, "/api/openai/analytics/logs?days=7&page=1&pageSize=20&endpoint="+url.QueryEscape("已删除站点A"), nil))
	if filterRecorder.Code != http.StatusOK {
		t.Fatalf("filtered logs status = %d, body = %s", filterRecorder.Code, filterRecorder.Body.String())
	}
	var filtered struct {
		Total int `json:"total"`
	}
	mustDecode(t, filterRecorder.Body.String(), &filtered)
	if filtered.Total != 2 {
		t.Fatalf("filter by archived name failed: total = %d", filtered.Total)
	}
}

func TestEndpointCustomHeadersForwardedToUpstream(t *testing.T) {
	gotHeaders := make(chan map[string]string, 16)

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions") {
			headers := map[string]string{
				"X-Custom-Header": r.Header.Get("X-Custom-Header"),
				"CF-Access-Key":   r.Header.Get("CF-Access-Key"),
				"HTTP-Referer":    r.Header.Get("HTTP-Referer"),
				"User-Agent":      r.Header.Get("User-Agent"),
			}
			gotHeaders <- headers
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			if r.Header.Get("X-Custom-Header") != "from-endpoint" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":{"message":"missing custom header"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createPayload := fmt.Sprintf(`{
		"name": "Headers Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key",
		"headers": [
			{"name": "X-Custom-Header", "value": "from-endpoint"},
			{"name": "CF-Access-Key", "value": "secret-token"},
			{"name": "HTTP-Referer", "value": "https://api-monitor.local"},
			{"name": "User-Agent", "value": "api-monitor-test/1.0"}
		]
	}`, mockUpstream.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}

	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if !createRes.Success || createRes.Endpoint.Status != "valid" {
		t.Fatalf("create with headers failed (verify should forward custom headers): %#v", createRes)
	}

	// Chat completions proxy should carry custom headers upstream.
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChat.Header.Set("x-endpoint-id", createRes.Endpoint.ID)
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("chat proxy status = %d body=%s", wChat.Code, wChat.Body.String())
	}

	select {
	case headers := <-gotHeaders:
		if headers["X-Custom-Header"] != "from-endpoint" {
			t.Errorf("X-Custom-Header = %q, want from-endpoint", headers["X-Custom-Header"])
		}
		if headers["CF-Access-Key"] != "secret-token" {
			t.Errorf("CF-Access-Key = %q, want secret-token", headers["CF-Access-Key"])
		}
		if headers["HTTP-Referer"] != "https://api-monitor.local" {
			t.Errorf("HTTP-Referer = %q, want https://api-monitor.local", headers["HTTP-Referer"])
		}
		if headers["User-Agent"] != "api-monitor-test/1.0" {
			t.Errorf("User-Agent = %q, want api-monitor-test/1.0", headers["User-Agent"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for upstream headers")
	}

	// Listed endpoint should expose stored headers.
	wList := httptest.NewRecorder()
	rList, _ := http.NewRequest("GET", "/api/openai/endpoints", nil)
	service.ServeHTTP(wList, rList)
	var endpoints []Endpoint
	mustDecode(t, wList.Body.String(), &endpoints)
	found := false
	for _, ep := range endpoints {
		if ep.ID == createRes.Endpoint.ID {
			found = true
			if len(ep.Headers) != 4 {
				t.Fatalf("expected 4 stored headers, got %#v", ep.Headers)
			}
		}
	}
	if !found {
		t.Fatal("created endpoint not found in list")
	}
}

func TestEndpointModelEnableToggleFiltersRouting(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"},{"id":"gpt-4-mini","object":"model"}]}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createPayload := fmt.Sprintf(`{
		"name": "Model Switch Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key",
		"skipVerify": true
	}`, mockUpstream.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	endpointID := createRes.Endpoint.ID

	// 手动插入模型列表，模拟已刷新过模型。
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4","gpt-4-mini"]`, endpointID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// 通过路由 toggle 禁用 gpt-4。
	wToggle := httptest.NewRecorder()
	rToggle, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/models/toggle", strings.NewReader(`{"model":"gpt-4","enabled":false}`))
	service.ServeHTTP(wToggle, rToggle)
	if wToggle.Code != http.StatusOK {
		t.Fatalf("toggle status = %d body=%s", wToggle.Code, wToggle.Body.String())
	}
	var toggleRes struct {
		Success        bool     `json:"success"`
		Enabled        bool     `json:"enabled"`
		DisabledModels []string `json:"disabledModels"`
	}
	mustDecode(t, wToggle.Body.String(), &toggleRes)
	if !toggleRes.Success || toggleRes.Enabled {
		t.Fatalf("toggle response unexpected: %#v", toggleRes)
	}
	if len(toggleRes.DisabledModels) != 1 || toggleRes.DisabledModels[0] != "gpt-4" {
		t.Fatalf("disabled models = %#v", toggleRes.DisabledModels)
	}

	// 再次开启 gpt-4-mini 保持启用，断言 disabled 不再包含它。
	wToggle2 := httptest.NewRecorder()
	rToggle2, _ := http.NewRequest("POST", "/api/openai/endpoints/"+endpointID+"/models/toggle", strings.NewReader(`{"model":"gpt-4-mini","enabled":true}`))
	service.ServeHTTP(wToggle2, rToggle2)
	if wToggle2.Code != http.StatusOK {
		t.Fatalf("toggle2 status = %d body=%s", wToggle2.Code, wToggle2.Body.String())
	}

	// /v1/models 不应再包含被禁用的 gpt-4。
	wModels := httptest.NewRecorder()
	rModels, _ := http.NewRequest("GET", "/v1/models", nil)
	service.ServeHTTP(wModels, rModels)
	if wModels.Code != http.StatusOK {
		t.Fatalf("models proxy status = %d body=%s", wModels.Code, wModels.Body.String())
	}
	var modelsRes struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	mustDecode(t, wModels.Body.String(), &modelsRes)
	foundIDs := map[string]bool{}
	for _, m := range modelsRes.Data {
		foundIDs[m.ID] = true
	}
	if foundIDs["gpt-4"] {
		t.Fatalf("disabled model gpt-4 should not appear in /v1/models, got %#v", modelsRes.Data)
	}
	if !foundIDs["gpt-4-mini"] {
		t.Fatalf("enabled model gpt-4-mini should appear in /v1/models, got %#v", modelsRes.Data)
	}

	// 指定端点请求被禁用模型应失败，被启用模型应成功。
	wChatDisabled := httptest.NewRecorder()
	rChatDisabled, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChatDisabled.Header.Set("x-endpoint-id", endpointID)
	service.ServeHTTP(wChatDisabled, rChatDisabled)
	if wChatDisabled.Code == http.StatusOK {
		t.Fatalf("disabled model request should fail, got %d body=%s", wChatDisabled.Code, wChatDisabled.Body.String())
	}

	wChatEnabled := httptest.NewRecorder()
	rChatEnabled, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4-mini",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChatEnabled.Header.Set("x-endpoint-id", endpointID)
	service.ServeHTTP(wChatEnabled, rChatEnabled)
	if wChatEnabled.Code != http.StatusOK {
		t.Fatalf("enabled model request failed: %d body=%s", wChatEnabled.Code, wChatEnabled.Body.String())
	}
}

func TestConvertNodeToProxy(t *testing.T) {
	cases := []struct {
		nodeType, raw, server string
		port                  int
		wantPrefix            string
	}{
		{"socks", "socks://user:pass@1.2.3.4:1080#台湾-01", "", 0, "socks5://user:pass@1.2.3.4:1080"},
		{"http", "http://u:p@5.6.7.8:8080#HK", "", 0, "http://u:p@5.6.7.8:8080"},
		{"socks", "socks5://1.2.3.4:1081#Socks", "", 0, "socks5://1.2.3.4:1081"},
		{"http", "", "9.9.9.9", 8899, "socks5://9.9.9.9:8899"},
	}
	for _, tc := range cases {
		proxy, _, ok := convertNodeToProxy(tc.nodeType, tc.raw, tc.server, tc.port, "节点")
		if !ok {
			t.Fatalf("convertNodeToProxy(%q) returned ok=false", tc.raw)
		}
		if !strings.HasPrefix(proxy, tc.wantPrefix) {
			t.Errorf("proxy = %q, want prefix %q", proxy, tc.wantPrefix)
		}
	}
}

func TestProxyPoolRotationAndAutoSwitch(t *testing.T) {
	// 第一个代理命中限流（429），第二个代理正常。网关应在第一次 429 后自动切到第二个。
	var proxy1Hits, proxy2Hits int32

	proxy1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy1Hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer proxy1.Close()

	proxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy2Hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer proxy2.Close()

	// 上游 /v1/models 需要一个正常响应以确认端点验证；chat 请求会经代理池转发。
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"}]}`))
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createPayload := fmt.Sprintf(`{
		"name": "Proxy Switch Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key",
		"skipVerify": true,
		"proxyPool": ["%s", "%s"],
		"proxyEnabled": true,
		"autoSwitch": true
	}`, mockUpstream.URL, proxy1.URL, proxy2.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	if len(createRes.Endpoint.ProxyPool) != 2 || !createRes.Endpoint.AutoSwitch {
		t.Fatalf("proxy pool not persisted: %#v", createRes.Endpoint)
	}

	// 写入 models，绕过前面手动确认端点模型列表。
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// 触发 /v1/chat/completions：第一次请求经 proxy1 拿 429，网关应切到 proxy2 并成功返回 200。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChat.Header.Set("x-endpoint-id", createRes.Endpoint.ID)
	service.ServeHTTP(wChat, rChat)

	// 最终响应必须是 200（来自 proxy2）。
	if wChat.Code != http.StatusOK {
		t.Fatalf("chat via proxy pool failed: code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if atomic.LoadInt32(&proxy1Hits) < 1 {
		t.Fatalf("proxy1 was never used, expected first attempt to hit it")
	}
	if atomic.LoadInt32(&proxy2Hits) < 1 {
		t.Fatalf("proxy2 was never used, expected retry to hit it")
	}
}

func TestWeightedProxyPick(t *testing.T) {
	// 快代理应被更频繁选中，但慢代理仍有机会出现（不独占）。
	fast, slow := 400, 3000
	candidates := []proxyCandidate{
		{idx: 0, ttfb: int64(fast), known: true},
		{idx: 1, ttfb: int64(slow), known: true},
	}
	fastCount, slowCount := 0, 0
	trials := 20000
	for i := 0; i < trials; i++ {
		if weightedProxyPick(candidates) == 0 {
			fastCount++
		} else {
			slowCount++
		}
	}
	// 快代理权重明显更高（400 vs 3000 毫秒 => 权重 14 vs 1），应占绝大多数。
	if fastCount <= slowCount {
		t.Fatalf("expected fast proxy to dominate, fast=%d slow=%d", fastCount, slowCount)
	}
	if slowCount == 0 {
		t.Fatalf("expected slow proxy to still get occasional pick, slow=%d", slowCount)
	}
	// 全部冷却退化的场景：单候选直接返回。
	if weightedProxyPick([]proxyCandidate{{idx: 2, ttfb: 100, known: true}}) != 2 {
		t.Fatalf("single candidate should be picked directly")
	}
}

func TestProxyPoolAutoSwitchOn5xx(t *testing.T) {
	// 第一个代理返回 502（上游故障），第二个代理正常。网关应切到第二个并成功返回 200。
	var proxy1Hits, proxy2Hits int32

	proxy1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy1Hits, 1)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":{"message":"upstream exploded"}}`))
	}))
	defer proxy1.Close()

	proxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy2Hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer proxy2.Close()

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"}]}`))
	}))
	defer mockUpstream.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createPayload := fmt.Sprintf(`{
		"name": "Proxy 5xx Switch Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key",
		"skipVerify": true,
		"proxyPool": ["%s", "%s"],
		"proxyEnabled": true,
		"autoSwitch": true
	}`, mockUpstream.URL, proxy1.URL, proxy2.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	rChat.Header.Set("x-endpoint-id", createRes.Endpoint.ID)
	service.ServeHTTP(wChat, rChat)

	if wChat.Code != http.StatusOK {
		t.Fatalf("chat via proxy pool failed: code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if atomic.LoadInt32(&proxy1Hits) < 1 {
		t.Fatalf("proxy1 was never used, expected first attempt to hit it")
	}
	if atomic.LoadInt32(&proxy2Hits) < 1 {
		t.Fatalf("proxy2 was never used, expected 5xx retry to hit it")
	}
}

func TestIsRetryableUpstreamResponse(t *testing.T) {
	// 限流类：应重试。
	for _, code := range []int{429, 439, 503, 529} {
		resp := &http.Response{StatusCode: code}
		if !isRetryableUpstreamResponse(resp, nil) {
			t.Fatalf("status %d should be retryable", code)
		}
	}
	// 常见 5xx：应重试。
	for _, code := range []int{500, 502, 504, 599} {
		resp := &http.Response{StatusCode: code}
		if !isRetryableUpstreamResponse(resp, nil) {
			t.Fatalf("status %d should be retryable", code)
		}
	}
	// 语义错误/成功：不重试。
	for _, code := range []int{200, 400, 401, 403, 404, 501, 505} {
		resp := &http.Response{StatusCode: code}
		if isRetryableUpstreamResponse(resp, nil) {
			t.Fatalf("status %d should not be retryable", code)
		}
	}
	// 限流关键词兜底：正文命中关键词时重试。
	bodyResp := &http.Response{StatusCode: 200}
	if !isRetryableUpstreamResponse(bodyResp, []byte(`{"error":"rate_limit exceeded"}`)) {
		t.Fatalf("rate limit keyword in body should be retryable")
	}
}

func TestRelayErrorsBufferAndHandler(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	// 记录三条失败事件，模拟最新的最后写入。
	service.recordRelayError(RelayErrorRecord{Route: "chat.completions", Kind: "dial", Endpoint: "ep-a", Model: "m1", Proxy: "203.0.113.5:8080", Error: "dial tcp: i/o timeout"})
	service.recordRelayError(RelayErrorRecord{Route: "chat.completions", Kind: "timeout", Endpoint: "ep-a", Model: "m1", Proxy: "203.0.113.6:8080", Error: "no first byte within 20s"})
	service.recordRelayError(RelayErrorRecord{Route: "responses", Kind: "no_endpoint", Model: "m2", Error: "no enabled endpoint serves model"})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/openai/relay-errors", nil)
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("relay-errors status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var result struct {
		Total   int `json:"total"`
		Records []struct {
			Route  string `json:"route"`
			Kind   string `json:"kind"`
			Proxy  string `json:"proxy"`
			Error  string `json:"error"`
			Client string `json:"clientIp"`
		} `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Records) != 3 {
		t.Fatalf("unexpected records: total=%d len=%d body=%s", result.Total, len(result.Records), recorder.Body.String())
	}
	// 最新在前：no_endpoint 应在第一条。
	if result.Records[0].Route != "responses" || result.Records[0].Kind != "no_endpoint" {
		t.Fatalf("newest record should be first: %+v", result.Records[0])
	}

	// limit 参数生效。
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/openai/relay-errors?limit=1", nil)
	service.ServeHTTP(recorder, request)
	var limited struct {
		Total   int `json:"total"`
		Records []struct {
			Kind string `json:"kind"`
		} `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &limited); err != nil {
		t.Fatal(err)
	}
	if limited.Total != 3 || len(limited.Records) != 1 || limited.Records[0].Kind != "no_endpoint" {
		t.Fatalf("limit param not honored: %+v", limited)
	}
}

func TestRelayErrorsBufferCap(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	for i := 0; i < relayErrorBufferSize+37; i++ {
		service.recordRelayError(RelayErrorRecord{Route: "chat.completions", Kind: "dial", Error: "x"})
	}

	service.relayErrMu.Lock()
	bufLen := len(service.relayErrors)
	service.relayErrMu.Unlock()

	if bufLen != relayErrorBufferSize {
		t.Fatalf("buffer cap = %d, want %d", bufLen, relayErrorBufferSize)
	}

	// 接口返回的 total 受上限约束。
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/openai/relay-errors", nil)
	service.ServeHTTP(recorder, request)
	var result struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != relayErrorBufferSize {
		t.Fatalf("handler total = %d, want %d", result.Total, relayErrorBufferSize)
	}
}

// setupTwoProxyEndpoint 创建一个带两个 mock 代理的端点，返回 service 与端点 id。
// proxyHandler 负责两个代理的响应行为；hitCounters 可选记录每个代理的命中次数。
func setupTwoProxyEndpoint(t *testing.T, proxy1Handler, proxy2Handler http.HandlerFunc) (*Service, string, string, string) {
	t.Helper()
	var proxy1Hits, proxy2Hits int32
	proxy1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy1Hits, 1)
		proxy1Handler(w, r)
	}))
	proxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxy2Hits, 1)
		proxy2Handler(w, r)
	}))
	t.Cleanup(proxy1.Close)
	t.Cleanup(proxy2.Close)

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4","object":"model"}]}`))
	}))
	t.Cleanup(mockUpstream.Close)

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	createPayload := fmt.Sprintf(`{
		"name": "Session Proxy Mock",
		"baseUrl": "%s",
		"apiKey": "test-api-key",
		"skipVerify": true,
		"proxyPool": ["%s", "%s"],
		"proxyEnabled": true,
		"autoSwitch": true
	}`, mockUpstream.URL, proxy1.URL, proxy2.URL)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, createRes.Endpoint.ID)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	return service, createRes.Endpoint.ID, proxy1.URL, proxy2.URL
}

func chatRequest(t *testing.T, service *Service, endpointID string, stream bool, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	if stream {
		body = `{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	}
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("x-endpoint-id", endpointID)
	if sessionID != "" {
		r.Header.Set("X-OpenCode-Session-ID", sessionID)
	}
	service.ServeHTTP(w, r)
	return w
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`))
}

func TestSessionProxyRotatesAfterRequestLimit(t *testing.T) {
	// 会话粘性 + 配额感知轮换：同一会话在 limit=2 时，前 2 个请求固定代理 A，
	// 第 3 个请求主动轮换到代理 B（规避上游按出口 IP 限额）。
	oldLimit := sessionProxyRequestLimit
	sessionProxyRequestLimit = 2
	defer func() { sessionProxyRequestLimit = oldLimit }()

	service, endpointID, _, _ := setupTwoProxyEndpoint(t, okHandler, okHandler)

	for i := 0; i < 2; i++ {
		w := chatRequest(t, service, endpointID, false, "sess-sticky")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d failed: %d %s", i+1, w.Code, w.Body.String())
		}
	}

	// 第三个请求：计数已达上限，应换到另一个代理（并重置绑定计数）。
	w := chatRequest(t, service, endpointID, false, "sess-sticky")
	if w.Code != http.StatusOK {
		t.Fatalf("request 3 failed: %d %s", w.Code, w.Body.String())
	}
	service.proxyMu.Lock()
	newBinding := service.proxyStateByEndpoint[endpointID].sessionBindings["sess-sticky"]
	service.proxyMu.Unlock()
	if newBinding == nil || newBinding.count != 1 {
		t.Fatalf("expected re-bound with count=1 after limit, got %+v", newBinding)
	}
}

func TestUpstream429DoesNotCoolProxy(t *testing.T) {
	// 上游 429 是上游限额，不是代理故障：单次 429 不惩罚代理（无冷却、无失败计数），
	// 只切换出口；但 429 会累计计数，达到阈值后触发禁用（见 TestProxy429BannedAfterThreshold）。
	service, endpointID, proxy1URL, _ := setupTwoProxyEndpoint(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limit"}}`))
		},
		okHandler,
	)

	w := chatRequest(t, service, endpointID, false, "")
	if w.Code != http.StatusOK {
		t.Fatalf("request failed: %d %s", w.Code, w.Body.String())
	}

	service.proxyMu.Lock()
	state := service.proxyStateByEndpoint[endpointID]
	cooldownCount := len(state.cooldown)
	failureCount := len(state.failures)
	rateCount := state.rate429[proxy1URL]
	limitedCount := len(state.rateLimited)
	service.proxyMu.Unlock()
	if cooldownCount != 0 || failureCount != 0 {
		t.Fatalf("429 must not penalize proxies: cooldown=%d failures=%d", cooldownCount, failureCount)
	}
	if rateCount != 1 {
		t.Fatalf("single 429 should count once toward ban, got %d", rateCount)
	}
	if limitedCount != 0 {
		t.Fatalf("single 429 must not ban the proxy yet, got %d limited", limitedCount)
	}
}

func TestProxy429BannedAfterThreshold(t *testing.T) {
	// 同一代理累计 3 次 429 后禁用 30 分钟；禁用期内选择逻辑跳过它，
	// 到期后自动释放（时间判断，无需主动清理）。
	service, endpointID, proxy1URL, _ := setupTwoProxyEndpoint(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limit"}}`))
		},
		okHandler,
	)

	for i := 0; i < proxy429BanThreshold; i++ {
		w := chatRequest(t, service, endpointID, false, "")
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d failed: %d %s", i+1, w.Code, w.Body.String())
		}
	}

	service.proxyMu.Lock()
	state := service.proxyStateByEndpoint[endpointID]
	until, banned := state.rateLimited[proxy1URL]
	service.proxyMu.Unlock()
	if !banned {
		t.Fatal("expected proxy1 banned after 3×429")
	}
	expect := time.Now().Add(proxy429BanDuration)
	if until.Before(expect.Add(-5*time.Second)) || until.After(expect.Add(5*time.Second)) {
		t.Fatalf("ban expiry = %v, want ~%v", until, expect)
	}
	// 禁用期内：选择应跳过 proxy1（此时 state.cursor 已推进，proxy2 可正常服务）。
	if !proxyRateLimited(state, proxy1URL, time.Now()) {
		t.Fatal("proxyRateLimited should report banned while within duration")
	}
	if proxyRateLimited(state, proxy1URL, until.Add(time.Second)) {
		t.Fatal("proxyRateLimited should release after expiry")
	}
}

func TestEndpointProxyBatchesRoundTrip(t *testing.T) {
	// 批次（文件导入）随端点创建/更新持久化，列表接口原样返回；脏批次（空 ID/名称/代理）被清洗。
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	payload := `{
		"name": "batch demo",
		"baseUrl": "https://example.com/v1",
		"apiKey": "k",
		"skipVerify": true,
		"proxyPool": ["http://a:1@1.2.3.4:3128", "http://b:2@5.6.7.8:3128"],
		"proxyBatches": [
			{"id": "pb_1", "name": "all.txt", "createdAt": "2026-08-12T00:00:00Z", "proxies": ["http://a:1@1.2.3.4:3128", "http://a:1@1.2.3.4:3128", "  http://c:3@9.9.9.9:3128  "]},
			{"id": "", "name": "no-id", "proxies": ["http://x:1@1.1.1.1:3128"]},
			{"id": "pb_3", "name": "empty", "proxies": ["  ", ""]}
		]
	}`
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(payload))
	service.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, w.Body.String(), &createRes)
	if len(createRes.Endpoint.ProxyBatches) != 1 {
		t.Fatalf("expected 1 cleaned batch, got %+v", createRes.Endpoint.ProxyBatches)
	}
	batch := createRes.Endpoint.ProxyBatches[0]
	if batch.ID != "pb_1" || batch.Name != "all.txt" || len(batch.Proxies) != 2 {
		t.Fatalf("unexpected batch: %+v", batch)
	}
	if len(createRes.Endpoint.ProxyPool) != 3 {
		t.Fatalf("expected 3 cleaned pool entries, got %v", createRes.Endpoint.ProxyPool)
	}

	// 列表接口返回同一批次数据。
	w2 := httptest.NewRecorder()
	r2, _ := http.NewRequest("GET", "/api/openai/endpoints", nil)
	service.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", w2.Code, w2.Body.String())
	}
	var list []Endpoint
	mustDecode(t, w2.Body.String(), &list)
	var found *Endpoint
	for i := range list {
		if list[i].ID == createRes.Endpoint.ID {
			found = &list[i]
			break
		}
	}
	if found == nil || len(found.ProxyBatches) != 1 || found.ProxyBatches[0].ID != "pb_1" {
		t.Fatalf("listed endpoint batches mismatch: %+v", found)
	}

	// 局部更新（提交 proxyPool 但不提交 proxyBatches）应保留存量批次，不清空。
	updatePayload := `{"proxyPool": ["http://a:1@1.2.3.4:3128", "http://b:2@5.6.7.8:3128", "http://d:4@1.1.1.1:3128"]}`
	w3 := httptest.NewRecorder()
	r3, _ := http.NewRequest("PUT", "/api/openai/endpoints/"+createRes.Endpoint.ID, strings.NewReader(updatePayload))
	service.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w3.Code, w3.Body.String())
	}
	w4 := httptest.NewRecorder()
	r4, _ := http.NewRequest("GET", "/api/openai/endpoints", nil)
	service.ServeHTTP(w4, r4)
	var list2 []Endpoint
	mustDecode(t, w4.Body.String(), &list2)
	for i := range list2 {
		if list2[i].ID == createRes.Endpoint.ID {
			if len(list2[i].ProxyBatches) != 1 || list2[i].ProxyBatches[0].ID != "pb_1" {
				t.Fatalf("partial update must keep batches, got %+v", list2[i].ProxyBatches)
			}
			if len(list2[i].ProxyPool) != 3 || list2[i].ProxyPool[2] != "http://d:4@1.1.1.1:3128" {
				t.Fatalf("partial update must apply new pool, got %v", list2[i].ProxyPool)
			}
		}
	}
}

func TestAllProxiesFrozenFallsBackToDirect(t *testing.T) {
	// 全部出口被 429 冻结时：不再硬选冻结代理（老行为），回退直连兜底；
	// 直连请求不累计 429，且调用日志按实际出口标记（viaProxy=false）。
	rate429Handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}
	service, endpointID, proxy1URL, proxy2URL := setupTwoProxyEndpoint(t, rate429Handler, rate429Handler)

	service.proxyMu.Lock()
	state, ok := service.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		service.proxyStateByEndpoint[endpointID] = state
	}
	state.rateLimited[proxy1URL] = time.Now().Add(proxy429BanDuration)
	state.rateLimited[proxy2URL] = time.Now().Add(proxy429BanDuration)
	service.proxyMu.Unlock()

	w := chatRequest(t, service, endpointID, false, "")
	if w.Code != http.StatusOK {
		t.Fatalf("all-frozen request failed: %d %s", w.Code, w.Body.String())
	}

	// 直连兜底：冻结代理不再被选中，429 计数保持 0。
	service.proxyMu.Lock()
	count := state.rate429[proxy1URL]
	service.proxyMu.Unlock()
	if count != 0 {
		t.Fatalf("direct fallback must not count 429 against proxies, got %d", count)
	}

	// 调用日志按实际出口记录：直连回退不应标「代」。
	service.flushAnalyticsQueue(5 * time.Second)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var viaProxy int
	err = db.QueryRow(`SELECT via_proxy FROM openai_gateway_analytics ORDER BY id DESC LIMIT 1`).Scan(&viaProxy)
	if err != nil {
		t.Fatalf("read analytics: %v", err)
	}
	if viaProxy != 0 {
		t.Fatalf("direct fallback request must be recorded as via_proxy=0, got %d", viaProxy)
	}
}

func TestAutoUnfreezeAllLocked(t *testing.T) {
	// 全部出口被禁用（429 冻结/坏代理沉淀）时：自动解冻全体代理，清除冷却/
	// 冻结/沉淀状态；节流窗口内重复触发不再解冻（返回 false）。
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	service.proxyMu.Lock()
	state := newEndpointProxyState()
	service.proxyStateByEndpoint["ep-unfreeze"] = state
	state.cooldown["proxy-a"] = time.Now().Add(30 * time.Minute)
	state.rateLimited["proxy-a"] = time.Now().Add(30 * time.Minute)
	state.sunk["proxy-a"] = time.Now().Add(6 * time.Hour)
	state.rate429["proxy-a"] = 3
	state.failures["proxy-a"] = 5
	service.proxyMu.Unlock()

	now := time.Now()
	if !service.autoUnfreezeAllLocked("ep-unfreeze", []string{"proxy-a", "proxy-b"}, now) {
		t.Fatalf("first auto-unfreeze must succeed")
	}
	service.proxyMu.Lock()
	state = service.proxyStateByEndpoint["ep-unfreeze"]
	_, cooled := state.cooldown["proxy-a"]
	_, banned := state.rateLimited["proxy-a"]
	_, sunk := state.sunk["proxy-a"]
	rate429 := state.rate429["proxy-a"]
	failures := state.failures["proxy-a"]
	service.proxyMu.Unlock()
	if cooled || banned || sunk || rate429 != 0 || failures != 0 {
		t.Fatalf("auto-unfreeze must clear cooldown/rateLimited/sunk/rate429/failures, got cooled=%v banned=%v sunk=%v rate429=%d failures=%d", cooled, banned, sunk, rate429, failures)
	}

	// 节流：紧接再次触发（仍在 proxyAllFrozenRetryInterval 内）应返回 false。
	service.proxyMu.Lock()
	service.proxyStateByEndpoint["ep-unfreeze"].rateLimited["proxy-a"] = time.Now().Add(30 * time.Minute)
	service.proxyMu.Unlock()
	if service.autoUnfreezeAllLocked("ep-unfreeze", []string{"proxy-a", "proxy-b"}, time.Now()) {
		t.Fatalf("throttled auto-unfreeze must return false within retry interval")
	}

	// 未知端点返回 false。
	if service.autoUnfreezeAllLocked("ep-missing", []string{"proxy-a"}, time.Now()) {
		t.Fatalf("auto-unfreeze on missing endpoint must return false")
	}

	// 等待异步持久化写库完成，避免 TempDir 清理时目录非空。
	proxyStateWriteWG.Wait()
}

func TestImportProxyListRoute(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	text := strings.Join([]string{
		"http://user:pass@1.2.3.4:3128",
		"http://user:pass@1.2.3.4:3128", // 重复
		"socks5://5.6.7.8:1080",
		"vmess://not-a-proxy-uri",
		"纯文本垃圾行",
		"",
	}, "\n")
	payload, _ := json.Marshal(map[string]string{"text": text})
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/openai/proxies/import-list", bytes.NewReader(payload))
	service.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Success bool     `json:"success"`
		Total   int      `json:"total"`
		Proxies []string `json:"proxies"`
	}
	mustDecode(t, w.Body.String(), &res)
	if !res.Success || res.Total != 2 {
		t.Fatalf("expected 2 valid proxies, got %+v", res)
	}
	if len(res.Proxies) != 2 || res.Proxies[0] != "http://user:pass@1.2.3.4:3128" || res.Proxies[1] != "socks5://5.6.7.8:1080" {
		t.Fatalf("unexpected proxy list: %v", res.Proxies)
	}
}

func TestMarkProxyFailedExponentialBackoff(t *testing.T) {
	// 指数退避：1min << min(failures-1, 5)，封顶 30min。
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	service.proxyMu.Lock()
	service.proxyStateByEndpoint["ep-backoff"] = newEndpointProxyState()
	service.proxyMu.Unlock()

	base := time.Now().Add(proxyCooldown)
	service.markProxyFailed("ep-backoff", "proxy-a")
	service.proxyMu.Lock()
	first := service.proxyStateByEndpoint["ep-backoff"].cooldown["proxy-a"]
	service.proxyMu.Unlock()
	if first.Before(base) || first.After(base.Add(5*time.Second)) {
		t.Fatalf("first cooldown = %v, want ~%v", first, base)
	}

	service.markProxyFailed("ep-backoff", "proxy-a")
	service.markProxyFailed("ep-backoff", "proxy-a")
	service.proxyMu.Lock()
	third := service.proxyStateByEndpoint["ep-backoff"].cooldown["proxy-a"]
	service.proxyMu.Unlock()
	// 第 3 次失败：1min << 2 = 4min。
	expect := time.Now().Add(4 * proxyCooldown)
	if third.Before(expect.Add(-5*time.Second)) || third.After(expect.Add(5*time.Second)) {
		t.Fatalf("third cooldown = %v, want ~%v", third, expect)
	}

	for i := 0; i < 5; i++ {
		service.markProxyFailed("ep-backoff", "proxy-a")
	}
	service.proxyMu.Lock()
	capped := service.proxyStateByEndpoint["ep-backoff"].cooldown["proxy-a"]
	service.proxyMu.Unlock()
	if capped.Before(time.Now().Add(proxyCooldownMax - 5*time.Second)) {
		t.Fatalf("cooldown should cap at 30min, got %v", capped)
	}

	// 成功恢复：清除冷却与失败计数。
	service.markProxySuccess("ep-backoff", "proxy-a")
	service.proxyMu.Lock()
	state := service.proxyStateByEndpoint["ep-backoff"]
	_, hasCooldown := state.cooldown["proxy-a"]
	_, hasFailures := state.failures["proxy-a"]
	service.proxyMu.Unlock()
	if hasCooldown || hasFailures {
		t.Fatalf("markProxySuccess should clear cooldown and failures")
	}

	// 等待异步持久化写库完成，避免 TempDir 清理时目录非空。
	proxyStateWriteWG.Wait()
}

func TestNormalizeResponsesTools(t *testing.T) {
	body := map[string]interface{}{
		"model": "m",
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "name": "shell", "description": "d"},
			map[string]interface{}{"type": "web_search", "external_web_access": false},
			map[string]interface{}{"type": "namespace", "name": "ns"},
			map[string]interface{}{"type": "web_search"},
			"not-a-tool",
		},
	}
	normalizeResponsesTools(body)
	tools := body["tools"].([]interface{})
	if tools[0].(map[string]interface{})["name"] != "shell" {
		t.Errorf("function tool name changed unexpectedly")
	}
	if tools[1].(map[string]interface{})["name"] != "web_search" {
		t.Errorf("web_search tool should get name=web_search, got %v", tools[1].(map[string]interface{})["name"])
	}
	if tools[2].(map[string]interface{})["name"] != "ns" {
		t.Errorf("namespace tool name should be preserved")
	}
	if tools[3].(map[string]interface{})["name"] != "web_search" {
		t.Errorf("bare web_search tool should get name=web_search")
	}
	if len(body["tools"].([]interface{})) != 5 {
		t.Errorf("tool list length should stay the same")
	}

	empty := map[string]interface{}{"model": "m"}
	normalizeResponsesTools(empty)
	if _, ok := empty["tools"]; ok {
		t.Errorf("tools should not be added when absent")
	}
}

func TestResponsesStreamNormalizer(t *testing.T) {
	norm := newResponsesStreamNormalizer("m")
	var out []byte
	var events []string
	write := func(blocks [][]byte) {
		for _, b := range blocks {
			out = append(out, b...)
		}
	}

	// 文本 delta：应注入 created + message added，然后透传 delta。
	write(norm.transform([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")))
	// 第二个 delta：不重复注入。
	write(norm.transform([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ya\"}\n\n")))
	// function_call added：先关闭 message，再透传。
	write(norm.transform([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"call_1\",\"name\":\"shell_command\",\"call_id\":\"call_1\",\"arguments\":\"\"}}\n\n")))
	// arguments delta。
	write(norm.transform([]byte("event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"command\\\":\\\"echo hi\\\"}\"}\n\n")))
	// completed：先关闭 function_call，再透传。
	write(norm.transform([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"usage\":{}}}\n\n")))

	data := string(out)
	if !strings.Contains(data, "event: response.created") {
		t.Errorf("missing created event")
	}
	if !strings.Contains(data, "response.output_item.added") {
		t.Errorf("missing message item added")
	}
	if !strings.Contains(data, "event: response.output_item.done") {
		t.Errorf("missing output_item.done")
	}
	if !strings.Contains(data, "event: response.function_call_arguments.delta") {
		t.Errorf("function args delta lost")
	}

	// 校验 message done 内容聚合了全部文本。
	for _, line := range strings.Split(data, "\n") {
		if strings.Contains(line, "output_item.done") {
			t.Logf("done event line: %s", line)
		}
	}
	if !strings.Contains(data, "hiya") {
		t.Errorf("message done should aggregate text deltas, got:\n%s", data)
	}
	if !strings.Contains(data, "\\\"command\\\":\\\"echo hi\\\"") {
		t.Errorf("function_call done should carry full arguments, got:\n%s", data)
	}

	// 事件计数：created+added+delta+delta+done(message)+added(fn)+delta+done(fn)+completed
	events = nil
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
		}
	}
	t.Logf("event sequence: %v", events)
}

func TestReadSSEBlock(t *testing.T) {
	br := bufio.NewReader(strings.NewReader("event: a\ndata: {\"x\":1}\n\nevent: b\ndata: {\"x\":2}\n\n"))
	first, err := readSSEBlock(br)
	if err != nil || !strings.Contains(string(first), "\"x\":1") {
		t.Errorf("first block wrong: %q err=%v", first, err)
	}
	second, err := readSSEBlock(br)
	if err != nil || !strings.Contains(string(second), "\"x\":2") {
		t.Errorf("second block wrong: %q err=%v", second, err)
	}
	if _, err := readSSEBlock(br); err == nil {
		t.Errorf("expect EOF on exhausted stream")
	}
}

func TestNormalizeResponsesInput(t *testing.T) {
	body := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "hi"},
			map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": "hello"},
			}},
			map[string]interface{}{"type": "function_call", "id": "c1", "call_id": "c1", "name": "get_goal", "arguments": "{}"},
			map[string]interface{}{"type": "function_call", "id": "c2", "call_id": "c2", "name": "get_goal", "arguments": "{\"x\":1}"},
			map[string]interface{}{"type": "function_call_output", "call_id": "c1", "output": "done"},
		},
	}
	normalizeResponsesInput(body)
	input := body["input"].([]interface{})
	if len(input) != 4 {
		t.Fatalf("should have user, assistant, output, trailing user = 4 items, got %d", len(input))
	}
	assistant := input[1].(map[string]interface{})
	if assistant["content"] != "hello" {
		t.Errorf("assistant content should be extracted to string, got %v", assistant["content"])
	}
	toolCalls, ok := assistant["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant should have 1 merged tool_call (c2 has no output, dropped), got %#v", assistant["tool_calls"])
	}
	fc0 := toolCalls[0].(map[string]interface{})
	if fc0["id"] != "c1" || fc0["type"] != "function" {
		t.Errorf("tool_call[0] malformed: %#v", fc0)
	}
	fn0 := fc0["function"].(map[string]interface{})
	if fn0["name"] != "get_goal" || fn0["arguments"] != "{}" {
		t.Errorf("tool_call[0].function malformed: %#v", fn0)
	}
	trailing := input[3].(map[string]interface{})
	if trailing["type"] != "message" || trailing["role"] != "user" {
		t.Errorf("trailing message should be empty user, got %v", trailing)
	}

	// 多 output_text 块拼接。
	body2 := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": "a"},
				map[string]interface{}{"type": "output_text", "text": "b"},
			}},
		},
	}
	normalizeResponsesInput(body2)
	if got := body2["input"].([]interface{})[0].(map[string]interface{})["content"]; got != "a\nb" {
		t.Errorf("multi output_text should join with newline, got %q", got)
	}

	// 末尾不是 function_call_output 时不追加。
	body3 := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "hi"},
		},
	}
	normalizeResponsesInput(body3)
	if len(body3["input"].([]interface{})) != 1 {
		t.Errorf("should not append when last is user")
	}

	// 无 input 时不动。
	body4 := map[string]interface{}{"model": "m"}
	normalizeResponsesInput(body4)
	if _, ok := body4["input"]; ok {
		t.Errorf("input should not be added when absent")
	}
}

func TestResponsesStreamNormalizerParallelTools(t *testing.T) {
	norm := newResponsesStreamNormalizer("m")
	var out []byte
	write := func(blocks [][]byte) {
		for _, b := range blocks {
			out = append(out, b...)
		}
	}
	write(norm.transform([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"call_1\",\"name\":\"get_goal\",\"call_id\":\"call_1\",\"arguments\":\"\"}}\n\n")))
	write(norm.transform([]byte("event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"a\\\":1}\"}\n\n")))
	write(norm.transform([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"call_2\",\"name\":\"get_goal\",\"call_id\":\"call_2\",\"arguments\":\"\"}}\n\n")))
	write(norm.transform([]byte("event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"b\\\":2}\"}\n\n")))
	write(norm.transform([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"usage\":{}}}\n\n")))

	data := string(out)
	if !strings.Contains(data, "{\\\"a\\\":1}") || !strings.Contains(data, "{\\\"b\\\":2}") {
		t.Errorf("each tool should keep its own arguments, got:\n%s", data)
	}
	if strings.Contains(data, "{\\\"a\\\":1}{\\\"b\\\":2}") {
		t.Errorf("arguments of different tools must not be concatenated:\n%s", data)
	}
	if strings.Count(data, "event: response.output_item.done") != 2 {
		t.Errorf("should emit done for both function calls, got:\n%s", data)
	}
	if strings.Count(data, "call_2") == 0 {
		t.Errorf("call_2 missing")
	}
}

// TestMultiEndpointFailoverOn5xx 验证端点级 failover：候选池首个端点返回 5xx（触发
// retryableUpstream）时，应切换到下一个端点并成功返回，且不因 lastErr 为 nil 而 panic。
func TestMultiEndpointFailoverOn5xx(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"server exploded"}}`))
	}))
	defer failing.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer ok.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	// 端点 A：总是 5xx。
	createA := fmt.Sprintf(`{"name":"Bad A","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, failing.URL)
	wA := httptest.NewRecorder()
	rA, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createA))
	service.ServeHTTP(wA, rA)
	if wA.Code != http.StatusOK {
		t.Fatalf("create A status = %d body=%s", wA.Code, wA.Body.String())
	}
	var resA struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wA.Body.String(), &resA)

	// 端点 B：总是 200。
	createB := fmt.Sprintf(`{"name":"Good B","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, ok.URL)
	wB := httptest.NewRecorder()
	rB, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createB))
	service.ServeHTTP(wB, rB)
	if wB.Code != http.StatusOK {
		t.Fatalf("create B status = %d body=%s", wB.Code, wB.Body.String())
	}
	var resB struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wB.Body.String(), &resB)

	// 两个端点都配置 gpt-4 模型。
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{resA.Endpoint.ID, resB.Endpoint.ID} {
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	// 触发 /v1/chat/completions：A 返回 500，网关应 failover 到 B 并返回 200，不得 panic。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("failover to healthy endpoint failed: code=%d body=%s", wChat.Code, wChat.Body.String())
	}
}

// TestSingleEndpoint5xxNoPanic 验证单端点返回 5xx 时（重试耗尽、无下一个候选）不 panic，
// 且把上游 5xx 响应透传给客户端。
func TestSingleEndpoint5xxNoPanic(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer failing.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createBody := fmt.Sprintf(`{"name":"Rate Limited","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, failing.URL)
	wC := httptest.NewRecorder()
	rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wC, rC)
	if wC.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wC.Code, wC.Body.String())
	}
	var created struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wC.Body.String(), &created)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, created.Endpoint.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusTooManyRequests {
		t.Fatalf("expected upstream 429 passthrough, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
}

// TestAllEndpointsSameErrorReturnsThatCode 所有候选端点返回同一错误码（429）时，
// 客户端应收到 429 且错误信息说明网关无可用渠道。
func TestAllEndpointsSameErrorReturnsThatCode(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer failing.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createBody := fmt.Sprintf(`{"name":"Limited A","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, failing.URL)
	wA := httptest.NewRecorder()
	rA, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wA, rA)
	if wA.Code != http.StatusOK {
		t.Fatalf("create A status = %d body=%s", wA.Code, wA.Body.String())
	}
	var resA struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wA.Body.String(), &resA)

	wB := httptest.NewRecorder()
	rB, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wB, rB)
	if wB.Code != http.StatusOK {
		t.Fatalf("create B status = %d body=%s", wB.Code, wB.Body.String())
	}
	var resB struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wB.Body.String(), &resB)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{resA.Endpoint.ID, resB.Endpoint.ID} {
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when all endpoints return 429, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if !strings.Contains(wChat.Body.String(), "网关无可用渠道") {
		t.Fatalf("expected gateway-unavailable message, got body=%s", wChat.Body.String())
	}
}

// TestMixedEndpointErrorsReturns503 各候选端点失败码不一致时，客户端应收到 503 网关无可用渠道。
func TestMixedEndpointErrorsReturns503(t *testing.T) {
	rateLimited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer rateLimited.Close()

	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer serverErr.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	mk := func(name, url string) string {
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"name":%q,"baseUrl":%q,"apiKey":"k","skipVerify":true,"autoSwitch":true}`, name, url)
		r, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(body))
		service.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", name, w.Code, w.Body.String())
		}
		var out struct {
			Success  bool     `json:"success"`
			Endpoint Endpoint `json:"endpoint"`
		}
		mustDecode(t, w.Body.String(), &out)
		return out.Endpoint.ID
	}
	idA := mk("Limited A", rateLimited.URL)
	idB := mk("Err B", serverErr.URL)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{idA, idB} {
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when endpoint errors differ, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if !strings.Contains(wChat.Body.String(), "网关无可用渠道") {
		t.Fatalf("expected gateway-unavailable message, got body=%s", wChat.Body.String())
	}
}

// TestRetryRoundRecoversOnSubsequentRound 验证对齐 New API RetryTimes 的多轮重试：
// 全部候选在第一轮都返回 429，但重试轮内上游恢复，最终请求成功返回 200，
// 客户端无需感知（期间保持等待）。
func TestRetryRoundRecoversOnSubsequentRound(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		first := calls
		mu.Unlock()
		if first <= 2 {
			// 前两次调用返回 429（模拟首轮 + 首个重试轮仍限流）
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rpm exhausted"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer flaky.Close()

	// 缩短重试间隔，避免测试过慢。
	oldDelay := endpointRetryDelay
	defer func() { endpointRetryDelay = oldDelay }()
	endpointRetryDelay = 50 * time.Millisecond

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createBody := fmt.Sprintf(`{"name":"Flaky","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, flaky.URL)
	wC := httptest.NewRecorder()
	rC, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createBody))
	service.ServeHTTP(wC, rC)
	if wC.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wC.Code, wC.Body.String())
	}
	var created struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wC.Body.String(), &created)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, created.Endpoint.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("expected eventual 200 after retry rounds, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 3 {
		t.Fatalf("expected at least 3 upstream calls across retry rounds, got %d", calls)
	}
}

// TestStream429NoAutoSwitchFailover 验证流式请求 + autoSwitch=false 的端点返回 429 时，
// 不因「无切换机会」路径误设 firstWritten 而透传 429，而是触发 retryableUpstream
// failover 到下一个候选端点（修复 429 在日日新 autoSwitch=false 时被直接透传）。
func TestStream429NoAutoSwitchFailover(t *testing.T) {
	rateLimited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rpm exhausted","type":"quota_exceeded_error"}}`))
	}))
	defer rateLimited.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer ok.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	// 端点 A：autoSwitch=false（对齐日日新配置），返回 429。
	createA := fmt.Sprintf(`{"name":"Limited NoSwitch","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":false}`, rateLimited.URL)
	wA := httptest.NewRecorder()
	rA, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createA))
	service.ServeHTTP(wA, rA)
	if wA.Code != http.StatusOK {
		t.Fatalf("create A status = %d body=%s", wA.Code, wA.Body.String())
	}
	var resA struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wA.Body.String(), &resA)

	// 端点 B：正常返回 SSE 流。
	createB := fmt.Sprintf(`{"name":"Good","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, ok.URL)
	wB := httptest.NewRecorder()
	rB, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(createB))
	service.ServeHTTP(wB, rB)
	if wB.Code != http.StatusOK {
		t.Fatalf("create B status = %d body=%s", wB.Code, wB.Body.String())
	}
	var resB struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wB.Body.String(), &resB)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{resA.Endpoint.ID, resB.Endpoint.ID} {
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	// 选路确定性由包级 TestMain 钩子保证（第一个候选 = 429 端点 A）。
	// 流式请求：A(429, autoSwitch=false) → 应 failover 到 B 并成功返回 SSE。
	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"messages": [{"role":"user","content":"hello"}],
		"stream": true
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("expected failover to healthy endpoint, got code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if !strings.Contains(wChat.Body.String(), "[DONE]") {
		t.Fatalf("expected SSE [DONE], got body=%s", wChat.Body.String())
	}

	// 无切换机会路径的最后一次 429 也应写入转发失败明细（fix：以前该事件不记录）。
	wRelay := httptest.NewRecorder()
	rRelay, _ := http.NewRequest("GET", "/api/openai/relay-errors?limit=20", nil)
	service.ServeHTTP(wRelay, rRelay)
	if wRelay.Code != http.StatusOK {
		t.Fatalf("relay-errors status = %d body=%s", wRelay.Code, wRelay.Body.String())
	}
	var relayResp struct {
		Records []RelayErrorRecord `json:"records"`
	}
	mustDecode(t, wRelay.Body.String(), &relayResp)
	foundFinal := false
	for _, rec := range relayResp.Records {
		if rec.Kind == "upstream" && rec.StatusCode == http.StatusTooManyRequests && rec.Stream {
			foundFinal = true
			break
		}
	}
	if !foundFinal {
		t.Fatalf("expected no-switch stream 429 to be recorded in relay-errors, records=%+v", relayResp.Records)
	}
}

func TestNormalizeReasoningEffort(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]interface{}
		want  map[string]interface{}
	}{
		{
			name: "chat max normalized to high",
			input: map[string]interface{}{
				"model":           "deepseek-v4-flash",
				"reasoning_effort": "max",
			},
			want: map[string]interface{}{
				"model":           "deepseek-v4-flash",
				"reasoning_effort": "high",
			},
		},
		{
			name: "chat standard values preserved",
			input: map[string]interface{}{
				"reasoning_effort": "high",
			},
			want: map[string]interface{}{
				"reasoning_effort": "high",
			},
		},
		{
			name: "responses reasoning.effort max normalized",
			input: map[string]interface{}{
				"model": "deepseek-v4-flash",
				"reasoning": map[string]interface{}{
					"effort": "max",
				},
			},
			want: map[string]interface{}{
				"model": "deepseek-v4-flash",
				"reasoning": map[string]interface{}{
					"effort": "high",
				},
			},
		},
		{
			name: "missing reasoning_effort untouched",
			input: map[string]interface{}{
				"model": "deepseek-v4-flash",
			},
			want: map[string]interface{}{
				"model": "deepseek-v4-flash",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			normalizeReasoningEffort(c.input)
			got, _ := json.Marshal(c.input)
			want, _ := json.Marshal(c.want)
			if string(got) != string(want) {
				t.Fatalf("normalizeReasoningEffort() = %s, want %s", got, want)
			}
		})
	}
}

// TestFailoverNormalizesReasoningEffort 验证网关在 failover 到候选端点时，会把
// 非标准的 reasoning_effort（max）归一化为 high 再转发：主端点 429 限流 ->
// failover 到备端点，备端点只接受 high（收到 max 就 400），客户端应最终拿到 200。
func TestFailoverNormalizesReasoningEffort(t *testing.T) {
	var receivedEffort string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if e, _ := body["reasoning_effort"].(string); e != "" {
			receivedEffort = e
		}
		if receivedEffort == "max" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"ReasoningEffort invalid, should be one of: low, medium, high, xhigh, none"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer ok.Close()

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer failing.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	mkEndpoint := func(name, url string) string {
		create := fmt.Sprintf(`{"name":%q,"baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, name, url)
		w := httptest.NewRecorder()
		r, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(create))
		service.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", name, w.Code, w.Body.String())
		}
		var created struct {
			Success  bool     `json:"success"`
			Endpoint Endpoint `json:"endpoint"`
		}
		mustDecode(t, w.Body.String(), &created)
		return created.Endpoint.ID
	}

	idA := mkEndpoint("rate limited A", failing.URL)
	idB := mkEndpoint("strict B", ok.URL)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{idA, idB} {
		if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, id); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"reasoning_effort": "max",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("failover with normalized reasoning_effort failed: code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if receivedEffort != "high" {
		t.Fatalf("expected failover endpoint to receive reasoning_effort=high, got %q", receivedEffort)
	}
}

// TestFirstCandidateKeepsReasoningEffort 首个候选应原样透传客户端的 reasoning_effort，
// 保证主链路行为不变（只有 failover 候选才归一化）。
func TestFirstCandidateKeepsReasoningEffort(t *testing.T) {
	var receivedEffort string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if e, _ := body["reasoning_effort"].(string); e != "" {
			receivedEffort = e
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer ok.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	create := fmt.Sprintf(`{"name":"ok","baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, ok.URL)
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(create))
	service.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, w.Body.String(), &created)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE openai_endpoints SET models = ? WHERE id = ?`, `["gpt-4"]`, created.Endpoint.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	wChat := httptest.NewRecorder()
	rChat, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gpt-4",
		"reasoning_effort": "max",
		"messages": [{"role":"user","content":"hello"}]
	}`))
	service.ServeHTTP(wChat, rChat)
	if wChat.Code != http.StatusOK {
		t.Fatalf("first candidate should succeed as-is: code=%d body=%s", wChat.Code, wChat.Body.String())
	}
	if receivedEffort != "max" {
		t.Fatalf("expected first candidate to receive reasoning_effort=max unchanged, got %q", receivedEffort)
	}
}

// TestStressEndpointSwitchNormalizesEffort 压测真实端点切换场景：
// 主端点持续 429 限流 -> 网关 failover 到严格端点（只接受 high，收到 max 即 400）。
// 以高并发 reasoning_effort=max 请求模拟生产流量，验证：
//  1. 全部请求最终 200（failover 成功）
//  2. 严格端点从未收到非标准值 max（归一化可靠）
//  3. 主端点稳定收到 429，切换确实发生
func TestStressEndpointSwitchNormalizesEffort(t *testing.T) {
	var (
		mu          sync.Mutex
		rateHit     int
		strictMax   int
		strictHigh  int
		nonMaxSeen  map[string]int
	)
	nonMaxSeen = make(map[string]int)

	rateLimited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rateHit++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer rateLimited.Close()

	strict := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		effort, _ := body["reasoning_effort"].(string)
		mu.Lock()
		switch effort {
		case "max":
			strictMax++
		case "high":
			strictHigh++
		default:
			nonMaxSeen[effort]++
		}
		mu.Unlock()
		if effort == "max" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"ReasoningEffort invalid, should be one of: low, medium, high, xhigh, none"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer strict.Close()

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	mkEndpoint := func(name, url string) string {
		create := fmt.Sprintf(`{"name":%q,"baseUrl":"%s","apiKey":"k","skipVerify":true,"autoSwitch":true}`, name, url)
		w := httptest.NewRecorder()
		r, _ := http.NewRequest("POST", "/api/openai/endpoints", strings.NewReader(create))
		service.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", name, w.Code, w.Body.String())
		}
		var created struct {
			Success  bool     `json:"success"`
			Endpoint Endpoint `json:"endpoint"`
		}
		mustDecode(t, w.Body.String(), &created)
		return created.Endpoint.ID
	}

	// 顺序创建：A(rate-limited) 在前，B(strict) 在后，保证候选列表 A 优先。
	mkEndpoint("rate limited A", rateLimited.URL)
	mkEndpoint("strict B", strict.URL)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 通过直接更新 sort_order 强制 A 排第一（A 先创建本就应在前，双保险）。
	if _, err := db.Exec(`UPDATE openai_endpoints SET sort_order = 0, models = ?`, `["deepseek-v4-flash"]`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	service.invalidateRouteCache()

	// 并发压测：100 个并发请求，全部携带 reasoning_effort=max。
	const workers = 100
	var wg sync.WaitGroup
	codes := make([]int, workers)
	errs := make([]int, workers)
	start := time.Now()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			body := `{
				"model": "deepseek-v4-flash",
				"reasoning_effort": "max",
				"messages": [{"role":"user","content":"hello"}]
			}`
			r, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			service.ServeHTTP(w, r)
			codes[idx] = w.Code
			if w.Code == http.StatusBadGateway {
				errs[idx]++
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 统计结果
	counts := map[int]int{}
	for _, c := range codes {
		counts[c]++
	}
	mu.Lock()
	defer mu.Unlock()

	t.Logf("并发压测 %d 请求耗时 %v，状态码分布 %v", workers, elapsed, counts)
	t.Logf("rate-limited 端点命中 %d 次", rateHit)
	t.Logf("strict 端点收到 high=%d max=%d 其它=%v", strictHigh, strictMax, nonMaxSeen)

	if counts[http.StatusOK] != workers {
		t.Fatalf("期望全部 %d 请求 200，实际分布 %v", workers, counts)
	}
	if strictMax != 0 {
		t.Fatalf("strict 端点收到 %d 次非标准 reasoning_effort=max，归一化未生效", strictMax)
	}
	if strictHigh == 0 {
		t.Fatalf("strict 端点未收到任何归一化后的 high，failover 未发生（rateHit=%d）", rateHit)
	}
	if rateHit == 0 {
		t.Fatalf("rate-limited 端点未被命中，压测未模拟到限流")
	}
}

// TestNormalizeResponsesInputDropsUnrespondedCalls 验证：assistant 已自带 tool_calls
// 但后续 function_call_output 不足时（codex 多轮并行工具分步回传），未被回应的
// tool_call 会被剔除，避免 zen 转 chat 报 "insufficient tool messages following
// tool_calls message"。
func TestNormalizeResponsesInputDropsUnrespondedCalls(t *testing.T) {
	body := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "hi"},
			map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{}, "tool_calls": []interface{}{
				map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "get_weather", "arguments": "{}"}},
				map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "get_weather", "arguments": "{}"}},
			}},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		},
	}
	normalizeResponsesInput(body)
	input := body["input"].([]interface{})
	// 末尾 function_call_output 后应追加空 user 消息 => 4 条。
	if len(input) != 4 {
		t.Fatalf("want 4 items (user, assistant, output, trailing user), got %d", len(input))
	}
	assistant := input[1].(map[string]interface{})
	toolCalls, ok := assistant["tool_calls"].([]interface{})
	if !ok {
		t.Fatalf("assistant should keep tool_calls, got %#v", assistant["tool_calls"])
	}
	if len(toolCalls) != 1 {
		t.Fatalf("unresponded call_2 should be dropped, kept %d: %#v", len(toolCalls), toolCalls)
	}
	fc := toolCalls[0].(map[string]interface{})
	if fc["id"] != "call_1" {
		t.Fatalf("kept tool_call should be the responded one (call_1), got %#v", fc)
	}
}

// TestNormalizeResponsesInputKeepsAllRespondedCalls 全部 tool_call 都有对应 output
// 时不做剔除，保持原样。
func TestNormalizeResponsesInputKeepsAllRespondedCalls(t *testing.T) {
	body := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{}, "tool_calls": []interface{}{
				map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "a", "arguments": "{}"}},
				map[string]interface{}{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "b", "arguments": "{}"}},
			}},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "o1"},
			map[string]interface{}{"type": "function_call_output", "call_id": "call_2", "output": "o2"},
		},
	}
	normalizeResponsesInput(body)
	assistant := body["input"].([]interface{})[0].(map[string]interface{})
	toolCalls := assistant["tool_calls"].([]interface{})
	if len(toolCalls) != 2 {
		t.Fatalf("all responded, want 2 kept, got %d", len(toolCalls))
	}
}

// TestNormalizeResponsesInputMergedCallsDrop 独立 function_call 归并进 assistant
// 后同样受「未被回应即剔除」约束：只归并有对应 output 的调用。
func TestNormalizeResponsesInputMergedCallsDrop(t *testing.T) {
	body := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "hi"},
			map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{}},
			map[string]interface{}{"type": "function_call", "id": "c1", "call_id": "c1", "name": "get_weather", "arguments": "{}"},
			map[string]interface{}{"type": "function_call", "id": "c2", "call_id": "c2", "name": "get_weather", "arguments": "{}"},
			map[string]interface{}{"type": "function_call_output", "call_id": "c1", "output": "done"},
		},
	}
	normalizeResponsesInput(body)
	assistant := body["input"].([]interface{})[1].(map[string]interface{})
	toolCalls := assistant["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("unresponded c2 should be dropped, kept %d: %#v", len(toolCalls), toolCalls)
	}
	fc := toolCalls[0].(map[string]interface{})
	if fc["id"] != "c1" {
		t.Fatalf("kept call should be c1, got %#v", fc)
	}
}

// TestNormalizeChatContentBlocks 覆盖 PI 等 agent 客户端以 openai-completions 协议
// 发送 Anthropic 风格 content blocks 数组时的归一化：thinking→顶层 reasoning_content、
// toolCall(arguments 对象/字符串)→标准 tool_calls、text→字符串；并保证纯文本/图片
// 数组与原字符串 content 不受影响。
func TestNormalizeChatContentBlocks(t *testing.T) {
	// PI 真实 wire 形态：assistant content 为数组，toolCall 用 arguments(对象)，
	// reasoning 块带 thinking 文本。归一化后 content 归字符串、reasoning_content 提顶、
	// tool_calls 转标准 function 结构（arguments 序列化为 JSON 字符串）。
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "let me think", "signature": "reasoning_content"},
				map[string]interface{}{"type": "text", "text": "ok"},
				map[string]interface{}{"type": "toolCall", "id": "call_1", "name": "bash", "arguments": map[string]interface{}{"command": "ls"}},
			}},
		},
	}
	normalizeChatContentBlocks(body)
	assistant := body["messages"].([]interface{})[1].(map[string]interface{})
	if assistant["reasoning_content"] != "let me think" {
		t.Errorf("reasoning_content should carry thinking text, got %v", assistant["reasoning_content"])
	}
	if assistant["content"] != "ok" {
		t.Errorf("content should be text string, got %v", assistant["content"])
	}
	toolCalls, ok := assistant["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant should have 1 tool_call, got %#v", assistant["tool_calls"])
	}
	fc := toolCalls[0].(map[string]interface{})
	if fc["id"] != "call_1" || fc["type"] != "function" {
		t.Errorf("tool_call malformed: %#v", fc)
	}
	fn := fc["function"].(map[string]interface{})
	if fn["name"] != "bash" {
		t.Errorf("function.name should be bash, got %v", fn["name"])
	}
	if fn["arguments"] != `{"command":"ls"}` {
		t.Errorf("arguments should be JSON-marshaled, got %q", fn["arguments"])
	}

	// user 消息的 toolCall 不应转 tool_calls（工具调用只属于 assistant）。
	bodyUser := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "toolCall", "id": "call_2", "name": "bash", "arguments": `{"command":"u"}`},
			}},
		},
	}
	normalizeChatContentBlocks(bodyUser)
	userMsg := bodyUser["messages"].([]interface{})[0].(map[string]interface{})
	if _, has := userMsg["tool_calls"]; has {
		t.Errorf("user message should not get tool_calls")
	}

	// Claude 风格：tool_use 用 id/name/input(对象)，reasoning 用 text 字段。
	bodyClaude := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "reasoning", "text": "thinking here"},
				map[string]interface{}{"type": "tool_use", "id": "call_9", "name": "run_code", "input": map[string]interface{}{"code": "x"}},
			}},
		},
	}
	normalizeChatContentBlocks(bodyClaude)
	claudeMsg := bodyClaude["messages"].([]interface{})[0].(map[string]interface{})
	if claudeMsg["reasoning_content"] != "thinking here" {
		t.Errorf("claude reasoning text should map to reasoning_content, got %v", claudeMsg["reasoning_content"])
	}
	claudeTC, _ := claudeMsg["tool_calls"].([]interface{})
	if len(claudeTC) != 1 {
		t.Fatalf("claude tool_use should convert to 1 tool_call, got %#v", claudeMsg["tool_calls"])
	}
	claudeFn := claudeTC[0].(map[string]interface{})["function"].(map[string]interface{})
	if claudeFn["arguments"] != `{"code":"x"}` {
		t.Errorf("claude input should be JSON-marshaled, got %q", claudeFn["arguments"])
	}

	// 纯文本数组（无 thinking/toolCall）归一化为字符串。
	bodyText := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "a"},
				map[string]interface{}{"type": "text", "text": "b"},
			}},
		},
	}
	normalizeChatContentBlocks(bodyText)
	if got := bodyText["messages"].([]interface{})[0].(map[string]interface{})["content"]; got != "a\nb" {
		t.Errorf("text parts should join with newline, got %q", got)
	}

	// 纯字符串 content 与图片数组不动；无 messages 时不动。
	bodyPlain := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "plain"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://x/y.png"}},
			}},
		},
	}
	normalizeChatContentBlocks(bodyPlain)
	if got := bodyPlain["messages"].([]interface{})[0].(map[string]interface{})["content"]; got != "plain" {
		t.Errorf("plain string content should be untouched, got %v", got)
	}
	imgContent := bodyPlain["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})
	if len(imgContent) != 1 {
		t.Errorf("image-only array should stay an array, got %#v", imgContent)
	}

	normalizeChatContentBlocks(map[string]interface{}{"model": "m"})
	normalizeChatContentBlocks(nil)
}

// TestNormalizeChatContentBlocksThinkingChain 覆盖 zen thinking 模式下工具循环的
// reasoning_content 续传兜底：assistant 开启思考后，后续缺失 reasoning 的 toolCall
// 轮次必须补空串，否则上游 400 "reasoning_content must be passed back"。
// 同时保证新用户轮次重置思考状态，不误注入后续独立对话。
func TestNormalizeChatContentBlocksThinkingChain(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "review"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "first", "thinkingSignature": "reasoning_content"},
				map[string]interface{}{"type": "toolCall", "id": "call_1", "name": "bash", "arguments": map[string]interface{}{"command": "ls"}},
			}},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "out"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "toolCall", "id": "call_2", "name": "bash", "arguments": map[string]interface{}{"command": "cat"}},
			}},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_2", "content": "data"},
		},
	}
	normalizeChatContentBlocks(body)
	msgs := body["messages"].([]interface{})
	a1 := msgs[1].(map[string]interface{})
	if a1["reasoning_content"] != "first" {
		t.Errorf("assistant1 should carry thinking text, got %v", a1["reasoning_content"])
	}
	a2 := msgs[3].(map[string]interface{})
	if _, has := a2["reasoning_content"]; !has {
		t.Error("assistant2 (toolCall round without thinking) should get empty reasoning_content injected")
	}
	if a2["reasoning_content"] != "" {
		t.Errorf("assistant2 reasoning_content should be empty string, got %v", a2["reasoning_content"])
	}
	if a2["tool_calls"] == nil {
		t.Error("assistant2 should keep converted tool_calls")
	}
}

// TestNormalizeChatContentBlocksThinkingReset 覆盖 thinking 状态在新用户轮次重置：
// 思考结束后新对话轮（user→assistant 无 thinking）不应被注入 reasoning_content。
func TestNormalizeChatContentBlocksThinkingReset(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "review"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "first", "thinkingSignature": "reasoning_content"},
				map[string]interface{}{"type": "text", "text": "done"},
			}},
			map[string]interface{}{"role": "user", "content": "now a new question"},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "toolCall", "id": "call_9", "name": "bash", "arguments": map[string]interface{}{"command": "pwd"}},
			}},
		},
	}
	normalizeChatContentBlocks(body)
	msgs := body["messages"].([]interface{})
	last := msgs[3].(map[string]interface{})
	if _, has := last["reasoning_content"]; has {
		t.Error("thinking state should reset after a new user turn; assistant without thinking got injected")
	}
}

// TestRecordAnalyticsErrorOnlyOnError 验证报错信息（kind/message/response）仅随失败请求
// （>= 400）落库，成功请求不占空间；logs 接口能返回 errorKind/errorMessage/errorResponse
// 字段供界面与 AI 排障读取。
func TestRecordAnalyticsErrorOnlyOnError(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	ctx := context.Background()

	errorJSON := `{"error":{"message":"insufficient balance","type":"insufficient_quota"}}`
	service.recordAnalyticsKey(ctx, "chat.completions", "ep-1", "m-1", http.StatusOK, 10, 0, 1, 2, 3, 0, 0, 0, "10.0.0.1", "198.51.100.7", 0, "", nil)
	service.recordAnalyticsKey(ctx, "chat.completions", "ep-1", "m-1", http.StatusInternalServerError, 20, 0, 0, 0, 0, 0, 0, 1, "10.0.0.1", "198.51.100.7", 0, "", &AnalyticsError{
		Kind:     "upstream",
		Message:  "insufficient balance",
		Response: errorResponseForLog([]byte(errorJSON), http.StatusInternalServerError),
	})
	service.flushAnalyticsQueue(5 * time.Second)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var okKind, okResp, errKind, errMsg, errResp sql.NullString
	if err := db.QueryRow(`SELECT error_kind, response_body FROM openai_gateway_analytics WHERE status_code = 200`).Scan(&okKind, &okResp); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT error_kind, error_message, response_body FROM openai_gateway_analytics WHERE status_code = 500`).Scan(&errKind, &errMsg, &errResp); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if okKind.Valid && okKind.String != "" {
		t.Fatalf("success request should not persist error info, got kind=%q resp=%q", okKind.String, okResp.String)
	}
	if !errKind.Valid || errKind.String != "upstream" {
		t.Fatalf("error request should persist error kind, got %q", errKind.String)
	}
	if errMsg.String != "insufficient balance" {
		t.Fatalf("error request should persist error message, got %q", errMsg.String)
	}
	if errResp.String != errorJSON {
		t.Fatalf("error request should persist error response JSON, got %q", errResp.String)
	}

	// logs 接口应返回 errorKind/errorMessage/errorResponse 字段。
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/openai/analytics/logs?days=7&page=1&pageSize=20", nil)
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logs status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Records []struct {
			StatusCode   int    `json:"statusCode"`
			ErrorKind    string `json:"errorKind"`
			ErrorMessage string `json:"errorMessage"`
			ErrorResp    string `json:"errorResponse"`
		} `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(result.Records))
	}
	for _, rec := range result.Records {
		if rec.StatusCode == http.StatusInternalServerError {
			if rec.ErrorKind != "upstream" || rec.ErrorMessage != "insufficient balance" || rec.ErrorResp != errorJSON {
				t.Fatalf("logs should carry error info for error row, got %+v", rec)
			}
		}
		if rec.StatusCode == http.StatusOK && (rec.ErrorKind != "" || rec.ErrorResp != "") {
			t.Fatalf("logs should not carry error info for success row, got %+v", rec)
		}
	}
}

// TestErrorResponseForLogTruncatesHugeBody 验证超长报错 JSON 被截断并带标记。
func TestErrorResponseForLogTruncatesHugeBody(t *testing.T) {
	huge := strings.Repeat("x", relayErrorResponseLimit+1024)
	out := errorResponseForLog([]byte(huge), http.StatusBadGateway)
	if len(out) > relayErrorResponseLimit+len(" ...(truncated)") {
		t.Fatalf("truncated body too long: %d", len(out))
	}
	if !strings.HasSuffix(out, "...(truncated)") {
		t.Fatalf("truncated body should carry marker, got %q", out[len(out)-40:])
	}
	if errorResponseForLog([]byte(`{"a":1}`), http.StatusOK) != "" {
		t.Fatal("success request should return empty error response")
	}
}

// TestTrimErrorDetailRetention 验证超出保留上限的错误详情被清空：
// 最新的 50 条保留 error_kind/error_message/response_body，更早的全部清空但行不删除。
func TestTrimErrorDetailRetention(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	ctx := context.Background()

	errInfo := &AnalyticsError{Kind: "upstream", Message: "boom", Response: `{"error":"boom"}`}
	for i := 0; i < relayErrorResponseRetention+5; i++ {
		service.recordAnalyticsKey(ctx, "chat.completions", "ep-1", "m-1", http.StatusInternalServerError, 10, 0, 0, 0, 0, 0, 0, 0, "10.0.0.1", "198.51.100.7", 0, "", errInfo)
	}
	service.flushAnalyticsQueue(5 * time.Second)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kept, cleared, total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM openai_gateway_analytics WHERE error_kind IS NOT NULL AND error_kind != ''`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM openai_gateway_analytics WHERE error_kind IS NULL OR error_kind = ''`).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM openai_gateway_analytics`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if kept != relayErrorResponseRetention {
		t.Fatalf("expected %d kept error details, got %d", relayErrorResponseRetention, kept)
	}
	if cleared != 5 {
		t.Fatalf("expected 5 cleared error details, got %d", cleared)
	}
	if total != relayErrorResponseRetention+5 {
		t.Fatalf("rows should be preserved, expected %d total, got %d", relayErrorResponseRetention+5, total)
	}
}

func TestSafeUploadPathJoin(t *testing.T) {
	dataDir := t.TempDir()
	cases := []struct {
		url      string
		ok       bool
		expected string
	}{
{"/uploads/a.png", true, dataDir + string(os.PathSeparator) + "uploads" + string(os.PathSeparator) + "a.png"},
		{"/uploads/sub/dir/img.webp", true, dataDir + string(os.PathSeparator) + "uploads" + string(os.PathSeparator) + "sub" + string(os.PathSeparator) + "dir" + string(os.PathSeparator) + "img.webp"},
		{"/uploads/../secret.txt", false, ""},
		{"/uploads/../../data/api.db", false, ""},
		{"/uploads/./../secret.txt", false, ""},
		{"/uploads/../", false, ""},
		{"/uploads", false, ""},
		{"/uploads//tmp/evil", true, dataDir + string(os.PathSeparator) + "uploads" + string(os.PathSeparator) + "tmp" + string(os.PathSeparator) + "evil"},
		{"/uploads/..%2f..%2fsecret", true, dataDir + string(os.PathSeparator) + "uploads" + string(os.PathSeparator) + "..%2f..%2fsecret"},
		{"//uploads/a.png", false, ""},
		{"/other/a.png", false, ""},
		{"uploads/a.png", false, ""},
		{"", false, ""},
	}
	for _, tc := range cases {
		got, ok := safeUploadPathJoin(dataDir, tc.url)
		if ok != tc.ok {
			t.Errorf("safeUploadPathJoin(%q) ok=%v, want %v", tc.url, ok, tc.ok)
			continue
		}
		if ok && got != tc.expected {
			t.Errorf("safeUploadPathJoin(%q) = %q, want %q", tc.url, got, tc.expected)
		}
	}
	if _, ok := safeUploadPathJoin("", "/uploads/a.png"); ok {
		t.Error("empty dataDir must be rejected")
	}
}

func TestInlineLocalUploadImageRejectsTraversal(t *testing.T) {
	dataDir := t.TempDir()
	uploadsDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(dataDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	picPath := filepath.Join(uploadsDir, "pic.png")
	if err := os.WriteFile(picPath, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := New(config.Config{DataDir: dataDir})

	traversal := map[string]interface{}{"url": "/uploads/../secret.txt"}
	s.inlineLocalUploadImage(traversal, s.cfg.DataDir)
	if traversal["url"] != "/uploads/../secret.txt" || traversal["_original_url"] != nil {
		t.Fatalf("traversal must not be inlined: %#v", traversal)
	}

	valid := map[string]interface{}{"url": "/uploads/pic.png"}
	s.inlineLocalUploadImage(valid, s.cfg.DataDir)
	if !strings.HasPrefix(valid["url"].(string), "data:image/png;base64,") {
		t.Fatalf("valid upload should be inlined, got %#v", valid)
	}
	if valid["_original_url"] != "/uploads/pic.png" {
		t.Fatalf("original url should be preserved, got %#v", valid)
	}
}

func TestIsRateLimitResponseExcludesOverloadCodes(t *testing.T) {
	// 503/529 是过载信号而非客户端限流：不累计进「连续 429 冻结」，
	// 但仍应可重试（isRetryableUpstreamResponse 单独兜底）。
	for _, code := range []int{503, 529} {
		resp := &http.Response{StatusCode: code}
		if isRateLimitResponse(resp, nil) {
			t.Fatalf("status %d must not count as rate limit", code)
		}
		if !isRetryableUpstreamResponse(resp, nil) {
			t.Fatalf("status %d should still be retryable", code)
		}
	}
	for _, code := range []int{429, 439} {
		resp := &http.Response{StatusCode: code}
		if !isRateLimitResponse(resp, nil) {
			t.Fatalf("status %d should count as rate limit", code)
		}
	}
	// 503 携限流关键词时仍应判定为限流
	resp := &http.Response{StatusCode: 503}
	if !isRateLimitResponse(resp, []byte("rate limit exceeded")) {
		t.Fatal("rate limit keyword in body should still count")
	}
}

func TestEndpointWeightIncludesPriority(t *testing.T) {
	if got := endpointWeight(Endpoint{Weight: 100}); got != 100 {
		t.Fatalf("default weight = %d, want 100", got)
	}
	if got := endpointWeight(Endpoint{Weight: 100, Priority: 2}); got != 200 {
		t.Fatalf("priority-weighted = %d, want 200", got)
	}
	if got := endpointWeight(Endpoint{Weight: 0, Priority: 1}); got != 51 {
		t.Fatalf("fallback weight = %d, want 51", got)
	}
	if got := endpointWeight(Endpoint{}); got != 1 {
		t.Fatalf("empty endpoint weight = %d, want 1", got)
	}
}

func TestEndpointExportImportPreservesAllFields(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	db, err := service.open(context.Background())
	if err != nil { t.Fatal(err) }
	defer db.Close()

	create := Endpoint{
		Name:        "full-fields",
		BaseURL:     "https://upstream.example.com/v1",
		APIKey:      "main-key-123",
		APIKeys:     []string{"extra-key-456", "extra-key-789"},
		Enabled:     true,
		Models:      []string{"gpt-4o", "gpt-4o-mini"},
		Headers:     []HeaderItem{{Name: "X-Custom", Value: "hello"}},
		DisabledModels: []string{"gpt-3.5-turbo"},
		ProxyPool:   []string{"http://p1:8080", "socks5://p2:1080"},
		ProxyBatches: []ProxyBatch{{ID: "b1", Name: "batch-a", CreatedAt: "2026-01-01T00:00:00Z", Proxies: []string{"http://p1:8080"}}},
		ProxyEnabled: true,
		AutoSwitch:   true,
		ForceProxy:   true,
		Protocol:     "http",
		ModelMappings: map[string]string{"deepseek-chat": "deepseek-v3"},
		Priority:     3,
		Weight:       7,
	}
	modelsJSON, _ := json.Marshal(create.Models)
	headersJSON, _ := json.Marshal(create.Headers)
	disabledJSON, _ := json.Marshal(create.DisabledModels)
	proxyJSON, _ := json.Marshal(create.ProxyPool)
	batchesJSON, _ := json.Marshal(create.ProxyBatches)
	mappingsJSON, _ := json.Marshal(create.ModelMappings)
	apiKeysJSON, _ := json.Marshal(create.APIKeys)
	id := "oai_full_fields"
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings, priority, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, ?, ?, ?, '2026-01-01T00:00:00Z')`,
		id, create.Name, create.BaseURL, create.APIKey, string(apiKeysJSON), string(headersJSON), string(disabledJSON), string(proxyJSON), string(batchesJSON), 1, 1, 1, create.Protocol, string(modelsJSON), string(mappingsJSON), create.Priority, create.Weight)
	if err != nil {
		t.Fatalf("insert full endpoint: %v", err)
	}
	_ = id

	wExport := httptest.NewRecorder()
	rExport, _ := http.NewRequest("GET", "/api/openai/export", nil)
	service.ServeHTTP(wExport, rExport)
	if wExport.Code != http.StatusOK {
		t.Fatalf("export status=%d", wExport.Code)
	}
	var exported struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	mustDecode(t, wExport.Body.String(), &exported)
	if len(exported.Endpoints) != 1 {
		t.Fatalf("export endpoints=%d", len(exported.Endpoints))
	}
	ep := exported.Endpoints[0]
	if ep.Priority != 3 || ep.Weight != 7 {
		t.Errorf("priority/weight lost: %d/%d", ep.Priority, ep.Weight)
	}
	if !ep.ProxyEnabled || !ep.ForceProxy || !ep.AutoSwitch {
		t.Errorf("proxy flags lost: %#v", ep)
	}
	if len(ep.ProxyBatches) != 1 || ep.ProxyBatches[0].Name != "batch-a" || len(ep.ProxyBatches[0].Proxies) != 1 {
		t.Errorf("proxy batches lost: %#v", ep.ProxyBatches)
	}
	if len(ep.APIKeys) != 2 {
		t.Errorf("api keys lost: %#v", ep.APIKeys)
	}
	if ep.ModelMappings["deepseek-chat"] != "deepseek-v3" {
		t.Errorf("model mappings lost: %#v", ep.ModelMappings)
	}
	if len(ep.Headers) != 1 || ep.Headers[0].Name != "X-Custom" {
		t.Errorf("headers lost: %#v", ep.Headers)
	}
	if len(ep.Models) != 2 || len(ep.DisabledModels) != 1 {
		t.Errorf("models/disabled lost: %#v %#v", ep.Models, ep.DisabledModels)
	}

	// 导入到全新实例（overwrite），再导出验证往返一致
	service2 := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	impBody, _ := json.Marshal(map[string]interface{}{"endpoints": exported.Endpoints, "overwrite": true})
	rImport, _ := http.NewRequest("POST", "/api/openai/import", bytes.NewReader(impBody))
	wImport := httptest.NewRecorder()
	service2.ServeHTTP(wImport, rImport)
	if wImport.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", wImport.Code, wImport.Body.String())
	}
	rExport2, _ := http.NewRequest("GET", "/api/openai/export", nil)
	wExport2 := httptest.NewRecorder()
	service2.ServeHTTP(wExport2, rExport2)
	var exported2 struct {
		Endpoints []Endpoint `json:"endpoints"`
	}
	mustDecode(t, wExport2.Body.String(), &exported2)
	if len(exported2.Endpoints) != 1 {
		t.Fatalf("re-export endpoints=%d", len(exported2.Endpoints))
	}
	ep2 := exported2.Endpoints[0]
	if ep2.Priority != 3 || ep2.Weight != 7 || len(ep2.ProxyBatches) != 1 || len(ep2.APIKeys) != 2 || ep2.ModelMappings["deepseek-chat"] != "deepseek-v3" || !ep2.ProxyEnabled || !ep2.ForceProxy {
		t.Errorf("round-trip lost fields: %#v", ep2)
	}
	if ep2.APIKey != "main-key-123" {
		t.Errorf("round-trip apiKey mismatch: %q", ep2.APIKey)
	}
}

// TestResolveEndpointModelPrefersRoutableMapping 覆盖多个内部模型映射到同一
// 外部名且部分被 disabled_models 禁用的场景：路由必须稳定选中未被禁用的别名，
// 不能受 Go map 迭代顺序影响而间歇性不可路由。
func TestResolveEndpointModelPrefersRoutableMapping(t *testing.T) {
	s := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	testCases := []struct {
		name      string
		mappings  map[string]string
		disabled  []string
		requested string
		wantReal  string
		wantOK    bool
	}{
		{
			name:      "dual mapping one disabled picks enabled",
			mappings:  map[string]string{"gcli-gemini-3.1-pro-preview": "gemini-3.1-pro-preview", "gcli-gemini-3.1-pro-preview-search": "gemini-3.1-pro-preview"},
			disabled:  []string{"gcli-gemini-3.1-pro-preview"},
			requested: "gemini-3.1-pro-preview",
			wantReal:  "gcli-gemini-3.1-pro-preview-search",
			wantOK:    true,
		},
		{
			name:      "dual mapping all disabled not routable",
			mappings:  map[string]string{"gcli-gemini-3.1-pro-preview": "gemini-3.1-pro-preview", "gcli-gemini-3.1-pro-preview-search": "gemini-3.1-pro-preview"},
			disabled:  []string{"gcli-gemini-3.1-pro-preview", "gcli-gemini-3.1-pro-preview-search"},
			requested: "gemini-3.1-pro-preview",
			wantReal:  "",
			wantOK:    false,
		},
		{
			name:      "single mapping enabled",
			mappings:  map[string]string{"deepseek-v4-flash-free": "deepseek-v4-flash"},
			disabled:  []string{"big-pickle"},
			requested: "deepseek-v4-flash",
			wantReal:  "deepseek-v4-flash-free",
			wantOK:    true,
		},
		{
			name:      "single mapping disabled not routable",
			mappings:  map[string]string{"deepseek-v4-flash-free": "deepseek-v4-flash"},
			disabled:  []string{"deepseek-v4-flash-free"},
			requested: "deepseek-v4-flash",
			wantReal:  "",
			wantOK:    false,
		},
		{
			name:      "no mapping requested enabled",
			mappings:  map[string]string{},
			disabled:  []string{"gpt-4"},
			requested: "gpt-4o",
			wantReal:  "gpt-4o",
			wantOK:    true,
		},
		{
			name:      "no mapping requested disabled",
			mappings:  map[string]string{},
			disabled:  []string{"gpt-4"},
			requested: "gpt-4",
			wantReal:  "gpt-4",
			wantOK:    false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ep := Endpoint{ModelMappings: tc.mappings, DisabledModels: tc.disabled}
			for i := 0; i < 50; i++ {
				real, ok := s.resolveEndpointModel(ep, tc.requested)
				if real != tc.wantReal || ok != tc.wantOK {
					t.Fatalf("iteration %d: resolveEndpointModel(%q) = (%q, %v); want (%q, %v)",
						i, tc.requested, real, ok, tc.wantReal, tc.wantOK)
				}
			}
		})
	}
}

// TestSelectEndpointCandidatesDualMapping 验证双映射 + 部分禁用时端点稳定进入
// 候选（模拟真实场景：gcli-gemini-3.1-pro-preview 被禁用、-search 已启用）。
func TestSelectEndpointCandidatesDualMapping(t *testing.T) {
	s := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	mappingsJSON, _ := json.Marshal(map[string]string{
		"gcli-gemini-3.1-pro-preview":       "gemini-3.1-pro-preview",
		"gcli-gemini-3.1-pro-preview-search": "gemini-3.1-pro-preview",
	})
	disabledJSON, _ := json.Marshal([]string{"gcli-gemini-3.1-pro-preview"})
	modelsJSON, _ := json.Marshal([]string{"gcli-gemini-3.1-pro-preview", "gcli-gemini-3.1-pro-preview-search"})
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, status, enabled, models, model_mappings, disabled_models)
		VALUES ('oai_dual_mapping', 'catiecli', 'https://example.com/v1', 'sk-test', 'valid', 1, ?, ?, ?)`,
		string(modelsJSON), string(mappingsJSON), string(disabledJSON))
	if err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		candidates, _, _, selectedModel, found := s.selectEndpointCandidates(context.Background(), db, "gemini-3.1-pro-preview", "", "")
		if !found {
			t.Fatalf("iteration %d: no candidate found", i)
		}
		if len(candidates) != 1 || candidates[0].ID != "oai_dual_mapping" {
			t.Fatalf("iteration %d: candidates=%#v", i, candidates)
		}
		if selectedModel != "gcli-gemini-3.1-pro-preview-search" {
			t.Fatalf("iteration %d: selectedModel=%q", i, selectedModel)
		}
		seen[selectedModel] = true
	}
	if len(seen) != 1 {
		t.Fatalf("selected model unstable across iterations: %#v", seen)
	}
}

func TestAnalyticsChartsByDimensionCarryTokenSeries(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO openai_gateway_analytics (endpoint_id, route, model, status_code, latency_ms, prompt_tokens, completion_tokens, total_tokens, cached_tokens, timestamp)
		VALUES
			('ep1', 'chat.completions', 'model-a', 200, 100, 500, 1000, 1500, 300, datetime('now', '-1 day')),
			('ep1', 'chat.completions', 'model-a', 200, 200, 200, 800, 1000, 700, datetime('now', '-2 days')),
			('ep1', 'chat.completions', 'model-b', 200, 150, 100, 100, 200, 0, datetime('now', '-1 day'))
	`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openai/analytics/charts?days=7&granularity=day", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("charts status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var charts struct {
		ByModel []struct {
			Model          string  `json:"model"`
			Data           []int   `json:"data"`
			Tokens         []int64 `json:"tokens"`
			TokensUncached []int64 `json:"tokensUncached"`
		} `json:"byModel"`
	}
	mustDecode(t, rec.Body.String(), &charts)

	idx := -1
	for i := range charts.ByModel {
		if charts.ByModel[i].Model == "model-a" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("byModel series missing model-a: %+v", charts.ByModel)
	}
	var totalTokens, totalData int64
	for i := range charts.ByModel[idx].Tokens {
		totalData += int64(charts.ByModel[idx].Data[i])
		totalTokens += charts.ByModel[idx].Tokens[i]
	}
	if totalData != 2 {
		t.Fatalf("model-a count series wrong: %+v", charts.ByModel[idx].Data)
	}
	if totalTokens != 2500 {
		t.Fatalf("model-a tokens series wrong: %+v", charts.ByModel[idx].Tokens)
	}
	totalUncached := int64(0)
	for _, v := range charts.ByModel[idx].TokensUncached {
		totalUncached += v
	}
	// 1500 + 1000 全部词元中，300 + 700 为缓存命中，未缓存应为 2500-1000=1500。
	if totalUncached != 1500 {
		t.Fatalf("model-a tokensUncached series wrong: %+v", charts.ByModel[idx].TokensUncached)
	}
}
