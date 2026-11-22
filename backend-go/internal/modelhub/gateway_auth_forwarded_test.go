package modelhub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// TestGatewayAuthReverseProxyExternalClientRequiresKey 同机反向代理转发的外部请求
// 不得因为直连地址是回环而被免密钥放行(回归:此前只看 RemoteAddr==127.0.0.1)。
func TestGatewayAuthReverseProxyExternalClientRequiresKey(t *testing.T) {
	s := newOpenAIService(t)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")

	if _, err := s.AuthorizeGatewayRequest(r); err != errGatewayKeyMissing {
		t.Fatalf("external request via same-host proxy must require a key, got err=%v", err)
	}
}

// TestGatewayAuthLocalBypassIsExplicitOptIn 本机免密钥默认关闭,只有显式开启开关
// 且可信代理链解析出的真实来源是回环时才放行。
func TestGatewayAuthLocalBypassIsExplicitOptIn(t *testing.T) {
	s := newOpenAIService(t)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.RemoteAddr = "127.0.0.1:54321"

	if _, err := s.AuthorizeGatewayRequest(r); err != errGatewayKeyMissing {
		t.Fatalf("local bypass must be disabled by default, got err=%v", err)
	}

	t.Setenv(gatewayLocalBypassEnv, "true")
	authorized, err := s.AuthorizeGatewayRequest(r)
	if err != nil {
		t.Fatalf("explicit local bypass should allow loopback request, got err=%v", err)
	}
	if identity := gatewayKeyFromContext(authorized.Context()); identity.ID != "internal" {
		t.Fatalf("explicit local bypass identity = %q, want internal", identity.ID)
	}
}

// TestGatewayLocalBypassStillRejectsForwardedExternal 开关开启时,经代理转发进来的
// 外部请求(真实来源非回环)仍必须带密钥。
func TestGatewayLocalBypassStillRejectsForwardedExternal(t *testing.T) {
	s := newOpenAIService(t)
	t.Setenv(gatewayLocalBypassEnv, "true")

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")

	if _, err := s.AuthorizeGatewayRequest(r); err != errGatewayKeyMissing {
		t.Fatalf("forwarded external request must require a key even with bypass on, got err=%v", err)
	}
}

// TestGatewayAuthInternalTokenGrantsLoopbackIdentity 同进程内部调用(管理 AI 会话)
// 携带进程内部随机令牌且来源为回环时,免网关密钥获得内部身份;令牌错误/缺失或
// 来源非回环仍一律要求密钥。
func TestGatewayAuthInternalTokenGrantsLoopbackIdentity(t *testing.T) {
	s := newOpenAIService(t)
	t.Setenv(internalCallTokenEnv, "tok-secret")

	internalReq := func(remoteAddr, header string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.RemoteAddr = remoteAddr
		if header != "" {
			r.Header.Set(internalCallTokenHeader, header)
		}
		return r
	}

	authorized, err := s.AuthorizeGatewayRequest(internalReq("127.0.0.1:54321", "tok-secret"))
	if err != nil {
		t.Fatalf("internal token should authorize loopback request, got err=%v", err)
	}
	if identity := gatewayKeyFromContext(authorized.Context()); identity.ID != "internal" {
		t.Fatalf("internal token identity = %q, want internal", identity.ID)
	}

	if _, err := s.AuthorizeGatewayRequest(internalReq("127.0.0.1:54321", "wrong")); err != errGatewayKeyMissing {
		t.Fatalf("wrong internal token must require a key, got err=%v", err)
	}
	if _, err := s.AuthorizeGatewayRequest(internalReq("127.0.0.1:54321", "")); err != errGatewayKeyMissing {
		t.Fatalf("missing internal token must require a key, got err=%v", err)
	}
	// 非回环来源即使带正确令牌也不放行(防令牌经反代从外部来源重放)。
	if _, err := s.AuthorizeGatewayRequest(internalReq("203.0.113.9:54321", "tok-secret")); err != errGatewayKeyMissing {
		t.Fatalf("non-loopback source must not use internal token, got err=%v", err)
	}
}

// TestRightmostForwardedClientSkipsTrustedHops 从右往左跳过受信代理跳取真实客户端,
// 不使用可被客户端伪造的最左值。
func TestRightmostForwardedClientSkipsTrustedHops(t *testing.T) {
	trusted := []string{"10.0.0.0/8"}

	got := rightmostForwardedClient("1.1.1.1, 203.0.113.9", trusted, true)
	if got == nil || got.String() != "203.0.113.9" {
		t.Fatalf("rightmost client = %v, want 203.0.113.9", got)
	}

	got = rightmostForwardedClient("203.0.113.9, 10.0.0.7", trusted, true)
	if got == nil || got.String() != "203.0.113.9" {
		t.Fatalf("rightmost client after trusted hop = %v, want 203.0.113.9", got)
	}

	if got = rightmostForwardedClient("10.0.0.5, 10.0.0.7", trusted, true); got != nil {
		t.Fatalf("all-trusted chain should return nil, got %v", got)
	}
}

// TestResolveClientIPUsesRightmostForwardedHop 直连回环为受信代理时,取 XFF 最右侧
// 非受信地址;客户端伪造的最左值被忽略。
func TestResolveClientIPUsesRightmostForwardedHop(t *testing.T) {
	s := New(config.Config{DataDir: t.TempDir(), DBName: "data.db", TrustedProxyCIDRs: []string{"127.0.0.0/8", "10.0.0.0/8"}})
	t.Cleanup(s.Shutdown)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if got := s.resolveClientIP(r); got != "203.0.113.9" {
		t.Fatalf("resolveClientIP = %q, want 203.0.113.9", got)
	}
}
