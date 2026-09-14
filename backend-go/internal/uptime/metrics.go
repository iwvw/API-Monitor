package uptime

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) monitorUptime(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	days := intValue(r.URL.Query().Get("days"), 1)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	uptime, err := calculateUptime(r.Context(), db, id, days)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"monitorId": id, "days": days, "uptime": uptime})
}

func calculateUptime(ctx context.Context, db *sql.DB, monitorID int64, days int) (string, error) {
	if days <= 0 {
		days = 1
	}
	totalMs := float64(days) * 24 * 60 * 60 * 1000
	rangeStart := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := db.QueryContext(ctx, `
		SELECT started_at, COALESCE(resolved_at, datetime('now')) as resolved_at
		FROM uptime_incidents
		WHERE monitor_id = ? AND (resolved_at > ? OR resolved_at IS NULL)
	`, monitorID, rangeStart.UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	downMs := 0.0
	for rows.Next() {
		var started, resolved string
		if err := rows.Scan(&started, &resolved); err != nil {
			return "", err
		}
		startTime := parseTimeFallbackAny(started, rangeStart)
		endTime := parseTimeFallbackAny(resolved, time.Now())
		if startTime.Before(rangeStart) {
			startTime = rangeStart
		}
		if endTime.After(time.Now()) {
			endTime = time.Now()
		}
		if endTime.After(startTime) {
			downMs += float64(endTime.Sub(startTime).Milliseconds())
		}
	}
	value := (1 - downMs/totalMs) * 100
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%.3f", value), rows.Err()
}

func (s *Service) monitorIncidents(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	limit := intValue(r.URL.Query().Get("limit"), 20)
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `SELECT * FROM uptime_incidents WHERE monitor_id = ? ORDER BY started_at DESC LIMIT ?`, id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items, err := scanAll(rows)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (s *Service) monitorState(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	state, _ := loadState(r.Context(), db, id)
	response.JSON(w, http.StatusOK, state)
}

func parseUptimeDBTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Parse(time.RFC3339Nano, value)
}

// lockMonitor 获取指定 monitor 的检查互斥锁，返回解锁函数。
// 同一 monitor 的定时检查、checkNow 与手动触发必须串行，才能保证
// processState 的 loadState→transition→saveState 是原子读改写。
