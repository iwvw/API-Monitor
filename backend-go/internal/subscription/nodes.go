package subscription

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) listNodes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	filterID := r.URL.Query().Get("subscription_id")
	if filterID != "" {
		filterID = firstNonEmpty(profileIDForSubscription(r.Context(), db, filterID), filterID)
	}
	nodes, err := loadNodes(r.Context(), db, filterID, true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, nodes)
}

func (s *Service) updateNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var node Node
	if !decodeJSON(w, r, &node) {
		return
	}
	rawEnc, _ := secure.SecureEncrypt(node.Raw)
	cfgEnc, _ := secure.SecureEncrypt(node.ConfigJSON)
	// subscription_nodes are imported external nodes by definition. Internal
	// nodes live in managed_proxy_nodes and cannot be converted by editing.
	ownership, management, reporting, trafficServerID := "external", "unmanaged", "unavailable", ""
	_, err := db.ExecContext(r.Context(), `UPDATE subscription_nodes SET name = ?, type = ?, server = ?, port = ?, country_code = ?, location = ?, tags = ?, traffic_server_id = ?, ownership = ?, management = ?, traffic_reporting = ?, enabled = ?, stable = ?, sort_order = ?, raw_encrypted = CASE WHEN ? = '' THEN raw_encrypted ELSE ? END, config_encrypted = CASE WHEN ? = '' THEN config_encrypted ELSE ? END, updated_at = datetime('now') WHERE id = ?`,
		node.Name, node.Type, node.Server, node.Port, node.CountryCode, node.Location, node.Tags, nullString(trafficServerID), ownership, management, reporting, boolToInt(node.Enabled), boolToInt(node.Stable), node.SortOrder, node.Raw, rawEnc, node.ConfigJSON, cfgEnc, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func (s *Service) deleteNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE node_id=? AND source='external'`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_nodes WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "外部节点不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func (s *Service) reorderNodes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var payload struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	for i, id := range payload.IDs {
		_, _ = tx.ExecContext(r.Context(), `UPDATE subscription_nodes SET sort_order = ?, updated_at = datetime('now') WHERE id = ?`, i+1, id)
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"updated": true})
}

func normalizeNodeOwnership(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "self") {
		return "self"
	}
	return "external"
}

func normalizeNodeManagement(value, ownership string) string {
	if ownership == "self" && strings.EqualFold(strings.TrimSpace(value), "agent") {
		return "agent"
	}
	return "unmanaged"
}

func normalizeNodeTrafficReporting(value, management string) string {
	if management == "agent" && strings.EqualFold(strings.TrimSpace(value), "trusted") {
		return "trusted"
	}
	return "unavailable"
}
