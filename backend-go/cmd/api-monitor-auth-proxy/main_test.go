package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

func newProxyHarness(t *testing.T, token string) (handler http.Handler, upstreamCalls *int) {
	t.Helper()
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream-ok"))
	}))
	t.Cleanup(upstream.Close)
	u, _ := url.Parse(upstream.URL)
	proxy := httputil.NewSingleHostReverseProxy(u)
	return authHandler(proxy, token), &calls
}

func doReq(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthProxyRejectsMissingAndWrongToken(t *testing.T) {
	h, _ := newProxyHarness(t, "am_secret")
	cases := []struct {
		name string
		req  func() *http.Request
	}{
		{"no token", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/", nil); return r }},
		{"wrong bearer", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/", nil); r.Header.Set("Authorization", "Bearer nope"); return r }},
		{"wrong query", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/?token=nope", nil); return r }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doReq(t, h, c.req())
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("missing WWW-Authenticate")
			}
		})
	}
}

func TestAuthProxyAllowsValidCredentials(t *testing.T) {
	h, calls := newProxyHarness(t, "am_secret")
	cases := []struct {
		name string
		req  func() *http.Request
	}{
		{"bearer", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/foo?x=1", nil); r.Header.Set("Authorization", "Bearer am_secret"); return r }},
		{"query", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/foo?token=am_secret", nil); return r }},
		{"cookie", func() *http.Request { r, _ := http.NewRequest("GET", "http://x/foo", nil); r.AddCookie(&http.Cookie{Name: "am_token", Value: "am_secret"}); return r }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doReq(t, h, c.req())
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if rec.Body.String() != "upstream-ok" {
				t.Fatalf("body = %q", rec.Body.String())
			}
		})
	}
	if *calls != len(cases) {
		t.Fatalf("upstream calls = %d, want %d", *calls, len(cases))
	}
}
