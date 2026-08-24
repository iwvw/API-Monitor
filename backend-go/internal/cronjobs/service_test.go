package cronjobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestTaskCRUDManualRunAndLogCleanup(t *testing.T) {
	service := newCronService(t)

	res := performCronRequest(service, http.MethodGet, "/api/cron/tasks", "")
	if res.Code != http.StatusOK {
		t.Fatalf("initial list status = %d body=%s", res.Code, res.Body.String())
	}
	if tasks := decodeCronData[[]Task](t, res); len(tasks) != 0 {
		t.Fatalf("initial tasks = %#v", tasks)
	}

	createBody := `{"name":"Echo","schedule":"0 0 1 1 *","command":"echo cron-ok","type":"shell","enabled":0}`
	res = performCronRequest(service, http.MethodPost, "/api/cron/tasks", createBody)
	if res.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", res.Code, res.Body.String())
	}
	created := decodeCronData[Task](t, res)
	if created.ID == 0 || created.Name != "Echo" || created.Type != "shell" || created.Enabled != 0 {
		t.Fatalf("unexpected created task: %#v", created)
	}

	taskPath := "/api/cron/tasks/" + strconv.FormatInt(created.ID, 10)
	res = performCronRequest(service, http.MethodPut, taskPath, `{"name":"Echo Updated","enabled":0}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", res.Code, res.Body.String())
	}
	updated := decodeCronData[Task](t, res)
	if updated.Name != "Echo Updated" || updated.Enabled != 0 {
		t.Fatalf("unexpected updated task: %#v", updated)
	}

	res = performCronRequest(service, http.MethodPost, taskPath+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("manual run status = %d body=%s", res.Code, res.Body.String())
	}

	logEntry := waitForCronLog(t, service, created.ID)
	if logEntry.Status != "success" || !strings.Contains(logEntry.Output, "cron-ok") {
		t.Fatalf("unexpected log entry after manual run: %#v", logEntry)
	}
	if logEntry.EndTime == nil || logEntry.Duration == nil {
		t.Fatalf("expected completed log timing, got %#v", logEntry)
	}

	res = performCronRequest(service, http.MethodGet, "/api/cron/logs?task_id="+strconv.FormatInt(created.ID, 10), "")
	if res.Code != http.StatusOK {
		t.Fatalf("logs by task status = %d body=%s", res.Code, res.Body.String())
	}
	taskLogs := decodeCronData[[]Log](t, res)
	if len(taskLogs) != 1 || taskLogs[0].TaskID != created.ID {
		t.Fatalf("unexpected task logs: %#v", taskLogs)
	}

	res = performCronRequest(service, http.MethodDelete, "/api/cron/logs?all=true", "")
	if res.Code != http.StatusOK {
		t.Fatalf("clear logs status = %d body=%s", res.Code, res.Body.String())
	}
	if !decodeCronSuccess(t, res) {
		t.Fatal("expected clear logs success")
	}

	res = performCronRequest(service, http.MethodGet, "/api/cron/logs", "")
	if logs := decodeCronData[[]Log](t, res); len(logs) != 0 {
		t.Fatalf("expected empty logs after cleanup, got %#v", logs)
	}

	res = performCronRequest(service, http.MethodDelete, taskPath, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete task status = %d body=%s", res.Code, res.Body.String())
	}
	if !decodeCronSuccess(t, res) {
		t.Fatal("expected delete task success")
	}

	res = performCronRequest(service, http.MethodGet, "/api/cron/tasks", "")
	if tasks := decodeCronData[[]Task](t, res); len(tasks) != 0 {
		t.Fatalf("expected empty tasks after delete, got %#v", tasks)
	}
}

func TestProductionDisablesLocalShellTasksByDefault(t *testing.T) {
	service := New(config.Config{
		Environment: "production",
		Host:        "127.0.0.1",
		Port:        0,
		DataDir:     t.TempDir(),
		DBName:      "data.db",
	})
	t.Cleanup(func() { <-service.Stop().Done() })
	_, err := service.executeSchedulerTaskCommand(context.Background(), SchedulerTask{
		Task:           Task{Type: "shell", Command: "echo blocked"},
		TimeoutSeconds: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "ALLOW_LOCAL_SHELL_TASKS") {
		t.Fatalf("expected production shell task rejection, got %v", err)
	}
}

func newCronService(t *testing.T) *Service {
	t.Helper()
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
	t.Cleanup(func() {
		ctx := service.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Error("cron scheduler did not stop within timeout")
		}
	})
	return service
}

func performCronRequest(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func waitForCronLog(t *testing.T, service *Service, taskID int64) Log {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	path := "/api/cron/logs?task_id=" + strconv.FormatInt(taskID, 10)
	for time.Now().Before(deadline) {
		res := performCronRequest(service, http.MethodGet, path, "")
		if res.Code != http.StatusOK {
			t.Fatalf("poll logs status = %d body=%s", res.Code, res.Body.String())
		}
		logs := decodeCronData[[]Log](t, res)
		for _, logEntry := range logs {
			if logEntry.Status != "running" {
				return logEntry
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for cron log")
	return Log{}
}

func decodeCronSuccess(t *testing.T, res *httptest.ResponseRecorder) bool {
	t.Helper()
	var payload struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
	return payload.Success
}

func decodeCronData[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	var payload struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", res.Body.String(), err)
	}
	if !payload.Success {
		t.Fatalf("expected success payload, got error=%q body=%s", payload.Error, res.Body.String())
	}
	var data T
	if err := json.Unmarshal(payload.Data, &data); err != nil {
		t.Fatalf("decode data %q: %v", string(payload.Data), err)
	}
	return data
}

type fakeNotifier struct {
	calls []string
	data  []map[string]interface{}
}

func (f *fakeNotifier) Trigger(_ context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	f.calls = append(f.calls, sourceModule+"."+eventType)
	f.data = append(f.data, eventData)
	return nil
}

func TestTaskAndWorkflowNotifyOnResult(t *testing.T) {
	service := newCronService(t)
	notifier := &fakeNotifier{}
	service.SetNotifier(notifier)

	// 创建任务并手动执行（禁用 shell 时任务会失败，验证 failed 事件）。
	res := performCronRequest(service, http.MethodPost, "/api/cron/tasks", `{"name":"Notify Test","schedule":"0 0 1 1 *","command":"echo notify","type":"shell","enabled":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", res.Code, res.Body.String())
	}
	task := decodeCronData[SchedulerTask](t, res)
	service.ExecuteTask(context.Background(), task.ID)
	if len(notifier.calls) == 0 {
		t.Fatalf("task execution did not trigger notifier, calls=%#v", notifier.calls)
	}

	// 工作流：创建含 start/end 标记 + 内联 shell 节点并运行，验证 completed 事件。
	body := `{
		"name":"Notify Flow","enabled":1,
		"nodes":[
			{"id":"start","name":"开始","type":"start","enabled":1},
			{"id":"work","name":"Work","type":"shell","command":"echo ok","enabled":1},
			{"id":"end","name":"结束","type":"end","enabled":1}
		],
		"edges":[
			{"from":"start","to":"work","condition":"success"},
			{"from":"work","to":"end","condition":"success"}
		]
	}`
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows", body)
	if res.Code != http.StatusOK {
		t.Fatalf("create workflow status = %d body=%s", res.Code, res.Body.String())
	}
	wf := decodeCronData[Workflow](t, res)
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(wf.ID, 10)+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run workflow status = %d body=%s", res.Code, res.Body.String())
	}

	workflowTriggered := false
	for _, call := range notifier.calls {
		if strings.HasPrefix(call, "cron.workflow.") {
			workflowTriggered = true
			break
		}
	}
	if !workflowTriggered {
		t.Fatalf("workflow execution did not trigger notifier, calls=%#v", notifier.calls)
	}
}
