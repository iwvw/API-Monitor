package cronjobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
	"github.com/robfig/cron/v3"
)

const (
	httpTaskTimeout  = 30 * time.Second
	shellTaskTimeout = 5 * time.Minute
	internalTimeout  = time.Minute
	aiTaskTimeout    = 5 * time.Minute
	maxLogOutput     = 5000
)

type Service struct {
	cfg             config.Config
	store           *database.Store
	schema          database.SchemaEnsurer
	scheduler       *cron.Cron
	entries         map[int64]cron.EntryID
	workflowEntries map[int64]cron.EntryID
	activeRuns      map[int64]int
	mu              sync.Mutex
	client          *http.Client
	agentRunner     AgentRunner
	notifier        Notifier
}

// Notifier 是通知中心的最小接口，供任务/工作流执行结果事件通知。
type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

func (s *Service) SetNotifier(notifier Notifier) {
	s.notifier = notifier
}

type AgentRunner interface {
	RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error)
}

type Task struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Command   string `json:"command"`
	Type      string `json:"type"`
	Enabled   int    `json:"enabled"`
	LastRun   *int64 `json:"last_run"`
	NextRun   *int64 `json:"next_run"`
	CreatedAt int64  `json:"created_at"`
	Config    string `json:"config"`
}

type Log struct {
	ID        int64   `json:"id"`
	TaskID    int64   `json:"task_id"`
	TaskName  *string `json:"task_name,omitempty"`
	Status    string  `json:"status"`
	Output    string  `json:"output"`
	StartTime int64   `json:"start_time"`
	EndTime   *int64  `json:"end_time"`
	Duration  *int64  `json:"duration"`
}

type taskPayload struct {
	Name     *string    `json:"name"`
	Schedule *string    `json:"schedule"`
	Command  *string    `json:"command"`
	Type     *string    `json:"type"`
	Enabled  *intOrBool `json:"enabled"`
	LastRun  *int64     `json:"last_run"`
	NextRun  *int64     `json:"next_run"`
	Config   *string    `json:"config"`
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:             cfg,
		store:           database.New(cfg),
		entries:         map[int64]cron.EntryID{},
		workflowEntries: map[int64]cron.EntryID{},
		activeRuns:      map[int64]int{},
		client:          &http.Client{},
	}
	service.scheduler = cron.New(cron.WithLocation(service.settingsLocation()))
	service.scheduler.Start()
	service.startTZWatcher()
	_ = service.ReloadAll(context.Background())
	return service
}

// settingsLocation 读取用户设置里的系统时区作为调度器时区，
// 保证定时任务的执行时刻与站点设置一致（服务器部署海外时不再按服务器本地时间跑）。
func (s *Service) settingsLocation() *time.Location {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		return time.Local
	}
	defer db.Close()
	return timeutil.LocationFromSettings(ctx, db)
}

// now 返回当前时间按设置时区的视角（与调度器同源），供预览/汇总等计算使用。
func (s *Service) now() time.Time {
	loc := s.schedulerLocation()
	return time.Now().In(loc)
}

// schedulerLocation 优先取调度器已配置的时区；未初始化时回退服务器本地。
func (s *Service) schedulerLocation() *time.Location {
	if s == nil || s.scheduler == nil {
		return time.Local
	}
	return s.scheduler.Location()
}

// rebuildIfLocationChangedLocked 在持 s.mu 前提下检测站点时区变化并重建调度器。
// 时区设置没有跨包变更回调，ReloadAll 与 startTZWatcher 都经由这里兜底。
func (s *Service) rebuildIfLocationChangedLocked(loc *time.Location) {
	if loc == nil || loc == s.scheduler.Location() {
		return
	}
	stopCtx := s.scheduler.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(2 * time.Second):
	}
	s.scheduler = cron.New(cron.WithLocation(loc))
	s.scheduler.Start()
}

// startTZWatcher 每分钟核对一次站点时区；检测到变化时通过 ReloadAll 重建调度器，
// 重新挂载全部任务/workflow（不能跳过 ReloadAll 只重建调度器，否则新调度器
// 为空、存量 entry 全部静默消失）。
func (s *Service) startTZWatcher() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			db, err := s.store.Open(ctx)
			if err != nil {
				cancel()
				continue
			}
			loc := timeutil.LocationFromSettings(ctx, db)
			db.Close()
			cancel()
			if loc == nil {
				continue
			}
			s.mu.Lock()
			changed := loc != s.scheduler.Location()
			s.mu.Unlock()
			if !changed {
				continue
			}
			if err := s.ReloadAll(context.Background()); err != nil {
				slog.Warn("cron-tz-watcher-reload-failed", "err", err.Error())
			}
		}
	}()
}

func (s *Service) SetAgentRunner(runner AgentRunner) {
	s.agentRunner = runner
}

func (s *Service) Stop() context.Context {
	return s.scheduler.Stop()
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/scheduler") {
		s.serveSchedulerHTTP(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/cron")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "tasks":
		switch r.Method {
		case http.MethodGet:
			s.listTasks(w, r)
		case http.MethodPost:
			s.createTask(w, r)
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
		case http.MethodPut:
			s.updateTask(w, r, parts[1])
		case http.MethodDelete:
			s.deleteTask(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "logs":
		switch r.Method {
		case http.MethodGet:
			s.listLogs(w, r)
		case http.MethodDelete:
			s.deleteLogs(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		response.Error(w, http.StatusNotFound, "cron route not implemented")
	}
}

func (s *Service) ReloadAll(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	tasks, err := loadTasks(ctx, db)
	if err != nil {
		return err
	}
	workflows, err := loadWorkflows(ctx, db)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 站点时区可能运行中被修改：与调度器当前时区不一致时重建调度器，
	// 否则存量 entry 会一直按旧时区触发到进程重启（预览/下次运行时间同源错位）。
	s.rebuildIfLocationChangedLocked(timeutil.LocationFromSettings(ctx, db))
	for _, entryID := range s.entries {
		s.scheduler.Remove(entryID)
	}
	for _, entryID := range s.workflowEntries {
		s.scheduler.Remove(entryID)
	}
	s.entries = map[int64]cron.EntryID{}
	s.workflowEntries = map[int64]cron.EntryID{}
	for _, task := range tasks {
		if task.Enabled != 0 {
			_ = s.scheduleTaskLocked(task)
		}
	}
	for _, workflow := range workflows {
		if workflow.Enabled != 0 && strings.TrimSpace(workflow.Schedule) != "" {
			_ = s.scheduleWorkflowLocked(workflow)
		}
	}
	return nil
}

func (s *Service) ReloadTask(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	task, ok, err := findTask(ctx, db, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if entryID, ok := s.entries[id]; ok {
		s.scheduler.Remove(entryID)
		delete(s.entries, id)
	}
	if ok && task.Enabled != 0 {
		return s.scheduleTaskLocked(task)
	}
	return nil
}

func (s *Service) scheduleTaskLocked(task Task) error {
	normalized, err := normalizeCronSchedule(task.Schedule)
	if err != nil {
		return err
	}
	if normalized == "" {
		return fmt.Errorf("empty cron schedule")
	}
	entryID, err := s.scheduler.AddFunc(normalized, func() {
		go s.ExecuteTask(context.Background(), task.ID)
	})
	if err != nil {
		return err
	}
	s.entries[task.ID] = entryID
	return nil
}

func (s *Service) ReloadWorkflow(ctx context.Context, id int64) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	workflow, ok, err := findWorkflow(ctx, db, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entryID, ok := s.workflowEntries[id]; ok {
		s.scheduler.Remove(entryID)
		delete(s.workflowEntries, id)
	}
	if ok && workflow.Enabled != 0 && strings.TrimSpace(workflow.Schedule) != "" {
		return s.scheduleWorkflowLocked(workflow)
	}
	return nil
}

func (s *Service) scheduleWorkflowLocked(workflow Workflow) error {
	normalized, err := normalizeCronSchedule(workflow.Schedule)
	if err != nil {
		return err
	}
	entryID, err := s.scheduler.AddFunc(normalized, func() {
		go func(id int64) {
			_, _ = s.executeWorkflow(context.Background(), id, "cron")
		}(workflow.ID)
	})
	if err != nil {
		return err
	}
	s.workflowEntries[workflow.ID] = entryID
	return nil
}

func (s *Service) listTasks(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tasks, err := loadTasks(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, tasks)
}

func (s *Service) createTask(w http.ResponseWriter, r *http.Request) {
	var payload taskPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	task := Task{
		Name:      stringValue(payload.Name, ""),
		Schedule:  stringValue(payload.Schedule, ""),
		Command:   stringValue(payload.Command, ""),
		Type:      stringValue(payload.Type, "shell"),
		Enabled:   intOrBoolValue(payload.Enabled, 1),
		CreatedAt: time.Now().Unix(),
	}
	if payload.Config != nil {
		task.Config = strings.TrimSpace(*payload.Config)
	}
	if task.Config != "" && !json.Valid([]byte(task.Config)) {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "config 必须是合法 JSON 字符串"})
		return
	}
	if strings.TrimSpace(task.Name) == "" || strings.TrimSpace(task.Schedule) == "" || strings.TrimSpace(task.Command) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "name, schedule, and command are required"})
		return
	}
	normalized, err := normalizeCronSchedule(task.Schedule)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid cron schedule")
		return
	}
	task.Schedule = normalized

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	created, err := insertTask(r.Context(), db, task)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadTask(context.Background(), created.ID)
	response.OK(w, created)
}

func (s *Service) updateTask(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid task id"})
		return
	}
	var payload taskPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.Schedule != nil {
		normalized, err := normalizeCronSchedule(*payload.Schedule)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		payload.Schedule = &normalized
	}
	if payload.Config != nil {
		if cfg := strings.TrimSpace(*payload.Config); cfg != "" && !json.Valid([]byte(cfg)) {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "config 必须是合法 JSON 字符串"})
			return
		}
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	task, err := updateTaskFields(r.Context(), db, id, payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadTask(context.Background(), id)
	response.OK(w, task)
}

func (s *Service) deleteTask(w http.ResponseWriter, r *http.Request, idText string) {
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
	if _, err := db.ExecContext(r.Context(), `DELETE FROM cron_tasks WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.ReloadTask(context.Background(), id)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) runTask(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid task id"})
		return
	}
	go s.ExecuteTask(context.Background(), id)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Task execution started"})
}

func (s *Service) listLogs(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	var logs []Log
	if taskID := strings.TrimSpace(r.URL.Query().Get("task_id")); taskID != "" {
		id, err := strconv.ParseInt(taskID, 10, 64)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid task_id"})
			return
		}
		logs, err = loadLogsByTask(r.Context(), db, id, 50)
	} else {
		logs, err = loadAllLogs(r.Context(), db, 100)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, logs)
}

func (s *Service) deleteLogs(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	if r.URL.Query().Get("all") == "true" {
		_, err = db.ExecContext(r.Context(), `DELETE FROM cron_logs`)
	} else {
		days := 7
		if text := strings.TrimSpace(r.URL.Query().Get("days")); text != "" {
			if parsed, parseErr := strconv.Atoi(text); parseErr == nil {
				days = parsed
			}
		}
		threshold := time.Now().Unix() - int64(days)*86400
		_, err = db.ExecContext(r.Context(), `DELETE FROM cron_logs WHERE start_time < ?`, threshold)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) ExecuteTask(ctx context.Context, taskID int64) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	task, ok, err := findSchedulerTask(ctx, db, taskID)
	if err != nil || !ok {
		db.Close()
		return
	}
	startTime := time.Now().Unix()
	if !s.acquireTaskSlot(task.ID, task.MaxConcurrency) {
		_, _ = createCompletedLog(ctx, db, task.ID, "skipped", "任务达到最大并发限制，已跳过本次运行", startTime, startTime)
		db.Close()
		return
	}
	defer s.releaseTaskSlot(task.ID)

	logID, err := createLog(ctx, db, task.ID, "running", "", startTime)
	if err != nil {
		db.Close()
		return
	}
	db.Close()

	status := "success"
	output, err := s.executeSchedulerTaskCommand(ctx, task)
	if err != nil {
		status = "failed"
		output = err.Error()
	}
	output = truncateOutput(output)
	endTime := time.Now().Unix()
	duration := endTime - startTime

	db, err = s.open(context.Background())
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(context.Background(), `
		UPDATE cron_logs
		SET status = ?, output = ?, end_time = ?, duration = ?
		WHERE id = ?
	`, status, output, endTime, duration, logID)
	_, _ = db.ExecContext(context.Background(), `UPDATE cron_tasks SET last_run = ? WHERE id = ?`, endTime, task.ID)
	s.notifyTaskResult(ctx, task, status, output, duration)
}

// notifyTaskResult 任务执行完成后触发通知中心事件（cron 源，task.completed / task.failed）。
func (s *Service) notifyTaskResult(ctx context.Context, task SchedulerTask, status, output string, duration int64) {
	if s.notifier == nil {
		return
	}
	eventType := "task.completed"
	if status != "success" {
		eventType = "task.failed"
	}
	payload := map[string]interface{}{
		"taskId":   task.ID,
		"taskName": task.Name,
		"status":   status,
		"output":   truncateOutput(output),
		"duration": duration,
		"eventType": "cron." + eventType,
	}
	_ = s.notifier.Trigger(ctx, "cron", eventType, payload)
}

func (s *Service) executeTaskCommand(ctx context.Context, task Task) (string, error) {
	return s.executeSchedulerTaskCommand(ctx, SchedulerTask{
		Task:                 task,
		TimeoutSeconds:       int(shellTaskTimeout / time.Second),
		RetryCount:           0,
		RetryIntervalSeconds: 30,
		MaxConcurrency:       1,
		NodeID:               "local",
	})
}

func (s *Service) executeSchedulerTaskCommand(ctx context.Context, task SchedulerTask) (string, error) {
	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = shellTaskTimeout
	}
	attempts := task.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	var history []string
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		output, err := s.executeSchedulerTaskAttempt(ctx, task, timeout)
		if err == nil {
			if attempt > 1 {
				history = append(history, fmt.Sprintf("第 %d 次尝试成功:\n%s", attempt, output))
				return strings.Join(history, "\n\n"), nil
			}
			return output, nil
		}
		lastErr = err
		history = append(history, fmt.Sprintf("第 %d 次尝试失败:\n%s", attempt, err.Error()))
		// AI 任务单次执行即可能已产生非幂等副作用（独立会话、工具调用写操作、频道推送），
		// 重试会整体重放这些副作用造成重复执行，因此失败后不再整体重试，
		// 由下一次调度（或人工重跑）重新执行。
		if task.Type == "ai" {
			break
		}
		if attempt < attempts && task.RetryIntervalSeconds > 0 {
			timer := time.NewTimer(time.Duration(task.RetryIntervalSeconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
	}
	if len(history) > 0 {
		return "", errors.New(strings.Join(history, "\n\n"))
	}
	return "", lastErr
}

func (s *Service) executeSchedulerTaskAttempt(ctx context.Context, task SchedulerTask, timeout time.Duration) (string, error) {
	switch task.Type {
	case "http":
		return s.executeHTTPTask(ctx, task.Command, timeout)
	case "internal":
		return s.executeInternalTask(ctx, task.Command, timeout)
	case "agent":
		return s.executeAgentTask(ctx, task, timeout)
	case "ai":
		return s.executeAITask(ctx, task, timeout)
	case "shell":
		if !s.cfg.LocalShellTasksAllowed() {
			return "", fmt.Errorf("本地 Shell 任务已在当前环境禁用，请显式设置 ALLOW_LOCAL_SHELL_TASKS=true")
		}
		return executeShellTask(ctx, task.Command, timeout)
	default:
		return "", fmt.Errorf("不支持的任务类型: %s", task.Type)
	}
}

func (s *Service) executeAgentTask(ctx context.Context, task SchedulerTask, timeout time.Duration) (string, error) {
	if s.agentRunner == nil {
		return "", fmt.Errorf("Agent 执行器未初始化")
	}
	nodeID := task.NodeID
	if nodeID == "" || nodeID == "local" {
		return "", fmt.Errorf("Agent 任务需要选择在线 Agent 节点")
	}
	return s.agentRunner.RunCommandTaskAndWait(nodeID, task.Command, timeout)
}

// executeAITask 定时 AI 任务：经 X-Internal-Cron 调用管理 AI 内部接口，无头执行提示词
// （默认策略「完全允许」写操作，仍受管理 AI 全局写开关约束），返回最终回复文本。
func (s *Service) executeAITask(ctx context.Context, task SchedulerTask, timeout time.Duration) (string, error) {
	cfg := map[string]interface{}{}
	if strings.TrimSpace(task.Config) != "" {
		_ = json.Unmarshal([]byte(task.Config), &cfg)
	}
	model, _ := cfg["model"].(string)
	policy, _ := cfg["policy"].(string)
	channelID, _ := cfg["channelId"].(string)
	body, err := json.Marshal(map[string]interface{}{
		"prompt":    task.Command,
		"title":     task.Name, // 会话标题按任务名命名，便于前端/审计区分机器人会话
		"model":     model,
		"policy":    policy, // 留空由接口回退默认 allow
		"channelId": channelID,
	})
	if err != nil {
		return "", err
	}
	target := "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port) + "/api/admin-ai/cron/task-run"
	if timeout <= 0 {
		timeout = aiTaskTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Internal-Cron", "true")
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxLogOutput))

	var payload struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && (payload.Output != "" || payload.Error != "") {
		if res.StatusCode >= 400 || payload.Error != "" {
			if payload.Error != "" {
				return "", fmt.Errorf("AI 任务失败: %s", payload.Error)
			}
			return "", fmt.Errorf("AI 任务返回 HTTP %d", res.StatusCode)
		}
		return payload.Output, nil
	}
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("AI 任务返回 HTTP %d: %s", res.StatusCode, formatBody(raw))
	}
	return formatBody(raw), nil
}

func (s *Service) executeHTTPTask(ctx context.Context, target string, timeout time.Duration) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxLogOutput))
	return fmt.Sprintf("Status: %d\nData: %s", res.StatusCode, formatBody(body)), nil
}

func (s *Service) executeInternalTask(ctx context.Context, command string, timeout time.Duration) (string, error) {
	parts := strings.Fields(strings.TrimSpace(command))
	method := http.MethodGet
	path := ""
	if len(parts) > 1 {
		method = strings.ToUpper(parts[0])
		path = parts[1]
	} else if len(parts) == 1 {
		path = parts[0]
	}
	if path == "" {
		return "", fmt.Errorf("empty internal command")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port) + path
	if timeout <= 0 {
		timeout = internalTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Internal-Cron", "true")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxLogOutput))
	return fmt.Sprintf("Status: %d\nData: %s", res.StatusCode, formatBody(body)), nil
}

func executeShellTask(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = shellTaskTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(reqCtx, command)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Error: %s\nStdout: %s\nStderr: %s", err.Error(), stdout.String(), stderr.String())
	}
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nStderr: " + stderr.String()
	}
	if strings.TrimSpace(output) == "" {
		output = "(no output)"
	}
	return output, nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return exec.CommandContext(ctx, comspec, "/C", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cron_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			schedule TEXT NOT NULL,
			command TEXT NOT NULL,
			type TEXT DEFAULT 'shell',
			enabled INTEGER DEFAULT 1,
			last_run INTEGER,
			next_run INTEGER,
			created_at INTEGER DEFAULT (strftime('%s', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS cron_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER,
			status TEXT,
			output TEXT,
			start_time INTEGER,
			end_time INTEGER,
			duration INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_workflows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			schedule TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			nodes TEXT NOT NULL DEFAULT '[]',
			edges TEXT NOT NULL DEFAULT '[]',
			concurrency_policy TEXT DEFAULT 'skip',
			failure_policy TEXT DEFAULT 'stop',
			created_at INTEGER DEFAULT (strftime('%s', 'now')),
			updated_at INTEGER DEFAULT (strftime('%s', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_workflow_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workflow_id INTEGER,
			workflow_name TEXT,
			trigger_type TEXT DEFAULT 'manual',
			status TEXT DEFAULT 'queued',
			start_time INTEGER,
			end_time INTEGER,
			duration INTEGER,
			summary TEXT DEFAULT '',
			created_at INTEGER DEFAULT (strftime('%s', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS scheduler_node_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL,
			node_id TEXT NOT NULL,
			node_name TEXT,
			task_id INTEGER,
			status TEXT DEFAULT 'queued',
			output TEXT DEFAULT '',
			start_time INTEGER,
			end_time INTEGER,
			duration INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_workflow_runs_workflow ON scheduler_workflow_runs(workflow_id, start_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_node_runs_run ON scheduler_node_runs(run_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure cron schema: %w", err)
		}
	}
	return ensureCronTaskColumns(ctx, db)
}

func (s *Service) acquireTaskSlot(taskID int64, maxConcurrency int) bool {
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeRuns[taskID] >= maxConcurrency {
		return false
	}
	s.activeRuns[taskID]++
	return true
}

func (s *Service) releaseTaskSlot(taskID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeRuns[taskID] <= 1 {
		delete(s.activeRuns, taskID)
		return
	}
	s.activeRuns[taskID]--
}

func ensureCronTaskColumns(ctx context.Context, db *sql.DB) error {
	columns := []struct {
		name string
		sql  string
	}{
		{"description", "ALTER TABLE cron_tasks ADD COLUMN description TEXT DEFAULT ''"},
		{"timeout_seconds", "ALTER TABLE cron_tasks ADD COLUMN timeout_seconds INTEGER DEFAULT 300"},
		{"retry_count", "ALTER TABLE cron_tasks ADD COLUMN retry_count INTEGER DEFAULT 0"},
		{"retry_interval_seconds", "ALTER TABLE cron_tasks ADD COLUMN retry_interval_seconds INTEGER DEFAULT 30"},
		{"max_concurrency", "ALTER TABLE cron_tasks ADD COLUMN max_concurrency INTEGER DEFAULT 1"},
		{"node_id", "ALTER TABLE cron_tasks ADD COLUMN node_id TEXT DEFAULT 'local'"},
		{"node_selector", "ALTER TABLE cron_tasks ADD COLUMN node_selector TEXT DEFAULT ''"},
		{"config", "ALTER TABLE cron_tasks ADD COLUMN config TEXT DEFAULT ''"},
	}
	for _, column := range columns {
		exists, err := hasColumn(ctx, db, "cron_tasks", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add cron_tasks.%s: %w", column.name, err)
			}
		}
	}
	return nil
}

func hasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan %s columns: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func loadTasks(ctx context.Context, db *sql.DB) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, schedule, command, type, enabled, last_run, next_run, created_at, config
		FROM cron_tasks
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("load cron tasks: %w", err)
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func findTask(ctx context.Context, db *sql.DB, id int64) (Task, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, schedule, command, type, enabled, last_run, next_run, created_at, config
		FROM cron_tasks
		WHERE id = ?
	`, id)
	task, err := scanTask(row)
	if err == nil {
		return task, true, nil
	}
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	return Task{}, false, err
}

type taskScanner interface {
	Scan(dest ...interface{}) error
}

func scanTask(scanner taskScanner) (Task, error) {
	var task Task
	var taskType sql.NullString
	var enabled sql.NullInt64
	var lastRun, nextRun sql.NullInt64
	var config sql.NullString
	err := scanner.Scan(&task.ID, &task.Name, &task.Schedule, &task.Command, &taskType, &enabled, &lastRun, &nextRun, &task.CreatedAt, &config)
	if err != nil {
		return Task{}, err
	}
	task.Type = stringOrDefault(taskType, "shell")
	task.Enabled = int(int64OrDefault(enabled, 1))
	task.LastRun = int64Ptr(lastRun)
	task.NextRun = int64Ptr(nextRun)
	task.Config = stringOrDefault(config, "")
	return task, nil
}

func insertTask(ctx context.Context, db *sql.DB, task Task) (Task, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO cron_tasks (name, schedule, command, type, enabled, created_at, config)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, task.Name, task.Schedule, task.Command, task.Type, task.Enabled, task.CreatedAt, task.Config)
	if err != nil {
		return Task{}, fmt.Errorf("create cron task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Task{}, err
	}
	created, _, err := findTask(ctx, db, id)
	return created, err
}

func updateTaskFields(ctx context.Context, db *sql.DB, id int64, payload taskPayload) (Task, error) {
	sets := []string{}
	args := []interface{}{}
	if payload.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *payload.Name)
	}
	if payload.Schedule != nil {
		sets = append(sets, "schedule = ?")
		args = append(args, *payload.Schedule)
	}
	if payload.Command != nil {
		sets = append(sets, "command = ?")
		args = append(args, *payload.Command)
	}
	if payload.Type != nil {
		sets = append(sets, "type = ?")
		args = append(args, *payload.Type)
	}
	if payload.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, int(*payload.Enabled))
	}
	if payload.Config != nil {
		sets = append(sets, "config = ?")
		args = append(args, strings.TrimSpace(*payload.Config))
	}
	if payload.LastRun != nil {
		sets = append(sets, "last_run = ?")
		args = append(args, *payload.LastRun)
	}
	if payload.NextRun != nil {
		sets = append(sets, "next_run = ?")
		args = append(args, *payload.NextRun)
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := db.ExecContext(ctx, `UPDATE cron_tasks SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
			return Task{}, fmt.Errorf("update cron task: %w", err)
		}
	}
	task, _, err := findTask(ctx, db, id)
	return task, err
}

func createLog(ctx context.Context, db *sql.DB, taskID int64, status, output string, startTime int64) (int64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO cron_logs (task_id, status, output, start_time)
		VALUES (?, ?, ?, ?)
	`, taskID, status, output, startTime)
	if err != nil {
		return 0, fmt.Errorf("create cron log: %w", err)
	}
	return result.LastInsertId()
}

func createCompletedLog(ctx context.Context, db *sql.DB, taskID int64, status, output string, startTime, endTime int64) (int64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO cron_logs (task_id, status, output, start_time, end_time, duration)
		VALUES (?, ?, ?, ?, ?, ?)
	`, taskID, status, output, startTime, endTime, endTime-startTime)
	if err != nil {
		return 0, fmt.Errorf("create completed cron log: %w", err)
	}
	return result.LastInsertId()
}

func loadAllLogs(ctx context.Context, db *sql.DB, limit int) ([]Log, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT l.id, l.task_id, t.name, l.status, l.output, l.start_time, l.end_time, l.duration
		FROM cron_logs l
		LEFT JOIN cron_tasks t ON l.task_id = t.id
		ORDER BY l.start_time DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("load cron logs: %w", err)
	}
	defer rows.Close()
	return scanLogs(rows)
}

func loadLogsByTask(ctx context.Context, db *sql.DB, taskID int64, limit int) ([]Log, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, task_id, NULL, status, output, start_time, end_time, duration
		FROM cron_logs
		WHERE task_id = ?
		ORDER BY start_time DESC
		LIMIT ?
	`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("load cron logs for task: %w", err)
	}
	defer rows.Close()
	return scanLogs(rows)
}

type logRows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}

func scanLogs(rows logRows) ([]Log, error) {
	logs := []Log{}
	for rows.Next() {
		var log Log
		var taskName sql.NullString
		var output sql.NullString
		var endTime, duration sql.NullInt64
		if err := rows.Scan(&log.ID, &log.TaskID, &taskName, &log.Status, &output, &log.StartTime, &endTime, &duration); err != nil {
			return nil, err
		}
		log.TaskName = stringPtr(taskName)
		log.Output = stringOrDefault(output, "")
		log.EndTime = int64Ptr(endTime)
		log.Duration = int64Ptr(duration)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		return false
	}
	return true
}

func truncateOutput(value string) string {
	if len(value) <= maxLogOutput {
		return value
	}
	return value[:maxLogOutput]
}

func formatBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "(no output)"
	}
	var parsed interface{}
	if json.Unmarshal(body, &parsed) == nil {
		encoded, err := json.MarshalIndent(parsed, "", "  ")
		if err == nil {
			return string(encoded)
		}
	}
	return text
}

func stringValue(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func intOrBoolValue(value *intOrBool, fallback int) int {
	if value == nil {
		return fallback
	}
	return int(*value)
}

func int64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrDefault(value sql.NullString, fallback string) string {
	if value.Valid {
		return value.String
	}
	return fallback
}

func (s *Service) nodeConfigPath() string {
	return filepath.Join(s.cfg.DataDir, "scheduler", "nodes.json")
}

func (s *Service) loadNodeConfig(ctx context.Context) (map[string]SchedulerNode, error) {
	data, err := os.ReadFile(s.nodeConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SchedulerNode{}, nil
		}
		return nil, err
	}
	var nodes map[string]SchedulerNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, err
	}
	if nodes == nil {
		nodes = map[string]SchedulerNode{}
	}
	return nodes, nil
}

func (s *Service) saveNodeConfig(nodes map[string]SchedulerNode) error {
	if err := os.MkdirAll(filepath.Dir(s.nodeConfigPath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.nodeConfigPath(), data, 0o600)
}

func applyNodeOverride(node *SchedulerNode, override SchedulerNode) {
	if strings.TrimSpace(override.Name) != "" {
		node.Name = override.Name
	}
	if override.Labels != nil {
		node.Labels = override.Labels
	}
	if override.MaxConcurrency > 0 {
		node.MaxConcurrency = override.MaxConcurrency
	}
	if strings.TrimSpace(override.CapabilityNote) != "" {
		node.CapabilityNote = override.CapabilityNote
	}
}

func normalizeStringList(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func int64OrDefault(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}
