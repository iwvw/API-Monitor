package posthogcode

import "testing"

func TestChatCompletionsURLValidatesGateway(t *testing.T) {
	// 合法：https 且 *.posthog.com。
	for _, u := range []string{
		"https://ai-gateway.us.posthog.com",
		"https://ai-gateway.us.posthog.com/",
		"https://ai-gateway.eu.posthog.com",
	} {
		if got := chatCompletionsURL(u); got != trimSlash(u)+"/v1/chat/completions" {
			t.Fatalf("valid gateway %q => %q", u, got)
		}
	}
	// 非法：非 https、非 *.posthog.com、非法 URL。
	for _, u := range []string{
		"http://ai-gateway.us.posthog.com",
		"https://evil.example.com",
		"https://posthog.com.evil.com",
		"https://attacker/x",
		"://bad",
		"",
		"   ",
	} {
		if got := chatCompletionsURL(u); got != "" {
			t.Fatalf("invalid gateway %q should be rejected, got %q", u, got)
		}
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func TestIsPostHogGatewayHost(t *testing.T) {
	cases := map[string]bool{
		"posthog.com":                  true,
		"ai-gateway.us.posthog.com":    true,
		"ai-gateway.eu.posthog.com":    true,
		"POSTHOG.COM":                  true,
		"evil.example.com":             false,
		"posthog.com.evil.com":         false,
		"notposthog.com":               false,
		"posthog.com.evil":             false,
		"":                             false,
	}
	for host, want := range cases {
		if got := isPostHogGatewayHost(host); got != want {
			t.Fatalf("isPostHogGatewayHost(%q) = %v, want %v", host, got, want)
		}
	}
}
