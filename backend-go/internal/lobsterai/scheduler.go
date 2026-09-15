package lobsterai

// 本文件实现每日签到调度：站点时区下 9 点与 21 点各跑一次全部账号的签到 +
// 余额刷新。按 CONTEXT.md 硬性规则，调度器必须绑定站点时区并带 TZ watcher。

import (
	"context"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"

	"github.com/robfig/cron/v3"
)

// cronRuntime 包装一个站点时区绑定的 cron 调度器。
type cronRuntime struct {
	scheduler *cron.Cron
	loc       *time.Location
}

// location 返回调度器当前绑定的时区。
func (c *cronRuntime) location() *time.Location {
	if c == nil || c.loc == nil {
		return time.Local
	}
	return c.loc
}

// StartCheckinScheduler 启动每日签到调度（幂等）。ctx 取消时停止调度器。
func (s *Service) StartCheckinScheduler(ctx context.Context) {
	s.schedStart.Do(func() {
		loc := s.siteLocation(ctx)
		rt := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
		// 9 点与 21 点各一次（与官方签到脚本一致，避开整点拥堵再加 5 分钟）。
		if _, err := rt.scheduler.AddFunc("5 9,21 * * *", func() {
			s.RunScheduledCheckin(ctx)
		}); err != nil {
			applog.Warn(ctx, "lobsterai", "register checkin cron failed", "error", err.Error())
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

// watchTimezone 每分钟核对站点时区；变化时重建调度器并重新挂载任务。
//
// 基准必须取 s.scheduler 的当前值，不能用启动时传入的 rt：重建后 rt 已被
// 替换，若继续拿旧 rt 比较，时区变更一次后每分钟都会判定「又变了」，
// 从而反复 Stop 已停止的旧调度器并让新建的调度器泄漏、任务重复触发。
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

// rebuildScheduler 用新时区重建调度器，重新挂载签到任务（旧调度器为空会导致任务丢失）。
// 旧调度器以持锁取得的当前值为准，避免与其它重建路径互相覆盖。
func (s *Service) rebuildScheduler(ctx context.Context, loc *time.Location) {
	next := &cronRuntime{scheduler: cron.New(cron.WithLocation(loc)), loc: loc}
	if _, err := next.scheduler.AddFunc("5 9,21 * * *", func() {
		s.RunScheduledCheckin(ctx)
	}); err != nil {
		applog.Warn(ctx, "lobsterai", "register checkin cron failed", "error", err.Error())
		return
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

// RunScheduledCheckin 是定时回调：自动签到关闭时静默跳过。
func (s *Service) RunScheduledCheckin(ctx context.Context) {
	if !s.autoCheckinEnabled() {
		return
	}
	if !s.Settings().Enabled {
		return
	}
	_ = s.CheckinAll(ctx)
}

// CheckinAll 对全部未停用账号执行签到 + 余额刷新，返回每账号结果。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	st := s.Settings()
	results := make([]map[string]interface{}, 0, len(st.Accounts))
	for _, a := range st.Accounts {
		if a.Disabled {
			continue
		}
		result, err := s.runCheckin(ctx, a)
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		if err != nil {
			item["success"] = false
			item["error"] = err.Error()
		} else {
			item["success"] = true
			item["status"] = result.Status
			item["message"] = result.Message
			if result.Gained > 0 {
				item["gained"] = result.Gained
			}
		}
		results = append(results, item)
	}
	return results
}
