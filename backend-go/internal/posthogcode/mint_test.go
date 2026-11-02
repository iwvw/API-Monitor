package posthogcode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestMintGatewayTokenAtParsesResponse 铸造响应应解析出 token/gateway_url/过期时间/免费层模型。
func TestMintGatewayTokenAtParsesResponse(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("铸造应为 POST，得到 %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer pha_test" {
			t.Errorf("Authorization = %q, want Bearer pha_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":        true,
			"token":          "phe_abc",
			"expires_at":     time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
			"gateway_url":    "https://ai-gateway.us.posthog.com/",
			"plan":           "free",
			"cap_usd":        "200",
			"allowed_models": []string{"zai-org/glm-5.2", "deepseek-ai/deepseek-v4-flash-0731"},
		})
	}))
	defer api.Close()

	gt, err := mintGatewayTokenAt(t.Context(), api.URL, "pha_test")
	if err != nil {
		t.Fatalf("铸造失败: %v", err)
	}
	if gt.Token != "phe_abc" {
		t.Fatalf("token = %q", gt.Token)
	}
	if gt.GatewayURL != "https://ai-gateway.us.posthog.com" {
		t.Fatalf("gateway_url 末尾斜杠应被去除，得到 %q", gt.GatewayURL)
	}
	if gt.Plan != "free" {
		t.Fatalf("plan = %q", gt.Plan)
	}
	if len(gt.AllowedModels) != 2 {
		t.Fatalf("allowed_models 应解析出 2 个，得到 %v", gt.AllowedModels)
	}
	if !gt.ExpiresAt.After(time.Now()) {
		t.Fatalf("过期时间应晚于当前，得到 %v", gt.ExpiresAt)
	}
}

// TestMintGatewayTokenAtRefusal 非 201 应返回带 reason/detail 的错误。
func TestMintGatewayTokenAtRefusal(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"reason":  "not_rolled_out",
			"detail":  "rollout off",
		})
	}))
	defer api.Close()

	_, err := mintGatewayTokenAt(t.Context(), api.URL, "pha_test")
	if err == nil {
		t.Fatal("非 201 应报错")
	}
	me, ok := err.(*mintError)
	if !ok {
		t.Fatalf("应返回 *mintError，得到 %#v", err)
	}
	if me.status != http.StatusForbidden || me.reason != "not_rolled_out" {
		t.Fatalf("错误字段不符: %+v", me)
	}
}

// TestCachedGatewayTokenHonoursExpiry 缓存命中有效凭据、拒绝临近过期凭据。
func TestCachedGatewayTokenHonoursExpiry(t *testing.T) {
	s := &Service{mintCache: map[string]*gatewayToken{}}
	s.mintCache["fresh"] = &gatewayToken{Token: "phe_fresh", ExpiresAt: time.Now().Add(5 * time.Minute)}
	s.mintCache["stale"] = &gatewayToken{Token: "phe_stale", ExpiresAt: time.Now().Add(10 * time.Second)}

	if got := s.cachedGatewayToken("fresh"); got == nil || got.Token != "phe_fresh" {
		t.Fatalf("有效凭据应命中缓存，得到 %#v", got)
	}
	if got := s.cachedGatewayToken("stale"); got != nil {
		t.Fatalf("临近过期的凭据不应命中，得到 %#v", got)
	}
}

// TestGoModelNameStripsCfAlias Go 网关不认 @cf/ 别名，转发前应去掉。
func TestGoModelNameStripsCfAlias(t *testing.T) {
	cases := map[string]string{
		"@cf/zai-org/glm-5.2":                "zai-org/glm-5.2",
		"zai-org/glm-5.2":                    "zai-org/glm-5.2",
		"deepseek-ai/deepseek-v4-flash-0731": "deepseek-ai/deepseek-v4-flash-0731",
	}
	for in, want := range cases {
		if got := goModelName(in); got != want {
			t.Errorf("goModelName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeDisabledModelsMigratesCfAlias 旧停用名单里的 @cf/ 写法应迁移为 Go 写法并去重。
func TestNormalizeDisabledModelsMigratesCfAlias(t *testing.T) {
	got := normalizeDisabledModels([]string{"@cf/zai-org/glm-5.2", "moonshotai/kimi-k3", "zai-org/glm-5.2", ""})
	want := []string{"zai-org/glm-5.2", "moonshotai/kimi-k3"}
	if len(got) != len(want) {
		t.Fatalf("去重后长度 = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeDisabledModels = %v, want %v", got, want)
		}
	}
}

// TestIsFreeTierModelMatchesGoSpelling 免费层判定应认 Go 写法与 @cf/ 别名两种输入。
func TestIsFreeTierModelMatchesGoSpelling(t *testing.T) {
	for _, id := range []string{"zai-org/glm-5.2", "@cf/zai-org/glm-5.2", "deepseek-ai/deepseek-v4-flash-0731", "moonshotai/kimi-k3", "zai-org/glm-5.3-flash"} {
		if !isFreeTierModel(id) {
			t.Errorf("%q 应判定为免费层", id)
		}
	}
	if isFreeTierModel("claude-opus-5") {
		t.Error("claude-opus-5 不应判定为免费层")
	}
}
