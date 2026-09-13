package serveragent

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/managedproxy"
	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS server_accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER DEFAULT 22,
			username TEXT NOT NULL,
			auth_type TEXT NOT NULL CHECK(auth_type IN ('password', 'key')),
			password TEXT,
			private_key TEXT,
			passphrase TEXT,
			status TEXT DEFAULT 'unknown' CHECK(status IN ('online', 'offline', 'unknown')),
			monitor_mode TEXT DEFAULT 'agent' CHECK(monitor_mode IN ('agent')),
			last_check_time DATETIME,
			last_check_status TEXT,
			response_time INTEGER,
			cached_info TEXT,
			tags TEXT,
			description TEXT,
			country TEXT,
			resolved_country TEXT,
			starts_at DATETIME,
			expires_at DATETIME,
			traffic_limit_bytes INTEGER DEFAULT 0,
			traffic_limit_mode TEXT DEFAULT 'total',
			traffic_alert_enabled INTEGER DEFAULT 0,
			traffic_alert_percent REAL DEFAULT 100,
			traffic_cycle_type TEXT DEFAULT 'none',
			traffic_cycle_day INTEGER DEFAULT 1,
			traffic_cycle_start DATETIME,
			traffic_cycle_end DATETIME,
			traffic_cycle_baseline INTEGER DEFAULT 0,
			order_index INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_monitor_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
			response_time INTEGER,
			error_message TEXT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS server_monitor_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			probe_interval INTEGER DEFAULT 60,
			probe_timeout INTEGER DEFAULT 10,
			log_retention_days INTEGER DEFAULT 7,
			max_connections INTEGER DEFAULT 10,
			session_timeout INTEGER DEFAULT 1800,
			auto_start INTEGER DEFAULT 1,
			metrics_collect_interval INTEGER DEFAULT 300,
			metrics_retention_days INTEGER DEFAULT 30,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			username TEXT NOT NULL,
			password TEXT,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_snippets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			category TEXT DEFAULT 'common',
			platform TEXT DEFAULT 'all',
			tags TEXT DEFAULT '[]',
			favorite INTEGER DEFAULT 0,
			run_count INTEGER DEFAULT 0,
			last_used_at DATETIME,
			is_builtin INTEGER DEFAULT 0,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_command_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			snippet_id INTEGER,
			server_id TEXT,
			server_name TEXT,
			command TEXT NOT NULL,
			rendered_command TEXT NOT NULL,
			execution_mode TEXT DEFAULT 'terminal',
			status TEXT DEFAULT 'sent',
			dangerous INTEGER DEFAULT 0,
			danger_reasons TEXT DEFAULT '[]',
			result_summary TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (snippet_id) REFERENCES server_snippets(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_metrics_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,
			cpu_usage REAL,
			cpu_load TEXT,
			cpu_cores INTEGER,
			cpu_threads INTEGER DEFAULT 0,
			cpu_temp REAL DEFAULT 0,
			cpu_power REAL DEFAULT 0,
			mem_used INTEGER,
			mem_total INTEGER,
			mem_usage REAL,
			disk_used TEXT,
			disk_total TEXT,
			disk_usage REAL,
			docker_installed INTEGER DEFAULT 0,
			docker_running INTEGER DEFAULT 0,
			docker_stopped INTEGER DEFAULT 0,
			gpu_usage REAL DEFAULT 0,
			gpu_mem_used INTEGER DEFAULT 0,
			gpu_mem_total INTEGER DEFAULT 0,
			gpu_power REAL DEFAULT 0,
			gpu_temp REAL DEFAULT 0,
			platform TEXT,
			net_rx REAL DEFAULT 0,
			net_tx REAL DEFAULT 0,
			recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS server_agent_credentials (
			server_id TEXT PRIMARY KEY,
			secret_encrypted TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS server_proxy_desired_state (
			server_id TEXT PRIMARY KEY,
			revision INTEGER NOT NULL DEFAULT 1,
			runtime TEXT NOT NULL,
			config_encrypted TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			applied_revision INTEGER NOT NULL DEFAULT 0,
			apply_status TEXT NOT NULL DEFAULT 'pending',
			last_error TEXT NOT NULL DEFAULT '',
			assigned_port INTEGER NOT NULL DEFAULT 0,
			stats_port INTEGER NOT NULL DEFAULT 0,
			transport TEXT NOT NULL DEFAULT 'tcp',
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS server_proxy_traffic_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,
			boot_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			node_id TEXT NOT NULL,
			upload_bytes INTEGER NOT NULL DEFAULT 0,
			download_bytes INTEGER NOT NULL DEFAULT 0,
			reported_at TEXT DEFAULT (datetime('now')),
			UNIQUE(server_id, boot_id, sequence, node_id)
		)`,
		managedproxy.NodeTableDDL,
		`CREATE TABLE IF NOT EXISTS managed_proxy_runtimes (
			server_id TEXT PRIMARY KEY,
			runtime TEXT NOT NULL DEFAULT 'sing-box',
			version TEXT NOT NULL DEFAULT '',
			desired_status TEXT NOT NULL DEFAULT 'running',
			apply_status TEXT NOT NULL DEFAULT 'not_installed',
			last_stage TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			observed_status TEXT NOT NULL DEFAULT 'unknown',
			observed_version TEXT NOT NULL DEFAULT '',
			observed_at TEXT,
			installed_at TEXT,
			updated_at TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS managed_proxy_tunnels (
			server_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			zone_id TEXT NOT NULL,
			zone_name TEXT NOT NULL DEFAULT '',
			tunnel_id TEXT NOT NULL DEFAULT '',
			tunnel_name TEXT NOT NULL DEFAULT '',
			hostname TEXT NOT NULL,
			dns_record_id TEXT NOT NULL DEFAULT '',
			token_encrypted TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 1,
			desired_status TEXT NOT NULL DEFAULT 'running',
			apply_status TEXT NOT NULL DEFAULT 'pending',
			last_stage TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			reconcile_attempts INTEGER NOT NULL DEFAULT 0,
			last_reconcile_at TEXT NOT NULL DEFAULT '',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS managed_proxy_preferences (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			address TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 443,
			enabled INTEGER NOT NULL DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT 'unknown',
			last_latency_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			checked_at TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_proxy_node_name_server ON managed_proxy_nodes(server_id, name)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_proxy_nodes_server ON managed_proxy_nodes(server_id, updated_at DESC)`,
		`CREATE TRIGGER IF NOT EXISTS trg_managed_proxy_node_port_insert
			BEFORE INSERT ON managed_proxy_nodes
			WHEN NEW.assigned_port > 0 AND EXISTS (
				SELECT 1 FROM managed_proxy_nodes WHERE server_id=NEW.server_id AND assigned_port=NEW.assigned_port
			)
			BEGIN SELECT RAISE(ABORT, 'managed proxy port already reserved'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_managed_proxy_node_port_update
			BEFORE UPDATE OF server_id,assigned_port ON managed_proxy_nodes
			WHEN NEW.assigned_port > 0 AND EXISTS (
				SELECT 1 FROM managed_proxy_nodes WHERE server_id=NEW.server_id AND assigned_port=NEW.assigned_port AND id<>NEW.id
			)
			BEGIN SELECT RAISE(ABORT, 'managed proxy port already reserved'); END`,
		`CREATE INDEX IF NOT EXISTS idx_managed_proxy_preferences_order ON managed_proxy_preferences(enabled DESC, is_default DESC, sort_order ASC)`,
		`CREATE TABLE IF NOT EXISTS managed_forwards (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			server_id TEXT NOT NULL,
			local_host TEXT NOT NULL DEFAULT '127.0.0.1',
			local_port INTEGER NOT NULL CHECK(local_port BETWEEN 1 AND 65535),
			protocol TEXT NOT NULL DEFAULT 'tcp' CHECK(protocol IN ('tcp','http','https')),
			transport TEXT NOT NULL CHECK(transport IN ('cloudflare_tunnel','tcp_relay','p2p')),
			tunnel_hostname TEXT NOT NULL DEFAULT '',
			tunnel_path TEXT NOT NULL DEFAULT '',
			tunnel_id TEXT NOT NULL DEFAULT '',
			tunnel_account_id TEXT NOT NULL DEFAULT '',
			tunnel_zone_id TEXT NOT NULL DEFAULT '',
			tunnel_zone_name TEXT NOT NULL DEFAULT '',
			dns_record_id TEXT NOT NULL DEFAULT '',
			tunnel_token_encrypted TEXT NOT NULL DEFAULT '',
			tunnel_revision INTEGER NOT NULL DEFAULT 0,
			tunnel_apply_status TEXT NOT NULL DEFAULT '',
			tunnel_last_stage TEXT NOT NULL DEFAULT '',
			tunnel_last_error TEXT NOT NULL DEFAULT '',
			tunnel_reconcile_attempts INTEGER NOT NULL DEFAULT 0,
			tunnel_last_reconcile_at TEXT NOT NULL DEFAULT '',
			whole_host INTEGER NOT NULL DEFAULT 0,
			udp INTEGER NOT NULL DEFAULT 0,
			relay_server_id TEXT NOT NULL DEFAULT '',
			remote_port INTEGER DEFAULT 0,
			p2p_peer_server_id TEXT NOT NULL DEFAULT '',
			auth_proxy_port INTEGER NOT NULL DEFAULT 0,
			access_mode TEXT NOT NULL DEFAULT 'public' CHECK(access_mode IN ('public','token','panel')),
			access_token TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT '',
			health_check_enabled INTEGER NOT NULL DEFAULT 0,
			health_check_interval INTEGER NOT NULL DEFAULT 30,
			health_check_timeout INTEGER NOT NULL DEFAULT 5,
			health_check_unhealthy_threshold INTEGER NOT NULL DEFAULT 3,
			health_check_healthy_threshold INTEGER NOT NULL DEFAULT 2,
			failover_enabled INTEGER NOT NULL DEFAULT 0,
			failover_current_server_id TEXT NOT NULL DEFAULT '',
			failover_switched_at TEXT NOT NULL DEFAULT '',
			failover_reason TEXT NOT NULL DEFAULT '',
			desired_status TEXT NOT NULL DEFAULT 'running' CHECK(desired_status IN ('running','stopped')),
			apply_status TEXT NOT NULL DEFAULT 'pending' CHECK(apply_status IN ('pending','deploying','running','stopped','failed','disconnected')),
			last_stage TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			connector_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_forwards_server ON managed_forwards(server_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_forwards_transport ON managed_forwards(transport, apply_status)`,
		`CREATE TABLE IF NOT EXISTS managed_forward_targets (
			id TEXT PRIMARY KEY,
			forward_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			role TEXT NOT NULL DEFAULT 'standby' CHECK(role IN ('primary','standby','backup')),
			health_status TEXT NOT NULL DEFAULT 'unknown' CHECK(health_status IN ('unknown','healthy','unhealthy','offline')),
			last_checked_at TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now')),
			FOREIGN KEY (forward_id) REFERENCES managed_forwards(id) ON DELETE CASCADE,
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE,
			UNIQUE(forward_id, server_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_forward_targets_forward ON managed_forward_targets(forward_id, priority)`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_traffic_server_time ON server_proxy_traffic_reports(server_id, reported_at DESC)`,
		`CREATE TABLE IF NOT EXISTS server_network_quality_targets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			host TEXT NOT NULL,
			port INTEGER DEFAULT 80,
			type TEXT DEFAULT 'tcp' CHECK(type IN ('tcp')),
			enabled INTEGER DEFAULT 1,
			order_index INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_network_quality_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,
			target_id INTEGER,
			target_name TEXT NOT NULL,
			target_host TEXT NOT NULL,
			target_port INTEGER DEFAULT 80,
			success INTEGER DEFAULT 0,
			latency_ms REAL,
			error_message TEXT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES server_network_quality_targets(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_status_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			domain TEXT,
			title TEXT NOT NULL,
			description TEXT,
			public INTEGER DEFAULT 1,
			cache_seconds INTEGER DEFAULT 300,
			config_json TEXT,
			server_ids_json TEXT DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS docker_stacks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT DEFAULT 'compose',
			source TEXT DEFAULT 'agent',
			working_dir TEXT,
			config_files TEXT DEFAULT '[]',
			status TEXT DEFAULT 'unknown',
			last_error TEXT,
			config_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(server_id, name),
			FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_accounts_status ON server_accounts(status)`,
		`CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_server ON server_monitor_logs(server_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_status ON server_monitor_logs(status, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_server_command_history_created ON server_command_history(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_server_command_history_server_created ON server_command_history(server_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_history_server_time ON server_metrics_history(server_id, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_history_time ON server_metrics_history(recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_network_quality_samples_server_time ON server_network_quality_samples(server_id, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_network_quality_samples_target_time ON server_network_quality_samples(target_id, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_docker_stacks_server ON docker_stacks(server_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_server_status_pages_slug ON server_status_pages(slug, public)`,
		`CREATE INDEX IF NOT EXISTS idx_server_status_pages_domain ON server_status_pages(domain, public)`,
		`INSERT OR IGNORE INTO server_monitor_config (id, probe_interval, probe_timeout, log_retention_days, max_connections, session_timeout, auto_start, metrics_collect_interval, metrics_retention_days) VALUES (1, 60, 10, 7, 10, 1800, 1, 300, 30)`,
		`INSERT OR IGNORE INTO server_network_quality_targets (id, name, host, port, type, enabled, order_index) VALUES (1, '联通', 'hb-cu-v4.ip.zstaticcdn.com', 80, 'tcp', 1, 1)`,
		`INSERT OR IGNORE INTO server_network_quality_targets (id, name, host, port, type, enabled, order_index) VALUES (2, '移动', 'hb-cm-v4.ip.zstaticcdn.com', 80, 'tcp', 1, 2)`,
		`INSERT OR IGNORE INTO server_network_quality_targets (id, name, host, port, type, enabled, order_index) VALUES (3, '电信', 'hb-ct-v4.ip.zstaticcdn.com', 80, 'tcp', 1, 3)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure server schema: %w", err)
		}
	}
	if err := reconcilequeue.EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := subscriptionledger.EnsureSchema(ctx, db); err != nil {
		return err
	}
	// Existing managed nodes are evidence that the sing-box runtime was
	// already installed before the runtime inventory was introduced.
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO managed_proxy_runtimes(server_id,runtime,version,desired_status,apply_status,last_stage,last_error,installed_at,updated_at) SELECT DISTINCT server_id,'sing-box','','running','running','legacy_detected','',datetime('now'),datetime('now') FROM managed_proxy_nodes WHERE apply_status='running'`); err != nil {
		return fmt.Errorf("backfill managed proxy runtime inventory: %w", err)
	}

	// Dynamic column migrations check
	if err := migrateColumns(ctx, db); err != nil {
		return err
	}
	if err := repairLegacyTunnelSubscriberFlow(ctx, db); err != nil {
		return err
	}
	// 存量整域 CF 转发迁移：为每条规则从主机级 Tunnel 的 Zone 自动分配独立子域名
	// （tunnel_hostname 空则分配 fwd-<id>.<zone>），由转发隧道健康循环自动部署独立隧道。
	if err := migrateLegacyWholeHostTunnels(ctx, db); err != nil {
		return err
	}

	return nil
}

func migrateColumns(ctx context.Context, db *sql.DB) error {
	proxyFields := []struct{ Name, SQL string }{
		{"assigned_port", "ALTER TABLE server_proxy_desired_state ADD COLUMN assigned_port INTEGER NOT NULL DEFAULT 0"},
		{"stats_port", "ALTER TABLE server_proxy_desired_state ADD COLUMN stats_port INTEGER NOT NULL DEFAULT 0"},
		{"transport", "ALTER TABLE server_proxy_desired_state ADD COLUMN transport TEXT NOT NULL DEFAULT 'tcp'"},
	}
	for _, f := range proxyFields {
		if exists, err := hasColumn(ctx, db, "server_proxy_desired_state", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				return fmt.Errorf("migrate managed proxy %s: %w", f.Name, err)
			}
		}
	}
	if err := managedproxy.EnsureNodeColumns(ctx, db); err != nil {
		return err
	}
	runtimeFields := []struct{ Name, SQL string }{
		{"observed_status", "ALTER TABLE managed_proxy_runtimes ADD COLUMN observed_status TEXT NOT NULL DEFAULT 'unknown'"},
		{"observed_version", "ALTER TABLE managed_proxy_runtimes ADD COLUMN observed_version TEXT NOT NULL DEFAULT ''"},
		{"observed_at", "ALTER TABLE managed_proxy_runtimes ADD COLUMN observed_at TEXT"},
	}
	for _, f := range runtimeFields {
		if exists, err := hasColumn(ctx, db, "managed_proxy_runtimes", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				return fmt.Errorf("migrate managed proxy runtime %s: %w", f.Name, err)
			}
		}
	}
	tunnelFields := []struct{ Name, SQL string }{
		{"reconcile_attempts", "ALTER TABLE managed_proxy_tunnels ADD COLUMN reconcile_attempts INTEGER NOT NULL DEFAULT 0"},
		{"last_reconcile_at", "ALTER TABLE managed_proxy_tunnels ADD COLUMN last_reconcile_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, f := range tunnelFields {
		if exists, err := hasColumn(ctx, db, "managed_proxy_tunnels", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				return fmt.Errorf("migrate managed proxy tunnel %s: %w", f.Name, err)
			}
		}
	}
	forwardFields := []struct{ Name, SQL string }{
		{"group_id", "ALTER TABLE managed_forwards ADD COLUMN group_id TEXT NOT NULL DEFAULT ''"},
		{"health_check_enabled", "ALTER TABLE managed_forwards ADD COLUMN health_check_enabled INTEGER NOT NULL DEFAULT 0"},
		{"health_check_interval", "ALTER TABLE managed_forwards ADD COLUMN health_check_interval INTEGER NOT NULL DEFAULT 30"},
		{"health_check_timeout", "ALTER TABLE managed_forwards ADD COLUMN health_check_timeout INTEGER NOT NULL DEFAULT 5"},
		{"health_check_unhealthy_threshold", "ALTER TABLE managed_forwards ADD COLUMN health_check_unhealthy_threshold INTEGER NOT NULL DEFAULT 3"},
		{"health_check_healthy_threshold", "ALTER TABLE managed_forwards ADD COLUMN health_check_healthy_threshold INTEGER NOT NULL DEFAULT 2"},
		{"failover_enabled", "ALTER TABLE managed_forwards ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 0"},
		{"failover_current_server_id", "ALTER TABLE managed_forwards ADD COLUMN failover_current_server_id TEXT NOT NULL DEFAULT ''"},
		{"failover_switched_at", "ALTER TABLE managed_forwards ADD COLUMN failover_switched_at TEXT NOT NULL DEFAULT ''"},
		{"failover_reason", "ALTER TABLE managed_forwards ADD COLUMN failover_reason TEXT NOT NULL DEFAULT ''"},
		{"connector_count", "ALTER TABLE managed_forwards ADD COLUMN connector_count INTEGER NOT NULL DEFAULT 0"},
		{"whole_host", "ALTER TABLE managed_forwards ADD COLUMN whole_host INTEGER NOT NULL DEFAULT 0"},
		{"auth_proxy_port", "ALTER TABLE managed_forwards ADD COLUMN auth_proxy_port INTEGER NOT NULL DEFAULT 0"},
		{"udp", "ALTER TABLE managed_forwards ADD COLUMN udp INTEGER NOT NULL DEFAULT 0"},
		{"p2p_peer_server_id", "ALTER TABLE managed_forwards ADD COLUMN p2p_peer_server_id TEXT NOT NULL DEFAULT ''"},
		{"tunnel_id", "ALTER TABLE managed_forwards ADD COLUMN tunnel_id TEXT NOT NULL DEFAULT ''"},
		{"tunnel_account_id", "ALTER TABLE managed_forwards ADD COLUMN tunnel_account_id TEXT NOT NULL DEFAULT ''"},
		{"tunnel_zone_id", "ALTER TABLE managed_forwards ADD COLUMN tunnel_zone_id TEXT NOT NULL DEFAULT ''"},
		{"tunnel_zone_name", "ALTER TABLE managed_forwards ADD COLUMN tunnel_zone_name TEXT NOT NULL DEFAULT ''"},
		{"dns_record_id", "ALTER TABLE managed_forwards ADD COLUMN dns_record_id TEXT NOT NULL DEFAULT ''"},
		{"tunnel_token_encrypted", "ALTER TABLE managed_forwards ADD COLUMN tunnel_token_encrypted TEXT NOT NULL DEFAULT ''"},
		{"tunnel_revision", "ALTER TABLE managed_forwards ADD COLUMN tunnel_revision INTEGER NOT NULL DEFAULT 0"},
		{"tunnel_apply_status", "ALTER TABLE managed_forwards ADD COLUMN tunnel_apply_status TEXT NOT NULL DEFAULT ''"},
		{"tunnel_last_stage", "ALTER TABLE managed_forwards ADD COLUMN tunnel_last_stage TEXT NOT NULL DEFAULT ''"},
		{"tunnel_last_error", "ALTER TABLE managed_forwards ADD COLUMN tunnel_last_error TEXT NOT NULL DEFAULT ''"},
		{"tunnel_reconcile_attempts", "ALTER TABLE managed_forwards ADD COLUMN tunnel_reconcile_attempts INTEGER NOT NULL DEFAULT 0"},
		{"tunnel_last_reconcile_at", "ALTER TABLE managed_forwards ADD COLUMN tunnel_last_reconcile_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, f := range forwardFields {
		if exists, err := hasColumn(ctx, db, "managed_forwards", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				applog.Error(ctx, "serveragent", "schema migration failed", "column", f.Name, "error", err.Error())
			}
		}
	}
	if exists, err := hasColumn(ctx, db, "server_monitor_config", "metrics_retention_days"); err == nil && !exists {
		if _, err := db.ExecContext(ctx, `ALTER TABLE server_monitor_config ADD COLUMN metrics_retention_days INTEGER DEFAULT 30`); err != nil {
			applog.Error(ctx, "serveragent", "schema migration failed", "column", "metrics_retention_days", "error", err.Error())
		}
	}
	if exists, err := hasColumn(ctx, db, "server_accounts", "monitor_mode"); err == nil && !exists {
		if _, err := db.ExecContext(ctx, `ALTER TABLE server_accounts ADD COLUMN monitor_mode TEXT DEFAULT 'agent'`); err != nil {
			applog.Error(ctx, "serveragent", "schema migration failed", "column", "monitor_mode", "error", err.Error())
		}
	}
	accountFields := []struct{ Name, SQL string }{
		{"traffic_limit_bytes", "ALTER TABLE server_accounts ADD COLUMN traffic_limit_bytes INTEGER DEFAULT 0"},
		{"traffic_limit_mode", "ALTER TABLE server_accounts ADD COLUMN traffic_limit_mode TEXT DEFAULT 'total'"},
		{"traffic_alert_enabled", "ALTER TABLE server_accounts ADD COLUMN traffic_alert_enabled INTEGER DEFAULT 0"},
		{"traffic_alert_percent", "ALTER TABLE server_accounts ADD COLUMN traffic_alert_percent REAL DEFAULT 100"},
		{"traffic_cycle_type", "ALTER TABLE server_accounts ADD COLUMN traffic_cycle_type TEXT DEFAULT 'none'"},
		{"traffic_cycle_day", "ALTER TABLE server_accounts ADD COLUMN traffic_cycle_day INTEGER DEFAULT 1"},
		{"traffic_cycle_start", "ALTER TABLE server_accounts ADD COLUMN traffic_cycle_start DATETIME"},
		{"traffic_cycle_end", "ALTER TABLE server_accounts ADD COLUMN traffic_cycle_end DATETIME"},
		{"traffic_cycle_baseline", "ALTER TABLE server_accounts ADD COLUMN traffic_cycle_baseline INTEGER DEFAULT 0"},
	}
	for _, f := range accountFields {
		if exists, err := hasColumn(ctx, db, "server_accounts", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				applog.Error(ctx, "serveragent", "schema migration failed", "column", f.Name, "error", err.Error())
			}
		}
	}

	gpuFields := []struct{ Name, SQL string }{
		{"gpu_usage", "ALTER TABLE server_metrics_history ADD COLUMN gpu_usage REAL DEFAULT 0"},
		{"cpu_threads", "ALTER TABLE server_metrics_history ADD COLUMN cpu_threads INTEGER DEFAULT 0"},
		{"cpu_temp", "ALTER TABLE server_metrics_history ADD COLUMN cpu_temp REAL DEFAULT 0"},
		{"cpu_power", "ALTER TABLE server_metrics_history ADD COLUMN cpu_power REAL DEFAULT 0"},
		{"gpu_mem_used", "ALTER TABLE server_metrics_history ADD COLUMN gpu_mem_used INTEGER DEFAULT 0"},
		{"gpu_mem_total", "ALTER TABLE server_metrics_history ADD COLUMN gpu_mem_total INTEGER DEFAULT 0"},
		{"gpu_power", "ALTER TABLE server_metrics_history ADD COLUMN gpu_power REAL DEFAULT 0"},
		{"gpu_temp", "ALTER TABLE server_metrics_history ADD COLUMN gpu_temp REAL DEFAULT 0"},
		{"platform", "ALTER TABLE server_metrics_history ADD COLUMN platform TEXT"},
	}
	for _, f := range gpuFields {
		if exists, err := hasColumn(ctx, db, "server_metrics_history", f.Name); err == nil && !exists {
			if _, err := db.ExecContext(ctx, f.SQL); err != nil {
				applog.Error(ctx, "serveragent", "schema migration failed", "column", f.Name, "error", err.Error())
			}
		}
	}

	return nil
}
