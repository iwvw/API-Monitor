package reconcilequeue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestQueueCoalescesNewGenerationWhileClaimed(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE managed_proxy_nodes(id TEXT PRIMARY KEY,apply_status TEXT,stats_port INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := EnqueueNodes(ctx, db, []string{"node"}, "first"); err != nil {
		t.Fatal(err)
	}
	job, ok, err := Claim(ctx, db, time.Now().UTC())
	if err != nil || !ok || job.Generation != 1 {
		t.Fatalf("job=%#v ok=%v err=%v", job, ok, err)
	}
	if err := EnqueueNodes(ctx, db, []string{"node"}, "second"); err != nil {
		t.Fatal(err)
	}
	if err := Complete(ctx, db, job); err != nil {
		t.Fatal(err)
	}
	job, ok, err = Claim(ctx, db, time.Now().UTC())
	if err != nil || !ok || job.Generation != 2 || job.Reason != "second" {
		t.Fatalf("second job=%#v ok=%v err=%v", job, ok, err)
	}
}

func TestNodeIDsForPlanSkipsDisabledManagedNodes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE subscription_plans(id TEXT PRIMARY KEY,selection_mode TEXT,include_internal_nodes INTEGER)`,
		`CREATE TABLE managed_proxy_nodes(id TEXT PRIMARY KEY,enabled INTEGER)`,
		`CREATE TABLE subscription_plan_nodes(plan_id TEXT,node_id TEXT,source TEXT)`,
		`INSERT INTO subscription_plans VALUES('plan','all',1)`,
		`INSERT INTO managed_proxy_nodes VALUES('enabled',1),('disabled',0)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := NodeIDsForPlan(context.Background(), db, "plan")
	if err != nil || len(ids) != 1 || ids[0] != "enabled" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
}

func TestNodeIDsForSubscriptionProfileRespectsToggle(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE subscription_subscriptions(id TEXT PRIMARY KEY,plan_id TEXT,profile_id TEXT)`,
		`CREATE TABLE subscription_profiles(id TEXT PRIMARY KEY,selection_mode TEXT,include_internal_nodes INTEGER)`,
		`CREATE TABLE managed_proxy_nodes(id TEXT PRIMARY KEY,enabled INTEGER)`,
		`CREATE TABLE subscription_plan_nodes(plan_id TEXT,node_id TEXT,source TEXT)`,
		`INSERT INTO subscription_subscriptions VALUES('sub','','profile')`,
		`INSERT INTO subscription_profiles VALUES('profile','all',1)`,
		`INSERT INTO managed_proxy_nodes VALUES('node',1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// 开关开启时返回内部节点
	ids, err := NodeIDsForSubscription(context.Background(), db, "sub")
	if err != nil || len(ids) != 1 || ids[0] != "node" {
		t.Fatalf("enabled profile ids=%v err=%v", ids, err)
	}
	// 开关关闭（默认）时不返回节点
	if _, err := db.Exec(`UPDATE subscription_profiles SET include_internal_nodes = 0 WHERE id = 'profile'`); err != nil {
		t.Fatal(err)
	}
	ids, err = NodeIDsForSubscription(context.Background(), db, "sub")
	if err != nil || len(ids) != 0 {
		t.Fatalf("disabled profile ids=%v err=%v", ids, err)
	}
}

func TestEnsureSchemaRecoversInterruptedJobs(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_runtime_reconcile(node_id,generation,state,last_error) VALUES('node',3,'running','')`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var state, nextAttempt, lastError string
	if err := db.QueryRow(`SELECT state,next_attempt_at,last_error FROM subscription_runtime_reconcile WHERE node_id='node'`).Scan(&state, &nextAttempt, &lastError); err != nil {
		t.Fatal(err)
	}
	if state != "retry" || nextAttempt != "" || lastError == "" {
		t.Fatalf("recovered job = state:%q next:%q error:%q", state, nextAttempt, lastError)
	}
	job, ok, err := Claim(context.Background(), db, time.Now().UTC())
	if err != nil || !ok || job.NodeID != "node" || job.Generation != 3 {
		t.Fatalf("claim recovered job=%#v ok=%v err=%v", job, ok, err)
	}
}
