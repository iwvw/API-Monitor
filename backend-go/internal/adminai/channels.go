package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// channelManager 字段挂载在 Service 上（fields 在 service.go 定义）。
type channelManager struct {
	mu       sync.Mutex
	registry *channel.Registry
	cancels  map[string]context.CancelFunc // 频道配置 id -> 运行 cancel
}

// SetupChannels 初始化频道注册表与入站回调（server 启动时调用）。
func (s *Service) SetupChannels() {
	if s.chanMgr != nil {
		return // 幂等：已初始化
	}
	s.chanMgr = &channelManager{
		registry: channel.NewRegistry(),
		cancels:  make(map[string]context.CancelFunc),
	}

	auth := func(userID, username, chatType string) bool {
		db, err := s.open(context.Background())
		if err != nil {
			return false
		}
		defer db.Close()
		var count int
		err = db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
			 WHERE c.type = 'telegram' AND c.enabled = 1 AND b.channel_user_id = ?`,
			userID).Scan(&count)
		return err == nil && count > 0
	}
	// 动态注册：把当前所有启用的 telegram 频道实例加入注册表
	s.reloadChannels(auth)
	s.chanMgr.registry.SetOnInbound(s.handleChannelInbound)
	// 默认启动：服务启动后自动拉起全部已启用频道（失败仅记日志，不阻断启动）
	s.startAllEnabledChannels()
}

// startAllEnabledChannels 自动启动 DB 中全部已启用的频道。
func (s *Service) startAllEnabledChannels() {
	db, err := s.open(context.Background())
	if err != nil {
		slog.Warn("channel-auto-start", "err", err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(),
		`SELECT id FROM admin_ai_channels WHERE enabled = 1`)
	if err != nil {
		slog.Warn("channel-auto-start", "err", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if err := s.startChannelInstance(context.Background(), id); err != nil {
			slog.Warn("channel-auto-start-failed", "channelId", id, "err", err.Error())
		}
	}
}

// reloadChannels 从 DB 重建频道实例（启动或配置变更后调用）。
func (s *Service) reloadChannels(auth func(userID, username, chatType string) bool) {
	db, err := s.open(context.Background())
	if err != nil {
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(),
		`SELECT id, type, name, enabled, config_encrypted FROM admin_ai_channels WHERE enabled = 1`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, ctype, name, encrypted string
		var enabled int
		if err := rows.Scan(&id, &ctype, &name, &enabled, &encrypted); err != nil {
			continue
		}
		if ctype != "telegram" {
			continue
		}
		var cfg channel.TelegramConfig
		if err := secure.DecryptJSON(encrypted, &cfg); err != nil {
			continue
		}
		tg := channel.NewTelegramChannel(id, cfg, s.chanMgr.registry)
		tg.SetAuthorize(auth)
		s.chanMgr.registry.Register(tg)
	}
}

// startChannelInstance 启动某个频道配置的后台轮询（记录 cancel，失败返回错误）。
func (s *Service) startChannelInstance(ctx context.Context, id string) error {
	s.chanMgr.mu.Lock()
	if _, running := s.chanMgr.cancels[id]; running {
		s.chanMgr.mu.Unlock()
		return nil // 已在运行
	}
	s.chanMgr.mu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	var ctype, encrypted string
	err = db.QueryRowContext(ctx,
		`SELECT type, config_encrypted FROM admin_ai_channels WHERE id = ? AND enabled = 1`, id).Scan(&ctype, &encrypted)
	db.Close()
	if err == sql.ErrNoRows {
		return fmt.Errorf("频道不存在或未启用")
	}
	if err != nil {
		return err
	}

	auth := func(userID, username, chatType string) bool {
		adb, aerr := s.open(context.Background())
		if aerr != nil {
			return false
		}
		defer adb.Close()
		var count int
		_ = adb.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
			 WHERE c.id = ? AND c.enabled = 1 AND b.channel_user_id = ?`,
			id, userID).Scan(&count)
		return count > 0
	}

	var ch channel.Channel
	switch ctype {
	case "telegram":
		var cfg channel.TelegramConfig
		if err := secure.DecryptJSON(encrypted, &cfg); err != nil {
			return fmt.Errorf("频道配置解密失败: %w", err)
		}
		tg := channel.NewTelegramChannel(id, cfg, s.chanMgr.registry)
		tg.SetAuthorize(auth)
		ch = tg
	default:
		return fmt.Errorf("不支持的频道类型: %s", ctype)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.chanMgr.mu.Lock()
	s.chanMgr.cancels[id] = cancel
	s.chanMgr.mu.Unlock()

	s.chanMgr.registry.Register(ch)
	go func() {
		// Start 阻塞；退出后清理 cancel 注册（含异常退出）
		err := ch.Start(runCtx)
		if err != nil {
			slog.Warn("channel-poll-exited", "channelId", id, "err", err.Error())
		}
		s.chanMgr.mu.Lock()
		if c, ok := s.chanMgr.cancels[id]; ok {
			c()
			delete(s.chanMgr.cancels, id)
		}
		s.chanMgr.mu.Unlock()
	}()
	return nil
}

// stopChannelInstance 停止频道轮询。
func (s *Service) stopChannelInstance(id string) {
	s.chanMgr.mu.Lock()
	cancel, ok := s.chanMgr.cancels[id]
	if ok {
		cancel()
		delete(s.chanMgr.cancels, id)
	}
	s.chanMgr.mu.Unlock()
	if ch, exists := s.chanMgr.registry.Get(id); exists {
		_ = ch.Stop(context.Background())
	}
}

// StopAllChannels 停止全部频道（server Shutdown 时调用）。
func (s *Service) StopAllChannels() {
	if s.chanMgr == nil {
		return
	}
	s.chanMgr.mu.Lock()
	for id, cancel := range s.chanMgr.cancels {
		cancel()
		delete(s.chanMgr.cancels, id)
	}
	s.chanMgr.mu.Unlock()
	s.chanMgr.registry.SetOnInbound(nil)
}

// handleChannelInbound 处理频道入站消息：斜杠命令（通知类）→ 其余消息进入 AI 对话。
// 普通消息异步处理，避免 AI 推理耗时阻塞频道轮询（阻塞会导致后续消息积压）。
func (s *Service) handleChannelInbound(env channel.InboundEnvelope) {
	// 斜杠命令优先处理（响应快，同步执行）
	if handled, reply := s.handleChannelCommand(env); handled {
		if strings.TrimSpace(reply) != "" {
			s.sendChannelReplyReport(env, reply)
		}
		return
	}
	go s.handleChannelConversation(env)
}

// handleChannelConversation 异步执行频道普通消息对话（先发占位消息，再流式 Edit 增量呈现）。
func (s *Service) handleChannelConversation(env channel.InboundEnvelope) {
	slog.Info("channel-inbound-start", "channelId", env.ChannelID, "chatId", env.ChatID, "text", env.Text)
	// 会话键：channels 用 chatID 派生（同一对话/群组共享上下文）
	sessionID := "cha_" + env.ChatID
	source := "channel:" + env.ChannelID
	identity, _ := json.Marshal(map[string]interface{}{
		"source":    source,
		"channelId": env.ChannelID,
		"userId":    env.UserID,
		"username":  env.Username,
		"chatId":    env.ChatID,
	})

	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		s.sendChannelReplyReport(env, "⚠️ 频道未就绪。")
		return
	}
	msgID, err := ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: "⏳ 正在处理中…"})
	if err != nil {
		slog.Error("channel-reply-failed", "channelId", env.ChannelID, "chatId", env.ChatID, "err", err.Error())
		return
	}
	slog.Info("channel-placeholder-sent", "channelId", env.ChannelID, "msgId", msgID)

	runID, err := s.RunLoop(context.Background(), source, sessionID, env.Text, string(identity), "")
	if err != nil {
		s.sendChannelEdit(env, msgID, "⚠️ 执行失败："+channel.EscapeV2(err.Error()))
		return
	}
	s.streamChannelReply(env, runID, msgID)
}

// streamChannelReply 订阅 runId 事件，把 delta 文本持续 Edit 到占位消息上，实现真流式。
// Edit 节流：最小间隔 + 最小增量，避免 Telegram 对高频编辑触发 429 限流。
func (s *Service) streamChannelReply(env channel.InboundEnvelope, runID, msgID string) {
	var mu sync.Mutex
	var contents strings.Builder
	var errMsg string
	timer := time.NewTicker(streamEditInterval)
	defer timer.Stop()
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-timer.C:
				mu.Lock()
				cur := contents.String()
				mu.Unlock()
				if cur == "" {
					continue
				}
				s.sendChannelEdit(env, msgID, cur)
			}
		}
	}()

	s.subscribeRunLive(runID, func(ev SSEEvent) {
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case "delta":
			if text, ok := ev.Fields["text"].(string); ok && text != "" {
				contents.WriteString(text)
			}
		case "error":
			if m, ok := ev.Fields["message"].(string); ok {
				errMsg = m
			}
		}
	})
	close(done)

	// 收尾定型：错误优先，无输出给占位文案。
	mu.Lock()
	final := contents.String()
	if errMsg != "" {
		final = "⚠️ " + errMsg
	}
	mu.Unlock()
	if strings.TrimSpace(final) == "" {
		final = "✅ 已处理（无文本输出）。"
	}
	// 超长回复 Edit 会截断丢内容：改用 Send 分片完整发送（保留占位消息不动）。
	if len([]rune(final)) > streamEditMaxRunes {
		s.sendChannelReplyReport(env, final)
	} else {
		s.sendChannelEdit(env, msgID, final)
	}
	slog.Info("channel-inbound-done", "channelId", env.ChannelID, "chatId", env.ChatID, "runId", runID, "replyLen", len(final))
}

// streamEditInterval 是 Telegram 消息编辑的最小间隔；过小会触发 429 限流。
const streamEditInterval = 500 * time.Millisecond

// streamEditMaxRunes 是流式收尾仍可用 Edit 的单条文本上限（Telegram 单条 4096，留余量）。
const streamEditMaxRunes = 4000

// sendChannelEdit 编辑频道占位消息（流式增量更新）。调用方负责转义动态内容。
func (s *Service) sendChannelEdit(env channel.InboundEnvelope, msgID, text string) error {
	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		return fmt.Errorf("频道 %s 未注册", env.ChannelID)
	}
	err := ch.Edit(context.Background(), env.ChatID, msgID, channel.OutboundMessage{Text: text})
	if err != nil {
		slog.Error("channel-edit-failed", "channelId", env.ChannelID, "msgId", msgID, "err", err.Error(), "textLen", len(text))
	}
	return err
}

// sendChannelReplyReport 与 sendChannelReply 同，但失败时打日志并尽量回传错误，避免静默丢消息。
func (s *Service) sendChannelReplyReport(env channel.InboundEnvelope, text string) {
	msgID, err := s.sendChannelReply(env, text)
	if err != nil {
		slog.Error("channel-reply-failed", "channelId", env.ChannelID, "chatId", env.ChatID, "err", err.Error())
		return
	}
	_ = msgID
	var preview string
	if r := []rune(text); len(r) > 60 {
		preview = string(r[:60])
	} else {
		preview = text
	}
	slog.Info("channel-reply-sent", "channelId", env.ChannelID, "chatId", env.ChatID, "msgId", msgID, "preview", preview)
}

// subscribeRunLive 订阅 runId 的事件通道直至关闭，把每个事件实时回调给 onEvent。返回事件总数。
// 注意：不能从 s.runs 中 delete —— runInference 结束时由 defer 统一 close 通道并清理，
// 若在此抢先删除，runInference 的 deferred close 会因找不到条目而跳过，导致 for range 永久阻塞。
func (s *Service) subscribeRunLive(runID string, onEvent func(SSEEvent)) int {
	s.mu.Lock()
	ch, exists := s.runs[runID]
	s.mu.Unlock()
	if !exists {
		return 0
	}
	count := 0
	for event := range ch {
		count++
		if onEvent != nil {
			onEvent(event)
		}
	}
	return count
}

// channelCommandPanel 返回全中文命令面板文本（Start 就绪消息与 /help 共用）。
func channelCommandPanel() string {
	return channel.CommandPanel()
}

// handleChannelCommand 处理频道斜杠命令。返回 (是否已处理, 回复文本)。
func (s *Service) handleChannelCommand(env channel.InboundEnvelope) (bool, string) {
	text := strings.TrimSpace(env.Text)
	if text == "" {
		return false, ""
	}
	first := strings.ToLower(strings.Fields(text)[0])

	var reply string
	switch first {
	case "/start", "/开始":
		reply = channelCommandPanel()
	case "/help", "/帮助":
		reply = channelCommandPanel()
	case "/status", "/状态":
		reply = s.channelStatusReply(env)
	case "/briefing", "/简报":
		reply = s.channelBriefingReply(env)
	default:
		return false, "" // 不是已知命令，静默忽略
	}
	return true, reply
}

// channelStatusReply 生成站点实时状态中文摘要。
func (s *Service) channelStatusReply(env channel.InboundEnvelope) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := s.gatherSiteSnapshot(ctx)
	if err != nil {
		return "⚠️ 获取站点状态失败：" + channel.EscapeV2(err.Error())
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(snapshot), &data); err != nil {
		return "⚠️ 状态数据解析失败：" + channel.EscapeV2(err.Error())
	}
	var sb strings.Builder
	sb.WriteString("📊 *站点实时状态*\n")

	host := statusSection(data["/api/system/host-metrics"])
	if host != nil {
		sb.WriteString("\n💻 *系统资源*\n")
		if v, ok := str(host["hostname"]); ok {
			sb.WriteString("· 主机：" + channel.EscapeV2(v) + "\n")
		}
		if v, ok := str(host["platformLabel"]); ok {
			sb.WriteString("· 平台：" + channel.EscapeV2(v) + "\n")
		}
		if cpu, ok := host["cpu"].(map[string]interface{}); ok {
			usage := numF(cpu["usage"])
			logical := int64(numF(cpu["logicalCores"]))
			sb.WriteString(fmt.Sprintf("· CPU：%.1f%%（%d 线程）\n", usage, logical))
		}
		if mem, ok := host["memory"].(map[string]interface{}); ok {
			usage := numF(mem["usage"])
			used := numF(mem["used"])
			total := numF(mem["total"])
			sb.WriteString(fmt.Sprintf("· 内存：%.1f%%（已用 %s / 共 %s）\n", usage, formatBytes(used), formatBytes(total)))
		}
		if disk, ok := host["disk"].(map[string]interface{}); ok {
			usage := numF(disk["usage"])
			used := numF(disk["used"])
			total := numF(disk["total"])
			sb.WriteString(fmt.Sprintf("· 磁盘：%.1f%%（已用 %s / 共 %s）\n", usage, formatBytes(used), formatBytes(total)))
		}
		if v := numF(host["uptime"]); v > 0 {
			sb.WriteString("· 运行时长：" + formatUptime(v) + "\n")
		}
	}

	stats := statusSection(data["/api/system/api-stats"])
	if stats != nil {
		sb.WriteString("\n📈 *API 调用*\n")
		if total, ok := stats["total"].(map[string]interface{}); ok {
			all := int64(numF(total["all"]))
			audit := int64(numF(total["audit"]))
			ops := int64(numF(total["ops"]))
			sb.WriteString(fmt.Sprintf("· 累计调用：%s 次（审计 %s / 操作 %s）\n", formatInt(all), formatInt(audit), formatInt(ops)))
		}
		if v := numF(stats["tokens"]); v > 0 {
			sb.WriteString("· 累计 Token：" + formatTokens(v) + "\n")
		}
		if trend, ok := stats["trend"].([]interface{}); ok && len(trend) > 0 {
			if day, ok := trend[len(trend)-1].(map[string]interface{}); ok {
				if bucket, ok := str(day["bucket"]); ok {
					total := int64(numF(day["total"]))
					sb.WriteString(fmt.Sprintf("· %s 调用：%s 次\n", channel.EscapeV2(bucket), formatInt(total)))
				}
			}
		}
	}
	return sb.String()
}

// statusSection 从快照单条目提取 data 字段（内部接口经 response.OK 包装为 {success,data}）。
func statusSection(v interface{}) map[string]interface{} {
	body, _ := json.Marshal(v)
	var inner struct {
		Data map[string]interface{} `json:"data"`
	}
	if json.Unmarshal(body, &inner) != nil || inner.Data == nil {
		return nil
	}
	return inner.Data
}

// str 返回 map 中的字符串值。
func str(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != ""
}

// numF 把数值字段转 float64（JSON 数字统一为 float64）。
func numF(v interface{}) float64 {
	f, _ := v.(float64)
	return f
}

// formatInt 数字加千分位逗号。
func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if len(s) <= 3 {
		return s
	}
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}

// formatBytes 把字节数压缩为人类可读单位。
func formatBytes(n float64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%.0f B", n)
	}
	div, exp := unit, 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", n/float64(div), "KMGTPE"[exp])
}

// formatTokens 把 Token 数压缩为中文习惯单位。
func formatTokens(n float64) string {
	const (
		yi  = 100000000
		wan = 10000
	)
	switch {
	case n >= yi:
		return fmt.Sprintf("%.2f 亿", n/yi)
	case n >= wan:
		return fmt.Sprintf("%.0f 万", n/wan)
	default:
		return formatInt(int64(n))
	}
}

// formatUptime 把秒数格式化为人类可读时长。
func formatUptime(seconds float64) string {
	sec := int64(seconds)
	switch {
	case sec < 60:
		return "不足 1 分钟"
	case sec < 3600:
		return fmt.Sprintf("%d 分钟", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%d 小时 %d 分钟", sec/3600, (sec%3600)/60)
	default:
		return fmt.Sprintf("%d 天 %d 小时", sec/86400, (sec%86400)/3600)
	}
}

// channelBriefingReply 立即生成站点简报并发送到当前 chat。
func (s *Service) channelBriefingReply(env channel.InboundEnvelope) string {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	model := s.getBriefingModel(ctx)
	if model == "" {
		return "⚠️ 未配置简报模型，请在「管理 AI 设置」中配置默认模型。"
	}
	snapshot, err := s.gatherSiteSnapshot(ctx)
	if err != nil {
		return "⚠️ 收集站点状态失败：" + err.Error()
	}
	briefing, err := s.generateBriefing(ctx, model, snapshot)
	if err != nil {
		return "⚠️ 生成简报失败：" + err.Error()
	}
	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		return "⚠️ 频道未注册：" + env.ChannelID
	}
	if _, err := ch.Send(ctx, env.ChatID, channel.OutboundMessage{Text: briefing}); err != nil {
		return "⚠️ 发送简报失败：" + err.Error()
	}
	return ""
}

// sendChannelReply 向频道发送 MarkdownV2 回复。调用方负责转义动态内容。
func (s *Service) sendChannelReply(env channel.InboundEnvelope, text string) (string, error) {
	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		return "", fmt.Errorf("频道 %s 未注册", env.ChannelID)
	}
	return ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: text})
}

/* ==================== HTTP 路由 ==================== */

type channelItem struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Name      string                 `json:"name"`
	Enabled   bool                   `json:"enabled"`
	Config    map[string]interface{} `json:"config,omitempty"` // 解密后的配置（botToken 打码）
	Status    string                 `json:"status,omitempty"`
	CreatedAt string                 `json:"createdAt"`
	UpdatedAt string                 `json:"updatedAt"`
}

func maskBotToken(cfg map[string]interface{}) map[string]interface{} {
	if token, ok := cfg["botToken"].(string); ok && len(token) > 8 {
		cfg["botToken"] = token[:4] + "****" + token[len(token)-4:]
	}
	return cfg
}

// listChannels GET /api/admin-ai/channels
func (s *Service) listChannels(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(r.Context(),
		`SELECT id, type, name, enabled, config_encrypted, created_at, updated_at FROM admin_ai_channels ORDER BY created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]channelItem, 0)
	for rows.Next() {
		var item channelItem
		var enabled int
		var encrypted string
		if err := rows.Scan(&item.ID, &item.Type, &item.Name, &enabled, &encrypted, &item.CreatedAt, &item.UpdatedAt); err != nil {
			continue
		}
		item.Enabled = enabled == 1
		var cfg map[string]interface{}
		if secure.DecryptJSON(encrypted, &cfg) == nil {
			item.Config = maskBotToken(cfg)
		}
		if s.chanMgr != nil {
			if _, running := s.chanMgr.cancels[item.ID]; running {
				item.Status = "running"
			} else {
				item.Status = "stopped"
			}
		}
		items = append(items, item)
	}
	response.OK(w, map[string]interface{}{"channels": items})
}

// createChannel POST /api/admin-ai/channels
func (s *Service) createChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type    string                 `json:"type"`
		Name    string                 `json:"name"`
		Enabled bool                   `json:"enabled"`
		Config  map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Type == "" {
		req.Type = "telegram"
	}
	if req.Type != "telegram" {
		response.Error(w, http.StatusBadRequest, "v1 仅支持 telegram 频道")
		return
	}
	if req.Name == "" {
		req.Name = "Telegram"
	}
	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}
	if token, _ := req.Config["botToken"].(string); token == "" {
		response.Error(w, http.StatusBadRequest, "botToken 不能为空")
		return
	}

	encrypted, err := secure.EncryptJSON(req.Config)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "配置加密失败")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	id, err := randomID("aac_")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if req.Enabled {
		enabled = 1
	}
	_, err = db.ExecContext(r.Context(),
		`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.Type, req.Name, enabled, encrypted, now, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 默认启动：新建即监听（失败不阻碍创建，返回 started/startError 供前端提示）
	result := map[string]interface{}{"id": id, "ok": true, "started": false}
	if enabled == 1 {
		if err := s.startChannelInstance(r.Context(), id); err != nil {
			result["startError"] = err.Error()
		} else {
			result["started"] = true
		}
	}
	response.OK(w, result)
}

// updateChannel PUT /api/admin-ai/channels/{id}
func (s *Service) updateChannel(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name    *string                `json:"name"`
		Enabled *bool                  `json:"enabled"`
		Config  map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	updates := []string{}
	args := []interface{}{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Enabled != nil {
		updates = append(updates, "enabled = ?")
		enabled := 0
		if *req.Enabled {
			enabled = 1
		}
		args = append(args, enabled)
	}
	if req.Config != nil {
		// 未提供 botToken 时保留原 Token（前端编辑表单拿不到明文，只回传打码值）
		if _, hasToken := req.Config["botToken"]; !hasToken {
			var oldEncrypted string
			if err := db.QueryRowContext(r.Context(),
				`SELECT config_encrypted FROM admin_ai_channels WHERE id = ?`, id).Scan(&oldEncrypted); err == nil {
				var oldCfg map[string]interface{}
				if secure.DecryptJSON(oldEncrypted, &oldCfg) == nil {
					if t, ok := oldCfg["botToken"].(string); ok {
						req.Config["botToken"] = t
					}
				}
			}
		}
		encrypted, err := secure.EncryptJSON(req.Config)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "配置加密失败")
			return
		}
		updates = append(updates, "config_encrypted = ?")
		args = append(args, encrypted)
	}
	updates = append(updates, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	result, err := db.ExecContext(r.Context(),
		`UPDATE admin_ai_channels SET `+strings.Join(updates, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "频道不存在")
		return
	}

	// 配置变更后重启实例
	s.stopChannelInstance(id)
	if req.Enabled != nil && *req.Enabled {
		_ = s.startChannelInstance(r.Context(), id)
	} else if req.Enabled == nil {
		_ = s.startChannelInstance(r.Context(), id)
	}
	response.OK(w, map[string]interface{}{"ok": true})
}

// deleteChannel DELETE /api/admin-ai/channels/{id}
func (s *Service) deleteChannel(w http.ResponseWriter, r *http.Request, id string) {
	s.stopChannelInstance(id)

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	result, err := db.ExecContext(r.Context(), "DELETE FROM admin_ai_channels WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "频道不存在")
		return
	}
	_, _ = db.ExecContext(r.Context(), "DELETE FROM admin_ai_channel_bindings WHERE channel_id = ?", id)
	response.OK(w, map[string]interface{}{"ok": true})
}

// channelAction POST /api/admin-ai/channels/{id}/start|stop|status
func (s *Service) channelAction(w http.ResponseWriter, r *http.Request, id, action string) {
	switch action {
	case "start":
		if err := s.startChannelInstance(r.Context(), id); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"ok": true, "status": "running"})
	case "stop":
		s.stopChannelInstance(id)
		response.OK(w, map[string]interface{}{"ok": true, "status": "stopped"})
	case "status":
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		var exists int
		_ = db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM admin_ai_channels WHERE id = ?", id).Scan(&exists)
		if exists == 0 {
			response.Error(w, http.StatusNotFound, "频道不存在")
			return
		}
		state := "stopped"
		lastErr := ""
		if s.chanMgr != nil {
			s.chanMgr.mu.Lock()
			_, running := s.chanMgr.cancels[id]
			s.chanMgr.mu.Unlock()
			if running {
				state = "running"
			}
			if ch, ok := s.chanMgr.registry.Get(id); ok {
				st := ch.Status()
				if st.Error != "" {
					lastErr = st.Error
				}
			}
		}
		response.OK(w, map[string]interface{}{"id": id, "status": state, "error": lastErr})
	default:
		response.Error(w, http.StatusNotFound, "未知频道操作")
	}
}

/* ---------- 绑定管理 ---------- */

type bindingItem struct {
	ID            string `json:"id"`
	ChannelID     string `json:"channelId"`
	ChannelUserID string `json:"channelUserId"`
	ChannelName   string `json:"channelName,omitempty"`
	Username      string `json:"username,omitempty"`
	PanelUserID   string `json:"panelUserId,omitempty"`
	Role          string `json:"role,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// listBindings GET /api/admin-ai/channel-bindings
func (s *Service) listBindings(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(r.Context(),
		`SELECT b.id, b.channel_id, b.channel_user_id, COALESCE(c.name,''), COALESCE(b.channel_username,''), COALESCE(b.panel_user_id,''), COALESCE(b.role,'admin'), b.created_at
		 FROM admin_ai_channel_bindings b LEFT JOIN admin_ai_channels c ON c.id = b.channel_id
		 ORDER BY b.created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]bindingItem, 0)
	for rows.Next() {
		var item bindingItem
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelUserID, &item.ChannelName,
			&item.Username, &item.PanelUserID, &item.Role, &item.CreatedAt); err != nil {
			continue
		}
		items = append(items, item)
	}
	response.OK(w, map[string]interface{}{"bindings": items})
}

// createBinding POST /api/admin-ai/channel-bindings
func (s *Service) createBinding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID     string `json:"channelId"`
		ChannelUserID string `json:"channelUserId"`
		Username      string `json:"username"`
		PanelUserID   string `json:"panelUserId"`
		Role          string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.ChannelID == "" || req.ChannelUserID == "" {
		response.Error(w, http.StatusBadRequest, "channelId 和 channelUserId 不能为空")
		return
	}
	if req.Role == "" {
		req.Role = "admin"
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var exists int
	_ = db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM admin_ai_channels WHERE id = ?", req.ChannelID).Scan(&exists)
	if exists == 0 {
		response.Error(w, http.StatusNotFound, "频道不存在")
		return
	}

	id, err := randomID("aacb_")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO admin_ai_channel_bindings (id, channel_id, channel_user_id, channel_username, panel_user_id, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.ChannelID, req.ChannelUserID, req.Username, req.PanelUserID, req.Role, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": id, "ok": true})
}

// deleteBinding DELETE /api/admin-ai/channel-bindings/{id}
func (s *Service) deleteBinding(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	result, err := db.ExecContext(r.Context(), "DELETE FROM admin_ai_channel_bindings WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "绑定不存在")
		return
	}
	response.OK(w, map[string]interface{}{"ok": true})
}
