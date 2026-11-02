package aiagent

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// cachedTokenAuth 是模块 Bearer token 鉴权结果的缓存值。
//
// 为什么带上 lastTouch：需求要求「缓存命中的请求仍然要更新 last_used_at 的
// 节流逻辑，否则面板上最后活跃时间会失真」。把上次落库时间放进缓存条目，
// 命中时就能只按 1 分钟节流写库，而不是每个请求都写。
//
// 为什么用指针 + mutex：缓存条目的过期时间必须保持不变（不能因为命中而滚动
// 延长，否则撤权失效失败时条目可能永远不过期）。所以命中时只原地更新
// lastTouch，不重新 Set 条目；并发命中需要自己的一把锁。
type cachedTokenAuth struct {
	auth authContext

	mu        sync.Mutex
	lastTouch time.Time
	hasTouch  bool
}

// tokenCacheKey 用明文 token 的 sha256 十六进制串作缓存键。
// 绝不能用明文当键：内存 dump 或日志泄露时那等同于泄露可用凭据。
func tokenCacheKey(plain string) string {
	return hashToken(plain)
}

// peekCachedToken 只读缓存，不触碰数据库。
//
// 这是鉴权热路径的入口：命中时连 store.Open 都不做。store.Open 会做
// MkdirAll + 连接池取引用 + ensureSchema + sql.OpenDB，实测量级远大于一次
// map 查找，把缓存判断放在 Open 之后等于只省了查询、没省连接开销。
func (s *Service) peekCachedToken(plain string) (*cachedTokenAuth, bool) {
	if !s.tokenAuthCache.Enabled() || plain == "" {
		return nil, false
	}
	entry, ok := s.tokenAuthCache.Get(tokenCacheKey(plain))
	if !ok || entry.Value == nil {
		return nil, false
	}
	return entry.Value, true
}

// takeTouch 判断本次命中是否需要落库 last_used_at，并在需要时先占位。
//
// 先占位再写库：并发命中同一 token 时只有一个 goroutine 拿到 true，
// 避免同一秒内多个请求各写一次。
func (c *cachedTokenAuth) takeTouch(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasTouch && now.Sub(c.lastTouch) < tokenTouchInterval {
		return false
	}
	c.lastTouch, c.hasTouch = now, true
	return true
}

// touchTokenLastUsed 落库 last_used_at / last_ip。调用方负责 db 的打开与关闭。
// 写失败不影响鉴权结果：活跃时间只用于展示。
func touchTokenLastUsed(ctx context.Context, db *sql.DB, cached *cachedTokenAuth, ip string) {
	if db == nil || cached == nil {
		return
	}
	var ipValue interface{}
	if ip != "" {
		ipValue = ip
	}
	_, _ = db.ExecContext(ctx, `UPDATE aiagent_tokens SET last_used_at = ?, last_ip = COALESCE(?, last_ip) WHERE id = ?`,
		nowRFC3339(), ipValue, cached.auth.TokenID)
}

// storeCachedToken 在鉴权成功且凭据可缓存时写入条目。DB 错误与凭据本身的问题
// （撤销/禁用/过期）都不缓存——前者会把瞬时故障固化一个 TTL，后者会让攻击者
// 能用缓存探测凭据有效性。
//
// credentialExpiresAt 为凭据自身的 expires_at（零值表示无自身有效期或不可解析，
// 此时只受 TTL 约束）。
func (s *Service) storeCachedToken(plain, tokenID, userID, username string, credentialExpiresAt time.Time, lastUsedAt sql.NullString) {
	if !s.tokenAuthCache.Enabled() {
		return
	}
	ttl := s.authCacheTTL
	if ttl <= 0 {
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	// 绝不越过凭据自身的有效期。
	if !credentialExpiresAt.IsZero() && credentialExpiresAt.Before(expiresAt) {
		expiresAt = credentialExpiresAt
	}
	if !now.Before(expiresAt) {
		return // 凭据已过期，不缓存
	}

	cached := &cachedTokenAuth{auth: authContext{UserID: userID, TokenID: tokenID, Username: username}}
	if lastUsedAt.Valid && lastUsedAt.String != "" {
		if previous, err := time.Parse(time.RFC3339, lastUsedAt.String); err == nil {
			cached.lastTouch, cached.hasTouch = previous, true
		}
	}
	s.tokenAuthCache.Set(tokenCacheKey(plain), cached, expiresAt)
}

// invalidateTokenCacheByTokenID 删除指定 token 的缓存条目（撤销单个 token）。
func (s *Service) invalidateTokenCacheByTokenID(tokenID string) {
	if !s.tokenAuthCache.Enabled() || tokenID == "" {
		return
	}
	s.tokenAuthCache.DeleteWhere(func(_ string, value *cachedTokenAuth) bool {
		return value != nil && value.auth.TokenID == tokenID
	})
}

// invalidateTokenCacheByUserID 删除某用户的全部 token 缓存条目
// （撤销全部 token / 禁用 / 删除用户 / 重置密码）。
func (s *Service) invalidateTokenCacheByUserID(userID string) {
	if !s.tokenAuthCache.Enabled() || userID == "" {
		return
	}
	s.tokenAuthCache.DeleteWhere(func(_ string, value *cachedTokenAuth) bool {
		return value != nil && value.auth.UserID == userID
	})
}
