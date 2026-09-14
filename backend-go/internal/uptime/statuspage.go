package uptime

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) statusPages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		pages, err := listStatusPages(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, pages)
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
		page, err := createStatusPage(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, page)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) statusPageByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid status page id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		page, ok, err := updateStatusPage(r.Context(), db, id, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "Not found")
			return
		}
		response.OK(w, page)
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_status_pages WHERE id = ?`, id)
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
}

func listStatusPages(ctx context.Context, db *sql.DB) ([]statusPage, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	rawRows := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	pages := []statusPage{}
	for _, row := range rawRows {
		page, err := parseStatusPage(ctx, db, row)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

func createStatusPage(ctx context.Context, db *sql.DB, data map[string]interface{}) (statusPage, error) {
	slug := normalizeSlug(firstNonNil(data["slug"], data["title"]))
	title := stringValue(data["title"], slug)
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_status_pages (slug, domain, title, description, theme, public, cache_seconds, config_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, slug, nullableString(normalizeStatusPageDomain(stringValue(data["domain"], ""))), title, stringValue(data["description"], ""), stringValue(data["theme"], "auto"),
		boolIntValue(data["public"], true), intValue(firstNonNil(data["cacheSeconds"], data["cache_seconds"]), 300), jsonOrNull(data["config"]))
	if err != nil {
		return statusPage{}, err
	}
	id, _ := result.LastInsertId()
	if err := replaceStatusPageMonitors(ctx, db, id, int64Slice(firstNonNil(data["monitorIds"], data["monitor_ids"]))); err != nil {
		return statusPage{}, err
	}
	page, _, err := getStatusPage(ctx, db, id)
	return page, err
}

func updateStatusPage(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (statusPage, bool, error) {
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	if value, ok := data["slug"]; ok {
		add("slug", normalizeSlug(value))
	}
	for key, column := range map[string]string{"domain": "domain", "title": "title", "description": "description", "theme": "theme"} {
		if value, ok := data[key]; ok {
			if key == "domain" {
				add(column, nullableString(normalizeStatusPageDomain(stringValue(value, ""))))
				continue
			}
			add(column, nullableString(value))
		}
	}
	if value, ok := firstExisting(data, "cacheSeconds", "cache_seconds"); ok {
		add("cache_seconds", intValue(value, 300))
	}
	if value, ok := data["public"]; ok {
		add("public", boolIntValue(value, true))
	}
	if value, ok := data["config"]; ok {
		add("config_json", jsonOrNull(value))
	}
	if len(fields) > 0 {
		fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
		values = append(values, id)
		if _, err := db.ExecContext(ctx, `UPDATE uptime_status_pages SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
			return statusPage{}, true, err
		}
	}
	if value, ok := firstExisting(data, "monitorIds", "monitor_ids"); ok {
		if err := replaceStatusPageMonitors(ctx, db, id, int64Slice(value)); err != nil {
			return statusPage{}, true, err
		}
	}
	return getStatusPage(ctx, db, id)
}

func getStatusPage(ctx context.Context, db *sql.DB, id int64) (statusPage, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_status_pages WHERE id = ?`, id)
	if err != nil {
		return statusPage{}, false, err
	}
	if !rows.Next() {
		rows.Close()
		return statusPage{}, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return statusPage{}, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return statusPage{}, false, err
	}
	rows.Close()
	page, err := parseStatusPage(ctx, db, row)
	return page, true, err
}

func parseStatusPage(ctx context.Context, db *sql.DB, row map[string]interface{}) (statusPage, error) {
	ids, err := statusPageMonitorIDs(ctx, db, int64Value(row["id"], 0))
	if err != nil {
		return statusPage{}, err
	}
	return statusPage{
		ID:           int64Value(row["id"], 0),
		Slug:         stringValue(row["slug"], ""),
		Domain:       stringPointer(row["domain"]),
		Title:        stringValue(row["title"], ""),
		Description:  stringValue(row["description"], ""),
		Theme:        stringValue(row["theme"], "auto"),
		Public:       boolValue(row["public"], true),
		CacheSeconds: intValue(row["cache_seconds"], 300),
		Config:       parseJSONMap(row["config_json"]),
		MonitorIDs:   ids,
		CreatedAt:    stringPointer(row["created_at"]),
		UpdatedAt:    stringPointer(row["updated_at"]),
	}, nil
}

func replaceStatusPageMonitors(ctx context.Context, db *sql.DB, pageID int64, ids []int64) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM uptime_status_page_monitors WHERE status_page_id = ?`, pageID); err != nil {
		return err
	}
	for index, id := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO uptime_status_page_monitors (status_page_id, monitor_id, order_index) VALUES (?, ?, ?)`, pageID, id, index); err != nil {
			return err
		}
	}
	return nil
}

func statusPageMonitorIDs(ctx context.Context, db *sql.DB, pageID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT monitor_id FROM uptime_status_page_monitors WHERE status_page_id = ? ORDER BY order_index ASC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
