package serveragent

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listMetricsHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()
	serverID := q.Get("serverId")
	startTime := firstNonEmpty(q.Get("startTime"), q.Get("start"))
	endTime := firstNonEmpty(q.Get("endTime"), q.Get("end"))
	page := clampQueryInt(q.Get("page"), 1, 1, 100000)
	pageSize := clampQueryInt(q.Get("pageSize"), 500, 1, 10000)
	offset := (page - 1) * pageSize

	where := "WHERE 1=1"
	args := []interface{}{}
	if serverID != "" {
		where += " AND server_id = ?"
		args = append(args, serverID)
	}
	if startTime != "" {
		where += " AND recorded_at >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		where += " AND recorded_at <= ?"
		args = append(args, endTime)
	}

	var total int
	if err := db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM server_metrics_history "+where, args...).Scan(&total); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	queryArgs := append(append([]interface{}{}, args...), pageSize, offset)
	rows, err := db.QueryContext(r.Context(), `
		SELECT id, server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads, cpu_temp, cpu_power,
		       mem_used, mem_total, mem_usage, disk_used, disk_total, disk_usage,
		       docker_installed, docker_running, docker_stopped,
		       gpu_usage, gpu_mem_used, gpu_mem_total, gpu_power, gpu_temp,
		       platform, net_rx, net_tx, recorded_at
		FROM server_metrics_history `+where+`
		ORDER BY recorded_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	data := scanMetricRows(rows)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    data,
		"pagination": map[string]interface{}{
			"page":       page,
			"pageSize":   pageSize,
			"total":      total,
			"totalPages": (total + pageSize - 1) / pageSize,
		},
	})
}

func (s *Service) clearMetricsHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	serverID := r.URL.Query().Get("serverId")
	var res sql.Result
	var err error
	if serverID != "" {
		res, err = db.ExecContext(r.Context(), "DELETE FROM server_metrics_history WHERE server_id = ?", serverID)
	} else {
		res, err = db.ExecContext(r.Context(), "DELETE FROM server_metrics_history")
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	deleted, _ := res.RowsAffected()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": deleted,
	})
}

// getMetricsHistory 获取主机指标历史数据
func (s *Service) getMetricsHistory(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	// 解析时间范围
	startTime := r.URL.Query().Get("start")
	endTime := r.URL.Query().Get("end")
	limit := r.URL.Query().Get("limit")

	limitInt := 100
	if limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 1000 {
			limitInt = l
		}
	}

	query := `SELECT
		id, server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads, cpu_temp, cpu_power,
		mem_used, mem_total, mem_usage, disk_used, disk_total, disk_usage,
		docker_installed, docker_running, docker_stopped,
		gpu_usage, gpu_mem_used, gpu_mem_total, gpu_power, gpu_temp,
		platform, net_rx, net_tx, recorded_at
	FROM server_metrics_history
	WHERE server_id = ?`

	args := []interface{}{serverID}

	if startTime != "" {
		query += " AND recorded_at >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		query += " AND recorded_at <= ?"
		args = append(args, endTime)
	}

	query += " ORDER BY recorded_at DESC LIMIT ?"
	args = append(args, limitInt)

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	metrics := scanMetricRows(rows)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    metrics,
	})
}

// getLatestMetrics 获取最新指标
func (s *Service) getLatestMetrics(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	query := `SELECT
		id, server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads, cpu_temp, cpu_power,
		mem_used, mem_total, mem_usage, disk_used, disk_total, disk_usage,
		docker_installed, docker_running, docker_stopped,
		gpu_usage, gpu_mem_used, gpu_mem_total, gpu_power, gpu_temp,
		platform, net_rx, net_tx, recorded_at
	FROM server_metrics_history
	WHERE server_id = ?
	ORDER BY recorded_at DESC
	LIMIT 1`

	rows, err := db.QueryContext(r.Context(), query, serverID)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			metrics := scanMetricRows(rows)
			if len(metrics) > 0 {
				response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": metrics[0]})
				return
			}
		}
	}
	if err == sql.ErrNoRows {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    nil,
		})
		return
	}

	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    nil,
	})
}

// getNetworkQuality 获取网络质量数据
func (s *Service) getNetworkQuality(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	days := r.URL.Query().Get("days")
	daysInt := 7
	if days != "" {
		if d, err := strconv.Atoi(days); err == nil && d > 0 && d <= 90 {
			daysInt = d
		}
	}

	query := `SELECT
		id, server_id, target_name, COALESCE(latency_ms, 0), CASE WHEN success = 1 THEN 0 ELSE 100 END, NULL,
		checked_at
	FROM server_network_quality_samples
	WHERE server_id = ?
	AND checked_at >= datetime('now', '-' || ? || ' days')
	ORDER BY checked_at DESC
	LIMIT 1000`

	rows, err := db.QueryContext(r.Context(), query, serverID, daysInt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var samples []map[string]interface{}
	for rows.Next() {
		var (
			id, srvID, target string
			latency           float64
			packetLoss        float64
			jitter            sql.NullFloat64
			sampledAt         string
		)

		if err := rows.Scan(&id, &srvID, &target, &latency, &packetLoss, &jitter, &sampledAt); err != nil {
			continue
		}

		sample := map[string]interface{}{
			"id":          id,
			"server_id":   srvID,
			"target":      target,
			"latency":     latency,
			"packet_loss": packetLoss,
			"sampled_at":  sampledAt,
		}

		if jitter.Valid {
			sample["jitter"] = jitter.Float64
		}

		samples = append(samples, sample)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    samples,
	})
}

// persistMetrics 持久化指标到数据库
func (s *Service) persistMetrics(ctx context.Context, db *sql.DB, serverID string, metrics map[string]interface{}) error {
	query := `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads,
		cpu_temp, cpu_power, mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage, net_rx, net_tx,
		gpu_usage, gpu_temp, gpu_power,
		recorded_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`

	cpuTemp := sql.NullFloat64{}
	if v, ok := metrics["cpu_temp"].(float64); ok {
		cpuTemp.Float64 = v
		cpuTemp.Valid = true
	}

	gpuUsage := sql.NullFloat64{}
	if v, ok := metrics["gpu_usage"].(float64); ok {
		gpuUsage.Float64 = v
		gpuUsage.Valid = true
	}

	gpuTemp := sql.NullFloat64{}
	if v, ok := metrics["gpu_temp"].(float64); ok {
		gpuTemp.Float64 = v
		gpuTemp.Valid = true
	}

	gpuPower := sql.NullFloat64{}
	if v, ok := metrics["gpu_power"].(float64); ok {
		gpuPower.Float64 = v
		gpuPower.Valid = true
	}

	_, err := db.ExecContext(ctx, query,
		serverID,
		getFloat(metrics, "cpu_usage"),
		getString(metrics, "load"),
		getInt(metrics, "cores"),
		getInt(metrics, "logical_cores"),
		cpuTemp,
		getFloat(metrics, "cpu_power"),
		getInt(metrics, "mem_used_mb"),
		getInt(metrics, "mem_total_mb"),
		firstFloat(metrics, "mem_usage_percent", "memory_usage"),
		getString(metrics, "disk_used"),
		getString(metrics, "disk_total"),
		getFloat(metrics, "disk_usage"),
		firstFloat(metrics, "net_rx", "network_rx"),
		firstFloat(metrics, "net_tx", "network_tx"),
		gpuUsage,
		gpuTemp,
		gpuPower,
	)

	return err
}

func scanMetricRows(rows *sql.Rows) []map[string]interface{} {
	var metrics []map[string]interface{}
	for rows.Next() {
		var (
			id                                                                            int64
			serverID, cpuLoad, diskUsed, diskTotal, platform, recordedAt                  sql.NullString
			cpuUsage, cpuTemp, cpuPower, memUsage, diskUsage, gpuUsage, gpuPower, gpuTemp sql.NullFloat64
			netRx, netTx                                                                  sql.NullFloat64
			cpuCores, cpuThreads, memUsed, memTotal                                       sql.NullInt64
			dockerInstalled, dockerRunning, dockerStopped, gpuMemUsed, gpuMemTotal        sql.NullInt64
		)
		if err := rows.Scan(&id, &serverID, &cpuUsage, &cpuLoad, &cpuCores, &cpuThreads, &cpuTemp, &cpuPower, &memUsed, &memTotal, &memUsage, &diskUsed, &diskTotal, &diskUsage, &dockerInstalled, &dockerRunning, &dockerStopped, &gpuUsage, &gpuMemUsed, &gpuMemTotal, &gpuPower, &gpuTemp, &platform, &netRx, &netTx, &recordedAt); err != nil {
			continue
		}
		metric := map[string]interface{}{
			"id":            id,
			"server_id":     nullString(serverID),
			"cpu_usage":     nullFloat(cpuUsage),
			"cpu_load":      nullString(cpuLoad),
			"cpu_cores":     nullInt(cpuCores),
			"cpu_threads":   nullInt(cpuThreads),
			"cpu_temp":      nullFloat(cpuTemp),
			"cpu_power":     nullFloat(cpuPower),
			"mem_used":      nullInt(memUsed),
			"mem_total":     nullInt(memTotal),
			"mem_usage":     nullFloat(memUsage),
			"disk_used":     nullString(diskUsed),
			"disk_total":    nullString(diskTotal),
			"disk_usage":    nullFloat(diskUsage),
			"net_rx":        nullFloat(netRx),
			"net_tx":        nullFloat(netTx),
			"network_rx":    nullFloat(netRx),
			"network_tx":    nullFloat(netTx),
			"gpu_usage":     nullFloat(gpuUsage),
			"gpu_mem_used":  nullInt(gpuMemUsed),
			"gpu_mem_total": nullInt(gpuMemTotal),
			"gpu_power":     nullFloat(gpuPower),
			"gpu_temp":      nullFloat(gpuTemp),
			"platform":      nullString(platform),
			"recorded_at":   nullString(recordedAt),
			"collected_at":  nullString(recordedAt),
			"timestamp":     parseTimeMillis(nullString(recordedAt)),
			"docker": map[string]interface{}{
				"installed": nullInt(dockerInstalled),
				"running":   nullInt(dockerRunning),
				"stopped":   nullInt(dockerStopped),
			},
		}
		metrics = append(metrics, metric)
	}
	return metrics
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func clampQueryInt(raw string, fallback, min, max int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func firstFloat(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return getFloat(m, key)
		}
	}
	return 0
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case fmt.Stringer:
			return val.String()
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		case float32:
			return strconv.FormatFloat(float64(val), 'f', -1, 64)
		case int:
			return strconv.Itoa(val)
		case int64:
			return strconv.FormatInt(val, 10)
		}
	}
	return ""
}

func nullString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func nullFloat(v sql.NullFloat64) float64 {
	if !v.Valid {
		return 0
	}
	return v.Float64
}

func nullInt(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func parseTimeMillis(value string) int64 {
	if value == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		}
	}
	return 0
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}
