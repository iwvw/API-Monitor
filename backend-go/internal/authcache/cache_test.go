package authcache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// 用可控时钟测试，避免 sleep 拖慢 CI。
func newTestCache(t *testing.T, ttl time.Duration, maxEntries int) (*Cache[int], *fakeClock) {
	t.Helper()
	cache := New[int](ttl)
	if cache == nil {
		t.Fatalf("New(%v) returned nil", ttl)
	}
	if maxEntries > 0 {
		cache.maxEntries = maxEntries
	}
	clock := &fakeClock{current: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	cache.now = clock.Now
	return cache, clock
}

type fakeClock struct {
	mu      sync.Mutex
	current time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

func TestDisabledCacheIsNilAndSafe(t *testing.T) {
	for _, ttl := range []time.Duration{0, -1} {
		var cache *Cache[int] = New[int](ttl)
		if cache != nil {
			t.Fatalf("New(%v) = non-nil, want nil (cache disabled)", ttl)
		}
		// nil 接收者必须全部安全，否则调用方需要到处判空。
		if cache.Enabled() {
			t.Fatal("nil cache reports Enabled")
		}
		if _, ok := cache.Get("k"); ok {
			t.Fatal("nil cache Get hit")
		}
		cache.Set("k", 1, time.Now().Add(time.Minute))
		cache.Delete("k")
		if n := cache.DeleteWhere(func(string, int) bool { return true }); n != 0 {
			t.Fatalf("nil cache DeleteWhere = %d", n)
		}
		cache.Clear()
		if n := cache.Len(); n != 0 {
			t.Fatalf("nil cache Len = %d", n)
		}
	}
}

func TestSetThenGetWithinTTL(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)

	cache.Set("k", 42, clock.Now().Add(30*time.Second))
	entry, ok := cache.Get("k")
	if !ok {
		t.Fatal("expected hit")
	}
	if entry.Value != 42 {
		t.Fatalf("value = %d, want 42", entry.Value)
	}

	clock.Advance(29 * time.Second)
	if _, ok := cache.Get("k"); !ok {
		t.Fatal("expected hit just before expiry")
	}
}

func TestEntryExpiresAtTTLBoundary(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)

	cache.Set("k", 1, clock.Now().Add(30*time.Second))
	clock.Advance(30 * time.Second)
	if _, ok := cache.Get("k"); ok {
		t.Fatal("entry must expire exactly at ExpiresAt")
	}
	// 过期条目在 Get 时被惰性删除。
	if n := cache.Len(); n != 0 {
		t.Fatalf("Len = %d after expiry, want 0", n)
	}
}

func TestSetRejectsAlreadyExpiredEntry(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)

	// 凭据自身已过期时，调用方算出的 ExpiresAt 会 <= now，此时不应写入。
	cache.Set("k", 1, clock.Now())
	if _, ok := cache.Get("k"); ok {
		t.Fatal("expired entry must not be stored")
	}

	cache.Set("k", 1, clock.Now().Add(-time.Second))
	if _, ok := cache.Get("k"); ok {
		t.Fatal("past entry must not be stored")
	}
}

func TestDeleteRemovesSingleKey(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)
	expiry := clock.Now().Add(30 * time.Second)

	cache.Set("a", 1, expiry)
	cache.Set("b", 2, expiry)
	cache.Delete("a")

	if _, ok := cache.Get("a"); ok {
		t.Fatal("key a should be deleted")
	}
	if _, ok := cache.Get("b"); !ok {
		t.Fatal("key b should survive")
	}
}

func TestDeleteWhereRemovesUserGroup(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)
	expiry := clock.Now().Add(30 * time.Second)

	// 值编码 userID#tokenID，模拟「撤某用户全部 token」的按值失效。
	cache.Set("k1", 100, expiry) // user 100
	cache.Set("k2", 100, expiry) // user 100
	cache.Set("k3", 200, expiry) // user 200

	removed := cache.DeleteWhere(func(_ string, tokenID int) bool { return tokenID == 100 })
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if _, ok := cache.Get("k1"); ok {
		t.Fatal("k1 should be removed")
	}
	if _, ok := cache.Get("k3"); !ok {
		t.Fatal("k3 should survive")
	}
}

func TestClearRemovesEverything(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 0)
	expiry := clock.Now().Add(30 * time.Second)
	for i := 0; i < 10; i++ {
		cache.Set(fmt.Sprintf("k%d", i), i, expiry)
	}
	cache.Clear()
	if n := cache.Len(); n != 0 {
		t.Fatalf("Len = %d after Clear", n)
	}
}

func TestCapacityBoundEvictsExpiredFirst(t *testing.T) {
	cache, clock := newTestCache(t, 30*time.Second, 4)

	// 先塞 2 条短寿命 + 2 条长寿命，填满容量。
	cache.Set("short1", 1, clock.Now().Add(1*time.Second))
	cache.Set("short2", 2, clock.Now().Add(2*time.Second))
	cache.Set("long1", 3, clock.Now().Add(20*time.Second))
	cache.Set("long2", 4, clock.Now().Add(20*time.Second))

	// 让短寿命条目过期，再写入新键触发清理。
	clock.Advance(3 * time.Second)
	cache.Set("fresh", 5, clock.Now().Add(20*time.Second))

	if _, ok := cache.Get("short1"); ok {
		t.Fatal("expired short1 should have been evicted on write")
	}
	if _, ok := cache.Get("long1"); !ok {
		t.Fatal("live long1 should survive capacity cleanup")
	}
	if _, ok := cache.Get("fresh"); !ok {
		t.Fatal("freshly written key must be present")
	}
	if n := cache.Len(); n > 4 {
		t.Fatalf("Len = %d exceeds maxEntries", n)
	}
}

func TestCapacityBoundEvictsEarliestWhenAllLive(t *testing.T) {
	cache, clock := newTestCache(t, time.Hour, 5)

	// 全部条目都活着，必须按过期时间淘汰最早的，且数量不能无限增长。
	for i := 0; i < 5; i++ {
		cache.Set(fmt.Sprintf("k%d", i), i, clock.Now().Add(time.Duration(i+1)*time.Minute))
	}
	cache.Set("newest", 99, clock.Now().Add(30*time.Minute))

	if n := cache.Len(); n > 5 {
		t.Fatalf("Len = %d exceeds maxEntries 5", n)
	}
	if _, ok := cache.Get("k0"); ok {
		t.Fatal("earliest-expiring key k0 should be evicted")
	}
	if _, ok := cache.Get("newest"); !ok {
		t.Fatal("newest key must be present")
	}
}

// 恶意大量无效凭据不能打满内存：容量上限必须真的生效。
func TestCapacityBoundUnderSustainedWrites(t *testing.T) {
	cache, clock := newTestCache(t, time.Hour, 100)
	for i := 0; i < 5000; i++ {
		cache.Set(fmt.Sprintf("k%d", i), i, clock.Now().Add(time.Hour))
		if n := cache.Len(); n > 100 {
			t.Fatalf("Len = %d at iteration %d, exceeds maxEntries 100", n, i)
		}
	}
}

func TestTTLFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", DefaultTTL},
		{"45s", 45 * time.Second},
		{"2m", 2 * time.Minute},
		{"0", 0},
		{"-30s", -30 * time.Second},
		{"garbage", DefaultTTL}, // 配置写错退回默认，而不是悄悄关闭缓存
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("AUTH_CACHE_TTL", tc.env)
			if got := TTL(); got != tc.want {
				t.Fatalf("TTL(%q) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestNewFromEnvRespectsZero(t *testing.T) {
	t.Setenv("AUTH_CACHE_TTL", "0")
	if cache := NewFromEnv[int](); cache != nil {
		t.Fatal("AUTH_CACHE_TTL=0 must disable cache")
	}
	t.Setenv("AUTH_CACHE_TTL", "15s")
	cache := NewFromEnv[int]()
	if cache == nil {
		t.Fatal("AUTH_CACHE_TTL=15s must enable cache")
	}
	if cache.ttl != 15*time.Second {
		t.Fatalf("ttl = %v, want 15s", cache.ttl)
	}
}

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	cache, clock := newTestCache(t, time.Minute, 64)
	expiry := clock.Now().Add(time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := fmt.Sprintf("k%d-%d", id, j%8)
				cache.Set(key, id, expiry)
				cache.Get(key)
				if j%16 == 0 {
					cache.Delete(key)
				}
				if j%64 == 0 {
					cache.DeleteWhere(func(string, int) bool { return false })
				}
			}
		}(i)
	}
	wg.Wait()
	if n := cache.Len(); n > 64 {
		t.Fatalf("Len = %d exceeds maxEntries", n)
	}
}
