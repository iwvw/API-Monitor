package workbuddy

// 本文件把「账号 × 模型」限流冷却簿落库，解决一个**可观测的流量浪费**：
//
// 限流状态原本只存内存（见 ratelimit.go 的说明），进程一重启就全丢。重启后
// 每个限流中的账号都要重新撞一次 429 才能重新学到，而这期间每次撞墙都把
// 完整请求体再发给上游一次 —— 长上下文对话的请求体常有几百 KB，账号池里
// 被限流的账号越多，浪费越大。生产实测：6 个国际号里 4 个在
// deepseek-v4.1-flash 上限流时，单次逻辑请求会把同一份 body 发出数次。
//
// 关键约束（与 ratelimit.go 的「纯内存」结论并不冲突，是它的补充）：
//   - 只恢复 **Until 仍在未来** 的记录，过期即丢弃：上游的恢复时刻会变，
//     沿用一条已经过期的记录没有意义；
//   - 恢复出来的 Until 仍经 clampModelLimitUntil 收敛，绝不因库里一条离谱的
//     时刻而长期锁死账号；
//   - 写库失败不影响转发（限流判定以内存为准），只是重启后要重新学一次。
//
// 表的行数有天然上界：账号数 × 模型数，且过期行在每次写入时顺手清理。

import (
	"context"
	"database/sql"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// ensureModelLimitTable 建冷却簿表（幂等）。
func ensureModelLimitTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workbuddy_model_limits (
		account_id TEXT NOT NULL,
		model TEXT NOT NULL,
		until INTEGER NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		set_at INTEGER NOT NULL,
		PRIMARY KEY (account_id, model)
	)`)
	return err
}

// loadModelLimits 在启动时把仍未过期的限流记录恢复进内存冷却簿。
// 过期行顺手删掉，避免表里堆积历史残留。
func (s *Service) loadModelLimits(ctx context.Context, db *sql.DB) {
	now := time.Now()
	rows, err := db.QueryContext(ctx,
		`SELECT account_id, model, until, reason, set_at FROM workbuddy_model_limits WHERE until > ?`,
		now.Unix())
	if err != nil {
		return
	}
	defer rows.Close()

	restored := make(map[string]modelLimit)
	for rows.Next() {
		var accountID, model, reason string
		var until, setAt int64
		if err := rows.Scan(&accountID, &model, &until, &reason, &setAt); err != nil {
			continue
		}
		if accountID == "" || model == "" {
			continue
		}
		// 一律重新收敛：库里可能存着旧版本写入的、超出上界的时刻。
		fixed := clampModelLimitUntil(time.Unix(until, 0), now)
		restored[modelLimitKey(accountID, model)] = modelLimit{
			AccountID: accountID,
			Model:     model,
			Until:     fixed,
			Reason:    reason,
			SetAt:     time.Unix(setAt, 0),
		}
	}
	if err := rows.Err(); err != nil {
		return
	}
	if len(restored) > 0 {
		s.modelLimitMu.Lock()
		if s.modelLimits == nil {
			s.modelLimits = map[string]modelLimit{}
		}
		for k, v := range restored {
			s.modelLimits[k] = v
		}
		s.modelLimitMu.Unlock()
		applog.Info(context.Background(), "workbuddy", "model rate limits restored",
			"count", len(restored))
	}
	// 清理过期行（best-effort）。
	_, _ = db.ExecContext(ctx, `DELETE FROM workbuddy_model_limits WHERE until <= ?`, now.Unix())
}

// persistModelLimit 把一条限流记录写库（best-effort，失败只留日志）。
// 顺带清理该行已过期的记录，保持表有界。
func (s *Service) persistModelLimit(l modelLimit) {
	if l.AccountID == "" || l.Model == "" || s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		applog.Warn(ctx, "workbuddy", "persist model limit: open db failed", "error", err.Error())
		return
	}
	defer db.Close()
	if err := ensureModelLimitTable(ctx, db); err != nil {
		applog.Warn(ctx, "workbuddy", "persist model limit: ensure table failed", "error", err.Error())
		return
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO workbuddy_model_limits (account_id, model, until, reason, set_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_id, model) DO UPDATE SET
			until = excluded.until,
			reason = excluded.reason,
			set_at = excluded.set_at`,
		l.AccountID, l.Model, l.Until.Unix(), l.Reason, l.SetAt.Unix()); err != nil {
		applog.Warn(ctx, "workbuddy", "persist model limit: upsert failed", "error", err.Error())
		return
	}
	// 已恢复的行没有保留价值，顺手删掉。
	_, _ = db.ExecContext(ctx, `DELETE FROM workbuddy_model_limits WHERE until <= ?`, time.Now().Unix())
}

// clearModelLimitRow 删除某个账号的全部限流记录（账号被删除时调用，避免残留）。
func (s *Service) clearModelLimitRow(accountID string) {
	if accountID == "" {
		return
	}
	// 内存冷却簿先清：账号已不存在，留着它只会让诊断面显示幽灵记录。
	s.modelLimitMu.Lock()
	for k, l := range s.modelLimits {
		if l.AccountID == accountID {
			delete(s.modelLimits, k)
		}
	}
	s.modelLimitMu.Unlock()

	if s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db, err := s.store.Open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `DELETE FROM workbuddy_model_limits WHERE account_id = ?`, accountID)
}
