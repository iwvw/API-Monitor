package adminai

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// sqlExecer 抽象 *sql.DB 的执行能力，供 execBusyRetry 在测试中注入瞬时忙锁故障。
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// execBusyRetry 执行落库写入并对 SQLite 忙锁（database is locked）做有限重试。
// 进程级连接池最多 4 条物理连接共享同一库，写高峰期（多 run + 会话标题异步写 +
// WAL 周期 TRUNCATE）可能瞬时击穿 busy_timeout 阈值；这类偶发锁冲突重试即可，
// 不必把错误一路顶到用户。超过重试上限返回最后一次错误，由调用方决定上报方式。
func execBusyRetry(ctx context.Context, db sqlExecer, query string, args ...interface{}) error {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		_, lastErr = db.ExecContext(ctx, query, args...)
		if lastErr == nil {
			return nil
		}
		msg := lastErr.Error()
		if !strings.Contains(msg, "database is locked") && !strings.Contains(msg, "SQLITE_BUSY") {
			return lastErr
		}
		if attempt+1 >= maxAttempts {
			break
		}
		slog.Warn("adminai-exec-busy-retry", "attempt", attempt+1, "query", query, "err", lastErr.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(150*(1<<attempt)) * time.Millisecond):
		}
	}
	return lastErr
}

// system_config 键名（与 adminAISettingDefs 对齐，供 getIntSetting 读取）。
const (
	adminAIKeyToolCallLimit          = "admin_ai_tool_call_limit"
	adminAIKeyTimeoutSeconds         = "admin_ai_timeout_seconds"
	adminAIKeyMemoriesEnabled        = "admin_ai_memories_enabled"
	adminAIKeyMemoriesBootstrapChars = "admin_ai_memories_bootstrap_chars"
	adminAIKeyMemoriesModel          = "admin_ai_memories_model" // 自动记忆提炼专用模型（留空回退会话模型/默认模型）
	adminAIKeyContextWindow          = "admin_ai_context_window"

	adminAIKeyMaxConcurrentRuns = "admin_ai_max_concurrent_runs" // 全局并发执行上限
	adminAIKeySummaryModel      = "admin_ai_summary_model"       // 推理摘要专用模型（留空回退默认模型）
	adminAIKeyReasoningEffort   = "admin_ai_reasoning_effort"    // 思考强度（low/medium/high，留空不传由上游决定）
)

// reasoningEffortValues 是思考强度的合法取值域。留空表示不携带 reasoning_effort，
// 保持上游默认行为；厂商差异（max→high、thinking/effort 别名、budget 语义）由
// /v1 网关的 normalizeReasoningEffort / requestEnablesReasoning 统一处理。
var reasoningEffortValues = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

// normalizeReasoningEffort 收敛思考强度取值：非法值一律归空（不传），避免把
// 拼写错误直接透传给上游导致请求被拒。
func normalizeReasoningEffort(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if reasoningEffortValues[v] {
		return v
	}
	return ""
}

const defaultMemoriesBootstrapChars = 2000

func (s *Service) getWriteEnabled(ctx context.Context, db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_write_enabled'").Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true" || value == "1", nil
}

// getAutoApprove 读取「完全批准模式」（admin_ai_auto_approve）：开启后
// 所有写操作免审批直接执行；未配置时默认关闭。
func (s *Service) getAutoApprove(ctx context.Context, db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_auto_approve'").Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true" || value == "1", nil
}

// getIntSetting 读取 system_config 的整数配置，缺失或非法时回默认值。
// 与 adminAISettingDefs（approvals.go）共用键名。
func (s *Service) getIntSetting(ctx context.Context, key string, def int) int {
	db, err := s.open(ctx)
	if err != nil {
		return def
	}
	defer db.Close()
	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if err != nil || value == "" {
		return def
	}
	if n, convErr := strconv.Atoi(value); convErr == nil && n > 0 {
		return n
	}
	return def
}

// getSetting 读取 system_config 的字符串配置，缺失时回默认值。
func (s *Service) getSetting(ctx context.Context, key, def string) string {
	db, err := s.open(ctx)
	if err != nil {
		return def
	}
	defer db.Close()
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", key).Scan(&value); err != nil || value == "" {
		return def
	}
	return value
}

// getBoolSetting 读取 system_config 的布尔配置，缺失或非法时回默认值。
func (s *Service) getBoolSetting(ctx context.Context, key string, def bool) bool {
	db, err := s.open(ctx)
	if err != nil {
		return def
	}
	defer db.Close()
	var value string
	err = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if err != nil || value == "" {
		return def
	}
	if value == "true" || value == "1" {
		return true
	}
	if value == "false" || value == "0" {
		return false
	}
	return def
}

func (s *Service) finishExecution(db *sql.DB, sessionID, execID, status string, toolCount int, llmModel string, promptTokens, completionTokens int, errMsg string) {
	// 独立短超时上下文落库：不依赖 run 的 ctx（结束瞬间可能已取消），
	// 也不无界阻塞；失败不再静默吞掉，写日志便于排查「执行状态不及时收口」。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339)
	var errField interface{}
	if errMsg != "" {
		errField = errMsg
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE admin_ai_executions SET status = ?, tool_calls_count = ?, llm_model = ?, llm_prompt_tokens = ?, llm_completion_tokens = ?, finished_at = ?, error = ? WHERE id = ?`,
		status, toolCount, llmModel, promptTokens, completionTokens, now, errField, execID); err != nil {
		slog.Warn("finish-execution", "execId", execID, "err", err.Error())
	}
	// 执行结束（无论成败）刷新会话活动时间，让前端轮询能感知到「有新消息」并重拉。
	// 此前只在 run 开始时更新一次：机器人/定时任务等外部来源的对话没有 SSE 推送通道，
	// 前端只能靠 lastActivityAt 变化触发 loadMessages，结束不更新则最终回复永远不出现。
	if _, err := db.ExecContext(ctx,
		`UPDATE admin_ai_sessions SET last_activity_at = ?, updated_at = ? WHERE id = ?`, now, now, sessionID); err != nil {
		slog.Warn("finish-execution-session", "sessionId", sessionID, "err", err.Error())
	}
}

// truncateContent 按 rune 边界截断，避免按字节截断切断多字节 UTF-8 字符
// （非法 UTF-8 进入 LLM 上下文/DB/UI 会造成乱码或部分上游编码错误）。
func truncateContent(s string) string {
	chars := []rune(s)
	if len(chars) <= contentSizeLimit {
		return s
	}
	return string(chars[:contentSizeLimit]) + "...[已截断]"
}
