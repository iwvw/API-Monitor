package adminai

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestApplyInternalGatewayAuthSetsToken 有内部令牌时,发往本机网关的请求必须带上
// 内部调用头,网关据此免密钥放行(回归:loopback 免密钥默认关闭后会话 401)。
func TestApplyInternalGatewayAuthSetsToken(t *testing.T) {
	t.Setenv(internalGatewayTokenEnv, "tok-123")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/chat/completions", nil)
	applyInternalGatewayAuth(req)

	if got := req.Header.Get(internalGatewayTokenHeader); got != "tok-123" {
		t.Fatalf("token header = %q, want tok-123", got)
	}
}

// TestApplyInternalGatewayAuthSkipsWhenNoToken 无令牌时不附加请求头(退化为普通
// 请求,由网关按缺失密钥处理),且不得 panic。
func TestApplyInternalGatewayAuthSkipsWhenNoToken(t *testing.T) {
	t.Setenv(internalGatewayTokenEnv, "")

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1/v1/chat/completions", nil)
	applyInternalGatewayAuth(req)

	if got := req.Header.Get(internalGatewayTokenHeader); got != "" {
		t.Fatalf("token header should be empty, got %q", got)
	}
}
