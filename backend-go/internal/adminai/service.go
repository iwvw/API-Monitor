package adminai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
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
	runs        map[string]chan SSEEvent // runId(execId) -> 事件通道
	sessionRuns map[string]string        // sessionId -> runId，同一会话只允许一个活跃执行
	approval    map[string]chan string   // approvalId -> 审批结果通道
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:         cfg,
		store:       database.New(cfg),
		runs:        make(map[string]chan SSEEvent),
		sessionRuns: make(map[string]string),
		approval:    make(map[string]chan string),
	}
}

func (s *Service) SetAICaller(caller AICaller) {
	s.aiCaller = caller
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
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_sessions_activity ON admin_ai_sessions(last_activity_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_messages_session ON admin_ai_messages(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_executions_session ON admin_ai_executions(session_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_tool_calls_exec ON admin_ai_tool_calls(execution_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_approvals_session ON admin_ai_approvals(session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_ai_approvals_status ON admin_ai_approvals(status)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("adminai ensureSchema: %w", err)
		}
	}
	return nil
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
	case strings.HasPrefix(path, "/approvals/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPost:
		approvalID := strings.TrimSuffix(strings.TrimPrefix(path, "/approvals/"), "/resolve")
		approvalID = strings.TrimSuffix(approvalID, "/")
		s.resolveApproval(w, r, approvalID)
	case path == "/settings" && (r.Method == http.MethodGet || r.Method == http.MethodPut):
		s.handleSettings(w, r)
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
	}

	sessions := make([]sessionItem, 0)
	for rows.Next() {
		var item sessionItem
		var we int
		if err := rows.Scan(&item.ID, &item.Source, &item.ChannelRef, &item.Title, &item.Model, &we, &item.CreatedAt, &item.UpdatedAt, &item.LastActivityAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.WriteEnabled = we == 1
		_ = db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = ?", item.ID).Scan(&item.MessageCount)
		sessions = append(sessions, item)
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
		ID           string `json:"id"`
		SessionID    string `json:"sessionId"`
		Role         string `json:"role"`
		Content      string `json:"content,omitempty"`
		ToolCallMeta string `json:"toolCallMeta,omitempty"`
		CreatedAt    string `json:"createdAt"`
	}

	var rows *sql.Rows
	if cursor != "" {
		parts := strings.SplitN(cursor, "_", 2)
		if len(parts) != 2 {
			response.Error(w, http.StatusBadRequest, "游标格式无效")
			return
		}
		rows, err = db.QueryContext(r.Context(),
			`SELECT id, session_id, role, COALESCE(content,''), COALESCE(tool_call_meta,''), created_at FROM admin_ai_messages WHERE session_id = ? AND (created_at < ? OR (created_at = ? AND id < ?)) ORDER BY created_at DESC, id DESC LIMIT ?`,
			sessionID, parts[0], parts[0], parts[1], limit+1)
	} else {
		rows, err = db.QueryContext(r.Context(),
			`SELECT id, session_id, role, COALESCE(content,''), COALESCE(tool_call_meta,''), created_at FROM admin_ai_messages WHERE session_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
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
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &item.ToolCallMeta, &item.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
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

	s.mu.Lock()
	if _, exists := s.sessionRuns[req.SessionID]; exists {
		s.mu.Unlock()
		response.Error(w, http.StatusConflict, "该会话已有执行进行中")
		return
	}
	s.mu.Unlock()

	runID, err := s.RunLoop(r.Context(), source, req.SessionID, req.Prompt, "", req.Model)
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

	s.mu.Lock()
	ch, exists := s.runs[runID]
	if exists {
		// 订阅方领取通道独占消费，避免多个 SSE 读者互相竞争。
		delete(s.runs, runID)
	}
	s.mu.Unlock()

	if !exists {
		response.Error(w, http.StatusNotFound, "执行不存在或已结束")
		return
	}
	serveSSE(w, r, ch)
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
	ch, exists := s.runs[req.RunID]
	if exists {
		delete(s.runs, req.RunID)
		close(ch)
	}
	var cancelledSession string
	for sid, rid := range s.sessionRuns {
		if rid == req.RunID {
			cancelledSession = sid
			delete(s.sessionRuns, sid)
		}
	}
	s.mu.Unlock()
	_ = cancelledSession
	response.OK(w, map[string]interface{}{"cancelled": exists})
}

func (s *Service) resolveApproval(w http.ResponseWriter, r *http.Request, approvalID string) {
	var req struct {
		Action string `json:"action"`
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
	_, err = db.ExecContext(r.Context(),
		`UPDATE admin_ai_approvals SET status = ?, resolved_at = ? WHERE id = ? AND status = 'pending'`,
		req.Action, now, approvalID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	if ch, exists := s.approval[approvalID]; exists {
		select {
		case ch <- req.Action:
		default:
		}
	}
	s.mu.Unlock()

	response.OK(w, map[string]interface{}{"ok": true, "action": req.Action})
}

func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if r.Method == http.MethodGet {
		var gatewayKey string
		_ = db.QueryRowContext(r.Context(), "SELECT value FROM system_config WHERE key = 'admin_ai_gateway_key'").Scan(&gatewayKey)
		response.OK(w, map[string]interface{}{"gatewayKey": gatewayKey})
		return
	}

	var req struct {
		GatewayKey string `json:"gatewayKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES ('admin_ai_gateway_key', ?, '管理 AI 网关密钥', ?)`,
		req.GatewayKey, now)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"ok": true})
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(buf)), nil
}
