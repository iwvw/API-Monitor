package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
}
