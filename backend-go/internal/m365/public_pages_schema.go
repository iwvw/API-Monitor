package m365

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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
