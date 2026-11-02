package mihomo

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControllerVerified(t *testing.T) {
	// 真实 mihomo 控制面：/version 鉴权后返回合法 JSON。
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"meta":true,"version":"v1.19.29"}`))
	}))
	defer ok.Close()
	m := &Manager{apiSecret: "test-secret"}

	port := ok.Listener.Addr().(*net.TCPAddr).Port
	if !m.controllerVerified(port) {
		t.Fatal("mihomo controller with valid /version should be verified")
	}

	// 非 mihomo 服务：返回 200 但非 /version 语义。
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"redis"}`))
	}))
	defer other.Close()
	port2 := other.Listener.Addr().(*net.TCPAddr).Port
	if m.controllerVerified(port2) {
		t.Fatal("non-mihomo service must not be verified")
	}

	// 未监听端口。
	if m.controllerVerified(1) {
		t.Fatal("unreachable port must not be verified")
	}
}
