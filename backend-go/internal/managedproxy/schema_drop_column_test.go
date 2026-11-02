package managedproxy

import (
	"context"
	"testing"
)

// 旧表同时满足「有 stable 列」+「协议约束是旧的」：重建会丢掉 stable，
// 之后按陈旧快照再 DROP COLUMN 会报错。
func TestStaleColumnSnapshotAfterRebuild(t *testing.T) {
	db := schemaTestDB(t, `CREATE TABLE managed_proxy_nodes (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL,
		name TEXT NOT NULL,
		protocol TEXT NOT NULL CHECK(protocol IN ('vless-reality', 'hysteria2', 'vless-ws-tunnel')),
		runtime TEXT NOT NULL DEFAULT 'sing-box',
		public_host TEXT NOT NULL,
		assigned_port INTEGER NOT NULL DEFAULT 0,
		transport TEXT NOT NULL,
		config_encrypted TEXT NOT NULL,
		client_uri_encrypted TEXT NOT NULL,
		revision INTEGER NOT NULL DEFAULT 1,
		enabled INTEGER NOT NULL DEFAULT 1,
		stable INTEGER NOT NULL DEFAULT 0,
		publishable INTEGER NOT NULL DEFAULT 0,
		apply_status TEXT NOT NULL DEFAULT 'pending',
		last_error TEXT NOT NULL DEFAULT ''
	)`)
	if err := EnsureNodeColumns(context.Background(), db); err != nil {
		t.Fatalf("EnsureNodeColumns on legacy table with stable: %v", err)
	}
	cols := tableColumnNames(t, db)
	if cols["stable"] {
		t.Error("stable column should be dropped")
	}
}
