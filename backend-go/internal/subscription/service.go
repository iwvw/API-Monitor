package subscription

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/managedproxy"
	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
	"gopkg.in/yaml.v3"
)

const (
	defaultTemplateID     = "builtin_mihomo_default"
	rawTemplateID         = "builtin_raw_uri"
	base64TemplateID      = "builtin_base64_uri"
	defaultNodeLibrary    = "sub_default_nodes"
	defaultLimitPerMin    = 30
	defaultRefreshHours   = 24
	planSelectionExplicit = "explicit"
	planSelectionAll      = "all"
)

var nodeLinkPattern = regexp.MustCompile(`(?im)(vmess|vless|trojan|ss|hysteria2|hy2|tuic|socks|http)://[^\s'"<>]+`)

//go:embed templates/default-mihomo.yaml
var defaultMihomoTemplateEmbedded string

type Service struct {
	cfg        config.Config
	store      *database.Store
	schema     database.SchemaEnsurer
	client     *http.Client
	refreshMu  sync.Mutex
	stopAuto   context.CancelFunc
	autoClosed chan struct{}
}

type Subscription struct {
	ID                    string           `json:"id"`
	ProfileID             string           `json:"profile_id"`
	PlanID                string           `json:"plan_id"`
	PlanEnabled           bool             `json:"plan_enabled"`
	Name                  string           `json:"name"`
	Remark                string           `json:"remark"`
	Enabled               bool             `json:"enabled"`
	PublicToken           string           `json:"public_token"`
	VLESSUUID             string           `json:"vless_uuid"`
	Hysteria2Password     string           `json:"hysteria2_password"`
	TemplateID            string           `json:"template_id"`
	TrafficSource         string           `json:"traffic_source"`
	TrafficServerID       string           `json:"traffic_server_id,omitempty"`
	UpstreamURL           string           `json:"upstream_url,omitempty"`
	UpstreamEnabled       bool             `json:"upstream_enabled"`
	UpstreamRefreshHours  int              `json:"upstream_refresh_hours"`
	UpstreamStatus        string           `json:"upstream_status,omitempty"`
	UpstreamLastError     string           `json:"upstream_last_error,omitempty"`
	UpstreamLastRefreshAt string           `json:"upstream_last_refresh_at,omitempty"`
	TotalBytes            int64            `json:"total_bytes"`
	ManualUploadBytes     int64            `json:"manual_upload_bytes"`
	ManualDownloadBytes   int64            `json:"manual_download_bytes"`
	ExpireAt              string           `json:"expire_at,omitempty"`
	CycleType             string           `json:"cycle_type"`
	CycleDay              int              `json:"cycle_day"`
	CycleStart            string           `json:"cycle_start,omitempty"`
	CycleEnd              string           `json:"cycle_end,omitempty"`
	BaselineUploadBytes   int64            `json:"baseline_upload_bytes"`
	BaselineDownloadBytes int64            `json:"baseline_download_bytes"`
	RateLimitEnabled      bool             `json:"rate_limit_enabled"`
	RateLimitPerMinute    int              `json:"rate_limit_per_minute"`
	NodeFilterIDs         []string         `json:"node_filter_ids,omitempty"`
	NodeSelectionMode     string           `json:"node_selection_mode,omitempty"`
	IncludeInternalNodes  bool             `json:"include_internal_nodes"`
	IncludeExternalNodes  bool             `json:"include_external_nodes"`
	CreatedAt             string           `json:"created_at"`
	UpdatedAt             string           `json:"updated_at"`
	NodeCount             int              `json:"node_count"`
	AccessCountToday      int              `json:"access_count_today"`
	LastAccessAt          string           `json:"last_access_at,omitempty"`
	Traffic               TrafficInfo      `json:"traffic"`
	RuntimeSyncStatus     string           `json:"runtime_sync_status"`
	Quality               []QualitySummary `json:"quality,omitempty"`
}

type NodeLibrary struct {
	ID                    string      `json:"id"`
	Name                  string      `json:"name"`
	Remark                string      `json:"remark"`
	Enabled               bool        `json:"enabled"`
	TemplateID            string      `json:"template_id"`
	TrafficSource         string      `json:"traffic_source"`
	TrafficServerID       string      `json:"traffic_server_id,omitempty"`
	UpstreamURL           string      `json:"upstream_url,omitempty"`
	UpstreamEnabled       bool        `json:"upstream_enabled"`
	UpstreamRefreshHours  int         `json:"upstream_refresh_hours"`
	UpstreamStatus        string      `json:"upstream_status,omitempty"`
	UpstreamLastError     string      `json:"upstream_last_error,omitempty"`
	UpstreamLastRefreshAt string      `json:"upstream_last_refresh_at,omitempty"`
	UpstreamUserinfo      string      `json:"upstream_userinfo,omitempty"`
	TotalBytes            int64       `json:"total_bytes"`
	ManualUploadBytes     int64       `json:"manual_upload_bytes"`
	ManualDownloadBytes   int64       `json:"manual_download_bytes"`
	ExpireAt              string      `json:"expire_at,omitempty"`
	CycleType             string      `json:"cycle_type"`
	CycleDay              int         `json:"cycle_day"`
	CycleStart            string      `json:"cycle_start,omitempty"`
	CycleEnd              string      `json:"cycle_end,omitempty"`
	BaselineUploadBytes   int64       `json:"baseline_upload_bytes"`
	BaselineDownloadBytes int64       `json:"baseline_download_bytes"`
	RateLimitEnabled      bool        `json:"rate_limit_enabled"`
	RateLimitPerMinute    int         `json:"rate_limit_per_minute"`
	NodeFilterTags        string      `json:"node_filter_tags,omitempty"`
	SortOrder             int         `json:"sort_order"`
	CreatedAt             string      `json:"created_at"`
	UpdatedAt             string      `json:"updated_at"`
	NodeCount             int         `json:"node_count"`
	SubscriptionCount     int         `json:"subscription_count"`
	Traffic               TrafficInfo `json:"traffic"`
}

type Node struct {
	ID               string           `json:"id"`
	SubscriptionID   string           `json:"subscription_id"`
	ProfileID        string           `json:"profile_id"`
	Name             string           `json:"name"`
	Type             string           `json:"type"`
	Server           string           `json:"server"`
	Port             int              `json:"port"`
	CountryCode      string           `json:"country_code,omitempty"`
	Location         string           `json:"location,omitempty"`
	Tags             string           `json:"tags,omitempty"`
	TrafficServerID  string           `json:"traffic_server_id,omitempty"`
	Ownership        string           `json:"ownership"`
	Management       string           `json:"management"`
	TrafficReporting string           `json:"traffic_reporting"`
	Enabled          bool             `json:"enabled"`
	Stable           bool             `json:"stable"`
	SortOrder        int              `json:"sort_order"`
	Raw              string           `json:"raw,omitempty"`
	ConfigJSON       string           `json:"config_json,omitempty"`
	Source           string           `json:"source,omitempty"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
	Quality          []QualitySummary `json:"quality,omitempty"`
}

type Template struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Format          string `json:"format"`
	Content         string `json:"content"`
	Builtin         bool   `json:"builtin"`
	IsDefault       bool   `json:"is_default"`
	Valid           bool   `json:"valid"`
	ValidationError string `json:"validation_error,omitempty"`
	Description     string `json:"description"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type TrafficInfo struct {
	Upload         int64   `json:"upload"`
	Download       int64   `json:"download"`
	Total          int64   `json:"total"`
	Expire         int64   `json:"expire"`
	Percent        float64 `json:"percent"`
	Source         string  `json:"source"`
	Status         string  `json:"status"`
	MeteringStatus string  `json:"metering_status"`
	CycleStart     string  `json:"cycle_start,omitempty"`
	CycleEnd       string  `json:"cycle_end,omitempty"`
}

type subscriptionUsageReport struct {
	ServerID      string `json:"server_id"`
	NodeID        string `json:"node_id"`
	CredentialID  string `json:"credential_id"`
	BootID        string `json:"boot_id"`
	Sequence      int64  `json:"sequence"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
}

type serverTrafficQuota struct {
	UsedBytes  int64
	LimitBytes int64
	Exhausted  bool
}

type QualitySummary struct {
	Name         string  `json:"name"`
	LatencyMS    float64 `json:"latency_ms"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	JitterMS     float64 `json:"jitter_ms"`
	LossRate     float64 `json:"loss_rate"`
	SampledAt    string  `json:"sampled_at"`
}

type Settings struct {
	DefaultTemplateID       string `json:"default_template_id"`
	DefaultRateLimitEnabled bool   `json:"default_rate_limit_enabled"`
	DefaultRateLimitPerMin  int    `json:"default_rate_limit_per_minute"`
	DefaultRefreshHours     int    `json:"default_refresh_hours"`
	GeoIPEnabled            bool   `json:"geoip_enabled"`
}

type Plan struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Remark               string   `json:"remark"`
	Enabled              bool     `json:"enabled"`
	TotalBytes           int64    `json:"total_bytes"`
	CycleType            string   `json:"cycle_type"`
	CycleDay             int      `json:"cycle_day"`
	RateLimitEnabled     bool     `json:"rate_limit_enabled"`
	RateLimitPerMinute   int      `json:"rate_limit_per_minute"`
	NodeIDs              []string `json:"node_ids"`
	SelectionMode        string   `json:"selection_mode"`
	IncludeInternalNodes bool     `json:"include_internal_nodes"`
	IncludeExternalNodes bool     `json:"include_external_nodes"`
	SubscriptionCount    int      `json:"subscription_count"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:    cfg,
		store:  database.New(cfg),
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// Initialize performs all subscription DDL before HTTP traffic is accepted.
// open retains a locked fallback for isolated tests, but production startup
// calls this method so ALTER TABLE never races with concurrent requests.
func (s *Service) Initialize(ctx context.Context) error {
	db, err := s.store.Open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	return s.schema.Ensure(func() error {
		return database.WithSchemaLock(ctx, func() error {
			if err := ensureSchema(ctx, db); err != nil {
				return err
			}
			if err := ensureBuiltins(ctx, db, false); err != nil {
				return err
			}
			return ensureDefaultNodeLibrary(ctx, db)
		})
	})
}

func (s *Service) StartAutoRefresh(ctx context.Context) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.stopAuto != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.stopAuto = cancel
	s.autoClosed = make(chan struct{})
	go s.autoRefreshLoop(runCtx)
}

func (s *Service) StopAutoRefresh() {
	s.refreshMu.Lock()
	cancel := s.stopAuto
	closed := s.autoClosed
	s.stopAuto = nil
	s.autoClosed = nil
	s.refreshMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closed != nil {
		<-closed
	}
}

func (s *Service) autoRefreshLoop(ctx context.Context) {
	defer close(s.autoClosed)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	s.refreshDueUpstreams(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshDueUpstreams(ctx)
		}
	}
}

func (s *Service) refreshDueUpstreams(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT s.id
		FROM subscription_subscriptions s
		LEFT JOIN subscription_upstreams u ON u.profile_id = COALESCE(s.profile_id, s.id)
		WHERE s.enabled = 1
			AND COALESCE(u.enabled, s.upstream_enabled, 0) = 1
			AND COALESCE(u.url, s.upstream_url, '') != ''
			AND (
				COALESCE(u.last_refresh_at, s.upstream_last_refresh_at, '') = ''
				OR datetime(COALESCE(u.last_refresh_at, s.upstream_last_refresh_at)) <= datetime('now', '-' || COALESCE(NULLIF(u.refresh_hours, 0), NULLIF(s.upstream_refresh_hours, 0), 24) || ' hours')
			)
		ORDER BY s.updated_at ASC
		LIMIT 10`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		_ = s.refreshUpstreamNow(ctx, db, id)
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/sub/") {
		s.servePublicSubscription(w, r)
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/subscription"), "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch {
	case len(parts) == 0 || (len(parts) == 1 && parts[0] == "summary"):
		s.summary(w, r, db)
	case len(parts) == 1 && parts[0] == "profiles":
		switch r.Method {
		case http.MethodGet:
			s.listProfiles(w, r, db)
		case http.MethodPost:
			s.createProfile(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "plans":
		s.handlePlans(w, r, db, "")
	case len(parts) == 2 && parts[0] == "plans":
		s.handlePlans(w, r, db, parts[1])
	case len(parts) == 2 && parts[0] == "profiles":
		switch r.Method {
		case http.MethodGet:
			s.getProfile(w, r, db, parts[1])
		case http.MethodPut:
			s.updateProfile(w, r, db, parts[1])
		case http.MethodDelete:
			s.deleteProfile(w, r, db, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "profiles" && parts[2] == "refresh-upstream":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.refreshProfileUpstream(w, r, db, parts[1])
	case len(parts) == 1 && parts[0] == "subscriptions":
		switch r.Method {
		case http.MethodGet:
			s.listSubscriptions(w, r, db)
		case http.MethodPost:
			s.createSubscription(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "subscriptions":
		switch r.Method {
		case http.MethodGet:
			s.getSubscription(w, r, db, parts[1])
		case http.MethodPut:
			s.updateSubscription(w, r, db, parts[1])
		case http.MethodPatch:
			s.setSubscriptionEnabled(w, r, db, parts[1])
		case http.MethodDelete:
			s.deleteSubscription(w, r, db, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "subscriptions" && parts[2] == "reset-token":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.resetToken(w, r, db, parts[1])
	case len(parts) == 3 && parts[0] == "subscriptions" && parts[2] == "rotate-address":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.rotateAddress(w, r, db, parts[1])
	case len(parts) == 3 && parts[0] == "subscriptions" && parts[2] == "refresh-upstream":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.refreshUpstream(w, r, db, parts[1])
	case len(parts) == 1 && parts[0] == "nodes":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listNodes(w, r, db)
	case len(parts) == 2 && parts[0] == "nodes" && parts[1] == "reorder":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.reorderNodes(w, r, db)
	case len(parts) == 2 && parts[0] == "nodes":
		switch r.Method {
		case http.MethodPut:
			s.updateNode(w, r, db, parts[1])
		case http.MethodDelete:
			s.deleteNode(w, r, db, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "preview":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.importPreview(w, r)
	case len(parts) == 2 && parts[0] == "import" && parts[1] == "commit":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.importCommit(w, r, db)
	case len(parts) == 1 && parts[0] == "templates":
		switch r.Method {
		case http.MethodGet:
			s.listTemplates(w, r, db)
		case http.MethodPost:
			s.createTemplate(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "templates":
		switch r.Method {
		case http.MethodPut:
			s.updateTemplate(w, r, db, parts[1])
		case http.MethodDelete:
			s.deleteTemplate(w, r, db, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "templates" && parts[2] == "default":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.setDefaultTemplate(w, r, db, parts[1])
	case len(parts) == 2 && parts[0] == "templates" && parts[1] == "restore-builtins":
		if r.Method != http.MethodPost {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := ensureBuiltins(r.Context(), db, true); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]bool{"restored": true})
	case len(parts) == 1 && parts[0] == "logs":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listLogs(w, r, db)
	case len(parts) == 1 && parts[0] == "settings":
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r, db)
		case http.MethodPut:
			s.updateSettings(w, r, db)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "servers":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listServers(w, r, db)
	case len(parts) == 1 && parts[0] == "export":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.exportAll(w, r, db)
	default:
		response.Error(w, http.StatusNotFound, "subscription route not found")
	}
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error {
		return database.WithSchemaLock(ctx, func() error {
			if err := ensureSchema(ctx, db); err != nil {
				return err
			}
			if err := ensureBuiltins(ctx, db, false); err != nil {
				return err
			}
			return ensureDefaultNodeLibrary(ctx, db)
		})
	}); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			default_template_id TEXT DEFAULT 'builtin_mihomo_default',
			default_rate_limit_enabled INTEGER DEFAULT 1,
			default_rate_limit_per_minute INTEGER DEFAULT 30,
			default_refresh_hours INTEGER DEFAULT 24,
			geoip_enabled INTEGER DEFAULT 1,
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`INSERT OR IGNORE INTO subscription_settings (id) VALUES (1)`,
		`CREATE TABLE IF NOT EXISTS subscription_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			format TEXT NOT NULL,
			content TEXT NOT NULL,
			builtin INTEGER DEFAULT 0,
			is_default INTEGER DEFAULT 0,
			description TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			remark TEXT,
			enabled INTEGER DEFAULT 1,
			template_id TEXT DEFAULT 'builtin_mihomo_default',
			traffic_source TEXT DEFAULT 'manual',
			traffic_server_id TEXT,
			ownership TEXT DEFAULT 'external',
			management TEXT DEFAULT 'unmanaged',
			traffic_reporting TEXT DEFAULT 'unavailable',
			total_bytes INTEGER DEFAULT 0,
			manual_upload_bytes INTEGER DEFAULT 0,
			manual_download_bytes INTEGER DEFAULT 0,
			expire_at TEXT,
			cycle_type TEXT DEFAULT 'none',
			cycle_day INTEGER DEFAULT 1,
			cycle_start TEXT,
			cycle_end TEXT,
			baseline_upload_bytes INTEGER DEFAULT 0,
			baseline_download_bytes INTEGER DEFAULT 0,
			rate_limit_enabled INTEGER DEFAULT 1,
			rate_limit_per_minute INTEGER DEFAULT 30,
			node_filter_tags TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_upstreams (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			refresh_hours INTEGER DEFAULT 24,
			status TEXT,
			last_error TEXT,
			last_refresh_at TEXT,
			userinfo TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_subscriptions (
			id TEXT PRIMARY KEY,
			profile_id TEXT,
			plan_id TEXT DEFAULT '',
			name TEXT NOT NULL,
			remark TEXT,
			enabled INTEGER DEFAULT 1,
			public_token TEXT NOT NULL UNIQUE,
			vless_uuid TEXT NOT NULL DEFAULT '',
			hysteria2_password TEXT NOT NULL DEFAULT '',
			template_id TEXT DEFAULT 'builtin_mihomo_default',
			traffic_source TEXT DEFAULT 'manual',
			traffic_server_id TEXT,
			upstream_url TEXT,
			upstream_enabled INTEGER DEFAULT 0,
			upstream_refresh_hours INTEGER DEFAULT 24,
			upstream_status TEXT,
			upstream_last_error TEXT,
			upstream_last_refresh_at TEXT,
			upstream_userinfo TEXT,
			total_bytes INTEGER DEFAULT 0,
			manual_upload_bytes INTEGER DEFAULT 0,
			manual_download_bytes INTEGER DEFAULT 0,
			expire_at TEXT,
			cycle_type TEXT DEFAULT 'none',
			cycle_day INTEGER DEFAULT 1,
			cycle_start TEXT,
			cycle_end TEXT,
			baseline_upload_bytes INTEGER DEFAULT 0,
			baseline_download_bytes INTEGER DEFAULT 0,
			rate_limit_enabled INTEGER DEFAULT 1,
			rate_limit_per_minute INTEGER DEFAULT 30,
			node_filter_ids TEXT DEFAULT '',
			include_internal_nodes INTEGER DEFAULT 1,
			include_external_nodes INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_plans (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			remark TEXT DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			total_bytes INTEGER NOT NULL DEFAULT 0,
			cycle_type TEXT NOT NULL DEFAULT 'monthly',
			cycle_day INTEGER NOT NULL DEFAULT 1,
			rate_limit_enabled INTEGER NOT NULL DEFAULT 1,
			rate_limit_per_minute INTEGER NOT NULL DEFAULT 30,
			node_ids TEXT NOT NULL DEFAULT '',
			selection_mode TEXT NOT NULL DEFAULT 'explicit' CHECK(selection_mode IN ('explicit','all')),
			include_internal_nodes INTEGER NOT NULL DEFAULT 1,
			include_external_nodes INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_plan_nodes (
			plan_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			source TEXT NOT NULL CHECK(source IN ('internal','external')),
			created_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY(plan_id,node_id,source)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_plan_nodes_node ON subscription_plan_nodes(node_id,source)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_subscriptions_token ON subscription_subscriptions(public_token)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_upstreams_profile ON subscription_upstreams(profile_id)`,
		`CREATE TABLE IF NOT EXISTS subscription_nodes (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL,
			profile_id TEXT,
			name TEXT NOT NULL,
			type TEXT,
			server TEXT,
			port INTEGER DEFAULT 0,
			country_code TEXT,
			location TEXT,
			tags TEXT,
			traffic_server_id TEXT,
			ownership TEXT DEFAULT 'external',
			management TEXT DEFAULT 'unmanaged',
			traffic_reporting TEXT DEFAULT 'unavailable',
			enabled INTEGER DEFAULT 1,
			stable INTEGER DEFAULT 0,
			sort_order INTEGER DEFAULT 0,
			raw_encrypted TEXT,
			config_encrypted TEXT,
			fingerprint TEXT,
			source TEXT DEFAULT 'manual',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_nodes_subscription ON subscription_nodes(subscription_id, sort_order)`,
		`CREATE TABLE IF NOT EXISTS subscription_access_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subscription_id TEXT,
			public_token TEXT,
			ip_address TEXT,
			user_agent TEXT,
			format TEXT,
			success INTEGER DEFAULT 0,
			status_code INTEGER DEFAULT 200,
			error_message TEXT,
			node_count INTEGER DEFAULT 0,
			upload_bytes INTEGER DEFAULT 0,
			download_bytes INTEGER DEFAULT 0,
			total_bytes INTEGER DEFAULT 0,
			expire_at INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now'))
		)`,
		managedproxy.NodeTableDDL,
		`CREATE TABLE IF NOT EXISTS managed_proxy_preferences (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 443, enabled INTEGER NOT NULL DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT 'unknown', last_latency_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '', checked_at TEXT,
			created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_access_logs_subscription ON subscription_access_logs(subscription_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_access_logs_ip ON subscription_access_logs(subscription_id, ip_address, created_at)`,
		`CREATE TRIGGER IF NOT EXISTS trg_subscription_plan_nodes_managed_delete AFTER DELETE ON managed_proxy_nodes
			BEGIN DELETE FROM subscription_plan_nodes WHERE node_id=OLD.id AND source='internal'; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_subscription_plan_nodes_external_delete AFTER DELETE ON subscription_nodes
			BEGIN DELETE FROM subscription_plan_nodes WHERE node_id=OLD.id AND source='external'; END`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure subscription schema: %w", err)
		}
	}
	if err := reconcilequeue.EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := subscriptionledger.EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "profile_id", "ALTER TABLE subscription_subscriptions ADD COLUMN profile_id TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "vless_uuid", "ALTER TABLE subscription_subscriptions ADD COLUMN vless_uuid TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "hysteria2_password", "ALTER TABLE subscription_subscriptions ADD COLUMN hysteria2_password TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_vless_uuid ON subscription_subscriptions(vless_uuid) WHERE vless_uuid<>''`); err != nil {
		return fmt.Errorf("create subscription VLESS credential index: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_hysteria2_password ON subscription_subscriptions(hysteria2_password) WHERE hysteria2_password<>''`); err != nil {
		return fmt.Errorf("create subscription Hysteria2 credential index: %w", err)
	}
	if err := ensureColumn(ctx, db, "subscription_plans", "selection_mode", "ALTER TABLE subscription_plans ADD COLUMN selection_mode TEXT NOT NULL DEFAULT 'explicit'"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "plan_id", "ALTER TABLE subscription_subscriptions ADD COLUMN plan_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_nodes", "profile_id", "ALTER TABLE subscription_nodes ADD COLUMN profile_id TEXT"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_nodes", "traffic_server_id", "ALTER TABLE subscription_nodes ADD COLUMN traffic_server_id TEXT"); err != nil {
		return err
	}
	for _, column := range []struct{ name, sql string }{
		{"ownership", "ALTER TABLE subscription_nodes ADD COLUMN ownership TEXT DEFAULT 'external'"},
		{"management", "ALTER TABLE subscription_nodes ADD COLUMN management TEXT DEFAULT 'unmanaged'"},
		{"traffic_reporting", "ALTER TABLE subscription_nodes ADD COLUMN traffic_reporting TEXT DEFAULT 'unavailable'"},
	} {
		if err := ensureColumn(ctx, db, "subscription_nodes", column.name, column.sql); err != nil {
			return err
		}
	}
	for _, column := range []struct{ name, sql string }{
		{"ownership", "ALTER TABLE subscription_profiles ADD COLUMN ownership TEXT DEFAULT 'external'"},
		{"management", "ALTER TABLE subscription_profiles ADD COLUMN management TEXT DEFAULT 'unmanaged'"},
		{"traffic_reporting", "ALTER TABLE subscription_profiles ADD COLUMN traffic_reporting TEXT DEFAULT 'unavailable'"},
	} {
		if err := ensureColumn(ctx, db, "subscription_profiles", column.name, column.sql); err != nil {
			return err
		}
	}
	if err := managedproxy.EnsureNodeColumns(ctx, db); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "node_filter_ids", "ALTER TABLE subscription_subscriptions ADD COLUMN node_filter_ids TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "include_external_nodes", "ALTER TABLE subscription_subscriptions ADD COLUMN include_external_nodes INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, db, "subscription_subscriptions", "include_internal_nodes", "ALTER TABLE subscription_subscriptions ADD COLUMN include_internal_nodes INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_subscription_subscriptions_profile ON subscription_subscriptions(profile_id)`); err != nil {
		return fmt.Errorf("create subscription profile index: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_subscriptions SET profile_id = id WHERE COALESCE(profile_id, '') = ''`); err != nil {
		return fmt.Errorf("normalize subscription profile ids: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_nodes SET profile_id = subscription_id WHERE COALESCE(profile_id, '') = ''`); err != nil {
		return fmt.Errorf("normalize node profile ids: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_profiles (
			id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, created_at, updated_at
		)
		SELECT profile_id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, created_at, updated_at
		FROM subscription_subscriptions
		WHERE COALESCE(profile_id, '') != ''`); err != nil {
		return fmt.Errorf("seed subscription profiles: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_upstreams (
			id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, userinfo, created_at, updated_at
		)
		SELECT 'up_' || profile_id, profile_id, '默认上游', upstream_url, upstream_enabled, upstream_refresh_hours,
			upstream_status, upstream_last_error, upstream_last_refresh_at, upstream_userinfo, created_at, updated_at
		FROM subscription_subscriptions
		WHERE COALESCE(profile_id, '') != '' AND COALESCE(upstream_url, '') != ''`); err != nil {
		return fmt.Errorf("seed subscription upstreams: %w", err)
	}
	if err := migratePlanNodeRelations(ctx, db); err != nil {
		return err
	}
	if err := backfillSubscriptionCredentials(ctx, db); err != nil {
		return err
	}
	return nil
}

func backfillSubscriptionCredentials(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM subscription_subscriptions WHERE COALESCE(vless_uuid,'')='' OR COALESCE(hysteria2_password,'')=''`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, `UPDATE subscription_subscriptions SET vless_uuid=CASE WHEN COALESCE(vless_uuid,'')='' THEN ? ELSE vless_uuid END,hysteria2_password=CASE WHEN COALESCE(hysteria2_password,'')='' THEN ? ELSE hysteria2_password END WHERE id=?`, randomUUID(), randomCredential(), id); err != nil {
			return fmt.Errorf("backfill subscription credentials: %w", err)
		}
	}
	return nil
}

func migratePlanNodeRelations(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id,COALESCE(node_ids,'') FROM subscription_plans WHERE COALESCE(node_ids,'')<>''`)
	if err != nil {
		return err
	}
	type legacyPlan struct{ id, nodeIDs string }
	legacy := []legacyPlan{}
	for rows.Next() {
		var item legacyPlan
		if err := rows.Scan(&item.id, &item.nodeIDs); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, plan := range legacy {
		var relationCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id=?`, plan.id).Scan(&relationCount); err != nil {
			return err
		}
		if relationCount == 0 {
			seen := map[string]struct{}{}
			for _, rawID := range decodeNodeFilterIDs(plan.nodeIDs) {
				nodeID := strings.TrimSpace(rawID)
				if nodeID == "" {
					continue
				}
				if _, exists := seen[nodeID]; exists {
					continue
				}
				seen[nodeID] = struct{}{}
				source := ""
				var exists int
				if scanErr := tx.QueryRowContext(ctx, `SELECT 1 FROM managed_proxy_nodes WHERE id=?`, nodeID).Scan(&exists); scanErr == nil {
					source = "internal"
				} else if scanErr := tx.QueryRowContext(ctx, `SELECT 1 FROM subscription_nodes WHERE id=?`, nodeID).Scan(&exists); scanErr == nil {
					source = "external"
				}
				// Legacy snapshots can outlive their node. Migration intentionally
				// drops those stale references; normal plan saves remain strict.
				if source == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_plan_nodes(plan_id,node_id,source) VALUES(?,?,?)`, plan.id, nodeID, source); err != nil {
					return fmt.Errorf("migrate plan node relations: %w", err)
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscription_plans SET node_ids='' WHERE id=?`, plan.id); err != nil {
			return fmt.Errorf("retire legacy plan node snapshot: %w", err)
		}
	}
	return tx.Commit()
}

func ensureColumn(ctx context.Context, db *sql.DB, tableName, columnName, alterSQL string) error {
	exists, err := schemaColumnExists(ctx, db, tableName, columnName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		// Another backend process may have completed the same ALTER between the
		// inspection and this statement. Re-inspect before treating it as fatal.
		if exists, inspectErr := schemaColumnExists(ctx, db, tableName, columnName); inspectErr == nil && exists {
			return nil
		}
		return fmt.Errorf("add %s.%s: %w", tableName, columnName, err)
	}
	return nil
}

func schemaColumnExists(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan %s columns: %w", tableName, err)
		}
		if name == columnName {
			rows.Close()
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close %s columns: %w", tableName, err)
	}
	return false, nil
}

func (s *Service) handlePlans(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			plans, err := loadPlans(r.Context(), db, id)
			if err != nil || len(plans) == 0 {
				response.Error(w, http.StatusNotFound, "套餐不存在")
				return
			}
			response.OK(w, plans[0])
			return
		}
		plans, err := loadPlans(r.Context(), db, "")
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.OK(w, plans)
	case http.MethodPatch:
		if id == "" {
			response.Error(w, http.StatusBadRequest, "套餐 ID 不能为空")
			return
		}
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Enabled == nil {
			response.Error(w, http.StatusBadRequest, "enabled 不能为空")
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE subscription_plans SET enabled=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			response.Error(w, http.StatusNotFound, "套餐不存在")
			return
		}
		nodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "plan enabled state changed") != nil {
			response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		plans, _ := loadPlans(r.Context(), db, id)
		response.OK(w, plans[0])
	case http.MethodPost, http.MethodPut:
		var input Plan
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			response.Error(w, 400, "套餐名称不能为空")
			return
		}
		if input.CycleDay < 1 || input.CycleDay > 31 {
			input.CycleDay = 1
		}
		input.CycleType = normalizeCycleType(input.CycleType)
		input.SelectionMode = normalizePlanSelectionMode(input.SelectionMode)
		if input.SelectionMode == planSelectionAll && !input.IncludeInternalNodes && !input.IncludeExternalNodes {
			response.Error(w, http.StatusBadRequest, "全部节点模式至少需要启用一个节点来源")
			return
		}
		if input.SelectionMode == planSelectionAll {
			input.NodeIDs = nil
		}
		if input.RateLimitPerMinute <= 0 {
			input.RateLimitPerMinute = defaultLimitPerMin
		}
		if id == "" {
			id = randomID("plan")
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		previousNodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO subscription_plans
			(id,name,remark,enabled,total_bytes,cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,node_ids,selection_mode,include_internal_nodes,include_external_nodes,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now')) ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,remark=excluded.remark,enabled=excluded.enabled,total_bytes=excluded.total_bytes,
			cycle_type=excluded.cycle_type,cycle_day=excluded.cycle_day,rate_limit_enabled=excluded.rate_limit_enabled,
			rate_limit_per_minute=excluded.rate_limit_per_minute,node_ids='',selection_mode=excluded.selection_mode,
			include_internal_nodes=excluded.include_internal_nodes,include_external_nodes=excluded.include_external_nodes,updated_at=datetime('now')`,
			id, input.Name, input.Remark, boolToInt(input.Enabled), maxInt64(0, input.TotalBytes), input.CycleType, input.CycleDay,
			boolToInt(input.RateLimitEnabled), input.RateLimitPerMinute, "", input.SelectionMode, boolToInt(input.IncludeInternalNodes), boolToInt(input.IncludeExternalNodes))
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		containsInternal, containsExternal, err := replacePlanNodeRelations(r.Context(), db, tx, id, input.NodeIDs)
		if err != nil {
			response.Error(w, 400, err.Error())
			return
		}
		if input.SelectionMode == planSelectionExplicit {
			if _, err := tx.ExecContext(r.Context(), `UPDATE subscription_plans SET include_internal_nodes=?,include_external_nodes=? WHERE id=?`, boolToInt(containsInternal), boolToInt(containsExternal), id); err != nil {
				response.Error(w, 500, err.Error())
				return
			}
		}
		currentNodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		if err := reconcilequeue.EnqueueNodes(r.Context(), tx, append(previousNodeIDs, currentNodeIDs...), "plan policy changed"); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		plans, _ := loadPlans(r.Context(), db, id)
		response.OK(w, plans[0])
	case http.MethodDelete:
		if id == "" {
			response.Error(w, 400, "套餐 ID 不能为空")
			return
		}
		var count int
		_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE plan_id=?`, id).Scan(&count)
		if count > 0 {
			response.Error(w, 409, "套餐仍有订阅使用，无法删除")
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE plan_id=?`, id)
		result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_plans WHERE id=?`, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			response.Error(w, 404, "套餐不存在")
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.OK(w, map[string]bool{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func loadPlans(ctx context.Context, db *sql.DB, id string) ([]Plan, error) {
	where, args := "", []interface{}{}
	if id != "" {
		where, args = " WHERE p.id=?", append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT p.id,p.name,COALESCE(p.remark,''),p.enabled,p.total_bytes,p.cycle_type,p.cycle_day,
		p.rate_limit_enabled,p.rate_limit_per_minute,COALESCE(p.selection_mode,'explicit'),p.include_internal_nodes,p.include_external_nodes,
		p.created_at,p.updated_at,(SELECT COUNT(*) FROM subscription_subscriptions s WHERE s.plan_id=p.id)
		FROM subscription_plans p`+where+` ORDER BY p.updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	plans := []Plan{}
	for rows.Next() {
		var p Plan
		var enabled, rateEnabled, includeInternal, includeExternal int
		if err := rows.Scan(&p.ID, &p.Name, &p.Remark, &enabled, &p.TotalBytes, &p.CycleType, &p.CycleDay, &rateEnabled, &p.RateLimitPerMinute, &p.SelectionMode, &includeInternal, &includeExternal, &p.CreatedAt, &p.UpdatedAt, &p.SubscriptionCount); err != nil {
			return nil, err
		}
		p.Enabled, p.RateLimitEnabled = enabled == 1, rateEnabled == 1
		p.IncludeInternalNodes, p.IncludeExternalNodes = includeInternal == 1, includeExternal == 1
		p.SelectionMode = normalizePlanSelectionMode(p.SelectionMode)
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range plans {
		nodeRows, err := db.QueryContext(ctx, `SELECT node_id FROM subscription_plan_nodes WHERE plan_id=? ORDER BY created_at,node_id`, plans[i].ID)
		if err != nil {
			return nil, err
		}
		relations := []string{}
		for nodeRows.Next() {
			var nodeID string
			if err := nodeRows.Scan(&nodeID); err != nil {
				nodeRows.Close()
				return nil, err
			}
			relations = append(relations, nodeID)
		}
		nodeRows.Close()
		plans[i].NodeIDs = relations
	}
	return plans, nil
}

type subscriptionExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func replacePlanNodeRelations(ctx context.Context, db *sql.DB, tx *sql.Tx, planID string, nodeIDs []string) (bool, bool, error) {
	var executor subscriptionExecutor = db
	if tx != nil {
		executor = tx
	}
	if _, err := executor.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE plan_id=?`, planID); err != nil {
		return false, false, err
	}
	seen := map[string]struct{}{}
	containsInternal, containsExternal := false, false
	for _, rawID := range nodeIDs {
		nodeID := strings.TrimSpace(rawID)
		if nodeID == "" {
			continue
		}
		if _, duplicate := seen[nodeID]; duplicate {
			continue
		}
		seen[nodeID] = struct{}{}
		source := ""
		var exists int
		if err := executor.QueryRowContext(ctx, `SELECT 1 FROM managed_proxy_nodes WHERE id=?`, nodeID).Scan(&exists); err == nil {
			source = "internal"
		} else if err := executor.QueryRowContext(ctx, `SELECT 1 FROM subscription_nodes WHERE id=?`, nodeID).Scan(&exists); err == nil {
			source = "external"
		}
		if source == "" {
			return false, false, fmt.Errorf("节点 %s 不存在或已被删除", nodeID)
		}
		if _, err := executor.ExecContext(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES(?,?,?)`, planID, nodeID, source); err != nil {
			return false, false, err
		}
		containsInternal = containsInternal || source == "internal"
		containsExternal = containsExternal || source == "external"
	}
	return containsInternal, containsExternal, nil
}

func applyPlanToSubscription(ctx context.Context, db *sql.DB, sub *Subscription) {
	if sub != nil {
		sub.PlanEnabled = true
	}
	if sub == nil || strings.TrimSpace(sub.PlanID) == "" {
		return
	}
	plans, err := loadPlans(ctx, db, sub.PlanID)
	if err != nil || len(plans) == 0 {
		sub.PlanEnabled = false
		return
	}
	p := plans[0]
	sub.PlanEnabled = p.Enabled
	sub.TotalBytes = p.TotalBytes
	sub.CycleType = p.CycleType
	sub.CycleDay = p.CycleDay
	sub.ExpireAt = ""
	sub.CycleStart, sub.CycleEnd = planCycleWindow(time.Now().UTC(), p.CycleType, p.CycleDay)
	sub.RateLimitEnabled = p.RateLimitEnabled
	sub.RateLimitPerMinute = p.RateLimitPerMinute
	sub.NodeFilterIDs = append([]string(nil), p.NodeIDs...)
	sub.NodeSelectionMode = p.SelectionMode
	sub.IncludeInternalNodes = p.IncludeInternalNodes
	sub.IncludeExternalNodes = p.IncludeExternalNodes
}

func countPublishedSubscriptionNodes(ctx context.Context, db *sql.DB, sub Subscription) int {
	nodes, err := loadPublishedNodesForSubscription(ctx, db, sub)
	if err != nil {
		return 0
	}
	return len(nodes)
}

func loadPublishedNodesForSubscription(ctx context.Context, db *sql.DB, sub Subscription) ([]Node, error) {
	nodes := []Node{}
	if sub.IncludeInternalNodes {
		internal, err := loadManagedSubscriptionNodes(ctx, db, sub)
		if err != nil {
			return nil, err
		}
		if sub.PlanID != "" {
			internal = filterPlanNodesByIDsForSource(internal, sub.NodeFilterIDs, sub.NodeSelectionMode)
		}
		nodes = append(nodes, internal...)
	}
	if sub.IncludeExternalNodes {
		profileID := firstNonEmpty(sub.ProfileID, sub.ID)
		if sub.PlanID != "" {
			profileID = ""
		}
		external, err := loadNodes(ctx, db, profileID, true)
		if err != nil {
			return nil, err
		}
		if sub.PlanID != "" {
			external = filterPlanNodesByIDsForSource(external, sub.NodeFilterIDs, sub.NodeSelectionMode)
		} else {
			external = filterNodesByIDsForSource(external, sub.NodeFilterIDs)
		}
		nodes = append(nodes, external...)
	}
	nodes = filterEnabledPublishedNodes(nodes)
	nodes, err := filterNodesByAvailableHostQuota(ctx, db, nodes)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func filterEnabledPublishedNodes(nodes []Node) []Node {
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func filterNodesByAvailableHostQuota(ctx context.Context, db *sql.DB, nodes []Node) ([]Node, error) {
	serverIDs := []string{}
	seen := map[string]bool{}
	for _, node := range nodes {
		serverID := strings.TrimSpace(node.TrafficServerID)
		if serverID == "" || seen[serverID] {
			continue
		}
		seen[serverID] = true
		serverIDs = append(serverIDs, serverID)
	}
	if len(serverIDs) == 0 {
		return nodes, nil
	}
	quotaByServerID, err := loadServerTrafficQuotaStates(ctx, db, serverIDs)
	if err != nil {
		return nil, err
	}
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		serverID := strings.TrimSpace(node.TrafficServerID)
		if quotaByServerID[serverID].Exhausted {
			continue
		}
		filtered = append(filtered, node)
	}
	return filtered, nil
}

func loadServerTrafficQuotaStates(ctx context.Context, db *sql.DB, serverIDs []string) (map[string]serverTrafficQuota, error) {
	states := make(map[string]serverTrafficQuota, len(serverIDs))
	serverIDs = compactStringList(serverIDs)
	if len(serverIDs) == 0 {
		return states, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(serverIDs)), ",")
	args := make([]interface{}, 0, len(serverIDs))
	for _, id := range serverIDs {
		args = append(args, id)
	}
	queries := []string{
		`SELECT id, COALESCE(traffic_limit_bytes, 0), COALESCE(traffic_limit_mode, 'total'), COALESCE(cached_info, '{}') FROM server_accounts WHERE id IN (` + placeholders + `)`,
		`SELECT id, COALESCE(traffic_limit_bytes, 0), 'total', COALESCE(cached_info, '{}') FROM server_accounts WHERE id IN (` + placeholders + `)`,
	}
	var lastErr error
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "no such table") || strings.Contains(lower, "no such column") {
				lastErr = err
				continue
			}
			return nil, err
		}
		for rows.Next() {
			var serverID, limitMode, cachedInfo string
			var limitBytes int64
			if err := rows.Scan(&serverID, &limitBytes, &limitMode, &cachedInfo); err != nil {
				rows.Close()
				return nil, err
			}
			usedBytes := trafficUsedBytesFromCachedInfo(cachedInfo, limitMode)
			limitBytes = maxInt64(limitBytes, 0)
			states[serverID] = serverTrafficQuota{
				UsedBytes:  usedBytes,
				LimitBytes: limitBytes,
				Exhausted:  limitBytes > 0 && usedBytes >= limitBytes,
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		return states, nil
	}
	if lastErr != nil {
		return states, nil
	}
	return states, nil
}

func trafficUsedBytesFromCachedInfo(raw, mode string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return 0
	}
	var cached map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return 0
	}
	network, _ := cached["network"].(map[string]interface{})
	rxTotal := firstFloatValue(cached, "net_in_transfer", "net_rx_total", "rx_total_bytes")
	txTotal := firstFloatValue(cached, "net_out_transfer", "net_tx_total", "tx_total_bytes")
	if value := getFloatFromMap(network, "rx_total_bytes"); value > 0 {
		rxTotal = value
	}
	if value := getFloatFromMap(network, "tx_total_bytes"); value > 0 {
		txTotal = value
	}
	return trafficUsedBytesForMode(rxTotal, txTotal, mode)
}

func ensureDefaultNodeLibrary(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_profiles (
			id,name,remark,enabled,template_id,traffic_source,ownership,management,traffic_reporting,
			cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,updated_at
		) VALUES (?, '外部节点池', '系统统一外部节点池', 1, ?, 'manual', 'external', 'unmanaged', 'unavailable', 'none', 1, 0, ?, datetime('now'))`,
		defaultNodeLibrary, rawTemplateID, defaultLimitPerMin); err != nil {
		return fmt.Errorf("create default external node pool: %w", err)
	}
	// Older releases represented the node pool as an enabled public
	// subscription. Retire that anchor so it cannot leak a hidden public URL or
	// pollute subscription counts; nodes continue to reference the profile ID.
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_access_logs WHERE subscription_id=?`, defaultNodeLibrary); err != nil {
		return fmt.Errorf("delete legacy node pool access logs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_subscriptions WHERE id=?`, defaultNodeLibrary); err != nil {
		return fmt.Errorf("delete legacy node pool subscription: %w", err)
	}
	return tx.Commit()
}

func ensureBuiltins(ctx context.Context, db *sql.DB, overwrite bool) error {
	defaultTemplate := loadDefaultMihomoTemplate()
	templates := []Template{
		{ID: rawTemplateID, Name: "Raw URI List", Format: "raw", Content: "{{ raw_uri_list }}", Builtin: true, Description: "一行一个节点链接"},
		{ID: base64TemplateID, Name: "Base64 URI List", Format: "base64", Content: "{{ raw_uri_list }}", Builtin: true, Description: "v2rayN 常用 Base64 订阅"},
	}
	if strings.TrimSpace(defaultTemplate) != "" {
		templates = append([]Template{{ID: defaultTemplateID, Name: "默认 Mihomo/Clash YAML", Format: "clash", Content: defaultTemplate, Builtin: true, IsDefault: true, Description: "基于项目默认三网分流配置的 Mihomo 模板"}}, templates...)
	}
	for _, tpl := range templates {
		if overwrite {
			_, err := db.ExecContext(ctx, `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
				ON CONFLICT(id) DO UPDATE SET name = excluded.name, format = excluded.format, content = excluded.content, builtin = excluded.builtin, description = excluded.description, updated_at = datetime('now')`,
				tpl.ID, tpl.Name, tpl.Format, tpl.Content, boolToInt(tpl.Builtin), boolToInt(tpl.IsDefault), tpl.Description)
			if err != nil {
				return err
			}
			continue
		}
		_, err := db.ExecContext(ctx, `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=CASE WHEN subscription_templates.builtin=1 THEN excluded.name ELSE subscription_templates.name END,
				format=CASE WHEN subscription_templates.builtin=1 THEN excluded.format ELSE subscription_templates.format END,
				content=CASE WHEN subscription_templates.builtin=1 THEN excluded.content ELSE subscription_templates.content END,
				builtin=CASE WHEN subscription_templates.builtin=1 THEN 1 ELSE subscription_templates.builtin END,
				description=CASE WHEN subscription_templates.builtin=1 THEN excluded.description ELSE subscription_templates.description END,
				updated_at=CASE WHEN subscription_templates.builtin=1 THEN datetime('now') ELSE subscription_templates.updated_at END`,
			tpl.ID, tpl.Name, tpl.Format, tpl.Content, boolToInt(tpl.Builtin), boolToInt(tpl.IsDefault), tpl.Description)
		if err != nil {
			return err
		}
	}
	_, _ = db.ExecContext(ctx, `UPDATE subscription_templates SET is_default = CASE WHEN id = (SELECT default_template_id FROM subscription_settings WHERE id = 1) THEN 1 ELSE 0 END`)
	return nil
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var total, enabled, expired, exhausted, today int
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(enabled), 0) FROM subscription_subscriptions`).Scan(&total, &enabled)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(plan_id,'')='' AND expire_at IS NOT NULL AND expire_at != '' AND datetime(expire_at) < datetime('now')`).Scan(&expired)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_access_logs WHERE date(created_at) = date('now')`).Scan(&today)
	subs, _ := loadSubscriptions(r.Context(), db, "")
	for _, sub := range subs {
		if sub.Traffic.Total > 0 && sub.Traffic.Upload+sub.Traffic.Download >= sub.Traffic.Total {
			exhausted++
		}
	}
	response.OK(w, map[string]interface{}{"total": total, "enabled": enabled, "expired": expired, "exhausted": exhausted, "todayAccess": today})
}

func (s *Service) listSubscriptions(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	subs, err := loadSubscriptions(r.Context(), db, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, subs)
}

func (s *Service) listProfiles(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	items, err := loadProfiles(r.Context(), db, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, items)
}

func (s *Service) getProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	items, err := loadProfiles(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		response.Error(w, http.StatusNotFound, "node library not found")
		return
	}
	response.OK(w, items[0])
}

func (s *Service) createProfile(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input NodeLibrary
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "节点库名称不能为空")
		return
	}
	settings, _ := loadSettings(r.Context(), db)
	id := firstNonEmpty(input.ID, randomID("profile"))
	subInput := subscriptionFromProfile(input)
	subInput.Enabled = input.Enabled || !isExplicitFalse(r, "enabled")
	subInput.RateLimitEnabled = input.RateLimitEnabled || settings.DefaultRateLimitEnabled
	templateID := firstNonEmpty(input.TemplateID, settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	trafficSource := normalizeTrafficSource(input.TrafficSource)
	cycleType := normalizeCycleType(input.CycleType)
	cycleDay := input.CycleDay
	if cycleDay <= 0 {
		cycleDay = 1
	}
	refreshHours := input.UpstreamRefreshHours
	if refreshHours <= 0 {
		refreshHours = settings.DefaultRefreshHours
	}
	limitPerMin := input.RateLimitPerMinute
	if limitPerMin <= 0 {
		limitPerMin = settings.DefaultRateLimitPerMin
	}
	if err := upsertProfile(r.Context(), db, id, subInput, templateID, trafficSource, cycleType, cycleDay, limitPerMin); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := upsertDefaultUpstream(r.Context(), db, id, subInput, refreshHours); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, _ := loadProfiles(r.Context(), db, id)
	response.OK(w, firstProfile(items))
}

func (s *Service) updateProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input NodeLibrary
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "节点库名称不能为空")
		return
	}
	if !profileExists(r.Context(), db, id) {
		response.Error(w, http.StatusNotFound, "node library not found")
		return
	}
	subInput := subscriptionFromProfile(input)
	cycleDay := input.CycleDay
	if cycleDay <= 0 {
		cycleDay = 1
	}
	templateID := firstNonEmpty(input.TemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	trafficSource := normalizeTrafficSource(input.TrafficSource)
	cycleType := normalizeCycleType(input.CycleType)
	refreshHours := intDefault(input.UpstreamRefreshHours, defaultRefreshHours)
	limitPerMin := intDefault(input.RateLimitPerMinute, defaultLimitPerMin)
	if err := upsertProfile(r.Context(), db, id, subInput, templateID, trafficSource, cycleType, cycleDay, limitPerMin); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := upsertDefaultUpstream(r.Context(), db, id, subInput, refreshHours); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, _ := loadProfiles(r.Context(), db, id)
	response.OK(w, firstProfile(items))
}

func (s *Service) deleteProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if id == defaultNodeLibrary {
		response.Error(w, http.StatusConflict, "系统外部节点池不能删除")
		return
	}
	var nodeCount, linkCount int
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, id).Scan(&nodeCount)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ? AND id != COALESCE(profile_id, id)`, id).Scan(&linkCount)
	force := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	if (nodeCount > 0 || linkCount > 0) && !force {
		response.Error(w, http.StatusConflict, "节点库仍包含节点或对外订阅，不能删除")
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if force {
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_access_logs WHERE subscription_id IN (
			SELECT id FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ?
		)`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE source='external' AND node_id IN (SELECT id FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?)`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ?`, id)
	} else {
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE id = ? AND id = COALESCE(profile_id, id)`, id)
	}
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_upstreams WHERE profile_id = ?`, id)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_profiles WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true, "forced": force})
}

func (s *Service) refreshProfileUpstream(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := s.refreshUpstreamNow(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]bool{"refreshed": true})
}

func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	subs, err := loadSubscriptions(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(subs) == 0 {
		response.Error(w, http.StatusNotFound, "subscription not found")
		return
	}
	nodes, _ := loadNodes(r.Context(), db, firstNonEmpty(subs[0].ProfileID, id), true)
	response.OK(w, map[string]interface{}{"subscription": subs[0], "nodes": nodes})
}

func (s *Service) createSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input Subscription
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "订阅名称不能为空")
		return
	}
	if strings.TrimSpace(input.PlanID) == "" {
		response.Error(w, http.StatusBadRequest, "请选择套餐")
		return
	}
	if plans, err := loadPlans(r.Context(), db, input.PlanID); err != nil || len(plans) == 0 || !plans[0].Enabled {
		response.Error(w, http.StatusBadRequest, "所选套餐不存在或已停用")
		return
	}
	applyPlanToSubscription(r.Context(), db, &input)
	settings, _ := loadSettings(r.Context(), db)
	id := randomID("sub")
	profileID := defaultNodeLibrary
	token := randomToken()
	templateID := firstNonEmpty(input.TemplateID, settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	effectiveEnabled := input.Enabled || !isExplicitFalse(r, "enabled")
	input.Enabled = effectiveEnabled
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if !profileExists(r.Context(), tx, profileID) {
		library := Subscription{Name: "外部节点池", Remark: "系统统一外部节点池", Enabled: true}
		if err := upsertProfile(r.Context(), tx, profileID, library, rawTemplateID, "manual", "none", 1, defaultLimitPerMin); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO subscription_subscriptions (
		id, profile_id, plan_id, name, remark, enabled, public_token, vless_uuid, hysteria2_password, template_id, traffic_source, traffic_server_id,
		upstream_url, upstream_enabled, upstream_refresh_hours, total_bytes, manual_upload_bytes,
		manual_download_bytes, expire_at, cycle_type, cycle_day, cycle_start, cycle_end,
		rate_limit_enabled, rate_limit_per_minute, node_filter_ids, include_internal_nodes, include_external_nodes, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, profileID, input.PlanID, input.Name, input.Remark, boolToInt(effectiveEnabled), token, randomUUID(), randomCredential(), templateID, "panel", nil,
		nil, 0, defaultRefreshHours, 0, 0,
		0, nil, "none", 1, nil, nil,
		0, 0, "", 0, 0)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, input.PlanID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription created") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) updateSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input Subscription
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "订阅名称不能为空")
		return
	}
	if strings.TrimSpace(input.PlanID) == "" {
		response.Error(w, http.StatusBadRequest, "请选择套餐")
		return
	}
	if plans, err := loadPlans(r.Context(), db, input.PlanID); err != nil || len(plans) == 0 {
		response.Error(w, http.StatusBadRequest, "所选套餐不存在")
		return
	}
	applyPlanToSubscription(r.Context(), db, &input)
	profileID := firstNonEmpty(profileIDForSubscription(r.Context(), db, id), defaultNodeLibrary)
	templateID := firstNonEmpty(input.TemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var previousPlanID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&previousPlanID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions SET
		profile_id = ?, plan_id = ?, name = ?, remark = ?, enabled = ?, template_id = ?, traffic_source = ?, traffic_server_id = ?,
		upstream_url = ?, upstream_enabled = ?, upstream_refresh_hours = ?, total_bytes = ?,
		manual_upload_bytes = ?, manual_download_bytes = ?, expire_at = ?, cycle_type = ?, cycle_day = ?,
		cycle_start = ?, cycle_end = ?, rate_limit_enabled = ?, rate_limit_per_minute = ?, node_filter_ids = ?, include_internal_nodes = ?, include_external_nodes = ?, updated_at = datetime('now')
		WHERE id = ?`,
		profileID, input.PlanID, input.Name, input.Remark, boolToInt(input.Enabled), templateID, "panel", nil,
		nil, 0, defaultRefreshHours, 0,
		0, 0, nil, "none", 1,
		nil, nil, 0, 0, "", 0, 0, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := reconcilequeue.NodeIDsForPlans(r.Context(), tx, previousPlanID, input.PlanID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription policy changed") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) setSubscriptionEnabled(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		response.Error(w, http.StatusBadRequest, "enabled 不能为空")
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions SET enabled=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription enabled state changed") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) deleteSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if id == defaultNodeLibrary {
		response.Error(w, http.StatusConflict, "系统外部节点池锚点不能删除")
		return
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM subscription_subscriptions WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_access_logs WHERE subscription_id = ?`, id)
	// Remove quota and Agent replay/audit rows explicitly so legacy databases
	// created before foreign-key enforcement cannot retain orphaned state.
	for _, statement := range []string{
		`DELETE FROM subscription_usage_reports WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_report_keys WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_hourly WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_cycles WHERE subscription_id=?`,
		`DELETE FROM subscription_cycle_state WHERE subscription_id=?`,
	} {
		if _, err := tx.ExecContext(r.Context(), statement, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription deleted"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func upsertProfile(ctx context.Context, executor subscriptionExecutor, id string, input Subscription, templateID, trafficSource, cycleType string, cycleDay, limitPerMin int) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("profile id is required")
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO subscription_profiles (
			id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			remark = excluded.remark,
			enabled = excluded.enabled,
			template_id = excluded.template_id,
			traffic_source = excluded.traffic_source,
			traffic_server_id = excluded.traffic_server_id,
			total_bytes = excluded.total_bytes,
			manual_upload_bytes = excluded.manual_upload_bytes,
			manual_download_bytes = excluded.manual_download_bytes,
			expire_at = excluded.expire_at,
			cycle_type = excluded.cycle_type,
			cycle_day = excluded.cycle_day,
			cycle_start = excluded.cycle_start,
			cycle_end = excluded.cycle_end,
			baseline_upload_bytes = excluded.baseline_upload_bytes,
			baseline_download_bytes = excluded.baseline_download_bytes,
			rate_limit_enabled = excluded.rate_limit_enabled,
			rate_limit_per_minute = excluded.rate_limit_per_minute,
			updated_at = datetime('now')`,
		id, input.Name, input.Remark, boolToInt(input.Enabled), templateID, trafficSource, nullString(input.TrafficServerID),
		input.TotalBytes, input.ManualUploadBytes, input.ManualDownloadBytes, nullString(input.ExpireAt), cycleType,
		cycleDay, nullString(input.CycleStart), nullString(input.CycleEnd), input.BaselineUploadBytes, input.BaselineDownloadBytes,
		boolToInt(input.RateLimitEnabled), limitPerMin)
	if err != nil {
		return fmt.Errorf("upsert subscription profile: %w", err)
	}
	return nil
}

func upsertDefaultUpstream(ctx context.Context, executor subscriptionExecutor, profileID string, input Subscription, refreshHours int) error {
	upstreamURL := strings.TrimSpace(input.UpstreamURL)
	upstreamID := "up_" + profileID
	if upstreamURL == "" {
		if _, err := executor.ExecContext(ctx, `DELETE FROM subscription_upstreams WHERE id = ?`, upstreamID); err != nil {
			return fmt.Errorf("delete default upstream: %w", err)
		}
		return nil
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO subscription_upstreams (
			id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, updated_at
		) VALUES (?, ?, '默认上游', ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			profile_id = excluded.profile_id,
			url = excluded.url,
			enabled = excluded.enabled,
			refresh_hours = excluded.refresh_hours,
			status = excluded.status,
			last_error = excluded.last_error,
			last_refresh_at = excluded.last_refresh_at,
			updated_at = datetime('now')`,
		upstreamID, profileID, upstreamURL, boolToInt(input.UpstreamEnabled), refreshHours, input.UpstreamStatus, input.UpstreamLastError, nullString(input.UpstreamLastRefreshAt))
	if err != nil {
		return fmt.Errorf("upsert default upstream: %w", err)
	}
	return nil
}

func profileIDForSubscription(ctx context.Context, db *sql.DB, id string) string {
	var profileID string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(profile_id, '') FROM subscription_subscriptions WHERE id = ?`, id).Scan(&profileID)
	return profileID
}

func profileExists(ctx context.Context, executor subscriptionExecutor, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	var count int
	_ = executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_profiles WHERE id = ?`, id).Scan(&count)
	return count > 0
}

func (s *Service) resetToken(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	token := randomToken()
	vlessUUID := randomUUID()
	hysteria2Password := randomCredential()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions
		SET public_token=?,vless_uuid=?,hysteria2_password=?,updated_at=datetime('now')
		WHERE id=?`, token, vlessUUID, hysteria2Password, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "无法解析订阅节点范围")
		return
	}
	if err := reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription credentials rotated"); err != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点凭据同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	syncStatus := "not_required"
	if len(nodeIDs) > 0 {
		syncStatus = "pending"
	}
	response.OK(w, map[string]interface{}{
		"public_token":        token,
		"credentials_rotated": true,
		"nodes_queued":        len(nodeIDs),
		"runtime_sync_status": syncStatus,
	})
}

// rotateAddress rotates only the public subscription URL token. Client node
// credentials (VLESS UUID, Hysteria2 password) are left untouched, so already
// configured clients keep working; no runtime reconciliation is enqueued.
func (s *Service) rotateAddress(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	token := randomToken()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions
		SET public_token=?,updated_at=datetime('now')
		WHERE id=?`, token, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"public_token":        token,
		"credentials_rotated": false,
		"nodes_queued":        0,
		"runtime_sync_status": "not_required",
	})
}

// nodeIDsForSubscription keeps credential rotation compatible with legacy
// subscriptions that predate mandatory plans. New subscriptions always use
// the plan path; legacy filters are treated as managed-node IDs when present.
func nodeIDsForSubscription(ctx context.Context, tx *sql.Tx, subscriptionID, planID string) ([]string, error) {
	if strings.TrimSpace(planID) != "" {
		return reconcilequeue.NodeIDsForPlan(ctx, tx, planID)
	}
	var filters string
	var includeInternal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(node_filter_ids,''),COALESCE(include_internal_nodes,1) FROM subscription_subscriptions WHERE id=?`, subscriptionID).Scan(&filters, &includeInternal); err != nil {
		return nil, err
	}
	if ids := decodeNodeFilterIDs(filters); len(ids) > 0 {
		return ids, nil
	}
	if includeInternal == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM managed_proxy_nodes WHERE enabled=1 ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) refreshUpstream(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := s.refreshUpstreamNow(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]bool{"refreshed": true})
}

func (s *Service) refreshUpstreamNow(ctx context.Context, db *sql.DB, id string) error {
	profileID := firstNonEmpty(profileIDForSubscription(ctx, db, id), id)
	var upstreamURL string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(url, '') FROM subscription_upstreams WHERE profile_id = ? AND enabled = 1 ORDER BY updated_at DESC LIMIT 1`, profileID).Scan(&upstreamURL)
	if err == sql.ErrNoRows {
		err = db.QueryRowContext(ctx, `SELECT COALESCE(upstream_url, '') FROM subscription_subscriptions WHERE id = ?`, id).Scan(&upstreamURL)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(upstreamURL) == "" {
		return fmt.Errorf("未配置托管源 URL")
	}
	bodyText, userinfo, err := s.fetchManagedSource(ctx, upstreamURL)
	if err != nil {
		msg := err.Error()
		_, _ = db.ExecContext(ctx, `UPDATE subscription_subscriptions SET upstream_status = 'failed', upstream_last_error = ?, updated_at = datetime('now') WHERE id = ?`, msg, id)
		_, _ = db.ExecContext(ctx, `UPDATE subscription_upstreams SET status = 'failed', last_error = ?, updated_at = datetime('now') WHERE profile_id = ?`, msg, profileID)
		return err
	}
	nodes := parseImportText(bodyText)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mergeManagedNodes(ctx, tx, profileID, nodes); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_subscriptions SET upstream_status = 'ok', upstream_last_error = '', upstream_last_refresh_at = datetime('now'), upstream_userinfo = ?, updated_at = datetime('now') WHERE id = ?`, userinfo, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_upstreams SET status = 'ok', last_error = '', last_refresh_at = datetime('now'), userinfo = ?, updated_at = datetime('now') WHERE profile_id = ?`, userinfo, profileID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) fetchManagedSource(ctx context.Context, sourceURL string) (string, string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", "", fmt.Errorf("托管源 URL 不能为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "API-Monitor-Subscription/1.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("托管源返回 HTTP %d", resp.StatusCode)
	}
	return string(body), resp.Header.Get("Subscription-Userinfo"), nil
}

func (s *Service) listNodes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	filterID := r.URL.Query().Get("subscription_id")
	if filterID != "" {
		filterID = firstNonEmpty(profileIDForSubscription(r.Context(), db, filterID), filterID)
	}
	nodes, err := loadNodes(r.Context(), db, filterID, true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, nodes)
}

func (s *Service) updateNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var node Node
	if !decodeJSON(w, r, &node) {
		return
	}
	rawEnc, _ := secure.SecureEncrypt(node.Raw)
	cfgEnc, _ := secure.SecureEncrypt(node.ConfigJSON)
	// subscription_nodes are imported external nodes by definition. Internal
	// nodes live in managed_proxy_nodes and cannot be converted by editing.
	ownership, management, reporting, trafficServerID := "external", "unmanaged", "unavailable", ""
	_, err := db.ExecContext(r.Context(), `UPDATE subscription_nodes SET name = ?, type = ?, server = ?, port = ?, country_code = ?, location = ?, tags = ?, traffic_server_id = ?, ownership = ?, management = ?, traffic_reporting = ?, enabled = ?, stable = ?, sort_order = ?, raw_encrypted = CASE WHEN ? = '' THEN raw_encrypted ELSE ? END, config_encrypted = CASE WHEN ? = '' THEN config_encrypted ELSE ? END, updated_at = datetime('now') WHERE id = ?`,
		node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(trafficServerID), ownership, management, reporting, boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, node.Raw, rawEnc, node.ConfigJSON, cfgEnc, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func normalizeNodeOwnership(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "self") {
		return "self"
	}
	return "external"
}

func normalizeNodeManagement(value, ownership string) string {
	if ownership == "self" && strings.EqualFold(strings.TrimSpace(value), "agent") {
		return "agent"
	}
	return "unmanaged"
}

func normalizeNodeTrafficReporting(value, management string) string {
	if management == "agent" && strings.EqualFold(strings.TrimSpace(value), "trusted") {
		return "trusted"
	}
	return "unavailable"
}

func (s *Service) deleteNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE node_id=? AND source='external'`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_nodes WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "外部节点不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func (s *Service) reorderNodes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var payload struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	for i, id := range payload.IDs {
		_, _ = tx.ExecContext(r.Context(), `UPDATE subscription_nodes SET sort_order = ?, updated_at = datetime('now') WHERE id = ?`, i+1, id)
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func (s *Service) importPreview(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Text      string `json:"text"`
		SourceURL string `json:"source_url"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	text := payload.Text
	if strings.TrimSpace(payload.SourceURL) != "" {
		fetched, _, err := s.fetchManagedSource(r.Context(), payload.SourceURL)
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		text = fetched
	}
	response.OK(w, parseImportText(text))
}

func (s *Service) importCommit(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var payload struct {
		SubscriptionID string `json:"subscription_id"`
		Text           string `json:"text"`
		SourceURL      string `json:"source_url"`
		Nodes          []Node `json:"nodes"`
		Replace        bool   `json:"replace"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.SubscriptionID = firstNonEmpty(strings.TrimSpace(payload.SubscriptionID), defaultNodeLibrary)
	profileID := firstNonEmpty(profileIDForSubscription(r.Context(), db, payload.SubscriptionID), payload.SubscriptionID)
	sourceURL := strings.TrimSpace(payload.SourceURL)
	text := payload.Text
	var userinfo string
	if sourceURL != "" {
		fetched, header, err := s.fetchManagedSource(r.Context(), sourceURL)
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		text = fetched
		userinfo = header
	}
	nodes := payload.Nodes
	if len(nodes) == 0 {
		nodes = parseImportText(text)
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var maxOrder int
	_ = tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sort_order), 0) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, profileID).Scan(&maxOrder)
	source := "manual"
	if sourceURL != "" {
		source = "managed"
	}
	for i := range nodes {
		nodes[i].SubscriptionID = profileID
		nodes[i].ProfileID = profileID
		nodes[i].Source = source
		nodes[i].SortOrder = maxOrder + i + 1
	}
	if payload.Replace {
		if err := replaceImportedNodes(r.Context(), tx, profileID, source, nodes); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		for i := range nodes {
			if err := insertNode(r.Context(), tx, nodes[i]); err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if sourceURL != "" {
		refreshHours := defaultRefreshHours
		_ = tx.QueryRowContext(r.Context(), `SELECT COALESCE(default_refresh_hours, 24) FROM subscription_settings WHERE id = 1`).Scan(&refreshHours)
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO subscription_upstreams (
				id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, userinfo, updated_at
			) VALUES (?, ?, '托管源', ?, 1, ?, 'ok', '', datetime('now'), ?, datetime('now'))
			ON CONFLICT(id) DO UPDATE SET
				profile_id = excluded.profile_id,
				name = excluded.name,
				url = excluded.url,
				enabled = 1,
				refresh_hours = excluded.refresh_hours,
				status = 'ok',
				last_error = '',
				last_refresh_at = datetime('now'),
				userinfo = excluded.userinfo,
				updated_at = datetime('now')`, "up_"+profileID, profileID, sourceURL, refreshHours, userinfo); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"imported": len(nodes)})
}

func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	items, err := loadTemplates(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, items)
}

func (s *Service) createTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var tpl Template
	if !decodeJSON(w, r, &tpl) {
		return
	}
	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Format = normalizeTemplateFormat(tpl.Format)
	if tpl.Name == "" || strings.TrimSpace(tpl.Content) == "" {
		response.Error(w, http.StatusBadRequest, "模板名称和内容不能为空")
		return
	}
	if err := validateTemplateDefinition(tpl.Format, tpl.Content); err != nil {
		response.Error(w, http.StatusBadRequest, "模板配置无效: "+err.Error())
		return
	}
	if tpl.ID == "" {
		tpl.ID = randomID("tpl")
	}
	_, err := db.ExecContext(r.Context(), `INSERT INTO subscription_templates (id, name, format, content, builtin, is_default, description) VALUES (?, ?, ?, ?, 0, 0, ?)`,
		tpl.ID, tpl.Name, tpl.Format, tpl.Content, tpl.Description)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	tpl.Valid = true
	response.OK(w, tpl)
}

func (s *Service) updateTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var tpl Template
	if !decodeJSON(w, r, &tpl) {
		return
	}
	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Format = normalizeTemplateFormat(tpl.Format)
	if tpl.Name == "" || strings.TrimSpace(tpl.Content) == "" {
		response.Error(w, http.StatusBadRequest, "模板名称和内容不能为空")
		return
	}
	if err := validateTemplateDefinition(tpl.Format, tpl.Content); err != nil {
		response.Error(w, http.StatusBadRequest, "模板配置无效: "+err.Error())
		return
	}
	result, err := db.ExecContext(r.Context(), `UPDATE subscription_templates SET name = ?, format = ?, content = ?, description = ?, updated_at = datetime('now') WHERE id = ?`,
		tpl.Name, tpl.Format, tpl.Content, tpl.Description, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "模板不存在")
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func (s *Service) deleteTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var builtin int
	if err := db.QueryRowContext(r.Context(), `SELECT builtin FROM subscription_templates WHERE id=?`, id).Scan(&builtin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "模板不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if builtin == 1 {
		response.Error(w, http.StatusConflict, "内置模板不能删除")
		return
	}
	var profiles, subscriptions, defaults int
	for _, dependency := range []struct {
		query string
		count *int
	}{
		{`SELECT COUNT(*) FROM subscription_profiles WHERE template_id=?`, &profiles},
		{`SELECT COUNT(*) FROM subscription_subscriptions WHERE template_id=?`, &subscriptions},
		{`SELECT COUNT(*) FROM subscription_settings WHERE default_template_id=?`, &defaults},
	} {
		if err := db.QueryRowContext(r.Context(), dependency.query, id).Scan(dependency.count); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if profiles+subscriptions+defaults > 0 {
		response.JSON(w, http.StatusConflict, map[string]interface{}{"success": false, "error": "模板仍被引用，请先更换关联模板", "dependencies": map[string]int{"profiles": profiles, "subscriptions": subscriptions, "defaults": defaults}})
		return
	}
	result, err := db.ExecContext(r.Context(), `DELETE FROM subscription_templates WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "模板不存在")
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func (s *Service) setDefaultTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := validateTemplateReference(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE subscription_settings SET default_template_id = ?, updated_at = datetime('now') WHERE id = 1`, id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE subscription_templates SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END`, id)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func (s *Service) listLogs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	limit := intDefault(queryInt(r, "limit"), 200)
	rows, err := db.QueryContext(r.Context(), `SELECT id, subscription_id, public_token, ip_address, user_agent, format, success, status_code, COALESCE(error_message, ''), node_count, upload_bytes, download_bytes, total_bytes, expire_at, created_at FROM subscription_access_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, status, nodeCount int
		var subID, token, ip, ua, format, errMsg, created string
		var success int
		var up, down, total, expire int64
		_ = rows.Scan(&id, &subID, &token, &ip, &ua, &format, &success, &status, &errMsg, &nodeCount, &up, &down, &total, &expire, &created)
		items = append(items, map[string]interface{}{"id": id, "subscription_id": subID, "public_token": token, "ip_address": ip, "user_agent": ua, "format": format, "success": success == 1, "status_code": status, "error_message": errMsg, "node_count": nodeCount, "upload_bytes": up, "download_bytes": down, "total_bytes": total, "expire_at": expire, "created_at": created})
	}
	response.OK(w, items)
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	settings, err := loadSettings(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var settings Settings
	if !decodeJSON(w, r, &settings) {
		return
	}
	settings.DefaultTemplateID = firstNonEmpty(settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, settings.DefaultTemplateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err := db.ExecContext(r.Context(), `UPDATE subscription_settings SET default_template_id = ?, default_rate_limit_enabled = ?, default_rate_limit_per_minute = ?, default_refresh_hours = ?, geoip_enabled = ?, updated_at = datetime('now') WHERE id = 1`,
		settings.DefaultTemplateID, boolToInt(settings.DefaultRateLimitEnabled), intDefault(settings.DefaultRateLimitPerMin, defaultLimitPerMin), intDefault(settings.DefaultRefreshHours, defaultRefreshHours), boolToInt(settings.GeoIPEnabled))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) listServers(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT id, name, host, COALESCE(resolved_country, ''), COALESCE(country, ''), COALESCE(traffic_limit_bytes, 0), COALESCE(cached_info, '{}'), COALESCE(status,'unknown'), COALESCE(last_check_time,'') FROM server_accounts ORDER BY order_index ASC, created_at DESC`)
	if err != nil {
		response.OK(w, []map[string]interface{}{})
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, name, host, location, country, cached, status, lastSeen string
		var limit int64
		if err := rows.Scan(&id, &name, &host, &location, &country, &limit, &cached, &status, &lastSeen); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		info := map[string]interface{}{}
		_ = json.Unmarshal([]byte(cached), &info)
		platform := firstNonEmpty(jsonString(info, "platform"), jsonString(info, "os"))
		platformVersion := firstNonEmpty(jsonString(info, "platform_version"), jsonString(info, "platformVersion"))
		if !isLinuxPlatform(platform, platformVersion) {
			continue
		}
		items = append(items, map[string]interface{}{"id": id, "name": name, "host": host, "location": firstNonEmpty(jsonString(info, "location"), jsonString(info, "region"), location), "country_code": firstNonEmpty(jsonString(info, "country_code"), jsonString(info, "country"), country, location), "uptime": info["uptime"], "traffic_limit_bytes": limit, "status": status, "last_seen": lastSeen, "platform": platform, "platform_version": platformVersion, "agent_version": firstNonEmpty(jsonString(info, "agent_version"), jsonString(info, "version"))})
	}
	response.OK(w, items)
}

func jsonString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func isLinuxPlatform(platform, platformVersion string) bool {
	value := strings.ToLower(strings.TrimSpace(platform + " " + platformVersion))
	if value == "" {
		// Older Agent records did not persist a platform. Keep them manageable;
		// explicit non-Linux platforms below are still rejected.
		return true
	}
	if strings.Contains(value, "windows") || strings.Contains(value, "darwin") || strings.Contains(value, "macos") || strings.Contains(value, "freebsd") {
		return false
	}
	for _, marker := range []string{"linux", "ubuntu", "debian", "centos", "rhel", "red hat", "fedora", "rocky", "alma", "alpine", "arch", "opensuse", "sles", "oracle linux", "amzn", "amazon linux"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (s *Service) exportAll(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	subs, _ := loadSubscriptions(r.Context(), db, "")
	nodes, _ := loadNodes(r.Context(), db, "", true)
	templates, _ := loadTemplates(r.Context(), db)
	response.OK(w, map[string]interface{}{"type": "api-monitor-subscription-backup", "version": 1, "exportedAt": time.Now().UTC().Format(time.RFC3339), "subscriptions": subs, "nodes": nodes, "templates": templates})
}

func (s *Service) servePublicSubscription(w http.ResponseWriter, r *http.Request) {
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sub/"), "/")
	format := r.URL.Query().Get("format")
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	statusCode := http.StatusOK
	success := false
	errMsg := ""
	nodeCount := 0
	var sub Subscription
	var traffic TrafficInfo
	defer func() {
		s.logAccess(r.Context(), db, sub.ID, token, clientIP(r), r.UserAgent(), format, success, statusCode, errMsg, nodeCount, traffic)
	}()

	subs, err := loadSubscriptionByToken(r.Context(), db, token)
	if err != nil || len(subs) == 0 {
		statusCode = http.StatusNotFound
		errMsg = "subscription not found"
		response.Error(w, statusCode, errMsg)
		return
	}
	sub = subs[0]
	if !sub.Enabled || !sub.PlanEnabled {
		statusCode = http.StatusForbidden
		errMsg = "subscription or plan disabled"
		response.Error(w, statusCode, errMsg)
		return
	}
	if sub.RateLimitEnabled && isRateLimited(r.Context(), db, sub.ID, clientIP(r), sub.RateLimitPerMinute) {
		statusCode = http.StatusTooManyRequests
		errMsg = "rate limited"
		response.Error(w, statusCode, errMsg)
		return
	}
	traffic = sub.Traffic
	nodes := []Node{}
	renderBlocked := traffic.Status == "expired" || traffic.Status == "exhausted"
	if !renderBlocked {
		nodes, err = loadPublishedNodesForSubscription(r.Context(), db, sub)
		if err != nil {
			statusCode = http.StatusInternalServerError
			errMsg = "load subscription nodes: " + err.Error()
			response.Error(w, statusCode, errMsg)
			return
		}
	}
	if format == "" {
		format = subscriptionFormatFromUA(r.UserAgent())
	}
	if format == "" {
		format = templateFormat(r.Context(), db, sub.TemplateID)
	}
	showInfoPage := wantsSubscriptionInfoPage(format, r)
	if showInfoPage {
		format = "info"
		nodeCount = len(nodes)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderSubscriptionInfoPage(sub, traffic, nodeCount, subscriptionRequestURL(r))))
		success = true
		return
	}
	body, contentType, err := renderOutput(r.Context(), db, sub, nodes, format, renderBlocked)
	if err != nil {
		statusCode = http.StatusInternalServerError
		errMsg = err.Error()
		response.Error(w, statusCode, errMsg)
		return
	}
	nodeCount = len(nodes)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": subscriptionOutputFilename(sub)}))
	w.Header().Set("Profile-Title", subscriptionProfileTitle(sub))
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", traffic.Upload, traffic.Download, traffic.Total, traffic.Expire))
	w.Header().Set("Profile-Update-Interval", "12")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
	success = true
}

// subscriptionRequestURL reconstructs the absolute subscription URL for the
// current request, honoring the X-Forwarded-Proto set by reverse proxies.
func subscriptionRequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded == "https" || forwarded == "http" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host + r.URL.Path
}

func loadManagedSubscriptionNodes(ctx context.Context, db *sql.DB, sub Subscription) ([]Node, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,server_id,name,protocol,public_host,assigned_port,client_uri_encrypted,COALESCE(access_mode,'direct'),COALESCE(preferred_address_id,''),COALESCE(connect_address,''),COALESCE(connect_port,0),COALESCE(tunnel_hostname,''),COALESCE(stable,0),created_at,updated_at FROM managed_proxy_nodes WHERE enabled=1 AND publishable=1 AND apply_status='running' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type managedRow struct {
		id, serverID, name, protocol, host, encrypted, accessMode, preferredID, connectAddress, tunnelHostname, createdAt, updatedAt string
		port, connectPort, stable                                                                                                    int
	}
	managedRows := []managedRow{}
	for rows.Next() {
		var item managedRow
		if err := rows.Scan(&item.id, &item.serverID, &item.name, &item.protocol, &item.host, &item.port, &item.encrypted, &item.accessMode, &item.preferredID, &item.connectAddress, &item.connectPort, &item.tunnelHostname, &item.stable, &item.createdAt, &item.updatedAt); err != nil {
			return nil, err
		}
		managedRows = append(managedRows, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(managedRows))
	for _, item := range managedRows {
		raw := secure.SecureDecrypt(item.encrypted)
		raw = bindSubscriptionCredential(raw, item.protocol, sub)
		if item.accessMode == "cloudflare_tunnel" {
			address, port := strings.TrimSpace(item.connectAddress), item.connectPort
			if address == "" && item.preferredID != "" {
				_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE id=? AND enabled=1`, item.preferredID).Scan(&address, &port)
			}
			if address == "" {
				_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE enabled=1 AND is_default=1 ORDER BY sort_order ASC,created_at ASC LIMIT 1`).Scan(&address, &port)
			}
			if address == "" {
				address = item.tunnelHostname
			}
			if port == 0 {
				port = 443
			}
			raw = replacePublishedURIAddress(raw, address, port)
		}
		node := parseURI(raw, len(nodes)+1)
		node.ID = item.id
		node.Name = item.name
		node.Server = item.host
		node.Port = item.port
		node.TrafficServerID = item.serverID
		node.Raw = raw
		node.Enabled = true
		// A running managed node is publishable, but that alone is not evidence
		// that it belongs in the operator-curated stable group.
		node.Stable = item.stable == 1
		node.Ownership = "self"
		node.Management = "agent"
		node.TrafficReporting = "trusted"
		node.Source = "internal"
		node.CreatedAt = item.createdAt
		node.UpdatedAt = item.updatedAt
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func bindSubscriptionCredential(raw, protocol string, sub Subscription) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "vless-reality", "vless-ws-tunnel", "vless":
		if strings.TrimSpace(sub.VLESSUUID) == "" {
			return raw
		}
		parsed.User = url.User(sub.VLESSUUID)
	case "hysteria2", "hy2":
		if strings.TrimSpace(sub.Hysteria2Password) == "" {
			return raw
		}
		parsed.User = url.User(sub.Hysteria2Password)
	case "socks", "http":
		if strings.TrimSpace(sub.VLESSUUID) == "" || strings.TrimSpace(sub.Hysteria2Password) == "" {
			return raw
		}
		parsed.User = url.UserPassword(sub.VLESSUUID, sub.Hysteria2Password)
	}
	return parsed.String()
}

func replacePublishedURIAddress(raw, address string, port int) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(address) == "" || port < 1 || port > 65535 {
		return raw
	}
	parsed.Host = net.JoinHostPort(strings.TrimSpace(address), strconv.Itoa(port))
	return parsed.String()
}

func ensureUniquePublishedNodeNames(nodes []Node) []Node {
	seen := make(map[string]int, len(nodes))
	for i := range nodes {
		base := strings.TrimSpace(nodes[i].Name)
		if base == "" {
			base = firstNonEmpty(nodes[i].Server, "未命名节点")
		}
		key := strings.ToLower(base)
		seen[key]++
		name := base
		if seen[key] > 1 {
			name = fmt.Sprintf("%s · %d", base, seen[key])
		}
		if nodes[i].Name == name {
			continue
		}
		nodes[i].Name = name
		if parsed, err := url.Parse(nodes[i].Raw); err == nil && parsed.Scheme != "" {
			parsed.Fragment = name
			nodes[i].Raw = parsed.String()
		}
		if strings.TrimSpace(nodes[i].ConfigJSON) != "" {
			var config map[string]interface{}
			if json.Unmarshal([]byte(nodes[i].ConfigJSON), &config) == nil {
				config["name"] = name
				if encoded, err := json.Marshal(config); err == nil {
					nodes[i].ConfigJSON = string(encoded)
				}
			}
		}
	}
	return nodes
}

func filterNodesByIDsForSource(nodes []Node, ids []string) []Node {
	if len(ids) == 0 {
		return nodes
	}
	available := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		available[node.ID] = struct{}{}
	}
	selected := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := available[id]; ok {
			selected = append(selected, id)
		}
	}
	if len(selected) == 0 {
		return []Node{}
	}
	return filterNodesByIDs(nodes, selected)
}

func normalizePlanSelectionMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), planSelectionAll) {
		return planSelectionAll
	}
	return planSelectionExplicit
}

func filterPlanNodesByIDsForSource(nodes []Node, ids []string, selectionMode string) []Node {
	if normalizePlanSelectionMode(selectionMode) == planSelectionAll {
		return nodes
	}
	if len(ids) == 0 {
		return []Node{}
	}
	return filterNodesByIDsForSource(nodes, ids)
}

func subscriptionProfileTitle(sub Subscription) string {
	title := strings.TrimSpace(sub.Name)
	if title == "" {
		title = strings.TrimSpace(sub.ID)
	}
	if title == "" {
		title = "subscription"
	}
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(title))
}

func subscriptionOutputFilename(sub Subscription) string {
	name := strings.TrimSpace(sub.Name)
	if name == "" {
		name = strings.TrimSpace(sub.ID)
	}
	if name == "" {
		name = "subscription"
	}

	replacer := strings.NewReplacer(
		`"`, "",
		`\`, "-",
		"/", "-",
		":", "-",
		"*", "-",
		"?", "",
		"<", "",
		">", "",
		"|", "-",
		"\r", " ",
		"\n", " ",
		"\t", " ",
	)
	name = strings.Join(strings.Fields(replacer.Replace(name)), " ")
	if name == "" {
		name = "subscription"
	}
	return name
}

func loadProfiles(ctx context.Context, db *sql.DB, id string) ([]NodeLibrary, error) {
	where := ""
	args := []interface{}{}
	if id != "" {
		where = "WHERE p.id = ?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT
			p.id, p.name, COALESCE(p.remark, ''), p.enabled, COALESCE(p.template_id, ''),
			COALESCE(p.traffic_source, 'manual'), COALESCE(p.traffic_server_id, ''),
			COALESCE(u.url, ''), COALESCE(u.enabled, 0), COALESCE(u.refresh_hours, 24),
			COALESCE(u.status, ''), COALESCE(u.last_error, ''), COALESCE(u.last_refresh_at, ''), COALESCE(u.userinfo, ''),
			p.total_bytes, p.manual_upload_bytes, p.manual_download_bytes, COALESCE(p.expire_at, ''),
			COALESCE(p.cycle_type, 'none'), COALESCE(p.cycle_day, 1), COALESCE(p.cycle_start, ''), COALESCE(p.cycle_end, ''),
			p.baseline_upload_bytes, p.baseline_download_bytes, p.rate_limit_enabled, COALESCE(p.rate_limit_per_minute, 30),
			COALESCE(p.node_filter_tags, ''), COALESCE(p.sort_order, 0), p.created_at, p.updated_at
		FROM subscription_profiles p
		LEFT JOIN subscription_upstreams u ON u.id = (
			SELECT id FROM subscription_upstreams
			WHERE profile_id = p.id
			ORDER BY updated_at DESC, created_at DESC
			LIMIT 1
		)
		`+where+`
		ORDER BY p.sort_order ASC, p.updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []NodeLibrary{}
	for rows.Next() {
		var item NodeLibrary
		var enabled, upstreamEnabled, rateEnabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Remark, &enabled, &item.TemplateID, &item.TrafficSource, &item.TrafficServerID, &item.UpstreamURL, &upstreamEnabled, &item.UpstreamRefreshHours, &item.UpstreamStatus, &item.UpstreamLastError, &item.UpstreamLastRefreshAt, &item.UpstreamUserinfo, &item.TotalBytes, &item.ManualUploadBytes, &item.ManualDownloadBytes, &item.ExpireAt, &item.CycleType, &item.CycleDay, &item.CycleStart, &item.CycleEnd, &item.BaselineUploadBytes, &item.BaselineDownloadBytes, &rateEnabled, &item.RateLimitPerMinute, &item.NodeFilterTags, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.UpstreamEnabled = upstreamEnabled == 1
		item.RateLimitEnabled = rateEnabled == 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, items[i].ID).Scan(&items[i].NodeCount)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ? AND id != COALESCE(profile_id, id)`, items[i].ID).Scan(&items[i].SubscriptionCount)
		items[i].Traffic = computeTraffic(ctx, db, Subscription{
			ID:                    items[i].ID,
			ProfileID:             items[i].ID,
			Name:                  items[i].Name,
			Enabled:               items[i].Enabled,
			TrafficSource:         items[i].TrafficSource,
			TrafficServerID:       items[i].TrafficServerID,
			TotalBytes:            items[i].TotalBytes,
			ManualUploadBytes:     items[i].ManualUploadBytes,
			ManualDownloadBytes:   items[i].ManualDownloadBytes,
			ExpireAt:              items[i].ExpireAt,
			CycleType:             items[i].CycleType,
			CycleDay:              items[i].CycleDay,
			CycleStart:            items[i].CycleStart,
			CycleEnd:              items[i].CycleEnd,
			BaselineUploadBytes:   items[i].BaselineUploadBytes,
			BaselineDownloadBytes: items[i].BaselineDownloadBytes,
		})
	}
	return items, nil
}

func loadSubscriptions(ctx context.Context, db *sql.DB, id string) ([]Subscription, error) {
	where := ""
	args := []interface{}{}
	if id != "" {
		where = "WHERE id = ?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(profile_id, id), COALESCE(plan_id, ''), name, COALESCE(remark, ''), enabled, public_token, COALESCE(vless_uuid,''), COALESCE(hysteria2_password,''), COALESCE(template_id, ''), COALESCE(traffic_source, 'manual'), COALESCE(traffic_server_id, ''), COALESCE(upstream_url, ''), upstream_enabled, COALESCE(upstream_refresh_hours, 24), COALESCE(upstream_status, ''), COALESCE(upstream_last_error, ''), COALESCE(upstream_last_refresh_at, ''), total_bytes, manual_upload_bytes, manual_download_bytes, COALESCE(expire_at, ''), COALESCE(cycle_type, 'none'), COALESCE(cycle_day, 1), COALESCE(cycle_start, ''), COALESCE(cycle_end, ''), baseline_upload_bytes, baseline_download_bytes, rate_limit_enabled, COALESCE(rate_limit_per_minute, 30), COALESCE(node_filter_ids, ''), COALESCE(include_internal_nodes,1), COALESCE(include_external_nodes,0), created_at, updated_at FROM subscription_subscriptions `+where+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []Subscription{}
	for rows.Next() {
		var sub Subscription
		var enabled, upstreamEnabled, rateEnabled, includeInternal, includeExternal int
		var nodeFilterIDs string
		if err := rows.Scan(&sub.ID, &sub.ProfileID, &sub.PlanID, &sub.Name, &sub.Remark, &enabled, &sub.PublicToken, &sub.VLESSUUID, &sub.Hysteria2Password, &sub.TemplateID, &sub.TrafficSource, &sub.TrafficServerID, &sub.UpstreamURL, &upstreamEnabled, &sub.UpstreamRefreshHours, &sub.UpstreamStatus, &sub.UpstreamLastError, &sub.UpstreamLastRefreshAt, &sub.TotalBytes, &sub.ManualUploadBytes, &sub.ManualDownloadBytes, &sub.ExpireAt, &sub.CycleType, &sub.CycleDay, &sub.CycleStart, &sub.CycleEnd, &sub.BaselineUploadBytes, &sub.BaselineDownloadBytes, &rateEnabled, &sub.RateLimitPerMinute, &nodeFilterIDs, &includeInternal, &includeExternal, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		sub.Enabled = enabled == 1
		sub.UpstreamEnabled = upstreamEnabled == 1
		sub.RateLimitEnabled = rateEnabled == 1
		sub.IncludeInternalNodes = includeInternal == 1
		sub.IncludeExternalNodes = includeExternal == 1
		sub.NodeFilterIDs = decodeNodeFilterIDs(nodeFilterIDs)
		items = append(items, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		// Store.Open intentionally limits SQLite to one connection. Applying a plan
		// while the subscription cursor is still open would wait forever for that
		// same connection, blocking both list and create/update responses.
		applyPlanToSubscription(ctx, db, &items[i])
		items[i].NodeCount = countPublishedSubscriptionNodes(ctx, db, items[i])
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND date(created_at) = date('now')`, items[i].ID).Scan(&items[i].AccessCountToday)
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(created_at), '') FROM subscription_access_logs WHERE subscription_id = ?`, items[i].ID).Scan(&items[i].LastAccessAt)
		items[i].Traffic = computeTraffic(ctx, db, items[i])
		if nodeIDs, err := reconcilequeue.NodeIDsForPlan(ctx, db, items[i].PlanID); err == nil {
			items[i].RuntimeSyncStatus = reconcilequeue.Status(ctx, db, nodeIDs)
		} else {
			items[i].RuntimeSyncStatus = "unknown"
		}
		items[i].Quality = loadQuality(ctx, db, items[i].TrafficServerID)
	}
	return items, nil
}

func loadSubscriptionByToken(ctx context.Context, db *sql.DB, token string) ([]Subscription, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT id FROM subscription_subscriptions WHERE public_token = ?`, token).Scan(&id)
	if err != nil {
		return nil, err
	}
	return loadSubscriptions(ctx, db, id)
}

func loadNodes(ctx context.Context, db *sql.DB, subscriptionID string, decrypt bool) ([]Node, error) {
	where := ""
	args := []interface{}{}
	if subscriptionID != "" {
		where = "WHERE COALESCE(profile_id, subscription_id) = ?"
		args = append(args, subscriptionID)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, subscription_id, COALESCE(profile_id, subscription_id), name, COALESCE(type, ''), COALESCE(server, ''), COALESCE(port, 0), COALESCE(country_code, ''), COALESCE(location, ''), COALESCE(tags, ''), COALESCE(traffic_server_id, ''), COALESCE(ownership, 'external'), COALESCE(management, 'unmanaged'), COALESCE(traffic_reporting, 'unavailable'), enabled, stable, sort_order, COALESCE(raw_encrypted, ''), COALESCE(config_encrypted, ''), created_at, updated_at FROM subscription_nodes `+where+` ORDER BY COALESCE(profile_id, subscription_id) ASC, sort_order ASC, created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	nodes := []Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &node.TrafficServerID, &node.Ownership, &node.Management, &node.TrafficReporting, &enabled, &stable, &node.SortOrder, &node.Raw, &node.ConfigJSON, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		if decrypt {
			node.Raw = secure.SecureDecrypt(node.Raw)
			node.ConfigJSON = secure.SecureDecrypt(node.ConfigJSON)
		} else {
			node.Raw = ""
			node.ConfigJSON = ""
		}
		nodes = append(nodes, node)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range nodes {
		if nodes[i].TrafficServerID != "" {
			nodes[i].Quality = loadQuality(ctx, db, nodes[i].TrafficServerID)
		}
	}
	return nodes, nil
}

func insertNode(ctx context.Context, tx *sql.Tx, node Node) error {
	if node.ID == "" {
		node.ID = randomID("node")
	}
	if strings.TrimSpace(node.Name) == "" {
		node.Name = firstNonEmpty(node.Server, "未命名节点")
	}
	node.ProfileID = firstNonEmpty(node.ProfileID, node.SubscriptionID)
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	fingerprint := nodeFingerprint(node)
	ownership := normalizeNodeOwnership(node.Ownership)
	if strings.TrimSpace(node.Ownership) == "" && node.TrafficServerID != "" {
		ownership = "self"
	}
	management := normalizeNodeManagement(node.Management, ownership)
	reporting := normalizeNodeTrafficReporting(node.TrafficReporting, management)
	trafficServerID := node.TrafficServerID
	if ownership != "self" {
		trafficServerID = ""
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_nodes (id, subscription_id, profile_id, name, type, server, port, country_code, location, tags, traffic_server_id, ownership, management, traffic_reporting, enabled, stable, sort_order, raw_encrypted, config_encrypted, fingerprint, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		node.ID, node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(trafficServerID), ownership, management, reporting, boolToInt(node.Enabled || !strings.EqualFold(node.Name, "__disabled__")), boolToInt(node.Stable), node.SortOrder, rawEnc, cfgEnc, fingerprint, firstNonEmpty(node.Source, "manual"))
	return err
}

// replaceImportedNodes reconciles a replace import in place. Plans reference
// node IDs, so deleting every row before re-importing silently disconnects all
// downstream subscriptions. Exact fingerprints are preferred; a unique name
// is the stable fallback when an upstream changes the endpoint or credentials.
func replaceImportedNodes(ctx context.Context, tx *sql.Tx, profileID, source string, incoming []Node) error {
	existing, err := loadReplaceCandidates(ctx, tx, profileID, source)
	if err != nil {
		return err
	}
	byFingerprint := make(map[string]Node, len(existing))
	nameCandidates := make(map[string][]Node, len(existing))
	for _, node := range existing {
		fingerprint := nodeFingerprint(node)
		if fingerprint != "" {
			byFingerprint[fingerprint] = node
		}
		nameKey := normalizedNodeIdentityName(node.Name)
		if nameKey != "" {
			nameCandidates[nameKey] = append(nameCandidates[nameKey], node)
		}
	}

	seenIDs := make(map[string]bool, len(incoming))
	for index := range incoming {
		node := incoming[index]
		fingerprint := nodeFingerprint(node)
		current, matched := byFingerprint[fingerprint]
		if !matched {
			matches := nameCandidates[normalizedNodeIdentityName(node.Name)]
			if len(matches) == 1 {
				current, matched = matches[0], true
			}
		}
		if matched && !seenIDs[current.ID] {
			node.ID = current.ID
			node.Enabled = current.Enabled
			node.Stable = current.Stable
			if err := updateImportedNode(ctx, tx, node, source, fingerprint); err != nil {
				return err
			}
			seenIDs[current.ID] = true
			continue
		}
		if err := insertNode(ctx, tx, node); err != nil {
			return err
		}
		seenIDs[node.ID] = true
	}

	for _, node := range existing {
		if seenIDs[node.ID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE node_id=? AND source='external'`, node.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE id=?`, node.ID); err != nil {
			return err
		}
	}
	return nil
}

func loadReplaceCandidates(ctx context.Context, tx *sql.Tx, profileID, source string) ([]Node, error) {
	sourceClause := "source = 'manual'"
	if source == "managed" {
		sourceClause = "source IN ('managed','upstream')"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,subscription_id,COALESCE(profile_id,subscription_id),name,
		COALESCE(type,''),COALESCE(server,''),COALESCE(port,0),COALESCE(country_code,''),COALESCE(location,''),
		COALESCE(tags,''),enabled,stable,sort_order,COALESCE(fingerprint,'')
		FROM subscription_nodes WHERE COALESCE(profile_id,subscription_id)=? AND `+sourceClause, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		var fingerprint string
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &enabled, &stable, &node.SortOrder, &fingerprint); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		items = append(items, node)
	}
	return items, rows.Err()
}

func updateImportedNode(ctx context.Context, tx *sql.Tx, node Node, source, fingerprint string) error {
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_nodes SET subscription_id=?,profile_id=?,name=?,type=?,server=?,port=?,country_code=?,location=?,tags=?,
		traffic_server_id=NULL,ownership='external',management='unmanaged',traffic_reporting='unavailable',enabled=?,stable=?,sort_order=?,
		raw_encrypted=?,config_encrypted=?,fingerprint=?,source=?,updated_at=datetime('now') WHERE id=?`,
		node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags,
		boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, rawEnc, cfgEnc, fingerprint, source, node.ID)
	return err
}

func normalizedNodeIdentityName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func mergeManagedNodes(ctx context.Context, tx *sql.Tx, profileID string, incoming []Node) error {
	existing, err := loadManagedNodeFingerprints(ctx, tx, profileID)
	if err != nil {
		return err
	}
	candidates, err := loadReplaceCandidates(ctx, tx, profileID, "managed")
	if err != nil {
		return err
	}
	nameCandidates := make(map[string][]Node, len(candidates))
	for _, candidate := range candidates {
		key := normalizedNodeIdentityName(candidate.Name)
		if key != "" {
			nameCandidates[key] = append(nameCandidates[key], candidate)
		}
	}
	seenIDs := map[string]bool{}
	for i := range incoming {
		node := incoming[i]
		node.SubscriptionID = profileID
		node.ProfileID = profileID
		node.Source = "managed"
		if node.SortOrder == 0 {
			node.SortOrder = i + 1
		}
		fingerprint := nodeFingerprint(node)
		current, ok := existing[fingerprint]
		if !ok {
			matches := nameCandidates[normalizedNodeIdentityName(node.Name)]
			if len(matches) == 1 {
				current, ok = matches[0], true
			}
		}
		if ok && !seenIDs[current.ID] {
			node.ID = current.ID
			node.Name = firstNonEmpty(current.Name, node.Name)
			node.CountryCode = firstNonEmpty(current.CountryCode, node.CountryCode)
			node.Location = firstNonEmpty(current.Location, node.Location)
			node.Tags = firstNonEmpty(current.Tags, node.Tags)
			node.TrafficServerID = current.TrafficServerID
			node.Enabled = current.Enabled
			node.Stable = current.Stable
			node.SortOrder = current.SortOrder
			if err := updateManagedNode(ctx, tx, node, fingerprint); err != nil {
				return err
			}
			seenIDs[node.ID] = true
			continue
		}
		if err := insertNode(ctx, tx, node); err != nil {
			return err
		}
		seenIDs[node.ID] = true
	}
	for _, node := range existing {
		if !seenIDs[node.ID] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE id = ?`, node.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadManagedNodeFingerprints(ctx context.Context, tx *sql.Tx, profileID string) (map[string]Node, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, subscription_id, COALESCE(profile_id, subscription_id), name, COALESCE(type, ''), COALESCE(server, ''), COALESCE(port, 0), COALESCE(country_code, ''), COALESCE(location, ''), COALESCE(tags, ''), COALESCE(traffic_server_id, ''), enabled, stable, sort_order, COALESCE(fingerprint, '')
		FROM subscription_nodes
		WHERE COALESCE(profile_id, subscription_id) = ? AND source IN ('managed', 'upstream')`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := map[string]Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		var fingerprint string
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &node.TrafficServerID, &enabled, &stable, &node.SortOrder, &fingerprint); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		stableFingerprint := nodeFingerprint(node)
		if fingerprint != "" {
			nodes[fingerprint] = node
		}
		if stableFingerprint != "" {
			// Keep compatibility with rows created before fingerprints stopped including the name.
			nodes[stableFingerprint] = node
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func updateManagedNode(ctx context.Context, tx *sql.Tx, node Node, fingerprint string) error {
	rawEnc, err := secure.SecureEncrypt(node.Raw)
	if err != nil {
		return err
	}
	cfgEnc, err := secure.SecureEncrypt(node.ConfigJSON)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_nodes SET subscription_id = ?, profile_id = ?, name = ?, type = ?, server = ?, port = ?, country_code = ?, location = ?, tags = ?, traffic_server_id = ?, enabled = ?, stable = ?, sort_order = ?, raw_encrypted = CASE WHEN ? = '' THEN raw_encrypted ELSE ? END, config_encrypted = CASE WHEN ? = '' THEN config_encrypted ELSE ? END, fingerprint = ?, source = 'managed', updated_at = datetime('now') WHERE id = ?`,
		node.SubscriptionID, node.ProfileID, node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(node.TrafficServerID), boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, node.Raw, rawEnc, node.ConfigJSON, cfgEnc, fingerprint, node.ID)
	return err
}

func loadTemplates(ctx context.Context, db *sql.DB) ([]Template, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, format, content, builtin, is_default, COALESCE(description, ''), created_at, updated_at FROM subscription_templates ORDER BY is_default DESC, builtin DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Template{}
	for rows.Next() {
		var tpl Template
		var builtin, def int
		if err := rows.Scan(&tpl.ID, &tpl.Name, &tpl.Format, &tpl.Content, &builtin, &def, &tpl.Description, &tpl.CreatedAt, &tpl.UpdatedAt); err != nil {
			return nil, err
		}
		tpl.Builtin = builtin == 1
		tpl.IsDefault = def == 1
		if validationErr := validateTemplateDefinition(tpl.Format, tpl.Content); validationErr != nil {
			tpl.ValidationError = validationErr.Error()
		} else {
			tpl.Valid = true
		}
		items = append(items, tpl)
	}
	return items, rows.Err()
}

func validateTemplateReference(ctx context.Context, db *sql.DB, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("请选择输出模板")
	}
	var format, content string
	if err := db.QueryRowContext(ctx, `SELECT format,content FROM subscription_templates WHERE id=?`, id).Scan(&format, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			switch id {
			case defaultTemplateID:
				format, content = "clash", loadDefaultMihomoTemplate()
			case rawTemplateID:
				format, content = "raw", "{{ raw_uri_list }}"
			case base64TemplateID:
				format, content = "base64", "{{ raw_uri_list }}"
			default:
				return errors.New("所选模板不存在")
			}
		} else {
			return fmt.Errorf("读取模板失败: %w", err)
		}
	}
	if err := validateTemplateDefinition(format, content); err != nil {
		return fmt.Errorf("所选模板配置无效: %w", err)
	}
	return nil
}

func loadSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	var settings Settings
	var limitEnabled, geo int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(default_template_id, 'builtin_mihomo_default'), default_rate_limit_enabled, COALESCE(default_rate_limit_per_minute, 30), COALESCE(default_refresh_hours, 24), geoip_enabled FROM subscription_settings WHERE id = 1`).Scan(&settings.DefaultTemplateID, &limitEnabled, &settings.DefaultRateLimitPerMin, &settings.DefaultRefreshHours, &geo)
	settings.DefaultRateLimitEnabled = limitEnabled == 1
	settings.GeoIPEnabled = geo == 1
	if settings.DefaultTemplateID == "" {
		settings.DefaultTemplateID = defaultTemplateID
	}
	return settings, err
}

func computeTraffic(ctx context.Context, db *sql.DB, sub Subscription) TrafficInfo {
	info := TrafficInfo{Total: sub.TotalBytes, Source: "panel", Status: "active", MeteringStatus: "pending", CycleStart: sub.CycleStart, CycleEnd: sub.CycleEnd}
	usage, err := subscriptionledger.Current(ctx, db, sub.ID, sub.CycleType, sub.CycleDay, sub.CreatedAt, time.Now().UTC())
	if err == nil {
		info.Upload = usage.UploadBytes
		info.Download = usage.DownloadBytes
		info.MeteringStatus = usage.Metering
		info.CycleStart = usage.CycleStart
		info.CycleEnd = usage.CycleEnd
	}
	if sub.ExpireAt != "" {
		if t, err := parseTime(sub.ExpireAt); err == nil {
			info.Expire = t.Unix()
			if time.Now().After(t) {
				info.Status = "expired"
			}
		}
	}
	used := info.Upload + info.Download
	if info.Total > 0 {
		info.Percent = float64(used) / float64(info.Total) * 100
		if used >= info.Total && info.Status == "active" {
			info.Status = "exhausted"
		}
	}
	return info
}

func recordSubscriptionUsage(ctx context.Context, db *sql.DB, report subscriptionUsageReport) (bool, error) {
	return subscriptionledger.Record(ctx, db, subscriptionledger.Report{
		ServerID: report.ServerID, NodeID: report.NodeID, CredentialID: report.CredentialID,
		BootID: report.BootID, Sequence: report.Sequence, UploadBytes: report.UploadBytes, DownloadBytes: report.DownloadBytes,
	}, time.Now().UTC())
}

func planCycleWindow(now time.Time, cycleType string, cycleDay int) (string, string) {
	if normalizeCycleType(cycleType) != "monthly" {
		return "", ""
	}
	if cycleDay < 1 || cycleDay > 31 {
		cycleDay = 1
	}
	now = now.UTC()
	boundary := func(year int, month time.Month) time.Time {
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		day := cycleDay
		if day > lastDay {
			day = lastDay
		}
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	current := boundary(now.Year(), now.Month())
	var start, end time.Time
	if now.Before(current) {
		previous := now.AddDate(0, -1, 0)
		start = boundary(previous.Year(), previous.Month())
		end = current
	} else {
		start = current
		next := now.AddDate(0, 1, 0)
		end = boundary(next.Year(), next.Month())
	}
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func loadQuality(ctx context.Context, db *sql.DB, serverID string) []QualitySummary {
	if serverID == "" {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT target_name, success, COALESCE(latency_ms, 0), checked_at FROM server_network_quality_samples WHERE server_id = ? AND checked_at >= datetime('now', '-1 day') ORDER BY checked_at DESC`, serverID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type agg struct {
		name        string
		count       int
		success     int
		total       float64
		prev        float64
		jitter      float64
		jitterCount int
		latest      float64
		sampledAt   string
	}
	items := map[string]*agg{}
	for rows.Next() {
		var name, checked string
		var success int
		var latency float64
		_ = rows.Scan(&name, &success, &latency, &checked)
		a := items[name]
		if a == nil {
			a = &agg{name: name, latest: latency, sampledAt: checked, prev: -1}
			items[name] = a
		}
		a.count++
		if success == 1 {
			a.success++
			a.total += latency
			if a.prev >= 0 {
				a.jitter += absFloat(latency - a.prev)
				a.jitterCount++
			}
			a.prev = latency
		}
	}
	out := []QualitySummary{}
	for _, a := range items {
		avg := 0.0
		if a.success > 0 {
			avg = a.total / float64(a.success)
		}
		jitter := 0.0
		if a.jitterCount > 0 {
			jitter = a.jitter / float64(a.jitterCount)
		}
		loss := 0.0
		if a.count > 0 {
			loss = (1 - float64(a.success)/float64(a.count)) * 100
		}
		out = append(out, QualitySummary{Name: a.name, LatencyMS: avg, AvgLatencyMS: avg, JitterMS: jitter, LossRate: loss, SampledAt: a.sampledAt})
	}
	return out
}

func renderOutput(ctx context.Context, db *sql.DB, sub Subscription, nodes []Node, format string, blocked bool) (string, string, error) {
	nodes = preparePublishedNodes(nodes)
	if blocked {
		nodes = nil
	}
	if format == "" || format == "clash" || format == "mihomo" {
		tpl := loadDefaultMihomoTemplate()
		usingCustomTemplate := false
		if sub.TemplateID != "" {
			var customTemplate string
			var builtin int
			_ = db.QueryRowContext(ctx, `SELECT content,builtin FROM subscription_templates WHERE id = ?`, sub.TemplateID).Scan(&customTemplate, &builtin)
			if strings.TrimSpace(customTemplate) != "" {
				tpl = customTemplate
				usingCustomTemplate = builtin == 0
			}
		}
		body := renderTemplate(tpl, sub, nodes)
		if err := validateMihomoOutput(body); err != nil {
			if !usingCustomTemplate {
				// Built-in templates are persisted for stable IDs. An older
				// record can retain removed node names, so fall back to the
				// current embedded template instead of breaking the feed.
				body = renderTemplate(loadDefaultMihomoTemplate(), sub, nodes)
				if fallbackErr := validateMihomoOutput(body); fallbackErr != nil {
					return "", "", fmt.Errorf("内置 Mihomo 模板输出无效，且当前模板回退失败: %w", fallbackErr)
				}
				return body, "text/yaml; charset=utf-8", nil
			}
			body = renderTemplate(loadDefaultMihomoTemplate(), sub, nodes)
			if fallbackErr := validateMihomoOutput(body); fallbackErr != nil {
				return "", "", fmt.Errorf("Mihomo 模板输出无效，且内置模板回退失败: %w", fallbackErr)
			}
		}
		return body, "text/yaml; charset=utf-8", nil
	}
	raw := rawURIList(nodes)
	if format == "base64" {
		return base64.StdEncoding.EncodeToString([]byte(raw)), "text/plain; charset=utf-8", nil
	}
	return raw, "text/plain; charset=utf-8", nil
}

var unresolvedTemplatePattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)

func normalizeTemplateFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "clash", "mihomo", "yaml", "yml":
		return "clash"
	case "raw":
		return "raw"
	case "base64":
		return "base64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// subscriptionFormatFromUA maps known proxy client User-Agents to a concrete
// subscription format. Unknown clients return "" so the caller falls back to
// the subscription's default template format (usually Mihomo/Clash YAML).
func subscriptionFormatFromUA(userAgent string) string {
	lower := strings.ToLower(userAgent)
	switch {
	case strings.Contains(lower, "clash"), strings.Contains(lower, "mihomo"), strings.Contains(lower, "stash"):
		return "clash"
	case strings.Contains(lower, "v2rayn"), strings.Contains(lower, "nekobox"), strings.Contains(lower, "quantumult"), strings.Contains(lower, "shadowrocket"):
		return "base64"
	case strings.Contains(lower, "sing-box"), strings.Contains(lower, "singbox"), strings.Contains(lower, "sfi"), strings.Contains(lower, "sfm"), strings.Contains(lower, "sfa"):
		return "base64"
	default:
		return ""
	}
}

// wantsSubscriptionInfoPage decides whether the request is a human opening the
// subscription URL in a browser and should get the readable info page instead
// of a raw config dump. Browsers are identified by a Mozilla-style UA plus an
// Accept header that asks for HTML; proxy clients send neither.
func wantsSubscriptionInfoPage(format string, r *http.Request) bool {
	if format == "info" {
		return true
	}
	ua := strings.ToLower(r.UserAgent())
	if !strings.Contains(ua, "mozilla") || strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo") || strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox") || strings.Contains(ua, "v2rayn") || strings.Contains(ua, "nekobox") || strings.Contains(ua, "sfa") || strings.Contains(ua, "sfm") || strings.Contains(ua, "sfi") || strings.Contains(ua, "quantumult") || strings.Contains(ua, "shadowrocket") {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/html")
}

// renderSubscriptionInfoPage builds a standalone readable summary page for the
// subscription. It deliberately exposes no node secrets — only the public link,
// quota state, node count, and copy buttons for supported formats.
func renderSubscriptionInfoPage(sub Subscription, traffic TrafficInfo, nodeCount int, subURL string) string {
	status := traffic.Status
	if status == "" {
		status = "active"
	}
	total := traffic.Total
	used := traffic.Upload + traffic.Download
	percent := 0
	if total > 0 {
		percent = int(traffic.Percent)
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
	}
	quota := "无限制"
	if total > 0 {
		quota = formatBytes(total)
	}
	expire := "无到期"
	if traffic.Expire > 0 {
		expire = time.Unix(traffic.Expire, 0).Local().Format("2006-01-02")
	}
	statusLabel := map[string]string{"active": "可用", "expired": "已到期", "exhausted": "流量已用尽"}[status]
	if statusLabel == "" {
		statusLabel = status
	}
	statusColor := map[string]string{"active": "#16a34a", "expired": "#dc2626", "exhausted": "#f59e0b"}[status]
	if statusColor == "" {
		statusColor = "#64748b"
	}
	name := strings.TrimSpace(sub.Name)
	if name == "" {
		name = "订阅"
	}
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>` + htmlEscape(name) + ` · 订阅信息</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;background:#0f172a;color:#e2e8f0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px}
.card{background:#1e293b;border:1px solid #334155;border-radius:16px;padding:28px;width:100%;max-width:420px;box-shadow:0 20px 50px rgba(0,0,0,.4)}
h1{font-size:18px;font-weight:600;margin-bottom:4px}
.sub{font-size:12px;color:#94a3b8;margin-bottom:20px;word-break:break-all}
.badge{display:inline-block;font-size:12px;font-weight:600;color:#fff;background:` + statusColor + `;border-radius:999px;padding:3px 12px;margin-bottom:20px}
.row{display:flex;justify-content:space-between;font-size:13px;padding:8px 0;border-top:1px solid #334155}
.row:first-of-type{border-top:none}
.row span{color:#94a3b8}
.row b{font-weight:600;text-align:right;word-break:break-all}
.bar{position:relative;height:8px;background:#0f172a;border-radius:999px;overflow:hidden}
.bar i{position:absolute;left:0;top:0;bottom:0;background:#3b82f6;border-radius:999px;width:` + itoa(percent) + `%}
.btns{display:flex;gap:8px;margin-top:20px}
.btns button{flex:1;font-size:13px;font-weight:600;color:#fff;background:#2563eb;border:0;border-radius:8px;padding:10px 0;cursor:pointer}
.btns button.raw{background:#0ea5e9}
.btns button.b64{background:#10b981}
.foot{margin-top:20px;font-size:11px;color:#64748b;text-align:center}
</style>
</head>
<body>
<div class="card">
<h1>` + htmlEscape(name) + `</h1>
<div class="sub">` + htmlEscape(subURL) + `</div>
<span class="badge">` + htmlEscape(statusLabel) + `</span>
<div class="row"><span>已用流量</span><b>` + htmlEscape(formatBytes(used)) + ` / ` + htmlEscape(quota) + `</b></div>
<div class="row"><span>节点数</span><b>` + itoa(nodeCount) + `</b></div>
<div class="row"><span>到期时间</span><b>` + htmlEscape(expire) + `</b></div>
<div class="row"><span>下次重置</span><b>` + htmlEscape(cycleEndLabel(sub.CycleEnd)) + `</b></div>
<div class="bar" style="margin-top:16px"><i></i></div>
<div class="btns">
<button onclick="copySub(0)">复制默认</button>
<button class="raw" onclick="copySub(1)">Raw</button>
<button class="b64" onclick="copySub(2)">Base64</button>
</div>
<div class="foot">API Monitor · 团队订阅</div>
</div>
<script>
var base='` + htmlEscape(subURL) + `';
function copySub(i){
  var fmt=['','?format=raw','?format=base64'][i];
  var url=base+fmt;
  if(navigator.clipboard){ navigator.clipboard.writeText(url).then(function(){var b=document.querySelector('.btns button:nth-child('+(i+1)+')'); if(b){b.textContent='已复制'} setTimeout(function(){b.textContent=['复制默认','Raw','Base64'][i]},1200)}) }
}
</script>
</body>
</html>`
}

func cycleEndLabel(cycleEnd string) string {
	if strings.TrimSpace(cycleEnd) == "" {
		return "不自动重置"
	}
	if t, err := parseTime(cycleEnd); err == nil {
		return t.Local().Format("2006-01-02")
	}
	return cycleEnd
}

func htmlEscape(value string) string {
	return strings.NewReplacer(`&`, "&amp;", `<`, "&lt;", `>`, "&gt;", `"`, "&#34;", `'`, "&#39;").Replace(value)
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func validateTemplateDefinition(format, content string) error {
	format = normalizeTemplateFormat(format)
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("模板内容不能为空")
	}
	if format != "clash" && format != "raw" && format != "base64" {
		return fmt.Errorf("不支持的模板格式 %q", format)
	}
	if format != "clash" {
		if unresolved := unresolvedTemplatePattern.FindString(content); unresolved != "" && unresolved != "{{ raw_uri_list }}" {
			return fmt.Errorf("包含未知占位符 %s", unresolved)
		}
		return nil
	}
	fixtureNodes := preparePublishedNodes([]Node{
		{Name: "模板验证节点", Type: "vless", Server: "node.example.com", Port: 443, ConfigJSON: `{"name":"模板验证节点","type":"vless","server":"node.example.com","port":443,"uuid":"00000000-0000-4000-8000-000000000001"}`, Enabled: true, Stable: true},
	})
	fixture := Subscription{Name: "模板验证", Traffic: TrafficInfo{Total: 1024}}
	rendered := renderTemplate(content, fixture, fixtureNodes)
	if unresolved := unresolvedTemplatePattern.FindString(rendered); unresolved != "" {
		return fmt.Errorf("包含未知占位符 %s", unresolved)
	}
	return validateMihomoOutput(rendered)
}

func validateMihomoOutput(body string) error {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(body), &document); err != nil {
		return fmt.Errorf("YAML 语法错误: %w", err)
	}
	if err := validateYAMLMappingKeys(&document); err != nil {
		return err
	}
	var raw interface{}
	if err := document.Decode(&raw); err != nil {
		return fmt.Errorf("YAML 解析失败: %w", err)
	}
	root, ok := normalizeStringMap(raw).(map[string]interface{})
	if !ok {
		return errors.New("顶层配置必须是对象")
	}
	proxyNames := map[string]string{}
	if rawProxies, exists := root["proxies"]; exists {
		proxies, ok := rawProxies.([]interface{})
		if !ok {
			return errors.New("proxies 必须是列表")
		}
		for index, item := range proxies {
			proxy, ok := normalizeStringMap(item).(map[string]interface{})
			if !ok {
				return fmt.Errorf("proxies[%d] 必须是对象", index)
			}
			name := strings.TrimSpace(stringVal(proxy["name"]))
			if name == "" {
				return fmt.Errorf("proxies[%d] 缺少名称", index)
			}
			key := strings.ToLower(name)
			if previous, duplicate := proxyNames[key]; duplicate {
				return fmt.Errorf("代理名称重复: %s（与 %s 冲突）", name, previous)
			}
			proxyNames[key] = name
			if err := validateMihomoProxy(proxy); err != nil {
				return fmt.Errorf("代理 %s 配置无效: %w", name, err)
			}
		}
	}

	groupNames := map[string]string{}
	groups := []map[string]interface{}{}
	if rawGroups, exists := root["proxy-groups"]; exists {
		items, ok := rawGroups.([]interface{})
		if !ok {
			return errors.New("proxy-groups 必须是列表")
		}
		for index, item := range items {
			group, ok := normalizeStringMap(item).(map[string]interface{})
			if !ok {
				return fmt.Errorf("proxy-groups[%d] 必须是对象", index)
			}
			name := strings.TrimSpace(stringVal(group["name"]))
			if name == "" || strings.TrimSpace(stringVal(group["type"])) == "" {
				return fmt.Errorf("proxy-groups[%d] 缺少名称或类型", index)
			}
			key := strings.ToLower(name)
			if previous, duplicate := groupNames[key]; duplicate {
				return fmt.Errorf("代理组名称重复: %s（与 %s 冲突）", name, previous)
			}
			if proxyName, collision := proxyNames[key]; collision {
				return fmt.Errorf("代理组名称 %s 与代理 %s 冲突", name, proxyName)
			}
			groupNames[key] = name
			groups = append(groups, group)
		}
	}
	allowed := map[string]bool{"direct": true, "reject": true, "reject-drop": true, "pass": true, "compatible": true}
	for key := range proxyNames {
		allowed[key] = true
	}
	for key := range groupNames {
		allowed[key] = true
	}
	graph := map[string][]string{}
	for _, group := range groups {
		groupName := strings.TrimSpace(stringVal(group["name"]))
		groupKey := strings.ToLower(groupName)
		rawRefs, exists := group["proxies"]
		if !exists {
			continue
		}
		refs, ok := rawRefs.([]interface{})
		if !ok {
			return fmt.Errorf("代理组 %s 的 proxies 必须是列表", groupName)
		}
		seenRefs := map[string]bool{}
		for _, rawRef := range refs {
			ref := strings.TrimSpace(stringVal(rawRef))
			key := strings.ToLower(ref)
			if ref == "" || !allowed[key] {
				return fmt.Errorf("代理组 %s 引用了不存在的代理或代理组 %q", groupName, ref)
			}
			if seenRefs[key] {
				return fmt.Errorf("代理组 %s 重复引用 %s", groupName, ref)
			}
			seenRefs[key] = true
			if _, isGroup := groupNames[key]; isGroup {
				graph[groupKey] = append(graph[groupKey], key)
			}
		}
	}
	if err := validateProxyGroupGraph(graph); err != nil {
		return err
	}
	return nil
}

func validateYAMLMappingKeys(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := strings.TrimSpace(node.Content[index].Value)
			if seen[key] {
				return fmt.Errorf("YAML 字段重复: %s", key)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLMappingKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func validateMihomoProxy(proxy map[string]interface{}) error {
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port < 1 || port > 65535 {
		return errors.New("缺少有效的 type、server 或 port")
	}
	switch typ {
	case "vless", "vmess":
		if strings.TrimSpace(stringVal(proxy["uuid"])) == "" {
			return errors.New("缺少 uuid")
		}
	case "trojan", "hysteria", "hysteria2", "tuic", "anytls":
		if strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return errors.New("缺少 password")
		}
	case "ss":
		if strings.TrimSpace(firstNonEmpty(stringVal(proxy["cipher"]), stringVal(proxy["method"]))) == "" || strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return errors.New("缺少加密方式或 password")
		}
	case "http", "socks", "socks5", "wireguard", "snell", "mieru":
	default:
		return fmt.Errorf("不支持的代理类型 %q", typ)
	}
	return nil
}

func validateProxyGroupGraph(graph map[string][]string) error {
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("代理组存在循环引用: %s", name)
		case 2:
			return nil
		}
		state[name] = 1
		for _, next := range graph[name] {
			if err := visit(next); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func renderTemplate(tpl string, sub Subscription, nodes []Node) string {
	replacements := map[string]string{
		"{{ subscription.name }}":                  sub.Name,
		"{{ subscription.expire_at }}":             sub.ExpireAt,
		"{{ traffic.upload }}":                     strconv.FormatInt(sub.Traffic.Upload, 10),
		"{{ traffic.download }}":                   strconv.FormatInt(sub.Traffic.Download, 10),
		"{{ traffic.total }}":                      strconv.FormatInt(sub.Traffic.Total, 10),
		"{{ proxies_yaml }}":                       proxiesYAML(nodes, 2),
		"{{ proxy_names_yaml | indent 6 }}":        proxyNamesYAML(nodes, false, 6),
		"{{ stable_proxy_names_yaml | indent 6 }}": stableProxyNamesYAML(nodes, 6),
		"{{ raw_uri_list }}":                       rawURIList(nodes),
	}
	out := tpl
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}

func preparePublishedNodes(nodes []Node) []Node {
	filtered := make([]Node, 0, len(nodes))
	seen := map[string]bool{}
	for _, node := range nodes {
		node.Name = strings.TrimSpace(node.Name)
		node.Raw = strings.TrimSpace(node.Raw)
		node.ConfigJSON = strings.TrimSpace(node.ConfigJSON)
		if node.Name == "" {
			node.Name = firstNonEmpty(strings.TrimSpace(node.Server), strings.TrimSpace(node.Type), "未命名节点")
		}
		if !hasValidPublishedRawURI(node) && validatedPublishedProxyConfig(node) == nil {
			continue
		}
		key := publishedNodeDedupKey(node)
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		filtered = append(filtered, node)
	}
	return ensureUniquePublishedNodeNames(filtered)
}

func publishedNodeDedupKey(node Node) string {
	if proxy := validatedPublishedProxyConfig(node); len(proxy) > 0 {
		canonical := make(map[string]interface{}, len(proxy))
		for key, value := range proxy {
			if key == "name" {
				continue
			}
			canonical[key] = value
		}
		if encoded, err := json.Marshal(canonical); err == nil {
			return "proxy:" + string(encoded)
		}
	}
	if hasValidPublishedRawURI(node) {
		return "raw:" + canonicalizePublishedRawURI(node.Raw)
	}
	return ""
}

func validatedPublishedProxyConfig(node Node) map[string]interface{} {
	proxy := nodeProxyConfig(node)
	if len(proxy) == 0 {
		return nil
	}
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port <= 0 {
		return nil
	}
	switch typ {
	case "vless", "vmess":
		if strings.TrimSpace(stringVal(proxy["uuid"])) == "" {
			return nil
		}
	case "trojan", "hysteria2", "tuic":
		if strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return nil
		}
	case "ss":
		if strings.TrimSpace(stringVal(proxy["cipher"])) == "" || strings.TrimSpace(stringVal(proxy["password"])) == "" {
			return nil
		}
	case "http", "socks5", "socks":
	default:
		return nil
	}
	return proxy
}

func hasValidPublishedRawURI(node Node) bool {
	raw := strings.TrimSpace(node.Raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		return false
	}
	switch scheme {
	case "vmess":
		if parsed.Host == "" {
			_, ok := parseVMessBase64URI(raw, 1)
			return ok
		}
		return parsed.Hostname() != ""
	case "vless", "trojan", "ss", "hysteria2", "hy2", "tuic", "socks", "http":
		return parsed.Hostname() != ""
	default:
		return false
	}
}

func canonicalizePublishedRawURI(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Fragment = ""
	return parsed.String()
}

func proxiesYAML(nodes []Node, indent int) string {
	lines := []string{}
	pad := strings.Repeat(" ", indent)
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		proxy := validatedPublishedProxyConfig(node)
		if len(proxy) == 0 {
			continue
		}
		// The node management record is authoritative for the published name.
		// ConfigJSON may retain the original URI name after a node is renamed;
		// exporting that stale value can create duplicate Mihomo proxy names.
		proxy["name"] = node.Name
		encoded, err := yaml.Marshal([]interface{}{proxy})
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(string(encoded), "\n"), "\n") {
			lines = append(lines, pad+line)
		}
	}
	if len(lines) == 0 {
		return pad + "[]"
	}
	return strings.Join(lines, "\n")
}

func proxyNamesYAML(nodes []Node, stableOnly bool, indent int) string {
	lines := []string{}
	for _, node := range nodes {
		if !node.Enabled || (stableOnly && !node.Stable) {
			continue
		}
		lines = append(lines, yamlStringListItem(node.Name, indent))
	}
	if stableOnly && len(lines) == 0 {
		lines = append(lines, yamlStringListItem("🚀 手动", indent))
	}
	if len(lines) == 0 {
		lines = append(lines, yamlStringListItem("DIRECT", indent))
	}
	return strings.Join(lines, "\n")
}

func stableProxyNamesYAML(nodes []Node, indent int) string {
	lines := []string{}
	seen := map[string]bool{}
	for _, node := range nodes {
		if !node.Enabled || !node.Stable || seen[node.Name] {
			continue
		}
		lines = append(lines, yamlStringListItem(node.Name, indent))
		seen[node.Name] = true
	}
	if len(lines) == 0 {
		lines = append(lines, yamlStringListItem("DIRECT", indent))
	}
	return strings.Join(lines, "\n")
}

func yamlStringListItem(value string, indent int) string {
	encoded, err := yaml.Marshal([]string{value})
	if err != nil {
		return strings.Repeat(" ", indent) + "- " + value
	}
	line := strings.TrimSpace(strings.Split(strings.TrimRight(string(encoded), "\n"), "\n")[0])
	return strings.Repeat(" ", indent) + line
}

func nodeProxyConfig(node Node) map[string]interface{} {
	cfg := strings.TrimSpace(node.ConfigJSON)
	if cfg != "" {
		var parsed interface{}
		if json.Unmarshal([]byte(cfg), &parsed) == nil {
			if proxy, ok := normalizeStringMap(parsed).(map[string]interface{}); ok {
				if proxyConfigComplete(proxy) {
					return proxy
				}
			}
		}
		if yaml.Unmarshal([]byte(cfg), &parsed) == nil {
			if proxy, ok := normalizeStringMap(parsed).(map[string]interface{}); ok {
				if proxyConfigComplete(proxy) {
					return proxy
				}
			}
		}
	}
	return uriToClashMap(node)
}

func proxyConfigComplete(proxy map[string]interface{}) bool {
	typ := strings.ToLower(strings.TrimSpace(stringVal(proxy["type"])))
	server := strings.TrimSpace(stringVal(proxy["server"]))
	port := int(floatVal(proxy["port"]))
	if typ == "" || server == "" || port <= 0 {
		return false
	}
	switch typ {
	case "vless", "vmess":
		return strings.TrimSpace(stringVal(proxy["uuid"])) != ""
	case "trojan", "hysteria2", "tuic":
		return strings.TrimSpace(stringVal(proxy["password"])) != ""
	case "ss":
		return strings.TrimSpace(stringVal(proxy["cipher"])) != "" && strings.TrimSpace(stringVal(proxy["password"])) != ""
	case "http", "socks5", "socks":
		return true
	default:
		return false
	}
}

func rawURIList(nodes []Node) string {
	lines := []string{}
	for _, node := range nodes {
		if node.Enabled && hasValidPublishedRawURI(node) {
			lines = append(lines, strings.TrimSpace(node.Raw))
		}
	}
	return strings.Join(lines, "\n")
}

func filterNodesByIDs(nodes []Node, ids []string) []Node {
	if len(ids) == 0 {
		return nodes
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			allowed[id] = true
		}
	}
	if len(allowed) == 0 {
		return nodes
	}
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if allowed[node.ID] {
			out = append(out, node)
		}
	}
	return out
}

func encodeNodeFilterIDs(ids []string) string {
	clean := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(clean)
	return string(encoded)
}

func decodeNodeFilterIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) == nil {
		return compactStringList(ids)
	}
	return compactStringList(strings.Split(raw, ","))
}

func compactStringList(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func parseImportText(text string) []Node {
	return parseImportTextDepth(text, 0)
}

func parseImportTextDepth(text string, depth int) []Node {
	if nodes := parseClashProxyNodes(text); len(nodes) > 0 {
		return nodes
	}
	nodes := parseNodeURIs(text)
	if len(nodes) > 0 {
		return nodes
	}
	if len(nodes) == 0 && depth < 2 {
		if decoded, ok := decodeSubscriptionBase64(text); ok {
			return parseImportTextDepth(decoded, depth+1)
		}
	}
	return nodes
}

func parseClashProxyNodes(text string) []Node {
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil
	}
	rawProxies, ok := doc["proxies"].([]interface{})
	if !ok || len(rawProxies) == 0 {
		return nil
	}
	nodes := []Node{}
	for _, item := range rawProxies {
		proxy, ok := normalizeStringMap(item).(map[string]interface{})
		if !ok {
			continue
		}
		name := stringVal(proxy["name"])
		server := stringVal(proxy["server"])
		typ := strings.ToLower(stringVal(proxy["type"]))
		if name == "" || server == "" || typ == "" {
			continue
		}
		countryCode := countryCodeFromNodeName(name)
		cfg, _ := json.Marshal(proxy)
		nodes = append(nodes, Node{
			ID:          randomID("node"),
			Name:        name,
			Type:        typ,
			Server:      server,
			Port:        int(floatVal(proxy["port"])),
			CountryCode: countryCode,
			Enabled:     true,
			ConfigJSON:  string(cfg),
			SortOrder:   len(nodes) + 1,
			Source:      "managed",
		})
	}
	return nodes
}

func parseNodeURIs(text string) []Node {
	nodes := []Node{}
	seen := map[string]bool{}
	for _, candidate := range nodeURICandidates(text) {
		clean := cleanNodeURI(candidate)
		if clean == "" || seen[clean] || isLikelyPlainHTTPAsset(clean) {
			continue
		}
		seen[clean] = true
		nodes = append(nodes, parseURI(clean, len(nodes)+1))
	}
	return nodes
}

func nodeURICandidates(text string) []string {
	candidates := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if loc := nodeLinkPattern.FindStringIndex(trimmed); loc != nil {
			candidates = append(candidates, trimmed[loc[0]:])
		}
	}
	if len(candidates) > 0 {
		return candidates
	}
	return nodeLinkPattern.FindAllString(text, -1)
}

func cleanNodeURI(value string) string {
	clean := strings.TrimSpace(value)
	clean = strings.TrimLeft(clean, "- ")
	clean = strings.Trim(clean, `"' ,`)
	return strings.TrimSpace(clean)
}

func normalizeStringMap(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range v {
			out[key] = normalizeStringMap(item)
		}
		return out
	case map[interface{}]interface{}:
		out := map[string]interface{}{}
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeStringMap(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeStringMap(item))
		}
		return out
	default:
		return value
	}
}

func isLikelyPlainHTTPAsset(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	query := strings.ToLower(parsed.RawQuery)
	fragment := strings.TrimSpace(parsed.Fragment)
	if strings.Contains(query, "type=") || strings.Contains(query, "server=") || fragment != "" {
		return false
	}
	path := strings.ToLower(parsed.Path)
	return strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".yml") ||
		strings.HasSuffix(path, ".txt") ||
		strings.Contains(path, "/ruleset/") ||
		strings.Contains(path, "clash-rules")
}

func decodeSubscriptionBase64(text string) (string, bool) {
	clean := strings.TrimSpace(text)
	if clean == "" || strings.Contains(clean, "\n") && strings.Contains(clean, "://") {
		return "", false
	}
	clean = strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(clean)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(clean); err == nil && strings.Contains(string(decoded), "://") {
			return string(decoded), true
		}
	}
	if remainder := len(clean) % 4; remainder != 0 {
		padded := clean + strings.Repeat("=", 4-remainder)
		if decoded, err := base64.StdEncoding.DecodeString(padded); err == nil && strings.Contains(string(decoded), "://") {
			return string(decoded), true
		}
	}
	return "", false
}

func parseURI(raw string, order int) Node {
	node := Node{ID: randomID("node"), Raw: raw, Enabled: true, SortOrder: order}
	parsed, err := url.Parse(raw)
	if err != nil {
		node.Name = fmt.Sprintf("节点 %d", order)
		return node
	}
	node.Type = strings.ToLower(parsed.Scheme)
	if node.Type == "hy2" {
		node.Type = "hysteria2"
	}
	if parsed.Fragment != "" {
		if name, err := url.QueryUnescape(parsed.Fragment); err == nil {
			node.Name = name
		}
	}
	if countryCode, displayName := normalizeImportedNodeName(node.Name); countryCode != "" {
		node.CountryCode = countryCode
		node.Name = displayName
	} else if countryCode := countryCodePrefix(node.Name); countryCode != "" {
		node.CountryCode = countryCode
	}
	if node.Name == "" {
		node.Name = fmt.Sprintf("%s-%d", node.Type, order)
	}
	node.Server = parsed.Hostname()
	if p, _ := strconv.Atoi(parsed.Port()); p > 0 {
		node.Port = p
	}
	if node.Type == "vmess" && parsed.Host == "" {
		if parsedNode, ok := parseVMessBase64URI(raw, order); ok {
			return parsedNode
		}
	}
	if cfg := uriToClashMap(node); len(cfg) > 0 {
		if encoded, err := json.Marshal(cfg); err == nil {
			node.ConfigJSON = string(encoded)
		}
	}
	return node
}

func countryCodeFromNodeName(name string) string {
	if countryCode, _ := normalizeImportedNodeName(name); countryCode != "" {
		return countryCode
	}
	return countryCodePrefix(name)
}

func normalizeImportedNodeName(name string) (string, string) {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) < 2 || !isRegionalIndicator(runes[0]) || !isRegionalIndicator(runes[1]) {
		return "", name
	}
	code := string([]rune{'A' + (runes[0] - 0x1F1E6), 'A' + (runes[1] - 0x1F1E6)})
	return code, name
}

func isRegionalIndicator(value rune) bool {
	return value >= 0x1F1E6 && value <= 0x1F1FF
}

var twoLetterCodeRegex = regexp.MustCompile(`^[A-Za-z]{2}$`)

func countryCodePrefix(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return ""
	}
	prefix := name[:2]
	if !twoLetterCodeRegex.MatchString(prefix) {
		return ""
	}
	if len(name) == 2 {
		return strings.ToUpper(prefix)
	}
	next := rune(name[2])
	if next == ' ' || next == '_' || next == '-' || (next >= 0x4e00 && next <= 0x9fff) {
		return strings.ToUpper(prefix)
	}
	return ""
}

func uriToClashJSON(node Node) string {
	m := uriToClashMap(node)
	b, _ := json.Marshal(m)
	return string(b)
}

func uriToClashMap(node Node) map[string]interface{} {
	m := map[string]interface{}{
		"name":   node.Name,
		"type":   node.Type,
		"server": node.Server,
		"port":   node.Port,
	}
	parsed, err := url.Parse(node.Raw)
	if err != nil {
		return m
	}
	query := parsed.Query()
	switch node.Type {
	case "vless":
		m["uuid"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("encryption"), "none"); value != "" {
			m["encryption"] = value
		}
		network := firstNonEmpty(query.Get("type"), query.Get("network"))
		if network != "" && network != "tcp" {
			m["network"] = network
		}
		if strings.EqualFold(query.Get("security"), "tls") {
			m["tls"] = true
		}
		if strings.EqualFold(query.Get("security"), "reality") {
			m["tls"] = true
			m["flow"] = firstNonEmpty(query.Get("flow"), "xtls-rprx-vision")
			reality := map[string]interface{}{}
			if value := firstNonEmpty(query.Get("pbk"), query.Get("public-key")); value != "" {
				reality["public-key"] = value
			}
			if value := firstNonEmpty(query.Get("sid"), query.Get("short-id")); value != "" {
				reality["short-id"] = value
			}
			if len(reality) > 0 {
				m["reality-opts"] = reality
			}
		}
		if value := firstNonEmpty(query.Get("sni"), query.Get("servername")); value != "" {
			m["servername"] = value
			m["sni"] = value
		}
		if value := query.Get("fp"); value != "" {
			m["client-fingerprint"] = value
		}
		if queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if network == "ws" {
			wsOpts := map[string]interface{}{}
			if value := query.Get("path"); value != "" {
				wsOpts["path"] = value
			}
			if value := firstNonEmpty(query.Get("host"), query.Get("Host"), query.Get("sni"), query.Get("servername")); value != "" {
				wsOpts["headers"] = map[string]interface{}{"Host": value}
			}
			if len(wsOpts) > 0 {
				m["ws-opts"] = wsOpts
			}
		}
	case "trojan":
		m["password"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("sni"), query.Get("peer"), query.Get("servername")); value != "" {
			sni, embeddedALPN := splitSNIAndEmbeddedALPN(value)
			m["sni"] = sni
			if len(embeddedALPN) > 0 {
				m["alpn"] = embeddedALPN
			}
		}
		m["tls"] = true
		if queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if alpn := splitCSV(query.Get("alpn")); len(alpn) > 0 {
			m["alpn"] = alpn
		}
		if network := firstNonEmpty(query.Get("type"), query.Get("network")); network != "" {
			m["network"] = network
		}
		if value := query.Get("fp"); value != "" {
			m["client-fingerprint"] = value
		}
	case "hysteria2":
		m["password"] = parsed.User.Username()
		if value := firstNonEmpty(query.Get("sni"), query.Get("peer"), query.Get("servername")); value != "" {
			m["sni"] = value
		}
		if queryBool(query, "insecure") || queryBool(query, "allowInsecure") || queryBool(query, "skip-cert-verify") {
			m["skip-cert-verify"] = true
		}
		if alpn := splitCSV(query.Get("alpn")); len(alpn) > 0 {
			m["alpn"] = alpn
		}
	case "ss":
		if user := parsed.User.String(); user != "" {
			if decoded, ok := decodeMaybeBase64(user); ok {
				parts := strings.SplitN(decoded, ":", 2)
				if len(parts) == 2 {
					m["cipher"] = parts[0]
					m["password"] = parts[1]
				}
			}
		}
	}
	return m
}

func queryBool(values url.Values, key string) bool {
	value := strings.ToLower(strings.TrimSpace(values.Get(key)))
	return value == "1" || value == "true" || value == "yes"
}

func splitSNIAndEmbeddedALPN(value string) (string, []string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	markers := []string{"h3,", "h2,", "http/1.1"}
	cut := -1
	for _, marker := range markers {
		if idx := strings.Index(value, marker); idx >= 0 && (cut == -1 || idx < cut) {
			cut = idx
		}
	}
	if cut <= 0 {
		return value, nil
	}
	sni := strings.TrimSpace(strings.TrimRight(value[:cut], ","))
	alpn := splitCSV(value[cut:])
	if sni == "" {
		return value, nil
	}
	return sni, alpn
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func decodeMaybeBase64(value string) (string, bool) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return string(decoded), true
	}
	return "", false
}

func parseVMessBase64URI(raw string, order int) (Node, bool) {
	payload := strings.TrimPrefix(raw, "vmess://")
	decoded, ok := decodeMaybeBase64(payload)
	if !ok {
		return Node{}, false
	}
	var cfg map[string]interface{}
	if json.Unmarshal([]byte(decoded), &cfg) != nil {
		return Node{}, false
	}
	name := firstNonEmpty(stringVal(cfg["ps"]), fmt.Sprintf("vmess-%d", order))
	server := stringVal(cfg["add"])
	port, _ := strconv.Atoi(stringVal(cfg["port"]))
	proxy := map[string]interface{}{
		"name":    name,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    stringVal(cfg["id"]),
		"alterId": int(floatVal(cfg["aid"])),
		"cipher":  firstNonEmpty(stringVal(cfg["scy"]), "auto"),
	}
	if network := stringVal(cfg["net"]); network != "" && network != "tcp" {
		proxy["network"] = network
	}
	if tls := stringVal(cfg["tls"]); tls == "tls" {
		proxy["tls"] = true
	}
	if sni := stringVal(cfg["sni"]); sni != "" {
		proxy["servername"] = sni
	}
	if stringVal(cfg["net"]) == "ws" {
		wsOpts := map[string]interface{}{}
		if path := stringVal(cfg["path"]); path != "" {
			wsOpts["path"] = path
		}
		if host := stringVal(cfg["host"]); host != "" {
			wsOpts["headers"] = map[string]interface{}{"Host": host}
		}
		if len(wsOpts) > 0 {
			proxy["ws-opts"] = wsOpts
		}
	}
	encoded, _ := json.Marshal(proxy)
	return Node{ID: randomID("node"), Raw: raw, Enabled: true, SortOrder: order, Name: name, Type: "vmess", Server: server, Port: port, ConfigJSON: string(encoded)}, true
}

func parseUserInfo(raw string) TrafficInfo {
	info := TrafficInfo{Status: "active"}
	for _, part := range strings.Split(raw, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		value, _ := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		switch strings.TrimSpace(kv[0]) {
		case "upload":
			info.Upload = value
		case "download":
			info.Download = value
		case "total":
			info.Total = value
		case "expire":
			info.Expire = value
		}
	}
	return info
}

func templateFormat(ctx context.Context, db *sql.DB, id string) string {
	var format string
	_ = db.QueryRowContext(ctx, `SELECT format FROM subscription_templates WHERE id = ?`, id).Scan(&format)
	return format
}

func isRateLimited(ctx context.Context, db *sql.DB, subID, ip string, perMin int) bool {
	if perMin <= 0 {
		perMin = defaultLimitPerMin
	}
	// Per-IP dimension for one subscription catches a single client hammering
	// the endpoint. A token-global dimension (all IPs combined) limits an
	// attacker who spreads the leaked token across many hosts and stays under
	// each per-IP ceiling.
	var perIP, total int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND ip_address = ? AND created_at >= datetime('now', '-1 minute')`, subID, ip).Scan(&perIP)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND created_at >= datetime('now', '-1 minute')`, subID).Scan(&total)
	return perIP >= perMin || total >= perMin
}

func (s *Service) logAccess(ctx context.Context, db *sql.DB, subID, token, ip, ua, format string, success bool, statusCode int, errMsg string, nodeCount int, traffic TrafficInfo) {
	_, _ = db.ExecContext(ctx, `INSERT INTO subscription_access_logs (subscription_id, public_token, ip_address, user_agent, format, success, status_code, error_message, node_count, upload_bytes, download_bytes, total_bytes, expire_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subID, token, ip, ua, format, boolToInt(success), statusCode, nullString(errMsg), nodeCount, traffic.Upload, traffic.Download, traffic.Total, traffic.Expire)
}

func queryInt(r *http.Request, key string) int {
	value, _ := strconv.Atoi(r.URL.Query().Get(key))
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		if w != nil {
			response.Error(w, http.StatusBadRequest, "request parameter validation failed")
		}
		return false
	}
	return true
}

func randomID(prefix string) string {
	return prefix + "_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "_" + randomHex(4)
}

func randomToken() string {
	return randomHex(24)
}

func randomCredential() string {
	buffer := make([]byte, 24)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func randomUUID() string {
	raw := randomHex(16)
	if len(raw) != 32 {
		return "00000000-0000-4000-8000-" + randomHex(6)
	}
	return raw[0:8] + "-" + raw[8:12] + "-4" + raw[13:16] + "-8" + raw[17:20] + "-" + raw[20:32]
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value != "" {
			return strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func intDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func firstSub(items []Subscription) interface{} {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func firstProfile(items []NodeLibrary) interface{} {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func subscriptionFromProfile(input NodeLibrary) Subscription {
	return Subscription{
		ProfileID:             input.ID,
		Name:                  input.Name,
		Remark:                input.Remark,
		Enabled:               input.Enabled,
		TemplateID:            input.TemplateID,
		TrafficSource:         input.TrafficSource,
		TrafficServerID:       input.TrafficServerID,
		UpstreamURL:           input.UpstreamURL,
		UpstreamEnabled:       input.UpstreamEnabled,
		UpstreamRefreshHours:  input.UpstreamRefreshHours,
		UpstreamStatus:        input.UpstreamStatus,
		UpstreamLastError:     input.UpstreamLastError,
		UpstreamLastRefreshAt: input.UpstreamLastRefreshAt,
		TotalBytes:            input.TotalBytes,
		ManualUploadBytes:     input.ManualUploadBytes,
		ManualDownloadBytes:   input.ManualDownloadBytes,
		ExpireAt:              input.ExpireAt,
		CycleType:             input.CycleType,
		CycleDay:              input.CycleDay,
		CycleStart:            input.CycleStart,
		CycleEnd:              input.CycleEnd,
		BaselineUploadBytes:   input.BaselineUploadBytes,
		BaselineDownloadBytes: input.BaselineDownloadBytes,
		RateLimitEnabled:      input.RateLimitEnabled,
		RateLimitPerMinute:    input.RateLimitPerMinute,
	}
}

func normalizeTrafficSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "panel", "server", "node_servers", "upstream":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "manual"
	}
}

func normalizeCycleType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "monthly", "custom":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "none"
	}
}

func isExplicitFalse(r *http.Request, field string) bool {
	return false
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	return floatVal(m[key])
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	return floatVal(m[key])
}

func firstFloatValue(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if value := getFloatValue(m, key); value != 0 {
			return value
		}
	}
	return 0
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

func stringVal(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func floatVal(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func nodeFingerprint(node Node) string {
	return strings.ToLower(fmt.Sprintf("%s|%s|%d", strings.TrimSpace(node.Type), strings.TrimSpace(node.Server), node.Port))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func parseTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.Add(24*time.Hour - time.Second), nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func loadDefaultMihomoTemplate() string {
	if strings.TrimSpace(defaultMihomoTemplateEmbedded) != "" {
		return defaultMihomoTemplateEmbedded
	}
	for _, candidate := range []string{
		filepath.Join("backend-go", "internal", "subscription", "templates", "default-mihomo.yaml"),
		filepath.Join("internal", "subscription", "templates", "default-mihomo.yaml"),
		filepath.Join("data", "subscription", "default-mihomo.yaml"),
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			return string(data)
		}
	}
	return ""
}
