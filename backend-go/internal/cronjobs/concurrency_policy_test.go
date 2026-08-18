package cronjobs

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestConcurrencyPolicySkipBlocksOverlappingRuns：策略为 skip 时，
// 已有 running 的 run 存在则新触发被拒绝；清理后再次触发恢复正常。
func TestConcurrencyPolicySkipBlocksOverlappingRuns(t *testing.T) {
	service := newCronService(t)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	wf, err := insertWorkflow(context.Background(), db, Workflow{
		Name:              "Concurrency Skip",
		Schedule:          "*/5 * * * *",
		Enabled:           1,
		ConcurrencyPolicy: "skip",
		FailurePolicy:     "stop",
		Nodes: []WorkflowNode{
			{ID: "start", Name: "开始", Type: "start", Enabled: 1},
			{ID: "work", Name: "Work", Type: "shell", Command: "echo ok", Enabled: 1},
			{ID: "end", Name: "结束", Type: "end", Enabled: 1},
		},
		Edges: []WorkflowEdge{
			{From: "start", To: "work", Condition: "success"},
			{From: "work", To: "end", Condition: "success"},
		},
	})
	if err != nil {
		t.Fatalf("insert workflow: %v", err)
	}

	// 模拟一个仍在 running 的 run（直接插库，避免真实执行耗时）
	runID, err := createWorkflowRun(context.Background(), db, wf, "manual", 0)
	if err != nil {
		t.Fatalf("create running run: %v", err)
	}

	res := performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(wf.ID, 10)+"/run", "")
	if res.Code == http.StatusOK {
		t.Fatalf("overlapping run should be rejected by skip policy, got 200 body=%s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "skip") {
		t.Fatalf("expected skip policy error message, body=%s", res.Body.String())
	}

	// 清理 running run 后再次触发应成功
	_, _ = db.ExecContext(context.Background(), `UPDATE scheduler_workflow_runs SET status = 'success' WHERE id = ?`, runID)
	res = performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(wf.ID, 10)+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("run after cleanup status = %d body=%s", res.Code, res.Body.String())
	}
	run := decodeCronData[WorkflowRun](t, res)
	if run.Status != "success" {
		t.Fatalf("expected success run, got %s", run.Status)
	}
}

// TestConcurrencyPolicyAllowDoesNotBlock：策略为 allow 时已有 run 不阻止新触发。
func TestConcurrencyPolicyAllowDoesNotBlock(t *testing.T) {
	service := newCronService(t)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	wf, err := insertWorkflow(context.Background(), db, Workflow{
		Name:              "Concurrency Allow",
		Schedule:          "*/5 * * * *",
		Enabled:           1,
		ConcurrencyPolicy: "allow",
		FailurePolicy:     "stop",
		Nodes: []WorkflowNode{
			{ID: "start", Name: "开始", Type: "start", Enabled: 1},
			{ID: "work", Name: "Work", Type: "shell", Command: "echo ok", Enabled: 1},
			{ID: "end", Name: "结束", Type: "end", Enabled: 1},
		},
		Edges: []WorkflowEdge{
			{From: "start", To: "work", Condition: "success"},
			{From: "work", To: "end", Condition: "success"},
		},
	})
	if err != nil {
		t.Fatalf("insert workflow: %v", err)
	}

	if _, err := createWorkflowRun(context.Background(), db, wf, "manual", 0); err != nil {
		t.Fatalf("create running run: %v", err)
	}

	res := performCronRequest(service, http.MethodPost, "/api/scheduler/workflows/"+strconv.FormatInt(wf.ID, 10)+"/run", "")
	if res.Code != http.StatusOK {
		t.Fatalf("allow policy should not block, status = %d body=%s", res.Code, res.Body.String())
	}
	run := decodeCronData[WorkflowRun](t, res)
	if run.Status != "success" {
		t.Fatalf("expected success run, got %s", run.Status)
	}
}
