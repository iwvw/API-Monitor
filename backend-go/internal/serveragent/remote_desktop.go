package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	// remoteDesktopIdleTTL 是空闲上限：真实的客户端活动（浏览器轮询信令、Agent
	// 上报信令/状态）会刷新 LastActivity；持续无活动超过该时长即回收。只要标签页
	// 还开着就会每 15s 长轮询一次，因此活跃会话不会被误回收，而关闭标签页或
	// Agent 断线后的会话会被及时清理。
	remoteDesktopIdleTTL = 5 * time.Minute
	// remoteDesktopMaxLifetime 是绝对安全上限，防止异常客户端无限滞留会话。
	remoteDesktopMaxLifetime = 12 * time.Hour
	remoteDesktopSignalTTL   = 2 * time.Minute
	remoteDesktopMaxSignals  = 256
	// remoteDesktopCleanupInterval 是周期清理的节拍，避免会话只能靠请求顺带回收。
	remoteDesktopCleanupInterval = time.Minute
)

type remoteDesktopSignal struct {
	ID        int64           `json:"id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt int64           `json:"createdAt"`
}

type remoteDesktopSession struct {
	ID           string
	ServerID     string
	AgentSocket  interface{}
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastActivity time.Time
	State        string
	Signals      []remoteDesktopSignal
	NextSignalID int64
	Notify       chan struct{}
	// Superseded 标记本会话是被同一主机的「新会话」强制接管后淘汰的，而不是
	// 空闲/绝对超时回收。前端据此区分「换页签接管」与「会话失效」：前者应当
	// 停止轮询并提示已被接管（新页签已在接管），后者才需要用户手动刷新。
	Superseded bool
}

type remoteDesktopManager struct {
	mu       sync.Mutex
	sessions map[string]*remoteDesktopSession
	// superseded 记录被新会话接管而淘汰的会话 ID → 淘汰时刻。保留一个短暂的
	// TTL 窗口，只为让处于在途长轮询/信令上报的旧页签能拿到明确的「已被接管」
	// 判定；窗口过后与普通 404 无异，不长期占用内存。
	superseded map[string]time.Time
}

// remoteDesktopSupersededTTL 被接管会话的判定窗口。前端长轮询最长 20s，
// 加上重试与网络抖动，保留 2 分钟足够覆盖，且不会无限增长。
const remoteDesktopSupersededTTL = 2 * time.Minute

func newRemoteDesktopManager() *remoteDesktopManager {
	return &remoteDesktopManager{
		sessions:   make(map[string]*remoteDesktopSession),
		superseded: make(map[string]time.Time),
	}
}

// markSupersededLocked 记录会话被接管。调用方需持有 m.mu。
func (m *remoteDesktopManager) markSupersededLocked(id string) {
	if m.superseded == nil {
		m.superseded = make(map[string]time.Time)
	}
	m.superseded[id] = time.Now()
}

// isSuperseded 判断会话是否因被接管而淘汰（而非超时回收）。
func (m *remoteDesktopManager) isSuperseded(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.superseded[id]
	return ok && time.Since(at) <= remoteDesktopSupersededTTL
}

// pruneSupersededLocked 清理超出判定窗口的被接管记录。调用方需持有 m.mu。
func (m *remoteDesktopManager) pruneSupersededLocked(now time.Time) {
	for id, at := range m.superseded {
		if now.Sub(at) > remoteDesktopSupersededTTL {
			delete(m.superseded, id)
		}
	}
}

func (m *remoteDesktopManager) create(serverID string, agentSocket interface{}) *remoteDesktopSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	for id, session := range m.sessions {
		if session.ServerID == serverID {
			delete(m.sessions, id)
			// 同一主机只允许一个远程桌面会话（Agent 侧 start() 会 stop_all）。
			// 新会话创建即接管：把旧会话记入 superseded 表，使其在途的长轮询
			// 能明确拿到「已被接管」而不是含糊的 404，避免旧页签误判为终态并
			// 永久停留在死页。
			m.markSupersededLocked(id)
			// 关闭被替换会话的 Notify，让旧前端的长轮询立即返回，而不是等到超时。
			close(session.Notify)
			session.Notify = nil
		}
	}
	now := time.Now()
	session := &remoteDesktopSession{
		ID:           uuid.NewString(),
		ServerID:     serverID,
		AgentSocket:  agentSocket,
		CreatedAt:    now,
		ExpiresAt:    now.Add(remoteDesktopMaxLifetime),
		LastActivity: now,
		State:        "connecting",
		Notify:       make(chan struct{}, 1),
	}
	m.sessions[session.ID] = session
	return session
}

func (m *remoteDesktopManager) get(id string) (*remoteDesktopSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	session, ok := m.sessions[id]
	return session, ok
}

func (m *remoteDesktopManager) appendAgentSignal(id, serverID string, payload json.RawMessage) bool {
	if len(payload) == 0 || len(payload) > 128*1024 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	session, ok := m.sessions[id]
	if !ok || session.ServerID != serverID {
		return false
	}
	session.NextSignalID++
	now := time.Now()
	session.LastActivity = now
	session.Signals = append(session.Signals, remoteDesktopSignal{
		ID:        session.NextSignalID,
		Payload:   append(json.RawMessage(nil), payload...),
		CreatedAt: now.UnixMilli(),
	})
	if len(session.Signals) > remoteDesktopMaxSignals {
		session.Signals = append([]remoteDesktopSignal(nil), session.Signals[len(session.Signals)-remoteDesktopMaxSignals:]...)
	}
	notifyRemoteDesktopSession(session)
	return true
}

func (m *remoteDesktopManager) setState(id, serverID, state string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	session, ok := m.sessions[id]
	if !ok || session.ServerID != serverID {
		return false
	}
	state = strings.TrimSpace(state)
	if state != "" {
		session.State = state
	}
	session.LastActivity = time.Now()
	notifyRemoteDesktopSession(session)
	return true
}

func (m *remoteDesktopManager) waitSignals(id string, since int64, wait time.Duration) ([]remoteDesktopSignal, string, bool) {
	signals, state, ok := m.signals(id, since)
	if !ok || len(signals) > 0 || wait <= 0 {
		return signals, state, ok
	}
	m.mu.Lock()
	session, ok := m.sessions[id]
	var notify <-chan struct{}
	if ok {
		notify = session.Notify
	}
	m.mu.Unlock()
	if !ok {
		return nil, "", false
	}
	select {
	case <-notify:
	case <-time.After(wait):
	}
	return m.signals(id, since)
}
func notifyRemoteDesktopSession(session *remoteDesktopSession) {
	if session == nil || session.Notify == nil {
		return
	}
	select {
	case session.Notify <- struct{}{}:
	default:
	}
}

func (m *remoteDesktopManager) signals(id string, since int64) ([]remoteDesktopSignal, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	session, ok := m.sessions[id]
	if !ok {
		return nil, "", false
	}
	// 客户端轮询即「会话仍被使用」的心跳，刷新空闲时间。标签页关闭后不再轮询，
	// 空闲超时后由周期清理回收。
	session.LastActivity = time.Now()
	out := make([]remoteDesktopSignal, 0)
	for _, signal := range session.Signals {
		if signal.ID > since {
			out = append(out, signal)
		}
	}
	return out, session.State, true
}

func (m *remoteDesktopManager) remove(id string) (*remoteDesktopSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
		// 关闭 Notify，唤醒仍在长轮询等待该会话的 goroutine，让它们立刻返回，
		// 而不是空等最长 20 秒才靠超时退出。
		close(session.Notify)
		session.Notify = nil
	}
	return session, ok
}

// cleanupLocked 回收过期会话并裁剪过期信令。会话过期判定为「绝对上限」或
// 「空闲上限」二者取先到者：空闲时间由客户端轮询与 Agent 信令刷新，因此活跃
// 会话不会被误回收；标签页关闭或 Agent 断线后空闲超时即被清理，绝对上限则
// 兜底异常客户端。
func (m *remoteDesktopManager) cleanupLocked(now time.Time) {
	m.pruneSupersededLocked(now)
	for id, session := range m.sessions {
		if now.After(session.ExpiresAt) || now.Sub(session.LastActivity) > remoteDesktopIdleTTL {
			delete(m.sessions, id)
			close(session.Notify)
			session.Notify = nil
			continue
		}
		cutoff := now.Add(-remoteDesktopSignalTTL).UnixMilli()
		first := 0
		for first < len(session.Signals) && session.Signals[first].CreatedAt < cutoff {
			first++
		}
		if first > 0 {
			session.Signals = append([]remoteDesktopSignal(nil), session.Signals[first:]...)
		}
	}
}

// closeForServer 移除某台主机上的远程桌面会话（Agent 断线或重连时调用），
// 并关闭其 Notify 唤醒等待者，避免失效会话与失效 socket 引用滞留内存。
func (m *remoteDesktopManager) closeForServer(serverID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for id, session := range m.sessions {
		if session.ServerID != serverID {
			continue
		}
		delete(m.sessions, id)
		close(session.Notify)
		session.Notify = nil
		removed++
	}
	return removed
}

// startCleanupLoop 周期回收过期会话。此前 cleanupLocked 只在请求路径被顺带调用，
// 无任何请求时过期会话会常驻内存。
func (m *remoteDesktopManager) startCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(remoteDesktopCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			m.cleanupLocked(time.Now())
			m.mu.Unlock()
		}
	}
}

// remoteDesktopProfile 是建会话时可选的初始视频档位。指针字段区分「未提供」
// 与「显式 0」，未提供的字段由 Agent 侧回落到自身默认值。
type remoteDesktopProfile struct {
	FPS         *uint32 `json:"fps,omitempty"`
	Bitrate     *uint32 `json:"bitrate,omitempty"`
	MaxLongEdge *uint32 `json:"maxLongEdge,omitempty"`
}

type remoteDesktopCreateRequest struct {
	ServerID string                `json:"serverId"`
	Offer    json.RawMessage       `json:"offer"`
	Profile  *remoteDesktopProfile `json:"profile,omitempty"`
}

type remoteDesktopBrowserSignalRequest struct {
	Signal json.RawMessage `json:"signal"`
}

func (s *Service) handleRemoteDesktopRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	// 惰性初始化加 sync.Once，避免并发首请求下对 s.remoteDesktop 字段的无锁写竞态。
	// 生产路径在 New() 已初始化，这里只是防御性兜底。
	s.remoteDesktopOnce.Do(func() {
		if s.remoteDesktop == nil {
			s.remoteDesktop = newRemoteDesktopManager()
		}
	})
	switch {
	case len(parts) == 1 && parts[0] == "sessions" && r.Method == http.MethodPost:
		s.createRemoteDesktopSession(w, r)
	case len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodGet:
		s.getRemoteDesktopSession(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "sessions" && r.Method == http.MethodDelete:
		s.closeRemoteDesktopSession(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "signals" && r.Method == http.MethodGet:
		s.getRemoteDesktopSignals(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "signals" && r.Method == http.MethodPost:
		s.postRemoteDesktopSignal(w, r, parts[1])
	default:
		response.Error(w, http.StatusNotFound, "remote desktop route not found")
	}
}

func (s *Service) createRemoteDesktopSession(w http.ResponseWriter, r *http.Request) {
	var req remoteDesktopCreateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160*1024))
	if err := decoder.Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid remote desktop request")
		return
	}
	req.ServerID = strings.TrimSpace(req.ServerID)
	if req.ServerID == "" || len(req.Offer) == 0 {
		response.Error(w, http.StatusBadRequest, "serverId and offer are required")
		return
	}
	agentConn, ok := s.registry.Get(req.ServerID)
	if !ok {
		response.Error(w, http.StatusConflict, "Windows Agent is offline")
		return
	}
	metadata := agentConn.GetMetadata()
	platform := strings.ToLower(strings.TrimSpace(fmt.Sprint(metadata["platform"])))
	if !strings.Contains(platform, "windows") && platform != "windows" {
		response.Error(w, http.StatusBadRequest, "remote desktop is only available for Windows Agents")
		return
	}
	if !agentConn.GetCapabilities()["remote_desktop_v1"] {
		response.Error(w, http.StatusConflict, "Agent does not support remote desktop; upgrade it first")
		return
	}
	session := s.remoteDesktop.create(req.ServerID, agentConn.Socket)
	startPayload := map[string]interface{}{
		"session_id": session.ID,
		"offer":      json.RawMessage(req.Offer),
		"ice_servers": []map[string]interface{}{
			{"urls": []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}},
		},
	}
	// 面板在建会话时一并下发初始视频档位（画质档 + 帧率档），使首帧就按用户
	// 选定参数编码。缺省时不下发字段，Agent 回落到自身默认档。
	if profile := req.Profile; profile != nil {
		startPayload["profile"] = profile
	}
	// 主机侧配置的 1:1 NAT / 公网 IP 直连广播：直接下发给 Agent，使其在 TURN/打洞
	// 之外多一条确定性直连路径。未配置时不下发字段，Agent 回落到自身环境变量配置。
	if natIPs, natType, ok := s.remoteDesktopNatConfig(r.Context(), req.ServerID); ok {
		startPayload["nat_1to1_ips"] = natIPs
		startPayload["nat_1to1_candidate_type"] = natType
	}
	if err := agentConn.SendEvent("dashboard:rd_start", startPayload); err != nil {
		s.remoteDesktop.remove(session.ID)
		response.Error(w, http.StatusBadGateway, "failed to start remote desktop: "+err.Error())
		return
	}
	applog.Info(r.Context(), "serveragent", "remote desktop session created", "server_id", req.ServerID, "session_id", session.ID, "remote_addr", r.RemoteAddr)
	response.OK(w, map[string]interface{}{
		"sessionId": session.ID,
		"serverId":  session.ServerID,
		"state":     session.State,
		"expiresAt": session.ExpiresAt.UnixMilli(),
	})
}

func (s *Service) getRemoteDesktopSession(w http.ResponseWriter, _ *http.Request, id string) {
	session, ok := s.remoteDesktop.get(id)
	if !ok {
		if s.remoteDesktop.isSuperseded(id) {
			response.JSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "remote desktop session superseded by a newer connection",
				"reason":  "superseded",
			})
			return
		}
		response.Error(w, http.StatusNotFound, "remote desktop session not found")
		return
	}
	response.OK(w, map[string]interface{}{
		"sessionId": session.ID,
		"serverId":  session.ServerID,
		"state":     session.State,
		"expiresAt": session.ExpiresAt.UnixMilli(),
	})
}

func (s *Service) getRemoteDesktopSignals(w http.ResponseWriter, r *http.Request, id string) {
	since, _ := parseInt64(r.URL.Query().Get("since"))
	waitMillis, _ := parseInt64(r.URL.Query().Get("wait"))
	if waitMillis < 0 {
		waitMillis = 0
	}
	if waitMillis > 20000 {
		waitMillis = 20000
	}
	signals, state, ok := s.remoteDesktop.waitSignals(id, since, time.Duration(waitMillis)*time.Millisecond)
	if !ok {
		// 会话已被同一主机的新会话接管：给出明确的 409 + reason，前端据此停止
		// 轮询并提示「已被其他页签接管」，而不是当成终态错误要求用户刷新。
		if s.remoteDesktop.isSuperseded(id) {
			response.JSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "remote desktop session superseded by a newer connection",
				"reason":  "superseded",
			})
			return
		}
		response.Error(w, http.StatusNotFound, "remote desktop session not found")
		return
	}
	response.OK(w, map[string]interface{}{"signals": signals, "state": state})
}

func (s *Service) postRemoteDesktopSignal(w http.ResponseWriter, r *http.Request, id string) {
	session, ok := s.remoteDesktop.get(id)
	if !ok {
		response.Error(w, http.StatusNotFound, "remote desktop session not found")
		return
	}
	var req remoteDesktopBrowserSignalRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
	if err := decoder.Decode(&req); err != nil || len(req.Signal) == 0 {
		response.Error(w, http.StatusBadRequest, "invalid remote desktop signal")
		return
	}
	agentConn, online := s.registry.Get(session.ServerID)
	if !online || agentConn.Socket != session.AgentSocket {
		response.Error(w, http.StatusConflict, "Agent connection changed or went offline")
		return
	}
	if err := agentConn.SendEvent("dashboard:rd_signal", map[string]interface{}{
		"session_id": id,
		"signal":     json.RawMessage(req.Signal),
	}); err != nil {
		response.Error(w, http.StatusBadGateway, "failed to forward remote desktop signal")
		return
	}
	response.OK(w, map[string]bool{"forwarded": true})
}

func (s *Service) closeRemoteDesktopSession(w http.ResponseWriter, r *http.Request, id string) {
	session, ok := s.remoteDesktop.remove(id)
	if !ok {
		response.OK(w, map[string]bool{"closed": true})
		return
	}
	if agentConn, online := s.registry.Get(session.ServerID); online && agentConn.Socket == session.AgentSocket {
		_ = agentConn.SendEvent("dashboard:rd_stop", map[string]string{"session_id": id})
	}
	applog.Info(r.Context(), "serveragent", "remote desktop session closed", "server_id", session.ServerID, "session_id", id, "remote_addr", r.RemoteAddr)
	response.OK(w, map[string]bool{"closed": true})
}

func (s *Service) handleRemoteDesktopAgentSignal(serverID string, data json.RawMessage) {
	var event struct {
		SessionID string          `json:"session_id"`
		Signal    json.RawMessage `json:"signal"`
		State     string          `json:"state"`
	}
	if err := json.Unmarshal(data, &event); err != nil || event.SessionID == "" {
		return
	}
	if len(event.Signal) > 0 {
		s.remoteDesktop.appendAgentSignal(event.SessionID, serverID, event.Signal)
	}
	if event.State != "" {
		s.remoteDesktop.setState(event.SessionID, serverID, event.State)
	}
}

func parseInt64(raw string) (int64, error) {
	var value int64
	_, err := fmt.Sscan(strings.TrimSpace(raw), &value)
	return value, err
}

// remoteDesktopNatConfig 读取主机侧配置的 1:1 NAT / 公网 IP 广播列表与候选类型。
// 未配置（列不存在或为空）时返回 ok=false，调用方不下发该字段。列表支持逗号或
// 空白分隔，便于在输入框里一次填多个地址。
func (s *Service) remoteDesktopNatConfig(ctx context.Context, serverID string) ([]string, string, bool) {
	// 单测等精简构造的 Service 可能没有 store；此时视为未配置。
	if s.store == nil {
		return nil, "", false
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, "", false
	}
	defer db.Close()
	var rawIPs, rawType sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(remote_desktop_nat_ips, ''), COALESCE(remote_desktop_nat_type, 'srflx') FROM server_accounts WHERE id = ?`,
		serverID,
	).Scan(&rawIPs, &rawType); err != nil {
		return nil, "", false
	}
	ips := splitRemoteDesktopNatIPs(rawIPs.String)
	if len(ips) == 0 {
		return nil, "", false
	}
	return ips, normalizeRemoteDesktopNatType(rawType.String), true
}

func splitRemoteDesktopNatIPs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
