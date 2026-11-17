package tokenharbor

// 本文件实现「下游线程 ↔ 上游 session」的绑定:上游按 session 保留多轮历史,
// 因此把同一线程的请求复用同一个 session,保证上下文连续;换模型时重建。

import (
	"context"
	"sort"
	"sync"
	"time"
)

// maxSessions 是会话绑定上限(对齐上游 200 会话限制,超出时淘汰最旧)。
const maxSessions = 200

// sessionIdleTTL 空闲会话的淘汰阈值。
const sessionIdleTTL = 24 * time.Hour

type sessionBinding struct {
	upstreamID   string
	model        string
	lastActive   time.Time
	messageCount int
}

type sessionMap struct {
	mu       sync.Mutex
	bindings map[string]*sessionBinding
}

func newSessionMap() *sessionMap {
	return &sessionMap{bindings: map[string]*sessionBinding{}}
}

func (m *sessionMap) get(key, model string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bindings[key]
	if !ok || b.model != model {
		return "", false
	}
	b.lastActive = time.Now()
	b.messageCount++
	return b.upstreamID, true
}

func (m *sessionMap) set(key, upstreamID, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bindings[key]; !exists && len(m.bindings) >= maxSessions {
		m.evictOldestLocked()
	}
	m.bindings[key] = &sessionBinding{
		upstreamID: upstreamID,
		model:      model,
		lastActive: time.Now(),
	}
}

func (m *sessionMap) remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bindings, key)
}

func (m *sessionMap) evictOldestLocked() {
	oldestKey := ""
	var oldest time.Time
	for k, b := range m.bindings {
		if oldestKey == "" || b.lastActive.Before(oldest) {
			oldestKey, oldest = k, b.lastActive
		}
	}
	if oldestKey != "" {
		delete(m.bindings, oldestKey)
	}
}

// sweep 清理空闲超时的会话绑定(上游会话由上游自身按上限回收)。
func (m *sessionMap) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-sessionIdleTTL)
	for k, b := range m.bindings {
		if b.lastActive.Before(cutoff) {
			delete(m.bindings, k)
		}
	}
}

// ensureSession 复用或创建线程对应的上游会话。
func (s *Service) ensureSession(ctx context.Context, key, model, cookie string) (string, error) {
	if id, ok := s.sessions.get(key, model); ok {
		return id, nil
	}
	id, err := s.createSession(ctx, cookie, model)
	if err != nil {
		return "", err
	}
	s.sessions.set(key, id, model)
	return id, nil
}

// StartSessionSweep 定期清理空闲会话绑定;ctx 取消时退出。
func (s *Service) StartSessionSweep(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sessions.sweep()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// sortedSessionKeys 仅用于测试:按确定性顺序返回当前绑定键。
func (m *sessionMap) sortedSessionKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.bindings))
	for k := range m.bindings {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
