package uptime

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

func (s *Service) maintenance(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		items, err := listMaintenance(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, items)
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
		item, err := createMaintenance(r.Context(), db, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		response.OK(w, item)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) maintenanceByID(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid maintenance id")
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
		item, ok, err := updateMaintenance(r.Context(), db, id, payload)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ok {
			response.Error(w, http.StatusNotFound, "Not found")
			return
		}
		response.OK(w, item)
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM uptime_maintenance_windows WHERE id = ?`, id)
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

func listMaintenance(ctx context.Context, db *sql.DB) ([]maintenanceWindow, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_maintenance_windows ORDER BY created_at DESC`)
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
	items := []maintenanceWindow{}
	for _, row := range rawRows {
		item, err := parseMaintenance(ctx, db, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func createMaintenance(ctx context.Context, db *sql.DB, data map[string]interface{}) (maintenanceWindow, error) {
	title := strings.TrimSpace(stringValue(data["title"], ""))
	if title == "" {
		return maintenanceWindow{}, errors.New("title is required")
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO uptime_maintenance_windows (
			title, description, strategy, timezone, start_at, end_at, cron, recurrence_json, active
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, title, stringValue(data["description"], ""), stringValue(data["strategy"], "manual"), stringValue(data["timezone"], "UTC"),
		nullableString(firstNonNil(data["startAt"], data["start_at"])), nullableString(firstNonNil(data["endAt"], data["end_at"])),
		nullableString(data["cron"]), jsonOrNull(firstNonNil(data["recurrence"], data["recurrence_json"])), boolIntValue(data["active"], true))
	if err != nil {
		return maintenanceWindow{}, err
	}
	id, _ := result.LastInsertId()
	if err := replaceMaintenanceTargets(ctx, db, id, firstNonNil(data["targets"], data["targetIds"])); err != nil {
		return maintenanceWindow{}, err
	}
	item, _, err := getMaintenance(ctx, db, id)
	return item, err
}

func updateMaintenance(ctx context.Context, db *sql.DB, id int64, data map[string]interface{}) (maintenanceWindow, bool, error) {
	fields := []string{}
	values := []interface{}{}
	add := func(column string, value interface{}) {
		fields = append(fields, column+" = ?")
		values = append(values, value)
	}
	for key, column := range map[string]string{"title": "title", "description": "description", "strategy": "strategy", "timezone": "timezone", "cron": "cron"} {
		if value, ok := data[key]; ok {
			add(column, nullableString(value))
		}
	}
	if value, ok := firstExisting(data, "startAt", "start_at"); ok {
		add("start_at", nullableString(value))
	}
	if value, ok := firstExisting(data, "endAt", "end_at"); ok {
		add("end_at", nullableString(value))
	}
	if value, ok := data["active"]; ok {
		add("active", boolIntValue(value, true))
	}
	if value, ok := data["recurrence"]; ok {
		add("recurrence_json", jsonOrNull(value))
	}
	if len(fields) > 0 {
		fields = append(fields, "updated_at = CURRENT_TIMESTAMP")
		values = append(values, id)
		if _, err := db.ExecContext(ctx, `UPDATE uptime_maintenance_windows SET `+strings.Join(fields, ", ")+` WHERE id = ?`, values...); err != nil {
			return maintenanceWindow{}, true, err
		}
	}
	if value, ok := firstExisting(data, "targets", "targetIds"); ok {
		if err := replaceMaintenanceTargets(ctx, db, id, value); err != nil {
			return maintenanceWindow{}, true, err
		}
	}
	return getMaintenance(ctx, db, id)
}

func getMaintenance(ctx context.Context, db *sql.DB, id int64) (maintenanceWindow, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT * FROM uptime_maintenance_windows WHERE id = ?`, id)
	if err != nil {
		return maintenanceWindow{}, false, err
	}
	if !rows.Next() {
		rows.Close()
		return maintenanceWindow{}, false, rows.Err()
	}
	row, err := scanMap(rows)
	if err != nil {
		rows.Close()
		return maintenanceWindow{}, false, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return maintenanceWindow{}, false, err
	}
	rows.Close()
	item, err := parseMaintenance(ctx, db, row)
	return item, true, err
}

func parseMaintenance(ctx context.Context, db *sql.DB, row map[string]interface{}) (maintenanceWindow, error) {
	targets, err := maintenanceTargets(ctx, db, int64Value(row["id"], 0))
	if err != nil {
		return maintenanceWindow{}, err
	}
	return maintenanceWindow{
		ID:          int64Value(row["id"], 0),
		Title:       stringValue(row["title"], ""),
		Description: stringValue(row["description"], ""),
		Strategy:    stringValue(row["strategy"], "manual"),
		Timezone:    stringValue(row["timezone"], "UTC"),
		StartAt:     stringPointer(row["start_at"]),
		EndAt:       stringPointer(row["end_at"]),
		Cron:        stringPointer(row["cron"]),
		Recurrence:  parseJSONAny(row["recurrence_json"]),
		Targets:     targets,
		Active:      boolValue(row["active"], true),
		CreatedAt:   stringPointer(row["created_at"]),
		UpdatedAt:   stringPointer(row["updated_at"]),
	}, nil
}

func replaceMaintenanceTargets(ctx context.Context, db *sql.DB, maintenanceID int64, value interface{}) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM uptime_maintenance_targets WHERE maintenance_id = ?`, maintenanceID); err != nil {
		return err
	}
	targets := normalizeTargets(value)
	if len(targets) == 0 {
		targets = []map[string]interface{}{{"type": "global", "id": nil}}
	}
	for _, target := range targets {
		if _, err := db.ExecContext(ctx, `INSERT INTO uptime_maintenance_targets (maintenance_id, target_type, target_id) VALUES (?, ?, ?)`,
			maintenanceID, stringValue(target["type"], "monitor"), nullableString(target["id"])); err != nil {
			return err
		}
	}
	return nil
}

func maintenanceTargets(ctx context.Context, db *sql.DB, maintenanceID int64) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT target_type as type, target_id as id FROM uptime_maintenance_targets WHERE maintenance_id = ? ORDER BY id ASC`, maintenanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []map[string]interface{}{}
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, row)
	}
	return targets, rows.Err()
}

func activeMaintenanceForMonitor(ctx context.Context, db *sql.DB, monitorID int64) (map[string]interface{}, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	// 必须在 SQL 查询前取站点时区：维护窗口 cron 需要按站点时区求值，
	// 且 sqlite 单连接库下查询前取值可避免 rows 持有连接时的二次查询死锁。
	siteLoc := timeutil.LocationFromSettings(ctx, db)
	rows, err := db.QueryContext(ctx, `
		SELECT mw.*
		FROM uptime_maintenance_windows mw
		LEFT JOIN uptime_maintenance_targets mt ON mt.maintenance_id = mw.id
		WHERE mw.active = 1
		  AND (mw.start_at IS NULL OR mw.start_at <= ?)
		  AND (mw.end_at IS NULL OR mw.end_at >= ?)
		  AND (
			mt.id IS NULL
			OR mt.target_type = 'global'
			OR (mt.target_type = 'monitor' AND mt.target_id = ?)
		  )
		ORDER BY mw.created_at DESC
		LIMIT 1
	`, now, now, strconv.FormatInt(monitorID, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanMap(rows)
		if err != nil {
			return nil, err
		}
		active, err := maintenanceRowActive(row, time.Now(), siteLoc)
		if err != nil {
			return nil, err
		}
		// cron 型窗口未被命中时继续找下一个候选（一次性窗口恒活跃）
		if !active {
			continue
		}
		return row, nil
	}
	return nil, rows.Err()
}

// maintenanceRowActive 判断维护窗口在指定时刻是否处于生效期。
// 一次性窗口（cron 为空）：SQL 已按 start_at/end_at 过滤，直接生效；
// cron 型窗口：当前分钟必须命中 cron 表达式（按窗口自身的 timezone 列评估，
// 未配置时默认跟随站点时区，站点时区不可用时回退服务器本地时区），
// 避免「配置了 cron 却按一次性起止时间永久生效」。
func maintenanceRowActive(row map[string]interface{}, at time.Time, siteLoc *time.Location) (bool, error) {
	cronExpr := strings.TrimSpace(stringValue(row["cron"], ""))
	if cronExpr == "" {
		// 兼容 recurrence_json 内携带 cron 表达式的情况
		if rec := parseJSONAny(row["recurrence_json"]); rec != nil {
			if recMap, ok := rec.(map[string]interface{}); ok {
				cronExpr = strings.TrimSpace(stringValue(recMap["cron"], ""))
			}
		}
	}
	if cronExpr == "" {
		return true, nil
	}
	loc := siteLoc
	if loc == nil {
		loc = time.Local
	}
	if tzName := strings.TrimSpace(stringValue(row["timezone"], "")); tzName != "" {
		if loaded, err := time.LoadLocation(tzName); err == nil {
			loc = loaded
		}
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		// 非法 cron 视为不生效：绝不能变成永久生效窗口
		return false, nil
	}
	now := at.In(loc)
	next := schedule.Next(now.Add(-time.Minute))
	return !next.After(now), nil
}
