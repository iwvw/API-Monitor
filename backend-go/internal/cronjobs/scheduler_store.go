package cronjobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func loadSchedulerTasks(ctx context.Context, db *sql.DB) ([]SchedulerTask, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			t.id, t.name, t.schedule, t.command, t.type, t.enabled, t.last_run, t.next_run, t.created_at,
			COALESCE(t.description, ''), COALESCE(t.timeout_seconds, 300), COALESCE(t.retry_count, 0),
			COALESCE(t.retry_interval_seconds, 30), COALESCE(t.max_concurrency, 1),
			COALESCE(t.node_id, 'local'), COALESCE(t.node_selector, ''), COALESCE(t.config, ''),
			(
				SELECT l.status
				FROM cron_logs l
				WHERE l.task_id = t.id
				ORDER BY l.start_time DESC
				LIMIT 1
			)
		FROM cron_tasks t
		ORDER BY t.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("load scheduler tasks: %w", err)
	}
	defer rows.Close()
	tasks := []SchedulerTask{}
	for rows.Next() {
		task, err := scanSchedulerTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func findSchedulerTask(ctx context.Context, db *sql.DB, id int64) (SchedulerTask, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT
			t.id, t.name, t.schedule, t.command, t.type, t.enabled, t.last_run, t.next_run, t.created_at,
			COALESCE(t.description, ''), COALESCE(t.timeout_seconds, 300), COALESCE(t.retry_count, 0),
			COALESCE(t.retry_interval_seconds, 30), COALESCE(t.max_concurrency, 1),
			COALESCE(t.node_id, 'local'), COALESCE(t.node_selector, ''), COALESCE(t.config, ''),
			(
				SELECT l.status
				FROM cron_logs l
				WHERE l.task_id = t.id
				ORDER BY l.start_time DESC
				LIMIT 1
			)
		FROM cron_tasks t
		WHERE t.id = ?
	`, id)
	task, err := scanSchedulerTask(row)
	if err == nil {
		return task, true, nil
	}
	if err == sql.ErrNoRows {
		return SchedulerTask{}, false, nil
	}
	return SchedulerTask{}, false, err
}

func scanSchedulerTask(scanner taskScanner) (SchedulerTask, error) {
	var task SchedulerTask
	var taskType sql.NullString
	var enabled sql.NullInt64
	var lastRun, nextRun sql.NullInt64
	var description, nodeID, nodeSelector, config sql.NullString
	var timeoutSeconds, retryCount, retryIntervalSeconds, maxConcurrency sql.NullInt64
	var recentStatus sql.NullString
	err := scanner.Scan(
		&task.ID,
		&task.Name,
		&task.Schedule,
		&task.Command,
		&taskType,
		&enabled,
		&lastRun,
		&nextRun,
		&task.CreatedAt,
		&description,
		&timeoutSeconds,
		&retryCount,
		&retryIntervalSeconds,
		&maxConcurrency,
		&nodeID,
		&nodeSelector,
		&config,
		&recentStatus,
	)
	if err != nil {
		return SchedulerTask{}, err
	}
	task.Type = stringOrDefault(taskType, "shell")
	task.Enabled = int(int64OrDefault(enabled, 1))
	task.LastRun = int64Ptr(lastRun)
	task.NextRun = int64Ptr(nextRun)
	task.Description = stringOrDefault(description, "")
	task.TimeoutSeconds = int(int64OrDefault(timeoutSeconds, 300))
	task.RetryCount = int(int64OrDefault(retryCount, 0))
	task.RetryIntervalSeconds = int(int64OrDefault(retryIntervalSeconds, 30))
	task.MaxConcurrency = int(int64OrDefault(maxConcurrency, 1))
	task.NodeID = stringOrDefault(nodeID, "local")
	task.NodeSelector = stringOrDefault(nodeSelector, "")
	task.Config = stringOrDefault(config, "")
	task.RecentStatus = stringPtr(recentStatus)
	return task, nil
}

func insertSchedulerTask(ctx context.Context, db *sql.DB, task SchedulerTask) (SchedulerTask, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO cron_tasks (
			name, schedule, command, type, enabled, created_at, description,
			timeout_seconds, retry_count, retry_interval_seconds, max_concurrency,
			node_id, node_selector, config
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.Name, task.Schedule, task.Command, task.Type, task.Enabled, task.CreatedAt, task.Description,
		task.TimeoutSeconds, task.RetryCount, task.RetryIntervalSeconds, task.MaxConcurrency, task.NodeID, task.NodeSelector, task.Config)
	if err != nil {
		return SchedulerTask{}, fmt.Errorf("create scheduler task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SchedulerTask{}, err
	}
	created, _, err := findSchedulerTask(ctx, db, id)
	return created, err
}

func updateSchedulerTaskRow(ctx context.Context, db *sql.DB, task SchedulerTask) (SchedulerTask, error) {
	_, err := db.ExecContext(ctx, `
		UPDATE cron_tasks
		SET
			name = ?,
			schedule = ?,
			command = ?,
			type = ?,
			enabled = ?,
			description = ?,
			timeout_seconds = ?,
			retry_count = ?,
			retry_interval_seconds = ?,
			max_concurrency = ?,
			node_id = ?,
			node_selector = ?,
			config = ?
		WHERE id = ?
	`, task.Name, task.Schedule, task.Command, task.Type, task.Enabled, task.Description,
		task.TimeoutSeconds, task.RetryCount, task.RetryIntervalSeconds, task.MaxConcurrency,
		task.NodeID, task.NodeSelector, task.Config, task.ID)
	if err != nil {
		return SchedulerTask{}, fmt.Errorf("update scheduler task: %w", err)
	}
	updated, _, err := findSchedulerTask(ctx, db, task.ID)
	return updated, err
}

func loadWorkflows(ctx context.Context, db *sql.DB) ([]Workflow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, description, schedule, enabled, nodes, edges, concurrency_policy, failure_policy, created_at, updated_at
		FROM scheduler_workflows
		ORDER BY updated_at DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("load workflows: %w", err)
	}
	defer rows.Close()
	workflows := []Workflow{}
	for rows.Next() {
		workflow, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, rows.Err()
}

func findWorkflow(ctx context.Context, db *sql.DB, id int64) (Workflow, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, description, schedule, enabled, nodes, edges, concurrency_policy, failure_policy, created_at, updated_at
		FROM scheduler_workflows
		WHERE id = ?
	`, id)
	workflow, err := scanWorkflow(row)
	if err == nil {
		return workflow, true, nil
	}
	if err == sql.ErrNoRows {
		return Workflow{}, false, nil
	}
	return Workflow{}, false, err
}

func scanWorkflow(scanner taskScanner) (Workflow, error) {
	var workflow Workflow
	var nodesJSON, edgesJSON string
	err := scanner.Scan(
		&workflow.ID,
		&workflow.Name,
		&workflow.Description,
		&workflow.Schedule,
		&workflow.Enabled,
		&nodesJSON,
		&edgesJSON,
		&workflow.ConcurrencyPolicy,
		&workflow.FailurePolicy,
		&workflow.CreatedAt,
		&workflow.UpdatedAt,
	)
	if err != nil {
		return Workflow{}, err
	}
	if err := json.Unmarshal([]byte(nodesJSON), &workflow.Nodes); err != nil {
		workflow.Nodes = []WorkflowNode{}
	}
	if err := json.Unmarshal([]byte(edgesJSON), &workflow.Edges); err != nil {
		workflow.Edges = []WorkflowEdge{}
	}
	return workflow, nil
}

func insertWorkflow(ctx context.Context, db *sql.DB, workflow Workflow) (Workflow, error) {
	nodesJSON, _ := json.Marshal(workflow.Nodes)
	edgesJSON, _ := json.Marshal(workflow.Edges)
	now := time.Now().Unix()
	result, err := db.ExecContext(ctx, `
		INSERT INTO scheduler_workflows (
			name, description, schedule, enabled, nodes, edges, concurrency_policy, failure_policy, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, workflow.Name, workflow.Description, workflow.Schedule, workflow.Enabled, string(nodesJSON), string(edgesJSON),
		workflow.ConcurrencyPolicy, workflow.FailurePolicy, now, now)
	if err != nil {
		return Workflow{}, fmt.Errorf("create workflow: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Workflow{}, err
	}
	created, _, err := findWorkflow(ctx, db, id)
	return created, err
}

func updateWorkflowRow(ctx context.Context, db *sql.DB, workflow Workflow) (Workflow, error) {
	nodesJSON, _ := json.Marshal(workflow.Nodes)
	edgesJSON, _ := json.Marshal(workflow.Edges)
	_, err := db.ExecContext(ctx, `
		UPDATE scheduler_workflows
		SET name = ?, description = ?, schedule = ?, enabled = ?, nodes = ?, edges = ?,
			concurrency_policy = ?, failure_policy = ?, updated_at = ?
		WHERE id = ?
	`, workflow.Name, workflow.Description, workflow.Schedule, workflow.Enabled, string(nodesJSON), string(edgesJSON),
		workflow.ConcurrencyPolicy, workflow.FailurePolicy, time.Now().Unix(), workflow.ID)
	if err != nil {
		return Workflow{}, fmt.Errorf("update workflow: %w", err)
	}
	updated, _, err := findWorkflow(ctx, db, workflow.ID)
	return updated, err
}

func createWorkflowRun(ctx context.Context, db *sql.DB, workflow Workflow, triggerType string, start int64) (int64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO scheduler_workflow_runs (workflow_id, workflow_name, trigger_type, status, start_time, created_at)
		VALUES (?, ?, ?, 'running', ?, ?)
	`, workflow.ID, workflow.Name, triggerType, start, start)
	if err != nil {
		return 0, fmt.Errorf("create workflow run: %w", err)
	}
	return result.LastInsertId()
}

func insertNodeRun(ctx context.Context, db *sql.DB, runID int64, node WorkflowNode, status, output string, start, end int64) error {
	duration := end - start
	var taskID interface{}
	if node.TaskID != 0 {
		taskID = node.TaskID
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO scheduler_node_runs (run_id, node_id, node_name, task_id, status, output, start_time, end_time, duration)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, runID, node.ID, node.Name, taskID, status, output, start, end, duration)
	return err
}

func loadRuns(ctx context.Context, db *sql.DB, limit int) ([]WorkflowRun, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, workflow_id, workflow_name, trigger_type, status, start_time, end_time, duration, summary, created_at
		FROM scheduler_workflow_runs
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("load workflow runs: %w", err)
	}
	defer rows.Close()
	runs := []WorkflowRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func findRun(ctx context.Context, db *sql.DB, id int64) (WorkflowRun, bool, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, workflow_id, workflow_name, trigger_type, status, start_time, end_time, duration, summary, created_at
		FROM scheduler_workflow_runs
		WHERE id = ?
	`, id)
	run, err := scanRun(row)
	if err == sql.ErrNoRows {
		return WorkflowRun{}, false, nil
	}
	if err != nil {
		return WorkflowRun{}, false, err
	}
	nodeRuns, err := loadNodeRuns(ctx, db, run.ID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	run.NodeRuns = nodeRuns
	if run.WorkflowID != nil {
		if workflow, ok, err := findWorkflow(ctx, db, *run.WorkflowID); err == nil && ok {
			run.Workflow = &workflow
		}
	}
	return run, true, nil
}

func scanRun(scanner taskScanner) (WorkflowRun, error) {
	var run WorkflowRun
	var workflowID, start, end, duration sql.NullInt64
	err := scanner.Scan(&run.ID, &workflowID, &run.WorkflowName, &run.TriggerType, &run.Status, &start, &end, &duration, &run.Summary, &run.CreatedAt)
	if err != nil {
		return WorkflowRun{}, err
	}
	run.WorkflowID = int64Ptr(workflowID)
	run.StartTime = int64Ptr(start)
	run.EndTime = int64Ptr(end)
	run.Duration = int64Ptr(duration)
	return run, nil
}

func loadNodeRuns(ctx context.Context, db *sql.DB, runID int64) ([]NodeRun, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, node_id, node_name, task_id, status, output, start_time, end_time, duration
		FROM scheduler_node_runs
		WHERE run_id = ?
		ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("load node runs: %w", err)
	}
	defer rows.Close()
	result := []NodeRun{}
	for rows.Next() {
		var nodeRun NodeRun
		var taskID, start, end, duration sql.NullInt64
		if err := rows.Scan(&nodeRun.ID, &nodeRun.RunID, &nodeRun.NodeID, &nodeRun.NodeName, &taskID, &nodeRun.Status, &nodeRun.Output, &start, &end, &duration); err != nil {
			return nil, err
		}
		nodeRun.TaskID = int64Ptr(taskID)
		nodeRun.StartTime = int64Ptr(start)
		nodeRun.EndTime = int64Ptr(end)
		nodeRun.Duration = int64Ptr(duration)
		result = append(result, nodeRun)
	}
	return result, rows.Err()
}

func loadAgentNodes(ctx context.Context, db *sql.DB) ([]SchedulerNode, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, status, tags, last_check_time
		FROM server_accounts
		ORDER BY order_index ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []SchedulerNode{}
	for rows.Next() {
		var node SchedulerNode
		var status string
		var tags sql.NullString
		var lastCheck sql.NullString
		if err := rows.Scan(&node.ID, &node.Name, &status, &tags, &lastCheck); err != nil {
			return nil, err
		}
		node.Kind = "agent"
		node.Status = status
		node.Labels = parseJSONStringList(tags.String)
		node.MaxConcurrency = 2
		node.ActiveRuns = 0
		node.CapabilityNote = "可作为分布式调度目标；执行能力取决于 Agent 连接状态"
		if lastCheck.Valid {
			if parsed, err := time.Parse("2006-01-02 15:04:05", lastCheck.String); err == nil {
				ts := parsed.Unix()
				node.LastHeartbeat = &ts
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func parseJSONStringList(value string) []string {
	if value == "" {
		return []string{}
	}
	var list []string
	if err := json.Unmarshal([]byte(value), &list); err == nil && list != nil {
		return list
	}
	return []string{}
}

