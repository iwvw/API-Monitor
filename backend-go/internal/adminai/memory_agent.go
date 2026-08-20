package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	adminAIKeyMemoriesAutoCapture = "admin_ai_memories_auto_capture"
	adminAIKeyMemoriesIdleMinutes = "admin_ai_memories_idle_minutes"
)

const memoryCaptureInterval = 60 * time.Second

// memoryCaptureCooldown 是单次提炼失败后的最短重试间隔，指数退避（首失败后最短，
// 连续失败越多间隔越长），避免模型不可用时每 60s 无限重试同一会话。
const memoryCaptureCooldownBase = 5 * time.Minute

// memoryCaptureFailLimit 连续失败超过该次数后，冷却窗口封顶（不再继续拉长）。
const memoryCaptureFailLimit = 5

// memoryCaptureLimits 提炼输入预算（防超长会话撑爆 prompt）。
const (
	memoryCaptureMaxMessages     = 30
	memoryCaptureMaxMsgChars     = 800
	memoryCaptureMaxConvChars    = 8000
	memoryCaptureContextMemories = 20
	memoryCaptureTimeout         = 5 * time.Minute
)

// memoryCaptureOperation 是提炼结果中的单条操作（add=新增 / update=改写现有记忆）。
type memoryCaptureOperation struct {
	Action     string `json:"action"`
	ID         string `json:"id,omitempty"`
	Content    string `json:"content"`
	Importance *int   `json:"importance,omitempty"`
	Triggers   string `json:"triggers,omitempty"`
}

// startMemoryCapture 启动自动记忆提炼后台循环（由 StartBackground 调用，幂等）。
func (s *Service) startMemoryCapture() {
	s.captureOnce.Do(func() {
		s.stopCapture = make(chan struct{})
		go s.memoryCaptureLoop()
	})
}

// memoryCaptureLoop 周期扫描空闲会话并提炼（OpenClaw dreaming 的轻量版：
// 无独立 cron，直接寄生在 service 后台循环；按会话空闲 + 增量消息触发，每会话一次）。
func (s *Service) memoryCaptureLoop() {
	ticker := time.NewTicker(memoryCaptureInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCapture:
			return
		case <-ticker.C:
			_ = s.runMemoryCaptureOnce()
		}
	}
}

// runMemoryCaptureOnce 扫描满足条件的 web 会话并逐个提炼（串行，单连接池安全）。
func (s *Service) runMemoryCaptureOnce() error {
	if !s.getBoolSetting(context.Background(), adminAIKeyMemoriesEnabled, true) ||
		!s.getBoolSetting(context.Background(), adminAIKeyMemoriesAutoCapture, true) {
		return nil
	}
	idleMinutes := s.getIntSetting(context.Background(), adminAIKeyMemoriesIdleMinutes, 10)
	if idleMinutes <= 0 {
		idleMinutes = 10
	}
	return s.runMemoryCaptureWithIdle(idleMinutes)
}

func (s *Service) runMemoryCaptureWithIdle(idleMinutes int) error {
	ctx, cancel := context.WithTimeout(context.Background(), memoryCaptureTimeout)
	defer cancel()

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	idleBefore := time.Now().UTC().Add(-time.Duration(idleMinutes) * time.Minute).Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(model,''), COALESCE(memory_extracted_at,''), COALESCE(memory_extracted_msg_id,''),
		       COALESCE(memory_capture_failed_at,''), COALESCE(memory_capture_fail_count,0)
		FROM admin_ai_sessions
		WHERE source = 'web'
		  AND last_activity_at <= ?
		  AND (memory_extracted_at = '' OR memory_extracted_at <
		       (SELECT MAX(created_at) FROM admin_ai_messages m
		        WHERE m.session_id = admin_ai_sessions.id AND m.role = 'user'))
		ORDER BY last_activity_at ASC`,
		idleBefore)
	if err != nil {
		return err
	}
	type candidate struct {
		id         string
		model      string
		extracted  string
		msgID      string
		failedAt   string
		failCount  int
		cooldownOk bool
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.model, &c.extracted, &c.msgID, &c.failedAt, &c.failCount) == nil {
			c.cooldownOk = memoryCaptureCooldownElapsed(c.failedAt, c.failCount)
			candidates = append(candidates, c)
		}
	}
	rows.Close()
	rows = nil

	var firstErr error
	for _, c := range candidates {
		// 失败冷却窗口内跳过：连续失败时指数拉长窗口，杜绝 60s 硬循环；
		// 冷却状态持久化在 DB，重启进程后依旧生效。
		if !c.cooldownOk {
			continue
		}
		s.mu.Lock()
		_, running := s.sessionRuns[c.id]
		s.mu.Unlock()
		if running {
			continue
		}
		if err := s.captureSessionMemory(ctx, db, c.id, c.model, c.extracted, c.msgID); err != nil {
			slog.Warn("memory-capture", "session", c.id, "err", err.Error())
			s.markMemoryCaptureFailed(ctx, db, c.id)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// memoryCaptureCooldownElapsed 返回该会话的失败冷却是否已到期（true=可以重试；false=冷却中跳过）。
// 无失败记录的直接可重试；有失败记录时按连续失败次数指数退避：base × 2^(count-1)，上限随
// memoryCaptureFailLimit 封顶。时间解析失败按可重试处理（防御性放行，避免陈旧坏数据永久卡死提炼）。
func memoryCaptureCooldownElapsed(failedAt string, failCount int) bool {
	if failedAt == "" {
		return true
	}
	parseAt, err := time.Parse(time.RFC3339, failedAt)
	if err != nil {
		return true
	}
	count := failCount
	if count < 1 {
		count = 1
	}
	if count > memoryCaptureFailLimit {
		count = memoryCaptureFailLimit
	}
	cooldown := memoryCaptureCooldownBase << uint(count-1)
	return time.Since(parseAt) >= cooldown
}

// markMemoryCaptureFailed 记录一次提炼失败：写入失败时间并递增连续失败计数。
// 与游标推进（成功清零）共用存储层，失败计数在 DB 持久化，重启后冷却窗口依然有效。
func (s *Service) markMemoryCaptureFailed(ctx context.Context, db *sql.DB, sessionID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`UPDATE admin_ai_sessions SET memory_capture_failed_at = ?, memory_capture_fail_count = memory_capture_fail_count + 1 WHERE id = ?`,
		now, sessionID); err != nil {
		slog.Warn("memory-capture-failed-mark", "session", sessionID, "err", err.Error())
	}
}

// captureSessionMemory 提炼单个会话的新增对话：加载消息与现有记忆 → 调用 LLM 输出操作清单 → 落库。
// 游标是 (memory_extracted_at, memory_extracted_msg_id) 二元组，仅在成功完成后推进到
// 本批最后一条消息的位置：中途进程崩溃/失败保留旧游标，消息不丢、下轮重试；
// 批内消息超过上限时游标只推进到本批末尾，剩余消息下一轮继续，不会跳过。
// 进程内 captureInFlight 防同一会话并发重复提炼。
func (s *Service) captureSessionMemory(ctx context.Context, db *sql.DB, sessionID, model, oldExtracted, oldMsgID string) error {
	s.mu.Lock()
	if s.captureInFlight[sessionID] {
		s.mu.Unlock()
		return nil
	}
	s.captureInFlight[sessionID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.captureInFlight, sessionID)
		s.mu.Unlock()
	}()

	// 本会话待提炼消息（用户 + 助手正文，排除 tool 行）。
	// 游标为 (created_at, id) 字典序：同一秒内的多条消息也不会漏或重复。
	rows, err := db.QueryContext(ctx, `
		SELECT id, role, created_at, content FROM admin_ai_messages
		WHERE session_id = ? AND role IN ('user', 'assistant') AND content <> ''
		  AND (created_at > ? OR (created_at = ? AND id > ?))
		ORDER BY created_at ASC, id ASC LIMIT ?`,
		sessionID, oldExtracted, oldExtracted, oldMsgID, memoryCaptureMaxMessages)
	if err != nil {
		return err
	}
	type msgPair struct{ id, role, content, createdAt string }
	var msgs []msgPair
	for rows.Next() {
		var m msgPair
		if rows.Scan(&m.id, &m.role, &m.createdAt, &m.content) == nil {
			msgs = append(msgs, m)
		}
	}
	rows.Close()
	if len(msgs) == 0 {
		return nil
	}

	var conv strings.Builder
	for _, m := range msgs {
		runes := []rune(m.content)
		if len(runes) > memoryCaptureMaxMsgChars {
			runes = runes[:memoryCaptureMaxMsgChars]
		}
		conv.WriteString(m.role)
		conv.WriteString(": ")
		conv.WriteString(string(runes))
		conv.WriteString("\n")
		if conv.Len() >= memoryCaptureMaxConvChars {
			break
		}
	}

	// 专用提炼模型最高优先：admin_ai_memories_model → 会话模型 → 默认推理模型。
	// 专用模型一旦在设置中配置立即生效（每轮循环重新读取，无需重启），下一轮提炼即采用。
	if configured := s.getSetting(ctx, adminAIKeyMemoriesModel, ""); configured != "" {
		model = configured
	}
	if model == "" {
		_ = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_default_model'").Scan(&model)
	}
	if model == "" {
		model = s.cfg.AdminAIDefaultModel
	}
	if model == "" {
		return fmt.Errorf("未配置模型，跳过提炼")
	}

	// 现有记忆摘要（去重/矛盾判断用，携带 id 供 update）
	existing, err := s.listMemoryContext(ctx, db)
	if err == nil && existing != "" {
		existing = "现有长期记忆：\n" + existing
	}

	systemPrompt := "你是 API Monitor 的长期记忆提炼器。给定一段对话与现有长期记忆，提取值得跨会话保留的事实、用户偏好与环境约束。\n" +
		"规则：\n" +
		"1. 只记住长期有价值的信息（用户偏好、固定环境事实、重要决策）；一次性任务细节与瞬时状态（当前用量、临时公告、一次性告警数值）一律不记；可通过系统接口实时查询的动态资源状态（实例规格、IP、端口、DNS 记录、任务配置、启停状态等）一律不记，只记资源标识（名称/ID）与用户偏好等稳定信息。\n" +
		"2. 与现有记忆内容重复的不再记录。\n" +
		"3. 与现有记忆矛盾时，用 action=update 并携带现有记忆 id 与最新内容。\n" +
		"4. 每条记忆独立成条、一句话表述、含具体名称/ID/取值。\n" +
		"5. 完全没有值得记录的，输出 {\"operations\":[]}。\n" +
		"只输出一个 JSON 对象（不要 markdown 代码块或任何解释）：" +
		`{"operations":[{"action":"add|update","id":"update 时必填","content":"记忆内容","importance":1-10的整数,"triggers":"逗号分隔触发词（选填）"}]}`

	userContent := existing + "\n\n对话内容：\n" + conv.String()
	if userContent == "\n\n对话内容：\n"+conv.String() {
		userContent = "对话内容：\n" + conv.String()
	}

	resp, err := s.callLLMPlain(ctx, model, []map[string]interface{}{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userContent},
	})
	if err != nil {
		return fmt.Errorf("提炼调用失败: %w", err)
	}

	ops, err := parseMemoryCaptureResponse(resp.Content)
	if err != nil {
		return fmt.Errorf("提炼输出解析失败: %w", err)
	}
	for _, op := range ops {
		if err := s.applyMemoryCaptureOp(ctx, db, sessionID, op); err != nil {
			return err
		}
	}
	if len(ops) > 0 {
		slog.Info("memory-capture", "session", sessionID, "operations", len(ops))
	}
	// 全部操作落库成功后才推进游标（崩溃/失败保留旧游标，消息不丢）。
	// 游标推进到本批最后一条消息的 (created_at, id)：超批的剩余消息
	// 满足「>」条件，下一轮继续处理，不会被跳过。
	// 成功同时清零失败冷却计数：提炼恢复后不再受退避窗口阻碍。
	last := msgs[len(msgs)-1]
	if _, err := db.ExecContext(ctx, `UPDATE admin_ai_sessions SET memory_extracted_at = ?, memory_extracted_msg_id = ?, memory_capture_failed_at = '', memory_capture_fail_count = 0 WHERE id = ?`, last.createdAt, last.id, sessionID); err != nil {
		return err
	}
	return nil
}

// listMemoryContext 返回现有记忆的 id+content 摘要行（供提炼去重/矛盾判断）。
func (s *Service) listMemoryContext(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, content FROM admin_ai_memories ORDER BY pinned DESC, importance DESC, updated_at DESC LIMIT ?`,
		memoryCaptureContextMemories)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var id, content string
		if rows.Scan(&id, &content) != nil {
			continue
		}
		sb.WriteString("- [")
		sb.WriteString(id)
		sb.WriteString("] ")
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}

// parseMemoryCaptureResponse 容错解析提炼输出：容忍 markdown 代码块与前后缀文本。
func parseMemoryCaptureResponse(raw string) ([]memoryCaptureOperation, error) {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("响应中未找到 JSON 对象")
	}
	var payload struct {
		Operations []memoryCaptureOperation `json:"operations"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
		return nil, err
	}
	if payload.Operations == nil {
		payload.Operations = []memoryCaptureOperation{}
	}
	return payload.Operations, nil
}

// applyMemoryCaptureOp 落库单条提炼操作（add=新增 / update=改写现有记忆）。
func (s *Service) applyMemoryCaptureOp(ctx context.Context, db *sql.DB, sessionID string, op memoryCaptureOperation) error {
	content := strings.TrimSpace(op.Content)
	if content == "" {
		return nil
	}
	if runes := []rune(content); len(runes) > 500 {
		content = string(runes[:500])
	}
	switch op.Action {
	case "add":
		importance := 5
		if op.Importance != nil {
			importance = clampImportance(*op.Importance)
		}
		_, err := s.insertMemory(ctx, db, content, importance, strings.TrimSpace(op.Triggers), false, "auto", sessionID)
		return err
	case "update":
		if op.ID == "" {
			return nil
		}
		patch := memoryPatch{Content: &content}
		if op.Importance != nil {
			v := clampImportance(*op.Importance)
			patch.Importance = &v
		}
		if strings.TrimSpace(op.Triggers) != "" {
			t := strings.TrimSpace(op.Triggers)
			patch.Triggers = &t
		}
		_, err := s.updateMemory(ctx, db, op.ID, patch)
		if err != nil && errors.Is(err, ErrMemoryNotFound) {
			// 引用记忆已被删除：忽略该更新
			return nil
		}
		return err
	default:
		// 未知 action 静默忽略（模型偶发幻觉，不阻塞整批）
		return nil
	}
}
