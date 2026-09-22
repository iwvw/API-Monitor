package assets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

const assetColumns = `id, origin, category, asset_type, name, provider, owner, location,
	serial_no, model, status, source_module, source_ref_id, source_synced_at,
	acquire_date, expire_at, warn_days_json, auto_renew, cost_amount, cost_currency,
	cost_cycle, tags_json, metadata_json, remark, created_at, updated_at`

type assetFilter struct {
	Category       string
	AssetType      string
	Status         string
	Provider       string
	Tag            string
	Query          string
	ExpiringWithin int
	Limit          int
	Offset         int
}

func (s *Service) LoadAssets(ctx context.Context, filter assetFilter) ([]Asset, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	conditions := []string{}
	args := []interface{}{}
	if filter.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, filter.Category)
	}
	if filter.AssetType != "" {
		conditions = append(conditions, "asset_type = ?")
		args = append(args, filter.AssetType)
	}
	if filter.Provider != "" {
		conditions = append(conditions, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Query != "" {
		conditions = append(conditions, "(name LIKE ? OR provider LIKE ? OR owner LIKE ? OR remark LIKE ?)")
		like := "%" + filter.Query + "%"
		args = append(args, like, like, like, like)
	}
	if filter.Tag != "" {
		conditions = append(conditions, "tags_json LIKE ?")
		args = append(args, "%\""+filter.Tag+"\"%")
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	query := "SELECT " + assetColumns + " FROM assets" + where + " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	defer rows.Close()

	assets := []Asset{}
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	loc := timeutil.LocationFromSettings(ctx, db)
	now := nowUTC()

	filtered := assets[:0]
	for i := range assets {
		applyDerived(&assets[i], now, loc)
		if !matchesPostFilter(assets[i], filter) {
			continue
		}
		filtered = append(filtered, assets[i])
	}
	return filtered, nil
}

// matchesPostFilter 处理需要派生状态才能判定的筛选：status 与 expiring_within。
func matchesPostFilter(asset Asset, filter assetFilter) bool {
	if filter.Status != "" && asset.DerivedStatus != filter.Status {
		return false
	}
	if filter.ExpiringWithin > 0 {
		if asset.DaysLeft == nil || *asset.DaysLeft > filter.ExpiringWithin {
			return false
		}
	}
	return true
}

func (s *Service) LoadAsset(ctx context.Context, id string) (Asset, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Asset{}, false, err
	}
	defer db.Close()

	row := db.QueryRowContext(ctx, "SELECT "+assetColumns+" FROM assets WHERE id = ?", id)
	asset, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return Asset{}, false, nil
	}
	if err != nil {
		return Asset{}, false, err
	}
	loc := timeutil.LocationFromSettings(ctx, db)
	applyDerived(&asset, nowUTC(), loc)
	return asset, true, nil
}

func (s *Service) CreateAsset(ctx context.Context, payload map[string]interface{}) (Asset, error) {
	name := trimmedString(payload["name"])
	category := trimmedString(payload["category"])
	assetType := trimmedString(payload["asset_type"])
	if name == "" || category == "" || assetType == "" {
		return Asset{}, fmt.Errorf("%w: missing required parameters", errInvalidInput)
	}
	if !validCategory(category) {
		return Asset{}, fmt.Errorf("%w: unsupported category", errInvalidInput)
	}
	if !validTypeForCategory(category, assetType) {
		return Asset{}, fmt.Errorf("%w: asset_type does not belong to category", errInvalidInput)
	}
	cycle := normalizeCycle(trimmedString(payload["cost_cycle"]))
	if raw := trimmedString(payload["cost_cycle"]); raw != "" && cycle == "" {
		return Asset{}, fmt.Errorf("%w: unsupported cost_cycle", errInvalidInput)
	}

	id, err := randomID("asset")
	if err != nil {
		return Asset{}, err
	}
	db, err := s.open(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `INSERT INTO assets (
		id, origin, category, asset_type, name, provider, owner, location, serial_no, model,
		status, source_module, source_ref_id, source_synced_at, acquire_date, expire_at,
		warn_days_json, auto_renew, cost_amount, cost_currency, cost_cycle,
		tags_json, metadata_json, remark
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "manual", category, assetType, name,
		trimmedString(payload["provider"]), trimmedString(payload["owner"]), trimmedString(payload["location"]),
		trimmedString(payload["serial_no"]), trimmedString(payload["model"]),
		normalizeAssetStatus(trimmedString(payload["status"])),
		"", "", nil,
		trimmedString(payload["acquire_date"]), normalizeExpireAt(trimmedString(payload["expire_at"])),
		jsonString(warnDaysFromPayload(payload["warn_days"])), boolInt(boolValue(payload["auto_renew"], false)),
		numberValue(payload["cost_amount"], 0), normalizeCurrency(trimmedString(payload["cost_currency"])), cycle,
		jsonString(stringListFromPayload(payload["tags"])), jsonString(objectFromPayload(payload["metadata"])),
		trimmedString(payload["remark"]),
	)
	if err != nil {
		return Asset{}, fmt.Errorf("create asset: %w", err)
	}

	asset, ok, err := s.LoadAsset(ctx, id)
	if err != nil || !ok {
		return Asset{}, err
	}
	_ = s.recordEvent(ctx, id, "created", name)
	return asset, nil
}

func (s *Service) UpdateAsset(ctx context.Context, id string, payload map[string]interface{}) error {
	existing, ok, err := s.LoadAsset(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return sql.ErrNoRows
	}

	updates := []string{}
	args := []interface{}{}
	set := func(column string, value interface{}) {
		updates = append(updates, column+" = ?")
		args = append(args, value)
	}

	if _, has := payload["name"]; has {
		name := trimmedString(payload["name"])
		if name == "" {
			return fmt.Errorf("%w: name cannot be empty", errInvalidInput)
		}
		set("name", name)
	}
	if _, has := payload["category"]; has {
		category := trimmedString(payload["category"])
		if !validCategory(category) {
			return fmt.Errorf("%w: unsupported category", errInvalidInput)
		}
		set("category", category)
	}
	if _, has := payload["asset_type"]; has {
		assetType := trimmedString(payload["asset_type"])
		category := existing.Category
		if raw, ok := payload["category"]; ok {
			category = trimmedString(raw)
		}
		if !validTypeForCategory(category, assetType) {
			return fmt.Errorf("%w: asset_type does not belong to category", errInvalidInput)
		}
		set("asset_type", assetType)
	}
	if _, has := payload["provider"]; has {
		set("provider", trimmedString(payload["provider"]))
	}
	if _, has := payload["owner"]; has {
		set("owner", trimmedString(payload["owner"]))
	}
	if _, has := payload["location"]; has {
		set("location", trimmedString(payload["location"]))
	}
	if _, has := payload["serial_no"]; has {
		set("serial_no", trimmedString(payload["serial_no"]))
	}
	if _, has := payload["model"]; has {
		set("model", trimmedString(payload["model"]))
	}
	if _, has := payload["status"]; has {
		set("status", normalizeAssetStatus(trimmedString(payload["status"])))
	}
	if _, has := payload["acquire_date"]; has {
		set("acquire_date", trimmedString(payload["acquire_date"]))
	}
	if _, has := payload["expire_at"]; has {
		set("expire_at", normalizeExpireAt(trimmedString(payload["expire_at"])))
	}
	if _, has := payload["warn_days"]; has {
		set("warn_days_json", jsonString(warnDaysFromPayload(payload["warn_days"])))
	}
	if _, has := payload["auto_renew"]; has {
		set("auto_renew", boolInt(boolValue(payload["auto_renew"], false)))
	}
	if _, has := payload["cost_amount"]; has {
		set("cost_amount", numberValue(payload["cost_amount"], 0))
	}
	if _, has := payload["cost_currency"]; has {
		set("cost_currency", normalizeCurrency(trimmedString(payload["cost_currency"])))
	}
	if _, has := payload["cost_cycle"]; has {
		raw := trimmedString(payload["cost_cycle"])
		cycle := normalizeCycle(raw)
		if raw != "" && cycle == "" {
			return fmt.Errorf("%w: unsupported cost_cycle", errInvalidInput)
		}
		set("cost_cycle", cycle)
	}
	if _, has := payload["tags"]; has {
		set("tags_json", jsonString(stringListFromPayload(payload["tags"])))
	}
	if _, has := payload["metadata"]; has {
		set("metadata_json", jsonString(objectFromPayload(payload["metadata"])))
	}
	if _, has := payload["remark"]; has {
		set("remark", trimmedString(payload["remark"]))
	}

	if len(updates) == 0 {
		return nil
	}
	updates = append(updates, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "UPDATE assets SET "+strings.Join(updates, ", ")+" WHERE id = ?", args...); err != nil {
		return fmt.Errorf("update asset: %w", err)
	}
	_ = s.recordEvent(ctx, id, "updated", "")
	return nil
}

func (s *Service) DeleteAsset(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DELETE FROM assets WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM asset_links WHERE asset_id = ?", id)
	_, _ = db.ExecContext(ctx, "DELETE FROM asset_events WHERE asset_id = ?", id)
	return nil
}

func (s *Service) recordEvent(ctx context.Context, assetID, eventType, detail string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx,
		"INSERT INTO asset_events (asset_id, event_type, detail) VALUES (?, ?, ?)",
		assetID, eventType, detail)
	return err
}

func (s *Service) LoadEvents(ctx context.Context, assetID string, limit int) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		"SELECT id, event_type, detail, created_at FROM asset_events WHERE asset_id = ? ORDER BY id DESC LIMIT ?",
		assetID, limit)
	if err != nil {
		return nil, fmt.Errorf("list asset events: %w", err)
	}
	defer rows.Close()
	events := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var eventType, detail, createdAt string
		if err := rows.Scan(&id, &eventType, &detail, &createdAt); err != nil {
			return nil, err
		}
		events = append(events, map[string]interface{}{
			"id":         id,
			"event_type": eventType,
			"detail":     detail,
			"created_at": createdAt,
		})
	}
	return events, rows.Err()
}

// LoadAlerts 返回资产的告警去重标记（用于详情页展示已触发的档位）。
func (s *Service) LoadAlerts(ctx context.Context, assetID string) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		"SELECT marker, created_at FROM asset_alerts WHERE asset_id = ? ORDER BY created_at DESC",
		assetID)
	if err != nil {
		return nil, fmt.Errorf("list asset alerts: %w", err)
	}
	defer rows.Close()
	alerts := []map[string]interface{}{}
	for rows.Next() {
		var marker, createdAt string
		if err := rows.Scan(&marker, &createdAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, map[string]interface{}{
			"marker":     marker,
			"created_at": createdAt,
		})
	}
	return alerts, rows.Err()
}

func (s *Service) LoadSettings(ctx context.Context) (Settings, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer db.Close()

	var baseCurrency, ratesJSON, warnJSON string
	err = db.QueryRowContext(ctx,
		"SELECT base_currency, exchange_rates_json, warn_days_json FROM asset_settings WHERE id = 1").
		Scan(&baseCurrency, &ratesJSON, &warnJSON)
	if err == sql.ErrNoRows {
		return Settings{
			BaseCurrency:  "",
			ExchangeRates: map[string]float64{},
			WarnDays:      append([]int{}, defaultWarnDays...),
		}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("load asset settings: %w", err)
	}
	warnDays := parseWarnDays(warnJSON)
	if len(warnDays) == 0 {
		warnDays = append([]int{}, defaultWarnDays...)
	}
	return Settings{
		BaseCurrency:  baseCurrency,
		ExchangeRates: parseFloatMap(ratesJSON),
		WarnDays:      warnDays,
	}, nil
}

func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	warnDays := settings.WarnDays
	if len(warnDays) == 0 {
		warnDays = defaultWarnDays
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO asset_settings (id, base_currency, exchange_rates_json, warn_days_json, updated_at)
		VALUES (1, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			base_currency = excluded.base_currency,
			exchange_rates_json = excluded.exchange_rates_json,
			warn_days_json = excluded.warn_days_json,
			updated_at = CURRENT_TIMESTAMP
	`, normalizeCurrency(settings.BaseCurrency), jsonString(settings.ExchangeRates), jsonString(warnDays))
	if err != nil {
		return fmt.Errorf("save asset settings: %w", err)
	}
	return nil
}

func (s *Service) loadAllForStats(ctx context.Context) ([]Asset, Settings, error) {
	settings, err := s.LoadSettings(ctx)
	if err != nil {
		return nil, Settings{}, err
	}
	assets, err := s.LoadAssets(ctx, assetFilter{Limit: maxLimit})
	if err != nil {
		return nil, Settings{}, err
	}
	return assets, settings, nil
}

func scanAsset(scanner interface {
	Scan(dest ...interface{}) error
}) (Asset, error) {
	var asset Asset
	var sourceSyncedAt sql.NullString
	var warnJSON, tagsJSON, metadataJSON, createdAt, updatedAt sql.NullString
	var autoRenew int
	var costAmount sql.NullFloat64

	err := scanner.Scan(
		&asset.ID, &asset.Origin, &asset.Category, &asset.AssetType, &asset.Name,
		&asset.Provider, &asset.Owner, &asset.Location, &asset.SerialNo, &asset.Model,
		&asset.Status, &asset.SourceModule, &asset.SourceRefID, &sourceSyncedAt,
		&asset.AcquireDate, &asset.ExpireAt, &warnJSON, &autoRenew,
		&costAmount, &asset.CostCurrency, &asset.CostCycle,
		&tagsJSON, &metadataJSON, &asset.Remark, &createdAt, &updatedAt,
	)
	if err != nil {
		return Asset{}, err
	}
	asset.SourceSyncedAt = nullableStringPtr(sourceSyncedAt)
	asset.WarnDays = parseWarnDays(warnJSON.String)
	asset.AutoRenew = autoRenew != 0
	if costAmount.Valid {
		asset.CostAmount = costAmount.Float64
	}
	asset.Tags = parseStringList(tagsJSON.String)
	asset.Metadata = parseObject(metadataJSON.String)
	asset.CreatedAt = createdAt.String
	asset.UpdatedAt = updatedAt.String
	return asset, nil
}
