package assets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// 纳管来源：以只读 SELECT 读取已有模块的对象，不依赖其表外键，也不写回来源模块。
// 这与 docs/guides/新模块接入指南.md 的「跨模块通过明确服务调用协调」一致：
// 这里只做只读快照读取，来源对象的生命周期仍由来源模块负责。
//
// 到期口径归一：各来源格式不一（RFC3339 / DATETIME / 纯日期 / Unix 时间戳），
// 统一经 normalizeExpireAt 归一为 UTC RFC3339。
//
// 不纳入的来源：云厂商账号表（cf/aliyun/tencent/huawei/oracle/gcp/m365）与域名表
// （aliyun_domains/tencent_domains）的表内没有到期列，到期时间只存在于实时 API
// 返回值中，读表无法取得，故不提供候选；这类对象仍可手工登记为资产。

type SourceCandidate struct {
	SourceModule   string   `json:"source_module"`
	SourceRefID    string   `json:"source_ref_id"`
	Name           string   `json:"name"`
	Provider       string   `json:"provider"`
	AssetType      string   `json:"asset_type"`
	Category       string   `json:"category"`
	ExpireAt       string   `json:"expire_at"`
	Tags           []string `json:"tags"`
	Remark         string   `json:"remark"`
	Linked         bool     `json:"linked"`
	LinkedAssetID  string   `json:"linked_asset_id,omitempty"`
}

type sourceGroup struct {
	Module    string            `json:"module"`
	Label     string            `json:"label"`
	AssetType string            `json:"asset_type"`
	Category  string            `json:"category"`
	Items     []SourceCandidate `json:"items"`
}

type sourceDescriptor struct {
	Module    string
	Label     string
	AssetType string
	Category  string
	Load      func(ctx context.Context, db *sql.DB) ([]SourceCandidate, error)
}

func sourceDescriptors() []sourceDescriptor {
	return []sourceDescriptor{
		// server_accounts 是面板纳管的云/远程主机（法兰克福、伦敦等 VPS 与实例），
		// 语义上是虚拟资产而非物理硬件，故归 cloud_instance / virtual。
		// 真正的物理设备（网络设备、存储、终端、线下机柜）由用户手工登记到 physical。
		{"server_accounts", "主机实例", "cloud_instance", categoryVirtual, loadServerAccounts},
		{"subscription_subscriptions", "订阅套餐", "subscription", categoryVirtual, loadSubscriptions},
		{"uptime_monitor_states", "SSL 证书", "ssl_cert", categoryVirtual, loadSSLMonitors},
		{"openai_gateway_keys", "模型网关 Key", "api_key", categoryVirtual, loadOpenAIKeys},
		{"api_access_keys", "集中访问密钥", "api_key", categoryVirtual, loadAccessKeys},
		{"aiagent_tokens", "Agent 令牌", "api_key", categoryVirtual, loadAgentTokens},
		{"managed_proxy_nodes", "托管代理节点", "proxy_node", categoryVirtual, loadProxyNodes},
	}
}

// LoadCandidates 汇总所有可读来源，并标注哪些已被纳管。
func (s *Service) LoadCandidates(ctx context.Context) ([]sourceGroup, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	linked, err := s.linkedRefs(ctx, db)
	if err != nil {
		return nil, err
	}

	groups := []sourceGroup{}
	for _, descriptor := range sourceDescriptors() {
		items, err := descriptor.Load(ctx, db)
		if err != nil {
			// 单个来源读取失败不影响其他来源，跳过并继续。
			continue
		}
		for i := range items {
			key := items[i].SourceModule + "\x00" + items[i].SourceRefID
			if assetID, ok := linked[key]; ok {
				items[i].Linked = true
				items[i].LinkedAssetID = assetID
			}
		}
		groups = append(groups, sourceGroup{
			Module:    descriptor.Module,
			Label:     descriptor.Label,
			AssetType: descriptor.AssetType,
			Category:  descriptor.Category,
			Items:     items,
		})
	}
	return groups, nil
}

func (s *Service) linkedRefs(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT source_module, source_ref_id, asset_id FROM asset_links")
	if err != nil {
		return nil, fmt.Errorf("load asset links: %w", err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var module, refID, assetID string
		if err := rows.Scan(&module, &refID, &assetID); err != nil {
			return nil, err
		}
		result[module+"\x00"+refID] = assetID
	}
	return result, rows.Err()
}

// LinkInput 描述一条纳管请求。
type LinkInput struct {
	SourceModule string `json:"source_module"`
	SourceRefID  string `json:"source_ref_id"`
}

// LinkResult 描述纳管结果。
type LinkResult struct {
	SourceModule string `json:"source_module"`
	SourceRefID  string `json:"source_ref_id"`
	AssetID      string `json:"asset_id,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
}

// LinkAssets 批量纳管：去重键为 (source_module, source_ref_id)，重复纳管返回 skipped。
func (s *Service) LinkAssets(ctx context.Context, inputs []LinkInput) ([]LinkResult, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: no links provided", errInvalidInput)
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	descriptors := map[string]sourceDescriptor{}
	for _, descriptor := range sourceDescriptors() {
		descriptors[descriptor.Module] = descriptor
	}

	results := make([]LinkResult, 0, len(inputs))
	for _, input := range inputs {
		module := strings.TrimSpace(input.SourceModule)
		refID := strings.TrimSpace(input.SourceRefID)
		result := LinkResult{SourceModule: module, SourceRefID: refID}
		if module == "" || refID == "" {
			result.Status = "invalid"
			result.Message = "缺少来源模块或来源 ID"
			results = append(results, result)
			continue
		}
		descriptor, ok := descriptors[module]
		if !ok {
			result.Status = "invalid"
			result.Message = "不支持的来源模块"
			results = append(results, result)
			continue
		}

		existing, err := s.findLinkedAssetID(ctx, db, module, refID)
		if err != nil {
			return nil, err
		}
		if existing != "" {
			result.Status = "skipped"
			result.AssetID = existing
			result.Message = "已纳管"
			results = append(results, result)
			continue
		}

		items, err := descriptor.Load(ctx, db)
		if err != nil {
			result.Status = "failed"
			result.Message = "读取来源失败"
			results = append(results, result)
			continue
		}
		var source *SourceCandidate
		for i := range items {
			if items[i].SourceRefID == refID {
				source = &items[i]
				break
			}
		}
		if source == nil {
			result.Status = "missing"
			result.Message = "来源对象不存在"
			results = append(results, result)
			continue
		}

		asset, err := s.insertLinkedAssetTx(ctx, db, descriptor, *source, module, refID)
		if err != nil {
			result.Status = "failed"
			result.Message = err.Error()
			results = append(results, result)
			continue
		}
		_ = s.recordEvent(ctx, asset.ID, "linked", source.Name)
		result.Status = "linked"
		result.AssetID = asset.ID
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) findLinkedAssetID(ctx context.Context, db *sql.DB, module, refID string) (string, error) {
	var assetID string
	err := db.QueryRowContext(ctx,
		"SELECT asset_id FROM asset_links WHERE source_module = ? AND source_ref_id = ?",
		module, refID).Scan(&assetID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return assetID, nil
}

// insertLinkedAssetTx 在单个事务里写入 assets 行与 asset_links 行。
// 两步必须原子：若 link 写入失败而 assets 行已落库，会留下没有来源映射的
// 孤儿资产，重试时 findLinkedAssetID 查不到 link，会再次插入新行持续累积。
func (s *Service) insertLinkedAssetTx(ctx context.Context, db *sql.DB, descriptor sourceDescriptor, source SourceCandidate, module, refID string) (Asset, error) {
	assetID, err := randomID("asset")
	if err != nil {
		return Asset{}, err
	}
	linkID, err := randomID("link")
	if err != nil {
		return Asset{}, err
	}
	name := source.Name
	if strings.TrimSpace(name) == "" {
		name = source.SourceRefID
	}
	tags := source.Tags
	if tags == nil {
		tags = []string{}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Asset{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO assets (
		id, origin, category, asset_type, name, provider, status,
		source_module, source_ref_id, source_synced_at,
		expire_at, warn_days_json, tags_json, metadata_json, remark
	) VALUES (?, 'linked', ?, ?, ?, ?, 'active', ?, ?, CURRENT_TIMESTAMP, ?, '[]', ?, '{}', ?)`,
		assetID, descriptor.Category, descriptor.AssetType, name, source.Provider,
		source.SourceModule, source.SourceRefID, normalizeExpireAt(source.ExpireAt, time.UTC),
		jsonString(tags), source.Remark,
	); err != nil {
		return Asset{}, fmt.Errorf("insert linked asset: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO asset_links (id, asset_id, source_module, source_ref_id) VALUES (?, ?, ?, ?)",
		linkID, assetID, module, refID); err != nil {
		return Asset{}, fmt.Errorf("insert asset link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Asset{}, err
	}
	asset, _, err := s.LoadAsset(ctx, assetID)
	return asset, err
}

// RefreshAsset 重新读取纳管来源并更新快照；来源消失时标记为 orphan 并记录事件。
func (s *Service) RefreshAsset(ctx context.Context, id string) (Asset, error) {
	asset, ok, err := s.LoadAsset(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if !ok {
		return Asset{}, sql.ErrNoRows
	}
	if asset.Origin != "linked" || asset.SourceModule == "" {
		return asset, fmt.Errorf("%w: asset is not linked", errInvalidInput)
	}

	db, err := s.open(ctx)
	if err != nil {
		return Asset{}, err
	}
	defer db.Close()

	var descriptor *sourceDescriptor
	for _, candidate := range sourceDescriptors() {
		if candidate.Module == asset.SourceModule {
			descriptor = &candidate
			break
		}
	}
	if descriptor == nil {
		return asset, fmt.Errorf("%w: unsupported source module", errInvalidInput)
	}

	items, err := descriptor.Load(ctx, db)
	if err != nil {
		return Asset{}, err
	}
	var source *SourceCandidate
	for i := range items {
		if items[i].SourceRefID == asset.SourceRefID {
			source = &items[i]
			break
		}
	}

	if source == nil {
		if asset.Status != statusOrphan {
			if _, err := db.ExecContext(ctx,
				"UPDATE assets SET status = ?, source_synced_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
				statusOrphan, id); err != nil {
				return Asset{}, err
			}
			_ = s.recordEvent(ctx, id, "source_lost", asset.Name)
		}
		refreshed, _, err := s.LoadAsset(ctx, id)
		return refreshed, err
	}

	name := source.Name
	if strings.TrimSpace(name) == "" {
		name = asset.Name
	}
	tags := source.Tags
	if tags == nil {
		tags = []string{}
	}
	status := asset.Status
	if status == statusOrphan {
		status = statusActive
	}
	if _, err := db.ExecContext(ctx, `UPDATE assets SET
		name = ?, provider = ?, status = ?, expire_at = ?, tags_json = ?,
		source_synced_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		name, source.Provider, status, normalizeExpireAt(source.ExpireAt, time.UTC), jsonString(tags), id,
	); err != nil {
		return Asset{}, err
	}
	if asset.Status == statusOrphan {
		_ = s.recordEvent(ctx, id, "source_restored", name)
	} else {
		_ = s.recordEvent(ctx, id, "refreshed", name)
	}
	refreshed, _, err := s.LoadAsset(ctx, id)
	return refreshed, err
}

// RefreshAllLinked 刷新全部纳管资产，返回成功与失败计数。
func (s *Service) RefreshAllLinked(ctx context.Context) (int, int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		"SELECT id FROM assets WHERE origin = 'linked'")
	if err != nil {
		return 0, 0, fmt.Errorf("list linked assets: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	success, failed := 0, 0
	for _, id := range ids {
		if _, err := s.RefreshAsset(ctx, id); err != nil {
			failed++
			continue
		}
		success++
	}
	return success, failed, nil
}

// ---------- 各来源读取实现 ----------

func loadServerAccounts(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, host, status, expires_at, tags, description FROM server_accounts")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, name, host, status, description string
		var expiresAt, tags sql.NullString
		if err := rows.Scan(&id, &name, &host, &status, &expiresAt, &tags, &description); err != nil {
			return nil, err
		}
		provider := host
		items = append(items, SourceCandidate{
			SourceModule: "server_accounts",
			SourceRefID:  id,
			Name:         name,
			Provider:     provider,
			AssetType:    "cloud_instance",
			Category:     categoryVirtual,
			ExpireAt:     expiresAt.String,
			Tags:         parseStringList(tags.String),
			Remark:       description,
		})
	}
	return items, rows.Err()
}

func loadSubscriptions(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, expire_at, remark, enabled FROM subscription_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, name string
		var expireAt, remark sql.NullString
		var enabled sql.NullInt64
		if err := rows.Scan(&id, &name, &expireAt, &remark, &enabled); err != nil {
			return nil, err
		}
		if enabled.Valid && enabled.Int64 == 0 {
			continue
		}
		items = append(items, SourceCandidate{
			SourceModule: "subscription_subscriptions",
			SourceRefID:  id,
			Name:         name,
			AssetType:    "subscription",
			Category:     categoryVirtual,
			ExpireAt:     expireAt.String,
			Remark:       remark.String,
		})
	}
	return items, rows.Err()
}

func loadSSLMonitors(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.name, m.url, m.active, s.ssl_expiry
		FROM uptime_monitors m
		LEFT JOIN uptime_monitor_states s ON s.monitor_id = m.id
		WHERE s.ssl_expiry IS NOT NULL AND s.ssl_expiry != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id int64
		var name, url string
		var active sql.NullInt64
		var sslExpiry sql.NullString
		if err := rows.Scan(&id, &name, &url, &active, &sslExpiry); err != nil {
			return nil, err
		}
		if active.Valid && active.Int64 == 0 {
			continue
		}
		items = append(items, SourceCandidate{
			SourceModule: "uptime_monitor_states",
			SourceRefID:  fmt.Sprint(id),
			Name:         name,
			Provider:     url,
			AssetType:    "ssl_cert",
			Category:     categoryVirtual,
			ExpireAt:     sslExpiry.String,
		})
	}
	return items, rows.Err()
}

func loadOpenAIKeys(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, expires_at, enabled FROM openai_gateway_keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, name string
		var expiresAt sql.NullString
		var enabled sql.NullInt64
		if err := rows.Scan(&id, &name, &expiresAt, &enabled); err != nil {
			return nil, err
		}
		if enabled.Valid && enabled.Int64 == 0 {
			continue
		}
		items = append(items, SourceCandidate{
			SourceModule: "openai_gateway_keys",
			SourceRefID:  id,
			Name:         name,
			Provider:     "模型网关",
			AssetType:    "api_key",
			Category:     categoryVirtual,
			ExpireAt:     expiresAt.String,
		})
	}
	return items, rows.Err()
}

func loadAccessKeys(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, expires_at, enabled, revoked_at FROM api_access_keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, name string
		var expiresAt, revokedAt sql.NullString
		var enabled sql.NullInt64
		if err := rows.Scan(&id, &name, &expiresAt, &enabled, &revokedAt); err != nil {
			return nil, err
		}
		if (enabled.Valid && enabled.Int64 == 0) || (revokedAt.Valid && revokedAt.String != "") {
			continue
		}
		items = append(items, SourceCandidate{
			SourceModule: "api_access_keys",
			SourceRefID:  id,
			Name:         name,
			Provider:     "集中密钥",
			AssetType:    "api_key",
			Category:     categoryVirtual,
			ExpireAt:     expiresAt.String,
		})
	}
	return items, rows.Err()
}

func loadAgentTokens(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, device_label, token_prefix, expires_at, revoked_at FROM aiagent_tokens")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, deviceLabel, tokenPrefix string
		var expiresAt, revokedAt sql.NullString
		if err := rows.Scan(&id, &deviceLabel, &tokenPrefix, &expiresAt, &revokedAt); err != nil {
			return nil, err
		}
		if revokedAt.Valid && revokedAt.String != "" {
			continue
		}
		name := strings.TrimSpace(deviceLabel)
		if name == "" {
			name = tokenPrefix
		}
		items = append(items, SourceCandidate{
			SourceModule: "aiagent_tokens",
			SourceRefID:  id,
			Name:         name,
			Provider:     "AI Agent",
			AssetType:    "api_key",
			Category:     categoryVirtual,
			ExpireAt:     expiresAt.String,
		})
	}
	return items, rows.Err()
}

func loadProxyNodes(ctx context.Context, db *sql.DB) ([]SourceCandidate, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.id, n.name, n.enabled, n.apply_status, a.name
		FROM managed_proxy_nodes n
		LEFT JOIN server_accounts a ON a.id = n.server_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourceCandidate{}
	for rows.Next() {
		var id, name, applyStatus string
		var enabled sql.NullInt64
		var serverName sql.NullString
		if err := rows.Scan(&id, &name, &enabled, &applyStatus, &serverName); err != nil {
			return nil, err
		}
		if enabled.Valid && enabled.Int64 == 0 {
			continue
		}
		items = append(items, SourceCandidate{
			SourceModule: "managed_proxy_nodes",
			SourceRefID:  id,
			Name:         name,
			Provider:     serverName.String,
			AssetType:    "proxy_node",
			Category:     categoryVirtual,
			Remark:       applyStatus,
		})
	}
	return items, rows.Err()
}
