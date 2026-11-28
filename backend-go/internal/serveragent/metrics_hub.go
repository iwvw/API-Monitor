package serveragent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MetricsHubClient 代表一个连接到 /metrics 命名空间的前端浏览器客户端
type MetricsHubClient struct {
	sessionID string
	session   *EngineIOSession
	connAt    time.Time
	// subscription 是该客户端关心的 serverId 集合。
	//
	// nil 表示「未订阅」—— 按全量推送处理，保持对旧前端（不认识
	// metrics:subscribe 的版本）的向后兼容。空集合表示「明确不关心任何主机」
	// （例如用户把所有主机都从看板移除了），此时一条都不推。
	subscription map[string]struct{}
}

// wants 判断该客户端是否需要 serverID 的指标。
//
// 未订阅（nil）→ true（兼容旧前端）；已订阅 → 只推集合内的。
func (c *MetricsHubClient) wants(serverID string) bool {
	if c.subscription == nil {
		return true
	}
	_, ok := c.subscription[serverID]
	return ok
}

// MetricsHub 管理所有前端 /metrics 命名空间的 WebSocket 连接
// 负责将 Agent 上报的实时指标广播给所有已连接的浏览器客户端
type MetricsHub struct {
	clients     map[string]*MetricsHubClient // /metrics namespace clients
	rootClients map[string]*MetricsHubClient // default namespace browser clients
	mu          sync.RWMutex
	// visibility 判定某主机是否允许在匿名命名空间广播。nil 表示未配置(放行,
	// 兼容未接线的测试构造);生产由 Service 在 New 时接上「公开状态页可见」判定。
	visibility func(serverID string) bool
}

// SetVisibilityFilter 注入广播可见性判定。回调应返回该主机是否可在匿名
// /metrics 命名空间广播(即是否出现在公开状态页上)。
func (h *MetricsHub) SetVisibilityFilter(fn func(serverID string) bool) {
	h.mu.Lock()
	h.visibility = fn
	h.mu.Unlock()
}

func (h *MetricsHub) isPubliclyVisible(serverID string) bool {
	h.mu.RLock()
	fn := h.visibility
	h.mu.RUnlock()
	if fn == nil {
		return true
	}
	return fn(serverID)
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

// metricsBroadcastAllowedKeys 是允许广播给匿名浏览器命名空间(/metrics)的
// 指标字段白名单。/socket.io/ 端点匿名可达,公开状态页依赖匿名订阅,不能靠
// 鉴权收敛,只能在广播侧只保留匿名可见字段。用白名单而非黑名单:漏列一个字段
// 不会泄露,而黑名单漏一个(例如 Agent 连接来源 IP 的 connection_ip)就会把
// 被管主机的真实地址暴露给匿名订阅者。hostname/ip/isp/org/asn/connection_ip
// 等标识字段一律不在表内。嵌套的 network/docker/gpu 等子对象整体保留。
// 经纬度等精确坐标也不广播(公开状态页按 0.1 度粗化后才展示)。
// Agent 上行数据与落库不受影响。
var metricsBroadcastAllowedKeys = map[string]struct{}{
	"cpu": {}, "cpu_usage": {}, "cpu_model": {}, "cpu_temp": {}, "cpu_power": {},
	"cpu_power_w": {}, "cpu_cores": {}, "cpu_threads": {}, "cpu_load": {},
	"cores": {}, "logical_cores": {}, "physical_cores": {},
	"load": {}, "load1": {}, "load5": {}, "load15": {},
	"mem": {}, "mem_used": {}, "mem_total": {}, "mem_used_mb": {}, "mem_total_mb": {},
	"mem_usage": {}, "mem_usage_percent": {}, "mem_percent": {}, "mem_used_raw": {},
	"mem_total_raw": {}, "memory": {}, "memory_usage": {}, "memory_used_raw": {},
	"memory_total_raw": {},
	"swap":             {}, "swap_used": {}, "swap_total": {}, "swap_usage": {},
	"disk": {}, "disk_used": {}, "disk_total": {}, "disk_usage": {}, "disk_percent": {},
	"disk_used_raw": {}, "disk_total_raw": {},
	"net_rx": {}, "net_tx": {}, "net_rx_total": {}, "net_tx_total": {},
	"net_in_speed": {}, "net_out_speed": {}, "net_in_transfer": {}, "net_out_transfer": {},
	"network_rx": {}, "network_tx": {}, "network": {},
	"connections": {}, "tcp_conn_count": {}, "udp_conn_count": {},
	"process_count": {}, "thread_count": {},
	"docker": {}, "docker_installed": {}, "docker_running": {}, "docker_stopped": {},
	"gpu": {}, "gpu_usage": {}, "gpu_mem": {}, "gpu_mem_percent": {}, "gpu_mem_used": {},
	"gpu_mem_total": {}, "gpu_model": {}, "gpu_name": {}, "gpu_temp": {}, "gpu_power": {},
	"gpu_memory": {},
	"uptime":     {}, "uptime_seconds": {}, "uptime_label": {}, "uptime_raw": {},
	"platform": {}, "platform_version": {}, "platformVersion": {}, "os": {}, "kernel": {},
	"arch":         {},
	"timestamp_ms": {}, "sequence": {}, "recorded_at": {},
	"sample_interval_ms": {}, "metrics_sample_interval_ms": {},
	"country_code": {}, "country": {}, "region": {}, "location": {},
	"resolved_country": {}, "city": {},
	"network_quality": {},
}

// sanitizeBroadcastPayload 按白名单裁剪广播负载的顶层字段。
// 只保留白名单内的键,返回新 map,调用方持有的原始数据不被修改。
func sanitizeBroadcastPayload(value interface{}) interface{} {
	typed, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	copied := make(map[string]interface{}, len(typed))
	for k, v := range typed {
		if _, allowed := metricsBroadcastAllowedKeys[k]; allowed {
			copied[k] = v
		}
	}
	return copied
}

// Subscribe 更新某个 /metrics 客户端的订阅集合。
//
// serverIDs 为空切片时表示「明确不关心任何主机」，与「从未订阅」（nil，
// 按全量推送）语义不同：前者一条都不推，后者为兼容旧前端全量推。
// maxSubscriptionSize 限制单个客户端的订阅集合大小。
//
// 这是匿名可达端点（/metrics 命名空间不需要 agent key），没有上限时
// 一个客户端可以塞几十万个 id 让服务端长期持有大 map。正常前端只会订阅
// 当前看板上的主机，几百台已远超实际；超限时截断而不是整体拒绝，
// 保证「主机多到超出上限」的用户仍然能看到前 N 台。
const maxSubscriptionSize = 2000

func (h *MetricsHub) Subscribe(sessionID string, serverIDs []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	client, ok := h.clients[sessionID]
	if !ok {
		return
	}
	next := make(map[string]struct{}, len(serverIDs))
	for _, id := range serverIDs {
		if len(next) >= maxSubscriptionSize {
			break
		}
		if id = strings.TrimSpace(id); id != "" {
			next[id] = struct{}{}
		}
	}
	client.subscription = next
}

// BroadcastMetrics 向关心该主机的客户端广播 metrics:update 事件
// data 格式: { serverId: string, metrics: {...}, timestamp: number }
//
// 以前是「推给所有客户端」：100 台主机 × N 个标签页时，服务端每秒要
// 序列化 N×100 次，且每个浏览器都收到与自己无关的流量。现在只发给
// 订阅了该 serverId 的客户端（未订阅的旧前端仍全量接收）。
// /metrics 命名空间匿名可达,只广播「公开状态页可见」主机的指标,
// 否则匿名订阅者可凭借 serverId 拿到所有私有主机的实时指标。
func (h *MetricsHub) BroadcastMetrics(serverID string, metrics map[string]interface{}) {
	if !h.isPubliclyVisible(serverID) {
		return
	}

	h.mu.RLock()
	clients := make([]*MetricsHubClient, 0, len(h.clients))
	for _, c := range h.clients {
		if c.wants(serverID) {
			clients = append(clients, c)
		}
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

	// root 命名空间同样匿名可达(公开状态页走 polling 订阅 uptime:heartbeat)。
	// 脱敏由调用方完成(见 Service.BroadcastUptimeHeartbeat 的白名单裁剪),
	// 这里不再叠加指标白名单,否则会把 uptime 心跳字段误删。
	h.broadcastPlainEvent(clients, event, data)
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
