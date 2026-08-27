package serveragent

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
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

func TestForwardPanelProxyCloudflareTunnelGoesThroughAgent(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('cf-src','源','127.0.0.1','u','password')`); err != nil {
		t.Fatal(err)
	}
	cipher, err := secure.SecureEncrypt("panel-token-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,access_mode,auth_proxy_port,access_token,desired_status,apply_status) VALUES('cf-panel-f','p','cf-src','127.0.0.1',18081,'http','cloudflare_tunnel','panel',12345,?,'running','running')`, cipher); err != nil {
		t.Fatal(err)
	}
	conn := service.registry.Register("cf-src", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(_ int, data string) string {
			if !strings.Contains(data, `"operation":"http_proxy"`) {
				t.Errorf("expected http_proxy payload, got %s", data)
			}
			if !strings.Contains(data, `"auth_proxy_port":12345`) {
				t.Errorf("expected auth_proxy_port in payload, got %s", data)
			}
			return `{"status":200,"headers":{"content-type":"text/html"},"body":"<h1>panel-ok</h1>"}`
		},
	})
	conn.UpdateCapabilities(map[string]bool{"tcp_forwarder_v1": true})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/server/forward/cf-panel-f/panel/proxy/", nil)
	service.handleForwardPanelProxy(rec, req, db, "cf-panel-f", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "panel-ok") {
		t.Fatalf("expected proxied body, got %s", rec.Body.String())
	}
}
