package uptime

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const (
	defaultIntervalSeconds = 60
	defaultTimeoutSeconds  = 30
	defaultConfirmCount    = 3
	maxProbeBodyBytes      = 1024 * 1024

	stateUp          = "up"
	stateDown        = "down"
	statePendingDown = "pending_down"
	statePendingUp   = "pending_up"
	stateMaintenance = "maintenance"
	statePaused      = "paused"
	stateUnknown     = "unknown"

	defaultHeartbeatRetentionDays = 30
	minHeartbeatRetentionDays     = 1
)

type Authenticator interface {
	IsAuthenticated(context.Context, *http.Request) (bool, error)
}

type Notifier interface {
	Trigger(context.Context, string, string, map[string]interface{}) error
}

type Service struct {
	cfg                  config.Config
	store                *database.Store
	schema               database.SchemaEnsurer
	auth                 Authenticator
	notifier             Notifier
	heartbeatBroadcaster func(monitorID int64, beat map[string]interface{})

	mu            sync.Mutex
	timers        map[int64]*time.Timer
	stopped       bool
	lastSslAlerts sync.Map // monitorID -> time.Time (expiry)

	// per-monitor 检查互斥：同一 monitor 的定时检查、checkNow、state 迁移
	// 必须串行执行，防止并发 processState 的读-改-写产生重复 incident。
	checkMux   sync.Mutex
	checkLocks map[int64]*sync.Mutex
}

type probeResult struct {
	OK         bool                   `json:"ok"`
	Status     string                 `json:"status"`
	LatencyMS  int64                  `json:"latencyMs"`
	Message    string                 `json:"message"`
	StatusCode *int                   `json:"statusCode,omitempty"`
	ErrorCode  string                 `json:"errorCode,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
	SslExpiry  *time.Time             `json:"sslExpiry,omitempty"`
}

type statusPage struct {
	ID           int64                  `json:"id"`
	Slug         string                 `json:"slug"`
	Domain       *string                `json:"domain"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Theme        string                 `json:"theme"`
	Public       bool                   `json:"public"`
	CacheSeconds int                    `json:"cacheSeconds"`
	Config       map[string]interface{} `json:"config"`
	MonitorIDs   []int64                `json:"monitorIds"`
	CreatedAt    *string                `json:"createdAt"`
	UpdatedAt    *string                `json:"updatedAt"`
}

type maintenanceWindow struct {
	ID          int64                    `json:"id"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Strategy    string                   `json:"strategy"`
	Timezone    string                   `json:"timezone"`
	StartAt     *string                  `json:"startAt"`
	EndAt       *string                  `json:"endAt"`
	Cron        *string                  `json:"cron"`
	Recurrence  interface{}              `json:"recurrence"`
	Targets     []map[string]interface{} `json:"targets"`
	Active      bool                     `json:"active"`
	CreatedAt   *string                  `json:"createdAt"`
	UpdatedAt   *string                  `json:"updatedAt"`
}

func New(cfg config.Config, auth Authenticator, notifier Notifier) *Service {
	service := &Service{
		cfg:        cfg,
		store:      database.New(cfg),
		auth:       auth,
		notifier:   notifier,
		timers:     map[int64]*time.Timer{},
		checkLocks: map[int64]*sync.Mutex{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		_ = service.migrateLegacyMonitors(ctx, db)
		db.Close()
	}
	go service.startHeartbeatCleanupLoop()
	_ = service.RestartAll(context.Background())
	return service
}

func (s *Service) SetHeartbeatBroadcaster(fn func(monitorID int64, beat map[string]interface{})) {
	s.heartbeatBroadcaster = fn
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
}

func (s *Service) startHeartbeatCleanupLoop() {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	for {
		<-timer.C
		if s.isStopped() {
			return
		}
		s.cleanupOldHeartbeats(context.Background())
		timer.Reset(time.Hour)
	}
}

func (s *Service) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Service) cleanupOldHeartbeats(ctx context.Context) {
	retentionDays := resolveHeartbeatRetentionDays()
	if retentionDays <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	// 分批删除：单批上限内循环，避免月积累的大量心跳单次 DELETE 长时间持锁。
	// LIMIT 置于子查询（modernc 驱动不支持 DELETE 主语句 LIMIT）。
	const batchSize = 2000
	for {
		result, err := db.ExecContext(ctx, `DELETE FROM uptime_heartbeats WHERE rowid IN (SELECT rowid FROM uptime_heartbeats WHERE created_at < datetime('now', '-' || ? || ' days') LIMIT ?)`, retentionDays, batchSize)
		if err != nil {
			return
		}
		deleted, err := result.RowsAffected()
		if err != nil || deleted < batchSize {
			return
		}
	}
}

func resolveHeartbeatRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv("API_MONITOR_UPTIME_HEARTBEAT_RETENTION_DAYS"))
	if raw == "" {
		return defaultHeartbeatRetentionDays
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultHeartbeatRetentionDays
	}
	if value < minHeartbeatRetentionDays {
		return minHeartbeatRetentionDays
	}
	return value
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/uptime")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	if s.isPublicRoute(parts, r.Method) {
		s.servePublic(w, r, parts)
		return
	}
	if !s.requireAuth(w, r) {
		return
	}

	switch {
	case len(parts) == 1 && parts[0] == "summary" && r.Method == http.MethodGet:
		s.summary(w, r)
	case len(parts) == 1 && parts[0] == "status-pages":
		s.statusPages(w, r)
	case len(parts) == 2 && parts[0] == "status-pages":
		s.statusPageByID(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "maintenance":
		s.maintenance(w, r)
	case len(parts) == 2 && parts[0] == "maintenance":
		s.maintenanceByID(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "export" && r.Method == http.MethodGet:
		s.exportConfig(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "preview" && r.Method == http.MethodPost:
		s.importPreview(w, r)
	case len(parts) == 1 && parts[0] == "import" && r.Method == http.MethodPost:
		s.importConfig(w, r)
	case len(parts) == 1 && parts[0] == "monitors":
		s.monitors(w, r)
	case len(parts) == 2 && parts[0] == "monitors" && parts[1] == "batch-delete" && r.Method == http.MethodPost:
		s.batchDelete(w, r)
	case len(parts) == 1 && parts[0] == "batch" && r.Method == http.MethodPost:
		s.batchAction(w, r)
	case len(parts) == 2 && parts[0] == "monitors":
		s.monitorByID(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "history" && r.Method == http.MethodGet:
		s.monitorHistory(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "clone" && r.Method == http.MethodPost:
		s.cloneMonitor(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "test" && r.Method == http.MethodPost:
		s.testMonitor(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "check-now" && r.Method == http.MethodPost:
		s.checkNow(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "toggle" && r.Method == http.MethodPost:
		s.toggleMonitor(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "uptime" && r.Method == http.MethodGet:
		s.monitorUptime(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "incidents" && r.Method == http.MethodGet:
		s.monitorIncidents(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "state" && r.Method == http.MethodGet:
		s.monitorState(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "monitors" && parts[2] == "ssl" && r.Method == http.MethodGet:
		s.monitorSSL(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "ssl-status" && r.Method == http.MethodGet:
		s.sslStatus(w, r)
	default:
		response.Error(w, http.StatusNotFound, "uptime route not implemented")
	}
}

func (s *Service) isPublicRoute(parts []string, method string) bool {
	return (len(parts) == 2 && parts[0] == "push" && method == http.MethodPost) ||
		(len(parts) == 3 && parts[0] == "public" && parts[1] == "status-pages" && method == http.MethodGet) ||
		(len(parts) == 2 && parts[0] == "public" && parts[1] == "status-page-by-domain" && method == http.MethodGet) ||
		(len(parts) == 3 && parts[0] == "public" && parts[1] == "badge" && method == http.MethodGet)
}

func (s *Service) servePublic(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case len(parts) == 2 && parts[0] == "push":
		s.recordPush(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "status-pages":
		s.publicStatusPage(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "public" && parts[1] == "status-page-by-domain":
		s.publicStatusPageByDomain(w, r)
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "badge":
		s.publicBadge(w, r, parts[2])
	default:
		response.Error(w, http.StatusNotFound, "uptime public route not implemented")
	}
}

func (s *Service) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	ok, err := s.auth.IsAuthenticated(r.Context(), r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "请先登录"})
		return false
	}
	return true
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
