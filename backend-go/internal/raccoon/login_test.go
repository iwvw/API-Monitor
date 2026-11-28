package raccoon

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestLoginStateLifecycle 验证 state 一次性消费与过期语义。
func TestLoginStateLifecycle(t *testing.T) {
	m := newLoginManager()

	p, expiresAt, err := m.issue(time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// 上游 loopback 校验要求 ^[a-f0-9]{64}$。
	if len(p.state) != 64 {
		t.Errorf("state length = %d, want 64", len(p.state))
	}
	for _, c := range p.state {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("state contains non-hex char %q", c)
		}
	}
	if expiresAt <= time.Now().UnixMilli() {
		t.Error("expiresAt should be future")
	}
	if ok, expired := m.consume(p.state); !ok || expired {
		t.Errorf("first consume=(%v,%v) want (true,false)", ok, expired)
	}
	if ok, _ := m.consume(p.state); ok {
		t.Error("replayed state must be rejected")
	}

	m2 := newLoginManager()
	m2.now = func() time.Time { return time.Unix(0, 0) }
	p2, _, _ := m2.issue(time.Second)
	m2.now = func() time.Time { return time.Unix(10, 0) }
	if ok, expired := m2.consume(p2.state); ok || !expired {
		t.Errorf("expired consume=(%v,%v) want (false,true)", ok, expired)
	}
}

// TestParseLoginCallback 验证自定义协议回调逐项校验（拒绝前缀绕过）。
func TestParseLoginCallback(t *testing.T) {
	got, err := parseLoginCallback("office-raccoon://auth/callback?code=abc123&state=deadbeef")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Code != "abc123" || got.State != "deadbeef" {
		t.Errorf("parsed=%+v", got)
	}
	bad := []string{
		"office-raccoon://auth/callback@evil.com?code=x&state=y",
		"office-raccoon://evil/callback?code=x&state=y",
		"https://auth/callback?code=x&state=y",
		"office-raccoon://auth/other?code=x&state=y",
		"http://evil.com/auth/callback?code=x&state=y",
		"http://127.0.0.1:55060/other?code=x&state=y",
	}
	for _, raw := range bad {
		if _, err := parseLoginCallback(raw); err == nil {
			t.Errorf("expected rejection for %q", raw)
		}
	}
}

// TestParseLoopbackCallback 验证 loopback 回调（远程访问时用户从地址栏复制的形式）。
func TestParseLoopbackCallback(t *testing.T) {
	raw := "http://127.0.0.1:55060/auth/callback?code=ac_XYZ&state=" + strings.Repeat("a", 64)
	got, err := parseLoginCallback(raw)
	if err != nil {
		t.Fatalf("parse loopback: %v", err)
	}
	if got.Code != "ac_XYZ" || len(got.State) != 64 {
		t.Errorf("parsed=%+v", got)
	}
	if _, err := parseLoginCallback("http://localhost:55060/auth/callback?code=x&state=y"); err != nil {
		t.Errorf("localhost should be accepted: %v", err)
	}
}

// TestBuildAuthorizeURL 验证 loopback 分支参数与中文 appname 编码。
func TestBuildAuthorizeURL(t *testing.T) {
	// loopback 分支：必须带 desktop_callback=loopback 与 redirect。
	u, err := buildAuthorizeURL("https://xiaohuanxiong.com", loginAppName, "ab12", loopbackRedirectURL())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.HasPrefix(u, "https://xiaohuanxiong.com/authorize?") {
		t.Errorf("unexpected base: %s", u)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse built url: %v", err)
	}
	q := parsed.Query()
	if q.Get("login_source") != "desktop" || q.Get("desktop_callback") != "loopback" {
		t.Errorf("query = %v", q)
	}
	if q.Get("redirect") != "http://127.0.0.1:55060/auth/callback" {
		t.Errorf("redirect = %q", q.Get("redirect"))
	}
	if q.Get("appname") != loginAppName {
		t.Errorf("appname = %q", q.Get("appname"))
	}
	if strings.Contains(u, `\u`) {
		t.Errorf("url contains escaped unicode: %s", u)
	}

	// 非 loopback 分支：不应带 desktop_callback / redirect。
	u2, _ := buildAuthorizeURL("https://xiaohuanxiong.com", loginAppName, "ab12", "")
	q2, _ := url.Parse(u2)
	if q2.Query().Get("desktop_callback") != "" || q2.Query().Get("redirect") != "" {
		t.Errorf("non-loopback should omit callback params: %s", u2)
	}
}

// TestLoopbackRedirectMatchesUpstreamRule 与上游正则保持一致的回归保护：
// ^http://127\.0\.0\.1:[1-9][0-9]{3,4}/auth/callback$，端口 ∈ [1024,65535]。
func TestLoopbackRedirectMatchesUpstreamRule(t *testing.T) {
	u := loopbackRedirectURL()
	if u != "http://127.0.0.1:55060/auth/callback" {
		t.Fatalf("redirect = %q", u)
	}
	if loopbackPort < 1024 || loopbackPort > 65535 {
		t.Errorf("port %d out of upstream allowed range", loopbackPort)
	}
	if !strings.HasPrefix(u, "http://127.0.0.1:") || !strings.HasSuffix(u, "/auth/callback") {
		t.Errorf("redirect shape invalid: %q", u)
	}
}
