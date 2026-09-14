package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

func (s *Service) handlePlans(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			plans, err := loadPlans(r.Context(), db, id)
			if err != nil || len(plans) == 0 {
				response.Error(w, http.StatusNotFound, "套餐不存在")
				return
			}
			response.OK(w, plans[0])
			return
		}
		plans, err := loadPlans(r.Context(), db, "")
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.OK(w, plans)
	case http.MethodPatch:
		if id == "" {
			response.Error(w, http.StatusBadRequest, "套餐 ID 不能为空")
			return
		}
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Enabled == nil {
			response.Error(w, http.StatusBadRequest, "enabled 不能为空")
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE subscription_plans SET enabled=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			response.Error(w, http.StatusNotFound, "套餐不存在")
			return
		}
		nodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "plan enabled state changed") != nil {
			response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		plans, err := loadPlans(r.Context(), db, id)
		if err != nil || len(plans) == 0 {
			response.Error(w, http.StatusNotFound, "套餐不存在或加载失败")
			return
		}
		response.OK(w, plans[0])
	case http.MethodPost, http.MethodPut:
		var input Plan
		if !decodeJSON(w, r, &input) {
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			response.Error(w, 400, "套餐名称不能为空")
			return
		}
		if input.CycleDay < 1 || input.CycleDay > 31 {
			input.CycleDay = 1
		}
		input.CycleType = normalizeCycleType(input.CycleType)
		input.SelectionMode = normalizePlanSelectionMode(input.SelectionMode)
		if input.SelectionMode == planSelectionAll && !input.IncludeInternalNodes && !input.IncludeExternalNodes {
			response.Error(w, http.StatusBadRequest, "全部节点模式至少需要启用一个节点来源")
			return
		}
		if input.SelectionMode == planSelectionAll {
			input.NodeIDs = nil
		}
		if input.RateLimitPerMinute <= 0 {
			input.RateLimitPerMinute = defaultLimitPerMin
		}
		if id == "" {
			id = randomID("plan")
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		previousNodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO subscription_plans
			(id,name,remark,enabled,total_bytes,cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,node_ids,selection_mode,include_internal_nodes,include_external_nodes,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now')) ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,remark=excluded.remark,enabled=excluded.enabled,total_bytes=excluded.total_bytes,
			cycle_type=excluded.cycle_type,cycle_day=excluded.cycle_day,rate_limit_enabled=excluded.rate_limit_enabled,
			rate_limit_per_minute=excluded.rate_limit_per_minute,node_ids='',selection_mode=excluded.selection_mode,
			include_internal_nodes=excluded.include_internal_nodes,include_external_nodes=excluded.include_external_nodes,updated_at=datetime('now')`,
			id, input.Name, input.Remark, boolToInt(input.Enabled), maxInt64(0, input.TotalBytes), input.CycleType, input.CycleDay,
			boolToInt(input.RateLimitEnabled), input.RateLimitPerMinute, "", input.SelectionMode, boolToInt(input.IncludeInternalNodes), boolToInt(input.IncludeExternalNodes))
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		containsInternal, containsExternal, err := replacePlanNodeRelations(r.Context(), db, tx, id, input.NodeIDs)
		if err != nil {
			response.Error(w, 400, err.Error())
			return
		}
		if input.SelectionMode == planSelectionExplicit {
			if _, err := tx.ExecContext(r.Context(), `UPDATE subscription_plans SET include_internal_nodes=?,include_external_nodes=? WHERE id=?`, boolToInt(containsInternal), boolToInt(containsExternal), id); err != nil {
				response.Error(w, 500, err.Error())
				return
			}
		}
		currentNodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		if err := reconcilequeue.EnqueueNodes(r.Context(), tx, append(previousNodeIDs, currentNodeIDs...), "plan policy changed"); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		plans, err := loadPlans(r.Context(), db, id)
		if err != nil || len(plans) == 0 {
			response.Error(w, http.StatusNotFound, "套餐不存在或加载失败")
			return
		}
		response.OK(w, plans[0])
	case http.MethodDelete:
		if id == "" {
			response.Error(w, 400, "套餐 ID 不能为空")
			return
		}
		var count int
		_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE plan_id=?`, id).Scan(&count)
		if count > 0 {
			response.Error(w, 409, "套餐仍有订阅使用，无法删除")
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE plan_id=?`, id)
		result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_plans WHERE id=?`, id)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			response.Error(w, 404, "套餐不存在")
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		response.OK(w, map[string]bool{"deleted": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func loadPlans(ctx context.Context, db *sql.DB, id string) ([]Plan, error) {
	where, args := "", []interface{}{}
	if id != "" {
		where, args = " WHERE p.id=?", append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT p.id,p.name,COALESCE(p.remark,''),p.enabled,p.total_bytes,p.cycle_type,p.cycle_day,
		p.rate_limit_enabled,p.rate_limit_per_minute,COALESCE(p.selection_mode,'explicit'),p.include_internal_nodes,p.include_external_nodes,
		p.created_at,p.updated_at,(SELECT COUNT(*) FROM subscription_subscriptions s WHERE s.plan_id=p.id)
		FROM subscription_plans p`+where+` ORDER BY p.updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	plans := []Plan{}
	for rows.Next() {
		var p Plan
		var enabled, rateEnabled, includeInternal, includeExternal int
		if err := rows.Scan(&p.ID, &p.Name, &p.Remark, &enabled, &p.TotalBytes, &p.CycleType, &p.CycleDay, &rateEnabled, &p.RateLimitPerMinute, &p.SelectionMode, &includeInternal, &includeExternal, &p.CreatedAt, &p.UpdatedAt, &p.SubscriptionCount); err != nil {
			return nil, err
		}
		p.Enabled, p.RateLimitEnabled = enabled == 1, rateEnabled == 1
		p.IncludeInternalNodes, p.IncludeExternalNodes = includeInternal == 1, includeExternal == 1
		p.SelectionMode = normalizePlanSelectionMode(p.SelectionMode)
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range plans {
		nodeRows, err := db.QueryContext(ctx, `SELECT node_id FROM subscription_plan_nodes WHERE plan_id=? ORDER BY created_at,node_id`, plans[i].ID)
		if err != nil {
			return nil, err
		}
		relations := []string{}
		for nodeRows.Next() {
			var nodeID string
			if err := nodeRows.Scan(&nodeID); err != nil {
				nodeRows.Close()
				return nil, err
			}
			relations = append(relations, nodeID)
		}
		nodeRows.Close()
		plans[i].NodeIDs = relations
	}
	return plans, nil
}

// loadProfileInternalNodeIDs 读取节点库在 explicit 模式下授权的内部节点清单
// （subscription_plan_nodes 中 plan_id=profile id AND source='internal'）。
func loadProfileInternalNodeIDs(ctx context.Context, db *sql.DB, profileID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT node_id FROM subscription_plan_nodes WHERE plan_id=? AND source='internal' ORDER BY created_at,node_id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func replacePlanNodeRelations(ctx context.Context, db *sql.DB, tx *sql.Tx, planID string, nodeIDs []string) (bool, bool, error) {
	var executor subscriptionExecutor = db
	if tx != nil {
		executor = tx
	}
	if _, err := executor.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE plan_id=?`, planID); err != nil {
		return false, false, err
	}
	seen := map[string]struct{}{}
	containsInternal, containsExternal := false, false
	for _, rawID := range nodeIDs {
		nodeID := strings.TrimSpace(rawID)
		if nodeID == "" {
			continue
		}
		if _, duplicate := seen[nodeID]; duplicate {
			continue
		}
		seen[nodeID] = struct{}{}
		source := ""
		var exists int
		if err := executor.QueryRowContext(ctx, `SELECT 1 FROM managed_proxy_nodes WHERE id=?`, nodeID).Scan(&exists); err == nil {
			source = "internal"
		} else if err := executor.QueryRowContext(ctx, `SELECT 1 FROM subscription_nodes WHERE id=?`, nodeID).Scan(&exists); err == nil {
			source = "external"
		}
		if source == "" {
			return false, false, fmt.Errorf("节点 %s 不存在或已被删除", nodeID)
		}
		if _, err := executor.ExecContext(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES(?,?,?)`, planID, nodeID, source); err != nil {
			return false, false, err
		}
		containsInternal = containsInternal || source == "internal"
		containsExternal = containsExternal || source == "external"
	}
	return containsInternal, containsExternal, nil
}

func applyPlanToSubscription(ctx context.Context, db *sql.DB, sub *Subscription) {
	if sub != nil {
		sub.PlanEnabled = true
	}
	if sub == nil || strings.TrimSpace(sub.PlanID) == "" {
		return
	}
	plans, err := loadPlans(ctx, db, sub.PlanID)
	if err != nil || len(plans) == 0 {
		sub.PlanEnabled = false
		return
	}
	p := plans[0]
	sub.PlanEnabled = p.Enabled
	sub.TotalBytes = p.TotalBytes
	sub.CycleType = p.CycleType
	sub.CycleDay = p.CycleDay
	sub.ExpireAt = ""
	sub.CycleStart, sub.CycleEnd = planCycleWindow(time.Now(), p.CycleType, p.CycleDay, timeutil.LocationFromSettings(ctx, db))
	sub.RateLimitEnabled = p.RateLimitEnabled
	sub.RateLimitPerMinute = p.RateLimitPerMinute
	sub.NodeFilterIDs = append([]string(nil), p.NodeIDs...)
	sub.NodeSelectionMode = p.SelectionMode
	sub.IncludeInternalNodes = p.IncludeInternalNodes
	sub.IncludeExternalNodes = p.IncludeExternalNodes
}

func countPublishedSubscriptionNodes(ctx context.Context, db *sql.DB, sub Subscription) int {
	nodes, err := loadPublishedNodesForSubscription(ctx, db, sub)
	if err != nil {
		return 0
	}
	return len(nodes)
}

func loadPublishedNodesForSubscription(ctx context.Context, db *sql.DB, sub Subscription) ([]Node, error) {
	nodes := []Node{}
	if sub.IncludeInternalNodes {
		internal, err := loadManagedSubscriptionNodes(ctx, db, sub)
		if err != nil {
			return nil, err
		}
		if sub.PlanID != "" {
			internal = filterPlanNodesByIDsForSource(internal, sub.NodeFilterIDs, sub.NodeSelectionMode)
		}
		nodes = append(nodes, internal...)
	}
	if sub.IncludeExternalNodes {
		profileID := firstNonEmpty(sub.ProfileID, sub.ID)
		if sub.PlanID != "" {
			profileID = ""
		}
		external, err := loadNodes(ctx, db, profileID, true)
		if err != nil {
			return nil, err
		}
		if sub.PlanID != "" {
			external = filterPlanNodesByIDsForSource(external, sub.NodeFilterIDs, sub.NodeSelectionMode)
		} else {
			external = filterNodesByIDsForSource(external, sub.NodeFilterIDs)
		}
		nodes = append(nodes, external...)
	}
	nodes = filterEnabledPublishedNodes(nodes)
	nodes, err := filterNodesByAvailableHostQuota(ctx, db, nodes)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func normalizePlanSelectionMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), planSelectionAll) {
		return planSelectionAll
	}
	return planSelectionExplicit
}

func filterPlanNodesByIDsForSource(nodes []Node, ids []string, selectionMode string) []Node {
	if normalizePlanSelectionMode(selectionMode) == planSelectionAll {
		return nodes
	}
	if len(ids) == 0 {
		return []Node{}
	}
	return filterNodesByIDsForSource(nodes, ids)
}

func filterNodesByIDsForSource(nodes []Node, ids []string) []Node {
	if len(ids) == 0 {
		return nodes
	}
	available := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		available[node.ID] = struct{}{}
	}
	selected := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := available[id]; ok {
			selected = append(selected, id)
		}
	}
	if len(selected) == 0 {
		return []Node{}
	}
	return filterNodesByIDs(nodes, selected)
}

func filterEnabledPublishedNodes(nodes []Node) []Node {
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func filterNodesByAvailableHostQuota(ctx context.Context, db *sql.DB, nodes []Node) ([]Node, error) {
	serverIDs := []string{}
	seen := map[string]bool{}
	for _, node := range nodes {
		serverID := strings.TrimSpace(node.TrafficServerID)
		if serverID == "" || seen[serverID] {
			continue
		}
		seen[serverID] = true
		serverIDs = append(serverIDs, serverID)
	}
	if len(serverIDs) == 0 {
		return nodes, nil
	}
	quotaByServerID, err := loadServerTrafficQuotaStates(ctx, db, serverIDs)
	if err != nil {
		return nil, err
	}
	filtered := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		serverID := strings.TrimSpace(node.TrafficServerID)
		if quotaByServerID[serverID].Exhausted {
			continue
		}
		filtered = append(filtered, node)
	}
	return filtered, nil
}

func loadServerTrafficQuotaStates(ctx context.Context, db *sql.DB, serverIDs []string) (map[string]serverTrafficQuota, error) {
	states := make(map[string]serverTrafficQuota, len(serverIDs))
	serverIDs = compactStringList(serverIDs)
	if len(serverIDs) == 0 {
		return states, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(serverIDs)), ",")
	args := make([]interface{}, 0, len(serverIDs))
	for _, id := range serverIDs {
		args = append(args, id)
	}
	queries := []string{
		`SELECT id, COALESCE(traffic_limit_bytes, 0), COALESCE(traffic_limit_mode, 'total'), COALESCE(cached_info, '{}') FROM server_accounts WHERE id IN (` + placeholders + `)`,
		`SELECT id, COALESCE(traffic_limit_bytes, 0), 'total', COALESCE(cached_info, '{}') FROM server_accounts WHERE id IN (` + placeholders + `)`,
	}
	var lastErr error
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "no such table") || strings.Contains(lower, "no such column") {
				lastErr = err
				continue
			}
			return nil, err
		}
		for rows.Next() {
			var serverID, limitMode, cachedInfo string
			var limitBytes int64
			if err := rows.Scan(&serverID, &limitBytes, &limitMode, &cachedInfo); err != nil {
				rows.Close()
				return nil, err
			}
			usedBytes := trafficUsedBytesFromCachedInfo(cachedInfo, limitMode)
			limitBytes = maxInt64(limitBytes, 0)
			states[serverID] = serverTrafficQuota{
				UsedBytes:  usedBytes,
				LimitBytes: limitBytes,
				Exhausted:  limitBytes > 0 && usedBytes >= limitBytes,
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		return states, nil
	}
	if lastErr != nil {
		return states, nil
	}
	return states, nil
}

func trafficUsedBytesFromCachedInfo(raw, mode string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return 0
	}
	var cached map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return 0
	}
	network, _ := cached["network"].(map[string]interface{})
	rxTotal := firstFloatValue(cached, "net_in_transfer", "net_rx_total", "rx_total_bytes")
	txTotal := firstFloatValue(cached, "net_out_transfer", "net_tx_total", "tx_total_bytes")
	if value := getFloatFromMap(network, "rx_total_bytes"); value > 0 {
		rxTotal = value
	}
	if value := getFloatFromMap(network, "tx_total_bytes"); value > 0 {
		txTotal = value
	}
	return trafficUsedBytesForMode(rxTotal, txTotal, mode)
}
