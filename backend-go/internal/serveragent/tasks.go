package serveragent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
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

// TaskRegistry 任务注册表
type TaskRegistry struct {
	tasks       map[string]*Task
	subscribers map[chan TaskEvent]struct{}
	mu          sync.RWMutex
}

// NewTaskRegistry 创建任务注册表
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{
		tasks:       make(map[string]*Task),
		subscribers: make(map[chan TaskEvent]struct{}),
	}
}

// Create 创建任务
func (r *TaskRegistry) Create(serverID, taskType, command string) *Task {
	task := &Task{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		Type:        taskType,
		Command:     command,
		Status:      TaskPending,
		Progress:    0,
		CreatedAt:   time.Now(),
		subscribers: []chan TaskEvent{},
	}

	r.mu.Lock()
	r.tasks[task.ID] = task
	r.mu.Unlock()

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

	return task
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

	event := TaskEvent{
		Type:   "failed",
		TaskID: taskID,
		Status: TaskFailed,
		Error:  errorMsg,
	}
	task.notifySubscribers(event)
	r.broadcast(event)
	task.closeSubscribers()
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

// Subscribe 订阅任务事件
func (t *Task) Subscribe() <-chan TaskEvent {
	t.mu.Lock()
	defer t.mu.Unlock()

	ch := make(chan TaskEvent, 10)
	t.subscribers = append(t.subscribers, ch)
	return ch
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

// createTask 创建任务
func (s *Service) createTask(w http.ResponseWriter, r *http.Request, taskRegistry *TaskRegistry) {
	var req struct {
		ServerID string `json:"server_id"`
		Type     string `json:"type"`
		Command  string `json:"command"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	task := taskRegistry.Create(req.ServerID, req.Type, req.Command)

	// TODO: 发送任务到 Agent
	// conn, exists := s.registry.Get(req.ServerID)
	// if exists {
	//     conn.SendEvent("task", map[string]interface{}{
	//         "task_id": task.ID,
	//         "command": req.Command,
	//     })
	// }

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

	// 发送当前状态
	initialEvent := TaskEvent{
		Type:     "status",
		TaskID:   task.ID,
		Status:   task.Status,
		Progress: task.Progress,
	}
	s.writeSSE(w, initialEvent)
	flusher.Flush()

	// 订阅事件
	eventCh := task.Subscribe()
	ctx := r.Context()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
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

func (s *Service) writeNamedSSE(w http.ResponseWriter, eventName string, payload interface{}) {
	data, _ := json.Marshal(payload)
	if eventName != "" {
		fmt.Fprintf(w, "event: %s\n", eventName)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
