package uptime

import (
	"context"
	randv2 "math/rand/v2"
	"time"
)

func (s *Service) RestartAll(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	monitors, err := loadMonitors(ctx, db, `SELECT * FROM uptime_monitors WHERE active = 1`)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for id, timer := range s.timers {
		timer.Stop()
		delete(s.timers, id)
	}
	s.stopped = false
	s.mu.Unlock()
	for _, monitor := range monitors {
		s.startMonitor(monitor)
	}
	return nil
}

func (s *Service) startMonitor(monitor map[string]interface{}) {
	id := int64Value(monitor["id"], 0)
	if id <= 0 || !boolValue(monitor["active"], true) {
		return
	}
	interval := intValue(firstNonNil(monitor["interval"], defaultIntervalSeconds), defaultIntervalSeconds)
	if interval < 5 {
		interval = defaultIntervalSeconds
	}
	s.stopMonitor(id)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	// 初次探测延迟带 0-3s 随机抖动：批量创建 monitor 时若全部 2s 后同时
	// 探测，会在同一时刻向 SQLite 并发写入（各 monitor 独立 goroutine），
	// 抖动错开相位后写高峰自然分散；后续排期保持各自固定间隔不变。
	timer := time.AfterFunc(2*time.Second+time.Duration(randv2.N(3000))*time.Millisecond, func() {
		s.runScheduledCheck(id)
	})
	s.timers[id] = timer
	s.mu.Unlock()
}

func (s *Service) runScheduledCheck(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	monitor, ok, err := loadMonitor(ctx, db, id)
	if err == nil && ok && boolValue(monitor["active"], true) {
		_, _ = s.check(ctx, db, monitor)
	}
	db.Close()
	if err != nil || !ok || !boolValue(monitor["active"], true) {
		s.stopMonitor(id)
		return
	}
	interval := intValue(firstNonNil(monitor["interval"], defaultIntervalSeconds), defaultIntervalSeconds)
	if interval < 5 {
		interval = defaultIntervalSeconds
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	timer := time.AfterFunc(time.Duration(interval)*time.Second, func() {
		s.runScheduledCheck(id)
	})
	s.timers[id] = timer
	s.mu.Unlock()
}

func (s *Service) stopMonitor(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if timer, ok := s.timers[id]; ok {
		timer.Stop()
		delete(s.timers, id)
	}
}
