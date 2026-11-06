package raccoon

import (
	"context"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newRefreshTestService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
}

// TestRefreshTokenLockedReloadsLatest 守住「并发刷新不覆盖新 token」的契约：
// refresh token 会轮换，第二个请求必须先读库里的最新副本；若库内 token 已被
// 并发请求刷新为可用，就直接返回，绝不再拿旧 refresh token 去打上游。
func TestRefreshTokenLockedReloadsLatest(t *testing.T) {
	s := newRefreshTestService(t)
	ctx := context.Background()

	good := Account{
		ID:           "u1",
		AccessToken:  "good-access",
		RefreshToken: "rotated-refresh",
		ExpiresAtMS:  time.Now().Add(time.Hour).UnixMilli(),
	}
	st := s.Settings()
	st.Accounts = []Account{good}
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	// 请求 A 拿到的旧副本：token 已过期、refresh token 是轮换前的旧值。
	stale := Account{
		ID:           "u1",
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAtMS:  time.Now().Add(-time.Hour).UnixMilli(),
	}
	changed, err := s.refreshTokenLocked(ctx, &stale, false)
	if err != nil {
		t.Fatalf("refreshTokenLocked 不应尝试用旧 refresh token 刷新: %v", err)
	}
	if changed {
		t.Fatal("库内 token 已可用，不应发生刷新")
	}
	if stale.AccessToken != good.AccessToken || stale.RefreshToken != good.RefreshToken {
		t.Fatalf("应回填库内最新 token，实际 access=%q refresh=%q", stale.AccessToken, stale.RefreshToken)
	}
}

// TestMarkAccountErrorKeepsLatestTokens 验证失败回写只改 LastError，
// 不会用请求里的旧 token 覆盖库内新 token。
func TestMarkAccountErrorKeepsLatestTokens(t *testing.T) {
	s := newRefreshTestService(t)
	ctx := context.Background()

	st := s.Settings()
	st.Accounts = []Account{{
		ID:           "u1",
		AccessToken:  "good-access",
		RefreshToken: "rotated-refresh",
		ExpiresAtMS:  time.Now().Add(time.Hour).UnixMilli(),
	}}
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	s.markAccountError(ctx, "u1", "上游返回 500")
	acc, ok := s.findAccount("u1")
	if !ok {
		t.Fatal("账号应仍存在")
	}
	if acc.AccessToken != "good-access" || acc.RefreshToken != "rotated-refresh" {
		t.Fatalf("LastError 回写不应改动 token，实际 access=%q refresh=%q", acc.AccessToken, acc.RefreshToken)
	}
	if acc.LastError != "上游返回 500" {
		t.Fatalf("LastError = %q", acc.LastError)
	}
}
