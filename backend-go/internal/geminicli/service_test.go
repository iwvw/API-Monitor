package geminicli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

type mockAuthenticator struct{}

func (m mockAuthenticator) IsAuthenticated(ctx context.Context, r *http.Request) (bool, error) {
	return true, nil
}

func TestGeminiCliLifecycle(t *testing.T) {
	// Spin up a mock upstream Google API server
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(path, ":fetchAvailableModels") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"models": {
					"gemini-2.5-pro": {
						"quotaInfo": {
							"remainingFraction": 0.85,
							"resetTime": "2026-06-15T18:00:00Z"
						}
					}
				}
			}`))
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(path, ":retrieveUserQuota") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"buckets": [
					{
						"modelId": "gemini-2.5-pro",
						"remainingFraction": 0.9,
						"resetTime": "2026-06-15T18:00:00Z",
						"tokenType": "INPUT"
					}
				]
			}`))
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(path, ":generateContent") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"thought": true,
									"text": "this is thought text"
								},
								{
									"text": "this is final answer"
								}
							]
						},
						"finishReason": "STOP"
					}
				],
				"usageMetadata": {
					"promptTokenCount": 10,
					"candidatesTokenCount": 20,
					"totalTokenCount": 30
				}
			}`))
			return
		}

		if r.Method == http.MethodPost && strings.HasSuffix(path, ":streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			chunk := `{"response":{"candidates":[{"content":{"parts":[{"thought":true,"text":"thinking chunk"}]},"finishReason":null}],"usageMetadata":{"totalTokenCount":5}}}`
			w.Write([]byte("data: " + chunk + "\n\n"))

			chunk2 := `{"response":{"candidates":[{"content":{"parts":[{"text":"final chunk"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":15}}}`
			w.Write([]byte("data: " + chunk2 + "\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	// Spin up a mock Google OAuth token endpoint
	mockOAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"access_token": "mock-access-token-456",
			"expires_in": 3600
		}`))
	}))
	defer mockOAuth.Close()

	tempDir := t.TempDir()
	s := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}, mockAuthenticator{})

	// Override URL to mock upstream
	s.codeAssistBase = mockUpstream.URL + "/"
	s.oauthTokenUrl = mockOAuth.URL

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// 1. Check stats empty
	wStats := httptest.NewRecorder()
	rStats, _ := http.NewRequest("GET", "/api/gemini-cli/stats", nil)
	s.ServeHTTP(wStats, rStats)
	if wStats.Code != http.StatusOK {
		t.Fatalf("stats status = %d, body = %s", wStats.Code, wStats.Body.String())
	}

	// 2. Matrix get/post
	wMatrixGet := httptest.NewRecorder()
	rMatrixGet, _ := http.NewRequest("GET", "/api/gemini-cli/config/matrix", nil)
	s.ServeHTTP(wMatrixGet, rMatrixGet)
	if wMatrixGet.Code != http.StatusOK {
		t.Fatalf("matrix get status = %d", wMatrixGet.Code)
	}

	wMatrixPost := httptest.NewRecorder()
	rMatrixPost, _ := http.NewRequest("POST", "/api/gemini-cli/config/matrix", strings.NewReader(`{
		"gemini-2.5-pro": {
			"base": true,
			"maxThinking": true
		}
	}`))
	s.ServeHTTP(wMatrixPost, rMatrixPost)
	if wMatrixPost.Code != http.StatusOK {
		t.Fatalf("matrix post status = %d", wMatrixPost.Code)
	}

	// 3. Settings save/get
	wSettingsGet := httptest.NewRecorder()
	rSettingsGet, _ := http.NewRequest("GET", "/api/gemini-cli/settings", nil)
	s.ServeHTTP(wSettingsGet, rSettingsGet)
	if wSettingsGet.Code != http.StatusOK {
		t.Fatalf("settings get status = %d", wSettingsGet.Code)
	}

	wSettingsPost := httptest.NewRecorder()
	rSettingsPost, _ := http.NewRequest("POST", "/api/gemini-cli/settings", strings.NewReader(`{
		"SYSTEM_INSTRUCTION": "custom instructions",
		"API_KEY": "123456"
	}`))
	s.ServeHTTP(wSettingsPost, rSettingsPost)
	if wSettingsPost.Code != http.StatusOK {
		t.Fatalf("settings post status = %d", wSettingsPost.Code)
	}

	// 4. Accounts CRUD
	wCreateAcc := httptest.NewRecorder()
	rCreateAcc, _ := http.NewRequest("POST", "/api/gemini-cli/accounts", strings.NewReader(`{
		"id": "my-account-id",
		"name": "My Account",
		"client_id": "test-client-id",
		"client_secret": "test-client-secret",
		"refresh_token": "test-refresh-token",
		"project_id": "gcp-project-123"
	}`))
	s.ServeHTTP(wCreateAcc, rCreateAcc)
	if wCreateAcc.Code != http.StatusOK {
		t.Fatalf("create account status = %d, body = %s", wCreateAcc.Code, wCreateAcc.Body.String())
	}

	wListAcc := httptest.NewRecorder()
	rListAcc, _ := http.NewRequest("GET", "/api/gemini-cli/accounts", nil)
	s.ServeHTTP(wListAcc, rListAcc)
	if wListAcc.Code != http.StatusOK {
		t.Fatalf("list accounts status = %d, body = %s", wListAcc.Code, wListAcc.Body.String())
	}
	var accounts []Account
	_ = json.NewDecoder(wListAcc.Body).Decode(&accounts)
	if len(accounts) != 1 || accounts[0].ID != "my-account-id" {
		t.Fatalf("expected 1 account, got %#v", accounts)
	}

	wExportAcc := httptest.NewRecorder()
	rExportAcc, _ := http.NewRequest("GET", "/api/gemini-cli/accounts/export", nil)
	s.ServeHTTP(wExportAcc, rExportAcc)
	if wExportAcc.Code != http.StatusOK {
		t.Fatalf("export accounts status = %d, body = %s", wExportAcc.Code, wExportAcc.Body.String())
	}
	var exportPayload struct {
		Type     string                   `json:"type"`
		Accounts []map[string]interface{} `json:"accounts"`
	}
	if err := json.NewDecoder(wExportAcc.Body).Decode(&exportPayload); err != nil {
		t.Fatalf("decode export accounts: %v", err)
	}
	if exportPayload.Type != "gemini-cli-accounts" || len(exportPayload.Accounts) != 1 || exportPayload.Accounts[0]["refresh_token"] != "test-refresh-token" {
		t.Fatalf("unexpected export payload: %#v", exportPayload)
	}

	wImportAcc := httptest.NewRecorder()
	rImportAcc, _ := http.NewRequest("POST", "/api/gemini-cli/accounts/import", strings.NewReader(`{
		"accounts": [
			{
				"id": "my-account-id",
				"name": "Duplicate",
				"client_id": "duplicate-client",
				"refresh_token": "duplicate-refresh"
			},
			{
				"id": "imported-account-id",
				"name": "Imported Account",
				"email": "imported@example.com",
				"client_id": "import-client-id",
				"client_secret": "import-client-secret",
				"refresh_token": "import-refresh-token",
				"project_id": "import-project"
			}
		]
	}`))
	s.ServeHTTP(wImportAcc, rImportAcc)
	if wImportAcc.Code != http.StatusOK {
		t.Fatalf("import accounts status = %d, body = %s", wImportAcc.Code, wImportAcc.Body.String())
	}
	var importPayload map[string]interface{}
	if err := json.NewDecoder(wImportAcc.Body).Decode(&importPayload); err != nil {
		t.Fatalf("decode import accounts: %v", err)
	}
	if importPayload["imported"] != float64(2) || importPayload["skipped"] != float64(0) {
		t.Fatalf("unexpected import accounts payload: %#v", importPayload)
	}
	_, _ = db.Exec("UPDATE gemini_cli_accounts SET enable = 0 WHERE id <> 'my-account-id'")
	_, _ = db.Exec("UPDATE gemini_cli_accounts SET enable = 1, status = 'online' WHERE id = 'my-account-id'")

	// 5. Redirect CRUD
	wRedirectPost := httptest.NewRecorder()
	rRedirectPost, _ := http.NewRequest("POST", "/api/gemini-cli/models/redirects", strings.NewReader(`{
		"sourceModel": "alias-pro",
		"targetModel": "gemini-2.5-pro"
	}`))
	s.ServeHTTP(wRedirectPost, rRedirectPost)
	if wRedirectPost.Code != http.StatusOK {
		t.Fatalf("redirect post status = %d", wRedirectPost.Code)
	}

	wRedirectGet := httptest.NewRecorder()
	rRedirectGet, _ := http.NewRequest("GET", "/api/gemini-cli/models/redirects", nil)
	s.ServeHTTP(wRedirectGet, rRedirectGet)
	if wRedirectGet.Code != http.StatusOK {
		t.Fatalf("redirect get status = %d", wRedirectGet.Code)
	}

	// Mock token retrieval using environment token endpoints
	// For testing, we mock token cache insertion directly or use mocked HTTP endpoint
	_, _ = db.Exec(`INSERT OR REPLACE INTO gemini_cli_tokens (id, account_id, access_token, expires_at, project_id, email, enable)
		VALUES ('my-account-id', 'my-account-id', 'mock-access-token-123', ?, 'gcp-project-123', 'a@b.com', 1)`,
		time.Now().Unix()+3600)

	// Set status to online in DB
	_, _ = db.Exec("UPDATE gemini_cli_accounts SET status = 'online' WHERE id = 'my-account-id'")

	// 6. Test completions streaming
	wStreamComp := httptest.NewRecorder()
	rStreamComp, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gemini-2.5-pro",
		"stream": true,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`))
	rStreamComp.Header.Set("Authorization", "Bearer 123456")
	s.ServeHTTP(wStreamComp, rStreamComp)
	if wStreamComp.Code != http.StatusOK {
		t.Fatalf("streaming completions status = %d, body = %s", wStreamComp.Code, wStreamComp.Body.String())
	}
	if !strings.Contains(wStreamComp.Body.String(), "thinking chunk") {
		t.Fatalf("expected thoughts in stream, got = %s", wStreamComp.Body.String())
	}

	// 7. Test completions non-streaming
	wNonStreamComp := httptest.NewRecorder()
	rNonStreamComp, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "gemini-2.5-pro",
		"stream": false,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`))
	rNonStreamComp.Header.Set("Authorization", "Bearer 123456")
	s.ServeHTTP(wNonStreamComp, rNonStreamComp)
	if wNonStreamComp.Code != http.StatusOK {
		t.Fatalf("non-streaming completions status = %d, body = %s", wNonStreamComp.Code, wNonStreamComp.Body.String())
	}
	var nonStreamRes map[string]interface{}
	_ = json.Unmarshal(wNonStreamComp.Body.Bytes(), &nonStreamRes)
	choices := nonStreamRes["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"].(string) != "this is final answer" {
		t.Fatalf("expected final answer, got = %v", msg)
	}

	// 8. Test delete account
	wDelAcc := httptest.NewRecorder()
	rDelAcc, _ := http.NewRequest("DELETE", "/api/gemini-cli/accounts/my-account-id", nil)
	s.ServeHTTP(wDelAcc, rDelAcc)
	if wDelAcc.Code != http.StatusOK {
		t.Fatalf("delete account status = %d", wDelAcc.Code)
	}
}

func TestHealthCheckUsesStreamingCompletions(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"candidates":[],"promptFeedback":{}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":streamGenerateContent"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":3}}}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockUpstream.Close()

	tempDir := t.TempDir()
	s := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}, mockAuthenticator{})
	s.codeAssistBase = mockUpstream.URL + "/"

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_accounts (id, name, email, client_id, client_secret, refresh_token, project_id, enable, status)
		VALUES ('health-account', 'Health Account', 'health@example.com', 'cid', 'secret', 'refresh', 'project-id', 1, 'online')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_tokens (id, account_id, access_token, expires_at, project_id, email, enable)
		VALUES ('health-account', 'health-account', 'mock-access-token', ?, 'project-id', 'health@example.com', 1)`,
		time.Now().Unix()+3600)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}

	account := Account{ID: "health-account", Name: "Health Account", Enable: true, Status: "online"}
	if err := s.testAccountModel(context.Background(), db, account, "gemini-2.5-flash"); err != nil {
		t.Fatalf("health check should pass via stream endpoint, got %v", err)
	}
}

func TestCheckAccountsRouteWaitsForCheckCompletion(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			time.Sleep(150 * time.Millisecond)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":3}}}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	tempDir := t.TempDir()
	s := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}, mockAuthenticator{})
	s.codeAssistBase = mockUpstream.URL + "/"
	if err := s.saveMatrixConfig(map[string]interface{}{
		"gemini-2.5-flash": map[string]interface{}{"base": true},
	}); err != nil {
		t.Fatalf("save matrix: %v", err)
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_accounts (id, name, email, client_id, client_secret, refresh_token, project_id, enable, status)
		VALUES ('route-account', 'Route Account', 'route@example.com', 'cid', 'secret', 'refresh', 'project-id', 1, 'online')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_tokens (id, account_id, access_token, expires_at, project_id, email, enable)
		VALUES ('route-account', 'route-account', 'mock-access-token', ?, 'project-id', 'route@example.com', 1)`,
		time.Now().Unix()+3600)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}

	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodPost, "/api/gemini-cli/accounts/check", nil)
	start := time.Now()
	s.ServeHTTP(w, r)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("check route status=%d body=%s", w.Code, w.Body.String())
	}
	if elapsed < 120*time.Millisecond {
		t.Fatalf("check route returned before upstream completed: elapsed=%s", elapsed)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["message"] != "检测完成" || int(payload["modelsChecked"].(float64)) != 1 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestRunCheckChecksEnabledAccountsConcurrently(t *testing.T) {
	var inFlight int64
	var maxConcurrent int64
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			current := atomic.AddInt64(&inFlight, 1)
			defer atomic.AddInt64(&inFlight, -1)
			for {
				previous := atomic.LoadInt64(&maxConcurrent)
				if current <= previous || atomic.CompareAndSwapInt64(&maxConcurrent, previous, current) {
					break
				}
			}
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"OK\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":3}}}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	tempDir := t.TempDir()
	s := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}, mockAuthenticator{})
	s.codeAssistBase = mockUpstream.URL + "/"
	if err := s.saveMatrixConfig(map[string]interface{}{
		"gemini-2.5-flash": map[string]interface{}{"base": true},
	}); err != nil {
		t.Fatalf("save matrix: %v", err)
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	for _, accountID := range []string{"parallel-a", "parallel-b"} {
		_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_accounts (id, name, email, client_id, client_secret, refresh_token, project_id, enable, status)
			VALUES (?, ?, ?, 'cid', 'secret', 'refresh', 'project-id', 1, 'online')`,
			accountID, accountID, accountID+"@example.com")
		if err != nil {
			t.Fatalf("insert account %s: %v", accountID, err)
		}
		_, err = db.Exec(`INSERT OR REPLACE INTO gemini_cli_tokens (id, account_id, access_token, expires_at, project_id, email, enable)
			VALUES (?, ?, 'mock-access-token', ?, 'project-id', ?, 1)`,
			accountID, accountID, time.Now().Unix()+3600, accountID+"@example.com")
		if err != nil {
			t.Fatalf("insert token %s: %v", accountID, err)
		}
	}

	start := time.Now()
	result := s.runCheck()
	elapsed := time.Since(start)

	if result.Error != "" || result.Attempts != 2 || result.PassedModels != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if atomic.LoadInt64(&maxConcurrent) < 2 {
		t.Fatalf("expected account checks to run concurrently, maxConcurrent=%d elapsed=%s", maxConcurrent, elapsed)
	}

	var passedAccounts string
	if err := db.QueryRow(`SELECT passed_accounts FROM gemini_cli_model_checks WHERE model_id = 'gemini-2.5-flash'`).Scan(&passedAccounts); err != nil {
		t.Fatalf("query check result: %v", err)
	}
	if passedAccounts != "0,1" {
		t.Fatalf("passed accounts = %q, want 0,1", passedAccounts)
	}
}
