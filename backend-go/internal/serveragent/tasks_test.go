package serveragent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

func TestTaskLateSubscriberReceivesTerminalResult(t *testing.T) {
	registry := NewTaskRegistry()
	task := registry.Create("server-1", "proxy.node.reconcile", "node-1")
	registry.Complete(task.ID, "node ready")
	event := <-task.Subscribe()
	if event.Status != TaskCompleted || event.Data != "node ready" {
		t.Fatalf("late subscriber event = %#v", event)
	}
}

func TestTaskExclusiveResourceReleasedAfterTerminalState(t *testing.T) {
	registry := NewTaskRegistry()
	first, err := registry.CreateExclusive("server-1", "proxy.runtime.install", "install", "proxy:server-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.CreateExclusive("server-1", "proxy.node.reconcile", "node-1", "proxy:server-1"); !errors.Is(err, ErrTaskResourceBusy) {
		t.Fatalf("second task error = %v", err)
	}
	registry.Complete(first.ID, "ready")
	if _, err := registry.CreateExclusive("server-1", "proxy.node.reconcile", "node-1", "proxy:server-1"); err != nil {
		t.Fatalf("resource was not released: %v", err)
	}
}

func TestTaskPersistenceRecoversInterruptedTaskAsFailed(t *testing.T) {
	ctx := context.Background()
	store := database.New(config.Config{DataDir: t.TempDir(), DBName: filepath.Base("tasks.db")})
	persistence := newSQLiteTaskPersistence(store)
	if err := persistence.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	created := NewTaskRegistry()
	created.persistence = persistence
	task := created.Create("server-1", "proxy.runtime.install", "install")
	created.UpdateProgress(task.ID, 40, nil)

	recovered := NewTaskRegistry()
	if err := recovered.AttachPersistence(ctx, persistence); err != nil {
		t.Fatal(err)
	}
	restored, ok := recovered.Get(task.ID)
	if !ok || restored.GetStatus() != TaskFailed {
		t.Fatalf("restored task = %#v", restored)
	}
	if event := restored.Snapshot(); !strings.Contains(event.Error, "backend restarted") {
		t.Fatalf("restored error = %q", event.Error)
	}
	loaded, err := persistence.LoadRecent(ctx, 24*time.Hour)
	if err != nil || len(loaded) != 1 || loaded[0].Status != TaskFailed {
		t.Fatalf("persisted recovery = %#v, %v", loaded, err)
	}
}

func TestStreamTaskReplaysCompletedTask(t *testing.T) {
	registry := NewTaskRegistry()
	task := registry.Create("server-1", "proxy.node.reconcile", "node-1")
	registry.Complete(task.ID, "node ready")
	req := httptest.NewRequest("GET", "/api/server/tasks/"+task.ID+"/stream", nil)
	recorder := httptest.NewRecorder()
	service := &Service{}
	service.streamTask(recorder, req, registry, task.ID)
	body := recorder.Body.String()
	if !strings.Contains(body, `"status":"completed"`) || !strings.Contains(body, "node ready") {
		t.Fatalf("completed task was not replayed: %s", body)
	}
}

func TestTaskPersistencePrunesHistoryOutsideRetention(t *testing.T) {
	ctx := context.Background()
	store := database.New(config.Config{DataDir: t.TempDir(), DBName: filepath.Base("tasks.db")})
	persistence := newSQLiteTaskPersistence(store)
	if err := persistence.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	oldCreated := time.Now().Add(-48 * time.Hour)
	oldCompleted := oldCreated.Add(5 * time.Minute)
	if err := persistence.Save(ctx, &Task{
		ID:          "old-task",
		ServerID:    "server-1",
		Type:        "docker.internal.3",
		Command:     "inspect",
		Status:      TaskCompleted,
		Progress:    100,
		Result:      "ok",
		CreatedAt:   oldCreated,
		CompletedAt: &oldCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	registry := NewTaskRegistry()
	if err := registry.AttachPersistence(ctx, persistence); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("old-task"); ok {
		t.Fatal("expected old task to stay out of in-memory history")
	}
	loaded, err := persistence.LoadRecent(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected pruned history, got %#v", loaded)
	}
}

// TestWriteNamedSSERenewsWriteDeadline 验证：SSE 写入前必须续期写超时，
// 否则 http.Server.WriteTimeout 的绝对截止时间一到，长连接流会静默死掉。
func TestWriteNamedSSERenewsWriteDeadline(t *testing.T) {
	service := &Service{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer must support flush")
			return
		}
		for i := 0; i < 3; i++ {
			if err := service.writeNamedSSE(w, "ping", map[string]interface{}{"i": i}); err != nil {
				return
			}
			flusher.Flush()
			// 每次写入间隔超过 WriteTimeout：不续期的实现在第二写就会失败。
			time.Sleep(60 * time.Millisecond)
		}
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(handler))
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()

	res, err := http.Get(server.URL + "/v2/tasks/stream")
	if err != nil {
		t.Fatalf("connect stream: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if got := strings.Count(string(body), "event: ping"); got != 3 {
		t.Fatalf("expected 3 events across the write deadline, got %d, body=%q", got, body)
	}
}
