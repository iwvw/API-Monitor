package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestApplySecurityHeadersHSTS(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		tls        bool
		remoteAddr string
		forwarded  string
		wantHSTS   bool
	}{
		{name: "production plain http still emits hsts", env: "production", remoteAddr: "203.0.113.5:1234", wantHSTS: true},
		{name: "development direct http does not emit hsts", env: "development", remoteAddr: "203.0.113.5:1234", wantHSTS: false},
		{name: "development tls conn emits hsts", env: "development", tls: true, remoteAddr: "203.0.113.5:1234", wantHSTS: true},
		{name: "development trusted proxy https emits hsts", env: "development", remoteAddr: "127.0.0.1:1234", forwarded: "https", wantHSTS: true},
		{name: "development trusted proxy http does not emit hsts", env: "development", remoteAddr: "127.0.0.1:1234", forwarded: "http", wantHSTS: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{cfg: config.Config{Environment: tc.env, TrustedProxyCIDRs: []string{"127.0.0.1"}}}
			req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", tc.forwarded)
			}
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()

			server.applySecurityHeaders(rec, req)

			got := rec.Header().Get("Strict-Transport-Security")
			if tc.wantHSTS && got == "" {
				t.Fatal("expected Strict-Transport-Security header")
			}
			if !tc.wantHSTS && got != "" {
				t.Fatalf("unexpected Strict-Transport-Security header: %q", got)
			}
		})
	}
}
