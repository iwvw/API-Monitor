package uptime

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) summary(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitors, err := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats := map[string]int{"total": len(monitors), "up": 0, "down": 0, "pending": 0, "paused": 0, "unknown": 0}
	for _, monitor := range monitors {
		if !boolValue(monitor["active"], true) {
			stats["paused"]++
			continue
		}
		state, _ := loadState(r.Context(), db, int64Value(monitor["id"], 0))
		switch stateText(state["state"]) {
		case stateUp:
			stats["up"]++
		case stateDown:
			stats["down"]++
		case statePendingDown, statePendingUp:
			stats["pending"]++
		default:
			stats["unknown"]++
		}
	}
	response.OK(w, stats)
}

func (s *Service) monitors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitors, err := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors ORDER BY created_at DESC`)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, monitor := range monitors {
			last, _ := getLastHeartbeat(r.Context(), db, int64Value(monitor["id"], 0))
			monitor["lastHeartbeat"] = last
			st, _ := loadState(r.Context(), db, int64Value(monitor["id"], 0))
			monitor["state"] = stringValue(st["state"], stateUnknown)
			monitor["sslExpiry"] = st["sslExpiry"]
		}
		response.JSON(w, http.StatusOK, monitors)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitor, err := s.createMonitor(r.Context(), db, payload)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if boolValue(monitor["active"], true) {
			s.startMonitor(monitor)
		}
		response.JSON(w, http.StatusOK, monitor)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) monitorByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		monitor, ok, err := s.updateMonitor(r.Context(), db, id, payload)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if !ok {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
			return
		}
		if boolValue(monitor["active"], true) {
			s.startMonitor(monitor)
		} else {
			s.stopMonitor(id)
		}
		response.JSON(w, http.StatusOK, monitor)
	case http.MethodDelete:
		s.stopMonitor(id)
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed, _ := result.RowsAffected()
		if changed == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) createMonitor(ctx context.Context, db *sql.DB, data map[string]interface{}) (map[string]interface{}, error) {
	name := strings.TrimSpace(stringValue(data["name"], ""))
	if name == "" {
		return nil, errors.New("Name is required")
	}
	typ := stringValue(data["type"], "http")
	urlValue := nullableString(data["url"])
	hostnameValue := nullableString(data["hostname"])
	portValue := nullableInt(data["port"])
	var existingID int64
	err := db.QueryRowContext(ctx, `
		SELECT id FROM uptime_monitors
		WHERE name = ? AND type = ? AND COALESCE(url, '') = ? AND COALESCE(hostname, '') = ? AND COALESCE(port, 0) = ?
	`, name, typ, stringPtrValue(urlValue), stringPtrValue(hostnameValue), intPtrValue(portValue)).Scan(&existingID)
	if err == nil && existingID > 0 {
		monitor, _, loadErr := loadMonitor(ctx, db, existingID)
		return monitor, loadErr
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	configMap := normalizeMonitorConfig(data, nil)
	configRaw := jsonOrNull(configMap)
	notificationChannels := jsonOrDefault(data["notificationChannels"], "[]")
	tags := jsonOrDefault(data["tags"], "[]")
	pushToken := stringValue(firstNonNil(data["pushToken"], data["push_token"]), "")
	if typ == "push" && pushToken == "" {
		pushToken = generateToken()
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_monitors (
			name, type, url, hostname, port, interval, timeout,
			confirm_count, active, method, headers, body, ignore_tls,
			accepted_status_codes, keyword, dns_resolve_type, dns_resolve_server,
			retry_interval, resend_interval, up_confirm_count, down_confirm_count,
			config_json, auth_json_encrypted, push_token, push_grace_seconds,
			expiry_notification, notification_channels, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		name,
		typ,
		urlValue,
		hostnameValue,
		portValue,
		intValue(data["interval"], defaultIntervalSeconds),
		intValue(data["timeout"], defaultTimeoutSeconds),
		intValue(firstNonNil(data["retries"], data["confirmCount"], data["confirm_count"]), defaultConfirmCount),
		boolIntValue(data["active"], true),
		stringValue(data["method"], "GET"),
		nullableString(data["headers"]),
		nullableString(data["body"]),
		boolIntValue(firstNonNil(data["ignoreTls"], data["ignore_tls"]), false),
		nullableString(data["accepted_status_codes"]),
		nullableString(data["keyword"]),
		stringValue(firstNonNil(data["dns_resolve_type"], data["dnsResolveType"]), "A"),
		nullableString(firstNonNil(data["dns_resolve_server"], data["dnsResolveServer"])),
		intValue(firstNonNil(data["retryInterval"], data["retry_interval"]), 30),
		intValue(firstNonNil(data["resendInterval"], data["resend_interval"]), 0),
		nullableInt(firstNonNil(data["upConfirmCount"], data["up_confirm_count"])),
		nullableInt(firstNonNil(data["downConfirmCount"], data["down_confirm_count"])),
		configRaw,
		nullableString(data["auth_json_encrypted"]),
		nullableString(pushToken),
		intValue(firstNonNil(data["pushGraceSeconds"], data["push_grace_seconds"], configMap["graceSeconds"]), 120),
		intValue(data["expiryNotification"], 7),
		notificationChannels,
		tags,
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	monitor, _, err := loadMonitor(ctx, db, id)
	return monitor, err
}

func (s *Service) updateMonitor(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (map[string]interface{}, bool, error) {
	existing, ok, err := loadMonitor(ctx, db, id)
	if err != nil || !ok {
		return nil, ok, err
	}
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	stringFields := map[string]string{
		"name": "name", "type": "type", "url": "url", "hostname": "hostname", "method": "method",
		"headers": "headers", "body": "body", "accepted_status_codes": "accepted_status_codes",
		"keyword": "keyword", "dns_resolve_type": "dns_resolve_type", "dnsResolveType": "dns_resolve_type",
		"dns_resolve_server": "dns_resolve_server", "dnsResolveServer": "dns_resolve_server",
		"pushToken": "push_token", "push_token": "push_token", "auth_json_encrypted": "auth_json_encrypted",
	}
	for key, column := range stringFields {
		if value, exists := data[key]; exists {
			add(column, nullableString(value))
		}
	}
	intFields := map[string]string{
		"port": "port", "interval": "interval", "timeout": "timeout", "retryInterval": "retry_interval",
		"retry_interval": "retry_interval", "resendInterval": "resend_interval", "resend_interval": "resend_interval",
		"upConfirmCount": "up_confirm_count", "up_confirm_count": "up_confirm_count",
		"downConfirmCount": "down_confirm_count", "down_confirm_count": "down_confirm_count",
		"expiryNotification": "expiry_notification", "pushGraceSeconds": "push_grace_seconds",
		"push_grace_seconds": "push_grace_seconds",
	}
	for key, column := range intFields {
		if value, exists := data[key]; exists {
			add(column, nullableInt(value))
		}
	}
	if value, exists := data["active"]; exists {
		add("active", boolIntValue(value, true))
	}
	if value, exists := firstExisting(data, "ignoreTls", "ignore_tls"); exists {
		add("ignore_tls", boolIntValue(value, false))
	}
	if value, exists := firstExisting(data, "retries", "confirmCount", "confirm_count"); exists {
		add("confirm_count", intValue(value, defaultConfirmCount))
	}
	if value, exists := data["notificationChannels"]; exists {
		add("notification_channels", jsonOrDefault(value, "[]"))
	}
	if value, exists := data["tags"]; exists {
		add("tags", jsonOrDefault(value, "[]"))
	}
	if shouldUpdateConfig(data) {
		existingConfig := objectValue(existing["config"])
		add("config_json", jsonOrNull(normalizeMonitorConfig(data, existingConfig)))
	}
	if stringValue(firstNonNil(data["type"], existing["type"]), "http") == "push" &&
		stringValue(firstNonNil(data["pushToken"], data["push_token"], existing["pushToken"]), "") == "" {
		add("push_token", generateToken())
	}
	if len(fields) == 0 {
		return existing, true, nil
	}
	fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
	values = append(values, id)
	if _, err := db.ExecContext(ctx, `UPDATE uptime_monitors SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
		return nil, true, err
	}
	monitor, ok, err := loadMonitor(ctx, db, id)
	return monitor, ok, err
}

func loadMonitor(ctx context.Context, db *sql.DB, id int64) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_monitors WHERE id = ?`, id)
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

func loadMonitors(ctx context.Context, db *sql.DB, query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	monitors := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, normalizeMonitor(row))
	}
	return monitors, rows.Err()
}

func normalizeMonitor(row map[string]interface{}) map[string]interface{} {
	out := copyMap(row)
	confirm := intValue(row["confirm_count"], defaultConfirmCount)
	config := parseJSONMap(row["config_json"])
	out["active"] = boolValue(row["active"], true)
	out["ignoreTls"] = boolValue(row["ignore_tls"], false)
	out["confirmCount"] = confirm
	out["retries"] = confirm
	out["keyword"] = stringValue(row["keyword"], "")
	out["dns_resolve_type"] = stringValue(row["dns_resolve_type"], "A")
	out["dns_resolve_server"] = stringValue(row["dns_resolve_server"], "")
	out["retryInterval"] = intValue(row["retry_interval"], 30)
	out["resendInterval"] = intValue(row["resend_interval"], 0)
	out["upConfirmCount"] = intValue(row["up_confirm_count"], confirm)
	out["downConfirmCount"] = intValue(row["down_confirm_count"], confirm)
	out["config"] = config
	out["pushToken"] = stringValue(row["push_token"], "")
	out["pushGraceSeconds"] = intValue(row["push_grace_seconds"], 120)
	out["expiryNotification"] = intValue(row["expiry_notification"], 7)
	out["notificationChannels"] = parseJSONArray(row["notification_channels"])
	out["tags"] = parseJSONArray(row["tags"])
	out["jsonQueryPath"] = stringValue(config["jsonQueryPath"], "")
	out["jsonQueryOperator"] = stringValue(config["jsonQueryOperator"], "equals")
	out["jsonExpectedValue"] = stringValue(config["jsonExpectedValue"], "")
	return out
}

func (s *Service) monitorHistory(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	limit := intValue(r.URL.Query().Get("limit"), 60)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	history, err := getHistory(r.Context(), db, id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, history)
}

func getHistory(ctx context.Context, db *sql.DB, monitorID int64, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 500 {
		limit = 60
	}
	rows, err := db.QueryContext(ctx, `
		SELECT status, ping, msg, created_at as time
		FROM uptime_heartbeats
		WHERE monitor_id = ?
		ORDER BY created_at DESC LIMIT ?
	`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		normalizeHeartbeatTime(row)
		items = append(items, row)
	}
	return items, rows.Err()
}

func getLastHeartbeat(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT status, ping, msg, created_at as time
		FROM uptime_heartbeats
		WHERE monitor_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		return nil, err
	}
	normalizeHeartbeatTime(row)
	return row, nil
}

func normalizeHeartbeatTime(row map[string]interface{}) {
	parsed := parseTimeFallbackAny(firstNonNil(row["time"], row["created_at"]), time.Time{})
	if parsed.IsZero() {
		return
	}
	row["time"] = parsed.UTC().Format(time.RFC3339Nano)
}

func saveHeartbeat(ctx context.Context, db *sql.DB, monitorID int64, beat map[string]interface{}) error {
	details := jsonOrNull(beat["details"])
	_, err := db.ExecContext(ctx, `
		INSERT INTO uptime_heartbeats (
			monitor_id, status, state, ping, duration_ms, status_code,
			error_code, details_json, maintenance, probe_id, msg, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		monitorID,
		intValue(beat["status"], 0),
		stringValue(beat["state"], stateDown),
		intValue(beat["ping"], 0),
		intValue(beat["durationMs"], intValue(beat["ping"], 0)),
		nullableInt(beat["statusCode"]),
		nullableString(beat["errorCode"]),
		details,
		boolIntValue(beat["maintenance"], false),
		nullableString(beat["probeId"]),
		stringValue(beat["msg"], ""),
		stringValue(beat["time"], time.Now().UTC().Format(time.RFC3339)),
	)
	return err
}
