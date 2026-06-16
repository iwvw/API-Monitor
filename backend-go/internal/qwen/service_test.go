package qwen

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestQwenLifecycleAndProxy(t *testing.T) {
	// Spin up a mock upstream Qwen server to handle completions and image generation
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"Invalid Auth Header"}}`))
			return
		}

		if r.Method == http.MethodGet && path == "/api/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"qwen-max"},{"id":"qwen-plus"}]}`))
			return
		}

		if r.Method == http.MethodPost && path == "/api/v2/chats/new" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":{"id":"mock-chat-123"}}`))
			return
		}

		if r.Method == http.MethodPost && path == "/api/v2/chat/completions" {
			var body struct {
				Messages []interface{} `json:"messages"`
				Stream   bool          `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			// Mock qwen stream chunk 1: thinking phase
			chunk1 := `{"phase":"thinking","choices":[{"delta":{"phase":"thinking","content":"thinking phase text","reasoning_content":"thinking phase text"}}]}`
			// Mock qwen stream chunk 2: output content
			chunk2 := `{"phase":"output","choices":[{"delta":{"phase":"output","content":"hello world output"}}], "text":"hello world output"}`
			
			w.Write([]byte("data: " + chunk1 + "\n\n"))
			w.Write([]byte("data: " + chunk2 + "\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		if r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v2/chats/") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockUpstream.Close()

	s := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	s.apiBaseURL = mockUpstream.URL

	// 1. Get stats on empty DB
	wStats := httptest.NewRecorder()
	rStats, _ := http.NewRequest("GET", "/api/qwen/stats", nil)
	s.ServeHTTP(wStats, rStats)
	if wStats.Code != http.StatusOK {
		t.Fatalf("stats empty status = %d body=%s", wStats.Code, wStats.Body.String())
	}
	var stats map[string]interface{}
	mustDecode(t, wStats.Body.String(), &stats)
	if stats["total_calls"].(float64) != 0 {
		t.Fatalf("expected 0 calls, got %v", stats["total_calls"])
	}

	// 2. Matrix settings
	wMatrixGet := httptest.NewRecorder()
	rMatrixGet, _ := http.NewRequest("GET", "/api/qwen/matrix", nil)
	s.ServeHTTP(wMatrixGet, rMatrixGet)
	if wMatrixGet.Code != http.StatusOK {
		t.Fatalf("matrix get empty status = %d", wMatrixGet.Code)
	}

	wMatrixPut := httptest.NewRecorder()
	rMatrixPut, _ := http.NewRequest("PUT", "/api/qwen/matrix/test-id", strings.NewReader(`{"checked":true,"model":"qwen-plus"}`))
	s.ServeHTTP(wMatrixPut, rMatrixPut)
	if wMatrixPut.Code != http.StatusOK {
		t.Fatalf("matrix put status = %d body=%s", wMatrixPut.Code, wMatrixPut.Body.String())
	}

	// 3. Sync models (dummy check)
	wSync := httptest.NewRecorder()
	rSync, _ := http.NewRequest("POST", "/api/qwen/sync-models", nil)
	s.ServeHTTP(wSync, rSync)
	if wSync.Code != http.StatusOK {
		t.Fatalf("sync-models status = %d", wSync.Code)
	}

	// 4. Accounts CRUD
	// Create account
	wCreateAcc := httptest.NewRecorder()
	rCreateAcc, _ := http.NewRequest("POST", "/api/qwen/accounts", strings.NewReader(`{
		"name": "My Qwen Account",
		"token": "token=my-secret-jwt-token-part.jwt-payload-encoded.signature-part",
		"enable": true
	}`))
	s.ServeHTTP(wCreateAcc, rCreateAcc)
	if wCreateAcc.Code != http.StatusOK {
		t.Fatalf("create account status = %d body=%s", wCreateAcc.Code, wCreateAcc.Body.String())
	}
	var createAccRes struct {
		Success bool   `json:"success"`
		ID      string `json:"id"`
	}
	mustDecode(t, wCreateAcc.Body.String(), &createAccRes)
	if !createAccRes.Success || createAccRes.ID == "" {
		t.Fatalf("invalid create response: %s", wCreateAcc.Body.String())
	}

	// List accounts
	wListAcc := httptest.NewRecorder()
	rListAcc, _ := http.NewRequest("GET", "/api/qwen/accounts", nil)
	s.ServeHTTP(wListAcc, rListAcc)
	if wListAcc.Code != http.StatusOK {
		t.Fatalf("list accounts status = %d body=%s", wListAcc.Code, wListAcc.Body.String())
	}
	var accounts []Account
	mustDecode(t, wListAcc.Body.String(), &accounts)
	if len(accounts) != 1 || accounts[0].ID != createAccRes.ID {
		t.Fatalf("expected 1 account with ID %s, got %#v", createAccRes.ID, accounts)
	}

	// We must ensure the account is updated to 'online' status for completions proxy to select it,
	// because `proxyChatCompletions` selects `status != 'invalid'`. The initial status is 'unknown'.
	// In listAccounts, it triggers async check, but let's force it to 'online' in DB to be deterministic.
	db, err := s.open(rListAcc.Context())
	if err != nil {
		t.Fatalf("db open failed: %v", err)
	}
	_, err = db.Exec("UPDATE qwen_accounts SET status = 'online' WHERE id = ?", createAccRes.ID)
	db.Close()
	if err != nil {
		t.Fatalf("failed to update account status to online: %v", err)
	}

	// 5. Settings
	wSaveSettings := httptest.NewRecorder()
	rSaveSettings, _ := http.NewRequest("POST", "/api/qwen/settings", strings.NewReader(`{
		"API_KEY": "some-api-key",
		"SYSTEM_INSTRUCTION": "custom system prompt"
	}`))
	s.ServeHTTP(wSaveSettings, rSaveSettings)
	if wSaveSettings.Code != http.StatusOK {
		t.Fatalf("save settings status = %d", wSaveSettings.Code)
	}

	wGetSettings := httptest.NewRecorder()
	rGetSettings, _ := http.NewRequest("GET", "/api/qwen/settings", nil)
	s.ServeHTTP(wGetSettings, rGetSettings)
	if wGetSettings.Code != http.StatusOK {
		t.Fatalf("get settings status = %d", wGetSettings.Code)
	}
	var settings map[string]string
	mustDecode(t, wGetSettings.Body.String(), &settings)
	if settings["API_KEY"] != "some-api-key" || settings["SYSTEM_INSTRUCTION"] != "custom system prompt" {
		t.Fatalf("unexpected settings: %#v", settings)
	}

	// 6. Redirects CRUD
	wCreateRedirect := httptest.NewRecorder()
	rCreateRedirect, _ := http.NewRequest("POST", "/api/qwen/models/redirects", strings.NewReader(`{
		"sourceModel": "qwen-max-src",
		"targetModel": "qwen-max"
	}`))
	s.ServeHTTP(wCreateRedirect, rCreateRedirect)
	if wCreateRedirect.Code != http.StatusOK {
		t.Fatalf("create redirect status = %d", wCreateRedirect.Code)
	}

	wGetRedirects := httptest.NewRecorder()
	rGetRedirects, _ := http.NewRequest("GET", "/api/qwen/models/redirects", nil)
	s.ServeHTTP(wGetRedirects, rGetRedirects)
	if wGetRedirects.Code != http.StatusOK {
		t.Fatalf("get redirects status = %d body=%s", wGetRedirects.Code, wGetRedirects.Body.String())
	}
	var redirects []Redirect
	mustDecode(t, wGetRedirects.Body.String(), &redirects)
	if len(redirects) != 1 || redirects[0].SourceModel != "qwen-max-src" || redirects[0].TargetModel != "qwen-max" {
		t.Fatalf("unexpected redirects list: %#v", redirects)
	}

	// 7. Completions Proxy (streaming)
	wCompletionsStream := httptest.NewRecorder()
	rCompletionsStream, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model": "qwen-max-src",
		"messages": [{"role":"user","content":"hello qwen"}],
		"stream": true
	}`))
	s.ServeHTTP(wCompletionsStream, rCompletionsStream)
	if wCompletionsStream.Code != http.StatusOK {
		t.Fatalf("completions stream status = %d body=%s", wCompletionsStream.Code, wCompletionsStream.Body.String())
	}

	bodyStr := wCompletionsStream.Body.String()
	if !strings.Contains(bodyStr, "reasoning_content") || !strings.Contains(bodyStr, "hello world output") {
		t.Fatalf("expected stream payload to contain reasoning_content and hello world output, got %s", bodyStr)
	}

	// 8. Completions Proxy (non-streaming)
	wCompletions := httptest.NewRecorder()
	rCompletions, _ := http.NewRequest("POST", "/api/qwen/v1/chat/completions", strings.NewReader(`{
		"model": "qwen-max",
		"messages": [{"role":"user","content":"hello qwen"}],
		"stream": false
	}`))
	s.ServeHTTP(wCompletions, rCompletions)
	if wCompletions.Code != http.StatusOK {
		t.Fatalf("completions status = %d body=%s", wCompletions.Code, wCompletions.Body.String())
	}
	var compRes struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	mustDecode(t, wCompletions.Body.String(), &compRes)
	if len(compRes.Choices) != 1 || compRes.Choices[0].Message.Content != "hello world output" || compRes.Choices[0].Message.ReasoningContent != "thinking phase text" {
		t.Fatalf("unexpected completion response: %#v", compRes)
	}

	// 9. Logs & Stats validation
	wLogs := httptest.NewRecorder()
	rLogs, _ := http.NewRequest("GET", "/api/qwen/logs", nil)
	s.ServeHTTP(wLogs, rLogs)
	if wLogs.Code != http.StatusOK {
		t.Fatalf("get logs status = %d body=%s", wLogs.Code, wLogs.Body.String())
	}
	var logs []LogItem
	mustDecode(t, wLogs.Body.String(), &logs)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 logs, got %d", len(logs))
	}

	wStats2 := httptest.NewRecorder()
	rStats2, _ := http.NewRequest("GET", "/api/qwen/stats", nil)
	s.ServeHTTP(wStats2, rStats2)
	mustDecode(t, wStats2.Body.String(), &stats)
	if stats["total_calls"].(float64) < 2 {
		t.Fatalf("expected stats total_calls >= 2, got %v", stats["total_calls"])
	}

	// Delete log check
	wClearLogs := httptest.NewRecorder()
	rClearLogs, _ := http.NewRequest("DELETE", "/api/qwen/logs", nil)
	s.ServeHTTP(wClearLogs, rClearLogs)
	if wClearLogs.Code != http.StatusOK {
		t.Fatalf("clear logs status = %d", wClearLogs.Code)
	}

	// 10. Delete redirect
	wDelRedir := httptest.NewRecorder()
	rDelRedir, _ := http.NewRequest("DELETE", "/api/qwen/models/redirects/qwen-max-src", nil)
	s.ServeHTTP(wDelRedir, rDelRedir)
	if wDelRedir.Code != http.StatusOK {
		t.Fatalf("delete redirect status = %d", wDelRedir.Code)
	}

	// 11. Delete Account
	wDelAcc := httptest.NewRecorder()
	rDelAcc, _ := http.NewRequest("DELETE", "/api/qwen/accounts/"+createAccRes.ID, nil)
	s.ServeHTTP(wDelAcc, rDelAcc)
	if wDelAcc.Code != http.StatusOK {
		t.Fatalf("delete account status = %d", wDelAcc.Code)
	}
}

func mustDecode(t *testing.T, body string, v interface{}) {
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("json decode failed: %v body=%q", err, body)
	}
}
