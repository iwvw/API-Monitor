package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sync"
	"time"
)

// cachedSession 是管理员 session 鉴权结果的缓存值。
//
// 只缓存「会话有效」这一个判定结果（加上会话自身的 expires_at 约束）。
// 无效会话（不存在 / 已撤销 / 已置 is_active=0）一律不缓存：否则「刚撤销」的
// 会话会在 TTL 内继续通过。
//
// 为什么用指针 + 独立锁：命中时只原地更新节流时间戳，不重新 Set 条目，
// 避免命中把过期时间无限滚动延长；并发命中需要自己的一把锁。
type cachedSession struct {
	sessionID string

	mu        sync.Mutex
	lastTouch time.Time
	hasTouch  bool
}

// sessionCacheKey 用 sid 的完整 sha256 摘要作缓存键，避免明文 sid 留在内存里。
//
// 与 sessionFingerprint 不同：后者只取 8 字节用于前端展示，这里必须用完整摘要，
// 否则摘要碰撞会让不同 sid 共享缓存条目。
func sessionCacheKey(sid string) string {
	sum := sha256.Sum256([]byte(sid))
	return hex.EncodeToString(sum[:])
}

// lookupCachedSession 命中缓存时返回 (true, true)。
// 第一个返回值是「是否命中」，第二个是「会话是否有效」——命中即有效，
// 保留两个返回值只是为了让调用点与未命中路径形状一致。
func (s *Service) lookupCachedSession(ctx context.Context, db *sql.DB, sid string) (sessionRecord, bool) {
	if !s.sessionCache.Enabled() {
		return sessionRecord{}, false
	}
	entry, ok := s.sessionCache.Get(sessionCacheKey(sid))
	if !ok {
		return sessionRecord{}, false
	}
	cached := entry.Value
	if cached == nil {
		return sessionRecord{}, false
	}
	s.touchCachedSession(ctx, db, cached)
	// 调用方只关心「有效」这一判定（IsAuthenticated / requireSession），
	// 需要完整记录的分支（列表展示）本来就直接查库。
	return sessionRecord{ID: cached.sessionID, IsActive: 1}, true
}

// touchCachedSession 维持 last_accessed_at 的节流写，避免缓存让「最后活跃时间」
// 停止更新（会话列表按它排序，停更会显示错误的活跃时间）。
func (s *Service) touchCachedSession(ctx context.Context, db *sql.DB, cached *cachedSession) {
	now := time.Now().UTC()
	cached.mu.Lock()
	shouldTouch := !cached.hasTouch || now.Sub(cached.lastTouch) >= sessionTouchInterval
	if shouldTouch {
		cached.lastTouch, cached.hasTouch = now, true
	}
	cached.mu.Unlock()
	if shouldTouch {
		_, _ = db.ExecContext(ctx, `UPDATE sessions SET last_accessed_at = ? WHERE session_id = ?`, formatTime(now), cached.sessionID)
	}
}

// storeCachedSession 在会话校验通过后回填缓存。
func (s *Service) storeCachedSession(session sessionRecord) {
	if !s.sessionCache.Enabled() {
		return
	}
	ttl := s.sessionCacheTTL
	if ttl <= 0 {
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	// 绝不越过会话自身有效期。
	if credentialExpiresAt, err := parseDBTime(session.ExpiresAt); err == nil {
		if credentialExpiresAt.Before(expiresAt) {
			expiresAt = credentialExpiresAt
		}
	}
	if !now.Before(expiresAt) {
		return
	}
	cached := &cachedSession{sessionID: session.ID}
	if lastAccessed, err := parseDBTime(session.LastAccessedAt); err == nil {
		cached.lastTouch, cached.hasTouch = lastAccessed, true
	}
	s.sessionCache.Set(sessionCacheKey(session.ID), cached, expiresAt)
}

// invalidateSessionCache 删除单个 sid 的缓存（登出 / 撤销单个会话 /
// validateSession 内把 is_active 置 0 的分支）。
func (s *Service) invalidateSessionCache(sid string) {
	if !s.sessionCache.Enabled() || sid == "" {
		return
	}
	s.sessionCache.Delete(sessionCacheKey(sid))
}

// invalidateAllSessionCache 清空全部 session 缓存
// （修改管理员密码 / 撤销全部会话）。
func (s *Service) invalidateAllSessionCache() {
	if !s.sessionCache.Enabled() {
		return
	}
	s.sessionCache.Clear()
}

// sessionTouchInterval 与未命中路径保持一致：60s 粒度。
const sessionTouchInterval = time.Minute
