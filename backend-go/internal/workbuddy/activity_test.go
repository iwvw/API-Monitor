package workbuddy

import "testing"

// TestActivitySupported 验证「每日活跃上报」的区域覆盖：
// 国内版与国际版都参与，办公版（企业账号）不参与。
//
// 依据：国际版官方文档《订阅政策》的「限时每日活跃奖励说明」——国际站个人版
// Free/Pro 用户当日发起有效对话即视为活跃，系统次日自动发放积分；国际版
// 没有独立的签到接口（daily-checkin 返回 10001「签到活动未开启或已过期」）。
func TestActivitySupported(t *testing.T) {
	cases := []struct {
		region string
		want   bool
	}{
		{regionCN, true},
		{regionIntl, true},
		{regionWB, false}, // 企业账号，官方明确不参与
		{"", true},        // 空区域按国内版处理（存量兼容）
	}
	for _, c := range cases {
		if got := activitySupported(Account{Region: c.region}); got != c.want {
			t.Errorf("activitySupported(%q) = %v, want %v", c.region, got, c.want)
		}
	}
}

// TestActivityModelFor 验证活跃上报申报的模型必须是该区域真实存在的模型，
// 否则上游会判为无效请求而不计入活跃。
func TestActivityModelFor(t *testing.T) {
	if id, name := activityModelFor(regionIntl); id != "deepseek-v4.1-flash" || name == "" {
		t.Errorf("intl model = (%q,%q)，应为国际版真实模型", id, name)
	}
	if id, name := activityModelFor(regionCN); id != "deepseek-v4-flash" || name == "" {
		t.Errorf("cn model = (%q,%q)", id, name)
	}
	// 空区域走国内版模型。
	if id, _ := activityModelFor(""); id != "deepseek-v4-flash" {
		t.Errorf("empty region model = %q，应回落国内版", id)
	}
}

// TestCheckinStillCNOnly 保证「签到」没有因为活跃上报的改动而被误扩展到国际版：
// 国际版 daily-checkin 实测返回 10001「签到活动未开启或已过期」。
func TestCheckinStillCNOnly(t *testing.T) {
	if checkinSupported(Account{Region: regionIntl}) {
		t.Error("国际版不应走签到接口")
	}
	if checkinSupported(Account{Region: regionWB}) {
		t.Error("办公版不应走签到接口")
	}
}
