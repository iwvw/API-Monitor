package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// triggerBudget 是单次 Trigger 的整体时间预算上限。
const triggerBudget = 90 * time.Second

func (s *Service) DryRun(ctx context.Context, rule Rule, eventData map[string]interface{}) (map[string]interface{}, error) {
	loc, timeZoneName := s.systemLocation(ctx)
	timeAllowed := checkTimeWindow(rule.TimeWindow, loc)
	conditions := evaluateConditions(rule.Conditions, eventData)
	maintenance, err := s.matchMaintenance(ctx, eventData)
	if err != nil {
		return nil, err
	}
	channels := []map[string]interface{}{}
	for _, channelID := range rule.Channels {
		channel, ok, err := s.loadStoredChannel(ctx, channelID)
		if err != nil {
			return nil, err
		}
		channels = append(channels, map[string]interface{}{
			"id":      channelID,
			"name":    defaultString(channel.Name, "Channel "+channelID),
			"type":    defaultString(channel.Type, "unknown"),
			"enabled": ok && channel.Enabled != 0,
			"exists":  ok,
		})
	}
	wouldNotify := timeAllowed && conditions.Allowed && maintenance == nil
	if wouldNotify {
		wouldNotify = false
		for _, channel := range channels {
			if channel["exists"] == true && channel["enabled"] == true {
				wouldNotify = true
				break
			}
		}
	}
	return map[string]interface{}{
		"matched":         true,
		"wouldNotify":     wouldNotify,
		"title":           formatTitle(rule, eventData),
		"message":         formatMessage(rule, eventData, loc),
		"fingerprint":     generateFingerprint(rule, eventData),
		"timeAllowed":     timeAllowed,
		"timeZone":        timeZoneName,
		"conditionResult": conditions,
		"maintenance":     maintenance,
		"channels":        channels,
		"diagnostics": []string{
			"dry-run does not enqueue or send notifications",
			map[bool]string{true: "conditions matched", false: "conditions did not match"}[conditions.Allowed],
		},
		"conditions": rule.Conditions,
	}, nil
}

func (s *Service) Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	// 单次触发的整体时间预算:多渠道 × 多重试最坏可达数分钟,
	// 而 Trigger 跑在 uptime/presence/cronjobs 同步检查链上,必须硬性封顶。
	ctx, cancel := context.WithTimeout(ctx, triggerBudget)
	defer cancel()
	rules, err := s.loadEnabledRulesByEvent(ctx, sourceModule, eventType)
	if err != nil {
		return err
	}
	loc, _ := s.systemLocation(ctx)
	lifecycle, hasLifecycle := notificationMessageLifecycle(sourceModule, eventType, eventData)
	if hasLifecycle {
		var err error
		eventData, err = s.enrichLifecycleEventData(ctx, sourceModule, eventType, lifecycle, eventData, time.Now())
		if err != nil {
			return err
		}
	}
	// 告警发送参数（重试/限流）逐触发读取；读失败退回默认值，不阻塞监控关键路径。
	var loadNotifyCfg = func() GlobalConfig {
		cfg, err := s.LoadConfig(ctx)
		if err != nil {
			cfg = GlobalConfig{MaxRetryTimes: 3, RetryIntervalSeconds: 60, GlobalRateLimitPerHr: 100, EnableBatch: true, BatchIntervalSeconds: 30}
		}
		return cfg
	}
	cfg := loadNotifyCfg()
	if cfg.EnableBatch && s.aggregator != nil {
		s.aggregator.SetWindow(time.Duration(cfg.BatchIntervalSeconds) * time.Second)
	}
	if cfg.BaseURL != "" {
		eventData = withDashboardURL(eventData, cfg.BaseURL)
	}
	for _, rule := range rules {
		dryRun, err := s.DryRun(ctx, rule, eventData)
		if err != nil {
			return err
		}
		if dryRun["wouldNotify"] != true {
			continue
		}
		// quiet_until 静默：未到期的时间窗内整条规则跳过
		if quietUntilActive(rule.QuietUntil, time.Now()) {
			continue
		}
		// 聚合：配置了 group_by 的规则，同一分组在窗口期内只发首条（或升级条）。
		// 恢复事件永不聚合 —— 漏掉恢复通知会让告警永远悬着。
		if cfg.EnableBatch && !(hasLifecycle && lifecycle.Phase == "resolve") {
			if groupBy := ruleGroupBy(rule); len(groupBy) > 0 {
				if key := groupKeyOf(groupBy, eventData); key != "" {
					decision := s.decisionForGroup(rule, key, eventData, time.Now())
					if !decision {
						continue
					}
				}
			}
		}
		// 恢复（resolve）阶段跳过重复抑制，避免恢复通知被吞导致告警永远悬着
		trackSuppression := !(hasLifecycle && lifecycle.Phase == "resolve")
		var fingerprint string
		if trackSuppression {
			fingerprint = generateFingerprint(rule, eventData)
			suppress, err := s.evaluateSuppression(ctx, rule, fingerprint, time.Now())
			if err != nil {
				// DB 错误 fail-open：宁多发不漏发
				suppress = false
			}
			if suppress {
				continue
			}
		}
		sentAny := false
		sendChannel := func(channelID string) (bool, error) {
			channel, ok, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return false, err
			}
			if !ok || channel.Enabled == 0 {
				return false, nil
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData, loc)
			if allowed, firstReject := s.rateLimiter.Allow(time.Now(), cfg.GlobalRateLimitPerHr); !allowed {
				if firstReject {
					slog.Warn("notification-global-rate-limit", "rule", rule.Name, "channel", channel.Name, "limit", cfg.GlobalRateLimitPerHr)
				}
				skipReason := fmt.Sprintf("global rate limit exceeded (%d/hour)", cfg.GlobalRateLimitPerHr)
				_, _ = s.createHistory(ctx, rule.ID, channelID, "failed", title, message, eventData, &skipReason)
				return false, nil
			}
			logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
			if err != nil {
				return false, err
			}
			channelConfig := decryptConfig(channel.ConfigRaw)
			send := func() (deliveryResult, error) {
				if channel.Type == "telegram" && hasLifecycle {
					return s.deliverLifecycleTelegram(ctx, channel, channelConfig, sourceModule, eventType, lifecycle, title, message)
				}
				return s.sendToChannel(ctx, channel, channelConfig, title, message)
			}
			delivery, retries, sendErr := s.sendWithRetry(ctx, send, cfg.MaxRetryTimes, cfg.RetryIntervalSeconds)
			if sendErr != nil {
				_ = s.updateHistoryRetry(ctx, logID, retries, sendErr.Error())
				return false, nil
			}
			if retries > 0 {
				_ = s.updateHistoryRetryCount(ctx, logID, retries)
			}
			if channel.Type == "telegram" && hasLifecycle && lifecycle.Phase != "resolve" && delivery.MessageID != 0 {
				_ = s.upsertTelegramMessageState(ctx, telegramMessageState{
					ChannelID: channel.ID, SourceModule: sourceModule, ResourceKey: lifecycle.ResourceKey,
					Kind: lifecycle.Kind, ChatID: delivery.ChatID, MessageID: delivery.MessageID,
				}, eventType, eventData)
			}
			now := time.Now().In(loc).Format(time.RFC3339)
			_ = s.updateHistoryStatus(ctx, logID, "sent", &now, nil)
			return true, nil
		}
		for _, channelID := range rule.Channels {
			sent, err := sendChannel(channelID)
			if err != nil {
				return err
			}
			if sent {
				sentAny = true
			}
		}
		// 主渠道全部失败时降级到规则配置的备用渠道。
		if !sentAny {
			for _, channelID := range rule.BackupChannels {
				sent, err := sendChannel(channelID)
				if err != nil {
					return err
				}
				if sent {
					sentAny = true
				}
			}
		}
		if trackSuppression && sentAny {
			_ = s.recordSuppressionSent(ctx, rule.ID, fingerprint, time.Now())
		}
	}
	if hasLifecycle && lifecycle.Phase == "resolve" {
		_ = s.deleteTelegramMessageStates(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	}
	return nil
}

// FlushGroupSummaries 把窗口期结束、且合并过内容的告警补发一条汇总。
//
// 由后台定时器调用（见 RunAggregationFlusher）。只处理「合并过至少一条」的
// 窗口：没有合并就没有汇总，避免给正常单条告警补发无意义的消息。
func (s *Service) FlushGroupSummaries(ctx context.Context, now time.Time) {
	if s.aggregator == nil {
		return
	}
	summaries := s.aggregator.Flush(now)
	if len(summaries) == 0 {
		return
	}
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		cfg = GlobalConfig{MaxRetryTimes: 3, RetryIntervalSeconds: 60, GlobalRateLimitPerHr: 100}
	}
	for _, summary := range summaries {
		rule, ok, err := s.loadRuleByID(ctx, summary.RuleID)
		if err != nil || !ok {
			continue
		}
		for _, channelID := range rule.Channels {
			channel, found, err := s.loadStoredChannel(ctx, channelID)
			if err != nil || !found || channel.Enabled == 0 {
				continue
			}
			if allowed, _ := s.rateLimiter.Allow(time.Now(), cfg.GlobalRateLimitPerHr); !allowed {
				continue
			}
			channelConfig := decryptConfig(channel.ConfigRaw)
			send := func() (deliveryResult, error) {
				return s.sendToChannel(ctx, channel, channelConfig, summary.Title, summary.Body)
			}
			// 汇总通知失败不重试到影响后续窗口：它是补充信息，不是关键告警。
			_, _, _ = s.sendWithRetry(ctx, send, cfg.MaxRetryTimes, cfg.RetryIntervalSeconds)
		}
	}
}

// RunAggregationFlusher 启动后台汇总循环。ctx 取消即退出。
//
// 间隔取窗口的一半：既不至于让汇总迟到太久，也不会空转太频繁。
func (s *Service) RunAggregationFlusher(ctx context.Context) {
	if s.aggregator == nil {
		return
	}
	interval := s.aggregator.window / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.FlushGroupSummaries(ctx, now)
			// 通知历史 TTL:每小时按 history_retention_days 清理一次,避免无限增长。
			if lastPrune.IsZero() || now.Sub(lastPrune) >= time.Hour {
				lastPrune = now
				if cfg, err := s.LoadConfig(ctx); err == nil {
					_, _ = s.PruneHistory(ctx, cfg.HistoryRetentionDays)
				}
			}
		}
	}
}

func (s *Service) loadRuleByID(ctx context.Context, ruleID string) (Rule, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Rule{}, false, err
	}
	defer db.Close()
	row := db.QueryRowContext(ctx, `
		SELECT id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until, created_at, updated_at
		FROM alert_rules WHERE id = ?
	`, ruleID)
	rule, err := scanRule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Rule{}, false, nil
		}
		return Rule{}, false, err
	}
	return rule, true, nil
}

// RefreshLifecycle updates an active Telegram lifecycle message without re-sending other channels.
// 支持 open（刷新进行中事件）与 resolve（自愈恢复：把残留 open 消息编辑为恢复内容并清除状态）。
func (s *Service) RefreshLifecycle(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	lifecycle, ok := notificationMessageLifecycle(sourceModule, eventType, eventData)
	if !ok {
		return nil
	}
	var err error
	eventData, err = s.enrichLifecycleEventData(ctx, sourceModule, eventType, lifecycle, eventData, time.Now())
	if err != nil {
		return err
	}
	if lifecycle.Phase == "open" {
		eventData["lifecycleMutation"] = "refresh"
	}
	rules, err := s.loadEnabledRulesByEvent(ctx, sourceModule, eventType)
	if err != nil {
		return err
	}
	loc, _ := s.systemLocation(ctx)
	for _, rule := range rules {
		dryRun, err := s.DryRun(ctx, rule, eventData)
		if err != nil {
			return err
		}
		if dryRun["wouldNotify"] != true {
			continue
		}
		for _, channelID := range rule.Channels {
			channel, found, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if !found || channel.Enabled == 0 || channel.Type != "telegram" {
				continue
			}
			state, found, err := s.loadTelegramMessageState(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
			if err != nil {
				return err
			}
			if !found || !telegramLifecycleRefreshDue(state.UpdatedAt, time.Now()) {
				continue
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData, loc)
			config := decryptConfig(channel.ConfigRaw)
			if err := s.editTelegram(ctx, config, state.ChatID, state.MessageID, title, message); err == nil {
				s.completeLifecycleRefresh(ctx, lifecycle, state, eventType, eventData, title, message, rule, channel.ID, loc)
				continue
			}
			delivery, sendErr := s.sendTelegram(ctx, config, title, message)
			if sendErr != nil {
				continue
			}
			s.completeLifecycleRefresh(ctx, lifecycle, state, eventType, eventData, title, message, rule, channel.ID, loc)
			_ = s.upsertTelegramMessageState(ctx, telegramMessageState{
				ChannelID: channel.ID, SourceModule: sourceModule, ResourceKey: lifecycle.ResourceKey,
				Kind: lifecycle.Kind, ChatID: delivery.ChatID, MessageID: delivery.MessageID,
			}, eventType, eventData)
		}
	}
	// resolve 兜底：后端重启后残留 open 状态且无恢复规则覆盖时，仍把动态消息编辑为恢复内容并清除状态。
	// 顺带把上文规则循环中 edit/重发均失败的残留状态再尝试一次。
	if lifecycle.Phase == "resolve" {
		s.reconcileStaleLifecycleMessages(ctx, sourceModule, eventType, lifecycle, eventData, loc)
	}
	return nil
}

// completeLifecycleRefresh 记录一次生命周期刷新历史；resolve 场景同时清除该渠道的消息状态。
func (s *Service) completeLifecycleRefresh(ctx context.Context, lifecycle messageLifecycle, state telegramMessageState, eventType string, eventData map[string]interface{}, title, message string, rule Rule, channelID string, loc *time.Location) {
	_ = s.recordLifecycleRefreshHistory(ctx, rule, channelID, title, message, eventData, loc)
	if lifecycle.Phase == "resolve" {
		_ = s.deleteTelegramMessageStateForChannel(ctx, channelID, lifecycle.SourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	} else {
		_ = s.touchTelegramMessageState(ctx, state, eventType, eventData)
	}
}

// reconcileStaleLifecycleMessages 处理规则未覆盖/刷新失败的残留生命周期状态：
// 逐一编辑为恢复内容（失败则重发新消息），成功后按渠道清除状态。
func (s *Service) reconcileStaleLifecycleMessages(ctx context.Context, sourceModule, eventType string, lifecycle messageLifecycle, eventData map[string]interface{}, loc *time.Location) {
	states, err := s.listTelegramMessageStates(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return
	}
	for _, state := range states {
		channel, found, err := s.loadStoredChannel(ctx, state.ChannelID)
		if err != nil || !found || channel.Enabled == 0 || channel.Type != "telegram" {
			continue
		}
		config := decryptConfig(channel.ConfigRaw)
		fallbackRule := Rule{
			Name:      firstNonEmpty(notificationSubject(eventData), "恢复通知"),
			EventType: eventType,
			Severity:  "warning",
		}
		title := formatTitle(fallbackRule, eventData)
		message := formatMessage(fallbackRule, eventData, loc)
		if err := s.editTelegram(ctx, config, state.ChatID, state.MessageID, title, message); err == nil {
			_ = s.deleteTelegramMessageStateForChannel(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
			_ = s.recordLifecycleRefreshHistory(ctx, fallbackRule, channel.ID, title, message, eventData, loc)
			continue
		}
		if _, sendErr := s.sendTelegram(ctx, config, title, message); sendErr != nil {
			continue
		}
		_ = s.deleteTelegramMessageStateForChannel(ctx, channel.ID, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
		_ = s.recordLifecycleRefreshHistory(ctx, fallbackRule, channel.ID, title, message, eventData, loc)
	}
}

func (s *Service) enrichLifecycleEventData(ctx context.Context, sourceModule, eventType string, lifecycle messageLifecycle, eventData map[string]interface{}, now time.Time) (map[string]interface{}, error) {
	result := cloneNotificationData(eventData)
	state, found, err := s.loadAnyTelegramMessageState(ctx, sourceModule, lifecycle.ResourceKey, lifecycle.Kind)
	if err != nil {
		return nil, err
	}
	startedAt := now.UTC()
	previousData := map[string]interface{}{}
	if found {
		if parsed, ok := parseLifecycleStateTime(state.CreatedAt); ok {
			startedAt = parsed
		}
		previousData = parseObject(state.LastData)
		result["lifecyclePreviousEvent"] = state.EventType
	}
	result["lifecycleKind"] = lifecycle.Kind
	result["lifecyclePhase"] = lifecycle.Phase
	result["lifecycleMutation"] = lifecycle.Phase
	result["lifecycleResourceKey"] = lifecycle.ResourceKey
	result["lifecycleStartedAt"] = startedAt.Format(time.RFC3339)
	// 可用性事件的持续时间以真实断开时间为起点，避免把离线判定延迟也算进故障时长。
	durationStart := startedAt
	if lifecycle.Kind == "availability" {
		disconnectedAt := resolveNotificationDisconnectTime(result, previousData, startedAt, now, lifecycle.Phase == "resolve")
		result["disconnectedAt"] = disconnectedAt.UTC().Format(time.RFC3339)
		durationStart = disconnectedAt
		if lifecycle.Phase == "resolve" {
			result["recoveredAt"] = now.UTC().Format(time.RFC3339)
		}
	}
	if found && stringValue(result["downDuration"]) == "" {
		result["downDuration"] = formatNotificationDuration(now.Sub(durationStart))
	}
	if len(previousData) > 0 {
		result["lifecycleChanges"] = notificationDataChanges(previousData, eventData)
	}
	return result, nil
}

// resolveNotificationDisconnectTime 推断主机/监控项的断开时间：
//  1. 事件自带 downDurationMs（如 uptime 故障时长）时，用 now - 时长还原故障起点；
//  2. 恢复（resolve）阶段优先沿用上一段 open 消息已记录的断开时间/最后活跃时间；
//     恢复事件自身的 lastActive 基本等于恢复时刻，不能拿来当断开时间；
//  3. open 阶段取事件里的断开时间/最后活跃时间；
//  4. 都缺失时退回生命周期开始时间。
func resolveNotificationDisconnectTime(current, previous map[string]interface{}, startedAt, now time.Time, resolve bool) time.Time {
	if durationMs := intValue(current["downDurationMs"], 0); durationMs > 0 {
		return now.Add(-time.Duration(durationMs) * time.Millisecond)
	}
	pick := func(data map[string]interface{}) (time.Time, bool) {
		if parsed, ok := parseLifecycleStateTime(stringValue(data["disconnectedAt"])); ok {
			return parsed, true
		}
		parsed, ok := parseLifecycleStateTime(stringValue(data["lastActive"]))
		return parsed, ok
	}
	if resolve && len(previous) > 0 {
		if parsed, ok := pick(previous); ok {
			return parsed
		}
		// 上一段状态没留下时间时，仍可用本次事件携带的真实断开时间。
		if parsed, ok := parseLifecycleStateTime(stringValue(current["disconnectedAt"])); ok {
			return parsed
		}
		return startedAt
	}
	if parsed, ok := pick(current); ok {
		return parsed
	}
	return startedAt
}

// previewAvailabilityData 为模板预览补齐可用性事件的时间字段，口径与线上
// enrichLifecycleEventData 一致，让用户保存规则前就能看到断开/恢复时间。
func previewAvailabilityData(data map[string]interface{}, sourceModule, eventType string, now time.Time) map[string]interface{} {
	if len(data) == 0 {
		return data
	}
	phase, ok := notificationAvailabilityPhase(sourceModule, eventType)
	if !ok {
		return data
	}
	result := cloneNotificationData(data)
	startedAt := now.UTC()
	if parsed, ok := parseLifecycleStateTime(stringValue(result["lastActive"])); ok {
		startedAt = parsed
	}
	if stringValue(result["disconnectedAt"]) == "" {
		result["disconnectedAt"] = resolveNotificationDisconnectTime(result, nil, startedAt, now, false).UTC().Format(time.RFC3339)
	}
	if phase == "resolve" && stringValue(result["recoveredAt"]) == "" {
		result["recoveredAt"] = now.UTC().Format(time.RFC3339)
	}
	return result
}

// notificationAvailabilityPhase 判断事件是否属于可用性生命周期及其阶段。
// 与 notificationMessageLifecycle 的可用性分支保持一致，但不要求资源 ID，
// 供模板预览这类没有真实资源记录的场合复用。
func notificationAvailabilityPhase(sourceModule, eventType string) (string, bool) {
	sourceModule = strings.ToLower(strings.TrimSpace(sourceModule))
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	switch sourceModule {
	case "uptime":
		switch eventType {
		case "down":
			return "open", true
		case "up":
			return "resolve", true
		}
	case "server":
		switch eventType {
		case "interrupted", "offline", "degraded":
			return "open", true
		case "online":
			return "resolve", true
		}
	}
	return "", false
}

func (s *Service) recordLifecycleRefreshHistory(ctx context.Context, rule Rule, channelID, title, message string, eventData map[string]interface{}, loc *time.Location) error {
	logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
	if err != nil {
		return err
	}
	now := time.Now()
	if loc != nil {
		now = now.In(loc)
	}
	formatted := now.Format(time.RFC3339)
	return s.updateHistoryStatus(ctx, logID, "sent", &formatted, nil)
}

// ruleGroupBy 从规则配置里读 group_by（字符串数组）；未配置返回 nil。
//
// 放在 suppression 字段里而不是新增列：`suppression` 本就是自由结构的
// JSON（前端已经在往里写 repeat_count/silence_minutes），新增键不需要
// 数据库迁移，老数据读出来自然为空 —— 也就自然保持「不聚合」的旧行为。
func ruleGroupBy(rule Rule) []string {
	if rule.Suppression == nil {
		return nil
	}
	raw, ok := rule.Suppression["group_by"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}

// decisionForGroup 交给聚合器判定这条是否要发。
//
// 聚合器为 nil（测试里直接构造 Service 的场景）时一律放行，保持向后兼容。
// withDashboardURL 把全局 base_url 注入模板数据({{baseUrl}} / {{dashboardUrl}}),
// 使通知模板可以附带面板看板链接;事件自带同名字段时保留事件值。
func withDashboardURL(eventData map[string]interface{}, baseURL string) map[string]interface{} {
	if _, ok := eventData["baseUrl"]; ok {
		return eventData
	}
	result := cloneNotificationData(eventData)
	base := strings.TrimRight(baseURL, "/")
	result["baseUrl"] = base
	result["dashboardUrl"] = base
	return result
}

func (s *Service) decisionForGroup(rule Rule, key string, eventData map[string]interface{}, now time.Time) bool {
	if s.aggregator == nil {
		return true
	}
	title := formatTitle(rule, eventData)
	return s.aggregator.Offer(rule.ID, key, rule.Severity, title, now).Send
}

func (s *Service) loadEnabledRulesByEvent(ctx context.Context, sourceModule, eventType string) ([]Rule, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, source_module, event_type, severity, enabled, channels,
			conditions, suppression, time_window, description, title_template,
			message_template, backup_channels, quiet_until, created_at, updated_at
		FROM alert_rules
		WHERE source_module = ? AND event_type = ? AND enabled = 1
	`, sourceModule, eventType)
	if err != nil {
		return nil, fmt.Errorf("load matching alert rules: %w", err)
	}
	defer rows.Close()
	rules := []Rule{}
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Service) matchMaintenance(ctx context.Context, eventData map[string]interface{}) (map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, target_type, target_id, start_at, end_at, reason, created_at
		FROM maintenance_schedules
		WHERE datetime(start_at) <= datetime('now') AND datetime(end_at) >= datetime('now')
	`)
	if err != nil {
		return nil, fmt.Errorf("load maintenance schedules: %w", err)
	}
	defer rows.Close()
	monitorID := stringValue(eventData["monitorId"])
	serverID := stringValue(eventData["serverId"])
	for rows.Next() {
		var id, targetType, startAt, endAt, createdAt string
		var targetID, reason sql.NullString
		if err := rows.Scan(&id, &targetType, &targetID, &startAt, &endAt, &reason, &createdAt); err != nil {
			return nil, err
		}
		if targetType == "global" || (targetType == "monitor" && targetID.String == monitorID) || (targetType == "server" && targetID.String == serverID) {
			return map[string]interface{}{
				"id":          id,
				"target_type": targetType,
				"target_id":   nullableStringPtr(targetID),
				"start_at":    startAt,
				"end_at":      endAt,
				"reason":      nullableStringPtr(reason),
				"created_at":  createdAt,
			}, nil
		}
	}
	return nil, rows.Err()
}

func notificationMessageLifecycle(sourceModule, eventType string, eventData map[string]interface{}) (messageLifecycle, bool) {
	sourceModule = strings.ToLower(strings.TrimSpace(sourceModule))
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	lifecycle := messageLifecycle{SourceModule: sourceModule}
	switch sourceModule {
	case "uptime":
		lifecycle.ResourceKey = stringValue(eventData["monitorId"])
		if phase, ok := notificationAvailabilityPhase(sourceModule, eventType); ok {
			lifecycle.Kind, lifecycle.Phase = "availability", phase
		}
	case "server":
		lifecycle.ResourceKey = stringValue(eventData["serverId"])
		if phase, ok := notificationAvailabilityPhase(sourceModule, eventType); ok {
			lifecycle.Kind, lifecycle.Phase = "availability", phase
			break
		}
		switch eventType {
		case "traffic_high":
			lifecycle.Kind = "traffic"
			lifecycle.Phase = "open"
		case "traffic_normal":
			lifecycle.Kind = "traffic"
			lifecycle.Phase = "resolve"
		case "cpu_high":
			lifecycle.Kind = "cpu"
			lifecycle.Phase = "open"
		case "cpu_normal":
			lifecycle.Kind = "cpu"
			lifecycle.Phase = "resolve"
		case "memory_high":
			lifecycle.Kind = "memory"
			lifecycle.Phase = "open"
		case "memory_normal":
			lifecycle.Kind = "memory"
			lifecycle.Phase = "resolve"
		case "disk_high":
			lifecycle.Kind = "disk"
			lifecycle.Phase = "open"
		case "disk_normal":
			lifecycle.Kind = "disk"
			lifecycle.Phase = "resolve"
		}
	case "system":
		lifecycle.ResourceKey = firstNonEmpty(stringValue(eventData["serverId"]), "local-host")
		lifecycle.Kind, lifecycle.Phase = metricNotificationLifecycle(eventType)
	case "github":
		lifecycle.ResourceKey = stringValue(eventData["repositoryId"])
		lifecycle.Kind = "actions"
		switch eventType {
		case "action_failed":
			lifecycle.Phase = "open"
		case "action_recovered":
			lifecycle.Phase = "resolve"
		}
	}
	if lifecycle.ResourceKey == "" || lifecycle.Kind == "" || lifecycle.Phase == "" {
		return messageLifecycle{}, false
	}
	return lifecycle, true
}

func metricNotificationLifecycle(eventType string) (string, string) {
	for _, metric := range []string{"cpu", "memory", "disk", "traffic"} {
		switch eventType {
		case metric + "_high":
			return metric, "open"
		case metric + "_normal":
			return metric, "resolve"
		}
	}
	return "", ""
}
