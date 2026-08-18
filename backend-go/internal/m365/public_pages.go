package m365

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func ensurePublicPageSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS m365_public_pages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			account_ids TEXT,
			domain TEXT NOT NULL,
			domains TEXT,
			usage_location TEXT,
			sku_ids TEXT,
			enabled INTEGER DEFAULT 1,
			force_change_password_next_sign_in INTEGER DEFAULT 0,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (account_id) REFERENCES m365_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_public_pages_account ON m365_public_pages(account_id)`,
		`CREATE TABLE IF NOT EXISTS m365_invite_codes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			public_page_id INTEGER NOT NULL,
			code TEXT NOT NULL UNIQUE,
			max_uses INTEGER DEFAULT 1,
			used_count INTEGER DEFAULT 0,
			enabled INTEGER DEFAULT 1,
			batch_id TEXT,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (public_page_id) REFERENCES m365_public_pages(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_invite_codes_page ON m365_invite_codes(public_page_id)`,
		`CREATE TABLE IF NOT EXISTS m365_public_page_registrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			public_page_id INTEGER,
			public_page_name TEXT NOT NULL,
			invite_code_id INTEGER,
			invite_code TEXT NOT NULL,
			account_id INTEGER NOT NULL,
			account_name TEXT,
			display_name TEXT NOT NULL,
			user_principal_name TEXT NOT NULL,
			graph_user_id TEXT,
			status TEXT NOT NULL,
			error_message TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_public_page_registrations_page ON m365_public_page_registrations(public_page_id)`,
		`CREATE INDEX IF NOT EXISTS idx_m365_public_page_registrations_code ON m365_public_page_registrations(invite_code_id)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure m365 public page schema: %w", err)
		}
	}
	return migrateLegacyPublicPages(ctx, db)
}

func migrateLegacyPublicPages(ctx context.Context, db *sql.DB) error {
	var pageCount int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM m365_public_pages`).Scan(&pageCount); err != nil {
		return err
	}
	if pageCount > 0 {
		return nil
	}

	legacyInvites, err := loadInvites(ctx, db)
	if err != nil {
		return nil
	}
	if len(legacyInvites) == 0 {
		return nil
	}

	mappings := map[int64]legacyInviteMapping{}
	groupOrder := []string{}
	grouped := map[string][]inviteRecord{}
	for _, item := range legacyInvites {
		key := strings.TrimSpace(item.BatchID)
		if key == "" {
			key = fmt.Sprintf("legacy-%d", item.ID)
		}
		if _, ok := grouped[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		grouped[key] = append(grouped[key], item)
	}

	for _, key := range groupOrder {
		items := grouped[key]
		if len(items) == 0 {
			continue
		}
		first := items[0]
		pageID, err := insertMigratedPublicPage(ctx, db, first)
		if err != nil {
			return err
		}
		for _, item := range items {
			codeID, err := insertMigratedInviteCode(ctx, db, pageID, item)
			if err != nil {
				return err
			}
			mappings[item.ID] = legacyInviteMapping{
				PublicPageID: pageID,
				InviteCodeID: codeID,
				InviteCode:   item.Code,
				PageName:     first.Name,
			}
		}
	}

	var registrationCount int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM m365_public_page_registrations`).Scan(&registrationCount); err != nil {
		return err
	}
	if registrationCount > 0 {
		return nil
	}

	legacyRegistrations, err := loadLegacyRegistrations(ctx, db)
	if err != nil {
		return nil
	}
	for _, item := range legacyRegistrations {
		mapping, ok := mappings[item.InviteID]
		if !ok {
			continue
		}
		if _, err := db.ExecContext(
			ctx,
			`INSERT INTO m365_public_page_registrations
			 (public_page_id, public_page_name, invite_code_id, invite_code, account_id, account_name, display_name, user_principal_name, graph_user_id, status, error_message, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			mapping.PublicPageID,
			mapping.PageName,
			mapping.InviteCodeID,
			mapping.InviteCode,
			item.AccountID,
			item.AccountName,
			item.DisplayName,
			item.UserPrincipalName,
			nullIfEmpty(item.GraphUserID),
			item.Status,
			nullIfEmpty(item.ErrorMessage),
			item.CreatedAt,
		); err != nil {
			return err
		}
	}

	return nil
}

func insertMigratedPublicPage(ctx context.Context, db *sql.DB, legacy inviteRecord) (int64, error) {
	accountIDsJSON, err := jsonString(legacy.AccountIDs)
	if err != nil {
		return 0, err
	}
	domainsJSON, err := jsonString(legacy.Domains)
	if err != nil {
		return 0, err
	}
	skuJSON, err := jsonString(legacy.SKUIDs)
	if err != nil {
		return 0, err
	}
	result, err := db.ExecContext(
		ctx,
		`INSERT INTO m365_public_pages
		 (name, account_id, account_ids, domain, domains, usage_location, sku_ids, enabled, force_change_password_next_sign_in, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		legacy.Name,
		legacy.AccountID,
		accountIDsJSON,
		legacy.Domain,
		domainsJSON,
		legacy.UsageLocation,
		skuJSON,
		boolToInt(legacy.Enabled),
		boolToInt(legacy.ForceChangePasswordNextSignIn),
		nullIfEmpty(legacy.ExpiresAt),
		legacy.CreatedAt,
		legacy.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func insertMigratedInviteCode(ctx context.Context, db *sql.DB, publicPageID int64, legacy inviteRecord) (int64, error) {
	lastUsedAt := ""
	if legacy.UsedCount > 0 {
		lastUsedAt = legacy.UpdatedAt
	}
	result, err := db.ExecContext(
		ctx,
		`INSERT INTO m365_invite_codes
		 (public_page_id, code, max_uses, used_count, enabled, batch_id, last_used_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publicPageID,
		legacy.Code,
		maxInt64(legacy.MaxUses, 1),
		legacy.UsedCount,
		boolToInt(legacy.Enabled),
		nullIfEmpty(legacy.BatchID),
		nullIfEmpty(lastUsedAt),
		legacy.CreatedAt,
		legacy.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func loadLegacyRegistrations(ctx context.Context, db *sql.DB) ([]legacyRegistrationRecord, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT r.id, r.invite_id, r.display_name, r.user_principal_name, COALESCE(r.graph_user_id, ''), r.status, COALESCE(r.error_message, ''), r.account_id, COALESCE(a.name, ''), r.created_at
		 FROM m365_registration_records r
		 LEFT JOIN m365_accounts a ON a.id = r.account_id
		 ORDER BY r.id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []legacyRegistrationRecord{}
	for rows.Next() {
		record := legacyRegistrationRecord{}
		if err := rows.Scan(
			&record.ID,
			&record.InviteID,
			&record.DisplayName,
			&record.UserPrincipalName,
			&record.GraphUserID,
			&record.Status,
			&record.ErrorMessage,
			&record.AccountID,
			&record.AccountName,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
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

func (s *Service) newPublicInvite(w http.ResponseWriter, r *http.Request, code string) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	record, err := loadInviteCodeByCode(r.Context(), db, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "invite code not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, publicInviteCodePayload(r.Context(), db, record, time.Now().UTC()))
}

func (s *Service) newPublicRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.newPublicRegisterDescriptor(w, r)
		return
	}

	payload, err := readObject(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.TrimSpace(stringValue(payload["code"], ""))
	batchID := strings.TrimSpace(stringValue(payload["batch"], ""))
	mailNickname := strings.TrimSpace(stringValue(payload["mailNickname"], ""))
	displayName := strings.TrimSpace(stringValue(payload["displayName"], mailNickname))
	if (code == "" && batchID == "") || mailNickname == "" {
		response.Error(w, http.StatusBadRequest, "code or batch and mailNickname are required")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	now := time.Now().UTC()
	inviteCode := inviteCodeRecord{}
	switch {
	case code != "":
		inviteCode, err = loadInviteCodeByCode(r.Context(), db, code)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite code not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if available, reason := evaluateInviteCodeAvailability(inviteCode, now); !available {
			response.Error(w, http.StatusBadRequest, "invite code unavailable: "+reason)
			return
		}
	case batchID != "":
		items, batchErr := loadInviteCodesByBatch(r.Context(), db, batchID)
		if batchErr != nil {
			if errors.Is(batchErr, sql.ErrNoRows) {
				response.Error(w, http.StatusNotFound, "invite batch not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, batchErr.Error())
			return
		}
		selected, available, reason := pickInviteCodeFromBatch(items, now)
		if !available {
			response.Error(w, http.StatusBadRequest, "invite batch unavailable: "+reason)
			return
		}
		inviteCode = selected
	}

	target, err := resolveInviteRegistrationTarget(r.Context(), db, inviteCodeToInviteRecord(inviteCode), payload)
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
	const maxPasswordAttempts = 5
	generatedPassword := ""
	created := map[string]interface{}{}
	var createErr error
	for attempt := 0; attempt < maxPasswordAttempts; attempt++ {
		generatedPassword, err = generateTemporaryPassword()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "generate temporary password: "+err.Error())
			return
		}
		created = map[string]interface{}{}
		body := map[string]interface{}{
			"accountEnabled":    true,
			"displayName":       displayName,
			"mailNickname":      mailNickname,
			"userPrincipalName": userPrincipalName,
			"passwordProfile": map[string]interface{}{
				"password":                      generatedPassword,
				"forceChangePasswordNextSignIn": inviteCode.ForceChangePasswordNextSignIn,
			},
		}
		body["usageLocation"] = resolvedInviteUsageLocation(inviteCode.UsageLocation)
		createErr = s.graphJSON(r.Context(), account, http.MethodPost, "/users", body, nil, &created)
		if createErr == nil || !isPasswordComplexityError(createErr) {
			break
		}
	}
	graphUserID := strings.TrimSpace(stringValue(created["id"], ""))
	status := "success"
	warning := ""
	errorMessage := ""
	if createErr != nil {
		status = "failed"
		errorMessage = createErr.Error()
	} else if len(inviteCode.SKUIDs) > 0 && graphUserID != "" {
		assignBody := map[string]interface{}{
			"addLicenses":    licenseAssignmentsFromIDs(inviteCode.SKUIDs),
			"removeLicenses": []string{},
		}
		if err := s.graphJSON(r.Context(), account, http.MethodPost, "/users/"+url.PathEscape(graphUserID)+"/assignLicense", assignBody, nil, nil); err != nil {
			status = "partial"
			errorMessage = err.Error()
			warning = err.Error()
		}
	}

	if persistErr := persistPublicPageRegistration(r.Context(), db, inviteCode, target.ID, target.Name, displayName, userPrincipalName, graphUserID, status, errorMessage); persistErr != nil {
		if errors.Is(persistErr, errInviteCodeExhausted) {
			// 并发竞争的第二个注册：邀请码额度已被对方占用
			response.Error(w, http.StatusConflict, "邀请码已被使用")
			return
		}
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
		"id":                            graphUserID,
		"status":                        status,
		"accountId":                     target.ID,
		"domain":                        target.Domain,
		"userPrincipalName":             userPrincipalName,
		"initialPassword":               generatedPassword,
		"forceChangePasswordNextSignIn": inviteCode.ForceChangePasswordNextSignIn,
		"warning":                       emptyToNil(warning),
	})
}

func (s *Service) newPublicRegisterDescriptor(w http.ResponseWriter, r *http.Request) {
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	payload := map[string]interface{}{
		"method":       "POST",
		"fields":       []string{"code", "batch", "mailNickname", "displayName", "accountId", "domain"},
		"passwordMode": "generated",
	}
	now := time.Now().UTC()
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		record, err := loadInviteCodeByCode(r.Context(), db, code)
		if err == nil {
			payload["invite"] = publicInviteCodePayload(r.Context(), db, record, now)
		}
	} else if batchID := strings.TrimSpace(r.URL.Query().Get("batch")); batchID != "" {
		items, err := loadInviteCodesByBatch(r.Context(), db, batchID)
		if err == nil {
			payload["invite"] = publicInviteBatchPayload(r.Context(), db, items, now)
		}
	}
	response.OK(w, payload)
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

func publicInviteCodePayload(ctx context.Context, db *sql.DB, record inviteCodeRecord, now time.Time) map[string]interface{} {
	payload := inviteCodeToMap(record, now)
	targets := resolveInviteTargetsFromAccounts(inviteCodeToInviteRecord(record), mustLoadAccounts(ctx, db))
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

func publicInviteBatchPayload(ctx context.Context, db *sql.DB, items []inviteCodeRecord, now time.Time) map[string]interface{} {
	if len(items) == 0 {
		return map[string]interface{}{
			"mode":           "batch",
			"inviteCount":    0,
			"usedCount":      0,
			"availableCount": 0,
			"available":      false,
		}
	}

	primary := items[0]
	payload := publicInviteCodePayload(ctx, db, primary, now)
	usedCount := int64(0)
	availableCount := int64(0)
	availabilityReason := ""
	for _, item := range items {
		if item.UsedCount > 0 {
			usedCount++
		}
		available, reason := evaluateInviteCodeAvailability(item, now)
		if available {
			availableCount++
		} else if availabilityReason == "" {
			availabilityReason = reason
		}
	}

	payload["mode"] = "batch"
	payload["batchId"] = emptyToNil(primary.BatchID)
	payload["inviteCount"] = len(items)
	payload["usedCount"] = usedCount
	payload["availableCount"] = availableCount
	payload["used"] = availableCount == 0
	payload["available"] = availableCount > 0
	payload["availabilityReason"] = emptyToNil(availabilityReason)
	delete(payload, "code")
	delete(payload, "id")
	return payload
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

func publicPageToInviteRecord(page publicPageRecord) inviteRecord {
	return inviteRecord{
		Name:                          page.Name,
		AccountID:                     page.AccountID,
		AccountName:                   page.AccountName,
		AccountIDs:                    page.AccountIDs,
		Domain:                        page.Domain,
		Domains:                       page.Domains,
		UsageLocation:                 page.UsageLocation,
		SKUIDs:                        page.SKUIDs,
		Enabled:                       page.Enabled,
		ForceChangePasswordNextSignIn: page.ForceChangePasswordNextSignIn,
		ExpiresAt:                     page.ExpiresAt,
		CreatedAt:                     page.CreatedAt,
		UpdatedAt:                     page.UpdatedAt,
	}
}

func inviteRecordToPublicPage(record inviteRecord, base publicPageRecord) publicPageRecord {
	return publicPageRecord{
		ID:                            base.ID,
		Name:                          record.Name,
		AccountID:                     record.AccountID,
		AccountName:                   record.AccountName,
		AccountIDs:                    record.AccountIDs,
		Domain:                        record.Domain,
		Domains:                       record.Domains,
		UsageLocation:                 record.UsageLocation,
		SKUIDs:                        record.SKUIDs,
		Enabled:                       record.Enabled,
		ForceChangePasswordNextSignIn: record.ForceChangePasswordNextSignIn,
		ExpiresAt:                     record.ExpiresAt,
		CreatedAt:                     base.CreatedAt,
		UpdatedAt:                     base.UpdatedAt,
		InviteCodeCount:               base.InviteCodeCount,
		UsedInviteCodeCount:           base.UsedInviteCodeCount,
	}
}

func inviteCodeToInviteRecord(record inviteCodeRecord) inviteRecord {
	return inviteRecord{
		ID:                            record.ID,
		Code:                          record.Code,
		Name:                          record.PublicPageName,
		AccountID:                     record.AccountID,
		AccountName:                   record.AccountName,
		AccountIDs:                    record.AccountIDs,
		Domain:                        record.Domain,
		Domains:                       record.Domains,
		UsageLocation:                 record.UsageLocation,
		SKUIDs:                        record.SKUIDs,
		MaxUses:                       record.MaxUses,
		UsedCount:                     record.UsedCount,
		Enabled:                       record.Enabled && record.PublicPageEnabled,
		ForceChangePasswordNextSignIn: record.ForceChangePasswordNextSignIn,
		BatchID:                       record.BatchID,
		ExpiresAt:                     record.PublicPageExpiresAt,
		CreatedAt:                     record.CreatedAt,
		UpdatedAt:                     record.UpdatedAt,
	}
}

func mustLoadAccounts(ctx context.Context, db *sql.DB) []accountRecord {
	items, err := loadAccounts(ctx, db)
	if err != nil {
		return []accountRecord{}
	}
	return items
}

func jsonString(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// errInviteCodeExhausted 表示并发注册竞争下邀请码额度已被另一方占用。
var errInviteCodeExhausted = errors.New("invite code exhausted")

const publicPageSelectSQL = `SELECT p.id, p.name, p.account_id, COALESCE(a.name, ''), COALESCE(p.account_ids, ''), p.domain, COALESCE(p.domains, ''), COALESCE(p.usage_location, ''), COALESCE(p.sku_ids, ''), COALESCE(p.enabled, 1), COALESCE(p.force_change_password_next_sign_in, 0), COALESCE(p.expires_at, ''), p.created_at, p.updated_at, COALESCE(stats.code_count, 0), COALESCE(stats.used_code_count, 0)
FROM m365_public_pages p
LEFT JOIN m365_accounts a ON a.id = p.account_id
LEFT JOIN (
	SELECT public_page_id, COUNT(*) AS code_count, SUM(CASE WHEN used_count > 0 THEN 1 ELSE 0 END) AS used_code_count
	FROM m365_invite_codes
	GROUP BY public_page_id
) stats ON stats.public_page_id = p.id`

const inviteCodeSelectSQL = `SELECT c.id, c.public_page_id, COALESCE(p.name, ''), c.code, COALESCE(c.max_uses, 1), COALESCE(c.used_count, 0), COALESCE(c.enabled, 1), COALESCE(c.batch_id, ''), COALESCE(c.last_used_at, ''), c.created_at, c.updated_at, p.account_id, COALESCE(a.name, ''), COALESCE(p.account_ids, ''), p.domain, COALESCE(p.domains, ''), COALESCE(p.usage_location, ''), COALESCE(p.sku_ids, ''), COALESCE(p.enabled, 1), COALESCE(p.force_change_password_next_sign_in, 0), COALESCE(p.expires_at, '')
FROM m365_invite_codes c
JOIN m365_public_pages p ON p.id = c.public_page_id
LEFT JOIN m365_accounts a ON a.id = p.account_id`
