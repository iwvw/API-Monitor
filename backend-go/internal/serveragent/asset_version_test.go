package serveragent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseAssetURL(t *testing.T) {
	fallback := "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-stun-linux-amd64"

	// 未解析到 tag：原样回退
	if got := releaseAssetURL(fallback); got != fallback {
		t.Fatalf("expected fallback, got %s", got)
	}

	// 解析到 tag：动态替换版本号
	res := &assetResolver{}
	res.setTag("v0.7.0")
	got := res.ReleaseAssetURL("api-monitor-stun-linux-amd64", fallback)
	want := "https://github.com/iwvw/API-Monitor/releases/download/v0.7.0/api-monitor-stun-linux-amd64"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestFetchLatestReleaseTagNoPanic(t *testing.T) {
	// 用 httptest 服务器模拟 GitHub latest release 响应，避免依赖真实网络
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.7.0"}`))
	}))
	defer srv.Close()

	tag, err := fetchReleaseTagFromURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v0.7.0" {
		t.Fatalf("expected v0.7.0, got %s", tag)
	}

	// 非 2xx 应返回错误
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	if _, err := fetchReleaseTagFromURL(context.Background(), bad.URL); err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

func TestReleaseAssetUrlHelperExtractsName(t *testing.T) {
	fallback := "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-relay-linux-arm64"
	res := &assetResolver{}
	res.setTag("v1.0.0")
	got := res.ReleaseAssetURL("api-monitor-relay-linux-arm64", fallback)
	if !strings.Contains(got, "/v1.0.0/api-monitor-relay-linux-arm64") {
		t.Fatalf("unexpected dynamic url: %s", got)
	}
}
