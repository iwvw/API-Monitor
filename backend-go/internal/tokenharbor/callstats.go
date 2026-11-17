package tokenharbor

// 本文件实现调用次数与用量的定期落盘。

import (
	"context"
	"database/sql"
	"time"
)

// callStatsFlushInterval 是调用次数与用量定期落盘的周期。
const callStatsFlushInterval = time.Minute

// loadCallStats 启动时把已落盘的调用次数读入内存基线。
func (s *Service) loadCallStats(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT identifier, count FROM tokenharbor_call_stats`)
	if err != nil {
		return
	}
	defer rows.Close()
	s.callMu.Lock()
	defer s.callMu.Unlock()
	for rows.Next() {
		var id string
		var n int64
		if rows.Scan(&id, &n) == nil {
			s.callBase[id] = n
		}
	}
}

// recordCall 把一次成功转发记入未落盘增量。
func (s *Service) recordCall(identifier string) {
	if identifier == "" {
		return
	}
	s.callMu.Lock()
	defer s.callMu.Unlock()
	s.callPending[identifier]++
}

// callDisplay 返回某账号的累计调用次数(已落盘 + 在途)。
func (s *Service) callDisplay(identifier string) int64 {
	s.callMu.Lock()
	defer s.callMu.Unlock()
	return s.callBase[identifier] + s.callPending[identifier]
}

// flushCallStats 把在途调用次数落库;失败时放回增量等下一轮。
func (s *Service) flushCallStats(ctx context.Context) {
	s.callMu.Lock()
	if len(s.callPending) == 0 {
		s.callMu.Unlock()
		return
	}
	pend := s.callPending
	s.callPending = map[string]int64{}
	s.callMu.Unlock()

	db, err := s.open(ctx)
	if err != nil {
		s.callMu.Lock()
		for k, v := range pend {
			s.callPending[k] += v
		}
		s.callMu.Unlock()
		return
	}
	defer db.Close()
	for id, delta := range pend {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO tokenharbor_call_stats (identifier, count) VALUES (?, ?)
			ON CONFLICT(identifier) DO UPDATE SET count = count + excluded.count`, id, delta); err != nil {
			s.callMu.Lock()
			s.callPending[id] += delta
			s.callMu.Unlock()
			continue
		}
		s.callMu.Lock()
		s.callBase[id] += delta
		s.callMu.Unlock()
	}
}

// StartCallStatsFlush 启动调用次数与用量定期落盘;ctx 取消时做最后一次落盘后退出。
func (s *Service) StartCallStatsFlush(ctx context.Context) {
	s.callFlush.Do(func() {
		go func() {
			ticker := time.NewTicker(callStatsFlushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.flushCallStats(context.Background())
					s.flushUsage(context.Background())
				case <-ctx.Done():
					s.flushCallStats(context.Background())
					s.flushUsage(context.Background())
					return
				}
			}
		}()
	})
}
