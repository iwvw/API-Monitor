package serveragent

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MetricsHubClient 代表一个连接到 /metrics 命名空间的前端浏览器客户端
type MetricsHubClient struct {
	sessionID string
	session   *EngineIOSession
	connAt    time.Time
}

// MetricsHub 管理所有前端 /metrics 命名空间的 WebSocket 连接
// 负责将 Agent 上报的实时指标广播给所有已连接的浏览器客户端
type MetricsHub struct {
	clients     map[string]*MetricsHubClient // /metrics namespace clients
	rootClients map[string]*MetricsHubClient // default namespace browser clients
	mu          sync.RWMutex
}

// NewMetricsHub 创建一个新的 MetricsHub 实例
func NewMetricsHub() *MetricsHub {
	hub := &MetricsHub{
		clients:     make(map[string]*MetricsHubClient),
		rootClients: make(map[string]*MetricsHubClient),
	}
	go hub.cleanupLoop()
	return hub
}

// Register 注册一个前端客户端到 /metrics 命名空间
func (h *MetricsHub) Register(sessionID string, session *EngineIOSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[sessionID] = &MetricsHubClient{
		sessionID: sessionID,
		session:   session,
		connAt:    time.Now(),
	}
}

// Unregister 移除一个前端客户端
func (h *MetricsHub) Unregister(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[sessionID]; ok {
		delete(h.clients, sessionID)
	}
}

// RegisterRoot 注册默认命名空间浏览器客户端（例如 Uptime 页面）。
func (h *MetricsHub) RegisterRoot(sessionID string, session *EngineIOSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rootClients[sessionID] = &MetricsHubClient{
		sessionID: sessionID,
		session:   session,
		connAt:    time.Now(),
	}
}

// UnregisterRoot 移除默认命名空间客户端。
func (h *MetricsHub) UnregisterRoot(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rootClients, sessionID)
}

// metricsBroadcastSensitiveKeys 是广播给浏览器命名空间（/metrics 与 root）
// 前必须剥离的敏感标识字段。/socket.io/ 端点匿名可达，公开状态页依赖匿名
// 订阅，因此不能靠鉴权收敛，只能在广播侧剥离主机名、出口 IP 等标识，
// 防止匿名订阅者拿到真实基础设施信息；Agent 上行数据不受影响。
var metricsBroadcastSensitiveKeys = map[string]struct{}{
	"hostname": {},
	"ip":       {},
	"isp":      {},
	"org":      {},
	"asn":      {},
}

// sanitizeBroadcastPayload 递归剥离广播负载中的敏感标识字段。
// 只做删除不改值，返回新建的容器，调用方持有的原始数据不被修改。
func sanitizeBroadcastPayload(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		copied := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			if _, sensitive := metricsBroadcastSensitiveKeys[k]; sensitive {
				continue
			}
			copied[k] = sanitizeBroadcastPayload(v)
		}
		return copied
	case []interface{}:
		copied := make([]interface{}, len(typed))
		for i, v := range typed {
			copied[i] = sanitizeBroadcastPayload(v)
		}
		return copied
	default:
		return value
	}
}

// BroadcastMetrics 向所有前端客户端广播 metrics:update 事件
// data 格式: { serverId: string, metrics: {...}, timestamp: number }
func (h *MetricsHub) BroadcastMetrics(serverID string, metrics map[string]interface{}) {
	h.mu.RLock()
	clients := make([]*MetricsHubClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	data := map[string]interface{}{
		"serverId":  serverID,
		"metrics":   sanitizeBroadcastPayload(metrics),
		"timestamp": time.Now().UnixMilli(),
	}

	h.broadcastEvent(clients, "metrics:update", data)
}

// BroadcastServerStatus 向所有前端客户端广播 server:status 事件
func (h *MetricsHub) BroadcastServerStatus(serverID string, status string, agentOnline bool) {
	h.mu.RLock()
	clients := make([]*MetricsHubClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	data := map[string]interface{}{
		"serverId":     serverID,
		"status":       status,
		"agent_online": agentOnline,
		"lastSeen":     time.Now().Format(time.RFC3339),
	}

	h.broadcastEvent(clients, "server:status", data)
}

func (h *MetricsHub) BroadcastRootEvent(event string, data interface{}) {
	h.mu.RLock()
	clients := make([]*MetricsHubClient, 0, len(h.rootClients))
	for _, c := range h.rootClients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	// root 命名空间同样匿名可达（公开状态页走 polling 订阅 uptime:heartbeat），
	// 广播负载统一过敏感字段剥离。
	h.broadcastPlainEvent(clients, event, sanitizeBroadcastPayload(data))
}

// broadcastEvent 向所有客户端发送 Socket.IO 事件（/metrics 命名空间）
func (h *MetricsHub) broadcastEvent(clients []*MetricsHubClient, event string, data interface{}) {
	h.broadcastSocketIOEvent(clients, fmt.Sprintf("42/metrics,"), event, data)
}

func (h *MetricsHub) broadcastPlainEvent(clients []*MetricsHubClient, event string, data interface{}) {
	h.broadcastSocketIOEvent(clients, "42", event, data)
}

// metricsHubWriteTimeout 单次广播写超时：不读数据的慢客户端最多阻塞
// 广播者一小段时间即被淘汰，避免单个客户端卡死所有 Agent 状态处理。
const metricsHubWriteTimeout = 3 * time.Second

func (h *MetricsHub) broadcastSocketIOEvent(clients []*MetricsHubClient, prefix string, event string, data interface{}) {
	payload := []interface{}{event, data}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Socket.IO v4 命名空间事件格式: 42/metrics,["event",{data}]
	// 4 = Engine.IO Message packet
	// 2 = Socket.IO Event packet
	// /metrics, = 命名空间前缀
	message := fmt.Sprintf("%s%s", prefix, jsonData)

	var stale []string

	for _, client := range clients {
		session := client.session
		if session == nil {
			stale = append(stale, client.sessionID)
			continue
		}

		session.mu.RLock()
		conn := session.wsConn
		session.mu.RUnlock()

		if conn == nil {
			enqueuePendingMessage(session, message)
			continue
		}

		// 安全写入（加锁防并发写；带写超时防慢客户端无限阻塞广播）
		session.mu.Lock()
		_ = session.wsConn.SetWriteDeadline(time.Now().Add(metricsHubWriteTimeout))
		err := session.wsConn.WriteMessage(websocket.TextMessage, []byte(message))
		session.mu.Unlock()

		if err != nil {
			stale = append(stale, client.sessionID)
		}
	}

	// 清理断开的客户端
	if len(stale) > 0 {
		h.mu.Lock()
		for _, sid := range stale {
			delete(h.clients, sid)
			delete(h.rootClients, sid)
		}
		h.mu.Unlock()
	}
}

// ClientCount 返回当前连接的前端客户端数量
func (h *MetricsHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// cleanupLoop 定期清理断开连接的客户端
func (h *MetricsHub) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		h.mu.Lock()
		for sid, client := range h.clients {
			session := client.session
			if session == nil {
				delete(h.clients, sid)
				delete(h.rootClients, sid)
				continue
			}
			session.mu.RLock()
			conn := session.wsConn
			transport := session.Transport
			session.mu.RUnlock()
			if conn == nil && transport != "polling" {
				delete(h.clients, sid)
				delete(h.rootClients, sid)
			}
		}
		for sid, client := range h.rootClients {
			session := client.session
			if session == nil {
				delete(h.rootClients, sid)
				continue
			}
			session.mu.RLock()
			conn := session.wsConn
			transport := session.Transport
			session.mu.RUnlock()
			if conn == nil && transport != "polling" {
				delete(h.rootClients, sid)
			}
		}
		h.mu.Unlock()
	}
}
