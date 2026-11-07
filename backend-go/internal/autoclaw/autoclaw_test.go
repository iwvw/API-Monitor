package autoclaw

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildUpstreamBodyGates 验证转发路径的四道上游闸。
func TestBuildUpstreamBodyGates(t *testing.T) {
	payload := map[string]interface{}{
		"model": "glm-5.3",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are a helpful assistant."},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
		"max_tokens": float64(1024),
		"metadata":   map[string]interface{}{"foo": "bar"},
	}
	out, err := buildUpstreamBody(payload, "glm-5.3")
	if err != nil {
		t.Fatalf("buildUpstreamBody: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 闸 1：max_tokens 抬到下限。
	if mt, _ := got["max_tokens"].(float64); int64(mt) < minMaxTokens {
		t.Errorf("max_tokens = %v，应 >= %d", got["max_tokens"], minMaxTokens)
	}
	// 闸 4：metadata 必须被删掉。
	if _, ok := got["metadata"]; ok {
		t.Error("metadata 必须删除（上游会 500 parse response failed）")
	}
	// 闸 2：首条 system 前置身份句。
	msgs, _ := got["messages"].([]interface{})
	if len(msgs) < 2 {
		t.Fatalf("messages 数量异常: %d", len(msgs))
	}
	first, _ := msgs[0].(map[string]interface{})
	content, _ := first["content"].(string)
	if !strings.HasPrefix(content, identityPrefix) {
		t.Errorf("首条 system 未前置身份句: %q", content)
	}
}

// TestNormalizeSystemPromptNoSystem 验证没有 system 消息时补一条身份句
// （否则上游回 406 空响应体）。
func TestNormalizeSystemPromptNoSystem(t *testing.T) {
	payload := []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}
	out := normalizeSystemPrompt(payload)
	if len(out) != 2 {
		t.Fatalf("应补一条 system，实际 %d 条", len(out))
	}
	first, _ := out[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Errorf("首条应为 system，实际 %v", first["role"])
	}
}

// TestRewriteBlockedIdentities 验证外来 harness 身份句被改写（大小写不敏感）。
func TestRewriteBlockedIdentities(t *testing.T) {
	cases := []string{
		"You are ZCode, an interactive coding agent.",
		"you are claude code, running in terminal.",
		"You are Codex, based on GPT-5.",
	}
	for _, in := range cases {
		got := rewriteBlockedIdentities(in)
		lower := strings.ToLower(got)
		for _, blocked := range blockedIdentities {
			if strings.Contains(lower, strings.ToLower(blocked)) {
				t.Errorf("%q 改写后仍含黑名单身份句 %q：%q", in, blocked, got)
			}
		}
	}
}

// TestUserAPIBasePerRegion 验证两地区域名。
func TestUserAPIBasePerRegion(t *testing.T) {
	if got := userAPIBaseFor(regionCN); got != "https://autoglm-acceleration-api.zhipuai.cn" {
		t.Errorf("国内版基址 = %q", got)
	}
	if got := userAPIBaseFor(regionIntl); got != "https://autoglm-api.autoglm.ai" {
		t.Errorf("国际版基址 = %q", got)
	}
	if got := llmBaseFor(regionCN); !strings.HasSuffix(got, "/autoclaw-proxy/proxy/autoclaw") {
		t.Errorf("LLM 基址 = %q", got)
	}
}

// TestSignedHeadersHasHarnessType 验证 userapi 域保留 X-Harness-Type
// （转发路径要删掉，两处口径不同）。
func TestSignedHeadersHasHarnessType(t *testing.T) {
	h := signedHeaders(Account{AccessToken: "tok", DeviceID: "dev"})
	if h["X-Harness-Type"] != "zcode" {
		t.Error("userapi 域必须保留 X-Harness-Type: zcode")
	}
	if h["X-Auth-Sign"] == "" || h["X-Tm"] == "" {
		t.Error("签名头缺失")
	}
	if h["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q", h["Authorization"])
	}
}
