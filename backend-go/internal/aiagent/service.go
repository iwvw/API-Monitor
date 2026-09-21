package aiagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const routePrefix = "/api/aiagent"

// ServerOption 是可供登记为实例的主机。
type ServerOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host,omitempty"`
	Online   bool   `json:"online"`
	Metadata string `json:"metadata,omitempty"`
}

// ServerProvider 由 server 层注入，提供可登记主机列表。
type ServerProvider interface {
	ListServerOptions(ctx context.Context) []ServerOption
}

// Service 是 AI Agent 管理模块的 HTTP 服务。
type Service struct {
	cfg        config.Config
	store      *database.Store
	schemaOnce sync.Once
	schemaErr  error

	auth         *auth.Service
	runtime      AgentRuntime
	servers      ServerProvider
	limiter      *loginLimiter
	streamTokens *streamTokenBroker
	// gatewaySlots 限制并发的网关流数量（含 Bearer 直连与一次性令牌两条路径）。
	gatewaySlots chan struct{}
	// 访问日志清理节流状态。
	accessLogPurgeMu   sync.Mutex
	lastAccessLogPurge time.Time
	// 后台收敛循环的生命周期。
	convergenceOnce   sync.Once
	convergenceCancel context.CancelFunc
	convergenceWG     sync.WaitGroup
	// 运行期计数：观测后台收敛与进程管理的健康度。
	metrics *Metrics
}

// maxConcurrentGatewayStreams 是网关并发流上限。
const maxConcurrentGatewayStreams = 256

// New 构造服务。cfg 是唯一必填入参；auth/runtime 通过 Setter 注入。
func New(cfg config.Config) *Service {
	return &Service{
		cfg:          cfg,
		store:        database.New(cfg),
		limiter:      newLoginLimiter(),
		streamTokens: newStreamTokenBroker(),
		gatewaySlots: make(chan struct{}, maxConcurrentGatewayStreams),
		metrics:      newMetrics(),
	}
}

// Initialize 在启动期建表并落库，失败即视为启动失败（与订阅模块一致）：
// 半迁移的模块只会在后续请求里反复 500，不如在启动期暴露确定性问题。
func (s *Service) Initialize(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return s.purgeAccessLogs(ctx, db, accessLogRetention)
}

// accessLogRetention 是访问日志保留时长；网关流式转发与登录尝试都会写日志，
// 不清理会在单机 SQLite 上无限增长。
const accessLogRetention = 30 * 24 * time.Hour

// ConvergenceInterval 是后台收敛的巡检间隔。
//
// 收敛曾只在实例视图被构建时触发，导致「进程崩溃但没人打开面板」永远不会被拉起。
// 这里改为后台定时巡检，覆盖「无人查看」的场景（ADR-0006 第 3.3 条）。
//
// 30 秒是权衡：足够快地恢复，又不会让纳管主机上的 Agent 承受高频任务往返。
// 主机侧的 supervisor 负责亚秒级的崩溃自愈，本循环只做期望状态层面的纠偏，
// 因此无需更短。
const ConvergenceInterval = 30 * time.Second

// StartConvergence 启动后台收敛循环。
//
// 由进程入口在启动期调用一次。自行持有可取消的 context（与 settings 模块的
// StartBackgroundCleanup 一致），收敛必须比任意请求活得更久，不能挂在某个
// HTTP 请求的生命周期上。幂等：重复调用不会启动第二个循环。
func (s *Service) StartConvergence() {
	s.convergenceOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.convergenceCancel = cancel
		s.convergenceWG.Add(1)
		go func() {
			defer s.convergenceWG.Done()
			ticker := time.NewTicker(ConvergenceInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.convergeAllInstances(ctx)
				}
			}
		}()
	})
}

// StopConvergence 停止后台收敛循环，供优雅退出使用。
func (s *Service) StopConvergence() {
	if s.convergenceCancel != nil {
		s.convergenceCancel()
		s.convergenceWG.Wait()
	}
}

// convergeAllInstances 对全部「已设期望状态」的实例做一次收敛。
//
// 单个实例失败不影响其它实例：收敛是互相独立的。
func (s *Service) convergeAllInstances(ctx context.Context) {
	if s.runtime == nil {
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		applog.Warn(ctx, "aiagent", "converge: open database failed", "error", err.Error())
		return
	}
	instances, err := s.listInstances(ctx, db)
	// 收敛会往返主机 Agent，期间不占用唯一的 SQLite 连接。
	db.Close()
	if err != nil {
		applog.Warn(ctx, "aiagent", "converge: list instances failed", "error", err.Error())
		return
	}

	// 逐个收敛：并发往返多台主机会让 Agent 侧任务队列拥塞，
	// 而收敛本身是低频后台任务，顺序执行的延迟可以接受。
	s.metrics.convergenceRounds.Add(1)
	converged := 0
	for _, instance := range instances {
		if instance.DesiredState == "" || !instance.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		s.convergeInstance(ctx, instance)
		converged++
	}
	s.metrics.lastConvergenceAt.Store(time.Now().Unix())
	if converged > 0 {
		applog.Info(ctx, "aiagent", "convergence round finished",
			"instances", converged,
			"starts", s.metrics.convergenceStarts.Load(),
			"stops", s.metrics.convergenceStops.Load(),
			"failures", s.metrics.convergenceFailures.Load())
	}
}

// purgeAccessLogs 删除超过保留期的访问日志。启动期执行一次，避免长期运行后表无界膨胀。
func (s *Service) purgeAccessLogs(ctx context.Context, db *sql.DB, retention time.Duration) error {
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `DELETE FROM aiagent_access_logs WHERE created_at < ?`, cutoff)
	return err
}

// SetAuthService 注入面板鉴权服务，用于管理员面的 session 校验。
func (s *Service) SetAuthService(authService *auth.Service) {
	s.auth = authService
}

// SetAgentRuntime 注入主机 Agent 运行时适配器。
func (s *Service) SetAgentRuntime(runtime AgentRuntime) {
	s.runtime = runtime
}

// SetServerProvider 注入可登记主机列表来源。
func (s *Service) SetServerProvider(provider ServerProvider) {
	s.servers = provider
}

// tokenTTLDaysValue 返回长期令牌有效期（天）。第一版为固定默认值；
// 若未来需要按环境调整，在此处接入 config 字段即可。
func (s *Service) tokenTTLDaysValue() int {
	return defaultTokenTTLDays
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	s.schemaOnce.Do(func() {
		s.schemaErr = s.ensureSchema(ctx, db)
	})
	if s.schemaErr != nil {
		db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

// tokenTTLDays 是 Service 的字段式读取入口（供 store 层使用）。
func (s *Service) getTokenTTLDays() int { return s.tokenTTLDaysValue() }

// ServeHTTP 按路径分发模块请求。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, routePrefix)
	if path == "" {
		path = "/"
	}

	switch {
	case path == "/auth/login" && r.Method == http.MethodPost:
		s.handleLogin(w, r)
	case path == "/auth/logout" && r.Method == http.MethodPost:
		s.handleLogout(w, r)

	case path == "/users" && r.Method == http.MethodGet:
		s.handleListUsers(w, r)
	case path == "/users" && r.Method == http.MethodPost:
		s.handleCreateUser(w, r)
	case strings.HasPrefix(path, "/users/") && strings.HasSuffix(path, "/reset-password") && r.Method == http.MethodPost:
		s.handleResetPassword(w, r, trimSegment(path, "/users/", "/reset-password"))
	case strings.HasPrefix(path, "/users/") && strings.HasSuffix(path, "/instances"):
		s.handleInstanceGrants(w, r, trimSegment(path, "/users/", "/instances"))
	case strings.HasPrefix(path, "/users/") && r.Method == http.MethodPut:
		s.handleUpdateUser(w, r, strings.TrimPrefix(path, "/users/"))
	case strings.HasPrefix(path, "/users/") && r.Method == http.MethodDelete:
		s.handleDeleteUser(w, r, strings.TrimPrefix(path, "/users/"))

	case path == "/tokens" && r.Method == http.MethodGet:
		s.handleListTokens(w, r)
	case strings.HasPrefix(path, "/tokens/") && strings.HasSuffix(path, "/revoke") && r.Method == http.MethodPost:
		s.handleRevokeToken(w, r, trimSegment(path, "/tokens/", "/revoke"))

	case path == "/instances" && r.Method == http.MethodGet:
		s.handleListInstances(w, r)
	case path == "/instances" && r.Method == http.MethodPost:
		s.handleCreateInstance(w, r)
	// 批量操作必须排在 /instances/{id} 的通配匹配之前，否则 "batch" 会被当成实例 ID。
	case path == "/instances/batch" && r.Method == http.MethodPost:
		s.handleBatchLifecycle(w, r)
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/status") && r.Method == http.MethodGet:
		s.handleInstanceStatus(w, r, trimSegment(path, "/instances/", "/status"))
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/lifecycle") && r.Method == http.MethodPost:
		s.handleInstanceLifecycle(w, r, trimSegment(path, "/instances/", "/lifecycle"))
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/grants") && r.Method == http.MethodGet:
		s.handleInstanceGrantUsers(w, r, trimSegment(path, "/instances/", "/grants"))
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/access-info") && r.Method == http.MethodGet:
		s.handleAccessInfo(w, r, trimSegment(path, "/instances/", "/access-info"))
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/meta"):
		s.handleInstanceMeta(w, r, trimSegment(path, "/instances/", "/meta"))
	case strings.HasPrefix(path, "/instances/") && r.Method == http.MethodPut:
		s.handleUpdateInstance(w, r, strings.TrimPrefix(path, "/instances/"))
	case strings.HasPrefix(path, "/instances/") && r.Method == http.MethodDelete:
		s.handleDeleteInstance(w, r, strings.TrimPrefix(path, "/instances/"))

	case path == "/providers" && r.Method == http.MethodGet:
		s.handleProviders(w, r)
	case path == "/servers" && r.Method == http.MethodGet:
		s.handleServers(w, r)
	case strings.HasPrefix(path, "/servers/") && strings.HasSuffix(path, "/diagnose") && r.Method == http.MethodGet:
		// 诊断某主机上指定 Provider 的可用性（exe 是否就绪 + 端口占用 + 建议空闲端口）。
		// 供创建/编辑实例时预检：默认端口被占时前端能自动建议切换。
		s.handleServerDiagnose(w, r, trimSegment(path, "/servers/", "/diagnose"))
	case path == "/logs" && r.Method == http.MethodGet:
		s.handleLogs(w, r)
	case path == "/metrics" && r.Method == http.MethodGet:
		s.handleMetrics(w, r)

	case path == "/preferences" && r.Method == http.MethodGet:
		s.handleListPreferences(w, r)
	case path == "/preferences" && r.Method == http.MethodPut:
		s.handlePutPreferences(w, r)
	case path == "/preferences" && r.Method == http.MethodDelete:
		s.handleDeletePreferences(w, r)

	case strings.HasPrefix(path, "/gw/"):
		rest := strings.TrimPrefix(path, "/gw/")
		parts := strings.SplitN(rest, "/", 2)
		instanceID := parts[0]
		inner := ""
		if len(parts) == 2 {
			inner = parts[1]
		}
		if inner == "stream-token" && r.Method == http.MethodPost {
			s.handleStreamToken(w, r, instanceID)
			return
		}
		s.handleGateway(w, r, instanceID, inner)

	default:
		writeError(w, http.StatusNotFound, CodeNotFound, "aiagent route not found")
	}
}

func trimSegment(path, prefix, suffix string) string {
	value := strings.TrimPrefix(path, prefix)
	value = strings.TrimSuffix(value, suffix)
	return strings.Trim(value, "/")
}

// ---------- 认证 ----------

// resolveAuth 解析请求身份：优先 Bearer 令牌（用户面），否则面板 session（管理员面）。
func (s *Service) resolveAuth(r *http.Request) (authContext, error) {
	// 管理 AI 内部调用：由 server/ai_caller.go 注入的 context 标记，无法被
	// 普通 HTTP 请求伪造（同 serveragent.WithAdminAIFullApprove 模式）。
	// AI 调用走 serveGoRoute 绕过外层 authorizeGoRoute，本模块是少数需要
	// 内部做会话鉴权的组件，故需显式识别这个内部管理员身份。
	if isInternalAICall(r.Context()) {
		return authContext{IsAdmin: true, UserID: adminActorID, Username: "admin"}, nil
	}
	if bearer := bearerToken(r); bearer != "" {
		db, err := s.store.Open(r.Context())
		if err != nil {
			return authContext{}, err
		}
		defer db.Close()
		return s.authenticateToken(r.Context(), db, bearer, s.clientIP(r))
	}
	if s.auth != nil {
		ok, err := s.auth.IsAuthenticated(r.Context(), r)
		if err == nil && ok {
			// 面板管理员没有模块用户 ID，但审计需要可归属的稳定标识。
			return authContext{IsAdmin: true, UserID: adminActorID, Username: "admin"}, nil
		}
	}
	return authContext{}, errInvalidCreds
}

// adminActorID 是面板管理员在模块访问日志中的归属标识。
const adminActorID = "admin"

func (s *Service) requireAuth(w http.ResponseWriter, r *http.Request) (authContext, bool) {
	auth, err := s.resolveAuth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
		return authContext{}, false
	}
	return auth, true
}

// assertUserActive 复核用户仍存在且未被禁用（短令牌路径在消费前调用）。
func (s *Service) assertUserActive(ctx context.Context, userID string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	user, err := s.getUserByID(ctx, db, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return err
	}
	if user.Disabled {
		return errUserDisabled
	}
	return nil
}

// requireAdmin 要求面板管理员会话。
func (s *Service) requireAdmin(w http.ResponseWriter, r *http.Request) (authContext, bool) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return authContext{}, false
	}
	if !auth.IsAdmin {
		writeError(w, http.StatusForbidden, CodeForbidden, "administrator session required")
		return authContext{}, false
	}
	return auth, true
}

// ---------- 登录 ----------

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	var payload loginPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.Username = strings.TrimSpace(payload.Username)
	ip := s.clientIP(r)
	if payload.Username == "" || payload.Password == "" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "username and password are required")
		return
	}
	if s.limiter.locked(payload.Username, ip) {
		s.writeAccessLog(r.Context(), AccessLog{
			Action: "login", Result: "denied", Error: "locked out", IP: ip, UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusTooManyRequests, CodeRateLimited, "too many failed attempts, try again later")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()

	user, passwordHash, err := s.getUserByName(r.Context(), db, payload.Username)
	if err != nil {
		s.limiter.fail(payload.Username, ip)
		s.writeAccessLog(r.Context(), AccessLog{
			Action: "login", Result: "denied", Error: "unknown user", IP: ip, UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid credentials")
		return
	}
	if user.Disabled {
		// 与「未知用户/密码错误」一致返回通用错误并计入限流，避免暴露账号是否被禁用。
		s.limiter.fail(payload.Username, ip)
		s.writeAccessLog(r.Context(), AccessLog{
			UserID: user.ID, Action: "login", Result: "denied", Error: "user disabled", IP: ip, UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid credentials")
		return
	}
	if !verifyPassword(passwordHash, payload.Password) {
		s.limiter.fail(payload.Username, ip)
		s.writeAccessLog(r.Context(), AccessLog{
			UserID: user.ID, Action: "login", Result: "denied", Error: "bad password", IP: ip, UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid credentials")
		return
	}

	s.limiter.reset(payload.Username, ip)
	plain, token, err := s.issueToken(r.Context(), db, user.ID, strings.TrimSpace(payload.DeviceLabel), ip)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "failed to issue token")
		return
	}
	s.touchLogin(r.Context(), db, user.ID)
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: user.ID, TokenID: token.ID, Action: "token.issue", Result: "ok", IP: ip, UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{
		"token":     plain,
		"expiresAt": token.ExpiresAt,
		"user":      user,
	})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if auth.TokenID != "" {
		db, err := s.open(r.Context())
		if err == nil {
			defer db.Close()
			_ = s.revokeToken(r.Context(), db, auth.TokenID, auth.UserID)
		}
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, TokenID: auth.TokenID, Action: "logout", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{"success": true})
}

// ---------- 用户（管理员面） ----------

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	users, err := s.listUsers(r.Context(), db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, users)
}

func (s *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var payload userPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.Username = strings.TrimSpace(payload.Username)
	if err := validateUsername(payload.Username); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalid, err.Error())
		return
	}
	if len(payload.Password) < 8 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "password must be at least 8 bytes")
		return
	}
	if len(payload.Password) > 72 {
		// bcrypt 对超过 72 字节的输入会直接报错，这里提前给出明确校验信息。
		writeError(w, http.StatusBadRequest, CodeInvalid, "password must be at most 72 bytes")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	createDisplayName := ""
	if payload.DisplayName != nil {
		createDisplayName = strings.TrimSpace(*payload.DisplayName)
	}
	user, err := s.createUser(r.Context(), db, payload.Username, payload.Password, createDisplayName)
	if err != nil {
		if err == errDuplicate {
			writeError(w, http.StatusConflict, CodeConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, Action: "user.create", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, user)
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var payload userPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	// DisplayName 用指针：nil 表示不修改，指向空串表示显式清空。
	if err := s.updateUser(r.Context(), db, id, payload.DisplayName, payload.Disabled); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	if payload.Disabled != nil && *payload.Disabled {
		_ = s.revokeUserTokens(r.Context(), db, id)
	}
	user, err := s.getUserByID(r.Context(), db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, user)
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	if err := s.deleteUser(r.Context(), db, id); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, map[string]interface{}{"success": true})
}

func (s *Service) handleResetPassword(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var payload userPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if len(payload.Password) < 8 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "password must be at least 8 bytes")
		return
	}
	if len(payload.Password) > 72 {
		// bcrypt 对超过 72 字节的输入会直接报错，这里提前给出明确校验信息。
		writeError(w, http.StatusBadRequest, CodeInvalid, "password must be at most 72 bytes")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	if err := s.resetUserPassword(r.Context(), db, id, payload.Password); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, map[string]interface{}{"success": true})
}

// ---------- 令牌 ----------

func (s *Service) handleListTokens(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	userID := auth.UserID
	if auth.IsAdmin {
		userID = strings.TrimSpace(r.URL.Query().Get("userId"))
	}
	tokens, err := s.listTokens(r.Context(), db, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, tokens)
}

func (s *Service) handleRevokeToken(w http.ResponseWriter, r *http.Request, tokenID string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	ownerScope := ""
	if !auth.IsAdmin {
		ownerScope = auth.UserID
	}
	if err := s.revokeToken(r.Context(), db, tokenID, ownerScope); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, TokenID: tokenID, Action: "token.revoke", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{"success": true})
}

// ---------- 实例 ----------

func (s *Service) handleListInstances(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	// 管理员看全部实例；用户只看被授权的实例（默认不授权）。
	var instances []Instance
	if auth.IsAdmin {
		instances, err = s.listInstances(r.Context(), db)
	} else {
		instances, err = s.listInstancesForUser(r.Context(), db, auth.UserID)
	}
	// 列表查询完成后立即归还唯一的 SQLite 连接：后续探测会往返主机 Agent，
	// 期间不应继续占用连接（连接池上限为 1）。
	db.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	views := make([]InstanceView, 0, len(instances))
	hostNames := s.resolveHostNames(r.Context())
	// 探测需逐个往返主机 Agent，默认关闭以免拖慢列表；控制台按需显式 probe=1。
	probe := parseBoolQuery(r, "probe", false)
	if !probe {
		for _, instance := range instances {
			views = append(views, s.buildInstanceViewWithHosts(r.Context(), instance, false, hostNames, nil))
		}
		writeOK(w, views)
		return
	}
	// 开启探测时限制并发，避免大量实例或单个卡住的 Agent 拖垮列表接口。
	views = probeInstancesConcurrently(r.Context(), s, instances, hostNames)
	writeOK(w, views)
}

const instanceProbeConcurrency = 4

func probeInstancesConcurrently(ctx context.Context, service *Service, instances []Instance, hostNames map[string]string) []InstanceView {
	views := make([]InstanceView, len(instances))
	sem := make(chan struct{}, instanceProbeConcurrency)
	var wg sync.WaitGroup
	for index, instance := range instances {
		// 在父循环获取信号量：并发上限同时限制了同时存在的 goroutine 数量，
		// 避免实例很多时一次性开出与实例数相等的阻塞 goroutine。
		sem <- struct{}{}
		wg.Add(1)
		go func(index int, instance Instance) {
			defer wg.Done()
			defer func() { <-sem }()
			views[index] = service.buildInstanceViewWithHosts(ctx, instance, true, hostNames, nil)
		}(index, instance)
	}
	wg.Wait()
	return views
}

func (s *Service) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var payload instancePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.ServerID = strings.TrimSpace(payload.ServerID)
	payload.Label = strings.TrimSpace(payload.Label)
	if payload.ServerID == "" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "serverId is required")
		return
	}
	if payload.Label == "" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "label is required")
		return
	}
	if _, ok := LookupProvider(payload.Provider); !ok {
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown provider")
		return
	}
	if !s.serverAllowed(r.Context(), payload.ServerID, auth.UserID, auth.IsAdmin) {
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown server")
		return
	}
	if payload.Port < 0 || payload.Port > 65535 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "port out of range")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.createInstance(r.Context(), db, payload)
	if err != nil {
		db.Close()
		if err == errDuplicate {
			writeError(w, http.StatusConflict, CodeConflict, "instance already exists for this host and provider")
			return
		}
		if err == errInvalidProvider {
			writeError(w, http.StatusBadRequest, CodeInvalid, "unknown provider")
			return
		}
		if err == errInvalidPort {
			writeError(w, http.StatusBadRequest, CodeInvalid, "port not allowed for this provider")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, InstanceID: instance.ID, Action: "instance.create", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	// 探测会往返主机 Agent（最长 8 秒），先归还唯一的 SQLite 连接再探测。
	db.Close()
	writeOK(w, s.buildInstanceView(r.Context(), instance, true))
}

// lifecycleCapabilityError 判断目标主机是否具备进程管理能力。
// 返回空串表示可用，否则返回可直接下发给客户端的错误说明。
//
// 设置 desiredState 之前必须调用：旧 Agent 上写入期望状态不会生效
// （收敛每轮都会被能力门禁挡下），若不提前报错，用户会看到「设置了但没反应」，
// 且没有任何线索指向「Agent 版本过旧」。
func (s *Service) lifecycleCapabilityError(instance Instance) string {
	if s.runtime == nil {
		return "agent runtime not configured"
	}
	if !s.runtime.AgentOnline(instance.ServerID) {
		return "host agent offline"
	}
	if !s.runtime.AgentSupportsLifecycle(instance.ServerID) {
		return "host agent does not support lifecycle management; please upgrade the agent"
	}
	return ""
}

// handleUpdateInstance 处理实例更新。
func (s *Service) handleUpdateInstance(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var payload instancePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.Port != 0 && (payload.Port < 0 || payload.Port > 65535) {
		writeError(w, http.StatusBadRequest, CodeInvalid, "port out of range")
		return
	}
	if payload.ServerID != "" && !s.serverAllowed(r.Context(), payload.ServerID, "", true) {
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown server")
		return
	}
	// 写期望状态前先校验能力：不支持时明确报错，
	// 而不是落库后静默不收敛（ADR-0006 第 6.5 条）。
	if payload.DesiredState != "" {
		db, err := s.open(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
			return
		}
		current, err := s.getInstance(r.Context(), db, id)
		db.Close()
		if err != nil {
			writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
			return
		}
		if reason := s.lifecycleCapabilityError(current); reason != "" {
			writeError(w, http.StatusServiceUnavailable, CodeChannelError, reason)
			return
		}
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.updateInstance(r.Context(), db, id, payload)
	if err != nil {
		db.Close()
		switch err {
		case errNotFound:
			writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		case errDuplicate:
			writeError(w, http.StatusConflict, CodeConflict, "instance already exists for this host and provider")
		case errInvalidProvider:
			writeError(w, http.StatusBadRequest, CodeInvalid, "unknown provider")
		case errInvalidPort:
			writeError(w, http.StatusBadRequest, CodeInvalid, "port out of range")
		default:
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		}
		return
	}
	// 探测会往返主机 Agent（最长 8 秒），先归还唯一的 SQLite 连接再探测。
	db.Close()

	// 期望状态变化时立即收敛一次，而不是等后台循环（最长 30 秒）。
	// 用户改了「期望运行」却要等半分钟才看到动静，会以为没生效。
	//
	// 只在期望状态真的被写入时收敛：编辑标签这类操作不该触发进程启停。
	// convergeInstance 内部已含全部守卫（runtime、主机在线、能力门禁），
	// 因此这里无需重复判断；它会按期望状态决定是否下发 start/stop。
	//
	// 禁用实例不立即收敛：与后台循环（convergeAllInstances 跳过 !Enabled）及
	// 单实例 lifecycle 接口（disabled 拒绝）语义一致。期望状态照常落库，
	// 重新启用后由后台收敛接管。
	//
	// 收敛结果直接带入视图，避免视图再查一次状态（省一次主机往返）。
	var converged *LifecycleState
	if payload.DesiredState != "" || payload.ClearDesiredState {
		if instance.Enabled {
			state := s.convergeInstance(r.Context(), instance)
			converged = &state
		}
	}
	writeOK(w, s.buildInstanceViewWithHosts(r.Context(), instance, true, nil, converged))
}

func (s *Service) handleDeleteInstance(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	if err := s.deleteInstance(r.Context(), db, id); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, map[string]interface{}{"success": true})
}

// requireInstanceAccess 校验调用方可用该实例：管理员放行，用户需已被授权。
// 未授权与不存在统一按「不存在」返回，避免暴露实例存在性。
func (s *Service) requireInstanceAccess(w http.ResponseWriter, r *http.Request, db *sql.DB, auth authContext, instance Instance) bool {
	if auth.IsAdmin {
		return true
	}
	granted, err := s.instanceGrantedTo(r.Context(), db, instance.ID, auth.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return false
	}
	if !granted {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return false
	}
	return true
}

// ---------- 实例授权（管理员） ----------

// handleInstanceGrants 读取或整体替换某用户可用的实例集合。
// GET 返回该用户的授权实例 ID 列表；PUT 用传入列表整体覆盖（默认不授权）。
func (s *Service) handleInstanceGrants(w http.ResponseWriter, r *http.Request, userID string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	if _, err := s.getUserByID(r.Context(), db, userID); err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "user not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ids, err := s.listInstanceIDsForUser(r.Context(), db, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
			return
		}
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		writeOK(w, map[string]interface{}{"userId": userID, "instanceIds": list})
	case http.MethodPut:
		var payload grantsPayload
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.setGrantsForUser(r.Context(), db, userID, payload.InstanceIDs); err != nil {
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"success": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, CodeInvalid, "method not allowed")
	}
}

// handleInstanceGrantUsers 返回某实例已授权给哪些用户（实例侧展示）。
func (s *Service) handleInstanceGrantUsers(w http.ResponseWriter, r *http.Request, instanceID string) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	if _, err := s.getInstance(r.Context(), db, instanceID); err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	grants, err := s.listGrantsForInstance(r.Context(), db, instanceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, grants)
}

func (s *Service) handleInstanceStatus(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.getInstance(r.Context(), db, id)
	if err != nil {
		db.Close()
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		db.Close()
		return
	}
	if !instance.Enabled {
		db.Close()
		writeError(w, http.StatusForbidden, CodeForbidden, "instance disabled")
		return
	}
	// 探测需要往返主机 Agent（最长 8 秒），先归还唯一的 SQLite 连接再探测。
	db.Close()
	writeOK(w, s.probeInstance(r.Context(), instance))
}

// handleBatchLifecycle 批量对多个实例执行同一生命周期操作。
//
// 请求体：{"action":"start"|"stop"|"restart","instanceIds":["..."]}
//
// 语义与单个操作一致（落期望状态 + 立即执行），但**逐个执行、不并发**：
// 并发启停多个实例会让主机 Agent 的任务队列拥塞，
// 且批量场景本来就是低频运维动作，串行的延迟可以接受。
//
// 部分失败不影响其它实例：逐个收集结果，整体返回 200 并标明每项成败，
// 让前端能精确展示「哪几个成功、哪几个失败」，而不是笼统报错。
func (s *Service) handleBatchLifecycle(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var payload struct {
		Action      string   `json:"action"`
		InstanceIDs []string `json:"instanceIds"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}

	verb := payload.Action
	if verb != "start" && verb != "stop" && verb != "restart" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "action must be start, stop or restart")
		return
	}
	// 上限保护：批量是运维便利功能，不应被当成批量接口滥用。
	const maxBatchSize = 50
	if len(payload.InstanceIDs) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "instanceIds is required")
		return
	}
	if len(payload.InstanceIDs) > maxBatchSize {
		writeError(w, http.StatusBadRequest, CodeInvalid,
			fmt.Sprintf("too many instances in one batch (max %d)", maxBatchSize))
		return
	}

	type itemResult struct {
		InstanceID   string `json:"instanceId"`
		Success      bool   `json:"success"`
		Error        string `json:"error,omitempty"`
		DesiredState string `json:"desiredState,omitempty"`
	}
	results := make([]itemResult, 0, len(payload.InstanceIDs))
	succeeded := 0

	for _, instanceID := range payload.InstanceIDs {
		if r.Context().Err() != nil {
			break
		}
		result, err := s.runLifecycleAction(r.Context(), instanceID, payload.Action, verb)
		if err != nil {
			results = append(results, itemResult{InstanceID: instanceID, Success: false, Error: err.Error()})
			continue
		}
		results = append(results, itemResult{
			InstanceID:   instanceID,
			Success:      true,
			DesiredState: result,
		})
		succeeded++
	}

	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, Action: "instance.batch." + verb,
		Result: fmt.Sprintf("%d/%d", succeeded, len(payload.InstanceIDs)),
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{
		"succeeded": succeeded,
		"failed":    len(results) - succeeded,
		"results":   results,
	})
}

// handleInstanceLifecycle 处理实例进程的生命周期操作（ADR-0006）。
//
// 请求体：{"action":"start"|"stop"|"restart","desiredState":"running"|"stopped"|"clear"}
//   - start / stop：立即对目标实例执行一次，并同步设置期望状态；
//   - restart：先 stop 再 start，期望状态置为 running；
//   - 只传 desiredState 时仅改期望状态，由后续查询触发收敛。
//
// 权限与其它实例接口一致：管理员全权，用户需被授权。
func (s *Service) handleInstanceLifecycle(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var payload struct {
		Action       string `json:"action"`
		DesiredState string `json:"desiredState"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.getInstance(r.Context(), db, id)
	if err != nil {
		db.Close()
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		db.Close()
		return
	}
	if !instance.Enabled {
		db.Close()
		writeError(w, http.StatusForbidden, CodeForbidden, "instance disabled")
		return
	}

	// 能力门禁：旧 Agent 明确报错，而不是静默降级为「只探测」（ADR-0006 第 6.5 条）。
	if reason := s.lifecycleCapabilityError(instance); reason != "" {
		db.Close()
		writeError(w, http.StatusServiceUnavailable, CodeChannelError, reason)
		return
	}

	// 先确定本次要落库的期望状态。
	nextDesired := instance.DesiredState
	switch payload.Action {
	case "start", "restart":
		nextDesired = DesiredStateRunning
	case "stop":
		nextDesired = DesiredStateStopped
	case "":
		// 仅改期望状态。
	default:
		db.Close()
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown action")
		return
	}
	if payload.DesiredState != "" {
		if !ValidDesiredState(payload.DesiredState) {
			db.Close()
			writeError(w, http.StatusBadRequest, CodeInvalid, "invalid desiredState")
			return
		}
		nextDesired = payload.DesiredState
	}

	if nextDesired != instance.DesiredState {
		if _, err := s.updateInstance(r.Context(), db, instance.ID, instancePayload{DesiredState: nextDesired}); err != nil {
			db.Close()
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
			return
		}
		instance.DesiredState = nextDesired
	}
	// 后续动作要往返主机 Agent，先归还唯一的 SQLite 连接。
	db.Close()

	result, err := s.executeLifecycleAction(r.Context(), instance, payload.Action)
	if err != nil {
		// 动作失败不改期望状态：保留它，让下一次收敛重试（ADR-0006 第 3.5 条）。
		s.writeAccessLog(r.Context(), AccessLog{
			UserID: auth.UserID, InstanceID: instance.ID,
			Action: "instance.lifecycle." + payload.Action, Result: "error",
			Error: err.Error(), IP: s.clientIP(r), UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusBadGateway, CodeChannelError, err.Error())
		return
	}

	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, InstanceID: instance.ID,
		Action: "instance.lifecycle." + payload.Action, Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{
		"desiredState": nextDesired,
		"lifecycle":    lifecycleFromResult(result, true),
	})
}

// executeLifecycleAction 对单个实例执行一次进程操作，返回操作后的实际状态。
//
// 抽出公共实现供单实例与批量接口复用，避免两条路径的行为漂移。
// 空 action 表示只回读状态（用于「仅改期望状态」的场景）。
func (s *Service) executeLifecycleAction(
	ctx context.Context,
	instance Instance,
	action string,
) (LifecycleResult, error) {
	// restart 先停：失败不阻断，start 会因为端口仍被占用而明确报错。
	if action == "restart" {
		stopCtx, stopCancel := context.WithTimeout(ctx, 20*time.Second)
		_, _ = s.runtime.StopProcess(stopCtx, instance.ServerID, instance.ID)
		stopCancel()
	}

	switch action {
	case "start", "restart":
		startCtx, startCancel := context.WithTimeout(ctx, 25*time.Second)
		defer startCancel()
		provider, _ := LookupProvider(instance.Provider)
		result, err := s.runtime.StartProcess(startCtx, instance.ServerID, LifecycleStartPayload{
			InstanceID: instance.ID,
			Provider:   provider.ID,
			Port:       instance.Port,
		})
		if err != nil {
			s.metrics.lifecycleFailures.Add(1)
			s.metrics.recordError("lifecycle " + action + " " + instance.ID + ": " + err.Error())
		} else {
			s.metrics.lifecycleStarts.Add(1)
		}
		return result, err
	case "stop":
		stopCtx, stopCancel := context.WithTimeout(ctx, 20*time.Second)
		defer stopCancel()
		result, err := s.runtime.StopProcess(stopCtx, instance.ServerID, instance.ID)
		if err != nil {
			s.metrics.lifecycleFailures.Add(1)
			s.metrics.recordError("lifecycle stop " + instance.ID + ": " + err.Error())
		} else {
			s.metrics.lifecycleStops.Add(1)
		}
		return result, err
	default:
		// 只改期望状态：回读一次实际状态。
		statusCtx, statusCancel := context.WithTimeout(ctx, 10*time.Second)
		defer statusCancel()
		return s.runtime.ProcessStatus(statusCtx, instance.ServerID, instance.ID)
	}
}

// runLifecycleAction 是批量接口的单实例入口：校验 + 落期望状态 + 执行。
// 返回落库后的期望状态字符串，供批量结果展示。
//
// 与单实例接口共用同一套校验（启用状态、能力门禁），保证两条路径语义一致。
func (s *Service) runLifecycleAction(ctx context.Context, instanceID, action, verb string) (string, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", errors.New("database unavailable")
	}
	instance, err := s.getInstance(ctx, db, instanceID)
	if err != nil {
		db.Close()
		return "", errors.New("instance not found")
	}
	if !instance.Enabled {
		db.Close()
		return "", errors.New("instance disabled")
	}
	if reason := s.lifecycleCapabilityError(instance); reason != "" {
		db.Close()
		return "", errors.New(reason)
	}

	nextDesired := DesiredStateRunning
	if verb == "stop" {
		nextDesired = DesiredStateStopped
	}
	if nextDesired != instance.DesiredState {
		if _, err := s.updateInstance(ctx, db, instance.ID, instancePayload{DesiredState: nextDesired}); err != nil {
			db.Close()
			return "", err
		}
		instance.DesiredState = nextDesired
	}
	// 执行动作要往返主机 Agent，先归还唯一的 SQLite 连接。
	db.Close()

	if _, err := s.executeLifecycleAction(ctx, instance, action); err != nil {
		return "", err
	}
	return nextDesired, nil
}

func (s *Service) handleAccessInfo(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	instance, err := s.getInstance(r.Context(), db, id)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		return
	}
	provider, _ := LookupProvider(instance.Provider)
	writeOK(w, map[string]interface{}{
		"instanceId":    instance.ID,
		"label":         instance.Label,
		"provider":      provider.ID,
		"providerLabel": provider.Label,
		"serverId":      instance.ServerID,
		"port":          instance.Port,
		"gatewayUrl":    s.gatewayURL(instance.ID),
		"gatewayPath":   routePrefix + "/gw/" + instance.ID,
		"streaming":     provider.Streaming,
	})
}

func (s *Service) handleInstanceMeta(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	instance, err := s.getInstance(r.Context(), db, id)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		meta, err := s.getMeta(r.Context(), db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"instanceId": id, "meta": json.RawMessage(meta)})
	case http.MethodPut:
		var payload metaPayload
		if !decodeJSON(w, r, &payload) {
			return
		}
		if !json.Valid([]byte(payload.Meta)) {
			writeError(w, http.StatusBadRequest, CodeInvalid, "meta must be valid JSON")
			return
		}
		if err := s.putMeta(r.Context(), db, id, auth.UserID, payload.Meta); err != nil {
			if err == errMetaTooLarge {
				writeError(w, http.StatusRequestEntityTooLarge, CodeInvalid, "meta too large")
				return
			}
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"success": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, CodeInvalid, "method not allowed")
	}
}

// ---------- 其它 ----------

// ---------- 用户偏好（多端同步） ----------

// handleListPreferences 返回当前用户的全部偏好。
func (s *Service) handleListPreferences(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	items, err := s.listPreferences(r.Context(), db, auth.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, items)
}

// handlePutPreferences 批量写入偏好。请求体可带 lastWriteWins 表示按时间戳
// 条件覆盖（客户端增量同步用）；不带则直接覆盖（客户端全量推送用）。
func (s *Service) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var payload preferencesPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if len(payload.Values) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "values required")
		return
	}
	lastWriteWins := r.URL.Query().Get("lastWriteWins") == "1"
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	written, err := s.putPreferences(r.Context(), db, auth.UserID, payload.Values, payload.UpdatedAt, lastWriteWins)
	if err != nil {
		switch {
		case errors.Is(err, errPreferenceTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, CodeInvalid, "preference value too large")
		case errors.Is(err, errInvalidPreferenceKey):
			writeError(w, http.StatusBadRequest, CodeInvalid, "invalid preference key")
		default:
			writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		}
		return
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, TokenID: auth.TokenID, Action: "preferences.put", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{"written": written})
}

// handleDeletePreferences 批量删除偏好，用于客户端清空若干项设置时同步删除。
// 键经请求体传入而非路径段：偏好键允许冒号等字符且可能含斜杠，放进路径会被
// 分段或需转义，改用与 PUT 一致的批量载荷可保证任意键可靠往返。
func (s *Service) handleDeletePreferences(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var payload preferencesPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if len(payload.Keys) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalid, "keys required")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	removed, err := s.deletePreferences(r.Context(), db, auth.UserID, payload.Keys)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	s.writeAccessLog(r.Context(), AccessLog{
		UserID: auth.UserID, TokenID: auth.TokenID, Action: "preferences.delete", Result: "ok",
		IP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	writeOK(w, map[string]interface{}{"removed": removed})
}

func (s *Service) handleProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeOK(w, Providers())
}

func (s *Service) handleServers(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	writeOK(w, s.listServersFor(r.Context(), auth.UserID, auth.IsAdmin))
}

// handleServerDiagnose 诊断指定主机上某 Provider 的可用性。
//
// 返回 Agent 侧解析出的可执行文件路径与是否就绪、Provider 端口区间、
// 区间内被占用的端口与第一个建议空闲端口。前端在创建/编辑实例时用它
// 预检：默认端口被占时提示并一键切换，避免 start 阶段才报「端口被占用」。
//
// 权限与实例接口一致：管理员全权，用户需能看到该主机。能力不足（旧 Agent）
// 或主机离线时给出可读错误，不回退到「默认端口可用」的猜测。
func (s *Service) handleServerDiagnose(w http.ResponseWriter, r *http.Request, serverID string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "server id is required")
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		writeError(w, http.StatusBadRequest, CodeInvalid, "provider is required")
		return
	}
	if _, ok := LookupProvider(provider); !ok {
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown provider")
		return
	}
	if !s.serverAllowed(r.Context(), serverID, auth.UserID, auth.IsAdmin) {
		writeError(w, http.StatusForbidden, CodeForbidden, "server not accessible")
		return
	}
	if s.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, CodeChannelError, "agent runtime not configured")
		return
	}
	if !s.runtime.AgentOnline(serverID) {
		writeError(w, http.StatusServiceUnavailable, CodeChannelError, "host agent offline")
		return
	}
	if !s.runtime.AgentSupportsLifecycle(serverID) {
		writeError(w, http.StatusServiceUnavailable, CodeChannelError,
			"host agent does not support lifecycle management; please upgrade the agent")
		return
	}

	result, err := s.runtime.Diagnose(r.Context(), serverID, provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeChannelError, err.Error())
		return
	}
	// 补全服务端已知的端口区间（Agent 返回的 portRange 可能为空时兜底）。
	if result.PortRange.Min == 0 && result.PortRange.Max == 0 {
		if p, ok := LookupProvider(provider); ok {
			result.PortRange = PortRange{Min: p.DefaultPort, Max: p.DefaultPort + p.PortRangeSize}
		}
	}
	writeOK(w, result)
}

func (s *Service) handleLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	limit, _ := parseIntQuery(r, "limit", 100)
	// instanceId 可选：用于排查单个实例的问题。
	instanceID := strings.TrimSpace(r.URL.Query().Get("instanceId"))
	logs, err := s.listAccessLogs(r.Context(), db, limit, instanceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, logs)
}

// handleMetrics 返回模块运行期计数，供运维排查「收敛是否在正常工作」。
//
// 仅管理员可读：计数值本身不敏感，但含最近错误摘要，可能带主机/实例标识。
func (s *Service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	writeOK(w, s.Metrics())
}

func (s *Service) handleStreamToken(w http.ResponseWriter, r *http.Request, instanceID string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	defer db.Close()
	instance, err := s.getInstance(r.Context(), db, instanceID)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !s.requireInstanceAccess(w, r, db, auth, instance) {
		return
	}
	if !instance.Enabled {
		writeError(w, http.StatusForbidden, CodeForbidden, "instance disabled")
		return
	}
	token := s.streamTokens.issue(auth.UserID, instance.ID)
	if token == "" {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "failed to issue stream token")
		return
	}
	writeOK(w, map[string]interface{}{
		"streamToken": token,
		"expiresIn":   int(streamTokenTTL.Seconds()),
	})
}

// serverAllowed 校验 serverId 是否属于调用方可用的主机（防止把网关当成任意
// 内网端口转发器，也防止跨租户探测他人主机）。
// 管理员：可任选已登记的可选主机；普通用户：只能使用自己已有实例用过的主机。
func (s *Service) serverAllowed(ctx context.Context, serverID, userID string, isAdmin bool) bool {
	if strings.TrimSpace(serverID) == "" {
		return false
	}
	db, err := s.store.Open(ctx)
	if err != nil {
		return false
	}
	defer db.Close()

	if !isAdmin {
		// 非管理员只允许使用自己被授权实例涉及的主机，避免枚举/探测全舰主机。
		var granted int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM aiagent_instance_grants g
			 INNER JOIN aiagent_instances i ON i.id = g.instance_id
			 WHERE i.server_id = ? AND g.user_id = ?`,
			serverID, userID).Scan(&granted); err != nil {
			return false
		}
		return granted > 0
	}

	// 管理员：必须是已登记的可选主机。未注入 ServerProvider 时退化为
	// 「已被任何实例使用过的主机」。
	if s.servers == nil {
		var used int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM aiagent_instances WHERE server_id = ?`, serverID).Scan(&used); err != nil {
			return false
		}
		return used > 0
	}
	for _, option := range s.servers.ListServerOptions(ctx) {
		if option.ID == serverID {
			return true
		}
	}
	return false
}

// listServersFor 返回调用方可见的主机：管理员看全部；普通用户只看被授权实例涉及的主机。
func (s *Service) listServersFor(ctx context.Context, userID string, isAdmin bool) []ServerOption {
	if s.servers == nil {
		return []ServerOption{}
	}
	all := s.servers.ListServerOptions(ctx)
	if isAdmin {
		return all
	}
	db, err := s.store.Open(ctx)
	if err != nil {
		return []ServerOption{}
	}
	defer db.Close()
	granted := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT i.server_id FROM aiagent_instance_grants g
		INNER JOIN aiagent_instances i ON i.id = g.instance_id WHERE g.user_id = ?`, userID)
	if err != nil {
		return []ServerOption{}
	}
	defer rows.Close()
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			return []ServerOption{}
		}
		granted[serverID] = true
	}
	if err := rows.Err(); err != nil {
		return []ServerOption{}
	}
	filtered := make([]ServerOption, 0, len(all))
	for _, option := range all {
		if granted[option.ID] {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

// ---------- 视图组装 ----------

func (s *Service) buildInstanceView(ctx context.Context, instance Instance, probe bool) InstanceView {
	return s.buildInstanceViewWithHosts(ctx, instance, probe, nil, nil)
}

// buildInstanceViewWithHosts 允许调用方传入一次解析好的主机名映射，
// 避免列表场景下每个实例都去查一次主机（N+1）。
//
// precomputedLifecycle 非 nil 时直接采用它，不再查询主机：
// 更新接口在写入期望状态后已收敛过一次，重复查询会白多一次往返。
func (s *Service) buildInstanceViewWithHosts(
	ctx context.Context,
	instance Instance,
	probe bool,
	hostNames map[string]string,
	precomputedLifecycle *LifecycleState,
) InstanceView {
	provider, _ := LookupProvider(instance.Provider)
	view := InstanceView{
		Instance:      instance,
		ProviderLabel: provider.Label,
		AccessPath:    routePrefix + "/gw/" + instance.ID,
		GatewayURL:    s.gatewayURL(instance.ID),
	}
	if hostNames != nil {
		view.HostName = hostNames[instance.ServerID]
	} else if s.servers != nil {
		for _, option := range s.servers.ListServerOptions(ctx) {
			if option.ID == instance.ServerID {
				view.HostName = option.Name
				break
			}
		}
	}
	if probe {
		// 读路径只读：收敛已移到 30 秒后台循环（convergeAllInstances），
		// GET 列表不再产生启动/停止副作用。
		//
		// 托管实例（desired_state 非空）走 lifecycle 查询：它已包含端口与资源信息，
		// 因此不必再单独发一次探测任务，每次刷新从 2-3 次主机往返降到 1 次。
		// 未托管实例保持原有探测路径，行为与升级前完全一致。
		managed := instance.DesiredState != ""
		var lifecycle LifecycleState
		switch {
		case precomputedLifecycle != nil:
			lifecycle = *precomputedLifecycle
		default:
			// 无论托管与否都查一次 lifecycle 能力标记：queryLifecycle 对空
			// DesiredState 会返回 Supported=true（仅查 Agent 能力，不往返主机），
			// 让前端能区分「可托管」与「Agent 版本过旧」而不是一律报旧版本。
			lifecycle = s.queryLifecycle(ctx, instance)
		}
		view.Lifecycle = lifecycle

		if managed {
			view.Status = statusFromLifecycle(lifecycle)
			view.HostOnline = lifecycle.Supported
		} else {
			view.Status = s.probeInstance(ctx, instance)
			view.HostOnline = view.Status.HostOnline
		}
	}
	return view
}

// statusFromLifecycle 把托管状态映射成探测视图。
//
// 托管实例的权威来源是 Agent 持有的进程表，因此这里直接以它为准构造
// InstanceState，而不是再发一次探测任务去「猜」。关联验证的语义保持一致：
// 在线要求「端口被监听」且「监听者是本 Provider 的进程」。
func statusFromLifecycle(lifecycle LifecycleState) InstanceState {
	state := InstanceState{
		HostOnline:             lifecycle.Supported,
		ProcessRunning:         lifecycle.Running,
		PortListening:          lifecycle.PortListening,
		PID:                    lifecycle.PID,
		ListenerPID:            lifecycle.ListenerPID,
		ListenerMatchesProcess: lifecycle.ListenerMatchesProcess,
		MemoryBytes:            lifecycle.MemoryBytes,
		CPUPercent:             lifecycle.CPUPercent,
		ProbedAt:               nowRFC3339(),
	}
	state.Online = lifecycle.Running && lifecycle.PortListening && lifecycle.ListenerMatchesProcess

	if !state.Online && lifecycle.Supported {
		switch {
		case lifecycle.Crashed:
			state.Error = "process crashed and restart attempts are exhausted"
		case lifecycle.PortListening && !lifecycle.ListenerMatchesProcess:
			state.Error = "port is occupied by a different process"
		case lifecycle.Running && !lifecycle.PortListening:
			state.Error = "process is running but not listening on the expected port"
		default:
			state.Error = "process is not running"
		}
	}
	return state
}

// resolveHostNames 一次取回 serverId → 主机名 映射，供列表组装复用。
func (s *Service) resolveHostNames(ctx context.Context) map[string]string {
	names := make(map[string]string)
	if s.servers == nil {
		return names
	}
	for _, option := range s.servers.ListServerOptions(ctx) {
		names[option.ID] = option.Name
	}
	return names
}

// queryLifecycle 只查询托管进程状态，不产生任何副作用。
//
// 读路径（实例列表/详情）专用：收敛是写操作，已移到后台循环，
// GET 请求不应触发进程启停。
//
// 未托管或能力不足时返回零值 + Supported 标记，让调用方知道「为什么没有状态」。
func (s *Service) queryLifecycle(ctx context.Context, instance Instance) LifecycleState {
	state := LifecycleState{}
	if s.runtime == nil {
		return state
	}
	if !s.runtime.AgentOnline(instance.ServerID) {
		return state
	}
	if !s.runtime.AgentSupportsLifecycle(instance.ServerID) {
		return state
	}
	state.Supported = true
	if instance.DesiredState == "" {
		// 未托管：能力可用但未纳管，返回 Supported 让前端可以展示「可托管」。
		return state
	}

	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := s.runtime.ProcessStatus(statusCtx, instance.ServerID, instance.ID)
	if err != nil {
		applog.Warn(ctx, "aiagent", "lifecycle query failed",
			"instance", instance.ID, "error", err.Error())
		return state
	}
	return lifecycleFromResult(result, true)
}

// convergeInstance 按期望状态收敛实例进程（ADR-0006 第 3.3 条），并返回收敛后的状态。
//
// 幂等：实际状态已符合期望时不下发任何任务。收敛失败只记录日志，
// 不修改期望状态——否则一次网络抖动就会把「期望 running」悄悄改成 stopped。
//
// 收敛后回读一次状态：启动/停止是异步的，直接返回动作前的结果会让 UI 看到过期状态。
func (s *Service) convergeInstance(ctx context.Context, instance Instance) LifecycleState {
	state := LifecycleState{}
	if s.runtime == nil || instance.DesiredState == "" {
		return state
	}
	if !s.runtime.AgentOnline(instance.ServerID) {
		return state
	}
	if !s.runtime.AgentSupportsLifecycle(instance.ServerID) {
		return state
	}
	state.Supported = true

	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	current, err := s.runtime.ProcessStatus(statusCtx, instance.ServerID, instance.ID)
	cancel()
	if err != nil {
		applog.Warn(ctx, "aiagent", "converge: status failed",
			"instance", instance.ID, "error", err.Error())
		return state
	}

	acted := false
	switch instance.DesiredState {
	case DesiredStateRunning:
		if !current.Managed || !current.Running {
			startCtx, startCancel := context.WithTimeout(ctx, 25*time.Second)
			provider, _ := LookupProvider(instance.Provider)
			_, startErr := s.runtime.StartProcess(startCtx, instance.ServerID, LifecycleStartPayload{
				InstanceID: instance.ID,
				Provider:   provider.ID,
				Port:       instance.Port,
			})
			startCancel()
			if startErr != nil {
				s.metrics.convergenceFailures.Add(1)
				s.metrics.recordError("converge start " + instance.ID + ": " + startErr.Error())
				applog.Warn(ctx, "aiagent", "converge: start failed",
					"instance", instance.ID, "error", startErr.Error())
			} else {
				s.metrics.convergenceStarts.Add(1)
			}
			acted = true
		}
	case DesiredStateStopped:
		if current.Managed && current.Running {
			stopCtx, stopCancel := context.WithTimeout(ctx, 20*time.Second)
			_, stopErr := s.runtime.StopProcess(stopCtx, instance.ServerID, instance.ID)
			stopCancel()
			if stopErr != nil {
				s.metrics.convergenceFailures.Add(1)
				s.metrics.recordError("converge stop " + instance.ID + ": " + stopErr.Error())
				applog.Warn(ctx, "aiagent", "converge: stop failed",
					"instance", instance.ID, "error", stopErr.Error())
			} else {
				s.metrics.convergenceStops.Add(1)
			}
			acted = true
		}
	}

	if !acted {
		return lifecycleFromResult(current, state.Supported)
	}

	// 动作之后回读，反映收敛结果。
	readbackCtx, readbackCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readbackCancel()
	after, err := s.runtime.ProcessStatus(readbackCtx, instance.ServerID, instance.ID)
	if err != nil {
		return state
	}
	return lifecycleFromResult(after, state.Supported)
}

// lifecycleFromResult 把 Agent 返回的进程状态映射为对外视图。
// 抽出来避免「无需动作」与「动作后回读」两条路径字段遗漏（曾漏过 memory/cpu）。
func lifecycleFromResult(result LifecycleResult, supported bool) LifecycleState {
	return LifecycleState{
		Managed:                result.Managed,
		Running:                result.Running,
		PID:                    result.PID,
		DesiredRunning:         result.DesiredRunning,
		Crashed:                result.Crashed,
		PortListening:          result.PortListening,
		ListenerPID:            result.ListenerPID,
		ListenerMatchesProcess: result.ListenerMatchesProcess,
		UptimeSeconds:          result.UptimeSeconds,
		Restarts:               result.Restarts,
		MemoryBytes:            result.MemoryBytes,
		CPUPercent:             result.CPUPercent,
		Supported:              supported,
	}
}

// probeInstance 解析实例运行时状态。探测失败与「进程未运行」严格区分。
func (s *Service) probeInstance(ctx context.Context, instance Instance) InstanceState {
	state := InstanceState{ProbedAt: nowRFC3339()}
	if s.runtime == nil {
		state.Error = "agent runtime not configured"
		return state
	}
	if !s.runtime.AgentOnline(instance.ServerID) {
		state.HostOnline = false
		state.Error = "host agent offline"
		return state
	}
	state.HostOnline = true
	provider, ok := LookupProvider(instance.Provider)
	if !ok {
		state.Error = "unknown provider"
		return state
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := s.runtime.Probe(probeCtx, instance.ServerID, provider.ID, instance.Port, provider.ProcessMatch)
	if err != nil {
		// 探测失败明细可能包含主机路径/权限等内部信息，不下发给客户端。
		state.Error = "host probe failed"
		return state
	}
	state.ProcessRunning = result.ProcessRunning
	state.PortListening = result.PortListening
	state.PID = result.PID
	state.ListenerPID = result.ListenerPID
	state.ListenerMatchesProcess = result.ListenerMatchesProcess
	// 资源占用：仅在真正在线时才有意义，否则会显示占用者的数值造成误读。
	state.MemoryBytes = result.MemoryBytes
	state.CPUPercent = result.CPUPercent

	// 关联判定（ADR-0006 第 2 条）：只有在「端口被监听」且「监听它的进程命中
	// Provider 进程规则」时才算在线。这样能区分三种原本会被混为一谈的情况：
	//   1. 目标进程在跑且占着端口 → 在线
	//   2. 端口被其它进程占用 → 离线，且给出明确原因
	//   3. 目标进程在跑但没监听该端口 → 离线，且给出明确原因
	state.Online = result.PortListening && result.ListenerMatchesProcess

	if !state.Online {
		switch {
		case !state.HostOnline:
			state.Error = "host agent offline"
		case result.PortListening && !result.ListenerMatchesProcess:
			state.Error = "port is occupied by a different process"
		case result.ProcessRunning && !result.PortListening:
			state.Error = "process is running but not listening on the expected port"
		default:
			state.Error = "process or port not reachable on host"
		}
	}
	return state
}

// gatewayURL 生成客户端可用的网关绝对地址。优先使用请求上下文中的公开地址不可得，
// 因此返回相对路径 + 由前端拼接面板域名；此处给出路径形式，绝对地址交由调用方。
func (s *Service) gatewayURL(instanceID string) string {
	return routePrefix + "/gw/" + instanceID
}

// ---------- HTTP 辅助 ----------

func writeOK(w http.ResponseWriter, data interface{}) {
	response.OK(w, data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	response.JSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    code,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, CodeInvalid, "request body required")
		return false
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalid, "invalid JSON body")
		return false
	}
	return true
}

func validateUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return fmt.Errorf("username must be 3-32 characters")
	}
	for _, r := range username {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.'
		if !valid {
			return fmt.Errorf("username may only contain letters, digits, '-', '_', and '.'")
		}
	}
	return nil
}

// clientIP 解析请求来源 IP：仅当直连对端属于受信代理时才采信转发头部，
// 否则一律使用直连地址。登录限流与审计都以该值为准，避免被伪造
// X-Forwarded-For 绕过锁定或污染日志（与 auth/requestClientIP 同一约定）。
func (s *Service) clientIP(r *http.Request) string {
	direct := ""
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		direct = host
	} else if r.RemoteAddr != "" {
		direct = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	}
	if direct == "" {
		return "unknown"
	}

	trusted := false
	if ip := net.ParseIP(direct); ip != nil {
		if s.isTrustedProxy(ip) {
			trusted = true
		} else if !s.cfg.IsProduction() && ip.IsLoopback() {
			trusted = true
		}
	}
	if !trusted {
		return direct
	}

	if candidate := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); candidate != "" {
		if parsed := net.ParseIP(candidate); parsed != nil {
			return parsed.String()
		}
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if parsed := net.ParseIP(strings.TrimSpace(strings.Split(forwarded, ",")[0])); parsed != nil {
			return parsed.String()
		}
	}
	if candidate := strings.TrimSpace(r.Header.Get("X-Real-IP")); candidate != "" {
		if parsed := net.ParseIP(candidate); parsed != nil {
			return parsed.String()
		}
	}
	return direct
}

func (s *Service) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, entry := range s.cfg.TrustedProxyCIDRs {
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if candidate := net.ParseIP(entry); candidate != nil && candidate.Equal(ip) {
			return true
		}
	}
	return false
}

func parseBoolQuery(r *http.Request, key string, fallback bool) bool {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseIntQuery(r *http.Request, key string, fallback int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, false
	}
	return value, true
}
