package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, traffic_cycle_baseline, updated_at FROM server_accounts ORDER BY order_index ASC, created_at DESC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		account, err := s.scanAccount(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.refreshAccountLocationIfMissingFromList(account)
		list = append(list, account)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	response.OK(w, list)
}

func (s *Service) refreshAccountLocations(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	updated, skipped, err := s.refreshAccountLocationsFromAgents(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"updated": updated, "skipped": skipped})
}

func (s *Service) refreshAccountLocationsFromAgents(ctx context.Context, db *sql.DB) (int, int, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, cached_info FROM server_accounts")
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	type candidate struct {
		id         string
		cachedInfo sql.NullString
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.cachedInfo); err != nil {
			return 0, 0, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	now := time.Now().Format(time.RFC3339)
	updated := 0
	skipped := 0
	for _, item := range candidates {
		ok, err := s.refreshAccountLocationFromAgent(ctx, db, item.id, item.cachedInfo, now, true)
		if err != nil {
			return updated, skipped, err
		}
		if ok {
			updated++
		} else {
			skipped++
		}
	}
	return updated, skipped, nil
}

func (s *Service) refreshAccountLocationIfMissingFromList(account map[string]interface{}) {
	if account == nil || !getBoolVal(account, "agent_online", false) {
		return
	}
	serverID := strings.TrimSpace(getString(account, "id"))
	if serverID == "" {
		return
	}
	if countryCode := cleanCountryCode(getString(account, "countryCode")); countryCode != "" {
		lat, hasLat := firstOptionalFloatValue(account, "lat", "latitude")
		lon, hasLon := firstOptionalFloatValue(account, "lon", "longitude")
		if hasLat && hasLon && hasUsableCoordinates(lat, lon) {
			return
		}
	}
	if last, ok := s.lastAutoLocationRefresh.Load(serverID); ok {
		if lastAt, ok := last.(time.Time); ok && time.Since(lastAt) < 5*time.Minute {
			return
		}
	}
	s.lastAutoLocationRefresh.Store(serverID, time.Now())
	if !s.trackPending() {
		return
	}
	go func(id string) {
		defer s.pendingWG.Done()
		s.refreshAccountLocationFromAgentIfMissing(id)
	}(serverID)
}

func (s *Service) refreshAccountLocationFromAgentIfMissing(serverID string) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	if _, loaded := s.autoLocationRefreshes.LoadOrStore(serverID, struct{}{}); loaded {
		return
	}
	defer s.autoLocationRefreshes.Delete(serverID)

	ctx, cancel := context.WithTimeout(s.backgroundCtx, 20*time.Second)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	var cachedInfo sql.NullString
	var country, resolvedCountry sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT country, resolved_country, cached_info FROM server_accounts WHERE id = ?", serverID).Scan(&country, &resolvedCountry, &cachedInfo); err != nil {
		return
	}
	if !accountNeedsLocation(country, resolvedCountry, cachedInfo) {
		return
	}
	if _, err := s.refreshAccountLocationFromAgent(ctx, nil, serverID, cachedInfo, time.Now().Format(time.RFC3339), false); err != nil {
		applog.Warn(ctx, "serveragent", "failed to refresh initial location from agent", "server_id", serverID, "error", err.Error())
	}
}

func (s *Service) refreshAccountLocationFromAgent(ctx context.Context, db *sql.DB, serverID string, cachedInfo sql.NullString, now string, force bool) (bool, error) {
	if _, ok := s.registry.Get(serverID); !ok {
		return false, nil
	}
	if !force && !accountNeedsLocation(sql.NullString{}, sql.NullString{}, cachedInfo) {
		return false, nil
	}
	output, err := s.RunCommandTaskAndWait(serverID, "curl -fsSL https://64.ipcheck.ing/geo", 15*time.Second)
	if err != nil {
		applog.Warn(ctx, "serveragent", "failed to refresh location from agent", "server_id", serverID, "error", err.Error())
		return false, nil
	}
	geo, ok := parseIPCheckGeo(output)
	if !ok {
		return false, nil
	}
	s.mergeConnectionLocationMetadata(serverID, geo)
	openedDB := false
	if db == nil {
		var err error
		db, err = s.open(ctx)
		if err != nil {
			return false, err
		}
		openedDB = true
	}
	if openedDB {
		defer db.Close()
	}
	nextCachedInfo := mergeCachedInfo(cachedInfo, geo)
	_, err = db.ExecContext(ctx, `
		UPDATE server_accounts
		SET resolved_country = ?, cached_info = ?, updated_at = ?
		WHERE id = ?`,
		firstNonEmpty(getString(geo, "location"), getString(geo, "region"), getString(geo, "country_code")), nextCachedInfo, now, serverID,
	)
	if err != nil {
		return false, err
	}
	if s.metricsHub != nil {
		s.metricsHub.BroadcastMetrics(serverID, geo)
	}
	return true, nil
}

func (s *Service) getAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, traffic_cycle_baseline, updated_at FROM server_accounts WHERE id = ?", id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	if !rows.Next() {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	}

	account, err := s.scanAccount(rows)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, account)
}

func (s *Service) createAccount(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Name                string      `json:"name"`
		Host                string      `json:"host"`
		Port                int         `json:"port"`
		Username            string      `json:"username"`
		AuthType            string      `json:"auth_type"`
		Password            string      `json:"password"`
		PrivateKey          string      `json:"private_key"`
		Passphrase          string      `json:"passphrase"`
		Tags                interface{} `json:"tags"`
		Description         string      `json:"description"`
		Country             string      `json:"country"`
		StartsAt            string      `json:"starts_at"`
		ExpiresAt           string      `json:"expires_at"`
		MonitorMode         string      `json:"monitor_mode"`
		TrafficLimitBytes   int64       `json:"traffic_limit_bytes"`
		TrafficLimitMode    string      `json:"traffic_limit_mode"`
		TrafficAlertEnabled bool        `json:"traffic_alert_enabled"`
		TrafficAlertPercent float64     `json:"traffic_alert_percent"`
		TrafficCycleType    string      `json:"traffic_cycle_type"`
		TrafficCycleDay     int         `json:"traffic_cycle_day"`
		TrafficCycleStart   string      `json:"traffic_cycle_start"`
		TrafficCycleEnd     string      `json:"traffic_cycle_end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	isAgentMode := req.MonitorMode == "agent"
	if req.Name == "" || (!isAgentMode && (req.Host == "" || req.Username == "" || req.AuthType == "")) {
		response.Error(w, http.StatusBadRequest, "缺少必填字段")
		return
	}

	id := uuid.NewString()
	now := time.Now().Format(time.RFC3339)
	country := cleanCountryCode(req.Country)

	// Max order_index
	var maxOrder sql.NullInt64
	_ = db.QueryRowContext(r.Context(), "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
	orderIndex := int(maxOrder.Int64) + 1

	encPassword := s.encryptField(req.Password)
	encPrivateKey := s.encryptField(req.PrivateKey)
	encPassphrase := s.encryptField(req.Passphrase)
	resolvedCountry := ""
	cachedInfo := sql.NullString{}
	if country == "" {
		if geo, ok := s.lookupHostLocation(r.Context(), req.Host); ok {
			resolvedCountry = getString(geo, "region")
			cachedInfo = sql.NullString{String: mergeCachedInfo(sql.NullString{}, geo), Valid: true}
		}
	}
	trafficLimitBytes := normalizeTrafficLimitBytes(req.TrafficLimitBytes)
	trafficLimitMode := normalizeTrafficLimitMode(req.TrafficLimitMode)
	trafficAlertPercent := normalizeTrafficAlertPercent(req.TrafficAlertPercent)
	trafficCycleType := normalizeTrafficCycleType(req.TrafficCycleType)
	trafficCycleDay := normalizeTrafficCycleDay(req.TrafficCycleDay)

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO server_accounts (
			id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, traffic_limit_mode, traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, updated_at, cached_info
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Host, coalesceInt(req.Port, 22), coalesceStr(req.Username, "agent"), coalesceStr(req.AuthType, "password"),
		encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(req.Tags), req.Description, coalesceStr(req.MonitorMode, "agent"), nullStr(country), nullStr(resolvedCountry), nullStr(req.StartsAt), nullStr(req.ExpiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(req.TrafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(req.TrafficCycleStart), nullStr(req.TrafficCycleEnd), orderIndex, now, now, nullStr(cachedInfo.String),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	account, err := s.queryAccountByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "服务器添加成功",
		"data":    account,
	})
}

func (s *Service) updateAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch existing raw
	var raw struct {
		name, host, username, authType, status, monitorMode, tags, createdAt string
		password, privateKey, passphrase                                     sql.NullString
		description, country, resolvedCountry, startsAt, expiresAt           sql.NullString
		trafficCycleType, trafficCycleStart, trafficCycleEnd                 sql.NullString
		port, orderIndex                                                     int
		trafficLimitBytes                                                    int64
		trafficLimitMode                                                     string
		trafficAlertEnabled                                                  int
		trafficAlertPercent                                                  float64
		trafficCycleDay                                                      int
		responseTime                                                         sql.NullInt64
		lastCheckTime, lastCheckStatus, cachedInfo                           sql.NullString
	}
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, tags, description, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, response_time, last_check_time, last_check_status, cached_info FROM server_accounts WHERE id = ?", id).
		Scan(&raw.name, &raw.host, &raw.port, &raw.username, &raw.authType, &raw.password, &raw.privateKey, &raw.passphrase, &raw.status, &raw.monitorMode, &raw.tags, &raw.description, &raw.country, &raw.resolvedCountry, &raw.startsAt, &raw.expiresAt, &raw.trafficLimitBytes, &raw.trafficLimitMode, &raw.trafficAlertEnabled, &raw.trafficAlertPercent, &raw.trafficCycleType, &raw.trafficCycleDay, &raw.trafficCycleStart, &raw.trafficCycleEnd, &raw.orderIndex, &raw.createdAt, &raw.responseTime, &raw.lastCheckTime, &raw.lastCheckStatus, &raw.cachedInfo)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	} else if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	name := getStringVal(req, "name", raw.name)
	host := getStringVal(req, "host", raw.host)
	port := getIntVal(req, "port", raw.port)
	username := getStringVal(req, "username", raw.username)
	authType := getStringVal(req, "auth_type", raw.authType)
	description := getStringVal(req, "description", raw.description.String)
	monitorMode := getStringVal(req, "monitor_mode", raw.monitorMode)
	country := cleanCountryCode(getStringVal(req, "country", raw.country.String))
	resolvedCountry := getStringVal(req, "resolved_country", raw.resolvedCountry.String)
	cachedInfo := raw.cachedInfo
	if country == "" {
		hostChanged := host != raw.host
		if hostChanged || resolvedCountry == "" || accountNeedsLocation(sql.NullString{String: country, Valid: country != ""}, sql.NullString{String: resolvedCountry, Valid: resolvedCountry != ""}, cachedInfo) {
			if geo, ok := s.lookupHostLocation(r.Context(), host); ok {
				resolvedCountry = getString(geo, "region")
				cachedInfo = sql.NullString{String: mergeCachedInfo(cachedInfo, geo), Valid: true}
			}
		}
	}
	startsAt := getStringVal(req, "starts_at", raw.startsAt.String)
	expiresAt := getStringVal(req, "expires_at", raw.expiresAt.String)
	orderIndex := getIntVal(req, "order_index", raw.orderIndex)
	trafficLimitBytes := normalizeTrafficLimitBytes(getInt64Val(req, "traffic_limit_bytes", raw.trafficLimitBytes))
	trafficLimitMode := normalizeTrafficLimitMode(getStringVal(req, "traffic_limit_mode", raw.trafficLimitMode))
	trafficAlertEnabled := getBoolVal(req, "traffic_alert_enabled", raw.trafficAlertEnabled != 0)
	trafficAlertPercent := normalizeTrafficAlertPercent(getFloatVal(req, "traffic_alert_percent", raw.trafficAlertPercent))
	trafficCycleType := normalizeTrafficCycleType(getStringVal(req, "traffic_cycle_type", raw.trafficCycleType.String))
	trafficCycleDay := normalizeTrafficCycleDay(getIntVal(req, "traffic_cycle_day", raw.trafficCycleDay))
	trafficCycleStart := getStringVal(req, "traffic_cycle_start", raw.trafficCycleStart.String)
	trafficCycleEnd := getStringVal(req, "traffic_cycle_end", raw.trafficCycleEnd.String)

	password := raw.password.String
	if p, ok := req["password"].(string); ok {
		password = s.encryptFieldString(p)
	}
	privateKey := raw.privateKey.String
	if k, ok := req["private_key"].(string); ok {
		privateKey = s.encryptFieldString(k)
	}
	passphrase := raw.passphrase.String
	if p, ok := req["passphrase"].(string); ok {
		passphrase = s.encryptFieldString(p)
	}

	tags := raw.tags
	if val, ok := req["tags"]; ok {
		tags = SerializeList(val)
	}

	now := time.Now().Format(time.RFC3339)

	_, err = db.ExecContext(r.Context(), `
		UPDATE server_accounts
		SET name = ?, host = ?, port = ?, username = ?, auth_type = ?, password = ?, private_key = ?, passphrase = ?, tags = ?, description = ?, monitor_mode = ?, country = ?, resolved_country = ?, starts_at = ?, expires_at = ?, traffic_limit_bytes = ?, traffic_limit_mode = ?, traffic_alert_enabled = ?, traffic_alert_percent = ?, traffic_cycle_type = ?, traffic_cycle_day = ?, traffic_cycle_start = ?, traffic_cycle_end = ?, order_index = ?, cached_info = ?, updated_at = ?
		WHERE id = ?`,
		name, host, port, username, authType, password, privateKey, passphrase, tags, description, monitorMode, nullStr(country), nullStr(resolvedCountry), nullStr(startsAt), nullStr(expiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(trafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(trafficCycleStart), nullStr(trafficCycleEnd), orderIndex, nullStr(cachedInfo.String), now, id,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	account, err := s.queryAccountByID(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "服务器更新成功",
		"data":    account,
	})
}

func (s *Service) testTrafficAlert(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if s.notifier == nil {
		response.Error(w, http.StatusBadRequest, "notification service is not available")
		return
	}
	var payload struct {
		TrafficAlertPercent float64 `json:"traffic_alert_percent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	var serverName, serverHost string
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertPercent float64
	var cachedInfo sql.NullString
	var trafficCycleType string
	var trafficCycleBaseline int64
	err := db.QueryRowContext(r.Context(), `
		SELECT name, host, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_percent, cached_info, COALESCE(traffic_cycle_type, 'none'), COALESCE(traffic_cycle_baseline, 0)
		FROM server_accounts
		WHERE id = ?`, id).
		Scan(&serverName, &serverHost, &trafficLimitBytes, &trafficLimitMode, &trafficAlertPercent, &cachedInfo, &trafficCycleType, &trafficCycleBaseline)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if serverName == "" {
		serverName = id
	}
	if serverHost == "" {
		serverHost = serverName
	}
	if payload.TrafficAlertPercent > 0 {
		trafficAlertPercent = payload.TrafficAlertPercent
	}
	trafficAlertPercent = normalizeTrafficAlertPercent(trafficAlertPercent)

	trafficUsedBytes := int64(0)
	if cachedInfo.Valid && cachedInfo.String != "" {
		var cached map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfo.String), &cached); err == nil {
			trafficUsedBytes = trafficUsedForCycle(trafficUsedBytesFromMetrics(cached, trafficLimitMode), trafficCycleType, trafficCycleBaseline)
		}
	}
	if trafficLimitBytes <= 0 {
		trafficLimitBytes = trafficUsedBytes
		if trafficLimitBytes <= 0 {
			trafficLimitBytes = 1
		}
	}
	trafficPercent := 0.0
	if trafficLimitBytes > 0 {
		trafficPercent = (float64(trafficUsedBytes) / float64(trafficLimitBytes)) * 100
	}

	eventData := map[string]interface{}{
		"serverId":            id,
		"serverName":          serverName,
		"host":                serverHost,
		"hostname":            serverName,
		"eventType":           "traffic_high",
		"traffic_used_bytes":  trafficUsedBytes,
		"traffic_limit_bytes": trafficLimitBytes,
		"traffic_limit_mode":  normalizeTrafficLimitMode(trafficLimitMode),
		"traffic_percent":     fmt.Sprintf("%.2f", trafficPercent),
		"traffic_used":        formatBytes(trafficUsedBytes),
		"traffic_limit":       formatBytes(trafficLimitBytes),
		"threshold":           fmt.Sprintf("%.2f%%", trafficAlertPercent),
		"test":                true,
	}
	if err := s.notifier.Trigger(r.Context(), "server", "traffic_high", eventData); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"sent": true})
}

type accountDeleteDependencies struct {
	Nodes       int
	Runtimes    int
	Tunnels     int
	StatusPages int
}

func (d accountDeleteDependencies) responseData() map[string]int {
	return map[string]int{
		"nodes":        d.Nodes,
		"runtimes":     d.Runtimes,
		"tunnels":      d.Tunnels,
		"status_pages": d.StatusPages,
	}
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var name, lastCheckStatus, lastCheckTime string
	if err := db.QueryRowContext(r.Context(), `SELECT name,COALESCE(last_check_status,''),COALESCE(last_check_time,'') FROM server_accounts WHERE id=?`, id).Scan(&name, &lastCheckStatus, &lastCheckTime); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "服务器不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	dependencies, err := loadAccountDeleteDependencies(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	forceRequested := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	connection, online := s.registry.Get(id)
	agentObserved := lastCheckTime != "" && !strings.EqualFold(lastCheckStatus, "uninstalled")
	requiresHostCleanup := online || agentObserved || dependencies.Nodes > 0 || dependencies.Runtimes > 0 || dependencies.Tunnels > 0
	forceDetach := forceRequested
	if requiresHostCleanup {
		if !online {
			if !forceRequested {
				response.JSON(w, http.StatusConflict, map[string]interface{}{
					"success": false,
					"error":   "Agent 离线，无法确认主机上的节点、代理程序和 Agent 已卸载",
					"data": map[string]interface{}{
						"can_force_delete": true,
						"server_id":        id,
						"dependencies":     dependencies.responseData(),
					},
				})
				return
			}
		} else {
			capabilities := connection.GetCapabilities()
			missing := []string{}
			if dependencies.Nodes > 0 && !capabilities["proxy_runtime_v1"] {
				missing = append(missing, "节点卸载")
			}
			if (dependencies.Runtimes > 0 || dependencies.Nodes > 0) && !capabilities["proxy_runtime_lifecycle_v2"] {
				missing = append(missing, "代理程序卸载")
			}
			if dependencies.Tunnels > 0 && !capabilities["cloudflared_runtime_v1"] {
				missing = append(missing, "Tunnel 卸载")
			}
			if !capabilities["self_uninstall_v1"] {
				missing = append(missing, "Agent 自卸载")
			}
			if len(missing) == 0 {
				// A force query must not bypass verified cleanup when a capable
				// Agent is online. Force is reserved for genuine recovery paths.
				forceDetach = false
			} else if !forceRequested {
				response.JSON(w, http.StatusConflict, map[string]interface{}{
					"success": false,
					"error":   "Agent 版本过旧，缺少安全级联删除能力：" + strings.Join(missing, "、") + "；请先升级 Agent",
					"data": map[string]interface{}{
						"can_force_delete": true,
						"server_id":        id,
						"dependencies":     dependencies.responseData(),
					},
				})
				return
			}
		}
	}

	task, ok := s.createExclusiveProxyTask(w, id, "server.delete", "cascade-delete")
	if !ok {
		return
	}
	if _, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET enabled=0,publishable=0,apply_status='removing',updated_at=datetime('now') WHERE server_id=?`, id); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusAccepted, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"task_id":      task.ID,
			"status":       task.Status,
			"server_id":    id,
			"dependencies": dependencies.responseData(),
		},
	})
	go s.runAccountCascadeDelete(task.ID, id, name, dependencies, requiresHostCleanup, forceDetach)
}

func loadAccountDeleteDependencies(ctx context.Context, db *sql.DB, serverID string) (accountDeleteDependencies, error) {
	var dependencies accountDeleteDependencies
	for _, check := range []struct {
		query string
		value *int
	}{
		{`SELECT COUNT(*) FROM managed_proxy_nodes WHERE server_id=?`, &dependencies.Nodes},
		{`SELECT COUNT(*) FROM managed_proxy_runtimes WHERE server_id=?`, &dependencies.Runtimes},
		{`SELECT COUNT(*) FROM managed_proxy_tunnels WHERE server_id=?`, &dependencies.Tunnels},
	} {
		if err := db.QueryRowContext(ctx, check.query, serverID).Scan(check.value); err != nil {
			return dependencies, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(server_ids_json,'[]') FROM server_status_pages`)
	if err != nil {
		return dependencies, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return dependencies, err
		}
		var serverIDs []string
		if err := json.Unmarshal([]byte(raw), &serverIDs); err != nil {
			rows.Close()
			return dependencies, fmt.Errorf("decode status page server references: %w", err)
		}
		for _, candidate := range serverIDs {
			if candidate == serverID {
				dependencies.StatusPages++
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dependencies, err
	}
	return dependencies, rows.Close()
}

func (s *Service) runAccountCascadeDelete(taskID, serverID, serverName string, dependencies accountDeleteDependencies, requiresHostCleanup, forceDetach bool) {
	ctx, cancel := context.WithTimeout(s.backgroundCtx, 10*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()
	progress := func(value int, stage, message string) {
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{
			"stage": stage, "message": message, "server_id": serverID, "server_name": serverName,
		})
	}
	fail := func(stage string, cause error) {
		progress(100, stage, cause.Error())
		s.taskRegistry.Fail(taskID, cause.Error())
	}

	progress(5, "unpublish", "已停止发布该主机的全部节点")
	if requiresHostCleanup && !forceDetach {
		if _, online := s.registry.Get(serverID); !online {
			fail("agent_offline", errors.New("Agent 在级联删除期间离线；节点已停止发布，请重试或选择强制移除"))
			return
		}
		progress(15, "remove_nodes", "正在清理主机上的节点服务与防火墙规则")
		if err := s.removeAccountManagedProxyResources(ctx, db, serverID, dependencies); err != nil {
			fail("remove_host_resources", err)
			return
		}
	}

	progress(58, "remove_cloudflare", "正在清理 Tunnel DNS 与 Cloudflare 资源")
	if err := s.removeAccountManagedTunnelControlPlane(ctx, db, serverID); err != nil {
		fail("remove_cloudflare", err)
		return
	}

	if requiresHostCleanup && !forceDetach {
		progress(72, "uninstall_agent", "正在卸载主机 Agent")
		if _, err := s.uninstallAgentAndWait(ctx, serverID); err != nil {
			fail("uninstall_agent", err)
			return
		}
		progress(82, "agent_uninstalled", "Agent 已卸载并断开连接")
	}

	progress(86, "delete_records", "正在删除主机及全部面板关联记录")
	if err := deleteAccountRecords(ctx, db, serverID); err != nil {
		fail("delete_records", err)
		return
	}
	if s.presence != nil {
		s.presence.suppress(serverID, 10*time.Minute)
		s.presence.recordDisconnect(serverID, "deleted")
	}
	s.registry.Disconnect(serverID)
	if s.metricsHub != nil {
		s.metricsHub.BroadcastServerStatus(serverID, "offline", false)
	}
	message := "主机、节点、代理程序、Agent 与全部面板关联资源已删除"
	if forceDetach {
		message = "主机与全部面板关联资源已删除；Agent 离线或版本过旧，主机本地可能仍有残留"
	}
	s.taskRegistry.Complete(taskID, message)
}

func (s *Service) removeAccountManagedProxyResources(ctx context.Context, db *sql.DB, serverID string, dependencies accountDeleteDependencies) error {
	rows, err := db.QueryContext(ctx, `SELECT id,runtime,revision,assigned_port,apply_status FROM managed_proxy_nodes WHERE server_id=? ORDER BY created_at`, serverID)
	if err != nil {
		return err
	}
	type nodeState struct {
		ID, Runtime, ApplyStatus string
		Revision                 int64
		AssignedPort             int
	}
	nodes := []nodeState{}
	for rows.Next() {
		var node nodeState
		if err := rows.Scan(&node.ID, &node.Runtime, &node.Revision, &node.AssignedPort, &node.ApplyStatus); err != nil {
			rows.Close()
			return err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, node := range nodes {
		if node.AssignedPort <= 0 && node.ApplyStatus != "running" && node.ApplyStatus != "removing" {
			continue
		}
		release, ok := managedProxyRuntime(node.Runtime)
		if !ok {
			return fmt.Errorf("node %s uses an unpinned proxy runtime", node.ID)
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"node_id": node.ID, "revision": node.Revision + 1, "runtime": release.Runtime,
			"runtime_version": release.Version, "asset_url_amd64": release.AMD64URL,
			"asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL,
			"asset_sha256_arm64": release.ARM64SHA256, "config": "{}", "remove": true,
			"asset_format": release.AssetFormat,
			"port_min":     45654, "port_max": 55654,
		})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove node %s: %w", node.ID, err)
		}
	}
	if dependencies.Tunnels > 0 {
		payload, _ := json.Marshal(cloudflaredTaskPayload("remove", ""))
		if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove cloudflared: %w", err)
		}
	}
	if dependencies.Runtimes > 0 || len(nodes) > 0 {
		release, ok := managedProxyRuntime("sing-box")
		if !ok {
			return errors.New("managed proxy runtime is not pinned")
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"operation": "remove_runtime", "node_id": "runtime-" + serverID, "revision": 1,
			"runtime": release.Runtime, "runtime_version": release.Version,
			"asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256,
			"asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256,
			"asset_format": release.AssetFormat,
			"config":       `{}`, "enabled": false, "port_min": 45654, "port_max": 55654, "transport": "tcp",
		})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			return fmt.Errorf("remove sing-box runtime: %w", err)
		}
	}
	return nil
}

func (s *Service) removeAccountManagedTunnelControlPlane(ctx context.Context, db *sql.DB, serverID string) error {
	var accountID, zoneID, tunnelID, dnsRecordID string
	err := db.QueryRowContext(ctx, `SELECT account_id,zone_id,tunnel_id,dns_record_id FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&accountID, &zoneID, &tunnelID, &dnsRecordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if (dnsRecordID != "" || tunnelID != "") && s.cloudflare == nil {
		return errors.New("Cloudflare Tunnel 管理器不可用，无法安全删除远端资源")
	}
	if dnsRecordID != "" {
		if err := s.cloudflare.DeleteManagedTunnelDNS(ctx, accountID, zoneID, dnsRecordID); err != nil {
			return fmt.Errorf("delete Tunnel DNS record: %w", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET dns_record_id='',updated_at=datetime('now') WHERE server_id=?`, serverID); err != nil {
			return err
		}
	}
	if tunnelID != "" {
		if err := s.cloudflare.DeleteManagedTunnel(ctx, accountID, tunnelID); err != nil {
			return fmt.Errorf("delete Cloudflare Tunnel: %w", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET tunnel_id='',token_encrypted='',updated_at=datetime('now') WHERE server_id=?`, serverID); err != nil {
			return err
		}
	}
	return nil
}

func deleteAccountRecords(ctx context.Context, db *sql.DB, serverID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := removeServerFromStatusPages(ctx, tx, serverID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE source='internal' AND node_id IN (SELECT id FROM managed_proxy_nodes WHERE server_id=?)`, serverID); err != nil && !isMissingTableError(err) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_runtime_reconcile WHERE node_id IN (SELECT id FROM managed_proxy_nodes WHERE server_id=?)`, serverID); err != nil && !isMissingTableError(err) {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM managed_proxy_nodes WHERE server_id=?`,
		`DELETE FROM managed_proxy_runtimes WHERE server_id=?`,
		`DELETE FROM managed_proxy_tunnels WHERE server_id=?`,
		`DELETE FROM server_monitor_logs WHERE server_id=?`,
		`DELETE FROM server_metrics_history WHERE server_id=?`,
		`DELETE FROM server_network_quality_samples WHERE server_id=?`,
		`DELETE FROM docker_stacks WHERE server_id=?`,
		`DELETE FROM server_agent_credentials WHERE server_id=?`,
		`DELETE FROM server_proxy_desired_state WHERE server_id=?`,
		`DELETE FROM server_proxy_traffic_reports WHERE server_id=?`,
		`DELETE FROM subscription_usage_reports WHERE server_id=?`,
		`DELETE FROM subscription_usage_report_keys WHERE server_id=?`,
		`DELETE FROM subscription_usage_hourly WHERE server_id=?`,
		`UPDATE server_command_history SET server_id=NULL WHERE server_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, serverID); err != nil && !isMissingTableError(err) {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM server_accounts WHERE id=?`, serverID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	committed = true
	return tx.Commit()
}

func removeServerFromStatusPages(ctx context.Context, tx *sql.Tx, serverID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(server_ids_json,'[]') FROM server_status_pages`)
	if err != nil {
		return err
	}
	type update struct {
		ID   int64
		JSON string
	}
	updates := []update{}
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var serverIDs []string
		if err := json.Unmarshal([]byte(raw), &serverIDs); err != nil {
			rows.Close()
			return fmt.Errorf("decode status page %d server references: %w", id, err)
		}
		filtered := make([]string, 0, len(serverIDs))
		changed := false
		for _, candidate := range serverIDs {
			if candidate == serverID {
				changed = true
				continue
			}
			filtered = append(filtered, candidate)
		}
		if changed {
			encoded, err := json.Marshal(filtered)
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, update{ID: id, JSON: string(encoded)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE server_status_pages SET server_ids_json=?,updated_at=datetime('now') WHERE id=?`, item.JSON, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT name, host, port, username, auth_type, password, private_key, passphrase, tags, description, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end FROM server_accounts ORDER BY order_index ASC")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var exportList []map[string]interface{}
	for rows.Next() {
		var name, host, username, authType, tagsStr string
		var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
		var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
		var port int
		var trafficLimitBytes int64
		var trafficLimitMode string
		var trafficAlertEnabled int
		var trafficAlertPercent float64
		var trafficCycleDay int
		err := rows.Scan(&name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		exportList = append(exportList, map[string]interface{}{
			"name":                  name,
			"host":                  host,
			"port":                  port,
			"username":              username,
			"auth_type":             authType,
			"password":              s.decryptField(password),
			"private_key":           s.decryptField(privateKey),
			"passphrase":            s.decryptField(passphrase),
			"tags":                  parseJSONTags(tagsStr),
			"description":           nullStringVal(description),
			"country":               nullStringVal(country),
			"resolved_country":      nullStringVal(resolvedCountry),
			"starts_at":             nullStringVal(startsAt),
			"expires_at":            nullStringVal(expiresAt),
			"traffic_limit_bytes":   trafficLimitBytes,
			"traffic_limit_mode":    normalizeTrafficLimitMode(trafficLimitMode),
			"traffic_alert_enabled": trafficAlertEnabled != 0,
			"traffic_alert_percent": normalizeTrafficAlertPercent(trafficAlertPercent),
			"traffic_cycle_type":    normalizeTrafficCycleType(trafficCycleType.String),
			"traffic_cycle_day":     normalizeTrafficCycleDay(trafficCycleDay),
			"traffic_cycle_start":   nullStringVal(trafficCycleStart),
			"traffic_cycle_end":     nullStringVal(trafficCycleEnd),
		})
	}
	if exportList == nil {
		exportList = []map[string]interface{}{}
	}
	response.OK(w, exportList)
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Servers []map[string]interface{} `json:"servers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var results []map[string]interface{}
	successCount := 0
	failedCount := 0

	for _, item := range req.Servers {
		name, _ := item["name"].(string)
		host, _ := item["host"].(string)
		portVal, _ := item["port"]
		username, _ := item["username"].(string)
		authType, _ := item["auth_type"].(string)
		password, _ := item["password"].(string)
		privateKey, _ := item["private_key"].(string)
		passphrase, _ := item["passphrase"].(string)
		tags := item["tags"]
		description, _ := item["description"].(string)
		country, _ := item["country"].(string)
		startsAt, _ := item["starts_at"].(string)
		expiresAt, _ := item["expires_at"].(string)
		trafficLimitBytes := normalizeTrafficLimitBytes(getInt64Val(item, "traffic_limit_bytes", 0))
		trafficLimitMode := normalizeTrafficLimitMode(getStringVal(item, "traffic_limit_mode", "total"))
		trafficAlertEnabled := getBoolVal(item, "traffic_alert_enabled", false)
		trafficAlertPercent := normalizeTrafficAlertPercent(getFloatVal(item, "traffic_alert_percent", 100))
		trafficCycleType := normalizeTrafficCycleType(getStringVal(item, "traffic_cycle_type", "none"))
		trafficCycleDay := normalizeTrafficCycleDay(getIntVal(item, "traffic_cycle_day", 1))
		trafficCycleStart := getStringVal(item, "traffic_cycle_start", "")
		trafficCycleEnd := getStringVal(item, "traffic_cycle_end", "")

		port := 22
		if portVal != nil {
			if f, err := toFloat(portVal); err == nil {
				port = int(f)
			}
		}

		if name == "" {
			results = append(results, map[string]interface{}{"success": false, "error": "Missing name", "data": item})
			failedCount++
			continue
		}

		id := uuid.NewString()
		now := time.Now().Format(time.RFC3339)

		var maxOrder sql.NullInt64
		_ = db.QueryRowContext(r.Context(), "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
		orderIndex := int(maxOrder.Int64) + 1

		encPassword := s.encryptField(password)
		encPrivateKey := s.encryptField(privateKey)
		encPassphrase := s.encryptField(passphrase)

		_, err := db.ExecContext(r.Context(), `
			INSERT INTO server_accounts (
				id, name, host, port, username, auth_type, password, private_key, passphrase, status, tags, description, monitor_mode, country, resolved_country, starts_at, expires_at, traffic_limit_bytes, traffic_limit_mode, traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, order_index, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, name, host, port, coalesceStr(username, "agent"), coalesceStr(authType, "password"),
			encPassword, encPrivateKey, encPassphrase, "unknown", SerializeList(tags), description, "agent", country, nil, nullStr(startsAt), nullStr(expiresAt), trafficLimitBytes, trafficLimitMode, boolToInt(trafficAlertEnabled), trafficAlertPercent, trafficCycleType, trafficCycleDay, nullStr(trafficCycleStart), nullStr(trafficCycleEnd), orderIndex, now, now,
		)

		if err != nil {
			results = append(results, map[string]interface{}{"success": false, "error": err.Error(), "data": item})
			failedCount++
		} else {
			acc, _ := s.queryAccountByID(r.Context(), db, id)
			results = append(results, map[string]interface{}{"success": true, "data": acc})
			successCount++
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("导入完成: 成功 %d, 失败 %d", successCount, failedCount),
		"results": results,
	})
}

func (s *Service) reorderAccounts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		OrderData []struct {
			ID         string `json:"id"`
			OrderIndex int    `json:"order_index"`
		} `json:"orderData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(r.Context(), "UPDATE server_accounts SET order_index = ? WHERE id = ?")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer stmt.Close()

	for _, item := range req.OrderData {
		if _, err := stmt.ExecContext(r.Context(), item.OrderIndex, item.ID); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "重排成功",
	})
}

// ==========================================
// SCANNERS & UTILS
// ==========================================

func (s *Service) scanAccount(row *sql.Rows) (map[string]interface{}, error) {
	var id, name, host, username, authType, status, monitorMode, createdAt, updatedAt string
	var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
	var lastCheckTime, lastCheckStatus, cachedInfo sql.NullString
	var responseTime sql.NullInt64
	var port, orderIndex int
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertEnabled int
	var trafficAlertPercent float64
	var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
	var trafficCycleDay int
	var trafficCycleBaseline int64
	var tagsStr string

	err := row.Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd, &trafficCycleBaseline, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
		trafficLimitBytes, trafficLimitMode, trafficAlertEnabled != 0, trafficAlertPercent,
		trafficCycleType, trafficCycleDay, trafficCycleStart, trafficCycleEnd, trafficCycleBaseline,
	), nil
}

func (s *Service) queryAccountByID(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	var name, host, username, authType, status, monitorMode, createdAt, updatedAt string
	var password, privateKey, passphrase, description, country, resolvedCountry, startsAt, expiresAt sql.NullString
	var lastCheckTime, lastCheckStatus, cachedInfo sql.NullString
	var responseTime sql.NullInt64
	var port, orderIndex int
	var trafficLimitBytes int64
	var trafficLimitMode string
	var trafficAlertEnabled int
	var trafficAlertPercent float64
	var trafficCycleType, trafficCycleStart, trafficCycleEnd sql.NullString
	var trafficCycleDay int
	var trafficCycleBaseline int64
	var tagsStr string

	err := db.QueryRowContext(ctx, "SELECT id, name, host, port, username, auth_type, password, private_key, passphrase, status, monitor_mode, last_check_time, last_check_status, response_time, cached_info, tags, description, country, resolved_country, starts_at, expires_at, order_index, created_at, traffic_limit_bytes, COALESCE(traffic_limit_mode, 'total'), traffic_alert_enabled, traffic_alert_percent, traffic_cycle_type, traffic_cycle_day, traffic_cycle_start, traffic_cycle_end, traffic_cycle_baseline, updated_at FROM server_accounts WHERE id = ?", id).
		Scan(&id, &name, &host, &port, &username, &authType, &password, &privateKey, &passphrase, &status, &monitorMode, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo, &tagsStr, &description, &country, &resolvedCountry, &startsAt, &expiresAt, &orderIndex, &createdAt, &trafficLimitBytes, &trafficLimitMode, &trafficAlertEnabled, &trafficAlertPercent, &trafficCycleType, &trafficCycleDay, &trafficCycleStart, &trafficCycleEnd, &trafficCycleBaseline, &updatedAt)
	if err != nil {
		return nil, err
	}

	return s.buildAccountResponse(
		id, name, host, port, username, authType,
		password, privateKey, passphrase,
		status, monitorMode, lastCheckTime, lastCheckStatus, responseTime, cachedInfo,
		tagsStr,
		description, country, resolvedCountry, startsAt, expiresAt, orderIndex, createdAt, updatedAt,
		trafficLimitBytes, trafficLimitMode, trafficAlertEnabled != 0, trafficAlertPercent,
		trafficCycleType, trafficCycleDay, trafficCycleStart, trafficCycleEnd, trafficCycleBaseline,
	), nil
}
