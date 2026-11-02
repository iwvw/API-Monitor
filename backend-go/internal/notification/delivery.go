package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/tgapi"
)

// SendToChannel 把一条消息直接投递到指定通知渠道的固定目标（bot token 与目标 chat 均取自渠道配置，
// 不做规则匹配/生命周期跟踪）。用于管理 AI 等模块复用通知中心已配置的渠道做结果推送。
func (s *Service) SendToChannel(ctx context.Context, channelID, title, message string) error {
	channel, ok, err := s.loadStoredChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("通知渠道 %s 不存在", channelID)
	}
	if channel.Enabled != 1 {
		return fmt.Errorf("通知渠道 %s 已停用", channelID)
	}
	cfg := decryptConfig(channel.ConfigRaw)
	if len(cfg) == 0 {
		return fmt.Errorf("通知渠道 %s 配置为空", channelID)
	}
	_, err = s.sendToChannel(ctx, channel, cfg, title, message)
	return err
}

// SendRichToChannel 以富消息（GFM Markdown）直接投递到通知渠道，与 SendToChannel 相同定位，
// 但保留 AI 简报的 Markdown 结构（标题/加粗/表格/代码块）——SendToChannel 的逐行转义
// 是为键值式监控通知设计的，会把 AI 输出的 | 表格 |、### 标题、**加粗** 全部转义成字面量。
// Telegram 走 sendRichMessage（Bot API 富消息扩展）；非 Telegram 渠道回退 sendToChannel。
func (s *Service) SendRichToChannel(ctx context.Context, channelID, title, markdown string) error {
	channel, ok, err := s.loadStoredChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("通知渠道 %s 不存在", channelID)
	}
	if channel.Enabled != 1 {
		return fmt.Errorf("通知渠道 %s 已停用", channelID)
	}
	cfg := decryptConfig(channel.ConfigRaw)
	if len(cfg) == 0 {
		return fmt.Errorf("通知渠道 %s 配置为空", channelID)
	}
	if channel.Type == "telegram" {
		return s.sendTelegramRich(ctx, cfg, title, markdown)
	}
	_, err = s.sendToChannel(ctx, channel, cfg, title, markdown)
	return err
}

// sendTelegramRich 用富消息（sendRichMessage + rich_message.markdown）发送，
// 保留 GFM 表格/标题/加粗；不可用时降级为普通 sendMessage（无 parse_mode，纯文本不丢消息）。
func (s *Service) sendTelegramRich(ctx context.Context, cfg map[string]interface{}, title, markdown string) error {
	token := stringValue(cfg["bot_token"])
	chatID := stringValue(cfg["chat_id"])
	if token == "" || chatID == "" {
		return errors.New("telegram channel config incomplete")
	}
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(markdown)
	if title != "" {
		text = "*" + telegramEscapeBold(title) + "*\n\n" + text
	}
	payload := map[string]interface{}{
		"chat_id": chatID,
		"rich_message": map[string]interface{}{
			"markdown": text,
		},
	}
	if _, err := s.callTelegram(ctx, client, token, "sendRichMessage", payload); err == nil {
		return nil
	} else {
		// 降级：老 Bot API 服务器不支持 sendRichMessage 时以普通文本发送。
		slog.Warn("telegram-send-rich-fallback", "chatId", chatID, "err", err.Error(), "textLen", len(text))
	}
	_, err = s.callTelegram(ctx, client, token, "sendMessage", map[string]interface{}{
		"chat_id": chatID, "text": text, "disable_web_page_preview": true,
	})
	return err
}

func (s *Service) deliverLifecycleTelegram(ctx context.Context, channel storedChannel, cfg map[string]interface{}, sourceModule, eventType string, lifecycle messageLifecycle, title, message string) (deliveryResult, error) {
	state, found, err := s.loadTelegramMessageState(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return deliveryResult{}, err
	}
	if found {
		if err := s.editTelegram(ctx, cfg, state.ChatID, state.MessageID, title, message); err == nil {
			_ = s.touchTelegramMessageState(ctx, state, eventType, nil)
			return deliveryResult{ChatID: state.ChatID, MessageID: state.MessageID}, nil
		}
	}
	return s.sendTelegram(ctx, cfg, title, message)
}

func (s *Service) sendToChannel(ctx context.Context, channel storedChannel, cfg map[string]interface{}, title, message string) (deliveryResult, error) {
	switch channel.Type {
	case "email":
		return deliveryResult{}, sendEmail(cfg, title, message)
	case "telegram":
		return s.sendTelegram(ctx, cfg, title, message)
	default:
		return deliveryResult{}, fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
}

func sendEmail(cfg map[string]interface{}, title, message string) error {
	host := stringValue(cfg["host"])
	port := intValue(cfg["port"], 465)
	authMap := objectValue(cfg["auth"])
	user := stringValue(authMap["user"])
	pass := stringValue(authMap["pass"])
	to := stringDefault(cfg["to"], user)
	if host == "" || user == "" || pass == "" || to == "" {
		return errors.New("email channel config incomplete")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	auth := smtp.PlainAuth("", user, pass, host)
	from := user

	// Helper to encode headers using RFC 2047 (B-encoding)
	encodeHeader := func(s string) string {
		return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
	}

	if sender := stringValue(cfg["sender_name"]); sender != "" {
		from = fmt.Sprintf("%s <%s>", encodeHeader(sender), user)
	}

	htmlBody := emailMessageHTML(title, message)
	var encodedBody bytes.Buffer
	encoder := quotedprintable.NewWriter(&encodedBody)
	if _, err := encoder.Write([]byte(htmlBody)); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	messageBytes := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + encodeHeader(title) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		encodedBody.String() + "\r\n")

	// 全链路超时护栏：网络/认证/数据阶段的任何阻塞最多持续 smtpSendTimeout，
	// 避免 SMTP 服务器不响应时通知轮询链路被无限卡死。
	dialer := &net.Dialer{Timeout: smtpSendTimeout}
	rawConn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer rawConn.Close()
	_ = rawConn.SetDeadline(time.Now().Add(smtpSendTimeout))

	var conn net.Conn = rawConn
	if boolValue(cfg["secure"], port == 465) {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Quit()
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(user); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(messageBytes); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// callTelegram 调用 Telegram Bot API，返回 result 对象。
// 底层复用 tgapi 共享客户端（与 adminai 频道同一份 API 调用代码）。
func (s *Service) callTelegram(ctx context.Context, client *http.Client, token, method string, payload map[string]interface{}) (telegramAPIResponse, error) {
	env, err := tgapi.NewClient(token, client).Call(ctx, method, payload)
	if err != nil {
		return telegramAPIResponse{}, err
	}
	var result telegramAPIResponse
	result.OK = env.OK
	result.Description = env.Description
	if err := json.Unmarshal(env.Result, &result.Result); err != nil {
		return telegramAPIResponse{}, err
	}
	return result, nil
}

func (s *Service) sendTelegram(ctx context.Context, cfg map[string]interface{}, title, message string) (deliveryResult, error) {
	token := stringValue(cfg["bot_token"])
	chatID := stringValue(cfg["chat_id"])
	if token == "" || chatID == "" {
		return deliveryResult{}, errors.New("telegram channel config incomplete")
	}
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return deliveryResult{}, err
	}
	text := telegramMessageText(title, message)
	applog.Info(ctx, "notification", "telegram-rich-outgoing", "chatId", chatID, "textLen", len(text), "textHex", fmt.Sprintf("%x", []byte(text)))
	// 富消息优先（sendRichMessage + rich_message.markdown，GFM 宽松解析，对中文/emoji 渲染稳定）；
	// 失败时降级为普通文本 sendMessage（不带 parse_mode），避免旧客户端 MarkdownV2 解析乱码。
	richPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"rich_message":             map[string]interface{}{"markdown": text},
		"disable_web_page_preview": true,
	}
	if result, err := s.callTelegram(ctx, client, token, "sendRichMessage", richPayload); err == nil {
		if result.Result.Chat.ID != 0 {
			chatID = strconv.FormatInt(result.Result.Chat.ID, 10)
		}
		return deliveryResult{ChatID: chatID, MessageID: result.Result.MessageID}, nil
	} else {
		slog.Warn("telegram-rich-fallback", "chatId", chatID, "err", err.Error(), "textLen", len(text))
	}
	plainPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	result, err := s.callTelegram(ctx, client, token, "sendMessage", plainPayload)
	if err != nil {
		return deliveryResult{}, err
	}
	if result.Result.Chat.ID != 0 {
		chatID = strconv.FormatInt(result.Result.Chat.ID, 10)
	}
	return deliveryResult{ChatID: chatID, MessageID: result.Result.MessageID}, nil
}

func (s *Service) editTelegram(ctx context.Context, cfg map[string]interface{}, chatID string, messageID int64, title, message string) error {
	token := stringValue(cfg["bot_token"])
	if token == "" || chatID == "" || messageID == 0 {
		return errors.New("telegram message state incomplete")
	}
	text := telegramMessageText(title, message)
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	// 富消息优先（editMessageText + rich_message.markdown，GFM 宽松解析）：
	// 消息由 sendRichMessage 创建，只有用相同富格式编辑才能覆盖原内容；
	// 旧实现用 MarkdownV2 编辑会解析失败，导致 RefreshLifecycle 回退重发新消息。
	richPayload := map[string]interface{}{
		"chat_id":     chatID,
		"message_id":  messageID,
		"rich_message": map[string]interface{}{"markdown": text},
	}
	_, err = s.callTelegram(ctx, client, token, "editMessageText", richPayload)
	if err == nil {
		return nil
	}
	if telegramEditIgnore(err) {
		return nil
	}
	slog.Warn("telegram-edit-rich-fallback", "chatId", chatID, "msgId", messageID, "err", err.Error(), "textLen", len(text))
	// 降级：富消息不可用时以普通文本编辑（不带 parse_mode，避免 MarkdownV2 解析乱码）。
	plainPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	_, derr := s.callTelegram(ctx, client, token, "editMessageText", plainPayload)
	if derr == nil || telegramEditIgnore(derr) {
		return nil
	}
	return derr
}

func (s *Service) telegramHTTPClient(cfg map[string]interface{}) (*http.Client, error) {
	proxyAddress := strings.TrimSpace(stringValue(cfg["proxy_url"]))
	if proxyAddress == "" {
		return s.client, nil
	}
	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Host == "" {
		return nil, errors.New("telegram proxy URL is invalid")
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("telegram proxy scheme must be http, https, or socks5")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	timeout := s.client.Timeout
	if timeout <= 0 {
		timeout = requestTimeout
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}