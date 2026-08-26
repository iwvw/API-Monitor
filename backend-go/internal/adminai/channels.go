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

// authorizeByWhitelist 判定频道白名单授权：无白名单条目 = 开放模式（任何人可对话）；
// 有条目则仅 channel_user_id 命中者放行。
func authorizeByWhitelist(db *sql.DB, channelID, userID string) bool {
	var total int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM admin_ai_channel_bindings WHERE channel_id = ?`, channelID).Scan(&total); err != nil {
		return false
	}
	if total == 0 {
		return true
	}
	var hit int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM admin_ai_channel_bindings WHERE channel_id = ? AND channel_user_id = ?`,
		channelID, userID).Scan(&hit); err != nil {
		return false
	}
	return hit > 0
}

// resolveBotToken 解析频道的 bot token：优先取来源通知渠道（telegram 且启用）的 bot_token，
// 否则沿用旧配置内联的 botToken（零迁移兼容：旧频道在重新选择来源前继续可用）。
func (s *Service) resolveBotToken(ctx context.Context, notificationChannelID string, cfg channel.TelegramConfig) (string, error) {
	if notificationChannelID != "" {
		if s.src == nil {
			return "", fmt.Errorf("通知中心服务未注入，无法解析来源渠道")
		}
		ch, ok, err := s.src.LoadChannel(ctx, notificationChannelID)
		if err != nil {
			return "", fmt.Errorf("读取来源通知渠道失败: %w", err)
		}
		if !ok {
			return "", fmt.Errorf("来源通知渠道 %s 不存在", notificationChannelID)
		}
		if ch.Type != "telegram" {
			return "", fmt.Errorf("来源通知渠道 %s 不是 Telegram 渠道", notificationChannelID)
		}
		if ch.Enabled != 1 {
			return "", fmt.Errorf("来源通知渠道 %s 已停用", notificationChannelID)
		}
		token, _ := ch.Config["bot_token"].(string)
		if strings.TrimSpace(token) == "" {
			return "", fmt.Errorf("来源通知渠道 %s 缺少 bot_token 配置", notificationChannelID)
		}
		return strings.TrimSpace(token), nil
	}
	token := strings.TrimSpace(cfg.BotToken)
	if token == "" {
		return "", fmt.Errorf("频道未选择来源通知渠道，且无旧 Token 配置")
	}
	return token, nil
}

// validateNotificationSource 校验通知中心来源渠道可用（存在、telegram、启用），供创建/更新频道复用。
func (s *Service) validateNotificationSource(ctx context.Context, notificationChannelID string) error {
	if s.src == nil {
		return fmt.Errorf("通知中心服务未注入")
	}
	if notificationChannelID == "" {
		return fmt.Errorf("必须选择来源通知渠道")
	}
	ch, ok, err := s.src.LoadChannel(ctx, notificationChannelID)
	if err != nil {
		return fmt.Errorf("读取来源通知渠道失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("来源通知渠道 %s 不存在", notificationChannelID)
	}
	if ch.Type != "telegram" {
		return fmt.Errorf("来源通知渠道 %s 不是 Telegram 渠道，无法作为 AI 机器人来源", notificationChannelID)
	}
	if ch.Enabled != 1 {
		return fmt.Errorf("来源通知渠道 %s 已停用，请先在通知中心启用", notificationChannelID)
	}
	return nil
}

// sourceChannelInUse 判断该来源渠道是否已被其他 AI 频道引用（同源双长轮询会互相抢 getUpdates offset）。
func (s *Service) sourceChannelInUse(ctx context.Context, sourceID, excludeChannelID string) (bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var count int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM admin_ai_channels WHERE notification_channel_id = ? AND id <> ?`,
		sourceID, excludeChannelID).Scan(&count)
	return count > 0, err
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

	// 全局兜底授权（仅注册表重建瞬间生效，随后被 per-channel authorize 覆盖）：
	// 无任何白名单条目 = 开放模式，任何人可对话；有则仅命中者放行。
	auth := func(userID, username, chatType string) bool {
		db, err := s.open(context.Background())
		if err != nil {
			return false
		}
		defer db.Close()
		var total int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
			 WHERE c.enabled = 1`).Scan(&total); err != nil {
			return false
		}
		if total == 0 {
			return true
		}
		var count int
		err = db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
			 WHERE c.enabled = 1 AND b.channel_user_id = ?`,
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
		`SELECT id, type, name, enabled, config_encrypted, COALESCE(notification_channel_id,'') FROM admin_ai_channels WHERE enabled = 1`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, ctype, name, encrypted, sourceID string
		var enabled int
		if err := rows.Scan(&id, &ctype, &name, &enabled, &encrypted, &sourceID); err != nil {
			continue
		}
		switch ctype {
		case "telegram":
			var cfg channel.TelegramConfig
			if err := secure.DecryptJSON(encrypted, &cfg); err != nil {
				continue
			}
			token, err := s.resolveBotToken(context.Background(), sourceID, cfg)
			if err != nil {
				slog.Warn("channel-load-token", "channelId", id, "err", err.Error())
				continue
			}
			cfg.BotToken = token
			tg := channel.NewTelegramChannel(id, cfg, s.chanMgr.registry)
			tg.SetAuthorize(auth)
			s.chanMgr.registry.Register(tg)
		case "wechat":
			var wcfg channel.WeChatConfig
			if err := secure.DecryptJSON(encrypted, &wcfg); err != nil {
				continue
			}
			wc := channel.NewWeChatChannel(id, wcfg, s.chanMgr.registry)
			s.chanMgr.registry.Register(wc)
		case "wecom":
			var wccfg channel.WeComConfig
			if err := secure.DecryptJSON(encrypted, &wccfg); err != nil {
				continue
			}
			wc := channel.NewWeComChannel(id, wccfg, s.chanMgr.registry)
			s.chanMgr.registry.Register(wc)
		}
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
	var ctype, encrypted, sourceID string
	err = db.QueryRowContext(ctx,
		`SELECT type, config_encrypted, COALESCE(notification_channel_id,'') FROM admin_ai_channels WHERE id = ? AND enabled = 1`, id).Scan(&ctype, &encrypted, &sourceID)
	db.Close()
	if err == sql.ErrNoRows {
		return fmt.Errorf("频道不存在或未启用")
	}
	if err != nil {
		return err
	}

	// per-channel 白名单授权：无条目=开放，有则仅命中者放行
	auth := func(userID, username, chatType string) bool {
		adb, aerr := s.open(context.Background())
		if aerr != nil {
			return false
		}
		defer adb.Close()
		return authorizeByWhitelist(adb, id, userID)
	}

	var ch channel.Channel
	switch ctype {
	case "telegram":
		var cfg channel.TelegramConfig
		if err := secure.DecryptJSON(encrypted, &cfg); err != nil {
			return fmt.Errorf("频道配置解密失败: %w", err)
		}
		token, err := s.resolveBotToken(ctx, sourceID, cfg)
		if err != nil {
			return err
		}
		cfg.BotToken = token
		tg := channel.NewTelegramChannel(id, cfg, s.chanMgr.registry)
		tg.SetAuthorize(auth)
		ch = tg
	case "wechat":
		var wcfg channel.WeChatConfig
		if err := secure.DecryptJSON(encrypted, &wcfg); err != nil {
			return fmt.Errorf("频道配置解密失败: %w", err)
		}
		wc := channel.NewWeChatChannel(id, wcfg, s.chanMgr.registry)
		ch = wc
	case "wecom":
		var wccfg channel.WeComConfig
		if err := secure.DecryptJSON(encrypted, &wccfg); err != nil {
			return fmt.Errorf("频道配置解密失败: %w", err)
		}
		wcc := channel.NewWeComChannel(id, wccfg, s.chanMgr.registry)
		ch = wcc
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

// stopChannelInstance 停止频道轮询并从注册表移除实例：
// 残留的已停止实例会让二次 Stop 触发重复 close，也会被发送链路继续复用。
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
		s.chanMgr.registry.Unregister(id)
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
// 会话已有活跃 run（web/BOT/上一条频道消息执行中）时，消息进入 per-session 排队：
// 等待当前 run 结束后自动启动新 run 消费，不再被互斥拒绝（对齐 web 的 join 语义）。
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

	s.mu.Lock()
	_, busy := s.sessionRuns[sessionID]
	s.mu.Unlock()
	if busy {
		go s.queueChannelConversation(env, sessionID, source, string(identity))
		return
	}

	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		s.sendChannelReplyReport(env, "⚠️ 频道未就绪。")
		return
	}
	supportsEdit := true
	if se, ok := ch.(interface{ SupportsEdit() bool }); ok {
		supportsEdit = se.SupportsEdit()
	}
	var msgID string
	if supportsEdit {
		var err error
		msgID, err = ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: "⏳ 正在处理中…", Stream: true})
		if err != nil {
			slog.Error("channel-reply-failed", "channelId", env.ChannelID, "chatId", env.ChatID, "err", err.Error())
			return
		}
		slog.Info("channel-placeholder-sent", "channelId", env.ChannelID, "msgId", msgID)
	}

	runID, err := s.RunLoop(context.Background(), source, sessionID, env.Text, string(identity), "", "", nil)
	if err != nil {
		if supportsEdit {
			s.sendChannelEdit(env, msgID, "⚠️ 执行失败："+channel.EscapeV2(err.Error()))
		} else {
			s.sendChannelReplyReport(env, "⚠️ 执行失败："+err.Error())
		}
		return
	}
	s.streamChannelReply(env, runID, msgID)
}

// channelQueueTimeout 排队等待上限：超过即提示用户稍后重试，避免 goroutine 无限挂起。
// 用 var 以便测试注入短值（运行期只读，勿修改）。
var channelQueueTimeout = 5 * time.Minute

// queueChannelConversation 会话繁忙时排队：per-session 互斥锁串行化排队者
// （多排同会话时先到先处理，不会并发唤醒互相踩踏 RunLoop 互斥），
// 当前 run 结束（sessionRuns 清空）后自动启动新 run 消费本条消息。
func (s *Service) queueChannelConversation(env channel.InboundEnvelope, sessionID, source, identity string) {
	// per-session 排队锁：无锁则建（锁表只增不减，会话级数量有限）
	s.mu.Lock()
	ql, exists := s.channelQueueLocks[sessionID]
	if !exists {
		ql = &sync.Mutex{}
		s.channelQueueLocks[sessionID] = ql
	}
	s.mu.Unlock()
	ql.Lock()
	defer ql.Unlock()

	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		s.sendChannelReplyReport(env, "⚠️ 频道未就绪。")
		return
	}
	supportsEdit := true
	if se, ok := ch.(interface{ SupportsEdit() bool }); ok {
		supportsEdit = se.SupportsEdit()
	}
	msgID, err := ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: "⏳ 正在排队（上一条仍在执行），完成后自动继续…", Stream: supportsEdit})
	if err != nil {
		slog.Error("channel-reply-failed", "channelId", env.ChannelID, "chatId", env.ChatID, "err", err.Error())
		return
	}

	deadline := time.Now().Add(channelQueueTimeout)
	waited := time.Duration(0)
	for {
		s.mu.Lock()
		_, busy := s.sessionRuns[sessionID]
		s.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			if supportsEdit {
				_ = s.sendChannelEdit(env, msgID, "⚠️ 排队超时，请稍后再试。")
			} else {
				s.sendChannelReplyReport(env, "⚠️ 排队超时，请稍后再试。")
			}
			slog.Warn("channel-queue-timeout", "channelId", env.ChannelID, "chatId", env.ChatID)
			return
		}
		time.Sleep(500 * time.Millisecond)
		waited += 500 * time.Millisecond
	}

	if supportsEdit {
		_ = s.sendChannelEdit(env, msgID, "⏳ 正在处理中…")
	}
	runID, err := s.RunLoop(context.Background(), source, sessionID, env.Text, identity, "", "", nil)
	if err != nil {
		if supportsEdit {
			_ = s.sendChannelEdit(env, msgID, "⚠️ 执行失败："+channel.EscapeV2(err.Error()))
		} else {
			s.sendChannelReplyReport(env, "⚠️ 执行失败："+err.Error())
		}
		return
	}
	slog.Info("channel-queued-run", "channelId", env.ChannelID, "chatId", env.ChatID, "runId", runID, "waitedMs", waited.Milliseconds())
	s.streamChannelReply(env, runID, msgID)
}

// streamChannelReply 订阅 runId 事件，把 delta 文本持续 Edit 到占位消息上，实现真流式。
// Edit 节流：最小间隔 + 最小增量，避免 Telegram 对高频编辑触发 429 限流。
func (s *Service) streamChannelReply(env channel.InboundEnvelope, runID, msgID string) {
	// 不支持编辑的频道（如微信）：跳过占位编辑流式，改为累积全部 delta 后一次性发送。
	if ch, ok := s.chanMgr.registry.Get(env.ChannelID); ok {
		if se, ok := ch.(interface{ SupportsEdit() bool }); ok && !se.SupportsEdit() {
			var contents strings.Builder
			var errMsg string
			s.subscribeRunLive(runID, func(ev SSEEvent) {
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
			final := contents.String()
			if errMsg != "" {
				final = "⚠️ " + errMsg
			}
			if strings.TrimSpace(final) == "" {
				final = "✅ 已处理（无文本输出）。"
			}
			s.sendChannelReplyReport(env, final)
			slog.Info("channel-inbound-done", "channelId", env.ChannelID, "chatId", env.ChatID, "runId", runID, "replyLen", len(final), "stream", false)
			return
		}
	}

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

// subscribeRunLive 订阅 runId 的事件流直至终态，把每个事件实时回调给 onEvent。返回事件总数。
// 通过 drainRunEvents 同时兼容：通道仍可实时读（电视流式增量）与已被 SSE 领走/run 已结束
// （回退环形缓冲补收终态，不会遗漏 done/error）。返回事件总数。
func (s *Service) subscribeRunLive(runID string, onEvent func(SSEEvent)) int {
	count := 0
	s.drainRunEvents(context.Background(), runID, func(ev SSEEvent) {
		count++
		if onEvent != nil {
			onEvent(ev)
		}
	})
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

// channelBriefingReply 响应 /briefing 命令：先发送占位提示消息，再异步生成简报并发送，
// 避免生成期间（可达数十秒）用户无任何反馈误以为机器人宕机。
func (s *Service) channelBriefingReply(env channel.InboundEnvelope) string {
	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		return "⚠️ 频道未注册：" + env.ChannelID
	}
	// 占位消息同步发出，保证用户立即看到处理状态；生成与发送在后台执行。
	phID, err := ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: "⏳ 正在生成站点简报（约需 10-60 秒），请稍候…"})
	if err != nil {
		return "⚠️ 发送占位消息失败：" + err.Error()
	}
	// clearPlaceholder 在简报（或错误提示）发出后删除占位状态消息，避免聊天区残留状态。
	clearPlaceholder := func() {
		if phID != "" {
			_ = ch.Delete(context.Background(), env.ChatID, phID)
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		model := s.getBriefingModel(ctx)
		if model == "" {
			_, _ = ch.Send(ctx, env.ChatID, channel.OutboundMessage{Text: "⚠️ 未配置简报模型，请在「管理 AI 设置」中配置默认模型。"})
			clearPlaceholder()
			return
		}
		snapshot, err := s.gatherSiteSnapshot(ctx)
		if err != nil {
			_, _ = ch.Send(ctx, env.ChatID, channel.OutboundMessage{Text: "⚠️ 收集站点状态失败：" + err.Error()})
			clearPlaceholder()
			return
		}
		briefing, err := s.generateBriefing(ctx, model, s.briefingTemplatePrompt(ctx), snapshot)
		if err != nil {
			_, _ = ch.Send(ctx, env.ChatID, channel.OutboundMessage{Text: "⚠️ 生成简报失败：" + err.Error()})
			clearPlaceholder()
			return
		}
		if _, err := ch.Send(ctx, env.ChatID, channel.OutboundMessage{Text: briefing}); err != nil {
			slog.Warn("channel-briefing-send-failed", "channelId", env.ChannelID, "chatId", env.ChatID, "err", err.Error())
		}
		clearPlaceholder()
	}()
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
	ID                      string                 `json:"id"`
	Type                    string                 `json:"type"`
	Name                    string                 `json:"name"`
	Enabled                 bool                   `json:"enabled"`
	Config                  map[string]interface{} `json:"config,omitempty"` // 解密后的配置（旧 botToken 打码）
	NotificationChannelID   string                 `json:"notificationChannelId,omitempty"`
	NotificationChannelName string                 `json:"notificationChannelName,omitempty"`
	Status                  string                 `json:"status,omitempty"`
	CreatedAt               string                 `json:"createdAt"`
	UpdatedAt               string                 `json:"updatedAt"`
}

func maskBotToken(cfg map[string]interface{}) map[string]interface{} {
	if token, ok := cfg["botToken"].(string); ok && len(token) > 8 {
		cfg["botToken"] = token[:4] + "****" + token[len(token)-4:]
	}
	if secret, ok := cfg["secret"].(string); ok && secret != "" {
		cfg["secret"] = maskSecret(secret)
	}
	return cfg
}

// maskSecret 对任意非空密钥打码：过短（≤4 位）整段打码，其余保留首尾 2 位。
func maskSecret(secret string) string {
	r := []rune(secret)
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
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
		`SELECT id, type, name, enabled, config_encrypted, COALESCE(notification_channel_id,''), created_at, updated_at FROM admin_ai_channels ORDER BY created_at DESC`)
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
		if err := rows.Scan(&item.ID, &item.Type, &item.Name, &enabled, &encrypted, &item.NotificationChannelID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			continue
		}
		item.Enabled = enabled == 1
		var cfg map[string]interface{}
		if secure.DecryptJSON(encrypted, &cfg) == nil {
			item.Config = maskBotToken(cfg)
		}
		if item.NotificationChannelID != "" && s.src != nil {
			if ch, ok, err := s.src.LoadChannel(r.Context(), item.NotificationChannelID); err == nil && ok {
				item.NotificationChannelName = ch.Name
			}
		}
		if s.chanMgr != nil {
			s.chanMgr.mu.Lock()
			_, running := s.chanMgr.cancels[item.ID]
			s.chanMgr.mu.Unlock()
			if running {
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
		Type                  string                 `json:"type"`
		Name                  string                 `json:"name"`
		Enabled               bool                   `json:"enabled"`
		NotificationChannelID string                 `json:"notificationChannelId"`
		Config                map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Type == "" {
		req.Type = "telegram"
	}
	if req.Type != "telegram" && req.Type != "wechat" && req.Type != "wecom" {
		response.Error(w, http.StatusBadRequest, "仅支持 telegram / wechat / wecom 频道")
		return
	}
	if req.Name == "" {
		switch req.Type {
		case "wechat":
			req.Name = "微信"
		case "wecom":
			req.Name = "企业微信"
		default:
			req.Name = "Telegram"
		}
	}
	if req.Type == "telegram" {
		if err := s.validateNotificationSource(r.Context(), req.NotificationChannelID); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		inUse, err := s.sourceChannelInUse(r.Context(), req.NotificationChannelID, "")
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if inUse {
			response.Error(w, http.StatusBadRequest, "该通知渠道已被其他 AI 频道引用（同一 bot token 只能有一个长轮询实例）")
			return
		}
	}
	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}
	if req.Type == "telegram" {
		// telegram：新频道只存运行期偏好，bot token 一律从来源通知渠道解析，不再落库
		delete(req.Config, "botToken")
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
		`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, notification_channel_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Type, req.Name, enabled, encrypted, req.NotificationChannelID, now, now)
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
		Name                  *string                `json:"name"`
		Enabled               *bool                  `json:"enabled"`
		NotificationChannelID *string                `json:"notificationChannelId"`
		Config                map[string]interface{} `json:"config"`
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

	if req.NotificationChannelID != nil {
		sourceID := strings.TrimSpace(*req.NotificationChannelID)
		if sourceID != "" {
			if err := s.validateNotificationSource(r.Context(), sourceID); err != nil {
				response.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			inUse, err := s.sourceChannelInUse(r.Context(), sourceID, id)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			if inUse {
				response.Error(w, http.StatusBadRequest, "该通知渠道已被其他 AI 频道引用（同一 bot token 只能有一个长轮询实例）")
				return
			}
		} else {
			// 清空来源：仅当频道存有旧 Token 配置时允许（否则启动后无 token 可用）
			var oldEncrypted string
			if err := db.QueryRowContext(r.Context(),
				`SELECT config_encrypted FROM admin_ai_channels WHERE id = ?`, id).Scan(&oldEncrypted); err == nil {
				var oldCfg channel.TelegramConfig
				if secure.DecryptJSON(oldEncrypted, &oldCfg) == nil && strings.TrimSpace(oldCfg.BotToken) == "" {
					response.Error(w, http.StatusBadRequest, "频道没有旧 Token 配置，清空来源后无法启动，请选择来源通知渠道")
					return
				}
			}
		}
	}

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
	if req.NotificationChannelID != nil {
		updates = append(updates, "notification_channel_id = ?")
		args = append(args, strings.TrimSpace(*req.NotificationChannelID))
	}
	if req.Config != nil {
		var channelType string
		_ = db.QueryRowContext(r.Context(), `SELECT type FROM admin_ai_channels WHERE id = ?`, id).Scan(&channelType)
		if channelType == "wecom" {
			// 企微凭据存自有配置：合并旧配置，空字符串不覆盖（secret 留空 = 保持不变）
			var oldEncrypted string
			_ = db.QueryRowContext(r.Context(), `SELECT config_encrypted FROM admin_ai_channels WHERE id = ?`, id).Scan(&oldEncrypted)
			merged := map[string]interface{}{}
			if oldEncrypted != "" {
				var oldCfg map[string]interface{}
				if secure.DecryptJSON(oldEncrypted, &oldCfg) == nil && oldCfg != nil {
					for k, v := range oldCfg {
						merged[k] = v
					}
				}
			}
			for k, v := range req.Config {
				if sv, isStr := v.(string); isStr && sv == "" {
					continue
				}
				merged[k] = v
			}
			req.Config = merged
		} else if channelType != "wechat" {
			delete(req.Config, "botToken")
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
