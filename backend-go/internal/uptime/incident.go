package uptime

import (
	"context"
	"database/sql"
	"time"
)

func (s *Service) notify(ctx context.Context, eventType string, monitor, beat map[string]interface{}, durationMs int64) {
	if s.notifier == nil {
		return
	}
	_ = s.notifier.Trigger(ctx, "uptime", eventType, uptimeNotificationData(eventType, monitor, beat, durationMs))
}

func (s *Service) refreshNotification(ctx context.Context, eventType string, monitor, beat map[string]interface{}, durationMs int64) {
	updater, ok := s.notifier.(interface {
		RefreshLifecycle(context.Context, string, string, map[string]interface{}) error
	})
	if !ok {
		return
	}
	_ = updater.RefreshLifecycle(ctx, "uptime", eventType, uptimeNotificationData(eventType, monitor, beat, durationMs))
}

func uptimeNotificationData(eventType string, monitor, beat map[string]interface{}, durationMs int64) map[string]interface{} {
	data := map[string]interface{}{
		"monitorId":   int64Value(monitor["id"], 0),
		"monitorName": stringValue(monitor["name"], ""),
		"url":         monitorTarget(monitor),
		"type":        stringValue(monitor["type"], "http"),
	}
	if lastChecked := monitor["last_checked_at"]; lastChecked != nil {
		data["lastActive"] = lastChecked
	}
	if eventType == "down" {
		data["error"] = stringValue(beat["msg"], "")
		if durationMs > 0 {
			data["downDurationMs"] = durationMs
			data["downDuration"] = formatDuration(durationMs)
		}
	} else {
		data["ping"] = intValue(beat["ping"], 0)
		data["downDurationMs"] = durationMs
		data["downDuration"] = formatDuration(durationMs)
	}
	return data
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
			last_error as lastError, last_ping as lastPing, ssl_expiry as sslExpiry, updated_at as updatedAt
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
			"sslExpiry": nil,
		}, rows.Err()
	}
	return scanMap(rows)
}

func saveState(ctx context.Context, db *sql.DB, monitorID int64, state map[string]interface{}) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO uptime_monitor_states (
			monitor_id, state, fail_count, recover_count, active_incident_id,
			last_transition_at, last_error, last_ping, ssl_expiry, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(monitor_id) DO UPDATE SET
			state = excluded.state,
			fail_count = excluded.fail_count,
			recover_count = excluded.recover_count,
			active_incident_id = excluded.active_incident_id,
			last_transition_at = excluded.last_transition_at,
			last_error = excluded.last_error,
			last_ping = excluded.last_ping,
			ssl_expiry = excluded.ssl_expiry,
			updated_at = CURRENT_TIMESTAMP
	`, monitorID, stateText(state["state"]), intValue(state["failCount"], 0), intValue(state["recoverCount"], 0),
		nullableInt(state["activeIncidentId"]), nullableString(state["lastTransitionAt"]), nullableString(state["lastError"]), intValue(state["lastPing"], 0),
		nullableString(state["sslExpiry"]))
	return err
}
