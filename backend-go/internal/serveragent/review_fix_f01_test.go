package serveragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---- A1:广播负载白名单,匿名订阅者拿不到主机标识 ----

func TestBroadcastMetricsPayloadUsesWhitelist(t *testing.T) {
	hub := NewMetricsHub()
	session := &EngineIOSession{
		ID:              "sec-a1",
		Transport:       "polling",
		Namespace:       "/metrics",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.Register(session.ID, session)

	hub.BroadcastMetrics("srv-a1", map[string]interface{}{
		"cpu":           12.5,
		"hostname":      "private-host",
		"ip":            "203.0.113.9",
		"isp":           "Example ISP",
		"org":           "Example Org",
		"asn":           "AS64500",
		"connection_ip": "198.51.100.7",
		"mac":           "00:11:22:33:44:55",
	})

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("pending messages = %#v", session.PendingMessages)
	}
	message := session.PendingMessages[0]
	for _, leaked := range []string{"hostname", "203.0.113.9", "198.51.100.7", "mac", "AS64500", "Example"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("payload leaked %q: %s", leaked, message)
		}
	}
	if !strings.Contains(message, `"cpu":12.5`) {
		t.Fatalf("cpu metric missing from payload: %s", message)
	}
}

// ---- A2:匿名 /metrics 只广播公开状态页可见主机 ----

func TestBroadcastMetricsSkipsNonPublicServers(t *testing.T) {
	hub := NewMetricsHub()
	hub.SetVisibilityFilter(func(serverID string) bool { return serverID == "public-1" })
	session := &EngineIOSession{
		ID:              "sec-a2",
		Transport:       "polling",
		Namespace:       "/metrics",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.Register(session.ID, session)

	hub.BroadcastMetrics("private-1", map[string]interface{}{"cpu": 1.0})
	session.mu.RLock()
	if len(session.PendingMessages) != 0 {
		session.mu.RUnlock()
		t.Fatal("非公开主机不应广播到匿名 /metrics")
	}
	session.mu.RUnlock()

	hub.BroadcastMetrics("public-1", map[string]interface{}{"cpu": 2.0})
	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("公开主机应广播,pending = %#v", session.PendingMessages)
	}
}

func TestIsServerPubliclyVisibleUsesPublicStatusPages(t *testing.T) {
	service, db := testService(t)

	if _, err := db.Exec(`INSERT INTO server_status_pages (slug, title, public, server_ids_json) VALUES ('pub', 'Pub', 1, '["node-public"]')`); err != nil {
		t.Fatalf("insert public page: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO server_status_pages (slug, title, public, server_ids_json) VALUES ('priv', 'Priv', 0, '["node-private"]')`); err != nil {
		t.Fatalf("insert private page: %v", err)
	}

	if !service.isServerPubliclyVisible("node-public") {
		t.Fatal("公开状态页绑定的主机应可见")
	}
	if service.isServerPubliclyVisible("node-private") {
		t.Fatal("非公开状态页绑定的主机不应可见")
	}
	if service.isServerPubliclyVisible("node-unknown") {
		t.Fatal("未绑定任何状态页的主机不应可见")
	}
	if service.isServerPubliclyVisible("") {
		t.Fatal("空 serverID 不应可见")
	}
}

// ---- A3:uptime 心跳白名单,错误只输出归一化错误码 ----

func TestSanitizeUptimeBeatDropsErrorText(t *testing.T) {
	beat := map[string]interface{}{
		"id":        float64(1),
		"status":    0,
		"time":      "2026-01-01T00:00:00Z",
		"msg":       `Get "http://10.0.0.5:8080/health": dial tcp 10.0.0.5:8080: connect: connection refused`,
		"errorCode": "CHECK_FAILED",
		"details": map[string]interface{}{
			"contentLength": 12,
		},
		"monitorState": map[string]interface{}{
			"state":     "down",
			"lastError": `Get "http://10.0.0.5:8080/health": refused`,
			"lastPing":  float64(0),
			"updatedAt": "2026-01-01T00:00:00Z",
		},
		"uptime24h": 0.5,
	}

	out := sanitizeUptimeBeatForBroadcast(beat)
	if _, ok := out["msg"]; ok {
		t.Fatal("msg 必须被剥离")
	}
	if _, ok := out["details"]; ok {
		t.Fatal("details 必须被剥离(非白名单字段)")
	}
	if out["errorCode"] != "CHECK_FAILED" {
		t.Fatalf("errorCode = %#v", out["errorCode"])
	}
	if out["uptime24h"] != 0.5 {
		t.Fatalf("白名单字段 uptime24h 丢失: %#v", out["uptime24h"])
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "10.0.0.5") {
		t.Fatalf("内网地址泄露: %s", encoded)
	}
	state, ok := out["monitorState"].(map[string]interface{})
	if !ok {
		t.Fatalf("monitorState = %#v", out["monitorState"])
	}
	if state["lastError"] != "CHECK_FAILED" {
		t.Fatalf("lastError 应归一化为错误码: %#v", state["lastError"])
	}
}

func TestSanitizeUptimeBeatClearsErrorOnRecovery(t *testing.T) {
	out := sanitizeUptimeBeatForBroadcast(map[string]interface{}{
		"status":    1,
		"msg":       "OK",
		"errorCode": "CHECK_FAILED",
		"monitorState": map[string]interface{}{
			"state":     "up",
			"lastError": nil,
		},
	})
	if _, ok := out["errorCode"]; ok {
		t.Fatalf("恢复上报不应带 errorCode: %#v", out)
	}
	state := out["monitorState"].(map[string]interface{})
	if state["lastError"] != nil {
		t.Fatalf("恢复时 lastError 应为 nil: %#v", state["lastError"])
	}
}

func TestBroadcastUptimeHeartbeatSanitizesRootPayload(t *testing.T) {
	hub := NewMetricsHub()
	session := &EngineIOSession{
		ID:              "root-a3",
		Transport:       "polling",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.RegisterRoot(session.ID, session)

	service := &Service{metricsHub: hub}
	service.BroadcastUptimeHeartbeat(7, map[string]interface{}{
		"status": 0,
		"msg":    `Get "http://192.168.1.10:9000": refused`,
		"monitorState": map[string]interface{}{
			"state":     "down",
			"lastError": "secret-internal-url",
		},
	})

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("pending messages = %#v", session.PendingMessages)
	}
	message := session.PendingMessages[0]
	if !strings.HasPrefix(message, `42["uptime:heartbeat"`) {
		t.Fatalf("unexpected event: %s", message)
	}
	if strings.Contains(message, "192.168.1.10") || strings.Contains(message, "secret-internal-url") {
		t.Fatalf("root 心跳泄露错误文本: %s", message)
	}
}

// ---- C4:重升级到已有 sid 必须校验归属并重置鉴权 ----

func newEngineWithSession(t *testing.T, session *EngineIOSession) *EngineIOServer {
	t.Helper()
	engine := NewEngineIOServer(NewConnectionRegistry())
	engine.sessions[session.ID] = session
	return engine
}

func TestHandleWebSocketRejectsReUpgradeFromDifferentIP(t *testing.T) {
	session := &EngineIOSession{
		ID:            "victim-ip",
		Transport:     "polling",
		RemoteIP:      "203.0.113.50",
		Authenticated: true,
		ServerID:      "srv-victim",
		LastActivity:  time.Now(),
	}
	engine := newEngineWithSession(t, session)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket&sid=victim-ip"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		conn.Close()
		t.Fatal("来自不同 IP 的重升级必须被拒绝")
	}
}

func TestHandleWebSocketRejectsReUpgradeFromDifferentUserAgent(t *testing.T) {
	session := &EngineIOSession{
		ID:            "victim-ua",
		Transport:     "polling",
		UserAgent:     "expected-agent/1.0",
		Authenticated: true,
		ServerID:      "srv-victim",
		LastActivity:  time.Now(),
	}
	engine := newEngineWithSession(t, session)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket&sid=victim-ua"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"User-Agent": []string{"intruder/9"}})
	if err == nil {
		conn.Close()
		t.Fatal("User-Agent 不一致的重升级必须被拒绝")
	}
}

func TestHandleWebSocketResetsAuthOnReUpgrade(t *testing.T) {
	session := &EngineIOSession{
		ID:              "reupgrade",
		Transport:       "polling",
		RemoteIP:        "127.0.0.1",
		Authenticated:   true,
		ServerID:        "srv-old",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	engine := newEngineWithSession(t, session)
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket&sid=reupgrade"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("同源重升级应被接受: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		session.mu.RLock()
		auth := session.Authenticated
		serverID := session.ServerID
		session.mu.RUnlock()
		if !auth && serverID == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("重升级后必须重置鉴权: authenticated=%v serverID=%q", auth, serverID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---- C5:disconnectLocked 必须能关闭 *EngineIOSession 的底层 socket ----

func TestConnectionRegistryDisconnectClosesEngineIOSession(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConns := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConns <- conn
	}))
	defer server.Close()

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	serverConn := <-serverConns

	registry := NewConnectionRegistry()
	defer registry.Stop()
	registry.Register("srv-c5", &EngineIOSession{ID: "sess-c5", Transport: "websocket", wsConn: serverConn})

	registry.Disconnect("srv-c5")

	if _, ok := registry.Get("srv-c5"); ok {
		t.Fatal("Disconnect 后连接应从注册表移除")
	}
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := client.ReadMessage(); err == nil {
		t.Fatal("Disconnect 必须关闭 EngineIOSession 的底层 WebSocket")
	}
}

// ---- C6:清理过期会话时 onDisconnect 必须能看到 session ----

func TestCleanupStaleSessionsInvokesDisconnectWithSessionPresent(t *testing.T) {
	engine := NewEngineIOServer(NewConnectionRegistry())
	session := &EngineIOSession{
		ID:              "stale-c6",
		Transport:       "polling",
		ServerID:        "srv-c6",
		PendingMessages: []string{},
		LastActivity:    time.Now().Add(-2 * time.Minute),
	}
	engine.sessions["stale-c6"] = session

	var seenServerID string
	var seenFound bool
	engine.SetHandlers(nil, nil, func(sessionID string) {
		if s := engine.getSession(sessionID); s != nil {
			s.mu.RLock()
			seenServerID = s.ServerID
			s.mu.RUnlock()
			seenFound = true
		}
	})

	engine.cleanupStaleSessions(time.Now())

	if !seenFound {
		t.Fatal("onDisconnect 回调查不到 session(先 delete 的旧问题)")
	}
	if seenServerID != "srv-c6" {
		t.Fatalf("onDisconnect 读到的 serverID = %q", seenServerID)
	}
	if engine.getSession("stale-c6") != nil {
		t.Fatal("过期会话应被移除")
	}
}

// ---- C7:告警检查必须能在调用方 ctx 取消 / db 关闭后仍触发 ----

func TestCheckMetricAlertsSurvivesCanceledContextAndClosedDB(t *testing.T) {
	service, db := testService(t)
	notifier := &recordingNotifier{}
	service.SetNotifier(notifier)

	if _, err := db.ExecContext(context.Background(), `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('alert-c7', 'c7', '127.0.0.1', 'root', 'password')`); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = db.Close()

	service.checkMetricAlerts(ctx, db, "alert-c7", 95, 95, 95)

	events, _ := notifier.snapshot()
	if len(events) != 3 {
		t.Fatalf("期望 cpu/memory/disk 三条告警,实际 %#v", events)
	}
}
