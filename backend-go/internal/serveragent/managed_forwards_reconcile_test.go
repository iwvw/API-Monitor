package serveragent

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

func registerForwarderMock(t *testing.T, service *Service, id string, calls *int) {
	t.Helper()
	var mu sync.Mutex
	conn := service.registry.Register(id, &taskReplySocket{
		t:       t,
		service: service,
		reply: func(int, string) string {
			mu.Lock()
			if calls != nil {
				*calls++
			}
			mu.Unlock()
			return "ok"
		},
	})
	conn.UpdateCapabilities(map[string]bool{"tcp_forwarder_v1": true})
	conn.SetMetadata("platform", "linux")
	conn.SetMetadata("arch", "amd64")
}

func seedRunningTCPRelayForward(t *testing.T, db *sql.DB, id, source, relay string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES(?,?,?,?,?)`, source, source+"名", "127.0.0.1", "u", "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES(?,?,?,?,?)`, relay, relay+"名", "192.0.2.10", "u", "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,relay_server_id,access_mode,desired_status,apply_status) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, id, source, "127.0.0.1", 18081, "http", "tcp_relay", relay, "public", "running", "running"); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileRunningForwardsReappliesTCPRelayLink(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	var srcCalls, relayCalls int
	registerForwarderMock(t, service, "src-reconcile", &srcCalls)
	registerForwarderMock(t, service, "relay-reconcile", &relayCalls)
	seedRunningTCPRelayForward(t, db, "fwd-reconcile-1", "src-reconcile", "relay-reconcile")

	service.reconcileRunningForwards(ctx, db, "src-reconcile")

	if srcCalls == 0 {
		t.Fatalf("expected source agent to receive a bridge task (install), got %d", srcCalls)
	}
	if relayCalls == 0 {
		t.Fatalf("expected relay agent to receive listen tasks, got %d", relayCalls)
	}
	var port, status string
	if err := db.QueryRow(`SELECT COALESCE(remote_port,''),apply_status FROM managed_forwards WHERE id='fwd-reconcile-1'`).Scan(&port, &status); err != nil {
		t.Fatal(err)
	}
	if status != "running" || port == "" || port == "0" {
		t.Fatalf("expected running with allocated relay port, got status=%q port=%q", status, port)
	}
}

func TestReconcileRunningForwardsThrottlesWithinWindow(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()

	var srcCalls int
	registerForwarderMock(t, service, "src-throttle", &srcCalls)
	registerForwarderMock(t, service, "relay-throttle", nil)
	seedRunningTCPRelayForward(t, db, "fwd-throttle-1", "src-throttle", "relay-throttle")

	service.reconcileRunningForwards(ctx, db, "src-throttle")
	service.reconcileRunningForwards(ctx, db, "src-throttle")

	if srcCalls != 1 {
		t.Fatalf("expected throttled single source deploy, got %d", srcCalls)
	}
}
