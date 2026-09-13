package subscription

import (
	_ "embed"
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
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
	SelectionMode         string      `json:"selection_mode,omitempty"`
	IncludeInternalNodes  bool        `json:"include_internal_nodes"`
	// InternalNodeIDs 为 explicit 模式下授权的内部节点清单，与套餐共用
	// subscription_plan_nodes 同表同语义（plan_id 存 profile id）。
	InternalNodeIDs       []string    `json:"internal_node_ids,omitempty"`
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
	case len(parts) == 2 && parts[0] == "public":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.servePublicSubscriptionInfo(w, r, db, parts[1])
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
	case len(parts) == 3 && parts[0] == "subscriptions" && parts[2] == "usage":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.getSubscriptionUsage(w, r, db, parts[1])
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



type subscriptionExecutor interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}






























































































// publicSubscriptionInfo is the payload served by the public subscription info
// endpoint. It carries only display data and never node credentials, so the
// frontend info page can render without exposing vless UUID or hy2 passwords.
type publicSubscriptionInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Upload        int64    `json:"upload"`
	Download      int64    `json:"download"`
	Total         int64    `json:"total"`
	Percent       float64  `json:"percent"`
	Expire        int64    `json:"expire"`
	CycleStart    string   `json:"cycle_start,omitempty"`
	CycleEnd      string   `json:"cycle_end,omitempty"`
	NodeCount     int      `json:"node_count"`
	Formats       []string `json:"formats"`
	PublicToken   string   `json:"public_token"`
	SiteName      string   `json:"site_name,omitempty"`
	RateLimited   bool     `json:"rate_limited"`
}
