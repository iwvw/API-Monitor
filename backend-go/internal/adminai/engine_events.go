package adminai

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// SSEEvent 是 RunLoop 下推给 SSE 消费方的事件，MarshalJSON 将 Fields 与 type 合并进 JSON 对象。
type SSEEvent struct {
	Type   string                 `json:"type"`
	Fields map[string]interface{} `json:"-"`
}

func (e SSEEvent) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(e.Fields)+1)
	m["type"] = e.Type
	for k, v := range e.Fields {
		m[k] = v
	}
	return json.Marshal(m)
}

func (s *Service) emit(ch chan SSEEvent, event SSEEvent) {
	// 先写入 run 级环形缓冲（断线重连重放用）并打上自增 seq，再非阻塞尝试实时下发。
	// defer recover 防异步生产者（会话标题/推理摘要）在 run 结束通道关闭后
	// 补发事件时 send-on-closed panic。
	defer func() { _ = recover() }()
	if buf := s.bufferFor(ch); buf != nil {
		buf.appendSeq(event)
	}
	select {
	case ch <- event:
	default:
	}
}

// drainRunEvents 消费 run 的事件流并回调解调器，直到收到终态（done/error）或 run 结束。
// 兼容「实时通道存在」（cron/频道同源订阅）与「已被 SSE 领走/已结束」（回退环形缓冲重放）：
//   - 实时通道存在时优先实时读取，通道由 runInference 无条件关闭，读完即返回；
//   - 通道缺失（被 streamEvents 领走）时退化为缓冲尾部轮询，终态仍可拿到，
//     避免 cron/频道等非 SSE 消费端因拿不到通道而永久挂起或误报「执行不存在」。
//
// 返回是否读取到终态事件。
func (s *Service) drainRunEvents(ctx context.Context, runID string, onEvent func(SSEEvent)) (terminal bool) {
	s.mu.Lock()
	eventCh, live := s.runs[runID]
	done := s.runDone[runID]
	_, hasBuf := s.runBuffers[runID]
	s.mu.Unlock()

	if !live && !done && !hasBuf {
		return false // run 不存在或已过保留期彻底清理，无事件可收
	}

	if live && eventCh != nil {
		for {
			select {
			case ev, ok := <-eventCh:
				if !ok {
					return false
				}
				if onEvent != nil {
					onEvent(ev)
				}
				if ev.Type == "done" || ev.Type == "error" {
					return true
				}
			case <-ctx.Done():
				return false
			}
		}
	}

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		if done {
			buf := s.bufferForRun(runID)
			if buf != nil {
				replayedTerminal := false
				buf.replayAfter(0, func(_ int64, ev SSEEvent) {
					if onEvent != nil {
						onEvent(ev)
					}
					if ev.Type == "done" || ev.Type == "error" {
						replayedTerminal = true
					}
				})
				if replayedTerminal {
					return true
				}
			}
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			s.mu.Lock()
			done = s.runDone[runID]
			s.mu.Unlock()
		}
	}
}

// runEventBuffer 是 run 级 SSE 事件环形缓冲：run 结束后事件仍可重放一段时间，
// 供断线重连的客户端补收 done/error 与工具状态事件（增量事件跳过，避免重复拼接）。
const (
	runEventBufferSize  = 4096
	runBufferRetention  = 10 * time.Minute
	runEventTypeSkipDLT = "delta"
	runEventTypeSkipREA = "reasoning"
)

type bufferedEvent struct {
	seq int64
	ev  SSEEvent
}

type runEventBuffer struct {
	mu     sync.Mutex
	events []bufferedEvent
	seq    int64
	start  int
	count  int
	done   bool
}

func newRunEventBuffer() *runEventBuffer {
	return &runEventBuffer{events: make([]bufferedEvent, runEventBufferSize)}
}

func (b *runEventBuffer) appendSeq(ev SSEEvent) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	if ev.Fields == nil {
		ev.Fields = map[string]interface{}{}
	}
	ev.Fields["__seq"] = b.seq
	if b.count < len(b.events) {
		idx := (b.start + b.count) % len(b.events)
		b.events[idx] = bufferedEvent{seq: b.seq, ev: ev}
		b.count++
		return b.seq
	}
	b.events[b.start] = bufferedEvent{seq: b.seq, ev: ev}
	b.start = (b.start + 1) % len(b.events)
	return b.seq
}

// replayAfter 按 seq 升序回调 seq > fromSeq 且非增量类型的事件；跳过 delta/reasoning
// （其内容由 DB 最终一致性兜底，重复重放会导致前端拼接重复文本）。
// 遇到 done/error（run 终态事件）时停止并返回该事件。
func (b *runEventBuffer) replayAfter(fromSeq int64, fn func(seq int64, ev SSEEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := 0; i < b.count; i++ {
		idx := (b.start + i) % len(b.events)
		item := b.events[idx]
		if item.seq <= fromSeq {
			continue
		}
		if item.ev.Type == runEventTypeSkipDLT || item.ev.Type == runEventTypeSkipREA {
			continue
		}
		if fn != nil {
			fn(item.seq, item.ev)
		}
		if item.ev.Type == "done" || item.ev.Type == "error" {
			break // run 已进入终态，其后不再有事件
		}
	}
}

func (b *runEventBuffer) markDone() {
	b.mu.Lock()
	b.done = true
	b.mu.Unlock()
}

func (s *Service) bufferFor(ch chan SSEEvent) *runEventBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chToBuf[ch]
}

func (s *Service) bufferForRun(runID string) *runEventBuffer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runBuffers[runID]
}

// setRunPhase 更新 run 的实时阶段（供会话列表 activeRun 展示：thinking/tooling）。
func (s *Service) setRunPhase(runID, phase string) {
	s.mu.Lock()
	s.runPhase[runID] = phase
	s.mu.Unlock()
}
