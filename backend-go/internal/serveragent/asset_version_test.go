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

func TestFetchReleaseAssetSHA256FromSidecar(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		// 常见写法：<hash>  <filename>
		{"414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f  api-monitor-relay-linux-arm64\n", "414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f"},
		// 只有 hash
		{"414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f\n", "414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f"},
		// 大写应归一化
		{"414532FCE36AAE3CE80EAA5DE54B8CAC3BCAEFB3CA8EC2A18DD7ED350A93D26F", "414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(c.body))
		}))
		got, err := fetchReleaseAssetSHA256(context.Background(), srv.URL)
		srv.Close()
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", c.body, err)
		}
		if got != c.want {
			t.Fatalf("body=%q got=%s want=%s", c.body, got, c.want)
		}
	}

	// 非法内容（没有 64 位 hex）应报错，而不是返回空串后静默通过
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-a-digest"))
	}))
	defer bad.Close()
	if _, err := fetchReleaseAssetSHA256(context.Background(), bad.URL); err == nil {
		t.Fatal("expected error when sidecar has no digest")
	}
}

// latest release 缺该资产时，必须退回 fallback 地址 + fallback 自己的 sidecar 哈希。
func TestResolveReleaseAssetFallsBackToSidecar(t *testing.T) {
	resetReleaseAssetCacheForTest()
	sidecarSHA := "414532fce36aae3ce80eaa5de54b8cac3bcaefb3ca8ec2a18dd7ed350a93d26f"

	// 模拟 GitHub：latest 是 v2.1.6（无 relay 资产 → sidecar 404），
	// v0.6.1 的 sidecar 存在。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v0.6.1/") && strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(sidecarSHA + "  api-monitor-relay-linux-arm64\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	res := &assetResolver{}
	res.setTag("v2.1.6")

	fallback := srv.URL + "/v0.6.1/api-monitor-relay-linux-arm64"
	staleSHA := "e163a95eb5b24945a834a8ca2ffc1d2112d1449f4ff839425d9630ab6f90aa85"

	url, sha := res.resolveReleaseAsset("api-monitor-relay-linux-arm64", fallback, staleSHA)
	if url != fallback {
		t.Fatalf("expected fallback url %s, got %s", fallback, url)
	}
	if sha != sidecarSHA {
		t.Fatalf("expected sidecar sha %s, got %s（不应回退到过期的硬编码哈希）", sidecarSHA, sha)
	}
}
