package serveragent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// EngineIOPacketType Engine.IO 包类型
type EngineIOPacketType int

const (
	PacketOpen    EngineIOPacketType = 0 // 连接建立
	PacketClose   EngineIOPacketType = 1 // 连接关闭
	PacketPing    EngineIOPacketType = 2 // Ping
	PacketPong    EngineIOPacketType = 3 // Pong
	PacketMessage EngineIOPacketType = 4 // 消息
	PacketUpgrade EngineIOPacketType = 5 // 协议升级
	PacketNoop    EngineIOPacketType = 6 // 无操作
)

// SocketIOPacketType Socket.IO 包类型
type SocketIOPacketType int

const (
	SocketConnect    SocketIOPacketType = 0 // 连接
	SocketDisconnect SocketIOPacketType = 1 // 断开
	SocketEvent      SocketIOPacketType = 2 // 事件
	SocketAck        SocketIOPacketType = 3 // 确认
	SocketError      SocketIOPacketType = 4 // 错误
)

// EngineIOSession Engine.IO 会话
type EngineIOSession struct {
	ID              string
	Transport       string // "polling" or "websocket"
	PingInterval    int
	PingTimeout     int
	Upgrades        []string
	CreatedAt       time.Time
	LastActivity    time.Time
	Authenticated   bool
	ServerID        string
	Namespace       string // Socket.IO 命名空间，"" = 默认(Agent), "/metrics" = 前端
	PendingMessages []string
	wsConn          *websocket.Conn
	mu              sync.RWMutex
}

// EngineIOServer Engine.IO 服务器
type EngineIOServer struct {
	sessions     map[string]*EngineIOSession
	mu           sync.RWMutex
	pingInterval time.Duration
	pingTimeout  time.Duration
	registry     *ConnectionRegistry
	metricsHub   *MetricsHub
	onConnect    func(sessionID string, serverID string)
	onMessage    func(sessionID string, event string, data json.RawMessage)
	onDisconnect func(sessionID string)
}

// NewEngineIOServer 创建 Engine.IO 服务器
func NewEngineIOServer(registry *ConnectionRegistry) *EngineIOServer {
	server := &EngineIOServer{
		sessions:     make(map[string]*EngineIOSession),
		pingInterval: 25 * time.Second,
		pingTimeout:  20 * time.Second,
		registry:     registry,
	}

	// 启动会话清理
	go server.cleanupLoop()

	return server
}

// ServeHTTP 实现 http.Handler 接口
func (s *EngineIOServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	transport := r.URL.Query().Get("transport")

	switch transport {
	case "polling":
		s.HandlePolling(w, r)
	case "websocket":
		s.HandleWebSocket(w, r)
	default:
		http.Error(w, "Invalid transport", http.StatusBadRequest)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（生产环境应该限制）
	},
}

// HandleWebSocket 处理 WebSocket 升级
func (s *EngineIOServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("sid")

	// Socket.IO v4 允许直接 WebSocket 连接（无需先 polling）
	if sid == "" {
		// 创建新会话
		sid = uuid.New().String()
		session := &EngineIOSession{
			ID:           sid,
			Transport:    "websocket",
			LastActivity: time.Now(),
		}

		s.mu.Lock()
		s.sessions[sid] = session
		s.mu.Unlock()

		// WebSocket 升级
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		session.mu.Lock()
		session.wsConn = conn
		session.mu.Unlock()

		// Socket.IO v4: 直接 WebSocket 连接时，服务器必须先发送握手包
		handshake := map[string]interface{}{
			"sid":          sid,
			"pingInterval": int(s.pingInterval / time.Millisecond),
			"pingTimeout":  int(s.pingTimeout / time.Millisecond),
			"upgrades":     []string{},
		}
		handshakeJSON, _ := json.Marshal(handshake)
		handshakePacket := "0" + string(handshakeJSON)
		if err := s.safeWrite(session, websocket.TextMessage, []byte(handshakePacket)); err != nil {
			return
		}

		// 启动消息处理
		s.handleWebSocketMessages(session, conn)
		return
	}

	session := s.getSession(sid)
	if session == nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	// WebSocket 升级
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// 更新传输类型和 WebSocket 连接
	session.mu.Lock()
	session.Transport = "websocket"
	session.wsConn = conn
	session.mu.Unlock()

	// Engine.IO v4: WebSocket 升级后，需要等待客户端的 probe 和 upgrade 确认
	// 然后等待客户端发送 Socket.IO CONNECT (40)

	s.handleWebSocketMessages(session, conn)
}

// handleWebSocketMessages 处理 WebSocket 消息
func (s *EngineIOServer) handleWebSocketMessages(session *EngineIOSession, conn *websocket.Conn) {

	// 启动消息发送协程
	done := make(chan struct{})
	go s.writeLoop(session, conn, done)

	// 读取消息循环
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			close(done)
			break
		}
		session.mu.Lock()
		session.LastActivity = time.Now()
		session.mu.Unlock()

		if messageType == websocket.TextMessage {
			msg := string(message)

			// Engine.IO v4 协议: 处理 probe 消息
			if msg == "2probe" {
				// 客户端发送 ping probe，响应 pong probe
				if err := s.safeWrite(session, websocket.TextMessage, []byte("3probe")); err != nil {
					close(done)
					break
				}
				continue
			}

			if msg == "5" {
				// 客户端确认升级
				// Socket.IO v4 不需要服务器主动发送 CONNECT
				// 客户端会在升级后发送 "40"，服务器应该等待并响应
				continue
			}

			// 处理普通 Engine.IO 消息
			if len(msg) > 0 {
				packetType := EngineIOPacketType(msg[0] - '0')

				if packetType == PacketPing {
					// 响应 ping
					if err := s.safeWrite(session, websocket.TextMessage, []byte("3")); err != nil {
						close(done)
						break
					}
					continue
				}

				if packetType == PacketMessage && len(msg) > 1 {
					// Socket.IO 消息: 4开头
					socketPayload := msg[1:]

					// 如果是 Socket.IO CONNECT (40)，响应 CONNECT ACK
					if len(socketPayload) > 0 && socketPayload[0] == '0' {
						connectData := socketPayload[1:]

						// 检查是否为 /metrics 命名空间连接
						// 前端发送 "40/metrics," 或 "40/metrics,{}"
						if strings.HasPrefix(connectData, "/metrics") {
							session.mu.Lock()
							session.Namespace = "/metrics"
							session.Authenticated = true
							session.mu.Unlock()

							if err := s.safeWrite(session, websocket.TextMessage, []byte(connectAckPacket("/metrics", session.ID))); err != nil {
								close(done)
								break
							}

							// 注册到 MetricsHub
							if s.metricsHub != nil {
								s.metricsHub.Register(session.ID, session)
							}
							continue
						}

						// 默认命名空间。浏览器页面（如 Uptime）会停留在 root namespace
						// 接收实时事件；Agent 随后会发送 agent:connect 并从 root 客户端池移除。
						if err := s.safeWrite(session, websocket.TextMessage, []byte(connectAckPacket("", session.ID))); err != nil {
							close(done)
							break
						}

						// 触发 onConnect 回调
						if s.onConnect != nil {
							s.onConnect(session.ID, session.ServerID)
						}
						if s.metricsHub != nil {
							s.metricsHub.RegisterRoot(session.ID, session)
						}
						continue
					}

					// 其他 Socket.IO 消息
					s.handleSocketIOMessage(session, socketPayload)
				}
			}
		}
	}

	// 断开连接
	session.mu.RLock()
	ns := session.Namespace
	session.mu.RUnlock()

	if ns == "/metrics" {
		// 前端客户端断开
		if s.metricsHub != nil {
			s.metricsHub.Unregister(session.ID)
		}
	} else if session.Authenticated && session.ServerID != "" {
		s.registry.Disconnect(session.ServerID)
	}
	if s.metricsHub != nil {
		s.metricsHub.UnregisterRoot(session.ID)
	}
	if s.onDisconnect != nil {
		s.onDisconnect(session.ID)
	}
	s.removeSession(session.ID)
}

// writeLoop WebSocket 消息发送循环
func (s *EngineIOServer) writeLoop(session *EngineIOSession, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// 发送 ping
			if err := s.safeWrite(session, websocket.TextMessage, []byte("2")); err != nil {
				return
			}
		default:
			// 检查是否有待发送的消息
			session.mu.Lock()
			if len(session.PendingMessages) > 0 {
				messages := session.PendingMessages
				session.PendingMessages = []string{}
				session.mu.Unlock()

				for _, msg := range messages {
					if err := s.safeWrite(session, websocket.TextMessage, []byte(msg)); err != nil {
						return
					}
				}
			} else {
				session.mu.Unlock()
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

// HandlePolling 处理 polling 请求
func (s *EngineIOServer) HandlePolling(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("sid")

	if sid == "" {
		// 新连接：handshake
		s.handleHandshake(w, r)
		return
	}

	// 已有会话
	session := s.getSession(sid)
	if session == nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		// Poll：返回待发送的消息
		s.handlePoll(w, r, session)
	} else if r.Method == "POST" {
		// 接收客户端消息
		s.handlePost(w, r, session)
	}
}

// handleHandshake 处理 handshake
func (s *EngineIOServer) handleHandshake(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.New().String()

	session := &EngineIOSession{
		ID:              sessionID,
		Transport:       "polling",
		PingInterval:    int(s.pingInterval.Milliseconds()),
		PingTimeout:     int(s.pingTimeout.Milliseconds()),
		Upgrades:        []string{"websocket"},
		CreatedAt:       time.Now(),
		LastActivity:    time.Now(),
		PendingMessages: []string{},
	}

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	// 发送 open 包
	openData := map[string]interface{}{
		"sid":          sessionID,
		"upgrades":     session.Upgrades,
		"pingInterval": session.PingInterval,
		"pingTimeout":  session.PingTimeout,
	}
	openJSON, _ := json.Marshal(openData)
	packet := fmt.Sprintf("0%s", openJSON)

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(packet))
}

// handlePoll 处理 GET polling
func (s *EngineIOServer) handlePoll(w http.ResponseWriter, r *http.Request, session *EngineIOSession) {
	session.mu.Lock()
	messages := session.PendingMessages
	session.PendingMessages = []string{}
	session.LastActivity = time.Now()
	session.mu.Unlock()

	// 如果没有消息，发送 noop
	if len(messages) == 0 {
		messages = []string{"6"} // noop
	}

	// Engine.IO v4 polling separates multiple packets with ASCII RS.
	payload := strings.Join(messages, "\x1e")

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(payload))
}

// handlePost 处理 POST polling
func (s *EngineIOServer) handlePost(w http.ResponseWriter, r *http.Request, session *EngineIOSession) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// 解析包
	packets := s.parsePackets(string(body))
	for _, packet := range packets {
		s.handlePacket(session, packet)
	}

	session.mu.Lock()
	session.LastActivity = time.Now()
	session.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// parsePackets 解析 Engine.IO 包
func (s *EngineIOServer) parsePackets(data string) []string {
	if data == "" {
		return nil
	}
	// Engine.IO v4 polling uses ASCII record separator for multiple packets.
	// Do not split on digits inside Socket.IO JSON payloads.
	parts := strings.Split(data, "\x1e")
	packets := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			packets = append(packets, part)
		}
	}
	return packets
}

// handlePacket 处理单个包
func (s *EngineIOServer) handlePacket(session *EngineIOSession, packet string) {
	if len(packet) == 0 {
		return
	}

	packetType := EngineIOPacketType(packet[0] - '0')
	payload := packet[1:]

	switch packetType {
	case PacketPing:
		// 响应 Pong
		s.queueMessage(session, "3") // pong

	case PacketMessage:
		// Socket.IO 消息
		s.handleSocketIOMessage(session, payload)

	case PacketClose:
		// 关闭连接
		if s.onDisconnect != nil {
			s.onDisconnect(session.ID)
		}
		s.removeSession(session.ID)
	}
}

// handleSocketIOMessage 处理 Socket.IO 消息
func (s *EngineIOServer) handleSocketIOMessage(session *EngineIOSession, payload string) {
	if len(payload) < 1 {
		return
	}

	socketType := SocketIOPacketType(payload[0] - '0')
	data := payload[1:]

	switch socketType {
	case SocketConnect:
		// Socket.IO v4 CONNECT 握手

		// 检查是否为 /metrics 命名空间
		if strings.HasPrefix(data, "/metrics") {
			session.mu.Lock()
			session.Namespace = "/metrics"
			session.Authenticated = true
			session.mu.Unlock()

			s.queueMessage(session, connectAckPacket("/metrics", session.ID))
			if s.metricsHub != nil {
				s.metricsHub.Register(session.ID, session)
			}
			return
		}

		// 默认命名空间（Agent 连接）
		session.mu.Lock()
		session.Authenticated = true
		session.mu.Unlock()

		// Socket.IO v4 CONNECT ACK 必须包含 sid，否则 v3+ 客户端会判定为 v2 协议。
		s.queueMessage(session, connectAckPacket("", session.ID))
		if s.metricsHub != nil {
			s.metricsHub.RegisterRoot(session.ID, session)
		}

		// 触发连接回调
		if s.onConnect != nil {
			s.onConnect(session.ID, session.ServerID)
		}

	case SocketEvent:
		// 事件：格式为 ["event_name", {data}]
		var eventData []json.RawMessage
		if err := json.Unmarshal([]byte(data), &eventData); err != nil {
			return
		}

		if len(eventData) < 1 {
			return
		}

		var eventName string
		json.Unmarshal(eventData[0], &eventName)

		var eventPayload json.RawMessage
		if len(eventData) > 1 {
			eventPayload = eventData[1]
		}

		// 处理认证事件
		if eventName == "authenticate" || eventName == "agent:connect" {
			var authData struct {
				ServerID string `json:"server_id"`
				Key      string `json:"key"`
				Hostname string `json:"hostname"`
				Version  string `json:"version"`
			}
			if err := json.Unmarshal(eventPayload, &authData); err == nil {
				session.mu.Lock()
				session.ServerID = authData.ServerID
				session.Authenticated = true
				session.mu.Unlock()
				if s.metricsHub != nil {
					s.metricsHub.UnregisterRoot(session.ID)
				}

				// 发送认证成功响应
				s.sendEvent(session, "dashboard:auth_ok", map[string]interface{}{
					"success":   true,
					"server_id": authData.ServerID,
				})

				applog.Info(context.Background(), "serveragent", "agent authenticated",
					"server_id", authData.ServerID,
					"hostname", authData.Hostname,
					"version", authData.Version,
				)

				// 触发 onConnect 回调，注册到连接池
				if s.onConnect != nil {
					s.onConnect(session.ID, authData.ServerID)
				}
			}
		}

		if s.onMessage != nil {
			s.onMessage(session.ID, eventName, eventPayload)
		}
	}
}

// queueMessage 排队消息
func (s *EngineIOServer) queueMessage(session *EngineIOSession, message string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.PendingMessages = append(session.PendingMessages, message)
}

// sendEvent 发送 Socket.IO 事件
func (s *EngineIOServer) sendEvent(session *EngineIOSession, event string, data interface{}) {
	payload := []interface{}{event, data}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Socket.IO event 包：42[...]
	// 4 = Engine.IO Message packet
	// 2 = Socket.IO Event packet
	message := fmt.Sprintf("42%s", jsonData)
	s.queueMessage(session, message)
}

// getSession 获取会话
func (s *EngineIOServer) getSession(sid string) *EngineIOSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sid]
}

// removeSession 移除会话
func (s *EngineIOServer) removeSession(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sid)
}

// cleanupLoop 清理过期会话
func (s *EngineIOServer) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for sid, session := range s.sessions {
			session.mu.RLock()
			lastActivity := session.LastActivity
			session.mu.RUnlock()

			if now.Sub(lastActivity) > 60*time.Second {
				// 清理 /metrics 命名空间的客户端
				session.mu.RLock()
				ns := session.Namespace
				session.mu.RUnlock()
				if ns == "/metrics" && s.metricsHub != nil {
					s.metricsHub.Unregister(sid)
				}
				if s.metricsHub != nil {
					s.metricsHub.UnregisterRoot(sid)
				}

				delete(s.sessions, sid)
				if s.onDisconnect != nil {
					go s.onDisconnect(sid)
				}
			}
		}
		s.mu.Unlock()
	}
}

// SetHandlers 设置事件处理器
func (s *EngineIOServer) SetHandlers(
	onConnect func(sessionID string, serverID string),
	onMessage func(sessionID string, event string, data json.RawMessage),
	onDisconnect func(sessionID string),
) {
	s.onConnect = onConnect
	s.onMessage = onMessage
	s.onDisconnect = onDisconnect
}

func connectAckPacket(namespace string, sid string) string {
	payload, _ := json.Marshal(map[string]string{"sid": sid})
	if namespace != "" {
		return fmt.Sprintf("40%s,%s", namespace, payload)
	}
	return "40" + string(payload)
}

// safeWrite 安全地并发写入 WebSocket 消息
func (s *EngineIOServer) safeWrite(session *EngineIOSession, messageType int, data []byte) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.wsConn == nil {
		return fmt.Errorf("connection closed")
	}
	return session.wsConn.WriteMessage(messageType, data)
}
