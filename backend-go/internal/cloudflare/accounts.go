package cloudflare

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		accounts, err := loadAccounts(r.Context(), db)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		safe := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			safe = append(safe, safeAccount(account))
		}
		response.JSON(w, http.StatusOK, safe)
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], ""))
		apiToken := strings.TrimSpace(stringValue(payload["apiToken"], stringValue(payload["api_token"], "")))
		email := strings.TrimSpace(stringValue(payload["email"], ""))
		userEmail := strings.TrimSpace(stringValue(payload["userEmail"], stringValue(payload["user_email"], "")))
		cfAccountID := strings.TrimSpace(stringValue(payload["cfAccountId"], stringValue(payload["cf_account_id"], "")))
		if name == "" || apiToken == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和 API Token 必填"})
			return
		}
		if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		if !boolValue(payload["skipVerify"]) {
			verification := s.verifyToken(r.Context(), apiToken, email, cfAccountID)
			if !verification.Valid {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Token 无效: " + verification.Error})
				return
			}
			if verification.Email != "" {
				userEmail = verification.Email
			}
		}
		encrypted, err := secure.SecureEncrypt(apiToken)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		id := newAccountID()
		createdAt := time.Now().UTC().Format(time.RFC3339)
		db, err := s.open(r.Context())
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		defer db.Close()
		if _, err := db.ExecContext(
			r.Context(),
			`INSERT INTO cf_accounts (id, name, api_token, email, user_email, cf_account_id, created_at, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			id,
			name,
			encrypted,
			nullableString(email),
			nullableString(userEmail),
			nullableString(cfAccountID),
			createdAt,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"account": map[string]interface{}{
				"id":          id,
				"name":        name,
				"email":       email,
				"userEmail":   userEmail,
				"cfAccountId": cfAccountID,
				"createdAt":   createdAt,
			},
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context(), false)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "accounts": exported})
}

func (s *Service) exportAccountsRaw(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context(), true)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, exported)
}

func (s *Service) exportedAccounts(ctx context.Context, includeID bool) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return nil, err
	}
	exported := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		item := map[string]interface{}{
			"name":        account["name"],
			"email":       account["email"],
			"userEmail":   stringValue(account["user_email"], ""),
			"cfAccountId": stringValue(account["cf_account_id"], ""),
			"apiToken":    secure.SecureDecrypt(stringValue(account["api_token"], "")),
		}
		if includeID {
			item["id"] = account["id"]
			item["createdAt"] = account["created_at"]
			item["lastUsed"] = account["last_used"]
		}
		exported = append(exported, item)
	}
	return exported, nil
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}
	items := arrayValue(payload["accounts"])
	if items == nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "需要提供 accounts 数组"})
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
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM cf_accounts`); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		account := objectValue(item)
		name := strings.TrimSpace(stringValue(account["name"], ""))
		apiToken := strings.TrimSpace(stringValue(account["apiToken"], stringValue(account["api_token"], "")))
		userEmail := strings.TrimSpace(stringValue(account["userEmail"], stringValue(account["user_email"], "")))
		cfAccountID := strings.TrimSpace(stringValue(account["cfAccountId"], stringValue(account["cf_account_id"], "")))
		if name == "" || apiToken == "" {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "账号名称和 API Token 必填"})
			return
		}
		if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		encrypted, err := secure.SecureEncrypt(secure.SecureDecrypt(apiToken))
		if err != nil {
			_ = tx.Rollback()
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		id := strings.TrimSpace(stringValue(account["id"], ""))
		if id == "" || !boolValue(payload["overwrite"]) {
			id = newAccountID()
		}
		createdAt := stringValue(account["createdAt"], stringValue(account["created_at"], now))
		lastUsed := stringValue(account["lastUsed"], stringValue(account["last_used"], ""))
		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO cf_accounts (id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
			id,
			name,
			encrypted,
			nullableString(strings.TrimSpace(stringValue(account["email"], ""))),
			nullableString(userEmail),
			nullableString(cfAccountID),
			createdAt,
			nullableString(lastUsed),
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

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, id string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodDelete:
		result, err := db.ExecContext(r.Context(), `DELETE FROM cf_accounts WHERE id = ?`, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			response.JSON(w, http.StatusNotFound, map[string]interface{}{"error": "账号不存在"})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	case http.MethodPut:
		existing, err := loadAccount(r.Context(), db, id)
		if err != nil {
			status := http.StatusInternalServerError
			message := err.Error()
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
				message = "账号不存在"
			}
			response.JSON(w, status, map[string]interface{}{"error": message})
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(stringValue(payload["name"], stringValue(existing["name"], "")))
		email := strings.TrimSpace(stringValue(payload["email"], stringValue(existing["email"], "")))
		userEmail := strings.TrimSpace(stringValue(payload["userEmail"], stringValue(payload["user_email"], stringValue(existing["user_email"], ""))))
		cfAccountID := strings.TrimSpace(stringValue(payload["cfAccountId"], stringValue(payload["cf_account_id"], stringValue(existing["cf_account_id"], ""))))
		apiToken := strings.TrimSpace(stringValue(payload["apiToken"], stringValue(payload["api_token"], "")))
		if apiToken == "" || strings.Contains(apiToken, "****") {
			apiToken = secure.SecureDecrypt(stringValue(existing["api_token"], ""))
		} else {
			if err := validateCloudflareCredential(apiToken, cfAccountID); err != nil {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
				return
			}
			verification := s.verifyToken(r.Context(), apiToken, email, cfAccountID)
			if !verification.Valid {
				response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Token 无效: " + verification.Error})
				return
			}
			if verification.Email != "" {
				userEmail = verification.Email
			}
		}
		encrypted, err := secure.SecureEncrypt(apiToken)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "数据加密失败"})
			return
		}
		if name == "" || apiToken == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "名称和 API Token 必填"})
			return
		}
		if _, err := db.ExecContext(
			r.Context(),
			`UPDATE cf_accounts SET name = ?, api_token = ?, email = ?, user_email = ?, cf_account_id = ? WHERE id = ?`,
			name,
			encrypted,
			nullableString(email),
			nullableString(userEmail),
			nullableString(cfAccountID),
			id,
		); err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) verifyStoredAccount(w http.ResponseWriter, r *http.Request, id string) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return
	}
	verification := s.verifyToken(r.Context(), secure.SecureDecrypt(stringValue(account["api_token"], "")), stringValue(account["email"], ""), stringValue(account["cf_account_id"], ""))
	if verification.Valid {
		response.JSON(w, http.StatusOK, map[string]interface{}{"valid": true, "status": verification.Status, "expiresOn": verification.ExpiresOn})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"valid": false, "error": verification.Error})
}

func (s *Service) accountToken(w http.ResponseWriter, r *http.Request, id string) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"apiToken": secure.SecureDecrypt(stringValue(account["api_token"], "")),
	})
}

func (s *Service) authForAccount(w http.ResponseWriter, r *http.Request, id string) (map[string]string, bool) {
	account, ok := s.accountForRequest(w, r, id)
	if !ok {
		return nil, false
	}
	token := secure.SecureDecrypt(stringValue(account["api_token"], ""))
	if token == "" {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"error": "账号 Token 为空"})
		return nil, false
	}
	_ = s.touchAccount(r.Context(), id)
	return cloudflareAuthForAccount(token, account), true
}

func (s *Service) touchAccount(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE cf_accounts SET last_used = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Service) verifyToken(ctx context.Context, apiToken, email, cfAccountID string) verificationResult {
	path := "/user/tokens/verify"
	auth := authHeaders(apiToken, email)
	if email != "" {
		path = "/user"
	}
	if cfAccountID != "" && email == "" {
		path = "/accounts/" + url.PathEscape(cfAccountID) + "/tokens/verify"
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, path, auth, nil)
	if err != nil {
		if cfAccountID != "" || email != "" {
			return verificationResult{Valid: false, Error: err.Error()}
		}
		accountID, accountErr := s.cloudflareAccountID(ctx, auth)
		if accountErr != nil {
			return verificationResult{Valid: false, Error: err.Error()}
		}
		payload, err = s.cfRequest(ctx, http.MethodGet, "/accounts/"+url.PathEscape(accountID)+"/tokens/verify", auth, nil)
		if err != nil {
			return verificationResult{Valid: false, Error: err.Error()}
		}
	}
	result := objectValue(payload["result"])
	userEmail := stringValue(result["email"], "")
	if userEmail == "" && email == "" && cfAccountID == "" {
		userEmail = s.cloudflareUserEmail(ctx, auth)
	}
	return verificationResult{
		Valid:     true,
		Status:    stringValue(result["status"], "active"),
		ExpiresOn: result["expires_on"],
		Email:     userEmail,
	}
}

func (s *Service) cloudflareUserEmail(ctx context.Context, auth map[string]string) string {
	payload, err := s.cfRequest(ctx, http.MethodGet, "/user", auth, nil)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stringValue(objectValue(payload["result"])["email"], ""))
}

func (s *Service) cloudflareAccountID(ctx context.Context, auth map[string]string) (string, error) {
	if id := strings.TrimSpace(auth["__cf_account_id"]); id != "" {
		return id, nil
	}
	payload, err := s.cfRequest(ctx, http.MethodGet, "/accounts?page=1&per_page=1", auth, nil)
	if err != nil {
		return "", err
	}
	accounts := arrayValue(payload["result"])
	if len(accounts) == 0 {
		return "", errors.New("未找到 Cloudflare Account ID")
	}
	id := stringValue(objectValue(accounts[0])["id"], "")
	if id == "" {
		return "", errors.New("Cloudflare Account ID 为空")
	}
	return id, nil
}

func (s *Service) cloudflareAccountIDRoute(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "cfAccountId": cfAccountID})
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active FROM cf_accounts WHERE COALESCE(is_active, 1) = 1 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []map[string]interface{}{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func loadAccount(ctx context.Context, db *sql.DB, id string) (map[string]interface{}, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, api_token, email, user_email, cf_account_id, created_at, last_used, is_active FROM cf_accounts WHERE id = ?`, id)
	return scanAccount(row)
}

func scanAccount(scanner accountScanner) (map[string]interface{}, error) {
	var id, name, apiToken string
	var email, userEmail, cfAccountID, createdAt, lastUsed sql.NullString
	var isActive sql.NullInt64
	if err := scanner.Scan(&id, &name, &apiToken, &email, &userEmail, &cfAccountID, &createdAt, &lastUsed, &isActive); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":            id,
		"name":          name,
		"api_token":     apiToken,
		"email":         email.String,
		"user_email":    userEmail.String,
		"cf_account_id": cfAccountID.String,
		"created_at":    createdAt.String,
		"last_used":     lastUsed.String,
		"is_active":     isActive.Int64,
	}, nil
}

func safeAccount(account map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":          account["id"],
		"name":        account["name"],
		"email":       account["email"],
		"userEmail":   account["user_email"],
		"cfAccountId": account["cf_account_id"],
		"createdAt":   account["created_at"],
		"lastUsed":    account["last_used"],
		"hasToken":    stringValue(account["api_token"], "") != "",
	}
}

func newAccountID() string {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("cf_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("cf_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}

func cloudflareAuthForAccount(token string, account map[string]interface{}) map[string]string {
	auth := authHeaders(token, stringValue(account["email"], ""))
	if id := strings.TrimSpace(stringValue(account["cf_account_id"], "")); id != "" {
		auth["__cf_account_id"] = id
	}
	return auth
}

func (s *Service) accountForRequest(w http.ResponseWriter, r *http.Request, id string) (map[string]interface{}, bool) {
	db, err := s.open(r.Context())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Database error"})
		return nil, false
	}
	defer db.Close()
	account, err := loadAccount(r.Context(), db, id)
	if err != nil {
		status := http.StatusInternalServerError
		message := "Database error"
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			message = "账号不存在"
		}
		response.JSON(w, status, map[string]interface{}{"error": message})
		return nil, false
	}
	return account, true
}
