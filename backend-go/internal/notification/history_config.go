package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

)

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