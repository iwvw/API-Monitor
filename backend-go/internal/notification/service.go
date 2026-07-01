package notification

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	defaultHistoryLimit = 100
	maxHistoryLimit     = 500
	requestTimeout      = 10 * time.Second
)

type Service struct {
	cfg    config.Config
	store  *database.Store
	client *http.Client
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
	title := "Notification connectivity test"
	message := fmt.Sprintf("Sent at: %s\nStatus: configuration accepted", time.Now().Format(time.RFC3339))
	if err := s.sendToChannel(r.Context(), channel, config, title, message); err != nil {
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
	response.OK(w, map[string]interface{}{
		"title":     formatTitle(rule, data),
		"message":   formatMessage(rule, data),
		"variables": sortedKeys(data),
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
	timeAllowed := checkTimeWindow(rule.TimeWindow)
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
		"message":         formatMessage(rule, eventData),
		"fingerprint":     generateFingerprint(rule, eventData),
		"timeAllowed":     timeAllowed,
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
	for _, rule := range rules {
		dryRun, err := s.DryRun(ctx, rule, eventData)
		if err != nil {
			return err
		}
		if dryRun["wouldNotify"] != true {
			continue
		}
		for _, channelID := range rule.Channels {
			channel, ok, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if !ok || channel.Enabled == 0 {
				continue
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData)
			logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
			if err != nil {
				return err
			}
			sendErr := s.sendToChannel(ctx, channel, decryptConfig(channel.ConfigRaw), title, message)
			if sendErr != nil {
				_ = s.updateHistoryStatus(ctx, logID, "failed", nil, ptr(sendErr.Error()))
			} else {
				now := time.Now().Format(time.RFC3339)
				_ = s.updateHistoryStatus(ctx, logID, "sent", &now, nil)
			}
		}
	}
	return nil
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

func (s *Service) sendToChannel(ctx context.Context, channel storedChannel, cfg map[string]interface{}, title, message string) error {
	switch channel.Type {
	case "email":
		return sendEmail(cfg, title, message)
	case "telegram":
		return s.sendTelegram(ctx, cfg, title, message)
	default:
		return fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
}

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
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", user, pass, host)
	from := user
	if sender := stringValue(cfg["sender_name"]); sender != "" {
		from = fmt.Sprintf("%s <%s>", sender, user)
	}
	messageBytes := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + title + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		message + "\r\n")
	if boolValue(cfg["secure"], port == 465) {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		defer conn.Close()
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer client.Close()
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
	return smtp.SendMail(addr, auth, user, []string{to}, messageBytes)
}

func (s *Service) sendTelegram(ctx context.Context, cfg map[string]interface{}, title, message string) error {
	token := stringValue(cfg["bot_token"])
	chatID := stringValue(cfg["chat_id"])
	if token == "" || chatID == "" {
		return errors.New("telegram channel config incomplete")
	}
	payload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     "<b>" + html.EscapeString(title) + "</b>\n\n" + html.EscapeString(message),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("telegram API status %d", res.StatusCode)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}
	return nil
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
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
		{"module": "uptime", "events": []string{"down", "up", "pending", "resource.created", "resource.deleted", "ssl_expiry"}},
		{"module": "server", "events": []string{"offline", "online", "cpu_high", "memory_high", "disk_high"}},
		{"module": "system", "events": []string{"database.backup", "database.import", "log.cleanup", "migration.failed", "cpu_high", "memory_high", "disk_high"}},
		{"module": "filebox", "events": []string{"resource.created", "resource.deleted", "cleanup"}},
		{"module": "totp", "events": []string{"resource.created", "resource.updated", "resource.deleted", "security.revealed", "backup.imported", "backup.exported"}},
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
	icon := map[string]string{"critical": "【严重】", "warning": "【警告】", "info": "【提示】"}[rule.Severity]
	if icon == "" {
		icon = "【警报】"
	}
	eventType := strings.ToLower(rule.EventType)
	if eventType == "up" || eventType == "online" || strings.HasSuffix(eventType, "_normal") {
		icon = "【已恢复】"
	}
	subject := firstNonEmpty(stringValue(data["monitorName"]), stringValue(data["serverName"]))
	if subject != "" {
		return fmt.Sprintf("%s %s - %s", icon, subject, rule.Name)
	}
	severityLabel := map[string]string{"critical": "严重", "warning": "警告", "info": "提示"}[rule.Severity]
	if severityLabel == "" {
		severityLabel = "提示"
	}
	return fmt.Sprintf("%s [%s] %s", icon, severityLabel, rule.Name)
}

func formatMessage(rule Rule, data map[string]interface{}) string {
	if rule.MessageTemplate != "" {
		return renderTemplate(rule.MessageTemplate, data)
	}
	lines := []string{}
	add := func(label string, value interface{}) {
		if value != nil && stringValue(value) != "" {
			lines = append(lines, fmt.Sprintf("%s: %v", label, value))
		}
	}

	// 状态汉化
	if statusVal := data["status"]; statusVal != nil {
		statusStr := stringValue(statusVal)
		if statusStr == "success" {
			statusStr = "成功"
		} else if statusStr == "failed" {
			statusStr = "失败"
		}
		add("状态", statusStr)
	}

	add("项目", data["monitorName"])
	add("服务器", data["serverName"])
	add("错误原因", data["error"])
	if data["url"] != nil {
		add("地址", data["url"])
	} else if data["host"] != nil {
		add("地址", data["host"])
	}
	add("主机名", data["hostname"])
	add("延迟 (Ping)", data["ping"])
	add("CPU 使用率", data["cpu_usage"])
	add("内存使用率", data["mem_percent"])
	add("磁盘使用率", data["disk_usage"])
	add("报警阈值", data["threshold"])
	add("持续时间", data["downDuration"])

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
	lines = append(lines, "", "时间: "+time.Now().Format("2006-01-02 15:04:05"))
	return strings.Join(lines, "\n")
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

func renderTemplate(template string, data map[string]interface{}) string {
	result := template
	for {
		start := strings.Index(result, "{{")
		if start < 0 {
			return result
		}
		end := strings.Index(result[start+2:], "}}")
		if end < 0 {
			return result
		}
		end += start + 2
		key := strings.TrimSpace(result[start+2 : end])
		value, ok := data[key]
		replacement := "{{" + key + "}}"
		if ok {
			replacement = stringValue(value)
		}
		result = result[:start] + replacement + result[end+2:]
	}
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

func checkTimeWindow(window map[string]interface{}) bool {
	if !boolValue(window["enabled"], false) {
		return true
	}
	start := stringDefault(window["start"], "00:00")
	end := stringDefault(window["end"], "23:59")
	startMinutes, okStart := parseClock(start)
	endMinutes, okEnd := parseClock(end)
	if !okStart || !okEnd {
		return true
	}
	now := time.Now()
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
