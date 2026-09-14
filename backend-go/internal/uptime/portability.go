package uptime

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) exportConfig(w http.ResponseWriter, r *http.Request) {
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
	pages, err := listStatusPages(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	maintenance, err := listMaintenance(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"type":               "api-monitor-uptime-export",
		"version":            1,
		"exportedAt":         time.Now().UTC().Format(time.RFC3339),
		"monitors":           monitors,
		"statusPages":        pages,
		"maintenanceWindows": maintenance,
	})
}

func (s *Service) importPreview(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	data := objectValue(firstNonNil(payload["data"], payload))
	if typ := stringValue(data["type"], ""); typ != "" && typ != "api-monitor-uptime-export" {
		response.Error(w, http.StatusBadRequest, "Invalid uptime export payload")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	preview, err := previewImport(r.Context(), db, data)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, preview)
}

func previewImport(ctx context.Context, db *sql.DB, payload map[string]interface{}) (map[string]interface{}, error) {
	existingMonitors, err := loadMonitors(ctx, db, `SELECT * FROM uptime_monitors`)
	if err != nil {
		return nil, err
	}
	monitorKeys := map[string]bool{}
	for _, monitor := range existingMonitors {
		monitorKeys[monitorKey(monitor)] = true
	}
	existingPages, err := listStatusPages(ctx, db)
	if err != nil {
		return nil, err
	}
	pageSlugs := map[string]bool{}
	for _, page := range existingPages {
		pageSlugs[page.Slug] = true
	}
	existingMaintenance, err := listMaintenance(ctx, db)
	if err != nil {
		return nil, err
	}
	maintenanceTitles := map[string]bool{}
	for _, item := range existingMaintenance {
		maintenanceTitles[item.Title] = true
	}
	monitors := objectSlice(payload["monitors"])
	pages := objectSlice(payload["statusPages"])
	maintenance := objectSlice(payload["maintenanceWindows"])
	return map[string]interface{}{
		"monitors": mapObjects(monitors, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if monitorKeys[monitorKey(item)] {
				action = "update"
			}
			return map[string]interface{}{"name": item["name"], "type": item["type"], "action": action}
		}),
		"statusPages": mapObjects(pages, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if pageSlugs[stringValue(item["slug"], "")] {
				action = "update"
			}
			return map[string]interface{}{"title": item["title"], "slug": item["slug"], "action": action}
		}),
		"maintenanceWindows": mapObjects(maintenance, func(item map[string]interface{}) map[string]interface{} {
			action := "create"
			if maintenanceTitles[stringValue(item["title"], "")] {
				action = "update"
			}
			return map[string]interface{}{"title": item["title"], "action": action}
		}),
		"counts": map[string]interface{}{"monitors": len(monitors), "statusPages": len(pages), "maintenanceWindows": len(maintenance)},
	}, nil
}

func (s *Service) importConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	data := objectValue(firstNonNil(payload["data"], payload))
	if typ := stringValue(data["type"], ""); typ != "" && typ != "api-monitor-uptime-export" {
		response.Error(w, http.StatusBadRequest, "Invalid uptime export payload")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitorsChanged, pagesChanged, maintenanceChanged := 0, 0, 0
	idMap := map[string]int64{}
	existing, _ := loadMonitors(r.Context(), db, `SELECT * FROM uptime_monitors`)
	existingByKey := map[string]int64{}
	for _, monitor := range existing {
		existingByKey[monitorKey(monitor)] = int64Value(monitor["id"], 0)
	}
	for _, monitor := range objectSlice(data["monitors"]) {
		oldID := stringValue(monitor["id"], "")
		if existingID, ok := existingByKey[monitorKey(monitor)]; ok {
			updated, _, err := s.updateMonitor(r.Context(), db, existingID, monitor)
			if err == nil {
				idMap[oldID] = int64Value(updated["id"], existingID)
			}
		} else {
			created, err := s.createMonitor(r.Context(), db, monitor)
			if err == nil {
				idMap[oldID] = int64Value(created["id"], 0)
				if boolValue(created["active"], true) {
					s.startMonitor(created)
				}
			}
		}
		monitorsChanged++
	}
	existingPages, _ := listStatusPages(r.Context(), db)
	pagesBySlug := map[string]int64{}
	for _, page := range existingPages {
		pagesBySlug[page.Slug] = page.ID
	}
	for _, page := range objectSlice(data["statusPages"]) {
		remapMonitorIDs(page, idMap)
		if existingID, ok := pagesBySlug[stringValue(page["slug"], "")]; ok {
			_, _, _ = updateStatusPage(r.Context(), db, existingID, page)
		} else {
			_, _ = createStatusPage(r.Context(), db, page)
		}
		pagesChanged++
	}
	existingMaintenance, _ := listMaintenance(r.Context(), db)
	maintenanceByTitle := map[string]int64{}
	for _, item := range existingMaintenance {
		maintenanceByTitle[item.Title] = item.ID
	}
	for _, item := range objectSlice(data["maintenanceWindows"]) {
		remapTargets(item, idMap)
		if existingID, ok := maintenanceByTitle[stringValue(item["title"], "")]; ok {
			_, _, _ = updateMaintenance(r.Context(), db, existingID, item)
		} else {
			_, _ = createMaintenance(r.Context(), db, item)
		}
		maintenanceChanged++
	}
	response.OK(w, map[string]interface{}{"monitorsChanged": monitorsChanged, "pagesChanged": pagesChanged, "maintenanceChanged": maintenanceChanged})
}
