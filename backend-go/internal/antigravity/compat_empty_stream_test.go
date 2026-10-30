package antigravity

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// 复现「上游 SSE 完全没有任何可解析数据」时 pipeOpenAIStream 的行为。
//
// 现象（真实环境）：opencode 侧 assistant 消息 `finish=stop`、`content=[]`、
// `tokens.output=0`——看起来像「模型正常回了一个空回复」，实际什么都没拿到。
//
// 对照 CLIProxyAPI（internal/translator/antigravity/openai/chat-completions/
// antigravity_openai_response.go:69-92）的 `SawResponse` 守卫：上游一个可解析
// payload 都没给时，**不得**补发成功收尾，否则调用方会把空响应当成正常回复。
func TestPipeOpenAIStream_EmptyUpstreamMustNotReportSuccess(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	// 上游只给空行与非 data 行：没有任何 candidates，也没有 usage。
	body := strings.NewReader("\n\n: keepalive\n\n")

	res := s.pipeOpenAIStream(
		context.Background(),
		rec,
		body,
		&Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	out := rec.Body.String()
	if strings.Contains(out, `"finish_reason":"stop"`) {
		t.Fatalf("上游没有任何数据时不应补发 finish_reason=stop，实际输出:\n%s", out)
	}
	if res.wrote {
		t.Fatalf("没拿到任何上游内容时不应标记为已写出")
	}
	if res.err == nil {
		t.Fatalf("空上游应返回错误（供上层换号重试），实际 err 为 nil")
	}
	if !res.retryable {
		t.Fatalf("空上游应标记为可重试，实际 retryable=false")
	}
}

// 对照用例：上游确实产生了文本时必须正常收尾，确保上面的守卫不误伤。
func TestPipeOpenAIStream_NormalTextStillFinishes(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		"data: {\"response\":{\"responseId\":\"r1\",\"modelVersion\":\"gemini-3.8-flash\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}}\n\n" +
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" world\"}]},\"finishReason\":\"STOP\"}]}}\n\n",
	)

	s.pipeOpenAIStream(
		context.Background(),
		rec,
		body,
		&Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	out := rec.Body.String()
	if !strings.Contains(out, "hello") {
		t.Fatalf("应把上游文本转成 delta 输出，实际:\n%s", out)
	}
	if !strings.Contains(out, `"finish_reason":"stop"`) {
		t.Fatalf("正常结束的流应带 finish_reason=stop，实际:\n%s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Fatalf("正常流应带 [DONE] 收尾，实际:\n%s", out)
	}
}

// 复现「上游以 MALFORMED_FUNCTION_CALL 收尾」的端到端行为。
//
// 这是用户报告「总是中断」最可能的直接原因：
//  1. 模型生成的工具调用结构坏了，上游用 MALFORMED_FUNCTION_CALL 结束；
//  2. 旧实现把这个坏的 functionCall 当成正常 tool_use 发给调用方；
//  3. 调用方执行一个空参数工具、带着坏调用进入下一轮，上游继续拒绝；
//  4. 整轮反复中断，界面上没有任何错误可排查。
//
// 对照 CLIProxyAPI 的 `isAntigravityContentFilterFinishReason`：这类
// finishReason 必须转成显式错误，不能伪造出可用的工具调用。
func TestPipeOpenAIStream_MalformedToolCallMustSurfaceAsError(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		`data: {"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"shell","args":{}}}]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}}` + "\n\n",
	)

	res := s.pipeOpenAIStream(
		context.Background(),
		rec,
		body,
		&Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	out := rec.Body.String()

	// 坏调用不能被当作可用工具交给调用方。
	if strings.Contains(out, `"tool_calls"`) {
		t.Fatalf("MALFORMED_FUNCTION_CALL 不应转成 tool_calls（调用方会去执行坏调用并继续下一轮），实际:\n%s", out)
	}
	// 必须把上游的判定如实告诉调用方。
	if !strings.Contains(out, "MALFORMED_FUNCTION_CALL") {
		t.Fatalf("应把上游错误原因透出给调用方，实际:\n%s", out)
	}
	// 不能再补一条成功的 finish_reason，否则错误会被当成正常结束吞掉。
	if strings.Contains(out, `"finish_reason":"stop"`) {
		t.Fatalf("出错时不应再发 finish_reason=stop（会把错误粉饰成正常结束），实际:\n%s", out)
	}
	if res.err == nil {
		t.Fatalf("本轮应以错误结束，实际 err 为 nil")
	}
}
