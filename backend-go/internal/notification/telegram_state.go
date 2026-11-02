package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

)

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