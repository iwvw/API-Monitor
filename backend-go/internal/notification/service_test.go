package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testNotificationService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestChannelCRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)

	channel, err := service.CreateChannel(ctx, map[string]interface{}{
		"name":    "Ops Email",
		"type":    "email",
		"enabled": true,
		"config": map[string]interface{}{
			"host":   "smtp.example.com",
			"port":   587,
			"secure": false,
			"auth": map[string]interface{}{
				"user": "ops@example.com",
				"pass": "smtp-secret",
			},
			"to": "alerts@example.com",
		},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if channel.ID == "" || channel.Type != "email" || channel.Enabled != 1 {
		t.Fatalf("unexpected channel: %#v", channel)
	}
	if channel.Config["host"] != "smtp.example.com" || objectValue(channel.Config["auth"])["pass"] != "smtp-secret" {
		t.Fatalf("channel config was not decrypted: %#v", channel.Config)
	}

	channels, err := service.LoadChannels(ctx)
	if err != nil {
		t.Fatalf("load channels: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != channel.ID {
		t.Fatalf("unexpected channel list: %#v", channels)
	}

	if err := service.UpdateChannel(ctx, channel.ID, map[string]interface{}{
		"name":    "Ops Email Updated",
		"enabled": false,
		"config": map[string]interface{}{
			"host": "smtp2.example.com",
			"auth": map[string]interface{}{
				"user": "ops2@example.com",
				"pass": "next-secret",
			},
			"to": "alerts2@example.com",
		},
	}); err != nil {
		t.Fatalf("update channel: %v", err)
	}
	updated, ok, err := service.LoadChannel(ctx, channel.ID)
	if err != nil || !ok {
		t.Fatalf("load updated channel ok=%v err=%v", ok, err)
	}
	if updated.Name != "Ops Email Updated" || updated.Enabled != 0 || updated.Config["host"] != "smtp2.example.com" {
		t.Fatalf("unexpected updated channel: %#v", updated)
	}

	if err := service.DeleteChannel(ctx, channel.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	if _, ok, err := service.LoadChannel(ctx, channel.ID); err != nil || ok {
		t.Fatalf("channel should be deleted ok=%v err=%v", ok, err)
	}
}

func TestTelegramLifecycleMessagesForUptimeAndServer(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	type telegramCall struct {
		method    string
		chatID    string
		messageID int64
	}
	calls := []telegramCall{}
	nextMessageID := int64(100)
	service.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload := map[string]interface{}{}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode telegram request: %v", err)
		}
		method := strings.TrimPrefix(req.URL.Path, "/bot123456:test-token/")
		call := telegramCall{method: method, chatID: stringValue(payload["chat_id"]), messageID: int64(intValue(payload["message_id"], 0))}
		resultMessageID := call.messageID
		if method == "sendMessage" {
			nextMessageID++
			resultMessageID = nextMessageID
			call.messageID = resultMessageID
		}
		calls = append(calls, call)
		body := fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"chat":{"id":10001}}}`, resultMessageID)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	channel, err := service.CreateChannel(ctx, map[string]interface{}{
		"name": "Ops Telegram", "type": "telegram", "enabled": true,
		"config": map[string]interface{}{"bot_token": "123456:test-token", "chat_id": "10001"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	createRule := func(module, event string) {
		t.Helper()
		_, err := service.CreateRule(ctx, map[string]interface{}{
			"name": module + " " + event, "source_module": module, "event_type": event,
			"channels": []string{channel.ID},
		})
		if err != nil {
			t.Fatalf("create %s/%s rule: %v", module, event, err)
		}
	}
	for _, item := range [][2]string{
		{"uptime", "down"}, {"uptime", "up"},
		{"server", "interrupted"}, {"server", "offline"}, {"server", "online"},
	} {
		createRule(item[0], item[1])
	}

	uptimeData := map[string]interface{}{"monitorId": 7, "monitorName": "API", "error": "timeout"}
	if err := service.Trigger(ctx, "uptime", "down", uptimeData); err != nil {
		t.Fatalf("trigger uptime down: %v", err)
	}
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open notification state: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE notification_message_state SET updated_at = datetime('now', '-31 seconds')`); err != nil {
		db.Close()
		t.Fatalf("age notification state: %v", err)
	}
	db.Close()
	uptimeData["error"] = "connection refused"
	if err := service.RefreshLifecycle(ctx, "uptime", "down", uptimeData); err != nil {
		t.Fatalf("refresh uptime down: %v", err)
	}
	callCount := len(calls)
	if err := service.RefreshLifecycle(ctx, "uptime", "down", uptimeData); err != nil {
		t.Fatalf("throttle uptime refresh: %v", err)
	}
	if len(calls) != callCount {
		t.Fatalf("uptime refresh was not throttled: %#v", calls)
	}
	if err := service.Trigger(ctx, "uptime", "up", map[string]interface{}{
		"monitorId": 7, "monitorName": "API", "ping": 42, "downDuration": "1 分钟",
	}); err != nil {
		t.Fatalf("resolve uptime: %v", err)
	}

	serverData := map[string]interface{}{"serverId": "srv-1", "serverName": "Edge", "status": "interrupted"}
	if err := service.Trigger(ctx, "server", "interrupted", serverData); err != nil {
		t.Fatalf("trigger server interrupted: %v", err)
	}
	serverData["status"] = "offline"
	if err := service.Trigger(ctx, "server", "offline", serverData); err != nil {
		t.Fatalf("update server offline: %v", err)
	}
	serverData["status"] = "online"
	if err := service.Trigger(ctx, "server", "online", serverData); err != nil {
		t.Fatalf("resolve server online: %v", err)
	}
	serverData["status"] = "offline"
	if err := service.Trigger(ctx, "server", "offline", serverData); err != nil {
		t.Fatalf("trigger next server outage: %v", err)
	}

	wantMethods := []string{"sendMessage", "editMessageText", "editMessageText", "sendMessage", "editMessageText", "editMessageText", "sendMessage"}
	if len(calls) != len(wantMethods) {
		t.Fatalf("telegram call count = %d, want %d: %#v", len(calls), len(wantMethods), calls)
	}
	for index, want := range wantMethods {
		if calls[index].method != want {
			t.Fatalf("telegram call %d method = %q, want %q", index, calls[index].method, want)
		}
	}
	if calls[1].messageID != calls[0].messageID || calls[2].messageID != calls[0].messageID {
		t.Fatalf("uptime lifecycle did not reuse message: %#v", calls[:3])
	}
	if calls[4].messageID != calls[3].messageID || calls[5].messageID != calls[3].messageID {
		t.Fatalf("server lifecycle did not reuse message: %#v", calls[3:6])
	}
	if calls[6].messageID == calls[3].messageID {
		t.Fatalf("new server incident reused resolved message: %#v", calls[3:])
	}

	history, err := service.LoadHistory(ctx, "sent", 100)
	if err != nil {
		t.Fatalf("load lifecycle history: %v", err)
	}
	foundRefresh := false
	foundServerResolve := false
	for _, item := range history {
		data := parseObject(item.Data)
		switch stringValue(data["lifecycleMutation"]) {
		case "refresh":
			foundRefresh = true
			changes := objectValue(data["lifecycleChanges"])
			if changes["error"] == nil {
				t.Fatalf("refresh history did not record changed error: %#v", data)
			}
		case "resolve":
			if stringValue(data["serverId"]) == "srv-1" {
				foundServerResolve = stringValue(data["downDuration"]) != ""
			}
		}
	}
	if !foundRefresh || !foundServerResolve {
		t.Fatalf("lifecycle history missing refresh=%v serverResolve=%v: %#v", foundRefresh, foundServerResolve, history)
	}
}

func TestNotificationMessageLifecyclePairs(t *testing.T) {
	tests := []struct {
		module, event, resourceKey, kind, phase string
		data                                    map[string]interface{}
	}{
		{"uptime", "down", "7", "availability", "open", map[string]interface{}{"monitorId": 7}},
		{"uptime", "up", "7", "availability", "resolve", map[string]interface{}{"monitorId": 7}},
		{"server", "cpu_high", "srv-1", "cpu", "open", map[string]interface{}{"serverId": "srv-1"}},
		{"server", "cpu_normal", "srv-1", "cpu", "resolve", map[string]interface{}{"serverId": "srv-1"}},
		{"server", "memory_high", "srv-1", "memory", "open", map[string]interface{}{"serverId": "srv-1"}},
		{"server", "disk_normal", "srv-1", "disk", "resolve", map[string]interface{}{"serverId": "srv-1"}},
		{"server", "traffic_high", "srv-1", "traffic", "open", map[string]interface{}{"serverId": "srv-1"}},
		{"system", "memory_normal", "local-host", "memory", "resolve", map[string]interface{}{}},
		{"github", "action_failed", "42", "actions", "open", map[string]interface{}{"repositoryId": 42}},
		{"github", "action_recovered", "42", "actions", "resolve", map[string]interface{}{"repositoryId": 42}},
	}
	for _, test := range tests {
		t.Run(test.module+"/"+test.event, func(t *testing.T) {
			lifecycle, ok := notificationMessageLifecycle(test.module, test.event, test.data)
			if !ok || lifecycle.ResourceKey != test.resourceKey || lifecycle.Kind != test.kind || lifecycle.Phase != test.phase {
				t.Fatalf("lifecycle = %#v ok=%v", lifecycle, ok)
			}
		})
	}
	if _, ok := notificationMessageLifecycle("github", "release_published", map[string]interface{}{"repositoryId": 42}); ok {
		t.Fatal("one-shot release event should not be dynamic")
	}
	if got := formatNotificationDuration(25*time.Hour + 2*time.Minute + 3*time.Second); got != "1 天 1 小时 2 分钟 3 秒" {
		t.Fatalf("formatted duration = %q", got)
	}
}

func TestTelegramProxyConfigAndTransportErrorsHideToken(t *testing.T) {
	service := testNotificationService(t)
	client, err := service.telegramHTTPClient(map[string]interface{}{"proxy_url": "http://127.0.0.1:7890"})
	if err != nil {
		t.Fatalf("configure telegram proxy: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("telegram proxy transport was not configured: %#v", client.Transport)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.telegram.org", nil)
	proxyURL, err := transport.Proxy(request)
	if err != nil || proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("telegram proxy = %v, err = %v", proxyURL, err)
	}
	if _, err := service.telegramHTTPClient(map[string]interface{}{"proxy_url": "file:///tmp/proxy"}); err == nil {
		t.Fatal("unsupported telegram proxy scheme should fail")
	}

	const token = "123456:secret-token"
	service.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial timeout")
	})}
	_, err = service.sendTelegram(context.Background(), map[string]interface{}{
		"bot_token": token,
		"chat_id":   "10001",
	}, "Test", "Message")
	if err == nil {
		t.Fatal("telegram transport failure should be returned")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("telegram transport error leaked bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "telegram API request failed") {
		t.Fatalf("unexpected telegram transport error: %v", err)
	}

	formatted := telegramMessageText("<测试>", "状态: 离线\n地址: https://example.com?a=1&b=2\n错误: <timeout>")
	for _, want := range []string{
		"*<测试\\>*", "*状态:* 🔴 离线", "`https://example.com?a=1&b=2`",
		"<timeout\\>", "_API Monitor_",
	} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted Telegram message missing %q: %s", want, formatted)
		}
	}
}

func TestRichNotificationTemplatesCoverDefaultEventsAndEmail(t *testing.T) {
	if got := formatNotificationPercent("86.35%"); got != "86.35%" {
		t.Fatalf("existing percentage was duplicated: %#v", got)
	}
	if got := formatNotificationPercent("86.35"); got != "86.35%" {
		t.Fatalf("missing percentage was not normalized: %#v", got)
	}

	uptimeRule := Rule{Name: "Uptime 故障", EventType: "down", Severity: "critical"}
	uptimeMessage := formatMessage(uptimeRule, map[string]interface{}{
		"monitorName": "API Gateway", "url": "https://example.com?a=1&b=2", "error": "timeout",
	}, time.UTC)
	for _, want := range []string{"状态: 故障", "事件: 服务不可用", "级别: 严重", "监控项: API Gateway", "错误原因: timeout"} {
		if !strings.Contains(uptimeMessage, want) {
			t.Fatalf("default uptime template missing %q: %s", want, uptimeMessage)
		}
	}

	githubRule := Rule{Name: "GitHub 事件", EventType: "release_published", Severity: "info"}
	githubMessage := formatMessage(githubRule, map[string]interface{}{
		"repositoryFullName": "iwvw/API-Monitor", "current": "v2.0.0", "previous": "v1.9.0",
		"htmlUrl": "https://github.com/iwvw/API-Monitor/releases/tag/v2.0.0",
	}, time.UTC)
	for _, want := range []string{"事件: GitHub 新版本发布", "仓库: iwvw/API-Monitor", "当前值: v2.0.0", "之前值: v1.9.0"} {
		if !strings.Contains(githubMessage, want) {
			t.Fatalf("default GitHub template missing %q: %s", want, githubMessage)
		}
	}
	trafficMessage := formatMessage(Rule{Name: "流量", EventType: "traffic_high", Severity: "warning"}, map[string]interface{}{
		"serverName": "edge-01", "traffic_percent": "86.35%", "threshold": "80%",
	}, time.UTC)
	if strings.Contains(trafficMessage, "%%") || !strings.Contains(trafficMessage, "流量使用率: 86.35%") || !strings.Contains(trafficMessage, "报警阈值: 80%") {
		t.Fatalf("traffic percentage template is invalid: %s", trafficMessage)
	}

	emailHTML := emailMessageHTML("<故障通知>", uptimeMessage)
	for _, want := range []string{"<!doctype html>", "API Monitor", "&lt;故障通知&gt;", "🔴 故障", "https://example.com?a=1&amp;b=2"} {
		if !strings.Contains(emailHTML, want) {
			t.Fatalf("rich email missing %q: %s", want, emailHTML)
		}
	}
	if strings.Contains(emailHTML, `\"`) {
		t.Fatalf("rich email contains escaped attribute quotes: %s", emailHTML)
	}
}

func TestRuleLifecycleDryRunAndRetiredModules(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	channel, err := service.CreateChannel(ctx, map[string]interface{}{
		"name":    "Ops Telegram",
		"type":    "telegram",
		"enabled": true,
		"config": map[string]interface{}{
			"bot_token": "123456:test-token",
			"chat_id":   "10001",
		},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	for _, module := range []string{"music", "openlist"} {
		_, err := service.CreateRule(ctx, map[string]interface{}{
			"name":          "Retired " + module,
			"source_module": module,
			"event_type":    "down",
			"channels":      []string{channel.ID},
		})
		if !errors.Is(err, errInvalidInput) {
			t.Fatalf("expected retired module validation for %s, got %v", module, err)
		}
	}

	rule, err := service.CreateRule(ctx, map[string]interface{}{
		"name":          "Uptime Down",
		"source_module": "uptime",
		"event_type":    "down",
		"severity":      "critical",
		"channels":      []string{channel.ID},
		"conditions": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"field": "monitorName", "operator": "equals", "value": "API Gateway"},
			},
		},
		"suppression":      map[string]interface{}{"repeat_count": 2, "silence_minutes": 30},
		"title_template":   "[{{severity}}] {{monitorName}}",
		"message_template": "{{monitorName}} failed: {{error}}",
		"backup_channels":  []string{channel.ID},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if rule.ID == "" || rule.Channels[0] != channel.ID || rule.BackupChannels[0] != channel.ID {
		t.Fatalf("unexpected rule: %#v", rule)
	}

	dryRun, err := service.DryRun(ctx, rule, map[string]interface{}{
		"severity":    "critical",
		"monitorId":   "monitor-1",
		"monitorName": "API Gateway",
		"error":       "timeout",
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dryRun["wouldNotify"] != true || dryRun["title"] != "[critical] API Gateway" {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open db for maintenance fixture: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO maintenance_schedules (id, target_type, target_id, start_at, end_at, reason)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "maint-1", "monitor", "monitor-1", time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Add(time.Hour).Format(time.RFC3339), "planned maintenance")
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close db: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("insert maintenance fixture: %v", err)
	}
	dryRun, err = service.DryRun(ctx, rule, map[string]interface{}{
		"severity":    "critical",
		"monitorId":   "monitor-1",
		"monitorName": "API Gateway",
		"error":       "timeout",
	})
	if err != nil {
		t.Fatalf("dry run during maintenance: %v", err)
	}
	if dryRun["wouldNotify"] != false || dryRun["maintenance"] == nil {
		t.Fatalf("maintenance should suppress dry-run result: %#v", dryRun)
	}

	if err := service.SetRuleEnabled(ctx, rule.ID, false); err != nil {
		t.Fatalf("disable rule: %v", err)
	}
	disabled, ok, err := service.LoadRule(ctx, rule.ID)
	if err != nil || !ok {
		t.Fatalf("load disabled rule ok=%v err=%v", ok, err)
	}
	if disabled.Enabled != 0 {
		t.Fatalf("expected disabled rule, got %#v", disabled)
	}

	if err := service.UpdateRule(ctx, rule.ID, map[string]interface{}{
		"name":          "Uptime Down Updated",
		"source_module": "openlist",
	}); !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected retired module validation on update, got %v", err)
	}
	if err := service.SetRuleEnabled(ctx, rule.ID, true); err != nil {
		t.Fatalf("enable rule: %v", err)
	}
	if err := service.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if _, ok, err := service.LoadRule(ctx, rule.ID); err != nil || ok {
		t.Fatalf("rule should be deleted ok=%v err=%v", ok, err)
	}
}

func TestHistoryConfigEventCatalogAndPreview(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)

	cfg, err := service.LoadConfig(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MaxRetryTimes != 3 || cfg.GlobalRateLimitPerHr != 100 || len(cfg.DefaultChannels) != 0 {
		t.Fatalf("unexpected default config: %#v", cfg)
	}
	if err := service.UpdateConfig(ctx, map[string]interface{}{
		"max_retry_times":            5,
		"retry_interval_seconds":     15,
		"history_retention_days":     7,
		"enable_batch":               false,
		"batch_interval_seconds":     45,
		"default_channels":           []string{"channel-a"},
		"global_rate_limit_per_hour": 42,
		"enable_auto_escalation":     true,
		"base_url":                   "https://monitor.example.com",
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}
	cfg, err = service.LoadConfig(ctx)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.MaxRetryTimes != 5 || cfg.EnableBatch || cfg.DefaultChannels[0] != "channel-a" || !cfg.EnableAutoEscalation || cfg.BaseURL == "" {
		t.Fatalf("unexpected updated config: %#v", cfg)
	}

	id, err := service.createHistory(ctx, "rule-1", "channel-a", "failed", "Title", "Message", map[string]interface{}{"source": "test"}, ptr("boom"))
	if err != nil || id == 0 {
		t.Fatalf("create history id=%d err=%v", id, err)
	}
	history, err := service.LoadHistory(ctx, "failed", 10)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 1 || history[0].ErrorMessage == nil || *history[0].ErrorMessage != "boom" {
		t.Fatalf("unexpected history: %#v", history)
	}
	if err := service.ClearHistory(ctx); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	history, err = service.LoadHistory(ctx, "", 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("history should be empty: %#v err=%v", history, err)
	}

	for _, item := range eventCatalog() {
		module := item["module"]
		if module == "music" || module == "openlist" {
			t.Fatalf("retired module leaked into event catalog: %#v", item)
		}
	}
	serverEvents := map[string]bool{}
	for _, item := range eventCatalog() {
		if item["module"] != "server" {
			continue
		}
		for _, event := range item["events"].([]string) {
			serverEvents[event] = true
		}
	}
	if !serverEvents["interrupted"] || !serverEvents["degraded"] {
		t.Fatalf("server event catalog missing interrupted/degraded: %#v", serverEvents)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/notification/templates/preview", strings.NewReader(`{
		"title_template":"[{{severity}}] {{monitorName}}",
		"message_template":"{{monitorName}} failed: {{error}}",
		"data":{"severity":"critical","monitorName":"API Gateway","error":"timeout"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Title     string   `json:"title"`
			Message   string   `json:"message"`
			Variables []string `json:"variables"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.Title != "[critical] API Gateway" || !strings.Contains(payload.Data.Message, "timeout") {
		t.Fatalf("unexpected preview payload: %#v", payload)
	}
}

func TestGlobalConfigMigrationAddsNewColumnsBeforeDefaultInsert(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "data.db"))
	if err != nil {
		t.Fatalf("open sqlite fixture: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE notification_global_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			max_retry_times INTEGER DEFAULT 3,
			retry_interval_seconds INTEGER DEFAULT 60,
			history_retention_days INTEGER DEFAULT 30,
			enable_batch INTEGER DEFAULT 1,
			batch_interval_seconds INTEGER DEFAULT 30,
			default_channels TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE notification_message_state (
			channel_id TEXT NOT NULL,
			source_module TEXT NOT NULL,
			resource_key TEXT NOT NULL,
			lifecycle_kind TEXT NOT NULL,
			chat_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (channel_id, source_module, resource_key, lifecycle_kind)
		);
		INSERT INTO notification_global_config (
			id, max_retry_times, retry_interval_seconds,
			history_retention_days, enable_batch, batch_interval_seconds, default_channels
		) VALUES (1, 4, 45, 14, 1, 20, '[]');
	`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close sqlite fixture: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("create legacy notification config fixture: %v", err)
	}

	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: dataDir,
		DBName:  "data.db",
	})
	cfg, err := service.LoadConfig(ctx)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if cfg.MaxRetryTimes != 4 || cfg.GlobalRateLimitPerHr != 100 || cfg.EnableAutoEscalation {
		t.Fatalf("unexpected migrated config: %#v", cfg)
	}
	db, err = service.open(ctx)
	if err != nil {
		t.Fatalf("open migrated notification db: %v", err)
	}
	defer db.Close()
	if exists, err := hasColumn(ctx, db, "notification_message_state", "last_data"); err != nil || !exists {
		t.Fatalf("notification_message_state.last_data migration exists=%v err=%v", exists, err)
	}
}

// TestSendToChannelErrors 验证 SendToChannel 的错误分支（渠道不存在 / 停用 / 配置缺失），
// 不触发真实网络发送。
func TestSendToChannelErrors(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)

	if err := service.SendToChannel(ctx, "notif_missing", "标题", "内容"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("不存在渠道应报错，实际 err=%v", err)
	}

	created, err := service.CreateChannel(ctx, map[string]interface{}{
		"name":    "TG 停用",
		"type":    "telegram",
		"enabled": false,
		"config":  map[string]interface{}{"bot_token": "123:abc", "chat_id": "-100123"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := service.SendToChannel(ctx, created.ID, "标题", "内容"); err == nil || !strings.Contains(err.Error(), "停用") {
		t.Fatalf("停用渠道应报错，实际 err=%v", err)
	}

	// 启用但配置为空：sendTelegram 内部会报 config incomplete（不经网络）
	enabledChan, err := service.CreateChannel(ctx, map[string]interface{}{
		"name":    "TG 缺配置",
		"type":    "telegram",
		"enabled": true,
		"config":  map[string]interface{}{"bot_token": "", "chat_id": ""},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := service.SendToChannel(ctx, enabledChan.ID, "标题", "内容"); err == nil || !strings.Contains(err.Error(), "config incomplete") {
		t.Fatalf("配置缺失应报错，实际 err=%v", err)
	}
}

// TestSendRichToChannelRichMessage 验证富消息路径：
// telegram 渠道调用 sendRichMessage（保留 Markdown 结构），失败时降级 sendMessage 纯文本。
func TestSendRichToChannelRichMessage(t *testing.T) {
	ctx := context.Background()
	service := testNotificationService(t)
	var calls []string
	service.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload := map[string]interface{}{}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode telegram request: %v", err)
		}
		method := strings.TrimPrefix(req.URL.Path, "/bot123456:test-token/")
		calls = append(calls, method)
		body := `{"ok":true,"result":{"message_id":1,"chat":{"id":10001}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	channel, err := service.CreateChannel(ctx, map[string]interface{}{
		"name": "TG Rich", "type": "telegram", "enabled": true,
		"config": map[string]interface{}{"bot_token": "123456:test-token", "chat_id": "10001"},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	markdown := "### 简报\n\n| 指标 | 值 |\n| --- | --- |\n| CPU | 15% |\n\n**结论**：正常"
	if err := service.SendRichToChannel(ctx, channel.ID, "每日简报", markdown); err != nil {
		t.Fatalf("send rich: %v", err)
	}
	if len(calls) != 1 || calls[0] != "sendRichMessage" {
		t.Fatalf("expected sendRichMessage, got calls=%v", calls)
	}

	// 富消息失败 -> 降级 sendMessage
	service.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		payload := map[string]interface{}{}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		method := strings.TrimPrefix(req.URL.Path, "/bot123456:test-token/")
		calls = append(calls, method)
		if method == "sendRichMessage" {
			return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"method not found"}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":2,"chat":{"id":10001}}}`))}, nil
	})}
	if err := service.SendRichToChannel(ctx, channel.ID, "每日简报", markdown); err != nil {
		t.Fatalf("send rich fallback: %v", err)
	}
	if len(calls) < 3 || calls[len(calls)-1] != "sendMessage" {
		t.Fatalf("expected fallback sendMessage, got calls=%v", calls)
	}

	// 渠道不存在报错而非 panic
	if err := service.SendRichToChannel(ctx, "notif_missing", "标题", "内容"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing channel should error, got %v", err)
	}
}
