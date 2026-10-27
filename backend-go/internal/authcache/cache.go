// Package authcache 提供鉴权结果的进程内短 TTL 缓存。
//
// 为什么存在
// ----------
// 鉴权热路径原先每个请求都查一次 SQLite：
//   - 模块 Bearer token（aiagent）：SELECT ... FROM aiagent_tokens JOIN aiagent_users
//   - 面板管理员 session（auth）：SELECT ... FROM sessions
//
// 压测显示鉴权路径比纯框架路径吞吐低 35%（14706 → 9611 RPS）。把结果缓存
// 30 秒即可把重复查询降到接近零。
//
// 设计约束（改动前请读）
// --------------------
//   - 键必须是凭据的摘要，不能是明文：内存泄漏时不能连带泄露可用凭据。
//   - 两类凭据各用一个 Cache 实例，键空间不混用。
//   - 条目的过期时间必须取 min(now+TTL, 凭据自身 expires_at)，绝不越过凭据有效期。
//   - 只缓存「通过」的结果。无效凭据每次都回源，避免攻击者用缓存探测凭据有效性。
//   - 查询本身失败（DB 错误）不缓存，避免把瞬时故障固化成一个 TTL 窗口的持续失败。
//   - 有容量上限 + 惰性清理，防止大量随机无效凭据打满内存。
//   - 撤权 / 登出 / 禁用 / 改密码必须主动 Delete/DeleteWhere，不能等 TTL 自然过期。
package authcache

import (
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultTTL 是未配置 AUTH_CACHE_TTL 时的缓存时长。
//
// 30 秒是安全与收益的折中：足以覆盖面板轮询与客户端的高频请求，又把「撤权后
// 仍被缓存放行」的最坏窗口压到 30 秒以内；真正的撤权路径都做了主动失效，
// 不依赖这个上限。
const DefaultTTL = 30 * time.Second

// DefaultMaxEntries 是单个缓存的容量上限。
//
// 1 万条足以覆盖正常使用（一个面板的管理员 session 只有几个；模块 token 按用户
// 与设备计也远达不到），同时把恶意构造无效凭据造成的内存占用限制在可控范围。
const DefaultMaxEntries = 10000

// TTL 返回配置的缓存时长。<=0 表示关闭缓存。
//
// 环境变量 AUTH_CACHE_TTL 接受 time.ParseDuration 格式（如 "30s"、"2m"）。
// 解析失败时退回默认值而不是关闭缓存：配置写错不该让性能悄悄退回改造前。
func TTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AUTH_CACHE_TTL"))
	if raw == "" {
		return DefaultTTL
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultTTL
	}
	return parsed
}

// Entry 是缓存条目。ExpiresAt 由调用方算好（已经是 min(now+TTL, 凭据有效期)）。
type Entry[V any] struct {
	Value     V
	ExpiresAt time.Time
}

// Cache 是带容量上限与惰性清理的 TTL map。零值不可用，请用 New。
//
// 并发模型：单把 sync.Mutex 保护全部字段。缓存操作只做内存读写（典型耗时
// 百纳秒级），而它替代的是一条 SQLite 查询，用 RWMutex 的收益抵不上复杂度——
// 命中的请求也要写回 last_used_at 节流状态，最终仍需要写锁。
type Cache[V any] struct {
	mu         sync.Mutex
	entries    map[string]Entry[V]
	ttl        time.Duration
	maxEntries int
	now        func() time.Time // 便于测试注入
}

// New 构造缓存。ttl <= 0 时返回 nil，表示关闭缓存（调用方据此走原路径）。
func New[V any](ttl time.Duration) *Cache[V] {
	if ttl <= 0 {
		return nil
	}
	return &Cache[V]{
		entries:    make(map[string]Entry[V]),
		ttl:        ttl,
		maxEntries: DefaultMaxEntries,
		now:        time.Now,
	}
}

// NewFromEnv 用 AUTH_CACHE_TTL 构造缓存。
func NewFromEnv[V any]() *Cache[V] {
	return New[V](TTL())
}

// Enabled 报告缓存是否启用。nil 接收者安全，便于调用方直接判空。
func (c *Cache[V]) Enabled() bool {
	return c != nil
}

// Get 返回本地命中的条目。条目的 ExpiresAt 已包含凭据自身有效期约束，
// 所以命中即代表「在 TTL 内且凭据未过期」。
func (c *Cache[V]) Get(key string) (Entry[V], bool) {
	if c == nil {
		return Entry[V]{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return Entry[V]{}, false
	}
	if !c.now().Before(entry.ExpiresAt) {
		delete(c.entries, key)
		return Entry[V]{}, false
	}
	return entry, true
}

// Set 写入条目。ExpiresAt 为空或已过期时不写入。
//
// 容量控制：写满后先清过期条目；仍然超限时按过期时间升序淘汰最早的条目。
// 不做后台 goroutine 清理——惰性清理足以控制内存，且不会在进程退出时留下泄漏。
func (c *Cache[V]) Set(key string, value V, expiresAt time.Time) {
	if c == nil {
		return
	}
	now := c.now()
	if !now.Before(expiresAt) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictLocked(now)
	}
	c.entries[key] = Entry[V]{Value: value, ExpiresAt: expiresAt}
}

// Delete 删除单个键。
func (c *Cache[V]) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// DeleteWhere 删除所有满足条件的键，返回删除数量。
//
// 用于「按用户维度整组失效」这类无法反查键的场景（键是凭据摘要，
// 撤销接口手里只有 userID / tokenID）。条目数上限 1 万，线性扫描成本可接受，
// 且这类调用只发生在撤销、禁用、删除用户等低频操作上。
func (c *Cache[V]) DeleteWhere(predicate func(key string, value V) bool) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for key, entry := range c.entries {
		if predicate(key, entry.Value) {
			delete(c.entries, key)
			removed++
		}
	}
	return removed
}

// Clear 清空缓存，用于「改密码后全部 session 失效」这类全量失效。
func (c *Cache[V]) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]Entry[V])
}

// Len 返回当前条目数（含尚未被惰性清理的过期条目），仅用于测试与观测。
func (c *Cache[V]) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// evictLocked 在持锁状态下清理过期条目；仍超限时按最早过期淘汰。
func (c *Cache[V]) evictLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
	// 清理后仍然满，说明容量里全是活条目：淘汰最早到期的一批，为新条目让位。
	// 每次腾出约 10%（至少 1 个）空间，避免每次写入都做一次全量排序。
	if len(c.entries) < c.maxEntries {
		return
	}
	free := c.maxEntries / 10
	if free < 1 {
		free = 1
	}
	target := c.maxEntries - free
	for len(c.entries) > target {
		var oldestKey string
		var oldest time.Time
		first := true
		for key, entry := range c.entries {
			if first || entry.ExpiresAt.Before(oldest) {
				oldestKey, oldest, first = key, entry.ExpiresAt, false
			}
		}
		if first {
			return
		}
		delete(c.entries, oldestKey)
	}
}
