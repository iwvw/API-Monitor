package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"

	"github.com/robfig/cron/v3"
)

// 到期扫描调度：按 CONTEXT.md 的时区硬规则，必须绑定站点时区并带 TZ watcher。
// 到期归属是日历语义（「今天到期」按站点日界算），与 expire.go 的 daysUntil 同源。
//
// 接入方式遵循本仓库既有范式：模块自持 cron 调度器（参考 workbuddy/scheduler.go
// 与 backup/service.go），而不是往 cronjobs 里塞内置任务——cronjobs 的调度对象
// 全部来自数据库行，没有内置任务注册点。

// Notifier 是通知模块的最小接口，由 server.go 注入。
type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

// expiryScanSpec 是到期扫描的触发时刻（站点时区）：每天 08:05。
const expiryScanSpec = "5 8 * * *"

type cronRuntime struct {
	scheduler *cron.Cron
	loc       *time.Location
}

func (c *cronRuntime) location() *time.Location {
	if c == nil || c.loc == nil {
		return time.Local
	}
	return c.loc
}

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

// StartExpiryScheduler 启动每日到期扫描（幂等）。ctx 取消时停止。
func (s *Service) StartExpiryScheduler(ctx context.Context) {
	s.schedStart.Do(func() {
		loc := s.siteLocation(ctx)
		rt := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
		if _, err := rt.scheduler.AddFunc(expiryScanSpec, func() { s.RunExpiryScan(context.Background()) }); err != nil {
			applog.Warn(ctx, "assets", "register expiry cron failed", "error", err.Error())
		}
		rt.scheduler.Start()
		s.schedMu.Lock()
		s.scheduler = rt
		s.schedMu.Unlock()

		go s.watchTimezone(ctx)
		go func() {
			<-ctx.Done()
			s.schedMu.Lock()
			if s.scheduler != nil {
				s.scheduler.scheduler.Stop()
			}
			s.schedMu.Unlock()
		}()
	})
}

func (s *Service) watchTimezone(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			db, err := s.open(ctx)
			if err != nil {
				continue
			}
			loc := timeutil.LocationFromSettings(ctx, db)
			db.Close()
			if loc == nil {
				continue
			}
			s.schedMu.Lock()
			cur := s.scheduler
			s.schedMu.Unlock()
			if cur == nil || loc == cur.location() {
				continue
			}
			s.rebuildScheduler(ctx, loc)
		}
	}
}

func (s *Service) rebuildScheduler(ctx context.Context, loc *time.Location) {
	next := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
	if _, err := next.scheduler.AddFunc(expiryScanSpec, func() { s.RunExpiryScan(context.Background()) }); err != nil {
		applog.Warn(ctx, "assets", "register expiry cron failed on rebuild", "error", err.Error())
	}
	next.scheduler.Start()
	s.schedMu.Lock()
	old := s.scheduler
	s.scheduler = next
	s.schedMu.Unlock()
	if old != nil {
		stopCtx := old.scheduler.Stop()
		select {
		case <-stopCtx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Service) siteLocation(ctx context.Context) *time.Location {
	db, err := s.open(ctx)
	if err != nil {
		return timeutil.LocationFromName("")
	}
	defer db.Close()
	return timeutil.LocationFromSettings(ctx, db)
}

// RunExpiryScan 扫描到期资产并按阈值分级告警。
// auto_renew 的资产跳过；同一资产同一阈值档位只提醒一次（以 alert_state 记录）。
func (s *Service) RunExpiryScan(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		applog.Warn(ctx, "assets", "expiry scan open failed", "error", err.Error())
		return
	}
	defer db.Close()

	settings, err := s.LoadSettings(ctx)
	if err != nil {
		applog.Warn(ctx, "assets", "expiry scan settings failed", "error", err.Error())
		return
	}
	loc := timeutil.LocationFromSettings(ctx, db)
	now := nowUTC()

	assets, err := s.LoadAssets(ctx, assetFilter{Limit: maxLimit})
	if err != nil {
		applog.Warn(ctx, "assets", "expiry scan load failed", "error", err.Error())
		return
	}

	for _, asset := range assets {
		if asset.AutoRenew || asset.DaysLeft == nil {
			continue
		}
		if asset.Status == statusRetired || asset.Status == statusOrphan {
			continue
		}
		days := *asset.DaysLeft
		thresholds := effectiveWarnDays(asset.WarnDays, settings.WarnDays)
		// 找到资产已跨入的最紧阈值档位（最小满足 days <= threshold 的阈值）。
		hit := 0
		for _, threshold := range thresholds {
			if days <= threshold && (hit == 0 || threshold < hit) {
				hit = threshold
			}
		}
		if days < 0 {
			hit = 0
		}
		if hit == 0 && days >= 0 {
			continue
		}
		marker := "expired"
		if hit > 0 {
			marker = fmt.Sprintf("d%d", hit)
		}
		if !s.claimAlertMarker(ctx, db, asset.ID, marker) {
			continue
		}
		eventData := map[string]interface{}{
			"assetId":     asset.ID,
			"assetName":   asset.Name,
			"assetType":   asset.AssetType,
			"category":    asset.Category,
			"expireAt":    asset.ExpireAt,
			"daysLeft":    days,
			"threshold":   hit,
			"provider":    asset.Provider,
			"scanAt":      now.Format(time.RFC3339),
			"location":    loc.String(),
		}
		if err := s.notifier.Trigger(ctx, "assets", "asset_expiry", eventData); err != nil {
			applog.Warn(ctx, "assets", "trigger asset_expiry failed", "error", err.Error(), "asset", asset.ID)
			s.releaseAlertMarker(ctx, db, asset.ID, marker)
			continue
		}
		_ = s.recordEvent(ctx, asset.ID, "expiry_alert", marker)
	}
}

// claimAlertMarker 以 (asset_id, marker) 为键做告警去重：
// 首次声明返回 true，已存在返回 false。重复提醒在档位变化时才会再次触发。
func (s *Service) claimAlertMarker(ctx context.Context, db *sql.DB, assetID, marker string) bool {
	result, err := db.ExecContext(ctx,
		"INSERT OR IGNORE INTO asset_alerts (asset_id, marker, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		assetID, marker)
	if err != nil {
		return false
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false
	}
	return affected > 0
}

func (s *Service) releaseAlertMarker(ctx context.Context, db *sql.DB, assetID, marker string) {
	_, _ = db.ExecContext(ctx,
		"DELETE FROM asset_alerts WHERE asset_id = ? AND marker = ?", assetID, marker)
}
