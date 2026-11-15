package qoder

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 本文件实现签到历史的持久化与日期工具。
//
// 为什么本地记账：上游 legacy 端点 /sash/api/v1/me/daily-check-in/status 在国内
// 已 DISABLED（streak 恒 0）、国际版直接 404，连续签到天数只能由本地计算。
//
// 窗口日期而非自然日：每日签到窗口是 10:00 → 次日 09:59（UTC+8，上游下发
// startAt/endAt 实测）。10:00 前点击仍属「昨天的窗口」，按自然日记账会把当天
// 误记为已签、连续天数虚高。
//
// 时区：窗口边界由上游定义为 UTC+8，属上游协议事实，故窗口日期统一按 cstZone
// 归属；而「几点执行签到」是日历语义，走站点时区（见 scheduler.go）。

// checkinRecord 是一条签到记录（同一窗口日期幂等）。
type checkinRecord struct {
	Date   string
	Amount float64
}

const dayLayout = "2006-01-02"

// todayIn 返回指定时区的今天（YYYY-MM-DD）。
func todayIn(loc *time.Location) string {
	return time.Now().In(loc).Format(dayLayout)
}

// shiftDay 把 YYYY-MM-DD 平移若干天；非法输入返回空串。
func shiftDay(date string, delta int) string {
	t, err := time.ParseInLocation(dayLayout, date, cstZone)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, delta).Format(dayLayout)
}

// addDays 是 shiftDay 的可读别名。
func addDays(date string, n int) string { return shiftDay(date, n) }

// monthDay 把 YYYY-MM-DD 渲染成 MM-DD。
func monthDay(date string) string {
	t, err := time.ParseInLocation(dayLayout, date, cstZone)
	if err != nil {
		return date
	}
	return t.Format("01-02")
}

// unixToDate 把上游时间戳（秒或毫秒）转成 UTC+8 的 YYYY-MM-DD。
func unixToDate(v int64) string {
	if v <= 0 {
		return ""
	}
	if v > 1e12 {
		v /= 1000
	}
	return time.Unix(v, 0).In(cstZone).Format(dayLayout)
}

// parseHHMM 解析 "09:15" → (9, 15, true)。
func parseHHMM(s string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func pad2(v int) string {
	if v < 10 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func itoa(v int) string { return strconv.Itoa(v) }

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func sortStrings(in []string) { sort.Strings(in) }

// ensureCheckinHistorySchema 建签到历史表（幂等）。
func ensureCheckinHistorySchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS qoder_checkin_history (
		account_id TEXT NOT NULL,
		window_date TEXT NOT NULL,
		amount REAL NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (account_id, window_date)
	)`)
	return err
}

// loadCheckinHistory 读取某账号的签到记录（按窗口日期升序）。
func (s *Service) loadCheckinHistory(ctx context.Context, accountID string) []checkinRecord {
	if strings.TrimSpace(accountID) == "" {
		return nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT window_date, amount FROM qoder_checkin_history WHERE account_id = ? ORDER BY window_date`, accountID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]checkinRecord, 0, 32)
	for rows.Next() {
		var r checkinRecord
		if rows.Scan(&r.Date, &r.Amount) == nil {
			out = append(out, r)
		}
	}
	return out
}

// recordCheckin 记录某窗口日期的签到（同日幂等；金额为 0 时不覆盖已有非 0 记录），
// 返回记录后的本地统计。
func (s *Service) recordCheckin(ctx context.Context, accountID, windowDate string, amount float64) (int, int, float64) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(windowDate) == "" {
		return 0, 0, 0
	}
	db, err := s.open(ctx)
	if err != nil {
		return s.localCheckinStats(ctx, accountID)
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `
		INSERT INTO qoder_checkin_history (account_id, window_date, amount, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, window_date) DO UPDATE SET
			amount = CASE WHEN excluded.amount > 0 THEN excluded.amount ELSE qoder_checkin_history.amount END,
			updated_at = excluded.updated_at`,
		accountID, windowDate, amount, time.Now().UTC().Format(time.RFC3339))
	return s.localCheckinStats(ctx, accountID)
}

// localCheckinStats 只读返回该账号的连续天数 / 累计天数 / 累计积分。
func (s *Service) localCheckinStats(ctx context.Context, accountID string) (int, int, float64) {
	return calcLocalCheckinStats(s.loadCheckinHistory(ctx, accountID))
}
