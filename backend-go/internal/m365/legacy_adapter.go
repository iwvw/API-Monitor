package m365

import (
	"context"
	"database/sql"
	"encoding/json"
)

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
