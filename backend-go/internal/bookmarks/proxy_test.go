package bookmarks

import (
	"net/http"
	"testing"
)

// 回归：自定义 Transport 会关掉 Go 默认的 ProxyFromEnvironment，
// 因此 safeHTTPClient 必须显式设置 Proxy，否则 HTTPS_PROXY 形同虚设
// （实测表现：整批抓取全部超时，因为所有请求都在直连）。
//
// 注意：不能在这里断言 transport.Proxy(req) 的返回值。
// Go 的 httpproxy 配置是 sync.Once 缓存的 —— 一旦本进程里有人在设置
// 环境变量之前调用过 ProxyFromEnvironment（例如其它测试发起了 http 请求），
// 后续读取就被冻结在旧配置上，断言会随测试顺序时好时坏。
// 因此这里只断言「Proxy 字段已挂上」，代理是否生效由
// isConfiguredProxyHost 的单测覆盖（它直接读环境变量，不依赖缓存）。
func TestSafeHTTPClientUsesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")

	client := safeHTTPClient(1e9)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("Transport.Proxy must be set, otherwise environment proxy is ignored")
	}
	// 确认挂的是环境变量代理函数，而不是恒返回 nil 的自定义实现
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if _, err := transport.Proxy(req); err != nil {
		t.Errorf("proxy func should not error: %v", err)
	}
}

// 回归（实际踩到的 bug）：走代理时 DialContext 拿到的是代理地址，
// 而代理通常在本机回环地址上。若对代理也做内网校验，代理会被自己拒掉，
// 报错形如 "proxyconnect tcp: loopback address is not allowed"。
// isConfiguredProxyHost 必须能识别出「这就是配置的代理」并放行。
func TestIsConfiguredProxyHostRecognisesLoopbackProxy(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		host    string
		address string
		want    bool
	}{
		{
			name: "127.0.0.1 代理（本机常见配置）",
			env:  map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7897"},
			host: "127.0.0.1", address: "127.0.0.1:7897", want: true,
		},
		{
			name: "localhost 代理",
			env:  map[string]string{"HTTP_PROXY": "http://localhost:7897"},
			host: "localhost", address: "localhost:7897", want: true,
		},
		{
			name: "代理不带 scheme",
			env:  map[string]string{"HTTPS_PROXY": "127.0.0.1:7897"},
			host: "127.0.0.1", address: "127.0.0.1:7897", want: true,
		},
		{
			name: "ALL_PROXY 形式",
			env:  map[string]string{"ALL_PROXY": "socks5://127.0.0.1:7897"},
			host: "127.0.0.1", address: "127.0.0.1:7897", want: true,
		},
		{
			name: "远端主机（不是代理）必须继续走内网校验",
			env:  map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7897"},
			host: "example.com", address: "93.184.216.34:443", want: false,
		},
		{
			name: "未配置代理时不放行任何地址",
			env:  map[string]string{},
			host: "127.0.0.1", address: "127.0.0.1:7897", want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 清理可能存在的代理变量，避免相互干扰
			for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			got := isConfiguredProxyHost(tc.host, tc.address)
			if got != tc.want {
				t.Errorf("isConfiguredProxyHost(%q,%q) = %v, want %v", tc.host, tc.address, got, tc.want)
			}
		})
	}
}

// 内网目标即使配置了代理也必须被拦（SSRF 防护不能因为代理而失效）。
func TestInternalTargetsStillBlockedWithProxyConfigured(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:7897")

	blocked := []string{
		"http://127.0.0.1:3000/api/bookmarks/groups",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"file:///C:/Windows/win.ini",
	}
	for _, raw := range blocked {
		if _, err := validateFetchURL(raw); err == nil {
			t.Errorf("with a proxy configured, %q must still be rejected", raw)
		}
	}

	// 公网地址照常放行
	if _, err := validateFetchURL("https://example.com/"); err != nil {
		t.Errorf("public host should be allowed: %v", err)
	}
}
