package workbuddy

// 本文件实现 WorkBuddy 的每日自动运营调度：签到（含连登管家）与活跃上报。
//
// 按 CONTEXT.md 的时区硬规则，调度器必须绑定站点时区（cron.WithLocation）并带
// TZ watcher；签到与连登归属「几点执行」与「连续天数按哪天算」，属日历语义，
// 因此这里的时间构造一律走站点时区（对齐 internal/cronjobs 与 lobsterai 的做法）。
//
// 账号范围：仅国内版（codebuddy.cn）参与。国际版无签到体系，接口返回
// 10001「签到活动未开启或已过期」，连登接口 500，故一律跳过。

import (
	"context"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"

	"github.com/robfig/cron/v3"
)

// checkinCronSpec 是签到与连登管家的触发时刻（站点时区）。
// 9 点与 21 点各一次，与官方客户端签到脚本一致；错开整点 5 分钟避开拥堵。
const checkinCronSpec = "5 9,21 * * *"

// activityCronSpec 是活跃上报的触发时刻（站点时区，每天一次即可）。
// 一条上报同时点亮连登并解锁 first_buddy（领养前置），按天去重，无需多时点。
const activityCronSpec = "5 10 * * *"

// cronRuntime 包装一个绑定站点时区的 cron 调度器。
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

// StartCheckinScheduler 启动每日签到与活跃上报调度（幂等）。ctx 取消时停止。
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

// armScheduler 往调度器挂载签到与活跃上报任务（重建调度器后需重新挂载）。
func (s *Service) armScheduler(ctx context.Context, rt *cronRuntime) {
	if _, err := rt.scheduler.AddFunc(checkinCronSpec, func() { s.RunScheduledCheckin(ctx) }); err != nil {
		applog.Warn(ctx, "workbuddy", "register checkin cron failed", "error", err.Error())
	}
	if _, err := rt.scheduler.AddFunc(activityCronSpec, func() { s.RunScheduledActivity(ctx) }); err != nil {
		applog.Warn(ctx, "workbuddy", "register activity cron failed", "error", err.Error())
	}
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

// rebuildScheduler 用新时区重建调度器，并重新挂载任务（新调度器为空，漏挂会丢任务）。
// 旧调度器以持锁取得的当前值为准，避免与其它重建路径互相覆盖。
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

// RunScheduledActivity 是活跃上报定时回调：开关关闭或插件停用时静默跳过。
func (s *Service) RunScheduledActivity(ctx context.Context) {
	st := s.Settings()
	if !st.autoActivityEnabled() || !st.Enabled {
		return
	}
	_ = s.ReportActivityAll(ctx)
}

// CheckinAll 对全部账号执行签到（含连登管家），返回每账号结果。
// 国际版账号返回 skipped，不计入失败。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	results := make([]map[string]interface{}, 0)
	for _, a := range s.Settings().Accounts {
		if a.Disabled {
			continue
		}
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		if !checkinSupported(a) {
			item["success"] = true
			item["status"] = "skipped"
			item["message"] = "国际版无签到体系"
			results = append(results, item)
			continue
		}
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
		if result.StreakDays > 0 {
			item["streakDays"] = result.StreakDays
		}
		results = append(results, item)
	}
	return results
}

// ReportActivityAll 对全部国内版账号执行一次对话活跃上报。
// 账号间限速，避免同秒集中打上游。
func (s *Service) ReportActivityAll(ctx context.Context) []map[string]interface{} {
	results := make([]map[string]interface{}, 0)
	first := true
	for _, a := range s.Settings().Accounts {
		if a.Disabled || !checkinSupported(a) {
			continue
		}
		if !first {
			time.Sleep(activityAccountDelay)
		}
		first = false
		item := map[string]interface{}{"accountId": a.ID, "nickname": a.Nickname}
		if err := s.reportChatActivity(ctx, a); err != nil {
			item["success"] = false
			item["error"] = err.Error()
		} else {
			item["success"] = true
			item["message"] = "活跃已上报"
		}
		results = append(results, item)
	}
	return results
}

// activityAccountDelay 是活跃上报的账号间隔。
const activityAccountDelay = 800 * time.Millisecond

// runCheckin 执行单账号签到并回写状态（余额 + 最近签到时刻 + 错误），
// 随后跑一次连登管家（兑换已解锁档位 + 抽完抽奖次数）。
func (s *Service) runCheckin(ctx context.Context, acc Account) (CheckinResult, error) {
	result, err := s.dailyCheckin(ctx, acc)
	if err != nil {
		acc.LastError = err.Error()
		_ = s.upsertAccount(ctx, acc)
		return CheckinResult{}, err
	}
	if bal, cerr := s.fetchBalance(ctx, acc); cerr == nil {
		acc.Credits = int64(bal.Remain)
	}
	acc.LastError = ""
	acc.LastCheckinAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.upsertAccount(ctx, acc)

	if result.Status == "success" || result.Status == "already" {
		if _, berr := s.runStreakBonus(ctx, acc); berr != nil {
			applog.Warn(ctx, "workbuddy", "streak bonus failed", "account", acc.ID, "error", berr.Error())
		}
	}
	return result, nil
}

