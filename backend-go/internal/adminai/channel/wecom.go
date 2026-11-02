package channel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WeComConfig 是企业微信（智能机器人·长链接模式）频道的配置。
// 凭据来自企微管理后台：工作台 → 智能机器人 → 创建机器人 → API 模式 → 通过长链接配置。
// 连接为出站 WebSocket，无需公网回调 URL。
type WeComConfig struct {
	BotID          string `json:"botId"`
	Secret         string `json:"secret"`
	WSURL          string `json:"wsUrl,omitempty"` // 测试覆盖用
	TextChunkLimit int    `json:"textChunkLimit"`
}

// wecomDefaultWS 是企微智能机器人长连接默认端点。
const wecomDefaultWS = "wss://openws.work.weixin.qq.com"

// ==================== WS 帧协议 ====================

type wecomFrameHeaders struct {
	ReqID string `json:"req_id"`
}

type wecomFrame struct {
	Cmd     string            `json:"cmd"`
	Headers wecomFrameHeaders `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Errcode int               `json:"errcode,omitempty"`
	Errmsg  string            `json:"errmsg,omitempty"`
}

// wecomReqID 生成 {prefix}_{ms}_{8hex} 格式的请求 ID。
func wecomReqID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}

// ==================== WeComChannel ====================

// wecomStreamState 跟踪一个 chat 的活跃流式气泡（企微 stream 消息）。
type wecomStreamState struct {
	id      string
	chatID  string
	content string
	lastUpd time.Time
	stopCh  chan struct{}
	stopped bool
	done    atomic.Bool // 已发送过 finish=true：绝不能二次发送（对已结束的流重推会被企微当作新消息重建，导致上一条回复整段重复）
	mu      sync.Mutex
}

func (st *wecomStreamState) stop() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.stopped {
		st.stopped = true
		close(st.stopCh)
	}
}

// inboundReqTTL 入站回调 req_id 有效期：超过后不再被动回复，避免对已失效 req_id 干等超时。
const inboundReqTTL = 60 * time.Second

// inboundReqCleanupInterval 过期入站回调 req_id 的清理扫描间隔。
const inboundReqCleanupInterval = 60 * time.Second

type inboundReqRec struct {
	reqID string
	at    time.Time
}

// cleanupInboundReqLoop 定期清理过期入站回调 req_id。req_id 需在一轮对话的
// 占位/编辑/finish 多帧间复用（deliver 用 Load），故不能每次回复即删；只能
// 由本循环按 TTL 过期删除，防止成功回复后条目长期残留。
func (w *WeComChannel) cleanupInboundReqLoop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(inboundReqCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			now := time.Now()
			w.inboundReq.Range(func(key, value any) bool {
				if rec, ok := value.(inboundReqRec); ok && now.Sub(rec.at) > inboundReqTTL {
					w.inboundReq.Delete(key)
				}
				return true
			})
		}
	}
}

// WeComChannel 是企业微信智能机器人长链接实现。
// SupportsEdit 返回 true：企微 stream 气泡支持覆盖式更新，
// 上层走占位 Send(Stream:true) + 周期 Edit 的真流式路径；
// 频道内部负责保活刷新与空闲自动定稿（finish）。
type WeComChannel struct {
	id        string
	cfg       WeComConfig
	registry  *Registry
	authorize AuthorizeFunc

	mu      sync.Mutex
	state   ChannelState
	lastErr string
	started time.Time
	stop    chan struct{}

	wsURL string

	connMu sync.Mutex
	conn   *websocket.Conn

	writeMu sync.Mutex // 序列化写帧
	pendMu  sync.Mutex
	pending map[string]chan *wecomFrame // req_id -> ack 等待（send 类命令，服务端原样回显 req_id）

	// 认证/心跳响应的 req_id 只保持前缀相同（不原样回显），需单独通道与前缀匹配。
	authMu     sync.Mutex
	authCh     chan *wecomFrame
	missedPings int32 // 连续未收到心跳 ack 计数（atomic）

	// 入站回调帧的 req_id（按 chatID 缓存）：被动回复（aibot_respond_msg）必须透传，
	// stream 气泡仅在被动回复通道上受支持。缓存带时间戳，过期后走主动推送兜底。
	inboundReq sync.Map // chatID -> inboundReqRec

	// 流式不可用标记（主动发送通道不支持 stream 等）：置位后 Edit 走防抖整条补发
	streamBroken atomic.Bool

	respondMu sync.Mutex // 串行化 respond 通道（同一 req_id 不可并发发出）

	debounceMu     sync.Mutex
	debounceText   map[string]string        // chatID -> 防抖待发内容（流式降级兜底）
	debounceTimer  map[string]*time.Timer

	dedupMu sync.Mutex
	dedup   map[string]struct{}

	streamMu sync.Mutex
	streams  map[string]*wecomStreamState // chatID -> 活跃流
}

// NewWeComChannel 构造企微频道。
func NewWeComChannel(id string, cfg WeComConfig, registry *Registry) *WeComChannel {
	if cfg.TextChunkLimit <= 0 {
		cfg.TextChunkLimit = 4000
	}
	if cfg.WSURL == "" {
		cfg.WSURL = wecomDefaultWS
	}
	return &WeComChannel{
		id:           id,
		cfg:          cfg,
		registry:     registry,
		state:        StateStopped,
		wsURL:        cfg.WSURL,
		pending:      make(map[string]chan *wecomFrame),
		debounceText: make(map[string]string),
		debounceTimer: make(map[string]*time.Timer),
		dedup:        make(map[string]struct{}),
		streams:      make(map[string]*wecomStreamState),
	}
}

// SetAuthorize 注入授权判定函数。
func (w *WeComChannel) SetAuthorize(fn AuthorizeFunc) {
	w.authorize = fn
}

func (w *WeComChannel) ID() string { return w.id }

// SupportsEdit 返回是否可用流式编辑。初始 true；流式打开/更新失败（如通道不支持 stream）后
// 置为 false，上层改走「完成态一次性发送」路径，保证内容不丢。
func (w *WeComChannel) SupportsEdit() bool { return !w.streamBroken.Load() }

func (w *WeComChannel) Status() ChannelStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return ChannelStatus{State: w.state, Error: w.lastErr, StartedAt: w.started}
}

// errWeComAuth 标记认证失败（凭据无效）：不应触发重连，直接报错终止。
var errWeComAuth = fmt.Errorf("wecom auth failed")

// Start 启动长连接（阻塞直到 Stop/ctx 取消）；断线指数退避重连。
func (w *WeComChannel) Start(ctx context.Context) error {
	if w.cfg.BotID == "" || w.cfg.Secret == "" {
		err := fmt.Errorf("企业微信频道缺少 botId / secret 配置")
		w.setError(err.Error())
		return err
	}

	w.mu.Lock()
	w.state = StateStarting
	w.stop = make(chan struct{})
	stop := w.stop
	w.mu.Unlock()
	go w.cleanupInboundReqLoop(ctx, stop)

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			w.setStopped()
			return ctx.Err()
		case <-stop:
			w.setStopped()
			return nil
		default:
		}

		err := w.runSession(ctx, stop)
		if err == nil || ctx.Err() != nil {
			w.setStopped()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return nil
		}
		if errors.Is(err, errWeComAuth) {
			// 凭据错误：重连无意义，直接失败（用户需修正 botId/secret 后重新启动）
			w.setError(err.Error())
			slog.Error("wecom-auth-failed", "channelId", w.id)
			return err
		}
		w.setError(err.Error())
		slog.Warn("wecom-session-exited", "channelId", w.id, "err", err.Error(), "reconnectIn", backoff.String())
		select {
		case <-ctx.Done():
			w.setStopped()
			return ctx.Err()
		case <-stop:
			w.setStopped()
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// runSession 建立一次 WS 会话：拨号 → 认证 → 心跳 + 读循环。返回会话错误。
func (w *WeComChannel) runSession(ctx context.Context, stop chan struct{}) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, w.wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接企微失败: %w", err)
	}
	w.connMu.Lock()
	w.conn = conn
	w.connMu.Unlock()

	defer func() {
		w.connMu.Lock()
		w.conn = nil
		w.connMu.Unlock()
		_ = conn.Close()
	}()

	// 先启动读循环再发订阅帧：否则认证响应无人读取会永久悬挂
	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	readErr := make(chan error, 1)
	go func() {
		readErr <- w.readLoop(sessCtx, conn)
	}()

	if err := w.subscribe(conn); err != nil {
		sessCancel()
		return fmt.Errorf("企微认证失败: %w", err)
	}

	w.mu.Lock()
	w.state = StateRunning
	w.started = time.Now()
	w.lastErr = ""
	w.mu.Unlock()
	slog.Info("wecom-channel-connected", "channelId", w.id)

	// 心跳循环；读循环退出或 Stop/ctx 取消即结束本会话
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			sessCancel()
			return nil
		case <-stop:
			sessCancel()
			return nil
		case err := <-readErr:
			sessCancel()
			if err != nil && err != context.Canceled {
				return err
			}
			return nil
		case <-ticker.C:
			// SDK 风格非阻塞心跳：发前检查连续未 ack 次数，发送即计数，收到 ack 由读循环清零
			if atomic.LoadInt32(&w.missedPings) >= 2 {
				sessCancel()
				return fmt.Errorf("心跳连续 %d 次无响应，重连", atomic.LoadInt32(&w.missedPings))
			}
			frame := &wecomFrame{Cmd: "ping", Headers: wecomFrameHeaders{ReqID: wecomReqID("ping")}}
			if err := w.writeFrame(conn, frame); err != nil {
				sessCancel()
				return fmt.Errorf("发送心跳失败: %w", err)
			}
			atomic.AddInt32(&w.missedPings, 1)
		}
	}
}

// subscribe 发送认证帧并等待确认（响应 req_id 仅保持 aibot_subscribe 前缀）。
func (w *WeComChannel) subscribe(conn *websocket.Conn) error {
	w.authMu.Lock()
	w.authCh = make(chan *wecomFrame, 1)
	authCh := w.authCh
	w.authMu.Unlock()

	frame := &wecomFrame{
		Cmd:     "aibot_subscribe",
		Headers: wecomFrameHeaders{ReqID: wecomReqID("aibot_subscribe")},
		Body:    mustJSON(map[string]interface{}{"bot_id": w.cfg.BotID, "secret": w.cfg.Secret}),
	}
	if err := w.writeFrame(conn, frame); err != nil {
		return fmt.Errorf("%w: 发送订阅帧失败: %v", errWeComAuth, err)
	}
	select {
	case resp := <-authCh:
		if resp.Errcode != 0 {
			return fmt.Errorf("%w: errcode=%d errmsg=%s", errWeComAuth, resp.Errcode, resp.Errmsg)
		}
		return nil
	case <-time.After(15 * time.Second):
		// 超时按可重试的网络问题处理（保持原语义），不视为凭据错误
		return fmt.Errorf("等待认证响应超时")
	}
}

// call 写帧并等待匹配 req_id 的响应。
func (w *WeComChannel) call(conn *websocket.Conn, frame *wecomFrame, timeout time.Duration) (*wecomFrame, error) {
	ch := make(chan *wecomFrame, 1)
	w.pendMu.Lock()
	w.pending[frame.Headers.ReqID] = ch
	w.pendMu.Unlock()
	defer func() {
		w.pendMu.Lock()
		delete(w.pending, frame.Headers.ReqID)
		w.pendMu.Unlock()
	}()

	if err := w.writeFrame(conn, frame); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("等待响应超时（req_id=%s）", frame.Headers.ReqID)
	}
}

func (w *WeComChannel) writeFrame(conn *websocket.Conn, frame *wecomFrame) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// readLoop 读帧并分发：消息回调 → 入站处理；其余按 req_id 匹配 ack。
func (w *WeComChannel) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var frame wecomFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			slog.Warn("wecom-frame-parse-failed", "channelId", w.id, "err", err.Error())
			continue
		}
		switch frame.Cmd {
		case "aibot_msg_callback":
			w.handleInbound(frame)
		case "aibot_event_callback":
			slog.Debug("wecom-event", "channelId", w.id)
		default:
			// 无 cmd 帧：send 回执 / 认证响应 / 心跳响应。
			// send 回执原样回显 req_id；认证与心跳响应仅保持前缀相同（对齐官方 SDK）。
			reqID := frame.Headers.ReqID
			handled := false
			if reqID != "" {
				w.pendMu.Lock()
				ch, ok := w.pending[reqID]
				if ok {
					delete(w.pending, reqID)
				}
				w.pendMu.Unlock()
				if ok {
					resp := frame
					select {
					case ch <- &resp:
					default:
					}
					handled = true
				}
			}
			if !handled && strings.HasPrefix(reqID, "aibot_subscribe") {
				w.authMu.Lock()
				ach := w.authCh
				w.authMu.Unlock()
				if ach != nil {
					resp := frame
					select {
					case ach <- &resp:
					default:
					}
				}
			} else if !handled && strings.HasPrefix(reqID, "ping") {
				atomic.StoreInt32(&w.missedPings, 0)
			}
		}
	}
}

// ==================== 入站消息 ====================

// handleInbound 解析 aibot_msg_callback 帧并分发。
// 回调帧的 req_id 会被缓存（按 chatID），被动回复（含流式）必须透传同一 req_id。
func (w *WeComChannel) handleInbound(frame wecomFrame) {
	body := frame.Body
	if len(body) == 0 {
		return
	}
	var msg struct {
		MsgID    string `json:"msgid"`
		MsgType  string `json:"msgtype"`
		From     struct {
			UserID   string `json:"userid"`
		} `json:"from"`
		ChatID   string `json:"chatid"`
		ChatType string `json:"chattype"`
		Text     struct {
			Content string `json:"content"`
		} `json:"text"`
		Voice struct {
			Content string `json:"content"`
		} `json:"voice"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		slog.Warn("wecom-msg-parse-failed", "channelId", w.id, "err", err.Error())
		return
	}
	if msg.From.UserID == "" {
		return
	}
	// 只处理文本与语音（ASR 文本）
	text := strings.TrimSpace(msg.Text.Content)
	if text == "" && msg.MsgType == "voice" {
		text = strings.TrimSpace(msg.Voice.Content)
	}
	if text == "" {
		return
	}

	// msgid 去重
	dedupKey := msg.MsgID
	if dedupKey == "" {
		dedupKey = msg.From.UserID + "_" + fmt.Sprint(time.Now().UnixNano())
	}
	w.dedupMu.Lock()
	if _, dup := w.dedup[dedupKey]; dup {
		w.dedupMu.Unlock()
		return
	}
	w.dedup[dedupKey] = struct{}{}
	if len(w.dedup) > 2000 {
		// 简单清理：重建 map
		w.dedup = make(map[string]struct{})
	}
	w.dedupMu.Unlock()

	// 授权检查
	if w.authorize != nil && !w.authorize(msg.From.UserID, "", "dm") {
		slog.Info("wecom-unauthorized", "channelId", w.id, "userId", msg.From.UserID)
		return
	}

	chatID := msg.From.UserID
	chatType := "dm"
	if msg.ChatType == "group" && msg.ChatID != "" {
		chatID = msg.ChatID
		chatType = "group"
	}

	// 缓存回调帧 req_id：被动回复（aibot_respond_msg，含流式）必须透传同一 req_id
	if frame.Headers.ReqID != "" {
		w.inboundReq.Store(chatID, inboundReqRec{reqID: frame.Headers.ReqID, at: time.Now()})
	}

	// 新消息到达时提前定稿上一轮流：避免与下一轮占位交错，且消除旧流 finish 被企微当作新消息重建的风险
	w.finishStream(chatID)

	slog.Info("wecom-inbound", "channelId", w.id, "from", msg.From.UserID, "chatType", chatType, "textLen", len(text))

	if w.registry != nil {
		w.registry.OnInbound(InboundEnvelope{
			ChannelID: w.id,
			ChatID:    chatID,
			UserID:    msg.From.UserID,
			Text:      text,
			ChatType:  chatType,
		})
	}
}

// ==================== 出站发送 ====================

// deliver 发送消息体。
// 优先被动回复通道（透传缓存的消息回调 req_id，aibot_respond_msg，支持 stream；
// 同一 req_id 通过 respondMu 串行化，避免并发覆盖 pending 匹配）。
// allowProactive=false 时失败不再降级（stream 类型主动推送不被企微支持）。
func (w *WeComChannel) deliver(chatID string, body map[string]interface{}, allowProactive bool) (*wecomFrame, error) {
	conn := w.currentConn()
	if conn == nil {
		return nil, fmt.Errorf("企微连接未就绪")
	}
	// 被动回复通道复用本消息回调的 req_id：占位/流式编辑/finish 多帧都须透传
	// 同一 req_id，故这里用 Load（不能取走即删）；条目由后台清理循环按
	// inboundReqTTL 过期删除，防止成功回复后永久残留（见 cleanupInboundReqLoop）。
	if reqIDVal, ok := w.inboundReq.Load(chatID); ok {
		rec, recOK := reqIDVal.(inboundReqRec)
		// 回调 req_id 只对「本次消息」有效：久未互动后失效，转主动推送省去 10s 干等
		if recOK && rec.reqID != "" && time.Since(rec.at) <= inboundReqTTL {
			w.respondMu.Lock()
			frame := &wecomFrame{
				Cmd:     "aibot_respond_msg",
				Headers: wecomFrameHeaders{ReqID: rec.reqID},
				Body:    mustJSON(body),
			}
			resp, err := w.call(conn, frame, 10*time.Second)
			w.respondMu.Unlock()
			if err == nil && resp.Errcode == 0 {
				return resp, nil
			}
			detail := ""
			if err != nil {
				detail = err.Error()
			} else {
				detail = fmt.Sprintf("errcode=%d %s", resp.Errcode, resp.Errmsg)
			}
			slog.Warn("wecom-respond-failed", "channelId", w.id, "chatId", chatID, "err", detail)
			if !allowProactive {
				return resp, err
			}
			// 被动回复失败（如 req_id 失效）：缓存失效，降级主动推送
			w.inboundReq.Delete(chatID)
		}
	}
	if !allowProactive {
		return nil, fmt.Errorf("无可用回复通道（缺少入站 req_id）")
	}
	body["chatid"] = chatID
	frame := &wecomFrame{
		Cmd:     "aibot_send_msg",
		Headers: wecomFrameHeaders{ReqID: wecomReqID("aibot_send_msg")},
		Body:    mustJSON(body),
	}
	return w.call(conn, frame, 10*time.Second)
}

// sendMessage 构造/发送消息（完整消息通道：markdown/媒体），失败可降级主动推送。
func (w *WeComChannel) sendMessage(ctx context.Context, chatID string, body map[string]interface{}) (*wecomFrame, error) {
	return w.deliver(chatID, body, true)
}

func (w *WeComChannel) currentConn() *websocket.Conn {
	w.connMu.Lock()
	defer w.connMu.Unlock()
	return w.conn
}

// respondStream 发送流式内容（finish 控制结束）。仅走被动回复通道，失败返回错误。
func (w *WeComChannel) respondStream(chatID, streamID, content string, finish bool) error {
	resp, err := w.deliver(chatID, map[string]interface{}{
		"msgtype": "stream",
		"stream": map[string]interface{}{
			"id":      streamID,
			"finish":  finish,
			"content": content,
		},
	}, false)
	if err != nil {
		return err
	}
	if resp != nil && resp.Errcode != 0 {
		return fmt.Errorf("errcode=%d errmsg=%s", resp.Errcode, resp.Errmsg)
	}
	return nil
}

// Send 发送消息。
// Stream:true 时开启流式气泡（aibot_respond_msg + stream.id，返回 stream_id 作为 msgID），
// 供后续 Edit 覆盖；否则发送完整 markdown 消息。
func (w *WeComChannel) Send(ctx context.Context, to string, msg OutboundMessage) (string, error) {
	text := msg.Text
	if strings.TrimSpace(text) == "" && msg.Blocks != nil {
		text = renderBlocksPlain(msg.Blocks)
	}
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	if !msg.Stream {
		// 完整消息：markdown 直发
		resp, err := w.sendMessage(ctx, to, map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]interface{}{
				"content": chunkFirst(text, w.cfg.TextChunkLimit),
			},
		})
		if err != nil {
			return "", err
		}
		if resp != nil && resp.Errcode != 0 {
			return "", fmt.Errorf("发送失败 errcode=%d errmsg=%s", resp.Errcode, resp.Errmsg)
		}
		return "", nil
	}

	// 流式占位：先定稿旧流，再开新流
	w.finishStream(to)
	sid := "st_" + wecomReqID("stream")
	if err := w.respondStream(to, sid, text, false); err != nil {
		// stream 不可用（如缺少入站 req_id 或通道不支持）：标记 broken，
		// 降级 markdown 发送占位内容，后续 Edit 由防抖兜底整条补发
		w.streamBroken.Store(true)
		slog.Warn("wecom-stream-open-failed-degrade", "channelId", w.id, "err", err.Error())
		_, _ = w.sendMessage(ctx, to, map[string]interface{}{
			"msgtype":  "markdown",
			"markdown": map[string]interface{}{"content": text},
		})
		return "", nil
	}

	st := &wecomStreamState{id: sid, chatID: to, content: text, lastUpd: time.Now(), stopCh: make(chan struct{})}
	w.streamMu.Lock()
	w.streams[to] = st
	w.streamMu.Unlock()
	go w.streamKeepalive(st)
	return sid, nil
}

// Edit 覆盖流式气泡内容（全量替换，同一 stream.id 推送）。流不可用时防抖整条补发。
func (w *WeComChannel) Edit(ctx context.Context, to, id string, msg OutboundMessage) error {
	content := msg.Text
	if content == "" && msg.Blocks != nil {
		content = renderBlocksPlain(msg.Blocks)
	}

	w.streamMu.Lock()
	st, ok := w.streams[to]
	if ok && st.id == id {
		st.content = content
		st.lastUpd = time.Now()
	}
	w.streamMu.Unlock()

	if ok && st.id == id {
		if err := w.respondStream(to, id, content, false); err != nil {
			// 流更新失败：转防抖补发，保证最终内容可达
			w.streamBroken.Store(true)
			w.clearStream(to, st)
			w.queueDebouncedReply(to, content)
		}
		return nil
	}
	// 无活跃流（流式降级或已结束）：防抖整条补发，避免最终回复静默丢失
	w.queueDebouncedReply(to, content)
	return nil
}

// Delete 定稿（finish）指定流式气泡。无匹配流或已 finish 过则静默忽略。
func (w *WeComChannel) Delete(ctx context.Context, to, id string) error {
	w.streamMu.Lock()
	st, ok := w.streams[to]
	if ok && st.id == id {
		w.streamMu.Unlock()
		w.endStream(st)
		return nil
	}
	w.streamMu.Unlock()
	return nil
}

// finishStream 定稿该 chat 的活跃流（每条流最多发送一次 finish）。
func (w *WeComChannel) finishStream(chatID string) {
	w.streamMu.Lock()
	st, ok := w.streams[chatID]
	if ok {
		delete(w.streams, chatID)
	}
	w.streamMu.Unlock()
	if !ok {
		return
	}
	w.endStream(st)
}

// endStream 对流发送 finish=true（CAS 保证每条流仅发送一次；重复推送会被企微当作新消息重建），
// 并停止其保活协程、从活跃表移除。
func (w *WeComChannel) endStream(st *wecomStreamState) {
	w.streamMu.Lock()
	if cur, ok := w.streams[st.chatID]; ok && cur == st {
		delete(w.streams, st.chatID)
	}
	w.streamMu.Unlock()
	if !st.done.CompareAndSwap(false, true) {
		return
	}
	st.stop()
	_ = w.respondStream(st.chatID, st.id, st.content, true)
}

// clearStream 清空该 chat 的活跃流（Edit 收到更新失败时调用）。
func (w *WeComChannel) clearStream(chatID string, st *wecomStreamState) {
	w.streamMu.Lock()
	if cur, ok := w.streams[chatID]; ok && cur == st {
		delete(w.streams, chatID)
	}
	w.streamMu.Unlock()
	if st != nil {
		st.stop()
	}
}

// queueDebouncedReply 防抖缓存待发内容：3 秒内无新内容则作为完整 markdown 补发。
// 用于流式通道不可用时保证最终回复不丢（不随 500ms 级 Edit 刷屏）。
func (w *WeComChannel) queueDebouncedReply(chatID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	w.debounceMu.Lock()
	w.debounceText[chatID] = text
	if t, ok := w.debounceTimer[chatID]; ok {
		t.Reset(3 * time.Second)
	} else {
		w.debounceTimer[chatID] = time.AfterFunc(3*time.Second, func() {
			w.debounceMu.Lock()
			content := w.debounceText[chatID]
			delete(w.debounceText, chatID)
			delete(w.debounceTimer, chatID)
			w.debounceMu.Unlock()
			if content == "" {
				return
			}
			_, _ = w.sendMessage(context.Background(), chatID, map[string]interface{}{
				"msgtype":  "markdown",
				"markdown": map[string]interface{}{"content": chunkFirst(content, w.cfg.TextChunkLimit)},
			})
		})
	}
	w.debounceMu.Unlock()
}

// Stop 停止频道（幂等）：断开连接、定稿全部活跃流。
func (w *WeComChannel) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
	w.mu.Unlock()
	w.streamMu.Lock()
	chats := make([]*wecomStreamState, 0, len(w.streams))
	for k, st := range w.streams {
		chats = append(chats, st)
		delete(w.streams, k)
	}
	w.streamMu.Unlock()
	for _, st := range chats {
		w.endStream(st)
	}
	w.connMu.Lock()
	conn := w.conn
	w.conn = nil
	w.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	w.mu.Lock()
	w.state = StateStopped
	w.mu.Unlock()
	return nil
}

// streamKeepalive 保活协程：
//   - idle < 12s：最近有更新，跳过
//   - 12s ≤ idle < 120s：重发最后内容保活（防止企微静默丢弃；官方限制为 10 分钟内须 finish）
//   - idle ≥ 120s：视为已结束，finish 定稿并清理
func (w *WeComChannel) streamKeepalive(st *wecomStreamState) {
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-st.stopCh:
			return
		case <-ticker.C:
			st.mu.Lock()
			idle := time.Since(st.lastUpd)
			st.mu.Unlock()
			if idle < 12*time.Second {
				continue
			}
			if idle >= 120*time.Second {
				_ = w.Delete(context.Background(), st.chatID, st.id)
				return
			}
			st.mu.Lock()
			content := st.content
			st.mu.Unlock()
			_ = w.respondStream(st.chatID, st.id, content, false)
		}
	}
}

// setError 记录运行错误。
func (w *WeComChannel) setError(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastErr = msg
	w.state = StateError
}

func (w *WeComChannel) setStopped() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = StateStopped
}

// renderBlocksPlain 把结构化块渲染为纯文本。
func renderBlocksPlain(blocks []OutboundBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "code":
			sb.WriteString("```\n")
			sb.WriteString(b.Code)
			sb.WriteString("\n```\n")
		case "error":
			sb.WriteString("⚠️ ")
			sb.WriteString(b.Title)
			sb.WriteString(": ")
			sb.WriteString(b.Text)
			sb.WriteString("\n")
		default:
			sb.WriteString(b.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// chunkFirst 取前 limit 字符（超长截断加省略标记）。
func chunkFirst(text string, limit int) string {
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…[已截断]"
}

// mustJSON 序列化；失败返回 null JSON（不应发生）。
func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}
