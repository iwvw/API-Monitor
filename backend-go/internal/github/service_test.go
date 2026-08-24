package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func TestParseRepoInput(t *testing.T) {
	tests := []struct {
		input string
		owner string
		repo  string
	}{
		{"openai/codex", "openai", "codex"},
		{"https://github.com/openai/codex", "openai", "codex"},
		{"https://github.com/openai/codex/", "openai", "codex"},
		{"https://github.com/openai/codex/actions/runs/1", "openai", "codex"},
		{"git@github.com:openai/codex.git", "openai", "codex"},
	}
	for _, tt := range tests {
		owner, repo := parseRepoInput(tt.input)
		if owner != tt.owner || repo != tt.repo {
			t.Fatalf("parseRepoInput(%q) = %q/%q, want %q/%q", tt.input, owner, repo, tt.owner, tt.repo)
		}
	}
}

func TestParseRepoInputRejectsNonGitHubURL(t *testing.T) {
	owner, repo := parseRepoInput("https://example.com/openai/codex")
	if owner != "" || repo != "" {
		t.Fatalf("expected non-GitHub URL to be rejected, got %q/%q", owner, repo)
	}
}

func TestAuthenticatedActionsPollingStaysNearRealtime(t *testing.T) {
	service := &Service{actionLastPoll: map[int64]time.Time{}}
	repo := Repository{ID: 1, LatestActionStatus: "completed"}
	now := time.Now()
	service.actionLastPoll[repo.ID] = now.Add(-9 * time.Second)
	if service.shouldPollActions(repo, true, now) {
		t.Fatal("authenticated repository polled before the 10 second interval")
	}
	service.actionLastPoll[repo.ID] = now.Add(-10 * time.Second)
	if !service.shouldPollActions(repo, true, now) {
		t.Fatal("authenticated repository should poll every 10 seconds")
	}
}

func TestWorkflowWebhookUsesActionsRefresh(t *testing.T) {
	for _, eventType := range []string{"workflow_run", "workflow_job"} {
		if !webhookUsesActionsRefresh(eventType) {
			t.Fatalf("%s should use the lightweight Actions refresh", eventType)
		}
	}
	if webhookUsesActionsRefresh("release") {
		t.Fatal("release webhook should use the full repository refresh")
	}
}

func TestPublicRealtimeResponsesDisableCaching(t *testing.T) {
	recorder := httptest.NewRecorder()
	setPublicPageCacheControl(recorder, PublicPage{CacheSeconds: 300}, true)
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("unexpected realtime cache policy: %q", got)
	}

	recorder = httptest.NewRecorder()
	setPublicPageCacheControl(recorder, PublicPage{CacheSeconds: 300}, false)
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("unexpected full page cache policy: %q", got)
	}
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	secret := "secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !verifySignature(body, secret, valid) {
		t.Fatal("expected signature to be valid")
	}
	if verifySignature(body, secret, "sha256=bad") {
		t.Fatal("expected bad signature to be rejected")
	}
}

func TestPublicPageEventPayloadOnlyExposesVisibleRepositoryIdentity(t *testing.T) {
	visibleRepositories := map[int64]struct{}{42: {}}
	payload, visible := publicPageEventPayload(visibleRepositories, map[string]interface{}{
		"kind":          "repository_actions_refresh",
		"repository_id": int64(42),
		"token":         "must-not-leak",
	})
	if !visible {
		t.Fatal("expected event for a public repository to be visible")
	}
	if payload["repository_id"] != int64(42) || payload["kind"] != "repository_actions_refresh" {
		t.Fatalf("unexpected public event payload: %#v", payload)
	}
	if _, leaked := payload["token"]; leaked {
		t.Fatalf("public event leaked private fields: %#v", payload)
	}

	if payload, visible := publicPageEventPayload(visibleRepositories, map[string]interface{}{
		"kind":          "repository_refresh",
		"repository_id": int64(7),
	}); visible || payload != nil {
		t.Fatalf("event for an unbound repository must stay private: %#v", payload)
	}
}

func TestRefreshActionsPublishesCommittedUpdate(t *testing.T) {
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/openai/codex/actions/runs" {
			t.Fatalf("unexpected GitHub API path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"workflow_runs": []map[string]interface{}{{
				"id": 42, "name": "CI", "status": "in_progress", "head_branch": "main",
				"run_started_at": "2026-07-16T00:00:00Z", "updated_at": "2026-07-16T00:01:00Z",
			}},
		})
	}))
	defer githubAPI.Close()

	cfg := config.Config{DataDir: t.TempDir(), DBName: "test.db"}
	service := New(cfg)
	defer service.Stop()
	service.client.baseURL = githubAPI.URL

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	result, err := db.Exec(`INSERT INTO github_repositories (owner, name, full_name) VALUES ('openai', 'codex', 'openai/codex')`)
	if err != nil {
		db.Close()
		t.Fatalf("insert repository: %v", err)
	}
	repoID, _ := result.LastInsertId()
	db.Close()

	events, cancel := service.subscribe()
	defer cancel()
	if err := service.refreshActionsRepositoryByID(t.Context(), repoID, "test"); err != nil {
		t.Fatalf("refresh actions: %v", err)
	}

	select {
	case event := <-events:
		if event["kind"] != "repository_actions_refresh" || event["repository_id"] != repoID {
			t.Fatalf("unexpected refresh event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing repository actions refresh event")
	}

	db, err = service.open(t.Context())
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRow(`SELECT latest_action_status FROM github_repositories WHERE id = ?`, repoID).Scan(&status); err != nil {
		t.Fatalf("read repository action status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("unexpected latest action status: %q", status)
	}
	item, _, _, ok, err := service.publicRepositorySummaryItem(t.Context(), db, repoID)
	if err != nil || !ok {
		t.Fatalf("read public repository summary: ok=%v err=%v", ok, err)
	}
	if asString(item["updated_at"]) == "" {
		t.Fatalf("public repository summary must expose its data timestamp: %#v", item)
	}
}

func TestServiceCreateRepositoryFromURL(t *testing.T) {
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/openai/codex":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "name": "codex", "full_name": "openai/codex", "html_url": "https://github.com/openai/codex",
				"description": "Code agent", "default_branch": "main", "language": "Go",
				"stargazers_count": 100, "forks_count": 5, "watchers_count": 9, "open_issues_count": 3,
			})
		case "/search/issues":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"total_count": 2})
		case "/repos/openai/codex/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tag_name": "v1.0.0", "html_url": "https://github.com/openai/codex/releases/tag/v1.0.0"})
		case "/repos/openai/codex/commits":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"sha": "a"}, {"sha": "b"}})
		case "/repos/openai/codex/contributors":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"login": "alice", "contributions": 10}})
		case "/repos/openai/codex/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"total_count": 1,
				"workflow_runs": []map[string]interface{}{{
					"id": 42, "name": "CI", "status": "completed", "conclusion": "success", "html_url": "https://example.test/run/42",
					"head_commit": map[string]interface{}{"message": "feat: add workflow commit details"},
				}},
			})
		case "/repos/openai/codex/traffic/views", "/repos/openai/codex/traffic/clones":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": 7, "uniques": 4})
		default:
			t.Fatalf("unexpected GitHub API path: %s", r.URL.Path)
		}
	}))
	defer githubAPI.Close()

	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	defer service.Stop()
	service.client.baseURL = githubAPI.URL

	req := httptest.NewRequest(http.MethodPost, "/api/github/repositories", strings.NewReader(`{"url":"https://github.com/openai/codex","collect_interval_seconds":60,"retention_days":30}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repository status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Success bool       `json:"success"`
		Data    Repository `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Success || envelope.Data.FullName != "openai/codex" || envelope.Data.Stars != 100 {
		t.Fatalf("unexpected repository response: %#v", envelope)
	}
}

func TestTokenTestCanBindRepositoryCredential(t *testing.T) {
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"login": "iwvw"})
		case "/rate_limit":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"resources": map[string]interface{}{}})
		case "/repos/iwvw/API-Monitor":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "name": "API-Monitor", "full_name": "iwvw/API-Monitor",
				"owner": map[string]interface{}{"login": "iwvw"},
			})
		case "/repos/iwvw/API-Monitor/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"total_count": 0, "workflow_runs": []interface{}{}})
		case "/repos/iwvw/API-Monitor/traffic/views", "/repos/iwvw/API-Monitor/traffic/clones":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"count": 0, "uniques": 0})
		case "/repos/iwvw/API-Monitor/hooks":
			_ = json.NewEncoder(w).Encode([]interface{}{})
		default:
			t.Fatalf("unexpected GitHub API path: %s", r.URL.Path)
		}
	}))
	defer githubAPI.Close()

	cfg := config.Config{DataDir: t.TempDir(), DBName: "test.db"}
	service := New(cfg)
	defer service.Stop()
	service.client.baseURL = githubAPI.URL

	encrypted, err := secure.SecureEncrypt("token-1")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	db, err := sql.Open("sqlite", cfg.DatabasePath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := ensureSchema(t.Context(), db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	tokenResult, err := db.Exec(`INSERT INTO github_tokens (name, token_encrypted) VALUES ('primary', ?)`, encrypted)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, _ := tokenResult.LastInsertId()
	repoResult, err := db.Exec(`INSERT INTO github_repositories (owner, name, full_name) VALUES ('iwvw', 'API-Monitor', 'iwvw/API-Monitor')`)
	if err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	repoID, _ := repoResult.LastInsertId()
	db.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/github/tokens/1/test?repositoryId=1&bind=true", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test token status=%d body=%s", rec.Code, rec.Body.String())
	}

	db, err = sql.Open("sqlite", cfg.DatabasePath())
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	var boundTokenID sql.NullInt64
	var ownedByToken int
	if err := db.QueryRow(`SELECT token_id, owned_by_token FROM github_repositories WHERE id = ?`, repoID).Scan(&boundTokenID, &ownedByToken); err != nil {
		t.Fatalf("read repository binding: %v", err)
	}
	if !boundTokenID.Valid || boundTokenID.Int64 != tokenID || ownedByToken != 1 {
		t.Fatalf("unexpected binding token=%v owned=%d", boundTokenID, ownedByToken)
	}
}

func TestWebhookPayloadsAreStoredAsCompactSummaries(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "github-storage-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	cfg := config.Config{DataDir: dataDir, DBName: "test.db"}
	service := New(cfg)
	defer func() {
		service.Stop()
		_ = os.RemoveAll(dataDir)
	}()

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	result, err := db.Exec(`INSERT INTO github_repositories (owner, name, full_name, html_url, webhook_secret) VALUES ('openai', 'codex', 'openai/codex', 'https://github.com/openai/codex', 'secret')`)
	if err != nil {
		db.Close()
		t.Fatalf("insert repository: %v", err)
	}
	repoID, _ := result.LastInsertId()
	db.Close()

	longBody := strings.Repeat("VERY-LONG-ISSUE-BODY-", 800)
	raw := `{"action":"opened","issue":{"id":123,"number":7,"title":"Bug report","body":"` + longBody + `","html_url":"https://github.com/openai/codex/issues/7","state":"open","user":{"login":"alice","html_url":"https://github.com/alice","type":"User"},"labels":[{"name":"bug"},{"name":"triage"}]},"repository":{"id":1,"name":"codex","full_name":"openai/codex","html_url":"https://github.com/openai/codex"}}`
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(raw))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook/"+strconv.FormatInt(repoID, 10), strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", signature)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status=%d body=%s", rec.Code, rec.Body.String())
	}

	db, err = service.open(t.Context())
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()

	var deliveryPayload string
	if err := db.QueryRow(`SELECT payload_json FROM github_webhook_deliveries WHERE delivery_id = 'delivery-1'`).Scan(&deliveryPayload); err != nil {
		t.Fatalf("read stored delivery payload: %v", err)
	}
	if strings.Contains(deliveryPayload, longBody) {
		t.Fatalf("delivery payload still contains the original large body")
	}
	if len(deliveryPayload) >= len(raw) {
		t.Fatalf("delivery payload was not compacted: stored=%d raw=%d", len(deliveryPayload), len(raw))
	}

	var eventPayload string
	if err := db.QueryRow(`SELECT payload_json FROM github_events WHERE repository_id = ? ORDER BY id DESC LIMIT 1`, repoID).Scan(&eventPayload); err != nil {
		t.Fatalf("read stored event payload: %v", err)
	}
	if strings.Contains(eventPayload, longBody) {
		t.Fatalf("event payload still contains the original large body")
	}
	if !strings.Contains(eventPayload, `"issue"`) || !strings.Contains(eventPayload, `"labels"`) {
		t.Fatalf("event payload summary lost expected issue fields: %s", eventPayload)
	}
}

func TestDeleteHistoryVacuumShrinksDatabaseFile(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "github-history-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	cfg := config.Config{DataDir: dataDir, DBName: "test.db"}
	service := New(cfg)
	defer func() {
		service.Stop()
		_ = os.RemoveAll(dataDir)
	}()

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	// 新建库默认 auto_vacuum=NONE（连接级 PRAGMA 不改既有文件）；先迁移为
	// INCREMENTAL（等价于设置页「数据库压缩」的迁移路径），使删除后的空间
	// 可被增量回收（incremental_vacuum），这正是被替换的同步全量 VACUUM
	// 原本的职责，这里改为在测试内显式完成迁移。
	if _, err := db.Exec(`PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		db.Close()
		t.Fatalf("enable incremental auto_vacuum: %v", err)
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		db.Close()
		t.Fatalf("migrate auto_vacuum: %v", err)
	}
	result, err := db.Exec(`INSERT INTO github_repositories (owner, name, full_name) VALUES ('openai', 'codex', 'openai/codex')`)
	if err != nil {
		db.Close()
		t.Fatalf("insert repository: %v", err)
	}
	repoID, _ := result.LastInsertId()
	largeMessage := strings.Repeat("payload-", 4000)
	for i := 0; i < 20; i++ {
		if _, err := db.Exec(`INSERT INTO github_events (repository_id, event_type, severity, title, message, payload_json, created_at) VALUES (?, 'issue_opened', 'info', 'Event', ?, ?, '2000-01-01T00:00:00Z')`, repoID, largeMessage, `{"body":"`+largeMessage+`"}`); err != nil {
			db.Close()
			t.Fatalf("insert event history: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO github_webhook_deliveries (repository_id, delivery_id, event_type, payload_json, created_at) VALUES (?, ?, 'issues', ?, '2000-01-01T00:00:00Z')`, repoID, "old-delivery-"+strconv.Itoa(i), `{"body":"`+largeMessage+`"}`); err != nil {
			db.Close()
			t.Fatalf("insert delivery history: %v", err)
		}
	}
	db.Close()

	req := httptest.NewRequest(http.MethodDelete, "/api/github/history?days=1", nil)
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete history status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 增量回收后空闲页应归零（全量 VACUUM 的副作用「文件必然变小」不适用
	// 于分批回收，改为验证 freelist 已被回收到位）。
	db, err = service.open(t.Context())
	if err != nil {
		t.Fatalf("reopen database after cleanup: %v", err)
	}
	defer db.Close()
	var freePages int64
	if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&freePages); err != nil {
		t.Fatalf("read freelist count: %v", err)
	}
	if freePages != 0 {
		t.Fatalf("expected freelist fully reclaimed after cleanup, got %d free pages", freePages)
	}
}

func TestCompactHistoryEndpointRewritesLargePayloads(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "github-compact-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	cfg := config.Config{DataDir: dataDir, DBName: "test.db"}
	service := New(cfg)
	defer func() {
		service.Stop()
		_ = os.RemoveAll(dataDir)
	}()

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	result, err := db.Exec(`INSERT INTO github_repositories (owner, name, full_name) VALUES ('openai', 'codex', 'openai/codex')`)
	if err != nil {
		db.Close()
		t.Fatalf("insert repository: %v", err)
	}
	repoID, _ := result.LastInsertId()
	largeBody := strings.Repeat("legacy-issue-body-", 1200)
	eventPayload := `{"id":123,"number":7,"title":"Legacy issue","body":"` + largeBody + `","html_url":"https://github.com/openai/codex/issues/7","state":"open","user":{"login":"alice"},"labels":[{"name":"bug"}],"repositoryId":` + strconv.FormatInt(repoID, 10) + `}`
	webhookPayload := `{"action":"opened","issue":{"id":123,"number":7,"title":"Legacy issue","body":"` + largeBody + `","html_url":"https://github.com/openai/codex/issues/7","state":"open","user":{"login":"alice"},"labels":[{"name":"bug"}]},"repository":{"id":1,"name":"codex","full_name":"openai/codex"}}`
	if _, err := db.Exec(`INSERT INTO github_events (repository_id, event_type, severity, title, message, payload_json, created_at) VALUES (?, 'issue_opened', 'info', 'Legacy issue', 'legacy', ?, '2000-01-01T00:00:00Z')`, repoID, eventPayload); err != nil {
		db.Close()
		t.Fatalf("insert legacy event payload: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO github_webhook_deliveries (repository_id, delivery_id, event_type, payload_json, created_at) VALUES (?, 'legacy-delivery', 'issues', ?, '2000-01-01T00:00:00Z')`, repoID, webhookPayload); err != nil {
		db.Close()
		t.Fatalf("insert legacy webhook payload: %v", err)
	}
	db.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/github/history/compact?repositoryId="+strconv.FormatInt(repoID, 10), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact history status=%d body=%s", rec.Code, rec.Body.String())
	}

	db, err = service.open(t.Context())
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()

	var compactEvent string
	if err := db.QueryRow(`SELECT payload_json FROM github_events WHERE repository_id = ?`, repoID).Scan(&compactEvent); err != nil {
		t.Fatalf("read compacted event payload: %v", err)
	}
	if strings.Contains(compactEvent, largeBody) {
		t.Fatalf("event payload still contains legacy large body")
	}
	var compactWebhook string
	if err := db.QueryRow(`SELECT payload_json FROM github_webhook_deliveries WHERE delivery_id = 'legacy-delivery'`).Scan(&compactWebhook); err != nil {
		t.Fatalf("read compacted webhook payload: %v", err)
	}
	if strings.Contains(compactWebhook, largeBody) {
		t.Fatalf("webhook payload still contains legacy large body")
	}
	if len(compactWebhook) >= len(webhookPayload) {
		t.Fatalf("expected webhook payload to shrink: before=%d after=%d", len(webhookPayload), len(compactWebhook))
	}
}

func TestPublicPageIconID(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "test.db"}
	service := New(cfg)
	defer service.Stop()
	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO github_public_pages (slug, domain, title, public, cache_seconds, config_json)
		VALUES ('fav-slug', 'fav.example.com', 'Fav Test', 1, 300, '{"publicIconId":"site-custom"}')`); err != nil {
		t.Fatalf("insert public page: %v", err)
	}

	iconID, found, err := service.PublicPageIconID(ctx, "fav-slug", false)
	if err != nil || !found || iconID != "site-custom" {
		t.Fatalf("custom slug lookup = (%q, %v, %v), want (site-custom, true, nil)", iconID, found, err)
	}
	iconID, found, err = service.PublicPageIconID(ctx, "fav.example.com", true)
	if err != nil || !found || iconID != "site-custom" {
		t.Fatalf("custom domain lookup = (%q, %v, %v), want (site-custom, true, nil)", iconID, found, err)
	}
	iconID, found, err = service.PublicPageIconID(ctx, "missing-slug", false)
	if err != nil || found {
		t.Fatalf("missing slug lookup = (%q, %v, %v), want ('', false, nil)", iconID, found, err)
	}
}

func TestCleanupHistoryKeepsBoundaryDayRowsPerTimestampFormat(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	defer service.Stop()
	db, err := service.open(t.Context())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	days := 30
	boundary := time.Now().AddDate(0, 0, -days).UTC()
	keptAt := boundary.Add(2 * time.Hour)
	deletedAt := boundary.Add(-2 * time.Hour)

	// github_events 等五表时间戳是 CURRENT_TIMESTAMP 空格格式
	for _, item := range []struct {
		id   int
		when time.Time
	}{
		{1, keptAt},
		{2, deletedAt},
	} {
		if _, err := db.Exec(`INSERT INTO github_events (repository_id, event_type, title, created_at)
			VALUES (1, 'issue_opened', '边界事件', ?)`, item.when.Format("2006-01-02 15:04:05")); err != nil {
			t.Fatalf("insert event %d: %v", item.id, err)
		}
	}
	// github_action_runs.created_at 存 GitHub API 原始 RFC3339「T」格式
	for _, item := range []struct {
		runID int64
		when  time.Time
	}{
		{101, keptAt},
		{102, deletedAt},
	} {
		if _, err := db.Exec(`INSERT INTO github_action_runs (repository_id, run_id, workflow_name, created_at)
			VALUES (1, ?, 'CI', ?)`, item.runID, item.when.Format(time.RFC3339)); err != nil {
			t.Fatalf("insert run %d: %v", item.runID, err)
		}
	}

	if _, err := cleanupHistory(ctx, db, 0, days); err != nil {
		t.Fatalf("cleanup history: %v", err)
	}

	var keptEvents, keptRuns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM github_events WHERE title = '边界事件'`).Scan(&keptEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM github_action_runs`).Scan(&keptRuns); err != nil {
		t.Fatal(err)
	}
	// 保留期首日当天（cutoff 之后）的数据必须保留，只删除 cutoff 之前的行
	if keptEvents != 1 || keptRuns != 1 {
		t.Fatalf("keptEvents=%d keptRuns=%d, want 1/1", keptEvents, keptRuns)
	}
}
