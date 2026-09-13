package uptime

import (
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) cloneMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	payload, _ := readObject(r)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	clone := copyMap(monitor)
	delete(clone, "id")
	delete(clone, "pushToken")
	delete(clone, "push_token")
	clone["name"] = stringValue(payload["name"], stringValue(monitor["name"], "Monitor")+" Copy")
	clone["active"] = false
	created, err := s.createMonitor(r.Context(), db, clone)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, created)
}

func (s *Service) testMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, _ := parseID(idText)
	payload, _ := readObject(r)
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor := map[string]interface{}{}
	if id > 0 {
		if stored, ok, err := loadMonitor(r.Context(), db, id); err == nil && ok {
			monitor = stored
		}
	}
	for key, value := range payload {
		monitor[key] = value
	}
	if len(monitor) == 0 || stringValue(monitor["type"], "") == "" {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	started := time.Now()
	result, err := s.probe(r.Context(), db, normalizeMonitor(monitor))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	payloadOut := map[string]interface{}{
		"ok":         result.OK,
		"status":     result.Status,
		"latencyMs":  result.LatencyMS,
		"message":    result.Message,
		"durationMs": time.Since(started).Milliseconds(),
	}
	if result.StatusCode != nil {
		payloadOut["statusCode"] = *result.StatusCode
	}
	if result.Details != nil {
		payloadOut["details"] = result.Details
	}
	response.OK(w, payloadOut)
}

func (s *Service) checkNow(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	beat, err := s.check(r.Context(), db, monitor)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.OK(w, beat)
}

func (s *Service) toggleMonitor(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	monitor, ok, err := loadMonitor(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "Not found"})
		return
	}
	active := !boolValue(monitor["active"], true)
	if _, err := db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, boolInt(active), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _, _ := loadMonitor(r.Context(), db, id)
	if active {
		s.startMonitor(updated)
	} else {
		s.stopMonitor(id)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "active": active})
}

func (s *Service) batchDelete(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := int64Slice(payload["ids"])
	if len(ids) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "IDs array is required"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	count := 0
	for _, id := range ids {
		s.stopMonitor(id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM uptime_heartbeats WHERE monitor_id = ?`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM uptime_incidents WHERE monitor_id = ?`, id)
		result, err := tx.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed, _ := result.RowsAffected()
		if changed > 0 {
			count++
		}
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": count})
}

func (s *Service) batchAction(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	action := stringValue(payload["action"], "")
	ids := int64Slice(payload["ids"])
	if len(ids) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "IDs array is required"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	count := 0
	for _, id := range ids {
		monitor, ok, err := loadMonitor(r.Context(), db, id)
		if err != nil || !ok {
			continue
		}
		switch action {
		case "pause":
			_, _ = db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
			s.stopMonitor(id)
			count++
		case "resume":
			_, _ = db.ExecContext(r.Context(), `UPDATE uptime_monitors SET active = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
			monitor["active"] = true
			s.startMonitor(monitor)
			count++
		case "delete":
			s.stopMonitor(id)
			result, _ := db.ExecContext(r.Context(), `DELETE FROM uptime_monitors WHERE id = ?`, id)
			changed, _ := result.RowsAffected()
			if changed > 0 {
				count++
			}
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": count})
}
