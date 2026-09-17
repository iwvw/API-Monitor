package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

type alertState struct {
	cpuHigh     bool
	memoryHigh  bool
	diskHigh    bool
	trafficHigh bool
}

type Service struct {
	cfg                           config.Config
	store                         *database.Store
	now                           func() time.Time
	taskRegistry                  *TaskRegistry
	agentBatches                  *AgentBatchManager
	engineIO                      *EngineIOServer
	registry                      *ConnectionRegistry
	metricsHub                    *MetricsHub
	ptyHub                        *ptyDataHub
	presence                      *agentPresenceManager
	terminalBroker                *agentTerminalBroker
	agentPortBroker               *agentPortBroker
	remoteDesktop                 *remoteDesktopManager
	lastCollect                   time.Time
	lastCollectMu                 sync.RWMutex
	lastPersist                   map[string]time.Time
	lastPersistMu                 sync.Mutex
	lastNetworkQualityPersist     map[string]time.Time
	lastNetworkQualityPersistMu   sync.Mutex
	realtimePersistInterval       time.Duration
	networkQualityPersistInterval time.Duration
	tunnelHealthCheckInterval     time.Duration
	tunnelHealthCheckAttempts     int
	tunnelHealthCheckDelay        time.Duration
	tunnelReconcileMaxAttempts    int
	tunnelReconcileBaseInterval   time.Duration
	agentTaskWaiters              sync.Map
	autoLocationRefreshes         sync.Map
	lastAutoLocationRefresh       sync.Map
	targetsCache                  []networkQualityTarget
	targetsCacheMu                sync.RWMutex
	notifier                      Notifier
	cloudflare                    cloudflare.ManagedTunnelAPI
	alertStates                   sync.Map // serverID -> *alertState
	forwardReconcileMu            sync.Mutex
	forwardReconcileAt            map[string]time.Time // serverID -> 最近一次转发对账时间（防风暴）
	backgroundCtx                 context.Context
	backgroundCancel              context.CancelFunc
	backgroundWG                  sync.WaitGroup
	pendingWG                     sync.WaitGroup
	pendingWGClosed               atomic.Bool
	pendingWGAddMu                sync.Mutex
	stopOnce                      sync.Once
	startupErr                    error
}

const defaultRealtimeMetricsPersistInterval = 30 * time.Second
const minRealtimeMetricsPersistInterval = 10 * time.Second
const defaultNetworkQualityPersistInterval = time.Minute
const minNetworkQualityPersistInterval = 30 * time.Second
const agentMetricsStaleAfter = 45 * time.Second

func New(cfg config.Config) *Service {
	registry := NewConnectionRegistry()
	taskRegistry := NewTaskRegistry()
	store := database.New(cfg)
	agentBatches := NewAgentBatchManager()
	metricsHub := NewMetricsHub()
	ptyHub := newPtyDataHub()
	engineIO := NewEngineIOServer(registry)
	engineIO.metricsHub = metricsHub

	s := &Service{
		cfg:                           cfg,
		store:                         store,
		now:                           time.Now,
		taskRegistry:                  taskRegistry,
		agentBatches:                  agentBatches,
		engineIO:                      engineIO,
		registry:                      registry,
		metricsHub:                    metricsHub,
		ptyHub:                        ptyHub,
		terminalBroker:                newAgentTerminalBroker(),
		agentPortBroker:               newAgentPortBroker(),
		remoteDesktop:                 newRemoteDesktopManager(),
		lastPersist:                   make(map[string]time.Time),
		lastNetworkQualityPersist:     make(map[string]time.Time),
		realtimePersistInterval:       resolveRealtimeMetricsPersistInterval(),
		networkQualityPersistInterval: resolveNetworkQualityPersistInterval(),
		tunnelHealthCheckInterval:     5 * time.Minute,
		tunnelHealthCheckAttempts:     3,
		tunnelHealthCheckDelay:        3 * time.Second,
		tunnelReconcileMaxAttempts:    3,
		tunnelReconcileBaseInterval:   5 * time.Minute,
	}
	engineIO.service = s
	s.presence = newAgentPresenceManager(s)
	s.forwardReconcileAt = map[string]time.Time{}

	s.backgroundCtx, s.backgroundCancel = context.WithCancel(context.Background())

	// 绑定 Engine.IO 事件处理器
	engineIO.SetHandlers(
		// onConnect: Agent 连接成功
		func(sessionID string, serverID string) {
			applog.Info(s.backgroundCtx, "serveragent", "agent connected", "session_id", sessionID, "server_id", serverID)
			if serverID != "" {
				var socket interface{}
				transport := ""
				capabilities := map[string]bool{}
				if sess := engineIO.getSession(sessionID); sess != nil {
					sess.mu.RLock()
					socket = sess
					transport = sess.Transport
					for _, capability := range sess.Capabilities {
						capabilities[capability] = true
					}
					sess.mu.RUnlock()
				}
				conn := registry.Register(serverID, socket) // 注册到连接池
				if len(capabilities) > 0 {
					conn.UpdateCapabilities(capabilities)
				}
				if sess := engineIO.getSession(sessionID); sess != nil {
					sess.mu.RLock()
					if sess.Hostname != "" {
						conn.SetMetadata("hostname", sess.Hostname)
					}
					if sess.Version != "" {
						conn.SetMetadata("version", sess.Version)
						conn.SetMetadata("agent_version", sess.Version)
					}
					if sess.Platform != "" {
						conn.SetMetadata("platform", sess.Platform)
					}
					if sess.Arch != "" {
						conn.SetMetadata("arch", sess.Arch)
					}
					if sess.RemoteIP != "" {
						conn.SetMetadata("connection_ip", sess.RemoteIP)
					}
					sess.mu.RUnlock()
				}
				if s.presence != nil && s.presence.legacyMode() {
					s.markAgentOnlineLegacy(serverID)
				} else if s.presence != nil {
					s.presence.recordConnect(serverID, transport)
				}
				if s.backgroundCtx.Err() == nil {
					if s.trackPending() {
						go func() {
							defer s.pendingWG.Done()
							s.refreshAccountLocationFromAgentIfMissing(serverID)
						}()
					}
					if s.trackPending() {
						go func(id string) {
							defer s.pendingWG.Done()
							time.Sleep(2 * time.Second)
							s.reconcileManagedProxyFacts(id)
						}(serverID)
					}
					// agent 重连后重放其负责的 running 转发（源桥接/中继监听），
					// 解决 agent/relay 重启后转发链路不自动恢复的问题。
					if s.trackPending() {
						go func(id string) {
							defer s.pendingWG.Done()
							time.Sleep(3 * time.Second)
							ctx, cancel := context.WithTimeout(s.backgroundCtx, 3*time.Minute)
							defer cancel()
							db, err := s.open(ctx)
							if err != nil {
								return
							}
							defer db.Close()
							s.reconcileRunningForwards(ctx, db, id)
						}(serverID)
					}
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
						s.recordAgentSignal(serverID, "state", state)

						// 提取并异步持久化 Agent 上报的网络波动质量指标
						if nqData, hasNq := state["network_quality"]; hasNq && nqData != nil {
							go func(nq interface{}, sid string) {
								ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
								defer cancel()
								db, err := s.open(ctx)
								if err == nil {
									defer db.Close()
									s.processAgentNetworkQuality(ctx, db, sid, nq)
								}
							}(nqData, serverID)
						}

						// 提取主机静态信息（比如核心数、总内存等，用于计算百分比）
						var hostInfo map[string]interface{}
						if conn, exists := registry.Get(serverID); exists {
							hostInfo = conn.GetMetadata()
						}

						// 格式化并合并为 cached_info 的 map
						cachedInfoMap := s.buildCachedInfo(state, hostInfo)
						s.markRealtimeMetricsHealthy(serverID, cachedInfoMap, time.Now())

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
							persistedInfoMap := cloneMap(cachedInfoMap)
							go func() {
								ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
								defer cancel()
								db, err := s.open(ctx)
								if err == nil {
									defer db.Close()
									persistedInfoMap = s.mergeCachedLocationFieldsFromDB(ctx, db, serverID, persistedInfoMap)
									if host, changed, hostErr := backfillAccountHostFromAgent(ctx, db, serverID, persistedInfoMap); hostErr != nil {
										applog.Warn(ctx, "serveragent", "failed to backfill host from Agent", "server_id", serverID, "error", hostErr.Error())
									} else if changed {
										applog.Info(ctx, "serveragent", "backfilled placeholder host from Agent", "server_id", serverID, "host", host)
									}
									now := time.Now().Format("2006-01-02 15:04:05")
									cachedInfoJSON, _ := json.Marshal(persistedInfoMap)
									_, _ = db.ExecContext(ctx, `UPDATE server_accounts
									SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, cached_info = ?, updated_at = ?
									WHERE id = ?`, now, string(cachedInfoJSON), now, serverID)

									if err := s.persistMetrics(ctx, db, serverID, persistedInfoMap); err != nil {
										s.markRealtimeMetricsPersistResult(serverID, false, err, time.Now())
										applog.Warn(ctx, "serveragent", "failed to persist realtime metrics", "server_id", serverID, "error", err.Error())
									} else {
										s.markRealtimeMetricsPersistResult(serverID, true, nil, time.Now())
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
						s.recordAgentSignal(serverID, "host_info", hostInfo)
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
								fullCachedInfo = s.mergeCachedLocationFieldsFromDB(ctx, db, serverID, fullCachedInfo)
								if host, changed, hostErr := backfillAccountHostFromAgent(ctx, db, serverID, fullCachedInfo); hostErr != nil {
									applog.Warn(ctx, "serveragent", "failed to backfill host from Agent", "server_id", serverID, "error", hostErr.Error())
								} else if changed {
									applog.Info(ctx, "serveragent", "backfilled placeholder host from Agent", "server_id", serverID, "host", host)
								}
								now := time.Now().Format("2006-01-02 15:04:05")
								cachedInfoJSON, _ := json.Marshal(fullCachedInfo)
								_, _ = db.ExecContext(ctx, `UPDATE server_accounts
									SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, cached_info = ?, updated_at = ?
									WHERE id = ?`, now, string(cachedInfoJSON), now, serverID)
							}
						}()
					}
				}
			case "agent:upgrade_status":
				// Agent 自更新结果上报（后台 updater 下载/替换成功或失败）。
				// 写入连接元数据，供批量升级等流程中的重连检测提前判定成败，
				// 避免 Agent 下载失败时只能干等验证超时。
				var upgradeStatus map[string]interface{}
				if err := json.Unmarshal(data, &upgradeStatus); err == nil {
					if conn, exists := registry.Get(serverID); exists {
						conn.SetMetadata("upgrade_status", upgradeStatus)
					}
					applog.Info(s.backgroundCtx, "serveragent", "agent self-upgrade status report", "server_id", serverID, "status", string(data))
				}
			case "agent:task_result":
				// Agent 任务结果上报
				var result struct {
					ID         string `json:"id"`
					Type       int    `json:"type"`
					Successful bool   `json:"successful"`
					Data       string `json:"data"`
					Delay      int64  `json:"delay"`
				}
				if err := json.Unmarshal(data, &result); err == nil {
					s.recordAgentSignal(serverID, "task_result", nil)
					if result.Successful {
						s.taskRegistry.Complete(result.ID, result.Data)
					} else {
						s.taskRegistry.Fail(result.ID, result.Data)
						// AI Agent 数据通道任务失败时唤醒等待方，返回具体错误而非空等超时。
						if s.agentPortBroker != nil {
							s.agentPortBroker.markAgentFailure(result.ID, result.Data)
						}
					}
				}
			case "agent:task_progress":
				// Agent 任务进度上报
				var prog struct {
					TaskID     string `json:"task_id"`
					Name       string `json:"name"`
					Percentage int    `json:"percentage"`
					Message    string `json:"message"`
					DetailMsg  string `json:"detail_msg"`
					IsDone     bool   `json:"is_done"`
					IsError    bool   `json:"is_error"`
				}
				if err := json.Unmarshal(data, &prog); err == nil {
					s.recordAgentSignal(serverID, "task_result", nil)
					s.taskRegistry.UpdateProgress(prog.TaskID, prog.Percentage, prog)
				}
			case "agent:pty_data":
				var ptyData struct {
					ID   string `json:"id"`
					Data string `json:"data"`
				}
				if err := json.Unmarshal(data, &ptyData); err == nil && ptyData.ID != "" {
					if s.ptyHub != nil {
						s.ptyHub.Publish(ptyData.ID, ptyData.Data)
					}
				}
			case "agent:pty_status":
				var ptyStatus struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Error  string `json:"error"`
				}
				if err := json.Unmarshal(data, &ptyStatus); err == nil && ptyStatus.ID != "" {
					s.recordAgentSignal(serverID, "pty_status", nil)
					if s.ptyHub != nil {
						s.ptyHub.Publish("status:"+ptyStatus.ID, string(data))
					}
				}
			case "agent:rd_signal":
				s.handleRemoteDesktopAgentSignal(serverID, data)
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
					s.recordAgentSignal(serverID, "heartbeat", nil)
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
						s.recordAgentSignal(serverID, "metrics", state)
					}
				}
			}
		},
		// onDisconnect: Agent 断开连接
		func(sessionID string) {
			applog.Info(s.backgroundCtx, "serveragent", "agent disconnected", "session_id", sessionID)
			// 查找此 session 对应的 serverID 并广播离线状态
			if sess := engineIO.getSession(sessionID); sess != nil {
				sess.mu.RLock()
				sid := sess.ServerID
				ns := sess.Namespace
				sess.mu.RUnlock()
				if ns != "/metrics" && sid != "" && registry.DisconnectIfSocket(sid, sess) {
					if s.terminalBroker != nil {
						s.terminalBroker.closeForServer(sid, "agent_control_disconnected")
					}
					if s.agentPortBroker != nil {
						s.agentPortBroker.closeForServer(sid, "agent_control_disconnected")
					}
					if s.presence != nil && s.presence.legacyMode() {
						s.markAgentOfflineLegacy(sid)
					} else if s.presence != nil {
						s.presence.recordDisconnect(sid, "socket_disconnected")
					}
				}
			}
		},
	)

	ctx, cancel := context.WithTimeout(s.backgroundCtx, 10*time.Second)
	defer cancel()
	if db, err := s.store.Open(ctx); err == nil {
		if schemaErr := database.WithSchemaLock(ctx, func() error { return ensureSchema(ctx, db) }); schemaErr != nil {
			applog.Error(s.backgroundCtx, "serveragent", "ensure schema failed", "error", schemaErr.Error())
			s.startupErr = schemaErr
		}
		db.Close()
	} else {
		applog.Error(s.backgroundCtx, "serveragent", "open database during startup failed", "error", err.Error())
		s.startupErr = err
	}
	if s.startupErr != nil {
		return s
	}
	taskPersistence := newSQLiteTaskPersistence(store)
	if err := taskPersistence.Ensure(ctx); err != nil {
		applog.Error(s.backgroundCtx, "serveragent", "ensure task persistence failed", "error", err.Error())
		s.startupErr = fmt.Errorf("ensure task persistence: %w", err)
		return s
	} else if err := taskRegistry.AttachPersistence(ctx, taskPersistence); err != nil {
		applog.Error(s.backgroundCtx, "serveragent", "restore task persistence failed", "error", err.Error())
		s.startupErr = fmt.Errorf("restore task persistence: %w", err)
		return s
	}

	s.initTargetsCache()

	// Start background telemetry metrics collection loop
	if s.presence != nil {
		s.presence.start()
	}
	backgroundCtx := s.backgroundCtx
	s.backgroundWG.Add(7)
	go func() {
		defer s.backgroundWG.Done()
		StartReleaseAssetResolver(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startMetricsCollectorLoop(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startManagedProxyFactsLoop(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startSubscriptionReconcileLoop(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startManagedTunnelHealthLoop(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startForwardHealthLoop(backgroundCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.startForwardConnectorSyncLoop(backgroundCtx)
	}()

	return s
}

// trackPending 注册一个待跟踪的后台 goroutine。返回 false 表示服务已进入
// 关闭流程（pendingWG 已被 Wait），此时不应再启动新的后台任务，避免
// WaitGroup 计数在 Wait 期间再次变为 0 时触发 Add 竞态 panic。
func (s *Service) trackPending() bool {
	s.pendingWGAddMu.Lock()
	defer s.pendingWGAddMu.Unlock()
	if s.pendingWGClosed.Load() {
		return false
	}
	s.pendingWG.Add(1)
	return true
}

// Stop terminates background work owned by the service. It is idempotent so
// tests and process shutdown can safely share the same cleanup path.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.backgroundCancel != nil {
			s.backgroundCancel()
		}
		s.pendingWGAddMu.Lock()
		s.pendingWGClosed.Store(true)
		s.pendingWGAddMu.Unlock()
		s.backgroundWG.Wait()
		s.pendingWG.Wait()
		if s.presence != nil {
			s.presence.stop()
		}
		if s.registry != nil {
			s.registry.Stop()
		}
	})
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	return s.store.Open(ctx)
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
	if r.URL.Path == "/ws/agent-terminal" {
		s.handleAgentTerminalStream(w, r)
		return
	}
	if r.URL.Path == "/ws/agent-port" {
		s.handleAgentPortStream(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/server")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	if len(parts) >= 1 && parts[0] == "public" {
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "database connection failed: "+err.Error())
			return
		}
		defer db.Close()
		s.handlePublicStatusPageRoutes(w, r, db, parts[1:])
		return
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

	case len(parts) >= 1 && parts[0] == "status-pages":
		s.handleStatusPageRoutes(w, r, db, parts[1:])

	// Agent routes (Wave 5b)
	case len(parts) >= 1 && parts[0] == "agent":
		s.handleAgentRoutes(w, r, db, parts[1:])

	case len(parts) >= 1 && parts[0] == "remote-desktop":
		s.handleRemoteDesktopRoutes(w, r, parts[1:])

	// 托管转发规则（面板侧路径 /api/server/forward，与 agent/forward 同 handler）
	case len(parts) >= 1 && parts[0] == "forward":
		s.handleManagedForwardRoutes(w, r, db, parts[1:])

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
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "refresh-locations" && r.Method == http.MethodPost:
		s.refreshAccountLocations(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "export" && r.Method == http.MethodGet:
		s.exportAccounts(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "import" && r.Method == http.MethodPost:
		s.importAccounts(w, r, db)
	case len(parts) == 2 && parts[0] == "accounts" && parts[1] == "reorder" && r.Method == http.MethodPost:
		s.reorderAccounts(w, r, db)
	case len(parts) == 3 && parts[0] == "accounts" && parts[2] == "test-traffic-alert" && r.Method == http.MethodPost:
		s.testTrafficAlert(w, r, db, parts[1])
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
		"rx_speed":       formatSpeed(netInSpeed),
		"tx_speed":       formatSpeed(netOutSpeed),
		"down":           formatSpeed(netInSpeed),
		"up":             formatSpeed(netOutSpeed),
		"rx_total":       formatBytes(int64(netInTransfer)),
		"tx_total":       formatBytes(int64(netOutTransfer)),
		"rx_total_bytes": int64(netInTransfer),
		"tx_total_bytes": int64(netOutTransfer),
		"connections":    tcpConn + udpConn,
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

	if cpuModel := extractCPUModel(hostInfo); cpuModel != "" {
		cached["cpu_model"] = cpuModel
	}
	if gpuModel := extractGPUModel(hostInfo); gpuModel != "" {
		cached["gpu_model"] = gpuModel
	}

	return cached
}

func (s *Service) initTargetsCache() {
	db, err := s.open(s.backgroundCtx)
	if err != nil {
		return
	}
	defer db.Close()
	targets, _ := s.listNetworkQualityTargets(s.backgroundCtx, db)
	s.targetsCacheMu.Lock()
	s.targetsCache = targets
	s.targetsCacheMu.Unlock()
}

func (s *Service) getTargetsCache() []networkQualityTarget {
	s.targetsCacheMu.RLock()
	defer s.targetsCacheMu.RUnlock()
	if s.targetsCache == nil {
		return []networkQualityTarget{}
	}
	copied := make([]networkQualityTarget, len(s.targetsCache))
	copy(copied, s.targetsCache)
	return copied
}

func (s *Service) setTargetsCache(targets []networkQualityTarget) {
	s.targetsCacheMu.Lock()
	s.targetsCache = targets
	s.targetsCacheMu.Unlock()
}

func (s *Service) checkMetricAlerts(ctx context.Context, db *sql.DB, serverID string, cpu, mem, disk float64) {
	if s.notifier == nil {
		return
	}
	val, _ := s.alertStates.LoadOrStore(serverID, &alertState{})
	state := val.(*alertState)

	var serverName, serverHost string
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertEnabled int
	var trafficAlertPercent float64
	var cachedInfo sql.NullString
	var trafficCycleType string
	var trafficCycleBaseline int64
	_ = db.QueryRowContext(ctx, `SELECT name, host, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, cached_info, COALESCE(traffic_cycle_type, 'none'), COALESCE(traffic_cycle_baseline, 0) FROM server_accounts WHERE id = ?`, serverID).Scan(&serverName, &serverHost, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &cachedInfo, &trafficCycleType, &trafficCycleBaseline)
	if serverName == "" {
		serverName = serverID
	}
	if serverHost == "" {
		serverHost = serverName
	}
	trafficAlertPercent = normalizeTrafficAlertPercent(trafficAlertPercent)
	trafficUsedBytes := int64(0)
	trafficPercent := 0.0
	if trafficLimitBytes > 0 && cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			trafficUsedBytes = trafficUsedForCycle(trafficUsedBytesFromMetrics(cached, trafficLimitMode), trafficCycleType, trafficCycleBaseline)
			trafficPercent = (float64(trafficUsedBytes) / float64(trafficLimitBytes)) * 100
		}
	}

	eventData := map[string]interface{}{
		"serverId":            serverID,
		"serverName":          serverName,
		"host":                serverHost,
		"hostname":            serverName,
		"cpu_usage":           cpu,
		"mem_percent":         mem,
		"disk_usage":          disk,
		"traffic_used_bytes":  trafficUsedBytes,
		"traffic_limit_bytes": trafficLimitBytes,
		"traffic_limit_mode":  normalizeTrafficLimitMode(trafficLimitMode),
		"traffic_percent":     trafficPercent,
		"traffic_used":        formatBytes(trafficUsedBytes),
		"traffic_limit":       formatBytes(trafficLimitBytes),
		"threshold":           fmt.Sprintf("%.2f%%", trafficAlertPercent),
	}

	if cpu >= 90 {
		if !state.cpuHigh {
			state.cpuHigh = true
			eventData["eventType"] = "cpu_high"
			_ = s.notifier.Trigger(ctx, "server", "cpu_high", eventData)
		}
	} else if cpu < 85 {
		if state.cpuHigh {
			state.cpuHigh = false
			eventData["eventType"] = "cpu_normal"
			_ = s.notifier.Trigger(ctx, "server", "cpu_normal", eventData)
		}
	}

	if mem >= 90 {
		if !state.memoryHigh {
			state.memoryHigh = true
			eventData["eventType"] = "memory_high"
			_ = s.notifier.Trigger(ctx, "server", "memory_high", eventData)
		}
	} else if mem < 85 {
		if state.memoryHigh {
			state.memoryHigh = false
			eventData["eventType"] = "memory_normal"
			_ = s.notifier.Trigger(ctx, "server", "memory_normal", eventData)
		}
	}

	if disk >= 90 {
		if !state.diskHigh {
			state.diskHigh = true
			eventData["eventType"] = "disk_high"
			_ = s.notifier.Trigger(ctx, "server", "disk_high", eventData)
		}
	} else if disk < 85 {
		if state.diskHigh {
			state.diskHigh = false
			eventData["eventType"] = "disk_normal"
			_ = s.notifier.Trigger(ctx, "server", "disk_normal", eventData)
		}
	}

	if trafficLimitBytes > 0 && trafficAlertEnabled != 0 {
		if trafficPercent >= trafficAlertPercent {
			if !state.trafficHigh {
				state.trafficHigh = true
				eventData["eventType"] = "traffic_high"
				_ = s.notifier.Trigger(ctx, "server", "traffic_high", eventData)
			}
		} else if trafficPercent < trafficAlertPercent-5 {
			if state.trafficHigh {
				state.trafficHigh = false
				eventData["eventType"] = "traffic_normal"
				_ = s.notifier.Trigger(ctx, "server", "traffic_normal", eventData)
			}
		}
	}
}
