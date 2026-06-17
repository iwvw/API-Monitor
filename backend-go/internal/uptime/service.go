package uptime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
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
	auth                 Authenticator
	notifier             Notifier
	heartbeatBroadcaster func(monitorID int64, beat map[string]interface{})

	mu      sync.Mutex
	timers  map[int64]*time.Timer
	stopped bool
}

type probeResult struct {
	OK         bool                   `json:"ok"`
	Status     string                 `json:"status"`
	LatencyMS  int64                  `json:"latencyMs"`
	Message    string                 `json:"message"`
	StatusCode *int                   `json:"statusCode,omitempty"`
	ErrorCode  string                 `json:"errorCode,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
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
		cfg:      cfg,
		store:    database.New(cfg),
		auth:     auth,
		notifier: notifier,
		timers:   map[int64]*time.Timer{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := service.open(ctx); err == nil {
		_ = service.migrateLegacyMonitors(ctx, db)
		db.Close()
	}
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
	default:
		response.Error(w, http.StatusNotFound, "uptime route not implemented")
	}
}

func (s *Service) isPublicRoute(parts []string, method string) bool {
	return (len(parts) == 2 && parts[0] == "push" && method == http.MethodPost) ||
		(len(parts) == 3 && parts[0] == "public" && parts[1] == "status-pages" && method == http.MethodGet) ||
		(len(parts) == 3 && parts[0] == "public" && parts[1] == "badge" && method == http.MethodGet)
}

func (s *Service) servePublic(w http.ResponseWriter, r *http.Request, parts []string) {
	switch {
	case len(parts) == 2 && parts[0] == "push":
		s.recordPush(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[1] == "status-pages":
		s.publicStatusPage(w, r, parts[2])
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
	if err := ensureSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS uptime_monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'http',
			url TEXT,
			hostname TEXT,
			port INTEGER,
			interval INTEGER DEFAULT 60,
			timeout INTEGER DEFAULT 30,
			confirm_count INTEGER DEFAULT 3,
			active INTEGER DEFAULT 1,
			method TEXT DEFAULT 'GET',
			headers TEXT,
			body TEXT,
			ignore_tls INTEGER DEFAULT 0,
			accepted_status_codes TEXT,
			keyword TEXT,
			dns_resolve_type TEXT DEFAULT 'A',
			dns_resolve_server TEXT,
			retry_interval INTEGER DEFAULT 30,
			resend_interval INTEGER DEFAULT 0,
			up_confirm_count INTEGER,
			down_confirm_count INTEGER,
			config_json TEXT,
			auth_json_encrypted TEXT,
			push_token TEXT UNIQUE,
			push_grace_seconds INTEGER DEFAULT 120,
			last_checked_at DATETIME,
			next_check_at DATETIME,
			expiry_notification INTEGER DEFAULT 7,
			notification_channels TEXT DEFAULT '[]',
			tags TEXT DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_heartbeats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			status INTEGER NOT NULL,
			state TEXT,
			ping INTEGER DEFAULT 0,
			duration_ms INTEGER,
			status_code INTEGER,
			error_code TEXT,
			details_json TEXT,
			maintenance INTEGER DEFAULT 0,
			probe_id TEXT,
			msg TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_incidents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			started_at DATETIME NOT NULL,
			resolved_at DATETIME,
			duration_ms INTEGER,
			cause TEXT,
			status TEXT DEFAULT 'open',
			severity TEXT,
			acknowledged_at DATETIME,
			acknowledged_by TEXT,
			maintenance_id INTEGER,
			resolved_reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_monitor_states (
			monitor_id INTEGER PRIMARY KEY,
			state TEXT DEFAULT 'up',
			fail_count INTEGER DEFAULT 0,
			recover_count INTEGER DEFAULT 0,
			active_incident_id INTEGER,
			last_transition_at DATETIME,
			last_error TEXT,
			last_ping INTEGER DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			domain TEXT,
			title TEXT NOT NULL,
			description TEXT,
			theme TEXT DEFAULT 'auto',
			public INTEGER DEFAULT 1,
			cache_seconds INTEGER DEFAULT 300,
			config_json TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_page_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status_page_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			order_index INTEGER DEFAULT 0,
			FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_status_page_monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status_page_id INTEGER NOT NULL,
			group_id INTEGER,
			monitor_id INTEGER NOT NULL,
			order_index INTEGER DEFAULT 0,
			display_name TEXT,
			FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE,
			FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_maintenance_windows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			description TEXT,
			strategy TEXT DEFAULT 'manual',
			timezone TEXT DEFAULT 'UTC',
			start_at DATETIME,
			end_at DATETIME,
			cron TEXT,
			recurrence_json TEXT,
			active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_maintenance_targets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			maintenance_id INTEGER NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT,
			FOREIGN KEY (maintenance_id) REFERENCES uptime_maintenance_windows(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_heartbeats_monitor_time ON uptime_heartbeats(monitor_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_heartbeats_created ON uptime_heartbeats(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_monitor ON uptime_incidents(monitor_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_states_state ON uptime_monitor_states(state, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_status_pages_slug ON uptime_status_pages(slug, public)`,
		`CREATE INDEX IF NOT EXISTS idx_uptime_maintenance_active ON uptime_maintenance_windows(active, start_at, end_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure uptime schema: %w", err)
		}
	}
	columns := []struct {
		table string
		name  string
		sql   string
	}{
		{"uptime_monitors", "created_at", "ALTER TABLE uptime_monitors ADD COLUMN created_at DATETIME DEFAULT CURRENT_TIMESTAMP"},
		{"uptime_monitors", "updated_at", "ALTER TABLE uptime_monitors ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP"},
		{"uptime_monitors", "keyword", "ALTER TABLE uptime_monitors ADD COLUMN keyword TEXT"},
		{"uptime_monitors", "dns_resolve_type", "ALTER TABLE uptime_monitors ADD COLUMN dns_resolve_type TEXT DEFAULT 'A'"},
		{"uptime_monitors", "dns_resolve_server", "ALTER TABLE uptime_monitors ADD COLUMN dns_resolve_server TEXT"},
		{"uptime_monitors", "retry_interval", "ALTER TABLE uptime_monitors ADD COLUMN retry_interval INTEGER DEFAULT 30"},
		{"uptime_monitors", "resend_interval", "ALTER TABLE uptime_monitors ADD COLUMN resend_interval INTEGER DEFAULT 0"},
		{"uptime_monitors", "up_confirm_count", "ALTER TABLE uptime_monitors ADD COLUMN up_confirm_count INTEGER"},
		{"uptime_monitors", "down_confirm_count", "ALTER TABLE uptime_monitors ADD COLUMN down_confirm_count INTEGER"},
		{"uptime_monitors", "config_json", "ALTER TABLE uptime_monitors ADD COLUMN config_json TEXT"},
		{"uptime_monitors", "auth_json_encrypted", "ALTER TABLE uptime_monitors ADD COLUMN auth_json_encrypted TEXT"},
		{"uptime_monitors", "push_token", "ALTER TABLE uptime_monitors ADD COLUMN push_token TEXT"},
		{"uptime_monitors", "push_grace_seconds", "ALTER TABLE uptime_monitors ADD COLUMN push_grace_seconds INTEGER DEFAULT 120"},
		{"uptime_monitors", "last_checked_at", "ALTER TABLE uptime_monitors ADD COLUMN last_checked_at DATETIME"},
		{"uptime_monitors", "next_check_at", "ALTER TABLE uptime_monitors ADD COLUMN next_check_at DATETIME"},
		{"uptime_heartbeats", "state", "ALTER TABLE uptime_heartbeats ADD COLUMN state TEXT"},
		{"uptime_heartbeats", "duration_ms", "ALTER TABLE uptime_heartbeats ADD COLUMN duration_ms INTEGER"},
		{"uptime_heartbeats", "status_code", "ALTER TABLE uptime_heartbeats ADD COLUMN status_code INTEGER"},
		{"uptime_heartbeats", "error_code", "ALTER TABLE uptime_heartbeats ADD COLUMN error_code TEXT"},
		{"uptime_heartbeats", "details_json", "ALTER TABLE uptime_heartbeats ADD COLUMN details_json TEXT"},
		{"uptime_heartbeats", "maintenance", "ALTER TABLE uptime_heartbeats ADD COLUMN maintenance INTEGER DEFAULT 0"},
		{"uptime_heartbeats", "probe_id", "ALTER TABLE uptime_heartbeats ADD COLUMN probe_id TEXT"},
		{"uptime_incidents", "status", "ALTER TABLE uptime_incidents ADD COLUMN status TEXT DEFAULT 'open'"},
		{"uptime_incidents", "severity", "ALTER TABLE uptime_incidents ADD COLUMN severity TEXT"},
		{"uptime_incidents", "acknowledged_at", "ALTER TABLE uptime_incidents ADD COLUMN acknowledged_at DATETIME"},
		{"uptime_incidents", "acknowledged_by", "ALTER TABLE uptime_incidents ADD COLUMN acknowledged_by TEXT"},
		{"uptime_incidents", "maintenance_id", "ALTER TABLE uptime_incidents ADD COLUMN maintenance_id INTEGER"},
		{"uptime_incidents", "resolved_reason", "ALTER TABLE uptime_incidents ADD COLUMN resolved_reason TEXT"},
	}
	for _, column := range columns {
		exists, err := hasColumn(ctx, db, column.table, column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add %s.%s: %w", column.table, column.name, err)
			}
		}
	}
	return nil
}

func hasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
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
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Service) migrateLegacyMonitors(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM uptime_monitors`).Scan(&count); err != nil || count > 0 {
		return err
	}
	var raw sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = 'uptime_monitors_json'`).Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	var monitors []map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &monitors); err != nil {
		return nil
	}
	for _, monitor := range monitors {
		if _, err := s.createMonitor(ctx, db, monitor); err != nil {
			continue
		}
	}
	return nil
}

func (s *Service) RestartAll(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	monitors, err := loadMonitors(ctx, db, `SELECT * FROM uptime_monitors WHERE active = 1`)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
	s.stopped = false
	s.mu.Unlock()
	for _, monitor := range monitors {
		s.startMonitor(monitor)
	}
	return nil
}

func (s *Service) startMonitor(monitor map[string]interface{}) {
	id := int64Value(monitor["id"], 0)
	if id <= 0 || !boolValue(monitor["active"], true) {
		return
	}
	interval := intValue(firstNonNil(monitor["interval"], defaultIntervalSeconds), defaultIntervalSeconds)
	if interval < 5 {
		interval = defaultIntervalSeconds
	}
	s.stopMonitor(id)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	timer := time.AfterFunc(2*time.Second, func() {
		s.runScheduledCheck(id)
	})
	s.timers[id] = timer
	s.mu.Unlock()
}

func (s *Service) runScheduledCheck(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	monitor, ok, err := loadMonitor(ctx, db, id)
	if err == nil && ok && boolValue(monitor["active"], true) {
		_, _ = s.check(ctx, db, monitor)
	}
	db.Close()
	if err != nil || !ok || !boolValue(monitor["active"], true) {
		s.stopMonitor(id)
		return
	}
	interval := intValue(firstNonNil(monitor["interval"], defaultIntervalSeconds), defaultIntervalSeconds)
	if interval < 5 {
		interval = defaultIntervalSeconds
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	timer := time.AfterFunc(time.Duration(interval)*time.Second, func() {
		s.runScheduledCheck(id)
	})
	s.timers[id] = timer
	s.mu.Unlock()
}

func (s *Service) stopMonitor(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if timer, ok := s.timers[id]; ok {
		timer.Stop()
		delete(s.timers, id)
	}
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitors, err := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats := map[string]int{"total": len(monitors), "up": 0, "down": 0, "pending": 0, "paused": 0, "unknown": 0}
	for _, monitor := range monitors {
		if !boolValue(monitor["active"], true) {
			stats["paused"]++
			continue
		}
		state, _ := loadState(r.Context(), db, int64Value(monitor["id"], 0))
		switch stateText(state["state"]) {
		case stateUp:
			stats["up"]++
		case stateDown:
			stats["down"]++
		case statePendingDown, statePendingUp:
			stats["pending"]++
		default:
			stats["unknown"]++
		}
	}
	response.OK(w, stats)
}

func (s *Service) monitors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitors, err := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors ORDER BY created_at DESC`)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, monitor := range monitors {
			last, _ := getLastHeartbeat(r.Context(), db, int64Value(monitor["id"], 0))
			monitor["lastHeartbeat"] = last
		}
		response.JSON(w, http.StatusOK, monitors)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitor, err := s.createMonitor(r.Context(), db, payload)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if boolValue(monitor["active"], true) {
			s.startMonitor(monitor)
		}
		response.JSON(w, http.StatusOK, monitor)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) monitorByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitor, ok, err := s.updateMonitor(r.Context(), db, id, payload)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if !ok {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
			return
		}
		if boolValue(monitor["active"], true) {
			s.startMonitor(monitor)
		} else {
			s.stopMonitor(id)
		}
		response.JSON(w, http.StatusOK, monitor)
	case http.MethodDelete:
		s.stopMonitor(id)
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed, _ := result.RowsAffected()
		if changed == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) createMonitor(ctx context.Context, db *sql.DB, data map[string]interface{}) (map[string]interface{}, error) {
	name := strings.TrimSpace(stringValue(data["name"], ""))
	if name == "" {
		return nil, errors.New("Name is required")
	}
	typ := stringValue(data["type"], "http")
	urlValue := nullableString(data["url"])
	hostnameValue := nullableString(data["hostname"])
	portValue := nullableInt(data["port"])
	var existingID int64
	err := db.QueryRowContext(ctx, `
		SELECT id FROM uptime_monitors
		WHERE name = ? AND type = ? AND COALESCE(url, '') = ? AND COALESCE(hostname, '') = ? AND COALESCE(port, 0) = ?
	`, name, typ, stringPtrValue(urlValue), stringPtrValue(hostnameValue), intPtrValue(portValue)).Scan(&existingID)
	if err == nil && existingID > 0 {
		monitor, _, loadErr := loadMonitor(ctx, db, existingID)
		return monitor, loadErr
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	configMap := normalizeMonitorConfig(data, nil)
	configRaw := jsonOrNull(configMap)
	notificationChannels := jsonOrDefault(data["notificationChannels"], "[]")
	tags := jsonOrDefault(data["tags"], "[]")
	pushToken := stringValue(firstNonNil(data["pushToken"], data["push_token"]), "")
	if typ == "push" && pushToken == "" {
		pushToken = generateToken()
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_monitors (
			name, type, url, hostname, port, interval, timeout,
			confirm_count, active, method, headers, body, ignore_tls,
			accepted_status_codes, keyword, dns_resolve_type, dns_resolve_server,
			retry_interval, resend_interval, up_confirm_count, down_confirm_count,
			config_json, auth_json_encrypted, push_token, push_grace_seconds,
			expiry_notification, notification_channels, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		name,
		typ,
		urlValue,
		hostnameValue,
		portValue,
		intValue(data["interval"], defaultIntervalSeconds),
		intValue(data["timeout"], defaultTimeoutSeconds),
		intValue(firstNonNil(data["retries"], data["confirmCount"], data["confirm_count"]), defaultConfirmCount),
		boolIntValue(data["active"], true),
		stringValue(data["method"], "GET"),
		nullableString(data["headers"]),
		nullableString(data["body"]),
		boolIntValue(firstNonNil(data["ignoreTls"], data["ignore_tls"]), false),
		nullableString(data["accepted_status_codes"]),
		nullableString(data["keyword"]),
		stringValue(firstNonNil(data["dns_resolve_type"], data["dnsResolveType"]), "A"),
		nullableString(firstNonNil(data["dns_resolve_server"], data["dnsResolveServer"])),
		intValue(firstNonNil(data["retryInterval"], data["retry_interval"]), 30),
		intValue(firstNonNil(data["resendInterval"], data["resend_interval"]), 0),
		nullableInt(firstNonNil(data["upConfirmCount"], data["up_confirm_count"])),
		nullableInt(firstNonNil(data["downConfirmCount"], data["down_confirm_count"])),
		configRaw,
		nullableString(data["auth_json_encrypted"]),
		nullableString(pushToken),
		intValue(firstNonNil(data["pushGraceSeconds"], data["push_grace_seconds"], configMap["graceSeconds"]), 120),
		intValue(data["expiryNotification"], 7),
		notificationChannels,
		tags,
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	monitor, _, err := loadMonitor(ctx, db, id)
	return monitor, err
}

func (s *Service) updateMonitor(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (map[string]interface{}, bool, error) {
	existing, ok, err := loadMonitor(ctx, db, id)
	if err != nil || !ok {
		return nil, ok, err
	}
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	stringFields := map[string]string{
		"name": "name", "type": "type", "url": "url", "hostname": "hostname", "method": "method",
		"headers": "headers", "body": "body", "accepted_status_codes": "accepted_status_codes",
		"keyword": "keyword", "dns_resolve_type": "dns_resolve_type", "dnsResolveType": "dns_resolve_type",
		"dns_resolve_server": "dns_resolve_server", "dnsResolveServer": "dns_resolve_server",
		"pushToken": "push_token", "push_token": "push_token", "auth_json_encrypted": "auth_json_encrypted",
	}
	for key, column := range stringFields {
		if value, exists := data[key]; exists {
			add(column, nullableString(value))
		}
	}
	intFields := map[string]string{
		"port": "port", "interval": "interval", "timeout": "timeout", "retryInterval": "retry_interval",
		"retry_interval": "retry_interval", "resendInterval": "resend_interval", "resend_interval": "resend_interval",
		"upConfirmCount": "up_confirm_count", "up_confirm_count": "up_confirm_count",
		"downConfirmCount": "down_confirm_count", "down_confirm_count": "down_confirm_count",
		"expiryNotification": "expiry_notification", "pushGraceSeconds": "push_grace_seconds",
		"push_grace_seconds": "push_grace_seconds",
	}
	for key, column := range intFields {
		if value, exists := data[key]; exists {
			add(column, nullableInt(value))
		}
	}
	if value, exists := data["active"]; exists {
		add("active", boolIntValue(value, true))
	}
	if value, exists := firstExisting(data, "ignoreTls", "ignore_tls"); exists {
		add("ignore_tls", boolIntValue(value, false))
	}
	if value, exists := firstExisting(data, "retries", "confirmCount", "confirm_count"); exists {
		add("confirm_count", intValue(value, defaultConfirmCount))
	}
	if value, exists := data["notificationChannels"]; exists {
		add("notification_channels", jsonOrDefault(value, "[]"))
	}
	if value, exists := data["tags"]; exists {
		add("tags", jsonOrDefault(value, "[]"))
	}
	if shouldUpdateConfig(data) {
		existingConfig := objectValue(existing["config"])
		add("config_json", jsonOrNull(normalizeMonitorConfig(data, existingConfig)))
	}
	if stringValue(firstNonNil(data["type"], existing["type"]), "http") == "push" &&
		stringValue(firstNonNil(data["pushToken"], data["push_token"], existing["pushToken"]), "") == "" {
		add("push_token", generateToken())
	}
	if len(fields) == 0 {
		return existing, true, nil
	}
	fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
	values = append(values, id)
	if _, err := db.ExecContext(ctx, `UPDATE uptime_monitors SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
		return nil, true, err
	}
	monitor, ok, err := loadMonitor(ctx, db, id)
	return monitor, ok, err
}

func loadMonitor(ctx context.Context, db *sql.DB, id int64) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_monitors WHERE id = ?`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		return nil, false, err
	}
	return normalizeMonitor(row), true, rows.Err()
}

func loadMonitors(ctx context.Context, db *sql.DB, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monitors := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, normalizeMonitor(row))
	}
	return monitors, rows.Err()
}

func normalizeMonitor(row map[string]interface{}) map[string]interface{} {
	out := copyMap(row)
	confirm := intValue(row["confirm_count"], defaultConfirmCount)
	config := parseJSONMap(row["config_json"])
	out["active"] = boolValue(row["active"], true)
	out["ignoreTls"] = boolValue(row["ignore_tls"], false)
	out["confirmCount"] = confirm
	out["retries"] = confirm
	out["keyword"] = stringValue(row["keyword"], "")
	out["dns_resolve_type"] = stringValue(row["dns_resolve_type"], "A")
	out["dns_resolve_server"] = stringValue(row["dns_resolve_server"], "")
	out["retryInterval"] = intValue(row["retry_interval"], 30)
	out["resendInterval"] = intValue(row["resend_interval"], 0)
	out["upConfirmCount"] = intValue(row["up_confirm_count"], confirm)
	out["downConfirmCount"] = intValue(row["down_confirm_count"], confirm)
	out["config"] = config
	out["pushToken"] = stringValue(row["push_token"], "")
	out["pushGraceSeconds"] = intValue(row["push_grace_seconds"], 120)
	out["expiryNotification"] = intValue(row["expiry_notification"], 7)
	out["notificationChannels"] = parseJSONArray(row["notification_channels"])
	out["tags"] = parseJSONArray(row["tags"])
	out["jsonQueryPath"] = stringValue(config["jsonQueryPath"], "")
	out["jsonQueryOperator"] = stringValue(config["jsonQueryOperator"], "equals")
	out["jsonExpectedValue"] = stringValue(config["jsonExpectedValue"], "")
	return out
}

func (s *Service) monitorHistory(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	limit := intValue(r.URL.Query().Get("limit"), 60)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	history, err := getHistory(r.Context(), db, id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, history)
}

func getHistory(ctx context.Context, db *sql.DB, monitorID int64, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 500 {
		limit = 60
	}
	rows, err := db.QueryContext(ctx, `
		SELECT status, ping, msg, created_at as time
		FROM uptime_heartbeats
		WHERE monitor_id = ?
		ORDER BY created_at DESC LIMIT ?
	`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		normalizeHeartbeatTime(row)
		items = append(items, row)
	}
	return items, rows.Err()
}

func getLastHeartbeat(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT status, ping, msg, created_at as time
		FROM uptime_heartbeats
		WHERE monitor_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		return nil, err
	}
	normalizeHeartbeatTime(row)
	return row, nil
}

func normalizeHeartbeatTime(row map[string]interface{}) {
	raw := stringValue(firstNonNil(row["time"], row["created_at"]), "")
	if raw == "" {
		return
	}
	parsed := parseTimeFallback(raw, time.Time{})
	if parsed.IsZero() {
		return
	}
	row["time"] = parsed.UTC().Format(time.RFC3339Nano)
}

func saveHeartbeat(ctx context.Context, db *sql.DB, monitorID int64, beat map[string]interface{}) error {
	details := jsonOrNull(beat["details"])
	_, err := db.ExecContext(ctx, `
		INSERT INTO uptime_heartbeats (
			monitor_id, status, state, ping, duration_ms, status_code,
			error_code, details_json, maintenance, probe_id, msg, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		monitorID,
		intValue(beat["status"], 0),
		stringValue(beat["state"], stateDown),
		intValue(beat["ping"], 0),
		intValue(beat["durationMs"], intValue(beat["ping"], 0)),
		nullableInt(beat["statusCode"]),
		nullableString(beat["errorCode"]),
		details,
		boolIntValue(beat["maintenance"], false),
		nullableString(beat["probeId"]),
		stringValue(beat["msg"], ""),
		stringValue(beat["time"], time.Now().UTC().Format(time.RFC3339)),
	)
	return err
}

func (s *Service) cloneMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	payload, _ := readObject(r)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	clone := copyMap(monitor)
	delete(clone, "id")
	delete(clone, "pushToken")
	delete(clone, "push_token")
	clone["name"] = stringValue(payload["name"], stringValue(monitor["name"], "Monitor")+" Copy")
	clone["active"] = false
	created, err := s.createMonitor(r.Context(), db, clone)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, created)
}

func (s *Service) testMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	payload, _ := readObject(r)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor := map[string]interface{}{}
	if id > 0 {
		if stored, ok, err := loadMonitor(r.Context(), db, id); err == nil && ok {
			monitor = stored
		}
	}
	for key, value := range payload {
		monitor[key] = value
	}
	if len(monitor) == 0 || stringValue(monitor["type"], "") == "" {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	started := time.Now()
	result, err := s.probe(r.Context(), db, normalizeMonitor(monitor))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	payloadOut := map[string]interface{}{
		"ok":         result.OK,
		"status":     result.Status,
		"latencyMs":  result.LatencyMS,
		"message":    result.Message,
		"durationMs": time.Since(started).Milliseconds(),
	}
	if result.StatusCode != nil {
		payloadOut["statusCode"] = *result.StatusCode
	}
	if result.Details != nil {
		payloadOut["details"] = result.Details
	}
	response.OK(w, payloadOut)
}

func (s *Service) checkNow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	beat, err := s.check(r.Context(), db, monitor)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.OK(w, beat)
}

func (s *Service) toggleMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	active := !boolValue(monitor["active"], true)
	if _, err := db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, boolInt(active), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _, _ := loadMonitor(r.Context(), db, id)
	if active {
		s.startMonitor(updated)
	} else {
		s.stopMonitor(id)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "active": active})
}

func (s *Service) batchDelete(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := int64Slice(payload["ids"])
	if len(ids) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "IDs array is required"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	count := 0
	for _, id := range ids {
		s.stopMonitor(id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM uptime_heartbeats WHERE monitor_id = ?`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM uptime_incidents WHERE monitor_id = ?`, id)
		result, err := tx.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed, _ := result.RowsAffected()
		if changed > 0 {
			count++
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": count})
}

func (s *Service) batchAction(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	action := stringValue(payload["action"], "")
	ids := int64Slice(payload["ids"])
	if len(ids) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "IDs array is required"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	count := 0
	for _, id := range ids {
		monitor, ok, err := loadMonitor(r.Context(), db, id)
		if err != nil || !ok {
			continue
		}
		switch action {
		case "pause":
			_, _ = db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
			s.stopMonitor(id)
			count++
		case "resume":
			_, _ = db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
			monitor["active"] = true
			s.startMonitor(monitor)
			count++
		case "delete":
			s.stopMonitor(id)
			result, _ := db.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
			changed, _ := result.RowsAffected()
			if changed > 0 {
				count++
			}
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": count})
}

func (s *Service) monitorUptime(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	days := intValue(r.URL.Query().Get("days"), 1)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	uptime, err := calculateUptime(r.Context(), db, id, days)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"monitorId": id, "days": days, "uptime": uptime})
}

func calculateUptime(ctx context.Context, db *sql.DB, monitorID int64, days int) (string, error) {
	if days <= 0 {
		days = 1
	}
	totalMs := float64(days) * 24 * 60 * 60 * 1000
	rangeStart := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.QueryContext(ctx, `
		SELECT started_at, COALESCE(resolved_at, datetime('now')) as resolved_at
		FROM uptime_incidents
		WHERE monitor_id = ? AND (resolved_at > ? OR resolved_at IS NULL)
	`, monitorID, rangeStart.UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	downMs := 0.0
	for rows.Next() {
		var started, resolved string
		if err := rows.Scan(&started, &resolved); err != nil {
			return "", err
		}
		startTime := parseTimeFallback(started, rangeStart)
		endTime := parseTimeFallback(resolved, time.Now())
		if startTime.Before(rangeStart) {
			startTime = rangeStart
		}
		if endTime.After(time.Now()) {
			endTime = time.Now()
		}
		if endTime.After(startTime) {
			downMs += float64(endTime.Sub(startTime).Milliseconds())
		}
	}
	value := (1 - downMs/totalMs) * 100
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%.3f", value), rows.Err()
}

func (s *Service) monitorIncidents(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	limit := intValue(r.URL.Query().Get("limit"), 20)
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `SELECT * FROM uptime_incidents WHERE monitor_id = ? ORDER BY started_at DESC LIMIT ?`, id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items, err := scanAll(rows)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (s *Service) monitorState(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	state, _ := loadState(r.Context(), db, id)
	response.JSON(w, http.StatusOK, state)
}

func (s *Service) check(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (map[string]interface{}, error) {
	result, err := s.probe(ctx, db, monitor)
	if err != nil {
		result = probeResult{
			OK:        false,
			Status:    stateDown,
			LatencyMS: 0,
			Message:   err.Error(),
			ErrorCode: "CHECK_FAILED",
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	beat := map[string]interface{}{
		"id":         time.Now().UnixMilli(),
		"status":     map[bool]int{true: 1, false: 0}[result.OK],
		"state":      stringFallback(result.Status, map[bool]string{true: stateUp, false: stateDown}[result.OK]),
		"msg":        stringFallback(result.Message, map[bool]string{true: "OK", false: "Failed"}[result.OK]),
		"ping":       map[bool]int64{true: result.LatencyMS, false: 0}[result.OK],
		"durationMs": result.LatencyMS,
		"time":       now,
	}
	if result.StatusCode != nil {
		beat["statusCode"] = *result.StatusCode
	}
	if result.ErrorCode != "" {
		beat["errorCode"] = result.ErrorCode
	}
	if result.Details != nil {
		beat["details"] = result.Details
	}
	maintenance, _ := activeMaintenanceForMonitor(ctx, db, int64Value(monitor["id"], 0))
	beat["maintenance"] = maintenance != nil
	if err := saveHeartbeat(ctx, db, int64Value(monitor["id"], 0), beat); err != nil {
		return nil, err
	}
	s.broadcastHeartbeat(int64Value(monitor["id"], 0), beat)
	if err := s.processState(ctx, db, monitor, result, beat, maintenance != nil); err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `UPDATE uptime_monitors SET last_checked_at = ?, next_check_at = datetime(?, '+' || interval || ' seconds') WHERE id = ?`, now, now, int64Value(monitor["id"], 0))
	return beat, nil
}

func (s *Service) processState(ctx context.Context, db *sql.DB, monitor map[string]interface{}, result probeResult, beat map[string]interface{}, inMaintenance bool) error {
	monitorID := int64Value(monitor["id"], 0)
	previous, _ := loadState(ctx, db, monitorID)
	next, action := transitionState(previous, monitor, result, inMaintenance)
	if action == "open" {
		incidentID, err := createIncident(ctx, db, monitorID, stringValue(beat["msg"], "Unknown"))
		if err == nil {
			next["activeIncidentId"] = incidentID
			next["active_incident_id"] = incidentID
		}
		s.notify(ctx, "down", monitor, beat, 0)
	}
	if action == "resolve" {
		duration := int64(0)
		if incident, _ := getOpenIncident(ctx, db, monitorID); incident != nil {
			started := parseTimeFallback(stringValue(incident["started_at"], ""), time.Now())
			duration = time.Since(started).Milliseconds()
		}
		_ = resolveIncident(ctx, db, monitorID, duration)
		next["activeIncidentId"] = nil
		next["active_incident_id"] = nil
		s.notify(ctx, "up", monitor, beat, duration)
	}
	return saveState(ctx, db, monitorID, next)
}

func transitionState(previous, monitor map[string]interface{}, result probeResult, inMaintenance bool) (map[string]interface{}, string) {
	now := time.Now().UTC().Format(time.RFC3339)
	state := stateText(previous["state"])
	if state == "" {
		state = stateUp
	}
	failCount := intValue(firstNonNil(previous["failCount"], previous["fail_count"]), 0)
	recoverCount := intValue(firstNonNil(previous["recoverCount"], previous["recover_count"]), 0)
	activeIncidentID := firstNonNil(previous["activeIncidentId"], previous["active_incident_id"])
	next := map[string]interface{}{
		"state":            state,
		"failCount":        failCount,
		"recoverCount":     recoverCount,
		"activeIncidentId": activeIncidentID,
		"lastTransitionAt": firstNonNil(previous["lastTransitionAt"], previous["last_transition_at"]),
		"lastError":        nil,
		"lastPing":         int64(0),
	}
	if !boolValue(monitor["active"], true) {
		next["state"] = statePaused
		next["lastTransitionAt"] = now
		return next, ""
	}
	if inMaintenance {
		next["state"] = stateMaintenance
		next["lastTransitionAt"] = now
		return next, ""
	}
	downConfirm := intValue(firstNonNil(monitor["downConfirmCount"], monitor["down_confirm_count"], monitor["confirmCount"], monitor["confirm_count"]), defaultConfirmCount)
	upConfirm := intValue(firstNonNil(monitor["upConfirmCount"], monitor["up_confirm_count"], monitor["confirmCount"], monitor["confirm_count"]), defaultConfirmCount)
	if downConfirm <= 0 {
		downConfirm = defaultConfirmCount
	}
	if upConfirm <= 0 {
		upConfirm = defaultConfirmCount
	}
	if result.OK {
		next["lastError"] = nil
		next["lastPing"] = result.LatencyMS
		if state == stateDown || state == statePendingUp || activeIncidentID != nil {
			recoverCount++
			next["state"] = statePendingUp
			next["recoverCount"] = recoverCount
			if recoverCount >= upConfirm {
				next["state"] = stateUp
				next["failCount"] = 0
				next["recoverCount"] = 0
				next["lastTransitionAt"] = now
				return next, "resolve"
			}
		} else {
			next["state"] = stateUp
			next["failCount"] = 0
			next["recoverCount"] = 0
		}
		return next, ""
	}
	next["lastError"] = result.Message
	next["lastPing"] = 0
	if state == stateUp || state == statePendingDown || state == stateUnknown || state == stateMaintenance || state == statePaused {
		if state == statePendingDown {
			failCount++
		} else {
			failCount = 1
		}
		next["state"] = statePendingDown
		next["failCount"] = failCount
		next["recoverCount"] = 0
		if failCount >= downConfirm {
			next["state"] = stateDown
			next["lastTransitionAt"] = now
			return next, "open"
		}
		return next, ""
	}
	next["state"] = stateDown
	next["recoverCount"] = 0
	return next, ""
}

func (s *Service) notify(ctx context.Context, eventType string, monitor, beat map[string]interface{}, durationMs int64) {
	if s.notifier == nil {
		return
	}
	data := map[string]interface{}{
		"monitorId":   int64Value(monitor["id"], 0),
		"monitorName": stringValue(monitor["name"], ""),
		"url":         monitorTarget(monitor),
		"type":        stringValue(monitor["type"], "http"),
	}
	if eventType == "down" {
		data["error"] = stringValue(beat["msg"], "")
	} else {
		data["ping"] = intValue(beat["ping"], 0)
		data["downDurationMs"] = durationMs
		data["downDuration"] = formatDuration(durationMs)
	}
	_ = s.notifier.Trigger(ctx, "uptime", eventType, data)
}

func createIncident(ctx context.Context, db *sql.DB, monitorID int64, cause string) (int64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_incidents (monitor_id, started_at, cause, status)
		VALUES (?, ?, ?, 'open')
	`, monitorID, time.Now().UTC().Format(time.RFC3339), cause)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func getOpenIncident(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT * FROM uptime_incidents
		WHERE monitor_id = ? AND resolved_at IS NULL
		ORDER BY started_at DESC LIMIT 1
	`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanMap(rows)
}

func resolveIncident(ctx context.Context, db *sql.DB, monitorID, durationMs int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE uptime_incidents
		SET resolved_at = ?, duration_ms = ?, status = 'resolved', resolved_reason = 'recovered'
		WHERE id = (
			SELECT id FROM uptime_incidents
			WHERE monitor_id = ? AND resolved_at IS NULL
			ORDER BY started_at DESC LIMIT 1
		)
	`, time.Now().UTC().Format(time.RFC3339), durationMs, monitorID)
	return err
}

func loadState(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT monitor_id as monitorId, state, fail_count as failCount, recover_count as recoverCount,
			active_incident_id as activeIncidentId, last_transition_at as lastTransitionAt,
			last_error as lastError, last_ping as lastPing, updated_at as updatedAt
		FROM uptime_monitor_states WHERE monitor_id = ?
	`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return map[string]interface{}{
			"monitorId": monitorID, "state": stateUp, "failCount": 0, "recoverCount": 0,
			"activeIncidentId": nil, "lastTransitionAt": nil, "lastError": nil, "lastPing": 0,
		}, rows.Err()
	}
	return scanMap(rows)
}

func saveState(ctx context.Context, db *sql.DB, monitorID int64, state map[string]interface{}) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO uptime_monitor_states (
			monitor_id, state, fail_count, recover_count, active_incident_id,
			last_transition_at, last_error, last_ping, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(monitor_id) DO UPDATE SET
			state = excluded.state,
			fail_count = excluded.fail_count,
			recover_count = excluded.recover_count,
			active_incident_id = excluded.active_incident_id,
			last_transition_at = excluded.last_transition_at,
			last_error = excluded.last_error,
			last_ping = excluded.last_ping,
			updated_at = CURRENT_TIMESTAMP
	`, monitorID, stateText(state["state"]), intValue(state["failCount"], 0), intValue(state["recoverCount"], 0),
		nullableInt(state["activeIncidentId"]), nullableString(state["lastTransitionAt"]), nullableString(state["lastError"]), intValue(state["lastPing"], 0))
	return err
}

func (s *Service) statusPages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		pages, err := listStatusPages(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, pages)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		page, err := createStatusPage(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, page)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) statusPageByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid status page id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		page, ok, err := updateStatusPage(r.Context(), db, id, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "Not found")
			return
		}
		response.OK(w, page)
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_status_pages WHERE id = ?`, id)
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
}

func listStatusPages(ctx context.Context, db *sql.DB) ([]statusPage, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	rawRows := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	pages := []statusPage{}
	for _, row := range rawRows {
		page, err := parseStatusPage(ctx, db, row)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

func createStatusPage(ctx context.Context, db *sql.DB, data map[string]interface{}) (statusPage, error) {
	slug := normalizeSlug(firstNonNil(data["slug"], data["title"]))
	title := stringValue(data["title"], slug)
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_status_pages (slug, domain, title, description, theme, public, cache_seconds, config_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, slug, nullableString(data["domain"]), title, stringValue(data["description"], ""), stringValue(data["theme"], "auto"),
		boolIntValue(data["public"], true), intValue(firstNonNil(data["cacheSeconds"], data["cache_seconds"]), 300), jsonOrNull(data["config"]))
	if err != nil {
		return statusPage{}, err
	}
	id, _ := result.LastInsertId()
	if err := replaceStatusPageMonitors(ctx, db, id, int64Slice(firstNonNil(data["monitorIds"], data["monitor_ids"]))); err != nil {
		return statusPage{}, err
	}
	page, _, err := getStatusPage(ctx, db, id)
	return page, err
}

func updateStatusPage(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (statusPage, bool, error) {
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	if value, ok := data["slug"]; ok {
		add("slug", normalizeSlug(value))
	}
	for key, column := range map[string]string{"domain": "domain", "title": "title", "description": "description", "theme": "theme"} {
		if value, ok := data[key]; ok {
			add(column, nullableString(value))
		}
	}
	if value, ok := firstExisting(data, "cacheSeconds", "cache_seconds"); ok {
		add("cache_seconds", intValue(value, 300))
	}
	if value, ok := data["public"]; ok {
		add("public", boolIntValue(value, true))
	}
	if value, ok := data["config"]; ok {
		add("config_json", jsonOrNull(value))
	}
	if len(fields) > 0 {
		fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
		values = append(values, id)
		if _, err := db.ExecContext(ctx, `UPDATE uptime_status_pages SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
			return statusPage{}, true, err
		}
	}
	if value, ok := firstExisting(data, "monitorIds", "monitor_ids"); ok {
		if err := replaceStatusPageMonitors(ctx, db, id, int64Slice(value)); err != nil {
			return statusPage{}, true, err
		}
	}
	return getStatusPage(ctx, db, id)
}

func getStatusPage(ctx context.Context, db *sql.DB, id int64) (statusPage, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages WHERE id = ?`, id)
	if err != nil {
		return statusPage{}, false, err
	}
	if !rows.Next() {
		rows.Close()
		return statusPage{}, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return statusPage{}, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return statusPage{}, false, err
	}
	rows.Close()
	page, err := parseStatusPage(ctx, db, row)
	return page, true, err
}

func parseStatusPage(ctx context.Context, db *sql.DB, row map[string]interface{}) (statusPage, error) {
	ids, err := statusPageMonitorIDs(ctx, db, int64Value(row["id"], 0))
	if err != nil {
		return statusPage{}, err
	}
	return statusPage{
		ID:           int64Value(row["id"], 0),
		Slug:         stringValue(row["slug"], ""),
		Domain:       stringPointer(row["domain"]),
		Title:        stringValue(row["title"], ""),
		Description:  stringValue(row["description"], ""),
		Theme:        stringValue(row["theme"], "auto"),
		Public:       boolValue(row["public"], true),
		CacheSeconds: intValue(row["cache_seconds"], 300),
		Config:       parseJSONMap(row["config_json"]),
		MonitorIDs:   ids,
		CreatedAt:    stringPointer(row["created_at"]),
		UpdatedAt:    stringPointer(row["updated_at"]),
	}, nil
}

func replaceStatusPageMonitors(ctx context.Context, db *sql.DB, pageID int64, ids []int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM uptime_status_page_monitors WHERE status_page_id = ?`, pageID); err != nil {
		return err
	}
	for index, id := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO uptime_status_page_monitors (status_page_id, monitor_id, order_index) VALUES (?, ?, ?)`, pageID, id, index); err != nil {
			return err
		}
	}
	return nil
}

func statusPageMonitorIDs(ctx context.Context, db *sql.DB, pageID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT monitor_id FROM uptime_status_page_monitors WHERE status_page_id = ? ORDER BY order_index ASC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) publicStatusPage(w http.ResponseWriter, r *http.Request, slug string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	page, ok, err := getPublicStatusPage(r.Context(), db, slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", intValue(page["cacheSeconds"], 300)))
	response.OK(w, page)
}

func getPublicStatusPage(ctx context.Context, db *sql.DB, slug string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages WHERE slug = ? AND public = 1`, normalizeSlug(slug))
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	page, err := parseStatusPage(ctx, db, row)
	if err != nil {
		return nil, false, err
	}
	out := structToMap(page)
	monitorRows, err := db.QueryContext(ctx, `
		SELECT m.id, COALESCE(spm.display_name, m.name) as name, m.type, m.url, m.hostname, m.port,
			s.state, s.last_error, s.last_ping, s.updated_at
		FROM uptime_status_page_monitors spm
		JOIN uptime_monitors m ON m.id = spm.monitor_id
		LEFT JOIN uptime_monitor_states s ON s.monitor_id = m.id
		WHERE spm.status_page_id = ?
		ORDER BY spm.order_index ASC, m.name ASC
	`, page.ID)
	if err != nil {
		return nil, false, err
	}
	rawMonitors := []map[string]interface{}{}
	for monitorRows.Next() {
		monitor, err := scanMap(monitorRows)
		if err != nil {
			monitorRows.Close()
			return nil, false, err
		}
		rawMonitors = append(rawMonitors, monitor)
	}
	if err := monitorRows.Err(); err != nil {
		monitorRows.Close()
		return nil, false, err
	}
	monitorRows.Close()
	monitors := []map[string]interface{}{}
	for _, monitor := range rawMonitors {
		target := stringValue(monitor["url"], "")
		if target == "" {
			target = strings.Trim(strings.Join([]string{stringValue(monitor["hostname"], ""), stringValue(monitor["port"], "")}, ":"), ":")
		}
		uptime, _ := calculateUptime(ctx, db, int64Value(monitor["id"], 0), 1)
		monitors = append(monitors, map[string]interface{}{
			"id":        monitor["id"],
			"name":      monitor["name"],
			"type":      monitor["type"],
			"target":    target,
			"state":     stringValue(monitor["state"], stateUnknown),
			"lastError": monitor["last_error"],
			"lastPing":  intValue(monitor["last_ping"], 0),
			"updatedAt": monitor["updated_at"],
			"uptime24h": uptime,
		})
	}
	out["monitors"] = monitors
	return out, true, nil
}

func (s *Service) publicBadge(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	state, _ := loadState(r.Context(), db, id)
	writeBadge(w, stringValue(monitor["name"], fmt.Sprintf("Monitor %d", id)), stringValue(state["state"], stateUnknown))
}

func writeBadge(w http.ResponseWriter, label, value string) {
	colorMap := map[string]string{
		stateUp: "#16a34a", stateDown: "#dc2626", statePendingDown: "#d97706",
		statePendingUp: "#d97706", stateMaintenance: "#2563eb", statePaused: "#64748b", stateUnknown: "#64748b",
	}
	color := colorMap[value]
	if color == "" {
		color = colorMap[stateUnknown]
	}
	label = escapeSVG(label)
	value = escapeSVG(value)
	labelWidth := clamp(len(label)*7+18, 80, 220)
	valueWidth := maxInt(len(value)*7+18, 58)
	width := labelWidth + valueWidth
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s"><linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient><rect rx="3" width="%d" height="20" fill="#555"/><rect rx="3" x="%d" width="%d" height="20" fill="%s"/><path fill="%s" d="M%d 0h4v20h-4z"/><rect rx="3" width="%d" height="20" fill="url(#s)"/><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,sans-serif" font-size="11"><text x="%d" y="15">%s</text><text x="%d" y="15">%s</text></g></svg>`,
		width, label, value, width, labelWidth, valueWidth, color, color, labelWidth, width, labelWidth/2, label, labelWidth+valueWidth/2, value)
}

func (s *Service) maintenance(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		items, err := listMaintenance(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, items)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		item, err := createMaintenance(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, item)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) maintenanceByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid maintenance id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		item, ok, err := updateMaintenance(r.Context(), db, id, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "Not found")
			return
		}
		response.OK(w, item)
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_maintenance_windows WHERE id = ?`, id)
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
}

func listMaintenance(ctx context.Context, db *sql.DB) ([]maintenanceWindow, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_maintenance_windows ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	rawRows := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	items := []maintenanceWindow{}
	for _, row := range rawRows {
		item, err := parseMaintenance(ctx, db, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func createMaintenance(ctx context.Context, db *sql.DB, data map[string]interface{}) (maintenanceWindow, error) {
	title := strings.TrimSpace(stringValue(data["title"], ""))
	if title == "" {
		return maintenanceWindow{}, errors.New("title is required")
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_maintenance_windows (
			title, description, strategy, timezone, start_at, end_at, cron, recurrence_json, active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, title, stringValue(data["description"], ""), stringValue(data["strategy"], "manual"), stringValue(data["timezone"], "UTC"),
		nullableString(firstNonNil(data["startAt"], data["start_at"])), nullableString(firstNonNil(data["endAt"], data["end_at"])),
		nullableString(data["cron"]), jsonOrNull(firstNonNil(data["recurrence"], data["recurrence_json"])), boolIntValue(data["active"], true))
	if err != nil {
		return maintenanceWindow{}, err
	}
	id, _ := result.LastInsertId()
	if err := replaceMaintenanceTargets(ctx, db, id, firstNonNil(data["targets"], data["targetIds"])); err != nil {
		return maintenanceWindow{}, err
	}
	item, _, err := getMaintenance(ctx, db, id)
	return item, err
}

func updateMaintenance(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (maintenanceWindow, bool, error) {
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	for key, column := range map[string]string{"title": "title", "description": "description", "strategy": "strategy", "timezone": "timezone", "cron": "cron"} {
		if value, ok := data[key]; ok {
			add(column, nullableString(value))
		}
	}
	if value, ok := firstExisting(data, "startAt", "start_at"); ok {
		add("start_at", nullableString(value))
	}
	if value, ok := firstExisting(data, "endAt", "end_at"); ok {
		add("end_at", nullableString(value))
	}
	if value, ok := data["active"]; ok {
		add("active", boolIntValue(value, true))
	}
	if value, ok := data["recurrence"]; ok {
		add("recurrence_json", jsonOrNull(value))
	}
	if len(fields) > 0 {
		fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
		values = append(values, id)
		if _, err := db.ExecContext(ctx, `UPDATE uptime_maintenance_windows SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
			return maintenanceWindow{}, true, err
		}
	}
	if value, ok := firstExisting(data, "targets", "targetIds"); ok {
		if err := replaceMaintenanceTargets(ctx, db, id, value); err != nil {
			return maintenanceWindow{}, true, err
		}
	}
	return getMaintenance(ctx, db, id)
}

func getMaintenance(ctx context.Context, db *sql.DB, id int64) (maintenanceWindow, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_maintenance_windows WHERE id = ?`, id)
	if err != nil {
		return maintenanceWindow{}, false, err
	}
	if !rows.Next() {
		rows.Close()
		return maintenanceWindow{}, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return maintenanceWindow{}, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return maintenanceWindow{}, false, err
	}
	rows.Close()
	item, err := parseMaintenance(ctx, db, row)
	return item, true, err
}

func parseMaintenance(ctx context.Context, db *sql.DB, row map[string]interface{}) (maintenanceWindow, error) {
	targets, err := maintenanceTargets(ctx, db, int64Value(row["id"], 0))
	if err != nil {
		return maintenanceWindow{}, err
	}
	return maintenanceWindow{
		ID:          int64Value(row["id"], 0),
		Title:       stringValue(row["title"], ""),
		Description: stringValue(row["description"], ""),
		Strategy:    stringValue(row["strategy"], "manual"),
		Timezone:    stringValue(row["timezone"], "UTC"),
		StartAt:     stringPointer(row["start_at"]),
		EndAt:       stringPointer(row["end_at"]),
		Cron:        stringPointer(row["cron"]),
		Recurrence:  parseJSONAny(row["recurrence_json"]),
		Targets:     targets,
		Active:      boolValue(row["active"], true),
		CreatedAt:   stringPointer(row["created_at"]),
		UpdatedAt:   stringPointer(row["updated_at"]),
	}, nil
}

func replaceMaintenanceTargets(ctx context.Context, db *sql.DB, maintenanceID int64, value interface{}) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM uptime_maintenance_targets WHERE maintenance_id = ?`, maintenanceID); err != nil {
		return err
	}
	targets := normalizeTargets(value)
	if len(targets) == 0 {
		targets = []map[string]interface{}{{"type": "global", "id": nil}}
	}
	for _, target := range targets {
		if _, err := db.ExecContext(ctx, `INSERT INTO uptime_maintenance_targets (maintenance_id, target_type, target_id) VALUES (?, ?, ?)`,
			maintenanceID, stringValue(target["type"], "monitor"), nullableString(target["id"])); err != nil {
			return err
		}
	}
	return nil
}

func maintenanceTargets(ctx context.Context, db *sql.DB, maintenanceID int64) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT target_type as type, target_id as id FROM uptime_maintenance_targets WHERE maintenance_id = ? ORDER BY id ASC`, maintenanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, row)
	}
	return targets, rows.Err()
}

func activeMaintenanceForMonitor(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `
		SELECT mw.*
		FROM uptime_maintenance_windows mw
		LEFT JOIN uptime_maintenance_targets mt ON mt.maintenance_id = mw.id
		WHERE mw.active = 1
		  AND (mw.start_at IS NULL OR mw.start_at <= ?)
		  AND (mw.end_at IS NULL OR mw.end_at >= ?)
		  AND (
			mt.id IS NULL
			OR mt.target_type = 'global'
			OR (mt.target_type = 'monitor' AND mt.target_id = ?)
		  )
		ORDER BY mw.created_at DESC
		LIMIT 1
	`, now, now, strconv.FormatInt(monitorID, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanMap(rows)
}

func (s *Service) exportConfig(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitors, err := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors ORDER BY created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	pages, err := listStatusPages(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	maintenance, err := listMaintenance(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"type":               "api-monitor-uptime-export",
		"version":            1,
		"exportedAt":         time.Now().UTC().Format(time.RFC3339),
		"monitors":           monitors,
		"statusPages":        pages,
		"maintenanceWindows": maintenance,
	})
}

func (s *Service) importPreview(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	data := objectValue(firstNonNil(payload["data"], payload))
	if typ := stringValue(data["type"], ""); typ != "" && typ != "api-monitor-uptime-export" {
		response.Error(w, http.StatusBadRequest, "Invalid uptime export payload")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	preview, err := previewImport(r.Context(), db, data)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, preview)
}

func previewImport(ctx context.Context, db *sql.DB, payload map[string]interface{}) (map[string]interface{}, error) {
	existingMonitors, err := loadMonitors(ctx, db, `SELECT * FROM uptime_monitors`)
	if err != nil {
		return nil, err
	}
	monitorKeys := map[string]bool{}
	for _, monitor := range existingMonitors {
		monitorKeys[monitorKey(monitor)] = true
	}
	existingPages, err := listStatusPages(ctx, db)
	if err != nil {
		return nil, err
	}
	pageSlugs := map[string]bool{}
	for _, page := range existingPages {
		pageSlugs[page.Slug] = true
	}
	existingMaintenance, err := listMaintenance(ctx, db)
	if err != nil {
		return nil, err
	}
	maintenanceTitles := map[string]bool{}
	for _, item := range existingMaintenance {
		maintenanceTitles[item.Title] = true
	}
	monitors := objectSlice(payload["monitors"])
	pages := objectSlice(payload["statusPages"])
	maintenance := objectSlice(payload["maintenanceWindows"])
	return map[string]interface{}{
		"monitors": mapObjects(monitors, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if monitorKeys[monitorKey(item)] {
				action = "update"
			}
			return map[string]interface{}{"name": item["name"], "type": item["type"], "action": action}
		}),
		"statusPages": mapObjects(pages, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if pageSlugs[stringValue(item["slug"], "")] {
				action = "update"
			}
			return map[string]interface{}{"title": item["title"], "slug": item["slug"], "action": action}
		}),
		"maintenanceWindows": mapObjects(maintenance, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if maintenanceTitles[stringValue(item["title"], "")] {
				action = "update"
			}
			return map[string]interface{}{"title": item["title"], "action": action}
		}),
		"counts": map[string]interface{}{"monitors": len(monitors), "statusPages": len(pages), "maintenanceWindows": len(maintenance)},
	}, nil
}

func (s *Service) importConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	data := objectValue(firstNonNil(payload["data"], payload))
	if typ := stringValue(data["type"], ""); typ != "" && typ != "api-monitor-uptime-export" {
		response.Error(w, http.StatusBadRequest, "Invalid uptime export payload")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitorsChanged, pagesChanged, maintenanceChanged := 0, 0, 0
	idMap := map[string]int64{}
	existing, _ := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors`)
	existingByKey := map[string]int64{}
	for _, monitor := range existing {
		existingByKey[monitorKey(monitor)] = int64Value(monitor["id"], 0)
	}
	for _, monitor := range objectSlice(data["monitors"]) {
		oldID := stringValue(monitor["id"], "")
		if existingID, ok := existingByKey[monitorKey(monitor)]; ok {
			updated, _, err := s.updateMonitor(r.Context(), db, existingID, monitor)
			if err == nil {
				idMap[oldID] = int64Value(updated["id"], existingID)
			}
		} else {
			created, err := s.createMonitor(r.Context(), db, monitor)
			if err == nil {
				idMap[oldID] = int64Value(created["id"], 0)
				if boolValue(created["active"], true) {
					s.startMonitor(created)
				}
			}
		}
		monitorsChanged++
	}
	existingPages, _ := listStatusPages(r.Context(), db)
	pagesBySlug := map[string]int64{}
	for _, page := range existingPages {
		pagesBySlug[page.Slug] = page.ID
	}
	for _, page := range objectSlice(data["statusPages"]) {
		remapMonitorIDs(page, idMap)
		if existingID, ok := pagesBySlug[stringValue(page["slug"], "")]; ok {
			_, _, _ = updateStatusPage(r.Context(), db, existingID, page)
		} else {
			_, _ = createStatusPage(r.Context(), db, page)
		}
		pagesChanged++
	}
	existingMaintenance, _ := listMaintenance(r.Context(), db)
	maintenanceByTitle := map[string]int64{}
	for _, item := range existingMaintenance {
		maintenanceByTitle[item.Title] = item.ID
	}
	for _, item := range objectSlice(data["maintenanceWindows"]) {
		remapTargets(item, idMap)
		if existingID, ok := maintenanceByTitle[stringValue(item["title"], "")]; ok {
			_, _, _ = updateMaintenance(r.Context(), db, existingID, item)
		} else {
			_, _ = createMaintenance(r.Context(), db, item)
		}
		maintenanceChanged++
	}
	response.OK(w, map[string]interface{}{"monitorsChanged": monitorsChanged, "pagesChanged": pagesChanged, "maintenanceChanged": maintenanceChanged})
}

func (s *Service) recordPush(w http.ResponseWriter, r *http.Request, token string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitorByPushToken(r.Context(), db, token)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Invalid push token")
		return
	}
	payload := map[string]interface{}{}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	beat := map[string]interface{}{
		"id":         time.Now().UnixMilli(),
		"status":     1,
		"state":      stateUp,
		"msg":        "Push heartbeat received",
		"ping":       0,
		"durationMs": 0,
		"details": map[string]interface{}{
			"payload":   payload,
			"ip":        r.RemoteAddr,
			"userAgent": r.UserAgent(),
		},
		"probeId": "push",
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	maintenance, _ := activeMaintenanceForMonitor(r.Context(), db, int64Value(monitor["id"], 0))
	beat["maintenance"] = maintenance != nil
	if err := saveHeartbeat(r.Context(), db, int64Value(monitor["id"], 0), beat); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.broadcastHeartbeat(int64Value(monitor["id"], 0), beat)
	_ = s.processState(r.Context(), db, monitor, probeResult{OK: true, Status: stateUp, Message: "Push heartbeat received"}, beat, maintenance != nil)
	response.OK(w, map[string]interface{}{"monitorId": monitor["id"], "receivedAt": beat["time"]})
}

func (s *Service) broadcastHeartbeat(monitorID int64, beat map[string]interface{}) {
	if s.heartbeatBroadcaster == nil || monitorID <= 0 || beat == nil {
		return
	}
	s.heartbeatBroadcaster(monitorID, beat)
}

func loadMonitorByPushToken(ctx context.Context, db *sql.DB, token string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_monitors WHERE push_token = ? AND type = 'push'`, token)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		return nil, false, err
	}
	return normalizeMonitor(row), true, rows.Err()
}

func (s *Service) probe(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (probeResult, error) {
	switch stringValue(monitor["type"], "http") {
	case "http", "keyword":
		return s.httpProbe(ctx, monitor, stringValue(monitor["type"], "http") == "keyword")
	case "json", "json-query":
		return s.jsonProbe(ctx, monitor)
	case "tcp":
		return tcpProbe(ctx, monitor)
	case "ping":
		return pingProbe(ctx, monitor)
	case "dns":
		return dnsProbe(ctx, monitor)
	case "push":
		return pushProbe(ctx, db, monitor)
	default:
		return probeResult{}, fmt.Errorf("Unsupported monitor type: %s", stringValue(monitor["type"], ""))
	}
}

func (s *Service) httpProbe(ctx context.Context, monitor map[string]interface{}, keyword bool) (probeResult, error) {
	started := time.Now()
	bodyReader := bytes.NewReader([]byte(stringValue(monitor["body"], "")))
	req, err := http.NewRequestWithContext(ctx, stringValue(monitor["method"], "GET"), stringValue(monitor["url"], ""), bodyReader)
	if err != nil {
		return probeResult{}, err
	}
	for key, value := range parseJSONMap(monitor["headers"]) {
		req.Header.Set(key, stringValue(value, ""))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: boolValue(monitor["ignoreTls"], false)}
	client := http.Client{Timeout: time.Duration(intValue(monitor["timeout"], defaultTimeoutSeconds)) * time.Second, Transport: transport}
	res, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, maxProbeBodyBytes))
	statusCode := res.StatusCode
	if !acceptedStatus(stringValue(monitor["accepted_status_codes"], ""), statusCode) {
		return probeResult{}, fmt.Errorf("HTTP %d not accepted", statusCode)
	}
	if keyword {
		expected := stringValue(monitor["keyword"], "")
		if expected != "" && !strings.Contains(string(content), expected) {
			return probeResult{}, fmt.Errorf("Keyword not found: %s", expected)
		}
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", StatusCode: &statusCode, Details: map[string]interface{}{"contentLength": len(content)}}, nil
}

func (s *Service) jsonProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, stringValue(monitor["method"], "GET"), stringValue(monitor["url"], ""), strings.NewReader(stringValue(monitor["body"], "")))
	if err != nil {
		return probeResult{}, err
	}
	for key, value := range parseJSONMap(monitor["headers"]) {
		req.Header.Set(key, stringValue(value, ""))
	}
	client := http.Client{Timeout: time.Duration(intValue(monitor["timeout"], defaultTimeoutSeconds)) * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer res.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(res.Body, maxProbeBodyBytes))
	statusCode := res.StatusCode
	if !acceptedStatus(stringValue(monitor["accepted_status_codes"], ""), statusCode) {
		return probeResult{}, fmt.Errorf("HTTP %d not accepted", statusCode)
	}
	var parsed interface{}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return probeResult{}, err
	}
	configMap := objectValue(monitor["config"])
	path := stringValue(firstNonNil(configMap["jsonQueryPath"], configMap["jsonPath"], configMap["query"], monitor["keyword"]), "")
	operator := stringValue(firstNonNil(configMap["jsonQueryOperator"], configMap["operator"]), "equals")
	expected := firstNonNil(configMap["jsonExpectedValue"], configMap["expectedValue"], monitor["expectedValue"])
	actual := jsonPathValue(parsed, path)
	if !compareValues(actual, expected, operator) {
		return probeResult{}, fmt.Errorf("JSON Query mismatch at %s: expected %s %v", stringFallback(path, "$"), operator, expected)
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", StatusCode: &statusCode, Details: map[string]interface{}{"contentLength": len(content), "jsonQueryPath": stringFallback(path, "$"), "jsonQueryOperator": operator, "jsonQueryActual": actual}}, nil
}

func tcpProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	dialer := net.Dialer{Timeout: time.Duration(intValue(monitor["timeout"], 10)) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(stringValue(monitor["hostname"], ""), strconv.Itoa(intValue(monitor["port"], 0))))
	if err != nil {
		return probeResult{}, err
	}
	_ = conn.Close()
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK"}, nil
}

func pingProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	var last error
	for _, port := range []int{80, 443, 53} {
		probeMonitor := copyMap(monitor)
		probeMonitor["port"] = port
		probeMonitor["timeout"] = math.Min(float64(intValue(monitor["timeout"], 2)), 2)
		result, err := tcpProbe(ctx, probeMonitor)
		if err == nil {
			return result, nil
		}
		last = err
	}
	return probeResult{}, fmt.Errorf("Ping TCP fallback failed: %v", last)
}

func dnsProbe(ctx context.Context, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(intValue(monitor["timeout"], 10))*time.Second)
	defer cancel()
	records, err := net.DefaultResolver.LookupHost(timeoutCtx, stringValue(monitor["hostname"], ""))
	if err != nil {
		return probeResult{}, err
	}
	expected := stringValue(firstNonNil(monitor["keyword"], monitor["expectedValue"], objectValue(monitor["config"])["expectedValue"]), "")
	if expected != "" && !strings.Contains(strings.Join(records, ","), expected) {
		return probeResult{}, fmt.Errorf("DNS expected value not found: %s", expected)
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "OK", Details: map[string]interface{}{"records": records, "type": stringValue(monitor["dns_resolve_type"], "A")}}, nil
}

func pushProbe(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (probeResult, error) {
	started := time.Now()
	configMap := objectValue(monitor["config"])
	grace := intValue(firstNonNil(monitor["pushGraceSeconds"], monitor["push_grace_seconds"], configMap["graceSeconds"]), 120)
	last, err := getLastHeartbeat(ctx, db, int64Value(monitor["id"], 0))
	if err != nil {
		return probeResult{}, err
	}
	if last == nil || intValue(last["status"], 0) != 1 {
		return probeResult{}, errors.New("Push heartbeat missing")
	}
	lastTime := parseTimeFallback(stringValue(firstNonNil(last["time"], last["created_at"]), ""), time.Time{})
	if lastTime.IsZero() {
		return probeResult{}, errors.New("Push heartbeat missing")
	}
	age := time.Since(lastTime)
	if age > time.Duration(grace)*time.Second {
		return probeResult{}, fmt.Errorf("Push heartbeat overdue (%ds > %ds)", int(age.Seconds()), grace)
	}
	return probeResult{OK: true, Status: stateUp, LatencyMS: time.Since(started).Milliseconds(), Message: "Push heartbeat received", Details: map[string]interface{}{"lastPushAt": lastTime.UTC().Format(time.RFC3339), "ageSeconds": int(age.Seconds()), "graceSeconds": grace}}, nil
}

func acceptedStatus(raw string, status int) bool {
	if strings.TrimSpace(raw) == "" {
		return status >= 200 && status < 300
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			pieces := strings.SplitN(part, "-", 2)
			min, _ := strconv.Atoi(strings.TrimSpace(pieces[0]))
			max, _ := strconv.Atoi(strings.TrimSpace(pieces[1]))
			if status >= min && status <= max {
				return true
			}
			continue
		}
		exact, err := strconv.Atoi(part)
		if err == nil && status == exact {
			return true
		}
	}
	return false
}

func scanAll(rows *sql.Rows) ([]map[string]interface{}, error) {
	items := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func scanMap(rows *sql.Rows) (map[string]interface{}, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	dest := make([]interface{}, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	row := map[string]interface{}{}
	for i, column := range columns {
		switch value := values[i].(type) {
		case []byte:
			row[column] = string(value)
		case int64, float64, string, nil:
			row[column] = value
		default:
			row[column] = value
		}
	}
	return row, nil
}

func readObject(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]interface{}{}, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func normalizeMonitorConfig(data map[string]interface{}, existing map[string]interface{}) map[string]interface{} {
	out := copyMap(existing)
	for key, value := range parseJSONMap(data["config_json"]) {
		out[key] = value
	}
	for key, value := range objectValue(data["config"]) {
		out[key] = value
	}
	for _, key := range []string{"jsonQueryPath", "jsonQueryOperator", "jsonExpectedValue", "expectedValue"} {
		if value, ok := data[key]; ok {
			out[key] = value
		}
	}
	if value, ok := firstExisting(data, "pushGraceSeconds", "push_grace_seconds"); ok {
		out["graceSeconds"] = value
	}
	for key, value := range out {
		if value == nil || stringValue(value, "") == "" {
			delete(out, key)
		}
	}
	return out
}

func shouldUpdateConfig(data map[string]interface{}) bool {
	for _, key := range []string{"config", "config_json", "jsonQueryPath", "jsonQueryOperator", "jsonExpectedValue", "expectedValue", "pushGraceSeconds", "push_grace_seconds"} {
		if _, ok := data[key]; ok {
			return true
		}
	}
	return false
}

func monitorKey(monitor map[string]interface{}) string {
	return strings.ToLower(strings.Join([]string{
		stringValue(monitor["name"], ""),
		stringValue(monitor["type"], "http"),
		stringValue(monitor["url"], ""),
		stringValue(monitor["hostname"], ""),
		strconv.Itoa(intValue(monitor["port"], 0)),
	}, "|"))
}

func monitorTarget(monitor map[string]interface{}) string {
	if target := stringValue(monitor["url"], ""); target != "" {
		return target
	}
	host := stringValue(monitor["hostname"], "")
	port := intValue(monitor["port"], 0)
	if port > 0 {
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	return host
}

func normalizeSlug(value interface{}) string {
	text := strings.ToLower(strings.TrimSpace(stringValue(value, "status")))
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "status"
	}
	return slug
}

func normalizeTargets(value interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				out = append(out, map[string]interface{}{"type": stringValue(firstNonNil(itemMap["type"], itemMap["targetType"]), "monitor"), "id": firstNonNil(itemMap["id"], itemMap["targetId"])})
			} else {
				out = append(out, map[string]interface{}{"type": "monitor", "id": item})
			}
		}
	case []int64:
		for _, id := range typed {
			out = append(out, map[string]interface{}{"type": "monitor", "id": id})
		}
	}
	return out
}

func remapMonitorIDs(page map[string]interface{}, idMap map[string]int64) {
	ids := int64Slice(firstNonNil(page["monitorIds"], page["monitor_ids"]))
	out := []int64{}
	for _, id := range ids {
		if mapped, ok := idMap[strconv.FormatInt(id, 10)]; ok {
			out = append(out, mapped)
		} else {
			out = append(out, id)
		}
	}
	page["monitorIds"] = out
}

func remapTargets(item map[string]interface{}, idMap map[string]int64) {
	targets := normalizeTargets(firstNonNil(item["targets"], item["targetIds"]))
	for _, target := range targets {
		if stringValue(target["type"], "monitor") == "monitor" {
			key := stringValue(target["id"], "")
			if mapped, ok := idMap[key]; ok {
				target["id"] = mapped
			}
		}
	}
	item["targets"] = targets
}

func mapObjects(items []map[string]interface{}, mapper func(map[string]interface{}) map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, mapper(item))
	}
	return out
}

func jsonPathValue(source interface{}, path string) interface{} {
	expr := strings.TrimSpace(path)
	if expr == "" || expr == "$" {
		return source
	}
	expr = strings.TrimPrefix(expr, "$.")
	expr = strings.TrimPrefix(expr, "$")
	expr = strings.ReplaceAll(expr, "[", ".")
	expr = strings.ReplaceAll(expr, "]", "")
	current := source
	for _, part := range strings.Split(expr, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			current = typed[part]
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func compareValues(actual, expected interface{}, operator string) bool {
	switch operator {
	case "exists":
		return actual != nil
	case "not_exists", "notExists":
		return actual == nil
	case "not_equals", "notEquals", "ne":
		return fmt.Sprint(coerce(actual)) != fmt.Sprint(coerce(expected))
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "not_contains", "notContains":
		return !strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "gt", "greater_than", "greaterThan":
		return floatValue(actual) > floatValue(expected)
	case "gte", "greater_or_equal", "greaterOrEqual":
		return floatValue(actual) >= floatValue(expected)
	case "lt", "less_than", "lessThan":
		return floatValue(actual) < floatValue(expected)
	case "lte", "less_or_equal", "lessOrEqual":
		return floatValue(actual) <= floatValue(expected)
	case "equals", "eq", "":
		return fmt.Sprint(coerce(actual)) == fmt.Sprint(coerce(expected))
	default:
		return fmt.Sprint(coerce(actual)) == fmt.Sprint(coerce(expected))
	}
}

func coerce(value interface{}) interface{} {
	text := strings.TrimSpace(fmt.Sprint(value))
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}
	if text == "true" {
		return true
	}
	if text == "false" {
		return false
	}
	if text == "null" {
		return nil
	}
	return text
}

func parseJSONMap(value interface{}) map[string]interface{} {
	return objectValue(parseJSONAny(value))
}

func parseJSONArray(value interface{}) []interface{} {
	parsed := parseJSONAny(value)
	switch typed := parsed.(type) {
	case []interface{}:
		return typed
	case nil:
		return []interface{}{}
	default:
		return []interface{}{typed}
	}
}

func parseJSONAny(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]interface{}, []interface{}:
		return typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return parsed
		}
	}
	return nil
}

func jsonOrNull(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	if mapValue, ok := value.(map[string]interface{}); ok && len(mapValue) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(data)
}

func jsonOrDefault(value interface{}, fallback string) string {
	data, err := json.Marshal(value)
	if err != nil || value == nil {
		return fallback
	}
	return string(data)
}

func structToMap(value interface{}) map[string]interface{} {
	data, _ := json.Marshal(value)
	out := map[string]interface{}{}
	_ = json.Unmarshal(data, &out)
	return out
}

func objectValue(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	}
	return map[string]interface{}{}
}

func objectSlice(value interface{}) []map[string]interface{} {
	items := []map[string]interface{}{}
	if value == nil {
		return items
	}
	if typed, ok := value.([]interface{}); ok {
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				items = append(items, itemMap)
			}
		}
	}
	return items
}

func int64Slice(value interface{}) []int64 {
	out := []int64{}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if id := int64Value(item, 0); id > 0 {
				out = append(out, id)
			}
		}
	case []int64:
		out = append(out, typed...)
	case []int:
		for _, id := range typed {
			out = append(out, int64(id))
		}
	}
	return out
}

func firstExisting(data map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return fallback
		}
		return typed
	case []byte:
		if len(typed) == 0 {
			return fallback
		}
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		text := fmt.Sprint(value)
		if text == "<nil>" || text == "" {
			return fallback
		}
		return text
	}
}

func stringFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringPointer(value interface{}) *string {
	text := stringValue(value, "")
	if text == "" {
		return nil
	}
	return &text
}

func stringPtrValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return stringValue(value, "")
}

func nullableString(value interface{}) interface{} {
	text := stringValue(value, "")
	if text == "" {
		return nil
	}
	return text
}

func intValue(value interface{}, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		parsed, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil {
			return fallback
		}
		return parsed
	}
}

func int64Value(value interface{}, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return fallback
		}
		return parsed
	default:
		parsed, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		if err != nil {
			return fallback
		}
		return parsed
	}
}

func floatValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}

func nullableInt(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
	}
	return intValue(value, 0)
}

func intPtrValue(value interface{}) int {
	if value == nil {
		return 0
	}
	return intValue(value, 0)
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case string:
		if typed == "" {
			return fallback
		}
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return parsed
		}
		number, err := strconv.Atoi(typed)
		if err == nil {
			return number != 0
		}
	}
	return fallback
}

func boolIntValue(value interface{}, fallback bool) int {
	return boolInt(boolValue(value, fallback))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stateText(value interface{}) string {
	text := stringValue(value, stateUp)
	switch text {
	case "ok":
		return stateUp
	case "pending":
		return statePendingDown
	case "firing":
		return stateDown
	case "recovery":
		return statePendingUp
	default:
		return text
	}
}

func parseTimeFallback(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return fallback
}

func formatDuration(ms int64) string {
	if ms < 60_000 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if ms < 3_600_000 {
		return fmt.Sprintf("%dm%ds", ms/60_000, (ms%60_000)/1000)
	}
	return fmt.Sprintf("%dh%dm", ms/3_600_000, (ms%3_600_000)/60_000)
}

func generateToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func escapeSVG(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
