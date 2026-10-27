package managedproxy

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// NodeTableDDL is shared by the subscription control plane and the Agent
// runtime service. Keeping one canonical definition prevents startup order
// from silently producing different managed-node schemas.
// indexNodeProtocols are the agent-rendered node protocols a managed inbound
// can assume. SOCKS (socks5) and HTTP inbounds are plaintext transport; they
// are still individually metered because sing-box populates metadata.User for
// authenticated users on every inbound type.
var indexNodeProtocols = []string{"vless-reality", "hysteria2", "vless-ws-tunnel", "socks", "http"}

const NodeTableDDL = `CREATE TABLE IF NOT EXISTS managed_proxy_nodes (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL,
	name TEXT NOT NULL,
	protocol TEXT NOT NULL CHECK(protocol IN ('vless-reality', 'hysteria2', 'vless-ws-tunnel', 'socks', 'http')),
	runtime TEXT NOT NULL DEFAULT 'sing-box',
	public_host TEXT NOT NULL,
	assigned_port INTEGER NOT NULL DEFAULT 0,
	stats_port INTEGER NOT NULL DEFAULT 0,
	transport TEXT NOT NULL CHECK(transport IN ('tcp', 'udp')),
	config_encrypted TEXT NOT NULL,
	client_uri_encrypted TEXT NOT NULL,
	revision INTEGER NOT NULL DEFAULT 1,
	enabled INTEGER NOT NULL DEFAULT 1,
	publishable INTEGER NOT NULL DEFAULT 0,
	apply_status TEXT NOT NULL DEFAULT 'pending',
	last_error TEXT NOT NULL DEFAULT '',
	observed_status TEXT NOT NULL DEFAULT 'unknown',
	observed_revision INTEGER NOT NULL DEFAULT 0,
	observed_port INTEGER NOT NULL DEFAULT 0,
	observed_at TEXT,
	health_status TEXT NOT NULL DEFAULT 'unknown',
	access_mode TEXT NOT NULL DEFAULT 'direct',
	tunnel_path TEXT NOT NULL DEFAULT '',
	preferred_address_id TEXT NOT NULL DEFAULT '',
	connect_address TEXT NOT NULL DEFAULT '',
	connect_port INTEGER NOT NULL DEFAULT 0,
	tunnel_hostname TEXT NOT NULL DEFAULT '',
	created_at TEXT DEFAULT (datetime('now')),
	updated_at TEXT DEFAULT (datetime('now')),
	FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
)`

var nodeColumnMigrations = []struct {
	name string
	sql  string
}{
	{"stats_port", "ALTER TABLE managed_proxy_nodes ADD COLUMN stats_port INTEGER NOT NULL DEFAULT 0"},
	{"access_mode", "ALTER TABLE managed_proxy_nodes ADD COLUMN access_mode TEXT NOT NULL DEFAULT 'direct'"},
	{"tunnel_path", "ALTER TABLE managed_proxy_nodes ADD COLUMN tunnel_path TEXT NOT NULL DEFAULT ''"},
	{"preferred_address_id", "ALTER TABLE managed_proxy_nodes ADD COLUMN preferred_address_id TEXT NOT NULL DEFAULT ''"},
	{"connect_address", "ALTER TABLE managed_proxy_nodes ADD COLUMN connect_address TEXT NOT NULL DEFAULT ''"},
	{"connect_port", "ALTER TABLE managed_proxy_nodes ADD COLUMN connect_port INTEGER NOT NULL DEFAULT 0"},
	{"tunnel_hostname", "ALTER TABLE managed_proxy_nodes ADD COLUMN tunnel_hostname TEXT NOT NULL DEFAULT ''"},
	{"observed_status", "ALTER TABLE managed_proxy_nodes ADD COLUMN observed_status TEXT NOT NULL DEFAULT 'unknown'"},
	{"observed_revision", "ALTER TABLE managed_proxy_nodes ADD COLUMN observed_revision INTEGER NOT NULL DEFAULT 0"},
	{"observed_port", "ALTER TABLE managed_proxy_nodes ADD COLUMN observed_port INTEGER NOT NULL DEFAULT 0"},
	{"observed_at", "ALTER TABLE managed_proxy_nodes ADD COLUMN observed_at TEXT"},
	{"health_status", "ALTER TABLE managed_proxy_nodes ADD COLUMN health_status TEXT NOT NULL DEFAULT 'unknown'"},
}

func EnsureNodeColumns(ctx context.Context, db *sql.DB) error {
	columns, err := tableColumns(ctx, db, "managed_proxy_nodes")
	if err != nil {
		return err
	}
	for _, migration := range nodeColumnMigrations {
		if columns[migration.name] {
			continue
		}
		if _, err := db.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("migrate managed proxy node %s: %w", migration.name, err)
		}
		columns[migration.name] = true
	}
	if err := rebuildNodeProtocolConstraint(ctx, db); err != nil {
		return err
	}
	// 稳定节点标记已下线：清理历史库中残留的列。重建表会按新 DDL 建表，
	// 因此这里必须重新读取列，不能复用重建前的陈旧快照（否则会因
	// 「no such column: stable」让启动直接失败）。
	columns, err = tableColumns(ctx, db, "managed_proxy_nodes")
	if err != nil {
		return err
	}
	if columns["stable"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE managed_proxy_nodes DROP COLUMN stable`); err != nil {
			return fmt.Errorf("drop managed proxy node stable: %w", err)
		}
	}
	return nil
}

// nodeProtocolConstraintOK reports whether the persisted managed_proxy_nodes
// table already accepts a wildcard that includes socks5/http. The CHECK was
// widened from the original three-protocol list; SQLite cannot alter a CHECK
// in place, so a table rebuild is required to admit the new protocols.
func nodeProtocolConstraintOK(ctx context.Context, db *sql.DB) (bool, error) {
	var ddl string
	err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='managed_proxy_nodes'`).Scan(&ddl)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	for _, protocol := range indexNodeProtocols {
		if !strings.Contains(strings.ToLower(ddl), "'"+protocol+"'") {
			return false, nil
		}
	}
	return true, nil
}

// rebuildNodeProtocolConstraint widens the persisted managed_proxy_nodes
// protocol CHECK constraint. SQLite cannot alter a CHECK in place, so the
// table is rebuilt: every dependent index and trigger is replayed from
// sqlite_master after the swap so routing behavior survives startup.
func rebuildNodeProtocolConstraint(ctx context.Context, db *sql.DB) error {
	ok, err := nodeProtocolConstraintOK(ctx, db)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	columns, err := tableColumns(ctx, db, "managed_proxy_nodes")
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	columnList := make([]string, 0, len(columns))
	ordered := []string{
		"id", "server_id", "name", "protocol", "runtime", "public_host", "assigned_port",
		"stats_port", "transport", "config_encrypted", "client_uri_encrypted", "revision",
		"enabled", "publishable", "apply_status", "last_error", "observed_status",
		"observed_revision", "observed_port", "observed_at", "health_status", "access_mode",
		"tunnel_path", "preferred_address_id", "connect_address", "connect_port",
		"tunnel_hostname", "created_at", "updated_at",
	}
	for _, name := range ordered {
		if columns[name] {
			columnList = append(columnList, name)
		}
	}
	if len(columnList) == 0 {
		return nil
	}
	listText := strings.Join(columnList, ", ")
	// Capture dependent schema (indexes and triggers) before dropping the
	// legacy table so they can be replayed after the rename.
	dependents := []string{}
	rows, err := db.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE type IN ('index','trigger') AND tbl_name='managed_proxy_nodes' AND sql IS NOT NULL AND sql<>''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		dependents = append(dependents, statement)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `BEGIN IMMEDIATE`)
	if err != nil {
		return fmt.Errorf("begin managed proxy node rebuild: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `ROLLBACK`)
	}()
	createSQL := strings.Replace(NodeTableDDL, "CREATE TABLE IF NOT EXISTS managed_proxy_nodes", "CREATE TABLE IF NOT EXISTS managed_proxy_node_v2", 1)
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create managed proxy node v2: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO managed_proxy_node_v2 (`+listText+`) SELECT `+listText+` FROM managed_proxy_nodes`); err != nil {
		return fmt.Errorf("copy managed proxy node rows: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE managed_proxy_nodes`); err != nil {
		return fmt.Errorf("drop managed proxy node legacy table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE managed_proxy_node_v2 RENAME TO managed_proxy_nodes`); err != nil {
		return fmt.Errorf("rename managed proxy node v2: %w", err)
	}
	for _, statement := range dependents {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("replay managed proxy node dependent %s: %w", truncateSQL(statement, 72), err)
		}
	}
	if _, err := db.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	return nil
}

func truncateSQL(statement string, limit int) string {
	compact := strings.Join(strings.Fields(strings.ReplaceAll(statement, "\n", " ")), " ")
	runes := []rune(compact)
	if len(runes) <= limit {
		return compact
	}
	return string(runes[:limit]) + "…"
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
