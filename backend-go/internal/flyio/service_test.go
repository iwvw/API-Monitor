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
				details := objectSlice(payload["details"])
				if len(details) != 1 || details[0]["targetImage"] != "registry.example/app:latest" || details[0]["digestChanged"] != true {
					t.Fatalf("unexpected update details: %#v", payload)
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
		{"machine detail", "/api/flyio/apps/app-smoke/machines/machine-1?accountId=" + id, "data"},
		{"machine wait", "/api/flyio/apps/app-smoke/machines/machine-1/wait?accountId=" + id + "&state=started&timeout=5", "data"},
		{"machine lease", "/api/flyio/apps/app-smoke/machines/machine-1/lease?accountId=" + id, "data"},
		{"machine metadata", "/api/flyio/apps/app-smoke/machines/machine-1/metadata?accountId=" + id, "data"},
		{"machine events", "/api/flyio/apps/app-smoke/machines/machine-1/events?accountId=" + id, "data"},
		{"machine memory", "/api/flyio/apps/app-smoke/machines/machine-1/memory?accountId=" + id, "data"},
		{"machine ps", "/api/flyio/apps/app-smoke/machines/machine-1/ps?accountId=" + id, "data"},
		{"machine versions", "/api/flyio/apps/app-smoke/machines/machine-1/versions?accountId=" + id, "data"},
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

	actionChecks := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"machine start", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/start", `{"accountId":"` + id + `"}`},
		{"machine stop", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/stop", `{"accountId":"` + id + `","signal":"SIGTERM","timeout":"30s"}`},
		{"machine suspend", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/suspend", `{"accountId":"` + id + `"}`},
		{"machine cordon", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/cordon", `{"accountId":"` + id + `"}`},
		{"machine uncordon", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/uncordon", `{"accountId":"` + id + `"}`},
		{"machine lease acquire", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/lease", `{"accountId":"` + id + `","ttl":30}`},
		{"machine lease release", http.MethodDelete, "/api/flyio/apps/app-smoke/machines/machine-1/lease", `{"accountId":"` + id + `","nonce":"lease-1"}`},
		{"machine metadata set", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/metadata/api-monitor", `{"accountId":"` + id + `","value":"enabled"}`},
		{"machine metadata delete", http.MethodDelete, "/api/flyio/apps/app-smoke/machines/machine-1/metadata/api-monitor", `{"accountId":"` + id + `"}`},
		{"machine restart", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/restart", `{"accountId":"` + id + `"}`},
		{"machine signal", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/signal", `{"accountId":"` + id + `","signal":"SIGTERM"}`},
		{"machine reclaim memory", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/memory", `{"accountId":"` + id + `"}`},
		{"machine reclaim memory official", http.MethodPost, "/api/flyio/apps/app-smoke/machines/machine-1/memory/reclaim", `{"accountId":"` + id + `"}`},
		{"machine set memory", http.MethodPut, "/api/flyio/apps/app-smoke/machines/machine-1/memory", `{"accountId":"` + id + `","memory_mb":512}`},
		{"machine set metadata", http.MethodPut, "/api/flyio/apps/app-smoke/machines/machine-1/metadata", `{"accountId":"` + id + `","metadata":{"api-monitor":"enabled"}}`},
		{"machine create", http.MethodPost, "/api/flyio/apps/app-smoke/machines", `{"accountId":"` + id + `","image":"registry.example/app:latest","region":"sin"}`},
		{"machine delete", http.MethodDelete, "/api/flyio/apps/app-smoke/machines/machine-1", `{"accountId":"` + id + `","force":true}`},
	}
	for _, check := range actionChecks {
		t.Run(check.name, func(t *testing.T) {
			res := perform(service, check.method, check.path, check.body)
			if res.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", check.name, res.Code, res.Body.String())
			}
			payload := map[string]interface{}{}
			mustDecode(t, res, &payload)
			if payload["success"] != true {
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

func TestWithImageTagPreservesRepository(t *testing.T) {
	tests := map[string]string{
		"registry.example/app:old":          "registry.example/app:latest",
		"registry.example:5000/ns/app:main": "registry.example:5000/ns/app:latest",
		"library/nginx":                     "library/nginx:latest",
		"ghcr.io/acme/app@sha256:abcdef":    "ghcr.io/acme/app:latest",
	}
	for input, want := range tests {
		if got := withImageTag(input, "latest"); got != want {
			t.Fatalf("withImageTag(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCreateAppBlankNameAutoGenerates(t *testing.T) {
	var captured string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "viewer") {
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
				"viewer":        map[string]interface{}{"email": "fly@example.com"},
				"organizations": map[string]interface{}{"nodes": []map[string]interface{}{{"id": "org-1", "slug": "org", "name": "Org"}}},
			}})
			return
		}
		if !strings.Contains(payload.Query, "createApp") {
			t.Fatalf("unexpected query: %s", payload.Query)
		}
		input := objectValue(payload.Variables["input"])
		captured = stringValue(objectValue(input)["name"], "")
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{
			"createApp": map[string]interface{}{"app": map[string]interface{}{"id": "app-id", "name": captured, "status": "deployed", "hostname": captured + ".fly.dev"}},
		}})
	}))
	t.Cleanup(fake.Close)

	t.Setenv("FLY_GRAPHQL_URL", fake.URL+"/graphql")
	t.Setenv("FLY_MACHINES_URL", fake.URL+"/machines")
	t.Setenv("FLY_LOGS_URL", fake.URL+"/logsapi")
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	res := perform(service, http.MethodPost, "/api/flyio/accounts", `{"name":"prod","api_token":"token-1"}`)
	var created map[string]interface{}
	mustDecode(t, res, &created)
	id := stringValue(objectAt(created, "data")["id"], "")

	res = perform(service, http.MethodPost, "/api/flyio/apps", `{"accountId":"`+id+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create app status = %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	mustDecode(t, res, &payload)
	if payload["success"] != true {
		t.Fatalf("create app success = %#v", payload)
	}
	if captured == "" || !strings.HasPrefix(captured, "app-") {
		t.Fatalf("expected auto-generated app name, got %q", captured)
	}
}

func TestNormalizeFlyImageForUpdateStripsDockerHubMirror(t *testing.T) {
	tests := map[string]string{
		"docker-hub-mirror.fly.io/iwvw/api-monitor:dev": "iwvw/api-monitor:dev",
		"docker-hub-mirror.fly.io/library/nginx:latest": "library/nginx:latest",
		"ghcr.io/acme/app:main":                         "ghcr.io/acme/app:main",
	}
	for input, want := range tests {
		if got := normalizeFlyImageForUpdate(input); got != want {
			t.Fatalf("normalizeFlyImageForUpdate(%q) = %q, want %q", input, got, want)
		}
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
				{"id": "machine-1", "region": "sin", "state": "started", "version": "1", "config": map[string]interface{}{"image": "registry.example/app:old"}, "image_ref": map[string]interface{}{"digest": "sha256:old"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if objectValue(payload["config"])["image"] != "registry.example/app:latest" || payload["region"] != "sin" {
				t.Fatalf("unexpected create machine payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"id": "machine-new", "region": "sin", "state": "created"})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1":
			writeJSON(w, http.StatusOK, map[string]interface{}{"id": "machine-1", "region": "sin", "state": "started", "version": "2", "config": map[string]interface{}{"image": "registry.example/app:latest"}, "image_ref": map[string]interface{}{"digest": "sha256:new"}})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/wait":
			if r.URL.Query().Get("state") != "started" || r.URL.Query().Get("timeout") != "5" {
				t.Fatalf("unexpected wait query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/restart":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/events":
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"type": "start"}})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/memory":
			writeJSON(w, http.StatusOK, map[string]interface{}{"memory_mb": 512})
		case r.Method == http.MethodPut && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/memory":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/memory/reclaim":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/ps":
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"pid": 1, "command": "app"}})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/versions":
			writeJSON(w, http.StatusOK, []map[string]interface{}{{"version": 1}})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/signal":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["signal"] != "SIGTERM" {
				t.Fatalf("unexpected signal payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/start":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/stop":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["signal"] != "SIGTERM" || payload["timeout"] != "30s" {
				t.Fatalf("unexpected stop payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/suspend":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/cordon":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/uncordon":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/lease":
			writeJSON(w, http.StatusOK, map[string]interface{}{"nonce": "lease-1", "expires_at": "2026-01-01T00:00:15Z"})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/lease":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["ttl"] != float64(30) {
				t.Fatalf("unexpected lease payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"nonce": "lease-1"})
		case r.Method == http.MethodDelete && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/lease":
			if r.Header.Get("fly-machine-lease-nonce") != "lease-1" {
				t.Fatalf("missing lease nonce header: %#v", r.Header)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/metadata":
			writeJSON(w, http.StatusOK, map[string]interface{}{"api-monitor": "enabled"})
		case (r.Method == http.MethodPut || r.Method == http.MethodPatch) && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/metadata":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if objectValue(payload["metadata"])["api-monitor"] != "enabled" {
				t.Fatalf("unexpected full metadata payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/metadata/api-monitor":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["value"] != "enabled" {
				t.Fatalf("unexpected metadata payload: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1/metadata/api-monitor":
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1":
			if r.URL.Query().Get("force") != "true" {
				t.Fatalf("expected force delete query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/machines/apps/app-smoke/machines/machine-1":
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			config := objectValue(payload["config"])
			if config["image"] != "registry.example/app:latest" {
				t.Fatalf("unexpected machine update image: %#v", payload)
			}
			if stringValue(objectValue(config["metadata"])["api-monitor-update"], "") == "" {
				t.Fatalf("missing update marker: %#v", payload)
			}
			if payload["skip_launch"] != false {
				t.Fatalf("expected skip_launch=false: %#v", payload)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"id": "machine-1", "region": "sin", "state": "starting", "version": "2", "config": map[string]interface{}{"image": "registry.example/app:latest"}, "image_ref": map[string]interface{}{"digest": "sha256:new"}})
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
