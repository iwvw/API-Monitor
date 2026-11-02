// Package accountpick 提供模型网关各插件共享的多账号选号原语：策略归一化、
// 按原始列表下标锚定的轮询游标，以及带 TTL 与刷新节流的额度快照。
//
// 这些原语在 posthogcode 插件里已被验证，抽到共享包后供 geminicli / lobsterai /
// workbuddy / antigravity 复用，避免每个插件各写一份易错的轮询与快照逻辑。
// 本包不依赖任何插件，也不查库、不打上游。
package accountpick

import (
	"strings"
	"sync"
	"time"
)

// 选号策略取值。
const (
	// First 固定用列表首个可用账号（主备），行为最可预期。
	First = "first"
	// RoundRobin 依次轮询，请求均匀分摊。
	RoundRobin = "round-robin"
	// LeastUsed 选额度最多的账号，按实际额度拉平消耗。
	LeastUsed = "least-used"
)

// Normalize 归一化策略值，未知值回落到 First。
func Normalize(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case RoundRobin:
		return RoundRobin
	case LeastUsed:
		return LeastUsed
	default:
		return First
	}
}

// Candidate 是一个候选账号在完整列表中的身份与下标。
type Candidate struct {
	// ID 是账号标识，用于去重与额度查询。
	ID string
	// Index 是它在完整账号列表中的下标，轮询用它锚定推进位置。
	Index int
}

// Cursor 是「按原始列表下标锚定」的轮询游标。
//
// 游标记录上次选中账号在完整列表中的下标，而不是候选集下标：候选集会随冷却、
// 停用、本次已尝试而变化，若用候选集下标取模，集合缩短时游标会回绕到前面，
// 导致部分账号被反复选中、另一些被跳过。用原始下标则始终「从上次位置继续向后
// 找下一个可用者」，与候选集变化无关。
type Cursor struct {
	mu      sync.Mutex
	lastIdx int
	inited  bool
}

// Pick 从 cands（须保持列表序）中选下一个。cands 为空返回 false。
func (r *Cursor) Pick(cands []Candidate) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	chosen := cands[0]
	if r.inited {
		for _, c := range cands {
			if c.Index > r.lastIdx {
				chosen = c
				break
			}
		}
	}
	r.lastIdx = chosen.Index
	r.inited = true
	return chosen, true
}

// Best 返回 weight 最大的候选（weight 越大越优先），相同取列表序靠前者。
// weight 由调用方提供（如剩余额度）；cands 为空返回 false。
func Best(cands []Candidate, weight func(id string) float64) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	best := cands[0]
	bestW := weight(best.ID)
	for _, c := range cands[1:] {
		if w := weight(c.ID); w > bestW {
			best, bestW = c, w
		}
	}
	return best, true
}

// Snapshot 是「账号 ID → 额度」的内存快照，带整体时间戳与单账号刷新节流。
//
// 读路径不查库、不打上游，可承受每个请求一次；写路径由用量查询、后台刷新与
// 转发后异步刷新共同维护。快照整体超过 ttl 未更新时 Get 一律返回 0（视为无数据），
// 避免基于陈旧值决策。
type Snapshot struct {
	mu          sync.RWMutex
	values      map[string]float64
	at          time.Time
	refreshedAt map[string]time.Time
}

// NewSnapshot 构造空快照。
func NewSnapshot() *Snapshot {
	return &Snapshot{
		values:      map[string]float64{},
		refreshedAt: map[string]time.Time{},
	}
}

// Set 写入某账号额度并刷新整体时间戳。
func (s *Snapshot) Set(id string, value float64) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	if s.values == nil {
		s.values = map[string]float64{}
	}
	s.values[id] = value
	s.at = time.Now()
	s.mu.Unlock()
}

// Get 读取某账号额度；快照整体超过 ttl 视为失效，返回 0。
func (s *Snapshot) Get(id string, ttl time.Duration) float64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.at.IsZero() || time.Since(s.at) > ttl {
		return 0
	}
	return s.values[id]
}

// At 返回最近一次写入时刻；从未写入时为零值。
func (s *Snapshot) At() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.at
}

// ClaimRefresh 原子判定某账号是否可刷新：距上次刷新不足 minInterval 时返回 false，
// 否则记录本次刷新时刻并返回 true。用于避免并发请求同时打上游。
func (s *Snapshot) ClaimRefresh(id string, minInterval time.Duration) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshedAt == nil {
		s.refreshedAt = map[string]time.Time{}
	}
	if last, ok := s.refreshedAt[id]; ok && time.Since(last) < minInterval {
		return false
	}
	s.refreshedAt[id] = time.Now()
	return true
}

// Throttle 是一个全局节流器：Allow 在距上次放行不足 minInterval 时返回 false。
// 用于后台额度快照刷新这类「按全局周期触发、但调用方 ticker 更密」的场景，
// 避免把上游额度接口打爆。
type Throttle struct {
	mu   sync.Mutex
	last time.Time
}

// Allow 判定本次是否放行；放行时记录当前时刻。
func (t *Throttle) Allow(minInterval time.Duration) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.last.IsZero() && time.Since(t.last) < minInterval {
		return false
	}
	t.last = time.Now()
	return true
}
