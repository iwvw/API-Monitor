package serveragent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestMetricsHubBroadcastRemovesDeadClient 验证：客户端断开后广播不会
// 永久阻塞，且死连接会被清理出注册表（慢/死客户端不得拖垮广播）。
func TestMetricsHubBroadcastRemovesDeadClient(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		<-time.After(30 * time.Second)
		_ = conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	hub := NewMetricsHub()
	clientConn := conn // server side of the pair is inside the handler; the dialed conn is the "browser"
	_ = clientConn

	// 为了让 server 侧拿到可写的连接，这里换一种方式：用 hub 直接持有
	// 服务端升级后的连接。httptest handler 无法把 conn 传出，改用内存对：
	// 注册一个「已关闭的挂起会话」，广播必须能清理它而不是永久阻塞。
	session := &EngineIOSession{ID: "dead-client"}
	session.mu.Lock()
	session.wsConn = clientConn
	session.mu.Unlock()
	hub.Register("dead-client", session)

	clientConn.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			hub.BroadcastMetrics("server-1", map[string]interface{}{"cpu": i})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("broadcast to dead client must not block forever")
	}

	if hub.ClientCount() != 0 {
		t.Fatalf("dead client should be evicted, clients=%d", hub.ClientCount())
	}
}

// TestMetricsHubBroadcastStripsSensitiveFields 验证：广播到浏览器命名空间的
// 负载必须剥离 hostname/ip/isp/org/asn 等敏感标识（匿名可订阅），且不修改
// 调用方持有的原始指标数据。
func TestMetricsHubBroadcastStripsSensitiveFields(t *testing.T) {
	hub := NewMetricsHub()
	session := &EngineIOSession{
		ID:              "browser-client",
		Transport:       "polling",
		Namespace:       "/metrics",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.Register(session.ID, session)

	metrics := map[string]interface{}{
		"cpu":      42.5,
		"hostname": "secret-host",
		"ip":       "203.0.113.7",
		"network": map[string]interface{}{
			"rx_speed": "1 MB/s",
			"ip":       "198.51.100.9",
		},
		"docker": []interface{}{
			map[string]interface{}{"name": "web", "ip": "10.0.0.2"},
		},
	}
	hub.BroadcastMetrics("srv-1", metrics)

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("pending messages = %#v", session.PendingMessages)
	}
	message := session.PendingMessages[0]
	for _, banned := range []string{"secret-host", "203.0.113.7", "198.51.100.9", "10.0.0.2", "hostname", "\"ip\""} {
		if strings.Contains(message, banned) {
			t.Fatalf("broadcast leaked sensitive field %q: %s", banned, message)
		}
	}
	if !strings.Contains(message, `"cpu":42.5`) || !strings.Contains(message, "rx_speed") {
		t.Fatalf("broadcast lost normal metrics fields: %s", message)
	}

	// 原始 metrics 不允许被广播路径修改（仍需用于落库与内部缓存）。
	if metrics["hostname"] != "secret-host" || metrics["ip"] != "203.0.113.7" {
		t.Fatalf("original metrics map was mutated: %#v", metrics)
	}
	nested := metrics["network"].(map[string]interface{})
	if nested["ip"] != "198.51.100.9" {
		t.Fatalf("original nested map was mutated: %#v", nested)
	}
}

// TestMetricsHubRootEventStripsSensitiveFields 验证 root 命名空间广播
// （uptime:heartbeat）同样剥离敏感标识，包括嵌套在 beat.details 内的字段。
func TestMetricsHubRootEventStripsSensitiveFields(t *testing.T) {
	hub := NewMetricsHub()
	session := &EngineIOSession{
		ID:              "root-browser-client",
		Transport:       "polling",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.RegisterRoot(session.ID, session)

	hub.BroadcastRootEvent("uptime:heartbeat", map[string]interface{}{
		"monitorId": float64(3),
		"beat": map[string]interface{}{
			"status":  float64(1),
			"details": map[string]interface{}{"ip": "192.0.2.44", "payload": "ok"},
		},
	})

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("pending messages = %#v", session.PendingMessages)
	}
	message := session.PendingMessages[0]
	if strings.Contains(message, "192.0.2.44") || strings.Contains(message, "\"ip\"") {
		t.Fatalf("root broadcast leaked sensitive field: %s", message)
	}
	if !strings.Contains(message, "uptime:heartbeat") || !strings.Contains(message, "payload") {
		t.Fatalf("root broadcast lost normal fields: %s", message)
	}
}
