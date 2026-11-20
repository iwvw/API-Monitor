package serveragent

import (
	"strings"
	"sync"
)

type ptyDataHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan string]struct{}
	published   int64
	dropped     int64
}

func newPtyDataHub() *ptyDataHub {
	return &ptyDataHub{
		subscribers: make(map[string]map[chan string]struct{}),
	}
}

func (h *ptyDataHub) Subscribe(id string) (<-chan string, func()) {
	ch := make(chan string, 128)

	h.mu.Lock()
	if h.subscribers[id] == nil {
		h.subscribers[id] = make(map[chan string]struct{})
	}
	h.subscribers[id][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if subs, ok := h.subscribers[id]; ok {
			if _, exists := subs[ch]; exists {
				delete(subs, ch)
				close(ch)
			}
			if len(subs) == 0 {
				delete(h.subscribers, id)
			}
		}
		h.mu.Unlock()
	}

	return ch, cancel
}

func (h *ptyDataHub) Publish(id string, data string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[id]
	if len(subs) == 0 {
		return false
	}

	delivered := false
	dropped := int64(0)
	for ch := range subs {
		select {
		case ch <- data:
			delivered = true
		default:
			dropped++
		}
	}
	if delivered {
		h.published++
	}
	h.dropped += dropped
	return delivered
}

func (h *ptyDataHub) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	active := 0
	queueDepths := map[string]int{}
	for id, subs := range h.subscribers {
		if !strings.HasPrefix(id, "status:") {
			active++
		}
		depth := 0
		for ch := range subs {
			depth += len(ch)
		}
		queueDepths[id] = depth
	}
	return map[string]interface{}{
		"active_count": active,
		"queue_depths": queueDepths,
		"published":    h.published,
		"dropped":      h.dropped,
	}
}
