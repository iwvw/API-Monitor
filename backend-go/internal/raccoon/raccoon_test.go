package raccoon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
)

// TestParseCatalog 验证上游目录解析：平铺 categories[].models、同名去重、
// 只保留上游模型名与实时倍率。
func TestParseCatalog(t *testing.T) {
	body := `{
		"code": 0,
		"data": {
			"categories": [
				{"type": "chat", "models": [
					{"name": "sn-glm-5-3-flash", "tags": ["lite"], "billing_multiplier": 0.2, "billing_effective_multiplier": 0.2},
					{"name": "sn-sensenova-6-8-flash", "billing_multiplier": 0.5, "billing_effective_multiplier": 0}
				]},
				{"type": "chat", "models": [
					{"name": "sn-glm-5-3-flash", "billing_multiplier": 9},
					{"name": "", "display_description": "empty name dropped"},
					{"model_name": "sn-glm-5-3", "billing_multiplier": 0.75, "billing_effective_multiplier": 0.75}
				]}
			]
		}
	}`
	models, err := parseCatalog([]byte(body))
	if err != nil {
		t.Fatalf("parseCatalog error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("want 3 models, got %d: %+v", len(models), models)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	// 同名首次出现者胜。
	if m, ok := byID["sn-glm-5-3-flash"]; !ok {
		t.Fatal("missing sn-glm-5-3-flash")
	} else if eff := m.effectiveMultiplier(); eff == nil || *eff != 0.2 {
		t.Errorf("flash effective = %v, want 0.2", eff)
	}
	// 免费模型：effective=0 必须保留为 0（不能当成「未提供」）。
	if m := byID["sn-sensenova-6-8-flash"]; m.effectiveMultiplier() == nil || *m.effectiveMultiplier() != 0 {
		t.Errorf("sensenova effective = %v, want 0", m.effectiveMultiplier())
	} else if *m.BillingMultiplier != 0.5 {
		t.Errorf("sensenova standard = %v, want 0.5", *m.BillingMultiplier)
	}
	// model_name 兜底。
	if m := byID["sn-glm-5-3"]; m.effectiveMultiplier() == nil || *m.effectiveMultiplier() != 0.75 {
		t.Errorf("glm effective = %v", m.effectiveMultiplier())
	}
}

// TestParseCatalogEmpty 验证空目录返回错误（避免把空当成功写缓存）。
func TestParseCatalogEmpty(t *testing.T) {
	if _, err := parseCatalog([]byte(`{"code":0,"data":{"categories":[]}}`)); err == nil {
		t.Fatal("want error on empty catalog")
	}
}

// TestJWTExpiryAndUserID 验证不验签解析 exp（秒→毫秒）与 user_id。
func TestJWTExpiryAndUserID(t *testing.T) {
	payload, _ := json.Marshal(map[string]interface{}{
		"exp":     1791529021,
		"user_id": 7591340,
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := fmt.Sprintf("h.%s.s", encoded)

	if got := jwtExpiryMS(token); got != 1791529021000 {
		t.Errorf("jwtExpiryMS = %d, want 1791529021000", got)
	}
	if got := jwtUserID(token); got != "7591340" {
		t.Errorf("jwtUserID = %q, want 7591340", got)
	}
}

// TestTokenState 验证 token 状态判定（毫秒时间戳）。
func TestTokenState(t *testing.T) {
	if got := tokenState(Account{AccessToken: "x"}); got != "unknown" {
		t.Errorf("no expiry = %q, want unknown", got)
	}
	if got := tokenState(Account{AccessToken: "x", ExpiresAtMS: 1}); got != "expired" {
		t.Errorf("past expiry = %q, want expired", got)
	}
}

// TestStripAnyPrefix 验证最长前缀优先剥除。
func TestStripAnyPrefix(t *testing.T) {
	if got := stripAnyPrefix("raccoon-sn-glm-5-3", "r-", "raccoon-"); got != "sn-glm-5-3" {
		t.Errorf("stripAnyPrefix = %q", got)
	}
}

// TestDeriveAccountID 验证账号 ID 派生：优先 user-<uid>。
func TestDeriveAccountID(t *testing.T) {
	if got := deriveAccountID(Account{UserID: "7591340"}); got != "user-7591340" {
		t.Errorf("deriveAccountID = %q, want user-7591340", got)
	}
	if got := deriveAccountID(Account{}); len(got) != len("acct-")+8 {
		t.Errorf("random id = %q, want acct-<8hex>", got)
	}
}

// TestRewriteSSELine 验证流式帧的 model 改写与 usage 提取，非 data 行原样透传。
func TestRewriteSSELine(t *testing.T) {
	out, usage := rewriteSSELine("data: {\"model\":\"upstream-x\",\"choices\":[]}\n\n", "raccoon-x")
	var frame map[string]interface{}
	payload := out[len("data: "):]
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		t.Fatalf("rewritten frame invalid json: %v (%q)", err, out)
	}
	if frame["model"] != "raccoon-x" {
		t.Errorf("model = %v, want raccoon-x", frame["model"])
	}
	if usage != nil {
		t.Errorf("usage should be nil, got %v", usage)
	}
	if out, _ := rewriteSSELine("data: [DONE]\n", "x"); out != "data: [DONE]\n" {
		t.Errorf("DONE line altered: %q", out)
	}
	if out, _ := rewriteSSELine(": keepalive\n", "x"); out != ": keepalive\n" {
		t.Errorf("non-data line altered: %q", out)
	}
}
