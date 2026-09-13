package m365

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type publicPageRecord struct {
	ID                            int64
	Name                          string
	AccountID                     int64
	AccountName                   string
	AccountIDs                    []int64
	Domain                        string
	Domains                       []string
	UsageLocation                 string
	SKUIDs                        []string
	Enabled                       bool
	ForceChangePasswordNextSignIn bool
	ExpiresAt                     string
	CreatedAt                     string
	UpdatedAt                     string
	InviteCodeCount               int64
	UsedInviteCodeCount           int64
}

type inviteCodeRecord struct {
	ID                            int64
	PublicPageID                  int64
	PublicPageName                string
	Code                          string
	MaxUses                       int64
	UsedCount                     int64
	Enabled                       bool
	BatchID                       string
	LastUsedAt                    string
	CreatedAt                     string
	UpdatedAt                     string
	AccountID                     int64
	AccountName                   string
	AccountIDs                    []int64
	Domain                        string
	Domains                       []string
	UsageLocation                 string
	SKUIDs                        []string
	PublicPageEnabled             bool
	ForceChangePasswordNextSignIn bool
	PublicPageExpiresAt           string
}

type publicPageRegistrationRecord struct {
	ID                int64
	PublicPageID      int64
	PublicPageName    string
	InviteCodeID      int64
	InviteCode        string
	AccountID         int64
	AccountName       string
	DisplayName       string
	UserPrincipalName string
	GraphUserID       string
	Status            string
	ErrorMessage      string
	CreatedAt         string
}

type legacyRegistrationRecord struct {
	ID                int64
	InviteID          int64
	DisplayName       string
	UserPrincipalName string
	GraphUserID       string
	Status            string
	ErrorMessage      string
	AccountID         int64
	AccountName       string
	CreatedAt         string
}

type legacyInviteMapping struct {
	PublicPageID int64
	InviteCodeID int64
	InviteCode   string
	PageName     string
}

func (s *Service) publicPages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		items, err := loadPublicPages(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload := make([]map[string]interface{}, 0, len(items))
		now := time.Now().UTC()
		for _, item := range items {
			payload = append(payload, publicPageToMap(item, now))
		}
		response.OK(w, map[string]interface{}{"items": payload})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToPublicPage(payload, publicPageRecord{
			Enabled:                       true,
			ForceChangePasswordNextSignIn: false,
		})
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()

		record, err = normalizePublicPageTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := insertPublicPage(r.Context(), db, record); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"created": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) publicPageMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid public page id")
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
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_public_pages WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		existing, err := loadPublicPageByID(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "public page not found")
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
		record, err := payloadToPublicPage(payload, existing)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err = normalizePublicPageTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := updatePublicPage(r.Context(), db, id, record); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"updated": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func payloadToPublicPage(payload map[string]interface{}, base publicPageRecord) (publicPageRecord, error) {
	record := publicPageToInviteRecord(base)
	normalized, err := payloadToInvite(payload, record)
	if err != nil {
		return publicPageRecord{}, err
	}
	return inviteRecordToPublicPage(normalized, base), nil
}

func normalizePublicPageTargets(ctx context.Context, db *sql.DB, record publicPageRecord) (publicPageRecord, error) {
	normalized, err := normalizeInviteTargets(ctx, db, publicPageToInviteRecord(record))
	if err != nil {
		return publicPageRecord{}, err
	}
	return inviteRecordToPublicPage(normalized, record), nil
}

func insertPublicPage(ctx context.Context, db *sql.DB, record publicPageRecord) error {
	accountIDsJSON, err := jsonString(record.AccountIDs)
	if err != nil {
		return err
	}
	domainsJSON, err := jsonString(record.Domains)
	if err != nil {
		return err
	}
	skuJSON, err := jsonString(record.SKUIDs)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO m365_public_pages
		 (name, account_id, account_ids, domain, domains, usage_location, sku_ids, enabled, force_change_password_next_sign_in, expires_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		record.Name,
		record.AccountID,
		accountIDsJSON,
		record.Domain,
		domainsJSON,
		record.UsageLocation,
		skuJSON,
		boolToInt(record.Enabled),
		boolToInt(record.ForceChangePasswordNextSignIn),
		nullIfEmpty(record.ExpiresAt),
	)
	return err
}

func updatePublicPage(ctx context.Context, db *sql.DB, id int64, record publicPageRecord) error {
	accountIDsJSON, err := jsonString(record.AccountIDs)
	if err != nil {
		return err
	}
	domainsJSON, err := jsonString(record.Domains)
	if err != nil {
		return err
	}
	skuJSON, err := jsonString(record.SKUIDs)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(
		ctx,
		`UPDATE m365_public_pages
		 SET name = ?, account_id = ?, account_ids = ?, domain = ?, domains = ?, usage_location = ?, sku_ids = ?, enabled = ?, force_change_password_next_sign_in = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		record.Name,
		record.AccountID,
		accountIDsJSON,
		record.Domain,
		domainsJSON,
		record.UsageLocation,
		skuJSON,
		boolToInt(record.Enabled),
		boolToInt(record.ForceChangePasswordNextSignIn),
		nullIfEmpty(record.ExpiresAt),
		id,
	)
	return err
}

func loadPublicPages(ctx context.Context, db *sql.DB) ([]publicPageRecord, error) {
	rows, err := db.QueryContext(ctx, publicPageSelectSQL+` ORDER BY p.created_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []publicPageRecord{}
	for rows.Next() {
		record, err := scanPublicPage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func loadPublicPageByID(ctx context.Context, db *sql.DB, id int64) (publicPageRecord, error) {
	row := db.QueryRowContext(ctx, publicPageSelectSQL+` WHERE p.id = ?`, id)
	return scanPublicPage(row)
}

func scanPublicPage(scanner rowScanner) (publicPageRecord, error) {
	record := publicPageRecord{}
	var accountIDsJSON, domainsJSON, skuIDsJSON string
	var enabled, forceChange int
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.AccountID,
		&record.AccountName,
		&accountIDsJSON,
		&record.Domain,
		&domainsJSON,
		&record.UsageLocation,
		&skuIDsJSON,
		&enabled,
		&forceChange,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.InviteCodeCount,
		&record.UsedInviteCodeCount,
	); err != nil {
		return publicPageRecord{}, err
	}
	record.Enabled = enabled != 0
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

func publicPageToMap(record publicPageRecord, now time.Time) map[string]interface{} {
	available, reason := evaluatePublicPageAvailability(record, now)
	return map[string]interface{}{
		"id":                            record.ID,
		"name":                          record.Name,
		"accountId":                     record.AccountID,
		"accountName":                   record.AccountName,
		"accountIds":                    record.AccountIDs,
		"domain":                        record.Domain,
		"domains":                       record.Domains,
		"usageLocation":                 record.UsageLocation,
		"skuIds":                        record.SKUIDs,
		"enabled":                       record.Enabled,
		"forceChangePasswordNextSignIn": record.ForceChangePasswordNextSignIn,
		"expiresAt":                     emptyToNil(record.ExpiresAt),
		"inviteCodeCount":               record.InviteCodeCount,
		"usedInviteCodeCount":           record.UsedInviteCodeCount,
		"unusedInviteCodeCount":         maxInt64(record.InviteCodeCount-record.UsedInviteCodeCount, 0),
		"available":                     available,
		"availabilityReason":            emptyToNil(reason),
		"createdAt":                     record.CreatedAt,
		"updatedAt":                     record.UpdatedAt,
	}
}

func evaluatePublicPageAvailability(record publicPageRecord, now time.Time) (bool, string) {
	if !record.Enabled {
		return false, "disabled"
	}
	if record.ExpiresAt != "" {
		expiresAt, err := parseFlexibleTime(record.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			return false, "expired"
		}
	}
	return true, ""
}

const publicPageSelectSQL = `SELECT p.id, p.name, p.account_id, COALESCE(a.name, ''), COALESCE(p.account_ids, ''), p.domain, COALESCE(p.domains, ''), COALESCE(p.usage_location, ''), COALESCE(p.sku_ids, ''), COALESCE(p.enabled, 1), COALESCE(p.force_change_password_next_sign_in, 0), COALESCE(p.expires_at, ''), p.created_at, p.updated_at, COALESCE(stats.code_count, 0), COALESCE(stats.used_code_count, 0)
FROM m365_public_pages p
LEFT JOIN m365_accounts a ON a.id = p.account_id
LEFT JOIN (
	SELECT public_page_id, COUNT(*) AS code_count, SUM(CASE WHEN used_count > 0 THEN 1 ELSE 0 END) AS used_code_count
	FROM m365_invite_codes
	GROUP BY public_page_id
) stats ON stats.public_page_id = p.id`
