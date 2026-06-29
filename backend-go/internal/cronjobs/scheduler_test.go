package cronjobs

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
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
