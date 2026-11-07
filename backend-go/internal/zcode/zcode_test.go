package zcode

import "testing"

// TestStaticModelsHasFlash 验证 glm-5.3-flash 被广告。
//
// 依据：它是**体验套餐（周末套餐）**领到后实际可用的那一档。不广告它，
// 用户领到套餐后在 /v1/models 里看不到能用的模型，会以为领取失败。
func TestStaticModelsHasFlash(t *testing.T) {
	found := false
	for _, m := range staticModels {
		if m.ID == "glm-5.3-flash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("glm-5.3-flash 必须广告（体验套餐实际可用的那一档）")
	}
}

// TestRegionBaseURLs 验证两地区推理平面基址。
func TestRegionBaseURLs(t *testing.T) {
	if got := openAIBaseFor(regionCN); got != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Errorf("国内版基址 = %q", got)
	}
	if got := openAIBaseFor(regionIntl); got != "https://api.z.ai/api/coding/paas/v4" {
		t.Errorf("国际版基址 = %q", got)
	}
	// 空区域回落国内版（存量兼容）。
	if got := openAIBaseFor(""); got != openAIBaseFor(regionCN) {
		t.Errorf("空区域应回落国内版，实际 %q", got)
	}
}

// TestUpstreamProvider 验证 OAuth/领取链路里的 provider 取值
// （这是上游命名空间，不是我们的 provider id）。
func TestUpstreamProvider(t *testing.T) {
	if got := upstreamProvider(regionCN); got != "bigmodel" {
		t.Errorf("国内版 upstream provider = %q, want bigmodel", got)
	}
	if got := upstreamProvider(regionIntl); got != "zai" {
		t.Errorf("国际版 upstream provider = %q, want zai", got)
	}
}

// TestDeriveAccountIDPrefix 验证两地区账号 id 前缀不同（避免撞 id）。
func TestDeriveAccountIDPrefix(t *testing.T) {
	cn := deriveAccountID(Account{UserID: "u1", Region: regionCN})
	intl := deriveAccountID(Account{UserID: "u1", Region: regionIntl})
	if cn == intl {
		t.Errorf("两地同 uid 必须得到不同 id，实际都是 %q", cn)
	}
	if cn != "zcode-user-u1" {
		t.Errorf("国内版 id = %q", cn)
	}
	if intl != "zcode-intl-user-u1" {
		t.Errorf("国际版 id = %q", intl)
	}
}

// TestIsCaptchaError 验证人机验证错误的识别。
func TestIsCaptchaError(t *testing.T) {
	yes := []string{"captcha required", "需要人机验证", "verify failed", "code 3001"}
	no := []string{"already claimed", "quota exceeded", ""}
	for _, m := range yes {
		if !isCaptchaError(m) {
			t.Errorf("%q 应判为需要人机验证", m)
		}
	}
	for _, m := range no {
		if isCaptchaError(m) {
			t.Errorf("%q 不应判为需要人机验证", m)
		}
	}
}
