package sse

import "strings"

// rateLimitKeywords 是 DeepSeek 网页端限流提示的特征文案片段（小写匹配）。
// 典型错误：HTTP 429/439 或 SSE hint error「消息发送过于频繁，请稍后重试」。
var rateLimitKeywords = []string{
	"过于频繁",
	"稍后重试",
	"过于频繁，请稍后重试",
	"rate limit",
	"too many requests",
	"too frequently",
	"频繁",
}

// IsRateLimitMessage 判断一段上游错误文案是否为限流提示。命中时调用方应
// 映射为 429 并触发账号切换重试，而不是把错误原样透传给客户端。
func IsRateLimitMessage(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	for _, kw := range rateLimitKeywords {
		if kw == "" {
			continue
		}
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
