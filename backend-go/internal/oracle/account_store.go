package oracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func createAccount(ctx context.Context, db *sql.DB, payload accountPayload) (int64, error) {
	payload = cleanAccountPayload(payload)
	if err := validateAccountPayload(payload, true); err != nil {
		return 0, err
	}
	privateKey, err := secure.SecureEncrypt(payload.PrivateKeyPEM)
	if err != nil {
		return 0, fmt.Errorf("encrypt private key: %w", err)
	}
	passphrase := ""
	if payload.Passphrase != "" {
		passphrase, err = secure.SecureEncrypt(payload.Passphrase)
		if err != nil {
			return 0, fmt.Errorf("encrypt passphrase: %w", err)
		}
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO oracle_accounts (
			name, tenancy_ocid, user_ocid, fingerprint, region,
			private_key_encrypted, passphrase_encrypted, default_compartment_id, description
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.Name, payload.TenancyOCID, payload.UserOCID, payload.Fingerprint, payload.Region,
		privateKey, nullEmpty(passphrase), nullEmpty(payload.DefaultCompartmentID), nullEmpty(payload.Description),
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
	if payload.TenancyOCID == "" {
		payload.TenancyOCID = current.TenancyOCID
	}
	if payload.UserOCID == "" {
		payload.UserOCID = current.UserOCID
	}
	if payload.Fingerprint == "" {
		payload.Fingerprint = current.Fingerprint
	}
	if payload.Region == "" {
		payload.Region = current.Region
	}
	if err := validateAccountPayload(payload, false); err != nil {
		return err
	}
	if payload.PrivateKeyPEM != "" {
		privateKey, err := secure.SecureEncrypt(payload.PrivateKeyPEM)
		if err != nil {
			return fmt.Errorf("encrypt private key: %w", err)
		}
		passphrase := current.PassphraseEncrypted
		if payload.Passphrase != "" {
			passphrase, err = secure.SecureEncrypt(payload.Passphrase)
			if err != nil {
				return fmt.Errorf("encrypt passphrase: %w", err)
			}
		}
		_, err = db.ExecContext(ctx, `
			UPDATE oracle_accounts SET
				name = ?, tenancy_ocid = ?, user_ocid = ?, fingerprint = ?, region = ?,
				private_key_encrypted = ?, passphrase_encrypted = ?,
				default_compartment_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			payload.Name, payload.TenancyOCID, payload.UserOCID, payload.Fingerprint, payload.Region,
			privateKey, nullEmpty(passphrase), nullEmpty(payload.DefaultCompartmentID), nullEmpty(payload.Description), id,
		)
		return err
	}
	_, err = db.ExecContext(ctx, `
		UPDATE oracle_accounts SET
			name = ?, tenancy_ocid = ?, user_ocid = ?, fingerprint = ?, region = ?,
			default_compartment_id = ?, description = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		payload.Name, payload.TenancyOCID, payload.UserOCID, payload.Fingerprint, payload.Region,
		nullEmpty(payload.DefaultCompartmentID), nullEmpty(payload.Description), id,
	)
	return err
}

func deleteAccount(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM oracle_accounts WHERE id = ?`, id)
	return err
}

func listAccounts(ctx context.Context, db *sql.DB) ([]Account, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, tenancy_ocid, user_ocid, fingerprint, region, private_key_encrypted,
			passphrase_encrypted, default_compartment_id, description, last_verified_at,
			last_verify_status, last_verify_error, created_at, updated_at
		FROM oracle_accounts
		ORDER BY created_at DESC`)
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
	return accounts, rows.Err()
}

func getAccount(ctx context.Context, db *sql.DB, id int64) (Account, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, tenancy_ocid, user_ocid, fingerprint, region, private_key_encrypted,
			passphrase_encrypted, default_compartment_id, description, last_verified_at,
			last_verify_status, last_verify_error, created_at, updated_at
		FROM oracle_accounts
		WHERE id = ?`, id)
	return scanAccount(row)
}

func updateVerifyStatus(ctx context.Context, db *sql.DB, id int64, status, errorText string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE oracle_accounts
		SET last_verified_at = CURRENT_TIMESTAMP,
			last_verify_status = ?,
			last_verify_error = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		status, nullEmpty(errorText), id,
	)
	return err
}

type accountScanner interface {
	Scan(dest ...interface{}) error
}

type accountExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func scanAccount(scanner accountScanner) (Account, error) {
	var account Account
	var passphrase, defaultCompartment, description, verifyStatus, verifyError sql.NullString
	var lastVerified sql.NullTime
	if err := scanner.Scan(
		&account.ID,
		&account.Name,
		&account.TenancyOCID,
		&account.UserOCID,
		&account.Fingerprint,
		&account.Region,
		&account.PrivateKeyEncrypted,
		&passphrase,
		&defaultCompartment,
		&description,
		&lastVerified,
		&verifyStatus,
		&verifyError,
		&account.CreatedAt,
		&account.UpdatedAt,
	); err != nil {
		return Account{}, err
	}
	account.PassphraseEncrypted = passphrase.String
	account.DefaultCompartmentID = defaultCompartment.String
	account.Description = description.String
	if lastVerified.Valid {
		t := lastVerified.Time
		account.LastVerifiedAt = &t
	}
	account.LastVerifyStatus = verifyStatus.String
	account.LastVerifyError = verifyError.String
	return account, nil
}

func upsertImportedAccount(ctx context.Context, exec accountExecQuerier, payload accountPayload) error {
	payload = cleanAccountPayload(payload)
	if err := validateAccountPayload(payload, true); err != nil {
		return err
	}

	privateKey, err := secure.SecureEncrypt(payload.PrivateKeyPEM)
	if err != nil {
		return fmt.Errorf("encrypt private key: %w", err)
	}

	passphrase := ""
	if payload.Passphrase != "" {
		passphrase, err = secure.SecureEncrypt(payload.Passphrase)
		if err != nil {
			return fmt.Errorf("encrypt passphrase: %w", err)
		}
	}

	var existingID int64
	findErr := exec.QueryRowContext(
		ctx,
		`SELECT id FROM oracle_accounts WHERE tenancy_ocid = ? AND user_ocid = ? AND fingerprint = ? AND region = ? LIMIT 1`,
		payload.TenancyOCID,
		payload.UserOCID,
		payload.Fingerprint,
		payload.Region,
	).Scan(&existingID)

	switch {
	case findErr == nil:
		_, err = exec.ExecContext(ctx, `
			UPDATE oracle_accounts SET
				name = ?, private_key_encrypted = ?, passphrase_encrypted = ?,
				default_compartment_id = ?, description = ?,
				last_verified_at = NULL, last_verify_status = NULL, last_verify_error = NULL,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			payload.Name,
			privateKey,
			nullEmpty(passphrase),
			nullEmpty(payload.DefaultCompartmentID),
			nullEmpty(payload.Description),
			existingID,
		)
		return err
	case errors.Is(findErr, sql.ErrNoRows):
		_, err = exec.ExecContext(ctx, `
			INSERT INTO oracle_accounts (
				name, tenancy_ocid, user_ocid, fingerprint, region,
				private_key_encrypted, passphrase_encrypted, default_compartment_id, description
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			payload.Name,
			payload.TenancyOCID,
			payload.UserOCID,
			payload.Fingerprint,
			payload.Region,
			privateKey,
			nullEmpty(passphrase),
			nullEmpty(payload.DefaultCompartmentID),
			nullEmpty(payload.Description),
		)
		return err
	default:
		return findErr
	}
}

func safeAccount(account Account) map[string]interface{} {
	item := map[string]interface{}{
		"id":                   account.ID,
		"name":                 account.Name,
		"tenancyOcid":          maskOCID(account.TenancyOCID),
		"userOcid":             maskOCID(account.UserOCID),
		"fingerprint":          account.Fingerprint,
		"region":               account.Region,
		"defaultCompartmentId": account.DefaultCompartmentID,
		"description":          account.Description,
		"lastVerifyStatus":     account.LastVerifyStatus,
		"lastVerifyError":      account.LastVerifyError,
		"createdAt":            account.CreatedAt.Format(time.RFC3339),
		"updatedAt":            account.UpdatedAt.Format(time.RFC3339),
		"hasPrivateKey":        account.PrivateKeyEncrypted != "",
		"hasPassphrase":        account.PassphraseEncrypted != "",
	}
	if account.LastVerifiedAt != nil {
		item["lastVerifiedAt"] = account.LastVerifiedAt.Format(time.RFC3339)
	}
	return item
}

func cleanAccountPayload(payload accountPayload) accountPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.TenancyOCID = strings.TrimSpace(payload.TenancyOCID)
	payload.UserOCID = strings.TrimSpace(payload.UserOCID)
	payload.Fingerprint = strings.TrimSpace(payload.Fingerprint)
	payload.Region = strings.TrimSpace(payload.Region)
	payload.PrivateKeyPEM = strings.TrimSpace(payload.PrivateKeyPEM)
	payload.DefaultCompartmentID = strings.TrimSpace(payload.DefaultCompartmentID)
	payload.Description = strings.TrimSpace(payload.Description)
	return payload
}

func validateAccountPayload(payload accountPayload, requirePrivateKey bool) error {
	if payload.Name == "" || payload.TenancyOCID == "" || payload.UserOCID == "" || payload.Fingerprint == "" || payload.Region == "" {
		return errors.New("请填写账号名称、Tenancy OCID、User OCID、Fingerprint 和 Region")
	}
	if requirePrivateKey && payload.PrivateKeyPEM == "" {
		return errors.New("请填写 Oracle API 私钥")
	}
	return nil
}

func nullEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func maskOCID(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:12] + "..." + value[len(value)-8:]
}
