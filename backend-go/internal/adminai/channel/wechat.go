package channel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WeChatConfig 是微信频道的明文配置（来自 admin_ai_channels.config_encrypted 解密后）。
// 与 Telegram 不同，微信的 bot_token 直接存储在本配置中（iLink 个人号 Bot 没有
// 对应的通知中心渠道类型），通过扫码登录写入。
type WeChatConfig struct {
	BotToken       string   `json:"botToken"`
	DMPolicy       string   `json:"dmPolicy"`     // "allowlist" | "open"
	AllowFrom      []string `json:"allowFrom"`    // 允许的微信用户 ID（xxx@im.wechat）
	TextChunkLimit int      `json:"textChunkLimit"` // 单条消息分片上限
}

// ilinkBaseURL 是 iLink Bot API 的默认基地址。
const ilinkBaseURL = "https://ilinkai.weixin.qq.com"

// ilinkChannelVersion 是 iLink API 协议版本号（与 QwenPaw client 保持一致）。
const ilinkChannelVersion = "2.0.1"

// ILinkClient 是 iLink Bot HTTP API 的异步客户端。
type ILinkClient struct {
	botToken string
	baseURL  string
	httpCli  *http.Client
}

// NewILinkClient 构造 ILink 客户端（botToken 可为空，用于扫码登录阶段）。
func NewILinkClient(botToken, baseURL string) *ILinkClient {
	if baseURL == "" {
		baseURL = ilinkBaseURL
	}
	return &ILinkClient{
		botToken: botToken,
		baseURL:  strings.TrimRight(baseURL, "/"),
		httpCli:  &http.Client{Timeout: 45 * time.Second},
	}
}

// ilinkHeaders 构建 iLink API 请求头。X-WECHAT-UIN 为防重放随机值（base64(随机 uint32 字符串)）。
func ilinkHeaders(botToken string) http.Header {
	maxU32 := new(big.Int).SetUint64(0xFFFFFFFF)
	n, _ := rand.Int(rand.Reader, maxU32)
	uinB64 := base64.StdEncoding.EncodeToString([]byte(n.String()))
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("AuthorizationType", "ilink_bot_token")
	h.Set("X-WECHAT-UIN", uinB64)
	if botToken != "" {
		h.Set("Authorization", "Bearer "+botToken)
	}
	return h
}

// ilinkResult 是 iLink API 的通用响应外壳。
type ilinkResult struct {
	Ret     int             `json:"ret"`
	Errcode int             `json:"errcode"`
	Body    json.RawMessage `json:"-"`
	Raw     json.RawMessage `json:"-"`
}

// GetBotQRCode 获取登录二维码。返回 {qrcode, qrcode_img_content, url}。
func (c *ILinkClient) GetBotQRCode(ctx context.Context) (map[string]interface{}, error) {
	return c.get(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3")
}

// GetQRCodeStatus 轮询二维码扫描状态。返回 {status, bot_token, baseurl}。
func (c *ILinkClient) GetQRCodeStatus(ctx context.Context, qrcode string) (map[string]interface{}, error) {
	return c.get(ctx, "/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(qrcode))
}

// WaitForLogin 阻塞等待二维码确认或超时。返回 (bot_token, base_url)。
func (c *ILinkClient) WaitForLogin(ctx context.Context, qrcode string, pollInterval, maxWait time.Duration) (string, string, error) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		data, err := c.GetQRCodeStatus(ctx, qrcode)
		if err != nil {
			return "", "", err
		}
		status, _ := data["status"].(string)
		if status == "confirmed" {
			token, _ := data["bot_token"].(string)
			baseURL, _ := data["baseurl"].(string)
			if baseURL == "" {
				baseURL = c.baseURL
			}
			return token, baseURL, nil
		}
		if status == "expired" {
			return "", "", fmt.Errorf("微信二维码已过期，请重新扫码")
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return "", "", fmt.Errorf("微信二维码等待超时（%v）", maxWait)
}

// GetUpdates 长轮询获取入站消息（服务端挂起最长约 35 秒）。
// 返回原始响应 map（含 ret, msgs, get_updates_buf）。
func (c *ILinkClient) GetUpdates(ctx context.Context, cursor string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"get_updates_buf": cursor,
		"base_info":       map[string]interface{}{"channel_version": ilinkChannelVersion},
	}
	return c.post(ctx, "/ilink/bot/getupdates", body)
}

// SendMessage 发送消息。msg 是 iLink 消息体（含 to_user_id, context_token, item_list 等）。
func (c *ILinkClient) SendMessage(ctx context.Context, msg map[string]interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"msg":      msg,
		"base_info": map[string]interface{}{"channel_version": ilinkChannelVersion},
	}
	return c.post(ctx, "/ilink/bot/sendmessage", body)
}

// SendText 是发送纯文本的便捷方法。
func (c *ILinkClient) SendText(ctx context.Context, toUserID, text, contextToken string) (map[string]interface{}, error) {
	return c.SendMessage(ctx, map[string]interface{}{
		"to_user_id":    toUserID,
		"client_id":     randomClientID(),
		"message_type":  2,
		"message_state": 2,
		"context_token": contextToken,
		"item_list":     []interface{}{map[string]interface{}{"type": 1, "text_item": map[string]interface{}{"text": text}}},
	})
}

// GetConfig 获取 Bot 配置（如 typing_ticket）。
func (c *ILinkClient) GetConfig(ctx context.Context, ilinkUserID, contextToken string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"base_info": map[string]interface{}{"channel_version": ilinkChannelVersion},
	}
	if ilinkUserID != "" {
		body["ilink_user_id"] = ilinkUserID
	}
	if contextToken != "" {
		body["context_token"] = contextToken
	}
	return c.post(ctx, "/ilink/bot/getconfig", body)
}

// SendTyping 发送打字状态指示。status: 1=开始, 2=停止。
func (c *ILinkClient) SendTyping(ctx context.Context, toUserID, typingTicket string, status int) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"ilink_user_id": toUserID,
		"typing_ticket": typingTicket,
		"status":        status,
		"base_info":     map[string]interface{}{"channel_version": ilinkChannelVersion},
	}
	return c.post(ctx, "/ilink/bot/sendtyping", body)
}

// --- 内部 HTTP 方法 ---

func (c *ILinkClient) get(ctx context.Context, path string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header = ilinkHeaders(c.botToken)
	return c.do(req)
}

func (c *ILinkClient) post(ctx context.Context, path string, body map[string]interface{}) (map[string]interface{}, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header = ilinkHeaders(c.botToken)
	return c.do(req)
}

func (c *ILinkClient) do(req *http.Request) (map[string]interface{}, error) {
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ilink 响应解析失败: %w", err)
	}
	return result, nil
}

// randomClientID 生成 UUID 风格的 client_id。
func randomClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ==================== WeChatChannel ====================

// typingState 管理单个用户的打字指示。
type typingState struct {
	ticket  string
	cancel  context.CancelFunc
	started time.Time
}

// WeChatChannel 是 iLink Bot API 长轮询实现。
// iLink Bot 不支持消息编辑/删除，因此 Edit/Delete 为空操作；
// SupportsEdit() 返回 false，上层跳过占位编辑，改为完成态一次性发送 + 打字指示。
type WeChatChannel struct {
	id        string
	cfg       WeChatConfig
	registry  *Registry
	authorize AuthorizeFunc

	mu      sync.Mutex
	state   ChannelState
	lastErr string
	started time.Time
	stop    chan struct{}
	httpCli *http.Client

	// 测试注入用：iLink API 基地址
	baseURL string
	client  *ILinkClient

	// getupdates 游标
	cursor string

	// 每用户的 context_token 缓存（发送回复时必需）。
	// 值类型为 *ctxTokenEntry，携带过期时间；由后台循环定期清理过期条目，
	// 防止频道长期运行下用户 token 无限累积。
	ctxTokens sync.Map // userID -> *ctxTokenEntry

	// 打字指示管理：userID -> *typingState
	typingMu sync.Mutex
	typing   map[string]*typingState
}

// ctxTokenTTL 是 context_token 缓存的有效期：超过后条目会被后台清理循环删除，
// 避免频道长期运行下用户 token 无限累积（token 在用户每次发消息时刷新）。
const ctxTokenTTL = 7 * 24 * time.Hour

// ctxTokenCleanupInterval 是 context_token 过期清理的扫描间隔。
const ctxTokenCleanupInterval = time.Hour

// ctxTokenEntry 是 context_token 缓存条目，携带过期时间。
type ctxTokenEntry struct {
	token     string
	expiresAt time.Time
}

// getCtxToken 读取缓存 token；已过期返回 ""。
func (w *WeChatChannel) getCtxToken(userID string) string {
	v, ok := w.ctxTokens.Load(userID)
	if !ok {
		return ""
	}
	entry, ok := v.(*ctxTokenEntry)
	if !ok || time.Now().After(entry.expiresAt) {
		return ""
	}
	return entry.token
}

// setCtxToken 缓存 token 并刷新过期时间。
func (w *WeChatChannel) setCtxToken(userID, token string) {
	w.ctxTokens.Store(userID, &ctxTokenEntry{token: token, expiresAt: time.Now().Add(ctxTokenTTL)})
}

// cleanupCtxTokensLoop 周期性清理过期 token，直到 ctx 结束或 stop 关闭。
func (w *WeChatChannel) cleanupCtxTokensLoop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(ctxTokenCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			now := time.Now()
			w.ctxTokens.Range(func(key, value any) bool {
				entry, ok := value.(*ctxTokenEntry)
				if !ok || now.After(entry.expiresAt) {
					w.ctxTokens.Delete(key)
				}
				return true
			})
		}
	}
}

// NewWeChatChannel 构造微信频道（id 如 "wechat" 或自定义频道配置 ID）。
func NewWeChatChannel(id string, cfg WeChatConfig, registry *Registry) *WeChatChannel {
	if cfg.TextChunkLimit <= 0 {
		cfg.TextChunkLimit = 4000
	}
	if cfg.DMPolicy == "" {
		cfg.DMPolicy = "open"
	}
	return &WeChatChannel{
		id:       id,
		cfg:      cfg,
		registry: registry,
		state:    StateStopped,
		httpCli:  &http.Client{Timeout: 50 * time.Second},
		baseURL:  ilinkBaseURL,
		client:   NewILinkClient(cfg.BotToken, ilinkBaseURL),
		typing:   make(map[string]*typingState),
	}
}

// SetBaseURL 覆盖 iLink API 基地址（测试注入 mock server 用）。
func (w *WeChatChannel) SetBaseURL(url string) {
	w.baseURL = url
	w.client = NewILinkClient(w.cfg.BotToken, url)
}

// SetAuthorize 注入授权判定函数。
func (w *WeChatChannel) SetAuthorize(fn AuthorizeFunc) {
	w.authorize = fn
}

func (w *WeChatChannel) ID() string { return w.id }

// SupportsEdit 返回 false：iLink Bot 不支持消息编辑。
// 上层通过类型断言检测此能力，跳过占位+编辑流式，改为打字指示 + 完成态发送。
func (w *WeChatChannel) SupportsEdit() bool { return false }

func (w *WeChatChannel) Status() ChannelStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return ChannelStatus{State: w.state, Error: w.lastErr, StartedAt: w.started}
}

// Start 启动长轮询（阻塞直到 Stop/ctx 取消）。
func (w *WeChatChannel) Start(ctx context.Context) error {
	if w.cfg.BotToken == "" {
		err := fmt.Errorf("微信频道缺少 bot_token，请先扫码登录")
		w.setError(err.Error())
		return err
	}

	w.mu.Lock()
	w.state = StateRunning
	w.started = time.Now()
	w.lastErr = ""
	w.stop = make(chan struct{})
	stop := w.stop
	w.mu.Unlock()

	slog.Info("wechat-channel-start", "channelId", w.id)
	go w.cleanupCtxTokensLoop(ctx, stop)

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

		data, err := w.client.GetUpdates(ctx, w.cursor)
		if err != nil {
			w.setError(err.Error())
			select {
			case <-ctx.Done():
				w.setStopped()
				return ctx.Err()
			case <-stop:
				w.setStopped()
				return nil
			case <-time.After(3 * time.Second):
				continue
			}
		}

		ret, _ := data["ret"].(float64)
		newCursor, _ := data["get_updates_buf"].(string)
		if newCursor != "" {
			w.cursor = newCursor
		}

		msgs, _ := data["msgs"].([]interface{})
		for _, m := range msgs {
			mm, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			w.handleMessage(mm)
		}

		// ret=-1 是正常长轮询超时（无新消息）
		if ret != 0 && ret != -1 && len(msgs) == 0 {
			slog.Warn("wechat-getupdates-nonzero", "channelId", w.id, "ret", ret)
			select {
			case <-ctx.Done():
				w.setStopped()
				return ctx.Err()
			case <-stop:
				w.setStopped()
				return nil
			case <-time.After(3 * time.Second):
			}
		}
	}
}

// Stop 停止轮询（幂等）。
func (w *WeChatChannel) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
	w.state = StateStopped
	w.stopAllTyping()
	return nil
}

// Send 发送文本消息（长文本自动分片）。
func (w *WeChatChannel) Send(ctx context.Context, to string, msg OutboundMessage) (string, error) {
	// 停止该用户的打字指示
	w.stopTyping(to)

	text := msg.Text
	if msg.Blocks != nil {
		text = w.renderBlocks(msg.Blocks, text)
	}
	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	chunks := chunkText(text, w.cfg.TextChunkLimit)
	var lastID string
	for _, chunk := range chunks {
		// 从缓存中取 context_token（发送回复时必需）
		tokenStr := w.getCtxToken(to)
		if tokenStr == "" {
			return "", fmt.Errorf("无 %s 的 context_token（用户尚未发过消息）", to)
		}
		resp, err := w.client.SendText(ctx, to, chunk, tokenStr)
		if err != nil {
			slog.Error("wechat-send-failed", "channelId", w.id, "to", to, "err", err.Error())
			return lastID, err
		}
		ret, _ := resp["ret"].(float64)
		errcode, _ := resp["errcode"].(float64)
		if ret != 0 || errcode != 0 {
			slog.Warn("wechat-send-rejected", "channelId", w.id, "ret", ret, "errcode", errcode)
			// ret=-2 表示 context_token 失效；后续分片跳过
			if ret == -2 {
				break
			}
		}
		lastID = fmt.Sprintf("%d", int64(time.Now().UnixNano()))
	}
	return lastID, nil
}

// Edit 是空操作（iLink Bot 不支持消息编辑）。
func (w *WeChatChannel) Edit(ctx context.Context, to, id string, msg OutboundMessage) error {
	return nil
}

// Delete 是空操作（iLink Bot 不支持消息删除）。
func (w *WeChatChannel) Delete(ctx context.Context, to, id string) error {
	return nil
}

// StartTyping 为指定用户启动打字指示（"对方正在输入..."）。
// 在收到入站消息时调用，在 Send 时自动停止。
func (w *WeChatChannel) StartTyping(userID string) {
	if userID == "" || w.cfg.BotToken == "" {
		return
	}
	// 先停止已有的
	w.stopTyping(userID)

	ctx, cancel := context.WithCancel(context.Background())
	ts := &typingState{cancel: cancel, started: time.Now()}

	w.typingMu.Lock()
	w.typing[userID] = ts
	w.typingMu.Unlock()

	go w.typingLoop(ctx, userID, ts)
}

// StopTyping 停止指定用户的打字指示。
func (w *WeChatChannel) StopTyping(userID string) {
	w.stopTyping(userID)
}

// stopTyping 内部停止实现。
func (w *WeChatChannel) stopTyping(userID string) {
	w.typingMu.Lock()
	ts, ok := w.typing[userID]
	if ok {
		delete(w.typing, userID)
	}
	w.typingMu.Unlock()
	if ts != nil && ts.cancel != nil {
		ts.cancel()
		// 尽力发送停止状态（不阻塞主流程）
		if ts.ticket != "" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = w.client.SendTyping(ctx, userID, ts.ticket, 2)
			}()
		}
	}
}

// stopAllTyping 停止全部打字指示（频道停止时调用）。
func (w *WeChatChannel) stopAllTyping() {
	w.typingMu.Lock()
	users := make([]string, 0, len(w.typing))
	for u := range w.typing {
		users = append(users, u)
	}
	w.typingMu.Unlock()
	for _, u := range users {
		w.stopTyping(u)
	}
}

// typingLoop 持续刷新打字指示。每 5 秒发送一次 sendtyping(status=1)，
// 直到被 cancel 或超过 120 秒自动停止。
func (w *WeChatChannel) typingLoop(ctx context.Context, userID string, ts *typingState) {
	client := w.client

	// 获取 typing_ticket
	contextToken := w.getCtxToken(userID)
	if contextToken == "" {
		return
	}

	cfgCtx, cfgCancel := context.WithTimeout(ctx, 10*time.Second)
	cfgData, err := client.GetConfig(cfgCtx, userID, contextToken)
	cfgCancel()
	if err != nil {
		slog.Debug("wechat-typing-getconfig-failed", "channelId", w.id, "err", err.Error())
		return
	}
	ticket, _ := cfgData["typing_ticket"].(string)
	if ticket == "" {
		return
	}

	w.typingMu.Lock()
	ts.ticket = ticket
	w.typingMu.Unlock()

	// 立即发送一次 typing
	typingCtx, typingCancel := context.WithTimeout(ctx, 5*time.Second)
	_, _ = client.SendTyping(typingCtx, userID, ticket, 1)
	typingCancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(ts.started) > 120*time.Second {
				w.stopTyping(userID)
				return
			}
			tCtx, tCancel := context.WithTimeout(ctx, 5*time.Second)
			_, _ = client.SendTyping(tCtx, userID, ticket, 1)
			tCancel()
		}
	}
}

// ==================== 入站消息处理 ====================

// handleMessage 解析单条 iLink 入站消息并分发到注册表。
func (w *WeChatChannel) handleMessage(msg map[string]interface{}) {
	messageType, _ := msg["message_type"].(float64)
	// message_type == 1 表示用户→Bot 消息
	if messageType != 1 {
		return
	}

	fromUserID, _ := msg["from_user_id"].(string)
	contextToken, _ := msg["context_token"].(string)
	groupID, _ := msg["group_id"].(string)

	if fromUserID == "" {
		return
	}

	// 缓存 context_token（发送回复时必需）
	if contextToken != "" {
		w.setCtxToken(fromUserID, contextToken)
	}

	// 提取文本内容
	var textParts []string
	itemList, _ := msg["item_list"].([]interface{})
	for _, item := range itemList {
		it, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		itemType, _ := it["type"].(float64)
		if itemType == 1 {
			// 文本
			textItem, _ := it["text_item"].(map[string]interface{})
			if textItem != nil {
				if t, ok := textItem["text"].(string); ok && t != "" {
					textParts = append(textParts, t)
				}
			}
		} else if itemType == 3 {
			// 语音 → ASR 转文字
			voiceItem, _ := it["voice_item"].(map[string]interface{})
			if voiceItem != nil {
				textItem, _ := voiceItem["text_item"].(map[string]interface{})
				if textItem != nil {
					if t, ok := textItem["text"].(string); ok && t != "" {
						textParts = append(textParts, t)
					}
				}
			}
		}
	}

	text := strings.Join(textParts, "\n")
	if strings.TrimSpace(text) == "" {
		return
	}

	// 授权检查
	if w.authorize != nil && !w.authorize(fromUserID, "", "dm") {
		slog.Info("wechat-unauthorized", "channelId", w.id, "userId", fromUserID)
		return
	}

	// 启动打字指示
	w.StartTyping(fromUserID)

	chatType := "dm"
	if groupID != "" {
		chatType = "group"
	}

	env := InboundEnvelope{
		ChannelID: w.id,
		ChatID:    fromUserID,
		UserID:    fromUserID,
		Text:      text,
		ChatType:  chatType,
	}
	if groupID != "" {
		env.ChatID = groupID
	}

	slog.Info("wechat-inbound", "channelId", w.id, "from", fromUserID[:min(len(fromUserID), 20)], "textLen", len(text))

	if w.registry != nil {
		w.registry.OnInbound(env)
	}
}

// ==================== 辅助方法 ====================

// renderBlocks 把结构化块渲染为纯文本（微信不支持 Markdown）。
func (w *WeChatChannel) renderBlocks(blocks []OutboundBlock, fallback string) string {
	var sb strings.Builder
	if fallback != "" {
		sb.WriteString(fallback)
		sb.WriteString("\n\n")
	}
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

// chunkText 把长文本按字符数分片。
func chunkText(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	var chunks []string
	for i := 0; i < len(runes); i += limit {
		end := i + limit
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func (w *WeChatChannel) setError(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastErr = msg
	w.state = StateError
}

func (w *WeChatChannel) setStopped() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = StateStopped
	w.stopAllTyping()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
