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
// 分页采用「两表各取 offset+limit 条 → 跨表归并排序 → 取目标窗口」，
// 保证 offset 翻页在两类记录交替出现时依然正确，total 为真实匹配总数。
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

	execWhere := "WHERE 1=1"
	var execArgs []interface{}
	tcWhere := "WHERE 1=1"
	var tcArgs []interface{}
	if sessionID != "" {
		execWhere += " AND session_id = ?"
		execArgs = append(execArgs, sessionID)
		tcWhere += " AND e.session_id = ?"
		tcArgs = append(tcArgs, sessionID)
	}
	if source != "" {
		execWhere += " AND source = ?"
		execArgs = append(execArgs, source)
		tcWhere += " AND e.source = ?"
		tcArgs = append(tcArgs, source)
	}

	// 真实总数：executions + tool_calls 的匹配行数
	var execCount, tcCount int
	if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM admin_ai_executions `+execWhere, execArgs...).Scan(&execCount); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM admin_ai_tool_calls t JOIN admin_ai_executions e ON e.id = t.execution_id `+tcWhere, tcArgs...).Scan(&tcCount); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	total := execCount + tcCount

	fetch := offset + limit
	if fetch <= 0 {
		response.OK(w, map[string]interface{}{"items": []auditItem{}, "total": total, "offset": offset, "limit": limit})
		return
	}

	execRows, err := db.QueryContext(r.Context(),
		`SELECT id, session_id, source, status, llm_model, llm_prompt_tokens, llm_completion_tokens, started_at, COALESCE(finished_at,''), COALESCE(error,'') FROM admin_ai_executions `+execWhere+` ORDER BY started_at DESC LIMIT ?`,
		append(execArgs, fetch)...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer execRows.Close()

	execs := make([]*auditItem, 0, fetch)
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
		 `+tcWhere+`
		 ORDER BY t.started_at DESC LIMIT ?`,
		append(tcArgs, fetch)...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tcRows.Close()

	toolCalls := make([]*auditItem, 0, fetch)
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

	// 归并排序（started_at 降序）后取目标窗口 [offset, offset+limit)
	merged := make([]*auditItem, 0, len(execs)+len(toolCalls))
	merged = append(merged, execs...)
	merged = append(merged, toolCalls...)
	merged = mergeAuditByTime(merged)

	// offset 超过过滤后总数时返回空页，避免 merged[offset:end] 切片越界 panic
	if offset >= len(merged) {
		response.OK(w, map[string]interface{}{"items": []auditItem{}, "total": total, "offset": offset, "limit": limit})
		return
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	page := merged[offset:end]
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
