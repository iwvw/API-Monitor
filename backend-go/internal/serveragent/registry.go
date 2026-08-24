package serveragent

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AgentConnection 表示一个 Agent 连接
type AgentConnection struct {
	ID              string
	ServerID        string
	Socket          interface{} // WebSocket 连接
	AuthenticatedAt time.Time
	LastHeartbeat   time.Time
	Capabilities    map[string]bool
	Metadata        map[string]interface{}
	mu              sync.RWMutex
}

// ConnectionRegistry Agent 连接注册表
type ConnectionRegistry struct {
	connections      map[string]*AgentConnection // serverID -> connection
	mu               sync.RWMutex
	heartbeatTimeout time.Duration
	cleanupInterval  time.Duration
	stopCh           chan struct{}
}

// NewConnectionRegistry 创建连接注册表
func NewConnectionRegistry() *ConnectionRegistry {
	registry := &ConnectionRegistry{
		connections:      make(map[string]*AgentConnection),
		heartbeatTimeout: envDurationMs("API_MONITOR_AGENT_REGISTRY_STALE_AFTER_MS", 120*time.Second),
		cleanupInterval:  60 * time.Second,
		stopCh:           make(chan struct{}),
	}

	// 启动清理协程
	go registry.cleanupLoop()

	return registry
}

// Register 注册新连接
func (r *ConnectionRegistry) Register(serverID string, socket interface{}) *AgentConnection {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 如果已存在，先断开旧连接
	if old, exists := r.connections[serverID]; exists {
		r.disconnectLocked(old)
	}

	conn := &AgentConnection{
		ID:              uuid.New().String(),
		ServerID:        serverID,
		Socket:          socket,
		AuthenticatedAt: time.Now(),
		LastHeartbeat:   time.Now(),
		Capabilities:    make(map[string]bool),
		Metadata:        make(map[string]interface{}),
	}

	r.connections[serverID] = conn
	return conn
}

// Get 获取连接
func (r *ConnectionRegistry) Get(serverID string) (*AgentConnection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, exists := r.connections[serverID]
	return conn, exists
}

// UpdateHeartbeat 更新心跳时间
func (r *ConnectionRegistry) UpdateHeartbeat(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, exists := r.connections[serverID]; exists {
		conn.mu.Lock()
		conn.LastHeartbeat = time.Now()
		conn.mu.Unlock()
	}
}

// Disconnect 断开连接
func (r *ConnectionRegistry) Disconnect(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conn, exists := r.connections[serverID]; exists {
		r.disconnectLocked(conn)
		delete(r.connections, serverID)
	}
}

// DisconnectIfSocket disconnects only when the registered socket still matches.
// This prevents an old session's late close callback from removing a newer
// replacement connection for the same server.
func (r *ConnectionRegistry) DisconnectIfSocket(serverID string, socket interface{}) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, exists := r.connections[serverID]
	if !exists || conn.Socket != socket {
		return false
	}
	r.disconnectLocked(conn)
	delete(r.connections, serverID)
	return true
}

// disconnectLocked 断开连接（内部使用，需持有锁）
func (r *ConnectionRegistry) disconnectLocked(conn *AgentConnection) {
	// 关闭 WebSocket 连接
	if ws, ok := conn.Socket.(interface{ Close() error }); ok {
		ws.Close()
	}
}

// List 列出所有连接
func (r *ConnectionRegistry) List() []*AgentConnection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*AgentConnection, 0, len(r.connections))
	for _, conn := range r.connections {
		list = append(list, conn)
	}
	return list
}

// Count 连接数量
func (r *ConnectionRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.connections)
}

// cleanupLoop 清理超时连接
func (r *ConnectionRegistry) cleanupLoop() {
	ticker := time.NewTicker(r.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanupStale()
		case <-r.stopCh:
			return
		}
	}
}

// cleanupStale 清理过期连接
func (r *ConnectionRegistry) cleanupStale() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for serverID, conn := range r.connections {
		conn.mu.RLock()
		lastHeartbeat := conn.LastHeartbeat
		conn.mu.RUnlock()

		if now.Sub(lastHeartbeat) > r.heartbeatTimeout {
			r.disconnectLocked(conn)
			delete(r.connections, serverID)
		}
	}
}

// Stop 停止注册表
func (r *ConnectionRegistry) Stop() {
	close(r.stopCh)

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, conn := range r.connections {
		r.disconnectLocked(conn)
	}
	r.connections = make(map[string]*AgentConnection)
}

// UpdateCapabilities 更新 Agent 能力
func (c *AgentConnection) UpdateCapabilities(capabilities map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Capabilities = capabilities
}

// GetCapabilities 获取能力
func (c *AgentConnection) GetCapabilities() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	caps := make(map[string]bool, len(c.Capabilities))
	for k, v := range c.Capabilities {
		caps[k] = v
	}
	return caps
}

// UpdateMetadata 更新元数据
func (c *AgentConnection) UpdateMetadata(metadata map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Metadata = metadata
}

// SetMetadata 设置单个元数据项
func (c *AgentConnection) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Metadata[key] = value
}

// GetMetadata 获取元数据
func (c *AgentConnection) GetMetadata() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	meta := make(map[string]interface{}, len(c.Metadata))
	for k, v := range c.Metadata {
		meta[k] = v
	}
	return meta
}

// SendEvent 发送事件到 Agent
func (c *AgentConnection) SendEvent(event string, data interface{}) error {
	c.mu.RLock()
	socket := c.Socket
	c.mu.RUnlock()

	// 构造 Socket.IO 事件帧：42["event", data]
	payload := []interface{}{event, data}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	frame := fmt.Sprintf("42%s", jsonData)

	// 如果 Socket 是 *EngineIOSession，将消息加入其发送队列，实现并发安全。
	// 队列有界（与 enqueuePendingMessage 相同语义）：满了丢弃最旧消息，
	// 防止积压无界增长。
	if session, ok := socket.(*EngineIOSession); ok {
		session.mu.Lock()
		if len(session.PendingMessages) >= maxPendingMessagesPerSession {
			copy(session.PendingMessages, session.PendingMessages[1:])
			session.PendingMessages[len(session.PendingMessages)-1] = frame
		} else {
			session.PendingMessages = append(session.PendingMessages, frame)
		}
		session.mu.Unlock()
		return nil
	}

	// 兼容直接写入 WebSocket 场景
	if ws, ok := socket.(interface {
		WriteMessage(messageType int, data []byte) error
	}); ok {
		return ws.WriteMessage(1, []byte(frame)) // 1 = TextMessage
	}

	return fmt.Errorf("invalid socket type")
}

// FrontendBroadcaster 前端 WebSocket 广播器
type FrontendBroadcaster struct {
	clients map[string]interface{} // clientID -> WebSocket
	mu      sync.RWMutex
}

// NewFrontendBroadcaster 创建广播器
func NewFrontendBroadcaster() *FrontendBroadcaster {
	return &FrontendBroadcaster{
		clients: make(map[string]interface{}),
	}
}

// Register 注册前端客户端
func (b *FrontendBroadcaster) Register(clientID string, socket interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[clientID] = socket
}

// Unregister 注销客户端
func (b *FrontendBroadcaster) Unregister(clientID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, clientID)
}

// Broadcast 广播消息到所有前端客户端
func (b *FrontendBroadcaster) Broadcast(event string, data interface{}) {
	b.mu.RLock()
	clients := make([]interface{}, 0, len(b.clients))
	for _, client := range b.clients {
		clients = append(clients, client)
	}
	b.mu.RUnlock()

	payload := map[string]interface{}{
		"event": event,
		"data":  data,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, client := range clients {
		if ws, ok := client.(interface {
			WriteMessage(messageType int, data []byte) error
		}); ok {
			ws.WriteMessage(1, jsonData)
		}
	}
}

// BroadcastToSubscribers 广播到订阅了特定 serverID 的客户端
func (b *FrontendBroadcaster) BroadcastToSubscribers(serverID string, event string, data interface{}) {
	// 简化版：广播到所有客户端，前端自行过滤
	b.Broadcast(event, map[string]interface{}{
		"server_id": serverID,
		"payload":   data,
	})
}
