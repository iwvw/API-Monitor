package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type Service struct {
	cfg           config.Config
	store         *database.Store
	taskRegistry  *TaskRegistry
	engineIO      *EngineIOServer
	registry      *ConnectionRegistry
	metricsHub    *MetricsHub
	lastCollect   time.Time
	lastCollectMu sync.RWMutex
	lastPersist   map[string]time.Time
	lastPersistMu sync.Mutex
}

const realtimeMetricsPersistInterval = 10 * time.Second

func New(cfg config.Config) *Service {
	registry := NewConnectionRegistry()
	taskRegistry := NewTaskRegistry()
	metricsHub := NewMetricsHub()
	engineIO := NewEngineIOServer(registry)
	engineIO.metricsHub = metricsHub

	s := &Service{
		cfg:          cfg,
		store:        database.New(cfg),
		taskRegistry: taskRegistry,
		engineIO:     engineIO,
		registry:     registry,
		metricsHub:   metricsHub,
		lastPersist:  make(map[string]time.Time),
	}

	// 绑定 Engine.IO 事件处理器
	engineIO.SetHandlers(
		// onConnect: Agent 连接成功
		func(sessionID string, serverID string) {
			applog.Info(context.Background(), "serveragent", "agent connected", "session_id", sessionID, "server_id", serverID)
			if serverID != "" {
				var socket interface{}
				if sess := engineIO.getSession(sessionID); sess != nil {
					sess.mu.RLock()
					socket = sess
					sess.mu.RUnlock()
				}
				registry.Register(serverID, socket) // 注册到连接池

				// 异步更新数据库状态为 online
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					db, err := s.open(ctx)
					if err == nil {
						defer db.Close()
						now := time.Now().Format("2006-01-02 15:04:05")
						_, _ = db.ExecContext(ctx, `UPDATE server_accounts
							SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, updated_at = ?
							WHERE id = ?`, now, now, serverID)
					}
				}()

				// 广播服务器上线状态给前端
				if metricsHub != nil {
					metricsHub.BroadcastServerStatus(serverID, "online", true)
				}
			}
		},
		// onMessage: 接收 Agent 消息
		func(sessionID string, event string, data json.RawMessage) {
			// 优先通过 sessionID 查找 serverID，防止 payload 里没有 serverID
			var serverID string
			if sess := engineIO.getSession(sessionID); sess != nil {
				sess.mu.RLock()
				serverID = sess.ServerID
				sess.mu.RUnlock()
			}

			// 处理不同事件类型
			switch event {
			case "agent:state":
				// Agent 状态上报（CPU、内存、磁盘等）
				var state map[string]interface{}
				if err := json.Unmarshal(data, &state); err == nil {
					if serverID == "" {
						if sid, ok := state["server_id"].(string); ok {
							serverID = sid
						}
					}
					if serverID != "" {
						registry.UpdateHeartbeat(serverID)

						// 提取主机静态信息（比如核心数、总内存等，用于计算百分比）
						var hostInfo map[string]interface{}
						if conn, exists := registry.Get(serverID); exists {
							hostInfo = conn.GetMetadata()
						}

						// 格式化并合并为 cached_info 的 map
						cachedInfoMap := s.buildCachedInfo(state, hostInfo)

						// 存储指标到连接元数据
						if conn, exists := registry.Get(serverID); exists {
							if cpu, ok := cachedInfoMap["cpu"].(float64); ok {
								conn.SetMetadata("cpu", cpu)
							}
							if memory, ok := cachedInfoMap["memory"].(float64); ok {
								conn.SetMetadata("memory", memory)
							}
							if disk, ok := cachedInfoMap["disk_usage"].(float64); ok {
								conn.SetMetadata("disk", disk)
							}
							// 缓存所有状态字段到内存
							for k, v := range cachedInfoMap {
								conn.SetMetadata(k, v)
							}
						}

						// 实时广播走内存；SQLite 落库按较低频率节流，避免 Agent 高频上报拖慢前端和后端。
						if s.shouldPersistRealtimeMetrics(serverID, time.Now()) {
							go func() {
								ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
								defer cancel()
								db, err := s.open(ctx)
								if err == nil {
									defer db.Close()
									now := time.Now().Format("2006-01-02 15:04:05")
									cachedInfoJSON, _ := json.Marshal(cachedInfoMap)
									_, _ = db.ExecContext(ctx, `UPDATE server_accounts
									SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, cached_info = ?, updated_at = ?
									WHERE id = ?`, now, string(cachedInfoJSON), now, serverID)

									if err := s.persistMetrics(ctx, db, serverID, cachedInfoMap); err != nil {
										applog.Warn(ctx, "serveragent", "failed to persist realtime metrics", "server_id", serverID, "error", err.Error())
									}
								}
							}()
						}

						// 广播实时指标给前端浏览器客户端
						if s.metricsHub != nil {
							s.metricsHub.BroadcastMetrics(serverID, cachedInfoMap)
						}
					}
				}
			case "agent:host_info":
				// Agent 主机信息（平台、版本等）
				var hostInfo map[string]interface{}
				if err := json.Unmarshal(data, &hostInfo); err == nil {
					if serverID == "" {
						if sid, ok := hostInfo["server_id"].(string); ok {
							serverID = sid
						}
					}
					if serverID != "" {
						var fullCachedInfo map[string]interface{}
						if conn, exists := registry.Get(serverID); exists {
							if platform, ok := hostInfo["platform"].(string); ok {
								conn.SetMetadata("platform", platform)
							}
							if version, ok := hostInfo["version"].(string); ok {
								conn.SetMetadata("version", version)
							}
							if version, ok := hostInfo["agent_version"].(string); ok {
								conn.SetMetadata("version", version)
							}
							if hostname, ok := hostInfo["hostname"].(string); ok {
								conn.SetMetadata("hostname", hostname)
							}
							// 存储静态字段到连接元数据，以便在计算使用率百分比时使用
							for k, v := range hostInfo {
								conn.SetMetadata(k, v)
							}
							fullCachedInfo = conn.GetMetadata()
						} else {
							fullCachedInfo = hostInfo
						}

						// 异步持久化到数据库
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							db, err := s.open(ctx)
							if err == nil {
								defer db.Close()
								now := time.Now().Format("2006-01-02 15:04:05")
								cachedInfoJSON, _ := json.Marshal(fullCachedInfo)
								_, _ = db.ExecContext(ctx, `UPDATE server_accounts
									SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, cached_info = ?, updated_at = ?
									WHERE id = ?`, now, string(cachedInfoJSON), now, serverID)
							}
						}()
					}
				}
			case "agent:heartbeat":
				// Agent 心跳
				var hb struct {
					ServerID string `json:"server_id"`
				}
				if err := json.Unmarshal(data, &hb); err == nil {
					if hb.ServerID != "" {
						serverID = hb.ServerID
					}
				}
				if serverID != "" {
					registry.UpdateHeartbeat(serverID)
				}
			case "metrics", "heartbeat":
				// 兼容旧事件名称
				var state map[string]interface{}
				if err := json.Unmarshal(data, &state); err == nil {
					if serverID == "" {
						if sid, ok := state["server_id"].(string); ok {
							serverID = sid
						}
					}
					if serverID != "" {
						registry.UpdateHeartbeat(serverID)
					}
				}
			}
		},
		// onDisconnect: Agent 断开连接
		func(sessionID string) {
			applog.Info(context.Background(), "serveragent", "agent disconnected", "session_id", sessionID)
			// 查找此 session 对应的 serverID 并广播离线状态
			if sess := engineIO.getSession(sessionID); sess != nil {
				sess.mu.RLock()
				sid := sess.ServerID
				ns := sess.Namespace
				sess.mu.RUnlock()
				if ns != "/metrics" && sid != "" {
					registry.Disconnect(sid)

					go func(serverID string) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						db, err := s.open(ctx)
						if err != nil {
							return
						}
						defer db.Close()
						now := time.Now().Format("2006-01-02 15:04:05")
						_, _ = db.ExecContext(ctx, `UPDATE server_accounts
							SET status = 'offline', last_check_time = ?, last_check_status = 'disconnected', updated_at = ?
							WHERE id = ?`, now, now, serverID)
					}(sid)

					if metricsHub != nil {
						metricsHub.BroadcastServerStatus(sid, "offline", false)
					}
				}
			}
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if db, err := s.store.Open(ctx); err == nil {
		_ = ensureSchema(ctx, db)
		db.Close()
	}

	// Start background telemetry metrics collection loop
	go s.startMetricsCollectorLoop()

	return s
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	return s.store.Open(ctx)
}

func (s *Service) shouldPersistRealtimeMetrics(serverID string, now time.Time) bool {
	if serverID == "" {
		return false
	}
	s.lastPersistMu.Lock()
	defer s.lastPersistMu.Unlock()
	last := s.lastPersist[serverID]
	if !last.IsZero() && now.Sub(last) < realtimeMetricsPersistInterval {
		return false
	}
	s.lastPersist[serverID] = now
	return true
}

func (s *Service) BroadcastUptimeHeartbeat(monitorID int64, beat map[string]interface{}) {
	if s.metricsHub == nil {
		return
	}
	s.metricsHub.BroadcastRootEvent("uptime:heartbeat", map[string]interface{}{
		"monitorId": monitorID,
		"beat":      beat,
	})
}

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
		`CREATE INDEX IF NOT EXISTS idx_server_accounts_status ON server_accounts(status)`,
		`CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_server ON server_monitor_logs(server_id, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_status ON server_monitor_logs(status, checked_at)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_history_server_time ON server_metrics_history(server_id, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_history_time ON server_metrics_history(recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_network_quality_samples_server_time ON server_network_quality_samples(server_id, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_network_quality_samples_target_time ON server_network_quality_samples(target_id, checked_at DESC)`,
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

	// Dynamic column migrations check
	if err := migrateColumns(ctx, db); err != nil {
		return err
	}

	return nil
}

func migrateColumns(ctx context.Context, db *sql.DB) error {
	if exists, err := hasColumn(ctx, db, "server_monitor_config", "metrics_retention_days"); err == nil && !exists {
		_, _ = db.ExecContext(ctx, `ALTER TABLE server_monitor_config ADD COLUMN metrics_retention_days INTEGER DEFAULT 30`)
	}
	if exists, err := hasColumn(ctx, db, "server_accounts", "monitor_mode"); err == nil && !exists {
		_, _ = db.ExecContext(ctx, `ALTER TABLE server_accounts ADD COLUMN monitor_mode TEXT DEFAULT 'agent'`)
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
			_, _ = db.ExecContext(ctx, f.SQL)
		}
	}

	return nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Special-case Socket.IO style routes that do not use the /api/server prefix.
	if strings.HasPrefix(r.URL.Path, "/socket.io/") {
		s.engineIO.ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/ws/ssh" {
		s.handleSSHTerminal(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/server")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	// REST API routes without database access.
	if len(parts) == 1 && parts[0] == "s" && r.Method == http.MethodGet {
		s.HandleGetServers(w, r)
		return
	}
	if len(parts) >= 2 && parts[0] == "s" && r.Method == http.MethodGet {
		if len(parts) == 3 && parts[2] == "history" {
			s.HandleGetServerHistory(w, r)
			return
		}
		if len(parts) == 2 {
			s.HandleGetServerDetail(w, r)
			return
		}
	}

	// Dashboard API used by the legacy overview.
	if len(parts) == 1 && parts[0] == "agents" && r.Method == http.MethodGet {
		s.HandleGetServers(w, r)
		return
	}

	// Setup database connection
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "database connection failed: "+err.Error())
		return
	}
	defer db.Close()

	switch {
	// SFTP routes (NEW)
	case len(parts) >= 1 && parts[0] == "sftp":
		s.handleSFTPRoutes(w, r, db, parts[1:])

	// Docker routes (NEW)
	case len(parts) >= 1 && parts[0] == "docker":
		if len(parts) == 2 && parts[1] == "check-update" && r.Method == http.MethodPost {
			s.handleDockerCheckUpdate(w, r, db)
		} else {
			s.handleDockerRoutes(w, r, db, parts[1:])
		}

	// Server info, test-connection, action, check-all (NEW)
	case len(parts) == 1 && parts[0] == "info" && r.Method == http.MethodPost:
		s.handleServerInfo(w, r, db)
	case len(parts) == 1 && parts[0] == "test-connection" && r.Method == http.MethodPost:
		s.handleTestConnection(w, r, db)
	case len(parts) == 1 && parts[0] == "action" && r.Method == http.MethodPost:
		s.handleServerAction(w, r, db)
	case len(parts) == 1 && parts[0] == "check-all" && r.Method == http.MethodPost:
		s.handleCheckAll(w, r, db)

	// Agent routes (Wave 5b)
	case len(parts) >= 1 && parts[0] == "agent":
		s.handleAgentRoutes(w, r, db, parts[1:])

	// Metrics routes (Wave 5b)
	case len(parts) >= 1 && parts[0] == "metrics":
		s.handleMetricsRoutes(w, r, db, parts[1:])

	// Network quality routes (Wave 5b)
	case len(parts) >= 1 && parts[0] == "network-quality":
		s.handleNetworkQualityRoutes(w, r, db, parts[1:])

	// Tasks routes (Wave 5b)
	case len(parts) >= 1 && parts[0] == "tasks":
		s.handleTasksRoutes(w, r, db, parts[1:])

	// v2 Tasks routes (NEW)
	case len(parts) >= 1 && parts[0] == "v2":
		if len(parts) >= 2 && parts[1] == "tasks" {
			s.handleV2TasksRoutes(w, r, db, parts[2:])
		} else if len(parts) >= 2 && parts[1] == "docker" {
			s.handleV2DockerRoutes(w, r, db, parts[2:])
		} else {
			response.Error(w, http.StatusNotFound, "v2 route not found")
		}

	// Credentials routes
	case len(parts) >= 1 && parts[0] == "credentials":
		s.handleCredentials(w, r, db, parts[1:])

	// Snippets routes
	case len(parts) >= 1 && parts[0] == "snippets":
		s.handleSnippets(w, r, db, parts[1:])

	// Monitor routes
	case len(parts) >= 1 && parts[0] == "monitor":
		s.handleMonitor(w, r, db, parts[1:])

	// Accounts routes
	case len(parts) == 1 && parts[0] == "accounts" && r.Method == http.MethodGet:
		s.listAccounts(w, r, db)
	case len(parts) == 1 && parts[0] == "accounts" && r.Method == http.MethodPost:
		s.createAccount(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "export" && r.Method == http.MethodGet:
		s.exportAccounts(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "import" && r.Method == http.MethodPost:
		s.importAccounts(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "reorder" && r.Method == http.MethodPost:
		s.reorderAccounts(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodGet:
		s.getAccount(w, r, db, parts[1])
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodPut:
		s.updateAccount(w, r, db, parts[1])
	case len(parts) == 2 && parts[0] == "accounts" && r.Method == http.MethodDelete:
		s.deleteAccount(w, r, db, parts[1])

	default:
		response.Error(w, http.StatusNotFound, "serveragent route not implemented")
	}
}

// ==========================================
// CREDENTIALS HANDLERS
// ==========================================

func (s *Service) handleCredentials(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		if r.Method == http.MethodGet {
			s.listCredentials(w, r, db)
		} else if r.Method == http.MethodPost {
			s.createCredential(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 && subparts[0] == "default" && r.Method == http.MethodGet {
		s.getDefaultCredential(w, r, db)
		return
	}

	if len(subparts) == 1 && r.Method == http.MethodDelete {
		s.deleteCredential(w, r, db, subparts[0])
		return
	}

	// PUT /api/server/credentials/{id} - 更新凭据
	if len(subparts) == 1 && r.Method == http.MethodPut {
		s.updateCredential(w, r, db, subparts[0])
		return
	}

	if len(subparts) == 2 && subparts[1] == "default" && (r.Method == http.MethodPut || r.Method == http.MethodPost) {
		s.setDefaultCredential(w, r, db, subparts[0])
		return
	}

	response.Error(w, http.StatusNotFound, "credentials sub-route not found")
}

func (s *Service) listCredentials(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, username, password, is_default, created_at, updated_at FROM server_credentials ORDER BY is_default DESC, created_at DESC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var name, username string
		var password sql.NullString
		var isDefault int
		var createdAt, updatedAt string
		if err := rows.Scan(&id, &name, &username, &password, &isDefault, &createdAt, &updatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		plainPassword := ""
		if password.Valid {
			plainPassword = secure.SecureDecrypt(password.String)
		}

		list = append(list, map[string]interface{}{
			"id":         id,
			"name":       name,
			"username":   username,
			"password":   plainPassword,
			"is_default": isDefault == 1,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) getDefaultCredential(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var id int
	var name, username string
	var password sql.NullString
	var isDefault int
	var createdAt, updatedAt string

	err := db.QueryRowContext(r.Context(), "SELECT id, name, username, password, is_default, created_at, updated_at FROM server_credentials WHERE is_default = 1 LIMIT 1").
		Scan(&id, &name, &username, &password, &isDefault, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		response.OK(w, nil)
		return
	} else if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	plainPassword := ""
	if password.Valid {
		plainPassword = secure.SecureDecrypt(password.String)
	}

	response.OK(w, map[string]interface{}{
		"id":         id,
		"name":       name,
		"username":   username,
		"password":   plainPassword,
		"is_default": true,
		"created_at": createdAt,
		"updated_at": updatedAt,
	})
}

func (s *Service) createCredential(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Username == "" {
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	encPassword := ""
	if req.Password != "" {
		var err error
		encPassword, err = secure.SecureEncrypt(req.Password)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "encryption failed: "+err.Error())
			return
		}
	}

	res, err := db.ExecContext(r.Context(), "INSERT INTO server_credentials (name, username, password) VALUES (?, ?, ?)", req.Name, req.Username, encPassword)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	lastID, _ := res.LastInsertId()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "凭据添加成功",
		"data": map[string]interface{}{
			"id":       lastID,
			"name":     req.Name,
			"username": req.Username,
			"password": req.Password,
		},
	})
}

func (s *Service) setDefaultCredential(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid credential ID")
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 0"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	res, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 1 WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "凭据不存在")
		return
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"success": true,
		"message": "已设置为默认凭据",
	})
}

func (s *Service) deleteCredential(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid credential ID")
		return
	}

	res, err := db.ExecContext(r.Context(), "DELETE FROM server_credentials WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "凭据不存在")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "凭据删除成功",
	})
}

// ==========================================
// SNIPPETS HANDLERS
// ==========================================

func (s *Service) handleSnippets(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		if r.Method == http.MethodGet {
			s.listSnippets(w, r, db)
		} else if r.Method == http.MethodPost {
			s.createSnippet(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 {
		if subparts[0] == "preview" && r.Method == http.MethodPost {
			s.previewSnippet(w, r, db)
			return
		}
		if subparts[0] == "history" {
			if r.Method == http.MethodGet {
				s.getSnippetHistory(w, r, db)
			} else if r.Method == http.MethodPost {
				s.addSnippetHistory(w, r, db)
			} else {
				response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}
	}

	if len(subparts) == 1 {
		if r.Method == http.MethodPut {
			s.updateSnippet(w, r, db, subparts[0])
			return
		}
		if r.Method == http.MethodDelete {
			s.deleteSnippet(w, r, db, subparts[0])
			return
		}
	}

	response.Error(w, http.StatusNotFound, "snippets sub-route not found")
}

func (s *Service) listSnippets(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	category := q.Get("category")
	platform := q.Get("platform")
	favoriteStr := q.Get("favorite")
	searchQ := q.Get("q")

	sqlQuery := "SELECT id, title, content, category, platform, tags, favorite, run_count, last_used_at, is_builtin, description, created_at, updated_at FROM server_snippets WHERE 1=1"
	var params []interface{}

	if category != "" {
		sqlQuery += " AND category = ?"
		params = append(params, category)
	}
	if platform != "" && platform != "all" {
		sqlQuery += " AND (platform = 'all' OR platform = ?)"
		params = append(params, platform)
	}
	if favoriteStr != "" {
		favVal := 0
		if favoriteStr == "true" || favoriteStr == "1" {
			favVal = 1
		}
		sqlQuery += " AND favorite = ?"
		params = append(params, favVal)
	}
	if searchQ != "" {
		sqlQuery += " AND (title LIKE ? OR content LIKE ? OR description LIKE ? OR tags LIKE ?)"
		likeVal := "%" + searchQ + "%"
		params = append(params, likeVal, likeVal, likeVal, likeVal)
	}

	sqlQuery += " ORDER BY favorite DESC, category ASC, run_count DESC, title ASC"

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var snippets []map[string]interface{}
	for rows.Next() {
		var id, favorite, runCount, isBuiltin int
		var title, content, category, platform, tagsStr string
		var lastUsedAt, description sql.NullString
		var createdAt, updatedAt string
		err := rows.Scan(&id, &title, &content, &category, &platform, &tagsStr, &favorite, &runCount, &lastUsedAt, &isBuiltin, &description, &createdAt, &updatedAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		descVal := ""
		if description.Valid {
			descVal = description.String
		}
		lastUsedVal := interface{}(nil)
		if lastUsedAt.Valid {
			lastUsedVal = lastUsedAt.String
		}

		dangerRes := DetectDangerousCommand(content)

		snippets = append(snippets, map[string]interface{}{
			"id":                id,
			"title":             title,
			"content":           content,
			"category":          category,
			"platform":          platform,
			"tags":              parseJSONTags(tagsStr),
			"favorite":          favorite == 1,
			"run_count":         runCount,
			"last_used_at":      lastUsedVal,
			"is_builtin":        isBuiltin == 1,
			"description":       descVal,
			"created_at":        createdAt,
			"updated_at":        updatedAt,
			"dangerous":         dangerRes.Dangerous,
			"dangerous_reasons": dangerRes.Reasons,
		})
	}
	if snippets == nil {
		snippets = []map[string]interface{}{}
	}
	response.OK(w, snippets)
}

func (s *Service) createSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Title       string      `json:"title"`
		Content     string      `json:"content"`
		Category    string      `json:"category"`
		Platform    string      `json:"platform"`
		Tags        interface{} `json:"tags"`
		Favorite    bool        `json:"favorite"`
		Description string      `json:"description"`
		IsBuiltin   bool        `json:"is_builtin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" || req.Content == "" {
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	now := time.Now().Format(time.RFC3339)
	favoriteVal := 0
	if req.Favorite {
		favoriteVal = 1
	}
	builtinVal := 0
	if req.IsBuiltin {
		builtinVal = 1
	}

	res, err := db.ExecContext(r.Context(), `
		INSERT INTO server_snippets (
			title, content, category, platform, tags, favorite, description, is_builtin, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Title, req.Content,
		coalesceStr(req.Category, "common"),
		coalesceStr(req.Platform, "all"),
		SerializeList(req.Tags),
		favoriteVal,
		req.Description,
		builtinVal,
		now, now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	lastID, _ := res.LastInsertId()
	snippetObj, err := s.querySnippetByID(r.Context(), db, int(lastID))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, snippetObj)
}

func (s *Service) updateSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid snippet ID")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing snippet
	existing, err := s.querySnippetByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "snippet not found")
		return
	}

	now := time.Now().Format(time.RFC3339)
	title := getStringVal(req, "title", existing["title"].(string))
	content := getStringVal(req, "content", existing["content"].(string))
	category := getStringVal(req, "category", existing["category"].(string))
	platform := getStringVal(req, "platform", existing["platform"].(string))
	description := getStringVal(req, "description", existing["description"].(string))

	favorite := existing["favorite"].(bool)
	if val, ok := req["favorite"].(bool); ok {
		favorite = val
	}
	favoriteVal := 0
	if favorite {
		favoriteVal = 1
	}

	tagsStr := SerializeList(existing["tags"])
	if val, ok := req["tags"]; ok {
		tagsStr = SerializeList(val)
	}

	_, err = db.ExecContext(r.Context(), `
		UPDATE server_snippets
		SET title = ?, content = ?, category = ?, platform = ?, tags = ?, favorite = ?, description = ?, updated_at = ?
		WHERE id = ?`,
		title, content, category, platform, tagsStr, favoriteVal, description, now, id,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, true)
}

func (s *Service) deleteSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB, idStr string) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid snippet ID")
		return
	}

	res, err := db.ExecContext(r.Context(), "DELETE FROM server_snippets WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "代码片段不存在")
		return
	}

	response.OK(w, true)
}

func (s *Service) previewSnippet(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Command   string                 `json:"command"`
		SnippetID int                    `json:"snippetId"`
		ServerID  string                 `json:"serverId"`
		Cwd       string                 `json:"cwd"`
		Variables map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	commandText := req.Command
	if req.SnippetID > 0 {
		var content string
		err := db.QueryRowContext(r.Context(), "SELECT content FROM server_snippets WHERE id = ?", req.SnippetID).Scan(&content)
		if err == nil {
			commandText = content
		}
	}

	if commandText == "" {
		response.Error(w, http.StatusBadRequest, "缺少命令内容")
		return
	}

	// Fetch server if serverId is provided
	serverMap := make(map[string]interface{})
	if req.ServerID != "" {
		var host, name, username string
		var port int
		err := db.QueryRowContext(r.Context(), "SELECT host, name, port, username FROM server_accounts WHERE id = ?", req.ServerID).
			Scan(&host, &name, &port, &username)
		if err == nil {
			serverMap["host"] = host
			serverMap["name"] = name
			serverMap["port"] = port
			serverMap["username"] = username
		}
	}

	extraVars := make(map[string]interface{})
	for k, v := range req.Variables {
		extraVars[k] = v
	}
	extraVars["cwd"] = req.Cwd

	resolvedVars := BuildCommandVariables(serverMap, extraVars)
	rendered := RenderCommandTemplate(commandText, resolvedVars)
	danger := DetectDangerousCommand(rendered)

	response.OK(w, map[string]interface{}{
		"command":       commandText,
		"rendered":      rendered,
		"dangerous":     danger.Dangerous,
		"dangerReasons": danger.Reasons,
	})
}

func (s *Service) addSnippetHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		SnippetID       *int    `json:"snippetId"`
		ServerID        *string `json:"serverId"`
		Command         string  `json:"command"`
		RenderedCommand string  `json:"renderedCommand"`
		ExecutionMode   string  `json:"executionMode"`
		Status          string  `json:"status"`
		ResultSummary   *string `json:"resultSummary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var serverName sql.NullString
	if req.ServerID != nil && *req.ServerID != "" {
		var name string
		err := db.QueryRowContext(r.Context(), "SELECT name FROM server_accounts WHERE id = ?", *req.ServerID).Scan(&name)
		if err == nil {
			serverName = sql.NullString{String: name, Valid: true}
		}
	}

	commandStr := req.Command
	if commandStr == "" {
		commandStr = req.RenderedCommand
	}
	renderedStr := req.RenderedCommand
	if renderedStr == "" {
		renderedStr = req.Command
	}

	danger := DetectDangerousCommand(renderedStr)
	dangerReasonsJSON, _ := json.Marshal(danger.Reasons)

	now := time.Now().Format(time.RFC3339)

	res, err := db.ExecContext(r.Context(), `
		INSERT INTO server_command_history (
			snippet_id, server_id, server_name, command, rendered_command, execution_mode, status, dangerous, danger_reasons, result_summary, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.SnippetID, req.ServerID, serverName, commandStr, renderedStr,
		coalesceStr(req.ExecutionMode, "terminal"),
		coalesceStr(req.Status, "sent"),
		boolToInt(danger.Dangerous),
		string(dangerReasonsJSON),
		req.ResultSummary,
		now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update run count
	if req.SnippetID != nil && *req.SnippetID > 0 {
		_, _ = db.ExecContext(r.Context(), "UPDATE server_snippets SET run_count = COALESCE(run_count, 0) + 1, last_used_at = ?, updated_at = ? WHERE id = ?", now, now, *req.SnippetID)
	}

	lastID, _ := res.LastInsertId()
	response.OK(w, map[string]interface{}{
		"id":               lastID,
		"snippet_id":       req.SnippetID,
		"server_id":        req.ServerID,
		"server_name":      serverName.String,
		"command":          commandStr,
		"rendered_command": renderedStr,
		"execution_mode":   req.ExecutionMode,
		"status":           req.Status,
		"created_at":       now,
	})
}

func (s *Service) getSnippetHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	serverId := q.Get("serverId")
	limitStr := q.Get("limit")

	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	if limit > 200 {
		limit = 200
	}

	sqlQuery := "SELECT id, snippet_id, server_id, server_name, command, rendered_command, execution_mode, status, dangerous, danger_reasons, result_summary, created_at FROM server_command_history"
	var params []interface{}
	if serverId != "" {
		sqlQuery += " WHERE server_id = ?"
		params = append(params, serverId)
	}
	sqlQuery += " ORDER BY created_at DESC LIMIT ?"
	params = append(params, limit)

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, dangerous int
		var snippetID sql.NullInt64
		var serverID, serverName, resultSummary sql.NullString
		var command, renderedCommand, executionMode, status, dangerReasonsJSON, createdAt string
		err := rows.Scan(&id, &snippetID, &serverID, &serverName, &command, &renderedCommand, &executionMode, &status, &dangerous, &dangerReasonsJSON, &resultSummary, &createdAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var snippetVal interface{} = nil
		if snippetID.Valid {
			snippetVal = snippetID.Int64
		}
		var serverIDVal interface{} = nil
		if serverID.Valid {
			serverIDVal = serverID.String
		}
		var serverNameVal interface{} = nil
		if serverName.Valid {
			serverNameVal = serverName.String
		}
		var resultVal interface{} = nil
		if resultSummary.Valid {
			resultVal = resultSummary.String
		}

		var reasons []string
		_ = json.Unmarshal([]byte(dangerReasonsJSON), &reasons)
		if reasons == nil {
			reasons = []string{}
		}

		list = append(list, map[string]interface{}{
			"id":               id,
			"snippet_id":       snippetVal,
			"server_id":        serverIDVal,
			"server_name":      serverNameVal,
			"command":          command,
			"rendered_command": renderedCommand,
			"execution_mode":   executionMode,
			"status":           status,
			"dangerous":        dangerous == 1,
			"danger_reasons":   reasons,
			"result_summary":   resultVal,
			"created_at":       createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) querySnippetByID(ctx context.Context, db *sql.DB, id int) (map[string]interface{}, error) {
	var favorite, runCount, isBuiltin int
	var title, content, category, platform, tagsStr string
	var lastUsedAt, description sql.NullString
	var createdAt, updatedAt string
	err := db.QueryRowContext(ctx, "SELECT id, title, content, category, platform, tags, favorite, run_count, last_used_at, is_builtin, description, created_at, updated_at FROM server_snippets WHERE id = ?", id).
		Scan(&id, &title, &content, &category, &platform, &tagsStr, &favorite, &runCount, &lastUsedAt, &isBuiltin, &description, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	descVal := ""
	if description.Valid {
		descVal = description.String
	}
	lastUsedVal := interface{}(nil)
	if lastUsedAt.Valid {
		lastUsedVal = lastUsedAt.String
	}

	dangerRes := DetectDangerousCommand(content)

	return map[string]interface{}{
		"id":                id,
		"title":             title,
		"content":           content,
		"category":          category,
		"platform":          platform,
		"tags":              parseJSONTags(tagsStr),
		"favorite":          favorite == 1,
		"run_count":         runCount,
		"last_used_at":      lastUsedVal,
		"is_builtin":        isBuiltin == 1,
		"description":       descVal,
		"created_at":        createdAt,
		"updated_at":        updatedAt,
		"dangerous":         dangerRes.Dangerous,
		"dangerous_reasons": dangerRes.Reasons,
	}, nil
}

// ==========================================
// MONITOR CONFIG & LOGS HANDLERS
// ==========================================

func (s *Service) handleMonitor(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "status" && r.Method == http.MethodGet {
		s.getMonitorStatus(w, r, db)
		return
	}

	if len(subparts) == 1 && subparts[0] == "collect" && r.Method == http.MethodPost {
		s.collectMonitorMetrics(w, r, db)
		return
	}

	if len(subparts) == 1 && subparts[0] == "config" {
		if r.Method == http.MethodGet {
			s.getMonitorConfig(w, r, db)
		} else if r.Method == http.MethodPut {
			s.updateMonitorConfig(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 && subparts[0] == "logs" && r.Method == http.MethodGet {
		s.listMonitorLogs(w, r, db)
		return
	}

	response.Error(w, http.StatusNotFound, "monitor sub-route not found")
}

func (s *Service) getMonitorStatus(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var interval int
	var autoStart int
	err := db.QueryRowContext(r.Context(), "SELECT metrics_collect_interval, auto_start FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.lastCollectMu.RLock()
	var lastCollectVal interface{} = nil
	if !s.lastCollect.IsZero() {
		lastCollectVal = s.lastCollect.Format("2006-01-02 15:04:05")
	}
	s.lastCollectMu.RUnlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status": map[string]interface{}{
			"running":     autoStart == 1,
			"isRunning":   autoStart == 1,
			"interval":    interval * 1000,
			"collector":   "go",
			"lastCollect": lastCollectVal,
		},
	})
}

func (s *Service) runPeriodicCollection(ctx context.Context, db *sql.DB) int {
	rows, err := db.QueryContext(ctx, "SELECT id, COALESCE(cached_info, '{}') FROM server_accounts")
	if err != nil {
		return 0
	}
	defer rows.Close()

	type serverMetric struct {
		serverID string
		raw      string
	}
	var list []serverMetric
	for rows.Next() {
		var sm serverMetric
		if err := rows.Scan(&sm.serverID, &sm.raw); err == nil {
			list = append(list, sm)
		}
	}

	collected := 0
	for _, sm := range list {
		var metrics map[string]interface{}
		if err := json.Unmarshal([]byte(sm.raw), &metrics); err != nil || len(metrics) == 0 {
			continue
		}
		if conn, exists := s.engineIO.registry.Get(sm.serverID); exists {
			if metadata := conn.GetMetadata(); len(metadata) > 0 {
				metrics = s.buildInfoStruct(metadata)
			}
		}
		if err := s.persistMetrics(ctx, db, sm.serverID, metrics); err == nil {
			collected++
		}
	}

	if collected > 0 {
		s.lastCollectMu.Lock()
		s.lastCollect = time.Now()
		s.lastCollectMu.Unlock()
	}

	return collected
}

func (s *Service) collectMonitorMetrics(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	collected := s.runPeriodicCollection(r.Context(), db)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"collected": collected,
	})
}

func (s *Service) startMetricsCollectorLoop() {
	// Wait a moment for database initialization and server startup
	time.Sleep(5 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastCollected time.Time

	for range ticker.C {
		ctx := context.Background()
		db, err := s.open(ctx)
		if err != nil {
			continue
		}

		// Query monitor config
		var interval int
		var autoStart int
		var retentionDays int
		err = db.QueryRowContext(ctx, "SELECT metrics_collect_interval, auto_start, metrics_retention_days FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart, &retentionDays)
		if err != nil {
			db.Close()
			continue
		}

		if autoStart != 1 {
			db.Close()
			continue
		}

		now := time.Now()
		// If it's time to collect
		if lastCollected.IsZero() || now.Sub(lastCollected) >= time.Duration(interval)*time.Second {
			// Trigger collection
			s.runPeriodicCollection(ctx, db)
			lastCollected = now

			// Clean up old metrics
			if retentionDays > 0 {
				_, _ = db.ExecContext(ctx, "DELETE FROM server_metrics_history WHERE recorded_at < datetime('now', '-' || ? || ' days')", retentionDays)
			}
		}

		db.Close()
	}
}

func (s *Service) getMonitorConfig(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var id, probeInterval, probeTimeout, logRetentionDays, maxConnections, sessionTimeout, autoStart, metricsCollectInterval, metricsRetentionDays int
	var updatedAt string

	err := db.QueryRowContext(r.Context(), "SELECT id, probe_interval, probe_timeout, log_retention_days, max_connections, session_timeout, auto_start, metrics_collect_interval, metrics_retention_days, updated_at FROM server_monitor_config WHERE id = 1").
		Scan(&id, &probeInterval, &probeTimeout, &logRetentionDays, &maxConnections, &sessionTimeout, &autoStart, &metricsCollectInterval, &metricsRetentionDays, &updatedAt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"id":                       id,
		"probe_interval":           probeInterval,
		"probe_timeout":            probeTimeout,
		"log_retention_days":       logRetentionDays,
		"max_connections":          maxConnections,
		"session_timeout":          sessionTimeout,
		"auto_start":               autoStart == 1,
		"metrics_collect_interval": metricsCollectInterval,
		"metrics_retention_days":   metricsRetentionDays,
		"updated_at":               updatedAt,
	})
}

func (s *Service) updateMonitorConfig(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing first
	var existing struct {
		probeInterval          int
		probeTimeout           int
		logRetentionDays       int
		maxConnections         int
		sessionTimeout         int
		autoStart              int
		metricsCollectInterval int
		metricsRetentionDays   int
	}
	err := db.QueryRowContext(r.Context(), "SELECT probe_interval, probe_timeout, log_retention_days, max_connections, session_timeout, auto_start, metrics_collect_interval, metrics_retention_days FROM server_monitor_config WHERE id = 1").
		Scan(&existing.probeInterval, &existing.probeTimeout, &existing.logRetentionDays, &existing.maxConnections, &existing.sessionTimeout, &existing.autoStart, &existing.metricsCollectInterval, &existing.metricsRetentionDays)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to get config: "+err.Error())
		return
	}

	probeInterval := getIntVal(req, "probe_interval", existing.probeInterval)
	probeTimeout := getIntVal(req, "probe_timeout", existing.probeTimeout)
	logRetentionDays := getIntVal(req, "log_retention_days", existing.logRetentionDays)
	maxConnections := getIntVal(req, "max_connections", existing.maxConnections)
	sessionTimeout := getIntVal(req, "session_timeout", existing.sessionTimeout)
	metricsCollectInterval := getIntVal(req, "metrics_collect_interval", existing.metricsCollectInterval)
	metricsRetentionDays := getIntVal(req, "metrics_retention_days", existing.metricsRetentionDays)

	autoStart := existing.autoStart
	if val, ok := req["auto_start"].(bool); ok {
		if val {
			autoStart = 1
		} else {
			autoStart = 0
		}
	}

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(), `
		UPDATE server_monitor_config
		SET probe_interval = ?, probe_timeout = ?, log_retention_days = ?, max_connections = ?, session_timeout = ?, auto_start = ?, metrics_collect_interval = ?, metrics_retention_days = ?, updated_at = ?
		WHERE id = 1`,
		probeInterval, probeTimeout, logRetentionDays, maxConnections, sessionTimeout, autoStart, metricsCollectInterval, metricsRetentionDays, now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.getMonitorConfig(w, r, db)
}

func (s *Service) listMonitorLogs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	serverId := q.Get("serverId")
	status := q.Get("status")
	pageStr := q.Get("page")
	pageSizeStr := q.Get("pageSize")

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	pageSize := 50
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	offset := (page - 1) * pageSize

	sqlQuery := "SELECT id, server_id, status, response_time, error_message, checked_at FROM server_monitor_logs WHERE 1=1"
	sqlCount := "SELECT COUNT(*) FROM server_monitor_logs WHERE 1=1"
	var params []interface{}

	if serverId != "" {
		sqlQuery += " AND server_id = ?"
		sqlCount += " AND server_id = ?"
		params = append(params, serverId)
	}
	if status != "" {
		sqlQuery += " AND status = ?"
		sqlCount += " AND status = ?"
		params = append(params, status)
	}

	// Count total
	var total int
	err := db.QueryRowContext(r.Context(), sqlCount, params...).Scan(&total)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	sqlQuery += " ORDER BY checked_at DESC LIMIT ? OFFSET ?"
	params = append(params, pageSize, offset)

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id int
		var srvID, logStatus, checkedAt string
		var respTime sql.NullInt64
		var errMsg sql.NullString
		err := rows.Scan(&id, &srvID, &logStatus, &respTime, &errMsg, &checkedAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		respVal := interface{}(nil)
		if respTime.Valid {
			respVal = respTime.Int64
		}
		errVal := interface{}(nil)
		if errMsg.Valid {
			errVal = errMsg.String
		}

		logs = append(logs, map[string]interface{}{
			"id":            id,
			"server_id":     srvID,
			"status":        logStatus,
			"response_time": respVal,
			"error_message": errVal,
			"checked_at":    checkedAt,
		})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}

	writeLogsWithPagination(w, logs, total, page, pageSize)
}

func writeLogsWithPagination(w http.ResponseWriter, logs []map[string]interface{}, total, page, pageSize int) {
	totalPages := 0
	if pageSize > 0 {
		totalPages = total / pageSize
		if total%pageSize != 0 {
			totalPages++
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    logs,
		"pagination": map[string]interface{}{
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		},
	})
}

// ==========================================
// ACCOUNTS HANDLERS
// ==========================================

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, updated_at FROM server_accounts ORDER BY order_index ASC, created_at DESC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		account, err := s.scanAccount(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		list = append(list, account)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, updated_at FROM server_accounts WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	if !rows.Next() {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	}

	account, err := s.scanAccount(rows)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, account)
}

func (s *Service) createAccount(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Name        string      `json:"name"`
		Host        string      `json:"host"`
		Port        int         `json:"port"`
		Username    string      `json:"username"`
		AuthType    string      `json:"auth_type"`
		Password    string      `json:"password"`
		PrivateKey  string      `json:"private_key"`
		Passphrase  string      `json:"passphrase"`
		Tags        interface{} `json:"tags"`
		Description string      `json:"description"`
		Country     string      `json:"country"`
		StartsAt    string      `json:"starts_at"`
		ExpiresAt   string      `json:"expires_at"`
		MonitorMode string      `json:"monitor_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	isAgentMode := req.MonitorMode == "agent"
	if req.Name == "" || (!isAgentMode && (req.Host == "" || req.Username == "" || req.AuthType == "")) {
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	id := uuid.NewString()
	now := time.Now().Format(time.RFC3339)

	// Max order_index
	var maxOrder sql.NullInt64
	_ = db.QueryRowContext(r.Context(), "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
	orderIndex := int(maxOrder.Int64) + 1

	encPassword := s.encryptField(req.Password)
	encPrivateKey := s.encryptField(req.PrivateKey)
	encPassphrase := s.encryptField(req.Passphrase)

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO server_accounts (
			id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, order_index, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Host, coalesceInt(req.Port, 22), coalesceStr(req.Username, "agent"), coalesceStr(req.AuthType, "password"),
		encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(req.Tags), req.Description, coalesceStr(req.MonitorMode, "agent"), req.Country, nil, nullStr(req.StartsAt), nullStr(req.ExpiresAt), orderIndex, now, now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	account, err := s.queryAccountByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "服务器添加成功",
		"data":    account,
	})
}

func (s *Service) updateAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing raw
	var raw struct {
		name, host, username, authType, status, monitorMode, tags, createdAt string
		password, privateKey, passphrase                                     sql.NullString
		description, country, resolvedCountry, startsAt, expiresAt           sql.NullString
		port, orderIndex                                                     int
		responseTime                                                         sql.NullInt64
		lastCheckTime, lastCheckStatus, cachedInfo                           sql.NullString
	}
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, response_time, last_check_time, last_check_status, cached_info FROM server_accounts WHERE id = ?", id).
		Scan(&raw.name, &raw.host, &raw.port, &raw.username, &raw.authType, &raw.password, &raw.privateKey, &raw.passphrase, &raw.status, &raw.monitorMode, &raw.tags, &raw.description, &raw.country, &raw.resolvedCountry, &raw.startsAt, &raw.expiresAt, &raw.orderIndex, &raw.createdAt, &raw.responseTime, &raw.lastCheckTime, &raw.lastCheckStatus, &raw.cachedInfo)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	} else if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	name := getStringVal(req, "name", raw.name)
	host := getStringVal(req, "host", raw.host)
	port := getIntVal(req, "port", raw.port)
	username := getStringVal(req, "username", raw.username)
	authType := getStringVal(req, "auth_type", raw.authType)
	description := getStringVal(req, "description", raw.description.String)
	monitorMode := getStringVal(req, "monitor_mode", raw.monitorMode)
	country := getStringVal(req, "country", raw.country.String)
	resolvedCountry := getStringVal(req, "resolved_country", raw.resolvedCountry.String)
	startsAt := getStringVal(req, "starts_at", raw.startsAt.String)
	expiresAt := getStringVal(req, "expires_at", raw.expiresAt.String)
	orderIndex := getIntVal(req, "order_index", raw.orderIndex)

	password := raw.password.String
	if p, ok := req["password"].(string); ok {
		password = s.encryptFieldString(p)
	}
	privateKey := raw.privateKey.String
	if k, ok := req["private_key"].(string); ok {
		privateKey = s.encryptFieldString(k)
	}
	passphrase := raw.passphrase.String
	if p, ok := req["passphrase"].(string); ok {
		passphrase = s.encryptFieldString(p)
	}

	tags := raw.tags
	if val, ok := req["tags"]; ok {
		tags = SerializeList(val)
	}

	now := time.Now().Format(time.RFC3339)

	_, err = db.ExecContext(r.Context(), `
		UPDATE server_accounts
		SET name = ?, host = ?, port = ?, username = ?, auth_type = ?, password = ?, private_key = ?, passphrase = ?, tags = ?, description = ?, monitor_mode = ?, country = ?, resolved_country = ?, starts_at = ?, expires_at = ?, order_index = ?, updated_at = ?
		WHERE id = ?`,
		name, host, port, username, authType, password, privateKey, passphrase, tags, description, monitorMode, nullStr(country), nullStr(resolvedCountry), nullStr(startsAt), nullStr(expiresAt), orderIndex, now, id,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	account, err := s.queryAccountByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "服务器更新成功",
		"data":    account,
	})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	res, err := db.ExecContext(r.Context(), "DELETE FROM server_accounts WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "服务器删除成功",
	})
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, tags, description, country, resolved_country, starts_at, expires_at FROM server_accounts ORDER BY order_index ASC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var exportList []map[string]interface{}
	for rows.Next() {
		var name, host, username, authType, tagsStr string
		var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
		var port int
		err := rows.Scan(&name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		exportList = append(exportList, map[string]interface{}{
			"name":             name,
			"host":             host,
			"port":             port,
			"username":         username,
			"auth_type":        authType,
			"password":         s.decryptField(password),
			"private_key":      s.decryptField(privateKey),
			"passphrase":       s.decryptField(passphrase),
			"tags":             parseJSONTags(tagsStr),
			"description":      nullStringVal(description),
			"country":          nullStringVal(country),
			"resolved_country": nullStringVal(resolvedCountry),
			"starts_at":        nullStringVal(startsAt),
			"expires_at":       nullStringVal(expiresAt),
		})
	}
	if exportList == nil {
		exportList = []map[string]interface{}{}
	}
	response.OK(w, exportList)
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Servers []map[string]interface{} `json:"servers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var results []map[string]interface{}
	successCount := 0
	failedCount := 0

	for _, item := range req.Servers {
		name, _ := item["name"].(string)
		host, _ := item["host"].(string)
		portVal, _ := item["port"]
		username, _ := item["username"].(string)
		authType, _ := item["auth_type"].(string)
		password, _ := item["password"].(string)
		privateKey, _ := item["private_key"].(string)
		passphrase, _ := item["passphrase"].(string)
		tags := item["tags"]
		description, _ := item["description"].(string)
		country, _ := item["country"].(string)
		startsAt, _ := item["starts_at"].(string)
		expiresAt, _ := item["expires_at"].(string)

		port := 22
		if portVal != nil {
			if f, err := toFloat(portVal); err == nil {
				port = int(f)
			}
		}

		if name == "" {
			results = append(results, map[string]interface{}{"success": false, "error": "Missing name", "data": item})
			failedCount++
			continue
		}

		id := uuid.NewString()
		now := time.Now().Format(time.RFC3339)

		var maxOrder sql.NullInt64
		_ = db.QueryRowContext(r.Context(), "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
		orderIndex := int(maxOrder.Int64) + 1

		encPassword := s.encryptField(password)
		encPrivateKey := s.encryptField(privateKey)
		encPassphrase := s.encryptField(passphrase)

		_, err := db.ExecContext(r.Context(), `
			INSERT INTO server_accounts (
				id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, order_index, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, name, host, port, coalesceStr(username, "agent"), coalesceStr(authType, "password"),
			encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(tags), description, "agent", country, nil, nullStr(startsAt), nullStr(expiresAt), orderIndex, now, now,
		)

		if err != nil {
			results = append(results, map[string]interface{}{"success": false, "error": err.Error(), "data": item})
			failedCount++
		} else {
			acc, _ := s.queryAccountByID(r.Context(), db, id)
			results = append(results, map[string]interface{}{"success": true, "data": acc})
			successCount++
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("导入完成: 成功 %d, 失败 %d", successCount, failedCount),
		"results": results,
	})
}

func (s *Service) reorderAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		OrderData []struct {
			ID         string `json:"id"`
			OrderIndex int    `json:"order_index"`
		} `json:"orderData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(r.Context(), "UPDATE server_accounts SET order_index = ? WHERE id = ?")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer stmt.Close()

	for _, item := range req.OrderData {
		if _, err := stmt.ExecContext(r.Context(), item.OrderIndex, item.ID); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "重排成功",
	})
}

// ==========================================
// SCANNERS & UTILS
// ==========================================

func (s *Service) scanAccount(row *sql.Rows) (map[string]interface{}, error) {
	var id, name, host, username, authType, status, monitorMode, createdAt, updatedAt string
	var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
	var lastCheckTime, lastCheckStatus, cachedInfo sql.NullString
	var responseTime sql.NullInt64
	var port, orderIndex int
	var tagsStr string

	err := row.Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
	), nil
}

func (s *Service) queryAccountByID(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	var name, host, username, authType, status, monitorMode, createdAt, updatedAt string
	var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
	var lastCheckTime, lastCheckStatus, cachedInfo sql.NullString
	var responseTime sql.NullInt64
	var port, orderIndex int
	var tagsStr string

	err := db.QueryRowContext(ctx, "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, updated_at FROM server_accounts WHERE id = ?", id).
		Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
	), nil
}

func (s *Service) buildAccountResponse(
	id, name, host string, port int, username, authType string,
	password, privateKey, passphrase sql.NullString,
	status, monitorMode string,
	lastCheckTime, lastCheckStatus sql.NullString,
	responseTime sql.NullInt64,
	cachedInfo sql.NullString,
	tagsStr string,
	description, country, resolvedCountry, startsAt, expiresAt sql.NullString,
	orderIndex int,
	createdAt, updatedAt string,
) map[string]interface{} {
	isOnline := status == "online"

	decryptedPassword := s.decryptField(password)
	decryptedPrivateKey := s.decryptField(privateKey)
	decryptedPassphrase := s.decryptField(passphrase)

	capabilities := getServerCapabilities(host, port, username, authType, decryptedPrivateKey, decryptedPassword, isOnline)

	res := map[string]interface{}{
		"id":                id,
		"name":              name,
		"host":              host,
		"port":              port,
		"username":          username,
		"auth_type":         authType,
		"password":          decryptedPassword,
		"private_key":       decryptedPrivateKey,
		"passphrase":        decryptedPassphrase,
		"status":            status,
		"monitor_mode":      monitorMode,
		"last_check_time":   nullStringVal(lastCheckTime),
		"last_check_status": nullStringVal(lastCheckStatus),
		"response_time":     nullIntVal(responseTime),
		"tags":              parseJSONTags(tagsStr),
		"description":       nullStringVal(description),
		"country":           nullStringVal(country),
		"resolved_country":  nullStringVal(resolvedCountry),
		"starts_at":         nullStringVal(startsAt),
		"expires_at":        nullStringVal(expiresAt),
		"order_index":       orderIndex,
		"created_at":        createdAt,
		"updated_at":        updatedAt,
	}

	for k, v := range capabilities {
		res[k] = v
	}

	// Metrics mapping
	if cachedInfo.Valid && cachedInfo.String != "" {
		var cachedMetrics map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cachedMetrics); err == nil {
			res["info"] = s.buildInfoField(cachedMetrics)
		}
	}

	return res
}

var diskRegexp = regexp.MustCompile(`([^/]+)/([^\s]+)\s\((\d+(?:\.\d+)?%?)\)`)

func (s *Service) buildInfoField(metrics map[string]interface{}) map[string]interface{} {
	cpuLoad := ""
	if l, ok := metrics["load"].(string); ok {
		cpuLoad = l
	}
	cpuCores := 1
	if c, ok := metrics["cores"]; ok {
		if f, err := toFloat(c); err == nil {
			cpuCores = int(f)
		}
	}
	logicalCores := cpuCores
	if c, ok := metrics["logical_cores"]; ok {
		if f, err := toFloat(c); err == nil {
			logicalCores = int(f)
		}
	}
	physicalCores := cpuCores
	if c, ok := metrics["physical_cores"]; ok {
		if f, err := toFloat(c); err == nil {
			physicalCores = int(f)
		}
	}
	cpuUsage := "0%"
	if u, ok := metrics["cpu_usage"].(string); ok {
		cpuUsage = u
	}
	var cpuTemp float64 = 0
	if t, ok := metrics["cpu_temp"]; ok {
		if f, err := toFloat(t); err == nil {
			cpuTemp = f
		}
	}
	var cpuPower float64 = 0
	if p, ok := metrics["cpu_power"]; ok {
		if f, err := toFloat(p); err == nil {
			cpuPower = f
		}
	}

	// Memory
	memPercent := ""
	if mp, ok := metrics["mem_percent"]; ok {
		if f, err := toFloat(mp); err == nil {
			memPercent = fmt.Sprintf("%.0f%%", f)
		}
	}
	mem := ""
	if m, ok := metrics["mem"].(string); ok {
		mem = m
	}
	memUsed := "-"
	memTotal := "-"
	if mem != "" {
		parts := strings.Split(mem, "/")
		if len(parts) == 2 {
			memUsed = parts[0]
			memTotal = parts[1]
		}
	}

	// Disk
	diskStr := ""
	if d, ok := metrics["disk"].(string); ok {
		diskStr = d
	}
	diskArray := []map[string]interface{}{}
	if diskStr != "" {
		diskMatch := diskRegexp.FindStringSubmatch(diskStr)
		if len(diskMatch) == 4 {
			diskArray = append(diskArray, map[string]interface{}{
				"device": "/",
				"used":   diskMatch[1],
				"total":  diskMatch[2],
				"usage":  diskMatch[3],
			})
		}
	}

	dockerVal := metrics["docker"]
	if dockerVal == nil {
		dockerVal = map[string]interface{}{"installed": false, "running": 0, "stopped": 0, "containers": []interface{}{}}
	}
	networkVal := metrics["network"]

	platform := ""
	if p, ok := metrics["platform"].(string); ok {
		platform = p
	}
	platformVersion := ""
	if pv, ok := metrics["platformVersion"].(string); ok {
		platformVersion = pv
	}
	agentVersion := ""
	if av, ok := metrics["agent_version"].(string); ok {
		agentVersion = av
	}
	ip := ""
	if i, ok := metrics["ip"].(string); ok {
		ip = i
	}
	uptime := ""
	if u, ok := metrics["uptime"].(string); ok {
		uptime = u
	}

	lastUpdate := "-"
	if lu, ok := metrics["lastUpdate"].(string); ok {
		lastUpdate = lu
	} else if ts, ok := metrics["timestamp"]; ok {
		if f, err := toFloat(ts); err == nil {
			t := time.UnixMilli(int64(f))
			lastUpdate = t.Format("15:04:05")
		}
	}

	return map[string]interface{}{
		"cpu": map[string]interface{}{
			"Load":          cpuLoad,
			"Cores":         cpuCores,
			"LogicalCores":  logicalCores,
			"PhysicalCores": physicalCores,
			"Usage":         cpuUsage,
			"Temp":          cpuTemp,
			"Power":         cpuPower,
		},
		"memory": map[string]interface{}{
			"Usage": memPercent,
			"Used":  memUsed,
			"Total": memTotal,
		},
		"disk":            diskArray,
		"docker":          dockerVal,
		"network":         networkVal,
		"gpu":             buildGpuInfo(metrics),
		"platform":        platform,
		"platformVersion": platformVersion,
		"agentVersion":    agentVersion,
		"ip":              ip,
		"uptime":          uptime,
		"lastUpdate":      lastUpdate,
	}
}

func (s *Service) encryptField(value string) interface{} {
	if value == "" {
		return nil
	}
	enc, err := secure.SecureEncrypt(value)
	if err != nil {
		return value
	}
	return enc
}

func (s *Service) encryptFieldString(value string) string {
	encrypted := s.encryptField(value)
	if encrypted == nil {
		return ""
	}
	if text, ok := encrypted.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", encrypted)
}

func (s *Service) decryptField(field sql.NullString) string {
	if !field.Valid || field.String == "" {
		return ""
	}
	return secure.SecureDecrypt(field.String)
}

func parseJSONTags(s string) []string {
	if s == "" {
		return []string{}
	}
	var res []string
	if err := json.Unmarshal([]byte(s), &res); err != nil {
		return []string{}
	}
	return res
}

func coalesceStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func coalesceInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullStringVal(s sql.NullString) interface{} {
	if !s.Valid {
		return nil
	}
	return s.String
}

func nullIntVal(i sql.NullInt64) interface{} {
	if !i.Valid {
		return nil
	}
	return i.Int64
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getStringVal(m map[string]interface{}, key, fallback string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return fallback
}

func getIntVal(m map[string]interface{}, key string, fallback int) int {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return int(f)
		}
	}
	return fallback
}

func hasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan %s column: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	return false, nil
}

func toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	case string:
		trimmed := strings.TrimSpace(strings.TrimSuffix(v, "%"))
		if trimmed == "" {
			return 0, errors.New("empty numeric string")
		}
		return strconv.ParseFloat(trimmed, 64)
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", value)
	}
}

func getServerCapabilities(host string, port int, username, authType, privateKey, password string, isOnline bool) map[string]interface{} {
	hasPassword := password != ""
	hasPrivateKey := privateKey != ""
	sshConfigured := host != "" && port > 0 && username != "" && ((authType == "key" && hasPrivateKey) || (authType != "key" && hasPassword))

	return map[string]interface{}{
		"is_online":         isOnline,
		"ssh_configured":    sshConfigured,
		"agent_connected":   isOnline,
		"has_password":      hasPassword,
		"has_private_key":   hasPrivateKey,
		"supports_agent":    true,
		"supports_ssh":      sshConfigured,
		"supports_sftp":     sshConfigured || isOnline,
		"supports_terminal": sshConfigured || isOnline,
		"supports_docker":   isOnline,
		"supports_metrics":  isOnline,
		"connection_type":   "agent",
		"capability_source": "go-serveragent",
	}
}

func buildGpuInfo(metrics map[string]interface{}) []map[string]interface{} {
	if raw, ok := metrics["gpu"]; ok {
		switch g := raw.(type) {
		case []map[string]interface{}:
			return g
		case []interface{}:
			result := make([]map[string]interface{}, 0, len(g))
			for _, item := range g {
				if obj, ok := item.(map[string]interface{}); ok {
					result = append(result, obj)
				}
			}
			if result != nil {
				return result
			}
		case map[string]interface{}:
			return []map[string]interface{}{g}
		}
	}

	usage := metricFloat(metrics, "gpu_usage")
	memUsed := metricFloat(metrics, "gpu_mem_used")
	memTotal := metricFloat(metrics, "gpu_mem_total")
	power := metricFloat(metrics, "gpu_power")
	temp := metricFloat(metrics, "gpu_temp")
	if usage == 0 && memUsed == 0 && memTotal == 0 && power == 0 && temp == 0 {
		return []map[string]interface{}{}
	}

	return []map[string]interface{}{{
		"name":        stringMetric(metrics, "gpu_name", "GPU"),
		"usage":       usage,
		"memUsed":     memUsed,
		"memTotal":    memTotal,
		"power":       power,
		"temp":        temp,
		"memoryUsed":  memUsed,
		"memoryTotal": memTotal,
	}}
}

func metricFloat(metrics map[string]interface{}, key string) float64 {
	if value, ok := metrics[key]; ok {
		if f, err := toFloat(value); err == nil {
			return f
		}
	}
	return 0
}

func stringMetric(metrics map[string]interface{}, key, fallback string) string {
	if value, ok := metrics[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, err := toFloat(v); err == nil {
			return f
		}
	}
	return 0
}

func getIntValue(m map[string]interface{}, key string) int {
	return int(getFloatValue(m, key))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatSpeed(bytesPerSecond float64) string {
	return formatBytes(int64(bytesPerSecond)) + "/s"
}

func formatUptime(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func (s *Service) buildCachedInfo(state map[string]interface{}, hostInfo map[string]interface{}) map[string]interface{} {
	cached := make(map[string]interface{})

	// Start with host metadata, then let the fresh report win for dynamic fields
	// such as timestamp_ms, sequence, CPU, memory, disk, and network counters.
	for k, v := range hostInfo {
		if v != nil && v != "" {
			cached[k] = v
		}
	}
	for k, v := range state {
		cached[k] = v
	}

	// Calculate and format cpu
	cpu := getFloatValue(state, "cpu")
	cached["cpu"] = cpu
	cached["cpu_usage"] = fmt.Sprintf("%.1f%%", cpu) // 格式化为字符串，前端实时显示使用

	// Load
	load1 := getFloatValue(state, "load1")
	load5 := getFloatValue(state, "load5")
	load15 := getFloatValue(state, "load15")
	cached["load"] = fmt.Sprintf("%.2f %.2f %.2f", load1, load5, load15)

	// Cores and CPU info
	cores := getIntValue(hostInfo, "cores")
	if cores == 0 {
		cores = getIntValue(hostInfo, "LogicalCores")
	}
	cached["cores"] = cores
	cached["logical_cores"] = getIntValue(hostInfo, "logical_cores")
	if getIntValue(cached, "logical_cores") == 0 {
		cached["logical_cores"] = getIntValue(hostInfo, "LogicalCores")
	}
	cached["physical_cores"] = getIntValue(hostInfo, "physical_cores")
	if getIntValue(cached, "physical_cores") == 0 {
		cached["physical_cores"] = getIntValue(hostInfo, "PhysicalCores")
	}

	// Memory calculations
	memUsed := getFloatValue(state, "mem_used")
	memTotal := getFloatValue(hostInfo, "mem_total_raw")
	if memTotal == 0 {
		memTotal = getFloatValue(hostInfo, "mem_total")
	}
	if memTotal == 0 {
		memTotal = getFloatValue(state, "mem_total")
	}
	var memPercent float64
	if memTotal > 0 {
		memPercent = (memUsed / memTotal) * 100
	}
	memUsedMB := int(memUsed / 1024 / 1024)
	memTotalMB := int(memTotal / 1024 / 1024)

	cached["mem_used_mb"] = memUsedMB
	cached["mem_total_mb"] = memTotalMB
	cached["mem_usage_percent"] = memPercent
	cached["mem_percent"] = memPercent
	cached["mem"] = fmt.Sprintf("%d/%dMB", memUsedMB, memTotalMB)
	cached["mem_usage"] = fmt.Sprintf("%d/%dMB", memUsedMB, memTotalMB) // 前端期望的字段名
	cached["memory"] = memPercent
	cached["mem_total_raw"] = memTotal
	cached["mem_used_raw"] = memUsed

	// Disk calculations
	diskUsed := getFloatValue(state, "disk_used")
	diskTotal := getFloatValue(hostInfo, "disk_total_raw")
	if diskTotal == 0 {
		diskTotal = getFloatValue(hostInfo, "disk_total")
	}
	if diskTotal == 0 {
		diskTotal = getFloatValue(state, "disk_total")
	}
	var diskPercent float64
	if diskTotal > 0 {
		diskPercent = (diskUsed / diskTotal) * 100
	}
	cached["disk_used"] = formatBytes(int64(diskUsed))
	cached["disk_total"] = formatBytes(int64(diskTotal))
	cached["disk_usage"] = diskPercent // Float for persistMetrics
	cached["disk_percent"] = diskPercent
	cached["disk"] = fmt.Sprintf("%s/%s (%.0f%%)", formatBytes(int64(diskUsed)), formatBytes(int64(diskTotal)), diskPercent)
	cached["disk_total_raw"] = diskTotal
	cached["disk_used_raw"] = diskUsed

	// Network speed
	netInSpeed := getFloatValue(state, "net_in_speed")
	netOutSpeed := getFloatValue(state, "net_out_speed")
	netInTransfer := getFloatValue(state, "net_in_transfer")
	netOutTransfer := getFloatValue(state, "net_out_transfer")
	tcpConn := getIntValue(state, "tcp_conn_count")
	udpConn := getIntValue(state, "udp_conn_count")

	cached["net_rx"] = netInSpeed
	cached["net_tx"] = netOutSpeed

	networkMap := map[string]interface{}{
		"rx_speed":    formatSpeed(netInSpeed),
		"tx_speed":    formatSpeed(netOutSpeed),
		"down":        formatSpeed(netInSpeed),
		"up":          formatSpeed(netOutSpeed),
		"rx_total":    formatBytes(int64(netInTransfer)),
		"tx_total":    formatBytes(int64(netOutTransfer)),
		"connections": tcpConn + udpConn,
	}
	cached["network"] = networkMap

	// Uptime
	uptime := getFloatValue(state, "uptime")
	cached["uptime"] = formatUptime(int64(uptime))

	// Docker
	if docker, ok := state["docker"].(map[string]interface{}); ok {
		cached["docker"] = docker
	} else if docker, ok := state["docker"]; ok {
		cached["docker"] = docker
	}

	// GPU
	gpuMemUsed := getFloatValue(state, "gpu_mem_used")
	gpuMemTotal := getFloatValue(hostInfo, "gpu_mem_total")
	if gpuMemTotal == 0 {
		gpuMemTotal = getFloatValue(state, "gpu_mem_total")
	}
	var gpuMemPercent float64
	if gpuMemTotal > 0 {
		gpuMemPercent = (gpuMemUsed / gpuMemTotal) * 100
	}
	cached["gpu_usage"] = getFloatValue(state, "gpu")
	cached["gpu_mem_used"] = gpuMemUsed
	cached["gpu_mem_total"] = gpuMemTotal
	cached["gpu_mem_percent"] = gpuMemPercent
	cached["gpu_temp"] = getFloatValue(state, "gpu_temp")
	cached["gpu_power"] = getFloatValue(state, "gpu_power")

	return cached
}
