package cronjobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/robfig/cron/v3"
)

const (
	httpTaskTimeout  = 30 * time.Second
	shellTaskTimeout = 5 * time.Minute
	internalTimeout  = time.Minute
	maxLogOutput     = 5000
)

type Service struct {
	cfg       config.Config
	store     *database.Store
	scheduler *cron.Cron
	entries   map[int64]cron.EntryID
	mu        sync.Mutex
	client    *http.Client
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
	Name     *string `json:"name"`
	Schedule *string `json:"schedule"`
	Command  *string `json:"command"`
	Type     *string `json:"type"`
	Enabled  *int    `json:"enabled"`
	LastRun  *int64  `json:"last_run"`
	NextRun  *int64  `json:"next_run"`
}

func New(cfg config.Config) *Service {
	service := &Service{
		cfg:       cfg,
		store:     database.New(cfg),
		scheduler: cron.New(),
		entries:   map[int64]cron.EntryID{},
		client:    &http.Client{},
	}
	service.scheduler.Start()
	_ = service.ReloadAll(context.Background())
	return service
}

func (s *Service) Stop() context.Context {
	return s.scheduler.Stop()
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entryID := range s.entries {
		s.scheduler.Remove(entryID)
	}
	s.entries = map[int64]cron.EntryID{}
	for _, task := range tasks {
		if task.Enabled != 0 {
			_ = s.scheduleTaskLocked(task)
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
	if strings.TrimSpace(task.Schedule) == "" {
		return fmt.Errorf("empty cron schedule")
	}
	entryID, err := s.scheduler.AddFunc(task.Schedule, func() {
		go s.ExecuteTask(context.Background(), task.ID)
	})
	if err != nil {
		return err
	}
	s.entries[task.ID] = entryID
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
		Enabled:   intValue(payload.Enabled, 1),
		CreatedAt: time.Now().Unix(),
	}
	if strings.TrimSpace(task.Name) == "" || strings.TrimSpace(task.Schedule) == "" || strings.TrimSpace(task.Command) == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "name, schedule, and command are required"})
		return
	}

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
	task, ok, err := findTask(ctx, db, taskID)
	if err != nil || !ok {
		db.Close()
		return
	}
	startTime := time.Now().Unix()
	logID, err := createLog(ctx, db, task.ID, "running", "", startTime)
	if err != nil {
		db.Close()
		return
	}
	db.Close()

	status := "success"
	output, err := s.executeTaskCommand(ctx, task)
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
}

func (s *Service) executeTaskCommand(ctx context.Context, task Task) (string, error) {
	switch task.Type {
	case "http":
		return s.executeHTTPTask(ctx, task.Command, httpTaskTimeout)
	case "internal":
		return s.executeInternalTask(ctx, task.Command)
	default:
		return executeShellTask(ctx, task.Command)
	}
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

func (s *Service) executeInternalTask(ctx context.Context, command string) (string, error) {
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
	reqCtx, cancel := context.WithTimeout(ctx, internalTimeout)
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

func executeShellTask(ctx context.Context, command string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, shellTaskTimeout)
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
	if err := ensureSchema(ctx, db); err != nil {
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
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure cron schema: %w", err)
		}
	}
	return nil
}

func loadTasks(ctx context.Context, db *sql.DB) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, schedule, command, type, enabled, last_run, next_run, created_at
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
		SELECT id, name, schedule, command, type, enabled, last_run, next_run, created_at
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
	err := scanner.Scan(&task.ID, &task.Name, &task.Schedule, &task.Command, &taskType, &enabled, &lastRun, &nextRun, &task.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	task.Type = stringOrDefault(taskType, "shell")
	task.Enabled = int(int64OrDefault(enabled, 1))
	task.LastRun = int64Ptr(lastRun)
	task.NextRun = int64Ptr(nextRun)
	return task, nil
}

func insertTask(ctx context.Context, db *sql.DB, task Task) (Task, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO cron_tasks (name, schedule, command, type, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, task.Name, task.Schedule, task.Command, task.Type, task.Enabled, task.CreatedAt)
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
		args = append(args, *payload.Enabled)
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

func int64OrDefault(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}
