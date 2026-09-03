package gcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func listAccounts(ctx context.Context, db *sql.DB) ([]Account, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, client_email, default_project_id, service_account_json_encrypted, COALESCE(description, ''), COALESCE(last_verified_at, ''), COALESCE(last_verify_status, ''), COALESCE(last_verify_error, ''), created_at, updated_at FROM gcp_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Account{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func getAccount(ctx context.Context, db *sql.DB, id int64) (Account, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, client_email, default_project_id, service_account_json_encrypted, COALESCE(description, ''), COALESCE(last_verified_at, ''), COALESCE(last_verify_status, ''), COALESCE(last_verify_error, ''), created_at, updated_at FROM gcp_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner rowScanner) (Account, error) {
	var account Account
	var defaultProjectID, description, lastVerifiedAt, lastVerifyStatus, lastVerifyError, createdAt, updatedAt string
	if err := scanner.Scan(
		&account.ID,
		&account.Name,
		&account.ClientEmail,
		&defaultProjectID,
		&account.ServiceAccountJSON,
		&description,
		&lastVerifiedAt,
		&lastVerifyStatus,
		&lastVerifyError,
		&createdAt,
		&updatedAt,
	); err != nil {
		return account, err
	}
	account.DefaultProjectID = defaultProjectID
	account.Description = description
	account.LastVerifyStatus = lastVerifyStatus
	account.LastVerifyError = lastVerifyError
	account.ServiceAccountJSONEncrypted = account.ServiceAccountJSON
	account.ServiceAccountJSON = secure.SecureDecrypt(account.ServiceAccountJSON)
	if parsed, err := time.Parse(time.RFC3339, lastVerifiedAt); err == nil {
		account.LastVerifiedAt = &parsed
	}
	if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
		account.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		account.UpdatedAt = parsed
	}
	return account, nil
}

func createAccount(ctx context.Context, db *sql.DB, payload accountPayload) (int64, error) {
	payload = cleanAccountPayload(payload)
	sa, err := parseServiceAccount(payload.ServiceAccountJSON)
	if err != nil {
		return 0, err
	}
	if err := validateAccountPayload(payload); err != nil {
		return 0, err
	}
	encrypted, err := secure.SecureEncrypt(payload.ServiceAccountJSON)
	if err != nil {
		return 0, fmt.Errorf("encrypt service account json: %w", err)
	}
	defaultProject := payload.DefaultProjectID
	if defaultProject == "" {
		defaultProject = sa.ProjectID
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO gcp_accounts (
			name, client_email, default_project_id, service_account_json_encrypted, description
		) VALUES (?, ?, ?, ?, ?)`,
		payload.Name, sa.ClientEmail, nullEmpty(defaultProject), encrypted, nullEmpty(payload.Description),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateAccount(ctx context.Context, db *sql.DB, id int64, payload accountPayload) error {
	payload = cleanAccountPayload(payload)
	current, err := getAccount(ctx, db, id)
	if err != nil {
		return err
	}
	if payload.Name == "" {
		payload.Name = current.Name
	}
	if payload.Description == "" {
		payload.Description = current.Description
	}
	if payload.DefaultProjectID == "" {
		payload.DefaultProjectID = current.DefaultProjectID
	}
	if payload.ClientEmail == "" {
		payload.ClientEmail = current.ClientEmail
	}
	encrypted := current.ServiceAccountJSONEncrypted
	if payload.ServiceAccountJSON != "" {
		sa, err := parseServiceAccount(payload.ServiceAccountJSON)
		if err != nil {
			return err
		}
		encrypted, err = secure.SecureEncrypt(payload.ServiceAccountJSON)
		if err != nil {
			return fmt.Errorf("encrypt service account json: %w", err)
		}
		payload.ClientEmail = sa.ClientEmail
		if payload.DefaultProjectID == "" {
			payload.DefaultProjectID = sa.ProjectID
		}
	}
	result, err := db.ExecContext(ctx, `
		UPDATE gcp_accounts
		SET name = ?, client_email = ?, default_project_id = ?, service_account_json_encrypted = ?, description = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		payload.Name, payload.ClientEmail, nullEmpty(payload.DefaultProjectID), encrypted, nullEmpty(payload.Description), id,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func deleteAccount(ctx context.Context, db *sql.DB, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM gcp_accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func setDefaultProject(ctx context.Context, db *sql.DB, id int64, projectID string) error {
	result, err := db.ExecContext(ctx, `UPDATE gcp_accounts SET default_project_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, nullEmpty(projectID), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func updateVerifyStatus(ctx context.Context, db *sql.DB, id int64, status, message string) error {
	_, err := db.ExecContext(ctx, `UPDATE gcp_accounts SET last_verify_status = ?, last_verify_error = ?, last_verified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, message, id)
	return err
}

func cleanAccountPayload(payload accountPayload) accountPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.ClientEmail = strings.TrimSpace(payload.ClientEmail)
	payload.DefaultProjectID = strings.TrimSpace(payload.DefaultProjectID)
	payload.ServiceAccountJSON = strings.TrimSpace(payload.ServiceAccountJSON)
	payload.Description = strings.TrimSpace(payload.Description)
	return payload
}

func validateAccountPayload(payload accountPayload) error {
	if payload.Name == "" {
		return errors.New("请填写账号名称")
	}
	if payload.ServiceAccountJSON == "" {
		return errors.New("请粘贴 Service Account JSON")
	}
	return nil
}

func safeAccount(account Account) map[string]interface{} {
	item := map[string]interface{}{
		"id":                     account.ID,
		"name":                   account.Name,
		"clientEmail":            account.ClientEmail,
		"defaultProjectId":       account.DefaultProjectID,
		"description":            account.Description,
		"lastVerifyStatus":       account.LastVerifyStatus,
		"lastVerifyError":        account.LastVerifyError,
		"createdAt":              account.CreatedAt.Format(time.RFC3339),
		"updatedAt":              account.UpdatedAt.Format(time.RFC3339),
		"hasServiceAccountJson":  account.ServiceAccountJSON != "",
	}
	if account.LastVerifiedAt != nil {
		item["lastVerifiedAt"] = account.LastVerifiedAt.Format(time.RFC3339)
	}
	return item
}

func nullEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}