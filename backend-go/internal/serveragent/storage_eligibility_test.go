package serveragent

import (
	"context"
	"net"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

func TestIsStorageNodeEligible(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		host     string
		platform string
		want     bool
	}{
		{
			name:     "Valid public Linux node",
			status:   "online",
			host:     "185.199.108.153",
			platform: "Linux",
			want:     true,
		},
		{
			name:     "Valid public domain Linux node",
			status:   "online",
			host:     "storage-node-1.example.com",
			platform: "Ubuntu 22.04 LTS",
			want:     true,
		},
		{
			name:     "Offline node rejected",
			status:   "offline",
			host:     "185.199.108.153",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Unknown status rejected",
			status:   "unknown",
			host:     "185.199.108.153",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Windows node rejected",
			status:   "online",
			host:     "185.199.108.153",
			platform: "Windows Server 2022",
			want:     false,
		},
		{
			name:     "Win32 platform rejected",
			status:   "online",
			host:     "185.199.108.153",
			platform: "win32",
			want:     false,
		},
		{
			name:     "Loopback IP rejected",
			status:   "online",
			host:     "127.0.0.1",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Private IP RFC1918 192.168 rejected",
			status:   "online",
			host:     "192.168.1.50",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Private IP RFC1918 10.x rejected",
			status:   "online",
			host:     "10.0.4.1",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Private IP RFC1918 172.16-31 rejected",
			status:   "online",
			host:     "172.20.0.1",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Localhost domain rejected",
			status:   "online",
			host:     "localhost",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Internal lan domain rejected",
			status:   "online",
			host:     "node.internal",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Unspecified 0.0.0.0 rejected",
			status:   "online",
			host:     "0.0.0.0",
			platform: "Linux",
			want:     false,
		},
		{
			name:     "Host with port stripped and validated",
			status:   "online",
			host:     "185.199.108.153:61208",
			platform: "Debian GNU/Linux",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStorageNodeEligible(tt.status, tt.host, tt.platform)
			if got != tt.want {
				t.Errorf("IsStorageNodeEligible(%q, %q, %q) = %v, want %v", tt.status, tt.host, tt.platform, got, tt.want)
			}
		})
	}
}

func TestIsIPPublic(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"172.16.0.1", false},
		{"169.254.1.1", false},
		{"224.0.0.1", false},
		{"::1", false},
		{"fe80::1", false},
		{"2606:4700:4700::1111", true},
	}

	for _, tt := range tests {
		parsed := net.ParseIP(tt.ip)
		if got := IsIPPublic(parsed); got != tt.want {
			t.Errorf("IsIPPublic(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestListEligibleStorageNodesDBFilter(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{DataDir: t.TempDir(), DBName: "eligibility_test.db"}
	store := database.New(cfg)
	db, err := store.Open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS server_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			status TEXT NOT NULL,
			cached_info TEXT,
			order_index INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS server_agent_credentials (
			server_id TEXT PRIMARY KEY,
			secret_encrypted TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		);
	`); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	// Insert nodes:
	// 1. Linux Online Public -> Eligible
	// 2. Windows Online Public -> Ineligible (Windows)
	// 3. Linux Offline Public -> Ineligible (Offline)
	// 4. Linux Online Private IP -> Ineligible (Private IP)
	_, err = db.ExecContext(ctx, `
		INSERT INTO server_accounts (id, name, host, status, cached_info, order_index) VALUES
		('node-linux-pub', 'Tokyo Edge', '185.200.1.1', 'online', '{"platform":"Linux"}', 1),
		('node-win-pub', 'Win Server', '185.200.1.2', 'online', '{"platform":"Windows"}', 2),
		('node-linux-off', 'Offline Node', '185.200.1.3', 'offline', '{"platform":"Linux"}', 3),
		('node-linux-priv', 'LAN Node', '192.168.1.10', 'online', '{"platform":"Linux"}', 4);
	`)
	if err != nil {
		t.Fatalf("insert test accounts: %v", err)
	}

	service := &Service{
		store:    store,
		registry: NewConnectionRegistry(),
	}
	// simulate node-linux-pub is in registry with metadata
	conn := service.registry.Register("node-linux-pub", nil)
	conn.SetMetadata("platform", "Linux")

	nodes, err := service.ListEligibleStorageNodes(ctx)
	if err != nil {
		t.Fatalf("ListEligibleStorageNodes failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 eligible node, got %d: %#v", len(nodes), nodes)
	}
	if nodes[0].ID != "node-linux-pub" {
		t.Fatalf("expected node-linux-pub, got %s", nodes[0].ID)
	}

	// Test GetEligibleStorageNode
	target, err := service.GetEligibleStorageNode(ctx, "node-linux-pub")
	if err != nil {
		t.Fatalf("GetEligibleStorageNode failed for eligible node: %v", err)
	}
	if target.Host != "185.200.1.1" {
		t.Fatalf("expected host 185.200.1.1, got %s", target.Host)
	}

	// GetEligibleStorageNode on Windows should fail
	if _, err := service.GetEligibleStorageNode(ctx, "node-win-pub"); err == nil {
		t.Fatal("expected error for Windows node, got nil")
	}

	// GetEligibleStorageNode on Offline should fail
	if _, err := service.GetEligibleStorageNode(ctx, "node-linux-off"); err == nil {
		t.Fatal("expected error for offline node, got nil")
	}

	// GetEligibleStorageNode on Private IP should fail
	if _, err := service.GetEligibleStorageNode(ctx, "node-linux-priv"); err == nil {
		t.Fatal("expected error for private IP node, got nil")
	}
}
