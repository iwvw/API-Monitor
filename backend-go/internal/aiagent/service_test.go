package aiagent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestMigrateInstanceOwnership 覆盖旧库（实例带 user_id 归属列）升级到
// 「平台实例 + 授权表」的迁移：旧归属转成授权，列与旧索引被移除。
func TestMigrateInstanceOwnership(t *testing.T) {
	cfg := testConfig(t)
	service := New(cfg)
	ctx := context.Background()

	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 构造旧版 schema：实例带 user_id，唯一索引含 user_id。
	legacy := []string{
		`CREATE TABLE aiagent_users (
			id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
			display_name TEXT, disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL, last_login_at TEXT)`,
		`CREATE TABLE aiagent_instances (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL, server_id TEXT NOT NULL, provider TEXT NOT NULL,
			label TEXT NOT NULL, port INTEGER NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX idx_aiagent_instances_user ON aiagent_instances(user_id)`,
		`CREATE UNIQUE INDEX idx_aiagent_instances_unique ON aiagent_instances(user_id, server_id, port)`,
	}
	for _, statement := range legacy {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("legacy schema %q: %v", statement, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO aiagent_users (id, username, password_hash, created_at, updated_at)
		VALUES ('usr_legacy', 'legacy', 'x', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO aiagent_instances
		(id, user_id, server_id, provider, label, port, enabled, created_at, updated_at)
		VALUES ('inst_legacy', 'usr_legacy', 'server-001', 'opencode', 'old', 4096, 1,
		'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	db.Close()

	if err := service.Initialize(ctx); err != nil {
		t.Fatalf("Initialize after legacy schema: %v", err)
	}
	db, err = service.store.Open(ctx)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	if has, err := columnExists(ctx, db, "aiagent_instances", "user_id"); err != nil || has {
		t.Fatalf("user_id column must be dropped, has=%v err=%v", has, err)
	}
	granted, err := service.instanceGrantedTo(ctx, db, "inst_legacy", "usr_legacy")
	if err != nil {
		t.Fatalf("instanceGrantedTo: %v", err)
	}
	if !granted {
		t.Fatal("legacy owner must be converted into a grant")
	}
	// 迁移后唯一索引按 (server_id, provider)：同一主机同 Provider 再建应冲突。
	if _, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Provider: "opencode", Label: "dup"}); err != errDuplicate {
		t.Fatalf("expected errDuplicate after migration, got %v", err)
	}
	// 另一 Provider 可共存。
	if _, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Provider: "pi", Label: "pi-node"}); err != nil {
		t.Fatalf("different provider on same host should be allowed: %v", err)
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

func TestInstanceGrantEnforced(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	alice, err := service.createUser(ctx, db, "alice", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser alice: %v", err)
	}
	bob, err := service.createUser(ctx, db, "bob", "secret-pass-2", "")
	if err != nil {
		t.Fatalf("createUser bob: %v", err)
	}

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "home-pc",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if instance.Port != 4096 {
		t.Fatalf("expected provider default port, got %d", instance.Port)
	}

	// 同一主机同一 Provider 不可重复登记。
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "dup",
	}); err != errDuplicate {
		t.Fatalf("expected duplicate instance error, got %v", err)
	}

	// 默认不授权：两个用户都看不到实例。
	for _, user := range []User{alice, bob} {
		list, err := service.listInstancesForUser(ctx, db, user.ID)
		if err != nil {
			t.Fatalf("listInstancesForUser: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("expected no granted instances for %s, got %d", user.Username, len(list))
		}
	}

	// 授权给 alice 后只有 alice 可见。
	if err := service.setGrantsForUser(ctx, db, alice.ID, []string{instance.ID}); err != nil {
		t.Fatalf("setGrantsForUser: %v", err)
	}
	aliceList, err := service.listInstancesForUser(ctx, db, alice.ID)
	if err != nil {
		t.Fatalf("listInstancesForUser alice: %v", err)
	}
	if len(aliceList) != 1 || aliceList[0].ID != instance.ID {
		t.Fatalf("expected alice to see the granted instance, got %+v", aliceList)
	}
	if granted, err := service.instanceGrantedTo(ctx, db, instance.ID, alice.ID); err != nil || !granted {
		t.Fatalf("expected grant for alice, got granted=%v err=%v", granted, err)
	}
	if granted, err := service.instanceGrantedTo(ctx, db, instance.ID, bob.ID); err != nil || granted {
		t.Fatalf("expected no grant for bob, got granted=%v err=%v", granted, err)
	}

	// 整体替换语义：清空后 alice 也不再可见。
	if err := service.setGrantsForUser(ctx, db, alice.ID, nil); err != nil {
		t.Fatalf("clear grants: %v", err)
	}
	cleared, err := service.listInstancesForUser(ctx, db, alice.ID)
	if err != nil {
		t.Fatalf("listInstancesForUser after clear: %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected grants to be cleared, got %d", len(cleared))
	}

	// 删除实例时授权一并清理。
	if err := service.setGrantsForUser(ctx, db, alice.ID, []string{instance.ID}); err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	if err := service.deleteInstance(ctx, db, instance.ID); err != nil {
		t.Fatalf("deleteInstance: %v", err)
	}
	grants, err := service.listGrantsForInstance(ctx, db, instance.ID)
	if err != nil {
		t.Fatalf("listGrantsForInstance: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected grants to be removed with the instance, got %d", len(grants))
	}
}

func TestSetGrantsIgnoresUnknownInstance(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "grant-user", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	// 无效 ID 被跳过，有效 ID 正常写入，整批不失败。
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{"inst_missing", instance.ID, instance.ID}); err != nil {
		t.Fatalf("setGrantsForUser: %v", err)
	}
	ids, err := service.listInstanceIDsForUser(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("listInstanceIDsForUser: %v", err)
	}
	if len(ids) != 1 || !ids[instance.ID] {
		t.Fatalf("expected only the valid instance to be granted, got %+v", ids)
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

	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "nope",
		Label:    "x",
	}); err == nil {
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
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"})
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

	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 70000}); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for out-of-range port, got %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: -1}); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for negative port, got %v", err)
	}

	// 端口必须落在 Provider 允许区间 [4096, 4195] 内：区间外同样拒绝，
	// 防止网关变成任意端口转发器（ADR-0006 第 1 条）。
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 8080}); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for out-of-range port, got %v", err)
	}
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Port: 4096}); err != nil {
		t.Fatalf("provider default port update should succeed: %v", err)
	}
}

// 端口区间放开后，区间内的非默认端口应当被接受（ADR-0006 第 1 条）。
func TestCreateInstanceAcceptsPortWithinProviderRange(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "second",
		Port:     4100,
	})
	if err != nil {
		t.Fatalf("区间内端口应被接受: %v", err)
	}
	if instance.Port != 4100 {
		t.Fatalf("expected port 4100, got %d", instance.Port)
	}
}

// 新建实例默认不托管：升级到本版本不应突然开始管理用户手工启动的进程。
func TestNewInstanceDefaultsToUnmanaged(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "manual",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if instance.DesiredState != "" {
		t.Fatalf("新实例期望状态应为空（不托管），got %q", instance.DesiredState)
	}
}

// 期望状态可被设置并持久化；非法值被拒绝。
func TestInstanceDesiredStateRoundTrip(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "managed",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	updated, err := service.updateInstance(ctx, db, instance.ID, instancePayload{
		DesiredState: DesiredStateRunning,
	})
	if err != nil {
		t.Fatalf("updateInstance: %v", err)
	}
	if updated.DesiredState != DesiredStateRunning {
		t.Fatalf("期望 running，got %q", updated.DesiredState)
	}

	// 回读确认落库。
	reloaded, err := service.getInstance(ctx, db, instance.ID)
	if err != nil {
		t.Fatalf("getInstance: %v", err)
	}
	if reloaded.DesiredState != DesiredStateRunning {
		t.Fatalf("期望状态未持久化，got %q", reloaded.DesiredState)
	}

	// 非法值必须拒绝，且不改变已存值。
	if _, err := service.updateInstance(ctx, db, instance.ID, instancePayload{
		DesiredState: "definitely-invalid",
	}); err != errInvalidDesiredState {
		t.Fatalf("expected errInvalidDesiredState, got %v", err)
	}
	after, err := service.getInstance(ctx, db, instance.ID)
	if err != nil {
		t.Fatalf("getInstance: %v", err)
	}
	if after.DesiredState != DesiredStateRunning {
		t.Fatalf("非法更新不应改变已存期望状态，got %q", after.DesiredState)
	}
}

// 不传 desiredState 时不应改动它（避免「改标签顺手关掉托管」）。
func TestUpdateWithoutDesiredStatePreservesIt(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID:     "server-001",
		Provider:     "opencode",
		Label:        "managed",
		DesiredState: DesiredStateRunning,
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	updated, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Label: "renamed"})
	if err != nil {
		t.Fatalf("updateInstance: %v", err)
	}
	if updated.Label != "renamed" {
		t.Fatalf("标签应更新，got %q", updated.Label)
	}
	if updated.DesiredState != DesiredStateRunning {
		t.Fatalf("期望状态不应被改动，got %q", updated.DesiredState)
	}
}

// ClearDesiredState 可把实例转回「不托管」。
func TestClearDesiredStateReturnsToUnmanaged(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID:     "server-001",
		Provider:     "opencode",
		Label:        "managed",
		DesiredState: DesiredStateRunning,
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}

	updated, err := service.updateInstance(ctx, db, instance.ID, instancePayload{ClearDesiredState: true})
	if err != nil {
		t.Fatalf("updateInstance: %v", err)
	}
	if updated.DesiredState != "" {
		t.Fatalf("期望状态应被清空，got %q", updated.DesiredState)
	}
}

// 迁移必须幂等：重复 Initialize 不应报错，也不应重复加列。
func TestDesiredStateMigrationIsIdempotent(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	for attempt := 0; attempt < 3; attempt++ {
		if err := service.Initialize(ctx); err != nil {
			t.Fatalf("第 %d 次 Initialize 失败: %v", attempt+1, err)
		}
	}
}

// ValidDesiredState 的取值边界。
func TestValidDesiredState(t *testing.T) {
	valid := []string{"", DesiredStateRunning, DesiredStateStopped}
	for _, value := range valid {
		if !ValidDesiredState(value) {
			t.Fatalf("%q 应合法", value)
		}
	}
	invalid := []string{"Running", "running ", "stopped2", "pause"}
	for _, value := range invalid {
		if ValidDesiredState(value) {
			t.Fatalf("%q 应非法", value)
		}
	}
}

// 区间边界：默认端口与上界都合法，超出上界一个即拒绝。
func TestProviderPortAllowedBoundaries(t *testing.T) {
	provider, ok := LookupProvider("opencode")
	if !ok {
		t.Fatal("opencode provider missing")
	}
	upper := provider.DefaultPort + provider.PortRangeSize

	cases := []struct {
		port int
		want bool
	}{
		{provider.DefaultPort - 1, false},
		{provider.DefaultPort, true},
		{provider.DefaultPort + 1, true},
		{upper, true},
		{upper + 1, false},
		{0, false},
		{-1, false},
		{70000, false},
	}
	for _, testCase := range cases {
		if got := provider.PortAllowed(testCase.port); got != testCase.want {
			t.Fatalf("PortAllowed(%d) = %v, want %v", testCase.port, got, testCase.want)
		}
	}
}

// 同一主机上两个实例不能指向同一端口，否则网关会把两股流量送到同一本地服务。
func TestInstanceRejectsDuplicatePortOnSameHost(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "first",
		Port:     4100,
	}); err != nil {
		t.Fatalf("first instance: %v", err)
	}

	// 同主机同端口必须被拒绝，否则网关会把两股流量送到同一本地服务。
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "dup",
		Port:     4100,
	}); err == nil {
		t.Fatal("expected error for duplicate port on same host")
	}

	// 换一台主机后同端口应当可以登记。
	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-002",
		Provider: "opencode",
		Label:    "other-host",
		Port:     4100,
	}); err != nil {
		t.Fatalf("same port on a different host should be allowed: %v", err)
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

	if _, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-001",
		Provider: "opencode",
		Label:    "redis",
		Port:     6379,
	}); err != errInvalidPort {
		t.Fatalf("expected errInvalidPort for non-provider port, got %v", err)
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
	// 未注入 ServerProvider：只有已登记实例涉及的主机可用（管理员）。
	if service.serverAllowed(ctx, "server-001", user.ID, false) {
		t.Fatal("unregistered server id must be rejected when provider is absent")
	}
	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Label: "x"})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if !service.serverAllowed(ctx, "server-001", user.ID, true) {
		t.Fatal("an already-registered server id must be allowed for admins")
	}
	// 普通用户未获授权时不可达该主机。
	if service.serverAllowed(ctx, "server-001", user.ID, false) {
		t.Fatal("non-admin without a grant must not reach the server")
	}
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{instance.ID}); err != nil {
		t.Fatalf("setGrantsForUser: %v", err)
	}
	if !service.serverAllowed(ctx, "server-001", user.ID, false) {
		t.Fatal("non-admin with a granted instance must reach its server")
	}

	service.SetServerProvider(stubServerProvider{options: []ServerOption{{ID: "server-002", Name: "desk"}}})
	// 管理员可选任意已登记主机。
	if !service.serverAllowed(ctx, "server-002", user.ID, true) {
		t.Fatal("admin must be allowed on a configured server")
	}
	if service.serverAllowed(ctx, "server-999", user.ID, true) {
		t.Fatal("unregistered server id must be rejected")
	}
	// 普通用户不得使用自己未被授权实例涉及的已登记主机（跨租户隔离）。
	if service.serverAllowed(ctx, "server-002", user.ID, false) {
		t.Fatal("non-admin must not reach a server they have no granted instance on")
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

	instance, err := service.createInstance(ctx, db, instancePayload{ServerID: "server-001", Provider: "opencode", Label: "x"})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if instance.Port != 4096 {
		t.Fatalf("expected opencode default 4096, got %d", instance.Port)
	}
	// 换 Provider 且不传端口：端口应回落到新 Provider 的默认端口（pi=3000），而非沿用 4096。
	updated, err := service.updateInstance(ctx, db, instance.ID, instancePayload{Provider: "pi"})
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

func TestPreferencesRoundTripAndIsolation(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	alice, err := service.createUser(ctx, db, "alice", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser alice: %v", err)
	}
	bob, err := service.createUser(ctx, db, "bob", "secret-pass-2", "")
	if err != nil {
		t.Fatalf("createUser bob: %v", err)
	}

	values := map[string]json.RawMessage{
		"srv:server-1:opencode-hidden-directories": json.RawMessage(`["/work/secret"]`),
		"theme-preset": json.RawMessage(`"eucalyptus"`),
	}
	if _, err := service.putPreferences(ctx, db, alice.ID, values, "2026-01-01T00:00:00Z", false); err != nil {
		t.Fatalf("putPreferences: %v", err)
	}

	items, err := service.listPreferences(ctx, db, alice.ID)
	if err != nil {
		t.Fatalf("listPreferences: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 preferences, got %d", len(items))
	}
	byKey := make(map[string]string, len(items))
	for _, item := range items {
		byKey[item.Key] = string(item.Value)
	}
	if got := byKey["srv:server-1:opencode-hidden-directories"]; got != `["/work/secret"]` {
		t.Fatalf("unexpected value for hidden directories: %s", got)
	}
	if got := byKey["theme-preset"]; got != `"eucalyptus"` {
		t.Fatalf("unexpected theme preset: %s", got)
	}

	// 用户隔离：bob 读不到 alice 的偏好。
	bobItems, err := service.listPreferences(ctx, db, bob.ID)
	if err != nil {
		t.Fatalf("listPreferences bob: %v", err)
	}
	if len(bobItems) != 0 {
		t.Fatalf("bob must not see alice preferences, got %d", len(bobItems))
	}

	// 删除单键只影响该用户。
	removed, err := service.deletePreferences(ctx, db, alice.ID, []string{"theme-preset"})
	if err != nil {
		t.Fatalf("deletePreferences: %v", err)
	}
	if len(removed) != 1 || removed[0] != "theme-preset" {
		t.Fatalf("unexpected removed keys: %v", removed)
	}
	items, err = service.listPreferences(ctx, db, alice.ID)
	if err != nil {
		t.Fatalf("listPreferences after delete: %v", err)
	}
	if len(items) != 1 || items[0].Key != "srv:server-1:opencode-hidden-directories" {
		t.Fatalf("unexpected items after delete: %+v", items)
	}
	// 删除不存在的键不改动他人数据，且不报错。
	removed, err = service.deletePreferences(ctx, db, bob.ID, []string{"theme-preset"})
	if err != nil {
		t.Fatalf("deletePreferences on other user: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("delete must not touch other user, removed %v", removed)
	}
	// 含斜杠的键必须能写入并删除，验证不走路径分段的回归。
	slashKey := map[string]json.RawMessage{"srv:a/b:c": json.RawMessage(`1`)}
	if _, err := service.putPreferences(ctx, db, alice.ID, slashKey, "", false); err != nil {
		t.Fatalf("putPreferences slash key: %v", err)
	}
	removed, err = service.deletePreferences(ctx, db, alice.ID, []string{"srv:a/b:c"})
	if err != nil {
		t.Fatalf("deletePreferences slash key: %v", err)
	}
	if len(removed) != 1 || removed[0] != "srv:a/b:c" {
		t.Fatalf("slash key not removed: %v", removed)
	}
}

func TestPreferencesLastWriteWins(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "carol", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}

	newer := map[string]json.RawMessage{"project-order": json.RawMessage(`["b","a"]`)}
	if _, err := service.putPreferences(ctx, db, user.ID, newer, "2026-05-01T00:00:00Z", true); err != nil {
		t.Fatalf("putPreferences newer: %v", err)
	}

	// 旧时间戳不得覆盖新值。
	stale := map[string]json.RawMessage{"project-order": json.RawMessage(`["a","b"]`)}
	written, err := service.putPreferences(ctx, db, user.ID, stale, "2026-01-01T00:00:00Z", true)
	if err != nil {
		t.Fatalf("putPreferences stale: %v", err)
	}
	if len(written) != 0 {
		t.Fatalf("stale write must be skipped, wrote %v", written)
	}
	items, err := service.listPreferences(ctx, db, user.ID)
	if err != nil {
		t.Fatalf("listPreferences: %v", err)
	}
	if len(items) != 1 || string(items[0].Value) != `["b","a"]` {
		t.Fatalf("stale write overwrote newer value: %+v", items)
	}

	// 不启用条件覆盖时，同样的旧值会直接写入。
	written, err = service.putPreferences(ctx, db, user.ID, stale, "2026-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatalf("putPreferences unconditional: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("unconditional write must apply, wrote %v", written)
	}
}

func TestPreferencesRejectsOversizedValueAndBadKey(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "dave", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}

	huge := map[string]json.RawMessage{"big": json.RawMessage(`"` + strings.Repeat("x", maxPreferenceValueBytes+1) + `"`)}
	if _, err := service.putPreferences(ctx, db, user.ID, huge, "", false); err != errPreferenceTooLarge {
		t.Fatalf("expected oversized error, got %v", err)
	}
	longKey := map[string]json.RawMessage{strings.Repeat("k", 129): json.RawMessage(`1`)}
	if _, err := service.putPreferences(ctx, db, user.ID, longKey, "", false); err != errInvalidPreferenceKey {
		t.Fatalf("expected invalid key error, got %v", err)
	}
}

func TestDeleteUserCascadesPreferences(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "erin", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if _, err := service.putPreferences(ctx, db, user.ID, map[string]json.RawMessage{"theme-preset": json.RawMessage(`"sakura"`)}, "", false); err != nil {
		t.Fatalf("putPreferences: %v", err)
	}
	if err := service.deleteUser(ctx, db, user.ID); err != nil {
		t.Fatalf("deleteUser: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM aiagent_user_preferences WHERE user_id = ?`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count preferences: %v", err)
	}
	if count != 0 {
		t.Fatalf("preferences must be removed with user, got %d rows", count)
	}
}

func TestPreferencesHTTPRequiresBearer(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	user, err := service.createUser(ctx, db, "frank", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "laptop", "")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}

	body := strings.NewReader(`{"values":{"theme-preset":"\"ocean\""}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/aiagent/preferences", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer, got %d body=%s", res.Code, res.Body.String())
	}

	body = strings.NewReader(`{"values":{"theme-preset":"\"ocean\""}}`)
	req = httptest.NewRequest(http.MethodPut, "/api/aiagent/preferences", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plain)
	res = httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with bearer, got %d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/aiagent/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	res = httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "theme-preset") {
		t.Fatalf("list response missing stored preference: %s", res.Body.String())
	}
}
