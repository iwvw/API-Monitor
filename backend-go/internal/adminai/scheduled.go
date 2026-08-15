package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// cronTaskRunReq 定时 AI 任务执行请求（内部接口，仅本机 cron 携带 X-Internal-Cron 调用）。
// policy=allow（默认）时写操作免审批直接执行（仍受「写操作全局开关」约束）；readonly 时禁用写操作。
type cronTaskRunReq struct {
	Prompt    string `json:"prompt"`    // AI 提示词（必填）
	Model     string `json:"model"`     // 指定模型，留空回退默认模型
	Policy    string `json:"policy"`    // allow（默认）| readonly
	ChannelID string `json:"channelId"` // 可选：完成后把输出推送到该 Telegram 频道的绑定接收者
	Title     string `json:"title"`     // 会话标题，留空取 prompt 摘要
}

// handleCronTaskRun POST /api/admin-ai/cron/task-run
// 定时任务内部接口（本机 cron 经 X-Internal-Cron 调用）：以独立会话无头执行一条 AI 提示词，
// 支持工具调用（默认完全允许写操作），执行结果写入会话/审计并返回最终回复文本；
// 可选把结果推送到绑定频道。外部请求一律拒绝。
func (s *Service) handleCronTaskRun(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Internal-Cron") != "true" {
		response.Error(w, http.StatusForbidden, "该接口仅允许本机定时任务调用")
		return
	}
	var req cronTaskRunReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		response.Error(w, http.StatusBadRequest, "prompt 不能为空")
		return
	}
	policy := strings.ToLower(strings.TrimSpace(req.Policy))
	if policy == "" {
		policy = "allow"
	}
	if policy != "allow" && policy != "readonly" {
		response.Error(w, http.StatusBadRequest, "policy 仅支持 allow / readonly")
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	// 管理 AI 总开关（admin_ai_enabled=false 时拒绝执行）
	var enabled string
	_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_enabled'").Scan(&enabled)
	if strings.TrimSpace(enabled) == "false" {
		response.Error(w, http.StatusForbidden, "管理 AI 总开关已关闭")
		return
	}

	// 独立会话（source=cron），标题取 prompt 摘要
	sessionID, err := randomID("aas_")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		var cfgModel string
		_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_default_model'").Scan(&cfgModel)
		model = strings.TrimSpace(cfgModel)
	}
	if model == "" {
		model = s.cfg.AdminAIDefaultModel
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = truncateContent(req.Prompt)
		if len(title) > 40 {
			title = title[:40] + "…"
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_ai_sessions (id, source, title, model, write_enabled, created_at, updated_at, last_activity_at) VALUES (?, 'cron', ?, ?, 0, ?, ?, ?)`,
		sessionID, title, model, now, now, now); err != nil {
		response.Error(w, http.StatusInternalServerError, "创建会话失败: "+err.Error())
		return
	}

	runID, err := s.RunLoop(ctx, "cron", sessionID, req.Prompt, "", model)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "启动执行失败: "+err.Error())
		return
	}

	// 策略 allow：本会话内写操作免审批（仍受全局写开关约束）
	if policy == "allow" {
		s.mu.Lock()
		s.runAutoApprove[runID] = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.runAutoApprove, runID)
			s.mu.Unlock()
		}()
	}

	// 同步等待本轮执行收尾（done / error / 请求超时取消）
	s.mu.Lock()
	eventCh := s.runs[runID]
	s.mu.Unlock()
	if eventCh == nil {
		response.Error(w, http.StatusInternalServerError, "执行通道不存在")
		return
	}
	var runErr string
	finished := false
	for !finished {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				finished = true
				break
			}
			switch ev.Type {
			case "done", "error":
				if ev.Type == "error" {
					if msg, ok := ev.Fields["message"].(string); ok {
						runErr = msg
					}
				}
				finished = true
			}
		case <-ctx.Done():
			runErr = "定时 AI 任务执行超时或已取消"
			finished = true
		}
	}
	if runErr != "" {
		response.Error(w, http.StatusInternalServerError, runErr)
		return
	}

	// 最终回复：会话内最后一条非空 assistant 正文；工具调用数一并统计
	var output string
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(content,'') FROM admin_ai_messages WHERE session_id = ? AND role = 'assistant' AND content <> '' ORDER BY created_at DESC, id DESC LIMIT 1`,
		sessionID).Scan(&output)
	var toolCalls int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = ? AND role = 'tool'`, sessionID).Scan(&toolCalls)
	if output == "" {
		output = "(AI 未返回正文)"
	}

	// 可选：推送到绑定频道（与简报共用绑定表语义，多目标逐一发送）
	pushResults := []map[string]interface{}{}
	if strings.TrimSpace(req.ChannelID) != "" {
		pushResults, err = s.pushCronTaskOutput(ctx, req.ChannelID, title, req.Prompt, output, toolCalls)
		if err != nil {
			pushResults = []map[string]interface{}{{"error": err.Error()}}
		}
	}

	response.OK(w, map[string]interface{}{
		"ok":        true,
		"sessionId": sessionID,
		"runId":     runID,
		"model":     model,
		"policy":    policy,
		"title":     title,
		"output":    output,
		"toolCalls": toolCalls,
		"push":      pushResults,
	})
}

// pushCronTaskOutput 把定时 AI 任务结果推送到指定 Telegram 频道的绑定接收者。
func (s *Service) pushCronTaskOutput(ctx context.Context, channelID, title, prompt, output string, toolCalls int) ([]map[string]interface{}, error) {
	if s.chanMgr == nil || s.chanMgr.registry == nil {
		return nil, fmt.Errorf("频道未初始化")
	}
	var ch channel.Channel
	for _, cand := range s.chanMgr.registry.All() {
		if cand.ID() == channelID {
			ch = cand
			break
		}
	}
	if ch == nil {
		// 兼容历史频道 ID 变化：未精确命中时回退第一个已注册 telegram 频道
		for _, cand := range s.chanMgr.registry.All() {
			if strings.HasPrefix(cand.ID(), "aac_") {
				ch = cand
				break
			}
		}
	}
	if ch == nil {
		return nil, fmt.Errorf("频道 %s 未注册，请先启动频道", channelID)
	}

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT b.channel_user_id FROM admin_ai_channel_bindings b WHERE b.channel_id = ? ORDER BY b.created_at DESC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	targets := make([]string, 0, 8)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		targets = append(targets, userID)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("频道 %s 没有绑定接收者", channelID)
	}

	sent := fmt.Sprintf("定时 AI 任务「%s」\n已调用 %d 次工具，输出：\n\n%s", title, toolCalls, output)
	results := make([]map[string]interface{}, 0, len(targets))
	for _, chat := range targets {
		item := map[string]interface{}{"chatId": chat}
		msgID, sendErr := ch.Send(ctx, chat, channel.OutboundMessage{Text: sent})
		if sendErr != nil {
			item["error"] = sendErr.Error()
		} else {
			item["messageId"] = msgID
		}
		results = append(results, item)
	}
	return results, nil
}