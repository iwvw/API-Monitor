package adminai

import (
	"net/http"
	"strconv"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// auditItem 是审计查询的合并视图行（executions + tool_calls）。
type auditItem struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionId"`
	Source        string `json:"source,omitempty"`
	Kind          string `json:"kind"` // "execution" | "tool_call"
	Status        string `json:"status"`
	ToolName      string `json:"toolName,omitempty"`
	InputSummary  string `json:"inputSummary,omitempty"`
	OutputSummary string `json:"outputSummary,omitempty"`
	LLMModel      string `json:"llmModel,omitempty"`
	PromptTokens  int    `json:"promptTokens,omitempty"`
	CompletionTok int    `json:"completionTokens,omitempty"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt,omitempty"`
	Error         string `json:"error,omitempty"`
}

// handleAudit GET /api/admin-ai/audit?sessionId=&source=&limit=&offset=
// 合并 executions 与 tool_calls 的审计视图（PRD-04 审计查询 API）。
func (s *Service) handleAudit(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	source := r.URL.Query().Get("source")
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

	// executions 分支
	execWhere := "WHERE 1=1"
	execArgs := []interface{}{}
	if sessionID != "" {
		execWhere += " AND session_id = ?"
		execArgs = append(execArgs, sessionID)
	}
	if source != "" {
		execWhere += " AND source = ?"
		execArgs = append(execArgs, source)
	}

	execRows, err := db.QueryContext(r.Context(),
		`SELECT id, session_id, source, status, llm_model, llm_prompt_tokens, llm_completion_tokens, started_at, COALESCE(finished_at,''), COALESCE(error,'') FROM admin_ai_executions `+execWhere+` ORDER BY started_at DESC LIMIT ?`,
		append(execArgs, limit)...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer execRows.Close()

	execs := make([]*auditItem, 0, limit)
	for execRows.Next() {
		var item auditItem
		if err := execRows.Scan(&item.ID, &item.SessionID, &item.Source, &item.Status,
			&item.LLMModel, &item.PromptTokens, &item.CompletionTok, &item.StartedAt, &item.FinishedAt, &item.Error); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		item.Kind = "execution"
		execs = append(execs, &item)
	}
	if err := execRows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// tool_calls：join executions 取来源
	tcRows, err := db.QueryContext(r.Context(),
		`SELECT t.id, t.execution_id, e.session_id, t.tool_name, t.status, t.input_json, COALESCE(t.output_summary,''), COALESCE(e.source,''), t.started_at, COALESCE(t.finished_at,''), COALESCE(t.blocked_by_approval,'')
		 FROM admin_ai_tool_calls t
		 JOIN admin_ai_executions e ON e.id = t.execution_id
		 WHERE (? = '' OR e.session_id = ?) AND (? = '' OR e.source = ?)
		 ORDER BY t.started_at DESC LIMIT ?`,
		sessionID, sessionID, source, source, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tcRows.Close()

	toolCalls := make([]*auditItem, 0, limit)
	for tcRows.Next() {
		var item auditItem
		var execID, inputJSON, blockedBy string
		if err := tcRows.Scan(&item.ID, &execID, &item.SessionID, &item.ToolName, &item.Status,
			&inputJSON, &item.OutputSummary, &item.Source, &item.StartedAt, &item.FinishedAt, &blockedBy); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(inputJSON) > 200 {
			item.InputSummary = inputJSON[:200] + "...[截断]"
		} else {
			item.InputSummary = inputJSON
		}
		item.Kind = "tool_call"
		if blockedBy != "" && item.Status == "blocked" {
			item.Error = "被审批拦截: " + blockedBy
		}
		toolCalls = append(toolCalls, &item)
	}
	if err := tcRows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 合并（按 started_at 倒序，应用 offset/limit）
	merged := make([]*auditItem, 0, len(execs)+len(toolCalls))
	merged = append(merged, execs...)
	merged = append(merged, toolCalls...)
	// 简单稳定排序：started_at 降序（同表内已有序，此处做整体归并）
	merged = mergeAuditByTime(merged)

	total := len(merged)
	if offset > total {
		offset = total
	}
	page := merged[offset:]
	if len(page) > limit {
		page = page[:limit]
	}
	items := make([]auditItem, 0, len(page))
	for _, it := range page {
		items = append(items, *it)
	}
	response.OK(w, map[string]interface{}{"items": items, "total": total, "offset": offset, "limit": limit})
}

func mergeAuditByTime(items []*auditItem) []*auditItem {
	// 插入排序（数据量小）按 started_at 降序
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j-1].StartedAt < items[j].StartedAt; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
	return items
}
