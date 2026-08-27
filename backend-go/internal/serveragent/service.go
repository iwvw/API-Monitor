package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/managedproxy"
	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
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

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

func (s *Service) StartupError() error { return s.startupErr }

func (s *Service) SetCloudflareTunnelManager(manager cloudflare.ManagedTunnelAPI) {
	s.cloudflare = manager
}

func (s *Service) validateAgentConnection(ctx context.Context, serverID, key string) error {
	serverID = strings.TrimSpace(serverID)
	key = strings.TrimSpace(key)
	if serverID == "" {
		return errors.New("server_id is required")
	}
	if key == "" {
		return errors.New("agent key is required")
	}

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := s.validateAgentKeyForServer(ctx, db, serverID, key); err != nil {
		return err
	}

	var exists int
	if err := db.QueryRowContext(ctx, "SELECT 1 FROM server_accounts WHERE id = ?", serverID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("server not found")
		}
		return err
	}
	return nil
}

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
	s.backgroundWG.Add(6)
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

func (s *Service) recordAgentSignal(serverID, source string, payload map[string]interface{}) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	if s.registry != nil {
		s.registry.UpdateHeartbeat(serverID)
	}
	sampleIntervalMs := int64(0)
	if payload != nil {
		sampleIntervalMs = getInt64Val(payload, "sample_interval_ms", 0)
		if sampleIntervalMs <= 0 {
			sampleIntervalMs = getInt64Val(payload, "metrics_sample_interval_ms", 0)
		}
	}
	if s.presence != nil {
		s.presence.recordHeartbeat(serverID, source, sampleIntervalMs)
	}
}

func (s *Service) markAgentOnlineLegacy(serverID string) {
	if serverID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := s.open(ctx)
		if err != nil {
			return
		}
		defer db.Close()
		now := time.Now().Format("2006-01-02 15:04:05")
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts
			SET status = 'online', last_check_time = ?, last_check_status = 'success', response_time = 0, updated_at = ?
			WHERE id = ?`, now, now, serverID)
		serverName, serverHost := s.serverIdentity(ctx, db, serverID)
		s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "online")
	}()
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "online", true)
	}
}

func (s *Service) markAgentOfflineLegacy(serverID string) {
	if serverID == "" {
		return
	}
	go func() {
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
		serverName, serverHost := s.serverIdentity(ctx, db, serverID)
		s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "offline")
	}()
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "offline", false)
	}
}

func (s *Service) serverIdentity(ctx context.Context, db *sql.DB, serverID string) (string, string) {
	var serverName, serverHost string
	_ = db.QueryRowContext(ctx, `SELECT name, host FROM server_accounts WHERE id = ?`, serverID).Scan(&serverName, &serverHost)
	if serverName == "" {
		serverName = serverID
	}
	if serverHost == "" {
		serverHost = serverName
	}
	return serverName, serverHost
}

func (s *Service) triggerServerStatusNotification(ctx context.Context, serverID, serverName, serverHost, status string) {
	if s.notifier == nil {
		return
	}
	eventData := map[string]interface{}{
		"serverId":   serverID,
		"serverName": serverName,
		"host":       serverHost,
		"hostname":   serverName,
		"status":     status,
	}
	_ = s.notifier.Trigger(ctx, "server", status, eventData)
}

func (s *Service) shouldPersistRealtimeMetrics(serverID string, now time.Time) bool {
	if serverID == "" {
		return false
	}
	interval := s.realtimePersistInterval
	if interval <= 0 {
		interval = defaultRealtimeMetricsPersistInterval
	}
	s.lastPersistMu.Lock()
	defer s.lastPersistMu.Unlock()
	last := s.lastPersist[serverID]
	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	s.lastPersist[serverID] = now
	return true
}

func resolveRealtimeMetricsPersistInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS"))
	if raw == "" {
		return defaultRealtimeMetricsPersistInterval
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultRealtimeMetricsPersistInterval
	}
	interval := time.Duration(value) * time.Millisecond
	if interval < minRealtimeMetricsPersistInterval {
		return minRealtimeMetricsPersistInterval
	}
	return interval
}

func (s *Service) shouldPersistNetworkQuality(serverID string, now time.Time) bool {
	if serverID == "" {
		return false
	}
	interval := s.networkQualityPersistInterval
	if interval <= 0 {
		return false
	}
	s.lastNetworkQualityPersistMu.Lock()
	defer s.lastNetworkQualityPersistMu.Unlock()
	last := s.lastNetworkQualityPersist[serverID]
	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	s.lastNetworkQualityPersist[serverID] = now
	return true
}

func resolveNetworkQualityPersistInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS"))
	if raw == "" {
		return defaultNetworkQualityPersistInterval
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return defaultNetworkQualityPersistInterval
	}
	if value == 0 {
		return 0
	}
	interval := time.Duration(value) * time.Millisecond
	if interval < minNetworkQualityPersistInterval {
		return minNetworkQualityPersistInterval
	}
	return interval
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
			traffic_limit_bytes INTEGER DEFAULT 0,
			traffic_limit_mode TEXT DEFAULT 'total',
			traffic_alert_enabled INTEGER DEFAULT 0,
			traffic_alert_percent REAL DEFAULT 100,
			traffic_cycle_type TEXT DEFAULT 'none',
			traffic_cycle_day INTEGER DEFAULT 1,
			traffic_cycle_start DATETIME,
			traffic_cycle_end DATETIME,
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
			whole_host INTEGER NOT NULL DEFAULT 0,
			relay_server_id TEXT NOT NULL DEFAULT '',
			remote_port INTEGER DEFAULT 0,
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

func (s *Service) handleStatusPageRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.listServerStatusPages(w, r, db)
		case http.MethodPost:
			s.saveServerStatusPage(w, r, db, 0)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 1 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid status page id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			s.saveServerStatusPage(w, r, db, id)
		case http.MethodDelete:
			result, err := db.ExecContext(r.Context(), `DELETE FROM server_status_pages WHERE id = ?`, id)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			changed, _ := result.RowsAffected()
			if changed == 0 {
				response.Error(w, http.StatusNotFound, "Not found")
				return
			}
			response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	response.Error(w, http.StatusNotFound, "status page route not found")
}

func (s *Service) handlePublicStatusPageRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, parts []string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var page map[string]interface{}
	var ok bool
	var err error
	switch {
	case len(parts) == 2 && parts[0] == "status-pages":
		page, ok, err = s.getPublicServerStatusPage(r.Context(), db, normalizeServerStatusSlug(parts[1]), "")
	case len(parts) == 1 && parts[0] == "status-page-by-domain":
		domain := strings.TrimSpace(r.URL.Query().Get("domain"))
		if domain == "" {
			domain = r.Host
		}
		page, ok, err = s.getPublicServerStatusPage(r.Context(), db, "", normalizeServerStatusDomain(domain))
	default:
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, page)
}

func (s *Service) listServerStatusPages(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages ORDER BY created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	pages := []map[string]interface{}{}
	for rows.Next() {
		page, err := scanServerStatusPageRows(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		pages = append(pages, page)
	}
	response.OK(w, pages)
}

func (s *Service) saveServerStatusPage(w http.ResponseWriter, r *http.Request, db *sql.DB, id int64) {
	payload := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(getStringVal(payload, "title", ""))
	slug := normalizeServerStatusSlug(getStringVal(payload, "slug", title))
	if title == "" {
		title = slug
	}
	if slug == "" {
		response.Error(w, http.StatusBadRequest, "slug is required")
		return
	}
	domain := normalizeServerStatusDomain(getStringVal(payload, "domain", ""))
	description := strings.TrimSpace(getStringVal(payload, "description", ""))
	isPublic := getBoolVal(payload, "public", true)
	cacheSeconds := getIntVal(payload, "cacheSeconds", 300)
	if cacheSeconds < 30 {
		cacheSeconds = 30
	}
	configJSON := serverStatusJSON(payload["config"], "{}")
	serverIDs := stringSliceValue(payload["serverIds"])
	if len(serverIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "serverIds is required")
		return
	}
	serverIDsJSON := serverStatusJSON(serverIDs, "[]")

	var err error
	if id > 0 {
		var result sql.Result
		result, err = db.ExecContext(r.Context(), `
			UPDATE server_status_pages
			SET slug = ?, domain = ?, title = ?, description = ?, public = ?, cache_seconds = ?, config_json = ?, server_ids_json = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, slug, nullableServerStatusString(domain), title, description, boolToInt(isPublic), cacheSeconds, configJSON, serverIDsJSON, id)
		if err == nil {
			changed, _ := result.RowsAffected()
			if changed == 0 {
				response.Error(w, http.StatusNotFound, "Not found")
				return
			}
		}
	} else {
		_, err = db.ExecContext(r.Context(), `
			INSERT INTO server_status_pages (slug, domain, title, description, public, cache_seconds, config_json, server_ids_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, slug, nullableServerStatusString(domain), title, description, boolToInt(isPublic), cacheSeconds, configJSON, serverIDsJSON)
	}
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	page, ok, err := s.getServerStatusPageBySlug(r.Context(), db, slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	response.OK(w, page)
}

func (s *Service) getServerStatusPageBySlug(ctx context.Context, db *sql.DB, slug string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE slug = ?`, slug)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	page, err := scanServerStatusPageRows(rows)
	return page, err == nil, err
}

func (s *Service) getPublicServerStatusPage(ctx context.Context, db *sql.DB, slug, domain string) (map[string]interface{}, bool, error) {
	query := `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE public = 1 AND slug = ?`
	arg := slug
	if domain != "" {
		query = `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE public = 1 AND lower(domain) = lower(?)`
		arg = domain
	}
	rows, err := db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, false, rows.Err()
	}
	page, err := scanServerStatusPageRows(rows)
	if closeErr := rows.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, false, err
	}
	servers, err := s.getPublicServerStatusItems(ctx, db, stringSliceValue(page["serverIds"]), mapValue(page["config"]))
	if err != nil {
		return nil, false, err
	}
	page["servers"] = servers
	return page, true, nil
}

// PublicPageIconID 返回公开状态页配置的自定义图标 ID（未设置时为空字符串），
// 供服务端 favicon 解析端点使用；lookup 为 slug 或域名。
func (s *Service) PublicPageIconID(ctx context.Context, lookup string, byDomain bool) (string, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	arg := normalizeServerStatusSlug(lookup)
	if byDomain {
		arg = normalizeServerStatusDomain(lookup)
	}
	return publicpageicon.LookupIconID(ctx, db, `server_status_pages`, arg, byDomain)
}

func (s *Service) getPublicServerStatusItems(ctx context.Context, db *sql.DB, ids []string, config map[string]interface{}) ([]map[string]interface{}, error) {
	if len(ids) == 0 {
		return []map[string]interface{}{}, nil
	}
	hideHosts := getBoolVal(config, "hideHosts", true)
	showTraffic := getBoolVal(config, "showTraffic", true)
	showCharts := getBoolVal(config, "showCharts", true)
	args := []interface{}{}
	holders := make([]string, 0, len(ids))
	for _, id := range ids {
		holders = append(holders, "?")
		args = append(args, id)
	}
	where := "WHERE id IN (" + strings.Join(holders, ",") + ")"
	rows, err := db.QueryContext(ctx, `SELECT id, name, host, status, COALESCE(cached_info, '{}'), COALESCE(description, ''), COALESCE(resolved_country, ''), COALESCE(country, ''), traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), COALESCE(response_time, 0), updated_at FROM server_accounts `+where+` ORDER BY order_index ASC, created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []map[string]interface{}{}
	historyServerIDs := []string{}
	latencyServerIDs := []string{}
	for rows.Next() {
		var id, name, host, status, cachedInfo, description, location, country, trafficLimitMode, updatedAt string
		var trafficLimit, responseTime int64
		if err := rows.Scan(&id, &name, &host, &status, &cachedInfo, &description, &location, &country, &trafficLimit, &trafficLimitMode, &responseTime, &updatedAt); err != nil {
			return nil, err
		}
		cached := map[string]interface{}{}
		_ = json.Unmarshal([]byte(cachedInfo), &cached)
		var lastHeartbeat string
		if conn, agentOnline := s.registry.Get(id); agentOnline {
			metadata := conn.GetMetadata()
			for key, value := range metadata {
				cached[key] = value
			}
			normalizePublicServerLiveMetrics(cached, metadata)
			conn.mu.RLock()
			if !conn.LastHeartbeat.IsZero() {
				lastHeartbeat = conn.LastHeartbeat.UTC().Format(time.RFC3339Nano)
			}
			conn.mu.RUnlock()
			status = "online"
		} else if status == "online" {
			status = "offline"
		}
		network := mapValue(cached["network"])
		trafficRxBytes := firstFloatValue(cached, "net_in_transfer", "net_rx_total", "rx_total_bytes")
		trafficTxBytes := firstFloatValue(cached, "net_out_transfer", "net_tx_total", "tx_total_bytes")
		if networkRx := getFloatFromMap(network, "rx_total_bytes"); networkRx > 0 {
			trafficRxBytes = networkRx
		}
		if networkTx := getFloatFromMap(network, "tx_total_bytes"); networkTx > 0 {
			trafficTxBytes = networkTx
		}
		trafficUsed := trafficUsedBytesForMode(trafficRxBytes, trafficTxBytes, trafficLimitMode)
		uptimeSeconds := firstFloatValue(cached, "uptime_seconds", "uptime_raw")
		if uptimeSeconds == 0 {
			uptimeSeconds = getFloatValue(cached, "uptime")
		}
		uptimeLabel := firstNonEmpty(getString(cached, "uptime_label"), getString(cached, "uptime"))
		if uptimeSeconds > 0 {
			uptimeLabel = formatUptime(int64(uptimeSeconds))
		}
		lat, hasLat := firstOptionalFloatValue(cached, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(cached, "lon", "longitude")
		item := map[string]interface{}{
			"id":               id,
			"name":             name,
			"status":           status,
			"online":           status == "online",
			"description":      description,
			"location":         firstNonEmpty(location, getString(cached, "location"), getString(cached, "resolved_country"), cleanCountryCode(getString(cached, "country_code")), cleanCountryCode(getString(cached, "country"))),
			"region":           getString(cached, "region"),
			"countryCode":      firstNonEmpty(cleanCountryCode(getString(cached, "country_code")), cleanCountryCode(getString(cached, "country")), cleanCountryCode(country)),
			"platform":         firstNonEmpty(getString(cached, "platform"), getString(cached, "os")),
			"platformVersion":  getString(cached, "platform_version"),
			"agentVersion":     getString(cached, "agent_version"),
			"uptime":           uptimeSeconds,
			"uptimeLabel":      uptimeLabel,
			"load":             getString(cached, "load"),
			"cpu":              firstFloatValue(cached, "cpu", "cpu_usage"),
			"cpuTemp":          firstFloatValue(cached, "cpu_temp", "cpuTemp"),
			"cpuPower":         firstFloatValue(cached, "cpu_power", "cpuPower"),
			"memory":           firstFloatValue(cached, "mem_percent", "mem_usage_percent", "memory", "memory_usage", "mem_usage"),
			"memoryUsedBytes":  firstFloatValue(cached, "mem_used_raw", "memory_used_raw"),
			"memoryTotalBytes": firstFloatValue(cached, "mem_total_raw", "memory_total_raw"),
			"disk":             firstFloatValue(cached, "disk_percent", "disk_usage"),
			"diskUsed":         firstNonEmpty(getString(cached, "disk_used"), getString(cached, "disk_used_text")),
			"diskTotal":        firstNonEmpty(getString(cached, "disk_total"), getString(cached, "disk_total_text")),
			"diskUsedBytes":    firstFloatValue(cached, "disk_used_raw"),
			"diskTotalBytes":   firstFloatValue(cached, "disk_total_raw"),
			"netRx":            firstFloatValue(cached, "net_rx", "net_in_speed", "network_rx"),
			"netTx":            firstFloatValue(cached, "net_tx", "net_out_speed", "network_tx"),
			"connections":      firstFloatValue(cached, "connections", "tcp_conn_count"),
			"dockerRunning":    firstFloatValue(cached, "docker_running"),
			"dockerStopped":    firstFloatValue(cached, "docker_stopped"),
			"gpu":              firstFloatValue(cached, "gpu_usage", "gpu"),
			"gpuTemp":          firstFloatValue(cached, "gpu_temp", "gpuTemp"),
			"gpuPower":         firstFloatValue(cached, "gpu_power", "gpuPower"),
			"gpuMemory":        firstFloatValue(cached, "gpu_mem_percent"),
			"gpuModel":         getString(cached, "gpu_model"),
			"responseTime":     responseTime,
			"updatedAt":        firstNonEmpty(lastHeartbeat, getString(cached, "metrics_last_seen"), updatedAt),
		}
		if hasLat && hasUsableCoordinates(lat, lon) {
			item["latitude"] = lat
		}
		if hasLon && hasUsableCoordinates(lat, lon) {
			item["longitude"] = lon
		}
		if !hideHosts {
			item["host"] = host
		}
		if showTraffic {
			item["trafficUsedBytes"] = trafficUsed
			item["trafficRxBytes"] = trafficRxBytes
			item["trafficTxBytes"] = trafficTxBytes
			item["trafficLimitBytes"] = trafficLimit
			item["trafficLimitMode"] = normalizeTrafficLimitMode(trafficLimitMode)
		}
		if showCharts {
			historyServerIDs = append(historyServerIDs, id)
		}
		latencyServerIDs = append(latencyServerIDs, id)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if showCharts {
		for index, id := range historyServerIDs {
			items[index]["history"] = getPublicServerMetricHistory(ctx, db, id)
		}
	}
	for index, id := range latencyServerIDs {
		items[index]["latencyHistory"] = getPublicServerLatencyHistory(ctx, db, id)
	}
	return items, nil
}

func getPublicServerMetricHistory(ctx context.Context, db *sql.DB, serverID string) []map[string]interface{} {
	rows, err := db.QueryContext(ctx, `SELECT recorded_at, cpu_usage, mem_usage, disk_usage, net_rx, net_tx FROM server_metrics_history WHERE server_id = ? ORDER BY recorded_at DESC LIMIT 60`, serverID)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var recordedAt string
		var cpu, mem, disk, rx, tx sql.NullFloat64
		if err := rows.Scan(&recordedAt, &cpu, &mem, &disk, &rx, &tx); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"time":   recordedAt,
			"cpu":    nullFloat(cpu),
			"memory": nullFloat(mem),
			"disk":   nullFloat(disk),
			"netRx":  nullFloat(rx),
			"netTx":  nullFloat(tx),
		})
	}
	return items
}

func getPublicServerLatencyHistory(ctx context.Context, db *sql.DB, serverID string) []map[string]interface{} {
	rows, err := db.QueryContext(ctx, `SELECT status, COALESCE(response_time, 0), checked_at FROM server_monitor_logs WHERE server_id = ? ORDER BY checked_at DESC LIMIT 28`, serverID)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var status, checkedAt string
		var responseTime int64
		if err := rows.Scan(&status, &responseTime, &checkedAt); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"status":       status,
			"responseTime": responseTime,
			"time":         checkedAt,
		})
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items
}

func normalizePublicServerLiveMetrics(cached map[string]interface{}, metadata map[string]interface{}) {
	network := mapValue(metadata["network"])
	docker := mapValue(metadata["docker"])
	gpu := mapValue(metadata["gpu"])
	if value, ok := metadata["cpu_usage"]; ok {
		cached["cpu"] = value
	}
	if value, ok := metadata["mem_usage_percent"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["memory"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["memory_usage"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["disk_usage"]; ok {
		cached["disk_percent"] = value
	}
	if value, ok := metadata["net_in_speed"]; ok {
		cached["net_rx"] = value
	}
	if value, ok := metadata["net_out_speed"]; ok {
		cached["net_tx"] = value
	}
	if value, ok := metadata["network_rx"]; ok {
		cached["net_rx"] = value
	}
	if value, ok := metadata["network_tx"]; ok {
		cached["net_tx"] = value
	}
	if value, ok := network["connections"]; ok {
		cached["connections"] = value
	}
	if value, ok := docker["running"]; ok {
		cached["docker_running"] = value
	}
	if value, ok := docker["stopped"]; ok {
		cached["docker_stopped"] = value
	}
	if value, ok := gpu["Usage"]; ok {
		cached["gpu_usage"] = value
	}
	if value, ok := gpu["Temp"]; ok {
		cached["gpu_temp"] = value
	}
	if value, ok := gpu["Power"]; ok {
		cached["gpu_power"] = value
	}
}

func scanServerStatusPageRows(rows *sql.Rows) (map[string]interface{}, error) {
	var id int64
	var public, cacheSeconds int
	var slug, title, createdAt, updatedAt string
	var domain, description, configJSON, serverIDsJSON sql.NullString
	if err := rows.Scan(&id, &slug, &domain, &title, &description, &public, &cacheSeconds, &configJSON, &serverIDsJSON, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	config := map[string]interface{}{}
	_ = json.Unmarshal([]byte(nullStringDefault(configJSON, "{}")), &config)
	serverIDs := []string{}
	_ = json.Unmarshal([]byte(nullStringDefault(serverIDsJSON, "[]")), &serverIDs)
	return map[string]interface{}{
		"id":           id,
		"slug":         slug,
		"domain":       nullStringVal(domain),
		"title":        title,
		"description":  nullStringDefault(description, ""),
		"public":       public != 0,
		"cacheSeconds": cacheSeconds,
		"config":       config,
		"serverIds":    serverIDs,
		"createdAt":    createdAt,
		"updatedAt":    updatedAt,
	}, nil
}

var nonSlugCharsRegex = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeServerStatusSlug(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	text = nonSlugCharsRegex.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	if text == "" {
		return "servers"
	}
	return text
}

func normalizeServerStatusDomain(value string) string {
	domain := strings.TrimSpace(strings.ToLower(value))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if index := strings.Index(domain, "/"); index >= 0 {
		domain = domain[:index]
	}
	domain = strings.TrimSuffix(domain, "/")
	if host, _, err := net.SplitHostPort(domain); err == nil {
		return host
	}
	return domain
}

func serverStatusJSON(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func nullableServerStatusString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullStringDefault(value sql.NullString, fallback string) string {
	if !value.Valid || value.String == "" {
		return fallback
	}
	return value.String
}

func mapValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return map[string]interface{}{}
}

func stringSliceValue(value interface{}) []string {
	if typed, ok := value.([]string); ok {
		return typed
	}
	if typed, ok := value.([]interface{}); ok {
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			text = strings.TrimSpace(text)
			if ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	}
	return []string{}
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
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var exists int
	if err := tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM server_credentials WHERE id = ?", id).Scan(&exists); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists == 0 {
		response.Error(w, http.StatusNotFound, "凭据不存在")
		return
	}

	if _, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 0"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if _, err := tx.ExecContext(r.Context(), "UPDATE server_credentials SET is_default = 1 WHERE id = ?", id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	committed = true
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

	lastID, serverName, err := s.insertSnippetHistory(r.Context(), db, req.SnippetID, req.ServerID, req.Command, req.RenderedCommand, req.ExecutionMode, req.Status, req.ResultSummary)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().Format(time.RFC3339)
	response.OK(w, map[string]interface{}{
		"id":               lastID,
		"snippet_id":       req.SnippetID,
		"server_id":        req.ServerID,
		"server_name":      serverName,
		"command":          firstNonEmpty(req.Command, req.RenderedCommand),
		"rendered_command": firstNonEmpty(req.RenderedCommand, req.Command),
		"execution_mode":   req.ExecutionMode,
		"status":           req.Status,
		"created_at":       now,
	})
}

// insertSnippetHistory 写入一条命令历史记录（片段执行与 API 命令执行共用），
// 并更新片段 run_count。返回新记录 ID 与解析出的服务器名。
func (s *Service) insertSnippetHistory(ctx context.Context, db *sql.DB, snippetID *int, serverID *string, command, renderedCommand, executionMode, status string, resultSummary *string) (int64, string, error) {
	var serverName sql.NullString
	if serverID != nil && *serverID != "" {
		var name string
		err := db.QueryRowContext(ctx, "SELECT name FROM server_accounts WHERE id = ?", *serverID).Scan(&name)
		if err == nil {
			serverName = sql.NullString{String: name, Valid: true}
		}
	}

	commandStr := firstNonEmpty(command, renderedCommand)
	renderedStr := firstNonEmpty(renderedCommand, command)

	danger := DetectDangerousCommand(renderedStr)
	dangerReasonsJSON, _ := json.Marshal(danger.Reasons)

	now := time.Now().Format(time.RFC3339)

	res, err := db.ExecContext(ctx, `
		INSERT INTO server_command_history (
			snippet_id, server_id, server_name, command, rendered_command, execution_mode, status, dangerous, danger_reasons, result_summary, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snippetID, serverID, serverName, commandStr, renderedStr,
		coalesceStr(executionMode, "terminal"),
		coalesceStr(status, "sent"),
		boolToInt(danger.Dangerous),
		string(dangerReasonsJSON),
		resultSummary,
		now,
	)
	if err != nil {
		return 0, "", err
	}

	// Update run count
	if snippetID != nil && *snippetID > 0 {
		_, _ = db.ExecContext(ctx, "UPDATE server_snippets SET run_count = COALESCE(run_count, 0) + 1, last_used_at = ?, updated_at = ? WHERE id = ?", now, now, *snippetID)
	}

	lastID, _ := res.LastInsertId()
	return lastID, serverName.String, nil
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
	err := db.QueryRowContext(r.Context(), "SELECT COALESCE(metrics_collect_interval, 300), auto_start FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart)
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

func (s *Service) startMetricsCollectorLoop(ctx context.Context) {
	// Wait a moment for database initialization and server startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastCollected time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		loopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		db, err := s.open(loopCtx)
		if err != nil {
			cancel()
			continue
		}

		// Query monitor config
		var interval int
		var autoStart int
		var retentionDays int
		var logRetentionDays int
		err = db.QueryRowContext(loopCtx, "SELECT COALESCE(metrics_collect_interval, 300), auto_start, COALESCE(metrics_retention_days, 30), COALESCE(log_retention_days, 7) FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart, &retentionDays, &logRetentionDays)
		if err != nil {
			db.Close()
			cancel()
			continue
		}

		if autoStart != 1 {
			db.Close()
			cancel()
			continue
		}

		now := time.Now()
		// If it's time to collect
		if lastCollected.IsZero() || now.Sub(lastCollected) >= time.Duration(interval)*time.Second {
			// Trigger collection
			s.runPeriodicCollection(loopCtx, db)
			lastCollected = now

			// 分批清理过期数据：单批上限避免大表上单次 DELETE 持写锁过久
			// 阻塞其它模块写入（含检查循环自身的后续写入）。
			if retentionDays > 0 {
				clearExpiredHistory(loopCtx, db, "server_metrics_history", "recorded_at", retentionDays)
				clearExpiredHistory(loopCtx, db, "server_network_quality_samples", "checked_at", retentionDays)
			}
			if logRetentionDays > 0 {
				clearExpiredHistory(loopCtx, db, "server_monitor_logs", "checked_at", logRetentionDays)
			}
		}

		db.Close()
		cancel()
	}
}

func (s *Service) getMonitorConfig(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var id, probeInterval, probeTimeout, logRetentionDays, maxConnections, sessionTimeout, autoStart, metricsCollectInterval, metricsRetentionDays int
	var updatedAt string

	err := db.QueryRowContext(r.Context(), "SELECT id, probe_interval, probe_timeout, log_retention_days, COALESCE(max_connections, 10), COALESCE(session_timeout, 1800), auto_start, COALESCE(metrics_collect_interval, 300), COALESCE(metrics_retention_days, 30), COALESCE(updated_at, '') FROM server_monitor_config WHERE id = 1").
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
	err := db.QueryRowContext(r.Context(), "SELECT probe_interval, probe_timeout, log_retention_days, COALESCE(max_connections, 10), COALESCE(session_timeout, 1800), auto_start, COALESCE(metrics_collect_interval, 300), COALESCE(metrics_retention_days, 30) FROM server_monitor_config WHERE id = 1").
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
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, updated_at FROM server_accounts ORDER BY order_index ASC, created_at DESC")
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
		s.refreshAccountLocationIfMissingFromList(account)
		list = append(list, account)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) refreshAccountLocations(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	updated, skipped, err := s.refreshAccountLocationsFromAgents(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"updated": updated, "skipped": skipped})
}

func (s *Service) refreshAccountLocationsFromAgents(ctx context.Context, db *sql.DB) (int, int, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, cached_info FROM server_accounts")
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	type candidate struct {
		id         string
		cachedInfo sql.NullString
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.cachedInfo); err != nil {
			return 0, 0, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	now := time.Now().Format(time.RFC3339)
	updated := 0
	skipped := 0
	for _, item := range candidates {
		ok, err := s.refreshAccountLocationFromAgent(ctx, db, item.id, item.cachedInfo, now, true)
		if err != nil {
			return updated, skipped, err
		}
		if ok {
			updated++
		} else {
			skipped++
		}
	}
	return updated, skipped, nil
}

func (s *Service) refreshAccountLocationIfMissingFromList(account map[string]interface{}) {
	if account == nil || !getBoolVal(account, "agent_online", false) {
		return
	}
	serverID := strings.TrimSpace(getString(account, "id"))
	if serverID == "" {
		return
	}
	if countryCode := cleanCountryCode(getString(account, "countryCode")); countryCode != "" {
		lat, hasLat := firstOptionalFloatValue(account, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(account, "lon", "longitude")
		if hasLat && hasLon && hasUsableCoordinates(lat, lon) {
			return
		}
	}
	if last, ok := s.lastAutoLocationRefresh.Load(serverID); ok {
		if lastAt, ok := last.(time.Time); ok && time.Since(lastAt) < 5*time.Minute {
			return
		}
	}
	s.lastAutoLocationRefresh.Store(serverID, time.Now())
	if !s.trackPending() {
		return
	}
	go func(id string) {
		defer s.pendingWG.Done()
		s.refreshAccountLocationFromAgentIfMissing(id)
	}(serverID)
}

func (s *Service) refreshAccountLocationFromAgentIfMissing(serverID string) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	if _, loaded := s.autoLocationRefreshes.LoadOrStore(serverID, struct{}{}); loaded {
		return
	}
	defer s.autoLocationRefreshes.Delete(serverID)

	ctx, cancel := context.WithTimeout(s.backgroundCtx, 20*time.Second)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	var cachedInfo sql.NullString
	var country, resolvedCountry sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT country, resolved_country, cached_info FROM server_accounts WHERE id = ?", serverID).Scan(&country, &resolvedCountry, &cachedInfo); err != nil {
		return
	}
	if !accountNeedsLocation(country, resolvedCountry, cachedInfo) {
		return
	}
	if _, err := s.refreshAccountLocationFromAgent(ctx, nil, serverID, cachedInfo, time.Now().Format(time.RFC3339), false); err != nil {
		applog.Warn(ctx, "serveragent", "failed to refresh initial location from agent", "server_id", serverID, "error", err.Error())
	}
}

func (s *Service) refreshAccountLocationFromAgent(ctx context.Context, db *sql.DB, serverID string, cachedInfo sql.NullString, now string, force bool) (bool, error) {
	if _, ok := s.registry.Get(serverID); !ok {
		return false, nil
	}
	if !force && !accountNeedsLocation(sql.NullString{}, sql.NullString{}, cachedInfo) {
		return false, nil
	}
	output, err := s.RunCommandTaskAndWait(serverID, "curl -fsSL https://64.ipcheck.ing/geo", 15*time.Second)
	if err != nil {
		applog.Warn(ctx, "serveragent", "failed to refresh location from agent", "server_id", serverID, "error", err.Error())
		return false, nil
	}
	geo, ok := parseIPCheckGeo(output)
	if !ok {
		return false, nil
	}
	s.mergeConnectionLocationMetadata(serverID, geo)
	openedDB := false
	if db == nil {
		var err error
		db, err = s.open(ctx)
		if err != nil {
			return false, err
		}
		openedDB = true
	}
	if openedDB {
		defer db.Close()
	}
	nextCachedInfo := mergeCachedInfo(cachedInfo, geo)
	_, err = db.ExecContext(ctx, `
		UPDATE server_accounts
		SET resolved_country = ?, cached_info = ?, updated_at = ?
		WHERE id = ?`,
		firstNonEmpty(getString(geo, "location"), getString(geo, "region"), getString(geo, "country_code")), nextCachedInfo, now, serverID,
	)
	if err != nil {
		return false, err
	}
	if s.metricsHub != nil {
		s.metricsHub.BroadcastMetrics(serverID, geo)
	}
	return true, nil
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, updated_at FROM server_accounts WHERE id = ?", id)
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
		Name                string      `json:"name"`
		Host                string      `json:"host"`
		Port                int         `json:"port"`
		Username            string      `json:"username"`
		AuthType            string      `json:"auth_type"`
		Password            string      `json:"password"`
		PrivateKey          string      `json:"private_key"`
		Passphrase          string      `json:"passphrase"`
		Tags                interface{} `json:"tags"`
		Description         string      `json:"description"`
		Country             string      `json:"country"`
		StartsAt            string      `json:"starts_at"`
		ExpiresAt           string      `json:"expires_at"`
		MonitorMode         string      `json:"monitor_mode"`
		TrafficLimitBytes   int64       `json:"traffic_limit_bytes"`
		TrafficLimitMode    string      `json:"traffic_limit_mode"`
		TrafficAlertEnabled bool        `json:"traffic_alert_enabled"`
		TrafficAlertPercent float64     `json:"traffic_alert_percent"`
		TrafficCycleType    string      `json:"traffic_cycle_type"`
		TrafficCycleDay     int         `json:"traffic_cycle_day"`
		TrafficCycleStart   string      `json:"traffic_cycle_start"`
		TrafficCycleEnd     string      `json:"traffic_cycle_end"`
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
	country := cleanCountryCode(req.Country)

	// Max order_index
	var maxOrder sql.NullInt64
	_ = db.QueryRowContext(r.Context(), "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
	orderIndex := int(maxOrder.Int64) + 1

	encPassword := s.encryptField(req.Password)
	encPrivateKey := s.encryptField(req.PrivateKey)
	encPassphrase := s.encryptField(req.Passphrase)
	resolvedCountry := ""
	cachedInfo := sql.NullString{}
	if country == "" {
		if geo, ok := s.lookupHostLocation(r.Context(), req.Host); ok {
			resolvedCountry = getString(geo, "region")
			cachedInfo = sql.NullString{String: mergeCachedInfo(sql.NullString{}, geo), Valid: true}
		}
	}
	trafficLimitBytes := normalizeTrafficLimitBytes(req.TrafficLimitBytes)
	trafficLimitMode := normalizeTrafficLimitMode(req.TrafficLimitMode)
	trafficAlertPercent := normalizeTrafficAlertPercent(req.TrafficAlertPercent)
	trafficCycleType := normalizeTrafficCycleType(req.TrafficCycleType)
	trafficCycleDay := normalizeTrafficCycleDay(req.TrafficCycleDay)

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO server_accounts (
			id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, traffic_limit_mode, traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, updated_at, cached_info
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Host, coalesceInt(req.Port, 22), coalesceStr(req.Username, "agent"), coalesceStr(req.AuthType, "password"),
		encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(req.Tags), req.Description, coalesceStr(req.MonitorMode, "agent"), nullStr(country), nullStr(resolvedCountry), nullStr(req.StartsAt), nullStr(req.ExpiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(req.TrafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(req.TrafficCycleStart), nullStr(req.TrafficCycleEnd), orderIndex, now, now, nullStr(cachedInfo.String),
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
		trafficCycleType, trafficCycleStart, trafficCycleEnd                 sql.NullString
		port, orderIndex                                                     int
		trafficLimitBytes                                                    int64
		trafficLimitMode                                                     string
		trafficAlertEnabled                                                  int
		trafficAlertPercent                                                  float64
		trafficCycleDay                                                      int
		responseTime                                                         sql.NullInt64
		lastCheckTime, lastCheckStatus, cachedInfo                           sql.NullString
	}
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, tags, description, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, response_time, last_check_time, last_check_status, cached_info FROM server_accounts WHERE id = ?", id).
		Scan(&raw.name, &raw.host, &raw.port, &raw.username, &raw.authType, &raw.password, &raw.privateKey, &raw.passphrase, &raw.status, &raw.monitorMode, &raw.tags, &raw.description, &raw.country, &raw.resolvedCountry, &raw.startsAt, &raw.expiresAt, &raw.trafficLimitBytes, &raw.trafficLimitMode, &raw.trafficAlertEnabled, &raw.trafficAlertPercent, &raw.trafficCycleType, &raw.trafficCycleDay, &raw.trafficCycleStart, &raw.trafficCycleEnd, &raw.orderIndex, &raw.createdAt, &raw.responseTime, &raw.lastCheckTime, &raw.lastCheckStatus, &raw.cachedInfo)
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
	country := cleanCountryCode(getStringVal(req, "country", raw.country.String))
	resolvedCountry := getStringVal(req, "resolved_country", raw.resolvedCountry.String)
	cachedInfo := raw.cachedInfo
	if country == "" {
		hostChanged := host != raw.host
		if hostChanged || resolvedCountry == "" || accountNeedsLocation(sql.NullString{String: country, Valid: country != ""}, sql.NullString{String: resolvedCountry, Valid: resolvedCountry != ""}, cachedInfo) {
			if geo, ok := s.lookupHostLocation(r.Context(), host); ok {
				resolvedCountry = getString(geo, "region")
				cachedInfo = sql.NullString{String: mergeCachedInfo(cachedInfo, geo), Valid: true}
			}
		}
	}
	startsAt := getStringVal(req, "starts_at", raw.startsAt.String)
	expiresAt := getStringVal(req, "expires_at", raw.expiresAt.String)
	orderIndex := getIntVal(req, "order_index", raw.orderIndex)
	trafficLimitBytes := normalizeTrafficLimitBytes(getInt64Val(req, "traffic_limit_bytes", raw.trafficLimitBytes))
	trafficLimitMode := normalizeTrafficLimitMode(getStringVal(req, "traffic_limit_mode", raw.trafficLimitMode))
	trafficAlertEnabled := getBoolVal(req, "traffic_alert_enabled", raw.trafficAlertEnabled != 0)
	trafficAlertPercent := normalizeTrafficAlertPercent(getFloatVal(req, "traffic_alert_percent", raw.trafficAlertPercent))
	trafficCycleType := normalizeTrafficCycleType(getStringVal(req, "traffic_cycle_type", raw.trafficCycleType.String))
	trafficCycleDay := normalizeTrafficCycleDay(getIntVal(req, "traffic_cycle_day", raw.trafficCycleDay))
	trafficCycleStart := getStringVal(req, "traffic_cycle_start", raw.trafficCycleStart.String)
	trafficCycleEnd := getStringVal(req, "traffic_cycle_end", raw.trafficCycleEnd.String)

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
		SET name = ?, host = ?, port = ?, username = ?, auth_type = ?, password = ?, private_key = ?, passphrase = ?, tags = ?, description = ?, monitor_mode = ?, country = ?, resolved_country = ?, starts_at = ?, expires_at = ?, traffic_limit_bytes = ?, traffic_limit_mode = ?, traffic_alert_enabled = ?, traffic_alert_percent = ?, traffic_cycle_type = ?, traffic_cycle_day = ?, traffic_cycle_start = ?, traffic_cycle_end = ?, order_index = ?, cached_info = ?, updated_at = ?
		WHERE id = ?`,
		name, host, port, username, authType, password, privateKey, passphrase, tags, description, monitorMode, nullStr(country), nullStr(resolvedCountry), nullStr(startsAt), nullStr(expiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(trafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(trafficCycleStart), nullStr(trafficCycleEnd), orderIndex, nullStr(cachedInfo.String), now, id,
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

func (s *Service) testTrafficAlert(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if s.notifier == nil {
		response.Error(w, http.StatusBadRequest, "notification service is not available")
		return
	}
	var payload struct {
		TrafficAlertPercent float64 `json:"traffic_alert_percent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	var serverName, serverHost string
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertPercent float64
	var cachedInfo sql.NullString
	err := db.QueryRowContext(r.Context(), `
		SELECT name, host, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_percent, cached_info
		FROM server_accounts
		WHERE id = ?`, id).
		Scan(&serverName, &serverHost, &trafficLimitBytes, &trafficLimitMode, &trafficAlertPercent, &cachedInfo)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if serverName == "" {
		serverName = id
	}
	if serverHost == "" {
		serverHost = serverName
	}
	if payload.TrafficAlertPercent > 0 {
		trafficAlertPercent = payload.TrafficAlertPercent
	}
	trafficAlertPercent = normalizeTrafficAlertPercent(trafficAlertPercent)

	trafficUsedBytes := int64(0)
	if cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			trafficUsedBytes = trafficUsedBytesFromMetrics(cached, trafficLimitMode)
		}
	}
	if trafficLimitBytes <= 0 {
		trafficLimitBytes = trafficUsedBytes
		if trafficLimitBytes <= 0 {
			trafficLimitBytes = 1
		}
	}
	trafficPercent := 0.0
	if trafficLimitBytes > 0 {
		trafficPercent = (float64(trafficUsedBytes) / float64(trafficLimitBytes)) * 100
	}

	eventData := map[string]interface{}{
		"serverId":            id,
		"serverName":          serverName,
		"host":                serverHost,
		"hostname":            serverName,
		"eventType":           "traffic_high",
		"traffic_used_bytes":  trafficUsedBytes,
		"traffic_limit_bytes": trafficLimitBytes,
		"traffic_limit_mode":  normalizeTrafficLimitMode(trafficLimitMode),
		"traffic_percent":     fmt.Sprintf("%.2f", trafficPercent),
		"traffic_used":        formatBytes(trafficUsedBytes),
		"traffic_limit":       formatBytes(trafficLimitBytes),
		"threshold":           fmt.Sprintf("%.2f%%", trafficAlertPercent),
		"test":                true,
	}
	if err := s.notifier.Trigger(r.Context(), "server", "traffic_high", eventData); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"sent": true})
}

type accountDeleteDependencies struct {
	Nodes       int
	Runtimes    int
	Tunnels     int
	StatusPages int
}

func (d accountDeleteDependencies) responseData() map[string]int {
	return map[string]int{
		"nodes":        d.Nodes,
		"runtimes":     d.Runtimes,
		"tunnels":      d.Tunnels,
		"status_pages": d.StatusPages,
	}
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var name, lastCheckStatus, lastCheckTime string
	if err := db.QueryRowContext(r.Context(), `SELECT name,COALESCE(last_check_status,''),COALESCE(last_check_time,'') FROM server_accounts WHERE id=?`, id).Scan(&name, &lastCheckStatus, &lastCheckTime); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "服务器不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	dependencies, err := loadAccountDeleteDependencies(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	forceRequested := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	connection, online := s.registry.Get(id)
	agentObserved := lastCheckTime != "" && !strings.EqualFold(lastCheckStatus, "uninstalled")
	requiresHostCleanup := online || agentObserved || dependencies.Nodes > 0 || dependencies.Runtimes > 0 || dependencies.Tunnels > 0
	forceDetach := forceRequested
	if requiresHostCleanup {
		if !online {
			if !forceRequested {
				response.JSON(w, http.StatusConflict, map[string]interface{}{
					"success": false,
					"error":   "Agent 离线，无法确认主机上的节点、代理程序和 Agent 已卸载",
					"data": map[string]interface{}{
						"can_force_delete": true,
						"server_id":        id,
						"dependencies":     dependencies.responseData(),
					},
				})
				return
			}
		} else {
			capabilities := connection.GetCapabilities()
			missing := []string{}
			if dependencies.Nodes > 0 && !capabilities["proxy_runtime_v1"] {
				missing = append(missing, "节点卸载")
			}
			if (dependencies.Runtimes > 0 || dependencies.Nodes > 0) && !capabilities["proxy_runtime_lifecycle_v2"] {
				missing = append(missing, "代理程序卸载")
			}
			if dependencies.Tunnels > 0 && !capabilities["cloudflared_runtime_v1"] {
				missing = append(missing, "Tunnel 卸载")
			}
			if !capabilities["self_uninstall_v1"] {
				missing = append(missing, "Agent 自卸载")
			}
			if len(missing) == 0 {
				// A force query must not bypass verified cleanup when a capable
				// Agent is online. Force is reserved for genuine recovery paths.
				forceDetach = false
			} else if !forceRequested {
				response.JSON(w, http.StatusConflict, map[string]interface{}{
					"success": false,
					"error":   "Agent 版本过旧，缺少安全级联删除能力：" + strings.Join(missing, "、") + "；请先升级 Agent",
					"data": map[string]interface{}{
						"can_force_delete": true,
						"server_id":        id,
						"dependencies":     dependencies.responseData(),
					},
				})
				return
			}
		}
	}

	task, ok := s.createExclusiveProxyTask(w, id, "server.delete", "cascade-delete")
	if !ok {
		return
	}
	if _, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET enabled=0,publishable=0,apply_status='removing',updated_at=datetime('now') WHERE server_id=?`, id); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusAccepted, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"task_id":      task.ID,
			"status":       task.Status,
			"server_id":    id,
			"dependencies": dependencies.responseData(),
		},
	})
	go s.runAccountCascadeDelete(task.ID, id, name, dependencies, requiresHostCleanup, forceDetach)
}

func loadAccountDeleteDependencies(ctx context.Context, db *sql.DB, serverID string) (accountDeleteDependencies, error) {
	var dependencies accountDeleteDependencies
	for _, check := range []struct {
		query string
		value *int
	}{
		{`SELECT COUNT(*) FROM managed_proxy_nodes WHERE server_id=?`, &dependencies.Nodes},
		{`SELECT COUNT(*) FROM managed_proxy_runtimes WHERE server_id=?`, &dependencies.Runtimes},
		{`SELECT COUNT(*) FROM managed_proxy_tunnels WHERE server_id=?`, &dependencies.Tunnels},
	} {
		if err := db.QueryRowContext(ctx, check.query, serverID).Scan(check.value); err != nil {
			return dependencies, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(server_ids_json,'[]') FROM server_status_pages`)
	if err != nil {
		return dependencies, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return dependencies, err
		}
		var serverIDs []string
		if err := json.Unmarshal([]byte(raw), &serverIDs); err != nil {
			rows.Close()
			return dependencies, fmt.Errorf("decode status page server references: %w", err)
		}
		for _, candidate := range serverIDs {
			if candidate == serverID {
				dependencies.StatusPages++
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dependencies, err
	}
	return dependencies, rows.Close()
}

func (s *Service) runAccountCascadeDelete(taskID, serverID, serverName string, dependencies accountDeleteDependencies, requiresHostCleanup, forceDetach bool) {
	ctx, cancel := context.WithTimeout(s.backgroundCtx, 10*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()
	progress := func(value int, stage, message string) {
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{
			"stage": stage, "message": message, "server_id": serverID, "server_name": serverName,
		})
	}
	fail := func(stage string, cause error) {
		progress(100, stage, cause.Error())
		s.taskRegistry.Fail(taskID, cause.Error())
	}

	progress(5, "unpublish", "已停止发布该主机的全部节点")
	if requiresHostCleanup && !forceDetach {
		if _, online := s.registry.Get(serverID); !online {
			fail("agent_offline", errors.New("Agent 在级联删除期间离线；节点已停止发布，请重试或选择强制移除"))
			return
		}
		progress(15, "remove_nodes", "正在清理主机上的节点服务与防火墙规则")
		if err := s.removeAccountManagedProxyResources(ctx, db, serverID, dependencies); err != nil {
			fail("remove_host_resources", err)
			return
		}
	}

	progress(58, "remove_cloudflare", "正在清理 Tunnel DNS 与 Cloudflare 资源")
	if err := s.removeAccountManagedTunnelControlPlane(ctx, db, serverID); err != nil {
		fail("remove_cloudflare", err)
		return
	}

	if requiresHostCleanup && !forceDetach {
		progress(72, "uninstall_agent", "正在卸载主机 Agent")
		if _, err := s.uninstallAgentAndWait(ctx, serverID); err != nil {
			fail("uninstall_agent", err)
			return
		}
		progress(82, "agent_uninstalled", "Agent 已卸载并断开连接")
	}

	progress(86, "delete_records", "正在删除主机及全部面板关联记录")
	if err := deleteAccountRecords(ctx, db, serverID); err != nil {
		fail("delete_records", err)
		return
	}
	if s.presence != nil {
		s.presence.suppress(serverID, 10*time.Minute)
		s.presence.recordDisconnect(serverID, "deleted")
	}
	s.registry.Disconnect(serverID)
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "offline", false)
	}
	message := "主机、节点、代理程序、Agent 与全部面板关联资源已删除"
	if forceDetach {
		message = "主机与全部面板关联资源已删除；Agent 离线或版本过旧，主机本地可能仍有残留"
	}
	s.taskRegistry.Complete(taskID, message)
}

func (s *Service) removeAccountManagedProxyResources(ctx context.Context, db *sql.DB, serverID string, dependencies accountDeleteDependencies) error {
	rows, err := db.QueryContext(ctx, `SELECT id,runtime,revision,assigned_port,apply_status FROM managed_proxy_nodes WHERE server_id=? ORDER BY created_at`, serverID)
	if err != nil {
		return err
	}
	type nodeState struct {
		ID, Runtime, ApplyStatus string
		Revision                 int64
		AssignedPort             int
	}
	nodes := []nodeState{}
	for rows.Next() {
		var node nodeState
		if err := rows.Scan(&node.ID, &node.Runtime, &node.Revision, &node.AssignedPort, &node.ApplyStatus); err != nil {
			rows.Close()
			return err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.AssignedPort <= 0 && node.ApplyStatus != "running" && node.ApplyStatus != "removing" {
			continue
		}
		release, ok := managedProxyRuntime(node.Runtime)
		if !ok {
			return fmt.Errorf("node %s uses an unpinned proxy runtime", node.ID)
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"node_id": node.ID, "revision": node.Revision + 1, "runtime": release.Runtime,
			"runtime_version": release.Version, "asset_url_amd64": release.AMD64URL,
			"asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL,
			"asset_sha256_arm64": release.ARM64SHA256, "config": "{}", "remove": true,
			"asset_format": release.AssetFormat,
			"port_min":     45654, "port_max": 55654,
		})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove node %s: %w", node.ID, err)
		}
	}
	if dependencies.Tunnels > 0 {
		payload, _ := json.Marshal(cloudflaredTaskPayload("remove", ""))
		if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove cloudflared: %w", err)
		}
	}
	if dependencies.Runtimes > 0 || len(nodes) > 0 {
		release, ok := managedProxyRuntime("sing-box")
		if !ok {
			return errors.New("managed proxy runtime is not pinned")
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"operation": "remove_runtime", "node_id": "runtime-" + serverID, "revision": 1,
			"runtime": release.Runtime, "runtime_version": release.Version,
			"asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256,
			"asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256,
			"asset_format": release.AssetFormat,
			"config":       `{}`, "enabled": false, "port_min": 45654, "port_max": 55654, "transport": "tcp",
		})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove sing-box runtime: %w", err)
		}
	}
	return nil
}

func (s *Service) removeAccountManagedTunnelControlPlane(ctx context.Context, db *sql.DB, serverID string) error {
	var accountID, zoneID, tunnelID, dnsRecordID string
	err := db.QueryRowContext(ctx, `SELECT account_id,zone_id,tunnel_id,dns_record_id FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&accountID, &zoneID, &tunnelID, &dnsRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if (dnsRecordID != "" || tunnelID != "") && s.cloudflare == nil {
		return errors.New("Cloudflare Tunnel 管理器不可用，无法安全删除远端资源")
	}
	if dnsRecordID != "" {
		if err := s.cloudflare.DeleteManagedTunnelDNS(ctx, accountID, zoneID, dnsRecordID); err != nil {
			return fmt.Errorf("delete Tunnel DNS record: %w", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET dns_record_id='',updated_at=datetime('now') WHERE server_id=?`, serverID); err != nil {
			return err
		}
	}
	if tunnelID != "" {
		if err := s.cloudflare.DeleteManagedTunnel(ctx, accountID, tunnelID); err != nil {
			return fmt.Errorf("delete Cloudflare Tunnel: %w", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET tunnel_id='',token_encrypted='',updated_at=datetime('now') WHERE server_id=?`, serverID); err != nil {
			return err
		}
	}
	return nil
}

func deleteAccountRecords(ctx context.Context, db *sql.DB, serverID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := removeServerFromStatusPages(ctx, tx, serverID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE source='internal' AND node_id IN (SELECT id FROM managed_proxy_nodes WHERE server_id=?)`, serverID); err != nil && !isMissingTableError(err) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_runtime_reconcile WHERE node_id IN (SELECT id FROM managed_proxy_nodes WHERE server_id=?)`, serverID); err != nil && !isMissingTableError(err) {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM managed_proxy_nodes WHERE server_id=?`,
		`DELETE FROM managed_proxy_runtimes WHERE server_id=?`,
		`DELETE FROM managed_proxy_tunnels WHERE server_id=?`,
		`DELETE FROM server_monitor_logs WHERE server_id=?`,
		`DELETE FROM server_metrics_history WHERE server_id=?`,
		`DELETE FROM server_network_quality_samples WHERE server_id=?`,
		`DELETE FROM docker_stacks WHERE server_id=?`,
		`DELETE FROM server_agent_credentials WHERE server_id=?`,
		`DELETE FROM server_proxy_desired_state WHERE server_id=?`,
		`DELETE FROM server_proxy_traffic_reports WHERE server_id=?`,
		`DELETE FROM subscription_usage_reports WHERE server_id=?`,
		`DELETE FROM subscription_usage_report_keys WHERE server_id=?`,
		`DELETE FROM subscription_usage_hourly WHERE server_id=?`,
		`UPDATE server_command_history SET server_id=NULL WHERE server_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, serverID); err != nil && !isMissingTableError(err) {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM server_accounts WHERE id=?`, serverID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	committed = true
	return tx.Commit()
}

func removeServerFromStatusPages(ctx context.Context, tx *sql.Tx, serverID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(server_ids_json,'[]') FROM server_status_pages`)
	if err != nil {
		return err
	}
	type update struct {
		ID   int64
		JSON string
	}
	updates := []update{}
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var serverIDs []string
		if err := json.Unmarshal([]byte(raw), &serverIDs); err != nil {
			rows.Close()
			return fmt.Errorf("decode status page %d server references: %w", id, err)
		}
		filtered := make([]string, 0, len(serverIDs))
		changed := false
		for _, candidate := range serverIDs {
			if candidate == serverID {
				changed = true
				continue
			}
			filtered = append(filtered, candidate)
		}
		if changed {
			encoded, err := json.Marshal(filtered)
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, update{ID: id, JSON: string(encoded)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE server_status_pages SET server_ids_json=?,updated_at=datetime('now') WHERE id=?`, item.JSON, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, tags, description, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end FROM server_accounts ORDER BY order_index ASC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var exportList []map[string]interface{}
	for rows.Next() {
		var name, host, username, authType, tagsStr string
		var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
		var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
		var port int
		var trafficLimitBytes int64
		var trafficLimitMode string
		var trafficAlertEnabled int
		var trafficAlertPercent float64
		var trafficCycleDay int
		err := rows.Scan(&name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		exportList = append(exportList, map[string]interface{}{
			"name":                  name,
			"host":                  host,
			"port":                  port,
			"username":              username,
			"auth_type":             authType,
			"password":              s.decryptField(password),
			"private_key":           s.decryptField(privateKey),
			"passphrase":            s.decryptField(passphrase),
			"tags":                  parseJSONTags(tagsStr),
			"description":           nullStringVal(description),
			"country":               nullStringVal(country),
			"resolved_country":      nullStringVal(resolvedCountry),
			"starts_at":             nullStringVal(startsAt),
			"expires_at":            nullStringVal(expiresAt),
			"traffic_limit_bytes":   trafficLimitBytes,
			"traffic_limit_mode":    normalizeTrafficLimitMode(trafficLimitMode),
			"traffic_alert_enabled": trafficAlertEnabled != 0,
			"traffic_alert_percent": normalizeTrafficAlertPercent(trafficAlertPercent),
			"traffic_cycle_type":    normalizeTrafficCycleType(trafficCycleType.String),
			"traffic_cycle_day":     normalizeTrafficCycleDay(trafficCycleDay),
			"traffic_cycle_start":   nullStringVal(trafficCycleStart),
			"traffic_cycle_end":     nullStringVal(trafficCycleEnd),
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
		trafficLimitBytes := normalizeTrafficLimitBytes(getInt64Val(item, "traffic_limit_bytes", 0))
		trafficLimitMode := normalizeTrafficLimitMode(getStringVal(item, "traffic_limit_mode", "total"))
		trafficAlertEnabled := getBoolVal(item, "traffic_alert_enabled", false)
		trafficAlertPercent := normalizeTrafficAlertPercent(getFloatVal(item, "traffic_alert_percent", 100))
		trafficCycleType := normalizeTrafficCycleType(getStringVal(item, "traffic_cycle_type", "none"))
		trafficCycleDay := normalizeTrafficCycleDay(getIntVal(item, "traffic_cycle_day", 1))
		trafficCycleStart := getStringVal(item, "traffic_cycle_start", "")
		trafficCycleEnd := getStringVal(item, "traffic_cycle_end", "")

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
				id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, traffic_limit_mode, traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, name, host, port, coalesceStr(username, "agent"), coalesceStr(authType, "password"),
			encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(tags), description, "agent", country, nil, nullStr(startsAt), nullStr(expiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(trafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(trafficCycleStart), nullStr(trafficCycleEnd), orderIndex, now, now,
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
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertEnabled int
	var trafficAlertPercent float64
	var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
	var trafficCycleDay int
	var tagsStr string

	err := row.Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
		trafficLimitBytes, trafficLimitMode, trafficAlertEnabled != 0, trafficAlertPercent,
		trafficCycleType, trafficCycleDay, trafficCycleStart, trafficCycleEnd,
	), nil
}

func (s *Service) queryAccountByID(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	var name, host, username, authType, status, monitorMode, createdAt, updatedAt string
	var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
	var lastCheckTime, lastCheckStatus, cachedInfo sql.NullString
	var responseTime sql.NullInt64
	var port, orderIndex int
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertEnabled int
	var trafficAlertPercent float64
	var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
	var trafficCycleDay int
	var tagsStr string

	err := db.QueryRowContext(ctx, "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, updated_at FROM server_accounts WHERE id = ?", id).
		Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
		trafficLimitBytes, trafficLimitMode, trafficAlertEnabled != 0, trafficAlertPercent,
		trafficCycleType, trafficCycleDay, trafficCycleStart, trafficCycleEnd,
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
	trafficLimitBytes int64,
	trafficLimitMode string,
	trafficAlertEnabled bool,
	trafficAlertPercent float64,
	trafficCycleType sql.NullString,
	trafficCycleDay int,
	trafficCycleStart, trafficCycleEnd sql.NullString,
) map[string]interface{} {
	conn, agentOnline := s.registry.Get(id)
	health := s.resolveAgentMetricsHealth(id, cachedInfo, agentOnline, time.Now())
	effectiveStatus := status
	if agentOnline {
		effectiveStatus = "online"
	} else if s.presence != nil {
		// 实时在线性以 Agent 心跳为准：连接中断时不应沿用数据库里的陈旧
		// "online" 状态，避免仪表盘把"中断"误判为在线。
		presenceStatus, _ := s.presence.snapshot(id)["presence_status"].(string)
		switch presenceStatus {
		case string(agentPresenceSuspect):
			effectiveStatus = "interrupted"
		case string(agentPresenceOffline):
			effectiveStatus = "offline"
		}
	}
	isOnline := effectiveStatus == "online"

	decryptedPassword := s.decryptField(password)
	decryptedPrivateKey := s.decryptField(privateKey)
	decryptedPassphrase := s.decryptField(passphrase)

	capabilities := getServerCapabilities(host, port, username, authType, decryptedPrivateKey, decryptedPassword, isOnline)

	res := map[string]interface{}{
		"id":                    id,
		"name":                  name,
		"host":                  host,
		"port":                  port,
		"username":              username,
		"auth_type":             authType,
		"password":              decryptedPassword,
		"private_key":           decryptedPrivateKey,
		"passphrase":            decryptedPassphrase,
		"status":                effectiveStatus,
		"monitor_mode":          monitorMode,
		"last_check_time":       nullStringVal(lastCheckTime),
		"last_check_status":     nullStringVal(lastCheckStatus),
		"response_time":         nullIntVal(responseTime),
		"tags":                  parseJSONTags(tagsStr),
		"description":           nullStringVal(description),
		"country":               nullStr(cleanCountryCode(country.String)),
		"resolved_country":      nullStringVal(resolvedCountry),
		"starts_at":             nullStringVal(startsAt),
		"expires_at":            nullStringVal(expiresAt),
		"traffic_limit_bytes":   trafficLimitBytes,
		"traffic_limit_mode":    normalizeTrafficLimitMode(trafficLimitMode),
		"traffic_alert_enabled": trafficAlertEnabled,
		"traffic_alert_percent": normalizeTrafficAlertPercent(trafficAlertPercent),
		"traffic_cycle_type":    normalizeTrafficCycleType(trafficCycleType.String),
		"traffic_cycle_day":     normalizeTrafficCycleDay(trafficCycleDay),
		"traffic_cycle_start":   nullStringVal(trafficCycleStart),
		"traffic_cycle_end":     nullStringVal(trafficCycleEnd),
		"order_index":           orderIndex,
		"created_at":            createdAt,
		"updated_at":            updatedAt,
	}

	for k, v := range capabilities {
		res[k] = v
	}
	res["agent_online"] = agentOnline
	res["agent_connected"] = agentOnline
	if agentOnline && conn != nil {
		res["agent_capabilities"] = conn.GetCapabilities()
	}
	res["supports_metrics"] = agentOnline && health["state"] == "fresh"
	res["metrics_health"] = health["state"]
	res["metrics_stale"] = health["stale"]
	res["metrics_last_seen"] = health["last_seen"]
	res["metrics_last_seen_at"] = health["last_seen_at"]
	res["metrics_age_ms"] = health["age_ms"]

	// Metrics mapping
	cachedMetrics := map[string]interface{}{}
	hasMetricsPayload := cachedInfo.Valid && cachedInfo.String != ""
	if cachedInfo.Valid && cachedInfo.String != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &parsed); err == nil {
			cachedMetrics = parsed
		}
	}
	if agentOnline && conn != nil {
		metadata := conn.GetMetadata()
		for key, value := range metadata {
			if !isEmptyHeartbeatInfoValue(value) {
				cachedMetrics[key] = value
			}
		}
		normalizePublicServerLiveMetrics(cachedMetrics, metadata)
		hasMetricsPayload = true
	}
	if hasMetricsPayload {
		info := s.buildInfoField(cachedMetrics)
		enrichTrafficQuota(info, cachedMetrics, trafficLimitBytes, trafficLimitMode, trafficAlertEnabled, trafficAlertPercent)
		res["info"] = info
		resolvedCountryText := ""
		if resolvedCountry.Valid {
			resolvedCountryText = resolvedCountry.String
		}
		countryText := ""
		if country.Valid {
			countryText = country.String
		}
		res["location"] = firstNonEmpty(getString(cachedMetrics, "location"), getString(cachedMetrics, "region"), resolvedCountryText)
		res["countryCode"] = firstNonEmpty(cleanCountryCode(getString(cachedMetrics, "country_code")), cleanCountryCode(getString(cachedMetrics, "country")), cleanCountryCode(countryText))
		lat, hasLat := firstOptionalFloatValue(cachedMetrics, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(cachedMetrics, "lon", "longitude")
		if hasLat && hasUsableCoordinates(lat, lon) {
			res["latitude"] = lat
		}
		if hasLon && hasUsableCoordinates(lat, lon) {
			res["longitude"] = lon
		}
	}

	return res
}

func (s *Service) markRealtimeMetricsHealthy(serverID string, metrics map[string]interface{}, now time.Time) {
	if metrics == nil {
		return
	}
	seenAt := now.UTC().Format(time.RFC3339Nano)
	metrics["metrics_last_seen"] = seenAt
	metrics["metrics_health"] = "fresh"
	metrics["metrics_stale_after_ms"] = int64(agentMetricsStaleAfter / time.Millisecond)
	if conn, exists := s.registry.Get(serverID); exists {
		conn.SetMetadata("metrics_last_seen", seenAt)
		conn.SetMetadata("metrics_health", "fresh")
		conn.SetMetadata("metrics_stale_after_ms", int64(agentMetricsStaleAfter/time.Millisecond))
		if sequence, ok := metrics["sequence"]; ok {
			conn.SetMetadata("metrics_sequence", sequence)
		}
		if interval, ok := metrics["sample_interval_ms"]; ok {
			conn.SetMetadata("metrics_sample_interval_ms", interval)
		}
	}
}

func (s *Service) markRealtimeMetricsPersistResult(serverID string, ok bool, err error, now time.Time) {
	conn, exists := s.registry.Get(serverID)
	if !exists {
		return
	}
	previousStatus := ""
	if metadata := conn.GetMetadata(); metadata != nil {
		previousStatus, _ = metadata["metrics_persist_status"].(string)
	}
	status := "ok"
	errorText := ""
	if !ok {
		status = "error"
		if err != nil {
			errorText = err.Error()
		}
	}
	conn.SetMetadata("metrics_persist_status", status)
	conn.SetMetadata("metrics_persist_error", errorText)
	conn.SetMetadata("metrics_persist_at", now.UTC().Format(time.RFC3339Nano))
	if !ok && previousStatus != "error" {
		if !s.trackPending() {
			return
		}
		go func() {
			defer s.pendingWG.Done()
			ctx, cancel := context.WithTimeout(s.backgroundCtx, 5*time.Second)
			defer cancel()
			db, openErr := s.open(ctx)
			if openErr != nil {
				return
			}
			defer db.Close()
			serverName, serverHost := s.serverIdentity(ctx, db, serverID)
			s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "degraded")
		}()
	}
}

func (s *Service) mergeConnectionLocationMetadata(serverID string, geo map[string]interface{}) {
	if geo == nil {
		return
	}
	conn, exists := s.registry.Get(serverID)
	if !exists {
		return
	}
	for _, key := range cachedLocationFieldNames() {
		if value, ok := geo[key]; ok && !isEmptyHeartbeatInfoValue(value) {
			conn.SetMetadata(key, value)
		}
	}
}

func (s *Service) mergeCachedLocationFieldsFromDB(ctx context.Context, db *sql.DB, serverID string, metrics map[string]interface{}) map[string]interface{} {
	if metrics == nil {
		metrics = map[string]interface{}{}
	}
	var raw string
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?", serverID).Scan(&raw); err != nil {
		return metrics
	}
	existing := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &existing); err != nil {
		return metrics
	}
	preserveCachedLocationFields(metrics, existing)
	s.mergeConnectionLocationMetadata(serverID, metrics)
	return metrics
}

func cachedLocationFieldNames() []string {
	return []string{
		"ip",
		"country_code",
		"country",
		"resolved_country",
		"region",
		"location",
		"city",
		"latitude",
		"longitude",
		"lat",
		"lon",
		"isp",
		"org",
		"asn",
		"timezone",
		"geo_source",
	}
}

func preserveCachedLocationFields(target map[string]interface{}, existing map[string]interface{}) {
	if target == nil || existing == nil {
		return
	}
	for _, key := range cachedLocationFieldNames() {
		if value, ok := existing[key]; ok && !isEmptyHeartbeatInfoValue(value) {
			if current, exists := target[key]; !exists || isEmptyHeartbeatInfoValue(current) {
				target[key] = value
			}
		}
	}
}

func cloneMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (s *Service) resolveAgentMetricsHealth(serverID string, cachedInfo sql.NullString, agentOnline bool, now time.Time) map[string]interface{} {
	state := "missing"
	var lastSeen time.Time

	if conn, exists := s.registry.Get(serverID); exists {
		metadata := conn.GetMetadata()
		if raw, ok := metadata["metrics_last_seen"]; ok {
			lastSeen = parseAgentMetricTime(raw)
		}
		if metadata["metrics_persist_status"] == "error" {
			state = "degraded"
		}
	}

	if lastSeen.IsZero() && cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			lastSeen = parseAgentMetricTime(cached["metrics_last_seen"])
		}
	}

	ageMs := int64(0)
	if !lastSeen.IsZero() {
		age := now.Sub(lastSeen)
		if age < 0 {
			age = 0
		}
		ageMs = int64(age / time.Millisecond)
		if age <= agentMetricsStaleAfter {
			if state != "degraded" {
				state = "fresh"
			}
		} else {
			state = "stale"
		}
	} else if !agentOnline {
		state = "offline"
	}

	return map[string]interface{}{
		"state":        state,
		"stale":        state == "stale" || state == "missing" || state == "degraded",
		"last_seen":    formatAgentMetricTime(lastSeen),
		"last_seen_at": timeToMillis(lastSeen),
		"age_ms":       ageMs,
	}
}

func parseAgentMetricTime(value interface{}) time.Time {
	switch v := value.(type) {
	case string:
		if v == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	case float64:
		if v > 0 {
			return time.UnixMilli(int64(v))
		}
	case int64:
		if v > 0 {
			return time.UnixMilli(v)
		}
	case int:
		if v > 0 {
			return time.UnixMilli(int64(v))
		}
	}
	return time.Time{}
}

func formatAgentMetricTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timeToMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
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
	cpuModel := extractCPUModel(metrics)

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
	diskArray := parseDiskString(diskStr)
	if diskStr == "" || (len(diskArray) > 0 && diskArray[0]["used"] == "-") {
		used := getString(metrics, "disk_used")
		total := getString(metrics, "disk_total")
		percent := getFloat(metrics, "disk_percent")
		if percent == 0.0 {
			percent = getFloat(metrics, "disk_usage")
		}
		if used != "" && total != "" {
			diskArray = []map[string]interface{}{
				{
					"device": "/",
					"used":   used,
					"total":  total,
					"usage":  fmt.Sprintf("%.0f%%", percent),
				},
			}
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
	countryCode := firstNonEmpty(cleanCountryCode(getString(metrics, "country_code")), cleanCountryCode(getString(metrics, "country")))
	resolvedCountry := firstNonEmpty(getString(metrics, "resolved_country"), countryCode)
	location := firstNonEmpty(getString(metrics, "location"), getString(metrics, "region"), resolvedCountry)
	latitude, hasLatitude := firstOptionalFloatValue(metrics, "lat", "latitude")
	longitude, hasLongitude := firstOptionalFloatValue(metrics, "lon", "longitude")
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

	cachedInfo := map[string]interface{}{
		"cpu": map[string]interface{}{
			"Model":         cpuModel,
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
		"disk":              diskArray,
		"docker":            dockerVal,
		"network":           networkVal,
		"gpu":               buildGpuInfo(metrics),
		"platform":          platform,
		"platformVersion":   platformVersion,
		"agentVersion":      agentVersion,
		"ip":                ip,
		"country_code":      countryCode,
		"resolved_country":  resolvedCountry,
		"location":          location,
		"region":            getString(metrics, "region"),
		"uptime":            uptime,
		"lastUpdate":        lastUpdate,
		"metrics_health":    getString(metrics, "metrics_health"),
		"metrics_last_seen": getString(metrics, "metrics_last_seen"),
		"metrics_last_seen_at": func() int64 {
			if seen := parseAgentMetricTime(metrics["metrics_last_seen"]); !seen.IsZero() {
				return seen.UnixMilli()
			}
			return 0
		}(),
		"metrics_stale_after_ms": getIntValue(metrics, "metrics_stale_after_ms"),
		"metrics_sequence":       getIntValue(metrics, "sequence"),
		"sample_interval_ms":     getIntValue(metrics, "sample_interval_ms"),
	}
	if hasLatitude && hasUsableCoordinates(latitude, longitude) {
		cachedInfo["latitude"] = latitude
	}
	if hasLongitude && hasUsableCoordinates(latitude, longitude) {
		cachedInfo["longitude"] = longitude
	}
	return cachedInfo
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

func cleanCountryCode(value string) string {
	code := strings.TrimSpace(value)
	if code == "" || strings.EqualFold(code, "auto") {
		return ""
	}
	return strings.ToLower(code)
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

func getInt64Val(m map[string]interface{}, key string, fallback int64) int64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return int64(f)
		}
	}
	return fallback
}

func getFloatVal(m map[string]interface{}, key string, fallback float64) float64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return f
		}
	}
	return fallback
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		if f, err := toFloat(val); err == nil {
			return f
		}
	}
	return 0
}

func getBoolVal(m map[string]interface{}, key string, fallback bool) bool {
	val, ok := m[key]
	if !ok {
		return fallback
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return fallback
}

func normalizeTrafficLimitBytes(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func normalizeTrafficLimitMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "upload", "download":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "total"
	}
}

func trafficUsedBytesForMode(rxTotal, txTotal float64, mode string) int64 {
	var used float64
	switch normalizeTrafficLimitMode(mode) {
	case "upload":
		used = txTotal
	case "download":
		used = rxTotal
	default:
		used = rxTotal + txTotal
	}
	if used < 0 {
		return 0
	}
	return int64(used)
}

func trafficUsedBytesFromMetrics(metrics map[string]interface{}, mode string) int64 {
	if metrics == nil {
		return 0
	}
	network := mapValue(metrics["network"])
	rxTotal := firstFloatValue(metrics, "net_in_transfer", "net_rx_total", "rx_total_bytes")
	txTotal := firstFloatValue(metrics, "net_out_transfer", "net_tx_total", "tx_total_bytes")
	if networkRx := getFloatFromMap(network, "rx_total_bytes"); networkRx > 0 {
		rxTotal = networkRx
	}
	if networkTx := getFloatFromMap(network, "tx_total_bytes"); networkTx > 0 {
		txTotal = networkTx
	}
	return trafficUsedBytesForMode(rxTotal, txTotal, mode)
}

func normalizeTrafficAlertPercent(value float64) float64 {
	if value <= 0 {
		return 100
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeTrafficCycleType(value string) string {
	normalized := strings.TrimSpace(value)
	switch normalized {
	case "calendar_month", "monthly", "custom", "none":
		return normalized
	default:
		return "none"
	}
}

func normalizeTrafficCycleDay(value int) int {
	if value < 1 {
		return 1
	}
	if value > 28 {
		return 28
	}
	return value
}

func enrichTrafficQuota(info map[string]interface{}, metrics map[string]interface{}, limitBytes int64, limitMode string, alertEnabled bool, alertPercent float64) {
	if info == nil || limitBytes <= 0 {
		return
	}
	network, ok := info["network"].(map[string]interface{})
	if !ok || network == nil {
		network = map[string]interface{}{}
		info["network"] = network
	}

	rxTotal := getFloatFromMap(network, "rx_total_bytes")
	txTotal := getFloatFromMap(network, "tx_total_bytes")
	if rxTotal == 0 {
		rxTotal = getFloatValue(metrics, "net_in_transfer")
	}
	if txTotal == 0 {
		txTotal = getFloatValue(metrics, "net_out_transfer")
	}
	usedBytes := trafficUsedBytesForMode(rxTotal, txTotal, limitMode)
	percent := 0.0
	if limitBytes > 0 {
		percent = (float64(usedBytes) / float64(limitBytes)) * 100
	}

	network["traffic_used_bytes"] = usedBytes
	network["traffic_limit_bytes"] = limitBytes
	network["traffic_limit_mode"] = normalizeTrafficLimitMode(limitMode)
	network["traffic_percent"] = percent
	network["traffic_alert_enabled"] = alertEnabled
	network["traffic_alert_percent"] = normalizeTrafficAlertPercent(alertPercent)
	network["traffic_used"] = formatBytes(usedBytes)
	network["traffic_limit"] = formatBytes(limitBytes)
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

	model := extractGPUModel(metrics)
	usage := metricFloat(metrics, "gpu_usage")
	memUsed := metricFloat(metrics, "gpu_mem_used")
	memTotal := metricFloat(metrics, "gpu_mem_total")
	power := metricFloat(metrics, "gpu_power")
	temp := metricFloat(metrics, "gpu_temp")
	if usage == 0 && memUsed == 0 && memTotal == 0 && power == 0 && temp == 0 && model == "" {
		return []map[string]interface{}{}
	}
	if model == "" {
		model = "GPU"
	}

	return []map[string]interface{}{{
		"name":        model,
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

func extractCPUModel(metrics map[string]interface{}) string {
	candidates := []string{
		stringMetric(metrics, "cpu_model", ""),
		stringMetric(metrics, "cpu_name", ""),
		stringMetric(metrics, "processor", ""),
		stringMetric(metrics, "model_name", ""),
		stringMetric(metrics, "hardware_model", ""),
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	if cpuRaw, ok := metrics["cpu"]; ok {
		switch cpu := cpuRaw.(type) {
		case []string:
			return strings.Join(filterNonEmptyStrings(cpu), " / ")
		case []interface{}:
			var models []string
			for _, item := range cpu {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					models = append(models, strings.TrimSpace(text))
				}
			}
			if len(models) > 0 {
				return strings.Join(models, " / ")
			}
		case map[string]interface{}:
			return firstNonEmpty(
				getString(cpu, "Model"),
				getString(cpu, "model"),
				getString(cpu, "Name"),
				getString(cpu, "name"),
			)
		}
	}
	return ""
}

func extractGPUModel(metrics map[string]interface{}) string {
	candidates := []string{
		stringMetric(metrics, "gpu_model", ""),
		stringMetric(metrics, "gpu_name", ""),
		stringMetric(metrics, "graphics", ""),
		stringMetric(metrics, "video", ""),
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	if gpuRaw, ok := metrics["gpu"]; ok {
		switch gpu := gpuRaw.(type) {
		case []string:
			return strings.Join(filterNonEmptyStrings(gpu), " / ")
		case []interface{}:
			var models []string
			for _, item := range gpu {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					models = append(models, strings.TrimSpace(text))
					continue
				}
				if obj, ok := item.(map[string]interface{}); ok {
					model := firstNonEmpty(
						getString(obj, "Model"),
						getString(obj, "model"),
						getString(obj, "Name"),
						getString(obj, "name"),
					)
					if model != "" {
						models = append(models, model)
					}
				}
			}
			if len(models) > 0 {
				return strings.Join(models, " / ")
			}
		case map[string]interface{}:
			return firstNonEmpty(
				getString(gpu, "Model"),
				getString(gpu, "model"),
				getString(gpu, "Name"),
				getString(gpu, "name"),
			)
		}
	}
	return ""
}

func filterNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, err := toFloat(v); err == nil {
			return f
		}
	}
	return 0
}

func firstFloatValue(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value := getFloatValue(m, key); value != 0 {
			return value
		}
	}
	return 0
}

func firstOptionalFloatValue(m map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, exists := m[key]
		if !exists || raw == nil {
			continue
		}
		value, err := toFloat(raw)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func hasUsableCoordinates(lat, lon float64) bool {
	return !(lat == 0 && lon == 0)
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
	_ = db.QueryRowContext(ctx, `SELECT name, host, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, cached_info FROM server_accounts WHERE id = ?`, serverID).Scan(&serverName, &serverHost, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &cachedInfo)
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
			trafficUsedBytes = trafficUsedBytesFromMetrics(cached, trafficLimitMode)
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
