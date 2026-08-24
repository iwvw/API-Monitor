package managedproxy

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

const legacyNodeTableDDL = `CREATE TABLE managed_proxy_nodes (
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
	publishable INTEGER NOT NULL DEFAULT 0,
	apply_status TEXT NOT NULL DEFAULT 'pending',
	last_error TEXT NOT NULL DEFAULT ''
)`

func schemaTestDB(t *testing.T, statements ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	base := []string{
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE server_accounts(id TEXT PRIMARY KEY, name TEXT, host TEXT)`,
	}
	for _, statement := range append(base, statements...) {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func tableColumnNames(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	columns, err := tableColumns(context.Background(), db, "managed_proxy_nodes")
	if err != nil {
		t.Fatal(err)
	}
	return columns
}

func TestEnsureNodeColumnsBackfillsLegacyTable(t *testing.T) {
	db := schemaTestDB(t, legacyNodeTableDDL)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host) VALUES('host','edge','192.0.2.1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('node','host','edge','vless-reality','sing-box','192.0.2.1',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNodeColumns(ctx, db); err != nil {
		t.Fatal(err)
	}
	columns := tableColumnNames(t, db)
	for _, migration := range nodeColumnMigrations {
		if !columns[migration.name] {
			t.Fatalf("column %s was not added", migration.name)
		}
	}
	var port int
	var name string
	if err := db.QueryRowContext(ctx, `SELECT assigned_port,name FROM managed_proxy_nodes WHERE id='node'`).Scan(&port, &name); err != nil || port != 45654 || name != "edge" {
		t.Fatalf("legacy row = port %d name %q err=%v", port, name, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('socks-node','host','socks','socks','sing-box','192.0.2.1',45655,'tcp','{}','')`); err != nil {
		t.Fatalf("widened CHECK should admit socks: %v", err)
	}
}

func TestEnsureNodeColumnsNoopOnCurrentSchema(t *testing.T) {
	db := schemaTestDB(t, NodeTableDDL)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host) VALUES('host','edge','192.0.2.1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('node','host','edge','hysteria2','sing-box','192.0.2.1',45654,'udp','{}','')`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNodeColumns(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_proxy_nodes`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count=%d err=%v", count, err)
	}
}

func TestEnsureNodeColumnsMissingTableFailsClearly(t *testing.T) {
	db := schemaTestDB(t)
	err := EnsureNodeColumns(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("EnsureNodeColumns on missing table err=%v, want missing table error", err)
	}
}

func TestRebuildNodeProtocolConstraintWidensLegacyCheck(t *testing.T) {
	db := schemaTestDB(t, legacyNodeTableDDL)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host) VALUES('host','edge','192.0.2.1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('node','host','edge','vless-reality','sing-box','192.0.2.1',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	ok, err := nodeProtocolConstraintOK(ctx, db)
	if err != nil || ok {
		t.Fatalf("legacy constraint ok=%v err=%v, want false", ok, err)
	}
	if err := rebuildNodeProtocolConstraint(ctx, db); err != nil {
		t.Fatal(err)
	}
	ok, err = nodeProtocolConstraintOK(ctx, db)
	if err != nil || !ok {
		t.Fatalf("widened constraint ok=%v err=%v, want true", ok, err)
	}
	var port int
	if err := db.QueryRowContext(ctx, `SELECT assigned_port FROM managed_proxy_nodes WHERE id='node'`).Scan(&port); err != nil || port != 45654 {
		t.Fatalf("row preserved port=%d err=%v", port, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('socks-node','host','socks','socks','sing-box','192.0.2.1',45655,'tcp','{}','')`); err != nil {
		t.Fatalf("widened CHECK should admit socks: %v", err)
	}
}

func TestRebuildNodeProtocolConstraintPreservesDependents(t *testing.T) {
	delegated := []string{
		legacyNodeTableDDL,
		`CREATE TABLE mpn_audit(server_id TEXT)`,
		`CREATE INDEX idx_mpn_protocol ON managed_proxy_nodes(protocol)`,
		`CREATE TRIGGER trg_mpn_audit AFTER INSERT ON managed_proxy_nodes BEGIN INSERT INTO mpn_audit(server_id) VALUES (NEW.server_id); END`,
	}
	db := schemaTestDB(t, delegated...)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host) VALUES('host','edge','192.0.2.1')`); err != nil {
		t.Fatal(err)
	}
	if err := rebuildNodeProtocolConstraint(ctx, db); err != nil {
		t.Fatal(err)
	}
	var indexCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_mpn_protocol' AND tbl_name='managed_proxy_nodes'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("index replay count=%d err=%v", indexCount, err)
	}
	var triggerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='trg_mpn_audit' AND tbl_name='managed_proxy_nodes'`).Scan(&triggerCount); err != nil || triggerCount != 1 {
		t.Fatalf("trigger replay count=%d err=%v", triggerCount, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('node','host','edge','http','sing-box','192.0.2.1',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	var audited string
	if err := db.QueryRowContext(ctx, `SELECT server_id FROM mpn_audit`).Scan(&audited); err != nil || audited != "host" {
		t.Fatalf("replayed trigger audit=%q err=%v", audited, err)
	}
}

func TestRebuildNodeProtocolConstraintNoopWhenAlreadyWidened(t *testing.T) {
	db := schemaTestDB(t, NodeTableDDL)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host) VALUES('host','edge','192.0.2.1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('node','host','edge','socks','sing-box','192.0.2.1',45654,'tcp','{}','')`); err != nil {
		t.Fatal(err)
	}
	if err := rebuildNodeProtocolConstraint(ctx, db); err != nil {
		t.Fatal(err)
	}
	ok, err := nodeProtocolConstraintOK(ctx, db)
	if err != nil || !ok {
		t.Fatalf("constraint ok=%v err=%v", ok, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_proxy_nodes`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("row count=%d err=%v", count, err)
	}
}

func TestRebuildNodeProtocolConstraintMissingTableIsNoop(t *testing.T) {
	db := schemaTestDB(t)
	if err := rebuildNodeProtocolConstraint(context.Background(), db); err != nil {
		t.Fatalf("rebuild on missing table: %v", err)
	}
}

func TestTableColumns(t *testing.T) {
	db := schemaTestDB(t, NodeTableDDL)
	columns, err := tableColumns(context.Background(), db, "managed_proxy_nodes")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"id", "server_id", "name", "protocol", "stats_port", "access_mode", "stable", "updated_at"} {
		if !columns[expected] {
			t.Fatalf("column %s missing: %v", expected, columns)
		}
	}
	if columns["missing_column"] {
		t.Fatal("unexpected column reported present")
	}
}

func TestNodeProtocolConstraintOKDetectsPartialWildcard(t *testing.T) {
	ddl := strings.Replace(legacyNodeTableDDL, "'vless-ws-tunnel'", "'vless-ws-tunnel', 'socks'", 1)
	db := schemaTestDB(t, ddl)
	ok, err := nodeProtocolConstraintOK(context.Background(), db)
	if err != nil || ok {
		t.Fatalf("partial wildcard constraint ok=%v err=%v, want false", ok, err)
	}
}
