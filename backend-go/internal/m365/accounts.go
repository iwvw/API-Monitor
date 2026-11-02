package m365

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func (s *Service) accounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		accounts, err := loadAccounts(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		items := make([]map[string]interface{}, 0, len(accounts))
		for _, account := range accounts {
			items = append(items, safeAccount(account))
		}
		response.OK(w, map[string]interface{}{"items": items})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToAccount(payload, accountRecord{})
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		encryptedSecret, err := secure.SecureEncrypt(record.ClientSecret)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		result, err := db.ExecContext(
			r.Context(),
			`INSERT INTO m365_accounts (name, tenant_id, client_id, client_secret, description, default_domain, verified_domains, organization_name, enabled, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		id, _ := result.LastInsertId()
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) exportAccounts(w http.ResponseWriter, r *http.Request) {
	exported, err := s.exportedAccounts(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"accounts": exported})
}

func (s *Service) exportedAccounts(ctx context.Context) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, map[string]interface{}{
			"name":            account.Name,
			"tenantId":        account.TenantID,
			"clientId":        account.ClientID,
			"clientSecret":    secure.SecureDecrypt(account.ClientSecret),
			"description":     account.Description,
			"defaultDomain":   account.DefaultDomain,
			"verifiedDomains": account.VerifiedDomains,
			"organization":    account.Organization,
			"enabled":         account.Enabled,
		})
	}
	return items, nil
}

func (s *Service) importAccounts(w http.ResponseWriter, r *http.Request) {
	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	items := interfaceArray(payload["accounts"])
	if len(items) == 0 {
		response.Error(w, http.StatusBadRequest, "需要提供 accounts 数组")
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

	if boolValue(payload["overwrite"], false) {
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM m365_accounts`); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, item := range items {
		record, err := payloadToAccount(objectValue(item), accountRecord{})
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		encryptedSecret, err := secure.SecureEncrypt(record.ClientSecret)
		if err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO m365_accounts (name, tenant_id, client_id, client_secret, description, default_domain, verified_domains, organization_name, enabled, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(tenant_id, client_id) DO UPDATE SET
			   name = excluded.name,
			   client_secret = excluded.client_secret,
			   description = excluded.description,
			   default_domain = excluded.default_domain,
			   verified_domains = excluded.verified_domains,
			   organization_name = excluded.organization_name,
			   enabled = excluded.enabled,
			   updated_at = CURRENT_TIMESTAMP`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
		); err != nil {
			_ = tx.Rollback()
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.OK(w, map[string]interface{}{"imported": len(items)})
}

func (s *Service) accountMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid account id")
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
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_accounts WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		existing, err := loadAccount(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, errAccountNotFound) {
				response.Error(w, http.StatusNotFound, err.Error())
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToAccount(payload, existing)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		secretValue := existing.ClientSecret
		if incoming := strings.TrimSpace(stringValue(payload["clientSecret"], "")); incoming != "" {
			secretValue = incoming
		} else if !secure.IsEncrypted(secretValue) {
			secretValue = existing.ClientSecret
		}
		encryptedSecret, err := secure.SecureEncrypt(secretValue)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, err = db.ExecContext(
			r.Context(),
			`UPDATE m365_accounts
			 SET name = ?, tenant_id = ?, client_id = ?, client_secret = ?, description = ?, default_domain = ?, verified_domains = ?, organization_name = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			record.Name,
			record.TenantID,
			record.ClientID,
			encryptedSecret,
			record.Description,
			record.DefaultDomain,
			mustJSONString(record.VerifiedDomains),
			record.Organization,
			boolToInt(record.Enabled),
			id,
		)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) verifyAccount(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	org, err := s.fetchOrganization(r.Context(), account)
	db, dbErr := s.open(r.Context())
	if dbErr == nil {
		defer db.Close()
		if err != nil {
			_, _ = db.ExecContext(r.Context(), `UPDATE m365_accounts SET last_verified_at = CURRENT_TIMESTAMP, last_verified_error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, err.Error(), account.ID)
		} else {
			_, _ = db.ExecContext(
				r.Context(),
				`UPDATE m365_accounts
				 SET organization_name = ?, default_domain = ?, verified_domains = ?, last_verified_at = CURRENT_TIMESTAMP, last_verified_error = '', updated_at = CURRENT_TIMESTAMP
				 WHERE id = ?`,
				stringValue(org["displayName"], account.Organization),
				pickDefaultDomain(org),
				mustJSONString(extractVerifiedDomains(org)),
				account.ID,
			)
		}
	}
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"organization": org,
		"permissionsHint": []string{
			"User.Read.All",
			"User.ReadWrite.All",
			"LicenseAssignment.Read.All",
			"LicenseAssignment.ReadWrite.All",
			"Group.Create",
			"GroupMember.ReadWrite.All",
		},
	})
}

func (s *Service) loadDecryptedAccount(ctx context.Context, idText string) (accountRecord, error) {
	id, err := parseID(idText)
	if err != nil {
		return accountRecord{}, fmt.Errorf("invalid account id")
	}
	db, err := s.open(ctx)
	if err != nil {
		return accountRecord{}, err
	}
	defer db.Close()
	account, err := loadAccount(ctx, db, id)
	if err != nil {
		return accountRecord{}, err
	}
	account.ClientSecret = secure.SecureDecrypt(account.ClientSecret)
	return account, nil
}

func loadAccounts(ctx context.Context, db *sql.DB) ([]accountRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, tenant_id, client_id, client_secret, description, default_domain, COALESCE(verified_domains, ''), organization_name, enabled, COALESCE(last_verified_at, ''), COALESCE(last_verified_error, ''), created_at, updated_at FROM m365_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []accountRecord{}
	for rows.Next() {
		record, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, record)
	}
	return accounts, rows.Err()
}

func loadAccount(ctx context.Context, db *sql.DB, id int64) (accountRecord, error) {
	row := db.QueryRowContext(ctx, `SELECT id, name, tenant_id, client_id, client_secret, description, default_domain, COALESCE(verified_domains, ''), organization_name, enabled, COALESCE(last_verified_at, ''), COALESCE(last_verified_error, ''), created_at, updated_at FROM m365_accounts WHERE id = ?`, id)
	record, err := scanAccount(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accountRecord{}, errAccountNotFound
		}
		return accountRecord{}, err
	}
	return record, nil
}

func scanAccount(scanner rowScanner) (accountRecord, error) {
	record := accountRecord{}
	var verifiedDomainsJSON string
	var enabled int
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.TenantID,
		&record.ClientID,
		&record.ClientSecret,
		&record.Description,
		&record.DefaultDomain,
		&verifiedDomainsJSON,
		&record.Organization,
		&enabled,
		&record.LastVerifiedAt,
		&record.LastVerifiedErr,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return accountRecord{}, err
	}
	record.Enabled = enabled != 0
	record.VerifiedDomains = normalizeDomainSlice(decodeStringSliceJSON(verifiedDomainsJSON))
	if len(record.VerifiedDomains) == 0 && strings.TrimSpace(record.DefaultDomain) != "" {
		record.VerifiedDomains = []string{strings.ToLower(strings.TrimSpace(record.DefaultDomain))}
	}
	return record, nil
}

func safeAccount(record accountRecord) map[string]interface{} {
	secretMask := maskSecret(record.ClientSecret)
	return map[string]interface{}{
		"id":              record.ID,
		"name":            record.Name,
		"tenantId":        record.TenantID,
		"clientId":        record.ClientID,
		"clientSecret":    secretMask,
		"description":     record.Description,
		"defaultDomain":   record.DefaultDomain,
		"verifiedDomains": record.VerifiedDomains,
		"organization":    record.Organization,
		"enabled":         record.Enabled,
		"lastVerifiedAt":  emptyToNil(record.LastVerifiedAt),
		"lastVerifiedErr": emptyToNil(record.LastVerifiedErr),
		"createdAt":       record.CreatedAt,
		"updatedAt":       record.UpdatedAt,
	}
}

func payloadToAccount(payload map[string]interface{}, base accountRecord) (accountRecord, error) {
	record := base
	record.Name = strings.TrimSpace(stringValue(payload["name"], record.Name))
	record.TenantID = strings.TrimSpace(stringValue(payload["tenantId"], stringValue(payload["tenant_id"], record.TenantID)))
	record.ClientID = strings.TrimSpace(stringValue(payload["clientId"], stringValue(payload["client_id"], record.ClientID)))
	record.ClientSecret = strings.TrimSpace(stringValue(payload["clientSecret"], stringValue(payload["client_secret"], record.ClientSecret)))
	record.Description = strings.TrimSpace(stringValue(payload["description"], record.Description))
	record.DefaultDomain = strings.TrimSpace(stringValue(payload["defaultDomain"], stringValue(payload["default_domain"], record.DefaultDomain)))
	if value, ok := payload["verifiedDomains"]; ok {
		record.VerifiedDomains = normalizeDomainSlice(stringArray(value))
	}
	if len(record.VerifiedDomains) == 0 && record.DefaultDomain != "" {
		record.VerifiedDomains = []string{strings.ToLower(record.DefaultDomain)}
	}
	if record.DefaultDomain == "" && len(record.VerifiedDomains) > 0 {
		record.DefaultDomain = record.VerifiedDomains[0]
	}
	record.Organization = strings.TrimSpace(stringValue(payload["organization"], stringValue(payload["organizationName"], record.Organization)))
	record.Enabled = boolValue(payload["enabled"], true)
	if record.Name == "" || record.TenantID == "" || record.ClientID == "" || record.ClientSecret == "" {
		return accountRecord{}, errors.New("name, tenantId, clientId and clientSecret are required")
	}
	return record, nil
}

func (s *Service) writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAccountNotFound):
		response.Error(w, http.StatusNotFound, err.Error())
	default:
		response.Error(w, http.StatusBadRequest, err.Error())
	}
}
