package cloudflare

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) templates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		templates, err := loadTemplates(r.Context(), db)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, templates)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		records, _ := templateRecordsFromPayload(payload)
		if name == "" || len(records) == 0 {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和至少一条记录必填"})
			return
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		id := newTemplateID()
		now := time.Now().UTC().Format(time.RFC3339)
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		if _, err := db.ExecContext(
			r.Context(),
			`INSERT INTO cf_dns_templates (id, name, description, records, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id,
			name,
			nullableString(stringValue(payload["description"], "")),
			string(recordJSON),
			now,
			now,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"template": map[string]interface{}{
				"id":          id,
				"name":        name,
				"description": stringValue(payload["description"], ""),
				"records":     records,
				"createdAt":   now,
				"updatedAt":   now,
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) templateMutation(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM cf_dns_templates WHERE id = ?`, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "模板不存在"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		existing, err := loadTemplate(r.Context(), db, id)
		if err != nil {
			status := http.StatusInternalServerError
			message := err.Error()
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
				message = "模板不存在"
			}
			response.JSON(w, status, map[string]interface{}{"error": message})
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := stringValue(existing["name"], "")
		if _, ok := payload["name"]; ok {
			name = strings.TrimSpace(stringValue(payload["name"], ""))
		}
		description := stringValue(existing["description"], "")
		if _, ok := payload["description"]; ok {
			description = stringValue(payload["description"], "")
		}
		records := arrayValue(existing["records"])
		if nextRecords, provided := templateRecordsFromPayload(payload); provided {
			records = nextRecords
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339)
		if _, err := db.ExecContext(
			r.Context(),
			`UPDATE cf_dns_templates SET name = ?, description = ?, records = ?, updated_at = ? WHERE id = ?`,
			name,
			nullableString(description),
			string(recordJSON),
			updatedAt,
			id,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		updated, err := loadTemplate(r.Context(), db, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "template": updated})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) applyTemplate(w http.ResponseWriter, r *http.Request, templateID string) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	accountID := strings.TrimSpace(stringValue(payload["accountId"], ""))
	zoneID := strings.TrimSpace(stringValue(payload["zoneId"], ""))
	if accountID == "" || zoneID == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "accountId 和 zoneId 必填"})
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	template, err := loadTemplate(r.Context(), db, templateID)
	_ = db.Close()
	if err != nil {
		status := http.StatusInternalServerError
		message := err.Error()
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			message = "模板不存在"
		}
		response.JSON(w, status, map[string]interface{}{"error": message})
		return
	}
	templateRecords := arrayValue(template["records"])
	if len(templateRecords) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "模板没有可应用的记录"})
		return
	}
	overrideName := strings.TrimSpace(stringValue(payload["recordName"], ""))
	results := []map[string]interface{}{}
	errorsOut := []map[string]interface{}{}
	for _, item := range templateRecords {
		record := objectValue(item)
		next := map[string]interface{}{
			"type":     record["type"],
			"name":     stringValue(record["name"], "@"),
			"content":  record["content"],
			"ttl":      record["ttl"],
			"proxied":  record["proxied"],
			"priority": record["priority"],
		}
		if overrideName != "" {
			next["name"] = overrideName
		}
		created, err := s.createDNSRecord(r.Context(), auth, zoneID, next)
		if err != nil {
			errorsOut = append(errorsOut, map[string]interface{}{"success": false, "record": next, "error": err.Error()})
			continue
		}
		results = append(results, map[string]interface{}{"success": true, "record": created})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": len(errorsOut) == 0,
		"created": len(results),
		"failed":  len(errorsOut),
		"results": results,
		"errors":  errorsOut,
	})
}

func (s *Service) importTemplates(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	items := arrayValue(payload["templates"])
	if items == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 templates 数组"})
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if boolValue(payload["overwrite"]) {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM cf_dns_templates`); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		template := objectValue(item)
		name := strings.TrimSpace(stringValue(template["name"], ""))
		records, _ := templateRecordsFromPayload(template)
		if name == "" || len(records) == 0 {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "模板名称和 records 必填"})
			return
		}
		recordJSON, err := json.Marshal(records)
		if err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		id := strings.TrimSpace(stringValue(template["id"], ""))
		if id == "" || !boolValue(payload["overwrite"]) {
			id = newTemplateID()
		}
		createdAt := stringValue(template["createdAt"], stringValue(template["created_at"], now))
		updatedAt := stringValue(template["updatedAt"], stringValue(template["updated_at"], now))
		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO cf_dns_templates (id, name, description, records, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id,
			name,
			nullableString(stringValue(template["description"], "")),
			string(recordJSON),
			createdAt,
			updatedAt,
		); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "count": len(items)})
}

func loadTemplates(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, description, records, created_at, updated_at FROM cf_dns_templates ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	templates := []map[string]interface{}{}
	for rows.Next() {
		template, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

func loadTemplate(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, description, records, created_at, updated_at FROM cf_dns_templates WHERE id = ?`, id)
	return scanTemplate(row)
}

func scanTemplate(scanner accountScanner) (map[string]interface{}, error) {
	var id, name, recordsRaw string
	var description, createdAt, updatedAt sql.NullString
	if err := scanner.Scan(&id, &name, &description, &recordsRaw, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	records := []interface{}{}
	if strings.TrimSpace(recordsRaw) != "" {
		_ = json.Unmarshal([]byte(recordsRaw), &records)
	}
	return map[string]interface{}{
		"id":          id,
		"name":        name,
		"description": description.String,
		"records":     records,
		"createdAt":   createdAt.String,
		"updatedAt":   updatedAt.String,
	}, nil
}

func templateRecordsFromPayload(payload map[string]interface{}) ([]interface{}, bool) {
	if value, ok := payload["records"]; ok {
		records := arrayValue(value)
		if records == nil {
			return []interface{}{}, true
		}
		return records, true
	}
	recordType := strings.TrimSpace(stringValue(payload["type"], ""))
	content := strings.TrimSpace(stringValue(payload["content"], ""))
	if recordType == "" || content == "" {
		return nil, false
	}
	return []interface{}{map[string]interface{}{
		"type":     recordType,
		"name":     stringValue(payload["recordName"], "@"),
		"content":  content,
		"proxied":  payload["proxied"],
		"ttl":      payload["ttl"],
		"priority": payload["priority"],
	}}, true
}

func newTemplateID() string {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("tpl_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("tpl_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}

// R2 Storage Management
