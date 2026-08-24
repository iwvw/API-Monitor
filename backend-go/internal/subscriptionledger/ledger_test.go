package subscriptionledger

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
	_ "modernc.org/sqlite"
)

func ledgerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	statements := []string{
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE subscription_plans(id TEXT PRIMARY KEY,enabled INTEGER,total_bytes INTEGER,cycle_type TEXT,cycle_day INTEGER,selection_mode TEXT,include_internal_nodes INTEGER)`,
		`CREATE TABLE subscription_subscriptions(id TEXT PRIMARY KEY,plan_id TEXT,profile_id TEXT,enabled INTEGER,vless_uuid TEXT,hysteria2_password TEXT,created_at TEXT)`,
		`CREATE TABLE managed_proxy_nodes(id TEXT PRIMARY KEY,server_id TEXT,enabled INTEGER,apply_status TEXT,stats_port INTEGER)`,
		`CREATE TABLE subscription_plan_nodes(plan_id TEXT,node_id TEXT,source TEXT,PRIMARY KEY(plan_id,node_id,source))`,
		`CREATE TABLE subscription_profiles(id TEXT PRIMARY KEY,selection_mode TEXT,include_internal_nodes INTEGER,enabled INTEGER,total_bytes INTEGER,cycle_type TEXT,cycle_day INTEGER)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := reconcilequeue.EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRecordBatchIsAtomicIdempotentAndQueuesExhaustion(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_plans VALUES('plan',1,1000,'monthly',1,'explicit',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('sub','plan',1,'uuid','password','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('plan','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	report := Report{ServerID: "host", NodeID: "node", CredentialID: "sub", BootID: "boot", Sequence: 1, UploadBytes: 400, DownloadBytes: 600}
	accepted, duplicates, err := RecordBatch(ctx, db, []Report{report}, now)
	if err != nil || accepted != 1 || duplicates != 0 {
		t.Fatalf("accepted=%d duplicates=%d err=%v", accepted, duplicates, err)
	}
	accepted, duplicates, err = RecordBatch(ctx, db, []Report{report}, now)
	if err != nil || accepted != 0 || duplicates != 1 {
		t.Fatalf("replay accepted=%d duplicates=%d err=%v", accepted, duplicates, err)
	}
	usage, err := Current(ctx, db, "sub", "monthly", 1, "2026-01-01 00:00:00", now)
	if err != nil || usage.UploadBytes != 400 || usage.DownloadBytes != 600 || usage.Metering != "available" {
		t.Fatalf("usage=%#v err=%v", usage, err)
	}
	var queued string
	if err := db.QueryRow(`SELECT state FROM subscription_runtime_reconcile WHERE node_id='node'`).Scan(&queued); err != nil || queued != "pending" {
		t.Fatalf("queued=%q err=%v", queued, err)
	}
}

func TestActiveCredentialsExcludeExhaustedSubscriber(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_plans VALUES('plan',1,100,'monthly',1,'explicit',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('active','plan',1,'uuid-a','pass-a','2026-01-01 00:00:00'),('spent','plan',1,'uuid-b','pass-b','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('plan','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	start, end := CycleWindow(now, "monthly", 1, "")
	_, _ = db.Exec(`INSERT INTO subscription_usage_cycles(subscription_id,cycle_start,cycle_end,upload_bytes,download_bytes) VALUES('spent',?,?,60,40)`, start, end)
	items, err := ActiveCredentialsForNode(ctx, db, "node", "vless-reality", now)
	if err != nil || len(items) != 1 || items[0].SubscriptionID != "active" || items[0].VLESSUUID != "uuid-a" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestRecordBatchCanonicalizesCredentialAliasesAndClampsQuota(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_plans VALUES('plan',1,100,'monthly',1,'explicit',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('sub','plan',1,'uuid','password','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('plan','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	first := Report{ServerID: "host", NodeID: "node", CredentialID: "uuid", BootID: "boot", Sequence: 1, UploadBytes: 80, DownloadBytes: 80}
	result, err := RecordBatchDetailed(ctx, db, []Report{first}, now)
	if err != nil || result.Accepted != 1 || result.UploadBytes != 80 || result.DownloadBytes != 20 {
		t.Fatalf("first result=%#v err=%v", result, err)
	}
	alias := first
	alias.CredentialID = "sub"
	result, err = RecordBatchDetailed(ctx, db, []Report{alias}, now)
	if err != nil || result.Accepted != 0 || result.Duplicates != 1 {
		t.Fatalf("alias replay result=%#v err=%v", result, err)
	}
	usage, err := Current(ctx, db, "sub", "monthly", 1, "2026-01-01 00:00:00", now)
	if err != nil || usage.UploadBytes != 80 || usage.DownloadBytes != 20 {
		t.Fatalf("usage=%#v err=%v", usage, err)
	}
}

func TestRecordBatchAcknowledgesStaleSubscriberWithoutBlockingHost(t *testing.T) {
	db := ledgerTestDB(t)
	report := Report{ServerID: "host", NodeID: "removed-node", CredentialID: "removed-sub", BootID: "boot", Sequence: 1, UploadBytes: 1}
	result, err := RecordBatchDetailed(context.Background(), db, []Report{report}, time.Now().UTC())
	if err != nil || result.Ignored != 1 || result.Accepted != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRecordBatchRejectsCrossServerNodeReport(t *testing.T) {
	db := ledgerTestDB(t)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','other-host',1,'running',21000)`)
	report := Report{ServerID: "host", NodeID: "node", CredentialID: "sub", BootID: "boot", Sequence: 1, UploadBytes: 1}
	if _, err := RecordBatchDetailed(context.Background(), db, []Report{report}, time.Now().UTC()); err == nil {
		t.Fatal("expected cross-server report to be rejected")
	}
}

func TestPruneKeepsAggregatedUsageWhileDroppingOldRawRows(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_plans VALUES('plan',1,0,'monthly',1,'explicit',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('sub','plan',1,'uuid','password','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('plan','node','internal')`)

	oldNow := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	newNow := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	if _, err := RecordBatchDetailed(ctx, db, []Report{{
		ServerID: "host", NodeID: "node", CredentialID: "sub", BootID: "boot", Sequence: 1, UploadBytes: 11, DownloadBytes: 22,
	}}, oldNow); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordBatchDetailed(ctx, db, []Report{{
		ServerID: "host", NodeID: "node", CredentialID: "sub", BootID: "boot", Sequence: 2, UploadBytes: 33, DownloadBytes: 44,
	}}, newNow); err != nil {
		t.Fatal(err)
	}

	if err := pruneHistory(ctx, db, newNow, 24*time.Hour, 24*time.Hour, 24*time.Hour); err != nil {
		t.Fatal(err)
	}

	var reports, keys, hourly int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_usage_reports`).Scan(&reports); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_usage_report_keys`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_usage_hourly`).Scan(&hourly); err != nil {
		t.Fatal(err)
	}
	// 明细表是旧安装兼容位，新写入只进 hourly；回放键与小时聚合各保留最新一行。
	if reports != 0 || keys != 1 || hourly != 1 {
		t.Fatalf("reports=%d keys=%d", reports, keys)
	}

	usage, err := Current(ctx, db, "sub", "monthly", 1, "2026-01-01 00:00:00", newNow)
	if err != nil {
		t.Fatal(err)
	}
if usage.UploadBytes != 44 || usage.DownloadBytes != 66 {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestRecordBatchAcceptsEnabledProfileSubscription(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_profiles VALUES('profile','all',1,1,0,'none',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,profile_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('profsub','','profile',1,'uuid','password','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('profile','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	report := Report{ServerID: "host", NodeID: "node", CredentialID: "profsub", BootID: "boot", Sequence: 1, UploadBytes: 100, DownloadBytes: 200}
	accepted, duplicates, err := RecordBatch(ctx, db, []Report{report}, now)
	if err != nil || accepted != 1 || duplicates != 0 {
		t.Fatalf("enabled profile subscription must be accepted, accepted=%d duplicates=%d err=%v", accepted, duplicates, err)
	}

	// 开关关闭后同节点上报被忽略
	_, _ = db.Exec(`UPDATE subscription_profiles SET include_internal_nodes = 0 WHERE id = 'profile'`)
	report.Sequence = 2
	accepted, _, err = RecordBatch(ctx, db, []Report{report}, now)
	if err != nil || accepted != 0 {
		t.Fatalf("disabled profile subscription must be ignored, accepted=%d err=%v", accepted, err)
	}
}

func TestRecordBatchIgnoresDisabledProfileEntitlement(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	// profile 被禁用：即便节点在 explicit 清单内，凭据与记账都必须拒绝
	_, _ = db.Exec(`INSERT INTO subscription_profiles VALUES('profile','explicit',1,0,0,'none',1)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,profile_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('profsub','','profile',1,'uuid','password','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('profile','node','internal')`)
	report := Report{ServerID: "host", NodeID: "node", CredentialID: "profsub", BootID: "boot", Sequence: 1, UploadBytes: 10}
	result, err := RecordBatchDetailed(ctx, db, []Report{report}, time.Now().UTC())
	if err != nil || result.Ignored != 1 || result.Accepted != 0 {
		t.Fatalf("disabled profile must not be entitled, result=%#v err=%v", result, err)
	}
}

func TestActiveCredentialsIncludeEnabledProfileSubscriptions(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_profiles VALUES('profile','explicit',1,1,1000,'monthly',15)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,profile_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('profsub','','profile',1,'uuid-p','pass-p','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000),('node2','host',1,'running',21001)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('profile','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	items, err := ActiveCredentialsForNode(ctx, db, "node", "vless-reality", now)
	if err != nil || len(items) != 1 || items[0].SubscriptionID != "profsub" || items[0].VLESSUUID != "uuid-p" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	// 不在 explicit 清单内的内部节点不下发
	items, err = ActiveCredentialsForNode(ctx, db, "node2", "vless-reality", now)
	if err != nil || len(items) != 0 {
		t.Fatalf("non-granted node items=%#v err=%v", items, err)
	}

	// profile 禁用后凭据不再下发
	if _, err := db.Exec(`UPDATE subscription_profiles SET enabled=0 WHERE id='profile'`); err != nil {
		t.Fatal(err)
	}
	items, err = ActiveCredentialsForNode(ctx, db, "node", "vless-reality", now)
	if err != nil || len(items) != 0 {
		t.Fatalf("disabled profile items=%#v err=%v", items, err)
	}

	// 'all' 模式覆盖全部启用中的内部节点
	if _, err := db.Exec(`UPDATE subscription_profiles SET enabled=1, selection_mode='all' WHERE id='profile'`); err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []string{"node", "node2"} {
		items, err = ActiveCredentialsForNode(ctx, db, nodeID, "vless-reality", now)
		if err != nil || len(items) != 1 {
			t.Fatalf("all-mode node %s items=%#v err=%v", nodeID, items, err)
		}
	}
}

func TestProfileSubscriptionLedgerSharesCycleAndQuotaCaliber(t *testing.T) {
	db := ledgerTestDB(t)
	ctx := context.Background()
	_, _ = db.Exec(`INSERT INTO subscription_profiles VALUES('profile','explicit',1,1,100,'monthly',15)`)
	_, _ = db.Exec(`INSERT INTO subscription_subscriptions(id,plan_id,profile_id,enabled,vless_uuid,hysteria2_password,created_at) VALUES('profsub','','profile',1,'uuid-p','pass-p','2026-01-01 00:00:00')`)
	_, _ = db.Exec(`INSERT INTO managed_proxy_nodes VALUES('node','host',1,'running',21000)`)
	_, _ = db.Exec(`INSERT INTO subscription_plan_nodes VALUES('profile','node','internal')`)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	result, err := RecordBatchDetailed(ctx, db, []Report{{
		ServerID: "host", NodeID: "node", CredentialID: "uuid-p", BootID: "boot", Sequence: 1, UploadBytes: 60, DownloadBytes: 40,
	}}, now)
	if err != nil || result.Accepted != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	// 记账窗口必须使用 profile 的周期口径（monthly / day=15），而非默认 none
	wantStart, wantEnd := CycleWindow(now, "monthly", 15, "2026-01-01 00:00:00")
	var storedStart, storedEnd string
	var upload, download int64
	if err := db.QueryRow(`SELECT cycle_start,cycle_end,upload_bytes,download_bytes FROM subscription_usage_cycles WHERE subscription_id='profsub'`).Scan(&storedStart, &storedEnd, &upload, &download); err != nil {
		t.Fatal(err)
	}
	if storedStart != wantStart || storedEnd != wantEnd {
		t.Fatalf("cycle window %s..%s, want %s..%s", storedStart, storedEnd, wantStart, wantEnd)
	}
	// 面板用同一口径读取时必须命中同一行（cycle 键一致）
	usage, err := Current(ctx, db, "profsub", "monthly", 15, "2026-01-01 00:00:00", now)
	if err != nil || usage.UploadBytes != 60 || usage.DownloadBytes != 40 || usage.Metering != "available" {
		t.Fatalf("usage=%#v err=%v", usage, err)
	}

	// 配额同样来自 profile（total=100）：用尽后按订阅维度入队 reconcile
	result, err = RecordBatchDetailed(ctx, db, []Report{{
		ServerID: "host", NodeID: "node", CredentialID: "uuid-p", BootID: "boot", Sequence: 2, UploadBytes: 200,
	}}, now)
	if err != nil || result.Accepted != 0 || result.Ignored != 1 {
		t.Fatalf("exhausted result=%#v err=%v", result, err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM subscription_runtime_reconcile WHERE node_id='node'`).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("exhausted profile must queue reconcile, state=%q err=%v", state, err)
	}
}