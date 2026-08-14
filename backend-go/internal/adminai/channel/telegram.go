package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TelegramConfig 是 Telegram 频道的明文配置（来自 admin_ai_channels.config_encrypted 解密后）。
type TelegramConfig struct {
	BotToken    string   `json:"botToken"`
	DMPolicy    string   `json:"dmPolicy"`    // "allowlist" | "open"
	AllowFrom   []string `json:"allowFrom"`   // 允许的 Telegram 用户 ID（数字字符串）
	GroupPolicy string   `json:"groupPolicy"` // "allowlist" | "open"
	Groups      map[string]struct {
		RequireMention bool `json:"requireMention"`
	} `json:"groups"`
	TextChunkLimit int `json:"textChunkLimit"` // 单条消息分片上限（Telegram 4096）
	Streaming      struct {
		Mode string `json:"mode"` // "partial" | "none"
	} `json:"streaming"`
}

// AuthorizeFunc 判定渠道用户是否被允许使用（由上层注入绑定表/白名单逻辑）。
type AuthorizeFunc func(userID, username, chatType string) bool

// TelegramChannel 是 Telegram Bot API 长轮询实现。
type TelegramChannel struct {
	id        string
	cfg       TelegramConfig
	registry  *Registry
	authorize AuthorizeFunc

	mu      sync.Mutex
	state   ChannelState
	lastErr string
	started time.Time
	stop    chan struct{}
	offset  int64
	botName string
	httpCli *http.Client
}

// NewTelegramChannel 构造 Telegram 频道（id 如 "telegram"）。
func NewTelegramChannel(id string, cfg TelegramConfig, registry *Registry) *TelegramChannel {
	if cfg.TextChunkLimit <= 0 {
		cfg.TextChunkLimit = 4000
	}
	return &TelegramChannel{
		id:       id,
		cfg:      cfg,
		registry: registry,
		state:    StateStopped,
		httpCli:  &http.Client{Timeout: 60 * time.Second},
	}
}

// SetAuthorize 注入授权判定函数（默认按 allowFrom/dmPolicy 配置判定）。
func (t *TelegramChannel) SetAuthorize(fn AuthorizeFunc) {
	t.authorize = fn
}

func (t *TelegramChannel) ID() string { return t.id }

// Status 返回运行状态。
func (t *TelegramChannel) Status() ChannelStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return ChannelStatus{State: t.state, Error: t.lastErr, StartedAt: t.started}
}

// Start 启动长轮询（阻塞直到 Stop/ctx 取消）。
func (t *TelegramChannel) Start(ctx context.Context) error {
	// getMe 校验 token 有效性
	me, err := t.callAPI(ctx, "getMe", map[string]interface{}{})
	if err != nil {
		t.setError(fmt.Sprintf("getMe 失败: %v", err))
		return err
	}
	t.botName, _ = me["username"].(string)

	t.mu.Lock()
	t.state = StateRunning
	t.started = time.Now()
	t.lastErr = ""
	t.stop = make(chan struct{})
	stop := t.stop
	t.mu.Unlock()

	// 发送就绪消息给 allowlist 用户（DM）：附中文命令面板
	for _, uid := range t.cfg.AllowFrom {
		_, _ = t.Send(ctx, uid, OutboundMessage{Text: CommandPanel()})
	}

	for {
		select {
		case <-ctx.Done():
			t.setStopped()
			return ctx.Err()
		case <-stop:
			t.setStopped()
			return nil
		default:
		}

		updates, err := t.getUpdates(ctx)
		if err != nil {
			t.setError(err.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stop:
				return nil
			case <-time.After(3 * time.Second):
				continue
			}
		}
		for _, upd := range updates {
			t.handleUpdate(upd)
		}
	}
}
// Stop 停止轮询。
func (t *TelegramChannel) Stop(ctx context.Context) error {
	t.mu.Lock()
	if t.stop != nil {
		close(t.stop)
	}
	t.state = StateStopped
	t.mu.Unlock()
	return nil
}

// Send 通过富消息（sendRichMessage）发送 Markdown 消息。
// 富消息原生支持 markdown 表格/分隔线/引用/标题；发送前做换行归一化
// （GFM 单换行会折叠成空格，需要对普通文本行之间补硬换行）。
// 若富消息不可用或内容非法，降级为普通文本 sendMessage，保证不丢消息。
func (t *TelegramChannel) Send(ctx context.Context, to string, msg OutboundMessage) (string, error) {
	text := msg.Text
	if msg.Blocks != nil {
		text = t.renderBlocks(msg.Blocks, text)
	}
	text = NormalizeRichMarkdown(text)
	chunks := t.chunkText(text)
	var lastID string
	for _, chunk := range chunks {
		result, err := t.sendRichMessage(ctx, to, chunk)
		if err != nil {
			return "", err
		}
		if id, ok := result["message_id"].(float64); ok {
			lastID = strconv.FormatInt(int64(id), 10)
		}
	}
	return lastID, nil
}

// sendRichMessage 发送富消息；失败时降级为普通文本（无 parse_mode）重发并记录错误。
func (t *TelegramChannel) sendRichMessage(ctx context.Context, to, text string) (map[string]interface{}, error) {
	richBody := map[string]interface{}{
		"chat_id": to,
		"rich_message": map[string]interface{}{
			"markdown": text,
		},
	}
	result, err := t.callAPI(ctx, "sendRichMessage", richBody)
	if err == nil {
		return result, nil
	}
	slog.Warn("telegram-send-rich-fallback", "chatId", to, "err", err.Error(), "textLen", len(text))
	// 降级：富消息不支持（旧 Bot API 服务器）或内容非法时，以普通文本发送。
	return t.callAPI(ctx, "sendMessage", map[string]interface{}{
		"chat_id": to, "text": text,
		"disable_web_page_preview": true,
	})
}

// Edit 编辑已有消息（"message is not modified" 静默忽略）。
// 优先用富消息（editMessageText + rich_message）重写为富格式；失败降级为普通文本，
// 保证流式占位消息能被持续更新。
func (t *TelegramChannel) Edit(ctx context.Context, to, id string, msg OutboundMessage) error {
	text := msg.Text
	if msg.Blocks != nil {
		text = t.renderBlocks(msg.Blocks, text)
	}
	text = NormalizeRichMarkdown(text)
	if len(text) > t.cfg.TextChunkLimit {
		text = text[:t.cfg.TextChunkLimit] + "…[已截断]"
	}
	_, err := t.callAPI(ctx, "editMessageText", map[string]interface{}{
		"chat_id": to, "message_id": id,
		"rich_message": map[string]interface{}{"markdown": text},
	})
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "message is not modified") {
		return nil
	}
	// 流式编辑竞争：前一次编辑未完成即发起新编辑，Telegram 返回 "canceled by new edit message request"。
	// 此为临时状态，后续编辑会覆盖，直接忽略，不降级为纯文本（否则消息变权码）。
	if strings.Contains(strings.ToLower(err.Error()), "canceled by new edit") {
		return nil
	}
	slog.Warn("telegram-edit-rich-fallback", "chatId", to, "msgId", id, "err", err.Error(), "textLen", len(text))
	// 降级：富消息不可用时以普通文本编辑。
	_, derr := t.callAPI(ctx, "editMessageText", map[string]interface{}{
		"chat_id": to, "message_id": id, "text": text,
		"disable_web_page_preview": true,
	})
	if derr == nil || (derr != nil && strings.Contains(strings.ToLower(derr.Error()), "message is not modified")) {
		return nil
	}
	return derr
}

// renderBlocks 把结构化块渲染为 MarkdownV2 文本。
func (t *TelegramChannel) renderBlocks(blocks []OutboundBlock, fallback string) string {
	var sb strings.Builder
	if fallback != "" {
		sb.WriteString(fallback)
		sb.WriteString("\n\n")
	}
	for _, b := range blocks {
		switch b.Type {
		case "code":
			sb.WriteString("```\n")
			sb.WriteString(EscapeCode(b.Code))
			sb.WriteString("\n```\n")
		case "error":
			sb.WriteString("⚠️ *")
			sb.WriteString(EscapeBold(b.Title))
			sb.WriteString("*: ")
			sb.WriteString(EscapeV2(b.Text))
			sb.WriteString("\n")
		default:
			sb.WriteString(EscapeV2(b.Text))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// shortenHTTP 以下为富消息相关。

// NormalizeRichMarkdown 把模型 markdown 归一化为 Telegram 富消息（GFM）可正确呈现的形态。
// 富消息 Markdown 遵循 GFM：单个换行 \n 会折叠成空格，必须空行分段或在行尾加两个空格
// 才是硬换行。这里保持代码块/表格/列表/引用/标题结构不动，仅对「连续普通文本行之间」
// 的单换行补成硬换行（行尾两个空格），避免多行普通段落被挤成一行。
func NormalizeRichMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return text
	}
	listRe := regexp.MustCompile(`^([-*+]|\d+[.)])\s`)
	hrRe := regexp.MustCompile(`^-{3,}$`)
	isPlain := func(s string) bool {
		t := strings.TrimSpace(s)
		if t == "" {
			return false
		}
		// 结构块：标题 / 引用 / 表格行 / 代码围栏 / 分隔线
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") ||
			strings.HasPrefix(t, "|") || strings.HasPrefix(t, "```") {
			return false
		}
		if listRe.MatchString(t) || hrRe.MatchString(t) {
			return false
		}
		return true
	}
	for i := 0; i < len(lines); i++ {
		if isPlain(lines[i]) && i+1 < len(lines) && isPlain(lines[i+1]) {
			// 相邻普通文本行之间补硬换行（GFM 两个空格 + \n）
			lines[i] = lines[i] + "  "
		}
	}
	return strings.Join(lines, "\n")
}

func EscapeV2(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EscapeBold 转义加粗实体内部内容（MarkdownV2 实体内部同样要求全保留字符转义）。
func EscapeBold(s string) string { return EscapeV2(s) }

// EscapeCode 转义代码块/行内代码内部的特殊字符（仅 ` 与 \ 需转义）。
func EscapeCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '`' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EscapeItalic 转义斜体实体内部内容（MarkdownV2 实体内部同样要求全保留字符转义）。
func EscapeItalic(s string) string { return EscapeV2(s) }

// EscapeLinkText 转义 MarkdownV2 链接文本内部的特殊字符（全保留字符转义）。
func EscapeLinkText(s string) string { return EscapeV2(s) }

// EscapeLinkURL 转义 MarkdownV2 链接 URL 内部的特殊字符（仅 ) 与 \ 需转义）。
func EscapeLinkURL(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ')' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// chunkText 按 textChunkLimit 分片（优先在换行处断开）。
func (t *TelegramChannel) chunkText(text string) []string {
	if len(text) <= t.cfg.TextChunkLimit {
		return []string{text}
	}
	chunks := make([]string, 0)
	rest := text
	for len(rest) > t.cfg.TextChunkLimit {
		cut := t.cfg.TextChunkLimit
		if idx := strings.LastIndex(rest[:cut], "\n"); idx > cut/2 {
			cut = idx + 1
		}
		chunks = append(chunks, rest[:cut])
		rest = rest[cut:]
	}
	if rest != "" {
		chunks = append(chunks, rest)
	}
	return chunks
}

// getUpdates 拉取增量更新（offset 游标 + 30s 长轮询）。result 是数组，单独解析。
func (t *TelegramChannel) getUpdates(ctx context.Context) ([]map[string]interface{}, error) {
	t.mu.Lock()
	offset := t.offset
	t.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"offset": offset, "timeout": 30, "allowed_updates": []string{"message"},
	})
	endpoint := "https://api.telegram.org/bot" + t.cfg.BotToken + "/getUpdates"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates 请求失败: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	var envelope struct {
		OK          bool                     `json:"ok"`
		Description string                   `json:"description"`
		Result      []map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("telegram getUpdates 响应解析失败: %w", err)
	}
	if !envelope.OK {
		return nil, fmt.Errorf("telegram getUpdates 错误: %s", envelope.Description)
	}

	updates := envelope.Result
	out := make([]map[string]interface{}, 0, len(updates))
	for _, upd := range updates {
		if id, ok := upd["update_id"].(float64); ok {
			newOffset := int64(id) + 1
			t.mu.Lock()
			if newOffset > t.offset {
				t.offset = newOffset
			}
			t.mu.Unlock()
		}
		out = append(out, upd)
	}
	return out, nil
}

// handleUpdate 处理单条更新（只处理 message）。
func (t *TelegramChannel) handleUpdate(upd map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			// 单条消息处理 panic 不得终止整个轮询
		}
	}()
	msg, ok := upd["message"].(map[string]interface{})
	if !ok {
		return
	}
	env := t.envelopeFromMessage(msg)
	if env.Text == "" {
		return
	}

	// 群组：requireMention 时仅响应 @bot
	if env.ChatType == "group" || env.ChatType == "supergroup" {
		requireMention := true
		if g, ok := t.cfg.Groups[env.ChatID]; ok {
			requireMention = g.RequireMention
		}
		if requireMention && !env.IsMention {
			return
		}
	}

	// 授权判定（DM/群组统一）
	allowed := false
	if t.authorize != nil {
		allowed = t.authorize(env.UserID, env.Username, env.ChatType)
	} else {
		policy := t.cfg.DMPolicy
		if env.ChatType != "dm" {
			policy = t.cfg.GroupPolicy
		}
		switch policy {
		case "open":
			allowed = true
		default: // allowlist
			for _, uid := range t.cfg.AllowFrom {
				if uid == env.UserID {
					allowed = true
					break
				}
			}
		}
	}
	if !allowed {
		if env.ChatType == "dm" && env.ChatID != "" {
			_, _ = t.Send(context.Background(), env.ChatID, OutboundMessage{Text: "⛔ 未授权。请先在面板中绑定你的 Telegram 用户。"})
		}
		return
	}

	if t.registry != nil {
		t.registry.OnInbound(env)
	}
}

// envelopeFromMessage 从 Telegram message 对象构造 InboundEnvelope。
func (t *TelegramChannel) envelopeFromMessage(msg map[string]interface{}) InboundEnvelope {
	env := InboundEnvelope{ChannelID: t.id}
	if chat, ok := msg["chat"].(map[string]interface{}); ok {
		if id, ok := chat["id"].(float64); ok {
			env.ChatID = strconv.FormatInt(int64(id), 10)
		}
		if typ, ok := chat["type"].(string); ok {
			env.ChatType = typ
			if env.ChatType != "private" {
				env.ChatType = "group"
			}
		}
	}
	if from, ok := msg["from"].(map[string]interface{}); ok {
		if id, ok := from["id"].(float64); ok {
			env.UserID = strconv.FormatInt(int64(id), 10)
		}
		if username, ok := from["username"].(string); ok {
			env.Username = username
		}
	}
	if text, ok := msg["text"].(string); ok {
		// 群组 @ 提及剥离
		if t.botName != "" {
			mention := "@" + t.botName
			if strings.Contains(text, mention) {
				env.IsMention = true
				text = strings.ReplaceAll(text, mention, "")
			}
		}
		env.Text = strings.TrimSpace(text)
	}
	if mid, ok := msg["message_id"].(float64); ok {
		env.MessageID = int64(mid)
	}
	if raw, err := json.Marshal(msg); err == nil {
		env.Raw = raw
	}
	return env
}

// callAPI 调用 Telegram Bot API，返回 result 对象（对象形态；数组形态见 getUpdates）。
func (t *TelegramChannel) callAPI(ctx context.Context, method string, payload map[string]interface{}) (map[string]interface{}, error) {
	body, _ := json.Marshal(payload)
	endpoint := "https://api.telegram.org/bot" + t.cfg.BotToken + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.httpCli.Do(req)
	if err != nil {
		var urlErr *url.Error
		if ok := asURLError(err, &urlErr); ok && urlErr != nil {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("telegram API 请求失败: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	var envelope struct {
		OK          bool                   `json:"ok"`
		Description string                 `json:"description"`
		Result      map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("telegram API 响应解析失败: %w", err)
	}
	if !envelope.OK {
		return nil, fmt.Errorf("telegram API 错误: %s", envelope.Description)
	}
	return envelope.Result, nil
}

func asURLError(err error, target **url.Error) bool {
	u, ok := err.(*url.Error)
	if ok {
		*target = u
	}
	return ok
}

// setError / setStopped 更新状态。
func (t *TelegramChannel) setError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = StateError
	t.lastErr = msg
}

func (t *TelegramChannel) setStopped() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = StateStopped
}
