package cronjobs

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSchedulerTaskCronPreviewValidationAndNodes(t *testing.T) {
	service := newCronService(t)

	res := performCronRequest(service, http.MethodPost, "/api/scheduler/cron/preview", `{"schedule":"0 3 * * *","count":5}`)
	if res.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", res.Code, res.Body.String())
	}
	preview := decodeCronData[struct {
		Schedule string  `json:"schedule"`
		Summary  string  `json:"summary"`
		Next     []int64 `json:"next"`
	}](t, res)
	if preview.Summary == "" || len(preview.Next) != 5 {
		t.Fatalf("unexpected preview: %#v", preview)
	}

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/tasks", `{"name":"Bad","schedule":"bad cron","command":"echo bad","type":"shell"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid cron status = %d body=%s", res.Code, res.Body.String())
	}

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/tasks", `{"name":"HTTP","schedule":"*/5 * * * *","command":"https://example.com/health","type":"http","enabled":0,"timeout_seconds":10,"retry_count":2}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create scheduler task status = %d body=%s", res.Code, res.Body.String())
	}
	task := decodeCronData[SchedulerTask](t, res)
	if task.ID == 0 || task.Type != "http" || task.TimeoutSeconds != 10 || task.RetryCount != 2 {
		t.Fatalf("unexpected scheduler task: %#v", task)
	}

	res = performCronRequest(service, http.MethodGet, "/api/scheduler/nodes", "")
	if res.Code != http.StatusOK {
		t.Fatalf("nodes status = %d body=%s", res.Code, res.Body.String())
	}
	nodes := decodeCronData[[]SchedulerNode](t, res)
	if len(nodes) == 0 || nodes[0].ID != "local" {
		t.Fatalf("expected local node, got %#v", nodes)
	}

	res = performCronRequest(service, http.MethodPut, "/api/scheduler/nodes/local", `{"labels":["local","gpu"],"max_concurrency":8,"capability_note":"custom"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update node status = %d body=%s", res.Code, res.Body.String())
	}

	res = performCronRequest(service, http.MethodGet, "/api/scheduler/nodes", "")
	if res.Code != http.StatusOK {
		t.Fatalf("nodes after update status = %d body=%s", res.Code, res.Body.String())
	}
	nodes = decodeCronData[[]SchedulerNode](t, res)
	if len(nodes) == 0 || nodes[0].MaxConcurrency != 8 || nodes[0].CapabilityNote != "custom" || !containsString(nodes[0].Labels, "gpu") {
		t.Fatalf("expected updated local node, got %#v", nodes)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestSchedulerWorkflowDagValidationAndRun(t *testing.T) {
	service := newCronService(t)

	res := performCronRequest(service, http.MethodPost, "/api/scheduler/tasks", `{"name":"Step A","command":"echo step-a","type":"shell","enabled":0}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create step A status = %d body=%s", res.Code, res.Body.String())
	}
	taskA := decodeCronData[SchedulerTask](t, res)

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/tasks", `{"name":"Step B","command":"echo step-b","type":"shell","enabled":0}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create step B status = %d body=%s", res.Code, res.Body.String())
	}
	taskB := decodeCronData[SchedulerTask](t, res)

	cyclic := `{
		"name":"Bad DAG",
		"enabled":1,
		"nodes":[
			{"id":"a","name":"A","task_id":` + strconv.FormatInt(taskA.ID, 10) + `,"enabled":1},
			{"id":"b","name":"B","task_id":` + strconv.FormatInt(taskB.ID, 10) + `,"enabled":1}
		],
		"edges":[
			{"from":"a","to":"b","condition":"success"},
			{"from":"b","to":"a","condition":"success"}
		]
	}`
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows", cyclic)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("cyclic workflow status = %d body=%s", res.Code, res.Body.String())
	}

	valid := `{
		"name":"Deploy Flow",
		"description":"test workflow",
		"enabled":1,
		"nodes":[
			{"id":"a","name":"A","task_id":` + strconv.FormatInt(taskA.ID, 10) + `,"enabled":1},
			{"id":"b","name":"B","task_id":` + strconv.FormatInt(taskB.ID, 10) + `,"enabled":1},
			{"id":"c","name":"C","type":"shell","command":"echo inline","enabled":1}
		],
		"edges":[
			{"from":"a","to":"b","condition":"success"},
			{"from":"b","to":"c","condition":"success"}
		],
		"failure_policy":"stop"
	}`
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows", valid)
	if res.Code != http.StatusOK {
		t.Fatalf("create workflow status = %d body=%s", res.Code, res.Body.String())
	}
	workflow := decodeCronData[Workflow](t, res)
	if workflow.ID == 0 || len(workflow.Nodes) != 3 || len(workflow.Edges) != 2 {
		t.Fatalf("unexpected workflow: %#v", workflow)
	}

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(workflow.ID, 10)+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run workflow status = %d body=%s", res.Code, res.Body.String())
	}
	run := decodeCronData[WorkflowRun](t, res)
	if run.Status != "success" || len(run.NodeRuns) != 3 {
		t.Fatalf("unexpected run: %#v", run)
	}
	if !strings.Contains(run.Summary, "成功 3") {
		t.Fatalf("unexpected summary: %q", run.Summary)
	}
}

func TestSchedulerWorkflowExportImportCancelAndRetry(t *testing.T) {
	service := newCronService(t)

	body := `{
		"name":"Fail Flow",
		"enabled":1,
		"nodes":[{"id":"fail","name":"Fail","type":"shell","command":"exit 1","enabled":1}],
		"failure_policy":"stop"
	}`
	res := performCronRequest(service, http.MethodPost, "/api/scheduler/workflows", body)
	if res.Code != http.StatusOK {
		t.Fatalf("create workflow status = %d body=%s", res.Code, res.Body.String())
	}
	workflow := decodeCronData[Workflow](t, res)

	res = performCronRequest(service, http.MethodGet, "/api/scheduler/workflows/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export workflow status = %d body=%s", res.Code, res.Body.String())
	}
	exported := decodeCronData[struct {
		WorkflowCnt int        `json:"workflow_cnt"`
		Workflows   []Workflow `json:"workflows"`
	}](t, res)
	if exported.WorkflowCnt != 1 || len(exported.Workflows) != 1 {
		t.Fatalf("unexpected export: %#v", exported)
	}

	importBody := `{"workflows":[{"name":"Imported Flow","enabled":1,"nodes":[{"id":"a","name":"A","type":"shell","command":"echo imported","enabled":1}]}]}`
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/import", importBody)
	if res.Code != http.StatusOK {
		t.Fatalf("import workflow status = %d body=%s", res.Code, res.Body.String())
	}
	imported := decodeCronData[struct {
		Imported  int        `json:"imported"`
		Workflows []Workflow `json:"workflows"`
	}](t, res)
	if imported.Imported != 1 || imported.Workflows[0].Name != "Imported Flow" {
		t.Fatalf("unexpected import: %#v", imported)
	}

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(workflow.ID, 10)+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run failed workflow status = %d body=%s", res.Code, res.Body.String())
	}
	failedRun := decodeCronData[WorkflowRun](t, res)
	if failedRun.Status != "failed" {
		t.Fatalf("expected failed run, got %#v", failedRun)
	}

	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflow-runs/"+strconv.FormatInt(failedRun.ID, 10)+"/retry", "")
	if res.Code != http.StatusOK {
		t.Fatalf("retry workflow status = %d body=%s", res.Code, res.Body.String())
	}
	retryRun := decodeCronData[WorkflowRun](t, res)
	if retryRun.TriggerType != "retry" || retryRun.ID == failedRun.ID {
		t.Fatalf("unexpected retry run: %#v", retryRun)
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Unix()
	runningID, err := createWorkflowRun(context.Background(), db, workflow, "manual", start)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflow-runs/"+strconv.FormatInt(runningID, 10)+"/cancel", "")
	if res.Code != http.StatusOK {
		t.Fatalf("cancel workflow status = %d body=%s", res.Code, res.Body.String())
	}
	cancelled := decodeCronData[WorkflowRun](t, res)
	if cancelled.Status != "cancelled" {
		t.Fatalf("unexpected cancelled run: %#v", cancelled)
	}
}

func TestSchedulerTaskRetryAndConcurrencyControls(t *testing.T) {
	service := newCronService(t)
	runner := &flakyAgentRunner{}
	service.SetAgentRunner(runner)

	output, err := service.executeSchedulerTaskCommand(context.Background(), SchedulerTask{
		Task: Task{
			Name:    "Retry Agent",
			Type:    "agent",
			Command: "echo ok",
		},
		TimeoutSeconds:       1,
		RetryCount:           1,
		RetryIntervalSeconds: 0,
		NodeID:               "server-1",
	})
	if err != nil {
		t.Fatalf("expected retry success, got %v", err)
	}
	if runner.calls != 2 || !strings.Contains(output, "第 2 次尝试成功") {
		t.Fatalf("expected second attempt success, calls=%d output=%q", runner.calls, output)
	}

	if !service.acquireTaskSlot(42, 1) {
		t.Fatal("expected first task slot acquisition to succeed")
	}
	if service.acquireTaskSlot(42, 1) {
		t.Fatal("expected second task slot acquisition to respect max concurrency")
	}
	service.releaseTaskSlot(42)
	if !service.acquireTaskSlot(42, 1) {
		t.Fatal("expected task slot acquisition after release to succeed")
	}
	service.releaseTaskSlot(42)
}

type flakyAgentRunner struct {
	calls int
}

func (r *flakyAgentRunner) RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error) {
	r.calls++
	if r.calls == 1 {
		return "", fmt.Errorf("temporary failure")
	}
	return "agent ok", nil
}
