package uptime

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

func (s *Service) lockMonitor(id int64) func() {
	if id <= 0 {
		return func() {}
	}
	s.checkMux.Lock()
	mu, ok := s.checkLocks[id]
	if !ok {
		mu = &sync.Mutex{}
		s.checkLocks[id] = mu
	}
	s.checkMux.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (s *Service) check(ctx context.Context, db *sql.DB, monitor map[string]interface{}) (map[string]interface{}, error) {
	unlock := s.lockMonitor(int64Value(monitor["id"], 0))
	defer unlock()

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
	if err := s.processState(ctx, db, monitor, result, beat, maintenance != nil); err != nil {
		return nil, err
	}
	s.enrichAndBroadcastHeartbeat(ctx, db, int64Value(monitor["id"], 0), beat)
	// Persist SSL expiry from probe result into monitor state
	if result.SslExpiry != nil {
		monitorID := int64Value(monitor["id"], 0)
		_, _ = db.ExecContext(ctx, `UPDATE uptime_monitor_states SET ssl_expiry = ? WHERE monitor_id = ?`,
			result.SslExpiry.UTC().Format(time.RFC3339), monitorID)

		// Check if SSL is expiring soon (<= 30 days)
		daysLeft := int(time.Until(*result.SslExpiry).Hours() / 24)
		if daysLeft <= 30 {
			lastExpiry, exists := s.lastSslAlerts.Load(monitorID)
			if !exists || !lastExpiry.(time.Time).Equal(*result.SslExpiry) {
				if s.notifier != nil {
					eventData := map[string]interface{}{
						"monitorId":   monitorID,
						"monitorName": stringValue(monitor["name"], ""),
						"url":         monitorTarget(monitor),
						"daysLeft":    daysLeft,
						"expiry":      result.SslExpiry.UTC().Format(time.RFC3339),
					}
					triggerErr := s.notifier.Trigger(ctx, "uptime", "ssl_expiry", eventData)
					// 只在通知成功时才记录去重游标：失败留待下轮重试，
					// 否则告警会因落库成功而永不重发。
					if triggerErr == nil {
						s.lastSslAlerts.Store(monitorID, *result.SslExpiry)
					}
				} else {
					s.lastSslAlerts.Store(monitorID, *result.SslExpiry)
				}
			}
		}
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
	if err := saveState(ctx, db, monitorID, next); err != nil {
		return err
	}
	if action == "" && stateText(next["state"]) == stateDown {
		duration := int64(0)
		if incident, _ := getOpenIncident(ctx, db, monitorID); incident != nil {
			started := parseTimeFallback(stringValue(incident["started_at"], ""), time.Now())
			duration = time.Since(started).Milliseconds()
		}
		s.refreshNotification(ctx, "down", monitor, beat, duration)
	}
	if action == "" && stateText(next["state"]) == stateUp {
		// 在线状态也周期触发 resolve 自愈：后端重启后残留的 down 生命周期消息
		// （无 up 规则覆盖时）需要被编辑为恢复内容并清除。
		s.refreshNotification(ctx, "up", monitor, beat, 0)
	}
	return nil
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
