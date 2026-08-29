package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"net/http"
	"strings"
	"testing"
)

// The target hostname must reach the SOCKS5 server unresolved, so the exit
// node performs the DNS lookup. Resolving locally would leak DNS queries for
// chat.deepseek.com from the machine running this proxy and could pin an
// account to an edge IP inconsistent with its exit node's geography.
func TestProxyDialAddressDoesNotResolveLocallyForSocks5(t *testing.T) {
	ctx := context.Background()
	lookups := 0
	resolved, err := proxyDialAddress(ctx, "socks5", "example.com:443", func(_ context.Context, _, _ string) ([]string, error) {
		lookups++
		return []string{"203.0.113.10"}, nil
	})
	if err != nil {
		t.Fatalf("proxyDialAddress returned error: %v", err)
	}
	if resolved != "example.com:443" {
		t.Fatalf("expected hostname to be passed through untouched, got %q", resolved)
	}
	if lookups != 0 {
		t.Fatalf("expected no local DNS lookup, got %d", lookups)
	}
}

func TestProxyDialAddressRejectsAddressWithoutPort(t *testing.T) {
	if _, err := proxyDialAddress(context.Background(), "socks5", "example.com", nil); err == nil {
		t.Fatal("expected an error for an address without a port")
	}
}

func TestProxyDialAddressKeepsHostnameForSocks5h(t *testing.T) {
	ctx := context.Background()
	lookups := 0
	resolved, err := proxyDialAddress(ctx, "socks5h", "example.com:443", func(_ context.Context, network, host string) ([]string, error) {
		lookups++
		return []string{"203.0.113.10"}, nil
	})
	if err != nil {
		t.Fatalf("proxyDialAddress returned error: %v", err)
	}
	if resolved != "example.com:443" {
		t.Fatalf("expected hostname preserved for remote DNS, got %q", resolved)
	}
	if lookups != 0 {
		t.Fatalf("expected no local DNS lookup for socks5h, got %d", lookups)
	}
}

func TestApplyProxyConnectivityHeadersUsesBaseHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://chat.deepseek.com/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest returned error: %v", err)
	}

	applyProxyConnectivityHeaders(req)

	for key, want := range dsprotocol.BaseHeaders {
		if got := req.Header.Get(key); got != want {
			t.Fatalf("expected header %q=%q, got %q", key, want, got)
		}
	}
}

func TestProxyConnectivityStatus(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		success    bool
		wantText   string
	}{
		{name: "ok", statusCode: 200, success: true, wantText: "HTTP 200"},
		{name: "challenge", statusCode: 403, success: true, wantText: "风控或挑战"},
		{name: "upstream error", statusCode: 502, success: false, wantText: "HTTP 502"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			success, message := proxyConnectivityStatus(tc.statusCode)
			if success != tc.success {
				t.Fatalf("expected success=%v, got %v", tc.success, success)
			}
			if message == "" || !strings.Contains(message, tc.wantText) {
				t.Fatalf("expected message to contain %q, got %q", tc.wantText, message)
			}
		})
	}
}
