package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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