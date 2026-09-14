package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func (s *Service) buildAccountResponse(
	id, name, host string, port int, username, authType string,
	password, privateKey, passphrase sql.NullString,
	status, monitorMode string,
	lastCheckTime, lastCheckStatus sql.NullString,
	responseTime sql.NullInt64,
	cachedInfo sql.NullString,
	tagsStr string,
	description, country, resolvedCountry, startsAt, expiresAt sql.NullString,
	orderIndex int,
	createdAt, updatedAt string,
	trafficLimitBytes int64,
	trafficLimitMode string,
	trafficAlertEnabled bool,
	trafficAlertPercent float64,
	trafficCycleType sql.NullString,
	trafficCycleDay int,
	trafficCycleStart, trafficCycleEnd sql.NullString,
	trafficCycleBaseline int64,
) map[string]interface{} {
	conn, agentOnline := s.registry.Get(id)
	health := s.resolveAgentMetricsHealth(id, cachedInfo, agentOnline, time.Now())
	effectiveStatus := status
	if agentOnline {
		effectiveStatus = "online"
	} else if s.presence != nil {
		// 实时在线性以 Agent 心跳为准：连接中断时不应沿用数据库里的陈旧
		// "online" 状态，避免仪表盘把"中断"误判为在线。
		presenceStatus, _ := s.presence.snapshot(id)["presence_status"].(string)
		switch presenceStatus {
		case string(agentPresenceSuspect):
			effectiveStatus = "interrupted"
		case string(agentPresenceOffline):
			effectiveStatus = "offline"
		}
	}
	isOnline := effectiveStatus == "online"

	decryptedPassword := s.decryptField(password)
	decryptedPrivateKey := s.decryptField(privateKey)
	decryptedPassphrase := s.decryptField(passphrase)

	capabilities := getServerCapabilities(host, port, username, authType, decryptedPrivateKey, decryptedPassword, isOnline)

	res := map[string]interface{}{
		"id":                    id,
		"name":                  name,
		"host":                  host,
		"port":                  port,
		"username":              username,
		"auth_type":             authType,
		"password":              decryptedPassword,
		"private_key":           decryptedPrivateKey,
		"passphrase":            decryptedPassphrase,
		"status":                effectiveStatus,
		"monitor_mode":          monitorMode,
		"last_check_time":       nullStringVal(lastCheckTime),
		"last_check_status":     nullStringVal(lastCheckStatus),
		"response_time":         nullIntVal(responseTime),
		"tags":                  parseJSONTags(tagsStr),
		"description":           nullStringVal(description),
		"country":               nullStr(cleanCountryCode(country.String)),
		"resolved_country":      nullStringVal(resolvedCountry),
		"starts_at":             nullStringVal(startsAt),
		"expires_at":            nullStringVal(expiresAt),
		"traffic_limit_bytes":   trafficLimitBytes,
		"traffic_limit_mode":    normalizeTrafficLimitMode(trafficLimitMode),
		"traffic_alert_enabled": trafficAlertEnabled,
		"traffic_alert_percent": normalizeTrafficAlertPercent(trafficAlertPercent),
		"traffic_cycle_type":    normalizeTrafficCycleType(trafficCycleType.String),
		"traffic_cycle_day":     normalizeTrafficCycleDay(trafficCycleDay),
		"traffic_cycle_start":   nullStringVal(trafficCycleStart),
		"traffic_cycle_end":     nullStringVal(trafficCycleEnd),
		"order_index":           orderIndex,
		"created_at":            createdAt,
		"updated_at":            updatedAt,
	}

	for k, v := range capabilities {
		res[k] = v
	}
	res["agent_online"] = agentOnline
	res["agent_connected"] = agentOnline
	if agentOnline && conn != nil {
		res["agent_capabilities"] = conn.GetCapabilities()
	}
	res["supports_metrics"] = agentOnline && health["state"] == "fresh"
	res["metrics_health"] = health["state"]
	res["metrics_stale"] = health["stale"]
	res["metrics_last_seen"] = health["last_seen"]
	res["metrics_last_seen_at"] = health["last_seen_at"]
	res["metrics_age_ms"] = health["age_ms"]

	// Metrics mapping
	cachedMetrics := map[string]interface{}{}
	hasMetricsPayload := cachedInfo.Valid && cachedInfo.String != ""
	if cachedInfo.Valid && cachedInfo.String != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &parsed); err == nil {
			cachedMetrics = parsed
		}
	}
	if agentOnline && conn != nil {
		metadata := conn.GetMetadata()
		for key, value := range metadata {
			if !isEmptyHeartbeatInfoValue(value) {
				cachedMetrics[key] = value
			}
		}
		normalizePublicServerLiveMetrics(cachedMetrics, metadata)
		hasMetricsPayload = true
	}
	if hasMetricsPayload {
		info := s.buildInfoField(cachedMetrics)
		enrichTrafficQuota(info, cachedMetrics, trafficLimitBytes, trafficLimitMode, trafficAlertEnabled, trafficAlertPercent, trafficCycleType.String, trafficCycleBaseline)
		res["info"] = info
		resolvedCountryText := ""
		if resolvedCountry.Valid {
			resolvedCountryText = resolvedCountry.String
		}
		countryText := ""
		if country.Valid {
			countryText = country.String
		}
		res["location"] = firstNonEmpty(getString(cachedMetrics, "location"), getString(cachedMetrics, "region"), resolvedCountryText)
		res["countryCode"] = firstNonEmpty(cleanCountryCode(getString(cachedMetrics, "country_code")), cleanCountryCode(getString(cachedMetrics, "country")), cleanCountryCode(countryText))
		lat, hasLat := firstOptionalFloatValue(cachedMetrics, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(cachedMetrics, "lon", "longitude")
		if hasLat && hasUsableCoordinates(lat, lon) {
			res["latitude"] = lat
		}
		if hasLon && hasUsableCoordinates(lat, lon) {
			res["longitude"] = lon
		}
	}

	return res
}

func (s *Service) markRealtimeMetricsHealthy(serverID string, metrics map[string]interface{}, now time.Time) {
	if metrics == nil {
		return
	}
	seenAt := now.UTC().Format(time.RFC3339Nano)
	metrics["metrics_last_seen"] = seenAt
	metrics["metrics_health"] = "fresh"
	metrics["metrics_stale_after_ms"] = int64(agentMetricsStaleAfter / time.Millisecond)
	if conn, exists := s.registry.Get(serverID); exists {
		conn.SetMetadata("metrics_last_seen", seenAt)
		conn.SetMetadata("metrics_health", "fresh")
		conn.SetMetadata("metrics_stale_after_ms", int64(agentMetricsStaleAfter/time.Millisecond))
		if sequence, ok := metrics["sequence"]; ok {
			conn.SetMetadata("metrics_sequence", sequence)
		}
		if interval, ok := metrics["sample_interval_ms"]; ok {
			conn.SetMetadata("metrics_sample_interval_ms", interval)
		}
	}
}

func (s *Service) markRealtimeMetricsPersistResult(serverID string, ok bool, err error, now time.Time) {
	conn, exists := s.registry.Get(serverID)
	if !exists {
		return
	}
	previousStatus := ""
	if metadata := conn.GetMetadata(); metadata != nil {
		previousStatus, _ = metadata["metrics_persist_status"].(string)
	}
	status := "ok"
	errorText := ""
	if !ok {
		status = "error"
		if err != nil {
			errorText = err.Error()
		}
	}
	conn.SetMetadata("metrics_persist_status", status)
	conn.SetMetadata("metrics_persist_error", errorText)
	conn.SetMetadata("metrics_persist_at", now.UTC().Format(time.RFC3339Nano))
	if !ok && previousStatus != "error" {
		if !s.trackPending() {
			return
		}
		go func() {
			defer s.pendingWG.Done()
			ctx, cancel := context.WithTimeout(s.backgroundCtx, 5*time.Second)
			defer cancel()
			db, openErr := s.open(ctx)
			if openErr != nil {
				return
			}
			defer db.Close()
			serverName, serverHost := s.serverIdentity(ctx, db, serverID)
			s.triggerServerStatusNotification(ctx, serverID, serverName, serverHost, "degraded")
		}()
	}
}

func (s *Service) mergeConnectionLocationMetadata(serverID string, geo map[string]interface{}) {
	if geo == nil {
		return
	}
	conn, exists := s.registry.Get(serverID)
	if !exists {
		return
	}
	for _, key := range cachedLocationFieldNames() {
		if value, ok := geo[key]; ok && !isEmptyHeartbeatInfoValue(value) {
			conn.SetMetadata(key, value)
		}
	}
}

func (s *Service) mergeCachedLocationFieldsFromDB(ctx context.Context, db *sql.DB, serverID string, metrics map[string]interface{}) map[string]interface{} {
	if metrics == nil {
		metrics = map[string]interface{}{}
	}
	var raw string
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?", serverID).Scan(&raw); err != nil {
		return metrics
	}
	existing := map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &existing); err != nil {
		return metrics
	}
	preserveCachedLocationFields(metrics, existing)
	s.mergeConnectionLocationMetadata(serverID, metrics)
	return metrics
}

func cachedLocationFieldNames() []string {
	return []string{
		"ip",
		"country_code",
		"country",
		"resolved_country",
		"region",
		"location",
		"city",
		"latitude",
		"longitude",
		"lat",
		"lon",
		"isp",
		"org",
		"asn",
		"timezone",
		"geo_source",
	}
}

func preserveCachedLocationFields(target map[string]interface{}, existing map[string]interface{}) {
	if target == nil || existing == nil {
		return
	}
	for _, key := range cachedLocationFieldNames() {
		if value, ok := existing[key]; ok && !isEmptyHeartbeatInfoValue(value) {
			if current, exists := target[key]; !exists || isEmptyHeartbeatInfoValue(current) {
				target[key] = value
			}
		}
	}
}

func cloneMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (s *Service) resolveAgentMetricsHealth(serverID string, cachedInfo sql.NullString, agentOnline bool, now time.Time) map[string]interface{} {
	state := "missing"
	var lastSeen time.Time

	if conn, exists := s.registry.Get(serverID); exists {
		metadata := conn.GetMetadata()
		if raw, ok := metadata["metrics_last_seen"]; ok {
			lastSeen = parseAgentMetricTime(raw)
		}
		if metadata["metrics_persist_status"] == "error" {
			state = "degraded"
		}
	}

	if lastSeen.IsZero() && cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			lastSeen = parseAgentMetricTime(cached["metrics_last_seen"])
		}
	}

	ageMs := int64(0)
	if !lastSeen.IsZero() {
		age := now.Sub(lastSeen)
		if age < 0 {
			age = 0
		}
		ageMs = int64(age / time.Millisecond)
		if age <= agentMetricsStaleAfter {
			if state != "degraded" {
				state = "fresh"
			}
		} else {
			state = "stale"
		}
	} else if !agentOnline {
		state = "offline"
	}

	return map[string]interface{}{
		"state":        state,
		"stale":        state == "stale" || state == "missing" || state == "degraded",
		"last_seen":    formatAgentMetricTime(lastSeen),
		"last_seen_at": timeToMillis(lastSeen),
		"age_ms":       ageMs,
	}
}

func parseAgentMetricTime(value interface{}) time.Time {
	switch v := value.(type) {
	case string:
		if v == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	case float64:
		if v > 0 {
			return time.UnixMilli(int64(v))
		}
	case int64:
		if v > 0 {
			return time.UnixMilli(v)
		}
	case int:
		if v > 0 {
			return time.UnixMilli(int64(v))
		}
	}
	return time.Time{}
}

func formatAgentMetricTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timeToMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

var diskRegexp = regexp.MustCompile(`([^/]+)/([^\s]+)\s\((\d+(?:\.\d+)?%?)\)`)

func (s *Service) buildInfoField(metrics map[string]interface{}) map[string]interface{} {
	cpuLoad := ""
	if l, ok := metrics["load"].(string); ok {
		cpuLoad = l
	}
	cpuCores := 1
	if c, ok := metrics["cores"]; ok {
		if f, err := toFloat(c); err == nil {
			cpuCores = int(f)
		}
	}
	logicalCores := cpuCores
	if c, ok := metrics["logical_cores"]; ok {
		if f, err := toFloat(c); err == nil {
			logicalCores = int(f)
		}
	}
	physicalCores := cpuCores
	if c, ok := metrics["physical_cores"]; ok {
		if f, err := toFloat(c); err == nil {
			physicalCores = int(f)
		}
	}
	cpuUsage := "0%"
	if u, ok := metrics["cpu_usage"].(string); ok {
		cpuUsage = u
	}
	var cpuTemp float64 = 0
	if t, ok := metrics["cpu_temp"]; ok {
		if f, err := toFloat(t); err == nil {
			cpuTemp = f
		}
	}
	var cpuPower float64 = 0
	if p, ok := metrics["cpu_power"]; ok {
		if f, err := toFloat(p); err == nil {
			cpuPower = f
		}
	}
	cpuModel := extractCPUModel(metrics)

	// Memory
	memPercent := ""
	if mp, ok := metrics["mem_percent"]; ok {
		if f, err := toFloat(mp); err == nil {
			memPercent = fmt.Sprintf("%.0f%%", f)
		}
	}
	mem := ""
	if m, ok := metrics["mem"].(string); ok {
		mem = m
	}
	memUsed := "-"
	memTotal := "-"
	if mem != "" {
		parts := strings.Split(mem, "/")
		if len(parts) == 2 {
			memUsed = parts[0]
			memTotal = parts[1]
		}
	}

	// Disk
	diskStr := ""
	if d, ok := metrics["disk"].(string); ok {
		diskStr = d
	}
	diskArray := parseDiskString(diskStr)
	if diskStr == "" || (len(diskArray) > 0 && diskArray[0]["used"] == "-") {
		used := getString(metrics, "disk_used")
		total := getString(metrics, "disk_total")
		percent := getFloat(metrics, "disk_percent")
		if percent == 0.0 {
			percent = getFloat(metrics, "disk_usage")
		}
		if used != "" && total != "" {
			diskArray = []map[string]interface{}{
				{
					"device": "/",
					"used":   used,
					"total":  total,
					"usage":  fmt.Sprintf("%.0f%%", percent),
				},
			}
		}
	}

	dockerVal := metrics["docker"]
	if dockerVal == nil {
		dockerVal = map[string]interface{}{"installed": false, "running": 0, "stopped": 0, "containers": []interface{}{}}
	}
	networkVal := metrics["network"]

	platform := ""
	if p, ok := metrics["platform"].(string); ok {
		platform = p
	}
	platformVersion := ""
	if pv, ok := metrics["platformVersion"].(string); ok {
		platformVersion = pv
	}
	agentVersion := ""
	if av, ok := metrics["agent_version"].(string); ok {
		agentVersion = av
	}
	ip := ""
	if i, ok := metrics["ip"].(string); ok {
		ip = i
	}
	countryCode := firstNonEmpty(cleanCountryCode(getString(metrics, "country_code")), cleanCountryCode(getString(metrics, "country")))
	resolvedCountry := firstNonEmpty(getString(metrics, "resolved_country"), countryCode)
	location := firstNonEmpty(getString(metrics, "location"), getString(metrics, "region"), resolvedCountry)
	latitude, hasLatitude := firstOptionalFloatValue(metrics, "lat", "latitude")
	longitude, hasLongitude := firstOptionalFloatValue(metrics, "lon", "longitude")
	uptime := ""
	if u, ok := metrics["uptime"].(string); ok {
		uptime = u
	}

	lastUpdate := "-"
	if lu, ok := metrics["lastUpdate"].(string); ok {
		lastUpdate = lu
	} else if ts, ok := metrics["timestamp"]; ok {
		if f, err := toFloat(ts); err == nil {
			t := time.UnixMilli(int64(f))
			lastUpdate = t.Format("15:04:05")
		}
	}

	cachedInfo := map[string]interface{}{
		"cpu": map[string]interface{}{
			"Model":         cpuModel,
			"Load":          cpuLoad,
			"Cores":         cpuCores,
			"LogicalCores":  logicalCores,
			"PhysicalCores": physicalCores,
			"Usage":         cpuUsage,
			"Temp":          cpuTemp,
			"Power":         cpuPower,
		},
		"memory": map[string]interface{}{
			"Usage": memPercent,
			"Used":  memUsed,
			"Total": memTotal,
		},
		"disk":              diskArray,
		"docker":            dockerVal,
		"network":           networkVal,
		"gpu":               buildGpuInfo(metrics),
		"platform":          platform,
		"platformVersion":   platformVersion,
		"agentVersion":      agentVersion,
		"ip":                ip,
		"country_code":      countryCode,
		"resolved_country":  resolvedCountry,
		"location":          location,
		"region":            getString(metrics, "region"),
		"uptime":            uptime,
		"lastUpdate":        lastUpdate,
		"metrics_health":    getString(metrics, "metrics_health"),
		"metrics_last_seen": getString(metrics, "metrics_last_seen"),
		"metrics_last_seen_at": func() int64 {
			if seen := parseAgentMetricTime(metrics["metrics_last_seen"]); !seen.IsZero() {
				return seen.UnixMilli()
			}
			return 0
		}(),
		"metrics_stale_after_ms": getIntValue(metrics, "metrics_stale_after_ms"),
		"metrics_sequence":       getIntValue(metrics, "sequence"),
		"sample_interval_ms":     getIntValue(metrics, "sample_interval_ms"),
	}
	if hasLatitude && hasUsableCoordinates(latitude, longitude) {
		cachedInfo["latitude"] = latitude
	}
	if hasLongitude && hasUsableCoordinates(latitude, longitude) {
		cachedInfo["longitude"] = longitude
	}
	return cachedInfo
}
