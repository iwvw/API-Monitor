package uptime

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/publicpageicon"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) publicStatusPage(w http.ResponseWriter, r *http.Request, slug string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	page, ok, err := getPublicStatusPage(r.Context(), db, slug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "Not found")
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", intValue(page["cacheSeconds"], 300)))
	response.OK(w, page)
}

func (s *Service) publicStatusPageByDomain(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		domain = r.Host
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	page, ok, err := getPublicStatusPageByDomain(r.Context(), db, domain)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.OK(w, map[string]interface{}{"found": false})
		return
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", intValue(page["cacheSeconds"], 300)))
	response.OK(w, page)
}

// PublicPageIconID 返回公开状态页配置的自定义图标 ID（未设置时为空字符串），
// 供服务端 favicon 解析端点使用；lookup 为 slug 或域名。
func (s *Service) PublicPageIconID(ctx context.Context, lookup string, byDomain bool) (string, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", false, err
	}
	defer db.Close()
	arg := normalizeSlug(lookup)
	if byDomain {
		arg = normalizeStatusPageDomain(lookup)
	}
	return publicpageicon.LookupIconID(ctx, db, `uptime_status_pages`, arg, byDomain)
}

func getPublicStatusPage(ctx context.Context, db *sql.DB, slug string) (map[string]interface{}, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages WHERE slug = ? AND public = 1`, normalizeSlug(slug))
	return getPublicStatusPageFromRows(ctx, db, rows, err)
}

func getPublicStatusPageByDomain(ctx context.Context, db *sql.DB, domain string) (map[string]interface{}, bool, error) {
	normalized := normalizeStatusPageDomain(domain)
	if normalized == "" {
		return nil, false, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages WHERE lower(domain) = lower(?) AND public = 1`, normalized)
	return getPublicStatusPageFromRows(ctx, db, rows, err)
}

func getPublicStatusPageFromRows(ctx context.Context, db *sql.DB, rows *sql.Rows, err error) (map[string]interface{}, bool, error) {
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	page, err := parseStatusPage(ctx, db, row)
	if err != nil {
		return nil, false, err
	}
	out := structToMap(page)
	hideTargets := boolValue(page.Config["hideTargets"], false)
	linkMonitorNames := boolValue(page.Config["linkMonitorNames"], false)
	monitorRows, err := db.QueryContext(ctx, `
		SELECT m.id, COALESCE(spm.display_name, m.name) as name, m.type, m.url, m.hostname, m.port,
			s.state, s.last_error, s.last_ping, s.updated_at
		FROM uptime_status_page_monitors spm
		JOIN uptime_monitors m ON m.id = spm.monitor_id
		LEFT JOIN uptime_monitor_states s ON s.monitor_id = m.id
		WHERE spm.status_page_id = ?
		ORDER BY spm.order_index ASC, m.name ASC
	`, page.ID)
	if err != nil {
		return nil, false, err
	}
	rawMonitors := []map[string]interface{}{}
	for monitorRows.Next() {
		monitor, err := scanMap(monitorRows)
		if err != nil {
			monitorRows.Close()
			return nil, false, err
		}
		rawMonitors = append(rawMonitors, monitor)
	}
	if err := monitorRows.Err(); err != nil {
		monitorRows.Close()
		return nil, false, err
	}
	monitorRows.Close()
	monitors := []map[string]interface{}{}
	for _, monitor := range rawMonitors {
		rawTarget := stringValue(monitor["url"], "")
		if rawTarget == "" {
			rawTarget = strings.Trim(strings.Join([]string{stringValue(monitor["hostname"], ""), stringValue(monitor["port"], "")}, ":"), ":")
		}
		displayTarget := rawTarget
		if hideTargets {
			displayTarget = ""
		}
		targetURL := ""
		if linkMonitorNames && (strings.HasPrefix(rawTarget, "http://") || strings.HasPrefix(rawTarget, "https://")) {
			targetURL = rawTarget
		}
		uptime, _ := calculateUptime(ctx, db, int64Value(monitor["id"], 0), 1)
		uptime30d, _ := calculateUptime(ctx, db, int64Value(monitor["id"], 0), 30)
		history, _ := getPublicMonitorHistory(ctx, db, int64Value(monitor["id"], 0), 60)
		monitors = append(monitors, map[string]interface{}{
			"id":         monitor["id"],
			"name":       monitor["name"],
			"type":       monitor["type"],
			"target":     displayTarget,
			"targetUrl":  targetURL,
			"state":      stringValue(monitor["state"], stateUnknown),
			"lastError":  monitor["last_error"],
			"lastPing":   intValue(monitor["last_ping"], 0),
			"updatedAt":  monitor["updated_at"],
			"uptime24h":  uptime,
			"uptime30d":  uptime30d,
			"heartbeats": history,
		})
	}
	out["monitors"] = monitors
	return out, true, nil
}

func getPublicMonitorHistory(ctx context.Context, db *sql.DB, monitorID int64, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 120 {
		limit = 60
	}
	rows, err := db.QueryContext(ctx, `
		SELECT status, state, ping, status_code, created_at as time
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
		status := stringValue(row["state"], "")
		if status == "" {
			if intValue(row["status"], 0) == 1 {
				status = "up"
			} else {
				status = "down"
			}
		}
		items = append(items, map[string]interface{}{
			"status":     status,
			"ping":       intValue(row["ping"], 0),
			"statusCode": nullableInt(row["status_code"]),
			"time":       row["time"],
		})
	}
	return items, rows.Err()
}

func normalizeStatusPageDomain(value string) string {
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

func (s *Service) publicBadge(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	state, _ := loadState(r.Context(), db, id)
	writeBadge(w, stringValue(monitor["name"], fmt.Sprintf("Monitor %d", id)), stringValue(state["state"], stateUnknown))
}

func writeBadge(w http.ResponseWriter, label, value string) {
	colorMap := map[string]string{
		stateUp: "#16a34a", stateDown: "#dc2626", statePendingDown: "#d97706",
		statePendingUp: "#d97706", stateMaintenance: "#2563eb", statePaused: "#64748b", stateUnknown: "#64748b",
	}
	color := colorMap[value]
	if color == "" {
		color = colorMap[stateUnknown]
	}
	label = escapeSVG(label)
	value = escapeSVG(value)
	labelWidth := clamp(len(label)*7+18, 80, 220)
	valueWidth := maxInt(len(value)*7+18, 58)
	width := labelWidth + valueWidth
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s"><linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient><rect rx="3" width="%d" height="20" fill="#555"/><rect rx="3" x="%d" width="%d" height="20" fill="%s"/><path fill="%s" d="M%d 0h4v20h-4z"/><rect rx="3" width="%d" height="20" fill="url(#s)"/><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,sans-serif" font-size="11"><text x="%d" y="15">%s</text><text x="%d" y="15">%s</text></g></svg>`,
		width, label, value, width, labelWidth, valueWidth, color, color, labelWidth, width, labelWidth/2, label, labelWidth+valueWidth/2, value)
}
