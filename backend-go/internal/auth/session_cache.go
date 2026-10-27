package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
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

// takeTouch 判断本次命中是否需要落库 last_accessed_at，并在需要时先占位。
//
// 先占位再写库：并发命中同一会话时只有一个 goroutine 拿到 true，
// 避免同一秒内多个请求各写一次。
func (c *cachedSession) takeTouch(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasTouch && now.Sub(c.lastTouch) < sessionTouchInterval {
		return false
	}
	c.lastTouch, c.hasTouch = now, true
	return true
}

// sessionCacheKey 用 sid 的完整 sha256 摘要作缓存键，避免明文 sid 留在内存里。
//
// 与 sessionFingerprint 不同：后者只取 8 字节用于前端展示，这里必须用完整摘要，
// 否则摘要碰撞会让不同 sid 共享缓存条目。
func sessionCacheKey(sid string) string {
	sum := sha256.Sum256([]byte(sid))
	return hex.EncodeToString(sum[:])
}

// requestSID 提取请求里的 sid cookie 值。
func requestSID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("sid")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

// authenticateCachedSession 是鉴权热路径的缓存快捷分支。
//
// 返回 (valid, err, handled)：
//   - handled=false：缓存未命中，调用方应继续走 openDB + validateSession。
//   - handled=true ：已有确定结论，valid/err 直接使用。
//
// db 可以为 nil（调用方还没打开数据库）：这种情况下只有节流写到期时才临时
// 打开一次。若调用方已经打开了 db，直接复用，避免一次多余的连接开销。
func (s *Service) authenticateCachedSession(ctx context.Context, db *sql.DB, sid string) (bool, error, bool) {
	if !s.sessionCache.Enabled() {
		return false, nil, false
	}
	entry, ok := s.sessionCache.Get(sessionCacheKey(sid))
	if !ok || entry.Value == nil {
		return false, nil, false
	}
	cached := entry.Value

	if cached.takeTouch(time.Now().UTC()) {
		// 节流落库是 best-effort：last_accessed_at 只用于展示与排序。
		// 数据库暂时不可用不应让已缓存的合法会话变成 500，
		// 更不能因此拒绝请求（缓存的意义就是减少 DB 依赖）。
		ownsDB := false
		handle := db
		if handle == nil {
			opened, err := s.openDB(ctx)
			if err != nil {
				return true, nil, true
			}
			handle, ownsDB = opened, true
		}
		_, _ = handle.ExecContext(ctx, `UPDATE sessions SET last_accessed_at = ? WHERE session_id = ?`,
			formatTime(time.Now().UTC()), cached.sessionID)
		if ownsDB {
			_ = handle.Close()
		}
	}
	return true, nil, true
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
