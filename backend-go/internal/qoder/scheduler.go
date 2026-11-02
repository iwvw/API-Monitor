package qoder

// 本文件实现 Qoder 的每日自动签到调度。
//
// 按 CONTEXT.md 的时区硬规则，调度器必须绑定站点时区（cron.WithLocation）并带
// TZ watcher；签到归属「几点执行」，属日历语义，因此时间构造一律走站点时区
// （对齐 internal/cronjobs 与 lobsterai/workbuddy 的做法）。
//
// 账号范围：全部「未停用」账号都参与签到（签到与转发解耦，停用转发不中断签到）。

import (
	"context"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"

	"github.com/robfig/cron/v3"
)

// checkinCronSpec 是签到触发时刻（站点时区）。9 点与 21 点各一次，错开整点 5 分钟。
const checkinCronSpec = "5 9,21 * * *"

// cronRuntime 包装一个绑定站点时区的 cron 调度器。
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

// StartCheckinScheduler 启动每日签到调度（幂等）。ctx 取消时停止。
func (s *Service) StartCheckinScheduler(ctx context.Context) {
	s.schedStart.Do(func() {
		loc := s.siteLocation(ctx)
		rt := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
		s.armScheduler(ctx, rt)
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

// armScheduler 往调度器挂载签到任务（重建调度器后需重新挂载）。
func (s *Service) armScheduler(ctx context.Context, rt *cronRuntime) {
	if _, err := rt.scheduler.AddFunc(checkinCronSpec, func() { s.RunScheduledCheckin(ctx) }); err != nil {
		applog.Warn(ctx, "qoder", "register checkin cron failed", "error", err.Error())
	}
}

// watchTimezone 每分钟核对站点时区；变化时重建调度器并重新挂载任务。
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

// rebuildScheduler 用新时区重建调度器，并重新挂载任务。
func (s *Service) rebuildScheduler(ctx context.Context, loc *time.Location) {
	next := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
	s.armScheduler(ctx, next)
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

// RunScheduledCheckin 是签到定时回调：自动签到关闭或插件停用时静默跳过。
func (s *Service) RunScheduledCheckin(ctx context.Context) {
	st := s.Settings()
	if !st.autoCheckinEnabled() || !st.Enabled {
		return
	}
	_ = s.CheckinAll(ctx)
}

// CheckinAll 对全部未停用账号执行签到（含余额刷新），返回每账号结果。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	results := make([]map[string]interface{}, 0)
	for _, a := range s.Settings().Accounts {
		if a.Disabled {
			continue
		}
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		result, err := s.runCheckin(ctx, a)
		if err != nil {
			item["success"] = false
			item["error"] = err.Error()
			results = append(results, item)
			continue
		}
		item["success"] = true
		item["status"] = result.Status
		item["message"] = result.Message
		if result.Gained > 0 {
			item["gained"] = result.Gained
		}
		results = append(results, item)
	}
	return results
}
