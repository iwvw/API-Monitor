package cronjobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/robfig/cron/v3"
)

type SchedulerTask struct {
	Task
	Description          string  `json:"description"`
	TimeoutSeconds       int     `json:"timeout_seconds"`
	RetryCount           int     `json:"retry_count"`
	RetryIntervalSeconds int     `json:"retry_interval_seconds"`
	MaxConcurrency       int     `json:"max_concurrency"`
	NodeID               string  `json:"node_id"`
	NodeSelector         string  `json:"node_selector"`
	Config               string  `json:"config"` // AI 任务扩展配置（JSON：model/policy/channelId 等）
	ScheduleSummary      string  `json:"schedule_summary"`
	RecentStatus         *string `json:"recent_status,omitempty"`
}

type Workflow struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Schedule          string         `json:"schedule"`
	Enabled           int            `json:"enabled"`
	Nodes             []WorkflowNode `json:"nodes"`
	Edges             []WorkflowEdge `json:"edges"`
	ConcurrencyPolicy string         `json:"concurrency_policy"`
	FailurePolicy     string         `json:"failure_policy"`
	CreatedAt         int64          `json:"created_at"`
	UpdatedAt         int64          `json:"updated_at"`
}

type WorkflowNode struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	TaskID         int64  `json:"task_id,omitempty"`
	Type           string `json:"type,omitempty"`
	Command        string `json:"command,omitempty"`
	Enabled        int    `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	RetryCount     int    `json:"retry_count,omitempty"`
	NodeID         string `json:"node_id,omitempty"`
	PositionX      int    `json:"x,omitempty"`
	PositionY      int    `json:"y,omitempty"`
	Config         string `json:"config,omitempty"`
}

type WorkflowEdge struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition"`
}

type WorkflowRun struct {
	ID           int64     `json:"id"`
	WorkflowID   *int64    `json:"workflow_id"`
	WorkflowName string    `json:"workflow_name"`
	TriggerType  string    `json:"trigger_type"`
	Status       string    `json:"status"`
	StartTime    *int64    `json:"start_time"`
	EndTime      *int64    `json:"end_time"`
	Duration     *int64    `json:"duration"`
	Summary      string    `json:"summary"`
	CreatedAt    int64     `json:"created_at"`
	NodeRuns     []NodeRun `json:"node_runs,omitempty"`
	Workflow     *Workflow `json:"workflow,omitempty"`
}

type NodeRun struct {
	ID        int64  `json:"id"`
	RunID     int64  `json:"run_id"`
	NodeID    string `json:"node_id"`
	NodeName  string `json:"node_name"`
	TaskID    *int64 `json:"task_id"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	StartTime *int64 `json:"start_time"`
	EndTime   *int64 `json:"end_time"`
	Duration  *int64 `json:"duration"`
}

type SchedulerNode struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Labels         []string `json:"labels"`
	MaxConcurrency int      `json:"max_concurrency"`
	ActiveRuns     int      `json:"active_runs"`
	LastHeartbeat  *int64   `json:"last_heartbeat,omitempty"`
	CapabilityNote string   `json:"capability_note"`
}

type schedulerTaskPayload struct {
	Name                 *string     `json:"name"`
	Description          *string     `json:"description"`
	Schedule             *string     `json:"schedule"`
	Command              *string     `json:"command"`
	Type                 *string     `json:"type"`
	Enabled              *intOrBool  `json:"enabled"`
	TimeoutSeconds       *int        `json:"timeout_seconds"`
	RetryCount           *int        `json:"retry_count"`
	RetryIntervalSeconds *int        `json:"retry_interval_seconds"`
	MaxConcurrency       *int        `json:"max_concurrency"`
	NodeID               *string     `json:"node_id"`
	NodeSelector         *string     `json:"node_selector"`
	Config               *string     `json:"config"`
}

type workflowPayload struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Schedule          string         `json:"schedule"`
	Enabled           int            `json:"enabled"`
	Nodes             []WorkflowNode `json:"nodes"`
	Edges             []WorkflowEdge `json:"edges"`
	ConcurrencyPolicy string         `json:"concurrency_policy"`
	FailurePolicy     string         `json:"failure_policy"`
}

type schedulerNodePayload struct {
	Name           *string  `json:"name"`
	Labels         []string `json:"labels"`
	MaxConcurrency *int     `json:"max_concurrency"`
	CapabilityNote *string  `json:"capability_note"`
}

// intOrBool 兼容 JSON 数字（0/1）与布尔值的 enabled 字段，
// 使调用方按契约传 boolean 或按实现传 integer 都能正确解析。
type intOrBool int

func (v *intOrBool) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*v = 0
		return nil
	}
	var integer int
	if err := json.Unmarshal(data, &integer); err == nil {
		*v = intOrBool(integer)
		return nil
	}
	var boolean bool
	if err := json.Unmarshal(data, &boolean); err == nil {
		if boolean {
			*v = 1
		} else {
			*v = 0
		}
		return nil
	}
	return fmt.Errorf("enabled 必须是布尔值或整数")
}

func (s *Service) serveSchedulerHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/scheduler")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "tasks":
		switch r.Method {
		case http.MethodGet:
			s.listSchedulerTasks(w, r)
		case http.MethodPost:
			s.createSchedulerTask(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "run":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.runTask(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "tasks":
		switch r.Method {
		case http.MethodGet:
			s.getSchedulerTask(w, r, parts[1])
		case http.MethodPut:
			s.updateSchedulerTask(w, r, parts[1])
		case http.MethodDelete:
			s.deleteTask(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "cron" && parts[1] == "preview":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.previewCron(w, r)
	case len(parts) == 1 && parts[0] == "workflows":
		switch r.Method {
		case http.MethodGet:
			s.listWorkflows(w, r)
		case http.MethodPost:
			s.createWorkflow(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "workflows" && parts[1] == "export":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.exportWorkflows(w, r)
	case len(parts) == 2 && parts[0] == "workflows" && parts[1] == "import":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.importWorkflows(w, r)
	case len(parts) == 3 && parts[0] == "workflows" && parts[2] == "run":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.runWorkflow(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "workflow-runs" && parts[2] == "cancel":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.cancelWorkflowRun(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "workflow-runs" && parts[2] == "retry":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.retryWorkflowRun(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "workflows":
		switch r.Method {
		case http.MethodGet:
			s.getWorkflow(w, r, parts[1])
		case http.MethodPut:
			s.updateWorkflow(w, r, parts[1])
		case http.MethodDelete:
			s.deleteWorkflow(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "runs":
		switch r.Method {
		case http.MethodGet:
			s.listRuns(w, r)
		case http.MethodDelete:
			s.deleteRuns(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "runs":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getRun(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "nodes":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listSchedulerNodes(w, r)
	case len(parts) == 2 && parts[0] == "nodes":
		if r.Method != http.MethodPut {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.updateSchedulerNode(w, r, parts[1])
	default:
		response.Error(w, http.StatusNotFound, "scheduler route not implemented")
	}
}

func (s *Service) listSchedulerTasks(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tasks, err := loadSchedulerTasks(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichTaskRuntime(tasks)
	response.OK(w, tasks)
}

func (s *Service) getSchedulerTask(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid task id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	task, ok, err := findSchedulerTask(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "task not found")
		return
	}
	enriched := []SchedulerTask{task}
	s.enrichTaskRuntime(enriched)
	response.OK(w, enriched[0])
}

func (s *Service) createSchedulerTask(w http.ResponseWriter, r *http.Request) {
	var payload schedulerTaskPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	task, err := buildSchedulerTask(payload, nil)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	created, err := insertSchedulerTask(r.Context(), db, task)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.ReloadTask(context.Background(), created.ID); err != nil {
		// 回滚已落库的任务，避免残留半生效行导致 AI 误判后重复创建。
		_, _ = db.ExecContext(r.Context(), `DELETE FROM cron_tasks WHERE id = ?`, created.ID)
		response.Error(w, http.StatusInternalServerError, "任务创建失败，调度注册失败: "+err.Error())
		return
	}
	enriched := []SchedulerTask{created}
	s.enrichTaskRuntime(enriched)
	response.OK(w, enriched[0])
}

func (s *Service) updateSchedulerTask(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid task id"})
		return
	}
	var payload schedulerTaskPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	existing, ok, err := findSchedulerTask(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "task not found")
		return
	}
	next, err := buildSchedulerTask(payload, &existing)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	next.ID = id
	updated, err := updateSchedulerTaskRow(r.Context(), db, next)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.ReloadTask(context.Background(), id); err != nil {
		// 任务已落库但调度注册失败：写回旧值并尝试恢复旧调度，保持 DB 与运行态一致。
		if _, rollbackErr := updateSchedulerTaskRow(r.Context(), db, existing); rollbackErr != nil {
			response.Error(w, http.StatusInternalServerError, "任务已更新但调度注册失败，旧值恢复也失败: "+err.Error())
			return
		}
		if rollbackErr := s.ReloadTask(context.Background(), id); rollbackErr != nil {
			response.Error(w, http.StatusInternalServerError, "任务已更新但调度注册失败（旧值已写回，调度恢复失败）: "+err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "任务已回滚，调度注册失败: "+err.Error())
		return
	}
	enriched := []SchedulerTask{updated}
	s.enrichTaskRuntime(enriched)
	response.OK(w, enriched[0])
}

func (s *Service) previewCron(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Schedule string `json:"schedule"`
		Count    int    `json:"count"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	count := payload.Count
	if count <= 0 || count > 20 {
		count = 5
	}
	next, summary, err := previewCronSchedule(payload.Schedule, count, s.now())
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.OK(w, map[string]interface{}{"schedule": payload.Schedule, "summary": summary, "next": next})
}

func (s *Service) listWorkflows(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	workflows, err := loadWorkflows(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, workflows)
}

func (s *Service) getWorkflow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid workflow id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	workflow, ok, err := findWorkflow(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	response.OK(w, workflow)
}

func (s *Service) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var payload workflowPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	workflow, err := buildWorkflow(payload, 0)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	created, err := insertWorkflow(r.Context(), db, workflow)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadWorkflow(context.Background(), created.ID)
	response.OK(w, created)
}

func (s *Service) updateWorkflow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid workflow id"})
		return
	}
	var payload workflowPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	workflow, err := buildWorkflow(payload, id)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	updated, err := updateWorkflowRow(r.Context(), db, workflow)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadWorkflow(context.Background(), updated.ID)
	response.OK(w, updated)
}

func (s *Service) deleteWorkflow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid workflow id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(r.Context(), `DELETE FROM scheduler_workflows WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadWorkflow(context.Background(), id)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) exportWorkflows(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	workflows, err := loadWorkflows(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"version":      1,
		"exported_at":  time.Now().UTC().Format(time.RFC3339),
		"workflow_cnt": len(workflows),
		"workflows":    workflows,
	})
}

func (s *Service) importWorkflows(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Workflows []Workflow `json:"workflows"`
		Overwrite bool       `json:"overwrite"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if len(payload.Workflows) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "没有可导入的工作流"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	imported := []Workflow{}
	for _, item := range payload.Workflows {
		raw, _ := json.Marshal(item)
		var body workflowPayload
		if err := json.Unmarshal(raw, &body); err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "工作流格式无效"})
			return
		}
		id := int64(0)
		if payload.Overwrite && item.ID > 0 {
			id = item.ID
		}
		workflow, err := buildWorkflow(body, id)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		var saved Workflow
		if id > 0 {
			if existing, ok, err := findWorkflow(r.Context(), db, id); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			} else if ok {
				workflow.CreatedAt = existing.CreatedAt
				saved, err = updateWorkflowRow(r.Context(), db, workflow)
			} else {
				saved, err = insertWorkflow(r.Context(), db, workflow)
			}
		} else {
			saved, err = insertWorkflow(r.Context(), db, workflow)
		}
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		imported = append(imported, saved)
	}
	_ = s.ReloadAll(context.Background())
	response.OK(w, map[string]interface{}{"imported": len(imported), "workflows": imported})
}

func (s *Service) runWorkflow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid workflow id"})
		return
	}
	run, err := s.executeWorkflow(r.Context(), id, "manual")
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.OK(w, run)
}

func (s *Service) cancelWorkflowRun(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid run id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	now := time.Now().Unix()
	result, err := db.ExecContext(r.Context(), `
		UPDATE scheduler_workflow_runs
		SET status = 'cancelled', end_time = ?, duration = COALESCE(?, 0) - COALESCE(start_time, ?), summary = '用户取消运行'
		WHERE id = ? AND status IN ('queued', 'running')
	`, now, now, now, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "运行不存在或已结束"})
		return
	}
	run, _, err := findRun(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, run)
}

func (s *Service) retryWorkflowRun(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid run id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	run, ok, err := findRun(r.Context(), db, id)
	db.Close()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok || run.WorkflowID == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "运行不存在或未关联工作流"})
		return
	}
	retryRun, err := s.executeWorkflow(r.Context(), *run.WorkflowID, "retry")
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.OK(w, retryRun)
}

func (s *Service) listRuns(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	runs, err := loadRuns(r.Context(), db, 100)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, runs)
}

func (s *Service) getRun(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid run id"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	run, ok, err := findRun(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "run not found")
		return
	}
	response.OK(w, run)
}

func (s *Service) deleteRuns(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if r.URL.Query().Get("all") == "true" {
		_, err = db.ExecContext(r.Context(), `DELETE FROM scheduler_node_runs`)
		if err == nil {
			_, err = db.ExecContext(r.Context(), `DELETE FROM scheduler_workflow_runs`)
		}
	} else {
		days := 30
		if text := strings.TrimSpace(r.URL.Query().Get("days")); text != "" {
			if parsed, parseErr := strconv.Atoi(text); parseErr == nil && parsed > 0 {
				days = parsed
			}
		}
		threshold := time.Now().Unix() - int64(days)*86400
		_, err = db.ExecContext(r.Context(), `DELETE FROM scheduler_node_runs WHERE run_id IN (SELECT id FROM scheduler_workflow_runs WHERE created_at < ?)`, threshold)
		if err == nil {
			_, err = db.ExecContext(r.Context(), `DELETE FROM scheduler_workflow_runs WHERE created_at < ?`, threshold)
		}
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) listSchedulerNodes(w http.ResponseWriter, r *http.Request) {
	nodes := []SchedulerNode{
		{
			ID:             "local",
			Name:           "本机",
			Kind:           "local",
			Status:         "online",
			Labels:         []string{"local", "default"},
			MaxConcurrency: 4,
			ActiveRuns:     0,
			LastHeartbeat:  int64Ref(time.Now().Unix()),
			CapabilityNote: "支持 Shell、HTTP、内部接口任务",
		},
	}
	db, err := s.open(r.Context())
	if err == nil {
		defer db.Close()
		agentNodes, _ := loadAgentNodes(r.Context(), db)
		nodes = append(nodes, agentNodes...)
	}
	overrides, _ := s.loadNodeConfig(r.Context())
	for i := range nodes {
		if override, ok := overrides[nodes[i].ID]; ok {
			applyNodeOverride(&nodes[i], override)
		}
	}
	response.OK(w, nodes)
}

func (s *Service) updateSchedulerNode(w http.ResponseWriter, r *http.Request, id string) {
	var payload schedulerNodePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(id) == "" {
		response.Error(w, http.StatusBadRequest, "invalid node id")
		return
	}
	overrides, err := s.loadNodeConfig(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	node := overrides[id]
	if payload.Name != nil {
		node.Name = strings.TrimSpace(*payload.Name)
	}
	if payload.Labels != nil {
		node.Labels = normalizeStringList(payload.Labels)
	}
	if payload.MaxConcurrency != nil {
		if *payload.MaxConcurrency <= 0 || *payload.MaxConcurrency > 100 {
			response.Error(w, http.StatusBadRequest, "max_concurrency must be 1-100")
			return
		}
		node.MaxConcurrency = *payload.MaxConcurrency
	}
	if payload.CapabilityNote != nil {
		node.CapabilityNote = strings.TrimSpace(*payload.CapabilityNote)
	}
	overrides[id] = node
	if err := s.saveNodeConfig(overrides); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	base := SchedulerNode{ID: id, Name: id, Kind: "agent", Status: "configured", Labels: []string{}, MaxConcurrency: 2, CapabilityNote: "手动配置节点"}
	if id == "local" {
		base.Kind = "local"
		base.Status = "online"
	}
	applyNodeOverride(&base, node)
	response.OK(w, base)
}

func buildSchedulerTask(payload schedulerTaskPayload, existing *SchedulerTask) (SchedulerTask, error) {
	now := time.Now().Unix()
	task := SchedulerTask{
		Task: Task{
			Type:      "shell",
			Enabled:   1,
			CreatedAt: now,
		},
		TimeoutSeconds:       300,
		RetryCount:           0,
		RetryIntervalSeconds: 30,
		MaxConcurrency:       1,
		NodeID:               "local",
	}
	if existing != nil {
		task = *existing
	}
	if payload.Name != nil {
		task.Name = strings.TrimSpace(*payload.Name)
	}
	if payload.Description != nil {
		task.Description = strings.TrimSpace(*payload.Description)
	}
	if payload.Schedule != nil {
		task.Schedule = strings.TrimSpace(*payload.Schedule)
	}
	if payload.Command != nil {
		task.Command = strings.TrimSpace(*payload.Command)
	}
	if payload.Type != nil {
		task.Type = strings.TrimSpace(*payload.Type)
	}
	if payload.Enabled != nil {
		task.Enabled = int(*payload.Enabled)
	}
	if payload.TimeoutSeconds != nil {
		task.TimeoutSeconds = *payload.TimeoutSeconds
	}
	if payload.RetryCount != nil {
		task.RetryCount = *payload.RetryCount
	}
	if payload.RetryIntervalSeconds != nil {
		task.RetryIntervalSeconds = *payload.RetryIntervalSeconds
	}
	if payload.MaxConcurrency != nil {
		task.MaxConcurrency = *payload.MaxConcurrency
	}
	if payload.NodeID != nil {
		task.NodeID = strings.TrimSpace(*payload.NodeID)
	}
	if payload.NodeSelector != nil {
		task.NodeSelector = strings.TrimSpace(*payload.NodeSelector)
	}
	if payload.Config != nil {
		task.Config = strings.TrimSpace(*payload.Config)
	}
	if task.Config != "" && !json.Valid([]byte(task.Config)) {
		return SchedulerTask{}, fmt.Errorf("config 必须是合法 JSON 字符串")
	}
	if task.Name == "" {
		return SchedulerTask{}, fmt.Errorf("任务名称不能为空")
	}
	if task.Command == "" {
		return SchedulerTask{}, fmt.Errorf("执行内容不能为空")
	}
	if err := validateTaskType(task.Type, task.Command); err != nil {
		return SchedulerTask{}, err
	}
	if task.Schedule != "" {
		normalized, err := normalizeCronSchedule(task.Schedule)
		if err != nil {
			return SchedulerTask{}, fmt.Errorf("Cron 表达式无效: %w", err)
		}
		task.Schedule = normalized
	}
	if task.TimeoutSeconds <= 0 || task.TimeoutSeconds > 86400 {
		return SchedulerTask{}, fmt.Errorf("超时时间必须在 1 到 86400 秒之间")
	}
	if task.RetryCount < 0 || task.RetryCount > 10 {
		return SchedulerTask{}, fmt.Errorf("重试次数必须在 0 到 10 之间")
	}
	if task.RetryIntervalSeconds < 0 || task.RetryIntervalSeconds > 86400 {
		return SchedulerTask{}, fmt.Errorf("重试间隔必须在 0 到 86400 秒之间")
	}
	if task.MaxConcurrency <= 0 || task.MaxConcurrency > 50 {
		return SchedulerTask{}, fmt.Errorf("最大并发必须在 1 到 50 之间")
	}
	if task.NodeID == "" {
		task.NodeID = "local"
	}
	return task, nil
}

func validateTaskType(taskType, command string) error {
	switch taskType {
	case "", "shell", "internal", "agent", "ai":
	case "http":
		if _, err := url.ParseRequestURI(command); err != nil || !strings.HasPrefix(command, "http") {
			return fmt.Errorf("HTTP 任务需要有效 URL")
		}
	default:
		return fmt.Errorf("不支持的任务类型: %s", taskType)
	}
	if taskType == "internal" && !strings.HasPrefix(command, "/") && !strings.Contains(command, " /") {
		return fmt.Errorf("内部接口任务需要以 / 开头的路径，或 METHOD /path")
	}
	if taskType == "ai" && strings.TrimSpace(command) == "" {
		return fmt.Errorf("AI 任务需要填写提示词")
	}
	return nil
}

func previewCronSchedule(schedule string, count int, start time.Time) ([]int64, string, error) {
	normalized, err := normalizeCronSchedule(schedule)
	if err != nil {
		return nil, "", fmt.Errorf("Cron 表达式无效: %w", err)
	}
	parsed, err := cron.ParseStandard(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("Cron 表达式无效: %w", err)
	}
	next := make([]int64, 0, count)
	cursor := start
	for i := 0; i < count; i++ {
		cursor = parsed.Next(cursor)
		next = append(next, cursor.Unix())
	}
	return next, cronSummary(normalized), nil
}

func normalizeCronSchedule(schedule string) (string, error) {
	trimmed := strings.TrimSpace(schedule)
	if trimmed == "" {
		return "", nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 6 {
		if fields[0] != "0" {
			return "", fmt.Errorf("带秒的 Cron 仅支持秒段为 0（如 0 0 2 * * *），当前秒段为 %s", fields[0])
		}
		trimmed = strings.Join(fields[1:], " ")
		fields = fields[1:]
	}
	if len(fields) != 5 {
		return "", fmt.Errorf("expected exactly 5 fields, found %d", len(fields))
	}
	if _, err := cron.ParseStandard(trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}

func cronSummary(schedule string) string {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return "自定义 Cron"
	}
	minute, hour, day, month, weekday := parts[0], parts[1], parts[2], parts[3], parts[4]
	switch {
	case minute == "*" && hour == "*" && day == "*" && month == "*" && weekday == "*":
		return "每分钟执行"
	case hour == "*" && day == "*" && month == "*" && weekday == "*":
		return fmt.Sprintf("每小时第 %s 分钟执行", minute)
	case day == "*" && month == "*" && weekday == "*":
		return fmt.Sprintf("每天 %s:%s 执行", padTwo(hour), padTwo(minute))
	case day == "*" && month == "*":
		return fmt.Sprintf("每周 %s %s:%s 执行", weekdayLabel(weekday), padTwo(hour), padTwo(minute))
	case month == "*" && weekday == "*":
		return fmt.Sprintf("每月 %s 日 %s:%s 执行", day, padTwo(hour), padTwo(minute))
	default:
		return "自定义 Cron"
	}
}

func padTwo(value string) string {
	if len(value) == 1 && value[0] >= '0' && value[0] <= '9' {
		return "0" + value
	}
	return value
}

func weekdayLabel(value string) string {
	labels := map[string]string{"0": "周日", "1": "周一", "2": "周二", "3": "周三", "4": "周四", "5": "周五", "6": "周六"}
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

func buildWorkflow(payload workflowPayload, id int64) (Workflow, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return Workflow{}, fmt.Errorf("工作流名称不能为空")
	}
	if len(payload.Nodes) == 0 {
		return Workflow{}, fmt.Errorf("工作流至少需要一个节点")
	}
	if payload.Schedule != "" {
		normalized, err := normalizeCronSchedule(payload.Schedule)
		if err != nil {
			return Workflow{}, fmt.Errorf("Cron 表达式无效: %w", err)
		}
		payload.Schedule = normalized
	}
	workflow := Workflow{
		ID:                id,
		Name:              name,
		Description:       strings.TrimSpace(payload.Description),
		Schedule:          strings.TrimSpace(payload.Schedule),
		Enabled:           payload.Enabled,
		Nodes:             normalizeWorkflowNodes(payload.Nodes),
		Edges:             normalizeWorkflowEdges(payload.Edges),
		ConcurrencyPolicy: firstNonEmpty(payload.ConcurrencyPolicy, "skip"),
		FailurePolicy:     firstNonEmpty(payload.FailurePolicy, "stop"),
		CreatedAt:         time.Now().Unix(),
		UpdatedAt:         time.Now().Unix(),
	}
	if workflow.Enabled != 0 {
		workflow.Enabled = 1
	}
	if err := validateWorkflowDAG(workflow.Nodes, workflow.Edges); err != nil {
		return Workflow{}, err
	}
	return workflow, nil
}

func normalizeWorkflowNodes(nodes []WorkflowNode) []WorkflowNode {
	result := make([]WorkflowNode, 0, len(nodes))
	for i, node := range nodes {
		node.ID = strings.TrimSpace(node.ID)
		if node.ID == "" {
			node.ID = fmt.Sprintf("node-%d", i+1)
		}
		node.Name = firstNonEmpty(strings.TrimSpace(node.Name), node.ID)
		node.Type = strings.TrimSpace(node.Type)
		if node.Type == "" || node.Type == "task" {
			// 内联 command 节点默认按 shell 执行；显式 task 同样归为 shell（与任务引擎合法类型一致）。
			node.Type = "shell"
		}
		if node.Enabled != 0 {
			node.Enabled = 1
		}
		result = append(result, node)
	}
	return result
}

func normalizeWorkflowEdges(edges []WorkflowEdge) []WorkflowEdge {
	result := make([]WorkflowEdge, 0, len(edges))
	for i, edge := range edges {
		edge.From = strings.TrimSpace(edge.From)
		edge.To = strings.TrimSpace(edge.To)
		edge.Condition = firstNonEmpty(strings.TrimSpace(edge.Condition), "success")
		if edge.ID == "" {
			edge.ID = fmt.Sprintf("edge-%d", i+1)
		}
		result = append(result, edge)
	}
	return result
}

func validateWorkflowDAG(nodes []WorkflowNode, edges []WorkflowEdge) error {
	nodeIDs := map[string]bool{}
	for _, node := range nodes {
		if node.ID == "" {
			return fmt.Errorf("工作流节点 ID 不能为空")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("工作流节点 ID 重复: %s", node.ID)
		}
		nodeIDs[node.ID] = true
		if node.TaskID == 0 && strings.TrimSpace(node.Command) == "" && node.Type != "start" && node.Type != "end" {
			return fmt.Errorf("节点 %s 需要引用任务或填写内联执行内容", node.Name)
		}
	}
	graph := map[string][]string{}
	indegree := map[string]int{}
	for id := range nodeIDs {
		indegree[id] = 0
	}
	for _, edge := range edges {
		if !nodeIDs[edge.From] || !nodeIDs[edge.To] {
			return fmt.Errorf("依赖边引用了不存在的节点")
		}
		if edge.From == edge.To {
			return fmt.Errorf("节点不能依赖自身: %s", edge.From)
		}
		graph[edge.From] = append(graph[edge.From], edge.To)
		indegree[edge.To]++
	}
	queue := []string{}
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range graph[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(nodes) {
		return fmt.Errorf("工作流不能包含环形依赖")
	}
	return nil
}

func (s *Service) executeWorkflow(ctx context.Context, workflowID int64, triggerType string) (WorkflowRun, error) {
	db, err := s.open(ctx)
	if err != nil {
		return WorkflowRun{}, err
	}
	defer db.Close()
	workflow, ok, err := findWorkflow(ctx, db, workflowID)
	if err != nil {
		return WorkflowRun{}, err
	}
	if !ok {
		return WorkflowRun{}, fmt.Errorf("工作流不存在")
	}
	start := time.Now().Unix()
	runID, err := createWorkflowRun(ctx, db, workflow, triggerType, start)
	if err != nil {
		return WorkflowRun{}, err
	}

	statuses := map[string]string{}
	outputs := map[string]string{}
	for _, node := range topologicalNodes(workflow.Nodes, workflow.Edges) {
		if node.Enabled == 0 {
			statuses[node.ID] = "skipped"
			_ = insertNodeRun(ctx, db, runID, node, "skipped", "节点已停用", start, start)
			continue
		}
		if !dependenciesSatisfied(node.ID, workflow.Edges, statuses) {
			statuses[node.ID] = "skipped"
			_ = insertNodeRun(ctx, db, runID, node, "skipped", "依赖条件未满足", start, start)
			continue
		}
		nodeStart := time.Now().Unix()
		status := "success"
		output, execErr := s.executeWorkflowNode(ctx, db, node)
		if execErr != nil {
			status = "failed"
			output = execErr.Error()
		}
		output = truncateOutput(output)
		nodeEnd := time.Now().Unix()
		_ = insertNodeRun(ctx, db, runID, node, status, output, nodeStart, nodeEnd)
		statuses[node.ID] = status
		outputs[node.ID] = output
		if status == "failed" && workflow.FailurePolicy == "stop" {
			break
		}
	}

	finalStatus := "success"
	for _, status := range statuses {
		if status == "failed" {
			finalStatus = "failed"
			break
		}
	}
	end := time.Now().Unix()
	summary := workflowRunSummary(statuses, outputs)
	if _, err := db.ExecContext(context.Background(), `
		UPDATE scheduler_workflow_runs
		SET status = ?, end_time = ?, duration = ?, summary = ?
		WHERE id = ?
	`, finalStatus, end, end-start, summary, runID); err != nil {
		return WorkflowRun{}, err
	}
	run, _, err := findRun(context.Background(), db, runID)
	s.notifyWorkflowResult(ctx, workflow, finalStatus, summary, end-start, triggerType)
	return run, err
}

// notifyWorkflowResult 工作流执行完成后触发通知中心事件（cron 源，workflow.completed / failed）。
func (s *Service) notifyWorkflowResult(ctx context.Context, workflow Workflow, status, summary string, duration int64, triggerType string) {
	if s.notifier == nil {
		return
	}
	eventType := "workflow.completed"
	if status != "success" {
		eventType = "workflow.failed"
	}
	payload := map[string]interface{}{
		"workflowId":   workflow.ID,
		"workflowName": workflow.Name,
		"status":       status,
		"summary":      summary,
		"duration":     duration,
		"triggerType":  triggerType,
		"eventType":    "cron." + eventType,
	}
	_ = s.notifier.Trigger(ctx, "cron", eventType, payload)
}

func (s *Service) executeWorkflowNode(ctx context.Context, db *sql.DB, node WorkflowNode) (string, error) {
	// start / end 是 DAG 控制标记节点，无实际任务语义，直接视为成功。
	if node.Type == "start" || node.Type == "end" {
		return "", nil
	}
	if node.TaskID != 0 {
		task, ok, err := findTask(ctx, db, node.TaskID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("节点引用的任务不存在")
		}
		return s.executeTaskCommand(ctx, task)
	}
	task := Task{
		Name:    node.Name,
		Type:    firstNonEmpty(node.Type, "shell"),
		Command: node.Command,
		Enabled: 1,
		Config:  node.Config,
	}
	return s.executeTaskCommand(ctx, task)
}

func topologicalNodes(nodes []WorkflowNode, edges []WorkflowEdge) []WorkflowNode {
	byID := map[string]WorkflowNode{}
	indegree := map[string]int{}
	graph := map[string][]string{}
	for _, node := range nodes {
		byID[node.ID] = node
		indegree[node.ID] = 0
	}
	for _, edge := range edges {
		graph[edge.From] = append(graph[edge.From], edge.To)
		indegree[edge.To]++
	}
	queue := []string{}
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)
	result := []WorkflowNode{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, byID[id])
		nextIDs := graph[id]
		sort.Strings(nextIDs)
		for _, next := range nextIDs {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}
	return result
}

func dependenciesSatisfied(nodeID string, edges []WorkflowEdge, statuses map[string]string) bool {
	incoming := 0
	for _, edge := range edges {
		if edge.To != nodeID {
			continue
		}
		incoming++
		status := statuses[edge.From]
		switch edge.Condition {
		case "failed":
			if status != "failed" {
				return false
			}
		case "complete":
			if status == "" || status == "skipped" {
				return false
			}
		default:
			if status != "success" {
				return false
			}
		}
	}
	return incoming == 0 || true
}

func workflowRunSummary(statuses map[string]string, outputs map[string]string) string {
	counts := map[string]int{}
	for _, status := range statuses {
		counts[status]++
	}
	return fmt.Sprintf("成功 %d，失败 %d，跳过 %d", counts["success"], counts["failed"], counts["skipped"])
}

func (s *Service) enrichTaskRuntime(tasks []SchedulerTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range tasks {
		if tasks[i].Schedule != "" {
			tasks[i].ScheduleSummary = cronSummary(tasks[i].Schedule)
		} else {
			tasks[i].ScheduleSummary = "手动触发"
		}
		if entryID, ok := s.entries[tasks[i].ID]; ok {
			entry := s.scheduler.Entry(entryID)
			if !entry.Next.IsZero() {
				next := entry.Next.Unix()
				tasks[i].NextRun = &next
			}
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func int64Ref(value int64) *int64 {
	return &value
}
