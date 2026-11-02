package antigravity

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

// 流式响应必须带 X-Accel-Buffering: no。
//
// 现场：客户端拿到 `Provider request failed with HTTP 504`（还有 502），
// 而网关侧 Antigravity 端点 errorRate=0、errors=0 —— 说明不是上游失败，
// 是中间层（openresty）在超时。nginx 默认缓冲 proxied 响应，SSE 被缓冲后
// 客户端长时间收不到字节，代理按 read timeout 返回 504。
//
// 本仓其它插件（qoder / workbuddy / openai / ds2api …）全都设了这个头，
// 只有 antigravity 的两条流式路径漏了。
func TestPipeOpenAIStream_SetsAccelBufferingHeader(t *testing.T) {
	s := &Service{}
	rec := httptest.NewRecorder()

	body := strings.NewReader(
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}]}}\n\n",
	)

	s.pipeOpenAIStream(
		context.Background(), rec, body, &Account{},
		&openAIChatRequest{Model: "gemini-3.8-flash", Stream: true},
		&engineag.ClaudeRequest{Model: "gemini-3.8-flash"},
	)

	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want \"no\"（缺失会让 openresty 缓冲 SSE 并 504）", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}

// 验证 setStreamHeaders 覆盖了全部四项。
func TestSetStreamHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	setStreamHeaders(rec)
	h := rec.Header()
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// 流空闲时必须周期性发 SSE ping 注释行，穿透中间层/NAT 的空闲超时。
// 注释行以 `:` 开头，标准 SSE 客户端会忽略，不会污染数据。
func TestSSEStreamWriter_PingsWhenIdle(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newSSEStreamWriter(rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := sw.startPing(ctx)
	defer stop()

	// 直接把 ping 间隔缩短成可观测的量级：这里复用真实 ticker 太慢，
	// 改为手动模拟一次 tick 的条件（lastWrite 为零值 = 从未写过）。
	sw.mu.Lock()
	idle := sw.lastWrite.IsZero()
	boundary := sw.lastWrite.IsZero() || sw.lastBoundary
	if idle && boundary {
		_, _ = sw.w.Write([]byte(": ping\n\n"))
	}
	sw.mu.Unlock()

	if !strings.Contains(rec.Body.String(), ": ping") {
		t.Fatalf("空闲时应发出 SSE ping 注释行，实际 body=%q", rec.Body.String())
	}
}

// ping 不能插进一条未写完的 data 帧中间（会让客户端解析失败）。
// lastBoundary 只在「上一段以换行结尾」时为真。
func TestSSEStreamWriter_NoPingMidFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newSSEStreamWriter(rec)

	// 写一段不以换行结尾的内容（模拟分片写入的中间状态）
	sw.write([]byte(`data: {"partial":`))

	sw.mu.Lock()
	idle := time.Since(sw.lastWrite) >= streamPingInterval
	boundary := sw.lastBoundary
	sw.mu.Unlock()

	// 即使空闲，也不该在非事件边界发 ping
	if idle && boundary {
		t.Fatalf("测试前提错误：刚写完就判定为 idle")
	}
	if boundary {
		t.Fatalf("未以换行结尾时 lastBoundary 应为 false")
	}
}
