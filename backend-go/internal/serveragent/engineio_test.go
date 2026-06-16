package serveragent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEngineIOPollingHandshakeAndWebSocketUpgrade(t *testing.T) {
	engine := NewEngineIOServer(NewConnectionRegistry())
	server := httptest.NewServer(engine)
	defer server.Close()

	res, err := http.Get(server.URL + "/socket.io/?EIO=4&transport=polling")
	if err != nil {
		t.Fatalf("polling handshake: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("handshake status = %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(raw)
	if !strings.HasPrefix(payload, "0") {
		t.Fatalf("handshake payload = %q", payload)
	}
	var open struct {
		SID      string   `json:"sid"`
		Upgrades []string `json:"upgrades"`
	}
	if err := json.Unmarshal([]byte(payload[1:]), &open); err != nil {
		t.Fatalf("decode open packet: %v", err)
	}
	if open.SID == "" || len(open.Upgrades) == 0 || open.Upgrades[0] != "websocket" {
		t.Fatalf("unexpected open packet: %#v", open)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket&sid=" + open.SID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket upgrade: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("2probe")); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read probe response: %v", err)
	}
	if string(msg) != "3probe" {
		t.Fatalf("probe response = %q", msg)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte("5")); err != nil {
		t.Fatalf("write upgrade packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		t.Fatalf("write socket connect: %v", err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read socket connect ack: %v", err)
	}
	ack := string(msg)
	if !strings.HasPrefix(ack, `40{"sid":`) {
		t.Fatalf("connect ack = %q", msg)
	}
}

func TestEngineIOParsePollingPayloadDoesNotSplitJSONNumbers(t *testing.T) {
	engine := NewEngineIOServer(NewConnectionRegistry())
	payload := `42["agent:state",{"server_id":"srv-123","cpu":12.5}]`
	packets := engine.parsePackets(payload)
	if len(packets) != 1 || packets[0] != payload {
		t.Fatalf("packets = %#v", packets)
	}

	multi := "2\x1e" + payload
	packets = engine.parsePackets(multi)
	if len(packets) != 2 || packets[0] != "2" || packets[1] != payload {
		t.Fatalf("multi packets = %#v", packets)
	}
}

func TestEngineIOPollingWaitsForQueuedMessages(t *testing.T) {
	engine := NewEngineIOServer(NewConnectionRegistry())
	engine.pollTimeout = 500 * time.Millisecond
	engine.pollInterval = 5 * time.Millisecond
	server := httptest.NewServer(engine)
	defer server.Close()

	res, err := http.Get(server.URL + "/socket.io/?EIO=4&transport=polling")
	if err != nil {
		t.Fatalf("polling handshake: %v", err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var open struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(raw[1:], &open); err != nil {
		t.Fatalf("decode open packet: %v", err)
	}

	resultCh := make(chan string, 1)
	started := time.Now()
	go func() {
		pollRes, err := http.Get(server.URL + "/socket.io/?EIO=4&transport=polling&sid=" + open.SID)
		if err != nil {
			resultCh <- "error:" + err.Error()
			return
		}
		defer pollRes.Body.Close()
		body, _ := io.ReadAll(pollRes.Body)
		resultCh <- string(body)
	}()

	time.Sleep(40 * time.Millisecond)
	session := engine.getSession(open.SID)
	if session == nil {
		t.Fatal("session not found")
	}
	engine.queueMessage(session, `42/metrics,["metrics:update",{"serverId":"srv-1"}]`)

	select {
	case body := <-resultCh:
		if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
			t.Fatalf("poll returned before waiting for messages: %s", elapsed)
		}
		if !strings.Contains(body, "metrics:update") {
			t.Fatalf("poll body = %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("poll did not return queued message")
	}
}

func TestMetricsHubQueuesPollingClientMessages(t *testing.T) {
	hub := NewMetricsHub()
	session := &EngineIOSession{
		ID:              "polling-client",
		Transport:       "polling",
		Namespace:       "/metrics",
		PendingMessages: []string{},
		LastActivity:    time.Now(),
	}
	hub.Register(session.ID, session)

	hub.BroadcastMetrics("srv-1", map[string]interface{}{"cpu": 12.5})

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.PendingMessages) != 1 {
		t.Fatalf("pending messages = %#v", session.PendingMessages)
	}
	if !strings.Contains(session.PendingMessages[0], "metrics:update") {
		t.Fatalf("pending message = %q", session.PendingMessages[0])
	}
}

func TestEngineIORootNamespaceBroadcastsUptimeHeartbeat(t *testing.T) {
	hub := NewMetricsHub()
	engine := NewEngineIOServer(NewConnectionRegistry())
	engine.metricsHub = hub
	server := httptest.NewServer(engine)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket connect: %v", err)
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if !strings.HasPrefix(string(msg), "0") {
		t.Fatalf("handshake = %q", msg)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if !strings.HasPrefix(string(msg), `40{"sid":`) {
		t.Fatalf("ack = %q", msg)
	}

	hub.BroadcastRootEvent("uptime:heartbeat", map[string]interface{}{
		"monitorId": float64(7),
		"beat":      map[string]interface{}{"status": float64(1)},
	})
	for {
		_, msg, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read broadcast: %v", err)
		}
		if string(msg) == "2" {
			continue
		}
		if !strings.HasPrefix(string(msg), `42["uptime:heartbeat"`) {
			t.Fatalf("broadcast = %q", msg)
		}
		break
	}
}
