package auth

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// newSessionCacheTestService 用指定 TTL 构造 auth 服务（TTL 在 New 时解析一次）。
func newSessionCacheTestService(t *testing.T, cacheTTL string) *Service {
	t.Helper()
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("DEMO_MODE", "")
	t.Setenv("ENCRYPTION_KEY", "")
	if cacheTTL != "" {
		t.Setenv("AUTH_CACHE_TTL", cacheTTL)
	}
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "auth_cache_test.db",
	})
}

// loginForCacheTest 设置密码并登录，返回 sid cookie。
func loginForCacheTest(t *testing.T, service *Service) *http.Cookie {
	t.Helper()
	res := performAuthRequest(service, http.MethodPost, "/api/auth/set-password", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("set-password status = %d body=%s", res.Code, res.Body.String())
	}
	res = performAuthRequest(service, http.MethodPost, "/api/auth/login", `{"password":"secret123"}`, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	cookie := findCookie(res.Result().Cookies(), "sid")
	if cookie == nil || cookie.Value == "" {
		t.Fatal("login did not set sid cookie")
	}
	return cookie
}

func isAuthenticated(t *testing.T, service *Service, cookie *http.Cookie) bool {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "/api/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	ok, err := service.IsAuthenticated(context.Background(), req)
	if err != nil {
		t.Fatalf("IsAuthenticated: %v", err)
	}
	return ok
}

// 缓存命中时不再回源：删掉 sessions 行后仍应通过，证明结果来自进程内缓存。
func TestSessionAuthCacheAvoidsDatabaseOnHit(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	if !service.sessionCache.Enabled() {
		t.Fatal("cache should be enabled for AUTH_CACHE_TTL=30s")
	}
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("first auth should pass")
	}
	if n := service.sessionCache.Len(); n != 1 {
		t.Fatalf("cache Len = %d after first auth, want 1", n)
	}

	for i := 0; i < 100; i++ {
		if !isAuthenticated(t, service, cookie) {
			t.Fatalf("auth #%d failed", i)
		}
	}
	if n := service.sessionCache.Len(); n != 1 {
		t.Fatalf("cache Len = %d after 100 requests, want 1", n)
	}

	// 绕过 service 直接删行：命中缓存时仍通过。
	ctx := context.Background()
	db, err := service.openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, cookie.Value); err != nil {
		t.Fatal(err)
	}
	if !isAuthenticated(t, service, cookie) {
		t.Fatal("cached auth must not hit the database")
	}
}

// 缓存命中时必须连 openDB 都不做。
//
// 这条守的是「缓存位置」这个具体缺陷：如果把缓存判定放在 openDB 之后，
// 每次请求仍要 MkdirAll + 取连接池引用 + sql.OpenDB，只省了 SELECT。
// 这里把 DataDir 指向不可写路径，openDB 必然失败；命中缓存仍应成功。
func TestSessionAuthCacheSkipsDatabaseOpenEntirely(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("priming auth should pass")
	}

	// openDB 内部会 os.MkdirAll(DataDir)；把 DataDir 换成已存在的普通文件，
	// MkdirAll 必然报错，从而保证任何 openDB 调用都会失败。
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	service.cfg.DataDir = blocker

	// 节流窗口内的命中不应打开数据库，因此必须仍然通过。
	if !isAuthenticated(t, service, cookie) {
		t.Fatal("cached auth must not require a database connection")
	}
}

// AUTH_CACHE_TTL=0 时行为必须与改造前一致：每次都查库。
func TestSessionAuthCacheDisabledQueriesEveryTime(t *testing.T) {
	service := newSessionCacheTestService(t, "0")
	if service.sessionCache.Enabled() {
		t.Fatal("cache must be disabled for AUTH_CACHE_TTL=0")
	}
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("first auth should pass")
	}

	ctx := context.Background()
	db, err := service.openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, cookie.Value); err != nil {
		t.Fatal(err)
	}
	if isAuthenticated(t, service, cookie) {
		t.Fatal("auth must fail immediately when cache is disabled")
	}
}

// 安全红线：登出后必须立即 401，不等待 TTL。
func TestSessionAuthCacheInvalidatedOnLogout(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth before logout should pass")
	}

	res := performAuthRequest(service, http.MethodPost, "/api/auth/logout", "", cookie)
	if res.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", res.Code, res.Body.String())
	}

	if isAuthenticated(t, service, cookie) {
		t.Fatal("auth after logout must fail immediately")
	}
}

// 安全红线：改密码后全部 session 缓存必须失效。
func TestSessionAuthCacheInvalidatedOnPasswordChange(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth before password change should pass")
	}

	res := performAuthRequest(service, http.MethodPost, "/api/auth/change-password",
		`{"oldPassword":"secret123","newPassword":"secret456"}`, cookie)
	if res.Code != http.StatusOK {
		t.Fatalf("change-password status = %d body=%s", res.Code, res.Body.String())
	}
	if n := service.sessionCache.Len(); n != 0 {
		t.Fatalf("cache Len = %d after password change, want 0", n)
	}
}

// 安全红线：撤销全部会话后缓存必须整体失效。
func TestSessionAuthCacheInvalidatedOnRevokeAll(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth before revoke-all should pass")
	}

	res := performAuthRequest(service, http.MethodPost, "/api/auth/sessions/revoke-all", "", cookie)
	if res.Code != http.StatusOK {
		t.Fatalf("revoke-all status = %d body=%s", res.Code, res.Body.String())
	}
	if isAuthenticated(t, service, cookie) {
		t.Fatal("auth after revoke-all must fail immediately")
	}
}

// 无效 sid 绝不缓存：不能用缓存探测 sid 是否存在。
func TestSessionAuthCacheDoesNotCacheInvalidSessions(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	loginForCacheTest(t, service) // 建一个合法会话，保证表非空

	bogus := &http.Cookie{Name: "sid", Value: "not-a-real-session-id"}
	for i := 0; i < 100; i++ {
		if isAuthenticated(t, service, bogus) {
			t.Fatalf("attempt #%d authenticated a bogus sid", i)
		}
	}
	if n := service.sessionCache.Len(); n != 0 {
		t.Fatalf("cache Len = %d, invalid sessions must not be cached", n)
	}
}

// 缓存键必须是完整 sha256 摘要，不能是明文 sid。
func TestSessionCacheKeyIsDigestNotPlaintext(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth should pass")
	}

	key := sessionCacheKey(cookie.Value)
	if key == cookie.Value {
		t.Fatal("cache key must not be the plaintext sid")
	}
	if len(key) != 64 {
		t.Fatalf("cache key length = %d, want 64 (sha256 hex)", len(key))
	}
	// 完整摘要（32 字节），不能退化成 sessionFingerprint 的 8 字节。
	if strings.Contains(key, sessionFingerprint(cookie.Value)) && len(key) == 16 {
		t.Fatal("cache key must not use the truncated fingerprint")
	}
}

// 会话自身 expires_at 更早时，缓存条目过期时间必须被收紧。
func TestSessionAuthCacheRespectsSessionExpiry(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth should pass")
	}

	entry, ok := service.sessionCache.Get(sessionCacheKey(cookie.Value))
	if !ok {
		t.Fatal("expected cache entry")
	}
	if entry.ExpiresAt.After(time.Now().UTC().Add(31 * time.Second)) {
		t.Fatalf("cache ExpiresAt = %v, exceeds TTL", entry.ExpiresAt)
	}
}

// 缓存命中时 last_accessed_at 必须继续前进（会话列表按它排序）。
func TestSessionAuthCacheStillTouchesLastAccessedAt(t *testing.T) {
	service := newSessionCacheTestService(t, "30s")
	cookie := loginForCacheTest(t, service)

	ctx := context.Background()
	db, err := service.openDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 制造一个很久以前的 last_accessed_at，让首次鉴权必然写库。
	old := formatTime(time.Now().UTC().Add(-2 * time.Hour))
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET last_accessed_at = ? WHERE session_id = ?`, old, cookie.Value); err != nil {
		t.Fatal(err)
	}
	if !isAuthenticated(t, service, cookie) {
		t.Fatal("auth should pass")
	}

	var firstTouch string
	if err := db.QueryRowContext(ctx, `SELECT last_accessed_at FROM sessions WHERE session_id = ?`, cookie.Value).Scan(&firstTouch); err != nil {
		t.Fatal(err)
	}
	if firstTouch == old {
		t.Fatalf("last_accessed_at = %q, want refreshed", firstTouch)
	}

	// 命中缓存且未到 60s 节流窗口时不应重复写库。
	for i := 0; i < 10; i++ {
		if !isAuthenticated(t, service, cookie) {
			t.Fatalf("auth #%d failed", i)
		}
	}
	var secondTouch string
	if err := db.QueryRowContext(ctx, `SELECT last_accessed_at FROM sessions WHERE session_id = ?`, cookie.Value).Scan(&secondTouch); err != nil {
		t.Fatal(err)
	}
	if secondTouch != firstTouch {
		t.Fatalf("last_accessed_at changed within throttle window: %q -> %q", firstTouch, secondTouch)
	}
}
