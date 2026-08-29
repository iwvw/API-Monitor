package client

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestHTTPCloakProxyURLNormalizesSocks5h(t *testing.T) {
	got := httpCloakProxyURL(config.Proxy{
		Type:     "socks5h",
		Host:     "127.0.0.1",
		Port:     1080,
		Username: "user",
		Password: "pass",
	})
	want := "socks5://user:pass@127.0.0.1:1080"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
