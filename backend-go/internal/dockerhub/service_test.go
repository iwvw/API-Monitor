package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T, fake *httptest.Server) *Service {
	t.Helper()
	t.Setenv("DOCKERHUB_API_URL", fake.URL)
	return New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
}

func perform(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
}

func fakeDockerHubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/search/repositories/"):
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"count":   1,
				"results": []map[string]interface{}{{"name": "nginx", "namespace": "library", "is_official": true, "star_count": 100, "pull_count": 5000, "short_description": "Nginx web server"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/dockeruser/":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"detail": "invalid token"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"count":   2,
				"results": []map[string]interface{}{
					{"namespace": "dockeruser", "name": "webapp", "description": "My webapp", "star_count": 5, "pull_count": 100, "is_private": true, "last_updated": "2026-01-01T00:00:00Z"},
					{"namespace": "dockeruser", "name": "worker", "description": "Background worker", "star_count": 2, "pull_count": 40, "is_private": false, "last_updated": "2026-01-01T00:00:00Z"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/library/nginx/":
			writeJSON(w, http.StatusOK, map[string]interface{}{"name": "nginx", "namespace": "library", "description": "Official nginx", "pull_count": 5000, "star_count": 100})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/library/nginx/tags/":
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"results": []map[string]interface{}{
					{"name": "latest", "digest": "sha256:abc", "last_updated": "2026-01-01T00:00:00Z", "tag_last_pushed": "2026-01-01T00:00:00Z"},
				},
			})
		default:
			t.Fatalf("unexpected fake Docker Hub request %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func TestDockerHubAccountLifecycle(t *testing.T) {
	fake := fakeDockerHubAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodPost, "/api/dockerhub/accounts", `{"username":"dockeruser","token":"tok-secret"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var created struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &created)
	if !created.Success {
		t.Fatalf("create account failed: %s", res.Body.String())
	}
	if created.Data["username"] != "dockeruser" {
		t.Fatalf("unexpected username: %#v", created.Data)
	}
	if _, leaked := created.Data["token_encrypted"]; leaked {
		t.Fatalf("token leaked in create response: %#v", created.Data)
	}
	accountID := stringValue(created.Data["id"], "")

	res = perform(service, http.MethodGet, "/api/dockerhub/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var list struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &list)
	if len(list.Data) != 1 {
		t.Fatalf("expected 1 account, got %d", len(list.Data))
	}
	if _, leaked := list.Data[0]["token_encrypted"]; leaked {
		t.Fatalf("token leaked in list response: %#v", list.Data[0])
	}

	res = perform(service, http.MethodPost, "/api/dockerhub/accounts/"+accountID+"/verify", "")
	if res.Code != http.StatusOK {
		t.Fatalf("verify account status=%d body=%s", res.Code, res.Body.String())
	}
	var verify struct {
		Success bool `json:"success"`
		Valid   bool `json:"valid"`
	}
	mustDecode(t, res, &verify)
	if !verify.Success || !verify.Valid {
		t.Fatalf("verify account not valid: %s", res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/dockerhub/accounts/"+accountID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/dockerhub/accounts", "")
	mustDecode(t, res, &list)
	if len(list.Data) != 0 {
		t.Fatalf("expected 0 accounts after delete, got %d", len(list.Data))
	}
}

func TestDockerHubAccountRepositories(t *testing.T) {
	fake := fakeDockerHubAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodPost, "/api/dockerhub/accounts", `{"username":"dockeruser","token":"tok-secret"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var created struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &created)
	if !created.Success {
		t.Fatalf("create account failed: %s", res.Body.String())
	}
	accountID := stringValue(created.Data["id"], "")

	res = perform(service, http.MethodGet, "/api/dockerhub/accounts/"+accountID+"/repositories", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list account repositories status=%d body=%s", res.Code, res.Body.String())
	}
	var result struct {
		Success bool                     `json:"success"`
		Count   int                      `json:"count"`
		Data    []map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &result)
	if !result.Success || result.Count != 2 || len(result.Data) != 2 {
		t.Fatalf("unexpected account repositories: %s", res.Body.String())
	}
	names := map[string]bool{}
	for _, repo := range result.Data {
		names[stringValue(repo["name"], "")] = boolValue(repo["is_private"])
	}
	if names["webapp"] != true || names["worker"] != false {
		t.Fatalf("unexpected repo private flags: %#v", names)
	}
}

func TestDockerHubSearch(t *testing.T) {
	fake := fakeDockerHubAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodGet, "/api/dockerhub/search?query=nginx", "")
	if res.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", res.Code, res.Body.String())
	}
	var result struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
		Count   int                      `json:"count"`
	}
	mustDecode(t, res, &result)
	if !result.Success || len(result.Data) != 1 || result.Data[0]["name"] != "nginx" {
		t.Fatalf("unexpected search result: %s", res.Body.String())
	}
	if result.Count != 1 {
		t.Fatalf("expected 1 official repo, got %d", result.Count)
	}

	res = perform(service, http.MethodGet, "/api/dockerhub/search?query=", "")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("empty query should be bad request, got %d", res.Code)
	}
}

func TestDockerHubRepositoryBrowse(t *testing.T) {
	fake := fakeDockerHubAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodGet, "/api/dockerhub/repositories/library/nginx", "")
	if res.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", res.Code, res.Body.String())
	}
	var detail struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &detail)
	if detail.Data["name"] != "nginx" {
		t.Fatalf("unexpected detail: %s", res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/dockerhub/repositories/library/nginx/tags", "")
	if res.Code != http.StatusOK {
		t.Fatalf("tags status=%d body=%s", res.Code, res.Body.String())
	}
	var tags struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &tags)
	if len(tags.Data) != 1 || tags.Data[0]["name"] != "latest" {
		t.Fatalf("unexpected tags: %s", res.Body.String())
	}
}
