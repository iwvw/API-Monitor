package subscriptionledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/reconcilequeue"
)

// Report is an Agent-produced delta for one subscriber credential on one node.
// BootID and Sequence form the replay key; byte values must never be cumulative.
type Report struct {
	ServerID      string
	NodeID        string
	CredentialID  string
	BootID        string
	Sequence      int64
	UploadBytes   int64
	DownloadBytes int64
}

type Usage struct {
	UploadBytes   int64
	DownloadBytes int64
	CycleStart    string
	CycleEnd      string
	Metering      string
}

type Credential struct {
	SubscriptionID    string
	Protocol          string
	VLESSUUID         string
	Hysteria2Password string
}

type BatchResult struct {
	Accepted      int
	Duplicates    int
	Ignored       int
	UploadBytes   int64
	DownloadBytes int64
}

const (
	// 原始流量增量明细（subscription_usage_reports）仅在旧安装的兼容期内保留，
	// 新写入一律聚合到小时级表；两个窗口都只支撑仪表盘"最近 7 天"的逐日流量趋势。
	reportRetentionWindow    = 7 * 24 * time.Hour
	replayKeyRetentionWindow = 7 * 24 * time.Hour
	// 小时级聚合行远少于逐条明细，可保留更久以便回溯更长趋势，且不会显著占空间。
	hourlyRetentionWindow = 30 * 24 * time.Hour
)

func EnsureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS subscription_usage_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id TEXT NOT NULL,node_id TEXT NOT NULL,subscription_id TEXT NOT NULL,
			credential_id TEXT NOT NULL,boot_id TEXT NOT NULL,sequence INTEGER NOT NULL,
			upload_bytes INTEGER NOT NULL DEFAULT 0,download_bytes INTEGER NOT NULL DEFAULT 0,
			reported_at TEXT DEFAULT (datetime('now')),
			UNIQUE(server_id,node_id,credential_id,boot_id,sequence),
			FOREIGN KEY(subscription_id) REFERENCES subscription_subscriptions(id) ON DELETE CASCADE
		)`,
		// Keep idempotency independent from the credential spelling used by an
		// Agent. A subscription may be reported by ID, VLESS UUID, or HY2
		// password; those aliases must never create separate billable events.
		`CREATE TABLE IF NOT EXISTS subscription_usage_report_keys (
			server_id TEXT NOT NULL,node_id TEXT NOT NULL,subscription_id TEXT NOT NULL,
			boot_id TEXT NOT NULL,sequence INTEGER NOT NULL,
			reported_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY(server_id,node_id,subscription_id,boot_id,sequence),
			FOREIGN KEY(subscription_id) REFERENCES subscription_subscriptions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_usage_cycles (
			subscription_id TEXT NOT NULL,cycle_start TEXT NOT NULL,cycle_end TEXT NOT NULL DEFAULT '',
			upload_bytes INTEGER NOT NULL DEFAULT 0,download_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT DEFAULT (datetime('now')),PRIMARY KEY(subscription_id,cycle_start),
			FOREIGN KEY(subscription_id) REFERENCES subscription_subscriptions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS subscription_cycle_state (
			subscription_id TEXT PRIMARY KEY,cycle_start TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now')),
			FOREIGN KEY(subscription_id) REFERENCES subscription_subscriptions(id) ON DELETE CASCADE
		)`,
		// 小时级聚合：Agent 上报的原始增量经幂等判重后，按 (server,node,sub,hour)
		// 折叠为累计行，行数降至逐条写入的 1/12 以下（15min 上报间隔）。
		`CREATE TABLE IF NOT EXISTS subscription_usage_hourly (
			server_id TEXT NOT NULL,node_id TEXT NOT NULL,subscription_id TEXT NOT NULL,
			hour TEXT NOT NULL,
			upload_bytes INTEGER NOT NULL DEFAULT 0,download_bytes INTEGER NOT NULL DEFAULT 0,
			reported_at TEXT DEFAULT (datetime('now')),
			UNIQUE(server_id,node_id,subscription_id,hour),
			FOREIGN KEY(subscription_id) REFERENCES subscription_subscriptions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_usage_hourly_sub_time ON subscription_usage_hourly(subscription_id,hour)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_usage_hourly_hour ON subscription_usage_hourly(hour)`,
		`CREATE INDEX IF NOT EXISTS idx_subscription_usage_reports_subscription_time ON subscription_usage_reports(subscription_id,reported_at)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure subscription ledger: %w", err)
		}
	}
	// Backfill the canonical replay key for installations that already have
	// ledger rows from the first implementation. The Agent service can start
	// before the subscription module in isolated tests and during a staged
	// upgrade, so defer this step until the parent table exists.
	var subscriptionsTable int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='subscription_subscriptions'`).Scan(&subscriptionsTable); err != nil {
		return fmt.Errorf("inspect subscription ledger parent table: %w", err)
	}
	if subscriptionsTable > 0 {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_usage_report_keys
			(server_id,node_id,subscription_id,boot_id,sequence,reported_at)
			SELECT server_id,node_id,subscription_id,boot_id,sequence,reported_at
			FROM subscription_usage_reports`); err != nil {
			return fmt.Errorf("backfill subscription ledger replay keys: %w", err)
		}
		// 从旧明细表折叠回填小时级聚合，确保仪表盘 7 天流量趋势在升级后不出现"归零"断层。
		// 判断条件：hourly 表为空 + reports 有数据 = 首次部署新代码，据此避免重复累加。
		var legacyCount, hourlyCount int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_usage_reports`).Scan(&legacyCount); err != nil {
			return fmt.Errorf("count legacy reports: %w", err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_usage_hourly`).Scan(&hourlyCount); err != nil {
			return fmt.Errorf("count hourly aggregates: %w", err)
		}
		if legacyCount > 0 && hourlyCount == 0 {
			if _, err := db.ExecContext(ctx, `INSERT INTO subscription_usage_hourly
				(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes,reported_at)
				SELECT server_id,node_id,subscription_id,
					strftime('%Y-%m-%dT%H:00:00Z', reported_at),
					SUM(upload_bytes),SUM(download_bytes),MAX(reported_at)
				FROM subscription_usage_reports
				GROUP BY server_id,node_id,subscription_id,strftime('%Y-%m-%dT%H:00:00Z', reported_at)`); err != nil {
				return fmt.Errorf("backfill hourly aggregates: %w", err)
			}
		}
	}
	return nil
}

// Prune removes short-lived raw traffic history after it has already been
// folded into subscription_usage_cycles. Aggregated cycle usage remains intact.
func Prune(ctx context.Context, db *sql.DB, now time.Time) error {
	return pruneHistory(ctx, db, now, reportRetentionWindow, replayKeyRetentionWindow, hourlyRetentionWindow)
}

func pruneHistory(ctx context.Context, db *sql.DB, now time.Time, reportRetention, replayKeyRetention, hourlyRetention time.Duration) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	cutoffs := []struct {
		query  string
		cutoff time.Time
		label  string
	}{
		{
			query:  `DELETE FROM subscription_usage_reports WHERE datetime(reported_at) < datetime(?)`,
			cutoff: now.Add(-reportRetention),
			label:  "raw usage reports",
		},
		{
			query:  `DELETE FROM subscription_usage_report_keys WHERE datetime(reported_at) < datetime(?)`,
			cutoff: now.Add(-replayKeyRetention),
			label:  "usage replay keys",
		},
		{
			query:  `DELETE FROM subscription_usage_hourly WHERE datetime(hour) < datetime(?)`,
			cutoff: now.Add(-hourlyRetention),
			label:  "hourly usage aggregates",
		},
	}
	for _, item := range cutoffs {
		if _, err := db.ExecContext(ctx, item.query, item.cutoff.Format(time.RFC3339)); err != nil {
			return fmt.Errorf("prune %s: %w", item.label, err)
		}
	}
	return nil
}

// ScheduleCycleTransitions enqueues runtime synchronization when a plan enters
// a new quota cycle so previously exhausted subscribers become usable again.
// Profile-based subscriptions are also included.
func ScheduleCycleTransitions(ctx context.Context, db *sql.DB, now time.Time) error {
	rows, err := db.QueryContext(ctx, `SELECT s.id,COALESCE(s.plan_id,''),COALESCE(s.profile_id,''),
		COALESCE(p.cycle_type,pf.cycle_type,'none'),COALESCE(p.cycle_day,pf.cycle_day,1),COALESCE(s.created_at,datetime('now'))
		FROM subscription_subscriptions s
		LEFT JOIN subscription_plans p ON p.id=s.plan_id
		LEFT JOIN subscription_profiles pf ON pf.id=s.profile_id
		WHERE (s.plan_id IS NOT NULL AND s.plan_id != '') OR (s.profile_id IS NOT NULL AND s.profile_id != '')`)
	if err != nil {
		return err
	}
	type item struct {
		subscriptionID, planID, cycleType, createdAt string
		cycleDay                                     int
	}
	items := []item{}
	for rows.Next() {
		var value item
		var profileID string
		if err := rows.Scan(&value.subscriptionID, &value.planID, &profileID, &value.cycleType, &value.cycleDay, &value.createdAt); err != nil {
			rows.Close()
			return err
		}
		items = append(items, value)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, value := range items {
		cycleStart, _ := CycleWindow(now, value.cycleType, value.cycleDay, value.createdAt)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO subscription_cycle_state(subscription_id,cycle_start,updated_at)
			VALUES(?,?,datetime('now')) ON CONFLICT(subscription_id) DO UPDATE SET cycle_start=excluded.cycle_start,updated_at=datetime('now')
			WHERE subscription_cycle_state.cycle_start<>excluded.cycle_start`, value.subscriptionID, cycleStart)
		if err != nil {
			tx.Rollback()
			return err
		}
		changed, _ := result.RowsAffected()
		if changed > 0 {
			// Resolve node IDs from either plan or profile
			nodeIDs, err := reconcilequeue.NodeIDsForSubscription(ctx, tx, value.subscriptionID)
			if err != nil {
				tx.Rollback()
				return err
			}
			if err := reconcilequeue.EnqueueNodes(ctx, tx, nodeIDs, "subscription quota cycle changed"); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (report Report) Validate() error {
	if strings.TrimSpace(report.ServerID) == "" || strings.TrimSpace(report.NodeID) == "" || strings.TrimSpace(report.CredentialID) == "" || strings.TrimSpace(report.BootID) == "" {
		return errors.New("traffic report identity is incomplete")
	}
	if report.Sequence < 0 || report.UploadBytes < 0 || report.DownloadBytes < 0 {
		return errors.New("traffic report counters must be non-negative")
	}
	return nil
}

// Record applies a delta exactly once and updates the subscriber's current
// cycle in the same transaction. A replay returns accepted=false without
// changing the ledger.
func Record(ctx context.Context, db *sql.DB, report Report, now time.Time) (accepted bool, err error) {
	acceptedCount, _, err := RecordBatch(ctx, db, []Report{report}, now)
	return acceptedCount == 1, err
}

// RecordBatch commits all valid reports atomically. Replayed entries are
// ignored individually while new entries in the same batch are still applied.
func RecordBatch(ctx context.Context, db *sql.DB, reports []Report, now time.Time) (accepted int, duplicates int, err error) {
	result, err := RecordBatchDetailed(ctx, db, reports, now)
	return result.Accepted, result.Duplicates, err
}

// RecordBatchDetailed also reports stale rows that were acknowledged without
// billing. Agent traffic collection is store-and-forward: a subscription or
// node may be removed while a batch is pending, and retrying that batch forever
// would block all later accounting from the host.
func RecordBatchDetailed(ctx context.Context, db *sql.DB, reports []Report, now time.Time) (result BatchResult, err error) {
	if len(reports) == 0 {
		return result, errors.New("traffic report batch is empty")
	}
	for _, report := range reports {
		if err := report.Validate(); err != nil {
			return result, err
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	hour := now.Truncate(time.Hour).Format(time.RFC3339)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin traffic ledger transaction: %w", err)
	}
	defer tx.Rollback()

	for _, report := range reports {
		var nodeServerID string
		if err := tx.QueryRowContext(ctx, `SELECT server_id FROM managed_proxy_nodes WHERE id=?`, report.NodeID).Scan(&nodeServerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				result.Ignored++
				continue
			}
			return result, fmt.Errorf("load managed node: %w", err)
		}
		if nodeServerID != report.ServerID {
			return result, errors.New("managed node does not belong to reporting server")
		}

		var subscriptionID, planID, cycleType, createdAt string
		var totalBytes int64
		var cycleDay int
		err = tx.QueryRowContext(ctx, `SELECT s.id,COALESCE(s.plan_id,''),COALESCE(p.total_bytes,0),COALESCE(p.cycle_type,'none'),COALESCE(p.cycle_day,1),COALESCE(s.created_at,datetime('now'))
		FROM subscription_subscriptions s
		LEFT JOIN subscription_plans p ON p.id=s.plan_id
		WHERE s.id=? OR s.vless_uuid=? OR s.hysteria2_password=?
		LIMIT 1`, report.CredentialID, report.CredentialID, report.CredentialID).Scan(&subscriptionID, &planID, &totalBytes, &cycleType, &cycleDay, &createdAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				result.Ignored++
				continue
			}
			return result, fmt.Errorf("resolve subscriber credential: %w", err)
		}
		// 节点授权判定：plan 型与 profile 型订阅都需要被识别（profile 的
		// include_internal_nodes/selection_mode 列由 subscription 模块
		// ensureSchema 补齐，默认关闭，开启后 profile 订阅的 internal
		// 节点流量才会记账）。
		var entitled int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1
			FROM subscription_subscriptions s
			JOIN managed_proxy_nodes n ON n.enabled=1 AND n.apply_status='running'
			WHERE s.id=? AND s.enabled=1 AND n.id=? AND n.server_id=?
			AND (
				(s.plan_id != '' AND EXISTS(
					SELECT 1 FROM subscription_plans p
					WHERE p.id=s.plan_id AND p.enabled=1 AND p.include_internal_nodes=1
					AND (COALESCE(p.selection_mode,'explicit')='all' OR EXISTS(
						SELECT 1 FROM subscription_plan_nodes pn
						WHERE pn.plan_id=p.id AND pn.node_id=n.id AND pn.source='internal'
					))
				))
				OR
				(s.plan_id = '' AND s.profile_id != '' AND EXISTS(
					SELECT 1 FROM subscription_profiles pf
					WHERE pf.id=s.profile_id AND pf.include_internal_nodes=1
					AND (COALESCE(pf.selection_mode,'explicit')='all' OR EXISTS(
						SELECT 1 FROM subscription_plan_nodes pn
						WHERE pn.plan_id=pf.id AND pn.node_id=n.id AND pn.source='internal'
					))
				))
			)
		)`, subscriptionID, report.NodeID, report.ServerID).Scan(&entitled); err != nil {
			return result, fmt.Errorf("validate subscriber node entitlement: %w", err)
		}
		if entitled == 0 {
			result.Ignored++
			continue
		}

		cycleStart, cycleEnd := CycleWindow(now, cycleType, cycleDay, createdAt)
		keyResult, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_usage_report_keys
			(server_id,node_id,subscription_id,boot_id,sequence,reported_at)
			VALUES(?,?,?,?,?,?)`, report.ServerID, report.NodeID, subscriptionID, report.BootID, report.Sequence, now.Format(time.RFC3339))
		if err != nil {
			return result, fmt.Errorf("insert traffic report replay key: %w", err)
		}
		rows, err := keyResult.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("inspect traffic report replay key: %w", err)
		}
		if rows == 0 {
			result.Duplicates++
			continue
		}
		// Clamp a delta at the plan quota. The Agent can be one collection
		// interval behind the reconciliation update; counting the entire stale
		// delta would allow an exhausted subscription to exceed its allowance.
		if totalBytes > 0 {
			var used int64
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(upload_bytes+download_bytes,0) FROM subscription_usage_cycles WHERE subscription_id=? AND cycle_start=?`, subscriptionID, cycleStart).Scan(&used); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return result, fmt.Errorf("load subscriber quota usage: %w", err)
			}
			remaining := totalBytes - used
			if remaining <= 0 {
				nodeIDs, queueErr := reconcilequeue.NodeIDsForPlan(ctx, tx, planID)
				if queueErr != nil {
					return result, fmt.Errorf("resolve exhausted subscription nodes: %w", queueErr)
				}
				if queueErr := reconcilequeue.EnqueueNodes(ctx, tx, nodeIDs, "subscription quota exhausted"); queueErr != nil {
					return result, queueErr
				}
				result.Ignored++
				continue
			}
			if report.UploadBytes > remaining {
				report.UploadBytes = remaining
				report.DownloadBytes = 0
			} else if report.UploadBytes+report.DownloadBytes > remaining {
				report.DownloadBytes = remaining - report.UploadBytes
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_usage_hourly
		(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes,reported_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(server_id,node_id,subscription_id,hour) DO UPDATE SET
		upload_bytes=subscription_usage_hourly.upload_bytes+excluded.upload_bytes,
		download_bytes=subscription_usage_hourly.download_bytes+excluded.download_bytes,
		reported_at=excluded.reported_at`, report.ServerID, report.NodeID, subscriptionID, hour, report.UploadBytes, report.DownloadBytes, now.Format(time.RFC3339)); err != nil {
			return result, fmt.Errorf("upsert hourly usage aggregate: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_usage_cycles
		(subscription_id,cycle_start,cycle_end,upload_bytes,download_bytes,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(subscription_id,cycle_start) DO UPDATE SET
		cycle_end=excluded.cycle_end,
		upload_bytes=subscription_usage_cycles.upload_bytes+excluded.upload_bytes,
		download_bytes=subscription_usage_cycles.download_bytes+excluded.download_bytes,
			updated_at=excluded.updated_at`, subscriptionID, cycleStart, cycleEnd, report.UploadBytes, report.DownloadBytes, now.Format(time.RFC3339)); err != nil {
			return result, fmt.Errorf("update traffic usage cycle: %w", err)
		}
		if totalBytes > 0 {
			var used int64
			if err := tx.QueryRowContext(ctx, `SELECT upload_bytes+download_bytes FROM subscription_usage_cycles WHERE subscription_id=? AND cycle_start=?`, subscriptionID, cycleStart).Scan(&used); err != nil {
				return result, fmt.Errorf("check subscriber quota: %w", err)
			}
			if used >= totalBytes {
				nodeIDs, err := reconcilequeue.NodeIDsForPlan(ctx, tx, planID)
				if err != nil {
					return result, fmt.Errorf("resolve quota reconciliation nodes: %w", err)
				}
				if err := reconcilequeue.EnqueueNodes(ctx, tx, nodeIDs, "subscription quota exhausted"); err != nil {
					return result, err
				}
			}
		}
		result.Accepted++
		result.UploadBytes += report.UploadBytes
		result.DownloadBytes += report.DownloadBytes
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit traffic ledger transaction: %w", err)
	}
	return result, nil
}

func Current(ctx context.Context, db *sql.DB, subscriptionID, cycleType string, cycleDay int, createdAt string, now time.Time) (Usage, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	start, end := CycleWindow(now.UTC(), cycleType, cycleDay, createdAt)
	usage := Usage{CycleStart: start, CycleEnd: end, Metering: "pending"}
	err := db.QueryRowContext(ctx, `SELECT upload_bytes,download_bytes FROM subscription_usage_cycles WHERE subscription_id=? AND cycle_start=?`, subscriptionID, start).Scan(&usage.UploadBytes, &usage.DownloadBytes)
	if err == nil {
		usage.Metering = "available"
		return usage, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Usage{}, fmt.Errorf("load traffic usage cycle: %w", err)
	}
	var internal, ready int
	err = db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM subscription_subscriptions s
		JOIN subscription_plans p ON p.id=s.plan_id
		WHERE s.id=? AND p.include_internal_nodes=1 AND (
			COALESCE(p.selection_mode,'explicit')='all' OR EXISTS(
				SELECT 1 FROM subscription_plan_nodes pn WHERE pn.plan_id=p.id AND pn.source='internal'
			)
		)
	), EXISTS(
		SELECT 1 FROM subscription_subscriptions s
		JOIN subscription_plans p ON p.id=s.plan_id
		JOIN managed_proxy_nodes n ON n.enabled=1 AND n.apply_status='running' AND n.stats_port>0
		WHERE s.id=? AND p.include_internal_nodes=1 AND (
			COALESCE(p.selection_mode,'explicit')='all' OR EXISTS(
				SELECT 1 FROM subscription_plan_nodes pn WHERE pn.plan_id=p.id AND pn.node_id=n.id AND pn.source='internal'
			)
		)
	)`, subscriptionID, subscriptionID).Scan(&internal, &ready)
	if err != nil {
		return Usage{}, fmt.Errorf("inspect subscription metering capability: %w", err)
	}
	if internal == 0 {
		usage.Metering = "unavailable"
	} else if ready == 1 {
		usage.Metering = "available"
	}
	return usage, nil
}

// ActiveCredentialsForNode returns the subscriber identities that may use a
// managed node now. Disabled plans, disabled subscriptions and exhausted
// subscribers are excluded.
func ActiveCredentialsForNode(ctx context.Context, db *sql.DB, nodeID, protocol string, now time.Time) ([]Credential, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := db.QueryContext(ctx, `SELECT s.id,s.vless_uuid,s.hysteria2_password,
		COALESCE(p.total_bytes,0),COALESCE(p.cycle_type,'none'),COALESCE(p.cycle_day,1),COALESCE(s.created_at,datetime('now'))
		FROM subscription_subscriptions s
		JOIN subscription_plans p ON p.id=s.plan_id
		WHERE s.enabled=1 AND p.enabled=1 AND p.include_internal_nodes=1 AND (
			COALESCE(p.selection_mode,'explicit')='all' OR EXISTS(
				SELECT 1 FROM subscription_plan_nodes pn WHERE pn.plan_id=p.id AND pn.node_id=? AND pn.source='internal'
			)
		)
		ORDER BY s.created_at,s.id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load node subscribers: %w", err)
	}
	type candidate struct {
		credential Credential
		total      int64
		cycleType  string
		cycleDay   int
		createdAt  string
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.credential.SubscriptionID, &item.credential.VLESSUUID, &item.credential.Hysteria2Password, &item.total, &item.cycleType, &item.cycleDay, &item.createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	credentials := make([]Credential, 0, len(candidates))
	for _, candidate := range candidates {
		usage, err := Current(ctx, db, candidate.credential.SubscriptionID, candidate.cycleType, candidate.cycleDay, candidate.createdAt, now)
		if err != nil {
			return nil, err
		}
		if candidate.total > 0 && usage.UploadBytes+usage.DownloadBytes >= candidate.total {
			continue
		}
		candidate.credential.Protocol = protocol
		credentials = append(credentials, candidate.credential)
	}
	return credentials, nil
}

func CycleWindow(now time.Time, cycleType string, cycleDay int, createdAt string) (string, string) {
	now = now.UTC()
	if strings.ToLower(strings.TrimSpace(cycleType)) != "monthly" {
		start := now
		if parsed, err := parseSQLiteTime(createdAt); err == nil {
			start = parsed.UTC()
		}
		return start.Format(time.RFC3339), ""
	}
	if cycleDay < 1 || cycleDay > 31 {
		cycleDay = 1
	}
	boundary := func(year int, month time.Month) time.Time {
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		day := cycleDay
		if day > lastDay {
			day = lastDay
		}
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	current := boundary(now.Year(), now.Month())
	if now.Before(current) {
		previous := now.AddDate(0, -1, 0)
		return boundary(previous.Year(), previous.Month()).Format(time.RFC3339), current.Format(time.RFC3339)
	}
	next := now.AddDate(0, 1, 0)
	return current.Format(time.RFC3339), boundary(next.Year(), next.Month()).Format(time.RFC3339)
}

func parseSQLiteTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid time")
}
