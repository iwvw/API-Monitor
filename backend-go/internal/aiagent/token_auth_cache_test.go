package aiagent

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// newCacheTestService 用指定的 AUTH_CACHE_TTL 构造服务。必须在 New 之前设置
// 环境变量，因为 TTL 在构造期解析一次。
func newCacheTestService(t *testing.T, cacheTTL string) *Service {
	t.Helper()
	if cacheTTL != "" {
		t.Setenv("AUTH_CACHE_TTL", cacheTTL)
	}
	service := New(config.Config{
		Version: "test",
		DataDir: t.TempDir(),
		DBName:  "aiagent_cache_test.db",
	})
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return service
}

// seedTokenUser 建一个用户并签发 token，返回明文 token 与用户 ID。
func seedTokenUser(t *testing.T, service *Service) (plain, userID string) {
	t.Helper()
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := service.createUser(ctx, db, "cache-user", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	plain, _, err = service.issueToken(ctx, db, user.ID, "cache-device", "")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	return plain, user.ID
}

// authenticate 走真实入口（resolveAuth → authenticateBearer），
// 而不是直接调用 authenticateToken：缓存判定发生在 Open 之前，
// 只有走完整入口才能验证「命中时完全不碰数据库」。
func authenticate(t *testing.T, service *Service, plain string) (authContext, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	req.RemoteAddr = "203.0.113.7:4567"
	return service.resolveAuth(req)
}

// 证明缓存命中时不再回源：把数据库里的 token 行删掉（绕过 service 的所有失效
// 逻辑），如果鉴权仍然成功，说明结果来自进程内缓存而不是 SQLite。
func TestTokenAuthCacheAvoidsDatabaseOnHit(t *testing.T) {
	service := newCacheTestService(t, "30s")
	if !service.tokenAuthCache.Enabled() {
		t.Fatal("cache should be enabled for AUTH_CACHE_TTL=30s")
	}
	plain, userID := seedTokenUser(t, service)

	first, err := authenticate(t, service, plain)
	if err != nil {
		t.Fatalf("first auth: %v", err)
	}
	if first.UserID != userID {
		t.Fatalf("UserID = %q, want %q", first.UserID, userID)
	}
	if n := service.tokenAuthCache.Len(); n != 1 {
		t.Fatalf("cache Len = %d after first auth, want 1", n)
	}

	// 100 次连续请求必须全部命中缓存，条目数不增长。
	for i := 0; i < 100; i++ {
		if _, err := authenticate(t, service, plain); err != nil {
			t.Fatalf("auth #%d: %v", i, err)
		}
	}
	if n := service.tokenAuthCache.Len(); n != 1 {
		t.Fatalf("cache Len = %d after 100 requests, want 1", n)
	}

	// 直接删库行：命中缓存时仍应通过（证明这 100 次没有查库）。
	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM aiagent_tokens WHERE user_id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("cached auth must not hit the database, got %v", err)
	}
}

// AUTH_CACHE_TTL=0 时必须与改造前完全一致：每次请求都查库。
func TestTokenAuthCacheDisabledQueriesEveryTime(t *testing.T) {
	service := newCacheTestService(t, "0")
	if service.tokenAuthCache.Enabled() {
		t.Fatal("cache must be disabled for AUTH_CACHE_TTL=0")
	}
	plain, userID := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("first auth: %v", err)
	}

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DELETE FROM aiagent_tokens WHERE user_id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	// 缓存关闭 → 每次都回源 → 立刻失败。
	if _, err := authenticate(t, service, plain); !errors.Is(err, errInvalidCreds) {
		t.Fatalf("auth after row deletion = %v, want errInvalidCreds", err)
	}
}

// 安全红线：撤销 token 后必须立即 401，不等待 TTL。
func TestTokenAuthCacheInvalidatedOnRevoke(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	auth, err := authenticate(t, service, plain)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := service.revokeToken(ctx, db, auth.TokenID, userID); err != nil {
		t.Fatalf("revokeToken: %v", err)
	}

	if _, err := authenticate(t, service, plain); !errors.Is(err, errTokenRevoked) {
		t.Fatalf("auth after revoke = %v, want errTokenRevoked", err)
	}
}

// 安全红线：撤销某用户全部 token 后必须立即全部失效。
func TestTokenAuthCacheInvalidatedOnRevokeUserTokens(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("auth: %v", err)
	}

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := service.revokeUserTokens(ctx, db, userID); err != nil {
		t.Fatalf("revokeUserTokens: %v", err)
	}

	if _, err := authenticate(t, service, plain); !errors.Is(err, errTokenRevoked) {
		t.Fatalf("auth after revokeUserTokens = %v, want errTokenRevoked", err)
	}
}

// 安全红线：用户被禁用后必须立即 401。
func TestTokenAuthCacheInvalidatedOnUserDisabled(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("auth: %v", err)
	}

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	disabled := true
	if err := service.updateUser(ctx, db, userID, nil, &disabled); err != nil {
		t.Fatalf("updateUser: %v", err)
	}

	if _, err := authenticate(t, service, plain); !errors.Is(err, errUserDisabled) {
		t.Fatalf("auth after disable = %v, want errUserDisabled", err)
	}
}

// 安全红线：用户被删除后必须立即 401。
func TestTokenAuthCacheInvalidatedOnUserDeleted(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("auth: %v", err)
	}

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := service.deleteUser(ctx, db, userID); err != nil {
		t.Fatalf("deleteUser: %v", err)
	}

	if _, err := authenticate(t, service, plain); !errors.Is(err, errInvalidCreds) {
		t.Fatalf("auth after delete = %v, want errInvalidCreds", err)
	}
}

// 无效凭据绝不缓存：攻击者不能用缓存探测 token 是否存在。
func TestTokenAuthCacheDoesNotCacheInvalidCredentials(t *testing.T) {
	service := newCacheTestService(t, "30s")
	// 先建一个合法 token（保证用户存在），再用无效凭据反复请求。
	seedTokenUser(t, service)

	for i := 0; i < 100; i++ {
		if _, err := authenticate(t, service, "definitely-not-a-valid-token"); !errors.Is(err, errInvalidCreds) {
			t.Fatalf("attempt #%d = %v, want errInvalidCreds", i, err)
		}
	}
	if n := service.tokenAuthCache.Len(); n != 0 {
		t.Fatalf("cache Len = %d, invalid credentials must not be cached", n)
	}
}

// 缓存命中时 last_used_at 必须继续前进（否则面板最后活跃时间停摆）。
func TestTokenAuthCacheStillTouchesLastUsedAt(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 先制造一个「很久以前」的 last_used_at，让首次鉴权必然触发写库。
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `UPDATE aiagent_tokens SET last_used_at = ? WHERE user_id = ?`, old, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("first auth: %v", err)
	}

	readLastUsed := func() string {
		var value sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT last_used_at FROM aiagent_tokens WHERE user_id = ?`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value.String
	}
	firstTouch := readLastUsed()
	if firstTouch == "" || firstTouch == old {
		t.Fatalf("last_used_at = %q, want refreshed", firstTouch)
	}

	// 命中缓存时（未到 1 分钟节流窗口）不应重复写库，但条目仍必须保持有效。
	for i := 0; i < 10; i++ {
		if _, err := authenticate(t, service, plain); err != nil {
			t.Fatalf("auth #%d: %v", i, err)
		}
	}
	if got := readLastUsed(); got != firstTouch {
		t.Fatalf("last_used_at changed within throttle window: %q -> %q", firstTouch, got)
	}
}

// 缓存条目的过期时间不得越过凭据自身的 expires_at。
func TestTokenAuthCacheRespectsCredentialExpiry(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, userID := seedTokenUser(t, service)

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 把 token 改成 1 秒后过期。
	soon := time.Now().UTC().Add(1 * time.Second).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `UPDATE aiagent_tokens SET expires_at = ? WHERE user_id = ?`, soon, userID); err != nil {
		t.Fatal(err)
	}

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("auth before expiry: %v", err)
	}

	// 条目过期时间应被收紧到凭据有效期（1s），而不是 30s。
	entry, ok := service.tokenAuthCache.Get(tokenCacheKey(plain))
	if !ok {
		t.Fatal("expected cache entry")
	}
	if entry.ExpiresAt.After(time.Now().UTC().Add(5 * time.Second)) {
		t.Fatalf("cache ExpiresAt = %v, must not exceed credential expiry", entry.ExpiresAt)
	}
}

// 缓存命中时必须连数据库都不碰。把 DataDir 指向不可创建的路径后，
// 任何 store.Open 都会失败；命中缓存仍应通过。
func TestTokenAuthCacheSkipsDatabaseOpenEntirely(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, _ := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("priming auth: %v", err)
	}

	// store.Open 内部会 os.MkdirAll(DataDir)；换成已存在的普通文件使其必然失败。
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	service.cfg.DataDir = blocker

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("cached auth must not require a database connection: %v", err)
	}
}
func TestTokenAuthCacheKeyIsDigestNotPlaintext(t *testing.T) {
	service := newCacheTestService(t, "30s")
	plain, _ := seedTokenUser(t, service)

	if _, err := authenticate(t, service, plain); err != nil {
		t.Fatalf("auth: %v", err)
	}

	key := tokenCacheKey(plain)
	if key == plain {
		t.Fatal("cache key must not be the plaintext token")
	}
	if len(key) != 64 {
		t.Fatalf("cache key length = %d, want 64 (sha256 hex)", len(key))
	}
	// 明文不能出现在键里，即使长度不同。
	for i := 0; i+8 <= len(plain); i++ {
		if key == plain[i:i+8] {
			t.Fatal("cache key contains plaintext token material")
		}
	}
}
