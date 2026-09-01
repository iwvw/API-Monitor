package sse

import "testing"

func TestIsRateLimitMessage(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"消息发送过于频繁，请稍后重试", true},
		{"操作过于频繁", true},
		{"请求过于频繁，请稍后再试", true},
		{"Rate limit exceeded, retry later", true},
		{"Too many requests", true},
		{"", false},
		{"内容超长，请删减后再试", false},
		{"服务器内部错误", false},
		{"正常回答里提到频率统计", false},
	}
	for _, c := range cases {
		if got := IsRateLimitMessage(c.msg); got != c.want {
			t.Errorf("IsRateLimitMessage(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}
