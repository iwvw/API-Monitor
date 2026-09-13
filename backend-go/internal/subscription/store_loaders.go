package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func loadTemplates(ctx context.Context, db *sql.DB) ([]Template, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, format, content, builtin, is_default, COALESCE(description, ''), created_at, updated_at FROM subscription_templates ORDER BY is_default DESC, builtin DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Template{}
	for rows.Next() {
		var tpl Template
		var builtin, def int
		if err := rows.Scan(&tpl.ID, &tpl.Name, &tpl.Format, &tpl.Content, &builtin, &def, &tpl.Description, &tpl.CreatedAt, &tpl.UpdatedAt); err != nil {
			return nil, err
		}
		tpl.Builtin = builtin == 1
		tpl.IsDefault = def == 1
		if validationErr := validateTemplateDefinition(tpl.Format, tpl.Content); validationErr != nil {
			tpl.ValidationError = validationErr.Error()
		} else {
			tpl.Valid = true
		}
		items = append(items, tpl)
	}
	return items, rows.Err()
}

func validateTemplateReference(ctx context.Context, db *sql.DB, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("请选择输出模板")
	}
	var format, content string
	if err := db.QueryRowContext(ctx, `SELECT format,content FROM subscription_templates WHERE id=?`, id).Scan(&format, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			switch id {
			case defaultTemplateID:
				format, content = "clash", loadDefaultMihomoTemplate()
			case rawTemplateID:
				format, content = "raw", "{{ raw_uri_list }}"
			case base64TemplateID:
				format, content = "base64", "{{ raw_uri_list }}"
			default:
				return errors.New("所选模板不存在")
			}
		} else {
			return fmt.Errorf("读取模板失败: %w", err)
		}
	}
	if err := validateTemplateDefinition(format, content); err != nil {
		return fmt.Errorf("所选模板配置无效: %w", err)
	}
	return nil
}

func loadProfiles(ctx context.Context, db *sql.DB, id string) ([]NodeLibrary, error) {
	where := ""
	args := []interface{}{}
	if id != "" {
		where = "WHERE p.id = ?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT
			p.id, p.name, COALESCE(p.remark, ''), p.enabled, COALESCE(p.template_id, ''),
			COALESCE(p.traffic_source, 'manual'), COALESCE(p.traffic_server_id, ''),
			COALESCE(u.url, ''), COALESCE(u.enabled, 0), COALESCE(u.refresh_hours, 24),
			COALESCE(u.status, ''), COALESCE(u.last_error, ''), COALESCE(u.last_refresh_at, ''), COALESCE(u.userinfo, ''),
			p.total_bytes, p.manual_upload_bytes, p.manual_download_bytes, COALESCE(p.expire_at, ''),
			COALESCE(p.cycle_type, 'none'), COALESCE(p.cycle_day, 1), COALESCE(p.cycle_start, ''), COALESCE(p.cycle_end, ''),
			p.baseline_upload_bytes, p.baseline_download_bytes, p.rate_limit_enabled, COALESCE(p.rate_limit_per_minute, 30),
			COALESCE(p.node_filter_tags, ''), COALESCE(p.sort_order, 0),
			COALESCE(p.selection_mode, 'explicit'), COALESCE(p.include_internal_nodes, 0),
			p.created_at, p.updated_at
		FROM subscription_profiles p
		LEFT JOIN subscription_upstreams u ON u.id = (
			SELECT id FROM subscription_upstreams
			WHERE profile_id = p.id
			ORDER BY updated_at DESC, created_at DESC
			LIMIT 1
		)
		`+where+`
		ORDER BY p.sort_order ASC, p.updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []NodeLibrary{}
	for rows.Next() {
		var item NodeLibrary
		var enabled, upstreamEnabled, rateEnabled, includeInternal int
		if err := rows.Scan(&item.ID, &item.Name, &item.Remark, &enabled, &item.TemplateID, &item.TrafficSource, &item.TrafficServerID, &item.UpstreamURL, &upstreamEnabled, &item.UpstreamRefreshHours, &item.UpstreamStatus, &item.UpstreamLastError, &item.UpstreamLastRefreshAt, &item.UpstreamUserinfo, &item.TotalBytes, &item.ManualUploadBytes, &item.ManualDownloadBytes, &item.ExpireAt, &item.CycleType, &item.CycleDay, &item.CycleStart, &item.CycleEnd, &item.BaselineUploadBytes, &item.BaselineDownloadBytes, &rateEnabled, &item.RateLimitPerMinute, &item.NodeFilterTags, &item.SortOrder, &item.SelectionMode, &includeInternal, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.UpstreamEnabled = upstreamEnabled == 1
		item.RateLimitEnabled = rateEnabled == 1
		item.IncludeInternalNodes = includeInternal == 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, items[i].ID).Scan(&items[i].NodeCount)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ? AND id != COALESCE(profile_id, id)`, items[i].ID).Scan(&items[i].SubscriptionCount)
		nodeIDs, err := loadProfileInternalNodeIDs(ctx, db, items[i].ID)
		if err == nil {
			items[i].InternalNodeIDs = nodeIDs
		}
		items[i].Traffic = computeTraffic(ctx, db, Subscription{
			ID:                    items[i].ID,
			ProfileID:             items[i].ID,
			Name:                  items[i].Name,
			Enabled:               items[i].Enabled,
			TrafficSource:         items[i].TrafficSource,
			TrafficServerID:       items[i].TrafficServerID,
			TotalBytes:            items[i].TotalBytes,
			ManualUploadBytes:     items[i].ManualUploadBytes,
			ManualDownloadBytes:   items[i].ManualDownloadBytes,
			ExpireAt:              items[i].ExpireAt,
			CycleType:             items[i].CycleType,
			CycleDay:              items[i].CycleDay,
			CycleStart:            items[i].CycleStart,
			CycleEnd:              items[i].CycleEnd,
			BaselineUploadBytes:   items[i].BaselineUploadBytes,
			BaselineDownloadBytes: items[i].BaselineDownloadBytes,
		})
	}
	return items, nil
}

func loadSubscriptions(ctx context.Context, db *sql.DB, id string) ([]Subscription, error) {
	where := ""
	args := []interface{}{}
	if id != "" {
		where = "WHERE id = ?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(profile_id, id), COALESCE(plan_id, ''), name, COALESCE(remark, ''), enabled, public_token, COALESCE(vless_uuid,''), COALESCE(hysteria2_password,''), COALESCE(template_id, ''), COALESCE(traffic_source, 'manual'), COALESCE(traffic_server_id, ''), COALESCE(upstream_url, ''), upstream_enabled, COALESCE(upstream_refresh_hours, 24), COALESCE(upstream_status, ''), COALESCE(upstream_last_error, ''), COALESCE(upstream_last_refresh_at, ''), total_bytes, manual_upload_bytes, manual_download_bytes, COALESCE(expire_at, ''), COALESCE(cycle_type, 'none'), COALESCE(cycle_day, 1), COALESCE(cycle_start, ''), COALESCE(cycle_end, ''), baseline_upload_bytes, baseline_download_bytes, rate_limit_enabled, COALESCE(rate_limit_per_minute, 30), COALESCE(node_filter_ids, ''), COALESCE(include_internal_nodes,1), COALESCE(include_external_nodes,0), created_at, updated_at FROM subscription_subscriptions `+where+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	items := []Subscription{}
	for rows.Next() {
		var sub Subscription
		var enabled, upstreamEnabled, rateEnabled, includeInternal, includeExternal int
		var nodeFilterIDs string
		if err := rows.Scan(&sub.ID, &sub.ProfileID, &sub.PlanID, &sub.Name, &sub.Remark, &enabled, &sub.PublicToken, &sub.VLESSUUID, &sub.Hysteria2Password, &sub.TemplateID, &sub.TrafficSource, &sub.TrafficServerID, &sub.UpstreamURL, &upstreamEnabled, &sub.UpstreamRefreshHours, &sub.UpstreamStatus, &sub.UpstreamLastError, &sub.UpstreamLastRefreshAt, &sub.TotalBytes, &sub.ManualUploadBytes, &sub.ManualDownloadBytes, &sub.ExpireAt, &sub.CycleType, &sub.CycleDay, &sub.CycleStart, &sub.CycleEnd, &sub.BaselineUploadBytes, &sub.BaselineDownloadBytes, &rateEnabled, &sub.RateLimitPerMinute, &nodeFilterIDs, &includeInternal, &includeExternal, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		sub.Enabled = enabled == 1
		sub.UpstreamEnabled = upstreamEnabled == 1
		sub.RateLimitEnabled = rateEnabled == 1
		sub.IncludeInternalNodes = includeInternal == 1
		sub.IncludeExternalNodes = includeExternal == 1
		sub.NodeFilterIDs = decodeNodeFilterIDs(nodeFilterIDs)
		items = append(items, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range items {
		// Store.Open intentionally limits SQLite to one connection. Applying a plan
		// while the subscription cursor is still open would wait forever for that
		// same connection, blocking both list and create/update responses.
		applyPlanToSubscription(ctx, db, &items[i])
		items[i].NodeCount = countPublishedSubscriptionNodes(ctx, db, items[i])
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND date(created_at) = date('now')`, items[i].ID).Scan(&items[i].AccessCountToday)
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(created_at), '') FROM subscription_access_logs WHERE subscription_id = ?`, items[i].ID).Scan(&items[i].LastAccessAt)
		items[i].Traffic = computeTraffic(ctx, db, items[i])
		if nodeIDs, err := reconcilequeue.NodeIDsForPlan(ctx, db, items[i].PlanID); err == nil {
			items[i].RuntimeSyncStatus = reconcilequeue.Status(ctx, db, nodeIDs)
		} else {
			items[i].RuntimeSyncStatus = "unknown"
		}
		items[i].Quality = loadQuality(ctx, db, items[i].TrafficServerID)
	}
	return items, nil
}

func loadSubscriptionByToken(ctx context.Context, db *sql.DB, token string) ([]Subscription, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT id FROM subscription_subscriptions WHERE public_token = ?`, token).Scan(&id)
	if err != nil {
		return nil, err
	}
	return loadSubscriptions(ctx, db, id)
}

func loadNodes(ctx context.Context, db *sql.DB, subscriptionID string, decrypt bool) ([]Node, error) {
	where := ""
	args := []interface{}{}
	if subscriptionID != "" {
		where = "WHERE COALESCE(profile_id, subscription_id) = ?"
		args = append(args, subscriptionID)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, subscription_id, COALESCE(profile_id, subscription_id), name, COALESCE(type, ''), COALESCE(server, ''), COALESCE(port, 0), COALESCE(country_code, ''), COALESCE(location, ''), COALESCE(tags, ''), COALESCE(traffic_server_id, ''), COALESCE(ownership, 'external'), COALESCE(management, 'unmanaged'), COALESCE(traffic_reporting, 'unavailable'), enabled, stable, sort_order, COALESCE(raw_encrypted, ''), COALESCE(config_encrypted, ''), created_at, updated_at FROM subscription_nodes `+where+` ORDER BY COALESCE(profile_id, subscription_id) ASC, sort_order ASC, created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	nodes := []Node{}
	for rows.Next() {
		var node Node
		var enabled, stable int
		if err := rows.Scan(&node.ID, &node.SubscriptionID, &node.ProfileID, &node.Name, &node.Type, &node.Server, &node.Port, &node.CountryCode, &node.Location, &node.Tags, &node.TrafficServerID, &node.Ownership, &node.Management, &node.TrafficReporting, &enabled, &stable, &node.SortOrder, &node.Raw, &node.ConfigJSON, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, err
		}
		node.Enabled = enabled == 1
		node.Stable = stable == 1
		if decrypt {
			node.Raw = secure.SecureDecrypt(node.Raw)
			node.ConfigJSON = secure.SecureDecrypt(node.ConfigJSON)
		} else {
			node.Raw = ""
			node.ConfigJSON = ""
		}
		nodes = append(nodes, node)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range nodes {
		if nodes[i].TrafficServerID != "" {
			nodes[i].Quality = loadQuality(ctx, db, nodes[i].TrafficServerID)
		}
	}
	return nodes, nil
}
