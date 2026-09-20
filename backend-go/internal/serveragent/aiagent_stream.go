package serveragent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// Agent 原生流通道（AI Agent 网关数据面）说明：
//
// 云端要访问某台主机上 127.0.0.1:<port> 的 AI Agent 服务，但不占用任何公网
// 端口。做法是让主机 Agent 主动反连一条独立 WebSocket，把该 TCP 连接上的字节
// 双向搬运。云端在此之上提供一个 net.Conn 门面，交给 http.Transport 使用，从
// 而获得原生 HTTP 语义（含 SSE 流式、chunked、大 body），无需在通道上再做一层
// HTTP 解析。
//
// 一次 HTTP 请求对应一条数据通道；通道为一次性凭证、短 TTL、单次消费。
const (
	agentPortTokenTTL   = 30 * time.Second
	agentPortAttachWait = 12 * time.Second
	agentPortInboundCap = 256
	// agentPortReadLimit 是数据通道单条 WebSocket 消息的上限，需覆盖 Agent 的 32KB 分片。
	agentPortReadLimit = 64 * 1024
	// agentPortBackpressureWait 是入站缓冲满时等待消费方追上的窗口。
	agentPortBackpressureWait = 5 * time.Second

	// taskAIAgentProbe 让 Agent 探测某个 AI Agent 进程是否运行、端口是否监听。
	taskAIAgentProbe = 56
	// taskAIAgentStream 让 Agent 反连数据通道并桥接本机端口。
	taskAIAgentStream = 55
	// taskAIAgentStart 让 Agent 按 Provider 模板启动本地服务（ADR-0006）。
	taskAIAgentStart = 57
	// taskAIAgentStop 让 Agent 停止该实例的托管进程。
	taskAIAgentStop = 58
	// taskAIAgentStatus 查询该实例的托管进程状态。
	taskAIAgentStatus = 59
	// taskAIAgentDiagnose 让 Agent 诊断某 Provider 在主机侧的可用性：
	// exe 是否就绪 + 端口区间占用 + 建议空闲端口（创建/编辑实例时前端预检用）。
	taskAIAgentDiagnose = 60
)

type agentPortStream struct {
	broker    *agentPortBroker
	serverID  string
	streamID  string
	token     string
	expiresAt time.Time
	consumed  bool

	agentConn    *websocket.Conn
	agentWriteMu sync.Mutex
	inbound      chan []byte
	ready        chan struct{}
	done         chan struct{}
	readyOnce    sync.Once
	closeOnce    sync.Once
	// closeReason 记录关闭原因，用于区分「正常结束」与「异常断开」。
	closeReasonMu sync.Mutex
	closeReason   string
	// agentFailure 在 Agent 回传该任务失败时被关闭，用于快速返回具体错误。
	agentFailure chan struct{}
	failureOnce  sync.Once
	failureMu    sync.Mutex
	failureText  string
}

type agentPortBroker struct {
	mu      sync.Mutex
	streams map[string]*agentPortStream
}

func newAgentPortBroker() *agentPortBroker {
	return &agentPortBroker{streams: make(map[string]*agentPortStream)}
}

func (b *agentPortBroker) create(serverID string) *agentPortStream {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(time.Now())
	stream := &agentPortStream{
		broker:       b,
		serverID:     serverID,
		streamID:     uuid.NewString(),
		token:        randomAgentPortToken(),
		expiresAt:    time.Now().Add(agentPortTokenTTL),
		inbound:      make(chan []byte, agentPortInboundCap),
		ready:        make(chan struct{}),
		done:         make(chan struct{}),
		agentFailure: make(chan struct{}),
	}
	b.streams[stream.streamID] = stream
	return stream
}

func (b *agentPortBroker) consume(serverID, streamID, token string) (*agentPortStream, bool) {
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

func (b *agentPortBroker) remove(streamID string) {
	b.mu.Lock()
	delete(b.streams, streamID)
	b.mu.Unlock()
}

func (b *agentPortBroker) closeForServer(serverID, reason string) {
	b.mu.Lock()
	streams := make([]*agentPortStream, 0)
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

func (b *agentPortBroker) cleanupExpiredLocked(now time.Time) {
	for streamID, stream := range b.streams {
		if !stream.consumed && now.After(stream.expiresAt) {
			delete(b.streams, streamID)
		}
	}
}

// markAgentFailure 由 taskRegistry 的失败通知调用：唤醒等待方并记录具体错误。
func (b *agentPortBroker) markAgentFailure(streamID, message string) {
	b.mu.Lock()
	stream := b.streams[streamID]
	b.mu.Unlock()
	if stream == nil {
		return
	}
	stream.failureMu.Lock()
	if stream.failureText == "" {
		stream.failureText = message
	}
	stream.failureMu.Unlock()
	stream.failureOnce.Do(func() { close(stream.agentFailure) })
}

func (s *agentPortStream) failureMessage() string {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	if s.failureText == "" {
		return "agent reported failure"
	}
	return s.failureText
}

func (s *agentPortStream) attachAgent(conn *websocket.Conn) {
	// 在同一把锁下重新检查关闭状态并赋值，避免与 close() 的 TOCTOU：
	// close() 也持有 agentWriteMu，因此二者必有一个先看到对方的状态。
	s.agentWriteMu.Lock()
	select {
	case <-s.done:
		s.agentWriteMu.Unlock()
		_ = conn.Close()
		return
	default:
	}
	s.agentConn = conn
	s.agentWriteMu.Unlock()
	s.readyOnce.Do(func() { close(s.ready) })
}

func (s *agentPortStream) close(reason string) {
	s.closeOnce.Do(func() {
		s.closeReasonMu.Lock()
		s.closeReason = reason
		s.closeReasonMu.Unlock()
		close(s.done)
		// 同时唤醒等待 attach 的调用方，避免反连失败/超时后仍空等满 attach 超时。
		s.readyOnce.Do(func() { close(s.ready) })
		// 取出连接引用后在锁外做网络 I/O：避免阻塞中的 WriteControl 把并发的
		// writeChunk 卡在同一把锁上（每个通道独立锁，但同流会互相等待）。
		s.agentWriteMu.Lock()
		conn := s.agentConn
		s.agentConn = nil
		s.agentWriteMu.Unlock()
		if conn != nil {
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason), time.Now().Add(2*time.Second))
			_ = conn.Close()
		}
		if s.broker != nil {
			s.broker.remove(s.streamID)
		}
	})
}

func (s *agentPortStream) writeChunk(payload []byte) error {
	s.agentWriteMu.Lock()
	defer s.agentWriteMu.Unlock()
	if s.agentConn == nil {
		return io.ErrClosedPipe
	}
	_ = s.agentConn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
	return s.agentConn.WriteMessage(websocket.BinaryMessage, payload)
}

// handleAgentPortStream 是 Agent 反连数据通道的入口（/ws/agent-port）。
func (s *Service) handleAgentPortStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	serverID := r.URL.Query().Get("server_id")
	streamID := r.URL.Query().Get("stream_id")
	token := r.URL.Query().Get("token")
	stream, ok := s.agentPortBroker.consume(serverID, streamID, token)
	if !ok {
		response.Error(w, http.StatusUnauthorized, "invalid or expired agent port stream token")
		return
	}
	conn, err := s.terminalUpgrader().Upgrade(w, r, nil)
	if err != nil {
		stream.close("agent_port_upgrade_failed")
		return
	}
	// gorilla 默认读上限 4096 字节，而 Agent 以 32KB 分片回传；这里按分片大小
	// 放宽读上限，否则持续传输会在中途因 ErrReadLimit 断链。
	conn.SetReadLimit(agentPortReadLimit)
	defer conn.Close()
	stream.attachAgent(conn)

	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			// 只有 CloseNormalClosure(1000) 视为正常结束；其它关闭码与任何
			// 非关闭类错误（TCP reset、读上限、I/O 超时等）都按异常中断处理，
			// 否则中途截断会被 http.Transport 当成完整成功响应。
			reason := "agent_port_abnormal_closed"
			if closeErr, ok := readErr.(*websocket.CloseError); ok {
				if closeErr.Code == websocket.CloseNormalClosure {
					reason = "agent_port_closed"
				} else {
					reason = fmt.Sprintf("agent_port_abnormal_close_%d", closeErr.Code)
				}
			}
			stream.close(reason)
			return
		}
		if messageType != websocket.BinaryMessage || len(payload) == 0 {
			continue
		}
		chunk := append([]byte(nil), payload...)
		select {
		case stream.inbound <- chunk:
		default:
			// 消费方短暂落后时给一个有界等待窗口，避免把健康中的流因瞬时背压直接断开；
			// 超过窗口仍无法投递才判定为真正的背压并回收通道。
			timer := time.NewTimer(agentPortBackpressureWait)
			select {
			case stream.inbound <- chunk:
				timer.Stop()
			case <-stream.done:
				timer.Stop()
				return
			case <-timer.C:
				stream.close("agent_port_backpressure")
				return
			}
		}
	}
}

// agentPortConn 把数据通道包装成 net.Conn，供 http.Transport 直接使用。
type agentPortConn struct {
	stream     *agentPortStream
	pending    []byte
	closedOnce sync.Once

	deadlineMu     sync.Mutex
	readDeadline   time.Time
	deadlineSignal chan struct{}
}

func (c *agentPortConn) Read(target []byte) (int, error) {
	for len(c.pending) == 0 {
		// 计算本次等待的读截止：由 SetReadDeadline（http.Transport 的
		// ResponseHeaderTimeout 等）驱动，使「连上但不回数据」的对端不再无限挂起。
		var timeout <-chan time.Time
		c.deadlineMu.Lock()
		deadline := c.readDeadline
		signal := c.deadlineSignal
		c.deadlineMu.Unlock()
		if !deadline.IsZero() {
			wait := time.Until(deadline)
			if wait <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			timer := time.NewTimer(wait)
			defer timer.Stop()
			timeout = timer.C
		}
		select {
		case chunk, ok := <-c.stream.inbound:
			if !ok {
				// 通道被关闭：按异常结束处理，避免无进展地重进循环空转。
				return 0, io.ErrClosedPipe
			}
			if len(chunk) > 0 {
				c.pending = chunk
				continue
			}
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		case <-signal:
			// 读截止被更新：重新计算。
			continue
		case <-c.stream.done:
			// 通道已关闭：把缓冲里剩余数据一次性排干，避免只取一块就返回 EOF 截断响应尾部。
			for {
				select {
				case chunk, ok := <-c.stream.inbound:
					if !ok {
						return 0, c.streamEOF()
					}
					if len(chunk) > 0 {
						c.pending = append(c.pending, chunk...)
					}
				default:
					return 0, c.streamEOF()
				}
			}
		}
	}
	n := copy(target, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// streamEOF 区分正常结束与异常断开：异常原因返回非 EOF 错误，使上游（http.Transport）
// 把中途断开的响应判定为失败而不是干净结束，避免截断被当成成功。
func (c *agentPortConn) streamEOF() error {
	c.stream.closeReasonMu.Lock()
	reason := c.stream.closeReason
	c.stream.closeReasonMu.Unlock()
	switch reason {
	case "conn_closed", "agent_port_closed":
		// 正常结束：本地/对端主动关闭。
		return io.EOF
	case "":
		return io.EOF
	default:
		return fmt.Errorf("agent stream closed abnormally: %s", reason)
	}
}

func (c *agentPortConn) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	if err := c.stream.writeChunk(payload); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *agentPortConn) Close() error {
	c.closedOnce.Do(func() { c.stream.close("conn_closed") })
	return nil
}

func (c *agentPortConn) LocalAddr() net.Addr { return agentPortAddr("agent-local") }
func (c *agentPortConn) RemoteAddr() net.Addr {
	return agentPortAddr("agent-remote:" + c.stream.serverID)
}

func (c *agentPortConn) SetDeadline(deadline time.Time) error {
	return c.SetReadDeadline(deadline)
}

func (c *agentPortConn) SetReadDeadline(deadline time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = deadline
	if c.deadlineSignal == nil {
		c.deadlineSignal = make(chan struct{})
	}
	signal := c.deadlineSignal
	c.deadlineSignal = make(chan struct{})
	c.deadlineMu.Unlock()
	// 唤醒正在等待的 Read，让它按新截止时间重新计算。
	close(signal)
	return nil
}

// SetWriteDeadline 有意为空实现：底层 WebSocket 写入统一使用固定的写超时
// （terminalWriteWait），由 writeChunk 施加；调用方（http.Transport）设置的
// 写截止时间不单独生效，避免与固定超时叠加产生难以推断的中断语义。
func (c *agentPortConn) SetWriteDeadline(time.Time) error { return nil }

type agentPortAddr string

func (a agentPortAddr) Network() string { return "agent" }
func (a agentPortAddr) String() string  { return string(a) }

// OpenAIAgentPortStream 打开一条到目标主机的数据通道，并桥接到其 127.0.0.1:<port>。
// 返回的 net.Conn 可直接交给 http.Transport 使用。
func (s *Service) OpenAIAgentPortStream(ctx context.Context, serverID string, port int) (net.Conn, error) {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return nil, fmt.Errorf("agent offline")
	}
	if s.agentPortBroker == nil {
		return nil, fmt.Errorf("agent port broker unavailable")
	}
	stream := s.agentPortBroker.create(serverID)
	if stream.token == "" {
		stream.close("aiagent_stream_token_generation_failed")
		return nil, fmt.Errorf("failed to generate agent stream token")
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"port":         port,
		"stream_id":    stream.streamID,
		"stream_token": stream.token,
	})
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      stream.streamID,
		"type":    taskAIAgentStream,
		"data":    string(payload),
		"timeout": 30,
	}); err != nil {
		stream.close("aiagent_stream_task_send_failed")
		return nil, fmt.Errorf("send agent stream task: %w", err)
	}

	select {
	case <-stream.ready:
		// ready 也可能由 close() 触发（反连失败/超时）：此时通道已作废。
		select {
		case <-stream.done:
			return nil, fmt.Errorf("agent stream closed before attach")
		default:
		}
	case <-stream.agentFailure:
		// Agent 侧快速失败（如目标端口拒绝连接）会在反连之前回传 TaskResult；
		// 据此立即返回具体错误，而不是空等满 attach 超时。
		stream.close("aiagent_stream_agent_failed")
		return nil, fmt.Errorf("agent stream task failed: %s", stream.failureMessage())
	case <-time.After(agentPortAttachWait):
		stream.close("aiagent_stream_attach_timeout")
		return nil, fmt.Errorf("agent stream attach timeout")
	case <-ctx.Done():
		stream.close("aiagent_stream_context_done")
		return nil, ctx.Err()
	}
	return &agentPortConn{stream: stream}, nil
}

// RunAIAgentProbeTaskAndWait 让目标主机的 Agent 探测 AI Agent 运行时状态。
// 返回 Agent 的原始 JSON 字符串，由调用方解析。
func (s *Service) RunAIAgentProbeTaskAndWait(serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitTransient(serverID, taskAIAgentProbe, payload, 15*time.Second)
}

// RunAIAgentProbeTaskAndWaitCtx 同上，但受调用方上下文约束：探测超时会随请求
// 取消/截止提前结束，而不是固定空等 15 秒。
func (s *Service) RunAIAgentProbeTaskAndWaitCtx(ctx context.Context, serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitCtx(ctx, serverID, taskAIAgentProbe, payload, 15*time.Second)
}

// AgentSupportsAIAgentStream 判断目标主机 Agent 是否声明了原生流通道能力。
func (s *Service) AgentSupportsAIAgentStream(serverID string) bool {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return false
	}
	return conn.GetCapabilities()["aiagent_stream_v1"]
}

// AgentSupportsAIAgentLifecycle 判断目标主机 Agent 是否声明了进程生命周期能力（ADR-0006）。
func (s *Service) AgentSupportsAIAgentLifecycle(serverID string) bool {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return false
	}
	return conn.GetCapabilities()["aiagent_lifecycle_v1"]
}

// RunAIAgentStartTaskAndWaitCtx 让 Agent 启动指定实例的本地服务（任务 57）。
// 启动包含 400ms 的存活确认，给足 20 秒。
func (s *Service) RunAIAgentStartTaskAndWaitCtx(ctx context.Context, serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitCtx(ctx, serverID, taskAIAgentStart, payload, 20*time.Second)
}

// RunAIAgentStopTaskAndWaitCtx 让 Agent 停止指定实例的托管进程（任务 58）。
// 停止含优雅终止 + 强杀兜底，给足 15 秒。
func (s *Service) RunAIAgentStopTaskAndWaitCtx(ctx context.Context, serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitCtx(ctx, serverID, taskAIAgentStop, payload, 15*time.Second)
}

// RunAIAgentStatusTaskAndWaitCtx 查询指定实例的托管进程状态（任务 59）。纯本地查表，短超时。
func (s *Service) RunAIAgentStatusTaskAndWaitCtx(ctx context.Context, serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitCtx(ctx, serverID, taskAIAgentStatus, payload, 10*time.Second)
}

// RunAIAgentDiagnoseTaskAndWaitCtx 诊断某 Provider 在主机侧的可用性（任务 60）：
// exe 是否就绪、端口区间占用与建议空闲端口。纯本地查询，短超时。
func (s *Service) RunAIAgentDiagnoseTaskAndWaitCtx(ctx context.Context, serverID string, payload string) (string, error) {
	return s.runAgentTaskAndWaitCtx(ctx, serverID, taskAIAgentDiagnose, payload, 10*time.Second)
}

// HasAgentConnection 报告目标主机的 Agent 控制连接是否在线。
func (s *Service) HasAgentConnection(serverID string) bool {
	return s.hasAgentConnection(serverID)
}

// AgentServerOption 是可供 AI Agent 实例登记的主机条目。
type AgentServerOption struct {
	ID     string
	Name   string
	Host   string
	Online bool
}

// ListAgentServerOptions 返回可登记主机列表（含 Agent 在线状态）。
func (s *Service) ListAgentServerOptions(ctx context.Context) []AgentServerOption {
	options := make([]AgentServerOption, 0)
	db, err := s.open(ctx)
	if err != nil {
		applog.Warn(ctx, "serveragent", "list aiagent server options: open database failed", "error", err.Error())
		return options
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, name, COALESCE(host, '') FROM server_accounts ORDER BY name`)
	if err != nil {
		applog.Warn(ctx, "serveragent", "list aiagent server options: query failed", "error", err.Error())
		return options
	}
	defer rows.Close()
	for rows.Next() {
		var option AgentServerOption
		if err := rows.Scan(&option.ID, &option.Name, &option.Host); err != nil {
			applog.Warn(ctx, "serveragent", "list aiagent server options: scan failed", "error", err.Error())
			return options
		}
		option.Online = s.hasAgentConnection(option.ID)
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		applog.Warn(ctx, "serveragent", "list aiagent server options: rows iteration failed", "error", err.Error())
	}
	return options
}

func randomAgentPortToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		// 随机源失败时返回空串，由调用方拒绝签发，避免退化到较弱的凭证。
		return ""
	}
	return hex.EncodeToString(buf)
}
