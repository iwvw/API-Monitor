package serveragent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type fakeManagedTunnelAPI struct {
	mu     sync.Mutex
	count  int
	err    error
	exists map[string]bool
}

func (f *fakeManagedTunnelAPI) PreflightManagedTunnel(context.Context, string, string, string) (cloudflare.ManagedTunnelPreflight, error) {
	return cloudflare.ManagedTunnelPreflight{}, nil
}

func (f *fakeManagedTunnelAPI) CreateManagedTunnel(context.Context, string, string) (cloudflare.ManagedTunnel, error) {
	return cloudflare.ManagedTunnel{}, nil
}

func (f *fakeManagedTunnelAPI) ManagedTunnelExists(_ context.Context, _, tunnelID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exists == nil {
		return tunnelID != "", nil
	}
	exists, ok := f.exists[tunnelID]
	return exists || !ok, nil
}

func (f *fakeManagedTunnelAPI) ConfigureManagedTunnel(context.Context, string, string, []cloudflare.ManagedTunnelIngress) error {
	return nil
}

func (f *fakeManagedTunnelAPI) EnsureManagedTunnelDNS(context.Context, string, string, string, string) (cloudflare.ManagedTunnelDNS, error) {
	return cloudflare.ManagedTunnelDNS{}, nil
}

func (f *fakeManagedTunnelAPI) ManagedTunnelToken(context.Context, string, string) (string, error) {
	return "token", nil
}

func (f *fakeManagedTunnelAPI) ManagedTunnelConnections(context.Context, string, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

func (f *fakeManagedTunnelAPI) DeleteManagedTunnelDNS(context.Context, string, string, string) error {
	return nil
}

func (f *fakeManagedTunnelAPI) DeleteManagedTunnel(context.Context, string, string) error {
	return nil
}

func (f *fakeManagedTunnelAPI) setConnections(count int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count, f.err = count, err
}

func TestReconcileManagedTunnelConnectionReflectsRealConnectivity(t *testing.T) {
	service, db := testService(t)
	service.tunnelHealthCheckAttempts = 1
	service.tunnelHealthCheckDelay = 0
	fake := &fakeManagedTunnelAPI{}
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('tunnel-host','隧道主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}

	insertTunnel := func(status string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,last_stage,last_error) VALUES('tunnel-host','account','zone','example.com','tunnel-id','t','edge.example.com','running',?,?,?) ON CONFLICT(server_id) DO UPDATE SET apply_status=excluded.apply_status,last_stage=excluded.last_stage,last_error=excluded.last_error`, status, "seed", ""); err != nil {
			t.Fatal(err)
		}
	}
	applyStatus := func() string {
		t.Helper()
		var status string
		if err := db.QueryRow(`SELECT apply_status FROM managed_proxy_tunnels WHERE server_id='tunnel-host'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	insertTunnel("running")

	fake.setConnections(0, nil)
	service.reconcileManagedTunnelConnection("tunnel-host")
	if got := applyStatus(); got != "disconnected" {
		t.Fatalf("expected disconnected when Cloudflare reports no connectors, got %q", got)
	}

	fake.setConnections(3, nil)
	service.reconcileManagedTunnelConnection("tunnel-host")
	if got := applyStatus(); got != "running" {
		t.Fatalf("expected running when Cloudflare reports active connectors, got %q", got)
	}

	fake.setConnections(0, nil)
	service.reconcileManagedTunnelConnection("tunnel-host")
	if got := applyStatus(); got != "disconnected" {
		t.Fatalf("expected disconnected again after connectors drop, got %q", got)
	}
}

func TestReconcileManagedTunnelConnectionKeepsStatusOnAPIError(t *testing.T) {
	service, db := testService(t)
	service.tunnelHealthCheckAttempts = 1
	service.tunnelHealthCheckDelay = 0
	fake := &fakeManagedTunnelAPI{}
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('error-host','错误主机','192.0.2.11','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,last_stage) VALUES('error-host','account','zone','example.com','tunnel-id','t','edge.example.com','running','running','health_check')`); err != nil {
		t.Fatal(err)
	}
	fake.setConnections(0, context.DeadlineExceeded)
	service.reconcileManagedTunnelConnection("error-host")
	var status string
	if err := db.QueryRow(`SELECT apply_status FROM managed_proxy_tunnels WHERE server_id='error-host'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("tunnel status must not be flipped on a control-plane API error, got %q", status)
	}
}

func TestAttemptTunnelSelfHealSkipsWhenAgentOffline(t *testing.T) {
	service, db := testService(t)
	service.tunnelReconcileMaxAttempts = 3
	fake := &fakeManagedTunnelAPI{}
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('heal-host','自愈主机','192.0.2.20','root','password')`); err != nil {
		t.Fatal(err)
	}
	encrypted, err := secure.SecureEncrypt(strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,token_encrypted) VALUES('heal-host','account','zone','example.com','tunnel-id','t','edge.example.com','running','disconnected',?)`, encrypted); err != nil {
		t.Fatal(err)
	}
	service.attemptTunnelSelfHeal("heal-host")
	var attempts int
	var applyStatus string
	if err := db.QueryRow(`SELECT reconcile_attempts,apply_status FROM managed_proxy_tunnels WHERE server_id='heal-host'`).Scan(&attempts, &applyStatus); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || applyStatus != "disconnected" {
		t.Fatalf("offline agent must not be replayed: attempts=%d status=%q", attempts, applyStatus)
	}
}

func TestAttemptTunnelSelfHealSkipsAtMaxAttempts(t *testing.T) {
	service, db := testService(t)
	service.tunnelReconcileMaxAttempts = 3
	fake := &fakeManagedTunnelAPI{}
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('heal-host','自愈主机','192.0.2.20','root','password')`); err != nil {
		t.Fatal(err)
	}
	encrypted, err := secure.SecureEncrypt(strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,token_encrypted,reconcile_attempts) VALUES('heal-host','account','zone','example.com','tunnel-id','t','edge.example.com','running','disconnected',?,3)`, encrypted); err != nil {
		t.Fatal(err)
	}
	conn := service.registry.Register("heal-host", &taskReplySocket{t: t, service: service, reply: func(int, string) string {
		t.Fatal("self-heal must not send a task when max attempts is reached")
		return ""
	}})
	conn.UpdateCapabilities(map[string]bool{"cloudflared_runtime_v1": true})
	service.attemptTunnelSelfHeal("heal-host")
	var lastError string
	if err := db.QueryRow(`SELECT last_error FROM managed_proxy_tunnels WHERE server_id='heal-host'`).Scan(&lastError); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastError, "自愈重试已达上限") {
		t.Fatalf("expected max attempts error, got %q", lastError)
	}
}

func TestAttemptTunnelSelfHealReinstallsAndRecovers(t *testing.T) {
	service, db := testService(t)
	service.tunnelReconcileMaxAttempts = 3
	service.tunnelHealthCheckAttempts = 1
	service.tunnelHealthCheckDelay = 0
	fake := &fakeManagedTunnelAPI{}
	fake.setConnections(1, nil)
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('heal-host','自愈主机','192.0.2.20','root','password')`); err != nil {
		t.Fatal(err)
	}
	encrypted, err := secure.SecureEncrypt(strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,token_encrypted) VALUES('heal-host','account','zone','example.com','tunnel-id','t','edge.example.com','running','disconnected',?)`, encrypted); err != nil {
		t.Fatal(err)
	}
	taskSent := false
	conn := service.registry.Register("heal-host", &taskReplySocket{t: t, service: service, reply: func(taskType int, data string) string {
		if taskType != 51 {
			t.Fatalf("expected cloudflared task type 51, got %d", taskType)
		}
		taskSent = true
		return `{"status":"running"}`
	}})
	conn.UpdateCapabilities(map[string]bool{"cloudflared_runtime_v1": true})
	service.attemptTunnelSelfHeal("heal-host")
	if !taskSent {
		t.Fatal("expected cloudflared install task to be sent during self-heal")
	}
	var applyStatus string
	var attempts int
	if err := db.QueryRow(`SELECT apply_status,reconcile_attempts FROM managed_proxy_tunnels WHERE server_id='heal-host'`).Scan(&applyStatus, &attempts); err != nil {
		t.Fatal(err)
	}
	if applyStatus != "running" {
		t.Fatalf("expected running after self-heal, got %q", applyStatus)
	}
	if attempts != 0 {
		t.Fatalf("expected attempts reset to 0 after recovery, got %d", attempts)
	}
}

func TestAttemptTunnelSelfHealKeepsDisconnectedWhenNotRecovered(t *testing.T) {
	service, db := testService(t)
	service.tunnelReconcileMaxAttempts = 3
	service.tunnelHealthCheckAttempts = 1
	service.tunnelHealthCheckDelay = 0
	fake := &fakeManagedTunnelAPI{}
	fake.setConnections(0, nil)
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('heal-host','自愈主机','192.0.2.20','root','password')`); err != nil {
		t.Fatal(err)
	}
	encrypted, err := secure.SecureEncrypt(strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,desired_status,apply_status,token_encrypted) VALUES('heal-host','account','zone','example.com','tunnel-id','t','edge.example.com','running','disconnected',?)`, encrypted); err != nil {
		t.Fatal(err)
	}
	conn := service.registry.Register("heal-host", &taskReplySocket{t: t, service: service, reply: func(int, string) string {
		return `{"status":"running"}`
	}})
	conn.UpdateCapabilities(map[string]bool{"cloudflared_runtime_v1": true})
	service.attemptTunnelSelfHeal("heal-host")
	var applyStatus string
	var attempts int
	if err := db.QueryRow(`SELECT apply_status,reconcile_attempts FROM managed_proxy_tunnels WHERE server_id='heal-host'`).Scan(&applyStatus, &attempts); err != nil {
		t.Fatal(err)
	}
	if applyStatus != "disconnected" {
		t.Fatalf("expected disconnected when Cloudflare still reports no connectors, got %q", applyStatus)
	}
	if attempts != 1 {
		t.Fatalf("expected attempts incremented to 1, got %d", attempts)
	}
}
