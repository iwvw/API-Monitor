package reconcilequeue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Executor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

type Queryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

type Job struct {
	NodeID     string
	Generation int64
	Attempts   int
	Reason     string
}

func EnsureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_runtime_reconcile (
			node_id TEXT PRIMARY KEY,
			generation INTEGER NOT NULL DEFAULT 1,
			applied_generation INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','retry','complete')),
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_runtime_reconcile_ready ON subscription_runtime_reconcile(state,next_attempt_at,updated_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure runtime reconcile queue: %w", err)
		}
	}
	// A process crash can leave a job in `running` after the Agent task has
	// disappeared. Treat those claims as recoverable on the next startup;
	// generation coalescing still prevents an older apply from overwriting a
	// newer desired state.
	if _, err := db.ExecContext(ctx, `UPDATE subscription_runtime_reconcile
		SET state='retry',next_attempt_at='',last_error=CASE WHEN last_error='' THEN 'backend restarted before reconciliation completed' ELSE last_error END,
		updated_at=datetime('now') WHERE state='running'`); err != nil {
		return fmt.Errorf("recover interrupted runtime reconciliation: %w", err)
	}
	return nil
}

func EnqueueNodes(ctx context.Context, executor Executor, nodeIDs []string, reason string) error {
	seen := map[string]struct{}{}
	for _, raw := range nodeIDs {
		nodeID := strings.TrimSpace(raw)
		if nodeID == "" {
			continue
		}
		if _, exists := seen[nodeID]; exists {
			continue
		}
		seen[nodeID] = struct{}{}
		if _, err := executor.ExecContext(ctx, `INSERT INTO subscription_runtime_reconcile
			(node_id,generation,applied_generation,state,attempts,next_attempt_at,reason,last_error,updated_at)
			VALUES(?,1,0,'pending',0,'',?,'',datetime('now'))
			ON CONFLICT(node_id) DO UPDATE SET
			generation=subscription_runtime_reconcile.generation+1,
			state='pending',attempts=0,next_attempt_at='',reason=excluded.reason,last_error='',updated_at=datetime('now')`, nodeID, strings.TrimSpace(reason)); err != nil {
			return fmt.Errorf("enqueue node %s: %w", nodeID, err)
		}
	}
	return nil
}

func NodeIDsForPlan(ctx context.Context, queryer Queryer, planID string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT n.id
		FROM managed_proxy_nodes n
		JOIN subscription_plans p ON p.id=?
		WHERE n.enabled=1 AND p.include_internal_nodes=1 AND (
			COALESCE(p.selection_mode,'explicit')='all' OR EXISTS(
				SELECT 1 FROM subscription_plan_nodes pn WHERE pn.plan_id=p.id AND pn.node_id=n.id AND pn.source='internal'
			)
		)
		ORDER BY n.id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NodeIDsForSubscription resolves managed node IDs for a subscription,
// supporting both plan-based and profile-based subscriptions.
func NodeIDsForSubscription(ctx context.Context, queryer Queryer, subscriptionID string) ([]string, error) {
	// Try plan-based first
	rows, err := queryer.QueryContext(ctx,
		`SELECT COALESCE(s.plan_id,''),COALESCE(s.profile_id,'') FROM subscription_subscriptions s WHERE s.id=?`,
		subscriptionID)
	if err != nil {
		return nil, err
	}
	var planID, profileID string
	if rows.Next() {
		rows.Scan(&planID, &profileID)
	}
	rows.Close()

	if planID != "" {
		return NodeIDsForPlan(ctx, queryer, planID)
	}
	// For profile-based subscriptions（include_internal_nodes/selection_mode 列
	// 由 subscription 模块 ensureSchema 补齐，默认关闭保持存量语义）
	if profileID != "" {
		nodeRows, err := queryer.QueryContext(ctx, `SELECT n.id
			FROM managed_proxy_nodes n
			JOIN subscription_profiles pf ON pf.id=?
			WHERE n.enabled=1 AND pf.include_internal_nodes=1 AND (
				COALESCE(pf.selection_mode,'explicit')='all' OR EXISTS(
					SELECT 1 FROM subscription_plan_nodes pn WHERE pn.plan_id=pf.id AND pn.node_id=n.id AND pn.source='internal'
				)
			)
			ORDER BY n.id`, profileID)
		if err != nil {
			return nil, err
		}
		defer nodeRows.Close()
		ids := []string{}
		for nodeRows.Next() {
			var id string
			if err := nodeRows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, nodeRows.Err()
	}
	return nil, nil
}

func NodeIDsForPlans(ctx context.Context, queryer Queryer, planIDs ...string) ([]string, error) {
	seen := map[string]struct{}{}
	ids := []string{}
	for _, planID := range planIDs {
		if strings.TrimSpace(planID) == "" {
			continue
		}
		items, err := NodeIDsForPlan(ctx, queryer, planID)
		if err != nil {
			return nil, err
		}
		for _, id := range items {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func Claim(ctx context.Context, db *sql.DB, now time.Time) (Job, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, err
	}
	defer tx.Rollback()
	var job Job
	err = tx.QueryRowContext(ctx, `SELECT node_id,generation,attempts,reason
		FROM subscription_runtime_reconcile
		WHERE state IN ('pending','retry') AND (next_attempt_at='' OR next_attempt_at<=?)
		ORDER BY updated_at,node_id LIMIT 1`, now.UTC().Format(time.RFC3339)).Scan(&job.NodeID, &job.Generation, &job.Attempts, &job.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE subscription_runtime_reconcile SET state='running',attempts=attempts+1,updated_at=datetime('now') WHERE node_id=? AND generation=? AND state IN ('pending','retry')`, job.NodeID, job.Generation)
	if err != nil {
		return Job{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Job{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, err
	}
	job.Attempts++
	return job, true, nil
}

func Complete(ctx context.Context, db *sql.DB, job Job) error {
	_, err := db.ExecContext(ctx, `UPDATE subscription_runtime_reconcile SET
		applied_generation=CASE WHEN applied_generation<? THEN ? ELSE applied_generation END,
		state=CASE WHEN generation>? THEN 'pending' ELSE 'complete' END,
		attempts=CASE WHEN generation>? THEN 0 ELSE attempts END,
		next_attempt_at='',last_error='',updated_at=datetime('now') WHERE node_id=?`, job.Generation, job.Generation, job.Generation, job.Generation, job.NodeID)
	return err
}

func Retry(ctx context.Context, db *sql.DB, job Job, cause error, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	delay := time.Duration(1<<min(job.Attempts, 8)) * time.Second
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	message := "runtime reconciliation failed"
	if cause != nil {
		message = cause.Error()
	}
	_, err := db.ExecContext(ctx, `UPDATE subscription_runtime_reconcile SET
		state=CASE WHEN generation>? THEN 'pending' ELSE 'retry' END,
		next_attempt_at=CASE WHEN generation>? THEN '' ELSE ? END,
		last_error=?,updated_at=datetime('now') WHERE node_id=?`, job.Generation, job.Generation, now.UTC().Add(delay).Format(time.RFC3339), message, job.NodeID)
	return err
}

func Status(ctx context.Context, db *sql.DB, nodeIDs []string) string {
	if len(nodeIDs) == 0 {
		return "not_required"
	}
	pending := 0
	for _, nodeID := range nodeIDs {
		var applyStatus string
		var statsPort int
		if err := db.QueryRowContext(ctx, `SELECT apply_status,COALESCE(stats_port,0) FROM managed_proxy_nodes WHERE id=?`, nodeID).Scan(&applyStatus, &statsPort); err != nil || applyStatus != "running" || statsPort == 0 {
			pending++
			continue
		}
		var state string
		var generation, applied int64
		err := db.QueryRowContext(ctx, `SELECT state,generation,applied_generation FROM subscription_runtime_reconcile WHERE node_id=?`, nodeID).Scan(&state, &generation, &applied)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil || state != "complete" || applied < generation {
			pending++
		}
	}
	if pending > 0 {
		return "pending"
	}
	return "synced"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
