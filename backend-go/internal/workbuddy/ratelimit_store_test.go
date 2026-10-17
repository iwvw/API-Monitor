package workbuddy

import (
	"context"
	"testing"
	"time"
)

// 限流记录必须在重启后仍然生效：这是本优化的全部意义。
// 若这条失效，每次部署都会重新撞一遍 429，每次撞墙都多传一份完整请求体。
func TestModelLimitSurvivesReload(t *testing.T) {
	s := newTestService(t)
	until := time.Now().Add(2 * time.Hour)
	s.markModelLimit("acc-1", "hy3", until, "usage exceeds frequency limit")

	// 模拟重启：丢弃内存状态后重新从库里加载。
	s.modelLimitMu.Lock()
	s.modelLimits = nil
	s.modelLimitMu.Unlock()

	if s.inModelLimit("acc-1", "hy3") {
		t.Fatal("内存已清空，不应仍判定为限流")
	}

	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.loadModelLimits(ctx, db)

	if !s.inModelLimit("acc-1", "hy3") {
		t.Fatal("重启后应恢复限流记录，否则会重新撞上游 429")
	}
	got, ok := s.modelLimitUntil("acc-1", "hy3")
	if !ok {
		t.Fatal("应能取到恢复时刻")
	}
	// 恢复时刻允许秒级截断误差。
	if d := got.Sub(until); d > time.Second || d < -time.Second {
		t.Fatalf("恢复时刻偏差过大：got=%v want=%v", got, until)
	}
}

// 已过期的记录不得恢复：上游恢复时刻会变，沿用过期记录没有意义。
func TestModelLimitExpiredNotRestored(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	past := time.Now().Add(-time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO workbuddy_model_limits (account_id, model, until, reason, set_at)
		VALUES (?, ?, ?, ?, ?)`,
		"acc-old", "hy3", past.Unix(), "stale", past.Unix()); err != nil {
		t.Fatal(err)
	}

	s.loadModelLimits(ctx, db)
	if s.inModelLimit("acc-old", "hy3") {
		t.Fatal("过期记录不应恢复进冷却簿")
	}

	// 过期行应被清理掉，避免表无限增长。
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workbuddy_model_limits WHERE account_id = ?`, "acc-old").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("过期行应被清理，实际剩 %d 行", n)
	}
}

// 库里存着离谱的未来时刻时，恢复必须重新收敛到上界，不能长期锁死账号。
func TestModelLimitRestoreClampsAbsurdUntil(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	absurd := time.Now().Add(365 * 24 * time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO workbuddy_model_limits (account_id, model, until, reason, set_at)
		VALUES (?, ?, ?, ?, ?)`,
		"acc-x", "hy3", absurd.Unix(), "absurd", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	s.loadModelLimits(ctx, db)

	got, ok := s.modelLimitUntil("acc-x", "hy3")
	if !ok {
		t.Fatal("应恢复该记录")
	}
	max := time.Now().Add(modelLimitMax)
	if got.After(max.Add(time.Minute)) {
		t.Fatalf("恢复时刻应收敛到 %v 以内，实际 %v", max, got)
	}
}

// 账号被删除时，其限流记录必须一并清掉（内存与库都要清），否则诊断面会出现幽灵记录。
func TestClearModelLimitRow(t *testing.T) {
	s := newTestService(t)
	s.markModelLimit("acc-del", "hy3", time.Now().Add(time.Hour), "limited")
	s.markModelLimit("acc-keep", "hy3", time.Now().Add(time.Hour), "limited")

	s.clearModelLimitRow("acc-del")

	if s.inModelLimit("acc-del", "hy3") {
		t.Fatal("已删除账号的内存记录应被清掉")
	}
	if !s.inModelLimit("acc-keep", "hy3") {
		t.Fatal("其它账号的记录不应被误删")
	}

	// 重新加载确认库里也没了。
	s.modelLimitMu.Lock()
	s.modelLimits = nil
	s.modelLimitMu.Unlock()
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.loadModelLimits(ctx, db)

	if s.inModelLimit("acc-del", "hy3") {
		t.Fatal("库里的记录应已删除")
	}
	if !s.inModelLimit("acc-keep", "hy3") {
		t.Fatal("其它账号的库记录应保留")
	}
}

// 选号必须跳过处于限流中的账号：这是「不把请求体浪费在必然失败的账号上」的关键。
func TestEligibleCandidatesSkipLimitedAccounts(t *testing.T) {
	s := &Service{
		creditDayUsed: map[string]float64{},
		modelLimits:   map[string]modelLimit{},
		settings: Settings{
			Accounts: []Account{
				validAccount("a1", "t1"),
				validAccount("a2", "t2"),
			},
		},
	}
	s.markModelLimit("a1", "hy3", time.Now().Add(time.Hour), "limited")

	cands := s.eligibleCandidates(s.Settings().Accounts, "hy3", nil)
	if len(cands) != 1 || cands[0].ID != "a2" {
		t.Fatalf("应只保留未限流的 a2，实际 %v", cands)
	}
	// 其它模型不受影响：限流是「账号 × 模型」维度，不是账号级。
	cands = s.eligibleCandidates(s.Settings().Accounts, "other-model", nil)
	if len(cands) != 2 {
		t.Fatalf("其它模型应保留全部账号，实际 %v", cands)
	}
}
