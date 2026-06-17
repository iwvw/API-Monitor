package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
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

func TestServerDetailResolvesCountryFromCachedAgentMetadata(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-geo', 'geo', '127.0.0.1', 'root', 'password', '{"country_code":"hk","location":"Hong Kong","platform":"Linux"}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	res := perform(service, http.MethodGet, "/api/server/s/server-geo", "")
	if res.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	if data["resolved_country"] != "hk" || data["location"] != "Hong Kong" {
		t.Fatalf("expected country fallback from cached agent metadata, data=%#v", data)
	}
	info := data["info"].(map[string]interface{})
	if info["resolved_country"] != "hk" || info["location"] != "Hong Kong" {
		t.Fatalf("expected info country fallback, info=%#v", info)
	}
}

func TestNetworkQualityCollectAndReadback(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-1', 'edge', '127.0.0.1', 'root', 'password', '{}')`,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, err = db.ExecContext(context.Background(), `DELETE FROM server_network_quality_targets`)
	if err != nil {
		t.Fatalf("clear targets: %v", err)
	}
	_, err = db.ExecContext(
		context.Background(),
		`INSERT INTO server_network_quality_targets (id, name, host, port, type, enabled, order_index) VALUES (1, '本地', '127.0.0.1', ?, 'tcp', 1, 1)`,
		listener.Addr().(*net.TCPAddr).Port,
	)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}

	res := perform(service, http.MethodPost, "/api/server/network-quality/server-1/collect", "")
	if res.Code != http.StatusOK {
		t.Fatalf("collect status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	if toIntTest(data["sampleCount"]) < 1 {
		t.Fatalf("expected sampleCount >= 1: %#v", data)
	}
	summary := data["summary"].([]interface{})
	if len(summary) != 1 {
		t.Fatalf("summary len=%d payload=%#v", len(summary), data)
	}

	res = perform(service, http.MethodGet, "/api/server/network-quality/server-1?days=1&maxPointsPerTarget=24", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data = payload["data"].(map[string]interface{})
	if toIntTest(data["sampleCount"]) < 1 {
		t.Fatalf("expected readback sampleCount >= 1: %#v", data)
	}
	series := data["series"].([]interface{})
	if len(series) != 1 {
		t.Fatalf("series len=%d payload=%#v", len(series), data)
	}
}

func TestPersistMetricsAcceptsCachedAgentInfoShape(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('server-agent', 'agent', '', 'root', 'password')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	err = service.persistMetrics(context.Background(), db, "server-agent", map[string]interface{}{
		"platform": "Windows",
		"cpu": map[string]interface{}{
			"Usage":        "22.0%",
			"Load":         "5.28 5.28 5.28",
			"Cores":        float64(24),
			"LogicalCores": float64(24),
			"Temp":         float64(52.1),
		},
		"memory": map[string]interface{}{
			"Used":  "12684",
			"Total": "16115MB",
			"Usage": "79%",
		},
		"disk": []interface{}{
			map[string]interface{}{"used": "1.1 TB", "total": "1.4 TB", "usage": "77%"},
		},
		"docker": map[string]interface{}{
			"installed": false,
			"running":   float64(0),
			"stopped":   float64(0),
		},
		"gpu": map[string]interface{}{
			"Usage":  "23%",
			"Memory": "2232/8188MB",
			"Power":  "13.2W",
			"Temp":   float64(60),
		},
		"network": map[string]interface{}{
			"rx_speed": "29.6 KB/s",
			"tx_speed": "1.6 KB/s",
		},
	})
	if err != nil {
		t.Fatalf("persist metrics: %v", err)
	}

	var cpuUsage, memUsage, gpuUsage, netRx float64
	var memUsed, memTotal, gpuMemUsed, gpuMemTotal int
	var platform string
	err = db.QueryRowContext(context.Background(), `SELECT cpu_usage, mem_used, mem_total, mem_usage, gpu_usage, gpu_mem_used, gpu_mem_total, net_rx, platform FROM server_metrics_history WHERE server_id = 'server-agent'`).
		Scan(&cpuUsage, &memUsed, &memTotal, &memUsage, &gpuUsage, &gpuMemUsed, &gpuMemTotal, &netRx, &platform)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}

	if cpuUsage != 22 || memUsed != 12684 || memTotal != 16115 || memUsage != 79 {
		t.Fatalf("unexpected cpu/memory metrics: cpu=%v memUsed=%d memTotal=%d memUsage=%v", cpuUsage, memUsed, memTotal, memUsage)
	}
	if gpuUsage != 23 || gpuMemUsed != 2232 || gpuMemTotal != 8188 {
		t.Fatalf("unexpected gpu metrics: usage=%v used=%d total=%d", gpuUsage, gpuMemUsed, gpuMemTotal)
	}
	if netRx != 29.6*1024 || platform != "Windows" {
		t.Fatalf("unexpected network/platform metrics: netRx=%v platform=%s", netRx, platform)
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
	if !strings.HasPrefix(data["installCommand"].(string), "curl -fsSL https://") {
		t.Fatalf("install command should default to https: %#v", data["installCommand"])
	}
	if !strings.Contains(data["installCommand"].(string), serverID) {
		t.Fatalf("install command should include server id: %#v", data["installCommand"])
	}
	if !strings.Contains(data["installCommand"].(string), "/api/server/agent/install/linux/"+serverID+"/") {
		t.Fatalf("install command should use public keyed linux route: %#v", data["installCommand"])
	}
	agentKey, ok := data["agentKey"].(string)
	if !ok || agentKey == "" {
		t.Fatalf("expected agent key in quick install data: %#v", data)
	}

	res = perform(service, http.MethodGet, "/api/server/agent/install/linux/"+serverID+"/"+agentKey, "")
	if res.Code != http.StatusOK {
		t.Fatalf("keyed linux install status=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `SERVER_ID="`+serverID+`"`) {
		t.Fatalf("keyed linux install script should include server id: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "systemctl restart api-monitor-agent") {
		t.Fatalf("linux install script should restart an existing service: %s", res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/agent/install/linux/"+serverID+"/bad-key", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("bad keyed linux install status=%d body=%s", res.Code, res.Body.String())
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

	req := httptest.NewRequest(http.MethodGet, "/api/server/agent/command/"+serverID+"?protocol=http", nil)
	req.Host = "189.1.217.109:3010"
	resCmd := httptest.NewRecorder()
	service.ServeHTTP(resCmd, req)
	if resCmd.Code != http.StatusOK {
		t.Fatalf("install command status=%d body=%s", resCmd.Code, resCmd.Body.String())
	}
	cmdPayload := decodePayload(t, resCmd)
	cmdData := cmdPayload["data"].(map[string]interface{})
	if !strings.Contains(cmdData["installCommand"].(string), "http://189.1.217.109:3010/api/server/agent/install/linux/"+serverID+"/") {
		t.Fatalf("http install command mismatch: %#v", cmdData["installCommand"])
	}
	if !strings.Contains(cmdData["installCommand"].(string), "protocol=http") {
		t.Fatalf("http install command should preserve protocol query: %#v", cmdData["installCommand"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/server/agent/install/linux/"+serverID+"/"+agentKey+"?protocol=http", nil)
	req.Host = "189.1.217.109:3010"
	resLinux := httptest.NewRecorder()
	service.ServeHTTP(resLinux, req)
	if resLinux.Code != http.StatusOK {
		t.Fatalf("http keyed linux install status=%d body=%s", resLinux.Code, resLinux.Body.String())
	}
	if !strings.Contains(resLinux.Body.String(), `SERVER_URL="http://189.1.217.109:3010"`) {
		t.Fatalf("linux install script should use http server url: %s", resLinux.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/server/agent/install/win/"+serverID+"/"+agentKey+"?protocol=http", nil)
	req.Host = "189.1.217.109:3010"
	resWin := httptest.NewRecorder()
	service.ServeHTTP(resWin, req)
	if resWin.Code != http.StatusOK {
		t.Fatalf("http keyed windows install status=%d body=%s", resWin.Code, resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `$SERVER_URL = "http://189.1.217.109:3010"`) {
		t.Fatalf("windows install script should use http server url: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `$AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.exe"`) {
		t.Fatalf("windows install script should use a valid agent path: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `$TEMP_AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.exe.download"`) {
		t.Fatalf("windows install script should use a temp download path: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `Move-Item -Path $TEMP_AGENT_PATH -Destination $AGENT_PATH -Force`) {
		t.Fatalf("windows install script should atomically replace the agent binary: %s", resWin.Body.String())
	}
	if strings.ContainsRune(resWin.Body.String(), '\a') {
		t.Fatalf("windows install script should not contain bell characters: %q", resWin.Body.String())
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

func TestBuildCachedInfoPreservesHardwareModelsFromHostInfo(t *testing.T) {
	service := &Service{}

	cached := service.buildCachedInfo(
		map[string]interface{}{
			"cpu": float64(42),
			"gpu": float64(30),
		},
		map[string]interface{}{
			"cpu": []interface{}{"GenuineIntel Intel(R) Core(TM) i9-14900HX"},
			"gpu": []interface{}{"NVIDIA GeForce RTX 4060 Laptop GPU"},
		},
	)

	if cached["cpu_model"] != "GenuineIntel Intel(R) Core(TM) i9-14900HX" {
		t.Fatalf("cpu_model = %#v", cached["cpu_model"])
	}
	if cached["gpu_model"] != "NVIDIA GeForce RTX 4060 Laptop GPU" {
		t.Fatalf("gpu_model = %#v", cached["gpu_model"])
	}
}

func TestBuildInfoFieldExtractsHardwareModelsFromHostInfoArrays(t *testing.T) {
	service := &Service{}

	info := service.buildInfoField(map[string]interface{}{
		"cpu":           []interface{}{"GenuineIntel Intel(R) Core(TM) i9-14900HX"},
		"gpu":           []interface{}{"NVIDIA GeForce RTX 4060 Laptop GPU"},
		"agent_version": "0.1.6",
	})

	cpu := info["cpu"].(map[string]interface{})
	if cpu["Model"] != "GenuineIntel Intel(R) Core(TM) i9-14900HX" {
		t.Fatalf("cpu model = %#v", cpu["Model"])
	}

	gpus := info["gpu"].([]map[string]interface{})
	if len(gpus) != 0 {
		t.Fatalf("gpu trend payload count = %d", len(gpus))
	}
	if extractGPUModel(map[string]interface{}{"gpu": []interface{}{"NVIDIA GeForce RTX 4060 Laptop GPU"}}) != "NVIDIA GeForce RTX 4060 Laptop GPU" {
		t.Fatalf("extractGPUModel failed")
	}
	if info["agentVersion"] != "0.1.6" {
		t.Fatalf("agentVersion = %#v", info["agentVersion"])
	}
}

func jsonNumber(n float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(jsonMarshal(n), ".0"), ".")
}

func jsonMarshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func toIntTest(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
