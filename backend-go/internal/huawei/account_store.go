package huawei

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
	rows, err := db.QueryContext(ctx, `SELECT id, name, site, access_key_id, secret_access_key_encrypted, COALESCE(domain_id, ''), COALESCE(default_region, ''), COALESCE(default_project_id, ''), COALESCE(description, ''), COALESCE(ssh_user, ''), COALESCE(ssh_port, 0), COALESCE(ssh_private_key_encrypted, ''), COALESCE(ssh_password_encrypted, ''), COALESCE(last_verified_at, ''), COALESCE(last_verify_status, ''), COALESCE(last_verify_error, ''), created_at, updated_at FROM huawei_accounts ORDER BY created_at DESC`)
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
	row := db.QueryRowContext(ctx, `SELECT id, name, site, access_key_id, secret_access_key_encrypted, COALESCE(domain_id, ''), COALESCE(default_region, ''), COALESCE(default_project_id, ''), COALESCE(description, ''), COALESCE(ssh_user, ''), COALESCE(ssh_port, 0), COALESCE(ssh_private_key_encrypted, ''), COALESCE(ssh_password_encrypted, ''), COALESCE(last_verified_at, ''), COALESCE(last_verify_status, ''), COALESCE(last_verify_error, ''), created_at, updated_at FROM huawei_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAccount(scanner rowScanner) (Account, error) {
	var account Account
	var domainID, defaultRegion, defaultProjectID, description, sshUser string
	var sshPort sql.NullInt64
	var sshPrivateKeyEncrypted, sshPasswordEncrypted string
	var lastVerifiedAt, lastVerifyStatus, lastVerifyError, createdAt, updatedAt string
	if err := scanner.Scan(
		&account.ID,
		&account.Name,
		&account.Site,
		&account.AccessKeyID,
		&account.SecretAccessKey,
		&domainID,
		&defaultRegion,
		&defaultProjectID,
		&description,
		&sshUser,
		&sshPort,
		&sshPrivateKeyEncrypted,
		&sshPasswordEncrypted,
		&lastVerifiedAt,
		&lastVerifyStatus,
		&lastVerifyError,
		&createdAt,
		&updatedAt,
	); err != nil {
		return account, err
	}
	account.DomainID = domainID
	account.DefaultRegion = defaultRegion
	account.DefaultProjectID = defaultProjectID
	account.Description = description
	account.SSHUser = sshUser
	if sshPort.Valid {
		account.SSHPort = int(sshPort.Int64)
	}
	account.SecretAccessKeyEncrypted = account.SecretAccessKey
	account.SecretAccessKey = secure.SecureDecrypt(account.SecretAccessKey)
	account.SSHPrivateKeyEncrypted = sshPrivateKeyEncrypted
	account.SSHPrivateKey = secure.SecureDecrypt(sshPrivateKeyEncrypted)
	account.SSHPasswordEncrypted = sshPasswordEncrypted
	account.SSHPassword = secure.SecureDecrypt(sshPasswordEncrypted)
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
	if err := validateAccountPayload(payload); err != nil {
		return 0, err
	}
	encrypted, err := secure.SecureEncrypt(payload.SecretAccessKey)
	if err != nil {
		return 0, fmt.Errorf("encrypt secret access key: %w", err)
	}
	sshPrivateKeyEncrypted, err := encryptOptional(payload.SSHPrivateKey)
	if err != nil {
		return 0, fmt.Errorf("encrypt ssh private key: %w", err)
	}
	sshPasswordEncrypted, err := encryptOptional(payload.SSHPassword)
	if err != nil {
		return 0, fmt.Errorf("encrypt ssh password: %w", err)
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO huawei_accounts (
			name, site, access_key_id, secret_access_key_encrypted, default_region, default_project_id, description,
			ssh_user, ssh_port, ssh_private_key_encrypted, ssh_password_encrypted
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.Name, siteOrDefault(payload.Site), payload.AccessKeyID, encrypted,
		nullEmpty(payload.DefaultRegion), nullEmpty(payload.DefaultProjectID), nullEmpty(payload.Description),
		nullEmpty(payload.SSHUser), sshPortOrDefault(payload.SSHPort), sshPrivateKeyEncrypted, sshPasswordEncrypted,
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
	if payload.Site == "" {
		payload.Site = current.Site
	}
	if payload.AccessKeyID == "" {
		payload.AccessKeyID = current.AccessKeyID
	}
	if payload.DefaultRegion == "" {
		payload.DefaultRegion = current.DefaultRegion
	}
	if payload.DefaultProjectID == "" {
		payload.DefaultProjectID = current.DefaultProjectID
	}
	if payload.Description == "" {
		payload.Description = current.Description
	}
	if payload.SSHUser == "" {
		payload.SSHUser = current.SSHUser
	}
	sshPort := payload.SSHPort
	if sshPort <= 0 {
		sshPort = current.SSHPort
	}
	if sshPort <= 0 {
		sshPort = 22
	}
	encrypted := current.SecretAccessKeyEncrypted
	if payload.SecretAccessKey != "" {
		encrypted, err = secure.SecureEncrypt(payload.SecretAccessKey)
		if err != nil {
			return fmt.Errorf("encrypt secret access key: %w", err)
		}
	}
	sshPrivateKeyEncrypted := current.SSHPrivateKeyEncrypted
	if payload.SSHPrivateKey != "" {
		sshPrivateKeyEncrypted, err = secure.SecureEncrypt(payload.SSHPrivateKey)
		if err != nil {
			return fmt.Errorf("encrypt ssh private key: %w", err)
		}
	}
	sshPasswordEncrypted := current.SSHPasswordEncrypted
	if payload.SSHPassword != "" {
		sshPasswordEncrypted, err = secure.SecureEncrypt(payload.SSHPassword)
		if err != nil {
			return fmt.Errorf("encrypt ssh password: %w", err)
		}
	}
	result, err := db.ExecContext(ctx, `
		UPDATE huawei_accounts
		SET name = ?, site = ?, access_key_id = ?, secret_access_key_encrypted = ?, default_region = ?, default_project_id = ?, description = ?,
			ssh_user = ?, ssh_port = ?, ssh_private_key_encrypted = ?, ssh_password_encrypted = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		payload.Name, siteOrDefault(payload.Site), payload.AccessKeyID, encrypted,
		nullEmpty(payload.DefaultRegion), nullEmpty(payload.DefaultProjectID), nullEmpty(payload.Description),
		nullEmpty(payload.SSHUser), sshPort, sshPrivateKeyEncrypted, sshPasswordEncrypted, id,
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
	result, err := db.ExecContext(ctx, `DELETE FROM huawei_accounts WHERE id = ?`, id)
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

func updateDefaults(ctx context.Context, db *sql.DB, id int64, region, projectID string) error {
	result, err := db.ExecContext(ctx, `UPDATE huawei_accounts SET default_region = ?, default_project_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		nullEmpty(region), nullEmpty(projectID), id)
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
	_, err := db.ExecContext(ctx, `UPDATE huawei_accounts SET last_verify_status = ?, last_verify_error = ?, last_verified_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, message, id)
	return err
}

func updateDomainID(ctx context.Context, db *sql.DB, id int64, domainID string) error {
	_, err := db.ExecContext(ctx, `UPDATE huawei_accounts SET domain_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, nullEmpty(domainID), id)
	return err
}

func cleanAccountPayload(payload accountPayload) accountPayload {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Site = strings.TrimSpace(payload.Site)
	payload.AccessKeyID = strings.TrimSpace(payload.AccessKeyID)
	payload.SecretAccessKey = strings.TrimSpace(payload.SecretAccessKey)
	payload.DefaultRegion = strings.TrimSpace(payload.DefaultRegion)
	payload.DefaultProjectID = strings.TrimSpace(payload.DefaultProjectID)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.SSHUser = strings.TrimSpace(payload.SSHUser)
	payload.SSHPrivateKey = strings.TrimSpace(payload.SSHPrivateKey)
	return payload
}

func validateAccountPayload(payload accountPayload) error {
	if payload.Name == "" {
		return errors.New("请填写账号名称")
	}
	if payload.AccessKeyID == "" {
		return errors.New("请填写 Access Key ID")
	}
	if payload.SecretAccessKey == "" {
		return errors.New("请填写 Secret Access Key")
	}
	return nil
}

func siteOrDefault(site string) string {
	switch strings.TrimSpace(site) {
	case "cn", "intl":
		return strings.TrimSpace(site)
	default:
		return "cn"
	}
}

func maskAccessKey(ak string) string {
	if len(ak) <= 8 {
		return "***"
	}
	return ak[:4] + "***" + ak[len(ak)-4:]
}

func safeAccount(account Account) map[string]interface{} {
	item := map[string]interface{}{
		"id":               account.ID,
		"name":             account.Name,
		"site":             siteOrDefault(account.Site),
		"accessKeyId":      maskAccessKey(account.AccessKeyID),
		"domainId":         account.DomainID,
		"defaultRegion":    account.DefaultRegion,
		"defaultProjectId": account.DefaultProjectID,
		"description":      account.Description,
		"sshUser":          account.SSHUser,
		"sshPort":          sshPortOrDefault(account.SSHPort),
		"hasSSHKey":        account.SSHPrivateKey != "",
		"hasSSHPassword":   account.SSHPassword != "",
		"lastVerifyStatus": account.LastVerifyStatus,
		"lastVerifyError":  account.LastVerifyError,
		"createdAt":        account.CreatedAt.Format(time.RFC3339),
		"updatedAt":        account.UpdatedAt.Format(time.RFC3339),
		"hasSecretKey":     account.SecretAccessKey != "",
	}
	if account.LastVerifiedAt != nil {
		item["lastVerifiedAt"] = account.LastVerifiedAt.Format(time.RFC3339)
	}
	return item
}

// sshPortOrDefault 默认 SSH 端口 22。
func sshPortOrDefault(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}

// encryptOptional 空值返回 nil（存 NULL），否则加密。
func encryptOptional(value string) (interface{}, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	encrypted, err := secure.SecureEncrypt(value)
	if err != nil {
		return nil, err
	}
	return encrypted, nil
}

func nullEmpty(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
