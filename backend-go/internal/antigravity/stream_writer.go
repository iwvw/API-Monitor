package antigravity

import (
	"context"
	"net/http"
	"sync"
	"time"
)

/**
 * 流式响应加固：与 `internal/openai/relay_chat.go` 的 sseStreamWriter 同一范式。
 *
 * ## 为什么 Antigravity 必须补上这三样
 *
 * 现场（客户端截图）：`重试中(第2次)` + `Provider request failed with HTTP 504`，
 * 同一会话稍早还出现过 502。而网关侧数据是：
 *
 *   endpointErrorRates: Antigravity  requests=131  errors=0  errorRate=0
 *
 * 即「上游没报错、网关也没记错」，但客户端拿到了 504 —— 典型的是**中间层**
 * （openresty 反向代理）在超时，而不是上游模型失败。三个直接原因：
 *
 * 1. 缺 `X-Accel-Buffering: no`
 *    nginx/openresty 默认会缓冲 proxied 响应。SSE 一旦被缓冲，客户端在缓冲区
 *    填满/请求结束前拿不到任何字节；长时间无字节会触发代理的 read timeout，
 *    返回 504。本仓其它插件（qoder / workbuddy / openai / ds2api …）**全部**
 *    都设了这个头，只有 antigravity 的两条流式路径漏了。
 *
 * 2. 缺写超时延长
 *    `http.Server.WriteTimeout` 覆盖整个响应写入期，长对话远超该值。
 *    `SetWriteDeadline` 每次写前延长，既保住长流，又保留慢客户端保护。
 *
 * 3. 缺 SSE ping 保活
 *    上游长时间不吐字（长推理、慢工具）时，中间层认为连接空闲并断开。
 *    周期性发 `: ping` 注释行（标准 SSE 客户端会忽略）即可穿透空闲超时。
 */

// streamPingInterval 是流式响应期间的 ping 间隔。与 openai 包保持一致。
const streamPingInterval = 15 * time.Second

// streamWriteWindow 是单次写入允许的时间窗口；每次写前都会重新延长。
const streamWriteWindow = 5 * time.Minute

// sseStreamWriter 为流式写回加锁并周期性发送 SSE ping 注释行。
//
// ping 只在「流空闲超过一个间隔」且「当前处于事件边界」时才发：
// 避免上游一条 data: 帧被分多次写出时，ping 被插进 JSON 中间导致解析失败。
type sseStreamWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	// lastWrite 记录最后一次写数据的时刻；lastBoundary 表示最后一次写是否
	// 以换行结尾（SSE 事件边界）。两者都在写锁保护下更新。
	lastWrite    time.Time
	lastBoundary bool
}

func newSSEStreamWriter(w http.ResponseWriter) *sseStreamWriter {
	sw := &sseStreamWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		sw.flusher = f
	}
	return sw
}

// write 在写锁内写入、刷新，并顺延写超时。
func (sw *sseStreamWriter) write(data []byte) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
	_ = http.NewResponseController(sw.w).SetWriteDeadline(time.Now().Add(streamWriteWindow))
	_, _ = sw.w.Write(data)
	sw.lastWrite = time.Now()
	sw.lastBoundary = len(data) > 0 && data[len(data)-1] == '\n'
	if sw.flusher != nil {
		sw.flusher.Flush()
	}
}

// startPing 启动保活 goroutine，返回停止函数。
func (sw *sseStreamWriter) startPing(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(streamPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				sw.mu.Lock()
				// 空闲：从未写过数据，或距上次写已超过一个间隔。
				idle := sw.lastWrite.IsZero() || time.Since(sw.lastWrite) >= streamPingInterval
				// 边界：从未写过数据，或上一段以换行结尾。
				boundary := sw.lastWrite.IsZero() || sw.lastBoundary
				if idle && boundary {
					_, _ = sw.w.Write([]byte(": ping\n\n"))
					sw.lastWrite = time.Now()
					if sw.flusher != nil {
						sw.flusher.Flush()
					}
				}
				sw.mu.Unlock()
			}
		}
	}()
	return func() { close(done) }
}

// setStreamHeaders 统一设置流式响应头。
//
// `X-Accel-Buffering: no` 是这里最容易被忽略、后果却最严重的一条：
// 少了它，openresty 会缓冲 SSE，长时间无字节后按 read timeout 返回 504。
func setStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}
