package aiagent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Version: "test",
		DataDir: t.TempDir(),
		DBName:  "aiagent_test.db",
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	service := New(testConfig(t))
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return service
}

func TestInitializeIsIdempotent(t *testing.T) {
	service := newTestService(t)
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}

func TestProviderRegistry(t *testing.T) {
	providers := Providers()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider")
	}
	opencode, ok := LookupProvider("opencode")
	if !ok {
		t.Fatal("opencode provider must be registered")
	}
	if opencode.DefaultPort != 4096 || !opencode.Verified {
		t.Fatalf("unexpected opencode provider: %+v", opencode)
	}
	if _, ok := LookupProvider("does-not-exist"); ok {
		t.Fatal("unknown provider must not resolve")
	}
	// 空 ID 回退到默认 Provider。
	if fallback, ok := LookupProvider(""); !ok || fallback.ID != defaultProviderID {
		t.Fatalf("empty provider must fall back to %s, got %+v ok=%v", defaultProviderID, fallback, ok)
	}
}

func TestValidateUsername(t *testing.T) {
	valid := []string{"salen", "work-pc", "a.b_c", "user123"}
	for _, name := range valid {
		if err := validateUsername(name); err != nil {
			t.Fatalf("expected %q valid: %v", name, err)
		}
	}
	invalid := []string{"ab", "with space", "bad/slash", "中文名", ""}
	for _, name := range invalid {
		if err := validateUsername(name); err == nil {
			t.Fatalf("expected %q invalid", name)
		}
	}
}

func TestUserLoginAndTokenLifecycle(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "salen", "secret-pass-1", "Salen")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if _, err := service.createUser(ctx, db, "salen", "secret-pass-2", ""); err != errDuplicate {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	plain, token, err := service.issueToken(ctx, db, user.ID, "iphone", "203.0.113.10")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	if plain == "" || token.Prefix == "" {
		t.Fatal("expected non-empty token and prefix")
	}

	auth, err := service.authenticateToken(ctx, db, plain, "203.0.113.10")
	if err != nil {
		t.Fatalf("authenticateToken: %v", err)
	}
	if auth.UserID != user.ID || auth.TokenID != token.ID {
		t.Fatalf("unexpected auth context: %+v", auth)
	}
	if _, err := service.authenticateToken(ctx, db, "not-a-token", ""); err != errInvalidCreds {
		t.Fatalf("expected invalid credentials, got %v", err)
	}

	if err := service.revokeToken(ctx, db, token.ID, user.ID); err != nil {
		t.Fatalf("revokeToken: %v", err)
	}
	if _, err := service.authenticateToken(ctx, db, plain, ""); err != errTokenRevoked {
		t.Fatalf("expected revoked error, got %v", err)
	}
}

func TestDisabledUserCannotAuthenticate(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "worker", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "", "")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	disabled := true
	if err := service.updateUser(ctx, db, user.ID, nil, &disabled); err != nil {
		t.Fatalf("updateUser: %v", err)
	}
	if _, err := service.authenticateToken(ctx, db, plain, ""); err != errUserDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestInstanceOwnershipEnforced(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	owner, err := service.createUser(ctx, db, "owner", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser owner: %v", err)
	}
	intruder, err := service.createUser(ctx, db, "intruder", "secret-pass-2", "")
	if err != nil {
		t.Fatalf("createUser intruder: %v", err)
	}

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "home-pc",
	}, owner.ID)
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if instance.Port != 4096 {
		t.Fatalf("expected provider default port, got %d", instance.Port)
	}

	// 同一用户同主机同端口不可重复登记。
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "dup",
	}, owner.ID); err != errDuplicate {
		t.Fatalf("expected duplicate instance error, got %v", err)
	}

	// 越权访问必须表现为「不存在」。
	if err := service.deleteInstance(ctx, db, instance.ID, intruder.ID, false); err != errNotFound {
		t.Fatalf("expected not-found for cross-user delete, got %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Label: "hijacked"}, intruder.ID, false); err != errNotFound {
		t.Fatalf("expected not-found for cross-user update, got %v", err)
	}
	// 管理员可跨用户操作。
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Label: "renamed"}, intruder.ID, true); err != nil {
		t.Fatalf("admin update should succeed: %v", err)
	}

	owned, err := service.listInstances(ctx, db, owner.ID, false)
	if err != nil {
		t.Fatalf("listInstances: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("expected 1 instance for owner, got %d", len(owned))
	}
	if len(owner.ID) == 0 || len(intruder.ID) == 0 {
		t.Fatal("expected user ids")
	}
}

func TestInstanceRejectsUnknownProviderAndBadPort(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "bee", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "nope",
		Label:    "x",
	}, user.ID); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestInstanceMetaRoundTripAndLimit(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "meta-user", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"}, user.ID)
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	empty, err := service.getMeta(ctx, db, instance.ID)
	if err != nil {
		t.Fatalf("getMeta: %v", err)
	}
	if empty != "{}" {
		t.Fatalf("expected empty meta object, got %q", empty)
	}
	if err := service.putMeta(ctx, db, instance.ID, user.ID, `{"pinned":["a"]}`); err != nil {
		t.Fatalf("putMeta: %v", err)
	}
	stored, err := service.getMeta(ctx, db, instance.ID)
	if err != nil {
		t.Fatalf("getMeta after put: %v", err)
	}
	if stored != `{"pinned":["a"]}` {
		t.Fatalf("unexpected stored meta: %q", stored)
	}

	oversized := make([]byte, maxMetadataBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	if err := service.putMeta(ctx, db, instance.ID, user.ID, string(oversized)); err != errMetaTooLarge {
		t.Fatalf("expected meta too large, got %v", err)
	}
}

func TestLoginLimiterLocksAfterFailures(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < loginMaxFailures; i++ {
		if limiter.locked("salen", "203.0.113.10") {
			t.Fatalf("should not be locked before threshold (i=%d)", i)
		}
		limiter.fail("salen", "203.0.113.10")
	}
	if !limiter.locked("salen", "203.0.113.10") {
		t.Fatal("expected lockout after threshold failures")
	}
	// 不同来源 IP 不受影响。
	if limiter.locked("salen", "198.51.100.7") {
		t.Fatal("other IP must not be locked")
	}
	limiter.reset("salen", "203.0.113.10")
	if limiter.locked("salen", "203.0.113.10") {
		t.Fatal("reset should clear lockout")
	}
}

func TestStreamTokenBrokerSingleUse(t *testing.T) {
	broker := newStreamTokenBroker()
	token := broker.issue("usr_1", "inst_1")
	if token == "" {
		t.Fatal("expected stream token")
	}
	userID, ok := broker.consume(token, "inst_1")
	if !ok || userID != "usr_1" {
		t.Fatalf("expected successful consume, got ok=%v user=%q", ok, userID)
	}
	if _, ok := broker.consume(token, "inst_1"); ok {
		t.Fatal("stream token must be single-use")
	}
	other := broker.issue("usr_1", "inst_1")
	if _, ok := broker.consume(other, "inst_2"); ok {
		t.Fatal("stream token must be bound to its instance")
	}
}

func TestUpdateInstanceRejectsOutOfRangePort(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "porter", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"}, user.ID)
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 70000}, user.ID, true); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for out-of-range port, got %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: -1}, user.ID, true); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for negative port, got %v", err)
	}
	// 第一版只允许 Provider 已知端口：非默认端口同样拒绝，防止网关变成任意端口转发器。
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 8080}, user.ID, true); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for non-provider port, got %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 4096}, user.ID, true); err != nil {
		t.Fatalf("provider default port update should succeed: %v", err)
	}
}

func TestCreateInstanceRejectsNonProviderPort(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "portguard", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "redis",
		Port:     6379,
	}, user.ID); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for non-provider port, got %v", err)
	}
}

func TestCreateInstanceRequiresExistingOwner(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "orphan"}, "usr_missing"); err != errNotFound {
		t.Fatalf("expected errNotFound for missing owner, got %v", err)
	}
	if _, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "orphan"}, ""); err != errInvalidCreds {
		t.Fatalf("expected errInvalidCreds for empty owner, got %v", err)
	}
}

func TestUpdateUserMissingReturnsNotFound(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	disabled := true
	if err := service.updateUser(ctx, db, "usr_missing", nil, &disabled); err != errNotFound {
		t.Fatalf("expected errNotFound for missing user, got %v", err)
	}
}

func TestServerAllowedValidation(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	// 空 ID 一律拒绝。
	if service.serverAllowed(ctx, "", "usr_1", true) {
		t.Fatal("empty server id must be rejected")
	}
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	user, err := service.createUser(ctx, db, "hostuser", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	// 未注入 ServerProvider：只有已登记实例涉及的主机可用（任何身份）。
	if service.serverAllowed(ctx, "server-001", user.ID, false) {
		t.Fatal("unregistered server id must be rejected when provider is absent")
	}
	if _, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"}, user.ID); err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if !service.serverAllowed(ctx, "server-001", user.ID, false) {
		t.Fatal("an already-registered server id must be allowed")
	}

	service.SetServerProvider(stubServerProvider{options: []ServerOption{{ID: "server-002", Name: "desk"}}})
	// 管理员可选任意已登记主机。
	if !service.serverAllowed(ctx, "server-002", user.ID, true) {
		t.Fatal("admin must be allowed on a configured server")
	}
	if service.serverAllowed(ctx, "server-999", user.ID, true) {
		t.Fatal("unregistered server id must be rejected")
	}
	// 普通用户不得使用自己未涉及的已登记主机（跨租户隔离）。
	if service.serverAllowed(ctx, "server-002", user.ID, false) {
		t.Fatal("non-admin must not reach a server they have no instance on")
	}
}

type stubServerProvider struct {
	options []ServerOption
}

func (p stubServerProvider) ListServerOptions(context.Context) []ServerOption {
	return p.options
}

func TestClientIPIgnoresSpoofedForwardedHeader(t *testing.T) {
	service := newTestService(t)
	service.cfg.TrustedProxyCIDRs = []string{"203.0.113.0/24"}

	// 直连方不在受信代理范围：忽略 X-Forwarded-For，使用直连地址。
	spoofed := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances", nil)
	spoofed.RemoteAddr = "198.51.100.9:51234"
	spoofed.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := service.clientIP(spoofed); got != "198.51.100.9" {
		t.Fatalf("expected direct peer ip, got %q", got)
	}

	// 直连方是受信代理：采信转发头。
	trusted := httptest.NewRequest(http.MethodGet, "/api/aiagent/instances", nil)
	trusted.RemoteAddr = "203.0.113.10:51234"
	trusted.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.10")
	if got := service.clientIP(trusted); got != "1.2.3.4" {
		t.Fatalf("expected forwarded client ip, got %q", got)
	}
}

func TestUpdateUserDisplayNameClearAndPreserve(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "display", "secret-pass-1", "初始名")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	// nil 表示不修改：保留原显示名。
	if err := service.updateUser(ctx, db, user.ID, nil, nil); err != nil {
		t.Fatalf("updateUser(nil): %v", err)
	}
	if got, _ := service.getUserByID(ctx, db, user.ID); got.DisplayName != "初始名" {
		t.Fatalf("nil displayName must preserve value, got %q", got.DisplayName)
	}
	// 指向空串表示显式清空。
	empty := ""
	if err := service.updateUser(ctx, db, user.ID, &empty, nil); err != nil {
		t.Fatalf("updateUser(empty): %v", err)
	}
	if got, _ := service.getUserByID(ctx, db, user.ID); got.DisplayName != "" {
		t.Fatalf("empty displayName must clear value, got %q", got.DisplayName)
	}
}

func TestLoginLimiterThrottlesPerIPAcrossUsernames(t *testing.T) {
	limiter := newLoginLimiter()
	ip := "203.0.113.77"
	// 同一个 IP 对多个不同用户名各失败若干次：单用户名未达阈值，但 IP 维度应被锁定。
	for index := 0; index < loginMaxFailures; index++ {
		for _, username := range []string{"alpha", "bravo", "charlie", "delta"} {
			limiter.fail(username, ip)
		}
	}
	if !limiter.locked("brand-new-user", ip) {
		t.Fatal("expected per-IP throttle to lock password spraying across usernames")
	}
	if limiter.locked("brand-new-user", "198.51.100.9") {
		t.Fatal("other IPs must not be affected")
	}
}

func TestAssertUserActive(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "activecheck", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if err := service.assertUserActive(ctx, user.ID); err != nil {
		t.Fatalf("active user must pass: %v", err)
	}
	disabled := true
	if err := service.updateUser(ctx, db, user.ID, nil, &disabled); err != nil {
		t.Fatalf("updateUser: %v", err)
	}
	if err := service.assertUserActive(ctx, user.ID); err != errUserDisabled {
		t.Fatalf("disabled user must fail with errUserDisabled, got %v", err)
	}
	if err := service.assertUserActive(ctx, "usr_missing"); err != errNotFound {
		t.Fatalf("missing user must fail with errNotFound, got %v", err)
	}
}

func TestUpdateInstanceProviderSwitchResetsPort(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "switchuser", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Provider: "opencode", Label: "x"}, user.ID)
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if instance.Port != 4096 {
		t.Fatalf("expected opencode default 4096, got %d", instance.Port)
	}
	// 换 Provider 且不传端口：端口应回落到新 Provider 的默认端口（pi=3000），而非沿用 4096。
	updated, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Provider: "pi"}, user.ID, false)
	if err != nil {
		t.Fatalf("provider switch should succeed: %v", err)
	}
	if updated.Provider != "pi" || updated.Port != 3000 {
		t.Fatalf("expected pi@3000 after switch, got %s@%d", updated.Provider, updated.Port)
	}
}

func TestBuildInstanceViewWithoutRuntime(t *testing.T) {
	service := newTestService(t)
	instance := Instance{ID: "inst_1", ServerID: "server-001", Provider: "opencode", Port: 4096, Enabled: true}
	view := service.buildInstanceView(context.Background(), instance, true)
	if view.ProviderLabel != "OpenCode" {
		t.Fatalf("expected provider label, got %q", view.ProviderLabel)
	}
	if view.Status.Online {
		t.Fatal("instance must not be online without a configured runtime")
	}
	if view.Status.Error == "" {
		t.Fatal("expected an explanatory error when runtime is missing")
	}
}

// TestFilterResponseHeadersStripsUpstreamCORS 回归：上游自带的 CORS 头必须剥离，
// 否则会与面板安全中间件设置的同名头叠加成两个值，浏览器以
// "contains multiple values" 直接拒绝响应。
func TestFilterResponseHeadersStripsUpstreamCORS(t *testing.T) {
	source := http.Header{}
	source.Set("Access-Control-Allow-Origin", "http://localhost:5174")
	source.Set("Access-Control-Allow-Credentials", "true")
	source.Set("Access-Control-Allow-Methods", "GET,POST")
	source.Set("Content-Type", "application/json")
	source.Set("Connection", "keep-alive")

	filtered := filterResponseHeaders(source)

	for _, header := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
	} {
		if got := filtered.Get(header); got != "" {
			t.Fatalf("upstream %s must be stripped, got %q", header, got)
		}
	}
	if got := filtered.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unrelated header must survive, got %q", got)
	}
	if got := filtered.Get("Connection"); got != "" {
		t.Fatalf("hop-by-hop header must be stripped, got %q", got)
	}
}

// slowWriter 每次写入前短暂休眠，给读取 goroutine 覆盖复用缓冲区的机会，
// 用于复现「发送未拷贝切片导致响应体错位」的缺陷。
type slowWriter struct {
	header http.Header
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (w *slowWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *slowWriter) WriteHeader(int) {}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *slowWriter) Flush() {}

func (w *slowWriter) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// patternReader 逐块返回按序号填充的固定内容，便于校验每块字节是否被错位覆盖。
type patternReader struct {
	remaining int
	index     byte
}

func (r *patternReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = r.index
	}
	r.remaining -= n
	r.index++
	return n, nil
}

// TestCopyStreamPreservesChunkData 回归：读取 goroutine 复用了 32KB 缓冲区，
// 若把 buf[:read] 直接交给写方，下一轮 Read 会覆盖写方尚未写完的内存，导致
// 响应体错位（大响应 gzip 流损坏，浏览器报 ERR_CONTENT_DECODING_FAILED）。
func TestCopyStreamPreservesChunkData(t *testing.T) {
	const chunkSize = 32 * 1024
	const chunks = 16
	source := &patternReader{remaining: chunkSize * chunks}

	expected := make([]byte, 0, chunkSize*chunks)
	for i := 0; i < chunks; i++ {
		expected = append(expected, bytes.Repeat([]byte{byte(i)}, chunkSize)...)
	}

	writer := &slowWriter{}
	if _, err := copyStream(context.Background(), writer, source, nil); err != nil {
		t.Fatalf("copyStream: %v", err)
	}

	got := writer.bytes()
	if !bytes.Equal(got, expected) {
		firstDiff := -1
		for i := 0; i < len(expected) && i < len(got); i++ {
			if expected[i] != got[i] {
				firstDiff = i
				break
			}
		}
		t.Fatalf("stream data corrupted: len got=%d want=%d, first diff at byte %d", len(got), len(expected), firstDiff)
	}
}
