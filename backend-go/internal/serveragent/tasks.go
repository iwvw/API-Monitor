package serveragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/sseutil"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

const taskHistoryRetention = 24 * time.Hour

// Task 任务
type Task struct {
	ID          string
	ServerID    string
	Type        string
	Command     string
	Status      TaskStatus
	Progress    int
	Result      string
	Error       string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	subscribers []chan TaskEvent
	transient   bool
	mu          sync.RWMutex
}

// TaskEvent 任务事件
type TaskEvent struct {
	Type     string      `json:"type"`
	TaskID   string      `json:"task_id"`
	Status   TaskStatus  `json:"status"`
	Progress int         `json:"progress"`
	Data     interface{} `json:"data,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// Snapshot returns a consistent, serializable view of a task. Terminal
// results are included so a client that reconnects after completion can still
// render the outcome.
func (t *Task) Snapshot() TaskEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	event := TaskEvent{Type: "status", TaskID: t.ID, Status: t.Status, Progress: t.Progress}
	if t.Status == TaskCompleted {
		event.Type = "completed"
		event.Data = t.Result
	} else if t.Status == TaskFailed {
		event.Type = "failed"
		event.Error = t.Error
	}
	return event
}

// TaskRegistry 任务注册表
type TaskRegistry struct {
	tasks       map[string]*Task
	subscribers map[chan TaskEvent]struct{}
	leases      map[string]string
	resources   map[string][]string
	persistence taskPersistence
	lastPrune   time.Time
	mu          sync.RWMutex
}

var ErrTaskResourceBusy = errors.New("task resource is busy")

type TaskResourceBusyError struct {
	Resource string
	TaskID   string
}

func (e *TaskResourceBusyError) Error() string {
	return fmt.Sprintf("%s is owned by task %s", e.Resource, e.TaskID)
}

func (e *TaskResourceBusyError) Unwrap() error { return ErrTaskResourceBusy }

// NewTaskRegistry 创建任务注册表
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{
		tasks:       make(map[string]*Task),
		subscribers: make(map[chan TaskEvent]struct{}),
		leases:      make(map[string]string),
		resources:   make(map[string][]string),
	}
}

func (r *TaskRegistry) AttachPersistence(ctx context.Context, persistence taskPersistence) error {
	if persistence == nil {
		return nil
	}
	tasks, err := persistence.LoadRecent(ctx, taskHistoryRetention)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.persistence = persistence
	for _, task := range tasks {
		if task.Status == TaskPending || task.Status == TaskRunning {
			task.Status = TaskFailed
			task.Error = "backend restarted before task completion; host facts will be reconciled"
			now := time.Now()
			task.CompletedAt = &now
			_ = persistence.Save(context.Background(), task)
		}
		task.subscribers = []chan TaskEvent{}
		r.tasks[task.ID] = task
	}
	r.mu.Unlock()
	_ = persistence.Prune(context.Background(), time.Now().Add(-taskHistoryRetention))
	return nil
}

// Create 创建任务
func (r *TaskRegistry) Create(serverID, taskType, command string) *Task {
	task, _ := r.create(serverID, taskType, command, nil, false)
	return task
}

func (r *TaskRegistry) CreateTransient(serverID, taskType, command string) *Task {
	task, _ := r.create(serverID, taskType, command, nil, true)
	return task
}

func (r *TaskRegistry) CreateExclusive(serverID, taskType, command string, resources ...string) (*Task, error) {
	return r.create(serverID, taskType, command, resources, false)
}

func (r *TaskRegistry) create(serverID, taskType, command string, resources []string, transient bool) (*Task, error) {
	task := &Task{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		Type:        taskType,
		Command:     command,
		Status:      TaskPending,
		Progress:    0,
		CreatedAt:   time.Now(),
		subscribers: []chan TaskEvent{},
		transient:   transient,
	}

	r.mu.Lock()
	for _, resource := range resources {
		if owner := r.leases[resource]; owner != "" {
			r.mu.Unlock()
			return nil, &TaskResourceBusyError{Resource: resource, TaskID: owner}
		}
	}
	r.tasks[task.ID] = task
	for _, resource := range resources {
		r.leases[resource] = task.ID
	}
	if len(resources) > 0 {
		r.resources[task.ID] = append([]string(nil), resources...)
	}
	persistence := r.persistence
	r.mu.Unlock()
	if persistence != nil && !task.transient {
		if err := persistence.Save(context.Background(), task); err != nil && len(resources) > 0 {
			r.mu.Lock()
			delete(r.tasks, task.ID)
			for _, resource := range resources {
				if r.leases[resource] == task.ID {
					delete(r.leases, resource)
				}
			}
			delete(r.resources, task.ID)
			r.mu.Unlock()
			return nil, fmt.Errorf("persist orchestration task: %w", err)
		}
		r.pruneIfDue()
	}

	r.broadcast(TaskEvent{
		Type:     "created",
		TaskID:   task.ID,
		Status:   task.Status,
		Progress: task.Progress,
		Data: map[string]interface{}{
			"serverId":  task.ServerID,
			"type":      task.Type,
			"command":   task.Command,
			"createdAt": task.CreatedAt.Format(time.RFC3339Nano),
		},
	})

	return task, nil
}

func (r *TaskRegistry) ActiveTask(resource string) (*Task, bool) {
	r.mu.RLock()
	owner := r.leases[resource]
	task := r.tasks[owner]
	r.mu.RUnlock()
	return task, task != nil
}

func proxyTaskResource(serverID string) string { return "proxy:" + serverID }

func (s *Service) requireAgentCapability(w http.ResponseWriter, serverID, capability string) bool {
	connection, online := s.registry.Get(serverID)
	if !online {
		response.Error(w, http.StatusBadGateway, "agent offline")
		return false
	}
	if !connection.GetCapabilities()[capability] {
		response.Error(w, http.StatusConflict, "Agent 版本过旧，不支持该编排操作，请先升级 Agent")
		return false
	}
	return true
}

func (s *Service) createExclusiveProxyTask(w http.ResponseWriter, serverID, taskType, command string) (*Task, bool) {
	task, err := s.taskRegistry.CreateExclusive(serverID, taskType, command, proxyTaskResource(serverID))
	if err == nil {
		return task, true
	}
	if errors.Is(err, ErrTaskResourceBusy) {
		data := map[string]interface{}{"server_id": serverID}
		if active, ok := s.taskRegistry.ActiveTask(proxyTaskResource(serverID)); ok {
			data["task_id"] = active.ID
			data["task"] = active.Snapshot()
		}
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "该实例已有代理编排任务正在执行，请等待完成后重试",
			"data":    data,
		})
		return nil, false
	}
	response.Error(w, http.StatusInternalServerError, err.Error())
	return nil, false
}

// Get 获取任务
func (r *TaskRegistry) Get(taskID string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	task, exists := r.tasks[taskID]
	return task, exists
}

// UpdateProgress 更新进度
func (r *TaskRegistry) UpdateProgress(taskID string, progress int, data interface{}) {
	task, exists := r.Get(taskID)
	if !exists {
		return
	}

	task.mu.Lock()
	task.Progress = progress
	if task.Status == TaskPending {
		task.Status = TaskRunning
		now := time.Now()
		task.StartedAt = &now
	}
	task.mu.Unlock()
	r.persist(task)

	// 通知订阅者
	event := TaskEvent{
		Type:     "progress",
		TaskID:   taskID,
		Status:   task.Status,
		Progress: progress,
		Data:     data,
	}
	task.notifySubscribers(event)
	r.broadcast(event)
}

// Complete 完成任务
func (r *TaskRegistry) Complete(taskID string, result string) {
	task, exists := r.Get(taskID)
	if !exists {
		return
	}

	task.mu.Lock()
	task.Status = TaskCompleted
	task.Progress = 100
	task.Result = result
	now := time.Now()
	task.CompletedAt = &now
	task.mu.Unlock()
	r.persist(task)
	r.release(taskID)

	event := TaskEvent{
		Type:     "completed",
		TaskID:   taskID,
		Status:   TaskCompleted,
		Progress: 100,
		Data:     result,
	}
	task.notifySubscribers(event)
	r.broadcast(event)
	task.closeSubscribers()
	r.removeTransient(task)
}

// Fail 失败任务
func (r *TaskRegistry) Fail(taskID string, errorMsg string) {
	task, exists := r.Get(taskID)
	if !exists {
		return
	}

	task.mu.Lock()
	task.Status = TaskFailed
	task.Error = errorMsg
	now := time.Now()
	task.CompletedAt = &now
	task.mu.Unlock()
	r.persist(task)
	r.release(taskID)

	event := TaskEvent{
		Type:   "failed",
		TaskID: taskID,
		Status: TaskFailed,
		Error:  errorMsg,
	}
	task.notifySubscribers(event)
	r.broadcast(event)
	task.closeSubscribers()
	r.removeTransient(task)
}

func (r *TaskRegistry) persist(task *Task) {
	if task.transient {
		return
	}
	r.mu.RLock()
	persistence := r.persistence
	r.mu.RUnlock()
	if persistence != nil {
		_ = persistence.Save(context.Background(), task)
	}
}

func (r *TaskRegistry) removeTransient(task *Task) {
	if task == nil || !task.transient {
		return
	}
	r.mu.Lock()
	delete(r.tasks, task.ID)
	r.mu.Unlock()
}

func (r *TaskRegistry) release(taskID string) {
	r.mu.Lock()
	for _, resource := range r.resources[taskID] {
		if r.leases[resource] == taskID {
			delete(r.leases, resource)
		}
	}
	delete(r.resources, taskID)
	r.mu.Unlock()
}

func (r *TaskRegistry) pruneIfDue() {
	r.mu.Lock()
	if time.Since(r.lastPrune) < time.Hour {
		r.mu.Unlock()
		return
	}
	r.lastPrune = time.Now()
	cutoff := time.Now().Add(-taskHistoryRetention)
	for id, task := range r.tasks {
		task.mu.RLock()
		terminal := task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskCancelled
		completedAt := task.CompletedAt
		task.mu.RUnlock()
		if terminal && completedAt != nil && completedAt.Before(cutoff) {
			delete(r.tasks, id)
		}
	}
	persistence := r.persistence
	r.mu.Unlock()
	if persistence != nil {
		_ = persistence.Prune(context.Background(), cutoff)
	}
}

func (r *TaskRegistry) SubscribeAll() (<-chan TaskEvent, func()) {
	ch := make(chan TaskEvent, 32)

	r.mu.Lock()
	r.subscribers[ch] = struct{}{}
	r.mu.Unlock()

	cancel := func() {
		r.mu.Lock()
		if _, ok := r.subscribers[ch]; ok {
			delete(r.subscribers, ch)
			close(ch)
		}
		r.mu.Unlock()
	}

	return ch, cancel
}

func (r *TaskRegistry) broadcast(event TaskEvent) {
	r.mu.RLock()
	subscribers := make([]chan TaskEvent, 0, len(r.subscribers))
	for ch := range r.subscribers {
		subscribers = append(subscribers, ch)
	}
	r.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// GetStatus 获取任务状态（线程安全）
func (t *Task) GetStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// Subscribe 订阅任务事件
func (t *Task) Subscribe() <-chan TaskEvent {
	t.mu.Lock()
	defer t.mu.Unlock()

	ch := make(chan TaskEvent, 10)
	// Complete/Fail may have happened before the HTTP stream was opened. Queue
	// the terminal event for this late subscriber instead of losing feedback.
	if t.Status == TaskCompleted || t.Status == TaskFailed {
		ch <- t.snapshotLocked()
		close(ch)
		return ch
	}
	t.subscribers = append(t.subscribers, ch)
	return ch
}

func (t *Task) snapshotLocked() TaskEvent {
	event := TaskEvent{Type: "status", TaskID: t.ID, Status: t.Status, Progress: t.Progress}
	if t.Status == TaskCompleted {
		event.Type, event.Data = "completed", t.Result
	} else if t.Status == TaskFailed {
		event.Type, event.Error = "failed", t.Error
	}
	return event
}

// notifySubscribers 通知订阅者
func (t *Task) notifySubscribers(event TaskEvent) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, ch := range t.subscribers {
		select {
		case ch <- event:
		default:
			// 如果通道满了，跳过
		}
	}
}

// closeSubscribers 关闭订阅者
func (t *Task) closeSubscribers() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, ch := range t.subscribers {
		close(ch)
	}
	t.subscribers = nil
}

// createTask 创建任务并下发到 Agent 执行。
// 兼容两类请求体：
//   - 旧式字段：server_id / type / command
//   - 结构化字段：serverId / params.command（AI 与自动化调用常用）
func (s *Service) createTask(w http.ResponseWriter, r *http.Request, taskRegistry *TaskRegistry) {
	var req struct {
		ServerID string `json:"server_id"`
		Type     string `json:"type"`
		Command  string `json:"command"`
		ServerId string `json:"serverId"`
		Params   struct {
			Command   string `json:"command"`
			TimeoutMs int64  `json:"timeoutMs"`
		} `json:"params"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	serverID := strings.TrimSpace(req.ServerID)
	if serverID == "" {
		serverID = strings.TrimSpace(req.ServerId)
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = strings.TrimSpace(req.Params.Command)
	}
	if serverID == "" {
		response.Error(w, http.StatusBadRequest, "server_id required")
		return
	}
	if command == "" {
		response.Error(w, http.StatusBadRequest, "command required")
		return
	}

	// 危险命令拦截
	danger := DetectDangerousCommand(command)
	if danger.Dangerous {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{
			"success":       false,
			"error":         "dangerous command rejected: " + strings.Join(danger.Reasons, ", "),
			"dangerous":     true,
			"dangerReasons": danger.Reasons,
		})
		return
	}

	timeout := 60 * time.Second
	if req.Params.TimeoutMs > 0 {
		timeout = time.Duration(req.Params.TimeoutMs) * time.Millisecond
	}
	if timeout > 30*time.Minute {
		timeout = 30 * time.Minute
	}

	conn, online := s.registry.Get(serverID)
	if !online {
		response.Error(w, http.StatusBadGateway, "agent offline: "+serverID)
		return
	}

	task := taskRegistry.Create(serverID, "shell", command)
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    1, // RUN_COMMAND
		"data":    command,
		"timeout": int(timeout.Seconds()),
	}); err != nil {
		taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"task_id": task.ID,
			"status":  task.Status,
		},
	})
}

// streamTask 任务 SSE 流
func (s *Service) streamTask(w http.ResponseWriter, r *http.Request, taskRegistry *TaskRegistry, taskID string) {
	task, exists := taskRegistry.Get(taskID)
	if !exists {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// 设置 SSE 头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Subscribe before reading the snapshot. This closes the completion race
	// between the initial status response and registration of the listener.
	eventCh := task.Subscribe()
	initialEvent := task.Snapshot()
	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return
	}
	s.writeSSE(w, initialEvent)
	flusher.Flush()
	if initialEvent.Status == TaskCompleted || initialEvent.Status == TaskFailed {
		return
	}
	ctx := r.Context()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
				return
			}
			s.writeSSE(w, event)
			flusher.Flush()

			if event.Status == TaskCompleted || event.Status == TaskFailed {
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// writeSSE 写入 SSE 事件
func (s *Service) writeSSE(w http.ResponseWriter, event TaskEvent) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// writeNamedSSE 写入带事件名的 SSE 事件。
// 每次写入前必须续期写超时：http.Server.WriteTimeout 是自请求开始的绝对
// 截止时间，长连接流不续期会在超时后写入静默失败。返回非 nil 错误表示
// 连接已不可写，调用方应停止输出。
func (s *Service) writeNamedSSE(w http.ResponseWriter, eventName string, payload interface{}) error {
	if err := sseutil.RenewWriteDeadline(w, 0); err != nil {
		return err
	}
	data, _ := json.Marshal(payload)
	if eventName != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

const (
	// defaultExecTimeout 命令执行默认等待超时
	defaultExecTimeout = 30 * time.Second
	// maxExecTimeout 命令执行等待超时上限
	maxExecTimeout = 300 * time.Second
)

// waitAgentTaskResult 等待任务到达终态并返回执行结果。
// 返回 status 为 "success"/"failed"；timedOut 为 true 表示等待超时（任务已被标记失败）。
// 事件通道提前关闭时回退到任务快照，保证已完成任务也能拿到终态结果。
func waitAgentTaskResult(registry *TaskRegistry, task *Task, eventCh <-chan TaskEvent, timeout time.Duration) (output, status string, timedOut bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				snap := task.Snapshot()
				if snap.Status == TaskCompleted {
					return fmt.Sprintf("%v", snap.Data), "success", false
				}
				return snap.Error, "failed", false
			}
			if event.Status == TaskCompleted {
				return fmt.Sprintf("%v", event.Data), "success", false
			}
			if event.Status == TaskFailed {
				return event.Error, "failed", false
			}
		case <-timer.C:
			registry.Fail(task.ID, "task timeout")
			return "", "failed", true
		}
	}
}
