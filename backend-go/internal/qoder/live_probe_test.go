package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestLiveIntlCampaignsWithFingerprint 是一次**需要真实凭据的联网探针**，
// 默认跳过：设置 QODER_LIVE_TOKEN / QODER_LIVE_REGION / （可选）指纹字段后才跑。
//
// 为什么放在测试里而不是命令行工具：验证的是「指纹是否真让上游下发签到活动」
// 这个纯协议事实，属于回归测试范畴；而它需要真实账号，不能让 CI 依赖网络。
//
// 用法（PowerShell）：
//
//	$env:QODER_LIVE_TOKEN="dt-..."
//	$env:QODER_LIVE_REGION="intl"
//	$env:QODER_LIVE_FP='{"machine_id":"...","machine_token":"...","machine_type":"...","machine_code":"...","machine_os":"x86_64_win32","hostname":"DESKTOP-X","version":"0.4.3"}'
//	go test ./internal/qoder/ -run TestLiveIntlCampaignsWithFingerprint -v
func TestLiveIntlCampaignsWithFingerprint(t *testing.T) {
	token := os.Getenv("QODER_LIVE_TOKEN")
	if token == "" {
		t.Skip("QODER_LIVE_TOKEN not set; skipping live upstream probe")
	}
	region := os.Getenv("QODER_LIVE_REGION")
	if region == "" {
		region = regionIntl
	}
	acc := Account{ID: "live-probe", AccessToken: token, Region: region}
	if raw := os.Getenv("QODER_LIVE_FP"); raw != "" {
		var fp CheckinFingerprint
		if err := json.Unmarshal([]byte(raw), &fp); err != nil {
			t.Fatalf("QODER_LIVE_FP is not valid JSON: %v", err)
		}
		acc.CheckinFingerprint = &fp
	}

	s := New(config.Config{DataDir: t.TempDir(), DBName: "probe.db"})
	var list campaignsResponse
	ident := identityForAccount(acc)
	extra := checkinExtraHeaders(&acc, ident, true)
	ctx := context.Background()
	if err := s.doBillingJSONWithHeaders(ctx, &acc, http.MethodGet, s.endpointCampaigns(acc.Region), nil, extra, &list); err != nil {
		t.Fatalf("campaigns request failed: %v", err)
	}
	targets, already := pickClaimableCampaigns(list.Campaigns)
	t.Logf("region=%s fingerprint=%v campaigns=%d claimable=%d alreadyClaimed=%v",
		acc.Region, ident.Real, len(list.Campaigns), len(targets), already)
	for _, c := range list.Campaigns {
		amount := 0.0
		if c.Benefit != nil {
			amount = c.Benefit.Amount
		}
		t.Logf("campaign key=%s action=%s status=%s window=%s..%s amount=%g",
			c.CampaignKey, c.ActionType, c.ClaimStatus,
			unixToDate(c.StartAt), unixToDate(c.EndAt), amount)
	}
	if ident.Real && len(targets) == 0 && !already {
		t.Errorf("with a real fingerprint we expected a CLAIM_BENEFIT campaign; got %d campaigns", len(list.Campaigns))
	}

	// 顺带验证余额接口：专属资源包（如 SOTA 积分）必须能解析出来。
	q, err := s.FetchQuota(ctx, acc)
	if err != nil {
		t.Fatalf("quota request failed: %v", err)
	}
	t.Logf("quota remaining=%g limit=%g used=%g unit=%s userType=%s packages=%d",
		q.Remaining, q.Limit, q.Used, q.Unit, q.UserType, len(q.Packages))
	for _, p := range q.Packages {
		t.Logf("package name=%q remaining=%g/%g expires=%s", p.Name, p.Remaining, p.Total, unixToDate(p.ExpiresAt))
	}
}
