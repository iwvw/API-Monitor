package m365

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) registrations(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	items, err := loadRegistrations(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, registrationToMap(item))
	}
	response.OK(w, map[string]interface{}{"items": payload})
}

func (s *Service) publicInvite(w http.ResponseWriter, r *http.Request, code string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	record, err := loadInviteByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, publicInvitePayload(r.Context(), db, record, time.Now().UTC()))
}

func (s *Service) publicRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.publicRegisterDescriptor(w, r)
		return
	}
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.TrimSpace(stringValue(payload["code"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	password := strings.TrimSpace(stringValue(payload["password"], ""))
	displayName := strings.TrimSpace(stringValue(payload["displayName"], mailNickname))
	if code == "" || mailNickname == "" {
		response.Error(w, http.StatusBadRequest, "code and mailNickname are required")
		return
	}

	// password 可选：留空时由系统生成并随注册结果返回一次（initialPassword），
	// 与公开注册页「初始密码自动生成」的文案与渲染保持一致。
	initialPassword := ""
	if password == "" {
		generated, err := generateRegistrationPassword()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to generate password")
			return
		}
		password = generated
		initialPassword = generated
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	invite, err := loadInviteByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if available, reason := evaluateInviteAvailability(invite, time.Now().UTC()); !available {
		response.Error(w, http.StatusBadRequest, "invite unavailable: "+reason)
		return
	}

	target, err := resolveInviteRegistrationTarget(r.Context(), db, invite, payload)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	account, err := s.loadDecryptedAccount(r.Context(), strconv.FormatInt(target.ID, 10))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if displayName == "" {
		displayName = mailNickname
	}
	userPrincipalName := mailNickname + "@" + target.Domain
	body := map[string]interface{}{
		"accountEnabled":    true,
		"displayName":       displayName,
		"mailNickname":      mailNickname,
		"userPrincipalName": userPrincipalName,
		"passwordProfile": map[string]interface{}{
			"password":                      password,
			"forceChangePasswordNextSignIn": invite.ForceChangePasswordNextSignIn,
		},
	}
	body["usageLocation"] = resolvedInviteUsageLocation(invite.UsageLocation)

	created := map[string]interface{}{}
	createErr := s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created)
	graphUserID := strings.TrimSpace(stringValue(created["id"], ""))
	status := "success"
	warning := ""
	errorMessage := ""
	if createErr != nil {
		status = "failed"
		errorMessage = createErr.Error()
	} else if len(invite.SKUIDs) > 0 && graphUserID != "" {
		assignBody := map[string]interface{}{
			"addLicenses":    licenseAssignmentsFromIDs(invite.SKUIDs),
			"removeLicenses": []string{},
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(graphUserID)+"/assignLicense", assignBody, nil, nil); err != nil {
			status = "partial"
			errorMessage = err.Error()
			warning = err.Error()
		}
	}

	if persistErr := persistRegistrationResult(r.Context(), db, invite, target.ID, displayName, userPrincipalName, graphUserID, status, errorMessage); persistErr != nil {
		if warning != "" {
			warning += "; "
		}
		warning += "local record save failed"
	}

	if status == "failed" {
		response.Error(w, http.StatusBadGateway, errorMessage)
		return
	}

	response.OK(w, map[string]interface{}{
		"id":                graphUserID,
		"status":            status,
		"accountId":         target.ID,
		"domain":            target.Domain,
		"userPrincipalName": userPrincipalName,
		"initialPassword":   emptyToNil(initialPassword),
		"warning":           emptyToNil(warning),
	})
}

func (s *Service) publicRegisterDescriptor(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	payload := map[string]interface{}{
		"method": "POST",
		"fields": []string{"code", "mailNickname", "password", "displayName", "accountId", "domain"},
	}
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		record, err := loadInviteByCode(r.Context(), db, code)
		if err == nil {
			payload["invite"] = publicInvitePayload(r.Context(), db, record, time.Now().UTC())
		}
	}
	response.OK(w, payload)
}

func loadRegistrations(ctx context.Context, db *sql.DB) ([]registrationRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT r.id, r.invite_id, COALESCE(i.name, ''), COALESCE(i.code, ''), r.account_id, COALESCE(a.name, ''), r.display_name, r.user_principal_name, COALESCE(r.graph_user_id, ''), r.status, COALESCE(r.error_message, ''), r.created_at
		 FROM m365_registration_records r
		 LEFT JOIN m365_registration_invites i ON i.id = r.invite_id
		 LEFT JOIN m365_accounts a ON a.id = r.account_id
		 ORDER BY r.created_at DESC, r.id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []registrationRecord{}
	for rows.Next() {
		record := registrationRecord{}
		if err := rows.Scan(
			&record.ID,
			&record.InviteID,
			&record.InviteName,
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

func registrationToMap(record registrationRecord) map[string]interface{} {
	return map[string]interface{}{
		"id":                record.ID,
		"inviteId":          record.InviteID,
		"inviteName":        record.InviteName,
		"publicPageName":    record.InviteName,
		"inviteCode":        emptyToNil(record.InviteCode),
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

func persistRegistrationResult(ctx context.Context, db *sql.DB, invite inviteRecord, accountID int64, displayName, userPrincipalName, graphUserID, status, errorMessage string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if status == "success" || status == "partial" {
		if _, err := tx.ExecContext(ctx, `UPDATE m365_registration_invites SET used_count = used_count + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, invite.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO m365_registration_records (invite_id, account_id, display_name, user_principal_name, graph_user_id, status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		invite.ID,
		accountID,
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
