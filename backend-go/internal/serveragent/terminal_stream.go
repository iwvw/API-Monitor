package serveragent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	agentTerminalTokenTTL     = 30 * time.Second
	agentTerminalAttachWait   = 10 * time.Second
	agentTerminalBrowserQueue = 512
)

type agentTerminalBroker struct {
	mu      sync.Mutex
	streams map[string]*agentTerminalStream
	dropped int64
}

type agentTerminalStream struct {
	broker    *agentTerminalBroker
	serverID  string
	streamID  string
	token     string
	expiresAt time.Time
	consumed  bool

	agentConn    *websocket.Conn
	agentWriteMu sync.Mutex
	browserOut   chan terminalWSMessage
	ready        chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
}

func newAgentTerminalBroker() *agentTerminalBroker {
	return &agentTerminalBroker{streams: make(map[string]*agentTerminalStream)}
}

func (b *agentTerminalBroker) create(serverID string) *agentTerminalStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(time.Now())

	stream := &agentTerminalStream{
		broker:     b,
		serverID:   serverID,
		streamID:   uuid.NewString(),
		token:      randomTerminalToken(),
		expiresAt:  time.Now().Add(agentTerminalTokenTTL),
		browserOut: make(chan terminalWSMessage, agentTerminalBrowserQueue),
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
	}
	b.streams[stream.streamID] = stream
	return stream
}

func (b *agentTerminalBroker) consume(serverID, streamID, token string) (*agentTerminalStream, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(time.Now())
	stream := b.streams[streamID]
	if stream == nil || stream.consumed || stream.serverID != serverID || stream.token != token || time.Now().After(stream.expiresAt) {
		return nil, false
	}
	stream.consumed = true
	return stream, true
}

func (b *agentTerminalBroker) remove(streamID string) {
	b.mu.Lock()
	delete(b.streams, streamID)
	b.mu.Unlock()
}

func (b *agentTerminalBroker) closeForServer(serverID, reason string) {
	b.mu.Lock()
	streams := make([]*agentTerminalStream, 0)
	for _, stream := range b.streams {
		if stream.serverID == serverID {
			streams = append(streams, stream)
		}
	}
	b.mu.Unlock()
	for _, stream := range streams {
		stream.close(reason)
	}
}

func (b *agentTerminalBroker) stats() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(time.Now())
	queueDepths := map[string]int{}
	for streamID, stream := range b.streams {
		queueDepths[streamID] = len(stream.browserOut)
	}
	return map[string]interface{}{
		"active_count": len(b.streams),
		"queue_depths": queueDepths,
		"dropped":      b.dropped,
	}
}

func (b *agentTerminalBroker) cleanupExpiredLocked(now time.Time) {
	for streamID, stream := range b.streams {
		if !stream.consumed && now.After(stream.expiresAt) {
			delete(b.streams, streamID)
		}
	}
}

func (s *agentTerminalStream) attachAgent(conn *websocket.Conn) bool {
	s.agentWriteMu.Lock()
	s.agentConn = conn
	s.agentWriteMu.Unlock()
	select {
	case <-s.ready:
	default:
		close(s.ready)
	}
	return true
}

func (s *agentTerminalStream) sendToAgent(msg terminalWSMessage) error {
	s.agentWriteMu.Lock()
	defer s.agentWriteMu.Unlock()
	if s.agentConn == nil {
		return websocket.ErrCloseSent
	}
	_ = s.agentConn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
	return s.agentConn.WriteJSON(msg)
}

func (s *agentTerminalStream) emitToBrowser(msg terminalWSMessage) bool {
	select {
	case <-s.done:
		return false
	case s.browserOut <- msg:
		return true
	default:
		if s.broker != nil {
			s.broker.mu.Lock()
			s.broker.dropped++
			s.broker.mu.Unlock()
		}
		return false
	}
}

func (s *agentTerminalStream) close(reason string) {
	s.closeOnce.Do(func() {
		close(s.done)
		s.agentWriteMu.Lock()
		if s.agentConn != nil {
			_ = s.agentConn.WriteJSON(terminalWSMessage{Type: "stop", Data: reason})
			_ = s.agentConn.Close()
		}
		s.agentWriteMu.Unlock()
		if s.broker != nil {
			s.broker.remove(s.streamID)
		}
	})
}

func (s *Service) handleAgentTerminalStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.terminalBroker == nil {
		response.Error(w, http.StatusServiceUnavailable, "terminal broker unavailable")
		return
	}
	serverID := strings.TrimSpace(r.URL.Query().Get("server_id"))
	streamID := strings.TrimSpace(r.URL.Query().Get("stream_id"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	stream, ok := s.terminalBroker.consume(serverID, streamID, token)
	if !ok {
		response.Error(w, http.StatusUnauthorized, "invalid or expired terminal stream token")
		return
	}

	conn, err := s.terminalUpgrader().Upgrade(w, r, nil)
	if err != nil {
		stream.close("terminal_agent_upgrade_failed")
		return
	}
	defer conn.Close()
	stream.attachAgent(conn)

	for {
		var msg terminalWSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			stream.close("terminal_agent_closed")
			return
		}
		if msg.Type == "" {
			continue
		}
		if msg.Transport == "" {
			msg.Transport = "agent"
		}
		if !stream.emitToBrowser(msg) && msg.Type != "data" {
			stream.close("terminal_browser_backpressure")
			return
		}
	}
}

func (s *Service) runAgentTerminalSessionV2(
	r *http.Request,
	browserConn *websocket.Conn,
	agentConn *AgentConnection,
	serverID string,
	writeJSON func(terminalWSMessage) bool,
	closeDone func(),
	done <-chan struct{},
) bool {
	if s.terminalBroker == nil || agentConn == nil {
		return false
	}
	stream := s.terminalBroker.create(serverID)
	cols := intQuery(r, "cols", 120)
	rows := intQuery(r, "rows", 32)
	startPayload := map[string]interface{}{
		"cols":               cols,
		"rows":               rows,
		"terminal_stream_v2": true,
		"stream_id":          stream.streamID,
		"stream_token":       stream.token,
	}
	if containerName := strings.TrimSpace(r.URL.Query().Get("container")); containerName != "" {
		startPayload["command"] = "docker"
		startPayload["args"] = []string{
			"exec",
			"-it",
			containerName,
			"sh",
			"-lc",
			"exec /bin/bash || exec /bin/sh || exec sh",
		}
	}
	dataBytes, _ := json.Marshal(startPayload)
	if err := agentConn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      stream.streamID,
		"type":    12,
		"data":    string(dataBytes),
		"timeout": 0,
	}); err != nil {
		stream.close("terminal_v2_task_send_failed")
		return false
	}

	select {
	case <-stream.ready:
	case <-time.After(agentTerminalAttachWait):
		stream.close("terminal_v2_attach_timeout")
		return false
	case <-done:
		stream.close("terminal_browser_closed")
		return true
	}

	defer func() {
		_ = stream.sendToAgent(terminalWSMessage{Type: "stop"})
		stream.close("terminal_browser_closed")
	}()

	go func() {
		for {
			select {
			case msg := <-stream.browserOut:
				if msg.Type == "ready" {
					if !writeJSON(terminalWSMessage{Type: "status", Data: "connected", Transport: "agent"}) {
						closeDone()
						return
					}
					continue
				}
				if msg.Transport == "" {
					msg.Transport = "agent"
				}
				if !writeJSON(msg) {
					closeDone()
					return
				}
			case <-stream.done:
				closeDone()
				return
			case <-done:
				return
			}
		}
	}()
	writeJSON(terminalWSMessage{Type: "status", Data: "connected", Transport: "agent"})

	for {
		select {
		case <-done:
			return true
		default:
		}

		_, payload, err := browserConn.ReadMessage()
		if err != nil {
			closeDone()
			return true
		}
		var msg terminalWSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input", "resize", "stop":
			_ = stream.sendToAgent(msg)
		case "disconnect":
			closeDone()
			return true
		}
	}
}

func randomTerminalToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(buf)
}
