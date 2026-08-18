package adminai

import (
	"errors"
	"testing"
	"time"
)

// 重试判定：瞬时故障值得重试，参数/鉴权类错误不重试。
func TestLLMRetryableError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("POST http://x: 500 internal server error"), true},
		{errors.New("upstream timeout: 未收到首个数据块"), true},
		{errors.New("429 too many requests"), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("503 service unavailable"), true},
		{errors.New("400 invalid request body"), false},
		{errors.New("401 unauthorized"), false},
		{errors.New("model not found: gemini-x"), false},
		{errors.New(`Post "http://127.0.0.1:0/v1/chat/completions": dial tcp 127.0.0.1:0: connect: connection refused`), false},
		{contextDeadlineError(), false}, // DeadlineExceeded 不重试
		{nil, false},
	}
	for _, c := range cases {
		if got := llmRetryableError(c.err); got != c.want {
			t.Fatalf("llmRetryableError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func contextDeadlineError() error {
	return errors.New("context deadline exceeded")
}

// 退避序列：500ms → 1s → 2s → 4s → 8s 封顶。
func TestLLMRetryDelay(t *testing.T) {
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := llmRetryDelay(i + 1); got != w {
			t.Fatalf("llmRetryDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
}
