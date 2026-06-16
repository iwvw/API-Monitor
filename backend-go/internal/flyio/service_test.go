package flyio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestAccountLifecycleAndProxyApps(t *testing.T) {
	fake := fakeFlyAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodPost, "/api/flyio/accounts", `{"name":"prod","api_token":" token-1\n"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status = %d body=%s", res.Code, res.Body.String())
	}
	var created map[string]interface{}
	mustDecode(t, res, &created)
	account := objectAt(created, "data")
	id := stringValue(account["id"], "")
	if id == "" || account["email"] != "fly@example.com" {
		t.Fatalf("unexpected created account: %#v", created)
	}

	res = perform(service, http.MethodGet, "/api/flyio/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var listed struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &listed)
	if !listed.Success || len(listed.Data) != 1 {
		t.Fatalf("unexpected list payload: %#v", listed)
	}
	if _, ok := listed.Data[0]["api_token"]; ok {
		t.Fatalf("safe account leaked api_token: %#v", listed.Data[0])
	}

	res = perform(service, http.MethodGet, "/api/flyio/accounts/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export accounts status = %d body=%s", res.Code, res.Body.String())
	}
	var exported struct {
		Success  bool                     `json:"success"`
		Accounts []map[string]interface{} `json:"accounts"`
	}
	mustDecode(t, res, &exported)
	if !exported.Success || exported.Accounts[0]["api_token"] != "token-1" {
		t.Fatalf("unexpected export payload: %#v", exported)
	}

	res = perform(service, http.MethodGet, "/api/flyio/proxy/apps", "")
	if res.Code != http.StatusOK {
		t.Fatalf("proxy apps status = %d body=%s", res.Code, res.Body.String())
	}
	var proxy struct {
		Success bool `json:"success"`
		Data    []struct {
			AccountID string                   `json:"accountId"`
			Apps      []map[string]interface{} `json:"apps"`
			Error     interface{}              `json:"error"`
		} `json:"data"`
	}
	mustDecode(t, res, &proxy)
	if !proxy.Success || len(proxy.Data) != 1 || proxy.Data[0].AccountID != id || proxy.Data[0].Apps[0]["name"] != "app-smoke" {
		t.Fatalf("unexpected proxy payload: %#v", proxy)
	}

	res = perform(service, http.MethodDelete, "/api/flyio/accounts/"+id, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestAppMachineLogsAndBatchOperations(t *testing.T) {
	fake := fakeFlyAPI(t)
	service := newTestService(t, fake)

	res := perform(service, http.MethodPost, "/api/flyio/accounts", `{"name":"prod","api_token":"token-1"}`)
	var created map[string]interface{}
	mustDecode(t, res, &created)
	id := stringValue(objectAt(created, "data")["id"], "")
	if id == "" {
		t.Fatalf("missing account id: %#v", created)
	}

	steps := []struct {
		name   string
		method string
		path   string
		body   string
		check  func(*testing.T, map[string]interface{})
	}{
		{
			name:   "create app",
			method: http.MethodPost,
			path:   "/api/flyio/apps",
			body:   `{"accountId":"` + id + `","name":"app-smoke"}`,
			check: func(t *testing.T, payload map[string]interface{}) {
				if objectAt(payload, "data")["name"] != "app-smoke" {
					t.Fatalf("unexpected create app payload: %#v", payload)
				}
			},
		},
		{
			name:   "rename app",
			method: http.MethodPost,
			path:   "/api/flyio/apps/app-smoke/rename",
			body:   `{"accountId":"` + id + `","newName":"app-renamed"}`,
		},
		{
			name:   "redeploy app",
			method: http.MethodPost,
			path:   "/api/flyio/apps/app-smoke/redeploy",
			body:   `{"accountId":"` + id + `"}`,
			check: func(t *testing.T, payload map[string]interface{}) {
				if payload["restarted"] != float64(1) || payload["failed"] != float64(0) {
					t.Fatalf("unexpected redeploy payload: %#v", payload)
				}
			},
		},
		{
			name:   "update image",
			method: http.MethodPost,
			path:   "/api/flyio/apps/app-smoke/update-image",
			body:   `{"accountId":"` + id + `","image":"latest"}`,
			check: func(t *testing.T, payload map[string]interface{}) {
				if payload["updated"] != float64(1) || payload["failed"] != float64(0) {
					t.Fatalf("unexpected update payload: %#v", payload)
				}
			},
		},
		{
			name:   "delete app",
			method: http.MethodDelete,
			path:   "/api/flyio/apps/app-smoke",
			body:   `{"accountId":"` + id + `"}`,
		},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			res := perform(service, step.method, step.path, step.body)
			if res.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", step.name, res.Code, res.Body.String())
			}
			payload := map[string]interface{}{}
			mustDecode(t, res, &payload)
			if payload["success"] != true {
				t.Fatalf("%s success = %#v", step.name, payload)
			}
			if step.check != nil {
				step.check(t, payload)
			}
		})
	}

	getChecks := []struct {
		name  string
		path  string
		field string
	}{
		{"machines", "/api/flyio/apps/app-smoke/machines?accountId=" + id, "data"},
		{"events", "/api/flyio/apps/app-smoke/events?accountId=" + id, "data"},
		{"config", "/api/flyio/apps/app-smoke/config?accountId=" + id, "data"},
		{"logs", "/api/flyio/apps/app-smoke/logs?accountId=" + id, "data"},
	}
	for _, check := range getChecks {
		t.Run(check.name, func(t *testing.T) {
			res := perform(service, http.MethodGet, check.path, "")
			if res.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", check.name, res.Code, res.Body.String())
			}
			payload := map[string]interface{}{}
			mustDecode(t, res, &payload)
			if payload["success"] != true || payload[check.field] == nil {
				t.Fatalf("%s unexpected payload: %#v", check.name, payload)
			}
		})
	}

	res = perform(service, http.MethodPost, "/api/flyio/accounts/"+id+"/update-all-images", `{"image":"latest"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update all status = %d body=%s", res.Code, res.Body.String())
	}
	var batch map[string]interface{}
	mustDecode(t, res, &batch)
	if batch["success"] != true || batch["total"] != float64(1) || batch["updated"] != float64(1) {
		t.Fatalf("unexpected batch payload: %#v", batch)
	}
}

func newTestService(t *testing.T, fake *httptest.Server) *Service {
	t.Helper()
	t.Setenv("FLY_GRAPHQL_URL", fake.URL+"/graphql")
	t.Setenv("FLY_MACHINES_URL", fake.URL+"/machines")
	t.Setenv("FLY_LOGS_URL", fake.URL+"/logsapi")
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

func fakeFlyAPI(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "bad token"})
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var payload struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			query := payload.Query
			switch {
			case strings.Contains(query, "viewer"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"viewer": map[string]interface{}{"email": "fly@example.com"},
					"organizations": map[string]interface{}{"nodes": []map[string]interface{}{
						{"id": "org-1", "slug": "org", "name": "Org"},
					}},
				}})
			case strings.Contains(query, "createApp"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"createApp": map[string]interface{}{"app": map[string]interface{}{"id": "app-id", "name": "app-smoke", "status": "deployed", "hostname": "app-smoke.fly.dev"}},
				}})
			case strings.Contains(query, "deleteApp"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"deleteApp": map[string]interface{}{"organization": map[string]interface{}{"id": "org-1"}},
				}})
			case strings.Contains(query, "updateApp"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"updateApp": map[string]interface{}{"app": map[string]interface{}{"id": "app-id", "name": "app-renamed"}},
				}})
			case strings.Contains(query, "releases"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"app": map[string]interface{}{"releases": map[string]interface{}{"nodes": []map[string]interface{}{
						{"id": "rel-1", "version": 3, "status": "succeeded", "reason": "deploy", "createdAt": "2026-01-01T00:00:00Z", "user": map[string]interface{}{"email": "fly@example.com"}},
					}}},
				}})
			case strings.Contains(query, "config"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"app": map[string]interface{}{"id": "app-id", "name": "app-smoke", "status": "deployed", "hostname": "app-smoke.fly.dev", "config": map[string]interface{}{"definition": map[string]interface{}{"services": []interface{}{}}}},
				}})
			case strings.Contains(query, "apps"):
				writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
					"apps": map[string]interface{}{"nodes": []map[string]interface{}{
						{
							"id":             "app-id",
							"name":           "app-smoke",
							"status":         "deployed",
							"deployed":       true,
							"hostname":       "app-smoke.fly.dev",
							"appUrl":         "https://app-smoke.fly.dev",
							"organization":   map[string]interface{}{"slug": "org"},
							"currentRelease": map[string]interface{}{"createdAt": "2026-01-01T00:00:00Z", "status": "succeeded"},
							"machines":       map[string]interface{}{"nodes": []map[string]interface{}{{"id": "machine-1", "region": "sin", "state": "started"}}},
						},
					}},
				}})
			default:
				t.Fatalf("unexpected GraphQL query: %s", query)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines":
			writeJSON(w, http.StatusOK, []map[string]interface{}{
				{"id": "machine-1", "region": "sin", "state": "started", "config": map[string]interface{}{"image": "registry.example/app:old"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/restart":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/logsapi/apps/app-smoke/logs":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(`{"timestamp":"2026-01-01T00:00:00Z","message":"started","level":"info","instance":"machine-1","region":"sin"}` + "\n"))
		default:
			t.Fatalf("unexpected fake Fly request %s %s", r.Method, r.URL.String())
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
