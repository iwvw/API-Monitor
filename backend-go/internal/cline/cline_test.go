package cline

import (
	"testing"
)

// TestParseCatalogByGroup 验证归池**按响应分组**判，不按 id 前缀猜。
//
// 依据：免费池里混着别家裸 id（z-ai/glm-5.3-flash），按前缀猜会把它错判进
// 订阅池 —— 而订阅池没账号时整家不广告，那条模型就彻底消失。
func TestParseCatalogByGroup(t *testing.T) {
	body := `{
		"free": [
			{"id": "cline-free/deepseek-v4.1-flash", "name": "Deepseek-v4.1-Flash"},
			{"id": "z-ai/glm-5.3-flash", "name": "glm-5.3-flash"}
		],
		"clinePass": [
			{"id": "cline-pass/glm-5.3", "name": "glm-5.3"}
		],
		"clineCloud": [
			{"id": "cline-cloud/glm-5.3", "name": "glm-5.3"}
		],
		"recommended": [
			{"id": "openai/gpt-6-astra", "name": "gpt-6-astra"}
		]
	}`
	models, err := parseCatalog([]byte(body))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	// 免费池两条都必须收，且都归 free。
	for _, id := range []string{"cline-free/deepseek-v4.1-flash", "z-ai/glm-5.3-flash"} {
		m, ok := byID[id]
		if !ok {
			t.Fatalf("免费池模型 %q 应被收录", id)
		}
		if m.Pool != "free" {
			t.Errorf("%q 归池 = %q, want free（必须按分组判，不按前缀）", id, m.Pool)
		}
	}
	if m, ok := byID["cline-pass/glm-5.3"]; !ok || m.Pool != "pass" {
		t.Errorf("订阅池模型归池错误: %+v", m)
	}
	// clineCloud 实测 403，不收录；recommended 与两池重叠，不收录。
	if _, ok := byID["cline-cloud/glm-5.3"]; ok {
		t.Error("clineCloud 实测 403，不应收录")
	}
	if _, ok := byID["openai/gpt-6-astra"]; ok {
		t.Error("recommended 与两池重叠，不应收录")
	}
}

// TestEnsureTokenPrefix 验证 workos: 前缀的补齐与剥离。
func TestEnsureTokenPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"eyJhbGci", "workos:eyJhbGci"},
		{"workos:eyJhbGci", "workos:eyJhbGci"},
		{"", ""},
		{"  workos:abc  ", "workos:abc"},
	}
	for _, c := range cases {
		if got := ensureTokenPrefix(c.in); got != c.want {
			t.Errorf("ensureTokenPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := stripTokenPrefix("workos:abc"); got != "abc" {
		t.Errorf("stripTokenPrefix = %q, want abc", got)
	}
}

// TestParseExpiresAt 验证续期响应里 expiresAt 的两种形态（ISO 字符串 / 毫秒数字）。
func TestParseExpiresAt(t *testing.T) {
	if ms := parseExpiresAt([]byte(`"2026-09-19T19:30:04Z"`)); ms <= 0 {
		t.Errorf("ISO 字符串解析失败: %d", ms)
	}
	if ms := parseExpiresAt([]byte(`1790000000000`)); ms != 1790000000000 {
		t.Errorf("毫秒数字解析错误: %d", ms)
	}
	// 秒级数字要放大到毫秒。
	if ms := parseExpiresAt([]byte(`1790000000`)); ms != 1790000000000 {
		t.Errorf("秒级数字应放大到毫秒: %d", ms)
	}
	if ms := parseExpiresAt([]byte(`null`)); ms != 0 {
		t.Errorf("null 应为 0: %d", ms)
	}
}

// TestBuildUpstreamBody 验证请求体整形：model 原样保留（含池前缀）、
// max_tokens 缺省、session_id 存在、reasoning_effort 缺省 high。
func TestBuildUpstreamBody(t *testing.T) {
	payload := map[string]interface{}{
		"model":    "cline-free/deepseek-v4.1-flash",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	out, err := buildUpstreamBody(payload, "cline-free/deepseek-v4.1-flash")
	if err != nil {
		t.Fatalf("buildUpstreamBody: %v", err)
	}
	body := string(out)
	for _, want := range []string{
		`"model":"cline-free/deepseek-v4.1-flash"`,
		`"stream":true`,
		`"max_tokens":128000`,
		`"reasoning_effort":"high"`,
		`"session_id":"sess_`,
	} {
		if !contains(body, want) {
			t.Errorf("请求体缺少 %s：%s", want, body)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
