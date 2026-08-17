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
//
// 断线恢复：连接中断（r.Context 取消）时若 run 尚未结束，把事件通道归还到
// s.runs，让后续重连可再次订阅；resume 重连会话先按 fromSeq 重放缓冲中
// 未送达的终态/工具状态事件（delta/reasoning 增量跳过，避免文本重复拼接），
// 再接管实时事件流。eventCh 为 nil 表示 run 已结束、仅重放缓冲尾部。
func serveSSE(w http.ResponseWriter, r *http.Request, s *Service, runID string, eventCh chan SSEEvent, resume bool, fromSeq int64) {
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

	writeEvent := func(ev SSEEvent) {
		seq := int64(0)
		if v, ok := ev.Fields["__seq"].(int64); ok {
			seq = v
			delete(ev.Fields, "__seq")
		}
		payload, err := json.Marshal(ev)
		if err != nil {
			return
		}
		if seq > 0 {
			_, _ = fmt.Fprintf(w, "id: %d\n", seq)
		}
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
		renewDeadline()
		flusher.Flush()
	}

	// 断连时若 run 仍在执行，把通道交还给 s.runs 供重连订阅；
	// run 已结束则由 runInference 的 defer 统一 close/清理。
	defer func() {
		s.mu.Lock()
		if !s.runDone[runID] {
			s.runs[runID] = eventCh
		}
		s.mu.Unlock()
	}()

	if resume {
		if buf := s.bufferForRun(runID); buf != nil {
			terminalReplayed := false
			buf.replayAfter(fromSeq, func(seq int64, ev SSEEvent) {
				writeEvent(ev)
				if ev.Type == "done" || ev.Type == "error" {
					terminalReplayed = true
				}
			})
			if terminalReplayed {
				return // 终态已补发，run 不再有新事件
			}
		}
	}

	if eventCh == nil {
		return // run 已结束且缓冲无终态（已过期），前端会回退拉取消息历史
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
			writeEvent(event)
		}
	}
}