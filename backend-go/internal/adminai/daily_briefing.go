package adminai

import (
	"context"
	"database/sql"
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
	adminAIKeyBriefingModel    = "admin_ai_briefing_model"   // 站点简报专用模型（留空回退默认模型）
	adminAIKeyBriefingTemplate = "admin_ai_briefing_template" // 站点简报模板配置（JSON：{"type","custom"}）
	briefingContextTimeout     = 600 * time.Second
)

// briefingTemplatePrompts 是简报模板库：type → 格式要求段落（追加到身份句之后）。
// 前端设置页与这里保持一致的模板清单。
var briefingTemplatePrompts = map[string]string{
	"standard": "格式要求：包含标题与关键指标小节（系统资源 / API 调用 / 可用性），突出异常与健康风险，正常项一笔带过；全文不超过 400 字，适合在 Telegram 中阅读。",
	"brief": "格式要求：极简风格——开头一句话结论，随后用项目符号列出关键指标与异常项，正常指标不逐项列出；全文不超过 150 字。",
	"detailed": "格式要求：完整报告风格——标题、摘要、分节（系统资源 / API 调用 / 可用性 / 风险与建议），每个指标给出具体数值，结尾给出优化建议；全文不超过 800 字。",
	"alert_only": "格式要求：仅报告异常——只有发现异常或风险时才输出内容，按严重度排序列出（每项包含影响与建议）；一切正常时仅输出一句“一切正常”。",
}

// defaultBriefingTemplateJSON 是简报模板配置的默认值（未配置时使用标准模板）。
const defaultBriefingTemplateJSON = `{"type":"standard","custom":""}`

// briefingTemplatePrompt 读取设置中的简报模板配置，返回拼入系统提示词的格式要求段落。
func (s *Service) briefingTemplatePrompt(ctx context.Context) string {
	db, err := s.open(ctx)
	if err == nil {
		var raw string
		if err := db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = ?`, adminAIKeyBriefingTemplate).Scan(&raw); err == nil && strings.TrimSpace(raw) != "" {
			db.Close()
			return resolveBriefingTemplatePrompt(raw)
		}
		db.Close()
	}
	return briefingTemplatePrompts["standard"]
}

// resolveBriefingTemplatePrompt 把模板配置 JSON 解析为格式要求段落（纯函数，便于测试）。
// 未知类型、非法 JSON 一律回退标准模板；custom 类型使用用户自定义文本。
func resolveBriefingTemplatePrompt(raw string) string {
	var cfg struct {
		Type   string `json:"type"`
		Custom string `json:"custom"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return briefingTemplatePrompts["standard"]
	}
	switch cfg.Type {
	case "custom":
		custom := strings.TrimSpace(cfg.Custom)
		if custom == "" {
			return briefingTemplatePrompts["standard"]
		}
		return "格式要求（用户自定义）：\n" + custom
	case "standard", "brief", "detailed", "alert_only":
		if prompt, ok := briefingTemplatePrompts[cfg.Type]; ok {
			return prompt
		}
	}
	return briefingTemplatePrompts["standard"]
}

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
	// 纵深防御：强制来源为本机回环地址，防同源登录会话伪造头触发群发推送。
	if !isLoopbackRemoteAddr(r.RemoteAddr) {
		response.Error(w, http.StatusForbidden, "该接口仅允许本机定时任务调用（来源需为回环地址）")
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

	briefing, err := s.generateBriefing(ctx, model, s.briefingTemplatePrompt(ctx), snapshot)
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
		response.Error(w, http.StatusBadRequest, "没有可推送的简报目标：请在「管理 AI → 频道配置」选择带来源通知渠道的 AI 频道，或在通知中心配置 Telegram 渠道")
		return
	}

	// 简报全文直接以富消息 Markdown 发送（Telegram 富消息原生支持 Markdown 结构，
	// 无需再做特殊字符转义；换行归一化由发送层处理）。
	results := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		if target.NotificationChannelID != "" {
			item := map[string]interface{}{"channelId": target.NotificationChannelID}
			if s.src == nil {
				item["error"] = "通知中心服务未注入"
			} else if sendErr := s.src.SendRichToChannel(ctx, target.NotificationChannelID, "每日站点简报", briefing); sendErr != nil {
				item["error"] = sendErr.Error()
			} else {
				item["ok"] = true
			}
			results = append(results, item)
			continue
		}
		// 旧频道无来源：白名单用户经机器人实例发送
		ch, ok := briefingLegacyInstance(s.chanMgr.registry)
		if !ok {
			results = append(results, map[string]interface{}{"chatId": target.ChatID, "error": "Telegram 频道未注册，请先启动频道"})
			continue
		}
		item := map[string]interface{}{"chatId": target.ChatID}
		msgID, sendErr := ch.Send(ctx, target.ChatID, channel.OutboundMessage{Text: briefing})
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
		"briefing": briefing,
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
// templatePrompt 为设置中选定的简报模板格式要求（见 briefingTemplatePrompt）。
func (s *Service) generateBriefing(ctx context.Context, model, templatePrompt, snapshot string) (string, error) {
	systemPrompt := "你是 API Monitor 站点的运维简报生成器。请根据提供的站点实时状态数据，生成一份简洁的中文《站点简报》。" +
		"使用 Telegram MarkdownV2 兼容的轻量 Markdown（粗体/项目符号/行内代码，避免复杂表格与嵌套）。\n" + templatePrompt
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

// briefingTarget 是简报的一个推送目标：经通知中心渠道直发（新语义），或经旧机器人实例发到白名单 chat。
type briefingTarget struct {
	NotificationChannelID string // 非空：调用通知中心 SendToChannel 直发该渠道的固定目标
	ChatID                string // 非空：旧频道白名单目标（经注册实例发送）
}

// briefingLegacyInstance 返回注册表中第一个 adminai 电报频道实例（旧无来源频道的发送通道）。
func briefingLegacyInstance(registry *channel.Registry) (channel.Channel, bool) {
	if registry == nil {
		return nil, false
	}
	for _, cand := range registry.All() {
		if strings.HasPrefix(cand.ID(), "aac_") {
			return cand, true
		}
	}
	return nil, false
}

// bindingUserIDs 返回某频道的白名单用户（channel_user_id，去重）。
func bindingUserIDs(ctx context.Context, db *sql.DB, channelID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT channel_user_id FROM admin_ai_channel_bindings WHERE channel_id = ? ORDER BY created_at DESC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			continue
		}
		uid = strings.TrimSpace(uid)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, uid)
	}
	return out, rows.Err()
}

// briefingTargets 解析简报推送目标：
// - chatId 指定时：若为通知中心渠道则经其直发，否则视为旧单发 chat；
// - 默认：每个启用的 AI 频道，有来源通知渠道 → 该渠道目标（同源去重）；旧频道无来源 → 其白名单用户。
func (s *Service) briefingTargets(ctx context.Context, chatID string) ([]briefingTarget, error) {
	if s.chanMgr == nil || s.chanMgr.registry == nil {
		return nil, fmt.Errorf("频道未初始化")
	}
	if chatID != "" {
		if s.src != nil {
			if ch, ok, err := s.src.LoadChannel(ctx, chatID); err == nil && ok && ch.Type == "telegram" {
				return []briefingTarget{{NotificationChannelID: chatID}}, nil
			}
		}
		return []briefingTarget{{ChatID: chatID}}, nil
	}

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(notification_channel_id,'') FROM admin_ai_channels WHERE type = 'telegram' AND enabled = 1 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	// 先完整收集再关闭 rows，避免在 sqlite 单连接池上嵌套查询死锁
	type channelRow struct{ id, sourceID string }
	var channels []channelRow
	for rows.Next() {
		var row channelRow
		if err := rows.Scan(&row.id, &row.sourceID); err != nil {
			continue
		}
		row.sourceID = strings.TrimSpace(row.sourceID)
		channels = append(channels, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seenNotif := map[string]bool{}
	targets := make([]briefingTarget, 0, 8)
	for _, row := range channels {
		if row.sourceID != "" {
			if s.src == nil || seenNotif[row.sourceID] {
				continue
			}
			ch, ok, err := s.src.LoadChannel(ctx, row.sourceID)
			if err == nil && ok && ch.Type == "telegram" && ch.Enabled == 1 {
				seenNotif[row.sourceID] = true
				targets = append(targets, briefingTarget{NotificationChannelID: row.sourceID})
			}
			continue
		}
		users, err := bindingUserIDs(ctx, db, row.id)
		if err != nil {
			continue
		}
		for _, uid := range users {
			targets = append(targets, briefingTarget{ChatID: uid})
		}
	}
	return targets, nil
}
