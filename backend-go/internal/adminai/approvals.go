package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// approvalTTL 是写操作审批的有效期（PRD-04：30 分钟 TTL，超时自动 expired）。
const approvalTTL = 30 * time.Minute

// approvalCleanerInterval 是后台清理 goroutine 的扫描间隔。
const approvalCleanerInterval = 1 * time.Minute

// StartBackground 启动 service 级后台任务（审批超时清理 + 自动记忆提炼）。
// 由 server 初始化后调用；Stop 后 goroutine 退出。
func (s *Service) StartBackground() {
	s.cleanerOnce.Do(func() {
		s.recoverStaleExecutions()
		s.stopCleaner = make(chan struct{})
		go s.approvalCleanerLoop()
	})
	s.startMemoryCapture()
}

// recoverStaleExecutions 启动恢复：上次进程中断残留的 running 执行统一标记为 error，
// 避免前端永远看到"进行中"的假状态（executions 表无启动恢复前无自愈路径）。
func (s *Service) recoverStaleExecutions() {
	db, err := s.open(context.Background())
	if err != nil {
		slog.Warn("recover-executions", "err", err.Error())
		return
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(context.Background(),
		`UPDATE admin_ai_executions SET status = 'error', finished_at = ?, error = '进程中断：服务重启前的执行未完成' WHERE status = 'running'`,
		now)
	if err != nil {
		slog.Warn("recover-executions", "err", err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("recover-executions", "recovered", n)
	}
}

// StopBackground 停止后台任务（幂等）。
func (s *Service) StopBackground() {
	if s.stopCleaner == nil {
		return
	}
	select {
	case <-s.stopCleaner:
		return
	default:
	}
	close(s.stopCleaner)
	s.stopCleaner = nil
	if s.stopCapture != nil {
		select {
		case <-s.stopCapture:
		default:
			close(s.stopCapture)
			s.stopCapture = nil
		}
	}
}

// approvalCleanerLoop 定时把已过期的 pending 审批标记为 expired。
func (s *Service) approvalCleanerLoop() {
	ticker := time.NewTicker(approvalCleanerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCleaner:
			return
		case <-ticker.C:
			s.expireOverdueApprovals()
		}
	}
}

func (s *Service) expireOverdueApprovals() {
	db, err := s.open(context.Background())
	if err != nil {
		return
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`UPDATE admin_ai_approvals SET status = 'expired', resolved_at = ? WHERE status = 'pending' AND expires_at < ?`,
		now, now)
}

type approvalItem struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId"`
	ToolCallID   string `json:"toolCallId,omitempty"`
	Status       string `json:"status"`
	PlanSummary  string `json:"planSummary,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	BodySnapshot string `json:"bodySnapshot,omitempty"`
	RequestedBy  string `json:"requestedBy,omitempty"`
	ApprovedBy   string `json:"approvedBy,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	CreatedAt    string `json:"createdAt"`
	ResolvedAt   string `json:"resolvedAt,omitempty"`
}

// listApprovals GET /api/admin-ai/approvals?status=&sessionId=&limit=&offset=
func (s *Service) listApprovals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	sessionID := r.URL.Query().Get("sessionId")
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o > 0 {
		offset = o
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	where := "WHERE 1=1"
	args := []interface{}{}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if sessionID != "" {
		where += " AND session_id = ?"
		args = append(args, sessionID)
	}

	rows, err := db.QueryContext(r.Context(),
		`SELECT id, session_id, COALESCE(tool_call_id,''), status, COALESCE(plan_summary,''), COALESCE(method,''), COALESCE(path,''), COALESCE(body_snapshot,''), COALESCE(requested_by,''), COALESCE(approved_by,''), COALESCE(expires_at,''), created_at, COALESCE(resolved_at,'') FROM admin_ai_approvals `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	items := make([]approvalItem, 0)
	for rows.Next() {
		var item approvalItem
		if err := rows.Scan(&item.ID, &item.SessionID, &item.ToolCallID, &item.Status, &item.PlanSummary,
			&item.Method, &item.Path, &item.BodySnapshot, &item.RequestedBy, &item.ApprovedBy,
			&item.ExpiresAt, &item.CreatedAt, &item.ResolvedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"items": items, "total": len(items)})
}

// getApproval GET /api/admin-ai/approvals/{id}
func (s *Service) getApproval(w http.ResponseWriter, r *http.Request, approvalID string) {
	// 惰性兜底：读取时若已过期且仍 pending，先标记 expired。
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	_, _ = db.ExecContext(r.Context(),
		`UPDATE admin_ai_approvals SET status = 'expired', resolved_at = ? WHERE id = ? AND status = 'pending' AND expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339), approvalID, time.Now().UTC().Format(time.RFC3339))

	var item approvalItem
	err = db.QueryRowContext(r.Context(),
		`SELECT id, session_id, COALESCE(tool_call_id,''), status, COALESCE(plan_summary,''), COALESCE(method,''), COALESCE(path,''), COALESCE(body_snapshot,''), COALESCE(requested_by,''), COALESCE(approved_by,''), COALESCE(expires_at,''), created_at, COALESCE(resolved_at,'') FROM admin_ai_approvals WHERE id = ?`,
		approvalID).Scan(&item.ID, &item.SessionID, &item.ToolCallID, &item.Status, &item.PlanSummary,
		&item.Method, &item.Path, &item.BodySnapshot, &item.RequestedBy, &item.ApprovedBy,
		&item.ExpiresAt, &item.CreatedAt, &item.ResolvedAt)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "审批记录不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, item)
}

// --- settings 多键管理（PRD-04 系统配置） ---

// adminAISettingDefs 描述 settings 接口暴露的 system_config 键。
var adminAISettingDefs = []struct {
	Key         string
	Default     string
	Description string
}{
	{"admin_ai_enabled", "true", "管理 AI 总开关"},
	{"admin_ai_default_model", "", "默认推理模型（endpointId/modelName）"},
	{"admin_ai_briefing_model", "", "站点简报专用模型（留空回退默认模型）"},
	{"admin_ai_briefing_template", `{"type":"standard","custom":""}`, "站点简报模板（JSON：{type,custom}，type: standard/brief/detailed/alert_only/custom）"},
	{"admin_ai_write_enabled", "false", "写操作全局开关"},
	{"admin_ai_auto_approve", "false", "完全批准模式（所有写操作免审批直接执行）"},
	{"admin_ai_tool_call_limit", "12", "单轮最大工具调用次数"},
	{"admin_ai_timeout_seconds", "600", "单轮执行超时秒数"},
	{"admin_ai_context_window", "40000", "上下文窗口 token 上限"},
	{"admin_ai_audit_retention_days", "90", "审计记录保留天数"},
	{"admin_ai_memories_enabled", "true", "长期记忆总开关（跨会话持久事实与偏好）"},
	{"admin_ai_memories_bootstrap_chars", "2000", "每轮对话注入系统提示词的长期记忆字符上限"},
	{"admin_ai_memories_auto_capture", "true", "自动记忆提炼：会话空闲后后台总结值得长期记住的内容"},
	{"admin_ai_memories_idle_minutes", "10", "会话空闲多少分钟后触发自动记忆提炼"},
}

// handleSettings GET/PUT /api/admin-ai/settings
func (s *Service) handleSettings(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	if r.Method == http.MethodGet {
		values := map[string]string{}
		keyArgs := make([]interface{}, 0, len(adminAISettingDefs))
		for _, def := range adminAISettingDefs {
			keyArgs = append(keyArgs, def.Key)
		}
		rows, err := db.QueryContext(r.Context(),
			`SELECT key, value FROM system_config WHERE key IN (`+systemConfigKeyPlaceholders(adminAISettingDefs)+`)`,
			keyArgs...)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil {
				values[k] = v
			}
		}
		// 未配置的键回默认值
		for _, def := range adminAISettingDefs {
			if _, ok := values[def.Key]; !ok {
				values[def.Key] = def.Default
			}
		}
		response.OK(w, map[string]interface{}{"settings": values})
		return
	}

	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	valid := map[string]bool{}
	for _, def := range adminAISettingDefs {
		valid[def.Key] = true
	}
	for key, value := range req {
		if !valid[key] {
			continue
		}
		value = clampAISetting(key, value)
		desc := key
		for _, def := range adminAISettingDefs {
			if def.Key == key {
				desc = def.Description
				break
			}
		}
		_, _ = db.ExecContext(r.Context(),
			`INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES (?, ?, ?, ?)`,
			key, value, desc, now)
	}
	response.OK(w, map[string]interface{}{"ok": true})
}

// clampAISetting 对关键数值设置做范围收敛，防止误配超大值拖垮执行（如把
// context_window 配到 100 万导致单轮输入膨胀、回复明显变慢）。
func clampAISetting(key, value string) string {
	switch key {
	case "admin_ai_timeout_seconds":
		if n, err := strconv.Atoi(value); err == nil {
			if n < 30 {
				n = 30
			}
			if n > 3600 {
				n = 3600
			}
			return strconv.Itoa(n)
		}
	case "admin_ai_context_window":
		if n, err := strconv.Atoi(value); err == nil {
			if n < 4000 {
				n = 4000
			}
			if n > 200000 {
				n = 200000
			}
			return strconv.Itoa(n)
		}
	case "admin_ai_tool_call_limit":
		if n, err := strconv.Atoi(value); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 100 {
				n = 100
			}
			return strconv.Itoa(n)
		}
	}
	return value
}

func systemConfigKeyPlaceholders(defs []struct {
	Key         string
	Default     string
	Description string
}) string {
	placeholders := ""
	for i := range defs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	return placeholders
}

