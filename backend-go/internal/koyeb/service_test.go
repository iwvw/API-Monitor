package koyeb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestAccountLifecycleAndDataAggregation(t *testing.T) {
	fake := fakeKoyebAPI(t)
	defer fake.Close()
	t.Setenv("KOYEB_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/koyeb/accounts", `{"name":"prod","token":" token-1\n"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	account := objectValue(createPayload["account"])
	id := int64Value(account["id"], 0)
	if id == 0 || account["email"] != "ops@example.com" || account["status"] != "active" {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var listPayload struct {
		Success  bool                     `json:"success"`
		Accounts []map[string]interface{} `json:"accounts"`
	}
	mustDecode(t, res, &listPayload)
	if !listPayload.Success || len(listPayload.Accounts) != 1 || listPayload.Accounts[0]["token"] != nil {
		t.Fatalf("unexpected account list: %#v", listPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/accounts/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var exportPayload struct {
		Success  bool                     `json:"success"`
		Accounts []map[string]interface{} `json:"accounts"`
	}
	mustDecode(t, res, &exportPayload)
	if !exportPayload.Success || exportPayload.Accounts[0]["token"] != "token-1" {
		t.Fatalf("unexpected account export: %#v", exportPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/data", "")
	if res.Code != http.StatusOK {
		t.Fatalf("data status=%d body=%s", res.Code, res.Body.String())
	}
	var dataPayload struct {
		Success  bool `json:"success"`
		Accounts []struct {
			Name     string                   `json:"name"`
			Projects []map[string]interface{} `json:"projects"`
			Error    *string                  `json:"error"`
		} `json:"accounts"`
	}
	mustDecode(t, res, &dataPayload)
	if !dataPayload.Success || len(dataPayload.Accounts) != 1 || len(dataPayload.Accounts[0].Projects) != 1 {
		t.Fatalf("unexpected data payload: %#v", dataPayload)
	}
	services := objectSlice(dataPayload.Accounts[0].Projects[0]["services"])
	if len(services) != 1 || services[0]["status"] != "RUNNING" {
		t.Fatalf("unexpected project services: %#v", dataPayload.Accounts[0].Projects)
	}

	res = perform(service, http.MethodPost, "/api/koyeb/accounts/"+stringValue(id, "")+"/refresh", "")
	if res.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestServiceOperationsAndTelemetry(t *testing.T) {
	fake := fakeKoyebAPI(t)
	defer fake.Close()
	t.Setenv("KOYEB_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/koyeb/accounts", `{"name":"prod","token":"token-1"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := int64Value(objectValue(createPayload["account"])["id"], 0)
	body := `{"accountId":` + stringValue(id, "") + `}`

	for _, endpoint := range []string{
		"/api/koyeb/services/svc-1/pause",
		"/api/koyeb/services/svc-1/restart",
		"/api/koyeb/services/svc-1/redeploy",
	} {
		res = perform(service, http.MethodPost, endpoint, body)
		if res.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, res.Code, res.Body.String())
		}
	}

	res = perform(service, http.MethodPost, "/api/koyeb/apps/app-1/rename", `{"accountId":`+stringValue(id, "")+`,"name":"next-app"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("rename app status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/services/svc-1/rename", `{"accountId":`+stringValue(id, "")+`,"name":"next-service"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("rename service status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/logs?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", res.Code, res.Body.String())
	}
	var logsPayload map[string]interface{}
	mustDecode(t, res, &logsPayload)
	if len(arrayValue(logsPayload["logs"])) != 1 {
		t.Fatalf("unexpected logs payload: %#v", logsPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/instances?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("instances status=%d body=%s", res.Code, res.Body.String())
	}
	var instancesPayload map[string]interface{}
	mustDecode(t, res, &instancesPayload)
	if len(arrayValue(instancesPayload["instances"])) != 1 {
		t.Fatalf("unexpected instances payload: %#v", instancesPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/metrics?accountId="+stringValue(id, "")+"&name=CPU_TOTAL_PERCENT", "")
	if res.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", res.Code, res.Body.String())
	}
	var metricsPayload map[string]interface{}
	mustDecode(t, res, &metricsPayload)
	if len(arrayValue(metricsPayload["metrics"])) != 1 {
		t.Fatalf("unexpected metrics payload: %#v", metricsPayload)
	}

	res = perform(service, http.MethodGet, "/api/koyeb/usage?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/koyeb/services/svc-1", body)
	if res.Code != http.StatusOK {
		t.Fatalf("delete service status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodDelete, "/api/koyeb/apps/app-1", body)
	if res.Code != http.StatusOK {
		t.Fatalf("delete app status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodDelete, "/api/koyeb/accounts/"+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestServiceConfigAndResourceManagement(t *testing.T) {
	fake := fakeKoyebAPI(t)
	defer fake.Close()
	t.Setenv("KOYEB_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/koyeb/accounts", `{"name":"prod","token":"token-1"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := int64Value(objectValue(createPayload["account"])["id"], 0)
	accBody := `{"accountId":` + stringValue(id, "") + `}`

	res = perform(service, http.MethodPost, "/api/koyeb/services/svc-1/update", `{"accountId":`+stringValue(id, "")+`,"image":"registry/app:2.0","env":[{"key":"A","value":"1"}],"instanceType":"medium"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update service status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/deployments?accountId="+stringValue(id, "")+"&limit=5", "")
	if res.Code != http.StatusOK {
		t.Fatalf("deployments status=%d body=%s", res.Code, res.Body.String())
	}
	var depPayload map[string]interface{}
	mustDecode(t, res, &depPayload)
	if len(arrayValue(depPayload["deployments"])) != 1 {
		t.Fatalf("unexpected deployments payload: %#v", depPayload)
	}

	res = perform(service, http.MethodPost, "/api/koyeb/deployments/dep-1/cancel", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("cancel deployment status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/scale?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("get scale status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPut, "/api/koyeb/services/svc-1/scale", `{"accountId":`+stringValue(id, "")+`,"scalings":[{"scopes":["web"],"instances":2}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("put scale status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodDelete, "/api/koyeb/services/svc-1/scale", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("delete scale status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodPost, "/api/koyeb/apps/app-1/pause", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("pause app status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/apps/app-1/resume", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("resume app status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/domains?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("list domains status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/domains", `{"accountId":`+stringValue(id, "")+`,"name":"app.example.com","appId":"app-1"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create domain status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/domains/dom-1/refresh", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("refresh domain status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodDelete, "/api/koyeb/domains/dom-1", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("delete domain status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/secrets?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("list secrets status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/secrets", `{"accountId":`+stringValue(id, "")+`,"name":"TOKEN","value":"abc"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create secret status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/koyeb/secrets/sec-1/update", `{"accountId":`+stringValue(id, "")+`,"value":"xyz"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update secret status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodDelete, "/api/koyeb/secrets/sec-1", accBody)
	if res.Code != http.StatusOK {
		t.Fatalf("delete secret status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodPost, "/api/koyeb/services", `{"accountId":`+stringValue(id, "")+`,"appId":"app-1","name":"worker","type":"worker","image":"registry/app:1.0","instanceType":"nano","env":["A=1"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create service status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/catalog/instances?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("catalog instances status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/koyeb/catalog/regions?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("catalog regions status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/koyeb/usage/details?accountId="+stringValue(id, ""), "")
	if res.Code != http.StatusOK {
		t.Fatalf("usage details status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestServiceLogsTail(t *testing.T) {
	fake := fakeKoyebAPI(t)
	defer fake.Close()
	t.Setenv("KOYEB_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/koyeb/accounts", `{"name":"prod","token":"token-1"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := int64Value(objectValue(createPayload["account"])["id"], 0)

	res = perform(service, http.MethodGet, "/api/koyeb/services/svc-1/logs/tail?accountId="+stringValue(id, "")+"&stream=stdout", "")
	if res.Code != http.StatusOK {
		t.Fatalf("logs tail status=%d body=%s", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("logs tail content-type = %q", ct)
	}
	body := res.Body.String()
	if !strings.Contains(body, "tail-msg") || !strings.Contains(body, "tail-msg-2") {
		t.Fatalf("logs tail body missing streamed events: %s", body)
	}
}

func testService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

func perform(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func fakeKoyebAPI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			http.Error(w, `{"message":"bad token"}`, http.StatusUnauthorized)
			return
		}
		path := r.URL.Path
		query := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && path == "/v1/apps":
			writeJSON(w, map[string]interface{}{"apps": []map[string]interface{}{{"id": "app-1", "name": "api", "created_at": "2026-01-01T00:00:00Z"}}})
		case r.Method == http.MethodGet && path == "/v1/account/profile":
			writeJSON(w, map[string]interface{}{"user": map[string]interface{}{"id": "user-1", "name": "Ops", "email": "ops@example.com"}})
		case r.Method == http.MethodGet && path == "/v1/organizations":
			writeJSON(w, map[string]interface{}{"organizations": []map[string]interface{}{{"id": "org-1", "name": "Org", "email": "org@example.com", "remaining_credits": 12.5}}})
		case r.Method == http.MethodGet && path == "/v1/services" && query.Get("app_id") == "app-1":
			writeJSON(w, map[string]interface{}{"services": []map[string]interface{}{{"id": "svc-1", "name": "web", "status": "HEALTHY", "type": "web", "active_deployment": true, "definition": map[string]interface{}{"instance_types": []map[string]interface{}{{"type": "small"}}}}}})
		case r.Method == http.MethodGet && path == "/v1/deployments":
			writeJSON(w, map[string]interface{}{"deployments": []map[string]interface{}{{"id": "dep-1", "status": "healthy"}}})
		case r.Method == http.MethodGet && path == "/v1/instances":
			writeJSON(w, map[string]interface{}{"instances": []map[string]interface{}{{"id": "inst-1", "region": "fra", "status": "running"}}})
		case r.Method == http.MethodGet && path == "/v1/services/svc-1":
			writeJSON(w, map[string]interface{}{"service": map[string]interface{}{"id": "svc-1", "status": "HEALTHY", "definition": map[string]interface{}{}}})
		case r.Method == http.MethodPost && (path == "/v1/services/svc-1/pause" || path == "/v1/services/svc-1/redeploy" || path == "/v1/services/svc-1/resume"):
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPatch && (path == "/v1/apps/app-1" || path == "/v1/services/svc-1"):
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodDelete && (path == "/v1/apps/app-1" || path == "/v1/services/svc-1"):
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && path == "/v1/streams/logs/query":
			writeJSON(w, map[string]interface{}{"result": []map[string]interface{}{{"result": map[string]interface{}{"msg": "started"}}}})
		case r.Method == http.MethodGet && path == "/v1/streams/logs/tail":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"result\":{\"msg\":\"tail-msg\",\"created_at\":\"2026-01-01T00:00:00Z\"}}\n\ndata: {\"result\":{\"msg\":\"tail-msg-2\"}}\n\n"))
		case r.Method == http.MethodGet && path == "/v1/streams/metrics":
			writeJSON(w, map[string]interface{}{"metrics": []map[string]interface{}{{"ts": 1, "value": 42}}})
		case r.Method == http.MethodGet && path == "/v1/usages":
			writeJSON(w, map[string]interface{}{"total": 1})
		case r.Method == http.MethodPost && path == "/v1/deployments/dep-1/cancel":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && (path == "/v1/apps/app-1/pause" || path == "/v1/apps/app-1/resume"):
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && path == "/v1/services/svc-1/scale":
			writeJSON(w, map[string]interface{}{"scalings": []map[string]interface{}{{"scopes": []string{"web"}, "instances": 2}}})
		case r.Method == http.MethodPut && path == "/v1/services/svc-1/scale":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodDelete && path == "/v1/services/svc-1/scale":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && path == "/v1/domains":
			writeJSON(w, map[string]interface{}{"domains": []map[string]interface{}{{"id": "dom-1", "name": "app-org.koyeb.app", "status": "active"}}})
		case r.Method == http.MethodPost && path == "/v1/domains":
			writeJSON(w, map[string]interface{}{"domain": map[string]interface{}{"id": "dom-1", "name": "app.example.com", "status": "pending"}})
		case r.Method == http.MethodDelete && path == "/v1/domains/dom-1":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && path == "/v1/domains/dom-1/refresh":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodGet && path == "/v1/secrets":
			writeJSON(w, map[string]interface{}{"secrets": []map[string]interface{}{{"id": "sec-1", "name": "TOKEN", "type": "SIMPLE"}}})
		case r.Method == http.MethodPost && path == "/v1/secrets":
			writeJSON(w, map[string]interface{}{"secret": map[string]interface{}{"id": "sec-1", "name": "TOKEN"}})
		case r.Method == http.MethodDelete && path == "/v1/secrets/sec-1":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPatch && path == "/v1/secrets/sec-1":
			writeJSON(w, map[string]interface{}{"ok": true})
		case r.Method == http.MethodPost && path == "/v1/services":
			writeJSON(w, map[string]interface{}{"service": map[string]interface{}{"id": "svc-2", "name": "worker", "status": "starting"}})
		case r.Method == http.MethodGet && path == "/v1/catalog/instances":
			writeJSON(w, map[string]interface{}{"instances": []map[string]interface{}{{"id": "nano", "memory": "256MB", "price_monthly": "$1"}}})
		case r.Method == http.MethodGet && path == "/v1/catalog/regions":
			writeJSON(w, map[string]interface{}{"regions": []map[string]interface{}{{"id": "fra", "name": "Frankfurt"}}})
		case r.Method == http.MethodGet && path == "/v1/usages/details":
			writeJSON(w, map[string]interface{}{"usages": []map[string]interface{}{{"service_id": "svc-1", "duration": 60}}})
		default:
			t.Fatalf("unexpected fake Koyeb request %s %s", r.Method, r.URL.String())
		}
	}))
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %T: %v body=%s", target, err, res.Body.String())
	}
}
