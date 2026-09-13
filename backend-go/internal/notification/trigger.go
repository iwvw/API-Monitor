package notification

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

)

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
			cfg = GlobalConfig{MaxRetryTimes: 3, RetryIntervalSeconds: 60, GlobalRateLimitPerHr: 100}
		}
		return cfg
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
		cfg := loadNotifyCfg()
		sentAny := false
		for _, channelID := range rule.Channels {
			channel, ok, err := s.loadStoredChannel(ctx, channelID)
			if err != nil {
				return err
			}
			if !ok || channel.Enabled == 0 {
				continue
			}
			if allowed, firstReject := s.rateLimiter.Allow(time.Now(), cfg.GlobalRateLimitPerHr); !allowed {
				if firstReject {
					slog.Warn("notification-global-rate-limit", "rule", rule.Name, "channel", channel.Name, "limit", cfg.GlobalRateLimitPerHr)
				}
				continue
			}
			title := formatTitle(rule, eventData)
			message := formatMessage(rule, eventData, loc)
			logID, err := s.createHistory(ctx, rule.ID, channelID, "pending", title, message, eventData, nil)
			if err != nil {
				return err
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
				continue
			}
			sentAny = true
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
		if stringValue(result["downDuration"]) == "" {
			result["downDuration"] = formatNotificationDuration(now.Sub(startedAt))
		}
	}
	result["lifecycleKind"] = lifecycle.Kind
	result["lifecyclePhase"] = lifecycle.Phase
	result["lifecycleMutation"] = lifecycle.Phase
	result["lifecycleResourceKey"] = lifecycle.ResourceKey
	result["lifecycleStartedAt"] = startedAt.Format(time.RFC3339)
	if len(previousData) > 0 {
		result["lifecycleChanges"] = notificationDataChanges(previousData, eventData)
	}
	return result, nil
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
		lifecycle.Kind = "availability"
		switch eventType {
		case "down":
			lifecycle.Phase = "open"
		case "up":
			lifecycle.Phase = "resolve"
		}
	case "server":
		lifecycle.ResourceKey = stringValue(eventData["serverId"])
		switch eventType {
		case "interrupted", "offline", "degraded":
			lifecycle.Kind = "availability"
			lifecycle.Phase = "open"
		case "online":
			lifecycle.Kind = "availability"
			lifecycle.Phase = "resolve"
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