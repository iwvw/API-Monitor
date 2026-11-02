package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listProfiles(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	items, err := loadProfiles(r.Context(), db, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, items)
}

func (s *Service) getProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	items, err := loadProfiles(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		response.Error(w, http.StatusNotFound, "node library not found")
		return
	}
	response.OK(w, items[0])
}

func (s *Service) createProfile(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input NodeLibrary
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "节点库名称不能为空")
		return
	}
	settings, _ := loadSettings(r.Context(), db)
	id := firstNonEmpty(input.ID, randomID("profile"))
	subInput := subscriptionFromProfile(input)
	subInput.Enabled = input.Enabled || !isExplicitFalse(r, "enabled")
	subInput.RateLimitEnabled = input.RateLimitEnabled || settings.DefaultRateLimitEnabled
	templateID := firstNonEmpty(input.TemplateID, settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	trafficSource := normalizeTrafficSource(input.TrafficSource)
	cycleType := normalizeCycleType(input.CycleType)
	cycleDay := input.CycleDay
	if cycleDay <= 0 {
		cycleDay = 1
	}
	refreshHours := input.UpstreamRefreshHours
	if refreshHours <= 0 {
		refreshHours = settings.DefaultRefreshHours
	}
	limitPerMin := input.RateLimitPerMinute
	if limitPerMin <= 0 {
		limitPerMin = settings.DefaultRateLimitPerMin
	}
	if err := upsertProfile(r.Context(), db, id, subInput, templateID, trafficSource, cycleType, cycleDay, limitPerMin, input.SelectionMode, input.IncludeInternalNodes); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// explicit 清单与套餐共用 subscription_plan_nodes（plan_id 存 profile id），
	// 写入后立即为涉及节点排队 reconcile，让 Agent 下发新凭据范围。
	if _, _, err := replacePlanNodeRelations(r.Context(), db, nil, id, input.InternalNodeIDs); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if nodeIDs, err := reconcilequeue.NodeIDsForProfile(r.Context(), db, id); err == nil && len(nodeIDs) > 0 {
		_ = reconcilequeue.EnqueueNodes(r.Context(), db, nodeIDs, "profile policy changed")
	}
	if err := upsertDefaultUpstream(r.Context(), db, id, subInput, refreshHours); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, _ := loadProfiles(r.Context(), db, id)
	response.OK(w, firstProfile(items))
}

func (s *Service) updateProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input NodeLibrary
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "节点库名称不能为空")
		return
	}
	if !profileExists(r.Context(), db, id) {
		response.Error(w, http.StatusNotFound, "node library not found")
		return
	}
	// 对齐套餐变更的做法：先记录旧节点范围，写入后对前后差集排队 reconcile，
	// 确保被移出/移入 explicit 清单的节点都能同步运行时凭据。
	previousNodeIDs, err := reconcilequeue.NodeIDsForProfile(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subInput := subscriptionFromProfile(input)
	cycleDay := input.CycleDay
	if cycleDay <= 0 {
		cycleDay = 1
	}
	templateID := firstNonEmpty(input.TemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	trafficSource := normalizeTrafficSource(input.TrafficSource)
	cycleType := normalizeCycleType(input.CycleType)
	refreshHours := intDefault(input.UpstreamRefreshHours, defaultRefreshHours)
	limitPerMin := intDefault(input.RateLimitPerMinute, defaultLimitPerMin)
	if err := upsertProfile(r.Context(), db, id, subInput, templateID, trafficSource, cycleType, cycleDay, limitPerMin, input.SelectionMode, input.IncludeInternalNodes); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, _, err := replacePlanNodeRelations(r.Context(), db, nil, id, input.InternalNodeIDs); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	currentNodeIDs, err := reconcilequeue.NodeIDsForProfile(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reconcilequeue.EnqueueNodes(r.Context(), db, append(previousNodeIDs, currentNodeIDs...), "profile policy changed"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := upsertDefaultUpstream(r.Context(), db, id, subInput, refreshHours); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, _ := loadProfiles(r.Context(), db, id)
	response.OK(w, firstProfile(items))
}

func (s *Service) deleteProfile(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if id == defaultNodeLibrary {
		response.Error(w, http.StatusConflict, "系统外部节点池不能删除")
		return
	}
	var nodeCount, linkCount int
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, id).Scan(&nodeCount)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ? AND id != COALESCE(profile_id, id)`, id).Scan(&linkCount)
	force := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	if (nodeCount > 0 || linkCount > 0) && !force {
		response.Error(w, http.StatusConflict, "节点库仍包含节点或对外订阅，不能删除")
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if force {
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_access_logs WHERE subscription_id IN (
			SELECT id FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ?
		)`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE source='external' AND node_id IN (SELECT id FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?)`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = ?`, id)
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ?`, id)
	} else {
		_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE id = ? AND id = COALESCE(profile_id, id)`, id)
	}
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_upstreams WHERE profile_id = ?`, id)
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_plan_nodes WHERE plan_id = ?`, id)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_profiles WHERE id = ?`, id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"deleted": true, "forced": force})
}

func (s *Service) refreshProfileUpstream(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if err := s.refreshUpstreamNow(r.Context(), db, id); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.OK(w, map[string]bool{"refreshed": true})
}

func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	subs, err := loadSubscriptions(r.Context(), db, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(subs) == 0 {
		response.Error(w, http.StatusNotFound, "subscription not found")
		return
	}
	nodes, _ := loadNodes(r.Context(), db, firstNonEmpty(subs[0].ProfileID, id), true)
	response.OK(w, map[string]interface{}{"subscription": subs[0], "nodes": nodes})
}

func (s *Service) createSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input Subscription
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "订阅名称不能为空")
		return
	}
	if strings.TrimSpace(input.PlanID) == "" {
		response.Error(w, http.StatusBadRequest, "请选择套餐")
		return
	}
	if plans, err := loadPlans(r.Context(), db, input.PlanID); err != nil || len(plans) == 0 || !plans[0].Enabled {
		response.Error(w, http.StatusBadRequest, "所选套餐不存在或已停用")
		return
	}
	applyPlanToSubscription(r.Context(), db, &input)
	settings, _ := loadSettings(r.Context(), db)
	id := randomID("sub")
	profileID := defaultNodeLibrary
	token := randomToken()
	templateID := firstNonEmpty(input.TemplateID, settings.DefaultTemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	effectiveEnabled := input.Enabled || !isExplicitFalse(r, "enabled")
	input.Enabled = effectiveEnabled
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if !profileExists(r.Context(), tx, profileID) {
		library := Subscription{Name: "外部节点池", Remark: "系统统一外部节点池", Enabled: true}
		if err := upsertProfile(r.Context(), tx, profileID, library, rawTemplateID, "manual", "none", 1, defaultLimitPerMin, "explicit", false); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO subscription_subscriptions (
		id, profile_id, plan_id, name, remark, enabled, public_token, vless_uuid, hysteria2_password, template_id, traffic_source, traffic_server_id,
		upstream_url, upstream_enabled, upstream_refresh_hours, total_bytes, manual_upload_bytes,
		manual_download_bytes, expire_at, cycle_type, cycle_day, cycle_start, cycle_end,
		rate_limit_enabled, rate_limit_per_minute, node_filter_ids, include_internal_nodes, include_external_nodes, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, profileID, input.PlanID, input.Name, input.Remark, boolToInt(effectiveEnabled), token, randomUUID(), randomCredential(), templateID, "panel", nil,
		nil, 0, defaultRefreshHours, 0, 0,
		0, nil, "none", 1, nil, nil,
		0, 0, "", 0, 0)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodeIDs, err := reconcilequeue.NodeIDsForPlan(r.Context(), tx, input.PlanID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription created") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) updateSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input Subscription
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		response.Error(w, http.StatusBadRequest, "订阅名称不能为空")
		return
	}
	if strings.TrimSpace(input.PlanID) == "" {
		response.Error(w, http.StatusBadRequest, "请选择套餐")
		return
	}
	if plans, err := loadPlans(r.Context(), db, input.PlanID); err != nil || len(plans) == 0 {
		response.Error(w, http.StatusBadRequest, "所选套餐不存在")
		return
	}
	applyPlanToSubscription(r.Context(), db, &input)
	profileID := firstNonEmpty(profileIDForSubscription(r.Context(), db, id), defaultNodeLibrary)
	templateID := firstNonEmpty(input.TemplateID, defaultTemplateID)
	if err := validateTemplateReference(r.Context(), db, templateID); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var previousPlanID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&previousPlanID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions SET
		profile_id = ?, plan_id = ?, name = ?, remark = ?, enabled = ?, template_id = ?, traffic_source = ?, traffic_server_id = ?,
		upstream_url = ?, upstream_enabled = ?, upstream_refresh_hours = ?, total_bytes = ?,
		manual_upload_bytes = ?, manual_download_bytes = ?, expire_at = ?, cycle_type = ?, cycle_day = ?,
		cycle_start = ?, cycle_end = ?, rate_limit_enabled = ?, rate_limit_per_minute = ?, node_filter_ids = ?, include_internal_nodes = ?, include_external_nodes = ?, updated_at = datetime('now')
		WHERE id = ?`,
		profileID, input.PlanID, input.Name, input.Remark, boolToInt(input.Enabled), templateID, "panel", nil,
		nil, 0, defaultRefreshHours, 0,
		0, 0, nil, "none", 1,
		nil, nil, 0, 0, "", 0, 0, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := reconcilequeue.NodeIDsForPlans(r.Context(), tx, previousPlanID, input.PlanID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription policy changed") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) setSubscriptionEnabled(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
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
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions SET enabled=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil || reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription enabled state changed") != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点配置同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, _ := loadSubscriptions(r.Context(), db, id)
	response.OK(w, firstSub(subs))
}

func (s *Service) deleteSubscription(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if id == defaultNodeLibrary {
		response.Error(w, http.StatusConflict, "系统外部节点池锚点不能删除")
		return
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM subscription_subscriptions WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = tx.ExecContext(r.Context(), `DELETE FROM subscription_access_logs WHERE subscription_id = ?`, id)
	// Remove quota and Agent replay/audit rows explicitly so legacy databases
	// created before foreign-key enforcement cannot retain orphaned state.
	for _, statement := range []string{
		`DELETE FROM subscription_usage_reports WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_report_keys WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_hourly WHERE subscription_id=?`,
		`DELETE FROM subscription_usage_cycles WHERE subscription_id=?`,
		`DELETE FROM subscription_cycle_state WHERE subscription_id=?`,
	} {
		if _, err := tx.ExecContext(r.Context(), statement, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM subscription_subscriptions WHERE id = ?`, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription deleted"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}

func upsertProfile(ctx context.Context, executor subscriptionExecutor, id string, input Subscription, templateID, trafficSource, cycleType string, cycleDay, limitPerMin int, selectionMode string, includeInternal bool) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("profile id is required")
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO subscription_profiles (
			id, name, remark, enabled, template_id, traffic_source, traffic_server_id,
			total_bytes, manual_upload_bytes, manual_download_bytes, expire_at, cycle_type,
			cycle_day, cycle_start, cycle_end, baseline_upload_bytes, baseline_download_bytes,
			rate_limit_enabled, rate_limit_per_minute, selection_mode, include_internal_nodes, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			remark = excluded.remark,
			enabled = excluded.enabled,
			template_id = excluded.template_id,
			traffic_source = excluded.traffic_source,
			traffic_server_id = excluded.traffic_server_id,
			total_bytes = excluded.total_bytes,
			manual_upload_bytes = excluded.manual_upload_bytes,
			manual_download_bytes = excluded.manual_download_bytes,
			expire_at = excluded.expire_at,
			cycle_type = excluded.cycle_type,
			cycle_day = excluded.cycle_day,
			cycle_start = excluded.cycle_start,
			cycle_end = excluded.cycle_end,
			baseline_upload_bytes = excluded.baseline_upload_bytes,
			baseline_download_bytes = excluded.baseline_download_bytes,
			rate_limit_enabled = excluded.rate_limit_enabled,
			rate_limit_per_minute = excluded.rate_limit_per_minute,
			selection_mode = excluded.selection_mode,
			include_internal_nodes = excluded.include_internal_nodes,
			updated_at = datetime('now')`,
		id, input.Name, input.Remark, boolToInt(input.Enabled), templateID, trafficSource, nullString(input.TrafficServerID),
		input.TotalBytes, input.ManualUploadBytes, input.ManualDownloadBytes, nullString(input.ExpireAt), cycleType,
		cycleDay, nullString(input.CycleStart), nullString(input.CycleEnd), input.BaselineUploadBytes, input.BaselineDownloadBytes,
		boolToInt(input.RateLimitEnabled), limitPerMin, normalizePlanSelectionMode(selectionMode), boolToInt(includeInternal))
	if err != nil {
		return fmt.Errorf("upsert subscription profile: %w", err)
	}
	return nil
}

func upsertDefaultUpstream(ctx context.Context, executor subscriptionExecutor, profileID string, input Subscription, refreshHours int) error {
	upstreamURL := strings.TrimSpace(input.UpstreamURL)
	upstreamID := "up_" + profileID
	if upstreamURL == "" {
		if _, err := executor.ExecContext(ctx, `DELETE FROM subscription_upstreams WHERE id = ?`, upstreamID); err != nil {
			return fmt.Errorf("delete default upstream: %w", err)
		}
		return nil
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO subscription_upstreams (
			id, profile_id, name, url, enabled, refresh_hours, status, last_error, last_refresh_at, updated_at
		) VALUES (?, ?, '默认上游', ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			profile_id = excluded.profile_id,
			url = excluded.url,
			enabled = excluded.enabled,
			refresh_hours = excluded.refresh_hours,
			status = excluded.status,
			last_error = excluded.last_error,
			last_refresh_at = excluded.last_refresh_at,
			updated_at = datetime('now')`,
		upstreamID, profileID, upstreamURL, boolToInt(input.UpstreamEnabled), refreshHours, input.UpstreamStatus, input.UpstreamLastError, nullString(input.UpstreamLastRefreshAt))
	if err != nil {
		return fmt.Errorf("upsert default upstream: %w", err)
	}
	return nil
}

func profileIDForSubscription(ctx context.Context, db *sql.DB, id string) string {
	var profileID string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(profile_id, '') FROM subscription_subscriptions WHERE id = ?`, id).Scan(&profileID)
	return profileID
}

func profileExists(ctx context.Context, executor subscriptionExecutor, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	var count int
	_ = executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_profiles WHERE id = ?`, id).Scan(&count)
	return count > 0
}

func (s *Service) resetToken(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	token := randomToken()
	vlessUUID := randomUUID()
	hysteria2Password := randomCredential()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE(plan_id,'') FROM subscription_subscriptions WHERE id=?`, id).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "订阅不存在")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions
		SET public_token=?,vless_uuid=?,hysteria2_password=?,updated_at=datetime('now')
		WHERE id=?`, token, vlessUUID, hysteria2Password, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	nodeIDs, err := nodeIDsForSubscription(r.Context(), tx, id, planID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "无法解析订阅节点范围")
		return
	}
	if err := reconcilequeue.EnqueueNodes(r.Context(), tx, nodeIDs, "subscription credentials rotated"); err != nil {
		response.Error(w, http.StatusInternalServerError, "无法安排节点凭据同步")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	syncStatus := "not_required"
	if len(nodeIDs) > 0 {
		syncStatus = "pending"
	}
	response.OK(w, map[string]interface{}{
		"public_token":        token,
		"credentials_rotated": true,
		"nodes_queued":        len(nodeIDs),
		"runtime_sync_status": syncStatus,
	})
}

// rotateAddress rotates only the public subscription URL token. Client node
// credentials (VLESS UUID, Hysteria2 password) are left untouched, so already
// configured clients keep working; no runtime reconciliation is enqueued.
func (s *Service) rotateAddress(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	token := randomToken()
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE subscription_subscriptions
		SET public_token=?,updated_at=datetime('now')
		WHERE id=?`, token, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		response.Error(w, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{
		"public_token":        token,
		"credentials_rotated": false,
		"nodes_queued":        0,
		"runtime_sync_status": "not_required",
	})
}

// nodeIDsForSubscription keeps credential rotation compatible with legacy
// subscriptions that predate mandatory plans. New subscriptions always use
// the plan path; legacy filters are treated as managed-node IDs when present.
func nodeIDsForSubscription(ctx context.Context, tx *sql.Tx, subscriptionID, planID string) ([]string, error) {
	if strings.TrimSpace(planID) != "" {
		return reconcilequeue.NodeIDsForPlan(ctx, tx, planID)
	}
	var filters string
	var includeInternal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(node_filter_ids,''),COALESCE(include_internal_nodes,1) FROM subscription_subscriptions WHERE id=?`, subscriptionID).Scan(&filters, &includeInternal); err != nil {
		return nil, err
	}
	if ids := decodeNodeFilterIDs(filters); len(ids) > 0 {
		return ids, nil
	}
	if includeInternal == 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM managed_proxy_nodes WHERE enabled=1 ORDER BY created_at ASC`)
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

func subscriptionFromProfile(input NodeLibrary) Subscription {
	return Subscription{
		ProfileID:             input.ID,
		Name:                  input.Name,
		Remark:                input.Remark,
		Enabled:               input.Enabled,
		TemplateID:            input.TemplateID,
		TrafficSource:         input.TrafficSource,
		TrafficServerID:       input.TrafficServerID,
		UpstreamURL:           input.UpstreamURL,
		UpstreamEnabled:       input.UpstreamEnabled,
		UpstreamRefreshHours:  input.UpstreamRefreshHours,
		UpstreamStatus:        input.UpstreamStatus,
		UpstreamLastError:     input.UpstreamLastError,
		UpstreamLastRefreshAt: input.UpstreamLastRefreshAt,
		TotalBytes:            input.TotalBytes,
		ManualUploadBytes:     input.ManualUploadBytes,
		ManualDownloadBytes:   input.ManualDownloadBytes,
		ExpireAt:              input.ExpireAt,
		CycleType:             input.CycleType,
		CycleDay:              input.CycleDay,
		CycleStart:            input.CycleStart,
		CycleEnd:              input.CycleEnd,
		BaselineUploadBytes:   input.BaselineUploadBytes,
		BaselineDownloadBytes: input.BaselineDownloadBytes,
		RateLimitEnabled:      input.RateLimitEnabled,
		RateLimitPerMinute:    input.RateLimitPerMinute,
	}
}

func (s *Service) listSubscriptions(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	subs, err := loadSubscriptions(r.Context(), db, "")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, subs)
}
