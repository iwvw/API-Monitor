package qoder

// 本文件实现 Qoder 的每日自动签到调度。
//
// 按 CONTEXT.md 的时区硬规则，调度器必须绑定站点时区（cron.WithLocation）并带
// TZ watcher；签到归属「几点执行」，属日历语义，因此时间构造一律走站点时区
// （对齐 internal/cronjobs 与 lobsterai/workbuddy 的做法）。
//
// 与上游 qoder2api 对齐的三条行为：
//   - 签到时刻可配置（AutoCheckinTimes，HH:MM 列表，留空默认 10:00）；
//   - 账号错峰：每个账号在时刻点附近有由账号 ID 哈希派生的**跨天稳定**偏移，
//     账号之间另有最小间隔 + 真随机抖动。批量同秒签到是最明显的自动化特征；
//   - 漏签重试：上游活动尚未下发（no_campaign 等）时不标记当日完成，每分钟重试
//     直到时间点 + 1 小时，避免服务端延迟下发导致漏签。
//
// 账号范围：全部「未停用」账号都参与签到（签到与转发解耦，停用转发不中断签到）。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"

	"github.com/robfig/cron/v3"
)

// checkinRetryGrace 是时间点后的重试兜底时长：超过它就不再重试该时段。
const checkinRetryGrace = time.Hour

// cronRuntime 包装一个绑定站点时区的 cron 调度器。
type cronRuntime struct {
	scheduler *cron.Cron
	loc       *time.Location
	// specs 记录已挂载的 cron spec，时区或签到时刻变化时据此判断是否需要重建。
	specs []string
}

func (c *cronRuntime) location() *time.Location {
	if c == nil || c.loc == nil {
		return time.Local
	}
	return c.loc
}

// doneSlots 记录「日期 + 时段」是否已完成本轮签到（仅内存，重启清空）。
type doneSlot struct {
	date string
	time string
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
		go s.retryPendingSlots(ctx)
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

// armScheduler 按当前签到时刻挂载任务（重建调度器后需重新挂载）。
func (s *Service) armScheduler(ctx context.Context, rt *cronRuntime) {
	st := s.Settings()
	times := st.checkinTimes()
	specs := make([]string, 0, len(times))
	for _, t := range times {
		h, m, ok := parseHHMM(t)
		if !ok {
			continue
		}
		slot := t
		cronSpec := buildCronSpec(h, m)
		specs = append(specs, cronSpec)
		if _, err := rt.scheduler.AddFunc(cronSpec, func() {
			s.runSlot(ctx, rt.location(), slot)
		}); err != nil {
			applog.Warn(ctx, "qoder", "register checkin cron failed", "spec", cronSpec, "error", err.Error())
		}
	}
	rt.specs = specs
}

// buildCronSpec 生成「每天 h 点 m 分」的 cron 表达式。
func buildCronSpec(h, m int) string {
	return pad2(m) + " " + pad2(h) + " * * *"
}

// watchTimezone 每分钟核对站点时区与签到时刻；变化时重建调度器并重新挂载任务。
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
			if cur != nil && loc == cur.location() && sameSpecs(cur.specs, s.currentCronSpecs()) {
				continue
			}
			s.rebuildScheduler(ctx, loc)
		}
	}
}

// retryPendingSlots 每分钟检查一次：已到点但还没完成的签到时段继续跑，
// 直到完成或超过 checkinRetryGrace。上游活动可能晚于整点下发，答一次就放弃会漏签。
func (s *Service) retryPendingSlots(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepSlots(ctx)
		}
	}
}

// sweepSlots 对本应在今天已经开始的签到时段做一次补跑检查。
func (s *Service) sweepSlots(ctx context.Context) {
	st := s.Settings()
	if !st.autoCheckinEnabled() || !st.Enabled {
		return
	}
	loc := s.siteLocation(ctx)
	now := time.Now().In(loc)
	for _, t := range st.checkinTimes() {
		h, m, ok := parseHHMM(t)
		if !ok {
			continue
		}
		base := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
		if now.Before(base) {
			continue
		}
		key := doneSlot{date: now.Format(dayLayout), time: t}
		s.slotMu.Lock()
		done := s.doneSlots[key]
		s.slotMu.Unlock()
		if done {
			continue
		}
		s.runSlot(ctx, loc, t)
	}
}

// currentCronSpecs 返回当前签到时刻对应的 cron spec 列表。
func (s *Service) currentCronSpecs() []string {
	st := s.Settings()
	times := st.checkinTimes()
	out := make([]string, 0, len(times))
	for _, t := range times {
		if h, m, ok := parseHHMM(t); ok {
			out = append(out, buildCronSpec(h, m))
		}
	}
	return out
}

func sameSpecs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

// runSlot 执行一个签到时段：按账号错峰逐个签到，可重试的失败留到下一分钟。
// slot 是该时段的 HH:MM（站点时区）。
func (s *Service) runSlot(ctx context.Context, loc *time.Location, slot string) {
	st := s.Settings()
	if !st.autoCheckinEnabled() || !st.Enabled {
		return
	}
	h, m, ok := parseHHMM(slot)
	if !ok {
		return
	}
	now := time.Now().In(loc)
	key := doneSlot{date: now.Format(dayLayout), time: slot}
	s.slotMu.Lock()
	if s.doneSlots[key] || s.runningSlots[key] {
		s.slotMu.Unlock()
		return
	}
	if s.runningSlots == nil {
		s.runningSlots = map[doneSlot]bool{}
	}
	s.runningSlots[key] = true
	s.slotMu.Unlock()
	defer func() {
		s.slotMu.Lock()
		delete(s.runningSlots, key)
		s.slotMu.Unlock()
	}()

	if retryable := s.checkinSlot(ctx, time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc), slot, key); retryable == 0 {
		s.markSlotDone(key)
		return
	}
	// 还有可重试账号：过兜底时长仍未完成则放弃本轮，避免无限重试。
	if now.After(time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc).Add(checkinRetryGrace)) {
		s.markSlotDone(key)
		applog.Warn(ctx, "qoder", "checkin slot gave up after grace", "slot", slot)
	}
}

func (s *Service) markSlotDone(key doneSlot) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.doneSlots == nil {
		s.doneSlots = map[doneSlot]bool{}
	}
	s.doneSlots[key] = true
	delete(s.slotFinished, key)
}

// slotAccountDone 报告该账号在本时段是否已完成签到（避免每分钟重试时把已成功的
// 账号再打一遍——上游按窗口幂等，但重复请求本身也是多余的自动化特征）。
func (s *Service) slotAccountDone(key doneSlot, accountID string) bool {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	return s.slotFinished[key][accountID]
}

// markSlotAccountDone 记录某账号在本时段已完成。
func (s *Service) markSlotAccountDone(key doneSlot, accountID string) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.slotFinished == nil {
		s.slotFinished = map[doneSlot]map[string]bool{}
	}
	if s.slotFinished[key] == nil {
		s.slotFinished[key] = map[string]bool{}
	}
	s.slotFinished[key][accountID] = true
}

// checkinSlot 在时段基准时刻附近按账号错峰执行签到，返回仍可重试的账号数。
// 已在本时段成功的账号会被跳过，因此每分钟的补跑只处理未完成的账号。
func (s *Service) checkinSlot(ctx context.Context, base time.Time, slot string, key doneSlot) int {
	st := s.Settings()
	first := true
	retryable := 0
	for _, a := range st.Accounts {
		if a.Disabled || s.slotAccountDone(key, a.ID) {
			continue
		}
		if checkedInToday(a, s.siteLocation(ctx)) {
			// 已签到（含当天手动签到 / 重启后补跑）：不要再打一次上游。
			s.markSlotAccountDone(key, a.ID)
			continue
		}
		if !first {
			if !sleepCtx(ctx, st.checkinGap()+randomJitter(st.checkinJitter())) {
				return retryable
			}
		}
		first = false
		if target := base.Add(staggerOffset(a.ID, st.checkinStaggerWindow())); st.checkinStaggerEnabled() {
			if wait := time.Until(target); wait > 0 && wait < checkinRetryGrace {
				if !sleepCtx(ctx, wait) {
					return retryable
				}
			}
		}
		result, err := s.runCheckin(ctx, a)
		if err != nil {
			retryable++
			applog.Warn(ctx, "qoder", "scheduled checkin failed", "account", a.ID, "slot", slot, "error", err.Error())
			continue
		}
		switch result.Status {
		case "success", "already":
			s.markSlotAccountDone(key, a.ID)
			applog.Info(ctx, "qoder", "scheduled checkin ok", "account", a.ID, "slot", slot, "status", result.Status)
		case "none":
			// 上游可能还没下发当日活动，留到下一分钟重试。
			retryable++
			applog.Info(ctx, "qoder", "scheduled checkin pending", "account", a.ID, "slot", slot, "status", result.Status)
		case "need_fingerprint":
			// 配置问题，重试不会自愈，不占用重试额度。
			s.markSlotAccountDone(key, a.ID)
			applog.Warn(ctx, "qoder", "checkin skipped: fingerprint required", "account", a.ID, "slot", slot)
		}
	}
	return retryable
}

// checkedInToday 报告账号在站点时区的今天是否已经签到成功。
// 依据：账号的 LastCheckinAt（success/already 时才写入）当天已存在。
func checkedInToday(a Account, loc *time.Location) bool {
	if strings.TrimSpace(a.LastCheckinAt) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, a.LastCheckinAt)
	if err != nil {
		return false
	}
	return t.In(loc).Format(dayLayout) == time.Now().In(loc).Format(dayLayout)
}

// staggerOffset 返回该账号在错峰窗口内的固定偏移（[-window, +window]）。
// 必须由账号 ID 稳定派生：每次随机会让同一账号的行为画像逐日漂移。
func staggerOffset(accountID string, window time.Duration) time.Duration {
	secs := int64(window.Seconds())
	if secs <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte("checkin-stagger:" + accountID))
	v := int64(binary.BigEndian.Uint64(sum[:8]) % uint64(2*secs+1))
	return time.Duration(v-secs) * time.Second
}

// randomJitter 返回 [0, max) 的真随机抖动；max<=0 返回 0。
// 用 crypto/rand：可重建的伪随机序列等于没有抖动。
func randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	span := uint64(max)
	return time.Duration(binary.BigEndian.Uint64(b[:]) % span)
}

// sleepCtx 等待指定时长，可被 ctx 取消；返回 false 表示被取消。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// CheckinAll 手动对全部未停用账号执行签到（含余额刷新），返回每账号结果。
// 顺序执行且账号间有最小间隔，避免手动「全部签到」也形成同秒批量特征。
func (s *Service) CheckinAll(ctx context.Context) []map[string]interface{} {
	st := s.Settings()
	results := make([]map[string]interface{}, 0, len(st.Accounts))
	first := true
	for _, a := range st.Accounts {
		if a.Disabled {
			continue
		}
		if !first {
			if !sleepCtx(ctx, st.checkinGap()) {
				return results
			}
		}
		first = false
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
		if result.StreakDays > 0 {
			item["streakDays"] = result.StreakDays
		}
		results = append(results, item)
	}
	return results
}
