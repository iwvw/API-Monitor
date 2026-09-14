package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode"
)

// historyMsg 是恢复历史时的会话消息内存形态，与 admin_ai_messages 行对应。
type historyMsg struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ReasoningSummary string     `json:"-"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCallRaw      string     `json:"-"`
}

// restoreSessionHistory 从库中恢复会话历史并做 tool_calls 配对重建：
// assistant 携带 tool_calls 时必须与同轮 tool 结果严格配对，否则上游 400
// "insufficient tool messages following tool_calls"。写入时用一个 assistant 行携带
// 全部 tool_calls（JSON 数组），逐条 tool 行落库时记录其 tool_call_id；重建时按 ID 配对。
// 兼容旧的逐条 assistant 落库格式（合并相邻行）。中断残留（有 tool_calls 无完整结果 /
// 无主的孤儿 tool 行）直接丢弃并从库中删除，保证每次发给上游的消息严格配对。
func (s *Service) restoreSessionHistory(ctx context.Context, db *sql.DB, sessionID string) ([]historyMsg, error) {
	historyRows, err := db.QueryContext(ctx,
		`SELECT id, role, COALESCE(content,''), COALESCE(reasoning_content,''), COALESCE(reasoning_summary,''), COALESCE(tool_call_meta,''), COALESCE(tool_call_id,'') FROM admin_ai_messages WHERE session_id = ? ORDER BY seq ASC`,
		sessionID)
	if err != nil {
		return nil, err
	}
	type histRow struct {
		id string
		historyMsg
	}
	rawRows := make([]histRow, 0, 64)
	for historyRows.Next() {
		var h histRow
		var toolID string
		if err := historyRows.Scan(&h.id, &h.Role, &h.Content, &h.ReasoningContent, &h.ReasoningSummary, &h.ToolCallRaw, &toolID); err == nil {
			if h.Role == "assistant" && h.ToolCallRaw != "" {
				// tool_call_meta 兼容 JSON 数组（写入格式）或单条（旧的逐条落库格式）
				if h.ToolCallRaw[0] == '[' {
					var tcs []toolCall
					if json.Unmarshal([]byte(h.ToolCallRaw), &tcs) == nil {
						h.ToolCalls = tcs
					}
				} else {
					var tc toolCall
					if json.Unmarshal([]byte(h.ToolCallRaw), &tc) == nil {
						h.ToolCalls = []toolCall{tc}
					}
				}
			}
			if h.Role == "tool" {
				h.ToolCallID = toolID
			}
			rawRows = append(rawRows, h)
		}
	}
	historyRows.Close()

	messages := make([]historyMsg, 0, len(rawRows))
	for i := 0; i < len(rawRows); {
		h := &rawRows[i]
		if h.Role == "assistant" && len(h.ToolCalls) > 0 {
			// 合并相邻的 assistant-tool_calls 行（兼容旧的逐条落库格式）成一轮
			asst := h.historyMsg
			j := i
			for j+1 < len(rawRows) && rawRows[j+1].Role == "assistant" && len(rawRows[j+1].ToolCalls) > 0 {
				j++
				asst.ToolCalls = append(asst.ToolCalls, rawRows[j].ToolCalls...)
			}
			// 收集紧随其后的 tool 结果行
			tools := make([]historyMsg, 0, len(asst.ToolCalls))
			k := j + 1
			for k < len(rawRows) && rawRows[k].Role == "tool" {
				tools = append(tools, rawRows[k].historyMsg)
				k++
			}
			// 按 ID 配对：缺失 ID 的按顺序回填到本轮 assistant 的 tool_calls
			idSet := map[string]bool{}
			for _, tc := range asst.ToolCalls {
				idSet[tc.ID] = true
			}
			ti := 0
			for l := range tools {
				if tools[l].ToolCallID == "" {
					for ti < len(asst.ToolCalls) && !idSet[asst.ToolCalls[ti].ID] {
						ti++
					}
					if ti < len(asst.ToolCalls) {
						tools[l].ToolCallID = asst.ToolCalls[ti].ID
						ti++
					}
				}
			}
			// 校验：assistant 的每个 tool_call 都必须有匹配的工具结果，否则视为中断残留整轮丢弃
			got := map[string]bool{}
			for _, t := range tools {
				if t.ToolCallID != "" {
					got[t.ToolCallID] = true
				}
			}
			valid := true
			for _, tc := range asst.ToolCalls {
				if !got[tc.ID] {
					valid = false
					break
				}
			}
			if !valid || len(tools) > len(asst.ToolCalls) {
				for m := i; m < k; m++ {
					_, _ = db.ExecContext(ctx, `DELETE FROM admin_ai_messages WHERE id = ?`, rawRows[m].id)
				}
				i = k
				continue
			}
			messages = append(messages, asst)
			for _, t := range tools {
				messages = append(messages, t)
			}
			i = k
			continue
		}
		if h.Role == "tool" {
			// 无前置 assistant tool_calls 的孤儿 tool 行，丢弃并清理
			_, _ = db.ExecContext(ctx, `DELETE FROM admin_ai_messages WHERE id = ?`, h.id)
			i++
			continue
		}
		messages = append(messages, h.historyMsg)
		i++
	}
	return messages, nil
}

// syncPendingPrompt 增量同步会话中新增的 user 消息：运行期间 submitMessage 把追问
// 直接入队（不再 409），本 run 每轮循环开头与最终落库前调用它归并队列——有新 user
// 行则重载历史（含新消息与已落库各轮次行）并返回最新 user 消息 id（即新的轮次归属）。
func (s *Service) syncPendingPrompt(ctx context.Context, db *sql.DB, sessionID, curUserMsgID string, messages *[]historyMsg) (string, []Mention, error) {
	var latest string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM admin_ai_messages WHERE session_id = ? AND role = 'user' ORDER BY seq DESC LIMIT 1`,
		sessionID).Scan(&latest); err != nil && err != sql.ErrNoRows {
		return curUserMsgID, nil, err
	}
	if latest == "" || latest == curUserMsgID {
		return curUserMsgID, nil, nil
	}
	reloaded, err := s.restoreSessionHistory(ctx, db, sessionID)
	if err != nil {
		return curUserMsgID, nil, err
	}
	*messages = reloaded
	// 追问（join）消息可能带新的 @ 引用：随最新 user 消息一起带回，
	// 由主循环重建引用快照块，避免旧 run 继续沿用上一轮的引用上下文。
	var mentionsJSON string
	var mentions []Mention
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(mentions,'') FROM admin_ai_messages WHERE id = ?`, latest).Scan(&mentionsJSON); err == nil && mentionsJSON != "" {
		_ = json.Unmarshal([]byte(mentionsJSON), &mentions)
	}
	return latest, normalizeMentions(mentions), nil
}

// generateSessionTitleAsync 异步生成会话标题：仅当会话尚无标题时才发起模型调用
// （避免每条新消息都触发一次标题生成），模型生成 ≤16 字中文标题并写库，
// 成功后下推 session_title 事件（前端实时更新会话列表）；失败回退消息截断。
// 独立连接写库避免与 runInference 主流程的单连接池互锁。
func (s *Service) generateSessionTitleAsync(ctx context.Context, sessionID, model, prompt, fallback string, eventCh chan SSEEvent) {
	db, err := s.open(ctx)
	if err != nil {
		slog.Warn("session-title-db", "err", err.Error())
		return
	}
	defer db.Close()
	// 标题已存在（含并发竞态下先写成功的）直接跳过：不再重复发起 LLM 调用
	var existing string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(title, '') FROM admin_ai_sessions WHERE id = ?`, sessionID).Scan(&existing); err != nil || existing != "" {
		return
	}
	title := s.generateSessionTitle(ctx, model, prompt)
	if strings.TrimSpace(title) == "" {
		title = fallback
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE admin_ai_sessions SET title = ? WHERE id = ? AND (title IS NULL OR title = '')`,
		title, sessionID); err != nil {
		slog.Warn("session-title-update", "err", err.Error())
		return
	}
	s.emit(eventCh, SSEEvent{Type: "session_title", Fields: map[string]interface{}{"sessionId": sessionID, "title": title}})
}

// generateSessionTitle 用同一模型生成 ≤16 字的会话标题；失败返回空串（调用方回退截断）。
// model 支持逗号分隔多候选，某候选失败自动回退下一个（与主对话回退语义一致）。
func (s *Service) generateSessionTitle(ctx context.Context, model, prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return ""
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": titleSystemPrompt},
		{"role": "user", "content": truncateContent(prompt)},
	}
	resp, err := s.callLLMPlainWithFallback(ctx, model, messages)
	if err != nil {
		slog.Warn("session-title-failed", "err", err.Error())
		return ""
	}
	text := strings.TrimSpace(resp.Content)
	text = strings.Trim(text, "\"'「」『』()（）:：")
	return trimTitle(text)
}

// 会话标题长度限制与智能截断：
// 不超过 maxTitleRunes 直接保留；超长时优先在收尾词（状态/结果/详情等）的完整
// 词尾截断，避免「…接口状」这类半截词标题。词尾最多放行 maxTitleRunes+2 字。
const maxTitleRunes = 16

var titleTrailingWords = []string{
	"状态", "情况", "总览", "概览", "配置", "数量", "结果", "详情", "列表",
	"记录", "汇总", "报告", "查询", "测试", "监控", "分析", "部署", "进度", "信息", "异常",
}

// trimTitle 对会话标题做长度治理：≤16 字原样返回；超长时若在截断点附近命中
// 收尾词则延展到完整词尾（最多 18 字），否则硬切到 16 字。
func trimTitle(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxTitleRunes {
		return string(runes)
	}
	// 从截断点开始向后扫描最多 2 个字符，命中收尾词则保留完整词尾
	for i := maxTitleRunes; i < len(runes) && i <= maxTitleRunes+2; i++ {
		for _, w := range titleTrailingWords {
			wr := []rune(w)
			if i+len(wr) <= len(runes) && string(runes[i:i+len(wr)]) == w {
				return string(runes[:i+len(wr)])
			}
		}
	}
	return string(runes[:maxTitleRunes])
}

// summaryModel 解析推理摘要专用模型：admin_ai_summary_model → session 模型 → 环境默认。
func (s *Service) summaryModel(ctx context.Context, db *sql.DB, fallback string) string {
	model := ""
	_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", adminAIKeySummaryModel).Scan(&model)
	if strings.TrimSpace(model) == "" {
		return fallback
	}
	return model
}

// splitModelList 把逗号分隔的模型配置拆成有序列表（去空格、去空项、去重）。
func splitModelList(spec string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, part := range strings.Split(spec, ",") {
		m := strings.TrimSpace(part)
		if m == "" || m == "default" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return []string{"default"}
	}
	return out
}

// scheduleReasoningSummary 异步生成思维链摘要：独立超时上下文 + 独立 DB 连接，
// 不阻塞 runInference 主循环的后续工具执行/下一轮 LLM 调用；摘要成功后
// emit reasoning_summary 事件并回填该轮 assistant 消息行的 reasoning_summary 列。
// model 支持逗号分隔多候选（如 "gemini-3.1-flash-lite,gpt-oss-120b"），失败自动回退。
// 空推理/过短推理直接跳过（与同步版行为一致）。
func (s *Service) scheduleReasoningSummary(model, reasoning, messageID string, eventCh chan SSEEvent) {
	if strings.TrimSpace(reasoning) == "" {
		return
	}
	if len([]rune(strings.TrimSpace(reasoning))) < 40 {
		return
	}
	go func() {
		sumCtx, sumCancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer sumCancel()
		text := s.summarizeReasoning(sumCtx, parseModelList(model), reasoning)
		if text == "" {
			return
		}
		// send-on-closed 已在 emit 内部 recover 兜底，run 结束后补发不会 panic
		s.emit(eventCh, SSEEvent{Type: "reasoning_summary", Fields: map[string]interface{}{"text": text}})
		db, err := s.open(sumCtx)
		if err != nil {
			return
		}
		defer db.Close()
		_, _ = db.ExecContext(sumCtx,
			`UPDATE admin_ai_messages SET reasoning_summary = ? WHERE id = ? AND (reasoning_summary IS NULL OR reasoning_summary = '')`,
			text, messageID)
	}()
}

// summarizeReasoning 按候选模型列表逐个尝试生成 ≤maxSummaryRunes 字思维链标题式摘要；
// 某模型调用失败或返回空内容时自动回退到下一个候选；全部失败返回空串（前端回退截断）。
// 输出强制清洗：删除全部标点符号与空白（cleanSummaryText），即使模型未遵守提示词。
func (s *Service) summarizeReasoning(ctx context.Context, models []string, reasoning string) string {
	if strings.TrimSpace(reasoning) == "" {
		return ""
	}
	// 思维链过短时不需要（也不值得）额外发起一次模型调用，直接跳过
	if len([]rune(strings.TrimSpace(reasoning))) < 40 {
		return ""
	}
	// 摘要使用独立短超时（父上下文仍受整轮预算约束），尽力而为，不显著占用整轮时间
	sumCtx, sumCancel := context.WithTimeout(ctx, 20*time.Second)
	defer sumCancel()
	for _, model := range models {
		messages := []map[string]interface{}{
			{"role": "system", "content": summarySystemPrompt},
			{"role": "user", "content": truncateContent(reasoning)},
		}
		resp, err := s.callLLMPlain(sumCtx, model, messages)
		if err != nil {
			slog.Warn("reasoning-summary-failed", "model", model, "err", err.Error())
			continue
		}
		text := strings.TrimSpace(resp.Content)
		if text == "" && len(resp.Choices) > 0 {
			text = strings.TrimSpace(resp.Choices[0].Message.Content)
		}
		text = cleanSummaryText(text)
		if text != "" {
			return text
		}
		slog.Warn("reasoning-summary-empty", "model", model)
	}
	return ""
}

// maxSummaryRunes 推理摘要长度上限：与提示词要求一致，超长时按边界截断。
const maxSummaryRunes = 16

// titleSystemPrompt 会话标题提示词：≤16 字、简体中文、禁用标点与空白，附对照示例。
const titleSystemPrompt = "为下面的用户消息生成一个不超过 16 个字的简体中文对话标题，" +
	"禁止使用任何标点符号、引号或空格，只输出标题本身，不要任何解释。" +
	"示例：用户消息「帮我查看所有主机状态并列出磁盘使用情况」应输出「查看主机与磁盘状态」。"

// summarySystemPrompt 推理摘要提示词：标题式 ≤16 字、简体中文、禁用全部标点与空白，
// 附带一个输入/输出对照示例，让模型理解「标题式」的具体形态。
const summarySystemPrompt = "把用户的思考内容压缩成一个不超过 16 个字的标题式摘要，必须使用简体中文，" +
	"禁止使用任何标点符号、引号、斜杠或空格，禁止添加解释性前缀。只输出摘要本身。" +
	"示例：思考「先查看主机列表，再检查磁盘剩余空间」应输出「查看主机磁盘空间」。"

// cleanSummaryText 强制清洗模型输出的摘要：删除全部标点符号（中文/英文/成对包裹符）、
// 全部空白（摘要是单行无间隙标题文字），最后按 maxSummaryRunes 做边界截断。
func cleanSummaryText(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 1)
	for _, r := range text {
		switch r {
		case '。', '！', '？', '；', '，', '、', '：', '·', '…', '—', '－',
			'「', '」', '『', '』', '（', '）', '(', ')', '【', '】', '《', '》',
			'“', '”', '‘', '’', '\u3000',
			'"', '\'', ',', '.', ';', ':', '!', '?', '/', '\\',
			' ', '\t', '\n', '\r':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return cutSummaryRunes(strings.TrimSpace(b.String()), maxSummaryRunes)
}

// summaryTrailingParticles 中文助词/连接词：硬切兜底时避开它们，避免残句结尾。
var summaryTrailingParticles = map[rune]bool{
	'的': true, '了': true, '着': true, '过': true, '和': true, '与': true,
	'及': true, '并': true, '且': true, '而': true, '在': true, '对': true,
	'把': true, '到': true, '为': true, '被': true, '于': true, '从': true,
	'向': true, '以': true, '会': true, '能': true, '要': true,
}

// cutSummaryRunes 把摘要截到 max 个 rune 内。优先停在数字量值短语边界（避免
// 「…占用52%内」这类把数值与后续短语劈开的残句），其次避开助词/连接词结尾
// （「…使用率和」回退成「…使用率」），无更优切点时退化为硬切。
func cutSummaryRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 2 {
		return string(runes[:max])
	}
	// 数字/百分号与其他字符的交界视为量值短语结束点，从右往左找最后一个
	isNum := func(r rune) bool { return (r >= '0' && r <= '9') || r == '%' }
	for i := max; i > max/2; i-- {
		if isNum(runes[i-1]) != isNum(runes[i]) {
			return string(runes[:i])
		}
	}
	// 无量值边界：切点若落在助词/连接词上（含「助词+汉字」组合，如「…使用率和内」
	// 切点虽为『内』，但紧邻的是连接词『和』），向前回退保留完整短语，避免残句结尾
	cut := max
	for cut > max/2 {
		last := runes[cut-1]
		if summaryTrailingParticles[last] {
			cut--
			continue
		}
		if cut >= 2 && summaryTrailingParticles[runes[cut-2]] && unicode.Is(unicode.Han, last) {
			cut--
			continue
		}
		break
	}
	return string(runes[:cut])
}

// parseModelList 解析逗号分隔的模型候选列表（去空白与空项）。
func parseModelList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
