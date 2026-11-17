package tokenharbor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildCookie 组装一个合法的测试 Cookie(base64-<json>)。
func buildCookie(t *testing.T, access, refresh string, expiresAt int64) string {
	t.Helper()
	obj := map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"expires_at":    expiresAt,
		"user":          map[string]interface{}{"id": "u-1", "email": "a@b.com"},
	}
	data, _ := json.Marshal(obj)
	return "sb-auth-auth-token.0=base64-" + base64.RawURLEncoding.EncodeToString(data) + "; th_sid=abc"
}

func TestNormalizeCookie(t *testing.T) {
	origin := buildCookie(t, "at", "rt", 1700000000)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"裸 cookie", origin, origin},
		{"带换行", origin + "\n", origin},
		{"curl -b 引号包裹", "-b '" + origin + "'", origin},
		{"-H Cookie 前缀", "-H 'Cookie: " + origin + "'", origin},
		{"识别不到", "hello=world", ""},
		{"空串", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCookie(tc.in); got != tc.want {
				t.Fatalf("normalizeCookie = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAuthSession(t *testing.T) {
	cookie := buildCookie(t, "access-xyz", "refresh-abc", 1700000000)
	sess, ok := parseAuthSession(cookie)
	if !ok {
		t.Fatal("parseAuthSession failed")
	}
	if sess.AccessToken != "access-xyz" || sess.RefreshToken != "refresh-abc" {
		t.Fatalf("unexpected session: %+v", sess)
	}
	if sess.ExpiresAtSec != 1700000000 {
		t.Fatalf("expiresAt = %d", sess.ExpiresAtSec)
	}
	if sess.Email != "a@b.com" {
		t.Fatalf("email = %q", sess.Email)
	}
	if got := refreshTokenFromCookie(cookie); got != "refresh-abc" {
		t.Fatalf("refreshTokenFromCookie = %q", got)
	}
	if got := expiresAtMS(cookie); got != 1700000000000 {
		t.Fatalf("expiresAtMS = %d", got)
	}
}

func TestNormalizeModel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", defaultModel},
		{"th-rudder:free", "th-rudder:free"},
		{"qwen3.8-flash", "alibaba/qwen3.8-flash:free"},
		{"qwen3.8-max", "alibaba/qwen3.8-max"},
		{"anthropic/claude-opus-5.5", "anthropic/claude-opus-5.5"},
		{"deepseek-v4.1-flash", "vercel-ai-gateway/deepseek/deepseek-v4.1-flash:free"},
		{"unknown-model", "unknown-model"},
	}
	for _, tc := range cases {
		if got := normalizeModel(tc.in); got != tc.want {
			t.Errorf("normalizeModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveModelFallsBackForExpiredFreeWindow(t *testing.T) {
	// qwen3.8-flash 免费窗口已在 2026-09-27 结束,应降级到其它可用免费模型。
	got := resolveModel("qwen3.8-flash")
	if got == "alibaba/qwen3.8-flash:free" {
		t.Fatalf("expected fallback from expired free model, got %q", got)
	}
	if !hasModel(got) {
		t.Fatalf("resolved model %q not in catalog", got)
	}
}

func TestMessageTextAndAttachments(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "hello "},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAAA"}},
		}},
	}
	if got := lastUserText(msgs); got != "hello " {
		t.Fatalf("lastUserText = %q", got)
	}
	atts := extractAttachments(msgs)
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	att, _ := atts[0].(map[string]interface{})
	if att["mime"] != "image/png" {
		t.Fatalf("mime = %v", att["mime"])
	}
}

func TestDeriveThreadKey(t *testing.T) {
	withUser := map[string]interface{}{"user": "thread-1"}
	if got := deriveThreadKey(withUser, "m", nil); got != "user:thread-1" {
		t.Fatalf("deriveThreadKey = %q", got)
	}
	msgs := []interface{}{map[string]interface{}{"role": "system", "content": "s"}}
	a := deriveThreadKey(map[string]interface{}{}, "m", msgs)
	b := deriveThreadKey(map[string]interface{}{}, "m", msgs)
	if a != b || !strings.HasPrefix(a, "hash:") {
		t.Fatalf("hash thread key unstable: %q / %q", a, b)
	}
}

func TestForwardJSONBridgesUpstreamSSE(t *testing.T) {
	sse := strings.Join([]string{
		"event: thinking",
		`data: {"delta":"think"}`,
		"",
		"event: chunk",
		`data: {"delta":"Hello"}`,
		"",
		"event: chunk",
		`data: {"delta":" world"}`,
		"",
		"event: done",
		`data: {}`,
		"",
	}, "\n")
	s := &Service{}
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse))}
	s.forwardJSON(rec, resp, "acct-1", "client-model")
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"Hello world"`) {
		t.Fatalf("content missing in %s", body)
	}
	if !strings.Contains(body, `"reasoning_content":"think"`) {
		t.Fatalf("reasoning missing in %s", body)
	}
	if !strings.Contains(body, `"model":"client-model"`) {
		t.Fatalf("model not rewritten in %s", body)
	}
}

func TestUpstreamUsageMapping(t *testing.T) {
	got := upstreamUsage(map[string]interface{}{"inputTokens": float64(3), "outputTokens": float64(2)})
	if got["prompt_tokens"] != int64(3) || got["completion_tokens"] != int64(2) || got["total_tokens"] != int64(5) {
		t.Fatalf("upstreamUsage = %#v", got)
	}
}

func TestForwardStreamBridgesUpstreamSSE(t *testing.T) {
	sse := strings.Join([]string{
		"event: chunk",
		`data: {"delta":"Hi"}`,
		"",
		"event: done",
		`data: {}`,
		"",
	}, "\n")
	s := &Service{}
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse))}
	s.forwardStream(context.Background(), rec, resp, "acct-1", "client-model")
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"Hi"`) {
		t.Fatalf("chunk missing in %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("[DONE] missing in %s", body)
	}
}

func TestSessionMapModelSwitchRebuilds(t *testing.T) {
	m := newSessionMap()
	m.set("k", "s1", "model-a")
	if id, ok := m.get("k", "model-a"); !ok || id != "s1" {
		t.Fatalf("expected reuse for same model, got %q %v", id, ok)
	}
	if _, ok := m.get("k", "model-b"); ok {
		t.Fatal("expected miss when model changes")
	}
}
