package subscription

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
)

func loadSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	var settings Settings
	var limitEnabled, geo int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(default_template_id, 'builtin_mihomo_default'), default_rate_limit_enabled, COALESCE(default_rate_limit_per_minute, 30), COALESCE(default_refresh_hours, 24), geoip_enabled FROM subscription_settings WHERE id = 1`).Scan(&settings.DefaultTemplateID, &limitEnabled, &settings.DefaultRateLimitPerMin, &settings.DefaultRefreshHours, &geo)
	settings.DefaultRateLimitEnabled = limitEnabled == 1
	settings.GeoIPEnabled = geo == 1
	if settings.DefaultTemplateID == "" {
		settings.DefaultTemplateID = defaultTemplateID
	}
	return settings, err
}

// effectiveCycleConfig 返回与记账（subscriptionledger.RecordBatchDetailed）
// 一致的周期/配额口径：plan 型订阅取套餐值，profile 型（plan_id=''）回退到
// 所属 profile；找不到订阅行时回退调用方传入的订阅行自身值。
func effectiveCycleConfig(ctx context.Context, db *sql.DB, sub Subscription) (int64, string, int) {
	var total int64
	var cycleType string
	var cycleDay int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(p.total_bytes, pf.total_bytes, 0), COALESCE(p.cycle_type, pf.cycle_type, 'none'), COALESCE(p.cycle_day, pf.cycle_day, 1)
		FROM subscription_subscriptions s
		LEFT JOIN subscription_plans p ON p.id = s.plan_id
		LEFT JOIN subscription_profiles pf ON pf.id = s.profile_id
		WHERE s.id = ?`, sub.ID).Scan(&total, &cycleType, &cycleDay)
	if err != nil {
		return sub.TotalBytes, sub.CycleType, sub.CycleDay
	}
	return total, cycleType, cycleDay
}

func computeTraffic(ctx context.Context, db *sql.DB, sub Subscription) TrafficInfo {
	totalBytes, cycleType, cycleDay := effectiveCycleConfig(ctx, db, sub)
	info := TrafficInfo{Total: totalBytes, Source: "panel", Status: "active", MeteringStatus: "pending", CycleStart: sub.CycleStart, CycleEnd: sub.CycleEnd}
	usage, err := subscriptionledger.Current(ctx, db, sub.ID, cycleType, cycleDay, sub.CreatedAt, time.Now().UTC())
	if err == nil {
		info.Upload = usage.UploadBytes
		info.Download = usage.DownloadBytes
		info.MeteringStatus = usage.Metering
		info.CycleStart = usage.CycleStart
		info.CycleEnd = usage.CycleEnd
	}
	if sub.ExpireAt != "" {
		if t, err := parseTime(sub.ExpireAt); err == nil {
			info.Expire = t.Unix()
			if time.Now().After(t) {
				info.Status = "expired"
			}
		}
	}
	used := info.Upload + info.Download
	if info.Total > 0 {
		info.Percent = float64(used) / float64(info.Total) * 100
		if used >= info.Total && info.Status == "active" {
			info.Status = "exhausted"
		}
	}
	return info
}

func recordSubscriptionUsage(ctx context.Context, db *sql.DB, report subscriptionUsageReport) (bool, error) {
	return subscriptionledger.Record(ctx, db, subscriptionledger.Report{
		ServerID: report.ServerID, NodeID: report.NodeID, CredentialID: report.CredentialID,
		BootID: report.BootID, Sequence: report.Sequence, UploadBytes: report.UploadBytes, DownloadBytes: report.DownloadBytes,
	}, time.Now().UTC())
}

// planCycleWindow 面板展示用周期窗口（仅 monthly；非 monthly 返回空）。
// 与 subscriptionledger.CycleWindow 同时边界，统一按站点时区结算日界。
func planCycleWindow(now time.Time, cycleType string, cycleDay int, loc *time.Location) (string, string) {
	if normalizeCycleType(cycleType) != "monthly" {
		return "", ""
	}
	if loc == nil {
		loc = time.Local
	}
	if cycleDay < 1 || cycleDay > 31 {
		cycleDay = 1
	}
	boundary := func(year int, month time.Month) time.Time {
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
		day := cycleDay
		if day > lastDay {
			day = lastDay
		}
		return time.Date(year, month, day, 0, 0, 0, 0, loc).UTC()
	}
	local := now.In(loc)
	current := boundary(local.Year(), local.Month())
	var start, end time.Time
	if now.Before(current) {
		previous := local.AddDate(0, -1, 0)
		start = boundary(previous.Year(), previous.Month())
		end = current
	} else {
		start = current
		next := local.AddDate(0, 1, 0)
		end = boundary(next.Year(), next.Month())
	}
	return start.Format(time.RFC3339), end.Format(time.RFC3339)
}

func loadQuality(ctx context.Context, db *sql.DB, serverID string) []QualitySummary {
	if serverID == "" {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT target_name, success, COALESCE(latency_ms, 0), checked_at FROM server_network_quality_samples WHERE server_id = ? AND checked_at >= datetime('now', '-1 day') ORDER BY checked_at DESC`, serverID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type agg struct {
		name        string
		count       int
		success     int
		total       float64
		prev        float64
		jitter      float64
		jitterCount int
		latest      float64
		sampledAt   string
	}
	items := map[string]*agg{}
	for rows.Next() {
		var name, checked string
		var success int
		var latency float64
		_ = rows.Scan(&name, &success, &latency, &checked)
		a := items[name]
		if a == nil {
			a = &agg{name: name, latest: latency, sampledAt: checked, prev: -1}
			items[name] = a
		}
		a.count++
		if success == 1 {
			a.success++
			a.total += latency
			if a.prev >= 0 {
				a.jitter += absFloat(latency - a.prev)
				a.jitterCount++
			}
			a.prev = latency
		}
	}
	out := []QualitySummary{}
	for _, a := range items {
		avg := 0.0
		if a.success > 0 {
			avg = a.total / float64(a.success)
		}
		jitter := 0.0
		if a.jitterCount > 0 {
			jitter = a.jitter / float64(a.jitterCount)
		}
		loss := 0.0
		if a.count > 0 {
			loss = (1 - float64(a.success)/float64(a.count)) * 100
		}
		out = append(out, QualitySummary{Name: a.name, LatencyMS: avg, AvgLatencyMS: avg, JitterMS: jitter, LossRate: loss, SampledAt: a.sampledAt})
	}
	return out
}

func isRateLimited(ctx context.Context, db *sql.DB, subID, ip string, perMin int) bool {
	if perMin <= 0 {
		perMin = defaultLimitPerMin
	}
	// Per-IP dimension for one subscription catches a single client hammering
	// the endpoint. A token-global dimension (all IPs combined) limits an
	// attacker who spreads the leaked token across many hosts and stays under
	// each per-IP ceiling.
	var perIP, total int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND ip_address = ? AND created_at >= datetime('now', '-1 minute')`, subID, ip).Scan(&perIP)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = ? AND created_at >= datetime('now', '-1 minute')`, subID).Scan(&total)
	return perIP >= perMin || total >= perMin
}

func (s *Service) logAccess(ctx context.Context, db *sql.DB, subID, token, ip, ua, format string, success bool, statusCode int, errMsg string, nodeCount int, traffic TrafficInfo) {
	_, _ = db.ExecContext(ctx, `INSERT INTO subscription_access_logs (subscription_id, public_token, ip_address, user_agent, format, success, status_code, error_message, node_count, upload_bytes, download_bytes, total_bytes, expire_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subID, token, ip, ua, format, boolToInt(success), statusCode, nullString(errMsg), nodeCount, traffic.Upload, traffic.Download, traffic.Total, traffic.Expire)
}

func normalizeTrafficLimitMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "upload", "download":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "total"
	}
}

func trafficUsedBytesForMode(rxTotal, txTotal float64, mode string) int64 {
	var used float64
	switch normalizeTrafficLimitMode(mode) {
	case "upload":
		used = txTotal
	case "download":
		used = rxTotal
	default:
		used = rxTotal + txTotal
	}
	if used < 0 {
		return 0
	}
	return int64(used)
}
