package subscription

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/subscriptionledger"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

func (s *Service) summary(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var total, enabled, expired, exhausted, today int
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(enabled), 0) FROM subscription_subscriptions`).Scan(&total, &enabled)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(plan_id,'')='' AND expire_at IS NOT NULL AND expire_at != '' AND datetime(expire_at) < datetime('now')`).Scan(&expired)
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM subscription_access_logs WHERE date(created_at) = date('now')`).Scan(&today)
	subs, _ := loadSubscriptions(r.Context(), db, "")
	for _, sub := range subs {
		if sub.Traffic.Total > 0 && sub.Traffic.Upload+sub.Traffic.Download >= sub.Traffic.Total {
			exhausted++
		}
	}
	response.OK(w, map[string]interface{}{"total": total, "enabled": enabled, "expired": expired, "exhausted": exhausted, "todayAccess": today})
}

// getSubscriptionUsage 返回某订阅的流量明细（对账用）：当前周期累计来自
// subscription_usage_cycles（与订阅分发面板同口径），逐日/逐时明细来自
// subscription_usage_hourly（UTC）。granularity=day|hour（默认 day），
// days 默认 30、上限 90。hourly 聚合自 2026-08-13 新版 ledger 起采集，
// 更早的周期只有周期累计、无逐日明细（趋势缺口属预期）。
func (s *Service) getSubscriptionUsage(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	ctx := r.Context()
	now := time.Now().UTC()

	var planID, cycleType, createdAt string
	var cycleDay int
	var totalBytes int64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(s.plan_id,''), COALESCE(p.total_bytes,0), COALESCE(p.cycle_type,'none'), COALESCE(p.cycle_day,1), COALESCE(s.created_at, datetime('now'))
		FROM subscription_subscriptions s
		LEFT JOIN subscription_plans p ON p.id = s.plan_id
		WHERE s.id = ?`, id).Scan(&planID, &totalBytes, &cycleType, &cycleDay, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "subscription not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = planID

	// 当前周期累计（cycles：面板口径，不受 hourly 采集历史影响）。
	cycleStart, cycleEnd := subscriptionledger.CycleWindow(now, cycleType, cycleDay, createdAt, timeutil.LocationFromSettings(ctx, db))
	var cycleUpload, cycleDownload int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(upload_bytes,0), COALESCE(download_bytes,0) FROM subscription_usage_cycles WHERE subscription_id=? AND cycle_start=?`, id, cycleStart).Scan(&cycleUpload, &cycleDownload); err != nil && !errors.Is(err, sql.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	granularity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("granularity")))
	if granularity != "hour" {
		granularity = "day"
	}
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d >= 1 && d <= 90 {
		days = d
	}
	start := now.AddDate(0, 0, -(days - 1)).Format(time.RFC3339)

	// 逐日/逐时明细（hourly，UTC 桶）。
	type usagePoint struct {
		Bucket        string `json:"bucket"`
		UploadBytes   int64  `json:"uploadBytes"`
		DownloadBytes int64  `json:"downloadBytes"`
	}
	points := []usagePoint{}
	var rows *sql.Rows
	if granularity == "hour" {
		rows, err = db.QueryContext(ctx, `SELECT hour, upload_bytes, download_bytes FROM subscription_usage_hourly WHERE subscription_id=? AND hour>=? ORDER BY hour ASC`, id, start)
		if err == nil {
			for rows.Next() {
				var p usagePoint
				if err := rows.Scan(&p.Bucket, &p.UploadBytes, &p.DownloadBytes); err == nil {
					points = append(points, p)
				}
			}
		}
	} else {
		rows, err = db.QueryContext(ctx, `SELECT strftime('%Y-%m-%d', hour) AS bucket, COALESCE(SUM(upload_bytes),0), COALESCE(SUM(download_bytes),0) FROM subscription_usage_hourly WHERE subscription_id=? AND hour>=? GROUP BY bucket ORDER BY bucket ASC`, id, start)
		if err == nil {
			for rows.Next() {
				var p usagePoint
				if err := rows.Scan(&p.Bucket, &p.UploadBytes, &p.DownloadBytes); err == nil {
					points = append(points, p)
				}
			}
		}
	}
	if rows != nil {
		rows.Close()
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	used := cycleUpload + cycleDownload
	percent := 0.0
	if totalBytes > 0 {
		percent = float64(used) / float64(totalBytes) * 100
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"subscriptionId":     id,
			"totalBytes":         totalBytes,
			"cycleStart":         cycleStart,
			"cycleEnd":           cycleEnd,
			"cycleUploadBytes":   cycleUpload,
			"cycleDownloadBytes": cycleDownload,
			"cycleUsedBytes":     used,
			"percent":            percent,
			"granularity":        granularity,
			"points":             points,
		},
	})
}
