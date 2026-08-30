package httpcompress

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"time"
)

// sseWriteDeadlineWindow 是 SSE 流式响应的写超时续期窗口：每写入一块事件
// 就重置一次，保证「持续有输出的长流（如 DeepSeek 思考 + 生成，常超过 60s）」
// 不受 http.Server 固定 WriteTimeout 的 60s 总时长硬限切断。超过该窗口仍无
// 新事件（上游停顿）才会被断，此时本就该断。
const sseWriteDeadlineWindow = 5 * time.Minute

// SSEWriteDeadline 为 text/event-stream 响应做写超时续期。
//
// Go http.Server 的 WriteTimeout 是「从请求开始到响应写完」的总时长硬限；
// 它不会因持续写入而自动顺延，除非 handler 显式调用 SetWriteDeadline。
// 面板嵌入了 DS2API 引擎，其 SSE 输出端只 Flush 不续期，因此 DeepSeek 思考
// 流在 60 秒整会被强制掐断。此中间件在每次写入 SSE 事件前用 ResponseController
// 把写 deadline 重置为「现在 + 窗口」，让持续流式的长响应不再受固定总时长限制。
// 非 SSE 响应原样透传，零开销。
func SSEWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &sseDeadlineWriter{
			ResponseWriter: w,
			rc:             http.NewResponseController(w),
		}
		next.ServeHTTP(sw, r)
	})
}

type sseDeadlineWriter struct {
	http.ResponseWriter
	rc          *http.ResponseController
	isStream    bool
	wroteHeader bool
}

func (w *sseDeadlineWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.isStream = strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream")
	w.ResponseWriter.WriteHeader(status)
}

func (w *sseDeadlineWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.isStream {
		_ = w.rc.SetWriteDeadline(time.Now().Add(sseWriteDeadlineWindow))
	}
	return w.ResponseWriter.Write(b)
}

func (w *sseDeadlineWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 支持 WebSocket 等需要接管底层连接的场景（面板 Agent/Engine.IO 等
// 长连接都经过本中间件链，缺此转发会导致 Hijack 失败）。
func (w *sseDeadlineWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap 透传底层 ResponseWriter：让 http.NewResponseController 能下钻到
// net/http 真实 connection 设置写 deadline（配合 applog/httpcompress 的
// Unwrap 链），否则封装层会挡住 SetWriteDeadline。
func (w *sseDeadlineWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
