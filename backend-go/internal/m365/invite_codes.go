package m365

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) inviteCodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		var pageFilter int64
		if raw := strings.TrimSpace(r.URL.Query().Get("publicPageId")); raw != "" {
			pageFilter, err = parseID(raw)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "invalid publicPageId")
				return
			}
		}
		items, err := loadInviteCodes(r.Context(), db, pageFilter)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload := make([]map[string]interface{}, 0, len(items))
		now := time.Now().UTC()
		for _, item := range items {
			payload = append(payload, inviteCodeToMap(item, now))
		}
		response.OK(w, map[string]interface{}{"items": payload})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		publicPageID := numberValue(payload["publicPageId"])
		if publicPageID <= 0 {
			response.Error(w, http.StatusBadRequest, "publicPageId is required")
			return
		}
		quantity := clampPositiveInt64(numberValue(payload["quantity"]), 1)
		if quantity > 5 {
			response.Error(w, http.StatusBadRequest, "quantity cannot exceed 5")
			return
		}

		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		page, err := loadPublicPageByID(r.Context(), db, publicPageID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "public page not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if available, reason := evaluatePublicPageAvailability(page, time.Now().UTC()); !available {
			response.Error(w, http.StatusBadRequest, "public page unavailable: "+reason)
			return
		}

		batchID, err := generateInviteCode()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		ids := make([]int64, 0, quantity)
		codes := make([]string, 0, quantity)
		for i := int64(0); i < quantity; i++ {
			code, err := generateInviteCode()
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			result, err := db.ExecContext(
				r.Context(),
				`INSERT INTO m365_invite_codes
				 (public_page_id, code, max_uses, used_count, enabled, batch_id, updated_at)
				 VALUES (?, ?, 1, 0, 1, ?, CURRENT_TIMESTAMP)`,
				publicPageID,
				code,
				batchID,
			)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			id, _ := result.LastInsertId()
			ids = append(ids, id)
			codes = append(codes, code)
		}
		response.OK(w, map[string]interface{}{
			"publicPageId":   publicPageID,
			"publicPageName": page.Name,
			"ids":            ids,
			"codes":          codes,
			"createdCount":   quantity,
			"batchId":        batchID,
		})
	case http.MethodDelete:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		publicPageID := numberValue(payload["publicPageId"])
		batchID := strings.TrimSpace(stringValue(payload["batchId"], ""))
		ids := int64Array(payload["ids"])
		if publicPageID <= 0 && batchID == "" && len(ids) == 0 {
			response.Error(w, http.StatusBadRequest, "publicPageId, batchId or ids is required")
			return
		}

		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		deletedCount, err := deleteInviteCodeBatch(r.Context(), db, publicPageID, batchID, ids)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{
			"deleted":      true,
			"deletedCount": deletedCount,
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) inviteCodeMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid invite code id")
		return
	}
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_invite_codes WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		enabled := boolValue(payload["enabled"], true)
		if _, err := db.ExecContext(r.Context(), `UPDATE m365_invite_codes SET enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, boolToInt(enabled), id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func deleteInviteCodeBatch(ctx context.Context, db *sql.DB, publicPageID int64, batchID string, ids []int64) (int64, error) {
	query := `DELETE FROM m365_invite_codes WHERE `
	args := []interface{}{}
	switch {
	case batchID != "" && publicPageID > 0:
		query += `public_page_id = ? AND batch_id = ?`
		args = append(args, publicPageID, batchID)
	case batchID != "":
		query += `batch_id = ?`
		args = append(args, batchID)
	case len(ids) > 0:
		placeholders := make([]string, 0, len(ids))
		for _, id := range ids {
			if id <= 0 {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		if len(placeholders) == 0 {
			return 0, nil
		}
		query += `id IN (` + strings.Join(placeholders, ",") + `)`
	default:
		query += `public_page_id = ?`
		args = append(args, publicPageID)
	}

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	deletedCount, _ := result.RowsAffected()
	return deletedCount, nil
}

func loadInviteCodes(ctx context.Context, db *sql.DB, publicPageID int64) ([]inviteCodeRecord, error) {
	query := inviteCodeSelectSQL + ``
	args := []interface{}{}
	if publicPageID > 0 {
		query += ` WHERE c.public_page_id = ?`
		args = append(args, publicPageID)
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []inviteCodeRecord{}
	for rows.Next() {
		record, err := scanInviteCode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func loadInviteCodesByBatch(ctx context.Context, db *sql.DB, batchID string) ([]inviteCodeRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		inviteCodeSelectSQL+` WHERE c.batch_id = ? ORDER BY c.id ASC`,
		strings.TrimSpace(batchID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []inviteCodeRecord{}
	for rows.Next() {
		record, scanErr := scanInviteCode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return items, nil
}

func loadInviteCodeByCode(ctx context.Context, db *sql.DB, code string) (inviteCodeRecord, error) {
	row := db.QueryRowContext(ctx, inviteCodeSelectSQL+` WHERE c.code = ?`, strings.TrimSpace(code))
	return scanInviteCode(row)
}

func pickInviteCodeFromBatch(items []inviteCodeRecord, now time.Time) (inviteCodeRecord, bool, string) {
	availabilityReason := ""
	for _, item := range items {
		available, reason := evaluateInviteCodeAvailability(item, now)
		if available {
			return item, true, ""
		}
		if availabilityReason == "" {
			availabilityReason = reason
		}
	}
	if availabilityReason == "" {
		availabilityReason = "used"
	}
	return inviteCodeRecord{}, false, availabilityReason
}

func scanInviteCode(scanner rowScanner) (inviteCodeRecord, error) {
	record := inviteCodeRecord{}
	var accountIDsJSON, domainsJSON, skuIDsJSON string
	var enabled, pageEnabled, forceChange int
	if err := scanner.Scan(
		&record.ID,
		&record.PublicPageID,
		&record.PublicPageName,
		&record.Code,
		&record.MaxUses,
		&record.UsedCount,
		&enabled,
		&record.BatchID,
		&record.LastUsedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.AccountID,
		&record.AccountName,
		&accountIDsJSON,
		&record.Domain,
		&domainsJSON,
		&record.UsageLocation,
		&skuIDsJSON,
		&pageEnabled,
		&forceChange,
		&record.PublicPageExpiresAt,
	); err != nil {
		return inviteCodeRecord{}, err
	}
	record.Enabled = enabled != 0
	record.PublicPageEnabled = pageEnabled != 0
	record.ForceChangePasswordNextSignIn = forceChange != 0
	record.AccountIDs = decodeInt64SliceJSON(accountIDsJSON)
	if len(record.AccountIDs) == 0 && record.AccountID > 0 {
		record.AccountIDs = []int64{record.AccountID}
	}
	record.Domains = normalizeDomainSlice(decodeStringSliceJSON(domainsJSON))
	if len(record.Domains) == 0 && record.Domain != "" {
		record.Domains = []string{record.Domain}
	}
	record.SKUIDs = decodeStringSliceJSON(skuIDsJSON)
	return record, nil
}

func inviteCodeToMap(record inviteCodeRecord, now time.Time) map[string]interface{} {
	available, reason := evaluateInviteCodeAvailability(record, now)
	return map[string]interface{}{
		"id":                 record.ID,
		"publicPageId":       record.PublicPageID,
		"publicPageName":     record.PublicPageName,
		"code":               record.Code,
		"maxUses":            record.MaxUses,
		"usedCount":          record.UsedCount,
		"used":               record.UsedCount > 0,
		"enabled":            record.Enabled,
		"batchId":            emptyToNil(record.BatchID),
		"lastUsedAt":         emptyToNil(record.LastUsedAt),
		"createdAt":          record.CreatedAt,
		"updatedAt":          record.UpdatedAt,
		"accountId":          record.AccountID,
		"accountName":        record.AccountName,
		"accountIds":         record.AccountIDs,
		"domain":             record.Domain,
		"domains":            record.Domains,
		"usageLocation":      record.UsageLocation,
		"skuIds":             record.SKUIDs,
		"publicPageEnabled":  record.PublicPageEnabled,
		"expiresAt":          emptyToNil(record.PublicPageExpiresAt),
		"available":          available,
		"availabilityReason": emptyToNil(reason),
	}
}

func evaluateInviteCodeAvailability(record inviteCodeRecord, now time.Time) (bool, string) {
	if !record.PublicPageEnabled {
		return false, "page_disabled"
	}
	if !record.Enabled {
		return false, "disabled"
	}
	if record.MaxUses > 0 && record.UsedCount >= record.MaxUses {
		return false, "used"
	}
	if record.PublicPageExpiresAt != "" {
		expiresAt, err := parseFlexibleTime(record.PublicPageExpiresAt)
		if err == nil && now.After(expiresAt) {
			return false, "expired"
		}
	}
	return true, ""
}

const inviteCodeSelectSQL = `SELECT c.id, c.public_page_id, COALESCE(p.name, ''), c.code, COALESCE(c.max_uses, 1), COALESCE(c.used_count, 0), COALESCE(c.enabled, 1), COALESCE(c.batch_id, ''), COALESCE(c.last_used_at, ''), c.created_at, c.updated_at, p.account_id, COALESCE(a.name, ''), COALESCE(p.account_ids, ''), p.domain, COALESCE(p.domains, ''), COALESCE(p.usage_location, ''), COALESCE(p.sku_ids, ''), COALESCE(p.enabled, 1), COALESCE(p.force_change_password_next_sign_in, 0), COALESCE(p.expires_at, '')
FROM m365_invite_codes c
JOIN m365_public_pages p ON p.id = c.public_page_id
LEFT JOIN m365_accounts a ON a.id = p.account_id`
