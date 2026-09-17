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
	case strings.HasPrefix(path, "/instances/") && strings.HasSuffix(path, "/status") && r.Method == http.MethodGet:
		s.handleInstanceStatus(w, r, trimSegment(path, "/instances/", "/status"))
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
	case path == "/logs" && r.Method == http.MethodGet:
		s.handleLogs(w, r)

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
	instances, err := s.listInstances(r.Context(), db, auth.UserID, auth.IsAdmin)
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
			views = append(views, s.buildInstanceViewWithHosts(r.Context(), instance, false, hostNames))
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
			views[index] = service.buildInstanceViewWithHosts(ctx, instance, true, hostNames)
		}(index, instance)
	}
	wg.Wait()
	return views
}

func (s *Service) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.requireAuth(w, r)
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
	// 实例必须有归属：管理员需显式指定目标用户，普通用户归属自己。
	ownerID := auth.UserID
	if auth.IsAdmin {
		ownerID = strings.TrimSpace(payload.UserID)
		if ownerID == "" {
			writeError(w, http.StatusBadRequest, CodeInvalid, "userId is required when creating instances as an administrator")
			return
		}
	}
	// 普通用户只能在自己的机器上建实例；把新主机纳入清单是管理员动作，
	// 避免任何登录用户枚举全舰主机并探测其端口状态。
	if !auth.IsAdmin && !s.serverAllowed(r.Context(), payload.ServerID, auth.UserID, false) {
		writeError(w, http.StatusForbidden, CodeForbidden, "host not assigned to this account; ask an administrator to add it")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.createInstance(r.Context(), db, payload, ownerID)
	if err != nil {
		db.Close()
		if err == errDuplicate {
			writeError(w, http.StatusConflict, CodeConflict, "instance already exists for this host and port")
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
		if err == errNotFound {
			writeError(w, http.StatusBadRequest, CodeInvalid, "owner user not found")
			return
		}
		if err == errInvalidCreds {
			writeError(w, http.StatusBadRequest, CodeInvalid, "instance owner is required")
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

func (s *Service) handleUpdateInstance(w http.ResponseWriter, r *http.Request, id string) {
	auth, ok := s.requireAuth(w, r)
	if !ok {
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
	if payload.ServerID != "" && !s.serverAllowed(r.Context(), payload.ServerID, auth.UserID, auth.IsAdmin) {
		writeError(w, http.StatusBadRequest, CodeInvalid, "unknown server")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}
	instance, err := s.updateInstance(r.Context(), db, id, payload, auth.UserID, auth.IsAdmin)
	if err != nil {
		db.Close()
		switch err {
		case errNotFound:
			writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		case errDuplicate:
			writeError(w, http.StatusConflict, CodeConflict, "instance already exists for this host and port")
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
	writeOK(w, s.buildInstanceView(r.Context(), instance, true))
}

func (s *Service) handleDeleteInstance(w http.ResponseWriter, r *http.Request, id string) {
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
	if err := s.deleteInstance(r.Context(), db, id, auth.UserID, auth.IsAdmin); err != nil {
		if err == errNotFound {
			writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
			return
		}
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, map[string]interface{}{"success": true})
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
	if !auth.IsAdmin && instance.UserID != auth.UserID {
		db.Close()
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
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
	if !auth.IsAdmin && instance.UserID != auth.UserID {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
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
	if !auth.IsAdmin && instance.UserID != auth.UserID {
		writeError(w, http.StatusNotFound, CodeNotFound, "instance not found")
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
		if err := s.putMeta(r.Context(), db, id, instance.UserID, payload.Meta); err != nil {
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
	logs, err := s.listAccessLogs(r.Context(), db, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeChannelError, err.Error())
		return
	}
	writeOK(w, logs)
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
	if !auth.IsAdmin && instance.UserID != auth.UserID {
		writeError(w, http.StatusForbidden, CodeForbidden, "instance not owned by caller")
		return
	}
	if !instance.Enabled {
		writeError(w, http.StatusForbidden, CodeForbidden, "instance disabled")
		return
	}
	token := s.streamTokens.issue(instance.UserID, instance.ID)
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
		// 非管理员只允许使用自己已登记实例涉及的主机，避免枚举/探测全舰主机。
		var owned int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM aiagent_instances WHERE server_id = ? AND user_id = ?`,
			serverID, userID).Scan(&owned); err != nil {
			return false
		}
		return owned > 0
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

// listServersFor 返回调用方可见的主机：管理员看全部；普通用户只看自己实例涉及的主机。
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
	owned := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT server_id FROM aiagent_instances WHERE user_id = ?`, userID)
	if err != nil {
		return []ServerOption{}
	}
	defer rows.Close()
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			return []ServerOption{}
		}
		owned[serverID] = true
	}
	if err := rows.Err(); err != nil {
		return []ServerOption{}
	}
	filtered := make([]ServerOption, 0, len(all))
	for _, option := range all {
		if owned[option.ID] {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

// ---------- 视图组装 ----------

func (s *Service) buildInstanceView(ctx context.Context, instance Instance, probe bool) InstanceView {
	return s.buildInstanceViewWithHosts(ctx, instance, probe, nil)
}

// buildInstanceViewWithHosts 允许调用方传入一次解析好的主机名映射，
// 避免列表场景下每个实例都去查一次主机（N+1）。
func (s *Service) buildInstanceViewWithHosts(ctx context.Context, instance Instance, probe bool, hostNames map[string]string) InstanceView {
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
		view.Status = s.probeInstance(ctx, instance)
		view.HostOnline = view.Status.HostOnline
	}
	return view
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
	state.Online = result.ProcessRunning && result.PortListening
	if !state.Online {
		// 探测明细可能包含主机路径/权限等内部信息，不下发给客户端，只保留统一说明。
		state.Error = "process or port not reachable on host"
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
