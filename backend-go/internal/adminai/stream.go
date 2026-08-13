package adminai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// serveSSE 以 SSE 协议消费 RunLoop 下推的事件通道，每 15 秒发送心跳防断连。
func serveSSE(w http.ResponseWriter, r *http.Request, eventCh <-chan SSEEvent) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-pingTicker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
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
			flusher.Flush()
		}
	}
}
