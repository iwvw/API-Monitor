package m365

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) invites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		items, err := loadInvites(r.Context(), db)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload := make([]map[string]interface{}, 0, len(items))
		now := time.Now().UTC()
		for _, item := range items {
			payload = append(payload, inviteToMap(item, now))
		}
		response.OK(w, map[string]interface{}{"items": payload})
	case http.MethodPost:
		payload, err := readObject(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err := payloadToInvite(payload, inviteRecord{
			Enabled:                       true,
			MaxUses:                       1,
			ForceChangePasswordNextSignIn: false,
		})
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if record.Code == "" {
			record.Code = ""
		}
		db, err := s.open(r.Context())
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer db.Close()
		record, err = normalizeInviteTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		skuJSON, err := json.Marshal(record.SKUIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		accountIDsJSON, err := json.Marshal(record.AccountIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		domainsJSON, err := json.Marshal(record.Domains)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		quantity := clampPositiveInt64(numberValue(payload["quantity"]), 1)
		if quantity > 200 {
			quantity = 200
		}
		batchID, err := generateInviteCode()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		codes := make([]string, 0, quantity)
		ids := make([]int64, 0, quantity)
		for i := int64(0); i < quantity; i++ {
			code := strings.TrimSpace(record.Code)
			if quantity > 1 || code == "" {
				code, err = generateInviteCode()
				if err != nil {
					response.Error(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			result, err := db.ExecContext(
				r.Context(),
				`INSERT INTO m365_registration_invites
				 (code, name, account_id, account_ids, domain, domains, usage_location, sku_ids, max_uses, used_count, enabled, force_change_password_next_sign_in, batch_id, expires_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 0, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				code,
				record.Name,
				record.AccountID,
				string(accountIDsJSON),
				record.Domain,
				string(domainsJSON),
				record.UsageLocation,
				string(skuJSON),
				boolToInt(record.Enabled),
				boolToInt(record.ForceChangePasswordNextSignIn),
				batchID,
				nullIfEmpty(record.ExpiresAt),
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
			"id":           firstInt64(ids),
			"code":         firstString(codes),
			"ids":          ids,
			"codes":        codes,
			"createdCount": quantity,
			"batchId":      batchID,
		})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) inviteMutation(w http.ResponseWriter, r *http.Request, idText string) {
	id, err := parseID(idText)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid invite id")
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
		if _, err := db.ExecContext(r.Context(), `DELETE FROM m365_registration_invites WHERE id = ?`, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"deleted": true})
	case http.MethodPut:
		existing, err := loadInviteByID(r.Context(), db, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite not found")
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
		record, err := payloadToInvite(payload, existing)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		record, err = normalizeInviteTargets(r.Context(), db, record)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		skuJSON, err := json.Marshal(record.SKUIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		accountIDsJSON, err := json.Marshal(record.AccountIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		domainsJSON, err := json.Marshal(record.Domains)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, err = db.ExecContext(
			r.Context(),
			`UPDATE m365_registration_invites
			 SET code = ?, name = ?, account_id = ?, account_ids = ?, domain = ?, domains = ?, usage_location = ?, sku_ids = ?, max_uses = ?, enabled = ?, force_change_password_next_sign_in = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			record.Code,
			record.Name,
			record.AccountID,
			string(accountIDsJSON),
			record.Domain,
			string(domainsJSON),
			record.UsageLocation,
			string(skuJSON),
			record.MaxUses,
			boolToInt(record.Enabled),
			boolToInt(record.ForceChangePasswordNextSignIn),
			nullIfEmpty(record.ExpiresAt),
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

func loadInvites(ctx context.Context, db *sql.DB) ([]inviteRecord, error) {
	rows, err := db.QueryContext(ctx, inviteSelectSQL+` ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []inviteRecord{}
	for rows.Next() {
		record, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func loadInviteByID(ctx context.Context, db *sql.DB, id int64) (inviteRecord, error) {
	row := db.QueryRowContext(ctx, inviteSelectSQL+` WHERE i.id = ?`, id)
	return scanInvite(row)
}

func loadInviteByCode(ctx context.Context, db *sql.DB, code string) (inviteRecord, error) {
	row := db.QueryRowContext(ctx, inviteSelectSQL+` WHERE i.code = ?`, strings.TrimSpace(code))
	return scanInvite(row)
}

func scanInvite(scanner rowScanner) (inviteRecord, error) {
	record := inviteRecord{}
	var accountIDsJSON, domainsJSON, skuIDsJSON, batchID string
	var enabled, forceChange int
	if err := scanner.Scan(
		&record.ID,
		&record.Code,
		&record.Name,
		&record.AccountID,
		&record.AccountName,
		&accountIDsJSON,
		&record.Domain,
		&domainsJSON,
		&record.UsageLocation,
		&skuIDsJSON,
		&record.MaxUses,
		&record.UsedCount,
		&enabled,
		&forceChange,
		&batchID,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return inviteRecord{}, err
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
	record.BatchID = strings.TrimSpace(batchID)
	return record, nil
}

func payloadToInvite(payload map[string]interface{}, base inviteRecord) (inviteRecord, error) {
	record := base
	if value, ok := payload["code"]; ok {
		record.Code = strings.TrimSpace(stringValue(value, record.Code))
	}
	record.Name = strings.TrimSpace(stringValue(payload["name"], record.Name))
	if value, ok := payload["accountId"]; ok {
		record.AccountID = numberValue(value)
	}
	if value, ok := payload["accountIds"]; ok {
		record.AccountIDs = int64Array(value)
	}
	record.Domain = strings.ToLower(strings.TrimSpace(stringValue(payload["domain"], record.Domain)))
	if value, ok := payload["domains"]; ok {
		record.Domains = normalizeDomainSlice(stringArray(value))
	}
	record.UsageLocation = strings.ToUpper(strings.TrimSpace(stringValue(payload["usageLocation"], record.UsageLocation)))
	if value, ok := payload["skuIds"]; ok {
		record.SKUIDs = stringArray(value)
	}
	if value, ok := payload["maxUses"]; ok {
		record.MaxUses = maxInt64(numberValue(value), 0)
	}
	if value, ok := payload["enabled"]; ok {
		record.Enabled = boolValue(value, record.Enabled)
	}
	if value, ok := payload["forceChangePasswordNextSignIn"]; ok {
		record.ForceChangePasswordNextSignIn = boolValue(value, record.ForceChangePasswordNextSignIn)
	}
	if value, ok := payload["expiresAt"]; ok {
		expiresAt, err := normalizeOptionalTimestamp(stringValue(value, ""))
		if err != nil {
			return inviteRecord{}, err
		}
		record.ExpiresAt = expiresAt
	}
	if record.Name == "" {
		return inviteRecord{}, errors.New("name is required")
	}
	if record.MaxUses < 0 {
		record.MaxUses = 0
	}
	return record, nil
}

func inviteToMap(record inviteRecord, now time.Time) map[string]interface{} {
	available, reason := evaluateInviteAvailability(record, now)
	remainingUses := interface{}(nil)
	if record.MaxUses > 0 {
		remainingUses = maxInt(int(record.MaxUses-record.UsedCount), 0)
	}
	return map[string]interface{}{
		"id":                            record.ID,
		"code":                          record.Code,
		"name":                          record.Name,
		"accountId":                     record.AccountID,
		"accountName":                   record.AccountName,
		"accountIds":                    record.AccountIDs,
		"domain":                        record.Domain,
		"domains":                       record.Domains,
		"usageLocation":                 record.UsageLocation,
		"skuIds":                        record.SKUIDs,
		"maxUses":                       record.MaxUses,
		"usedCount":                     record.UsedCount,
		"remainingUses":                 remainingUses,
		"enabled":                       record.Enabled,
		"forceChangePasswordNextSignIn": record.ForceChangePasswordNextSignIn,
		"batchId":                       emptyToNil(record.BatchID),
		"expiresAt":                     emptyToNil(record.ExpiresAt),
		"available":                     available,
		"availabilityReason":            emptyToNil(reason),
		"createdAt":                     record.CreatedAt,
		"updatedAt":                     record.UpdatedAt,
	}
}

func evaluateInviteAvailability(record inviteRecord, now time.Time) (bool, string) {
	if !record.Enabled {
		return false, "disabled"
	}
	if record.MaxUses > 0 && record.UsedCount >= record.MaxUses {
		return false, "exhausted"
	}
	if record.ExpiresAt != "" {
		expiresAt, err := parseFlexibleTime(record.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			return false, "expired"
		}
	}
	return true, ""
}

func normalizeOptionalTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := parseFlexibleTime(value)
	if err != nil {
		return "", errors.New("invalid expiresAt")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func resolvedInviteUsageLocation(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized != "" {
		return normalized
	}
	return "CN"
}

func normalizeInviteTargets(ctx context.Context, db *sql.DB, record inviteRecord) (inviteRecord, error) {
	if record.AccountID > 0 && len(record.AccountIDs) == 0 {
		record.AccountIDs = []int64{record.AccountID}
	}
	record.AccountIDs = uniqueInt64(record.AccountIDs)
	record.Domains = normalizeDomainSlice(append(record.Domains, record.Domain))
	if len(record.AccountIDs) == 0 && len(record.Domains) == 0 {
		return inviteRecord{}, errors.New("at least one account or domain is required")
	}

	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return inviteRecord{}, err
	}
	targets := resolveInviteTargetsFromAccounts(record, accounts)
	if len(targets) == 0 {
		return inviteRecord{}, errors.New("no available tenant/domain targets matched")
	}

	record.AccountID = targets[0].ID
	record.AccountName = targets[0].Name
	record.Domain = targets[0].Domain
	record.AccountIDs = make([]int64, 0, len(targets))
	record.Domains = make([]string, 0, len(targets))
	for _, target := range targets {
		record.AccountIDs = append(record.AccountIDs, target.ID)
		record.Domains = append(record.Domains, target.Domain)
	}
	record.AccountIDs = uniqueInt64(record.AccountIDs)
	record.Domains = normalizeDomainSlice(record.Domains)
	if record.MaxUses <= 0 {
		record.MaxUses = 1
	}
	return record, nil
}

func resolveInviteTargetsFromAccounts(record inviteRecord, accounts []accountRecord) []inviteTarget {
	accountFilter := map[int64]bool{}
	for _, id := range record.AccountIDs {
		if id > 0 {
			accountFilter[id] = true
		}
	}
	domainFilter := map[string]bool{}
	for _, domain := range record.Domains {
		normalized := strings.ToLower(strings.TrimSpace(domain))
		if normalized != "" {
			domainFilter[normalized] = true
		}
	}

	if len(accountFilter) == 1 && len(domainFilter) > 0 {
		targets := []inviteTarget{}
		for _, account := range accounts {
			if !accountFilter[account.ID] {
				continue
			}
			for domain := range domainFilter {
				targets = append(targets, inviteTarget{
					ID:     account.ID,
					Name:   account.Name,
					Domain: domain,
				})
			}
			break
		}
		if len(targets) > 0 {
			sortInviteTargets(targets)
			return targets
		}
	}

	targets := []inviteTarget{}
	for _, account := range accounts {
		domains := accountDomains(account)
		if len(domains) == 0 {
			continue
		}
		if len(accountFilter) > 0 && !accountFilter[account.ID] {
			continue
		}
		for _, domain := range domains {
			if len(domainFilter) > 0 && !domainFilter[domain] {
				continue
			}
			targets = append(targets, inviteTarget{
				ID:     account.ID,
				Name:   account.Name,
				Domain: domain,
			})
		}
	}
	sortInviteTargets(targets)
	return targets
}

func sortInviteTargets(targets []inviteTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Domain != targets[j].Domain {
			return targets[i].Domain < targets[j].Domain
		}
		if targets[i].ID != targets[j].ID {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].Name < targets[j].Name
	})
}

func resolveInviteRegistrationTarget(ctx context.Context, db *sql.DB, invite inviteRecord, payload map[string]interface{}) (inviteTarget, error) {
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		return inviteTarget{}, err
	}
	targets := resolveInviteTargetsFromAccounts(invite, accounts)
	if len(targets) == 0 {
		return inviteTarget{}, errors.New("invite has no available tenant targets")
	}

	accountID := numberValue(payload["accountId"])
	domain := strings.ToLower(strings.TrimSpace(stringValue(payload["domain"], "")))
	if accountID == 0 && domain == "" && len(targets) == 1 {
		return targets[0], nil
	}
	for _, target := range targets {
		if accountID > 0 && target.ID == accountID {
			if domain == "" || domain == target.Domain {
				return target, nil
			}
		}
		if domain != "" && target.Domain == domain {
			if accountID == 0 || accountID == target.ID {
				return target, nil
			}
		}
	}
	if len(targets) > 1 {
		return inviteTarget{}, errors.New("accountId or domain is required for this invite")
	}
	return inviteTarget{}, errors.New("selected account/domain is not allowed by this invite")
}

func publicInvitePayload(ctx context.Context, db *sql.DB, record inviteRecord, now time.Time) map[string]interface{} {
	payload := inviteToMap(record, now)
	accounts, err := loadAccounts(ctx, db)
	if err != nil {
		payload["targets"] = []map[string]interface{}{}
		return payload
	}
	targets := resolveInviteTargetsFromAccounts(record, accounts)
	targetItems := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		targetItems = append(targetItems, map[string]interface{}{
			"accountId":   target.ID,
			"accountName": target.Name,
			"domain":      target.Domain,
		})
	}
	payload["targets"] = targetItems
	payload["targetCount"] = len(targetItems)
	return payload
}

const inviteSelectSQL = `SELECT i.id, i.code, i.name, i.account_id, COALESCE(a.name, ''), COALESCE(i.account_ids, ''), i.domain, COALESCE(i.domains, ''), COALESCE(i.usage_location, ''), COALESCE(i.sku_ids, ''), COALESCE(i.max_uses, 1), COALESCE(i.used_count, 0), COALESCE(i.enabled, 1), COALESCE(i.force_change_password_next_sign_in, 0), COALESCE(i.batch_id, ''), COALESCE(i.expires_at, ''), i.created_at, i.updated_at
FROM m365_registration_invites i
LEFT JOIN m365_accounts a ON a.id = i.account_id`
