package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
	daysInt := clampQueryInt(r.URL.Query().Get("days"), 1, 1, 90)
	maxPoints := clampQueryInt(r.URL.Query().Get("maxPointsPerTarget"), 96, 1, 2880)
	payload, err := s.buildNetworkQualityPayload(r.Context(), db, serverID, daysInt, maxPoints)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    payload,
	})
}

func (s *Service) collectNetworkQualitySamples(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	targets := s.getTargetsCache()
	if len(targets) > 0 {
		_, isOnline := s.registry.Get(serverID)
		if isOnline {
			targetsJSON, _ := json.Marshal(map[string]interface{}{
				"targets":    targets,
				"timeout_ms": 4000,
			})
			// 发送探测任务并等待结果 (最多等 8 秒)
			resultStr, err := s.runAgentTaskAndWaitTransient(serverID, 40, string(targetsJSON), 8*time.Second)
			if err == nil {
				var nqData interface{}
				if json.Unmarshal([]byte(resultStr), &nqData) == nil {
					s.processAgentNetworkQualityForced(r.Context(), db, serverID, nqData)
				}
			}
		} else {
			// 如果 Agent 未在线，回退到服务端本地拨测
			_, _ = s.collectNetworkQuality(r.Context(), db, serverID)
		}
	}

	payload, err := s.buildNetworkQualityPayload(r.Context(), db, serverID, 1, clampQueryInt(r.URL.Query().Get("maxPointsPerTarget"), 96, 1, 2880))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    payload,
	})
}

// persistMetrics 持久化指标到数据库
func (s *Service) persistMetrics(ctx context.Context, db *sql.DB, serverID string, metrics map[string]interface{}) error {
	query := `INSERT INTO server_metrics_history (
		server_id, cpu_usage, cpu_load, cpu_cores, cpu_threads,
		cpu_temp, cpu_power, mem_used, mem_total, mem_usage,
		disk_used, disk_total, disk_usage,
		docker_installed, docker_running, docker_stopped,
		gpu_usage, gpu_mem_used, gpu_mem_total, gpu_temp, gpu_power,
		platform, net_rx, net_tx,
		recorded_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`

	cpuInfo := nestedMap(metrics, "cpu")
	memInfo := nestedMap(metrics, "memory")
	gpuInfo := nestedMap(metrics, "gpu")
	networkInfo := nestedMap(metrics, "network")
	dockerInfo := nestedMap(metrics, "docker")
	diskInfo := firstMap(metrics["disk"])

	gpuMemUsed := firstMetricInt(metrics, []string{"gpu_mem_used", "gpuMemoryUsed"}, gpuInfo, []string{"memoryUsed", "MemoryUsed"})
	gpuMemTotal := firstMetricInt(metrics, []string{"gpu_mem_total", "gpuMemoryTotal"}, gpuInfo, []string{"memoryTotal", "MemoryTotal"})
	if gpuMemTotal == 0 {
		gpuMemUsed, gpuMemTotal = parsePairBytes(firstString(metrics, "gpu_mem", "gpu_memory", "gpuMemory"))
	}
	if gpuMemTotal == 0 {
		gpuMemUsed, gpuMemTotal = parsePairBytes(stringFromMap(gpuInfo, "Memory"))
	}

	cpuTemp := sql.NullFloat64{}
	if v := firstMetricFloat(metrics, []string{"cpu_temp", "cpuTemp"}, cpuInfo, []string{"Temp"}); v != 0 {
		cpuTemp.Float64 = v
		cpuTemp.Valid = true
	}

	gpuUsage := sql.NullFloat64{}
	if v := firstMetricFloat(metrics, []string{"gpu_usage", "gpu"}, gpuInfo, []string{"Usage"}); v != 0 {
		gpuUsage.Float64 = v
		gpuUsage.Valid = true
	}

	gpuTemp := sql.NullFloat64{}
	if v := firstMetricFloat(metrics, []string{"gpu_temp", "gpuTemp"}, gpuInfo, []string{"Temp"}); v != 0 {
		gpuTemp.Float64 = v
		gpuTemp.Valid = true
	}

	gpuPower := sql.NullFloat64{}
	if v := firstMetricFloat(metrics, []string{"gpu_power", "gpuPower"}, gpuInfo, []string{"Power"}); v != 0 {
		gpuPower.Float64 = v
		gpuPower.Valid = true
	}

	cpuVal := firstMetricFloat(metrics, []string{"cpu_usage", "cpu"}, cpuInfo, []string{"Usage"})
	memVal := firstMetricFloat(metrics, []string{"mem_usage_percent", "memory_usage", "mem_usage"}, memInfo, []string{"Usage"})
	diskVal := firstMetricFloat(metrics, []string{"disk_usage"}, diskInfo, []string{"usage", "Usage"})

	_, err := db.ExecContext(ctx, query,
		serverID,
		cpuVal,
		firstNonEmpty(firstString(metrics, "load", "cpu_load", "cpuLoad"), firstString(cpuInfo, "Load")),
		firstMetricInt(metrics, []string{"cores", "cpu_cores"}, cpuInfo, []string{"Cores", "PhysicalCores"}),
		firstMetricInt(metrics, []string{"logical_cores", "cpu_threads", "threads"}, cpuInfo, []string{"LogicalCores", "Threads"}),
		cpuTemp,
		firstMetricFloat(metrics, []string{"cpu_power", "cpu_power_w", "cpuPower"}, cpuInfo, []string{"Power"}),
		firstMetricInt(metrics, []string{"mem_used_mb", "mem_used", "memory_used"}, memInfo, []string{"Used", "UsedMB"}),
		firstMetricInt(metrics, []string{"mem_total_mb", "mem_total", "memory_total"}, memInfo, []string{"Total", "TotalMB"}),
		memVal,
		firstNonEmpty(getString(metrics, "disk_used"), stringFromMap(diskInfo, "used"), stringFromMap(diskInfo, "Used")),
		firstNonEmpty(getString(metrics, "disk_total"), stringFromMap(diskInfo, "total"), stringFromMap(diskInfo, "Total")),
		diskVal,
		firstMetricInt(metrics, []string{"docker_installed"}, dockerInfo, []string{"installed"}),
		firstMetricInt(metrics, []string{"docker_running"}, dockerInfo, []string{"running"}),
		firstMetricInt(metrics, []string{"docker_stopped"}, dockerInfo, []string{"stopped"}),
		gpuUsage,
		gpuMemUsed,
		gpuMemTotal,
		gpuTemp,
		gpuPower,
		firstNonEmpty(getString(metrics, "platform"), stringFromMap(metrics, "platform")),
		firstMetricBytes(metrics, []string{"net_rx", "network_rx"}, networkInfo, []string{"rx_speed", "down"}),
		firstMetricBytes(metrics, []string{"net_tx", "network_tx"}, networkInfo, []string{"tx_speed", "up"}),
	)

	if err == nil && s.notifier != nil {
		go s.checkMetricAlerts(ctx, db, serverID, cpuVal, memVal, diskVal)
	}

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

var metricNumberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

func parseMetricNumber(value interface{}) float64 {
	switch val := value.(type) {
	case nil:
		return 0
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		match := metricNumberPattern.FindString(strings.ReplaceAll(val, ",", ""))
		if match == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(match, 64)
		return f
	default:
		return 0
	}
}

func nestedMap(m map[string]interface{}, key string) map[string]interface{} {
	if raw, ok := m[key]; ok {
		if nested, ok := raw.(map[string]interface{}); ok {
			return nested
		}
	}
	return map[string]interface{}{}
}

func firstMap(value interface{}) map[string]interface{} {
	switch val := value.(type) {
	case []interface{}:
		for _, item := range val {
			if nested, ok := item.(map[string]interface{}); ok {
				return nested
			}
		}
	case []map[string]interface{}:
		if len(val) > 0 {
			return val[0]
		}
	case map[string]interface{}:
		return val
	}
	return map[string]interface{}{}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case fmt.Stringer:
			return val.String()
		case float64, float32, int, int64, int32, json.Number:
			return strconv.FormatFloat(parseMetricNumber(val), 'f', -1, 64)
		}
	}
	return ""
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(m, key); value != "" {
			return value
		}
	}
	return ""
}

func firstMetricFloat(primary map[string]interface{}, primaryKeys []string, nested map[string]interface{}, nestedKeys []string) float64 {
	for _, key := range primaryKeys {
		if raw, ok := primary[key]; ok {
			if value := parseMetricNumber(raw); value != 0 {
				return value
			}
		}
	}
	for _, key := range nestedKeys {
		if raw, ok := nested[key]; ok {
			if value := parseMetricNumber(raw); value != 0 {
				return value
			}
		}
	}
	return 0
}

func firstMetricInt(primary map[string]interface{}, primaryKeys []string, nested map[string]interface{}, nestedKeys []string) int {
	return int(firstMetricFloat(primary, primaryKeys, nested, nestedKeys))
}

func firstMetricBytes(primary map[string]interface{}, primaryKeys []string, nested map[string]interface{}, nestedKeys []string) float64 {
	for _, key := range primaryKeys {
		if raw, ok := primary[key]; ok {
			if value := parseByteValue(raw); value != 0 {
				return value
			}
		}
	}
	for _, key := range nestedKeys {
		if raw, ok := nested[key]; ok {
			if value := parseByteValue(raw); value != 0 {
				return value
			}
		}
	}
	return 0
}

func parseByteValue(value interface{}) float64 {
	switch val := value.(type) {
	case float64, float32, int, int64, int32, json.Number:
		return parseMetricNumber(val)
	case string:
		raw := strings.TrimSpace(strings.ReplaceAll(val, ",", ""))
		amount := parseMetricNumber(raw)
		if amount == 0 {
			return 0
		}
		upper := strings.ToUpper(raw)
		switch {
		case strings.Contains(upper, "TB"):
			return amount * 1024 * 1024 * 1024 * 1024
		case strings.Contains(upper, "GB"):
			return amount * 1024 * 1024 * 1024
		case strings.Contains(upper, "MB"):
			return amount * 1024 * 1024
		case strings.Contains(upper, "KB"):
			return amount * 1024
		default:
			return amount
		}
	default:
		return 0
	}
}

func parsePairBytes(value string) (int, int) {
	if !strings.Contains(value, "/") {
		return 0, 0
	}
	parts := strings.SplitN(value, "/", 2)
	return int(parseMetricNumber(parts[0])), int(parseMetricNumber(parts[1]))
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


// clearExpiredHistory 分批删除指定表过期行，避免大表单次 DELETE 持写锁过久。
// 表名/列名为内部常量（非用户输入）；LIMIT 置于子查询（modernc 驱动不支持
// DELETE 主语句 LIMIT）。每批到上限后继续下一批直至删完。
func clearExpiredHistory(ctx context.Context, db *sql.DB, table, column string, retentionDays int) {
	const batchSize = 2000
	query := fmt.Sprintf("DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE %s < datetime('now', '-' || ? || ' days') LIMIT %d)", table, table, column, batchSize)
	for {
		result, err := db.ExecContext(ctx, query, retentionDays)
		if err != nil {
			return
		}
		deleted, err := result.RowsAffected()
		if err != nil || deleted < batchSize {
			return
		}
	}
}
