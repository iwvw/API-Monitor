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
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

const (
	adminAIKeyBriefingModel = "admin_ai_briefing_model" // 站点简报专用模型（留空回退默认模型）
	briefingContextTimeout  = 600 * time.Second
)

// handleDailyBriefing GET /api/admin-ai/cron/daily-briefing
// 定时任务内部接口（本机 cron 经 X-Internal-Cron 调用）：收集站点实时状态，
// 用 AI 生成中文每日简报并推送到已绑定的 Telegram 接收者。
// 查询参数：model（可选，指定简报模型，留空回退设置项/默认模型）、chatId（可选，仅推送单个接收者）。
func (s *Service) handleDailyBriefing(w http.ResponseWriter, r *http.Request) {
	// 仅接受本机定时任务的内部调用（cronjobs internal 任务会带 X-Internal-Cron 头），
	// 避免已登录会话用户通过 GET 直连触发群发推送；外部请求一律拒绝。
	if r.Header.Get("X-Internal-Cron") != "true" {
		response.Error(w, http.StatusForbidden, "该接口仅允许本机定时任务调用")
		return
	}
	if s.aiCaller == nil {
		response.Error(w, http.StatusInternalServerError, "AI 调用器未配置")
		return
	}

	model := strings.TrimSpace(r.URL.Query().Get("model"))
	chatID := strings.TrimSpace(r.URL.Query().Get("chatId"))

	ctx, cancel := context.WithTimeout(r.Context(), briefingContextTimeout)
	defer cancel()

	if model == "" {
		model = s.getBriefingModel(ctx)
	}
	if model == "" {
		response.Error(w, http.StatusInternalServerError, "未配置简报模型：请在「管理 AI 设置」中配置默认模型或 admin_ai_briefing_model")
		return
	}

	snapshot, err := s.gatherSiteSnapshot(ctx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "收集站点状态失败: "+err.Error())
		return
	}

	briefing, err := s.generateBriefing(ctx, model, snapshot)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "生成简报失败: "+err.Error())
		return
	}

	targets, err := s.briefingTargets(ctx, chatID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(targets) == 0 {
		response.Error(w, http.StatusBadRequest, "没有可推送的 Telegram 接收者，请先在「管理 AI → 频道配置」绑定用户")
		return
	}
	// 按绑定频道解析注册实例（多频道：取第一个已启用的 telegram 频道）
	var ch channel.Channel
	var ok bool
	for _, cand := range s.chanMgr.registry.All() {
		if strings.HasPrefix(cand.ID(), "aac_") {
			ch, ok = cand, true
			break
		}
	}
	if !ok {
		response.Error(w, http.StatusInternalServerError, "Telegram 频道未注册，请先启动频道")
		return
	}

	// 简报全文直接以富消息 Markdown 发送（Telegram 富消息原生支持 Markdown 结构，
	// 无需再做特殊字符转义；换行归一化由频道发送层处理）。
	sent := briefing
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

	response.OK(w, map[string]interface{}{
		"ok":       true,
		"model":    model,
		"briefing": sent,
		"targets":  results,
	})
}

// getBriefingModel 解析简报模型：admin_ai_briefing_model → admin_ai_default_model → 环境默认。
func (s *Service) getBriefingModel(ctx context.Context) string {
	db, err := s.open(ctx)
	if err != nil {
		return s.cfg.AdminAIDefaultModel
	}
	defer db.Close()
	var model string
	if err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", adminAIKeyBriefingModel).Scan(&model); err == nil && strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model)
	}
	if err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_default_model'").Scan(&model); err == nil && strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model)
	}
	return strings.TrimSpace(s.cfg.AdminAIDefaultModel)
}

// gatherSiteSnapshot 通过内部调用收集站点实时状态（系统指标 + API 统计），返回紧凑 JSON 文本。
func (s *Service) gatherSiteSnapshot(ctx context.Context) (string, error) {
	snapshot := map[string]interface{}{}
	for _, path := range []string{"/api/system/host-metrics", "/api/system/api-stats"} {
		resp, err := s.aiCaller(ctx, systemmetrics.AICallRequest{Method: http.MethodGet, Path: path})
		if err != nil {
			snapshot[path] = map[string]interface{}{"error": err.Error()}
			continue
		}
		if resp.Body != nil {
			snapshot[path] = resp.Body
		} else {
			snapshot[path] = resp.Raw
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return truncateContent(string(raw)), nil
}

// generateBriefing 调用 AI 生成简洁的中文站点每日简报。
func (s *Service) generateBriefing(ctx context.Context, model, snapshot string) (string, error) {
	systemPrompt := "你是 API Monitor 站点的运维简报生成器。请根据提供的站点实时状态数据，生成一份简洁的中文《站点每日简报》，要求：" +
		"1. 使用 Telegram MarkdownV2 兼容的轻量 Markdown（粗体/项目符号/行内代码，避免复杂表格与嵌套）；" +
		"2. 包含标题与关键指标小节（系统资源 / API 调用 / 可用性）；" +
		"3. 突出异常与健康风险，正常项一笔带过；" +
		"4. 全文不超过 400 字，适合在 Telegram 中阅读。"
	resp, err := s.callLLMPlain(ctx, model, []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": "站点实时状态数据：\n" + snapshot},
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", fmt.Errorf("模型返回空简报")
	}
	return text, nil
}

// briefingTargets 解析推送目标：指定 chatId 时单发，否则推送给全部已绑定 Telegram 用户。
func (s *Service) briefingTargets(ctx context.Context, chatID string) ([]string, error) {
	if s.chanMgr == nil || s.chanMgr.registry == nil {
		return nil, fmt.Errorf("频道未初始化")
	}
	if chatID != "" {
		return []string{chatID}, nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT b.channel_user_id FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
		 WHERE c.type = 'telegram' AND c.enabled = 1 ORDER BY b.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		userID = strings.TrimSpace(userID)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, userID)
	}
	return out, rows.Err()
}
