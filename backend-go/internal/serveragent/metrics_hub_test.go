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