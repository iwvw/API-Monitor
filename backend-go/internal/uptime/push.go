package uptime

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) recordPush(w http.ResponseWriter, r *http.Request, token string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitorByPushToken(r.Context(), db, token)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Invalid push token")
		return
	}
	// push 心跳与定时 pushProbe/checkNow 必须按 monitor 串行，
	// 否则 processState 的读改写会交错，产生重复 incident 或通知乱序。
	unlock := s.lockMonitor(int64Value(monitor["id"], 0))
	defer unlock()
	payload := map[string]interface{}{}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	beat := map[string]interface{}{
		"id":         time.Now().UnixMilli(),
		"status":     1,
		"state":      stateUp,
		"msg":        "Push heartbeat received",
		"ping":       0,
		"durationMs": 0,
		"details": map[string]interface{}{
			"payload":   payload,
			"ip":        r.RemoteAddr,
			"userAgent": r.UserAgent(),
		},
		"probeId": "push",
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	maintenance, _ := activeMaintenanceForMonitor(r.Context(), db, int64Value(monitor["id"], 0))
	beat["maintenance"] = maintenance != nil
	if err := saveHeartbeat(r.Context(), db, int64Value(monitor["id"], 0), beat); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.processState(r.Context(), db, monitor, probeResult{OK: true, Status: stateUp, Message: "Push heartbeat received"}, beat, maintenance != nil)
	s.enrichAndBroadcastHeartbeat(r.Context(), db, int64Value(monitor["id"], 0), beat)
	response.OK(w, map[string]interface{}{"monitorId": monitor["id"], "receivedAt": beat["time"]})
}

func (s *Service) enrichAndBroadcastHeartbeat(ctx context.Context, db *sql.DB, monitorID int64, beat map[string]interface{}) {
	state, _ := loadState(ctx, db, monitorID)
	uptime24h, _ := calculateUptime(ctx, db, monitorID, 1)
	uptime30d, _ := calculateUptime(ctx, db, monitorID, 30)
	beat["monitorState"] = state
	beat["uptime24h"] = uptime24h
	beat["uptime30d"] = uptime30d
	s.broadcastHeartbeat(monitorID, beat)
}

func (s *Service) broadcastHeartbeat(monitorID int64, beat map[string]interface{}) {
	if s.heartbeatBroadcaster == nil || monitorID <= 0 || beat == nil {
		return
	}
	s.heartbeatBroadcaster(monitorID, beat)
}

func loadMonitorByPushToken(ctx context.Context, db *sql.DB, token string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_monitors WHERE push_token = ? AND type = 'push'`, token)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		return nil, false, err
	}
	return normalizeMonitor(row), true, rows.Err()
}
