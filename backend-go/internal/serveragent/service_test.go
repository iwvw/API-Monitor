package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	subscriptionservice "github.com/iwvw/api-monitor/backend-go/internal/subscription"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
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
	t.Cleanup(func() {
		service.Stop()
		_ = db.Close()
	})
	return service, db
}

func TestDeleteAccountRequiresForceWhenAgentOfflineWithManagedDependencies(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('delete-host','待删除主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_runtimes(server_id,runtime,apply_status) VALUES('delete-host','sing-box','running')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/server/accounts/delete-host", nil)
	service.deleteAccount(rec, req, db, "delete-host")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"can_force_delete":true`) {
		t.Fatalf("force delete recovery is missing: %s", rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_accounts WHERE id='delete-host'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("host was deleted with managed resources: count=%d err=%v", count, err)
	}
}

func TestDeleteAccountForceCascadesPanelRecordsAndStatusPageMembership(t *testing.T) {
	service, db := testService(t)
	if err := subscriptionservice.New(service.cfg).Initialize(context.Background()); err != nil {
		t.Fatalf("initialize subscription schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('clean-host','可删除主机','192.0.2.11','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_runtimes(server_id,runtime,apply_status) VALUES('clean-host','sing-box','running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,apply_status) VALUES('clean-node','clean-host','测试节点','vless-reality','sing-box','192.0.2.11',45654,'tcp','{}','','running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name) VALUES('cascade-plan','级联套餐')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,name,public_token) VALUES('cascade-sub','cascade-plan','级联订阅','cascade-token')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_reports(server_id,node_id,subscription_id,credential_id,boot_id,sequence,upload_bytes,download_bytes) VALUES('clean-host','clean-node','cascade-sub','cascade-sub','boot',1,2,3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_report_keys(server_id,node_id,subscription_id,boot_id,sequence) VALUES('clean-host','clean-node','cascade-sub','boot',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_hourly(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes) VALUES('clean-host','clean-node','cascade-sub','2026-07-01T12:00:00Z',1,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_status_pages(slug,title,server_ids_json) VALUES('cascade-page','级联页','["clean-host","other-host"]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_agent_credentials(server_id,secret_encrypted) VALUES('clean-host','secret')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_proxy_traffic_reports(server_id,boot_id,sequence,node_id) VALUES('clean-host','boot',1,'node')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/server/accounts/clean-host?force=1", nil)
	service.deleteAccount(rec, req, db, "clean-host")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var accepted struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil || accepted.Data.TaskID == "" {
		t.Fatalf("decode delete task: task=%q err=%v body=%s", accepted.Data.TaskID, err, rec.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		task, ok := service.taskRegistry.Get(accepted.Data.TaskID)
		if !ok {
			t.Fatal("delete task disappeared")
		}
		snapshot := task.Snapshot()
		if snapshot.Status == TaskCompleted {
			break
		}
		if snapshot.Status == TaskFailed {
			t.Fatalf("delete task failed: %s", snapshot.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("delete task did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM server_accounts WHERE id='clean-host'`,
		`SELECT COUNT(*) FROM managed_proxy_nodes WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM managed_proxy_runtimes WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM server_agent_credentials WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM server_proxy_traffic_reports WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM subscription_usage_reports WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM subscription_usage_report_keys WHERE server_id='clean-host'`,
		`SELECT COUNT(*) FROM subscription_usage_hourly WHERE server_id='clean-host'`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("query %q count=%d err=%v", query, count, err)
		}
	}
	var statusPageServers string
	if err := db.QueryRow(`SELECT server_ids_json FROM server_status_pages WHERE slug='cascade-page'`).Scan(&statusPageServers); err != nil {
		t.Fatal(err)
	}
	if statusPageServers != `["other-host"]` {
		t.Fatalf("status page references = %s", statusPageServers)
	}
}

func TestDeleteManagedNodeRemovesRelationsAndRawTrafficHistory(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if err := subscriptionservice.New(service.cfg).Initialize(ctx); err != nil {
		t.Fatalf("initialize subscription schema: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('node-host','节点主机','192.0.2.20','agent','password')`,
		`INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,apply_status) VALUES('deleted-node','node-host','待删除节点','vless-reality','sing-box','192.0.2.20',45654,'tcp','{}','','running')`,
		`INSERT INTO subscription_plans(id,name) VALUES('node-plan','节点套餐')`,
		`INSERT INTO subscription_subscriptions(id,plan_id,name,public_token) VALUES('node-sub','node-plan','节点订阅','node-token')`,
		`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('node-plan','deleted-node','internal')`,
		`INSERT INTO subscription_runtime_reconcile(node_id,state) VALUES('deleted-node','pending')`,
		`INSERT INTO subscription_usage_reports(server_id,node_id,subscription_id,credential_id,boot_id,sequence) VALUES('node-host','deleted-node','node-sub','node-sub','boot',1)`,
		`INSERT INTO subscription_usage_report_keys(server_id,node_id,subscription_id,boot_id,sequence) VALUES('node-host','deleted-node','node-sub','boot',1)`,
		`INSERT INTO subscription_usage_hourly(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes) VALUES('node-host','deleted-node','node-sub','2026-07-01T12:00:00Z',1,2)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("fixture %q: %v", statement, err)
		}
	}
	task := service.taskRegistry.Create("node-host", "proxy.node.delete", "deleted-node")
	service.runManagedProxyNodeDelete(task.ID, "deleted-node", "node-host", "sing-box", 1, false, true, "direct")
	for _, query := range []string{
		`SELECT COUNT(*) FROM managed_proxy_nodes WHERE id='deleted-node'`,
		`SELECT COUNT(*) FROM subscription_plan_nodes WHERE node_id='deleted-node' AND source='internal'`,
		`SELECT COUNT(*) FROM subscription_runtime_reconcile WHERE node_id='deleted-node'`,
		`SELECT COUNT(*) FROM subscription_usage_reports WHERE node_id='deleted-node'`,
		`SELECT COUNT(*) FROM subscription_usage_report_keys WHERE node_id='deleted-node'`,
		`SELECT COUNT(*) FROM subscription_usage_hourly WHERE node_id='deleted-node'`,
	} {
		var count int
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("query %q count=%d err=%v", query, count, err)
		}
	}
	var subscriptionCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_subscriptions WHERE id='node-sub'`).Scan(&subscriptionCount); err != nil || subscriptionCount != 1 {
		t.Fatalf("subscription should survive node deletion: count=%d err=%v", subscriptionCount, err)
	}
}

func TestDeleteAccountUninstallsAgentBeforeDeletingHostRecord(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('online-delete-host','在线主机','192.0.2.12','root','password')`); err != nil {
		t.Fatal(err)
	}
	recordPresentBeforeDisconnect := make(chan bool, 1)
	socket := &selfUninstallReplySocket{
		t:               t,
		service:         service,
		serverID:        "online-delete-host",
		disconnectDelay: 20 * time.Millisecond,
		beforeDisconnect: func() {
			var count int
			err := db.QueryRow(`SELECT COUNT(*) FROM server_accounts WHERE id='online-delete-host'`).Scan(&count)
			recordPresentBeforeDisconnect <- err == nil && count == 1
		},
	}
	connection := service.registry.Register("online-delete-host", socket)
	connection.UpdateCapabilities(map[string]bool{"self_uninstall_v1": true})

	rec := httptest.NewRecorder()
	// Even an explicit force query must use the verified cleanup path while a
	// capable Agent is online.
	req := httptest.NewRequest(http.MethodDelete, "/api/server/accounts/online-delete-host?force=1", nil)
	service.deleteAccount(rec, req, db, "online-delete-host")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var accepted struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil || accepted.Data.TaskID == "" {
		t.Fatalf("decode delete task: task=%q err=%v body=%s", accepted.Data.TaskID, err, rec.Body.String())
	}

	select {
	case present := <-recordPresentBeforeDisconnect:
		if !present {
			t.Fatal("host record was deleted before the Agent disconnected")
		}
	case <-time.After(time.Second):
		t.Fatal("self-uninstall task was not sent")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		task, ok := service.taskRegistry.Get(accepted.Data.TaskID)
		if !ok {
			t.Fatal("delete task disappeared")
		}
		snapshot := task.Snapshot()
		if snapshot.Status == TaskCompleted {
			break
		}
		if snapshot.Status == TaskFailed {
			t.Fatalf("delete task failed: %s", snapshot.Error)
		}
		if time.Now().After(deadline) {
			t.Fatal("delete task did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_accounts WHERE id='online-delete-host'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("host record remains after Agent uninstall: count=%d err=%v", count, err)
	}
}

func TestAgentUninstallRequiresOnlineCapableAgentUnlessForced(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('uninstall-host','卸载测试','192.0.2.13','root','password')`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/uninstall/uninstall-host", nil)
	service.handleAgentUninstall(rec, req, db, "uninstall-host")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"can_force_detach":true`) {
		t.Fatalf("offline status=%d body=%s", rec.Code, rec.Body.String())
	}

	connection := service.registry.Register("uninstall-host", &taskReplySocket{
		t:       t,
		service: service,
		reply:   func(int, string) string { return "scheduled" },
	})
	connection.UpdateCapabilities(map[string]bool{"self_update_v1": true})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/server/agent/uninstall/uninstall-host", nil)
	service.handleAgentUninstall(rec, req, db, "uninstall-host")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Agent 版本过旧") {
		t.Fatalf("legacy status=%d body=%s", rec.Code, rec.Body.String())
	}
	service.registry.Disconnect("uninstall-host")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/server/agent/uninstall/uninstall-host?force=1", nil)
	service.handleAgentUninstall(rec, req, db, "uninstall-host")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "未确认主机本地程序已清理") {
		t.Fatalf("force status=%d body=%s", rec.Code, rec.Body.String())
	}
	var status string
	if err := db.QueryRow(`SELECT last_check_status FROM server_accounts WHERE id='uninstall-host'`).Scan(&status); err != nil || status != "uninstalled" {
		t.Fatalf("last_check_status=%q err=%v", status, err)
	}
}

func TestAgentUninstallRefusesToOrphanManagedProxyResources(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('managed-uninstall','托管资源主机','192.0.2.16','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_runtimes(server_id,runtime,apply_status) VALUES('managed-uninstall','sing-box','running')`); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/server/agent/uninstall/managed-uninstall",
		"/api/server/agent/uninstall/managed-uninstall?force=1",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		service.handleAgentUninstall(rec, req, db, "managed-uninstall")
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "安全级联流程") {
			t.Fatalf("path=%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), `"can_force_detach":true`) {
			t.Fatalf("managed resources must not allow force detach: %s", rec.Body.String())
		}
	}
}

func TestAgentUninstallWaitsForDisconnectConfirmation(t *testing.T) {
	t.Setenv("API_MONITOR_AGENT_UNINSTALL_VERIFY_TIMEOUT_MS", "1000")
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('confirmed-uninstall','确认卸载','192.0.2.14','root','password')`); err != nil {
		t.Fatal(err)
	}
	socket := &selfUninstallReplySocket{
		t:               t,
		service:         service,
		serverID:        "confirmed-uninstall",
		disconnectDelay: 20 * time.Millisecond,
	}
	connection := service.registry.Register("confirmed-uninstall", socket)
	connection.UpdateCapabilities(map[string]bool{"self_uninstall_v1": true})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/uninstall/confirmed-uninstall", nil)
	service.handleAgentUninstall(rec, req, db, "confirmed-uninstall")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, online := service.registry.Get("confirmed-uninstall"); online {
		t.Fatal("Agent remained connected after uninstall returned success")
	}
	var status string
	if err := db.QueryRow(`SELECT last_check_status FROM server_accounts WHERE id='confirmed-uninstall'`).Scan(&status); err != nil || status != "uninstalled" {
		t.Fatalf("last_check_status=%q err=%v", status, err)
	}
}

func TestAgentUninstallDoesNotDetachWhenDisconnectCannotBeConfirmed(t *testing.T) {
	t.Setenv("API_MONITOR_AGENT_UNINSTALL_VERIFY_TIMEOUT_MS", "20")
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('stuck-uninstall','卸载超时','192.0.2.15','root','password')`); err != nil {
		t.Fatal(err)
	}
	connection := service.registry.Register("stuck-uninstall", &taskReplySocket{
		t:       t,
		service: service,
		reply:   func(int, string) string { return "scheduled" },
	})
	connection.UpdateCapabilities(map[string]bool{"self_uninstall_v1": true})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/uninstall/stuck-uninstall", nil)
	service.handleAgentUninstall(rec, req, db, "stuck-uninstall")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "未在") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var status string
	if err := db.QueryRow(`SELECT COALESCE(last_check_status,'') FROM server_accounts WHERE id='stuck-uninstall'`).Scan(&status); err != nil || status == "uninstalled" {
		t.Fatalf("unconfirmed uninstall changed status=%q err=%v", status, err)
	}
}

func TestResolveRealtimeMetricsPersistInterval(t *testing.T) {
	t.Setenv("API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS", "")
	if got := resolveRealtimeMetricsPersistInterval(); got != defaultRealtimeMetricsPersistInterval {
		t.Fatalf("default interval = %v, want %v", got, defaultRealtimeMetricsPersistInterval)
	}

	t.Setenv("API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS", "1500")
	if got := resolveRealtimeMetricsPersistInterval(); got != minRealtimeMetricsPersistInterval {
		t.Fatalf("minimum interval = %v, want %v", got, minRealtimeMetricsPersistInterval)
	}

	t.Setenv("API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS", "60000")
	if got := resolveRealtimeMetricsPersistInterval(); got != time.Minute {
		t.Fatalf("custom interval = %v, want %v", got, time.Minute)
	}
}

func TestShouldPersistRealtimeMetricsUsesConfiguredInterval(t *testing.T) {
	service := &Service{
		lastPersist:             make(map[string]time.Time),
		realtimePersistInterval: 30 * time.Second,
	}
	now := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)

	if !service.shouldPersistRealtimeMetrics("server-1", now) {
		t.Fatal("first sample should persist")
	}
	if service.shouldPersistRealtimeMetrics("server-1", now.Add(29*time.Second)) {
		t.Fatal("sample inside configured interval should be skipped")
	}
	if !service.shouldPersistRealtimeMetrics("server-1", now.Add(30*time.Second)) {
		t.Fatal("sample at configured interval should persist")
	}
}

func TestResolveNetworkQualityPersistIntervalDefaultsToOneMinute(t *testing.T) {
	t.Setenv("API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS", "")
	if got := resolveNetworkQualityPersistInterval(); got != time.Minute {
		t.Fatalf("default network quality interval = %v, want %v", got, time.Minute)
	}

	t.Setenv("API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS", "0")
	if got := resolveNetworkQualityPersistInterval(); got != 0 {
		t.Fatalf("zero network quality interval = %v, want disabled", got)
	}

	t.Setenv("API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS", "10000")
	if got := resolveNetworkQualityPersistInterval(); got != minNetworkQualityPersistInterval {
		t.Fatalf("minimum network quality interval = %v, want %v", got, minNetworkQualityPersistInterval)
	}
}

func TestShouldPersistNetworkQualityCanBeDisabled(t *testing.T) {
	service := &Service{
		lastNetworkQualityPersist:     make(map[string]time.Time),
		networkQualityPersistInterval: 0,
	}
	now := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)
	if service.shouldPersistNetworkQuality("server-1", now) {
		t.Fatal("network quality samples should not persist when interval is disabled")
	}

	service.networkQualityPersistInterval = time.Minute
	if !service.shouldPersistNetworkQuality("server-1", now) {
		t.Fatal("first network quality sample should persist when enabled")
	}
	if service.shouldPersistNetworkQuality("server-1", now.Add(59*time.Second)) {
		t.Fatal("network quality sample inside configured interval should be skipped")
	}
	if !service.shouldPersistNetworkQuality("server-1", now.Add(time.Minute)) {
		t.Fatal("network quality sample at configured interval should persist")
	}
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

func TestResolveInstallOriginPrefersPublicBaseURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/server/agent/install/linux/id/key?protocol=http&base_url=https%3A%2F%2Fpanel.example.com%2Fnested", nil)
	req.Host = "127.0.0.1:3000"
	req.Header.Set("X-Forwarded-Host", "internal.local:8080")
	req.Header.Set("X-Forwarded-Proto", "http")

	service := &Service{}
	proto, host := service.resolveInstallOrigin(context.Background(), nil, req, "")
	if proto != "https" || host != "panel.example.com" {
		t.Fatalf("origin = %s://%s, want https://panel.example.com", proto, host)
	}
}

type taskReplySocket struct {
	t       *testing.T
	service *Service
	reply   func(taskType int, data string) string
}

func (s *taskReplySocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	if len(frame) != 2 || frame[0] != "dashboard:task" {
		s.t.Fatalf("unexpected socket event: %#v", frame)
	}
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}
	taskID, _ := payload["id"].(string)
	taskType, _ := payload["type"].(float64)
	taskData, _ := payload["data"].(string)
	result := s.reply(int(taskType), taskData)
	go s.service.taskRegistry.Complete(taskID, result)
	return nil
}

type selfUninstallReplySocket struct {
	t                *testing.T
	service          *Service
	serverID         string
	disconnectDelay  time.Duration
	beforeDisconnect func()
}

func (s *selfUninstallReplySocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	if len(frame) != 2 || frame[0] != "dashboard:task" {
		s.t.Fatalf("unexpected socket event: %#v", frame)
	}
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}
	taskID, _ := payload["id"].(string)
	taskType, _ := payload["type"].(float64)
	if int(taskType) != 52 {
		s.t.Fatalf("task type = %d, want self-uninstall task 52", int(taskType))
	}
	go func() {
		s.service.taskRegistry.Complete(taskID, "scheduled")
		time.Sleep(s.disconnectDelay)
		if s.beforeDisconnect != nil {
			s.beforeDisconnect()
		}
		s.service.registry.Disconnect(s.serverID)
	}()
	return nil
}

type terminalCaptureSocket struct {
	t       *testing.T
	service *Service
	mu      sync.Mutex
	events  []capturedSocketEvent
}

type capturedSocketEvent struct {
	Name string
	Data map[string]interface{}
}

type recordingNotifier struct {
	mu        sync.Mutex
	events    []string
	data      []map[string]interface{}
	refreshes []string
}

func (n *recordingNotifier) RefreshLifecycle(_ context.Context, _ string, eventType string, _ map[string]interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.refreshes = append(n.refreshes, eventType)
	return nil
}

func (n *recordingNotifier) Trigger(_ context.Context, _ string, eventType string, eventData map[string]interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, eventType)
	n.data = append(n.data, eventData)
	return nil
}

func (n *recordingNotifier) snapshot() ([]string, []map[string]interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	events := append([]string(nil), n.events...)
	data := append([]map[string]interface{}(nil), n.data...)
	return events, data
}

func (n *recordingNotifier) refreshSnapshot() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.refreshes...)
}

func (s *terminalCaptureSocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	if len(frame) != 2 {
		s.t.Fatalf("unexpected socket event: %#v", frame)
	}
	name, _ := frame[0].(string)
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}

	s.mu.Lock()
	s.events = append(s.events, capturedSocketEvent{Name: name, Data: payload})
	s.mu.Unlock()

	if name == "dashboard:task" {
		taskID, _ := payload["id"].(string)
		taskType, _ := payload["type"].(float64)
		if int(taskType) == 12 && taskID != "" {
			go s.service.ptyHub.Publish("status:"+taskID, `{"id":"`+taskID+`","status":"ready"}`)
		}
	}
	return nil
}

type reconnectOnUpgradeSocket struct {
	t        *testing.T
	service  *Service
	serverID string
}

func (s *reconnectOnUpgradeSocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	if len(frame) != 2 || frame[0] != "dashboard:task" {
		s.t.Fatalf("unexpected socket event: %#v", frame)
	}
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}
	taskID, _ := payload["id"].(string)
	taskType, _ := payload["type"].(float64)
	if int(taskType) == 5 {
		if taskID != "" {
			go s.service.taskRegistry.Complete(taskID, "scheduled")
		}
		go func() {
			time.Sleep(20 * time.Millisecond)
			s.service.registry.Register(s.serverID, s)
		}()
	}
	return nil
}

func (s *terminalCaptureSocket) Events() []capturedSocketEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]capturedSocketEvent, len(s.events))
	copy(out, s.events)
	return out
}

func TestAgentBatchSnapshotPreservesRequestOrder(t *testing.T) {
	manager := NewAgentBatchManager()
	batch := manager.Create(AgentBatchUpgrade, "https", false, false, 4, []serverIdentity{
		{ID: "server-b", Name: "Beta"},
		{ID: "server-a", Name: "Alpha"},
		{ID: "server-c", Name: "Gamma"},
	})

	for i := 0; i < 20; i++ {
		snapshot := batch.snapshot()
		got := make([]string, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			got = append(got, item.ServerID)
		}
		if strings.Join(got, ",") != "server-b,server-a,server-c" {
			t.Fatalf("snapshot order drifted: %v", got)
		}
	}
}

func TestAccountsLifecycleAndNullableUpdate(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/accounts", `{"name":"edge","host":"127.0.0.1","port":22,"username":"root","auth_type":"password","password":"secret","tags":["prod"],"traffic_limit_bytes":1099511627776,"traffic_alert_enabled":true,"traffic_cycle_type":"monthly","traffic_cycle_day":15}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	id := data["id"].(string)
	if data["password"] != "secret" {
		t.Fatalf("expected decrypted password, got %#v", data["password"])
	}
	if data["traffic_limit_bytes"] != float64(1099511627776) || data["traffic_alert_enabled"] != true || data["traffic_alert_percent"] != float64(100) {
		t.Fatalf("unexpected traffic quota fields after create: %#v", data)
	}
	if data["traffic_cycle_type"] != "monthly" || data["traffic_cycle_day"] != float64(15) {
		t.Fatalf("unexpected traffic cycle fields after create: %#v", data)
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
	if data["traffic_limit_bytes"] != float64(1099511627776) || data["traffic_alert_enabled"] != true {
		t.Fatalf("traffic quota should be preserved by partial update: %#v", data)
	}
	if data["traffic_cycle_type"] != "monthly" || data["traffic_cycle_day"] != float64(15) {
		t.Fatalf("traffic cycle should be preserved by partial update: %#v", data)
	}

	res = perform(service, http.MethodPut, "/api/server/accounts/"+id, `{"traffic_limit_bytes":2199023255552,"traffic_alert_enabled":false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quota update status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data = payload["data"].(map[string]interface{})
	if data["traffic_limit_bytes"] != float64(2199023255552) || data["traffic_alert_enabled"] != false {
		t.Fatalf("unexpected traffic quota fields after update: %#v", data)
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
	listItem := list[0].(map[string]interface{})
	if listItem["traffic_limit_bytes"] != float64(2199023255552) || listItem["traffic_alert_enabled"] != false {
		t.Fatalf("list should include traffic quota fields: %#v", listItem)
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
	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES ('server-1','测试主机','127.0.0.1','root','password')`); err != nil {
		t.Fatal(err)
	}

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

	res := perform(service, http.MethodPost, "/api/server/v2/tasks", `{"serverId":"server-1","domain":"docker","action":"image.list"}`)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline docker task status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestPublicServerStatusPageReturnsWithHistory(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info, traffic_limit_bytes, response_time) VALUES ('server-1', 'edge', '127.0.0.1', 'root', 'password', 'online', '{"cpu":12.5,"mem_percent":48,"disk_percent":63,"net_rx":1280,"net_tx":640,"uptime":3600,"country_code":"jp","location":"Tokyo"}', 1000000, 1080)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_metrics_history (server_id, cpu_usage, mem_usage, disk_usage, net_rx, net_tx, recorded_at) VALUES ('server-1', 12.5, 48, 63, 1280, 640, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert metrics: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_monitor_logs (server_id, status, response_time, checked_at) VALUES ('server-1', 'success', 1080, datetime('now', '-2 minutes')), ('server-1', 'success', 2194, datetime('now', '-1 minutes'))`)
	if err != nil {
		t.Fatalf("insert monitor logs: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_status_pages (slug, title, public, config_json, server_ids_json) VALUES ('infra', 'Infra', 1, '{"showCharts":true,"hideHosts":false,"showTraffic":true}', '["server-1"]')`)
	if err != nil {
		t.Fatalf("insert status page: %v", err)
	}

	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- perform(service, http.MethodGet, "/api/server/public/status-pages/infra", "")
	}()

	var res *httptest.ResponseRecorder
	select {
	case res = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("public server status page request timed out")
	}
	if res.Code != http.StatusOK {
		t.Fatalf("public status page status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	servers := data["servers"].([]interface{})
	if len(servers) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
	server := servers[0].(map[string]interface{})
	history, ok := server["history"].([]interface{})
	if !ok || len(history) != 1 {
		t.Fatalf("expected history item, server=%#v", server)
	}
	if server["host"] != "127.0.0.1" {
		t.Fatalf("expected host to be visible, server=%#v", server)
	}
	if server["countryCode"] != "jp" || server["location"] != "Tokyo" {
		t.Fatalf("expected location fields, server=%#v", server)
	}
	if server["responseTime"] != float64(1080) {
		t.Fatalf("expected response time, server=%#v", server)
	}
	latencyHistory, ok := server["latencyHistory"].([]interface{})
	if !ok || len(latencyHistory) != 2 {
		t.Fatalf("expected latency history, server=%#v", server)
	}
}

func TestPublicServerStatusPageUsesLiveAgentMetadata(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info) VALUES ('server-1', 'edge', '127.0.0.1', 'root', 'password', 'offline', '{"cpu":1,"mem_percent":2,"disk_percent":3,"net_rx":4,"net_tx":5,"uptime":60}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_status_pages (slug, title, public, config_json, server_ids_json) VALUES ('infra', 'Infra', 1, '{"showCharts":false,"hideHosts":true,"showTraffic":true}', '["server-1"]')`)
	if err != nil {
		t.Fatalf("insert status page: %v", err)
	}
	conn := service.registry.Register("server-1", &taskReplySocket{t: t, service: service})
	conn.UpdateMetadata(map[string]interface{}{
		"cpu":               72.5,
		"mem_percent":       63.5,
		"disk_usage":        41.5,
		"net_in_speed":      2048,
		"net_out_speed":     1024,
		"uptime_seconds":    7200,
		"location":          "Tokyo",
		"platform":          "linux",
		"metrics_last_seen": time.Now().UTC().Format(time.RFC3339Nano),
	})

	res := perform(service, http.MethodGet, "/api/server/public/status-pages/infra", "")
	if res.Code != http.StatusOK {
		t.Fatalf("public status page status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	servers := data["servers"].([]interface{})
	server := servers[0].(map[string]interface{})
	if server["online"] != true || server["status"] != "online" {
		t.Fatalf("expected live server to be online, server=%#v", server)
	}
	if server["cpu"] != 72.5 || server["memory"] != 63.5 || server["disk"] != 41.5 {
		t.Fatalf("expected live metadata metrics, server=%#v", server)
	}
	if server["host"] != nil {
		t.Fatalf("expected host to be hidden, server=%#v", server)
	}
}

func TestListAccountsUsesLiveAgentLocationMetadata(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, tags, cached_info) VALUES ('server-geo', 'geo', '127.0.0.1', 'root', 'password', 'offline', '[]', '{"cpu":1,"mem_percent":2}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	conn := service.registry.Register("server-geo", &taskReplySocket{t: t, service: service})
	conn.UpdateMetadata(map[string]interface{}{
		"country_code":      "us",
		"country":           "US",
		"location":          "San Jose, California, US",
		"region":            "San Jose, California, US",
		"latitude":          37.33939,
		"longitude":         -121.89496,
		"metrics_last_seen": time.Now().UTC().Format(time.RFC3339Nano),
	})

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	account := list[0].(map[string]interface{})
	if account["countryCode"] != "us" || account["location"] != "San Jose, California, US" {
		t.Fatalf("expected live location fields on account, account=%#v", account)
	}
	if account["latitude"] != 37.33939 || account["longitude"] != -121.89496 {
		t.Fatalf("expected live coordinates on account, account=%#v", account)
	}
	info := account["info"].(map[string]interface{})
	if info["country_code"] != "us" || info["location"] != "San Jose, California, US" {
		t.Fatalf("expected live location fields in info, info=%#v", info)
	}
	if info["latitude"] != 37.33939 || info["longitude"] != -121.89496 {
		t.Fatalf("expected live coordinates in info, info=%#v", info)
	}
}

func TestListAccountsUsesLiveAgentLocationMetadataWithoutCachedInfo(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, tags) VALUES ('server-geo-empty', 'geo', '127.0.0.1', 'root', 'password', 'offline', '[]')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	conn := service.registry.Register("server-geo-empty", &taskReplySocket{t: t, service: service})
	conn.UpdateMetadata(map[string]interface{}{
		"country_code":      "us",
		"country":           "US",
		"location":          "San Jose, California, US",
		"latitude":          37.33939,
		"longitude":         -121.89496,
		"metrics_last_seen": time.Now().UTC().Format(time.RFC3339Nano),
	})

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	account := list[0].(map[string]interface{})
	if account["countryCode"] != "us" || account["location"] != "San Jose, California, US" {
		t.Fatalf("expected live location fields on account without cached info, account=%#v", account)
	}
	if account["latitude"] != 37.33939 || account["longitude"] != -121.89496 {
		t.Fatalf("expected live coordinates on account without cached info, account=%#v", account)
	}
	info := account["info"].(map[string]interface{})
	if info["country_code"] != "us" || info["location"] != "San Jose, California, US" {
		t.Fatalf("expected live location fields in info without cached info, info=%#v", info)
	}
}

func TestListAccountsQueuesMissingAgentLocationRefresh(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, tags, cached_info) VALUES ('server-ip', 'ip', '127.0.0.1', 'root', 'password', 'online', '[]', '{"cpu":1,"mem_percent":2}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	service.registry.Register("server-ip", &taskReplySocket{t: t, service: service, reply: func(_ int, command string) string {
		if command != "curl -fsSL https://64.ipcheck.ing/geo" {
			t.Fatalf("unexpected command: %s", command)
		}
		return `IP: 64.181.246.5
City: San Jose
Region: California
Country: US
Latitude: 37.33939
Longitude: -121.89496
Org: Oracle Corporation
Timezone: America/Los_Angeles
ASN: AS31898`
	}})

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}

	var cachedRaw string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(context.Background(), "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = 'server-ip'").Scan(&cachedRaw); err != nil {
			t.Fatalf("read cached info: %v", err)
		}
		cached := map[string]interface{}{}
		_ = json.Unmarshal([]byte(cachedRaw), &cached)
		if cached["country_code"] == "us" && cached["latitude"] == 37.33939 && cached["longitude"] == -121.89496 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected location refresh to update cached_info, got %s", cachedRaw)
}

func TestMergeCachedLocationFieldsFromDBPreservesGeoOnHostInfoUpdate(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-geo', 'geo', '127.0.0.1', 'root', 'password', '{"country_code":"us","location":"San Jose, California, US","latitude":37.33939,"longitude":-121.89496}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	next := service.mergeCachedLocationFieldsFromDB(context.Background(), db, "server-geo", map[string]interface{}{
		"platform":      "Windows",
		"agent_version": "1.2.3",
	})

	if next["country_code"] != "us" || next["location"] != "San Jose, California, US" {
		t.Fatalf("expected location fields to be preserved, next=%#v", next)
	}
	if next["latitude"] != 37.33939 || next["longitude"] != -121.89496 {
		t.Fatalf("expected coordinates to be preserved, next=%#v", next)
	}
	if next["platform"] != "Windows" || next["agent_version"] != "1.2.3" {
		t.Fatalf("expected host info fields to be preserved, next=%#v", next)
	}
}

func TestPublicServerStatusPageWithNoServersDoesNotExposeAllHosts(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info) VALUES ('server-1', 'edge', '127.0.0.1', 'root', 'password', 'online', '{"cpu":12.5}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO server_status_pages (slug, title, public, config_json, server_ids_json) VALUES ('empty', 'Empty', 1, '{}', '[]')`)
	if err != nil {
		t.Fatalf("insert status page: %v", err)
	}

	res := perform(service, http.MethodGet, "/api/server/public/status-pages/empty", "")
	if res.Code != http.StatusOK {
		t.Fatalf("public status page status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	servers := data["servers"].([]interface{})
	if len(servers) != 0 {
		t.Fatalf("expected no public servers, got %#v", servers)
	}
}

func TestSaveServerStatusPageRequiresServers(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/status-pages", `{"title":"Empty","slug":"empty","serverIds":[]}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create empty status page status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestUpdateMissingServerStatusPageReturnsNotFound(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPut, "/api/server/status-pages/999", `{"title":"Missing","slug":"missing","serverIds":["server-1"],"config":{}}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("update missing status page status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestDockerOverviewDoesNotInferInstalledFromUnrequestedScopes(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info) VALUES ('docker-empty', 'docker', '', 'root', 'password', 'online', '{"docker":{"installed":false}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	res := perform(service, http.MethodGet, "/api/server/v2/docker/overview", "")
	if res.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	servers := data["servers"].([]interface{})
	docker := servers[0].(map[string]interface{})["docker"].(map[string]interface{})
	if docker["installed"] != false {
		t.Fatalf("docker installed inferred from unrequested scopes: %#v", docker)
	}
}

func TestDockerOverviewInvalidLiveJSONDoesNotInferInstalled(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info) VALUES ('docker-bad-json', 'docker', '', 'root', 'password', 'online', '{"docker":{"installed":false}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	service.registry.Register("docker-bad-json", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			if taskType != dockerTaskImages {
				t.Fatalf("unexpected task type: %d data=%s", taskType, data)
			}
			return "not-json"
		},
	})

	res := perform(service, http.MethodGet, "/api/server/v2/docker/overview?scope=images", "")
	if res.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	server := data["servers"].([]interface{})[0].(map[string]interface{})
	docker := server["docker"].(map[string]interface{})
	if docker["installed"] != false {
		t.Fatalf("docker installed inferred from invalid json: %#v", docker)
	}
	errors := server["errors"].(map[string]interface{})
	if !strings.Contains(errors["images"].(string), "invalid docker json") {
		t.Fatalf("expected image parse error, got %#v", errors)
	}
}

func TestDockerOverviewQueriesServersConcurrently(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info) VALUES
		('docker-one', 'docker one', '', 'root', 'password', 'online', '{"docker":{"installed":false}}'),
		('docker-two', 'docker two', '', 'root', 'password', 'online', '{"docker":{"installed":false}}')
	`)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	reply := func(taskType int, data string) string {
		if taskType != dockerTaskContainers {
			t.Fatalf("unexpected task type: %d data=%s", taskType, data)
		}
		started <- struct{}{}
		<-release
		return `[{"id":"abc123","name":"web","state":"running"}]`
	}
	service.registry.Register("docker-one", &taskReplySocket{t: t, service: service, reply: reply})
	service.registry.Register("docker-two", &taskReplySocket{t: t, service: service, reply: reply})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- perform(service, http.MethodGet, "/api/server/v2/docker/overview?scope=containers", "")
	}()

	bothStarted := true
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for count := 0; count < 2; count++ {
		select {
		case <-started:
		case <-timer.C:
			bothStarted = false
		}
		if !bothStarted {
			break
		}
	}
	close(release)
	res := <-done
	if !bothStarted {
		t.Fatal("overview did not start both server queries concurrently")
	}
	if res.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", res.Code, res.Body.String())
	}

	payload := decodePayload(t, res)
	data := payload["data"].(map[string]interface{})
	servers := data["servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %#v", servers)
	}
}

func TestDockerProxyRoutesUseDockerSemanticsOverAgentTasks(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('docker-agent', 'docker', '', 'root', 'password', '{"docker":{"installed":true}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	service.registry.Register("docker-agent", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			switch taskType {
			case dockerTaskContainers:
				return `[{"id":"abc123","name":"web","state":"running"}]`
			case dockerTaskAction:
				if !strings.Contains(data, `"action":"start"`) || !strings.Contains(data, `"container_id":"abc123"`) {
					t.Fatalf("unexpected action data: %s", data)
				}
				return "container start success"
			case dockerTaskLogs:
				if !strings.Contains(data, `"container_id":"abc123"`) || !strings.Contains(data, `"tail":50`) {
					t.Fatalf("unexpected logs data: %s", data)
				}
				return "line one\nline two\n"
			case dockerTaskImageAction:
				if !strings.Contains(data, `"action":"remove"`) || !strings.Contains(data, `"image":"library/nginx:latest"`) {
					t.Fatalf("unexpected image action data: %s", data)
				}
				return "image removed"
			case dockerTaskComposeList:
				return `[{"Name":"edge","Status":"running(2)","ConfigFiles":"/srv/edge/docker-compose.yml","WorkingDir":"/srv/edge"}]`
			case dockerTaskComposeAct:
				var req map[string]interface{}
				if err := json.Unmarshal([]byte(data), &req); err != nil {
					t.Fatalf("decode compose action: %v data=%s", err, data)
				}
				action, _ := req["action"].(string)
				if action != "up" && action != "start" && action != "stop" && action != "update" {
					t.Fatalf("unexpected compose action: %s data=%s", action, data)
				}
				if req["project"] != "edge" || req["config_file"] != "/srv/edge/docker-compose.yml" {
					t.Fatalf("unexpected compose action data: %s", data)
				}
				for _, alias := range []string{"configFile", "configFiles", "ConfigFiles", "configDir", "config_dir"} {
					if _, exists := req[alias]; exists {
						t.Fatalf("compose action kept alias field %s: %s", alias, data)
					}
				}
				return "compose " + action + " success"
			default:
				t.Fatalf("unexpected task type: %d", taskType)
				return ""
			}
		},
	})

	res := perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/containers/json", "")
	if res.Code != http.StatusOK {
		t.Fatalf("containers status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	if len(list) != 1 || list[0].(map[string]interface{})["id"] != "abc123" {
		t.Fatalf("unexpected containers payload=%#v", payload)
	}

	res = perform(service, http.MethodPost, "/api/server/v2/docker/docker-agent/containers/abc123/start", "")
	if res.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	if payload["success"] != true {
		t.Fatalf("unexpected start payload=%#v", payload)
	}

	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/containers/abc123/logs?tail=50", "")
	if res.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", res.Code, res.Body.String())
	}
	if res.Body.String() != "line one\nline two\n" {
		t.Fatalf("unexpected logs body=%q", res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/server/v2/docker/docker-agent/images?image=library%2Fnginx%3Alatest", "")
	if res.Code != http.StatusOK {
		t.Fatalf("image remove status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/compose/projects", "")
	if res.Code != http.StatusOK {
		t.Fatalf("compose projects status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/stacks", "")
	if res.Code != http.StatusOK {
		t.Fatalf("stacks status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	stacks := payload["data"].([]interface{})
	if len(stacks) != 1 || stacks[0].(map[string]interface{})["name"] != "edge" {
		t.Fatalf("unexpected stacks payload=%#v", payload)
	}

	res = perform(service, http.MethodPost, "/api/server/v2/docker/docker-agent/stacks/edge/up", `{"configFile":"/srv/edge/docker-compose.yml"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("stack up status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/stacks", "")
	if res.Code != http.StatusOK {
		t.Fatalf("stacks after up status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	stacks = payload["data"].([]interface{})
	if len(stacks) != 1 || stacks[0].(map[string]interface{})["status"] != "running" {
		t.Fatalf("expected stack status to be updated, payload=%#v", payload)
	}

	res = perform(service, http.MethodPost, "/api/server/v2/docker/docker-agent/stacks/edge/update", `{"configFiles":["/srv/edge/docker-compose.yml"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("stack update status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodPost, "/api/server/v2/docker/docker-agent/compose/edge/start", `{"configFile":"/srv/edge/docker-compose.yml"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("compose start status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodPost, "/api/server/v2/docker/docker-agent/stacks/edge/stop", `{"configFile":"/srv/edge/docker-compose.yml"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("stack stop status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/stacks", "")
	if res.Code != http.StatusOK {
		t.Fatalf("stacks after stop status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	stacks = payload["data"].([]interface{})
	if len(stacks) != 1 || stacks[0].(map[string]interface{})["status"] != "stopped" {
		t.Fatalf("expected stack status to be stopped, payload=%#v", payload)
	}

	res = perform(service, http.MethodDelete, "/api/server/v2/docker/docker-agent/stacks/edge", "")
	if res.Code != http.StatusOK {
		t.Fatalf("stack delete status=%d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/server/v2/docker/docker-agent/stacks", "")
	if res.Code != http.StatusOK {
		t.Fatalf("stacks after delete status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	if got := len(payload["data"].([]interface{})); got != 0 {
		t.Fatalf("expected stack record to be deleted, got %d: %#v", got, payload)
	}
}

func TestDockerCreatePolicyBlocksHighRiskOptions(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('docker-policy', 'docker', '', 'root', 'password', '{"docker":{"installed":true}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	res := perform(service, http.MethodPost, "/api/server/v2/tasks", `{"serverId":"docker-policy","domain":"docker","action":"container.create","payload":{"image":"nginx:latest","privileged":true}}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("privileged create status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	if !strings.Contains(payload["error"].(string), "privileged") {
		t.Fatalf("unexpected policy error=%#v", payload)
	}

	res = perform(service, http.MethodPost, "/api/server/v2/tasks", `{"serverId":"docker-policy","domain":"docker","action":"container.create","payload":{"image":"nginx:latest","extraArgs":["--network=host"]}}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("host network create status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestDockerComposeTaskNormalizesConfigFiles(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('compose-agent', 'compose', '', 'root', 'password', '{"docker":{"installed":true}}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	service.registry.Register("compose-agent", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			if taskType != dockerTaskComposeAct {
				t.Fatalf("unexpected task type: %d", taskType)
			}
			if !strings.Contains(data, `"config_file":"/srv/app/compose.yml,/srv/app/compose.prod.yml"`) {
				t.Fatalf("compose config files were not normalized: %s", data)
			}
			return "compose restart success"
		},
	})

	res := perform(service, http.MethodPost, "/api/server/v2/tasks", `{"serverId":"compose-agent","domain":"docker","action":"compose.restart","payload":{"project":"edge","configFiles":["/srv/app/compose.yml","/srv/app/compose.prod.yml"]}}`)
	if res.Code != http.StatusOK {
		t.Fatalf("compose task status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	taskID, _ := payload["taskId"].(string)
	if taskID == "" {
		taskID, _ = payload["data"].(map[string]interface{})["taskId"].(string)
	}
	if taskID == "" {
		t.Fatalf("missing compose task id: %#v", payload)
	}
	deadline := time.Now().Add(time.Second)
	for {
		task, ok := service.taskRegistry.Get(taskID)
		if !ok {
			t.Fatalf("compose task disappeared: %s", taskID)
		}
		snapshot := task.Snapshot()
		if snapshot.Status == TaskCompleted {
			break
		}
		if snapshot.Status == TaskFailed {
			t.Fatalf("compose task failed: %s", snapshot.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("compose task did not complete in time: %#v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDockerComposeUpdateActionUsesLongTimeout(t *testing.T) {
	if !isDockerComposeAction("update") {
		t.Fatal("compose update action should be supported")
	}
	if got := dockerComposeActionTimeout("update"); got != 300*time.Second {
		t.Fatalf("compose update timeout=%s, want=5m", got)
	}
}

func TestDockerExecShellCommandQuotesContainerName(t *testing.T) {
	got := dockerExecShellCommand("web'app")
	want := "docker exec -it 'web'\"'\"'app' sh -lc 'exec /bin/bash || exec /bin/sh || exec sh'\n"
	if got != want {
		t.Fatalf("docker exec command = %q, want %q", got, want)
	}
}

func TestAgentTerminalContainerUsesDirectExecPayload(t *testing.T) {
	service, _ := testService(t)
	service.ptyHub = newPtyDataHub()
	socket := &terminalCaptureSocket{t: t, service: service}
	service.registry.Register("server-terminal", socket)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		service.runAgentTerminalSession(r, conn, "server-terminal")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/terminal?pty_id=pty-test&container=web%27app&cols=100&rows=40"
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial terminal websocket: %v", err)
	}
	defer client.Close()

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var status terminalWSMessage
	if err := client.ReadJSON(&status); err != nil {
		t.Fatalf("read terminal status: %v", err)
	}
	if status.Type != "status" || status.Data != "connected" || status.Transport != "agent" {
		t.Fatalf("unexpected status message: %#v", status)
	}

	events := socket.Events()
	if len(events) == 0 || events[0].Name != "dashboard:task" {
		t.Fatalf("expected dashboard:task event, got %#v", events)
	}
	taskData, _ := events[0].Data["data"].(string)
	var startPayload map[string]interface{}
	if err := json.Unmarshal([]byte(taskData), &startPayload); err != nil {
		t.Fatalf("decode terminal start payload: %v data=%s", err, taskData)
	}
	if startPayload["command"] != "docker" {
		t.Fatalf("expected docker command payload, got %#v", startPayload)
	}
	args, ok := startPayload["args"].([]interface{})
	if !ok || len(args) != 6 {
		t.Fatalf("expected docker exec args, got %#v", startPayload["args"])
	}
	if args[2] != "web'app" {
		t.Fatalf("container arg was not passed raw: %#v", args)
	}
	for _, event := range events {
		if event.Name == "dashboard:pty_input" {
			t.Fatalf("new agent ready path should not send legacy pty input: %#v", events)
		}
	}

	if err := client.WriteJSON(terminalWSMessage{Type: "disconnect"}); err != nil {
		t.Fatalf("send disconnect: %v", err)
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

func TestAgentHeartbeatPreservesCachedLocationMetadata(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-geo', 'geo', '127.0.0.1', 'root', 'password', '{"country_code":"us","location":"San Jose, California, US","latitude":37.33939,"longitude":-121.89496,"platform":"Windows"}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('global_agent_key', 'good-key', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/heartbeat", strings.NewReader(`{"server_id":"server-geo","status":"online","info":{"cpu":18,"country_code":"","location":""}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer good-key")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", res.Code, res.Body.String())
	}

	var cached string
	if err := db.QueryRowContext(context.Background(), `SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = 'server-geo'`).Scan(&cached); err != nil {
		t.Fatalf("read cached info: %v", err)
	}
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(cached), &info); err != nil {
		t.Fatalf("decode cached info: %v", err)
	}
	if info["country_code"] != "us" || info["location"] != "San Jose, California, US" {
		t.Fatalf("expected heartbeat to preserve location metadata, info=%#v", info)
	}
	if info["latitude"] != 37.33939 || info["longitude"] != -121.89496 {
		t.Fatalf("expected heartbeat to preserve coordinates, info=%#v", info)
	}
	if info["cpu"] != float64(18) {
		t.Fatalf("expected heartbeat metrics to update, info=%#v", info)
	}
}

func TestRealtimeMetricsPersistPreservesCachedLocationMetadata(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-geo', 'geo', '127.0.0.1', 'root', 'password', '{"country_code":"us","country":"US","location":"San Jose, California, US","latitude":37.33939,"longitude":-121.89496,"geo_source":"ipcheck-agent"}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	service.registry.Register("server-geo", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		return ""
	}})

	metrics := map[string]interface{}{
		"cpu":         12.5,
		"mem_used":    float64(512 * 1024 * 1024),
		"mem_total":   float64(1024 * 1024 * 1024),
		"disk_used":   float64(10 * 1024 * 1024),
		"disk_total":  float64(20 * 1024 * 1024),
		"uptime":      float64(60),
		"server_id":   "server-geo",
		"countryCode": "",
	}

	merged := service.mergeCachedLocationFieldsFromDB(context.Background(), db, "server-geo", metrics)
	if merged["country_code"] != "us" || merged["country"] != "US" || merged["location"] != "San Jose, California, US" {
		t.Fatalf("expected realtime metrics to preserve cached location, merged=%#v", merged)
	}
	if merged["latitude"] != 37.33939 || merged["longitude"] != -121.89496 {
		t.Fatalf("expected realtime metrics to preserve coordinates, merged=%#v", merged)
	}

	conn, ok := service.registry.Get("server-geo")
	if !ok {
		t.Fatal("expected registered agent")
	}
	metadata := conn.GetMetadata()
	if metadata["country_code"] != "us" || metadata["location"] != "San Jose, California, US" {
		t.Fatalf("expected preserved location in connection metadata, metadata=%#v", metadata)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestListAccountsDoesNotResolveMissingLocationAutomatically(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, tags, country, cached_info) VALUES ('server-ip', 'edge', '185.255.55.55', 'root', 'password', '[]', 'auto', '{}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	previousClient := geoHTTPClient
	geoHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("list accounts should not call geo provider: %s", req.URL.String())
		return nil, nil
	})}
	t.Cleanup(func() { geoHTTPClient = previousClient })

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	account := list[0].(map[string]interface{})
	if value, _ := account["resolved_country"].(string); value != "" {
		t.Fatalf("resolved_country = %#v", account["resolved_country"])
	}
	if value, _ := account["countryCode"].(string); value != "" {
		t.Fatalf("countryCode should not expose auto country: %#v", account)
	}
	if value, _ := account["location"].(string); value == "auto" {
		t.Fatalf("location should not expose auto country: %#v", account)
	}
	info := account["info"].(map[string]interface{})
	if info["country_code"] != "" || info["location"] != "" {
		t.Fatalf("expected empty cached location info, info=%#v", info)
	}
	if _, ok := account["latitude"]; ok {
		t.Fatalf("account should not expose missing latitude as zero: %#v", account)
	}
	if _, ok := account["longitude"]; ok {
		t.Fatalf("account should not expose missing longitude as zero: %#v", account)
	}
	if _, ok := info["latitude"]; ok {
		t.Fatalf("info should not expose missing latitude as zero: %#v", info)
	}
	if _, ok := info["longitude"]; ok {
		t.Fatalf("info should not expose missing longitude as zero: %#v", info)
	}
}

func TestRefreshAccountLocationsQueriesOnlineAgentIPCheck(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, tags, country, cached_info) VALUES ('server-ip', 'edge', '185.255.55.55', 'root', 'password', '[]', 'auto', '{}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	service.registry.Register("server-ip", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		if taskType != 1 {
			t.Fatalf("task type = %d, want 1", taskType)
		}
		if !strings.Contains(data, "curl -fsSL https://64.ipcheck.ing/geo") {
			t.Fatalf("unexpected command: %s", data)
		}
		return `IP: 64.181.246.5
City: San Jose
Region: California
Country: US
Latitude: 37.33939
Longitude: -121.89496
Org: Oracle Corporation
Timezone: America/Los_Angeles
ASN: AS31898`
	}})

	res := perform(service, http.MethodPost, "/api/server/accounts/refresh-locations", "")
	if res.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	account := list[0].(map[string]interface{})
	if account["resolved_country"] != "San Jose, California, US" {
		t.Fatalf("resolved_country = %#v", account["resolved_country"])
	}
	info := account["info"].(map[string]interface{})
	if info["country_code"] != "us" || info["location"] != "San Jose, California, US" {
		t.Fatalf("expected ipcheck location in info, info=%#v", info)
	}
	if account["countryCode"] != "us" || account["location"] != "San Jose, California, US" {
		t.Fatalf("expected location fields on account, account=%#v", account)
	}
	if account["latitude"] != 37.33939 || account["longitude"] != -121.89496 {
		t.Fatalf("expected coordinates on account, account=%#v", account)
	}
}

func TestInitialAgentConnectRefreshesMissingLocationOnce(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, tags, country, cached_info) VALUES ('server-ip', 'edge', '185.255.55.55', 'root', 'password', '[]', 'auto', '{}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	calls := 0
	service.registry.Register("server-ip", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		calls++
		if taskType != 1 {
			t.Fatalf("task type = %d, want 1", taskType)
		}
		if !strings.Contains(data, "curl -fsSL https://64.ipcheck.ing/geo") {
			t.Fatalf("unexpected command: %s", data)
		}
		return `IP: 64.181.246.5
City: San Jose
Region: California
Country: US
Latitude: 37.33939
Longitude: -121.89496
Org: Oracle Corporation
Timezone: America/Los_Angeles
ASN: AS31898`
	}})

	service.refreshAccountLocationFromAgentIfMissing("server-ip")

	if calls != 1 {
		t.Fatalf("initial refresh calls = %d, want 1", calls)
	}
	var cached string
	if err := db.QueryRowContext(context.Background(), `SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = 'server-ip'`).Scan(&cached); err != nil {
		t.Fatalf("read cached info: %v", err)
	}
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(cached), &info); err != nil {
		t.Fatalf("decode cached info: %v", err)
	}
	if info["country_code"] != "us" || info["location"] != "San Jose, California, US" {
		t.Fatalf("expected initial refresh location, info=%#v", info)
	}
	if info["latitude"] != 37.33939 || info["longitude"] != -121.89496 {
		t.Fatalf("expected initial refresh coordinates, info=%#v", info)
	}
}

func TestInitialAgentConnectSkipsCachedLocation(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, tags, cached_info) VALUES ('server-ip', 'edge', '185.255.55.55', 'root', 'password', '[]', '{"country_code":"us","location":"San Jose, California, US","latitude":37.33939,"longitude":-121.89496}')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	service.registry.Register("server-ip", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		t.Fatalf("initial refresh should not run when location is cached: type=%d data=%s", taskType, data)
		return ""
	}})

	service.refreshAccountLocationFromAgentIfMissing("server-ip")
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

func TestPersistMetricsAcceptsAgentNumericGpuMemoryFields(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('server-agent-gpu', 'agent gpu', '', 'root', 'password')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	err = service.persistMetrics(context.Background(), db, "server-agent-gpu", map[string]interface{}{
		"gpu_usage":     float64(23),
		"gpu_mem_used":  float64(2_615_148_544),
		"gpu_mem_total": float64(8_585_740_288),
		"gpu_power":     float64(10),
		"gpu_temp":      float64(56),
	})
	if err != nil {
		t.Fatalf("persist metrics: %v", err)
	}

	var gpuUsage, gpuPower, gpuTemp float64
	var gpuMemUsed, gpuMemTotal int64
	err = db.QueryRowContext(context.Background(), `SELECT gpu_usage, gpu_mem_used, gpu_mem_total, gpu_power, gpu_temp FROM server_metrics_history WHERE server_id = 'server-agent-gpu'`).
		Scan(&gpuUsage, &gpuMemUsed, &gpuMemTotal, &gpuPower, &gpuTemp)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}

	if gpuUsage != 23 || gpuMemUsed != 2_615_148_544 || gpuMemTotal != 8_585_740_288 || gpuPower != 10 || gpuTemp != 56 {
		t.Fatalf("unexpected gpu metrics: usage=%v used=%d total=%d power=%v temp=%v", gpuUsage, gpuMemUsed, gpuMemTotal, gpuPower, gpuTemp)
	}
}

func TestListAccountsReportsAgentMetricsFreshnessSeparately(t *testing.T) {
	service, db := testService(t)
	now := time.Now().UTC()
	freshInfo, _ := json.Marshal(map[string]interface{}{
		"metrics_last_seen": now.Format(time.RFC3339Nano),
		"metrics_health":    "fresh",
		"cpu":               float64(12),
	})
	staleInfo, _ := json.Marshal(map[string]interface{}{
		"metrics_last_seen": now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		"metrics_health":    "fresh",
		"cpu":               float64(12),
	})

	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info, tags, order_index) VALUES
		('fresh-agent', 'fresh', '0.0.0.0', 'agent', 'password', 'online', ?, '[]', 1),
		('stale-agent', 'stale', '0.0.0.0', 'agent', 'password', 'online', ?, '[]', 2)`, string(freshInfo), string(staleInfo))
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}
	service.registry.Register("fresh-agent", &taskReplySocket{t: t, service: service, reply: func(int, string) string { return "" }})
	service.registry.Register("stale-agent", &taskReplySocket{t: t, service: service, reply: func(int, string) string { return "" }})
	service.markRealtimeMetricsPersistResult("fresh-agent", false, errors.New("database busy"), time.Now())

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	byID := map[string]map[string]interface{}{}
	for _, item := range list {
		server := item.(map[string]interface{})
		byID[server["id"].(string)] = server
	}

	if byID["fresh-agent"]["agent_online"] != true || byID["fresh-agent"]["metrics_health"] != "degraded" || byID["fresh-agent"]["metrics_stale"] != true {
		t.Fatalf("degraded health mismatch: %#v", byID["fresh-agent"])
	}
	if byID["stale-agent"]["agent_online"] != true || byID["stale-agent"]["metrics_health"] != "stale" || byID["stale-agent"]["metrics_stale"] != true {
		t.Fatalf("stale health mismatch: %#v", byID["stale-agent"])
	}
}

func TestListAccountsUsesLiveAgentConnectionAsOnlineStatus(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info, tags, order_index) VALUES
		('live-agent', 'live', '0.0.0.0', 'agent', 'password', 'offline', '{}', '[]', 1)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	connection := service.registry.Register("live-agent", &taskReplySocket{t: t, service: service, reply: func(int, string) string { return "" }})
	connection.UpdateCapabilities(map[string]bool{"remote_desktop_v1": true})

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("expected one account, got %#v", list)
	}
	server := list[0].(map[string]interface{})
	if server["status"] != "online" || server["agent_online"] != true {
		t.Fatalf("live agent should override stale offline db status: %#v", server)
	}
	agentCapabilities, ok := server["agent_capabilities"].(map[string]interface{})
	if !ok || agentCapabilities["remote_desktop_v1"] != true {
		t.Fatalf("live agent capabilities should be included in account list: %#v", server)
	}
}

func TestListAccountsReportsInterruptedAsNotOnline(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, cached_info, tags, order_index) VALUES
		('interrupted-agent', '中断主机', '0.0.0.0', 'agent', 'password', 'online', '{}', '[]', 1),
		('offline-agent', '离线主机', '0.0.0.0', 'agent', 'password', 'online', '{}', '[]', 2)`)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	service.presence.mu.Lock()
	interrupted := service.presence.ensureLocked("interrupted-agent")
	interrupted.Status = agentPresenceSuspect
	offline := service.presence.ensureLocked("offline-agent")
	offline.Status = agentPresenceOffline
	service.presence.mu.Unlock()

	res := perform(service, http.MethodGet, "/api/server/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	list := payload["data"].([]interface{})
	byID := map[string]map[string]interface{}{}
	for _, item := range list {
		server := item.(map[string]interface{})
		byID[server["id"].(string)] = server
	}

	if got := byID["interrupted-agent"]["status"]; got != "interrupted" {
		t.Fatalf("interrupted host status = %#v, want interrupted", got)
	}
	if byID["interrupted-agent"]["agent_online"] != false {
		t.Fatalf("interrupted host agent_online = %#v, want false", byID["interrupted-agent"]["agent_online"])
	}
	if got := byID["offline-agent"]["status"]; got != "offline" {
		t.Fatalf("offline host status = %#v, want offline", got)
	}
}

func TestConnectionRegistryIgnoresStaleSocketDisconnect(t *testing.T) {
	registry := NewConnectionRegistry()
	oldSocket := &taskReplySocket{t: t, reply: func(int, string) string { return "" }}
	newSocket := &taskReplySocket{t: t, reply: func(int, string) string { return "" }}

	registry.Register("agent-1", oldSocket)
	registry.Register("agent-1", newSocket)

	if registry.DisconnectIfSocket("agent-1", oldSocket) {
		t.Fatal("old socket disconnect should not remove replacement connection")
	}
	conn, exists := registry.Get("agent-1")
	if !exists || conn.Socket != newSocket {
		t.Fatalf("replacement connection should remain registered: exists=%v conn=%#v", exists, conn)
	}

	if !registry.DisconnectIfSocket("agent-1", newSocket) {
		t.Fatal("current socket disconnect should remove connection")
	}
	if _, exists := registry.Get("agent-1"); exists {
		t.Fatal("current socket should be removed after matching disconnect")
	}
}

func TestAgentSocketAuthenticationRequiresValidServerAndKey(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status, country, resolved_country, cached_info, tags, order_index) VALUES
		('auth-agent', 'auth', '0.0.0.0', 'agent', 'password', 'offline', 'us', 'San Jose, California, US', '{"country_code":"us","location":"San Jose, California, US","latitude":37.33939,"longitude":-121.89496}', '[]', 1)`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('global_agent_key', 'good-key', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	badSession := &EngineIOSession{ID: "bad-auth", PendingMessages: []string{}}
	service.engineIO.mu.Lock()
	service.engineIO.sessions[badSession.ID] = badSession
	service.engineIO.mu.Unlock()
	service.engineIO.handleSocketIOMessage(badSession, `2["agent:connect",{"server_id":"auth-agent","key":"bad-key","hostname":"edge"}]`)
	if _, exists := service.registry.Get("auth-agent"); exists {
		t.Fatal("bad key should not register agent connection")
	}
	if badSession.Authenticated {
		t.Fatal("bad key should leave session unauthenticated")
	}
	if joined := strings.Join(badSession.PendingMessages, "\n"); !strings.Contains(joined, "dashboard:auth_fail") || strings.Contains(joined, "dashboard:auth_ok") {
		t.Fatalf("expected only auth_fail for bad key, messages=%s", joined)
	}

	missingSession := &EngineIOSession{ID: "missing-auth", PendingMessages: []string{}}
	service.engineIO.mu.Lock()
	service.engineIO.sessions[missingSession.ID] = missingSession
	service.engineIO.mu.Unlock()
	service.engineIO.handleSocketIOMessage(missingSession, `2["agent:connect",{"server_id":"missing-agent","key":"good-key","hostname":"edge"}]`)
	if _, exists := service.registry.Get("missing-agent"); exists {
		t.Fatal("missing server should not register agent connection")
	}
	if joined := strings.Join(missingSession.PendingMessages, "\n"); !strings.Contains(joined, "dashboard:auth_fail") || strings.Contains(joined, "dashboard:auth_ok") {
		t.Fatalf("expected only auth_fail for missing server, messages=%s", joined)
	}

	goodSession := &EngineIOSession{ID: "good-auth", PendingMessages: []string{}}
	service.engineIO.mu.Lock()
	service.engineIO.sessions[goodSession.ID] = goodSession
	service.engineIO.mu.Unlock()
	service.engineIO.handleSocketIOMessage(goodSession, `2["agent:connect",{"server_id":"auth-agent","key":"good-key","hostname":"edge"}]`)
	if _, exists := service.registry.Get("auth-agent"); !exists {
		t.Fatal("valid agent should register connection")
	}
	if !goodSession.Authenticated || goodSession.ServerID != "auth-agent" {
		t.Fatalf("valid agent should authenticate session: %#v", goodSession)
	}
	if joined := strings.Join(goodSession.PendingMessages, "\n"); !strings.Contains(joined, "dashboard:auth_ok") || strings.Contains(joined, "dashboard:auth_fail") {
		t.Fatalf("expected auth_ok for valid agent, messages=%s", joined)
	}
}

func TestAnonymousSessionCannotForgeAgentState(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status) VALUES
		('anon-agent', 'anon', '0.0.0.0', 'agent', 'password', 'offline')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	anonSession := &EngineIOSession{ID: "anon", PendingMessages: []string{}}
	service.engineIO.mu.Lock()
	service.engineIO.sessions[anonSession.ID] = anonSession
	service.engineIO.mu.Unlock()

	service.engineIO.handleSocketIOMessage(anonSession, `2["agent:state",{"server_id":"anon-agent","cpu":99,"memory":99,"disk_usage":99}]`)
	service.engineIO.handleSocketIOMessage(anonSession, `2["agent:heartbeat",{"server_id":"anon-agent","ts":1}]`)
	service.engineIO.handleSocketIOMessage(anonSession, `2["agent:host_info",{"server_id":"anon-agent","hostname":"spoofed"}]`)

	var status, cachedInfo string
	err = db.QueryRowContext(context.Background(), `SELECT status, COALESCE(cached_info,'') FROM server_accounts WHERE id = 'anon-agent'`).Scan(&status, &cachedInfo)
	if err != nil {
		t.Fatalf("lookup account: %v", err)
	}
	if status != "offline" {
		t.Fatalf("anonymous agent:state must not mark host online, status=%q", status)
	}
	if cachedInfo != "" {
		t.Fatalf("anonymous agent:state must not persist cached_info, got %q", cachedInfo)
	}
	if anonSession.Authenticated {
		t.Fatal("anonymous session must stay unauthenticated")
	}
	if _, exists := service.registry.Get("anon-agent"); exists {
		t.Fatal("anonymous session must not register an agent connection")
	}
}

func TestAuthenticatedSessionStateUpdatesHost(t *testing.T) {
	service, db := testService(t)
	service.realtimePersistInterval = 50 * time.Millisecond
	service.presence = nil
	service.registry = NewConnectionRegistry()
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, status) VALUES
		('real-agent', 'real', '0.0.0.0', 'agent', 'password', 'offline')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('global_agent_key', 'good-key', datetime('now'))`); err != nil {
		t.Fatalf("insert key: %v", err)
	}

	authSession := &EngineIOSession{ID: "auth", PendingMessages: []string{}}
	service.engineIO.mu.Lock()
	service.engineIO.sessions[authSession.ID] = authSession
	service.engineIO.mu.Unlock()
	service.engineIO.handleSocketIOMessage(authSession, `2["agent:connect",{"server_id":"real-agent","key":"good-key","hostname":"real"}]`)
	if !authSession.Authenticated || authSession.ServerID != "real-agent" {
		t.Fatalf("session should be authenticated: %#v", authSession)
	}

	service.engineIO.handleSocketIOMessage(authSession, `2["agent:state",{"server_id":"real-agent","cpu":42,"memory":50,"disk_usage":30}]`)

	deadline := time.Now().Add(5 * time.Second)
	for {
		var status string
		err := db.QueryRowContext(context.Background(), `SELECT status FROM server_accounts WHERE id = 'real-agent'`).Scan(&status)
		if err != nil {
			t.Fatalf("lookup account: %v", err)
		}
		if status == "online" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("authenticated agent:state should mark host online, status=%q", status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestAgentCredentialsAreScopedPerServer(t *testing.T) {
	service, db := testService(t)
	for _, id := range []string{"agent-a", "agent-b"} {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES (?, ?, '0.0.0.0', 'agent', 'password')`, id, id); err != nil {
			t.Fatalf("insert account %s: %v", id, err)
		}
	}
	keyA, err := service.getOrGenerateAgentKeyForServer(context.Background(), db, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := service.getOrGenerateAgentKeyForServer(context.Background(), db, "agent-b")
	if err != nil {
		t.Fatal(err)
	}
	if keyA == keyB {
		t.Fatal("different servers must not share an agent credential")
	}
	if err := service.validateAgentKeyForServer(context.Background(), db, "agent-a", keyA); err != nil {
		t.Fatalf("own credential rejected: %v", err)
	}
	if err := service.validateAgentKeyForServer(context.Background(), db, "agent-a", keyB); err == nil {
		t.Fatal("credential from another server must be rejected")
	}
}

func TestProxyTrafficReportsAreIdempotent(t *testing.T) {
	service, db := testService(t)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('proxy-agent', 'proxy-agent', '0.0.0.0', 'agent', 'password')`); err != nil {
		t.Fatal(err)
	}
	key, err := service.getOrGenerateAgentKeyForServer(context.Background(), db, "proxy-agent")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"boot_id":"boot-1","sequence":1,"node_id":"node-1","upload_bytes":100,"download_bytes":200}`
	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/server/agent/proxy/proxy-agent/traffic", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		service.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, res.Code, res.Body.String())
		}
	}
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM server_proxy_traffic_reports WHERE server_id = 'proxy-agent'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("traffic report count=%d, want 1", count)
	}
}

func TestProxyTrafficHTTPBatchValidatesScopeAndQuota(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	if err := subscriptionservice.New(service.cfg).Initialize(ctx); err != nil {
		t.Fatalf("initialize subscription schema: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES ('traffic-host','traffic-host','192.0.2.40','agent','password'),('other-host','other-host','192.0.2.41','agent','password')`,
		`INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,revision,enabled,publishable,apply_status,stats_port) VALUES ('traffic-node','traffic-host','traffic-node','vless-reality','sing-box','192.0.2.40',45654,'tcp','{}','',1,1,1,'running',21000),('other-node','other-host','other-node','vless-reality','sing-box','192.0.2.41',45655,'tcp','{}','',1,1,1,'running',21001)`,
		`INSERT INTO subscription_plans (id,name,enabled,total_bytes,cycle_type,cycle_day,selection_mode,include_internal_nodes,include_external_nodes) VALUES ('traffic-plan','Traffic plan',1,100,'monthly',1,'explicit',1,0)`,
		`INSERT INTO subscription_subscriptions (id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled,created_at) VALUES ('traffic-sub','traffic-sub','traffic-plan','Traffic subscription','public-token','11111111-1111-4111-8111-111111111111','traffic-password',1,'2026-01-01 00:00:00')`,
		`INSERT INTO subscription_plan_nodes (plan_id,node_id,source) VALUES ('traffic-plan','traffic-node','internal')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("fixture %q: %v", statement, err)
		}
	}
	key, err := service.getOrGenerateAgentKeyForServer(ctx, db, "traffic-host")
	if err != nil {
		t.Fatal(err)
	}
	post := func(serverID, body string) (int, map[string]interface{}) {
		req := httptest.NewRequest(http.MethodPost, "/api/server/agent/proxy/"+serverID+"/traffic", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		service.ServeHTTP(res, req)
		return res.Code, decodePayload(t, res)
	}
	status, payload := post("traffic-host", `{"boot_id":"boot-batch","sequence":1,"reports":[{"node_id":"traffic-node","credential_id":"traffic-sub","upload_bytes":80,"download_bytes":50}]}`)
	if status != http.StatusOK || payload["data"].(map[string]interface{})["accepted"] != float64(1) {
		t.Fatalf("accepted batch status=%d payload=%#v", status, payload)
	}
	usage, err := subscriptionledger.Current(ctx, db, "traffic-sub", "monthly", 1, "2026-01-01 00:00:00", fixedNow)
	if err != nil || usage.UploadBytes != 80 || usage.DownloadBytes != 20 {
		t.Fatalf("clamped usage=%#v err=%v", usage, err)
	}
	status, payload = post("traffic-host", `{"boot_id":"boot-batch","sequence":1,"reports":[{"node_id":"traffic-node","credential_id":"11111111-1111-4111-8111-111111111111","upload_bytes":1,"download_bytes":1}]}`)
	data := payload["data"].(map[string]interface{})
	if status != http.StatusOK || data["duplicates"] != float64(1) {
		t.Fatalf("canonical duplicate status=%d payload=%#v", status, payload)
	}
	status, payload = post("traffic-host", `{"boot_id":"boot-batch","sequence":2,"reports":[{"node_id":"traffic-node","credential_id":"traffic-password","upload_bytes":1,"download_bytes":1}]}`)
	data = payload["data"].(map[string]interface{})
	if status != http.StatusOK || data["ignored"] != float64(1) {
		t.Fatalf("exhausted report status=%d payload=%#v", status, payload)
	}
	status, payload = post("traffic-host", `{"boot_id":"boot-stale","sequence":1,"reports":[{"node_id":"removed-node","credential_id":"traffic-sub","upload_bytes":1,"download_bytes":1}]}`)
	data = payload["data"].(map[string]interface{})
	if status != http.StatusOK || data["ignored"] != float64(1) {
		t.Fatalf("stale report status=%d payload=%#v", status, payload)
	}
	status, _ = post("other-host", `{"boot_id":"boot-cross","sequence":1,"reports":[{"node_id":"traffic-node","credential_id":"traffic-sub","upload_bytes":1,"download_bytes":1}]}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("cross-server credential status=%d, want unauthorized", status)
	}
	otherKey, err := service.getOrGenerateAgentKeyForServer(ctx, db, "other-host")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/proxy/other-host/traffic", strings.NewReader(`{"boot_id":"boot-cross","sequence":1,"reports":[{"node_id":"traffic-node","credential_id":"traffic-sub","upload_bytes":1,"download_bytes":1}]}`))
	req.Header.Set("Authorization", "Bearer "+otherKey)
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "does not belong") {
		t.Fatalf("cross-server node status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestManagedProxyBindingUsesHighPortsAndProtocolTransport(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantPort  int
		transport string
	}{
		{"vless default allocation", `{"inbounds":[{"port":443,"protocol":"vless"}]}`, 0, "tcp"},
		{"vless retained high port", `{"inbounds":[{"port":50001,"protocol":"vless"}]}`, 50001, "tcp"},
		{"hysteria2 udp", `{"inbounds":[{"listen_port":443,"type":"hysteria2"}]}`, 0, "udp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port, transport, err := managedProxyBinding(test.config)
			if err != nil {
				t.Fatal(err)
			}
			if port != test.wantPort || transport != test.transport {
				t.Fatalf("binding=(%d,%s), want=(%d,%s)", port, transport, test.wantPort, test.transport)
			}
		})
	}
}

func TestManagedProxyBindingRejectsMultipleInbounds(t *testing.T) {
	if _, _, err := managedProxyBinding(`{"inbounds":[{},{}]}`); err == nil {
		t.Fatal("multiple inbounds should be rejected because one managed node owns one port")
	}
}

func TestManagedProxyNodesRouteIsNotCapturedAsLegacyServerID(t *testing.T) {
	service, _ := testService(t)
	res := perform(service, http.MethodGet, "/api/server/agent/proxy/nodes", "")
	if res.Code != http.StatusOK {
		t.Fatalf("managed node list route status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	if _, ok := payload["data"].([]interface{}); !ok {
		t.Fatalf("managed node list should return an array: %#v", payload)
	}
}

func TestManagedProxyNodeListIncludesHostNetworkQuality(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	statements := []string{
		`INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES ('quality-host','东京','192.0.2.90','agent','password')`,
		`INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status) VALUES ('quality-node','quality-host','东京节点','vless-reality','sing-box','192.0.2.90',45654,'tcp','{}','',1,1,'running')`,
		`INSERT INTO server_network_quality_samples (server_id,target_name,target_host,success,latency_ms,checked_at) VALUES ('quality-host','联通','cu.example',1,80,datetime('now','-5 minutes'))`,
		`INSERT INTO server_network_quality_samples (server_id,target_name,target_host,success,latency_ms,checked_at) VALUES ('quality-host','联通','cu.example',1,120,datetime('now','-10 minutes'))`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	res := perform(service, http.MethodGet, "/api/server/agent/proxy/nodes", "")
	if res.Code != http.StatusOK {
		t.Fatalf("managed node list status=%d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"quality"`) || !strings.Contains(res.Body.String(), `"avg_latency_ms":100`) {
		t.Fatalf("managed node list should include aggregated host quality: %s", res.Body.String())
	}
}

func TestGeneratedRealityNodeHasMihomoCompatibleFields(t *testing.T) {
	config, raw, transport, err := generateManagedNode("mnode-test", "Tokyo", "vless-reality", "edge.example.com", "www.cloudflare.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if transport != "tcp" {
		t.Fatalf("transport=%s", transport)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(config), &root); err != nil {
		t.Fatal(err)
	}
	inbound := root["inbounds"].([]interface{})[0].(map[string]interface{})
	reality := inbound["tls"].(map[string]interface{})["reality"].(map[string]interface{})
	shortIDs := reality["short_id"].([]interface{})
	if len(shortIDs) != 1 || len(shortIDs[0].(string)) != 8 {
		t.Fatalf("invalid short IDs: %#v", shortIDs)
	}
	if !strings.Contains(raw, "security=reality") || !strings.Contains(raw, "flow=xtls-rprx-vision") || !strings.Contains(raw, "pbk=") || !strings.Contains(raw, "sid=") {
		t.Fatalf("invalid client URI: %s", raw)
	}
}

func TestGeneratedSocksNodeHasPlainUsernamePassword(t *testing.T) {
	config, raw, transport, err := generateManagedNode("mnode-socks", "LA SOCKS", "socks", "edge.example.com", "www.cloudflare.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if transport != "tcp" {
		t.Fatalf("transport=%s", transport)
	}
	if !strings.HasPrefix(raw, "socks://") || !strings.Contains(raw, "@edge.example.com:0#") {
		t.Fatalf("invalid client URI: %s", raw)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(config), &root); err != nil {
		t.Fatal(err)
	}
	inbound := root["inbounds"].([]interface{})[0].(map[string]interface{})
	if inbound["type"] != "socks" {
		t.Fatalf("inbound type=%v", inbound["type"])
	}
	users := inbound["users"].([]interface{})
	user := users[0].(map[string]interface{})
	if strings.TrimSpace(user["username"].(string)) == "" || strings.TrimSpace(user["password"].(string)) == "" {
		t.Fatalf("missing plain credential: %#v", user)
	}
}

func TestGeneratedHTTPNodeHasPlainUsernamePassword(t *testing.T) {
	config, raw, transport, err := generateManagedNode("mnode-http", "Tokyo HTTP", "http", "edge.example.com", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if transport != "tcp" {
		t.Fatalf("transport=%s", transport)
	}
	if !strings.HasPrefix(raw, "http://") || !strings.Contains(raw, "@edge.example.com:0#") {
		t.Fatalf("invalid client URI: %s", raw)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(config), &root); err != nil {
		t.Fatal(err)
	}
	inbound := root["inbounds"].([]interface{})[0].(map[string]interface{})
	if inbound["type"] != "http" {
		t.Fatalf("inbound type=%v", inbound["type"])
	}
}

func TestFailedManagedProxyNodeCanBeDeletedWithoutAgentCleanup(t *testing.T) {
	service, db := testService(t)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES ('failed-proxy-host','Debian 11','192.0.2.10','agent','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,revision,enabled,publishable,apply_status,last_error) VALUES ('failed-node','failed-proxy-host','JP VLESS','vless-reality','sing-box','192.0.2.10',0,'tcp','{}','vless://example',1,1,0,'failed','unsupported managed proxy host: debian 11')`); err != nil {
		t.Fatal(err)
	}

	res := perform(service, http.MethodDelete, "/api/server/agent/proxy/nodes/failed-node", "")
	if res.Code != http.StatusAccepted {
		t.Fatalf("delete failed node status=%d body=%s", res.Code, res.Body.String())
	}
	var count int
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM managed_proxy_nodes WHERE id='failed-node'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 0 {
		t.Fatalf("failed node was not deleted, count=%d", count)
	}
}

func TestManagedProxyNodeListResolvesPreferredAddressWithoutNestedQueryDeadlock(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	encrypted, err := secure.SecureEncrypt("vless://user@origin.example:45654?security=tls#edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO server_accounts (id,name,host,username,auth_type) VALUES ('preferred-host','edge','192.0.2.80','agent','password')`, nil},
		{`INSERT INTO managed_proxy_preferences (id,name,address,port,enabled,is_default) VALUES ('preferred-one','优选','saas.sin.fan',443,1,1)`, nil},
		{`INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status,access_mode) VALUES ('preferred-node','preferred-host','edge','vless-reality','sing-box','origin.example',45654,'tcp','{}',?,1,1,'running','cloudflare_tunnel')`, []interface{}{encrypted}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- perform(service, http.MethodGet, "/api/server/agent/proxy/nodes", "") }()
	select {
	case response := <-result:
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "saas.sin.fan:443") {
			t.Fatalf("node list status=%d body=%s", response.Code, response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("managed node list deadlocked while resolving preferred address")
	}
}

func TestManagedProxyReconcileDoesNotAdvanceRevisionWhileAgentOffline(t *testing.T) {
	service, db := testService(t)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id,name,host,username,auth_type,status) VALUES ('proxy-reconcile-host','edge','192.0.2.11','agent','password','online')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO managed_proxy_nodes (id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,revision,enabled,publishable,apply_status) VALUES ('reconcile-node','proxy-reconcile-host','edge VLESS','vless-reality','sing-box','192.0.2.11',45654,'tcp','{"inbounds":[{"type":"vless","listen_port":45654}]}','vless://example@192.0.2.11:45654#edge',1,1,1,'running')`); err != nil {
		t.Fatal(err)
	}

	res := perform(service, http.MethodPost, "/api/server/agent/proxy/nodes/reconcile-node/reconcile", "")
	if res.Code != http.StatusBadGateway {
		t.Fatalf("offline reconcile status=%d body=%s", res.Code, res.Body.String())
	}
	var revision int
	if err := db.QueryRowContext(context.Background(), `SELECT revision FROM managed_proxy_nodes WHERE id='reconcile-node'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("stored revision=%d, want 1", revision)
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
	if !strings.Contains(res.Body.String(), `TMP_AGENT="$(mktemp /tmp/api-monitor-agent.XXXXXX)"`) ||
		!strings.Contains(res.Body.String(), `if [ "$(id -u)" -eq 0 ]; then`) ||
		!strings.Contains(res.Body.String(), `$SUDO install -m 0755 "$TMP_AGENT" "$INSTALL_DIR/api-monitor-agent"`) {
		t.Fatalf("linux install script should download to a temp file before replacing binary: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "systemctl is-active --quiet api-monitor-agent") ||
		!strings.Contains(res.Body.String(), "journalctl -u api-monitor-agent -n 50 --no-pager") {
		t.Fatalf("linux install script should verify service start and print logs on failure: %s", res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/agent/install/linux/"+serverID+"/bad-key", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("bad keyed linux install status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/server/agent/install/linux/"+serverID, "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("keyless linux install must not exist: status=%d body=%s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "AGENT_KEY=") {
		t.Fatalf("keyless linux install must not leak agent key: %s", res.Body.String())
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
	if !strings.Contains(resLinux.Body.String(), `TARGET_HOST_NAME="edge-agent"`) || !strings.Contains(resLinux.Body.String(), `cat >/dev/null || true`) {
		t.Fatalf("linux installer should preserve the host label and drain its curl pipe after detached upgrade: %s", resLinux.Body.String())
	}
	if strings.Contains(resLinux.Body.String(), "Debian 12+ is required") || strings.Contains(resLinux.Body.String(), "unsupported managed host distribution") {
		t.Fatalf("agent installer must not impose distribution-version policy: %s", resLinux.Body.String())
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
	if !strings.Contains(resWin.Body.String(), `$TEMP_AGENT_PATH = "$INSTALL_DIR\api-monitor-agent.download.exe"`) {
		t.Fatalf("windows install script should use a temp download path: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `$CONFIG_PATH = "$INSTALL_DIR\config.json"`) ||
		!strings.Contains(resWin.Body.String(), `Remove-Item -Path $CONFIG_PATH -Force`) ||
		!strings.Contains(resWin.Body.String(), `"serverUrl": "$SERVER_URL"`) ||
		!strings.Contains(resWin.Body.String(), `"serverId": "$SERVER_ID"`) {
		t.Fatalf("windows install script should replace stale agent config: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `API_MONITOR_AGENT_INSTALL_DETACHED`) ||
		!strings.Contains(resWin.Body.String(), `-EncodedCommand`) ||
		!strings.Contains(resWin.Body.String(), `installation will continue in the background`) {
		t.Fatalf("windows install script should detach before replacing a running Agent: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `Move-Item -Path $TEMP_AGENT_PATH -Destination $AGENT_PATH -Force`) {
		t.Fatalf("windows install script should atomically replace the agent binary: %s", resWin.Body.String())
	}
	if !strings.Contains(resWin.Body.String(), `$DOWNLOADED_VERSION = (& $TEMP_AGENT_PATH --version 2>&1 | Out-String).Trim()`) ||
		!strings.Contains(resWin.Body.String(), `Agent download completed: $DOWNLOADED_VERSION`) {
		t.Fatalf("windows install script should validate and report the downloaded agent version: %s", resWin.Body.String())
	}
	if strings.ContainsRune(resWin.Body.String(), '\a') {
		t.Fatalf("windows install script should not contain bell characters: %q", resWin.Body.String())
	}

	_, err = db.ExecContext(context.Background(), `UPDATE user_settings SET agent_download_url = 'https://cdn.example.com/custom-agent' WHERE id = 1`)
	if err != nil {
		t.Fatalf("set custom agent download url: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/server/agent/install/linux/"+serverID+"/"+agentKey+"?protocol=http", nil)
	req.Host = "189.1.217.109:3010"
	resLinux = httptest.NewRecorder()
	service.ServeHTTP(resLinux, req)
	if resLinux.Code != http.StatusOK {
		t.Fatalf("custom linux install status=%d body=%s", resLinux.Code, resLinux.Body.String())
	}
	if !strings.Contains(resLinux.Body.String(), `AGENT_DOWNLOAD_BASE_URL="https://cdn.example.com/custom-agent"`) ||
		!strings.Contains(resLinux.Body.String(), `AGENT_URL="$AGENT_DOWNLOAD_BASE_URL/agent-linux-$AGENT_ARCH"`) ||
		!strings.Contains(resLinux.Body.String(), "systemd-run") {
		t.Fatalf("linux install script should use custom download base and detached systemd install: %s", resLinux.Body.String())
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

func TestAgentUpgradeTaskUsesSelfUpdateDownloadURL(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `UPDATE user_settings SET agent_download_url = 'https://cdn.example.com/agent' WHERE id = 1`)
	if err != nil {
		t.Fatalf("set custom agent download url: %v", err)
	}

	socket := &terminalCaptureSocket{t: t, service: service}
	conn := service.registry.Register("upgrade-agent", socket)
	conn.SetMetadata("platform", "linux")
	conn.SetMetadata("arch", "arm64")

	downloadURL := service.agentUpgradeDownloadURL(context.Background(), db, conn, agentInstallOrigin{Proto: "https", Host: "panel.example.com"})
	if downloadURL != "https://cdn.example.com/agent/agent-linux-arm64" {
		t.Fatalf("downloadURL = %q", downloadURL)
	}
	if !service.sendUpgradeTask(conn, downloadURL) {
		t.Fatal("sendUpgradeTask failed")
	}

	events := socket.Events()
	if len(events) != 1 || events[0].Name != "dashboard:task" {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Data["type"] != float64(5) {
		t.Fatalf("task type = %#v, want 5", events[0].Data["type"])
	}
	var data map[string]string
	rawData, _ := events[0].Data["data"].(string)
	if err := json.Unmarshal([]byte(rawData), &data); err != nil {
		t.Fatalf("decode upgrade task data: %v raw=%q", err, rawData)
	}
	if data["download_url"] != downloadURL {
		t.Fatalf("upgrade task download_url = %#v, want %q", data, downloadURL)
	}
}

func TestAgentBatchUpgradeUsesServerSideBatchAndVerifiesReconnect(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/agent/quick-install", `{"name":"batch-agent"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quick install status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	serverID := payload["data"].(map[string]interface{})["serverId"].(string)

	socket := &reconnectOnUpgradeSocket{t: t, service: service, serverID: serverID}
	service.registry.Register(serverID, socket)

	res = perform(service, http.MethodPost, "/api/server/agent/batch-upgrade?protocol=http", `{"serverIds":["`+serverID+`"],"concurrency":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("batch upgrade status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	if payload["success"] != true {
		t.Fatalf("batch upgrade payload=%#v", payload)
	}
	batchID := payload["data"].(map[string]interface{})["id"].(string)
	if batchID == "" {
		t.Fatalf("missing batch id: %#v", payload)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		service.registry.Register(serverID, &reconnectOnUpgradeSocket{t: t, service: service, serverID: serverID})
	}()

	deadline := time.Now().Add(4 * time.Second)
	var lastPayload map[string]interface{}
	for time.Now().Before(deadline) {
		res = perform(service, http.MethodGet, "/api/server/agent/batch/"+batchID, "")
		if res.Code != http.StatusOK {
			t.Fatalf("batch status code=%d body=%s", res.Code, res.Body.String())
		}
		payload = decodePayload(t, res)
		lastPayload = payload
		data := payload["data"].(map[string]interface{})
		if data["status"] == string(AgentBatchSucceeded) {
			items := data["items"].([]interface{})
			if len(items) != 1 || items[0].(map[string]interface{})["status"] != string(AgentBatchSucceeded) {
				t.Fatalf("unexpected completed items: %#v", items)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("batch did not complete in time, last payload=%#v", lastPayload)
}

func TestAgentBatchUpgradeNeverForcesOnlineWindowsAgentThroughSSH(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/agent/quick-install", `{"name":"windows-agent"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quick install status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	serverID := payload["data"].(map[string]interface{})["serverId"].(string)

	socket := &reconnectOnUpgradeSocket{t: t, service: service, serverID: serverID}
	connection := service.registry.Register(serverID, socket)
	connection.SetMetadata("platform", "windows")
	connection.SetMetadata("arch", "amd64")

	res = perform(service, http.MethodPost, "/api/server/agent/batch-upgrade?protocol=http", `{"serverIds":["`+serverID+`"],"force_ssh":true,"fallback_ssh":true,"concurrency":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("batch upgrade status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	batchID := payload["data"].(map[string]interface{})["id"].(string)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		res = perform(service, http.MethodGet, "/api/server/agent/batch/"+batchID, "")
		payload = decodePayload(t, res)
		data := payload["data"].(map[string]interface{})
		if data["status"] == string(AgentBatchSucceeded) {
			items := data["items"].([]interface{})
			item := items[0].(map[string]interface{})
			if item["status"] != string(AgentBatchSucceeded) {
				t.Fatalf("unexpected completed item: %#v", item)
			}
			logs, _ := item["log"].([]interface{})
			for _, rawLine := range logs {
				line, _ := rawLine.(string)
				if strings.Contains(line, "SSH") {
					t.Fatalf("online Windows Agent must not use SSH: %#v", logs)
				}
			}
			return
		}
		if data["status"] == string(AgentBatchFailed) {
			t.Fatalf("Windows Agent upgrade failed: %#v", data)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Windows Agent batch upgrade did not complete")
}

func TestAgentBatchUpgradeVerificationTimeoutMarksItemFailed(t *testing.T) {
	t.Setenv("API_MONITOR_AGENT_UPGRADE_VERIFY_TIMEOUT_MS", "40")
	t.Setenv("API_MONITOR_AGENT_UPGRADE_ACK_TIMEOUT_MS", "40")

	service, _ := testService(t)

	res := perform(service, http.MethodPost, "/api/server/agent/quick-install", `{"name":"timeout-agent"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("quick install status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	serverID := payload["data"].(map[string]interface{})["serverId"].(string)

	service.registry.Register(serverID, &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			if taskType != 5 {
				t.Fatalf("task type = %d, want 5", taskType)
			}
			if !strings.Contains(data, "download_url") {
				t.Fatalf("upgrade task should include download_url, got %s", data)
			}
			return "scheduled"
		},
	})

	res = perform(service, http.MethodPost, "/api/server/agent/batch-upgrade?protocol=http", `{"serverIds":["`+serverID+`"],"concurrency":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("batch upgrade status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	batchID := payload["data"].(map[string]interface{})["id"].(string)

	deadline := time.Now().Add(2 * time.Second)
	var lastPayload map[string]interface{}
	for time.Now().Before(deadline) {
		res = perform(service, http.MethodGet, "/api/server/agent/batch/"+batchID, "")
		if res.Code != http.StatusOK {
			t.Fatalf("batch status code=%d body=%s", res.Code, res.Body.String())
		}
		payload = decodePayload(t, res)
		lastPayload = payload
		data := payload["data"].(map[string]interface{})
		if data["status"] == string(AgentBatchFailed) {
			items := data["items"].([]interface{})
			if len(items) != 1 {
				t.Fatalf("unexpected items: %#v", items)
			}
			item := items[0].(map[string]interface{})
			if item["status"] != string(AgentBatchFailed) {
				t.Fatalf("item should fail after verify timeout: %#v", item)
			}
			if !strings.Contains(item["error"].(string), "验证超时") {
				t.Fatalf("item error should mention verify timeout: %#v", item)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("batch did not fail in time, last payload=%#v", lastPayload)
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

func TestSFTPListUsesAgentTunnelWhenOnlineWithoutSSHCredentials(t *testing.T) {
	service, db := testService(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type, cached_info) VALUES ('server-agent-file', 'agent-file', '', '', 'password', '{}')`)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	service.registry.Register("server-agent-file", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			if taskType != agentFileListTask {
				t.Fatalf("task type = %d, want %d", taskType, agentFileListTask)
			}
			if !strings.Contains(data, `"path":"."`) {
				t.Fatalf("task data should include normalized path, got %s", data)
			}
			return `{"cwd":"/home/agent","files":[{"name":"app.log","path":"/home/agent/app.log","isDirectory":false,"isFile":true,"size":12,"mode":420,"mtime":1000,"permissions":"-rw-r--r--"}]}`
		},
	})

	res := perform(service, http.MethodPost, "/api/server/sftp/list", `{"serverId":"server-agent-file","path":"."}`)
	if res.Code != http.StatusOK {
		t.Fatalf("agent sftp list status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	if payload["success"] != true || payload["transport"] != "agent" || payload["path"] != "/home/agent" {
		t.Fatalf("unexpected agent file payload=%#v", payload)
	}
	files := payload["files"].([]interface{})
	if len(files) != 1 || files[0].(map[string]interface{})["name"] != "app.log" {
		t.Fatalf("unexpected files=%#v", files)
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

func TestBuildCachedInfoIncludesRawNetworkTotals(t *testing.T) {
	service := &Service{}
	cached := service.buildCachedInfo(
		map[string]interface{}{
			"net_in_speed":     float64(1024),
			"net_out_speed":    float64(2048),
			"net_in_transfer":  float64(10 * 1024 * 1024),
			"net_out_transfer": float64(20 * 1024 * 1024),
		},
		map[string]interface{}{},
	)

	network := cached["network"].(map[string]interface{})
	if network["rx_total_bytes"] != int64(10*1024*1024) || network["tx_total_bytes"] != int64(20*1024*1024) {
		t.Fatalf("raw network totals missing: %#v", network)
	}
}

func TestTrafficQuotaAlertUsesConfiguredLimit(t *testing.T) {
	service, db := testService(t)
	notifier := &recordingNotifier{}
	service.SetNotifier(notifier)

	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (
		id, name, host, username, auth_type, traffic_limit_bytes, traffic_alert_enabled, traffic_alert_percent, cached_info
	) VALUES (
		'quota-server', 'quota', '127.0.0.1', 'root', 'password', 1000, 1, 100,
		'{"net_in_transfer":600,"net_out_transfer":500}'
	)`)
	if err != nil {
		t.Fatalf("insert quota server: %v", err)
	}

	service.checkMetricAlerts(context.Background(), db, "quota-server", 1, 1, 1)
	events, data := notifier.snapshot()
	if len(events) != 1 || events[0] != "traffic_high" {
		t.Fatalf("events after high = %#v", events)
	}
	if data[0]["traffic_used_bytes"] != int64(1100) || data[0]["traffic_limit_bytes"] != int64(1000) {
		t.Fatalf("traffic alert data = %#v", data[0])
	}

	service.checkMetricAlerts(context.Background(), db, "quota-server", 1, 1, 1)
	events, _ = notifier.snapshot()
	if len(events) != 1 {
		t.Fatalf("traffic_high should not repeat while still high: %#v", events)
	}

	_, err = db.ExecContext(context.Background(), `UPDATE server_accounts SET cached_info = '{"net_in_transfer":400,"net_out_transfer":400}' WHERE id = 'quota-server'`)
	if err != nil {
		t.Fatalf("lower traffic usage: %v", err)
	}
	service.checkMetricAlerts(context.Background(), db, "quota-server", 1, 1, 1)
	events, _ = notifier.snapshot()
	if len(events) != 2 || events[1] != "traffic_normal" {
		t.Fatalf("events after normal = %#v", events)
	}
}

func TestTrafficAlertTestRouteTriggersNotification(t *testing.T) {
	service, db := testService(t)
	notifier := &recordingNotifier{}
	service.SetNotifier(notifier)

	_, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (
		id, name, host, username, auth_type, traffic_limit_bytes, traffic_alert_enabled, traffic_alert_percent, cached_info
	) VALUES (
		'quota-route-server', 'quota route', '127.0.0.1', 'root', 'password', 1000, 1, 80,
		'{"net_in_transfer":600,"net_out_transfer":250}'
	)`)
	if err != nil {
		t.Fatalf("insert quota route server: %v", err)
	}

	res := perform(service, http.MethodPost, "/api/server/accounts/quota-route-server/test-traffic-alert", `{"traffic_alert_percent":80}`)
	if res.Code != http.StatusOK {
		t.Fatalf("test traffic alert route status = %d body=%s", res.Code, res.Body.String())
	}
	events, data := notifier.snapshot()
	if len(events) != 1 || events[0] != "traffic_high" {
		t.Fatalf("events = %#v", events)
	}
	if data[0]["threshold"] != "80.00%" || data[0]["traffic_used"] == "" {
		t.Fatalf("traffic test data = %#v", data[0])
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

func TestPublicPageIconID(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO server_status_pages (slug, domain, title, public, cache_seconds, config_json, server_ids_json)
		VALUES ('fav-slug', 'fav.example.com', 'Fav Test', 1, 300, '{"publicIconId":"site-custom"}', '[]')`); err != nil {
		t.Fatalf("insert status page: %v", err)
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

// ---- agent command (POST /api/server/agent/command/{id}) ----

// taskFailedReplySocket 与 taskReplySocket 相同，但以失败状态回执（模拟命令退出码非 0）
type taskFailedReplySocket struct {
	t       *testing.T
	service *Service
	reply   func(taskType int, data string) string
}

func (s *taskFailedReplySocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v frame=%s", err, raw)
	}
	if len(frame) != 2 || frame[0] != "dashboard:task" {
		s.t.Fatalf("unexpected socket event: %#v", frame)
	}
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}
	taskID, _ := payload["id"].(string)
	taskType, _ := payload["type"].(float64)
	taskData, _ := payload["data"].(string)
	result := s.reply(int(taskType), taskData)
	go s.service.taskRegistry.Fail(taskID, result)
	return nil
}

// silentTaskSocket 接收任务下发但从不回执（用于超时测试）
type silentTaskSocket struct{}

func (s *silentTaskSocket) WriteMessage(_ int, _ []byte) error {
	return nil
}

func TestAgentExecCommandSuccess(t *testing.T) {
	service, db := testService(t)
	service.registry.Register("server-1", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		if taskType != 1 {
			t.Fatalf("task type = %d, want RUN_COMMAND(1)", taskType)
		}
		if data != "echo hello" {
			t.Fatalf("task data = %q, want echo hello", data)
		}
		return "hello from agent\n"
	}})

	rec := perform(service, http.MethodPost, "/api/server/agent/command/server-1", `{"command":"echo hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodePayload(t, rec)
	if payload["success"] != true {
		t.Fatalf("success=%v, want true: %s", payload["success"], rec.Body.String())
	}
	if payload["output"] != "hello from agent\n" {
		t.Fatalf("output=%v, want hello from agent", payload["output"])
	}

	// 命令历史应写入 execution_mode=api
	var mode, status string
	if err := db.QueryRow(`SELECT execution_mode, status FROM server_command_history WHERE server_id='server-1' ORDER BY id DESC LIMIT 1`).Scan(&mode, &status); err != nil {
		t.Fatalf("command history not written: %v", err)
	}
	if mode != "api" || status != "success" {
		t.Fatalf("history = (%s, %s), want (api, success)", mode, status)
	}
}

func TestAgentExecCommandFailed(t *testing.T) {
	service, _ := testService(t)
	service.registry.Register("server-1", &taskFailedReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		return "some error output\n"
	}})

	rec := perform(service, http.MethodPost, "/api/server/agent/command/server-1", `{"command":"false"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodePayload(t, rec)
	if payload["success"] != false {
		t.Fatalf("success=%v, want false: %s", payload["success"], rec.Body.String())
	}
	if payload["output"] != "some error output\n" {
		t.Fatalf("output=%v, want error output preserved", payload["output"])
	}
}

func TestAgentExecCommandDangerous(t *testing.T) {
	service, _ := testService(t)
	// 不注册 socket：危险检测必须先于在线检查返回 400
	rec := perform(service, http.MethodPost, "/api/server/agent/command/server-1", `{"command":"rm -rf /tmp/x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodePayload(t, rec)
	if payload["dangerous"] != true {
		t.Fatalf("dangerous=%v, want true: %s", payload["dangerous"], rec.Body.String())
	}
	if len(payload["dangerReasons"].([]interface{})) == 0 {
		t.Fatalf("dangerReasons empty: %s", rec.Body.String())
	}
}

func TestAgentExecCommandOffline(t *testing.T) {
	service, _ := testService(t)
	rec := perform(service, http.MethodPost, "/api/server/agent/command/server-1", `{"command":"echo hi"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentExecCommandTimeout(t *testing.T) {
	service, _ := testService(t)
	service.registry.Register("server-1", &silentTaskSocket{})

	start := time.Now()
	rec := perform(service, http.MethodPost, "/api/server/agent/command/server-1", `{"command":"sleep 100","timeout":1}`)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Fatalf("returned before timeout window elapsed")
	}
	payload := decodePayload(t, rec)
	if payload["task_id"] == "" || payload["task_id"] == nil {
		t.Fatalf("task_id missing in timeout response: %s", rec.Body.String())
	}
}

func TestCreateTaskDangerous(t *testing.T) {
	service, _ := testService(t)
	rec := perform(service, http.MethodPost, "/api/server/tasks", `{"server_id":"server-1","command":"rm -rf /tmp/x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodePayload(t, rec)
	if payload["dangerous"] != true {
		t.Fatalf("dangerous=%v, want true: %s", payload["dangerous"], rec.Body.String())
	}
}

// TestDetectDangerousCommandVariants 覆盖 rm 递归+强制删除的常见变体与误伤场景
func TestDetectDangerousCommandVariants(t *testing.T) {
	shouldBlock := []string{
		"rm -rf /tmp/x",
		"rm -fr /tmp/x",
		"rm -rfv /tmp/x",
		"rm -vfr /tmp/x",
		"rm -rvf /tmp/x",
		"rm -r -f /tmp/x",
		"rm -i -rf /tmp/x",
		"rm --recursive --force /tmp/x",
		"rm --force --recursive /tmp/x",
	}
	shouldAllow := []string{
		"echo hello",
		"rm -f /tmp/single.log",
		"rm -r /tmp/emptydir",
		"rm --recursive /tmp/emptydir",
		"ls -la",
		"systemctl status sshd",
		"uname -a",
	}
	for _, c := range shouldBlock {
		if !DetectDangerousCommand(c).Dangerous {
			t.Errorf("should block: %q", c)
		}
	}
	for _, c := range shouldAllow {
		if DetectDangerousCommand(c).Dangerous {
			t.Errorf("should allow: %q (reasons=%v)", c, DetectDangerousCommand(c).Reasons)
		}
	}
}
