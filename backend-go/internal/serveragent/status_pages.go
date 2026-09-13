package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) handleStatusPageRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, parts []string) {
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.listServerStatusPages(w, r, db)
		case http.MethodPost:
			s.saveServerStatusPage(w, r, db, 0)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 1 {
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid status page id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			s.saveServerStatusPage(w, r, db, id)
		case http.MethodDelete:
			result, err := db.ExecContext(r.Context(), `DELETE FROM server_status_pages WHERE id = ?`, id)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			changed, _ := result.RowsAffected()
			if changed == 0 {
				response.Error(w, http.StatusNotFound, "Not found")
				return
			}
			response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	response.Error(w, http.StatusNotFound, "status page route not found")
}

func (s *Service) handlePublicStatusPageRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, parts []string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var page map[string]interface{}
	var ok bool
	var err error
	switch {
	case len(parts) == 2 && parts[0] == "status-pages":
		page, ok, err = s.getPublicServerStatusPage(r.Context(), db, normalizeServerStatusSlug(parts[1]), "")
	case len(parts) == 1 && parts[0] == "status-page-by-domain":
		domain := strings.TrimSpace(r.URL.Query().Get("domain"))
		if domain == "" {
			domain = r.Host
		}
		page, ok, err = s.getPublicServerStatusPage(r.Context(), db, "", normalizeServerStatusDomain(domain))
	default:
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, page)
}

func (s *Service) listServerStatusPages(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages ORDER BY created_at DESC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	pages := []map[string]interface{}{}
	for rows.Next() {
		page, err := scanServerStatusPageRows(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		pages = append(pages, page)
	}
	response.OK(w, pages)
}

func (s *Service) saveServerStatusPage(w http.ResponseWriter, r *http.Request, db *sql.DB, id int64) {
	payload := map[string]interface{}{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(getStringVal(payload, "title", ""))
	slug := normalizeServerStatusSlug(getStringVal(payload, "slug", title))
	if title == "" {
		title = slug
	}
	if slug == "" {
		response.Error(w, http.StatusBadRequest, "slug is required")
		return
	}
	domain := normalizeServerStatusDomain(getStringVal(payload, "domain", ""))
	description := strings.TrimSpace(getStringVal(payload, "description", ""))
	isPublic := getBoolVal(payload, "public", true)
	cacheSeconds := getIntVal(payload, "cacheSeconds", 300)
	if cacheSeconds < 30 {
		cacheSeconds = 30
	}
	configJSON := serverStatusJSON(payload["config"], "{}")
	serverIDs := stringSliceValue(payload["serverIds"])
	if len(serverIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "serverIds is required")
		return
	}
	serverIDsJSON := serverStatusJSON(serverIDs, "[]")

	var err error
	if id > 0 {
		var result sql.Result
		result, err = db.ExecContext(r.Context(), `
			UPDATE server_status_pages
			SET slug = ?, domain = ?, title = ?, description = ?, public = ?, cache_seconds = ?, config_json = ?, server_ids_json = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, slug, nullableServerStatusString(domain), title, description, boolToInt(isPublic), cacheSeconds, configJSON, serverIDsJSON, id)
		if err == nil {
			changed, _ := result.RowsAffected()
			if changed == 0 {
				response.Error(w, http.StatusNotFound, "Not found")
				return
			}
		}
	} else {
		_, err = db.ExecContext(r.Context(), `
			INSERT INTO server_status_pages (slug, domain, title, description, public, cache_seconds, config_json, server_ids_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, slug, nullableServerStatusString(domain), title, description, boolToInt(isPublic), cacheSeconds, configJSON, serverIDsJSON)
	}
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	page, ok, err := s.getServerStatusPageBySlug(r.Context(), db, slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	response.OK(w, page)
}

func (s *Service) getServerStatusPageBySlug(ctx context.Context, db *sql.DB, slug string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE slug = ?`, slug)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	page, err := scanServerStatusPageRows(rows)
	return page, err == nil, err
}

func (s *Service) getPublicServerStatusPage(ctx context.Context, db *sql.DB, slug, domain string) (map[string]interface{}, bool, error) {
	query := `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE public = 1 AND slug = ?`
	arg := slug
	if domain != "" {
		query = `SELECT id, slug, domain, title, description, public, cache_seconds, config_json, server_ids_json, created_at, updated_at FROM server_status_pages WHERE public = 1 AND lower(domain) = lower(?)`
		arg = domain
	}
	rows, err := db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, false, rows.Err()
	}
	page, err := scanServerStatusPageRows(rows)
	if closeErr := rows.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, false, err
	}
	servers, err := s.getPublicServerStatusItems(ctx, db, stringSliceValue(page["serverIds"]), mapValue(page["config"]))
	if err != nil {
		return nil, false, err
	}
	page["servers"] = servers
	return page, true, nil
}

// PublicPageIconID 返回公开状态页配置的自定义图标 ID（未设置时为空字符串），
// 供服务端 favicon 解析端点使用；lookup 为 slug 或域名。
func (s *Service) PublicPageIconID(ctx context.Context, lookup string, byDomain bool) (string, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	arg := normalizeServerStatusSlug(lookup)
	if byDomain {
		arg = normalizeServerStatusDomain(lookup)
	}
	return publicpageicon.LookupIconID(ctx, db, `server_status_pages`, arg, byDomain)
}

func (s *Service) getPublicServerStatusItems(ctx context.Context, db *sql.DB, ids []string, config map[string]interface{}) ([]map[string]interface{}, error) {
	if len(ids) == 0 {
		return []map[string]interface{}{}, nil
	}
	hideHosts := getBoolVal(config, "hideHosts", true)
	showTraffic := getBoolVal(config, "showTraffic", true)
	showCharts := getBoolVal(config, "showCharts", true)
	args := []interface{}{}
	holders := make([]string, 0, len(ids))
	for _, id := range ids {
		holders = append(holders, "?")
		args = append(args, id)
	}
	where := "WHERE id IN (" + strings.Join(holders, ",") + ")"
	rows, err := db.QueryContext(ctx, `SELECT id, name, host, status, COALESCE(cached_info, '{}'), COALESCE(description, ''), COALESCE(resolved_country, ''), COALESCE(country, ''), traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), COALESCE(response_time, 0), COALESCE(traffic_cycle_type, 'none'), COALESCE(traffic_cycle_baseline, 0), updated_at FROM server_accounts `+where+` ORDER BY order_index ASC, created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []map[string]interface{}{}
	historyServerIDs := []string{}
	latencyServerIDs := []string{}
	for rows.Next() {
		var id, name, host, status, cachedInfo, description, location, country, trafficLimitMode, trafficCycleType, updatedAt string
		var trafficLimit, responseTime int64
		var trafficCycleBaseline int64
		if err := rows.Scan(&id, &name, &host, &status, &cachedInfo, &description, &location, &country, &trafficLimit, &trafficLimitMode, &responseTime, &trafficCycleType, &trafficCycleBaseline, &updatedAt); err != nil {
			return nil, err
		}
		cached := map[string]interface{}{}
		_ = json.Unmarshal([]byte(cachedInfo), &cached)
		var lastHeartbeat string
		if conn, agentOnline := s.registry.Get(id); agentOnline {
			metadata := conn.GetMetadata()
			for key, value := range metadata {
				cached[key] = value
			}
			normalizePublicServerLiveMetrics(cached, metadata)
			conn.mu.RLock()
			if !conn.LastHeartbeat.IsZero() {
				lastHeartbeat = conn.LastHeartbeat.UTC().Format(time.RFC3339Nano)
			}
			conn.mu.RUnlock()
			status = "online"
		} else if status == "online" {
			status = "offline"
		}
		network := mapValue(cached["network"])
		trafficRxBytes := firstFloatValue(cached, "net_in_transfer", "net_rx_total", "rx_total_bytes")
		trafficTxBytes := firstFloatValue(cached, "net_out_transfer", "net_tx_total", "tx_total_bytes")
		if networkRx := getFloatFromMap(network, "rx_total_bytes"); networkRx > 0 {
			trafficRxBytes = networkRx
		}
		if networkTx := getFloatFromMap(network, "tx_total_bytes"); networkTx > 0 {
			trafficTxBytes = networkTx
		}
		trafficUsed := trafficUsedForCycle(trafficUsedBytesForMode(trafficRxBytes, trafficTxBytes, trafficLimitMode), trafficCycleType, trafficCycleBaseline)
		uptimeSeconds := firstFloatValue(cached, "uptime_seconds", "uptime_raw")
		if uptimeSeconds == 0 {
			uptimeSeconds = getFloatValue(cached, "uptime")
		}
		uptimeLabel := firstNonEmpty(getString(cached, "uptime_label"), getString(cached, "uptime"))
		if uptimeSeconds > 0 {
			uptimeLabel = formatUptime(int64(uptimeSeconds))
		}
		lat, hasLat := firstOptionalFloatValue(cached, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(cached, "lon", "longitude")
		item := map[string]interface{}{
			"id":               id,
			"name":             name,
			"status":           status,
			"online":           status == "online",
			"description":      description,
			"location":         firstNonEmpty(location, getString(cached, "location"), getString(cached, "resolved_country"), cleanCountryCode(getString(cached, "country_code")), cleanCountryCode(getString(cached, "country"))),
			"region":           getString(cached, "region"),
			"countryCode":      firstNonEmpty(cleanCountryCode(getString(cached, "country_code")), cleanCountryCode(getString(cached, "country")), cleanCountryCode(country)),
			"platform":         firstNonEmpty(getString(cached, "platform"), getString(cached, "os")),
			"platformVersion":  getString(cached, "platform_version"),
			"agentVersion":     getString(cached, "agent_version"),
			"uptime":           uptimeSeconds,
			"uptimeLabel":      uptimeLabel,
			"load":             getString(cached, "load"),
			"cpu":              firstFloatValue(cached, "cpu", "cpu_usage"),
			"cpuTemp":          firstFloatValue(cached, "cpu_temp", "cpuTemp"),
			"cpuPower":         firstFloatValue(cached, "cpu_power", "cpuPower"),
			"memory":           firstFloatValue(cached, "mem_percent", "mem_usage_percent", "memory", "memory_usage", "mem_usage"),
			"memoryUsedBytes":  firstFloatValue(cached, "mem_used_raw", "memory_used_raw"),
			"memoryTotalBytes": firstFloatValue(cached, "mem_total_raw", "memory_total_raw"),
			"disk":             firstFloatValue(cached, "disk_percent", "disk_usage"),
			"diskUsed":         firstNonEmpty(getString(cached, "disk_used"), getString(cached, "disk_used_text")),
			"diskTotal":        firstNonEmpty(getString(cached, "disk_total"), getString(cached, "disk_total_text")),
			"diskUsedBytes":    firstFloatValue(cached, "disk_used_raw"),
			"diskTotalBytes":   firstFloatValue(cached, "disk_total_raw"),
			"netRx":            firstFloatValue(cached, "net_rx", "net_in_speed", "network_rx"),
			"netTx":            firstFloatValue(cached, "net_tx", "net_out_speed", "network_tx"),
			"connections":      firstFloatValue(cached, "connections", "tcp_conn_count"),
			"dockerRunning":    firstFloatValue(cached, "docker_running"),
			"dockerStopped":    firstFloatValue(cached, "docker_stopped"),
			"gpu":              firstFloatValue(cached, "gpu_usage", "gpu"),
			"gpuTemp":          firstFloatValue(cached, "gpu_temp", "gpuTemp"),
			"gpuPower":         firstFloatValue(cached, "gpu_power", "gpuPower"),
			"gpuMemory":        firstFloatValue(cached, "gpu_mem_percent"),
			"gpuModel":         getString(cached, "gpu_model"),
			"responseTime":     responseTime,
			"updatedAt":        firstNonEmpty(lastHeartbeat, getString(cached, "metrics_last_seen"), updatedAt),
		}
		if hasLat && hasUsableCoordinates(lat, lon) {
			item["latitude"] = lat
		}
		if hasLon && hasUsableCoordinates(lat, lon) {
			item["longitude"] = lon
		}
		if !hideHosts {
			item["host"] = host
		}
		if showTraffic {
			item["trafficUsedBytes"] = trafficUsed
			item["trafficRxBytes"] = trafficRxBytes
			item["trafficTxBytes"] = trafficTxBytes
			item["trafficLimitBytes"] = trafficLimit
			item["trafficLimitMode"] = normalizeTrafficLimitMode(trafficLimitMode)
		}
		if showCharts {
			historyServerIDs = append(historyServerIDs, id)
		}
		latencyServerIDs = append(latencyServerIDs, id)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if showCharts {
		for index, id := range historyServerIDs {
			items[index]["history"] = getPublicServerMetricHistory(ctx, db, id)
		}
	}
	for index, id := range latencyServerIDs {
		items[index]["latencyHistory"] = getPublicServerLatencyHistory(ctx, db, id)
	}
	return items, nil
}

func getPublicServerMetricHistory(ctx context.Context, db *sql.DB, serverID string) []map[string]interface{} {
	rows, err := db.QueryContext(ctx, `SELECT recorded_at, cpu_usage, mem_usage, disk_usage, net_rx, net_tx FROM server_metrics_history WHERE server_id = ? ORDER BY recorded_at DESC LIMIT 60`, serverID)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var recordedAt string
		var cpu, mem, disk, rx, tx sql.NullFloat64
		if err := rows.Scan(&recordedAt, &cpu, &mem, &disk, &rx, &tx); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"time":   recordedAt,
			"cpu":    nullFloat(cpu),
			"memory": nullFloat(mem),
			"disk":   nullFloat(disk),
			"netRx":  nullFloat(rx),
			"netTx":  nullFloat(tx),
		})
	}
	return items
}

func getPublicServerLatencyHistory(ctx context.Context, db *sql.DB, serverID string) []map[string]interface{} {
	rows, err := db.QueryContext(ctx, `SELECT status, COALESCE(response_time, 0), checked_at FROM server_monitor_logs WHERE server_id = ? ORDER BY checked_at DESC LIMIT 28`, serverID)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var status, checkedAt string
		var responseTime int64
		if err := rows.Scan(&status, &responseTime, &checkedAt); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"status":       status,
			"responseTime": responseTime,
			"time":         checkedAt,
		})
	}
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items
}

func normalizePublicServerLiveMetrics(cached map[string]interface{}, metadata map[string]interface{}) {
	network := mapValue(metadata["network"])
	docker := mapValue(metadata["docker"])
	gpu := mapValue(metadata["gpu"])
	if value, ok := metadata["cpu_usage"]; ok {
		cached["cpu"] = value
	}
	if value, ok := metadata["mem_usage_percent"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["memory"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["memory_usage"]; ok {
		cached["mem_percent"] = value
	}
	if value, ok := metadata["disk_usage"]; ok {
		cached["disk_percent"] = value
	}
	if value, ok := metadata["net_in_speed"]; ok {
		cached["net_rx"] = value
	}
	if value, ok := metadata["net_out_speed"]; ok {
		cached["net_tx"] = value
	}
	if value, ok := metadata["network_rx"]; ok {
		cached["net_rx"] = value
	}
	if value, ok := metadata["network_tx"]; ok {
		cached["net_tx"] = value
	}
	if value, ok := network["connections"]; ok {
		cached["connections"] = value
	}
	if value, ok := docker["running"]; ok {
		cached["docker_running"] = value
	}
	if value, ok := docker["stopped"]; ok {
		cached["docker_stopped"] = value
	}
	if value, ok := gpu["Usage"]; ok {
		cached["gpu_usage"] = value
	}
	if value, ok := gpu["Temp"]; ok {
		cached["gpu_temp"] = value
	}
	if value, ok := gpu["Power"]; ok {
		cached["gpu_power"] = value
	}
}

func scanServerStatusPageRows(rows *sql.Rows) (map[string]interface{}, error) {
	var id int64
	var public, cacheSeconds int
	var slug, title, createdAt, updatedAt string
	var domain, description, configJSON, serverIDsJSON sql.NullString
	if err := rows.Scan(&id, &slug, &domain, &title, &description, &public, &cacheSeconds, &configJSON, &serverIDsJSON, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	config := map[string]interface{}{}
	_ = json.Unmarshal([]byte(nullStringDefault(configJSON, "{}")), &config)
	serverIDs := []string{}
	_ = json.Unmarshal([]byte(nullStringDefault(serverIDsJSON, "[]")), &serverIDs)
	return map[string]interface{}{
		"id":           id,
		"slug":         slug,
		"domain":       nullStringVal(domain),
		"title":        title,
		"description":  nullStringDefault(description, ""),
		"public":       public != 0,
		"cacheSeconds": cacheSeconds,
		"config":       config,
		"serverIds":    serverIDs,
		"createdAt":    createdAt,
		"updatedAt":    updatedAt,
	}, nil
}

var nonSlugCharsRegex = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeServerStatusSlug(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	text = nonSlugCharsRegex.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	if text == "" {
		return "servers"
	}
	return text
}

func normalizeServerStatusDomain(value string) string {
	domain := strings.TrimSpace(strings.ToLower(value))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if index := strings.Index(domain, "/"); index >= 0 {
		domain = domain[:index]
	}
	domain = strings.TrimSuffix(domain, "/")
	if host, _, err := net.SplitHostPort(domain); err == nil {
		return host
	}
	return domain
}

func serverStatusJSON(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func nullableServerStatusString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullStringDefault(value sql.NullString, fallback string) string {
	if !value.Valid || value.String == "" {
		return fallback
	}
	return value.String
}

func mapValue(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return map[string]interface{}{}
}

func stringSliceValue(value interface{}) []string {
	if typed, ok := value.([]string); ok {
		return typed
	}
	if typed, ok := value.([]interface{}); ok {
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			text = strings.TrimSpace(text)
			if ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	}
	return []string{}
}

// ==========================================
// CREDENTIALS HANDLERS
// ==========================================
