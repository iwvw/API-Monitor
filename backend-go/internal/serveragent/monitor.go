package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

func (s *Service) handleMonitor(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "status" && r.Method == http.MethodGet {
		s.getMonitorStatus(w, r, db)
		return
	}

	if len(subparts) == 1 && subparts[0] == "collect" && r.Method == http.MethodPost {
		s.collectMonitorMetrics(w, r, db)
		return
	}

	if len(subparts) == 1 && subparts[0] == "config" {
		if r.Method == http.MethodGet {
			s.getMonitorConfig(w, r, db)
		} else if r.Method == http.MethodPut {
			s.updateMonitorConfig(w, r, db)
		} else {
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(subparts) == 1 && subparts[0] == "logs" && r.Method == http.MethodGet {
		s.listMonitorLogs(w, r, db)
		return
	}

	response.Error(w, http.StatusNotFound, "monitor sub-route not found")
}

func (s *Service) getMonitorStatus(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var interval int
	var autoStart int
	err := db.QueryRowContext(r.Context(), "SELECT COALESCE(metrics_collect_interval, 300), auto_start FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.lastCollectMu.RLock()
	var lastCollectVal interface{} = nil
	if !s.lastCollect.IsZero() {
		lastCollectVal = s.lastCollect.Format("2006-01-02 15:04:05")
	}
	s.lastCollectMu.RUnlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status": map[string]interface{}{
			"running":     autoStart == 1,
			"isRunning":   autoStart == 1,
			"interval":    interval * 1000,
			"collector":   "go",
			"lastCollect": lastCollectVal,
		},
	})
}

// syncTrafficCycleBaselines 对启用周期的主机做"基线回滚"：
// 当当前时刻已越过存储的 cycle_end（或尚未初始化），把基线更新为此刻的累计用量，
// 并把周期窗口推进到当前窗口。这样"当前周期内用量 = 累计 - 基线"即实现了每月自动归零。
func (s *Service) syncTrafficCycleBaselines(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(traffic_cycle_type,'none'), COALESCE(traffic_cycle_day,1), COALESCE(traffic_limit_mode,'total'), COALESCE(traffic_cycle_start,''), COALESCE(traffic_cycle_end,''), COALESCE(traffic_cycle_baseline,0), COALESCE(cached_info,'{}') FROM server_accounts`)
	if err != nil {
		return
	}
	type candidate struct {
		id, cycleType, limitMode, cycleStart, cycleEnd, cached string
		cycleDay int
		baseline int64
	}
	var list []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.cycleType, &c.cycleDay, &c.limitMode, &c.cycleStart, &c.cycleEnd, &c.baseline, &c.cached); err == nil {
			list = append(list, c)
		}
	}
	rows.Close()

	loc := timeutil.LocationFromSettings(ctx, db)
	now := time.Now()

	for _, c := range list {
		if normalizeTrafficCycleType(c.cycleType) == "none" {
			continue
		}
		start, end, ok := trafficCycleWindow(c.cycleType, c.cycleDay, loc, now)
		if !ok {
			continue
		}
		var metrics map[string]interface{}
		if err := json.Unmarshal([]byte(c.cached), &metrics); err != nil || len(metrics) == 0 {
			continue
		}
		cumulative := trafficUsedBytesFromMetrics(metrics, c.limitMode)

		// 未初始化，或已越过存储的 cycle_end，或存储窗口与当前窗口不一致 → 需要回滚
		storedStartT, _ := time.Parse(time.RFC3339, c.cycleStart)
		storedEndT, _ := time.Parse(time.RFC3339, c.cycleEnd)
		needRoll := c.cycleStart == "" || c.cycleEnd == "" || c.baseline <= 0 ||
			!storedStartT.Equal(start) || now.After(storedEndT) || !now.Before(storedEndT)
		if !needRoll {
			continue
		}
		startStr := start.UTC().Format(time.RFC3339)
		endStr := end.UTC().Format(time.RFC3339)
		_, _ = db.ExecContext(ctx, `UPDATE server_accounts SET traffic_cycle_baseline = ?, traffic_cycle_start = ?, traffic_cycle_end = ?, updated_at = ? WHERE id = ?`,
			cumulative, startStr, endStr, time.Now().UTC().Format(time.RFC3339), c.id)
	}
}

func (s *Service) runPeriodicCollection(ctx context.Context, db *sql.DB) int {	rows, err := db.QueryContext(ctx, "SELECT id, COALESCE(cached_info, '{}') FROM server_accounts")
	if err != nil {
		return 0
	}
	defer rows.Close()

	type serverMetric struct {
		serverID string
		raw      string
	}
	var list []serverMetric
	for rows.Next() {
		var sm serverMetric
		if err := rows.Scan(&sm.serverID, &sm.raw); err == nil {
			list = append(list, sm)
		}
	}

	collected := 0
	for _, sm := range list {
		var metrics map[string]interface{}
		if err := json.Unmarshal([]byte(sm.raw), &metrics); err != nil || len(metrics) == 0 {
			continue
		}
		if conn, exists := s.engineIO.registry.Get(sm.serverID); exists {
			if metadata := conn.GetMetadata(); len(metadata) > 0 {
				metrics = s.buildInfoStruct(metadata)
			}
		}
		if err := s.persistMetrics(ctx, db, sm.serverID, metrics); err == nil {
			collected++
		}
	}

	if collected > 0 {
		s.lastCollectMu.Lock()
		s.lastCollect = time.Now()
		s.lastCollectMu.Unlock()
	}

	return collected
}

func (s *Service) collectMonitorMetrics(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	collected := s.runPeriodicCollection(r.Context(), db)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"collected": collected,
	})
}

func (s *Service) startMetricsCollectorLoop(ctx context.Context) {
	// Wait a moment for database initialization and server startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastCollected time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		loopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		db, err := s.open(loopCtx)
		if err != nil {
			cancel()
			continue
		}

		// 周期流量基线回滚：每月 1 号自动把基线推进到新周期，使"周期内用量"归零。
		s.syncTrafficCycleBaselines(loopCtx, db)

		// Query monitor config
		var interval int
		var autoStart int
		var retentionDays int
		var logRetentionDays int
		err = db.QueryRowContext(loopCtx, "SELECT COALESCE(metrics_collect_interval, 300), auto_start, COALESCE(metrics_retention_days, 30), COALESCE(log_retention_days, 7) FROM server_monitor_config WHERE id = 1").Scan(&interval, &autoStart, &retentionDays, &logRetentionDays)
		if err != nil {
			db.Close()
			cancel()
			continue
		}

		if autoStart != 1 {
			db.Close()
			cancel()
			continue
		}

		now := time.Now()
		// If it's time to collect
		if lastCollected.IsZero() || now.Sub(lastCollected) >= time.Duration(interval)*time.Second {
			// Trigger collection
			s.runPeriodicCollection(loopCtx, db)
			lastCollected = now

			// 分批清理过期数据：单批上限避免大表上单次 DELETE 持写锁过久
			// 阻塞其它模块写入（含检查循环自身的后续写入）。
			if retentionDays > 0 {
				clearExpiredHistory(loopCtx, db, "server_metrics_history", "recorded_at", retentionDays)
				clearExpiredHistory(loopCtx, db, "server_network_quality_samples", "checked_at", retentionDays)
			}
			if logRetentionDays > 0 {
				clearExpiredHistory(loopCtx, db, "server_monitor_logs", "checked_at", logRetentionDays)
			}
		}

		db.Close()
		cancel()
	}
}

func (s *Service) getMonitorConfig(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var id, probeInterval, probeTimeout, logRetentionDays, maxConnections, sessionTimeout, autoStart, metricsCollectInterval, metricsRetentionDays int
	var updatedAt string

	err := db.QueryRowContext(r.Context(), "SELECT id, probe_interval, probe_timeout, log_retention_days, COALESCE(max_connections, 10), COALESCE(session_timeout, 1800), auto_start, COALESCE(metrics_collect_interval, 300), COALESCE(metrics_retention_days, 30), COALESCE(updated_at, '') FROM server_monitor_config WHERE id = 1").
		Scan(&id, &probeInterval, &probeTimeout, &logRetentionDays, &maxConnections, &sessionTimeout, &autoStart, &metricsCollectInterval, &metricsRetentionDays, &updatedAt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{
		"id":                       id,
		"probe_interval":           probeInterval,
		"probe_timeout":            probeTimeout,
		"log_retention_days":       logRetentionDays,
		"max_connections":          maxConnections,
		"session_timeout":          sessionTimeout,
		"auto_start":               autoStart == 1,
		"metrics_collect_interval": metricsCollectInterval,
		"metrics_retention_days":   metricsRetentionDays,
		"updated_at":               updatedAt,
	})
}

func (s *Service) updateMonitorConfig(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing first
	var existing struct {
		probeInterval          int
		probeTimeout           int
		logRetentionDays       int
		maxConnections         int
		sessionTimeout         int
		autoStart              int
		metricsCollectInterval int
		metricsRetentionDays   int
	}
	err := db.QueryRowContext(r.Context(), "SELECT probe_interval, probe_timeout, log_retention_days, COALESCE(max_connections, 10), COALESCE(session_timeout, 1800), auto_start, COALESCE(metrics_collect_interval, 300), COALESCE(metrics_retention_days, 30) FROM server_monitor_config WHERE id = 1").
		Scan(&existing.probeInterval, &existing.probeTimeout, &existing.logRetentionDays, &existing.maxConnections, &existing.sessionTimeout, &existing.autoStart, &existing.metricsCollectInterval, &existing.metricsRetentionDays)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to get config: "+err.Error())
		return
	}

	probeInterval := getIntVal(req, "probe_interval", existing.probeInterval)
	probeTimeout := getIntVal(req, "probe_timeout", existing.probeTimeout)
	logRetentionDays := getIntVal(req, "log_retention_days", existing.logRetentionDays)
	maxConnections := getIntVal(req, "max_connections", existing.maxConnections)
	sessionTimeout := getIntVal(req, "session_timeout", existing.sessionTimeout)
	metricsCollectInterval := getIntVal(req, "metrics_collect_interval", existing.metricsCollectInterval)
	metricsRetentionDays := getIntVal(req, "metrics_retention_days", existing.metricsRetentionDays)

	autoStart := existing.autoStart
	if val, ok := req["auto_start"].(bool); ok {
		if val {
			autoStart = 1
		} else {
			autoStart = 0
		}
	}

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(r.Context(), `
		UPDATE server_monitor_config
		SET probe_interval = ?, probe_timeout = ?, log_retention_days = ?, max_connections = ?, session_timeout = ?, auto_start = ?, metrics_collect_interval = ?, metrics_retention_days = ?, updated_at = ?
		WHERE id = 1`,
		probeInterval, probeTimeout, logRetentionDays, maxConnections, sessionTimeout, autoStart, metricsCollectInterval, metricsRetentionDays, now,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.getMonitorConfig(w, r, db)
}

func (s *Service) listMonitorLogs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	serverId := q.Get("serverId")
	status := q.Get("status")
	pageStr := q.Get("page")
	pageSizeStr := q.Get("pageSize")

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	pageSize := 50
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	offset := (page - 1) * pageSize

	sqlQuery := "SELECT id, server_id, status, response_time, error_message, checked_at FROM server_monitor_logs WHERE 1=1"
	sqlCount := "SELECT COUNT(*) FROM server_monitor_logs WHERE 1=1"
	var params []interface{}

	if serverId != "" {
		sqlQuery += " AND server_id = ?"
		sqlCount += " AND server_id = ?"
		params = append(params, serverId)
	}
	if status != "" {
		sqlQuery += " AND status = ?"
		sqlCount += " AND status = ?"
		params = append(params, status)
	}

	// Count total
	var total int
	err := db.QueryRowContext(r.Context(), sqlCount, params...).Scan(&total)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	sqlQuery += " ORDER BY checked_at DESC LIMIT ? OFFSET ?"
	params = append(params, pageSize, offset)

	rows, err := db.QueryContext(r.Context(), sqlQuery, params...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id int
		var srvID, logStatus, checkedAt string
		var respTime sql.NullInt64
		var errMsg sql.NullString
		err := rows.Scan(&id, &srvID, &logStatus, &respTime, &errMsg, &checkedAt)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		respVal := interface{}(nil)
		if respTime.Valid {
			respVal = respTime.Int64
		}
		errVal := interface{}(nil)
		if errMsg.Valid {
			errVal = errMsg.String
		}

		logs = append(logs, map[string]interface{}{
			"id":            id,
			"server_id":     srvID,
			"status":        logStatus,
			"response_time": respVal,
			"error_message": errVal,
			"checked_at":    checkedAt,
		})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}

	writeLogsWithPagination(w, logs, total, page, pageSize)
}

func writeLogsWithPagination(w http.ResponseWriter, logs []map[string]interface{}, total, page, pageSize int) {
	totalPages := 0
	if pageSize > 0 {
		totalPages = total / pageSize
		if total%pageSize != 0 {
			totalPages++
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    logs,
		"pagination": map[string]interface{}{
			"total":      total,
			"page":       page,
			"pageSize":   pageSize,
			"totalPages": totalPages,
		},
	})
}

// ==========================================
// ACCOUNTS HANDLERS
// ==========================================
