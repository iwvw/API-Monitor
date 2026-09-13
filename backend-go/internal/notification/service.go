package notification

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

const (
	defaultHistoryLimit      = 100
	maxHistoryLimit          = 500
	requestTimeout           = 10 * time.Second
	lifecycleRefreshInterval = 30 * time.Second
)

type Service struct {
	cfg         config.Config
	store       *database.Store
	client      *http.Client
	schemaOnce  sync.Once
	schemaErr   error
	rateLimiter *hourlyRateLimiter
}

type Channel struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Enabled   int                    `json:"enabled"`
	Config    map[string]interface{} `json:"config"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
}

type storedChannel struct {
	ID        string
	Name      string
	Type      string
	Enabled   int
	ConfigRaw string
	CreatedAt string
	UpdatedAt string
}

type deliveryResult struct {
	ChatID    string
	MessageID int64
}

type messageLifecycle struct {
	SourceModule string
	ResourceKey  string
	Kind         string
	Phase        string
}

type telegramMessageState struct {
	ChannelID    string
	SourceModule string
	ResourceKey  string
	Kind         string
	ChatID       string
	MessageID    int64
	EventType    string
	LastData     string
	CreatedAt    string
	UpdatedAt    string
}

type Rule struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	SourceModule    string                 `json:"source_module"`
	EventType       string                 `json:"event_type"`
	Severity        string                 `json:"severity"`
	Enabled         int                    `json:"enabled"`
	Channels        []string               `json:"channels"`
	Conditions      map[string]interface{} `json:"conditions"`
	Suppression     map[string]interface{} `json:"suppression"`
	TimeWindow      map[string]interface{} `json:"time_window"`
	Description     string                 `json:"description"`
	TitleTemplate   string                 `json:"title_template"`
	MessageTemplate string                 `json:"message_template"`
	BackupChannels  []string               `json:"backup_channels"`
	QuietUntil      *string                `json:"quiet_until,omitempty"`
	CreatedAt       string                 `json:"created_at,omitempty"`
	UpdatedAt       string                 `json:"updated_at,omitempty"`
}

type History struct {
	ID           int64   `json:"id"`
	RuleID       string  `json:"rule_id"`
	ChannelID    string  `json:"channel_id"`
	Status       string  `json:"status"`
	Title        string  `json:"title"`
	Message      string  `json:"message"`
	Data         string  `json:"data,omitempty"`
	ErrorMessage *string `json:"error_message"`
	SentAt       *string `json:"sent_at"`
	RetryCount   int     `json:"retry_count"`
	CreatedAt    string  `json:"created_at"`
}

type GlobalConfig struct {
	MaxRetryTimes        int      `json:"max_retry_times"`
	RetryIntervalSeconds int      `json:"retry_interval_seconds"`
	HistoryRetentionDays int      `json:"history_retention_days"`
	EnableBatch          bool     `json:"enable_batch"`
	BatchIntervalSeconds int      `json:"batch_interval_seconds"`
	DefaultChannels      []string `json:"default_channels"`
	GlobalRateLimitPerHr int      `json:"global_rate_limit_per_hour"`
	EnableAutoEscalation bool     `json:"enable_auto_escalation"`
	BaseURL              string   `json:"base_url"`
}

type conditionResult struct {
	Allowed bool                     `json:"allowed"`
	Mode    string                   `json:"mode"`
	Results []map[string]interface{} `json:"results"`
}

func New(cfg config.Config) *Service {
	return &Service{
		cfg:         cfg,
		store:       database.New(cfg),
		client:      &http.Client{Timeout: requestTimeout},
		rateLimiter: &hourlyRateLimiter{},
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/notification")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "channels":
		switch r.Method {
		case http.MethodGet:
			s.listChannels(w, r)
		case http.MethodPost:
			s.createChannel(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "channels" && parts[2] == "test" && r.Method == http.MethodPost:
		s.testChannel(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "channels":
		switch r.Method {
		case http.MethodGet:
			s.getChannel(w, r, parts[1])
		case http.MethodPut:
			s.updateChannel(w, r, parts[1])
		case http.MethodDelete:
			s.deleteChannel(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "rules":
		switch r.Method {
		case http.MethodGet:
			s.listRules(w, r)
		case http.MethodPost:
			s.createRule(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 3 && parts[0] == "rules" && parts[2] == "dry-run" && r.Method == http.MethodPost:
		s.dryRunRule(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "rules" && (parts[2] == "enable" || parts[2] == "disable") && r.Method == http.MethodPost:
		s.setRuleEnabled(w, r, parts[1], parts[2] == "enable")
	case len(parts) == 2 && parts[0] == "rules":
		switch r.Method {
		case http.MethodGet:
			s.getRule(w, r, parts[1])
		case http.MethodPut:
			s.updateRule(w, r, parts[1])
		case http.MethodDelete:
			s.deleteRule(w, r, parts[1])
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && (parts[0] == "event-catalog" || parts[0] == "events"):
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.OK(w, eventCatalog())
	case len(parts) == 2 && parts[0] == "events" && parts[1] == "catalog":
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		response.OK(w, eventCatalog())
	case len(parts) == 2 && parts[0] == "templates" && parts[1] == "preview" && r.Method == http.MethodPost:
		s.previewTemplate(w, r)
	case len(parts) == 1 && parts[0] == "history":
		switch r.Method {
		case http.MethodGet:
			s.listHistory(w, r)
		case http.MethodDelete:
			s.clearHistory(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "config":
		switch r.Method {
		case http.MethodGet:
			s.getConfig(w, r)
		case http.MethodPut:
			s.updateConfig(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 1 && parts[0] == "trigger" && r.Method == http.MethodPost:
		s.trigger(w, r)
	default:
		response.Error(w, http.StatusNotFound, "notification route not implemented")
	}
}

var errInvalidInput = errors.New("invalid input")

// smtpSendTimeout 是 SMTP 发送全链路的阻塞上限：网络连接、TLS 握手、
// 认证与数据传输任何一步停滞都不会无限等待。
const smtpSendTimeout = 30 * time.Second

type telegramAPIResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"result"`
}

// telegramEditIgnore 判断编辑失败是否可静默忽略：
// "message is not modified"（内容一致）与 "canceled by new edit message request"（流式编辑竞争，
// 后续编辑会覆盖）都不算真正的失败。
func telegramEditIgnore(err error) bool {
	if err == nil {
		return true
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "message is not modified") || strings.Contains(low, "canceled by new edit")
}

type notificationMessageField struct {
	Label string
	Value string
	Empty bool
}

func (s *Service) systemLocation(ctx context.Context) (*time.Location, string) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return timeutil.LocationFromName(""), "system"
	}
	defer db.Close()

	zone := timeutil.ReadTimeZone(ctx, db)
	loc := timeutil.LocationFromName(zone)
	name := zone
	if loc == time.Local || strings.TrimSpace(zone) == "" || strings.TrimSpace(zone) == "system" {
		name = "system"
	}
	return loc, name
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	// schema 幂等且启动后不变，进程内只执行一次，避免 24 个调用点每次
	// 打开连接都重放 ~30 条 DDL。
	s.schemaOnce.Do(func() {
		s.schemaErr = ensureSchema(ctx, db)
	})
	if s.schemaErr != nil {
		_ = db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}