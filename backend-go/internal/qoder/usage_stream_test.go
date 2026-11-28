package qoder

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPipeStreamReturnsUsageObject 守住流式路径的记账契约：
// pipeStreamCounted 返回的是 **usage 对象本身**（不是包着 usage 的响应）。
//
// 旧实现把它当作完整响应传给 recordUsageFromOpenAI，后者取 resp["usage"]
// 恒为 nil，于是流式调用（网关的正常路径）永远不落用量，用量页一直是空的。
func TestPipeStreamReturnsUsageObject(t *testing.T) {
	s := newTestService(t)
	inner := `{"choices":[{"delta":{"content":"hi"},"index":0}],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4}}}`
	upstream := "data: " + `{"body":` + strconvQuote(inner) + `}` + "\n\ndata: [DONE]\n\n"
	rec := httptest.NewRecorder()
	usage, err := s.pipeStreamCounted(context.Background(), rec, io.NopCloser(strings.NewReader("")), strings.NewReader(upstream), "acct-1", "dfmodel")
	if err != nil {
		t.Fatalf("pipeStreamCounted: %v", err)
	}
	if usage == nil {
		t.Fatal("stream usage should be captured")
	}
	if got := jsonInt(usage["prompt_tokens"]); got != 12 {
		t.Fatalf("prompt_tokens = %d, want 12", got)
	}
	if got := cachedTokens(usage); got != 4 {
		t.Fatalf("cached_tokens = %d, want 4", got)
	}
}

// TestRecordUsageMapFromStream 验证流式记账会落库，且模型列存对外名而非上游短 key。
func TestRecordUsageMapFromStream(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	s.recordUsageMap("acct-1", "dfmodel", map[string]interface{}{
		"prompt_tokens":         float64(12),
		"completion_tokens":     float64(3),
		"prompt_tokens_details": map[string]interface{}{"cached_tokens": float64(4)},
	})
	s.flushUsage(ctx)

	db, err := s.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var model string
	var requests, prompt, completion, cached int64
	err = db.QueryRowContext(ctx, `SELECT model, requests, prompt_tokens, completion_tokens, cached_tokens
		FROM qoder_usage_daily`).Scan(&model, &requests, &prompt, &completion, &cached)
	if err != nil {
		t.Fatalf("query usage: %v", err)
	}
	if model != "DeepSeek-Flash" {
		t.Errorf("model = %q，应存对外名 DeepSeek-Flash 而不是短 key", model)
	}
	if requests != 1 || prompt != 12 || completion != 3 || cached != 4 {
		t.Errorf("usage = (req=%d,prompt=%d,completion=%d,cached=%d)，与上报不符",
			requests, prompt, completion, cached)
	}
}

// TestRecordUsageMapIgnoresNil 保证空 usage 不会虚增调用次数。
func TestRecordUsageMapIgnoresNil(t *testing.T) {
	s := newTestService(t)
	s.recordUsageMap("acct-1", "dfmodel", nil)
	if keys := s.sortedUsageKeys(); len(keys) != 0 {
		t.Fatalf("nil usage should not be recorded, got %d keys", len(keys))
	}
}
