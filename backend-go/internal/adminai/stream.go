package adminai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// serveSSE 以 SSE 协议消费 RunLoop 下推的事件通道，每 15 秒发送心跳防断连。
// 注意：http.Server 的 WriteTimeout（60s）会掐断长连接 SSE 流，这里用
// ResponseController 在每次心跳/写事件时续期 write deadline（与 openai
// 流式代理相同的手法），保证多轮工具调用（可长达数分钟）不中断。
func serveSSE(w http.ResponseWriter, r *http.Request, eventCh <-chan SSEEvent) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rc := http.NewResponseController(w)
	renewDeadline := func() {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))
	}

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-pingTicker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			renewDeadline()
			flusher.Flush()
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload)
			renewDeadline()
			flusher.Flush()
		}
	}
}
