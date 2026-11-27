package serveragent

import "testing"

func TestNormalizeHostCandidate(t *testing.T) {
	cases := map[string]string{
		"64.181.246.5":                    "64.181.246.5",
		"64.181.246.5:61208":              "64.181.246.5",
		"[2603:c024:c021:2e00::5]:61208":  "2603:c024:c021:2e00::5",
		"2603:c024:c021:2e00::5":          "2603:c024:c021:2e00::5",
		"http://node.example.com:8080/x/y": "node.example.com",
		"":                                "",
	}
	for in, want := range cases {
		if got := normalizeHostCandidate(in); got != want {
			t.Errorf("normalizeHostCandidate(%q)=%q want %q", in, got, want)
		}
	}
}

// 存储节点公网地址选择必须优先 IPv4:实测 ARM 节点 DB host 是公网 IPv4、实时 ip 是 IPv6,
// 用 IPv6 会导致存储直传 dial network unreachable。
func TestPreferPublicHostPrefersIPv4(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"实时 IPv6 vs DB IPv4", []string{"2603:c024:c021:2e00::5", "64.181.246.5"}, "64.181.246.5"},
		{"两个 IPv4 取先", []string{"64.181.246.5", "1.2.3.4"}, "64.181.246.5"},
		{"带端口的 IPv4", []string{"", "203.0.113.9:61208"}, "203.0.113.9"},
		{"域名优先于 IPv6", []string{"node.example.com", "2001:db8::1"}, "node.example.com"},
		{"仅 IPv6", []string{"2001:db8::1"}, "2001:db8::1"},
		{"仅内网", []string{"10.0.0.1", "192.168.1.1"}, ""},
		{"空", []string{"", ""}, ""},
	}
	for _, tc := range cases {
		if got := preferPublicHost(tc.in...); got != tc.want {
			t.Errorf("%s: preferPublicHost(%v)=%q want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
