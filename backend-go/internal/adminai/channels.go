package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
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
		tg := channel.NewTelegramChannel("telegram", cfg, s.chanMgr.registry)
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
		tg := channel.NewTelegramChannel("telegram", cfg, s.chanMgr.registry)
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
		// Start 阻塞；退出后清理 cancel 注册
		_ = ch.Start(runCtx)
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
	if ch, exists := s.chanMgr.registry.Get("telegram"); exists {
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

// handleChannelInbound 处理频道入站消息：绑定校验 → RunLoop → 事件收集 → 出站回复。
func (s *Service) handleChannelInbound(env channel.InboundEnvelope) {
	// 会话键：channels 用 chatID 派生（同一对话/群组共享会话）
	sessionID := "cha_" + env.ChatID
	source := "channel:" + env.ChannelID

	identity, _ := json.Marshal(map[string]interface{}{
		"source":    source,
		"channelId": env.ChannelID,
		"userId":    env.UserID,
		"username":  env.Username,
		"chatId":    env.ChatID,
	})

	runID, err := s.RunLoop(context.Background(), source, sessionID, env.Text, string(identity), "")
	if err != nil {
		_, _ = s.sendChannelReply(env, "⚠️ 执行失败："+err.Error())
		return
	}

	// 消费事件直到结束，收集回答文本
	texts := s.subscribeRun(runID)
	reply := strings.Join(texts, "\n")
	if strings.TrimSpace(reply) == "" {
		reply = "✅ 已处理（无文本输出）。"
	}
	_, _ = s.sendChannelReply(env, reply)
}

// subscribeRun 订阅 runId 的事件通道直至关闭，返回累计的 delta 文本。
func (s *Service) subscribeRun(runID string) []string {
	s.mu.Lock()
	ch, exists := s.runs[runID]
	if exists {
		delete(s.runs, runID)
	}
	s.mu.Unlock()
	if !exists {
		return nil
	}

	texts := make([]string, 0)
	for event := range ch {
		if event.Type == "delta" {
			if text, ok := event.Fields["text"].(string); ok && text != "" {
				texts = append(texts, text)
			}
		}
	}
	return texts
}

// sendChannelReply 向频道发送 HTML 回复（简单转义）。
func (s *Service) sendChannelReply(env channel.InboundEnvelope, text string) (string, error) {
	ch, ok := s.chanMgr.registry.Get(env.ChannelID)
	if !ok {
		return "", fmt.Errorf("频道 %s 未注册", env.ChannelID)
	}
	escaped := html.EscapeString(text)
	escaped = strings.ReplaceAll(escaped, "\n", "\n")
	return ch.Send(context.Background(), env.ChatID, channel.OutboundMessage{Text: escaped})
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
	response.OK(w, map[string]interface{}{"id": id, "ok": true})
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
			if ch, ok := s.chanMgr.registry.Get("telegram"); ok {
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
