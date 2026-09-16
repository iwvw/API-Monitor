package posthogcode

import (
	"testing"
)

func TestNormalizeProxyURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"http://u:p@host:8080", "http://u:p@host:8080", false},
		{"https://host:443", "https://host:443", false},
		{"socks5://u:p@host:1080", "socks5://u:p@host:1080", false},
		{"socks://host:1080", "socks5://host:1080", false},
		{"socks5h://host:1080", "socks5://host:1080", false},
		{"host:1080", "socks5://host:1080", false},
		{"ftp://host:21", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		u, err := normalizeProxyURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%q 应报错，得到 %v", c.in, u)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q 不应报错: %v", c.in, err)
		}
		if u.String() != c.want {
			t.Fatalf("%q => %q，期望 %q", c.in, u.String(), c.want)
		}
	}
}

func TestRedactProxyHidesCredentials(t *testing.T) {
	got := redactProxy("http://user:secret@host:8080")
	if got == "" || got == "http://user:secret@host:8080" {
		t.Fatalf("应隐去凭据，得到 %q", got)
	}
	for _, leak := range []string{"user", "secret"} {
		if contains(got, leak) {
			t.Fatalf("输出泄露了 %q: %s", leak, got)
		}
	}
}

func TestRedactProxyPlainHost(t *testing.T) {
	if got := redactProxy("socks5://host:1080"); got == "" {
		t.Fatal("无凭据代理应原样展示")
	}
	if got := redactProxy(""); got != "已配置" {
		t.Fatalf("空值应返回占位，得到 %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
