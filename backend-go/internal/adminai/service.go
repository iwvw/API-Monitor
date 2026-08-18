package adminai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/notification"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// AICaller 与 system.AICaller 同构，通过 Server 注入的 callAPIFromAI 传递。
type AICaller systemmetrics.AICaller

// Service 是管理 AI 核心引擎的 HTTP 服务。
type Service struct {
	cfg        config.Config
	store      *database.Store
	schemaOnce sync.Once
	schemaErr  error
	aiCaller   AICaller

	mu          sync.Mutex
	runs        map[string]chan SSEEvent           // runId(execId) -> 事件通道
	sessionRuns map[string]string                  // sessionId -> runId，同一会话只允许一个活跃执行
	cancels     map[string]context.CancelFunc      // runId -> runCtx 取消函数（订阅后仍可真正终止执行）
	approval    map[string]chan approvalResolution // approvalId -> 审批结果通道
	runPolicy   map[string]string                          // runID → 定时任务写策略（"" / allow / readonly）
	runBuffers  map[string]*runEventBuffer                 // runID → 事件环形缓冲（断线重连重放）
	chToBuf     map[chan SSEEvent]*runEventBuffer          // 事件通道 → 缓冲（emit 时写缓冲）
	runDone     map[string]bool                            // runID → 执行是否已结束（serveSSE 归还通道判定）
	runPhase    map[string]string                          // runID → 实时阶段（starting/thinking/tooling），供会话列表展示                  // runId -> 定时任务策略："" 普通 | "allow" 写操作免审批 | "readonly" 禁用写操作

	catalogMu    sync.Mutex // 确定性接口清单缓存（apiCatalogText）
	catalogText  string
	catalogDone  bool
	catalogDescs map[string]string // path -> 中文描述（工具步骤展示用）
	catalogRoutes []map[string]interface{} // 完整路由契约缓存（get_route 用），与 catalogText 同锁
	catalogPrefixes map[string]string // 聚合前缀（模块总入口） -> 描述；不可直接调用（buildCatalogText 构建）

	chanMgr     *channelManager // PRD-03 频道接入（channels.go）
	channelQueueLocks map[string]*sync.Mutex // sessionId → 频道入站排队锁（channels.go，串行化同会话排队者）
	cleanerOnce sync.Once       // PRD-04 审批超时清理 goroutine
	stopCleaner chan struct{}
	captureOnce sync.Once       // 自动记忆提炼 goroutine（memory_agent.go）
	stopCapture chan struct{}
	captureInFlight map[string]bool // 提炼防重：同会话同时只允许一次提炼（进程内）
	toolLoops      map[string]int // 工具循环检测：runId|指纹 -> 累计调用次数（engine.go）
	src         *notification.Service // 通知中心：AI 频道 bot token 来源 + 结果推送出口
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:         cfg,
		store:       database.New(cfg),
		runs:        make(map[string]chan SSEEvent),
		sessionRuns: make(map[string]string),
		cancels:     make(map[string]context.CancelFunc),
		approval:    make(map[string]chan approvalResolution),
		runPolicy:   make(map[string]string),
		runBuffers:      make(map[string]*runEventBuffer),
		chToBuf:         make(map[chan SSEEvent]*runEventBuffer),
		runDone:         make(map[string]bool),
		runPhase:        make(map[string]string),
		captureInFlight: make(map[string]bool),
		toolLoops:       make(map[string]int),
		channelQueueLocks: make(map[string]*sync.Mutex),
	}
}

func (s *Service) SetAICaller(caller AICaller) {
	s.aiCaller = caller
}

// SetNotificationSource 注入通知中心服务：AI 频道 bot token 复用通知渠道配置，
// 定时任务/简报结果经通知中心渠道直发（出站不再依赖自建频道）。
func (s *Service) SetNotificationSource(src *notification.Service) {
	s.src = src
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	s.schemaOnce.Do(func() {
		s.schemaErr = s.ensureSchema(ctx, db)
	})
	if s.schemaErr != nil {
		db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

func (s *Service) ensureSchema(ctx context.Context, db *sql.DB) error {
	// FTS 触发器升级：旧版为全列 AFTER UPDATE（访问计数更新也会重插 FTS 索引），
	// 新版仅 content 变更触发；DROP 后由下方 CREATE IF NOT EXISTS 重建，幂等。
	if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_admin_ai_memories_au`); err != nil {
		return fmt.Errorf("adminai ensureSchema drop trg: %w", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_ai_sessions (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			channel_ref TEXT,
			title TEXT,
			model TEXT,
			write_enabled INTEGER DEFAULT 0,
			identity_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_activity_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
			role TEXT NOT NULL,
			content TEXT,
			tool_call_meta TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_executions (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
			source TEXT NOT NULL,
			status TEXT NOT NULL,
			tool_calls_count INTEGER DEFAULT 0,
			llm_model TEXT,
			llm_prompt_tokens INTEGER DEFAULT 0,
			llm_completion_tokens INTEGER DEFAULT 0,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			error TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_tool_calls (
			id TEXT PRIMARY KEY,
			execution_id TEXT NOT NULL REFERENCES admin_ai_executions(id) ON DELETE CASCADE,
			tool_name TEXT NOT NULL,
			input_json TEXT,
			output_summary TEXT,
			status TEXT NOT NULL,
			blocked_by_approval TEXT,
			started_at TEXT NOT NULL,
			finished_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_approvals (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
			tool_call_id TEXT,
			status TEXT DEFAULT 'pending',
			plan_summary TEXT,
			method TEXT,
			path TEXT,
			body_snapshot TEXT,
			requested_by TEXT,
			approved_by TEXT,
			expires_at TEXT,
			created_at TEXT NOT NULL,
			resolved_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_channels (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			config_encrypted TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_channel_bindings (
			id TEXT PRIMARY KEY,
			channel_id TEXT NOT NULL REFERENCES admin_ai_channels(id) ON DELETE CASCADE,
			channel_user_id TEXT NOT NULL,
			channel_username TEXT,
			panel_user_id TEXT,
			role TEXT DEFAULT 'admin',
			created_at TEXT NOT NULL,
			UNIQUE(channel_id, channel_user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS admin_ai_memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			importance INTEGER NOT NULL DEFAULT 5,
			triggers TEXT NOT NULL DEFAULT '',
			pinned INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'manual',
			session_id TEXT,
			access_count INTEGER NOT NULL DEFAULT 0,
			last_accessed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS admin_ai_memories_fts USING fts5(content, id UNINDEXED, content='admin_ai_memories', content_rowid='rowid', tokenize='trigram case_sensitive 0')`,
		`CREATE TRIGGER IF NOT EXISTS trg_admin_ai_memories_ai AFTER INSERT ON admin_ai_memories BEGIN
			INSERT INTO admin_ai_memories_fts(rowid, content, id) VALUES (new.rowid, new.content, new.id);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_admin_ai_memories_ad AFTER DELETE ON admin_ai_memories BEGIN
			INSERT INTO admin_ai_memories_fts(admin_ai_memories_fts, rowid, content, id) VALUES ('delete', old.rowid, old.content, old.id);
		END`,
		`CREATE TRIGGER IF NOT EXISTS trg_admin_ai_memories_au AFTER UPDATE OF content ON admin_ai_memories BEGIN
			INSERT INTO admin_ai_memories_fts(admin_ai_memories_fts, rowid, content, id) VALUES ('delete', old.rowid, old.content, old.id);
			INSERT INTO admin_ai_memories_fts(rowid, content, id) VALUES (new.rowid, new.content, new.id);
		END`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_sessions_activity ON admin_ai_sessions(last_activity_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_messages_session ON admin_ai_messages(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_executions_session ON admin_ai_executions(session_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_tool_calls_exec ON admin_ai_tool_calls(execution_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_approvals_session ON admin_ai_approvals(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_approvals_status ON admin_ai_approvals(status)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_channels_type ON admin_ai_channels(type)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_bindings_user ON admin_ai_channel_bindings(channel_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_memories_updated ON admin_ai_memories(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_memories_importance ON admin_ai_memories(importance DESC)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("adminai ensureSchema: %w", err)
		}
	}
	// 现有 ai_access_audit 表扩展 channel 列（PRD-04，不改既有行）
	if err := ensureSQLiteColumn(ctx, db, "ai_access_audit", "channel", "TEXT"); err != nil {
		return fmt.Errorf("adminai ensureSchema ai_access_audit: %w", err)
	}
	// admin_ai_channels 扩展 notification_channel_id 列（AI 频道 bot token 复用通知中心渠道；空=沿用旧 config token）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_channels", "notification_channel_id", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_channels.notification_channel_id: %w", err)
	}
	// admin_ai_sessions 扩展 memory_extracted_at 列（自动记忆提炼游标：已提炼到的消息时间，空=尚未提炼）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_sessions", "memory_extracted_at", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_sessions.memory_extracted_at: %w", err)
	}
	// memory_extracted_msg_id：与 memory_extracted_at 组成 (created_at, id) 二元游标，
	// 批内超限时不把游标跳到「当前时刻」，剩余消息不会因时间盲区被永久跳过。
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_sessions", "memory_extracted_msg_id", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_sessions.memory_extracted_msg_id: %w", err)
	}
	// admin_ai_messages 扩展 reasoning_content 列（推理模型要求回传思考内容）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_messages", "reasoning_content", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_messages.reasoning_content: %w", err)
	}
	// admin_ai_messages 扩展 reasoning_summary 列（AI 生成的 ≤16 字推理摘要，前端收起态展示）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_messages", "reasoning_summary", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_messages.reasoning_summary: %w", err)
	}
	// admin_ai_messages 扩展 tool_call_id 列（tool 结果行记录其对应的 tool_call，用于恢复历史时按 ID 配对）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_messages", "tool_call_id", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_messages.tool_call_id: %w", err)
	}
	// admin_ai_messages 扩展 tool_status 列（tool 结果行记录执行成败，历史恢复时
	// 前端据此渲染红叉/绿勾，避免失败调用刷新后显示成功）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_messages", "tool_status", "TEXT DEFAULT ''"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_messages.tool_status: %w", err)
	}
	// admin_ai_approvals 扩展 reason 列（请求更改/拒绝原因）
	if err := ensureSQLiteColumn(ctx, db, "admin_ai_approvals", "reason", "TEXT"); err != nil {
		return fmt.Errorf("adminai ensureSchema admin_ai_approvals.reason: %w", err)
	}
	return nil
}

// ensureSQLiteColumn 幂等加列（模式与 openai 包一致，PRD-04 要求）。
// 表不存在时静默跳过（表由其他模块创建，如 ai_access_audit 属 system 包）。
func ensureSQLiteColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	var exists int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin-ai")
	path = strings.TrimSuffix(path, "/")

	switch {
	case path == "/sessions" && r.Method == http.MethodGet:
		s.listSessions(w, r)
	case path == "/sessions" && r.Method == http.MethodPost:
		s.createSession(w, r)
	case strings.HasPrefix(path, "/sessions/") && r.Method == http.MethodDelete:
		s.deleteSession(w, r, strings.TrimPrefix(path, "/sessions/"))
	case strings.HasPrefix(path, "/sessions/") && strings.HasSuffix(path, "/messages") && r.Method == http.MethodGet:
		sessionID := strings.TrimSuffix(strings.TrimPrefix(path, "/sessions/"), "/messages")
		sessionID = strings.TrimSuffix(sessionID, "/")
		s.listMessages(w, r, sessionID)
	case path == "/messages" && r.Method == http.MethodPost:
		s.submitMessage(w, r)
	case path == "/messages/stream" && r.Method == http.MethodGet:
		s.streamEvents(w, r)
	case path == "/cancel" && r.Method == http.MethodPost:
		s.cancelRun(w, r)
	case path == "/approvals" && r.Method == http.MethodGet:
		s.listApprovals(w, r)
	case strings.HasPrefix(path, "/approvals/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPost:
		approvalID := strings.TrimSuffix(strings.TrimPrefix(path, "/approvals/"), "/resolve")
		approvalID = strings.TrimSuffix(approvalID, "/")
		s.resolveApproval(w, r, approvalID)
	case strings.HasPrefix(path, "/approvals/") && r.Method == http.MethodGet:
		s.getApproval(w, r, strings.TrimPrefix(path, "/approvals/"))
	case path == "/audit" && r.Method == http.MethodGet:
		s.handleAudit(w, r)
	case path == "/channels" && r.Method == http.MethodGet:
		s.listChannels(w, r)
	case path == "/channels" && r.Method == http.MethodPost:
		s.createChannel(w, r)
	case strings.HasPrefix(path, "/channels/") && !strings.Contains(strings.TrimPrefix(path, "/channels/"), "/") && r.Method == http.MethodPut:
		s.updateChannel(w, r, strings.TrimPrefix(path, "/channels/"))
	case strings.HasPrefix(path, "/channels/") && !strings.Contains(strings.TrimPrefix(path, "/channels/"), "/") && r.Method == http.MethodDelete:
		s.deleteChannel(w, r, strings.TrimPrefix(path, "/channels/"))
	case strings.HasPrefix(path, "/channels/"):
		rest := strings.TrimPrefix(path, "/channels/")
		parts := strings.SplitN(rest, "/", 2)
		channelID := parts[0]
		action := ""
		if len(parts) == 2 {
			action = parts[1]
		}
		if action == "start" || action == "stop" || action == "status" {
			s.channelAction(w, r, channelID, action)
			return
		}
		response.Error(w, http.StatusNotFound, "频道路由不存在")
	case path == "/channel-bindings" && r.Method == http.MethodGet:
		s.listBindings(w, r)
	case path == "/channel-bindings" && r.Method == http.MethodPost:
		s.createBinding(w, r)
	case strings.HasPrefix(path, "/channel-bindings/") && r.Method == http.MethodDelete:
		s.deleteBinding(w, r, strings.TrimPrefix(path, "/channel-bindings/"))
	case path == "/settings" && (r.Method == http.MethodGet || r.Method == http.MethodPut):
		s.handleSettings(w, r)
	case path == "/memories" && r.Method == http.MethodGet:
		s.handleListMemories(w, r)
	case path == "/memories" && r.Method == http.MethodPost:
		s.handleCreateMemory(w, r)
	case strings.HasPrefix(path, "/memories/") && r.Method == http.MethodPut:
		s.handleUpdateMemory(w, r, strings.TrimPrefix(path, "/memories/"))
	case strings.HasPrefix(path, "/memories/") && r.Method == http.MethodDelete:
		s.handleDeleteMemory(w, r, strings.TrimPrefix(path, "/memories/"))
	case path == "/cron/daily-briefing" && r.Method == http.MethodGet:
		s.handleDailyBriefing(w, r)
	case path == "/cron/task-run" && r.Method == http.MethodPost:
		s.handleCronTaskRun(w, r)
	default:
		response.Error(w, http.StatusNotFound, "管理 AI 路由不存在")
	}
}

func (s *Service) listSessions(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(r.Context(), `SELECT id, source, COALESCE(channel_ref,''), COALESCE(title,''), COALESCE(model,''), write_enabled, COALESCE(identity_json,''), created_at, updated_at, last_activity_at FROM admin_ai_sessions ORDER BY last_activity_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type sessionItem struct {
		ID             string `json:"id"`
		Source         string `json:"source"`
		ChannelRef     string `json:"channelRef,omitempty"`
		Title          string `json:"title,omitempty"`
		Model          string `json:"model,omitempty"`
		WriteEnabled   bool   `json:"writeEnabled"`
		CreatedAt      string `json:"createdAt"`
		UpdatedAt      string `json:"updatedAt"`
LastActivityAt string `json:"lastActivityAt"`
	MessageCount   int    `json:"messageCount"`
	ActiveRun      *struct {
		RunID string `json:"runId"`
		Phase string `json:"phase"`
	} `json:"activeRun,omitempty"`
}

	sessions := make([]sessionItem, 0)
	for rows.Next() {
		var item sessionItem
		var we int
		var identity string
		if err := rows.Scan(&item.ID, &item.Source, &item.ChannelRef, &item.Title, &item.Model, &we, &identity, &item.CreatedAt, &item.UpdatedAt, &item.LastActivityAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.WriteEnabled = we == 1
		sessions = append(sessions, item)
	}
	rows.Close() // 先收行再查询：单连接池（SetMaxOpenConns(1)）下 rows 未关时同连接嵌套查询会自锁
	// 活跃 run 快照：锁内一次读 sessionRuns + runPhase，前端据此展示「思考中/执行工具…」指示
	s.mu.Lock()
	activeSnapshot := map[string]struct{ runID, phase string }{}
	for sid, rid := range s.sessionRuns {
		phase := s.runPhase[rid]
		if phase == "" {
			phase = "starting"
		}
		activeSnapshot[sid] = struct{ runID, phase string }{rid, phase}
	}
	s.mu.Unlock()
	for i := range sessions {
		_ = db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = ?", sessions[i].ID).Scan(&sessions[i].MessageCount)
		if ar, ok := activeSnapshot[sessions[i].ID]; ok {
			sessions[i].ActiveRun = &struct {
				RunID string `json:"runId"`
				Phase string `json:"phase"`
			}{RunID: ar.runID, Phase: ar.phase}
		}
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"sessions": sessions})
}

func (s *Service) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title  string `json:"title"`
		Model  string `json:"model"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Source == "" {
		req.Source = "web"
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	id, err := randomID("aas_")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	model := req.Model
	if model == "" {
		// 动态读设置（管理 AI 设置页保存的默认模型），兼容旧环境变量
		_ = db.QueryRowContext(r.Context(), "SELECT value FROM system_config WHERE key = 'admin_ai_default_model'").Scan(&model)
	}
	if model == "" {
		model = s.cfg.AdminAIDefaultModel
	}
	_, err = db.ExecContext(r.Context(),
		`INSERT INTO admin_ai_sessions (id, source, title, model, write_enabled, created_at, updated_at, last_activity_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
		id, req.Source, req.Title, model, now, now, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"id":             id,
		"source":         req.Source,
		"title":          req.Title,
		"model":          model,
		"createdAt":      now,
		"updatedAt":      now,
		"lastActivityAt": now,
	})
}

func (s *Service) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	result, err := db.ExecContext(r.Context(), "DELETE FROM admin_ai_sessions WHERE id = ?", sessionID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "会话不存在")
		return
	}
	response.OK(w, map[string]interface{}{"ok": true})
}

func (s *Service) listMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	type messageItem struct {
		ID               string `json:"id"`
		SessionID        string `json:"sessionId"`
		Role             string `json:"role"`
		Content          string `json:"content,omitempty"`
		ReasoningContent string `json:"reasoning_content,omitempty"`
		ReasoningSummary string `json:"reasoning_summary,omitempty"`
		ToolCallMeta     string `json:"toolCallMeta,omitempty"`
		ToolCallDesc     string `json:"toolCallDesc,omitempty"`
		ToolStatus       string `json:"toolStatus,omitempty"`
		CreatedAt        string `json:"createdAt"`
	}

	var rows *sql.Rows
	if cursor != "" {
		parts := strings.SplitN(cursor, "_", 2)
		if len(parts) != 2 {
			response.Error(w, http.StatusBadRequest, "游标格式无效")
			return
		}
		rows, err = db.QueryContext(r.Context(),
			`SELECT id, session_id, role, COALESCE(content,''), COALESCE(reasoning_content,''), COALESCE(reasoning_summary,''), COALESCE(tool_call_meta,''), COALESCE(tool_status,''), created_at FROM admin_ai_messages WHERE session_id = ? AND (created_at < ? OR (created_at = ? AND id < ?)) ORDER BY created_at DESC, id DESC LIMIT ?`,
			sessionID, parts[0], parts[0], parts[1], limit+1)
	} else {
		rows, err = db.QueryContext(r.Context(),
			`SELECT id, session_id, role, COALESCE(content,''), COALESCE(reasoning_content,''), COALESCE(reasoning_summary,''), COALESCE(tool_call_meta,''), COALESCE(tool_status,''), created_at FROM admin_ai_messages WHERE session_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
			sessionID, limit+1)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]messageItem, 0, limit)
	for rows.Next() {
		var item messageItem
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &item.ReasoningContent, &item.ReasoningSummary, &item.ToolCallMeta, &item.ToolStatus, &item.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		// 工具调用行补中文动作描述（与实时 tool_start 事件的 desc 一致，保证刷新前后样式统一）。
		// 逐调用计算并注入 meta（不落库，仅响应层装饰）：前端按调用序号取描述，
		// 刷新/轮询重拉后多轮 tool_calls 不再丢失各自描述。
		if item.ToolCallMeta != "" {
			var raw []map[string]interface{}
			if json.Unmarshal([]byte(item.ToolCallMeta), &raw) == nil && len(raw) > 0 {
				for _, tc := range raw {
					fn, _ := tc["function"].(map[string]interface{})
					name, _ := fn["name"].(string)
					argsJSON := ""
					if a, ok := fn["arguments"].(string); ok {
						argsJSON = a
					}
					if d := s.toolDesc(name, argsJSON); d != "" {
						tc["desc"] = d
					}
				}
				if b, err := json.Marshal(raw); err == nil {
					item.ToolCallMeta = string(b)
				}
				if fn, _ := raw[0]["function"].(map[string]interface{}); fn != nil {
					name, _ := fn["name"].(string)
					argsJSON := ""
					if a, ok := fn["arguments"].(string); ok {
						argsJSON = a
					}
					item.ToolCallDesc = s.toolDesc(name, argsJSON)
				}
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var nextCursor string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		nextCursor = last.CreatedAt + "_" + last.ID
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}

	resp := map[string]interface{}{"items": items}
	// 活跃 run 快照：与会话列表同源（锁内读 sessionRuns + runPhase）。
	// 前端据此在无 SSE 通道的外部 run（MCP/API/BOT/定时任务）期间把最后一条
	// 助手消息标为 live，推理/工具/代理图标继续动态化，而不是退化为静态终态。
	s.mu.Lock()
	activeRunID := s.sessionRuns[sessionID]
	phase := s.runPhase[activeRunID]
	s.mu.Unlock()
	if activeRunID != "" {
		if phase == "" {
			phase = "starting"
		}
		resp["activeRun"] = map[string]interface{}{"runId": activeRunID, "phase": phase}
	}
	if nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	response.OK(w, resp)
}

func (s *Service) submitMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
		Source    string `json:"source"`
		RewindID  string `json:"rewindId"` // 编辑重发：删除该消息及其后所有消息后再执行
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.SessionID == "" || req.Prompt == "" {
		response.Error(w, http.StatusBadRequest, "sessionId 和 prompt 不能为空")
		return
	}
	source := req.Source
	if source == "" {
		source = "web"
	}

	if req.RewindID != "" {
		// 编辑重发是排他操作：运行中的 run 正在逐轮消费历史，此时截断会让其
		// 上下文与落库错位，与 opencode 的 assertNotBusy（revert/delete）语义一致。
		s.mu.Lock()
		_, busy := s.sessionRuns[req.SessionID]
		s.mu.Unlock()
		if busy {
			response.Error(w, http.StatusConflict, "该会话已有执行进行中，请稍候再编辑")
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		var createdAt string
		err = db.QueryRowContext(r.Context(),
			`SELECT created_at FROM admin_ai_messages WHERE id = ? AND session_id = ?`,
			req.RewindID, req.SessionID).Scan(&createdAt)
		if err == sql.ErrNoRows {
			db.Close()
			response.Error(w, http.StatusBadRequest, "要编辑的消息不存在或已被清理")
			return
		}
		if err != nil {
			db.Close()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, err = db.ExecContext(r.Context(),
			`DELETE FROM admin_ai_messages WHERE session_id = ? AND (created_at > ? OR (created_at = ? AND id >= ?))`,
			req.SessionID, createdAt, createdAt, req.RewindID)
		db.Close()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// join 语义：会话已有活跃 run 时，追问不再 409——直接入库，由活跃 run 在
	// 每轮循环/最终落库前增量同步并续跑消费（对齐 opencode：运行中提交的消息
	// 不会被拒绝，只会被当前执行在下一轮接住）。
	s.mu.Lock()
	activeRunID, _ := s.sessionRuns[req.SessionID]
	s.mu.Unlock()
	if activeRunID != "" {
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		newID := nextID(r.Context(), db, "aam_")
		_, err = db.ExecContext(r.Context(),
			`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'user', ?, ?)`,
			newID, req.SessionID, req.Prompt, time.Now().UTC().Format(time.RFC3339))
		db.Close()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		// 锁内复查：若活跃 run 恰在「最终检查→退场」窗口（检查先于入队），则由
		// 本请求兜底启动新 run 消费；两处检查共用 s.mu 串行，不会双双错过。
		s.mu.Lock()
		if rid, still := s.sessionRuns[req.SessionID]; still {
			s.mu.Unlock()
			response.OK(w, map[string]interface{}{"sessionId": req.SessionID, "runId": rid, "queued": true})
			return
		}
		s.mu.Unlock()
		runID, err := s.RunLoop(context.Background(), source, req.SessionID, req.Prompt, "", req.Model, "")
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"sessionId": req.SessionID, "runId": runID, "queued": false})
		return
	}

	runID, err := s.RunLoop(context.Background(), source, req.SessionID, req.Prompt, "", req.Model, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"sessionId": req.SessionID, "runId": runID})
}

func (s *Service) streamEvents(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runId")
	if runID == "" {
		response.Error(w, http.StatusBadRequest, "runId 不能为空")
		return
	}
	resume := r.URL.Query().Get("resume") == "1"
	fromSeq, _ := strconv.ParseInt(r.URL.Query().Get("fromSeq"), 10, 64)

	s.mu.Lock()
	ch, exists := s.runs[runID]
	if exists {
		// 原子领取通道：无论首连还是 resume 重连，同一 run 同一时刻只允许
		// 一个 SSE 消费者。此前 resume 不领取会让断线重连期间的多个连接
		// 同时读共享 channel，事件被分散到不同连接上。断连时 serveSSE 会
		// 把通道归还到 s.runs（run 未结束的情况下），重连仍可再次取用。
		delete(s.runs, runID)
	}
	s.mu.Unlock()

	if !exists {
		if buf := s.bufferForRun(runID); buf != nil {
			// run 已结束但缓冲仍在保留期内：重放尾部终态/状态事件后关闭
			serveSSE(w, r, s, runID, nil, true, fromSeq)
			return
		}
		slog.Warn("stream-not-found", "runId", runID, "activeRuns", len(s.runs))
		response.Error(w, http.StatusNotFound, "执行不存在或已结束")
		return
	}
	serveSSE(w, r, s, runID, ch, resume, fromSeq)
}

func (s *Service) cancelRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.RunID == "" {
		response.Error(w, http.StatusBadRequest, "runId 不能为空")
		return
	}

	s.mu.Lock()
	cancel, hasCancel := s.cancels[req.RunID]
	if hasCancel {
		// 通道关闭交给 runInference 自身（避免生产者向已关闭通道发送导致 panic）
		delete(s.cancels, req.RunID)
	}
	var cancelledSession string
	for sid, rid := range s.sessionRuns {
		if rid == req.RunID {
			cancelledSession = sid
			delete(s.sessionRuns, sid)
		}
	}
	s.mu.Unlock()
	if hasCancel {
		cancel()
	}
	_ = cancelledSession
	response.OK(w, map[string]interface{}{"cancelled": hasCancel})
}

func (s *Service) resolveApproval(w http.ResponseWriter, r *http.Request, approvalID string) {
	var req struct {
		Action         string `json:"action"`
		ApplyToSession bool   `json:"applyToSession"`
		Reason         string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		response.Error(w, http.StatusBadRequest, "action 必须为 approve 或 reject")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(r.Context(),
		`UPDATE admin_ai_approvals SET status = ?, resolved_at = ?, reason = ? WHERE id = ? AND status = 'pending'`,
		req.Action, now, req.Reason, approvalID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		response.Error(w, http.StatusConflict, "该审批已过期或已处理")
		return
	}

	// 允许此对话：批准的同时把本会话标记为后续写操作免审批（仅本会话生效）
	if req.Action == "approve" && req.ApplyToSession {
		var sessionID string
		if err := db.QueryRowContext(r.Context(), "SELECT session_id FROM admin_ai_approvals WHERE id = ?", approvalID).Scan(&sessionID); err == nil && sessionID != "" {
			_, _ = db.ExecContext(r.Context(), "UPDATE admin_ai_sessions SET write_enabled = 1 WHERE id = ?", sessionID)
		}
	}

	s.mu.Lock()
	if ch, exists := s.approval[approvalID]; exists {
		select {
		case ch <- approvalResolution{Action: req.Action, Reason: req.Reason}:
		default:
		}
	}
	s.mu.Unlock()

	response.OK(w, map[string]interface{}{"ok": true, "action": req.Action})
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(buf)), nil
}
