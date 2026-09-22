package assets

import (
	"context"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

type fakeNotifier struct {
	events []map[string]interface{}
}

func (f *fakeNotifier) Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error {
	f.events = append(f.events, map[string]interface{}{
		"module": sourceModule,
		"type":   eventType,
		"data":   eventData,
	})
	return nil
}

func seedSourceTables(t *testing.T, service *Service) {
	t.Helper()
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS server_accounts (
			id TEXT PRIMARY KEY, name TEXT, host TEXT, status TEXT,
			expires_at DATETIME, tags TEXT, description TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_subscriptions (
			id TEXT PRIMARY KEY, name TEXT, expire_at TEXT, remark TEXT, enabled INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_monitors (
			id INTEGER PRIMARY KEY, name TEXT, url TEXT, active INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS uptime_monitor_states (
			monitor_id INTEGER PRIMARY KEY, ssl_expiry DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS openai_gateway_keys (
			id TEXT PRIMARY KEY, name TEXT, expires_at DATETIME, enabled INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS api_access_keys (
			id TEXT PRIMARY KEY, name TEXT, expires_at TEXT, enabled INTEGER, revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS aiagent_tokens (
			id TEXT PRIMARY KEY, device_label TEXT, token_prefix TEXT, expires_at TEXT, revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS managed_proxy_nodes (
			id TEXT PRIMARY KEY, server_id TEXT, name TEXT, enabled INTEGER, apply_status TEXT
		)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed table: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO server_accounts (id, name, host, status, expires_at, tags, description) VALUES
			('srv1', '主机一', '203.0.113.10', 'online', '2027-01-01T00:00:00Z', '["生产"]', '备注')`,
		`INSERT INTO subscription_subscriptions (id, name, expire_at, remark, enabled) VALUES
			('sub1', '订阅一', '2026-10-01', '订阅备注', 1),
			('sub2', '订阅停用', '2026-10-01', '', 0)`,
		`INSERT INTO uptime_monitors (id, name, url, active) VALUES (1, '站点一', 'https://example.com', 1)`,
		`INSERT INTO uptime_monitor_states (monitor_id, ssl_expiry) VALUES (1, '2026-11-01T00:00:00Z')`,
		`INSERT INTO openai_gateway_keys (id, name, expires_at, enabled) VALUES ('key1', '网关Key', '2026-12-01T00:00:00Z', 1)`,
		`INSERT INTO api_access_keys (id, name, expires_at, enabled, revoked_at) VALUES
			('ak1', '访问密钥', '2026-12-15T00:00:00Z', 1, ''),
			('ak2', '已吊销', '2026-12-15T00:00:00Z', 1, '2026-01-01T00:00:00Z')`,
		`INSERT INTO aiagent_tokens (id, device_label, token_prefix, expires_at, revoked_at) VALUES
			('tok1', '设备一', 'aka_abcd', '2026-12-20T00:00:00Z', '')`,
		`INSERT INTO managed_proxy_nodes (id, server_id, name, enabled, apply_status) VALUES
			('node1', 'srv1', '代理节点一', 1, 'applied')`,
	}
	for _, statement := range seed {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
}

func TestLoadCandidates(t *testing.T) {
	service := newTestService(t)
	seedSourceTables(t, service)

	groups, err := service.LoadCandidates(context.Background())
	if err != nil {
		t.Fatalf("load candidates: %v", err)
	}
	byModule := map[string]int{}
	for _, group := range groups {
		byModule[group.Module] = len(group.Items)
	}
	expectations := map[string]int{
		"server_accounts":            1,
		"subscription_subscriptions": 1,
		"uptime_monitor_states":      1,
		"openai_gateway_keys":        1,
		"api_access_keys":            1,
		"aiagent_tokens":             1,
		"managed_proxy_nodes":        1,
	}
	for module, want := range expectations {
		if byModule[module] != want {
			t.Errorf("%s candidates=%d want %d", module, byModule[module], want)
		}
	}
}

func TestLinkAssetsAndDedup(t *testing.T) {
	service := newTestService(t)
	seedSourceTables(t, service)
	ctx := context.Background()

	results, err := service.LinkAssets(ctx, []LinkInput{
		{SourceModule: "server_accounts", SourceRefID: "srv1"},
		{SourceModule: "subscription_subscriptions", SourceRefID: "sub1"},
	})
	if err != nil {
		t.Fatalf("link assets: %v", err)
	}
	if len(results) != 2 || results[0].Status != "linked" || results[1].Status != "linked" {
		t.Fatalf("unexpected link results: %#v", results)
	}
	if results[0].AssetID == "" {
		t.Fatal("expected asset id for linked server")
	}

	linked, ok, err := service.LoadAsset(ctx, results[0].AssetID)
	if err != nil || !ok {
		t.Fatalf("load linked asset: ok=%v err=%v", ok, err)
	}
	if linked.Origin != "linked" || linked.SourceModule != "server_accounts" {
		t.Fatalf("unexpected linked asset: %#v", linked)
	}
	if linked.ExpireAt != "2027-01-01T00:00:00Z" {
		t.Fatalf("expected normalized expire, got %s", linked.ExpireAt)
	}

	// 重复纳管应被跳过。
	again, err := service.LinkAssets(ctx, []LinkInput{
		{SourceModule: "server_accounts", SourceRefID: "srv1"},
	})
	if err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if again[0].Status != "skipped" || again[0].AssetID != results[0].AssetID {
		t.Fatalf("expected skipped with same asset id, got %#v", again[0])
	}

	// 候选应标记为已纳管。
	groups, _ := service.LoadCandidates(ctx)
	for _, group := range groups {
		if group.Module != "server_accounts" {
			continue
		}
		if !group.Items[0].Linked {
			t.Fatal("expected candidate marked as linked")
		}
	}
}

func TestLinkInvalidAndMissing(t *testing.T) {
	service := newTestService(t)
	seedSourceTables(t, service)
	ctx := context.Background()

	results, err := service.LinkAssets(ctx, []LinkInput{
		{SourceModule: "unknown_module", SourceRefID: "x"},
		{SourceModule: "server_accounts", SourceRefID: "nope"},
		{SourceModule: "", SourceRefID: ""},
	})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if results[0].Status != "invalid" || results[1].Status != "missing" || results[2].Status != "invalid" {
		t.Fatalf("unexpected statuses: %#v", results)
	}
}

func TestRefreshAssetAndOrphan(t *testing.T) {
	service := newTestService(t)
	seedSourceTables(t, service)
	ctx := context.Background()

	results, _ := service.LinkAssets(ctx, []LinkInput{{SourceModule: "server_accounts", SourceRefID: "srv1"}})
	assetID := results[0].AssetID

	db, _ := service.open(ctx)
	if _, err := db.ExecContext(ctx, "UPDATE server_accounts SET name = '主机改名', expires_at = '2028-06-01T00:00:00Z' WHERE id = 'srv1'"); err != nil {
		t.Fatalf("update source: %v", err)
	}
	db.Close()

	refreshed, err := service.RefreshAsset(ctx, assetID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Name != "主机改名" || refreshed.ExpireAt != "2028-06-01T00:00:00Z" {
		t.Fatalf("refresh did not pick up source change: %#v", refreshed)
	}

	// 来源消失后刷新应标记为 orphan 并保留记录。
	db, _ = service.open(ctx)
	db.ExecContext(ctx, "DELETE FROM server_accounts WHERE id = 'srv1'")
	db.Close()

	orphaned, err := service.RefreshAsset(ctx, assetID)
	if err != nil {
		t.Fatalf("refresh after source loss: %v", err)
	}
	if orphaned.Status != statusOrphan {
		t.Fatalf("expected orphan status, got %s", orphaned.Status)
	}
	events, _ := service.LoadEvents(ctx, assetID, 20)
	hasLost := false
	for _, event := range events {
		if event["event_type"] == "source_lost" {
			hasLost = true
		}
	}
	if !hasLost {
		t.Fatal("expected source_lost event")
	}
}

func TestRefreshNonLinkedRejected(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	asset, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "手工", "category": "physical", "asset_type": "server",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.RefreshAsset(ctx, asset.ID); err == nil {
		t.Fatal("expected refresh of manual asset to fail")
	}
}

func TestExpiryScanTriggersOncePerTier(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	notifier := &fakeNotifier{}
	service.SetNotifier(notifier)

	soon := nowUTC().AddDate(0, 0, 5).Format(time.RFC3339)
	if _, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "即将到期", "category": "virtual", "asset_type": "domain", "expire_at": soon,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "自动续费", "category": "virtual", "asset_type": "domain",
		"expire_at": soon, "auto_renew": true,
	}); err != nil {
		t.Fatalf("create auto-renew: %v", err)
	}

	service.RunExpiryScan(ctx)
	if len(notifier.events) != 1 {
		t.Fatalf("expected 1 alert (auto-renew skipped), got %d", len(notifier.events))
	}
	event := notifier.events[0]
	if event["module"] != "assets" || event["type"] != "asset_expiry" {
		t.Fatalf("unexpected event: %#v", event)
	}
	data := event["data"].(map[string]interface{})
	if data["assetName"] != "即将到期" {
		t.Fatalf("unexpected event asset: %#v", data)
	}

	// 同一档位再次扫描不应重复告警。
	service.RunExpiryScan(ctx)
	if len(notifier.events) != 1 {
		t.Fatalf("expected dedup, got %d events", len(notifier.events))
	}
}

func TestExpiryScanWithoutNotifier(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	if _, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "无通知", "category": "virtual", "asset_type": "domain",
		"expire_at": nowUTC().AddDate(0, 0, 3).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// 未注入 notifier 时不应 panic。
	service.RunExpiryScan(ctx)
}

func TestRefreshAllLinked(t *testing.T) {
	service := newTestService(t)
	seedSourceTables(t, service)
	ctx := context.Background()

	service.LinkAssets(ctx, []LinkInput{
		{SourceModule: "server_accounts", SourceRefID: "srv1"},
		{SourceModule: "subscription_subscriptions", SourceRefID: "sub1"},
	})
	success, failed, err := service.RefreshAllLinked(ctx)
	if err != nil {
		t.Fatalf("refresh all: %v", err)
	}
	if success != 2 || failed != 0 {
		t.Fatalf("refresh all success=%d failed=%d", success, failed)
	}
}

func TestSiteLocationUsesSettings(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	loc := service.siteLocation(ctx)
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
}

var _ = config.Config{}
