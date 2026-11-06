package workbuddy

import (
	"testing"
	"time"
)

// TestShouldKeepAlive 验证「定期续期」判定：超过空闲阈值或从未记录都要刷。
//
// 背景：国内版 refresh token 是 Keycloak offline session，除绝对寿命外还有
// ~30 天空闲超时；不主动续期就会被回收，只能重新扫码。
func TestShouldKeepAlive(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name          string
		lastRefreshAt string
		want          bool
	}{
		{"从未刷新过", "", true},
		{"刚刷新过", now.Add(-time.Hour).Format(time.RFC3339), false},
		{"5 天前刷新", now.Add(-5 * 24 * time.Hour).Format(time.RFC3339), false},
		{"正好 7 天", now.Add(-7 * 24 * time.Hour).Format(time.RFC3339), true},
		{"20 天前刷新", now.Add(-20 * 24 * time.Hour).Format(time.RFC3339), true},
		{"41 天前刷新（事故场景）", now.Add(-41 * 24 * time.Hour).Format(time.RFC3339), true},
		{"时间格式非法", "not-a-time", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldKeepAlive(Account{LastRefreshAt: c.lastRefreshAt}, now)
			if got != c.want {
				t.Errorf("shouldKeepAlive(%q) = %v, want %v", c.lastRefreshAt, got, c.want)
			}
		})
	}
}

// TestKeepAliveBelowOfflineIdle 保证定期续期间隔显著小于 offline 空闲超时（~30 天），
// 否则预防措施失效。
func TestKeepAliveBelowOfflineIdle(t *testing.T) {
	const offlineIdleTimeout = 30 * 24 * time.Hour
	if keepAliveRefreshInterval >= offlineIdleTimeout {
		t.Fatalf("keepAliveRefreshInterval(%v) 必须小于 offline idle timeout(%v)",
			keepAliveRefreshInterval, offlineIdleTimeout)
	}
}

// TestRefreshBroken 验证「refresh 已失效」的识别，用于状态列预警。
func TestRefreshBroken(t *testing.T) {
	cases := []struct {
		lastError string
		want      bool
	}{
		{"", false},
		{"刷新被上游拒绝（HTTP 401），refresh token 可能已失效", true},
		{"上游业务码 12153: refresh token is invalid", false}, // 业务码路径未带「失效」字样
		{"刷新 token 失败: context deadline exceeded", false},
	}
	for _, c := range cases {
		if got := refreshBroken(c.lastError); got != c.want {
			t.Errorf("refreshBroken(%q) = %v, want %v", c.lastError, got, c.want)
		}
	}
}
