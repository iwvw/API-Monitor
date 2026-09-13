package m365

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) publicPageRegistrations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		items, err := loadPublicPageRegistrations(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			payload = append(payload, publicPageRegistrationToMap(item))
		}
		response.OK(w, map[string]interface{}{"items": payload})
	case http.MethodDelete:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		ids := int64Array(payload["ids"])
		deleteAll := boolValue(payload["all"], false)
		if !deleteAll && len(ids) == 0 {
			response.Error(w, http.StatusBadRequest, "ids or all is required")
			return
		}

		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		deletedCount, err := deletePublicPageRegistrations(r.Context(), db, ids, deleteAll)
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

func deletePublicPageRegistrations(ctx context.Context, db *sql.DB, ids []int64, deleteAll bool) (int64, error) {
	query := `DELETE FROM m365_public_page_registrations`
	args := []interface{}{}

	if !deleteAll {
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
		query += ` WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	}

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	deletedCount, _ := result.RowsAffected()
	return deletedCount, nil
}

func loadPublicPageRegistrations(ctx context.Context, db *sql.DB) ([]publicPageRegistrationRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT id, COALESCE(public_page_id, 0), public_page_name, COALESCE(invite_code_id, 0), invite_code, account_id, COALESCE(account_name, ''), display_name, user_principal_name, COALESCE(graph_user_id, ''), status, COALESCE(error_message, ''), created_at
		 FROM m365_public_page_registrations
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []publicPageRegistrationRecord{}
	for rows.Next() {
		record := publicPageRegistrationRecord{}
		if err := rows.Scan(
			&record.ID,
			&record.PublicPageID,
			&record.PublicPageName,
			&record.InviteCodeID,
			&record.InviteCode,
			&record.AccountID,
			&record.AccountName,
			&record.DisplayName,
			&record.UserPrincipalName,
			&record.GraphUserID,
			&record.Status,
			&record.ErrorMessage,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func publicPageRegistrationToMap(record publicPageRegistrationRecord) map[string]interface{} {
	return map[string]interface{}{
		"id":                record.ID,
		"publicPageId":      record.PublicPageID,
		"publicPageName":    record.PublicPageName,
		"inviteCodeId":      record.InviteCodeID,
		"inviteCode":        record.InviteCode,
		"accountId":         record.AccountID,
		"accountName":       record.AccountName,
		"displayName":       record.DisplayName,
		"userPrincipalName": record.UserPrincipalName,
		"graphUserId":       emptyToNil(record.GraphUserID),
		"status":            record.Status,
		"errorMessage":      emptyToNil(record.ErrorMessage),
		"createdAt":         record.CreatedAt,
	}
}

func persistPublicPageRegistration(ctx context.Context, db *sql.DB, inviteCode inviteCodeRecord, accountID int64, accountName, displayName, userPrincipalName, graphUserID, status, errorMessage string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if status == "success" || status == "partial" {
		// 条件自增：可用性检查发生在 Graph 建号之前的窗口内，并发注册会
		// 双双通过；此处把「已用完」的并发消耗用原子条件拒绝掉，防止双兑换。
		result, err := tx.ExecContext(
			ctx,
			`UPDATE m365_invite_codes SET used_count = used_count + 1, last_used_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND (max_uses = 0 OR used_count < max_uses)`,
			inviteCode.ID,
		)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return errInviteCodeExhausted
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO m365_public_page_registrations
		 (public_page_id, public_page_name, invite_code_id, invite_code, account_id, account_name, display_name, user_principal_name, graph_user_id, status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inviteCode.PublicPageID,
		inviteCode.PublicPageName,
		inviteCode.ID,
		inviteCode.Code,
		accountID,
		accountName,
		displayName,
		userPrincipalName,
		nullIfEmpty(graphUserID),
		status,
		nullIfEmpty(errorMessage),
	); err != nil {
		return err
	}
	return tx.Commit()
}
