package cronjobs

import (
	"context"
)

// BuiltinTask 是模块内置的定时调度条目:不落 cron_tasks 表,不暴露给用户编辑,
// 计划与开关由业务模块自己决定(Enabled/Schedule 每次重载时回调求值)。
// 它与用户任务共用同一个调度器,因此天然走站点时区与 TZ watcher。
type BuiltinTask struct {
	Name     string
	Enabled  func() bool
	Schedule func() string
	Run      func(ctx context.Context)
}

type builtinPlanItem struct {
	name     string
	schedule string
	run      func(ctx context.Context)
}

// RegisterBuiltins 注册内置调度条目并立即刷新(接线在服务组装阶段调用)。
func (s *Service) RegisterBuiltins(tasks ...BuiltinTask) {
	if len(tasks) == 0 {
		return
	}
	s.mu.Lock()
	s.builtins = append(s.builtins, tasks...)
	s.mu.Unlock()
	_ = s.ReloadBuiltins(context.Background())
}

// ReloadBuiltins 重新求值内置条目的开关与计划,用于业务侧配置变更后即时生效。
func (s *Service) ReloadBuiltins(ctx context.Context) error {
	plan := s.builtinPlan()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyBuiltinsLocked(plan)
	return nil
}

func (s *Service) builtinPlan() []builtinPlanItem {
	s.mu.Lock()
	builtins := append([]BuiltinTask(nil), s.builtins...)
	s.mu.Unlock()

	plan := make([]builtinPlanItem, 0, len(builtins))
	for _, b := range builtins {
		if b.Run == nil || b.Enabled == nil || !b.Enabled() {
			continue
		}
		schedule := ""
		if b.Schedule != nil {
			schedule = b.Schedule()
		}
		normalized, err := normalizeCronSchedule(schedule)
		if err != nil || normalized == "" {
			continue
		}
		plan = append(plan, builtinPlanItem{name: b.Name, schedule: normalized, run: b.Run})
	}
	return plan
}

func (s *Service) applyBuiltinsLocked(plan []builtinPlanItem) {
	for name, entryID := range s.builtinEntries {
		s.scheduler.Remove(entryID)
		delete(s.builtinEntries, name)
	}
	for _, item := range plan {
		item := item
		entryID, err := s.scheduler.AddFunc(item.schedule, func() {
			go item.run(context.Background())
		})
		if err != nil {
			continue
		}
		s.builtinEntries[item.name] = entryID
	}
}
