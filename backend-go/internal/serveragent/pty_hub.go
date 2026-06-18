package serveragent

import "sync"

type ptyDataHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan string]struct{}
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
	h.mu.RLock()
	subs := h.subscribers[id]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return false
	}
	targets := make([]chan string, 0, len(subs))
	for ch := range subs {
		targets = append(targets, ch)
	}
	h.mu.RUnlock()

	delivered := false
	for _, ch := range targets {
		select {
		case ch <- data:
			delivered = true
		default:
		}
	}
	return delivered
}
