package antigravity

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// extractClaudeStreamUsage 必须读出 cache 字段。
//
// 背景：上游 Gemini 把「命中上下文的 token」放在 cache_read_input_tokens，
// 而 Claude 口径的 input_tokens **不含**这部分（见 stream_transformer.go
// 的 `InputTokens = PromptTokenCount - cached`）。此前只解析 input/output，
// cache 恒为 0 —— 用量面板表现为「缓存全是 0」，看着像完全没命中。
func TestExtractClaudeStreamUsage_ReadsCacheFields(t *testing.T) {
	event := []byte(`event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1551,"output_tokens":0,"cache_read_input_tokens":197514,"cache_creation_input_tokens":2048}}}

`)

	u, ok := extractClaudeStreamUsage(event)
	if !ok {
		t.Fatalf("应解析出 usage")
	}
	if u.InputTokens != 1551 {
		t.Errorf("input_tokens = %d, want 1551", u.InputTokens)
	}
	if u.CacheReadTokens != 197514 {
		t.Errorf("cache_read_input_tokens = %d, want 197514（这个字段此前被完全丢弃）", u.CacheReadTokens)
	}
	if u.CacheCreationTokens != 2048 {
		t.Errorf("cache_creation_input_tokens = %d, want 2048", u.CacheCreationTokens)
	}
}

// 只有 cache 字段、input/output 都是 0 时也必须判为「有 usage」，
// 否则这类事件会被跳过，记账仍然是 0。
func TestExtractClaudeStreamUsage_CacheOnlyStillCounts(t *testing.T) {
	event := []byte(`event: message_delta
data: {"type":"message_delta","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":999}}

`)

	u, ok := extractClaudeStreamUsage(event)
	if !ok {
		t.Fatalf("只有 cache 字段时也应算有 usage")
	}
	if u.CacheReadTokens != 999 {
		t.Errorf("cache_read_input_tokens = %d, want 999", u.CacheReadTokens)
	}
}

// 端到端：流式响应应把缓存一并透出给调用方，而不是只给 input/output。
func TestPipeOpenAIStream_PropagatesCacheUsage(t *testing.T) {
	// 用带 DB 的 service：修复后这条路径会真正调用 recordUsage（此前 cache 被丢，
	// input/output 条件也不满足时根本不会记账），空 Service{} 会在 open() 处 panic。
	s := newTestAntigravityService(t)
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":199065,"candidatesTokenCount":37,"cachedContentTokenCount":197514}}}` + "\n\n" +
			`data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":199065,"candidatesTokenCount":37,"cachedContentTokenCount":197514}}}` + "\n\n",
	)

	s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	out := rec.Body.String()
	t.Logf("OUTPUT:\n%s", out)

	if !strings.Contains(out, "197514") {
		t.Fatalf("响应里应带上 cache_read 用量（197514），实际:\n%s", out)
	}
}

// OpenAI 口径的 prompt_tokens **包含**缓存命中部分（cached_tokens 是它的子集），
// 而 Claude/Gemini 口径的 input_tokens 已经把 cache_read 减掉了。
// 若直接透出 input_tokens，调用方看到的「输入词元」只有未命中部分，
// 网关按 cached/prompt 算命中率会 >100% 被 clamp 成 100%。
func TestPipeOpenAIStream_PromptTokensIncludesCached(t *testing.T) {
	s := newTestAntigravityService(t)
	rec := httptest.NewRecorder()

	// Gemini: promptTokenCount 含缓存，candidatesTokenCount 为输出。
	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":199065,"candidatesTokenCount":37,"cachedContentTokenCount":197514}}}` + "\n\n" +
			`data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":199065,"candidatesTokenCount":37,"cachedContentTokenCount":197514}}}` + "\n\n",
	)

	s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	var lastUsage map[string]any
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "data: ")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(line), &chunk) != nil {
			continue
		}
		if u, ok := chunk["usage"].(map[string]any); ok {
			lastUsage = u
		}
	}
	if lastUsage == nil {
		t.Fatalf("收尾 chunk 应带 usage，实际 body:\n%s", rec.Body.String())
	}
	if got := lastUsage["prompt_tokens"]; got != float64(199065) {
		t.Fatalf("prompt_tokens = %v, want 199065（须含缓存命中部分；直接透出 input_tokens 会得到 1551）", got)
	}
	if got := lastUsage["total_tokens"]; got != float64(199102) {
		t.Fatalf("total_tokens = %v, want 199102（prompt 含缓存 + completion）", got)
	}
	details, _ := lastUsage["prompt_tokens_details"].(map[string]any)
	if details == nil || details["cached_tokens"] != float64(197514) {
		t.Fatalf("prompt_tokens_details.cached_tokens 应为 197514，实际 %v", lastUsage["prompt_tokens_details"])
	}
}
