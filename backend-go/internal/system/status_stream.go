package system

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const statusHeartbeatInterval = 5 * time.Second

type statusHub struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func newStatusHub() *statusHub {
	return &statusHub{subscribers: make(map[chan struct{}]struct{})}
}

func (h *statusHub) subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		h.mu.Unlock()
	}
}

func (h *statusHub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// runStatusBroadcastLoop 定期向所有订阅的中断推送心跳，既是运行状态信号也是 SSE 保活。
func (s *Service) runStatusBroadcastLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(statusHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.statusHub.broadcast()
		case <-s.stopChan:
			return
		}
	}
}

// serveStatusStream 以 SSE 推送系统运行状态心跳。后端停止时连接断开，
// 前端据此标记离线；后端恢复后浏览器自动重连并恢复在线。
func (s *Service) serveStatusStream(w http.ResponseWriter, r *http.Request) {
	ch, unsubscribe := s.statusHub.subscribe()
	defer unsubscribe()

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

	writeHeartbeat := func() {
		payload, err := json.Marshal(map[string]interface{}{
			"status": "ok",
			"ts":     time.Now().UnixMilli(),
		})
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", payload)
		renewDeadline()
		flusher.Flush()
	}

	writeHeartbeat()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			writeHeartbeat()
		}
	}
}