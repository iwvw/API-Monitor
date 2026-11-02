package antigravity

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// 「上游正常收尾但零输出」必须被判成失败。
//
// 这是那两条真实记录（completion=0 / status=200 / 46~47s）的形态：
// 上游确实回了 finishReason（甚至带了 usage），但没有任何正文、没有任何
// 工具调用。旧实现照样补一条 finish_reason=stop + [DONE]，调用方就把它
// 当成「模型回了个空回复」。
//
// 对照 CLIProxyAPI 的 IsCodexTerminalEmptyIncomplete：
// 只在「明确零输出」时才判失败 —— 有任何 text / reasoning / tool 参数 delta
// 都算产出了内容。这里用同样的口径。
func TestPipeOpenAIStream_ZeroOutputWithNormalFinishMustFail(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{
			name: "只有 finishReason + usage，没有任何 part",
			line: `data: {"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2058,"candidatesTokenCount":0}}}`,
		},
		{
			name: "空 parts 数组 + finishReason",
			line: `data: {"response":{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}}`,
		},
		{
			name: "只有空字符串 text",
			line: `data: {"response":{"candidates":[{"content":{"parts":[{"text":""}]},"finishReason":"STOP"}]}}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Service{}
			rec := httptest.NewRecorder()
			res := s.pipeOpenAIStream(
				context.Background(), rec, strings.NewReader(c.line+"\n\n"), &Account{},
				&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
				&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
			)

			out := rec.Body.String()
			if strings.Contains(out, `"finish_reason":"stop"`) {
				t.Fatalf("零输出时不应补 finish_reason=stop（会被当成成功的空回复），实际:\n%s", out)
			}
			if res.err == nil {
				t.Fatalf("零输出应判为失败，实际 err=nil")
			}
			if !res.retryable {
				t.Fatalf("零输出应可重试（换号），实际 retryable=false")
			}
		})
	}
}

// 对照：只有 reasoning（思考）也算产出了内容——不能因为「没有正文」就判失败。
// 长思考的模型经常先吐一大段 thought 再给正文；若把这种判成空响应会误杀。
func TestPipeOpenAIStream_ThinkingOnlyIsNotEmpty(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"text":"let me think","thought":true}]}}]}}` + "\n\n" +
			`data: {"response":{"candidates":[{"content":{"parts":[{"text":"final answer"}]},"finishReason":"STOP"}]}}` + "\n\n",
	)

	res := s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	if res.err != nil {
		t.Fatalf("有正文的流不应报错: %v", res.err)
	}
	if !strings.Contains(rec.Body.String(), "final answer") {
		t.Fatalf("应输出正文，实际:\n%s", rec.Body.String())
	}
}

// 对照：只有工具调用、没有正文，也是有效产出（大量 agent 轮次就是纯工具调用）。
func TestPipeOpenAIStream_ToolCallOnlyIsNotEmpty(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"shell","args":{"command":"ls"}}}]},"finishReason":"STOP"}]}}` + "\n\n",
	)

	res := s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	if res.err != nil {
		t.Fatalf("纯工具调用不应报错: %v", res.err)
	}
	if !strings.Contains(rec.Body.String(), `"finish_reason":"tool_calls"`) {
		t.Fatalf("纯工具调用应以 tool_calls 收尾，实际:\n%s", rec.Body.String())
	}
}

// 对照：只有 reasoning（thinking）没有正文也没有工具调用，也算有效产出——
// 长思考模型常以 thought-only 收尾，不能被「上游未返回任何可解析内容」的
// 空响应守卫误杀而进入换号重试。
func TestPipeOpenAIStream_ThinkingOnlyStreamIsNotEmpty(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"text":"let me think","thought":true}]},"finishReason":"STOP"}]}}` + "\n\n",
	)

	res := s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	if res.err != nil {
		t.Fatalf("thinking-only 流不应判为空响应重试，实际 err: %v", res.err)
	}
}
