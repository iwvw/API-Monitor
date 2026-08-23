package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestShouldSendSuppression(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	lastSent := base.Add(-20 * time.Minute)
	cases := []struct {
		name          string
		state         suppressionState
		now           time.Time
		repeatCount   int
		silenceMin    int
		want          bool
	}{
		{"no state means first occurrence always sends", suppressionState{}, base, 1, 0, true},
		{"state with zero time always sends", suppressionState{OccurrenceCount: 3}, base, 1, 0, true},
		{"within silence window suppressed", suppressionState{OccurrenceCount: 3, LastNotifiedAt: base.Add(-10 * time.Minute)}, base, 3, 30, false},
		{"after silence window uses count", suppressionState{OccurrenceCount: 3, LastNotifiedAt: lastSent}, base, 3, 5, true},
		{"outside silence below repeat count suppressed", suppressionState{OccurrenceCount: 1, LastNotifiedAt: lastSent}, base, 3, 5, false},
		{"exactly silence boundary uses count", suppressionState{OccurrenceCount: 2, LastNotifiedAt: base.Add(-5 * time.Minute)}, base, 2, 5, true},
		{"repeat count below one clamps to one", suppressionState{OccurrenceCount: 1, LastNotifiedAt: lastSent}, base, 0, 0, true},
		{"zero silence skips window", suppressionState{OccurrenceCount: 1, LastNotifiedAt: base.Add(-1 * time.Second)}, base, 1, 0, true},
		{"future last notified not treated as silence", suppressionState{OccurrenceCount: 2, LastNotifiedAt: base.Add(5 * time.Minute)}, base, 2, 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSendSuppression(tc.state, tc.now, tc.repeatCount, tc.silenceMin); got != tc.want {
				t.Fatalf("shouldSendSuppression(%#v, %v, %d, %d) = %v want %v", tc.state, tc.now, tc.repeatCount, tc.silenceMin, got, tc.want)
			}
		})
	}
}

func TestQuietUntilActive(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	future := ptr(now.Add(time.Hour).Format(time.RFC3339))
	past := ptr(now.Add(-time.Hour).Format(time.RFC3339))
	futureSpace := ptr(now.Add(2 * time.Hour).UTC().Format("2006-01-02 15:04:05"))
	cases := []struct {
		name      string
		quietUntil *string
		now       time.Time
		want      bool
	}{
		{"nil not active", nil, now, false},
		{"empty not active", ptr(""), now, false},
		{"future active", future, now, true},
		{"past not active", past, now, false},
		{"sqlite space format future active", futureSpace, now, true},
		{"garbage not active", ptr("not-a-time"), now, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := quietUntilActive(tc.quietUntil, tc.now); got != tc.want {
				t.Fatalf("quietUntilActive(%v, %v) = %v want %v", tc.quietUntil, tc.now, got, tc.want)
			}
		})
	}
}

func TestHourlyRateLimiter(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	t.Run("unlimited passes everything", func(t *testing.T) {
		limiter := &hourlyRateLimiter{}
		for i := 0; i < 10; i++ {
			if allowed, _ := limiter.Allow(base, 0); !allowed {
				t.Fatalf("limit<=0 should always allow, iteration %d", i)
			}
		}
	})

	t.Run("caps within hour and rejects once", func(t *testing.T) {
		limiter := &hourlyRateLimiter{}
		for i := 0; i < 3; i++ {
			allowed, firstReject := limiter.Allow(base.Add(time.Duration(i)*time.Minute), 2)
			if i < 2 {
				if !allowed || firstReject {
					t.Fatalf("allow %d = (%v, firstReject=%v) want (true, false)", i, allowed, firstReject)
				}
			} else if i == 2 {
				if allowed || !firstReject {
					t.Fatalf("allow %d = (%v, firstReject=%v) want (false, true)", i, allowed, firstReject)
				}
			} else if firstReject {
				t.Fatalf("later reject should not log again, got firstReject at %d", i)
			}
		}
	})

	t.Run("resets on hour boundary", func(t *testing.T) {
		limiter := &hourlyRateLimiter{}
		first, _ := limiter.Allow(base, 1)
		second, _ := limiter.Allow(base, 1)
		if !first || second {
			t.Fatalf("hourly bucket should cap at 1: first=%v second=%v", first, second)
		}
		nextHour, _ := limiter.Allow(base.Add(time.Hour), 1)
		if !nextHour {
			t.Fatal("new hour bucket should reset count")
		}
	})
}

func TestSendWithRetrySucceedsAfterFailures(t *testing.T) {
	s := testNotificationService(t)
	attempts := 0
	_, retries, err := s.sendWithRetry(context.Background(), func() (deliveryResult, error) {
		attempts++
		if attempts < 3 {
			return deliveryResult{}, errors.New("transient failure")
		}
		return deliveryResult{MessageID: int64(attempts)}, nil
	}, 3, 0)
	if err != nil {
		t.Fatalf("sendWithRetry should succeed after retries: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3", attempts)
	}
	if retries != 2 {
		t.Fatalf("retries = %d want 2", retries)
	}
}

func TestSendWithRetryGivesUpAndReportsRetries(t *testing.T) {
	s := testNotificationService(t)
	attempts := 0
	_, retries, err := s.sendWithRetry(context.Background(), func() (deliveryResult, error) {
		attempts++
		return deliveryResult{}, errors.New("always failing")
	}, 2, 0)
	if err == nil {
		t.Fatal("sendWithRetry should return the final error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3 (initial + 2 retries)", attempts)
	}
	if retries != 2 {
		t.Fatalf("retries = %d want 2", retries)
	}
}

func TestSendWithRetryRespectsContextCancellation(t *testing.T) {
	s := testNotificationService(t)
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, _, err := s.sendWithRetry(ctx, func() (deliveryResult, error) {
		attempts++
		cancel()
		return deliveryResult{}, errors.New("fail")
	}, 10, 60)
	if err == nil {
		t.Fatal("cancelled context should surface error")
	}
	if attempts != 1 {
		t.Fatalf("should stop after cancellation, attempts = %d", attempts)
	}
}

// TestTriggerSuppressionRepeatCountSendSuppressSend 验证「发-抑-发」：
// repeat_count=2/silence=0（无静默窗）三次触发，应恰好发送两次（首末）。
func TestTriggerSuppressionRepeatCountSendSuppressSend(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	sends := 0
	service.client = telegramRoundTrip(t, func(method string) {
		if method == "sendRichMessage" {
			sends++
		}
	})
	channel := createTelegramChannel(t, service)
	rule, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Task Failed", "source_module": "cron", "event_type": "task.failed",
		"channels": []string{channel.ID},
		"suppression": map[string]interface{}{"repeat_count": 2, "silence_minutes": 0},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	data := map[string]interface{}{"taskName": "nightly-sync", "summary": "boom"}
	for i := 0; i < 3; i++ {
		if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
			t.Fatalf("trigger %d: %v", i+1, err)
		}
	}
	if sends != 2 {
		t.Fatalf("sends = %d want 2 (发-抑-发)", sends)
	}
	if err := verifySuppressionState(t, service, rule, data, 1); err != nil {
		t.Fatalf("suppression state after reset: %v", err)
	}
}

// TestTriggerSuppressionSilenceWindowWithinAndAfter 验证静默窗内必抑、
// 窗外按计数恢复发送。
func TestTriggerSuppressionSilenceWindowWithinAndAfter(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	sends := 0
	service.client = telegramRoundTrip(t, func(method string) {
		if method == "sendRichMessage" {
			sends++
		}
	})
	channel := createTelegramChannel(t, service)
	rule, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Task Failed", "source_module": "cron", "event_type": "task.failed",
		"channels": []string{channel.ID},
		"suppression": map[string]interface{}{"repeat_count": 1, "silence_minutes": 30},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	data := map[string]interface{}{"taskName": "nightly-sync", "summary": "boom"}
	if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if sends != 1 {
		t.Fatalf("first trigger sends = %d want 1", sends)
	}
	// 静默窗内：即使 repeat_count=1 也抑制
	if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if sends != 1 {
		t.Fatalf("silence window should suppress, sends = %d want 1", sends)
	}
	// 把上次发送时间拨回静默窗外，第三次触发恢复发送
	ageLastNotified(t, service, rule, data, 40*time.Minute)
	if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
		t.Fatalf("third trigger: %v", err)
	}
	if sends != 2 {
		t.Fatalf("after silence window sends = %d want 2", sends)
	}
}

// TestTriggerSuppressionQuietUntilSkips 验证 quiet_until 未来时间整段跳过、
// 到期后恢复。
func TestTriggerSuppressionQuietUntilSkips(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	sends := 0
	service.client = telegramRoundTrip(t, func(method string) {
		if method == "sendRichMessage" {
			sends++
		}
	})
	channel := createTelegramChannel(t, service)
	rule, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Task Failed", "source_module": "cron", "event_type": "task.failed",
		"channels": []string{channel.ID},
		"quiet_until": time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	data := map[string]interface{}{"taskName": "nightly-sync"}
	if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
		t.Fatalf("trigger during quiet: %v", err)
	}
	if sends != 0 {
		t.Fatalf("quiet_until should suppress all sends, sends = %d want 0", sends)
	}
	if err := service.UpdateRule(ctx, rule.ID, map[string]interface{}{
		"quiet_until": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("update quiet_until to past: %v", err)
	}
	if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
		t.Fatalf("trigger after quiet expires: %v", err)
	}
	if sends != 1 {
		t.Fatalf("after quiet expires sends = %d want 1", sends)
	}
}

// TestTriggerRateLimitCaps 验证全局小时限流封顶：限 2 次、触发 3 次只发 2 次。
func TestTriggerRateLimitCaps(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	sends := 0
	service.client = telegramRoundTrip(t, func(method string) {
		if method == "sendRichMessage" {
			sends++
		}
	})
	if err := service.UpdateConfig(ctx, map[string]interface{}{
		"global_rate_limit_per_hour": 2,
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	channel := createTelegramChannel(t, service)
	if _, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Task Failed", "source_module": "cron", "event_type": "task.failed",
		"channels": []string{channel.ID},
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	data := map[string]interface{}{"taskName": "nightly-sync"}
	for i := 0; i < 3; i++ {
		if err := service.Trigger(ctx, "cron", "task.failed", data); err != nil {
			t.Fatalf("trigger %d: %v", i+1, err)
		}
	}
	if sends != 2 {
		t.Fatalf("rate limit should cap at 2, sends = %d", sends)
	}
}

// TestTriggerRetryOnTransientFailure 验证发送失败按 MaxRetryTimes 重试并落历史 retry_count。
func TestTriggerRetryOnTransientFailure(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	attempts := 0
	service.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 5 {
			return nil, errors.New("dial timeout")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1,"chat":{"id":10001}}}`))}, nil
	})}
	if err := service.UpdateConfig(ctx, map[string]interface{}{
		"retry_interval_seconds": 0,
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	channel := createTelegramChannel(t, service)
	if _, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Task Failed", "source_module": "cron", "event_type": "task.failed",
		"channels": []string{channel.ID},
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := service.Trigger(ctx, "cron", "task.failed", map[string]interface{}{"taskName": "x"}); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d want 5 (rich+fallback across 1 initial + 2 retries)", attempts)
	}
	history, err := service.LoadHistory(ctx, "sent", 10)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 1 || history[0].RetryCount != 2 {
		t.Fatalf("history should record retries = 2, got %#v", history)
	}
}

// TestTriggerResolveSkipsSuppression 验证生命周期 resolve（恢复）阶段不被重复抑制卡住。
func TestTriggerResolveSkipsSuppression(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	calls := []string{}
	nextID := int64(100)
	service.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload := map[string]interface{}{}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		method := strings.TrimPrefix(req.URL.Path, "/bot123456:test-token/")
		mid := int64(intValue(payload["message_id"], 0))
		if method == "sendRichMessage" || method == "sendMessage" {
			nextID++
			mid = nextID
		}
		calls = append(calls, method)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"chat":{"id":10001}}}`, mid)))}, nil
	})}
	channel := createTelegramChannel(t, service)
	if _, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Uptime Down", "source_module": "uptime", "event_type": "down",
		"channels": []string{channel.ID},
		"suppression": map[string]interface{}{"repeat_count": 100, "silence_minutes": 0},
	}); err != nil {
		t.Fatalf("create down rule: %v", err)
	}
	if _, err := service.CreateRule(ctx, map[string]interface{}{
		"name": "Uptime Up", "source_module": "uptime", "event_type": "up",
		"channels": []string{channel.ID},
		"suppression": map[string]interface{}{"repeat_count": 100, "silence_minutes": 0},
	}); err != nil {
		t.Fatalf("create up rule: %v", err)
	}
	data := map[string]interface{}{"monitorId": 7, "monitorName": "API", "error": "timeout"}
	if err := service.Trigger(ctx, "uptime", "down", data); err != nil {
		t.Fatalf("trigger down: %v", err)
	}
	// 恢复事件即使 suppress 水位很高（repeat_count=100）也必须发出（edit 原消息）
	if err := service.Trigger(ctx, "uptime", "up", map[string]interface{}{"monitorId": 7, "monitorName": "API"}); err != nil {
		t.Fatalf("trigger up (resolve): %v", err)
	}
	if len(calls) != 2 || calls[1] != "editMessageText" {
		t.Fatalf("resolve should not be suppressed, calls = %#v", calls)
	}
}

func telegramRoundTrip(t *testing.T, onSend func(method string)) *http.Client {
	t.Helper()
	nextID := int64(100)
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload := map[string]interface{}{}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode telegram request: %v", err)
		}
		method := strings.TrimPrefix(req.URL.Path, "/bot123456:test-token/")
		mid := int64(intValue(payload["message_id"], 0))
		if method == "sendRichMessage" || method == "sendMessage" {
			nextID++
			mid = nextID
		}
		onSend(method)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"chat":{"id":10001}}}`, mid)))}, nil
	})}
}

func createTelegramChannel(t *testing.T, service *Service) Channel {
	t.Helper()
	channel, err := service.CreateChannel(context.Background(), map[string]interface{}{
		"name": "Ops Telegram", "type": "telegram", "enabled": true,
		"config": map[string]interface{}{"bot_token": "123456:test-token", "chat_id": "10001"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func ageLastNotified(t *testing.T, service *Service, rule Rule, data map[string]interface{}, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	fp := generateFingerprint(rule, data)
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
		UPDATE alert_state_tracking SET last_notified_at = datetime('now', ?)
		WHERE fingerprint = ?
	`, fmt.Sprintf("-%d seconds", int64(age.Seconds())), fp); err != nil {
		t.Fatalf("age last_notified_at: %v (fp=%s)", err, fp)
	}
}

func verifySuppressionState(t *testing.T, service *Service, rule Rule, data map[string]interface{}, wantCount int) error {
	t.Helper()
	ctx := context.Background()
	fp := generateFingerprint(rule, data)
	db, err := service.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	var count int
	var lastNotified sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT consecutive_failures, last_notified_at FROM alert_state_tracking
		WHERE fingerprint = ?
	`, fp).Scan(&count, &lastNotified); err != nil {
		return fmt.Errorf("query state: %w", err)
	}
	if count != wantCount {
		return fmt.Errorf("consecutive_failures = %d want %d", count, wantCount)
	}
	if lastNotified.Valid && lastNotified.String == "" {
		return errors.New("last_notified_at empty")
	}
	return nil
}