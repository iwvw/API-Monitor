package notification

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/iwvw/api-monitor/backend-go/internal/tgapi"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

const (
	defaultHistoryLimit      = 100
	maxHistoryLimit          = 500
	requestTimeout           = 10 * time.Second
	lifecycleRefreshInterval = 30 * time.Second
)

type Service struct {
	cfg        config.Config
	store      *database.Store
	client     *http.Client
	schemaOnce sync.Once
	schemaErr  error
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
		cfg:    cfg,
		store:  database.New(cfg),
		client: &http.Client{Timeout: requestTimeout},
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

func (s *Service) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.LoadChannels(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, channels)
}

func (s *Service) getChannel(w http.ResponseWriter, r *http.Request, id string) {
	channel, ok, err := s.LoadChannel(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	response.OK(w, channel)
}

func (s *Service) createChannel(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	channel, err := s.CreateChannel(r.Context(), payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, channel)
}

func (s *Service) updateChannel(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateChannel(r.Context(), id, payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) deleteChannel(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.DeleteChannel(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) testChannel(w http.ResponseWriter, r *http.Request, id string) {
	channel, ok, err := s.loadStoredChannel(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	config := decryptConfig(channel.ConfigRaw)
	title := "🧪 通知渠道连通性测试"
	loc, _ := s.systemLocation(r.Context())
	message := fmt.Sprintf("状态: 配置有效\n发送时间: %s", time.Now().In(loc).Format(time.RFC3339))
	if _, err := s.sendToChannel(r.Context(), channel, config, title, message); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "test message sent"})
}

func (s *Service) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.LoadRules(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, rules)
}

func (s *Service) getRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, ok, err := s.LoadRule(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "rule not found")
		return
	}
	response.OK(w, rule)
}

func (s *Service) createRule(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	rule, err := s.CreateRule(r.Context(), payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, rule)
}

func (s *Service) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateRule(r.Context(), id, payload); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.DeleteRule(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) setRuleEnabled(w http.ResponseWriter, r *http.Request, id string, enabled bool) {
	if err := s.SetRuleEnabled(r.Context(), id, enabled); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	data := objectValue(payload["data"])
	rule := Rule{
		Name:            "Template Preview",
		Severity:        stringDefault(data["severity"], "info"),
		EventType:       stringDefault(data["eventType"], "preview"),
		TitleTemplate:   stringValue(payload["title_template"]),
		MessageTemplate: stringValue(payload["message_template"]),
	}
	loc, _ := s.systemLocation(r.Context())
	templateData := notificationTemplateData(data, loc)
	response.OK(w, map[string]interface{}{
		"title":     formatTitle(rule, data),
		"message":   formatMessage(rule, data, loc),
		"variables": sortedKeys(templateData),
	})
}

func (s *Service) dryRunRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, ok, err := s.LoadRule(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "rule not found")
		return
	}
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	data := objectValue(payload["data"])
	result, err := s.DryRun(r.Context(), rule, data)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) listHistory(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"))
	history, err := s.LoadHistory(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, history)
}

func (s *Service) clearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.ClearHistory(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.LoadConfig(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, cfg)
}

func (s *Service) updateConfig(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateConfig(r.Context(), payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) trigger(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	sourceModule := stringValue(payload["source_module"])
	eventType := stringValue(payload["event_type"])
	if sourceModule == "" || eventType == "" {
		response.Error(w, http.StatusBadRequest, "missing required parameters")
		return
	}
	if err := s.Trigger(r.Context(), sourceModule, eventType, objectValue(payload["data"])); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "alert triggered"})
}

var errInvalidInput = errors.New("invalid input")

func (s *Service) LoadChannels(ctx context.Context) ([]Channel, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, type, enabled, config, created_at, updated_at
		FROM notification_channels
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer rows.Close()
	channels := []Channel{}
	for rows.Next() {
		stored, err := scanStoredChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, publicChannel(stored))
	}
	return channels, rows.Err()
}

func (s *Service) LoadChannel(ctx context.Context, id string) (Channel, bool, error) {
	stored, ok, err := s.loadStoredChannel(ctx, id)
	if err != nil || !ok {
		return Channel{}, ok, err
	}
	return publicChannel(stored), true, nil
}

func (s *Service) CreateChannel(ctx context.Context, payload map[string]interface{}) (Channel, error) {
	name := strings.TrimSpace(stringValue(payload["name"]))
	channelType := strings.TrimSpace(stringValue(payload["type"]))
	if name == "" || channelType == "" || payload["config"] == nil {
		return Channel{}, fmt.Errorf("%w: missing required parameters", errInvalidInput)
	}
	if channelType != "email" && channelType != "telegram" {
		return Channel{}, fmt.Errorf("%w: unsupported channel type", errInvalidInput)
	}
	configMap := objectValue(payload["config"])
	encrypted, err := secure.EncryptJSON(configMap)
	if err != nil {
		return Channel{}, fmt.Errorf("encrypt notification channel config: %w", err)
	}
	id, err := randomID("notif")
	if err != nil {
		return Channel{}, err
	}
	enabled := boolInt(boolValue(payload["enabled"], true))

	db, err := s.open(ctx)
	if err != nil {
		return Channel{}, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO notification_channels (id, name, type, enabled, config)
		VALUES (?, ?, ?, ?, ?)
	`, id, name, channelType, enabled, encrypted)
	if err != nil {
		return Channel{}, fmt.Errorf("create notification channel: %w", err)
	}
	channel, ok, err := s.LoadChannel(ctx, id)
	if err != nil || !ok {
		return Channel{}, err
	}
	return channel, nil
}

func (s *Service) UpdateChannel(ctx context.Context, id string, payload map[string]interface{}) error {
	updates := []string{}
	args := []interface{}{}
	if value, ok := payload["name"]; ok {
		updates = append(updates, "name = ?")
		args = append(args, strings.TrimSpace(stringValue(value)))
	}
	if value, ok := payload["enabled"]; ok {
		updates = append(updates, "enabled = ?")
		args = append(args, boolInt(boolValue(value, false)))
	}
	if value, ok := payload["config"]; ok {
		encrypted, err := secure.EncryptJSON(objectValue(value))
		if err != nil {
			return fmt.Errorf("encrypt notification channel config: %w", err)
		}
		updates = append(updates, "config = ?")
		args = append(args, encrypted)
	}
	if len(updates) == 0 {
		return nil
	}
	updates = append(updates, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE notification_channels SET `+strings.Join(updates, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update notification channel: %w", err)
	}
	return nil
}

func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	return nil
}

func (s *Service) LoadRules(ctx context.Context) ([]Rule, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until, created_at, updated_at
		FROM alert_rules
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Service) LoadRule(ctx context.Context, id string) (Rule, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Rule{}, false, err
	}
	defer db.Close()
	rule, err := findRule(ctx, db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, false, nil
	}
	return rule, err == nil, err
}

func (s *Service) CreateRule(ctx context.Context, payload map[string]interface{}) (Rule, error) {
	name := strings.TrimSpace(stringValue(payload["name"]))
	sourceModule := strings.TrimSpace(stringValue(payload["source_module"]))
	eventType := strings.TrimSpace(stringValue(payload["event_type"]))
	channels := stringList(payload["channels"])
	if name == "" || sourceModule == "" || eventType == "" || len(channels) == 0 {
		return Rule{}, fmt.Errorf("%w: missing required parameters", errInvalidInput)
	}
	if err := validateSourceModule(sourceModule); err != nil {
		return Rule{}, err
	}
	id, err := randomID("notif")
	if err != nil {
		return Rule{}, err
	}
	severity := stringDefault(payload["severity"], "warning")
	enabled := boolInt(boolValue(payload["enabled"], true))
	conditions := objectDefault(payload["conditions"], map[string]interface{}{})
	suppression := objectDefault(payload["suppression"], map[string]interface{}{})
	timeWindow := objectDefault(payload["time_window"], map[string]interface{}{"enabled": false})
	backupChannels := stringList(payload["backup_channels"])
	quietUntil := nullableString(stringValue(payload["quiet_until"]))

	db, err := s.open(ctx)
	if err != nil {
		return Rule{}, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO alert_rules (
			id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, sourceModule, eventType, severity, enabled, jsonString(channels), jsonString(conditions),
		jsonString(suppression), jsonString(timeWindow), stringValue(payload["description"]),
		stringValue(payload["title_template"]), stringValue(payload["message_template"]),
		jsonString(backupChannels), quietUntil)
	if err != nil {
		return Rule{}, fmt.Errorf("create alert rule: %w", err)
	}
	rule, ok, err := s.LoadRule(ctx, id)
	if err != nil || !ok {
		return Rule{}, err
	}
	return rule, nil
}

func (s *Service) UpdateRule(ctx context.Context, id string, payload map[string]interface{}) error {
	updates := []string{}
	args := []interface{}{}
	add := func(column string, value interface{}) {
		updates = append(updates, column+" = ?")
		args = append(args, value)
	}
	if value, ok := payload["name"]; ok {
		add("name", stringValue(value))
	}
	if value, ok := payload["source_module"]; ok {
		sourceModule := strings.TrimSpace(stringValue(value))
		if err := validateSourceModule(sourceModule); err != nil {
			return err
		}
		add("source_module", sourceModule)
	}
	if value, ok := payload["event_type"]; ok {
		add("event_type", stringValue(value))
	}
	if value, ok := payload["severity"]; ok {
		add("severity", stringValue(value))
	}
	if value, ok := payload["enabled"]; ok {
		add("enabled", boolInt(boolValue(value, false)))
	}
	if value, ok := payload["channels"]; ok {
		add("channels", jsonString(stringList(value)))
	}
	if value, ok := payload["conditions"]; ok {
		add("conditions", jsonString(objectValue(value)))
	}
	if value, ok := payload["suppression"]; ok {
		add("suppression", jsonString(objectValue(value)))
	}
	if value, ok := payload["time_window"]; ok {
		add("time_window", jsonString(objectValue(value)))
	}
	if value, ok := payload["description"]; ok {
		add("description", stringValue(value))
	}
	if value, ok := payload["title_template"]; ok {
		add("title_template", stringValue(value))
	}
	if value, ok := payload["message_template"]; ok {
		add("message_template", stringValue(value))
	}
	if value, ok := payload["backup_channels"]; ok {
		add("backup_channels", jsonString(stringList(value)))
	}
	if value, ok := payload["quiet_until"]; ok {
		add("quiet_until", nullableString(stringValue(value)))
	}
	if len(updates) == 0 {
		return nil
	}
	updates = append(updates, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE alert_rules SET `+strings.Join(updates, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	return nil
}

func (s *Service) DeleteRule(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	return nil
}

func (s *Service) SetRuleEnabled(ctx context.Context, id string, enabled bool) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE alert_rules SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, boolInt(enabled), id)
	if err != nil {
		return fmt.Errorf("set alert rule enabled: %w", err)
	}
	return nil
}

func (s *Service) LoadHistory(ctx context.Context, status string, limit int) ([]History, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var rows *sql.Rows
	if strings.TrimSpace(status) != "" {
		rows, err = db.QueryContext(ctx, `
			SELECT id, rule_id, channel_id, status, title, message, data, error_message, sent_at, retry_count, created_at
			FROM notification_history
			WHERE status = ?
			ORDER BY created_at DESC
			LIMIT ?
		`, status, limit)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT id, rule_id, channel_id, status, title, message, data, error_message, sent_at, retry_count, created_at
			FROM notification_history
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list notification history: %w", err)
	}
	defer rows.Close()
	items := []History{}
	for rows.Next() {
		item, err := scanHistory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ClearHistory(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM notification_history`)
	if err != nil {
		return fmt.Errorf("clear notification history: %w", err)
	}
	return nil
}

func (s *Service) LoadConfig(ctx context.Context) (GlobalConfig, error) {
	db, err := s.open(ctx)
	if err != nil {
		return GlobalConfig{}, err
	}
	defer db.Close()
	var row struct {
		maxRetry       sql.NullInt64
		retryInterval  sql.NullInt64
		retention      sql.NullInt64
		enableBatch    sql.NullInt64
		batchInterval  sql.NullInt64
		defaultCh      sql.NullString
		rateLimit      sql.NullInt64
		autoEscalation sql.NullInt64
		baseURL        sql.NullString
	}
	err = db.QueryRowContext(ctx, `
		SELECT max_retry_times, retry_interval_seconds, history_retention_days,
			enable_batch, batch_interval_seconds, default_channels,
			global_rate_limit_per_hour, enable_auto_escalation, base_url
		FROM notification_global_config WHERE id = 1
	`).Scan(&row.maxRetry, &row.retryInterval, &row.retention, &row.enableBatch, &row.batchInterval, &row.defaultCh, &row.rateLimit, &row.autoEscalation, &row.baseURL)
	if err != nil {
		return GlobalConfig{}, fmt.Errorf("load notification config: %w", err)
	}
	return GlobalConfig{
		MaxRetryTimes:        intDefault(row.maxRetry, 3),
		RetryIntervalSeconds: intDefault(row.retryInterval, 60),
		HistoryRetentionDays: intDefault(row.retention, 30),
		EnableBatch:          !row.enableBatch.Valid || row.enableBatch.Int64 == 1,
		BatchIntervalSeconds: intDefault(row.batchInterval, 30),
		DefaultChannels:      parseStringList(row.defaultCh.String),
		GlobalRateLimitPerHr: intDefault(row.rateLimit, 100),
		EnableAutoEscalation: row.autoEscalation.Valid && row.autoEscalation.Int64 == 1,
		BaseURL:              row.baseURL.String,
	}, nil
}

func (s *Service) UpdateConfig(ctx context.Context, payload map[string]interface{}) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE notification_global_config
		SET max_retry_times = ?,
			retry_interval_seconds = ?,
			history_retention_days = ?,
			enable_batch = ?,
			batch_interval_seconds = ?,
			default_channels = ?,
			global_rate_limit_per_hour = ?,
			enable_auto_escalation = ?,
			base_url = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`,
		intValue(payload["max_retry_times"], 3),
		intValue(payload["retry_interval_seconds"], 60),
		intValue(payload["history_retention_days"], 30),
		boolInt(boolValue(payload["enable_batch"], true)),
		intValue(payload["batch_interval_seconds"], 30),
		jsonString(stringList(payload["default_channels"])),
		intValue(payload["global_rate_limit_per_hour"], 100),
		boolInt(boolValue(payload["enable_auto_escalation"], false)),
		stringValue(payload["base_url"]),
	)
	if err != nil {
		return fmt.Errorf("update notification config: %w", err)
	}
	return nil
}

func (s *Service) DryRun(ctx context.Context, rule Rule, eventData map[string]interface{}) (map[string]interface{}, error) {
	loc, timeZoneName := s.systemLocation(ctx)
	timeAllowed := checkTimeWindow(rule.TimeWindow, loc)
	conditions := evaluateConditions(rule.Conditions, eventData)
	maintenance, err := s.matchMaintenance(ctx, eventData)
	if err != nil {
		return nil, err
	}
	channels := []map[string]interface{}{}
	for _, channelID := range rule.Channels {
		channel, ok, err := s.loadStoredChannel(ctx, channelID)
		if err != nil {
			return nil, err
		}
		channels = append(channels, map[string]interface{}{
			"id":      channelID,
			"name":    defaultString(channel.Name, "Channel "+channelID),
			"type":    defaultString(channel.Type, "unknown"),
			"enabled": ok && channel.Enabled != 0,
			"exists":  ok,
		})
	}
	wouldNotify := timeAllowed && conditions.Allowed && maintenance == nil
	if wouldNotify {
		wouldNotify = false
		for _, channel := range channels {
			if channel["exists"] == true && channel["enabled"] == true {
				wouldNotify = true
				break
			}
		}
	}
	return map[string]interface{}{
		"matched":         true,
		"wouldNotify":     wouldNotify,
		"title":           formatTitle(rule, eventData),
		"message":         formatMessage(rule, eventData, loc),
		"fingerprint":     generateFingerprint(rule, eventData),
		"timeAllowed":     timeAllowed,
		"timeZone":        timeZoneName,
		"conditionResult": conditions,
		"maintenance":     maintenance,
		"channels":        channels,
		"diagnostics": []string{
			"dry-run does not enqueue or send notifications",
			map[bool]string{true: "conditions matched", false: "conditions did not match"}[conditions.Allowed],
		},
		"conditions": rule.Conditions,
	}, nil
}

func (s *Service) Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	rules, err := s.loadEnabledRulesByEvent(ctx, sourceModule, eventType)
	if err != nil {
		return err
	}
	loc, _ := s.systemLocation(ctx)
	lifecycle, hasLifecycle := notificationMessageLifecycle(sourceModule, eventType, eventData)
	if hasLifecycle {
		var err error
		eventData, err = s.enrichLifecycleEventData(ctx, sourceModule, eventType, lifecycle, eventData, time.Now())
		if err != nil {
			return err
		}
	}
	// 告警发送参数（重试/限流）逐触发读取；读失败退回默认值，不阻塞监控关键路径。
	var loadNotifyCfg = func() GlobalConfig {
		cfg, err := s.LoadConfig(ctx)
		if err != nil {
			cfg = GlobalConfig{MaxRetryTimes: 3, RetryIntervalSeconds: 60, GlobalRateLimitPerHr: 100}
		}
		return cfg
	}
	for _, rule := range rules {
		dryRun, err := s.DryRun(ctx, rule, eventData)
		if err != nil {
			return err
		}
		if dryRun["wouldNotify"] != true {
			continue
		}
		// quiet_until 静默：未到期的时间窗内整条规则跳过
		if quietUntilActive(rule.QuietUntil, time.Now()) {
			continue
		}
		// 恢复（resolve）阶段跳过重复抑制，避免恢复通知被吞导致告警永远悬着
		trackSuppression := !(hasLifecycle && lifecycle.Phase == "resolve")
		var fingerprint string
		if trackSuppression {
			fingerprint = generateFingerprint(rule, eventData)
			suppress, err := s.evaluateSuppression(ctx, rule, fingerprint, time.Now())
			if err != nil {
				// DB 错误 fail-open：宁多发不漏发
				suppress = false
			}
			if suppress {
				continue
			}
		}
		cfg := loadNotifyCfg()
		sentAny := false
		for _, channelID := range rule.Channels {
			channel, ok, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if !ok || channel.Enabled == 0 {
				continue
			}
			if allowed, firstReject := s.rateLimiter.Allow(time.Now(), cfg.GlobalRateLimitPerHr); !allowed {
				if firstReject {
					slog.Warn("notification-global-rate-limit", "rule", rule.Name, "channel", channel.Name, "limit", cfg.GlobalRateLimitPerHr)
				}
				continue
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData, loc)
			logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
			if err != nil {
				return err
			}
			channelConfig := decryptConfig(channel.ConfigRaw)
			send := func() (deliveryResult, error) {
				if channel.Type == "telegram" && hasLifecycle {
					return s.deliverLifecycleTelegram(ctx, channel, channelConfig, sourceModule, eventType, lifecycle, title, message)
				}
				return s.sendToChannel(ctx, channel, channelConfig, title, message)
			}
			delivery, retries, sendErr := s.sendWithRetry(ctx, send, cfg.MaxRetryTimes, cfg.RetryIntervalSeconds)
			if sendErr != nil {
				_ = s.updateHistoryRetry(ctx, logID, retries, sendErr.Error())
				continue
			}
			sentAny = true
			if retries > 0 {
				_ = s.updateHistoryRetryCount(ctx, logID, retries)
			}
			if channel.Type == "telegram" && hasLifecycle && lifecycle.Phase != "resolve" && delivery.MessageID != 0 {
				_ = s.upsertTelegramMessageState(ctx, telegramMessageState{
					ChannelID: channel.ID, SourceModule: sourceModule, ResourceKey: lifecycle.ResourceKey,
					Kind: lifecycle.Kind, ChatID: delivery.ChatID, MessageID: delivery.MessageID,
				}, eventType, eventData)
			}
			now := time.Now().In(loc).Format(time.RFC3339)
			_ = s.updateHistoryStatus(ctx, logID, "sent", &now, nil)
		}
		if trackSuppression && sentAny {
			_ = s.recordSuppressionSent(ctx, rule.ID, fingerprint, time.Now())
		}
	}
	if hasLifecycle && lifecycle.Phase == "resolve" {
		_ = s.deleteTelegramMessageStates(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	}
	return nil
}

// RefreshLifecycle updates an active Telegram lifecycle message without re-sending other channels.
// 支持 open（刷新进行中事件）与 resolve（自愈恢复：把残留 open 消息编辑为恢复内容并清除状态）。
func (s *Service) RefreshLifecycle(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	lifecycle, ok := notificationMessageLifecycle(sourceModule, eventType, eventData)
	if !ok {
		return nil
	}
	var err error
	eventData, err = s.enrichLifecycleEventData(ctx, sourceModule, eventType, lifecycle, eventData, time.Now())
	if err != nil {
		return err
	}
	if lifecycle.Phase == "open" {
		eventData["lifecycleMutation"] = "refresh"
	}
	rules, err := s.loadEnabledRulesByEvent(ctx, sourceModule, eventType)
	if err != nil {
		return err
	}
	loc, _ := s.systemLocation(ctx)
	for _, rule := range rules {
		dryRun, err := s.DryRun(ctx, rule, eventData)
		if err != nil {
			return err
		}
		if dryRun["wouldNotify"] != true {
			continue
		}
		for _, channelID := range rule.Channels {
			channel, found, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if !found || channel.Enabled == 0 || channel.Type != "telegram" {
				continue
			}
			state, found, err := s.loadTelegramMessageState(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
			if err != nil {
				return err
			}
			if !found || !telegramLifecycleRefreshDue(state.UpdatedAt, time.Now()) {
				continue
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData, loc)
			config := decryptConfig(channel.ConfigRaw)
			if err := s.editTelegram(ctx, config, state.ChatID, state.MessageID, title, message); err == nil {
				s.completeLifecycleRefresh(ctx, lifecycle, state, eventType, eventData, title, message, rule, channel.ID, loc)
				continue
			}
			delivery, sendErr := s.sendTelegram(ctx, config, title, message)
			if sendErr != nil {
				continue
			}
			s.completeLifecycleRefresh(ctx, lifecycle, state, eventType, eventData, title, message, rule, channel.ID, loc)
			_ = s.upsertTelegramMessageState(ctx, telegramMessageState{
				ChannelID: channel.ID, SourceModule: sourceModule, ResourceKey: lifecycle.ResourceKey,
				Kind: lifecycle.Kind, ChatID: delivery.ChatID, MessageID: delivery.MessageID,
			}, eventType, eventData)
		}
	}
	// resolve 兜底：后端重启后残留 open 状态且无恢复规则覆盖时，仍把动态消息编辑为恢复内容并清除状态。
	// 顺带把上文规则循环中 edit/重发均失败的残留状态再尝试一次。
	if lifecycle.Phase == "resolve" {
		s.reconcileStaleLifecycleMessages(ctx, sourceModule, eventType, lifecycle, eventData, loc)
	}
	return nil
}

// completeLifecycleRefresh 记录一次生命周期刷新历史；resolve 场景同时清除该渠道的消息状态。
func (s *Service) completeLifecycleRefresh(ctx context.Context, lifecycle messageLifecycle, state telegramMessageState, eventType string, eventData map[string]interface{}, title, message string, rule Rule, channelID string, loc *time.Location) {
	_ = s.recordLifecycleRefreshHistory(ctx, rule, channelID, title, message, eventData, loc)
	if lifecycle.Phase == "resolve" {
		_ = s.deleteTelegramMessageStateForChannel(ctx, channelID, lifecycle.SourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	} else {
		_ = s.touchTelegramMessageState(ctx, state, eventType, eventData)
	}
}

// reconcileStaleLifecycleMessages 处理规则未覆盖/刷新失败的残留生命周期状态：
// 逐一编辑为恢复内容（失败则重发新消息），成功后按渠道清除状态。
func (s *Service) reconcileStaleLifecycleMessages(ctx context.Context, sourceModule, eventType string, lifecycle messageLifecycle, eventData map[string]interface{}, loc *time.Location) {
	states, err := s.listTelegramMessageStates(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return
	}
	for _, state := range states {
		channel, found, err := s.loadStoredChannel(ctx, state.ChannelID)
		if err != nil || !found || channel.Enabled == 0 || channel.Type != "telegram" {
			continue
		}
		config := decryptConfig(channel.ConfigRaw)
		fallbackRule := Rule{
			Name:      firstNonEmpty(notificationSubject(eventData), "恢复通知"),
			EventType: eventType,
			Severity:  "warning",
		}
		title := formatTitle(fallbackRule, eventData)
		message := formatMessage(fallbackRule, eventData, loc)
		if err := s.editTelegram(ctx, config, state.ChatID, state.MessageID, title, message); err == nil {
			_ = s.deleteTelegramMessageStateForChannel(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
			_ = s.recordLifecycleRefreshHistory(ctx, fallbackRule, channel.ID, title, message, eventData, loc)
			continue
		}
		if _, sendErr := s.sendTelegram(ctx, config, title, message); sendErr != nil {
			continue
		}
		_ = s.deleteTelegramMessageStateForChannel(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
		_ = s.recordLifecycleRefreshHistory(ctx, fallbackRule, channel.ID, title, message, eventData, loc)
	}
}

func (s *Service) enrichLifecycleEventData(ctx context.Context, sourceModule, eventType string, lifecycle messageLifecycle, eventData map[string]interface{}, now time.Time) (map[string]interface{}, error) {
	result := cloneNotificationData(eventData)
	state, found, err := s.loadAnyTelegramMessageState(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return nil, err
	}
	startedAt := now.UTC()
	previousData := map[string]interface{}{}
	if found {
		if parsed, ok := parseLifecycleStateTime(state.CreatedAt); ok {
			startedAt = parsed
		}
		previousData = parseObject(state.LastData)
		result["lifecyclePreviousEvent"] = state.EventType
		if stringValue(result["downDuration"]) == "" {
			result["downDuration"] = formatNotificationDuration(now.Sub(startedAt))
		}
	}
	result["lifecycleKind"] = lifecycle.Kind
	result["lifecyclePhase"] = lifecycle.Phase
	result["lifecycleMutation"] = lifecycle.Phase
	result["lifecycleResourceKey"] = lifecycle.ResourceKey
	result["lifecycleStartedAt"] = startedAt.Format(time.RFC3339)
	if len(previousData) > 0 {
		result["lifecycleChanges"] = notificationDataChanges(previousData, eventData)
	}
	return result, nil
}

func (s *Service) recordLifecycleRefreshHistory(ctx context.Context, rule Rule, channelID, title, message string, eventData map[string]interface{}, loc *time.Location) error {
	logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
	if err != nil {
		return err
	}
	now := time.Now()
	if loc != nil {
		now = now.In(loc)
	}
	formatted := now.Format(time.RFC3339)
	return s.updateHistoryStatus(ctx, logID, "sent", &formatted, nil)
}

func cloneNotificationData(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(data)+8)
	for key, value := range data {
		result[key] = value
	}
	return result
}

func notificationDataChanges(previous, current map[string]interface{}) map[string]interface{} {
	changes := map[string]interface{}{}
	keys := map[string]struct{}{}
	for key := range previous {
		if !strings.HasPrefix(key, "lifecycle") && key != "downDuration" {
			keys[key] = struct{}{}
		}
	}
	for key := range current {
		if !strings.HasPrefix(key, "lifecycle") && key != "downDuration" {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		before, beforeOK := previous[key]
		after, afterOK := current[key]
		if beforeOK == afterOK && jsonString(before) == jsonString(after) {
			continue
		}
		changes[key] = map[string]interface{}{"from": before, "to": after}
	}
	return changes
}

func parseLifecycleStateTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func formatNotificationDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int64(duration.Round(time.Second) / time.Second)
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d 天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d 分钟", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d 秒", seconds))
	}
	return strings.Join(parts, " ")
}

func (s *Service) loadEnabledRulesByEvent(ctx context.Context, sourceModule, eventType string) ([]Rule, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until, created_at, updated_at
		FROM alert_rules
		WHERE source_module = ? AND event_type = ? AND enabled = 1
	`, sourceModule, eventType)
	if err != nil {
		return nil, fmt.Errorf("load matching alert rules: %w", err)
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Service) loadStoredChannel(ctx context.Context, id string) (storedChannel, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return storedChannel{}, false, err
	}
	defer db.Close()
	row := db.QueryRowContext(ctx, `
		SELECT id, name, type, enabled, config, created_at, updated_at
		FROM notification_channels
		WHERE id = ?
	`, id)
	channel, err := scanStoredChannel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedChannel{}, false, nil
	}
	if err != nil {
		return storedChannel{}, false, err
	}
	return channel, true, nil
}

// SendToChannel 把一条消息直接投递到指定通知渠道的固定目标（bot token 与目标 chat 均取自渠道配置，
// 不做规则匹配/生命周期跟踪）。用于管理 AI 等模块复用通知中心已配置的渠道做结果推送。
func (s *Service) SendToChannel(ctx context.Context, channelID, title, message string) error {
	channel, ok, err := s.loadStoredChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("通知渠道 %s 不存在", channelID)
	}
	if channel.Enabled != 1 {
		return fmt.Errorf("通知渠道 %s 已停用", channelID)
	}
	cfg := decryptConfig(channel.ConfigRaw)
	if len(cfg) == 0 {
		return fmt.Errorf("通知渠道 %s 配置为空", channelID)
	}
	_, err = s.sendToChannel(ctx, channel, cfg, title, message)
	return err
}

// SendRichToChannel 以富消息（GFM Markdown）直接投递到通知渠道，与 SendToChannel 相同定位，
// 但保留 AI 简报的 Markdown 结构（标题/加粗/表格/代码块）——SendToChannel 的逐行转义
// 是为键值式监控通知设计的，会把 AI 输出的 | 表格 |、### 标题、**加粗** 全部转义成字面量。
// Telegram 走 sendRichMessage（Bot API 富消息扩展）；非 Telegram 渠道回退 sendToChannel。
func (s *Service) SendRichToChannel(ctx context.Context, channelID, title, markdown string) error {
	channel, ok, err := s.loadStoredChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("通知渠道 %s 不存在", channelID)
	}
	if channel.Enabled != 1 {
		return fmt.Errorf("通知渠道 %s 已停用", channelID)
	}
	cfg := decryptConfig(channel.ConfigRaw)
	if len(cfg) == 0 {
		return fmt.Errorf("通知渠道 %s 配置为空", channelID)
	}
	if channel.Type == "telegram" {
		return s.sendTelegramRich(ctx, cfg, title, markdown)
	}
	_, err = s.sendToChannel(ctx, channel, cfg, title, markdown)
	return err
}

// sendTelegramRich 用富消息（sendRichMessage + rich_message.markdown）发送，
// 保留 GFM 表格/标题/加粗；不可用时降级为普通 sendMessage（无 parse_mode，纯文本不丢消息）。
func (s *Service) sendTelegramRich(ctx context.Context, cfg map[string]interface{}, title, markdown string) error {
	token := stringValue(cfg["bot_token"])
	chatID := stringValue(cfg["chat_id"])
	if token == "" || chatID == "" {
		return errors.New("telegram channel config incomplete")
	}
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(markdown)
	if title != "" {
		text = "*" + telegramEscapeBold(title) + "*\n\n" + text
	}
	payload := map[string]interface{}{
		"chat_id": chatID,
		"rich_message": map[string]interface{}{
			"markdown": text,
		},
	}
	if _, err := s.callTelegram(ctx, client, token, "sendRichMessage", payload); err == nil {
		return nil
	} else {
		// 降级：老 Bot API 服务器不支持 sendRichMessage 时以普通文本发送。
		slog.Warn("telegram-send-rich-fallback", "chatId", chatID, "err", err.Error(), "textLen", len(text))
	}
	_, err = s.callTelegram(ctx, client, token, "sendMessage", map[string]interface{}{
		"chat_id": chatID, "text": text, "disable_web_page_preview": true,
	})
	return err
}

func (s *Service) createHistory(ctx context.Context, ruleID, channelID, status, title, message string, data map[string]interface{}, errorMessage *string) (int64, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	dataJSON := jsonString(data)
	result, err := db.ExecContext(ctx, `
		INSERT INTO notification_history (rule_id, channel_id, status, title, message, data, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, ruleID, channelID, status, title, message, dataJSON, errorMessage)
	if err != nil {
		return 0, fmt.Errorf("create notification history: %w", err)
	}
	return result.LastInsertId()
}

func (s *Service) updateHistoryStatus(ctx context.Context, id int64, status string, sentAt *string, errorMessage *string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE notification_history
		SET status = ?, sent_at = ?, error_message = ?
		WHERE id = ?
	`, status, sentAt, errorMessage, id)
	if err != nil {
		return fmt.Errorf("update notification history status: %w", err)
	}
	return nil
}

func (s *Service) matchMaintenance(ctx context.Context, eventData map[string]interface{}) (map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, target_type, target_id, start_at, end_at, reason, created_at
		FROM maintenance_schedules
		WHERE datetime(start_at) <= datetime('now') AND datetime(end_at) >= datetime('now')
	`)
	if err != nil {
		return nil, fmt.Errorf("load maintenance schedules: %w", err)
	}
	defer rows.Close()
	monitorID := stringValue(eventData["monitorId"])
	serverID := stringValue(eventData["serverId"])
	for rows.Next() {
		var id, targetType, startAt, endAt, createdAt string
		var targetID, reason sql.NullString
		if err := rows.Scan(&id, &targetType, &targetID, &startAt, &endAt, &reason, &createdAt); err != nil {
			return nil, err
		}
		if targetType == "global" || (targetType == "monitor" && targetID.String == monitorID) || (targetType == "server" && targetID.String == serverID) {
			return map[string]interface{}{
				"id":          id,
				"target_type": targetType,
				"target_id":   nullableStringPtr(targetID),
				"start_at":    startAt,
				"end_at":      endAt,
				"reason":      nullableStringPtr(reason),
				"created_at":  createdAt,
			}, nil
		}
	}
	return nil, rows.Err()
}

func notificationMessageLifecycle(sourceModule, eventType string, eventData map[string]interface{}) (messageLifecycle, bool) {
	sourceModule = strings.ToLower(strings.TrimSpace(sourceModule))
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	lifecycle := messageLifecycle{SourceModule: sourceModule}
	switch sourceModule {
	case "uptime":
		lifecycle.ResourceKey = stringValue(eventData["monitorId"])
		lifecycle.Kind = "availability"
		switch eventType {
		case "down":
			lifecycle.Phase = "open"
		case "up":
			lifecycle.Phase = "resolve"
		}
	case "server":
		lifecycle.ResourceKey = stringValue(eventData["serverId"])
		switch eventType {
		case "interrupted", "offline", "degraded":
			lifecycle.Kind = "availability"
			lifecycle.Phase = "open"
		case "online":
			lifecycle.Kind = "availability"
			lifecycle.Phase = "resolve"
		case "traffic_high":
			lifecycle.Kind = "traffic"
			lifecycle.Phase = "open"
		case "traffic_normal":
			lifecycle.Kind = "traffic"
			lifecycle.Phase = "resolve"
		case "cpu_high":
			lifecycle.Kind = "cpu"
			lifecycle.Phase = "open"
		case "cpu_normal":
			lifecycle.Kind = "cpu"
			lifecycle.Phase = "resolve"
		case "memory_high":
			lifecycle.Kind = "memory"
			lifecycle.Phase = "open"
		case "memory_normal":
			lifecycle.Kind = "memory"
			lifecycle.Phase = "resolve"
		case "disk_high":
			lifecycle.Kind = "disk"
			lifecycle.Phase = "open"
		case "disk_normal":
			lifecycle.Kind = "disk"
			lifecycle.Phase = "resolve"
		}
	case "system":
		lifecycle.ResourceKey = firstNonEmpty(stringValue(eventData["serverId"]), "local-host")
		lifecycle.Kind, lifecycle.Phase = metricNotificationLifecycle(eventType)
	case "github":
		lifecycle.ResourceKey = stringValue(eventData["repositoryId"])
		lifecycle.Kind = "actions"
		switch eventType {
		case "action_failed":
			lifecycle.Phase = "open"
		case "action_recovered":
			lifecycle.Phase = "resolve"
		}
	}
	if lifecycle.ResourceKey == "" || lifecycle.Kind == "" || lifecycle.Phase == "" {
		return messageLifecycle{}, false
	}
	return lifecycle, true
}

func metricNotificationLifecycle(eventType string) (string, string) {
	for _, metric := range []string{"cpu", "memory", "disk", "traffic"} {
		switch eventType {
		case metric + "_high":
			return metric, "open"
		case metric + "_normal":
			return metric, "resolve"
		}
	}
	return "", ""
}

func (s *Service) deliverLifecycleTelegram(ctx context.Context, channel storedChannel, cfg map[string]interface{}, sourceModule, eventType string, lifecycle messageLifecycle, title, message string) (deliveryResult, error) {
	state, found, err := s.loadTelegramMessageState(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return deliveryResult{}, err
	}
	if found {
		if err := s.editTelegram(ctx, cfg, state.ChatID, state.MessageID, title, message); err == nil {
			_ = s.touchTelegramMessageState(ctx, state, eventType, nil)
			return deliveryResult{ChatID: state.ChatID, MessageID: state.MessageID}, nil
		}
	}
	return s.sendTelegram(ctx, cfg, title, message)
}

func (s *Service) loadTelegramMessageState(ctx context.Context, channelID, sourceModule, resourceKey, kind string) (telegramMessageState, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return telegramMessageState{}, false, err
	}
	defer db.Close()
	state := telegramMessageState{}
	err = db.QueryRowContext(ctx, `
		SELECT channel_id, source_module, resource_key, lifecycle_kind, chat_id, message_id,
			event_type, COALESCE(last_data, '{}'), created_at, updated_at
		FROM notification_message_state
		WHERE channel_id = ? AND source_module = ? AND resource_key = ? AND lifecycle_kind = ?
	`, channelID, sourceModule, resourceKey, kind).Scan(
		&state.ChannelID, &state.SourceModule, &state.ResourceKey, &state.Kind, &state.ChatID, &state.MessageID,
		&state.EventType, &state.LastData, &state.CreatedAt, &state.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return telegramMessageState{}, false, nil
	}
	if err != nil {
		return telegramMessageState{}, false, fmt.Errorf("load telegram message state: %w", err)
	}
	return state, true, nil
}

func (s *Service) loadAnyTelegramMessageState(ctx context.Context, sourceModule, resourceKey, kind string) (telegramMessageState, bool, error) {
	states, err := s.listTelegramMessageStates(ctx, sourceModule, resourceKey, kind)
	if err != nil {
		return telegramMessageState{}, false, err
	}
	if len(states) == 0 {
		return telegramMessageState{}, false, nil
	}
	return states[0], true, nil
}

func (s *Service) listTelegramMessageStates(ctx context.Context, sourceModule, resourceKey, kind string) ([]telegramMessageState, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT channel_id, source_module, resource_key, lifecycle_kind, chat_id, message_id,
			event_type, COALESCE(last_data, '{}'), created_at, updated_at
		FROM notification_message_state
		WHERE source_module = ? AND resource_key = ? AND lifecycle_kind = ?
	`, sourceModule, resourceKey, kind)
	if err != nil {
		return nil, fmt.Errorf("load telegram lifecycle states: %w", err)
	}
	defer rows.Close()
	states := []telegramMessageState{}
	for rows.Next() {
		state := telegramMessageState{}
		if err := rows.Scan(
			&state.ChannelID, &state.SourceModule, &state.ResourceKey, &state.Kind, &state.ChatID, &state.MessageID,
			&state.EventType, &state.LastData, &state.CreatedAt, &state.UpdatedAt,
		); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func telegramLifecycleRefreshDue(updatedAt string, now time.Time) bool {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		updated, err := time.Parse(layout, updatedAt)
		if err == nil {
			return now.UTC().Sub(updated.UTC()) >= lifecycleRefreshInterval
		}
	}
	return true
}

func (s *Service) upsertTelegramMessageState(ctx context.Context, state telegramMessageState, eventType string, eventData map[string]interface{}) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO notification_message_state (
			channel_id, source_module, resource_key, lifecycle_kind, chat_id, message_id, event_type, last_data
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, source_module, resource_key, lifecycle_kind) DO UPDATE SET
			chat_id = excluded.chat_id,
			message_id = excluded.message_id,
			event_type = excluded.event_type,
			last_data = excluded.last_data,
			updated_at = CURRENT_TIMESTAMP
	`, state.ChannelID, state.SourceModule, state.ResourceKey, state.Kind, state.ChatID, state.MessageID, eventType, jsonString(eventData))
	if err != nil {
		return fmt.Errorf("save telegram message state: %w", err)
	}
	return nil
}

func (s *Service) touchTelegramMessageState(ctx context.Context, state telegramMessageState, eventType string, eventData map[string]interface{}) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	lastData := ""
	if eventData != nil {
		lastData = jsonString(eventData)
	}
	_, err = db.ExecContext(ctx, `
		UPDATE notification_message_state
		SET event_type = ?, last_data = CASE WHEN ? = '' THEN last_data ELSE ? END, updated_at = CURRENT_TIMESTAMP
		WHERE channel_id = ? AND source_module = ? AND resource_key = ? AND lifecycle_kind = ?
	`, eventType, lastData, lastData, state.ChannelID, state.SourceModule, state.ResourceKey, state.Kind)
	return err
}

func (s *Service) deleteTelegramMessageStates(ctx context.Context, sourceModule, resourceKey, kind string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		DELETE FROM notification_message_state
		WHERE source_module = ? AND resource_key = ? AND lifecycle_kind = ?
	`, sourceModule, resourceKey, kind)
	return err
}

func (s *Service) deleteTelegramMessageStateForChannel(ctx context.Context, channelID, sourceModule, resourceKey, kind string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		DELETE FROM notification_message_state
		WHERE channel_id = ? AND source_module = ? AND resource_key = ? AND lifecycle_kind = ?
	`, channelID, sourceModule, resourceKey, kind)
	return err
}

func (s *Service) sendToChannel(ctx context.Context, channel storedChannel, cfg map[string]interface{}, title, message string) (deliveryResult, error) {
	switch channel.Type {
	case "email":
		return deliveryResult{}, sendEmail(cfg, title, message)
	case "telegram":
		return s.sendTelegram(ctx, cfg, title, message)
	default:
		return deliveryResult{}, fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
}

// smtpSendTimeout 是 SMTP 发送全链路的阻塞上限：网络连接、TLS 握手、
// 认证与数据传输任何一步停滞都不会无限等待。
const smtpSendTimeout = 30 * time.Second

func sendEmail(cfg map[string]interface{}, title, message string) error {
	host := stringValue(cfg["host"])
	port := intValue(cfg["port"], 465)
	authMap := objectValue(cfg["auth"])
	user := stringValue(authMap["user"])
	pass := stringValue(authMap["pass"])
	to := stringDefault(cfg["to"], user)
	if host == "" || user == "" || pass == "" || to == "" {
		return errors.New("email channel config incomplete")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	auth := smtp.PlainAuth("", user, pass, host)
	from := user

	// Helper to encode headers using RFC 2047 (B-encoding)
	encodeHeader := func(s string) string {
		return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
	}

	if sender := stringValue(cfg["sender_name"]); sender != "" {
		from = fmt.Sprintf("%s <%s>", encodeHeader(sender), user)
	}

	htmlBody := emailMessageHTML(title, message)
	var encodedBody bytes.Buffer
	encoder := quotedprintable.NewWriter(&encodedBody)
	if _, err := encoder.Write([]byte(htmlBody)); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	messageBytes := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + encodeHeader(title) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
		encodedBody.String() + "\r\n")

	// 全链路超时护栏：网络/认证/数据阶段的任何阻塞最多持续 smtpSendTimeout，
	// 避免 SMTP 服务器不响应时通知轮询链路被无限卡死。
	dialer := &net.Dialer{Timeout: smtpSendTimeout}
	rawConn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer rawConn.Close()
	_ = rawConn.SetDeadline(time.Now().Add(smtpSendTimeout))

	var conn net.Conn = rawConn
	if boolValue(cfg["secure"], port == 465) {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Quit()
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(user); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(messageBytes); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

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

// callTelegram 调用 Telegram Bot API，返回 result 对象。
// 底层复用 tgapi 共享客户端（与 adminai 频道同一份 API 调用代码）。
func (s *Service) callTelegram(ctx context.Context, client *http.Client, token, method string, payload map[string]interface{}) (telegramAPIResponse, error) {
	env, err := tgapi.NewClient(token, client).Call(ctx, method, payload)
	if err != nil {
		return telegramAPIResponse{}, err
	}
	var result telegramAPIResponse
	result.OK = env.OK
	result.Description = env.Description
	if err := json.Unmarshal(env.Result, &result.Result); err != nil {
		return telegramAPIResponse{}, err
	}
	return result, nil
}

func (s *Service) sendTelegram(ctx context.Context, cfg map[string]interface{}, title, message string) (deliveryResult, error) {
	token := stringValue(cfg["bot_token"])
	chatID := stringValue(cfg["chat_id"])
	if token == "" || chatID == "" {
		return deliveryResult{}, errors.New("telegram channel config incomplete")
	}
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return deliveryResult{}, err
	}
	text := telegramMessageText(title, message)
	applog.Info(ctx, "notification", "telegram-rich-outgoing", "chatId", chatID, "textLen", len(text), "textHex", fmt.Sprintf("%x", []byte(text)))
	// 富消息优先（sendRichMessage + rich_message.markdown，GFM 宽松解析，对中文/emoji 渲染稳定）；
	// 失败时降级为普通文本 sendMessage（不带 parse_mode），避免旧客户端 MarkdownV2 解析乱码。
	richPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"rich_message":             map[string]interface{}{"markdown": text},
		"disable_web_page_preview": true,
	}
	if result, err := s.callTelegram(ctx, client, token, "sendRichMessage", richPayload); err == nil {
		if result.Result.Chat.ID != 0 {
			chatID = strconv.FormatInt(result.Result.Chat.ID, 10)
		}
		return deliveryResult{ChatID: chatID, MessageID: result.Result.MessageID}, nil
	} else {
		slog.Warn("telegram-rich-fallback", "chatId", chatID, "err", err.Error(), "textLen", len(text))
	}
	plainPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	result, err := s.callTelegram(ctx, client, token, "sendMessage", plainPayload)
	if err != nil {
		return deliveryResult{}, err
	}
	if result.Result.Chat.ID != 0 {
		chatID = strconv.FormatInt(result.Result.Chat.ID, 10)
	}
	return deliveryResult{ChatID: chatID, MessageID: result.Result.MessageID}, nil
}

func (s *Service) editTelegram(ctx context.Context, cfg map[string]interface{}, chatID string, messageID int64, title, message string) error {
	token := stringValue(cfg["bot_token"])
	if token == "" || chatID == "" || messageID == 0 {
		return errors.New("telegram message state incomplete")
	}
	text := telegramMessageText(title, message)
	client, err := s.telegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	// 富消息优先（editMessageText + rich_message.markdown，GFM 宽松解析）：
	// 消息由 sendRichMessage 创建，只有用相同富格式编辑才能覆盖原内容；
	// 旧实现用 MarkdownV2 编辑会解析失败，导致 RefreshLifecycle 回退重发新消息。
	richPayload := map[string]interface{}{
		"chat_id":     chatID,
		"message_id":  messageID,
		"rich_message": map[string]interface{}{"markdown": text},
	}
	_, err = s.callTelegram(ctx, client, token, "editMessageText", richPayload)
	if err == nil {
		return nil
	}
	if telegramEditIgnore(err) {
		return nil
	}
	slog.Warn("telegram-edit-rich-fallback", "chatId", chatID, "msgId", messageID, "err", err.Error(), "textLen", len(text))
	// 降级：富消息不可用时以普通文本编辑（不带 parse_mode，避免 MarkdownV2 解析乱码）。
	plainPayload := map[string]interface{}{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	_, derr := s.callTelegram(ctx, client, token, "editMessageText", plainPayload)
	if derr == nil || telegramEditIgnore(derr) {
		return nil
	}
	return derr
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

// telegramMessageText 组装 Rich Markdown（GFM）消息体：标题加粗、键值行用
// **label：** 粗体标签，行间用 GFM 硬换行（行尾两个空格）保证富文本渲染真正换行
// （普通 LF 在 GFM 中属于软换行，会合并为一行）。
func telegramMessageText(title, message string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered := telegramMessageLine(line)
		if rendered == "" {
			continue
		}
		formatted = append(formatted, rendered+"  ")
	}
	body := strings.Join(formatted, "\n")
	head := title
	if body == "" {
		return normalizeRichTextColons(head)
	}
	return normalizeRichTextColons(head + "\n\n" + body + "\n\nAPI Monitor")
}

// normalizeRichTextColons 富文本（sendRichMessage markdown）渲染在部分客户端会把
// 全角冒号（U+FF1A）显示为替换字符（��），发送前统一转为半角冒号避免乱码。
func normalizeRichTextColons(text string) string {
	return strings.ReplaceAll(text, "：", ":")
}

func telegramMessageLine(line string) string {
	applog.Info(context.Background(), "notification", "telegram-line-in", "lineHex", fmt.Sprintf("%x", []byte(line)))
	field := parseNotificationMessageLine(line)
	if field.Empty {
		return ""
	}
	applog.Info(context.Background(), "notification", "telegram-line-parse", "labelHex", fmt.Sprintf("%x", []byte(field.Label)), "valueHex", fmt.Sprintf("%x", []byte(field.Value)))
	if field.Label == "" {
		return field.Value
	}
	value := field.Value
	if field.Label == "状态" || strings.EqualFold(field.Label, "status") {
		value = notificationStatusIcon(field.Value) + value
	}
	if isNotificationCodeField(field.Label) {
		value = "`" + field.Value + "`"
	}
	return field.Label + ": " + value
}

// telegramEscapeV2 转义 Telegram MarkdownV2 特殊字符。
// MarkdownV2 规定除 pre/code（仅 ` 与 \）和链接 URL（仅 ) 与 \）外，
// 全部保留字符 _ * [ ] ( ) ~ ` > # + - = | { } . ! 在普通文本与实体内部都必须转义，
// 否则 Telegram 返回 "can't parse entities"。
func telegramEscapeV2(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// telegramEscapeBold 转义 MarkdownV2 加粗实体内部内容（同全量转义）。
func telegramEscapeBold(s string) string { return telegramEscapeV2(s) }

// telegramEscapeCode 转义 MarkdownV2 行内代码内部（仅 ` 与 \ 需转义）。
func telegramEscapeCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '`' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

type notificationMessageField struct {
	Label string
	Value string
	Empty bool
}

func parseNotificationMessageLine(line string) notificationMessageField {
	line = strings.TrimSpace(line)
	if line == "" {
		return notificationMessageField{Empty: true}
	}
	separator := strings.Index(line, ":")
	separatorLen := 1
	if chineseSeparator := strings.Index(line, "："); chineseSeparator >= 0 && (separator < 0 || chineseSeparator < separator) {
		// 全角冒号为 3 字节（U+FF1A），value 切片必须跳过完整分隔符，
		// 否则残留后两字节（BC 9A）导致 Telegram 端显示乱码。
		separator = chineseSeparator
		separatorLen = 3
	}
	if separator <= 0 {
		return notificationMessageField{Value: line}
	}
	label := strings.TrimSpace(line[:separator])
	if len([]rune(label)) > 32 {
		return notificationMessageField{Value: line}
	}
	return notificationMessageField{Label: label, Value: strings.TrimSpace(line[separator+separatorLen:])}
}

func isNotificationCodeField(label string) bool {
	return label == "地址" || label == "链接" || label == "云端链接" || strings.EqualFold(label, "url") ||
		strings.EqualFold(label, "host") || strings.EqualFold(label, "address")
}

func notificationStatusIcon(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "online", "up", "recovered", "success", "在线", "恢复", "已恢复", "成功":
		return "🟢 "
	case "offline", "down", "failed", "failure", "离线", "故障", "失败":
		return "🔴 "
	case "interrupted", "degraded", "warning", "中断", "采集异常", "告警", "警告":
		return "🟠 "
	default:
		return ""
	}
}

func emailMessageHTML(title, message string) string {
	accent := "#3b82f6"
	statusBackground := "#eff6ff"
	statusText := "#1d4ed8"
	lowerMessage := strings.ToLower(message)
	if strings.Contains(lowerMessage, "状态: 离线") || strings.Contains(lowerMessage, "状态: 故障") || strings.Contains(lowerMessage, "status: offline") || strings.Contains(lowerMessage, "status: down") {
		accent, statusBackground, statusText = "#dc2626", "#fef2f2", "#b91c1c"
	} else if strings.Contains(lowerMessage, "状态: 在线") || strings.Contains(lowerMessage, "状态: 已恢复") || strings.Contains(lowerMessage, "status: online") || strings.Contains(lowerMessage, "status: recovered") {
		accent, statusBackground, statusText = "#16a34a", "#f0fdf4", "#15803d"
	} else if strings.Contains(lowerMessage, "状态: 告警") || strings.Contains(lowerMessage, "状态: 中断") || strings.Contains(lowerMessage, "status: warning") {
		accent, statusBackground, statusText = "#d97706", "#fffbeb", "#b45309"
	}

	var rows strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(message), "\n") {
		field := parseNotificationMessageLine(line)
		switch {
		case field.Empty:
			rows.WriteString(`<tr><td colspan="2" style="height:8px"></td></tr>`)
		case field.Label == "":
			rows.WriteString(`<tr><td colspan="2" style="padding:5px 0;color:#334155;font-size:14px;line-height:1.6">`)
			rows.WriteString(html.EscapeString(field.Value))
			rows.WriteString(`</td></tr>`)
		default:
			value := html.EscapeString(field.Value)
			if field.Label == "状态" || strings.EqualFold(field.Label, "status") {
				value = notificationStatusIcon(field.Value) + value
			}
			if isNotificationCodeField(field.Label) {
				value = `<code style="font-family:Consolas,monospace;font-size:12px;color:#0f172a;word-break:break-all">` + value + `</code>`
			}
			rows.WriteString(`<tr><td style="width:104px;padding:5px 12px 5px 0;color:#64748b;font-size:13px;vertical-align:top">`)
			rows.WriteString(html.EscapeString(field.Label))
			rows.WriteString(`</td><td style="padding:5px 0;color:#0f172a;font-size:14px;font-weight:600;line-height:1.5">`)
			rows.WriteString(value)
			rows.WriteString(`</td></tr>`)
		}
	}

	return `<!doctype html><html><body style="margin:0;padding:24px;background:#f1f5f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:620px;background:#ffffff;border:1px solid #e2e8f0;border-radius:8px;overflow:hidden">` +
		`<tr><td style="height:5px;background:` + accent + `"></td></tr>` +
		`<tr><td style="padding:24px 28px 10px"><div style="font-size:12px;font-weight:700;color:` + statusText + `;text-transform:uppercase">API Monitor</div>` +
		`<h1 style="margin:8px 0 0;font-size:20px;line-height:1.4;color:#0f172a">` + html.EscapeString(title) + `</h1></td></tr>` +
		`<tr><td style="padding:12px 28px 26px"><div style="padding:14px 16px;border-left:3px solid ` + accent + `;background:` + statusBackground + `;border-radius:4px">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0">` + rows.String() + `</table></div></td></tr>` +
		`</table></td></tr></table></body></html>`
}

func (s *Service) telegramHTTPClient(cfg map[string]interface{}) (*http.Client, error) {
	proxyAddress := strings.TrimSpace(stringValue(cfg["proxy_url"]))
	if proxyAddress == "" {
		return s.client, nil
	}
	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Host == "" {
		return nil, errors.New("telegram proxy URL is invalid")
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("telegram proxy scheme must be http, https, or socks5")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	timeout := s.client.Timeout
	if timeout <= 0 {
		timeout = requestTimeout
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
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

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('email', 'telegram')),
			enabled INTEGER DEFAULT 1,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source_module TEXT NOT NULL,
			event_type TEXT NOT NULL,
			severity TEXT DEFAULT 'warning' CHECK(severity IN ('critical', 'warning', 'info')),
			enabled INTEGER DEFAULT 1,
			channels TEXT NOT NULL,
			conditions TEXT,
			suppression TEXT,
			time_window TEXT,
			description TEXT,
			title_template TEXT DEFAULT '',
			message_template TEXT DEFAULT '',
			backup_channels TEXT DEFAULT '[]',
			quiet_until DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS maintenance_schedules (
			id TEXT PRIMARY KEY,
			target_type TEXT NOT NULL,
			target_id TEXT,
			start_at DATETIME NOT NULL,
			end_at DATETIME NOT NULL,
			reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS notification_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'failed', 'retrying')),
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			data TEXT,
			error_message TEXT,
			sent_at DATETIME,
			retry_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS notification_message_state (
			channel_id TEXT NOT NULL,
			source_module TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			lifecycle_kind TEXT NOT NULL,
			chat_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			last_data TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (channel_id, source_module, resource_key, lifecycle_kind)
		)`,
		`CREATE TABLE IF NOT EXISTS alert_state_tracking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			last_triggered_at DATETIME NOT NULL,
			consecutive_failures INTEGER DEFAULT 1,
			last_notified_at DATETIME,
			metadata TEXT,
			state_history TEXT DEFAULT '[]',
			is_flapping INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(rule_id, fingerprint)
		)`,
		`CREATE TABLE IF NOT EXISTS notification_global_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			max_retry_times INTEGER DEFAULT 3,
			retry_interval_seconds INTEGER DEFAULT 60,
			history_retention_days INTEGER DEFAULT 30,
			enable_batch INTEGER DEFAULT 1,
			batch_interval_seconds INTEGER DEFAULT 30,
			default_channels TEXT,
			global_rate_limit_per_hour INTEGER DEFAULT 100,
			enable_auto_escalation INTEGER DEFAULT 0,
			base_url TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_channels_type ON notification_channels(type, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_rules_source ON alert_rules(source_module, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_rule ON notification_history(rule_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_status ON notification_history(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_history_created ON notification_history(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_message_resource ON notification_message_state(source_module, resource_key, lifecycle_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_rule ON alert_state_tracking(rule_id, fingerprint)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_triggered ON alert_state_tracking(last_triggered_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure notification schema: %w", err)
		}
	}
	for _, column := range []struct {
		table string
		name  string
		sql   string
	}{
		{"alert_rules", "title_template", "ALTER TABLE alert_rules ADD COLUMN title_template TEXT DEFAULT ''"},
		{"alert_rules", "message_template", "ALTER TABLE alert_rules ADD COLUMN message_template TEXT DEFAULT ''"},
		{"alert_rules", "backup_channels", "ALTER TABLE alert_rules ADD COLUMN backup_channels TEXT DEFAULT '[]'"},
		{"alert_rules", "quiet_until", "ALTER TABLE alert_rules ADD COLUMN quiet_until DATETIME"},
		{"alert_state_tracking", "state_history", "ALTER TABLE alert_state_tracking ADD COLUMN state_history TEXT DEFAULT '[]'"},
		{"alert_state_tracking", "is_flapping", "ALTER TABLE alert_state_tracking ADD COLUMN is_flapping INTEGER DEFAULT 0"},
		{"notification_message_state", "last_data", "ALTER TABLE notification_message_state ADD COLUMN last_data TEXT DEFAULT '{}'"},
		{"notification_global_config", "global_rate_limit_per_hour", "ALTER TABLE notification_global_config ADD COLUMN global_rate_limit_per_hour INTEGER DEFAULT 100"},
		{"notification_global_config", "enable_auto_escalation", "ALTER TABLE notification_global_config ADD COLUMN enable_auto_escalation INTEGER DEFAULT 0"},
		{"notification_global_config", "base_url", "ALTER TABLE notification_global_config ADD COLUMN base_url TEXT"},
	} {
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
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO notification_global_config (
			id, max_retry_times, retry_interval_seconds,
			history_retention_days, enable_batch, batch_interval_seconds,
			default_channels, global_rate_limit_per_hour, enable_auto_escalation, base_url
		) VALUES (1, 3, 60, 30, 1, 30, '[]', 100, 0, '')
	`); err != nil {
		return fmt.Errorf("ensure notification default config: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanStoredChannel(row scanner) (storedChannel, error) {
	var item storedChannel
	var createdAt, updatedAt sql.NullString
	if err := row.Scan(&item.ID, &item.Name, &item.Type, &item.Enabled, &item.ConfigRaw, &createdAt, &updatedAt); err != nil {
		return storedChannel{}, err
	}
	item.CreatedAt = createdAt.String
	item.UpdatedAt = updatedAt.String
	return item, nil
}

func publicChannel(item storedChannel) Channel {
	return Channel{
		ID:        item.ID,
		Name:      item.Name,
		Type:      item.Type,
		Enabled:   item.Enabled,
		Config:    decryptConfig(item.ConfigRaw),
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func findRule(ctx context.Context, db *sql.DB, id string) (Rule, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until, created_at, updated_at
		FROM alert_rules
		WHERE id = ?
	`, id)
	return scanRule(row)
}

func scanRule(row scanner) (Rule, error) {
	var rule Rule
	var channels, conditions, suppression, timeWindow, backupChannels sql.NullString
	var description, titleTemplate, messageTemplate, quietUntil, createdAt, updatedAt sql.NullString
	if err := row.Scan(
		&rule.ID,
		&rule.Name,
		&rule.SourceModule,
		&rule.EventType,
		&rule.Severity,
		&rule.Enabled,
		&channels,
		&conditions,
		&suppression,
		&timeWindow,
		&description,
		&titleTemplate,
		&messageTemplate,
		&backupChannels,
		&quietUntil,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Rule{}, err
	}
	rule.Channels = parseStringList(channels.String)
	rule.Conditions = parseObject(conditions.String)
	rule.Suppression = parseObject(suppression.String)
	rule.TimeWindow = parseObject(timeWindow.String)
	if len(rule.TimeWindow) == 0 {
		rule.TimeWindow = map[string]interface{}{"enabled": false}
	}
	rule.Description = description.String
	rule.TitleTemplate = titleTemplate.String
	rule.MessageTemplate = messageTemplate.String
	rule.BackupChannels = parseStringList(backupChannels.String)
	rule.QuietUntil = nullableStringPtr(quietUntil)
	rule.CreatedAt = createdAt.String
	rule.UpdatedAt = updatedAt.String
	return rule, nil
}

func scanHistory(row scanner) (History, error) {
	var item History
	var data, errorMessage, sentAt sql.NullString
	if err := row.Scan(&item.ID, &item.RuleID, &item.ChannelID, &item.Status, &item.Title, &item.Message, &data, &errorMessage, &sentAt, &item.RetryCount, &item.CreatedAt); err != nil {
		return History{}, err
	}
	item.Data = data.String
	item.ErrorMessage = nullableStringPtr(errorMessage)
	item.SentAt = nullableStringPtr(sentAt)
	return item, nil
}

func decryptConfig(raw string) map[string]interface{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}
	}
	plain := secure.SecureDecrypt(raw)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func eventCatalog() []map[string]interface{} {
	return []map[string]interface{}{
		{"module": "uptime", "events": []string{"down", "up", "pending", "resource.created", "resource.deleted", "ssl_expiry"}, "dynamic_events": []string{"down", "up"}},
		{"module": "server", "events": []string{"offline", "online", "interrupted", "degraded", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal", "traffic_high", "traffic_normal"}, "dynamic_events": []string{"offline", "online", "interrupted", "degraded", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal", "traffic_high", "traffic_normal"}},
		{"module": "system", "events": []string{"database.backup", "database.import", "log.cleanup", "migration.failed", "cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal"}, "dynamic_events": []string{"cpu_high", "cpu_normal", "memory_high", "memory_normal", "disk_high", "disk_normal"}},
		{"module": "filebox", "events": []string{"resource.created", "resource.deleted", "cleanup"}},
		{"module": "github", "events": []string{"action_failed", "action_recovered", "release_published", "star_spike", "issue_opened", "pull_request_opened", "repository_unreachable", "token_invalid", "rate_limit_low", "webhook_delivery_failed", "webhook_ping"}, "dynamic_events": []string{"action_failed", "action_recovered"}},
		{"module": "totp", "events": []string{"resource.created", "resource.updated", "resource.deleted", "security.revealed", "backup.imported", "backup.exported"}},
		{"module": "openai", "events": []string{"gateway_error_high", "gateway_error_normal"}, "dynamic_events": []string{"gateway_error_high", "gateway_error_normal"}},
		{"module": "cron", "events": []string{"task.completed", "task.failed", "workflow.completed", "workflow.failed"}, "dynamic_events": []string{}},
	}
}

func validateSourceModule(module string) error {
	switch strings.ToLower(strings.TrimSpace(module)) {
	case "music", "openlist":
		return fmt.Errorf("%w: source module retired", errInvalidInput)
	default:
		return nil
	}
}

func formatTitle(rule Rule, data map[string]interface{}) string {
	if rule.TitleTemplate != "" {
		return renderTemplate(rule.TitleTemplate, data)
	}
	icon := notificationEventIcon(rule.EventType, rule.Severity)
	subject := notificationSubject(data)
	if subject != "" {
		return fmt.Sprintf("%s %s - %s", icon, subject, rule.Name)
	}
	return fmt.Sprintf("%s %s", icon, rule.Name)
}

func formatMessage(rule Rule, data map[string]interface{}, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	if rule.MessageTemplate != "" {
		return renderTemplate(rule.MessageTemplate, notificationTemplateData(data, loc))
	}
	lines := []string{}
	add := func(label string, value interface{}) {
		if value != nil && stringValue(value) != "" {
			lines = append(lines, fmt.Sprintf("%s: %v", label, value))
		}
	}

	statusAdded := false
	if statusVal := data["status"]; statusVal != nil {
		add("状态", notificationStatusLabel(stringValue(statusVal)))
		statusAdded = true
	}
	if !statusAdded {
		add("状态", notificationEventStatus(rule.EventType))
	}
	add("事件", notificationEventLabel(rule.EventType))
	add("级别", notificationSeverityLabel(rule.Severity))

	if monitorName := stringValue(data["monitorName"]); monitorName != "" {
		add("监控项", monitorName)
	} else {
		add("主机", firstNonEmpty(stringValue(data["serverName"]), stringValue(data["hostname"])))
	}
	add("仓库", data["repositoryFullName"])
	add("资源", firstNonEmpty(stringValue(data["resourceName"]), stringValue(data["name"])))
	add("任务", data["taskName"])
	add("工作流", data["workflowName"])
	add("结果", data["summary"])
	add("输出", data["output"])
	add("耗时", data["duration"])
	add("触发方式", data["triggerType"])

	if data["url"] != nil {
		add("地址", data["url"])
	} else if data["host"] != nil {
		add("地址", data["host"])
	}

	// 最后活跃时间
	if lastActiveVal := data["lastActive"]; lastActiveVal != nil {
		add("最后活跃", toLocalTimeStr(stringValue(lastActiveVal), loc))
	}

	add("错误原因", data["error"])
	add("延迟 (Ping)", data["ping"])
	add("CPU 使用率", formatNotificationPercent(data["cpu_usage"]))
	add("内存使用率", formatNotificationPercent(data["mem_percent"]))
	add("磁盘使用率", formatNotificationPercent(data["disk_usage"]))
	add("流量使用率", formatNotificationPercent(data["traffic_percent"]))
	add("已用流量", data["traffic_used"])
	add("流量配额", data["traffic_limit"])
	add("报警阈值", formatNotificationPercent(data["threshold"]))
	add("持续时间", data["downDuration"])
	add("证书剩余天数", data["daysLeft"])
	if expiry := stringValue(data["expiry"]); expiry != "" {
		add("证书到期时间", toLocalTimeStr(expiry, loc))
	}

	add("当前值", data["current"])
	add("之前值", data["previous"])
	add("变化量", data["delta"])
	add("Actions 状态", data["conclusion"])
	add("剩余 API 额度", data["rateLimitRemaining"])
	if reset := stringValue(data["rateLimitReset"]); reset != "" {
		add("额度重置时间", toLocalTimeStr(reset, loc))
	}
	add("链接", data["htmlUrl"])
	add("说明", firstNonEmpty(stringValue(data["message"]), stringValue(data["reason"])))

	// 网关告警（openai 模块）字段
	if rateVal, ok := data["error_rate"].(float64); ok {
		add("错误率", fmt.Sprintf("%.1f%%", rateVal))
	}
	add("请求数", data["requests"])
	add("错误数", data["errors"])
	add("统计窗口", data["windowMin"])

	// 数据库备份相关字段
	add("备份 ID", data["backupId"])
	add("备份文件名", data["fileName"])
	if sizeVal := data["size"]; sizeVal != nil {
		var sizeInt int64
		switch v := sizeVal.(type) {
		case int:
			sizeInt = int64(v)
		case int64:
			sizeInt = v
		case float64:
			sizeInt = int64(v)
		}
		if sizeInt > 0 {
			add("文件大小", formatNotificationBytes(sizeInt))
		} else {
			add("文件大小", sizeVal)
		}
	}
	add("存储位置", data["location"])
	add("云端链接", data["remoteUrl"])

	if len(lines) == 0 {
		return jsonString(data)
	}
	lines = append(lines, "", "时间: "+time.Now().In(loc).Format("2006/01/02 15:04:05"))
	return strings.Join(lines, "\n")
}

func notificationSubject(data map[string]interface{}) string {
	return firstNonEmpty(
		stringValue(data["monitorName"]), stringValue(data["serverName"]),
		stringValue(data["repositoryFullName"]), stringValue(data["fileName"]),
		stringValue(data["resourceName"]),
	)
}

func notificationEventIcon(eventType, severity string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType == "up" || eventType == "online" || eventType == "action_recovered" || strings.HasSuffix(eventType, "_normal") {
		return "🟢"
	}
	if eventType == "database.backup" || eventType == "database.import" || eventType == "release_published" || strings.HasSuffix(eventType, ".created") || strings.HasSuffix(eventType, ".exported") || strings.HasSuffix(eventType, ".imported") {
		return "✅"
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "🚨"
	case "warning":
		return "🟠"
	default:
		return "ℹ️"
	}
}

func notificationEventStatus(eventType string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case eventType == "up", eventType == "online", eventType == "action_recovered", strings.HasSuffix(eventType, "_normal"):
		return "已恢复"
	case eventType == "down", eventType == "offline":
		return "故障"
	case eventType == "interrupted":
		return "中断"
	case eventType == "degraded":
		return "采集异常"
	case strings.HasSuffix(eventType, "_high"), strings.Contains(eventType, "failed"), eventType == "ssl_expiry":
		return "告警"
	case strings.HasSuffix(eventType, ".created"), strings.HasSuffix(eventType, ".imported"), strings.HasSuffix(eventType, ".exported"), eventType == "database.backup", eventType == "database.import":
		return "成功"
	default:
		return "已触发"
	}
}

func notificationStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "online", "up":
		return "在线"
	case "offline", "down":
		return "离线"
	case "interrupted":
		return "中断"
	case "degraded":
		return "采集异常"
	case "success":
		return "成功"
	case "failed", "failure":
		return "失败"
	default:
		return status
	}
}

func notificationSeverityLabel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "严重"
	case "warning":
		return "警告"
	case "info":
		return "信息"
	default:
		return severity
	}
}

func notificationEventLabel(eventType string) string {
	labels := map[string]string{
		"down": "服务不可用", "up": "服务恢复", "pending": "状态待确认", "ssl_expiry": "SSL 证书即将到期",
		"offline": "主机离线", "online": "主机上线", "interrupted": "连接中断", "degraded": "采集异常",
		"cpu_high": "CPU 使用率过高", "cpu_normal": "CPU 恢复正常", "memory_high": "内存使用率过高", "memory_normal": "内存恢复正常",
		"disk_high": "磁盘使用率过高", "disk_normal": "磁盘恢复正常", "traffic_high": "流量使用率过高", "traffic_normal": "流量恢复正常",
		"action_failed": "GitHub Actions 执行失败", "action_recovered": "GitHub Actions 恢复正常", "release_published": "GitHub 新版本发布",
		"star_spike": "GitHub Star 激增", "issue_opened": "GitHub Issue 新增", "pull_request_opened": "GitHub PR 新增",
		"repository_unreachable": "GitHub 仓库无法访问", "token_invalid": "GitHub Token 已失效", "rate_limit_low": "GitHub API 额度偏低",
		"webhook_delivery_failed": "GitHub Webhook 投递失败", "webhook_ping": "GitHub Webhook 连通成功",
		"database.backup": "数据库备份", "database.import": "数据库恢复", "log.cleanup": "日志清理", "migration.failed": "数据库迁移失败",
		"resource.created": "资源已创建", "resource.updated": "资源已更新", "resource.deleted": "资源已删除", "cleanup": "清理任务",
		"security.revealed": "敏感信息已查看", "backup.imported": "备份已导入", "backup.exported": "备份已导出",
		"gateway_error_high": "网关错误率过高", "gateway_error_normal": "网关错误率恢复正常",
		"task.completed": "定时任务执行完成", "task.failed": "定时任务执行失败",
		"workflow.completed": "工作流执行完成", "workflow.failed": "工作流执行失败",
	}
	if label := labels[strings.ToLower(strings.TrimSpace(eventType))]; label != "" {
		return label
	}
	return eventType
}

func notificationTemplateData(data map[string]interface{}, loc *time.Location) map[string]interface{} {
	if loc == nil {
		loc = time.Local
	}
	result := make(map[string]interface{}, len(data)+3)
	for key, value := range data {
		result[key] = value
	}
	result["time"] = time.Now().In(loc).Format("2006/01/02 15:04:05")
	result["timeZone"] = loc.String()
	if lastActive := stringValue(data["lastActive"]); lastActive != "" {
		result["lastActiveLocal"] = toLocalTimeStr(lastActive, loc)
	}
	return result
}

func toLocalTimeStr(utcStr string, loc *time.Location) string {
	if utcStr == "" {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	var t time.Time
	var err error
	t, err = time.ParseInLocation("2006-01-02 15:04:05", utcStr, time.UTC)
	if err != nil {
		t, err = time.Parse(time.RFC3339, utcStr)
	}
	if err != nil {
		return utcStr
	}
	return t.In(loc).Format("2006/01/02 15:04:05")
}

func formatNotificationBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatNotificationPercent(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(stringValue(value))
	if text == "" || strings.HasSuffix(text, "%") {
		return text
	}
	return text + "%"
}

// renderTemplate 渲染通知模板。用游标式单遍扫描替换占位符：
// 缺失 key 的占位符原样保留（便于用户定位），不重复扫描、不阻塞后续
// 正常占位符，保证渲染必然终止（旧实现重建相同占位符导致死循环）。
func renderTemplate(template string, data map[string]interface{}) string {
	const maxKeys = 100
	var b strings.Builder
	b.Grow(len(template) + 32)
	rest := template
	for i := 0; i < maxKeys; i++ {
		start := strings.Index(rest, "{{")
		if start < 0 {
			b.WriteString(rest)
			return normalizeTemplateNewlines(b.String())
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			b.WriteString(rest)
			return normalizeTemplateNewlines(b.String())
		}
		end += start + 2
		b.WriteString(rest[:start])
		key := strings.TrimSpace(rest[start+2 : end])
		if value, ok := data[key]; ok {
			b.WriteString(stringValue(value))
		} else {
			b.WriteString("{{" + key + "}}")
		}
		rest = rest[end+2:]
	}
	b.WriteString(rest)
	return normalizeTemplateNewlines(b.String())
}

// normalizeTemplateNewlines 把模板里以字面量存储的 \n（反斜杠+n 两个字符，常见于
// 旧版编辑器/转义序列落库）统一还原为真实换行，保证行级解析（telegramMessageLine
// 等按行切分状态/指标）不会把整段消息当成一行导致格式错乱。
func normalizeTemplateNewlines(text string) string {
	return strings.ReplaceAll(text, `\n`, "\n")
}

func generateFingerprint(rule Rule, data map[string]interface{}) string {
	parts := []string{rule.SourceModule, rule.EventType}
	switch {
	case stringValue(data["monitorId"]) != "":
		parts = append(parts, "monitor:"+stringValue(data["monitorId"]))
	case stringValue(data["serverId"]) != "":
		parts = append(parts, "server:"+stringValue(data["serverId"]))
	case stringValue(data["accountId"]) != "":
		parts = append(parts, "account:"+stringValue(data["accountId"]))
	default:
		parts = append(parts, "global")
	}
	return strings.Join(parts, ":")
}

func evaluateConditions(conditions map[string]interface{}, data map[string]interface{}) conditionResult {
	mode := "all"
	items := []map[string]interface{}{}
	if value := stringValue(conditions["mode"]); value == "any" || value == "or" {
		mode = "any"
	}
	if rawItems, ok := conditions["items"].([]interface{}); ok {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if rawItems, ok := conditions["rules"].([]interface{}); ok && len(items) == 0 {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if rawItems, ok := conditions["conditions"].([]interface{}); ok && len(items) == 0 {
		for _, raw := range rawItems {
			items = append(items, objectValue(raw))
		}
	}
	if len(items) == 0 {
		for key, value := range conditions {
			if key == "mode" || key == "match" || key == "operator" || key == "items" || key == "rules" || key == "conditions" {
				continue
			}
			items = append(items, map[string]interface{}{"field": key, "operator": "equals", "value": value})
		}
	}
	results := []map[string]interface{}{}
	if len(items) == 0 {
		return conditionResult{Allowed: true, Mode: mode, Results: results}
	}
	allowed := mode != "any"
	for _, item := range items {
		field := firstNonEmpty(stringValue(item["field"]), stringValue(item["key"]), stringValue(item["path"]), stringValue(item["name"]))
		operator := stringDefault(item["operator"], "equals")
		expected := item["value"]
		if expected == nil {
			expected = item["expected"]
		}
		actual := pathValue(data, field)
		passed := compare(actual, expected, operator)
		results = append(results, map[string]interface{}{"field": field, "operator": operator, "expected": expected, "actual": actual, "passed": passed})
		if mode == "any" {
			allowed = allowed || passed
		} else {
			allowed = allowed && passed
		}
	}
	return conditionResult{Allowed: allowed, Mode: mode, Results: results}
}

func compare(actual, expected interface{}, operator string) bool {
	switch operator {
	case "exists":
		return actual != nil
	case "not_exists", "notExists":
		return actual == nil
	case "not_equals", "notEquals", "ne":
		return fmt.Sprint(actual) != fmt.Sprint(expected)
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "not_contains", "notContains":
		return !strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected))
	case "gt", "greater_than", "greaterThan":
		return number(actual) > number(expected)
	case "gte", "greater_or_equal", "greaterOrEqual":
		return number(actual) >= number(expected)
	case "lt", "less_than", "lessThan":
		return number(actual) < number(expected)
	case "lte", "less_or_equal", "lessOrEqual":
		return number(actual) <= number(expected)
	default:
		return fmt.Sprint(actual) == fmt.Sprint(expected)
	}
}

func checkTimeWindow(window map[string]interface{}, loc *time.Location) bool {
	if !boolValue(window["enabled"], false) {
		return true
	}
	if loc == nil {
		loc = time.Local
	}
	start := stringDefault(window["start"], "00:00")
	end := stringDefault(window["end"], "23:59")
	startMinutes, okStart := parseClock(start)
	endMinutes, okEnd := parseClock(end)
	if !okStart || !okEnd {
		return true
	}
	now := time.Now().In(loc)
	current := now.Hour()*60 + now.Minute()
	if endMinutes < startMinutes {
		return current >= startMinutes || current <= endMinutes
	}
	return current >= startMinutes && current <= endMinutes
}

func pathValue(data map[string]interface{}, path string) interface{} {
	clean := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(path), "$."), "$")
	if clean == "" {
		return data
	}
	var current interface{} = data
	for _, part := range strings.Split(clean, ".") {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
		if current == nil {
			return nil
		}
	}
	return current
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
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		response.Error(w, http.StatusBadRequest, "request parameter validation failed")
		return false
	}
	return true
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(buf)), nil
}

func jsonString(value interface{}) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func parseObject(value string) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(defaultString(value, "{}")), &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

func objectValue(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if m, ok := value.(map[string]interface{}); ok {
		return m
	}
	if raw, ok := value.(string); ok {
		return parseObject(raw)
	}
	return map[string]interface{}{}
}

func objectDefault(value interface{}, fallback map[string]interface{}) map[string]interface{} {
	result := objectValue(value)
	if len(result) == 0 {
		return fallback
	}
	return result
}

func stringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		result := []string{}
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				result = append(result, text)
			}
		}
		return result
	case string:
		return parseStringList(typed)
	default:
		return []string{}
	}
}

func parseStringList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var stringsResult []string
	if err := json.Unmarshal([]byte(value), &stringsResult); err == nil {
		return stringsResult
	}
	var raw []interface{}
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return []string{}
	}
	return stringList(raw)
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func stringDefault(value interface{}, fallback string) string {
	if text := stringValue(value); text != "" {
		return text
	}
	return fallback
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func boolValue(value interface{}, fallback bool) bool {
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
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" {
			return true
		}
		if normalized == "false" || normalized == "0" || normalized == "no" || normalized == "off" {
			return false
		}
	}
	return fallback
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func intDefault(value sql.NullInt64, fallback int) int {
	if value.Valid {
		return int(value.Int64)
	}
	return fallback
}

func boundedLimit(value string) int {
	limit := intValue(value, defaultHistoryLimit)
	if limit < 1 {
		return defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		return maxHistoryLimit
	}
	return limit
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}

func ptr(value string) *string {
	return &value
}

func sortedKeys(value map[string]interface{}) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func number(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(typed), 64)
		return parsed
	}
}

func parseClock(value string) (int, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, errHour := strconv.Atoi(parts[0])
	minute, errMinute := strconv.Atoi(parts[1])
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}
