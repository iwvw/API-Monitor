package serveragent

import (
	"context"
	"sync"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
)

func TestValidTunnelHostname(t *testing.T) {
	for _, ok := range []string{"api.085014.xyz", "fwd-a.example.com", "a-b.example.co.uk", "x.io", "a.a"} {
		if !validTunnelHostname(ok) {
			t.Fatalf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "..", "-bad.example.com", "bad-.example.com", "has space.example.com", "a/b.example.com", "ApP.example.com"} {
		if validTunnelHostname(bad) {
			t.Fatalf("%q should be rejected", bad)
		}
	}
}

func TestForwardTunnelInstanceSlug(t *testing.T) {
	cases := map[string]string{
		"fwd_56a28e363cfe9cac": "fwd-56a28e363cfe9cac",
		"fwd.AbC_1":            "fwd-abc-1",
		"a b/c":                "a-b-c",
	}
	for in, want := range cases {
		if got := forwardTunnelInstance(in); got != want {
			t.Fatalf("forwardTunnelInstance(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMigrateLegacyWholeHostTunnels(t *testing.T) {
	_, db := testService(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host','ip','192.0.2.1','root','password')`,
		`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,apply_status) VALUES('host','acc','zone','085014.xyz','host-t','t','fwd-demo.085014.xyz','running')`,
		`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,whole_host,access_mode) VALUES('fwd_56a28e363cfe9cac','整域','host','127.0.0.1',3000,'http','cloudflare_tunnel',1,'public')`,
		`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,whole_host,access_mode,tunnel_hostname) VALUES('legacy-sub','子路径','host','127.0.0.1',3000,'http','cloudflare_tunnel',0,'public','existing.example.com')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("fixture %q: %v", stmt, err)
		}
	}
	if err := migrateLegacyWholeHostTunnels(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var hostname string
	if err := db.QueryRowContext(ctx, `SELECT tunnel_hostname FROM managed_forwards WHERE id='fwd_56a28e363cfe9cac'`).Scan(&hostname); err != nil {
		t.Fatal(err)
	}
	if hostname != "fwd-56a28e363cfe9cac.085014.xyz" {
		t.Fatalf("legacy whole-host domain = %q, want fwd-56a28e363cfe9cac.085014.xyz", hostname)
	}
	// 非整域规则不受迁移影响
	if err := db.QueryRowContext(ctx, `SELECT tunnel_hostname FROM managed_forwards WHERE id='legacy-sub'`).Scan(&hostname); err != nil {
		t.Fatal(err)
	}
	if hostname != "existing.example.com" {
		t.Fatalf("subpath tunnel_hostname changed to %q", hostname)
	}
}

type forwardTunnelFakeAPI struct {
	mu       sync.Mutex
	tunnelID string
	dnsID    string
	conns    int
	err      error
}

func (f *forwardTunnelFakeAPI) PreflightManagedTunnel(context.Context, string, string, string) (cloudflare.ManagedTunnelPreflight, error) {
	return cloudflare.ManagedTunnelPreflight{ZoneName: "085014.xyz"}, nil
}
func (f *forwardTunnelFakeAPI) CreateManagedTunnel(context.Context, string, string) (cloudflare.ManagedTunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloudflare.ManagedTunnel{ID: f.tunnelID, Name: "fwd"}, f.err
}
func (f *forwardTunnelFakeAPI) ManagedTunnelExists(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *forwardTunnelFakeAPI) ConfigureManagedTunnel(context.Context, string, string, []cloudflare.ManagedTunnelIngress) error {
	return nil
}
func (f *forwardTunnelFakeAPI) EnsureManagedTunnelDNS(context.Context, string, string, string, string) (cloudflare.ManagedTunnelDNS, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloudflare.ManagedTunnelDNS{ID: f.dnsID}, f.err
}
func (f *forwardTunnelFakeAPI) ManagedTunnelToken(context.Context, string, string) (string, error) {
	return "token", nil
}
func (f *forwardTunnelFakeAPI) ManagedTunnelConnections(context.Context, string, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns, f.err
}
func (f *forwardTunnelFakeAPI) DeleteManagedTunnelDNS(context.Context, string, string, string) error {
	return nil
}
func (f *forwardTunnelFakeAPI) DeleteManagedTunnel(context.Context, string, string) error { return nil }

func TestRunForwardTunnelDeployPublic(t *testing.T) {
	service, db := testService(t)
	fake := &forwardTunnelFakeAPI{tunnelID: "tun-1", dnsID: "dns-1", conns: 3}
	service.SetCloudflareTunnelManager(fake)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host','ip','192.0.2.1','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,apply_status) VALUES('host','acc','zone','085014.xyz','host-t','t','fwd-demo.085014.xyz','running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,tunnel_hostname,whole_host,access_mode,desired_status,apply_status) VALUES('fwdwh','整域','host','127.0.0.1',3000,'http','cloudflare_tunnel','api.085014.xyz',1,'public','running','pending')`); err != nil {
		t.Fatal(err)
	}
	service.registry.Register("host", &taskReplySocket{t: t, service: service, reply: func(int, string) string { return "" }})

	task := service.taskRegistry.Create("host", "proxy.tunnel.forward.deploy", "fwdwh")
	service.runForwardTunnelDeploy(task.ID, "fwdwh")

	var tunnelApply, applyStatus, tunnelID, dnsID string
	if err := db.QueryRow(`SELECT COALESCE(tunnel_apply_status,''),apply_status,COALESCE(tunnel_id,''),COALESCE(dns_record_id,'') FROM managed_forwards WHERE id='fwdwh'`).Scan(&tunnelApply, &applyStatus, &tunnelID, &dnsID); err != nil {
		t.Fatal(err)
	}
	if tunnelApply != "running" || applyStatus != "running" {
		t.Fatalf("deploy did not reach running: tunnel_apply=%q apply=%q", tunnelApply, applyStatus)
	}
	if tunnelID != "tun-1" || dnsID != "dns-1" {
		t.Fatalf("tunnel/dns not persisted: tunnel=%q dns=%q", tunnelID, dnsID)
	}
	var hostname string
	if err := db.QueryRow(`SELECT tunnel_hostname FROM managed_forwards WHERE id='fwdwh'`).Scan(&hostname); err != nil || hostname != "api.085014.xyz" {
		t.Fatalf("tunnel_hostname=%q err=%v", hostname, err)
	}
}

func TestRunForwardTunnelDeployMissingDomainFails(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host','ip','192.0.2.1','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,whole_host,access_mode,desired_status,apply_status) VALUES('fwdwh','整域','host','127.0.0.1',3000,'http','cloudflare_tunnel',1,'public','running','pending')`); err != nil {
		t.Fatal(err)
	}
	task := service.taskRegistry.Create("host", "proxy.tunnel.forward.deploy", "fwdwh")
	service.runForwardTunnelDeploy(task.ID, "fwdwh")
	var applyStatus string
	if err := db.QueryRow(`SELECT apply_status FROM managed_forwards WHERE id='fwdwh'`).Scan(&applyStatus); err != nil {
		t.Fatal(err)
	}
	if applyStatus != "failed" {
		t.Fatalf("expected failed deploy for missing domain, got %q", applyStatus)
	}
	var tunnelApply string
	if err := db.QueryRow(`SELECT tunnel_apply_status FROM managed_forwards WHERE id='fwdwh'`).Scan(&tunnelApply); err != nil || tunnelApply != "failed" {
		t.Fatalf("tunnel_apply_status=%q err=%v", tunnelApply, err)
	}
}
