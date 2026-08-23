package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// maxRetryWait 是发送失败重试的单次最大等待时间：Trigger 跑在调用方监控关键路径上
// （uptime/presence/cronjobs/github 同步调用），用户配置的 retry_interval_seconds 可能
// 高达 60s，无论如何都不允许长时间拖住检查链。
const maxRetryWait = 5 * time.Second

// suppressionState 是 alert_state_tracking(rule_id, fingerprint) 单行在判定抑制时的快照。
// OccurrenceCount 为自上次成功发送以来已累计出现的次数（含最近一次记录；
// 发送成功后会重置为 1，见 recordSuppressionSent）。LastNotifiedAt 为上次成功发送时间。
type suppressionState struct {
	OccurrenceCount int
	LastNotifiedAt  time.Time
}

// shouldSendSuppression 判定本 fingerprint 的一次触发是否应发送。
// 语义（对应前端「重复抑制 N 次 / 静默期 M 分钟」）：
//   - 无记录或从未成功发送过：首次出现必发；
//   - 距上次发送不足 silence_minutes 分钟：一律静默；
//   - 静默窗外：自上次发送起累计出现 repeat_count 次（含本次）才重发，repeat_count<1 视为 1。
func shouldSendSuppression(state suppressionState, now time.Time, repeatCount, silenceMinutes int) bool {
	if state.OccurrenceCount <= 0 || state.LastNotifiedAt.IsZero() {
		return true
	}
	if silenceMinutes > 0 {
		if since := now.Sub(state.LastNotifiedAt); since >= 0 && since < time.Duration(silenceMinutes)*time.Minute {
			return false
		}
	}
	if repeatCount < 1 {
		repeatCount = 1
	}
	return state.OccurrenceCount >= repeatCount
}

// quietUntilActive 判断规则的 quiet_until 静默截止时间是否仍然生效（未到期）。
// 支持 RFC3339 与 SQLite 空格时间格式；无法解析视为未生效（宁发不漏）。
func quietUntilActive(quietUntil *string, now time.Time) bool {
	if quietUntil == nil || strings.TrimSpace(*quietUntil) == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, *quietUntil)
		if err == nil {
			return !now.After(parsed)
		}
	}
	return false
}

// hourlyRateLimiter 是进程内小时窗限流（UTC 桶）：先判后计，窗口翻转重置。
// limit<=0 表示不限。供 Trigger 渠道发送前调用，属于全局限流
// （notification_global_config.global_rate_limit_per_hour）。
type hourlyRateLimiter struct {
	mu           sync.Mutex
	hour         int64
	count        int
	rejectLogged bool
}

// Allow 判定是否放行，并返回本窗口是否首次拒绝（供调用方打单条 Warn 日志，
// 避免逐事件刷屏）。limit<=0 时不限流。
func (r *hourlyRateLimiter) Allow(now time.Time, limit int) (allowed, firstReject bool) {
	if limit <= 0 {
		return true, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	hour := now.UTC().Unix() / 3600
	if hour != r.hour {
		r.hour = hour
		r.count = 0
		r.rejectLogged = false
	}
	if r.count < limit {
		r.count++
		return true, false
	}
	firstReject = !r.rejectLogged
	r.rejectLogged = true
	return false, firstReject
}

// loadSuppressionState 读取 alert_state_tracking(rule_id, fingerprint) 的快照。
func (s *Service) loadSuppressionState(ctx context.Context, ruleID, fingerprint string) (suppressionState, bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return suppressionState{}, false, err
	}
	defer db.Close()
	var consecutive int
	var lastNotified sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT consecutive_failures, COALESCE(last_notified_at, '')
		FROM alert_state_tracking
		WHERE rule_id = ? AND fingerprint = ?
	`, ruleID, fingerprint).Scan(&consecutive, &lastNotified)
	if errors.Is(err, sql.ErrNoRows) {
		return suppressionState{}, false, nil
	}
	if err != nil {
		return suppressionState{}, false, fmt.Errorf("load suppression state: %w", err)
	}
	state := suppressionState{OccurrenceCount: consecutive}
	if lastNotified.Valid && lastNotified.String != "" {
		if parsed, ok := parseLifecycleStateTime(lastNotified.String); ok {
			state.LastNotifiedAt = parsed
		}
	}
	return state, true, nil
}

// evaluateSuppression 判定本次触发是否应被抑制，并把本次出现计入
// alert_state_tracking（无论判决如何都累计次数）。返回 true 表示抑制。
// DB 读取失败透传错误由调用方 fail-open（宁多发不漏发）。
func (s *Service) evaluateSuppression(ctx context.Context, rule Rule, fingerprint string, now time.Time) (bool, error) {
	state, found, err := s.loadSuppressionState(ctx, rule.ID, fingerprint)
	if err != nil {
		return false, err
	}
	suppress := true
	if !found {
		// 首次出现必发
		suppress = false
	} else {
		repeatCount := intValue(rule.Suppression["repeat_count"], 1)
		silenceMinutes := intValue(rule.Suppression["silence_minutes"], 0)
		if shouldSendSuppression(state, now, repeatCount, silenceMinutes) {
			suppress = false
		}
	}
	if err := s.recordSuppressionOccurrence(ctx, rule.ID, fingerprint, now); err != nil {
		return false, err
	}
	return suppress, nil
}

// recordSuppressionOccurrence 记录一次出现：无行则插入（连续计数=1），有行则 +1。
func (s *Service) recordSuppressionOccurrence(ctx context.Context, ruleID, fingerprint string, now time.Time) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO alert_state_tracking (rule_id, fingerprint, last_triggered_at, consecutive_failures, updated_at)
		VALUES (?, ?, ?, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(rule_id, fingerprint) DO UPDATE SET
			last_triggered_at = excluded.last_triggered_at,
			consecutive_failures = consecutive_failures + 1,
			updated_at = CURRENT_TIMESTAMP
	`, ruleID, fingerprint, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record suppression occurrence: %w", err)
	}
	return nil
}

// recordSuppressionSent 在任一渠道发送成功后重置抑制水位（last_notified_at=now、
// consecutive_failures=1），作为下一轮重复抑制的基准。
func (s *Service) recordSuppressionSent(ctx context.Context, ruleID, fingerprint string, now time.Time) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE alert_state_tracking
		SET last_notified_at = ?, consecutive_failures = 1, updated_at = CURRENT_TIMESTAMP
		WHERE rule_id = ? AND fingerprint = ?
	`, now.UTC().Format(time.RFC3339), ruleID, fingerprint)
	if err != nil {
		return fmt.Errorf("record suppression sent: %w", err)
	}
	return nil
}

// sendWithRetry 执行一次发送；失败按 maxRetries 重试，每次等待取
// retryIntervalSeconds 与 5s 的较小者（Trigger 跑在监控关键路径上，不允许长时间阻塞），
// 并响应 ctx 取消。返回 (结果, 实际重试次数, 最终错误)。
func (s *Service) sendWithRetry(ctx context.Context, send func() (deliveryResult, error), maxRetries, retryIntervalSeconds int) (deliveryResult, int, error) {
	res, err := send()
	if err == nil || maxRetries <= 0 {
		return res, 0, err
	}
	retries := 0
	for attempt := 1; attempt <= maxRetries; attempt++ {
		wait := time.Duration(retryIntervalSeconds) * time.Second
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		select {
		case <-ctx.Done():
			return res, retries, ctx.Err()
		case <-time.After(wait):
		}
		res, err = send()
		retries = attempt
		if err == nil {
			return res, retries, nil
		}
	}
	return res, retries, err
}

// updateHistoryRetry 标记一条历史记录为重试后失败：记录实际重试次数与错误信息。
func (s *Service) updateHistoryRetry(ctx context.Context, id int64, retries int, errorMessage string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE notification_history
		SET status = 'failed', retry_count = ?, error_message = ?
		WHERE id = ?
	`, retries, errorMessage, id)
	if err != nil {
		return fmt.Errorf("update notification history failure: %w", err)
	}
	return nil
}

// updateHistoryRetryCount 在重试后发送成功的记录上回写实际重试次数。
func (s *Service) updateHistoryRetryCount(ctx context.Context, id int64, retries int) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE notification_history SET retry_count = ? WHERE id = ?
	`, retries, id)
	if err != nil {
		return fmt.Errorf("update notification history retry count: %w", err)
	}
	return nil
}