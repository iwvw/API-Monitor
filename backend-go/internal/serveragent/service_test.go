package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	service := New(config.Config{
		Version: "test",
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return service, db
}

func perform(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func decodePayload(t *testing.T, res *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
	return payload
}

func TestAccountsLifecycleAndNullableUpdate(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/accounts", `{"name":"edge","host":"127.0.0.1","port":22,"username":"root","auth_type":"password","password":"secret","tags":["prod"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	id := data["id"].(string)
	if data["password"] != "secret" {
		t.Fatalf("expected decrypted password, got %#v", data["password"])
	}

	res = perform(service, http.MethodPut, "/api/server/accounts/"+id, `{"description":"updated","tags":["prod","go"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data = payload["data"].(map[string]interface{})
	if data["description"] != "updated" {
		t.Fatalf("description = %#v", data["description"])
	}

	res = perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	list := payload["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("account count = %d", len(list))
	}

	res = perform(service, http.MethodGet, "/api/server/accounts/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestCredentialsAcceptFrontendDefaultPost(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/credentials", `{"name":"root","username":"root","password":"secret"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create credential status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	id := data["id"].(float64)

	res = perform(service, http.MethodPost, "/api/server/credentials/"+jsonNumber(id)+"/default", "")
	if res.Code != http.StatusOK {
		t.Fatalf("set default status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/credentials/default", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get default status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data = payload["data"].(map[string]interface{})
	if data["username"] != "root" || data["password"] != "secret" {
		t.Fatalf("default credential = %#v", data)
	}
}

func TestSnippetsPreviewHistoryAndMonitorLogs(t *testing.T) {
	service, db := testService(t)

	res := perform(service, http.MethodPost, "/api/server/snippets", `{"title":"echo","content":"echo {host}","tags":["ops"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create snippet status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	snippet := payload["data"].(map[string]interface{})
	snippetID := snippet["id"].(float64)

	res = perform(service, http.MethodPost, "/api/server/snippets/preview", `{"snippetId":`+jsonNumber(snippetID)+`,"variables":{"host":"example.com"}}`)
	if res.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	preview := payload["data"].(map[string]interface{})
	if preview["rendered"] != "echo example.com" {
		t.Fatalf("preview = %#v", preview)
	}

	res = perform(service, http.MethodPost, "/api/server/snippets/history", `{"snippetId":`+jsonNumber(snippetID)+`,"command":"echo {host}","renderedCommand":"echo example.com"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("history add status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/snippets/history", "")
	if res.Code != http.StatusOK {
		t.Fatalf("history list status=%d body=%s", res.Code, res.Body.String())
	}

	_, err := db.ExecContext(context.Background(), `INSERT INTO server_monitor_logs (server_id, status, response_time) VALUES ('server-1', 'success', 12)`)
	if err != nil {
		t.Fatalf("insert log: %v", err)
	}
	res = perform(service, http.MethodGet, "/api/server/monitor/logs?page=1&pageSize=10", "")
	if res.Code != http.StatusOK {
		t.Fatalf("monitor logs status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	if payload["pagination"] == nil {
		t.Fatalf("expected pagination in payload: %#v", payload)
	}
}

func TestFrontendCompatibilityRoutes(t *testing.T) {
	service, db := testService(t)
	t.Log("Inserting server_accounts...")
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-1', 'edge', '127.0.0.1', 'root', 'password', '{"docker":{"installed":true,"running":1}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Log("Inserting server_metrics_history...")
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_metrics_history (server_id, cpu_usage, mem_total, mem_usage, recorded_at) VALUES ('server-1', 12.5, 1024, 50, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/server/monitor/status", ""},
		{http.MethodPost, "/api/server/monitor/collect", ""},
		{http.MethodGet, "/api/server/metrics/history?serverId=server-1&page=1&pageSize=10", ""},
		{http.MethodDelete, "/api/server/metrics/history?serverId=server-1", ""},
		{http.MethodGet, "/api/server/v2/tasks", ""},
		{http.MethodPost, "/api/server/v2/tasks", `{"serverId":"server-1","domain":"docker","action":"container.list"}`},
		{http.MethodGet, "/api/server/v2/docker/overview", ""},
		{http.MethodPost, "/api/server/docker/check-update", `{"serverId":"server-1"}`},
		{http.MethodGet, "/api/server/agent/connection-info/server-1", ""},
	} {
		t.Logf("Performing %s %s...", tc.method, tc.path)
		res := perform(service, tc.method, tc.path, tc.body)
		t.Logf("Completed %s %s with status %d", tc.method, tc.path, res.Code)
		if res.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, res.Code, res.Body.String())
		}
		payload := decodePayload(t, res)
		if payload["success"] != true {
			t.Fatalf("%s %s payload=%#v", tc.method, tc.path, payload)
		}
	}
}

func TestAgentQuickInstallCreatesHostFromName(t *testing.T) {
	service, db := testService(t)

	res := perform(service, http.MethodPost, "/api/server/agent/quick-install", `{"name":"edge-agent"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quick install status=%d body=%s", res.Code, res.Body.String())
	}

	payload := decodePayload(t, res)
	if payload["success"] != true {
		t.Fatalf("quick install payload=%#v", payload)
	}
	data := payload["data"].(map[string]interface{})
	serverID, ok := data["serverId"].(string)
	if !ok || serverID == "" {
		t.Fatalf("expected serverId in quick install data: %#v", data)
	}
	if data["isNew"] != true {
		t.Fatalf("expected newly created host, data=%#v", data)
	}
	if !strings.Contains(data["installCommand"].(string), serverID) {
		t.Fatalf("install command should include server id: %#v", data["installCommand"])
	}

	var name, host, username, monitorMode string
	var port int
	err := db.QueryRowContext(context.Background(), `SELECT name, host, port, username, monitor_mode FROM server_accounts WHERE id = ?`, serverID).
		Scan(&name, &host, &port, &username, &monitorMode)
	if err != nil {
		t.Fatalf("lookup created host: %v", err)
	}
	if name != "edge-agent" || host != "0.0.0.0" || port != 22 || username != "agent" || monitorMode != "agent" {
		t.Fatalf("created host = name:%q host:%q port:%d username:%q monitor_mode:%q", name, host, port, username, monitorMode)
	}

	res = perform(service, http.MethodPost, "/api/server/agent/quick-install", `{"name":"edge-agent"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quick install reuse status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data = payload["data"].(map[string]interface{})
	if data["serverId"] != serverID || data["isNew"] != false {
		t.Fatalf("expected existing host reuse, data=%#v", data)
	}
}

func TestSFTPRequiresValidServerConfig(t *testing.T) {
	service, _ := testService(t)
	res := perform(service, http.MethodPost, "/api/server/sftp/list", `{"serverId":"missing","path":"."}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing server status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	if payload["success"] != false || payload["code"] != "SERVER_NOT_FOUND" {
		t.Fatalf("unexpected sftp error payload=%#v", payload)
	}
}

func TestBuildCachedInfoKeepsFreshStateOverStaleMetadata(t *testing.T) {
	service := &Service{}
	state := map[string]interface{}{
		"timestamp_ms":       float64(2000),
		"sequence":           float64(2),
		"sample_interval_ms": float64(1500),
		"cpu":                float64(42),
		"mem_used":           float64(512 * 1024 * 1024),
		"mem_total":          float64(1024 * 1024 * 1024),
		"disk_used":          float64(10 * 1024 * 1024 * 1024),
		"disk_total":         float64(20 * 1024 * 1024 * 1024),
	}
	hostInfo := map[string]interface{}{
		"timestamp_ms":       float64(1000),
		"sequence":           float64(1),
		"sample_interval_ms": float64(9999),
		"cpu":                float64(5),
		"platform":           "Windows",
	}

	cached := service.buildCachedInfo(state, hostInfo)
	if cached["timestamp_ms"] != state["timestamp_ms"] {
		t.Fatalf("timestamp_ms = %#v, want fresh state %#v", cached["timestamp_ms"], state["timestamp_ms"])
	}
	if cached["sequence"] != state["sequence"] {
		t.Fatalf("sequence = %#v, want fresh state %#v", cached["sequence"], state["sequence"])
	}
	if cached["sample_interval_ms"] != state["sample_interval_ms"] {
		t.Fatalf("sample_interval_ms = %#v, want fresh state %#v", cached["sample_interval_ms"], state["sample_interval_ms"])
	}
	if cached["cpu"] != state["cpu"] {
		t.Fatalf("cpu = %#v, want fresh state %#v", cached["cpu"], state["cpu"])
	}
	if cached["platform"] != "Windows" {
		t.Fatalf("platform = %#v", cached["platform"])
	}
}

func jsonNumber(n float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(jsonMarshal(n), ".0"), ".")
}

func jsonMarshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
