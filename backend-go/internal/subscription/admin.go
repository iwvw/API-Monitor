package subscription

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listLogs(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	limit := intDefault(queryInt(r, "limit"), 200)
	rows, err := db.QueryContext(r.Context(), `SELECT id, subscription_id, public_token, ip_address, user_agent, format, success, status_code, COALESCE(error_message, ''), node_count, upload_bytes, download_bytes, total_bytes, expire_at, created_at FROM subscription_access_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, status, nodeCount int
		var subID, token, ip, ua, format, errMsg, created string
		var success int
		var up, down, total, expire int64
		_ = rows.Scan(&id, &subID, &token, &ip, &ua, &format, &success, &status, &errMsg, &nodeCount, &up, &down, &total, &expire, &created)
		items = append(items, map[string]interface{}{"id": id, "subscription_id": subID, "public_token": token, "ip_address": ip, "user_agent": ua, "format": format, "success": success == 1, "status_code": status, "error_message": errMsg, "node_count": nodeCount, "upload_bytes": up, "download_bytes": down, "total_bytes": total, "expire_at": expire, "created_at": created})
	}
	response.OK(w, items)
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	settings, err := loadSettings(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var settings Settings
	if !decodeJSON(w, r, &settings) {
		return
	}
	settings.DefaultTemplateID = firstNonEmpty(settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, settings.DefaultTemplateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err := db.ExecContext(r.Context(), `UPDATE subscription_settings SET default_template_id = ?, default_rate_limit_enabled = ?, default_rate_limit_per_minute = ?, default_refresh_hours = ?, geoip_enabled = ?, updated_at = datetime('now') WHERE id = 1`,
		settings.DefaultTemplateID, boolToInt(settings.DefaultRateLimitEnabled), intDefault(settings.DefaultRateLimitPerMin, defaultLimitPerMin), intDefault(settings.DefaultRefreshHours, defaultRefreshHours), boolToInt(settings.GeoIPEnabled))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) listServers(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT id, name, host, COALESCE(resolved_country, ''), COALESCE(country, ''), COALESCE(traffic_limit_bytes, 0), COALESCE(cached_info, '{}'), COALESCE(status,'unknown'), COALESCE(last_check_time,'') FROM server_accounts ORDER BY order_index ASC, created_at DESC`)
	if err != nil {
		response.OK(w, []map[string]interface{}{})
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, name, host, location, country, cached, status, lastSeen string
		var limit int64
		if err := rows.Scan(&id, &name, &host, &location, &country, &limit, &cached, &status, &lastSeen); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		info := map[string]interface{}{}
		_ = json.Unmarshal([]byte(cached), &info)
		platform := firstNonEmpty(jsonString(info, "platform"), jsonString(info, "os"))
		platformVersion := firstNonEmpty(jsonString(info, "platform_version"), jsonString(info, "platformVersion"))
		if !isLinuxPlatform(platform, platformVersion) {
			continue
		}
		items = append(items, map[string]interface{}{"id": id, "name": name, "host": host, "location": firstNonEmpty(jsonString(info, "location"), jsonString(info, "region"), location), "country_code": firstNonEmpty(jsonString(info, "country_code"), jsonString(info, "country"), country, location), "uptime": info["uptime"], "traffic_limit_bytes": limit, "status": status, "last_seen": lastSeen, "platform": platform, "platform_version": platformVersion, "agent_version": firstNonEmpty(jsonString(info, "agent_version"), jsonString(info, "version"))})
	}
	response.OK(w, items)
}

func (s *Service) exportAll(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	subs, _ := loadSubscriptions(r.Context(), db, "")
	nodes, _ := loadNodes(r.Context(), db, "", true)
	templates, _ := loadTemplates(r.Context(), db)
	response.OK(w, map[string]interface{}{"type": "api-monitor-subscription-backup", "version": 1, "exportedAt": time.Now().UTC().Format(time.RFC3339), "subscriptions": subs, "nodes": nodes, "templates": templates})
}
