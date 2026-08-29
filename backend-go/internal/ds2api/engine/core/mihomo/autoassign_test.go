package mihomo

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// newAutoBindTestStore 构造含启用/禁用账号、手动代理账号与 3 个节点的内存配置。
// 初始桥未启用（避免 BindAccount/Apply 需要真实 mihomo 二进制），
// 需要自动调度时通过 enableAutoBridge 打开。
func newAutoBindTestStore(t *testing.T) *config.Store {
	t.Helper()
	t.Setenv("DS2API_MIHOMO_DIR", filepath.Join(t.TempDir(), "mihomo"))
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys": ["k1"],
		"accounts": [
			{"email": "a@test.com", "password": "x"},
			{"email": "b@test.com", "password": "x"},
			{"email": "c@test.com", "password": "x", "disabled": true},
			{"email": "d@test.com", "password": "x", "proxy_id": "manual-1"}
		],
		"proxies": [
			{"id": "manual-1", "name": "手动代理", "type": "socks5", "host": "10.0.0.1", "port": 1080}
		],
		"mihomo": {
			"enabled": false,
			"subscriptions": [{
				"id": "sub-1",
				"name": "测试机场",
				"url": "https://example.com/sub",
				"nodes": [
					{"name": "香港 01", "type": "ss", "raw": {"name": "香港 01", "type": "ss", "server": "hk.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "pw"}},
					{"name": "日本 01", "type": "vmess", "raw": {"name": "日本 01", "type": "vmess", "server": "jp.example.com", "port": 443, "uuid": "u-u-i-d"}},
					{"name": "美国 01", "type": "trojan", "raw": {"name": "美国 01", "type": "trojan", "server": "us.example.com", "port": 443, "password": "pw"}}
				]
			}]
		}
	}`)
	return config.LoadStore()
}

func nodeKeys3() (hk, jp, us string) {
	return config.MihomoNodeKey("sub-1", "香港 01"),
		config.MihomoNodeKey("sub-1", "日本 01"),
		config.MihomoNodeKey("sub-1", "美国 01")
}

func markHealth(m *Manager, key, status string, latency, streak int) {
	m.setNodeHealth(key, NodeHealth{
		Status:     status,
		LatencyMS:  latency,
		FailStreak: streak,
		TestedAt:   time.Now().Unix(),
	})
}

// enableAutoBridge 打开桥与自动调度开关（reconcile 生效前提）。
func enableAutoBridge(t *testing.T, store *config.Store) {
	t.Helper()
	if err := store.Update(func(c *config.Config) error {
		c.Mihomo.Enabled = true
		c.Mihomo.AutoBind = true
		return nil
	}); err != nil {
		t.Fatalf("enable auto bridge failed: %v", err)
	}
}

func accountProxyID(t *testing.T, store *config.Store, identifier string) string {
	t.Helper()
	acc, ok := store.FindAccount(identifier)
	if !ok {
		t.Fatalf("account %s not found", identifier)
	}
	return strings.TrimSpace(acc.ProxyID)
}

func TestAssignAccountsSkipsHealthFailedNodes(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	jp := config.MihomoNodeKey("sub-1", "日本 01")
	markHealth(mgr, jp, NodeHealthFail, 0, 1)

	bound, err := mgr.AssignAccounts(context.Background(), []string{jp, hk})
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if bound != 2 {
		t.Fatalf("expected 2 bound, got %d", bound)
	}
	pa := proxyIDOf(t, store, "a@test.com")
	pb := proxyIDOf(t, store, "b@test.com")
	if pa != config.MihomoManagedProxyID(hk) || pb != config.MihomoManagedProxyID(hk) {
		t.Fatalf("failed node must be skipped: a=%s b=%s", pa, pb)
	}
	if _, ok := store.Snapshot().Mihomo.PortMap[jp]; ok {
		t.Fatal("failed node should not get a port allocation")
	}
}

func TestReconcileFailoverMovesAccountOffDeadNode(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", jp); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)

	markHealth(mgr, hk, NodeHealthFail, 0, 5) // 连续失败达到阈值
	markHealth(mgr, jp, NodeHealthOK, 120, 0)

	if !mgr.reconcileBindings() {
		t.Fatal("expected reconcile to apply failover")
	}
	us := config.MihomoNodeKey("sub-1", "美国 01")
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(us) {
		t.Errorf("a@test.com should fail over to 美国 01, got %q", got)
	}
	if got := accountProxyID(t, store, "b@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Errorf("b@test.com should stay on healthy node, got %q", got)
	}
}

func TestReconcileIgnoresSingleFlap(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", jp); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)

	// 单次失败未达阈值：不触发故障转移。
	markHealth(mgr, hk, NodeHealthFail, 0, 1)
	if mgr.reconcileBindings() {
		t.Fatal("single flap should not trigger failover")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(hk) {
		t.Errorf("binding should be unchanged, got %q", got)
	}
}

func TestReconcileBackfillNewlyEnabledAccount(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)
	markHealth(mgr, hk, NodeHealthOK, 80, 0)
	markHealth(mgr, jp, NodeHealthOK, 90, 0)

	// b@test.com 相当于弹性号池刚补位上线的账号：无代理、已启用。
	if !mgr.reconcileBindings() {
		t.Fatal("expected reconcile to backfill unbound account")
	}
	if got := accountProxyID(t, store, "b@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Errorf("backfill should pick least-loaded healthy node, got %q", got)
	}
	if got := accountProxyID(t, store, "c@test.com"); got != "" {
		t.Errorf("disabled account should not be backfilled, got %q", got)
	}
	if got := accountProxyID(t, store, "d@test.com"); got != "manual-1" {
		t.Errorf("manual proxy overwritten: %q", got)
	}
}

func TestReconcileKeepsBindingsWhenNoHealthyCandidate(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, us := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", jp); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)

	markHealth(mgr, hk, NodeHealthFail, 0, 5)
	markHealth(mgr, jp, NodeHealthFail, 0, 5)
	markHealth(mgr, us, NodeHealthFail, 0, 5)
	if mgr.reconcileBindings() {
		t.Fatal("reconcile should be no-op without healthy candidates")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(hk) {
		t.Errorf("binding should be kept when no backup available, got %q", got)
	}
}

func TestReconcileRequiresAutoBind(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", jp); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	// 只开桥、不开 auto_bind：不做任何自动调度。
	if err := store.Update(func(c *config.Config) error {
		c.Mihomo.Enabled = true
		return nil
	}); err != nil {
		t.Fatalf("enable bridge failed: %v", err)
	}
	markHealth(mgr, hk, NodeHealthFail, 0, 5)
	if mgr.reconcileBindings() {
		t.Fatal("reconcile must be gated by auto_bind")
	}
}

func TestReconcileSkipsManuallyUnboundAccount(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	// b 绑定到 hk（避免 reconcile 为 b 补位）；a 绑定后显式解绑（NoProxy）。
	if err := mgr.BindAccount(context.Background(), "b@test.com", hk); err != nil {
		t.Fatalf("bind b failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind a failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "a@test.com", ""); err != nil {
		t.Fatalf("unbind a failed: %v", err)
	}
	acc, ok := store.FindAccount("a@test.com")
	if !ok || !acc.NoProxy {
		t.Fatal("manual unbind must set no_proxy")
	}
	enableAutoBridge(t, store)
	markHealth(mgr, jp, NodeHealthOK, 50, 0)

	if mgr.reconcileBindings() {
		t.Fatal("reconcile must not reassign a manually unbound account")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != "" {
		t.Fatalf("manually unbound account must stay unbound, got %q", got)
	}

	// 显式重新绑定节点后，账号重新纳入自动调度。
	if err := mgr.BindAccount(context.Background(), "a@test.com", jp); err != nil {
		t.Fatalf("rebind failed: %v", err)
	}
	acc, _ = store.FindAccount("a@test.com")
	if acc.NoProxy {
		t.Fatal("explicit rebind must clear no_proxy")
	}
}

func TestAssignAccountsSkipsManuallyUnboundAccount(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "b@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", ""); err != nil {
		t.Fatalf("unbind failed: %v", err)
	}
	markHealth(mgr, jp, NodeHealthOK, 40, 0)

	bound, err := mgr.AssignAccounts(context.Background(), []string{jp})
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if bound != 1 {
		t.Fatalf("expected 1 bound (manually unbound account skipped), got %d", bound)
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Fatalf("a@test.com should be assigned, got %q", got)
	}
	if got := accountProxyID(t, store, "b@test.com"); got != "" {
		t.Fatalf("manually unbound account must be skipped by one-click assign, got %q", got)
	}
}

func TestRequestReconcileEnqueuesNonBlocking(t *testing.T) {
	mgr := NewManager(newAutoBindTestStore(t), nil)
	mgr.RequestReconcile()
	mgr.RequestReconcile() // 二次请求应被缓冲（容量 1）去抖，不阻塞
	select {
	case <-mgr.reconcileCh:
	default:
		t.Fatal("expected one reconcile signal after RequestReconcile")
	}
	select {
	case <-mgr.reconcileCh:
		t.Fatal("reconcile channel must be deduped to a single pending signal")
	default:
	}
	// nil manager / 未初始化通道应安全跳过。
	var nilMgr *Manager
	nilMgr.RequestReconcile()
}

func TestListNodesExposesHealth(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, _, _ := nodeKeys3()
	markHealth(mgr, hk, NodeHealthOK, 88, 0)

	nodes := mgr.ListNodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	for _, node := range nodes {
		if node["node_key"] == hk {
			if node["health"] != NodeHealthOK || node["latency_ms"] != 88 {
				t.Errorf("health not merged: %v", node)
			}
			return
		}
	}
	t.Fatal("node not found in ListNodes")
}

func TestHealthSummaryCounts(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()
	markHealth(mgr, hk, NodeHealthOK, 80, 0)
	markHealth(mgr, jp, NodeHealthFail, 0, 2)

	keys := []string{hk, jp, config.MihomoNodeKey("sub-1", "美国 01")}
	summary := mgr.healthSummary(keys)
	if summary["available"] != 1 || summary["dead"] != 1 || summary["unknown"] != 1 {
		t.Errorf("unexpected summary: %v", summary)
	}
}

func TestAssignAccountsSkipsDisabledAccounts(t *testing.T) {
	store := newBindingTestStore(t)
	if err := store.Update(func(c *config.Config) error {
		c.Accounts = append(c.Accounts, config.Account{Email: "c@test.com", Password: "x", Disabled: true})
		return nil
	}); err != nil {
		t.Fatalf("seed disabled account failed: %v", err)
	}
	mgr := NewManager(store, nil)
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	jp := config.MihomoNodeKey("sub-1", "日本 01")

	bound, err := mgr.AssignAccounts(context.Background(), []string{jp, hk})
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if bound != 2 {
		t.Fatalf("expected 2 bound (enabled only), got %d", bound)
	}
	if got := proxyIDOf(t, store, "a@test.com"); got == "" {
		t.Fatal("enabled account should be bound")
	}
	if got := proxyIDOf(t, store, "c@test.com"); got != "" {
		t.Errorf("disabled account must not be bound, got %q", got)
	}
}

func TestReconcileUnbindsDisabledAccount(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, us := nodeKeys3()

	// 绑定两个启用账号 + 一个已禁用账号，模拟禁用账号残留的托管绑定。
	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind a failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", jp); err != nil {
		t.Fatalf("bind b failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "c@test.com", us); err != nil {
		t.Fatalf("bind disabled account failed: %v", err)
	}
	enableAutoBridge(t, store)
	markHealth(mgr, jp, NodeHealthOK, 50, 0)

	if !mgr.reconcileBindings() {
		t.Fatal("expected reconcile to release disabled account binding")
	}
	if got := accountProxyID(t, store, "c@test.com"); got != "" {
		t.Errorf("disabled account binding must be released, got %q", got)
	}
	// 释放的节点端口被 GC 回收（该节点不再被任何账号引用）。
	if _, ok := store.Snapshot().Mihomo.PortMap[us]; ok {
		t.Error("released port should be reclaimed")
	}
	// 启用账号的绑定不受影响。
	if accountProxyID(t, store, "a@test.com") == "" || accountProxyID(t, store, "b@test.com") == "" {
		t.Error("enabled accounts must keep their bindings")
	}
}

func TestReconcileFailoverCooldownBlocksRepeatedMove(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, us := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)

	// 第一轮：hk 死掉，a 应安全转移到 jp，并进入换号冷却期。
	markHealth(mgr, hk, NodeHealthFail, 0, 5)
	markHealth(mgr, jp, NodeHealthOK, 50, 0)
	markHealth(mgr, us, NodeHealthOK, 60, 0)
	if !mgr.reconcileBindings() {
		t.Fatal("expected first reconcile to failover")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Fatalf("a@test.com should move to 日本 01, got %q", got)
	}
	acc, _ := store.FindAccount("a@test.com")
	if acc.NodeCooldownUntil <= float64(time.Now().Unix()) {
		t.Fatalf("failover must set node cooldown, got %v", acc.NodeCooldownUntil)
	}

	// 冷却期内：jp 也死掉，a 不应再被自动切换（避免反复横跳）。
	markHealth(mgr, jp, NodeHealthFail, 0, 5)
	if mgr.reconcileBindings() {
		t.Fatal("reconcile should not move account during cooldown")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Fatalf("account must stay put during cooldown, got %q", got)
	}

	// 冷却到期后：再次发现死节点，应允许转移到新的健康节点。
	if err := store.Update(func(c *config.Config) error {
		for i := range c.Accounts {
			if c.Accounts[i].Email == "a@test.com" {
				c.Accounts[i].NodeCooldownUntil = 0
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("clear cooldown failed: %v", err)
	}
	if !mgr.reconcileBindings() {
		t.Fatal("expected reconcile to failover after cooldown expires")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(us) {
		t.Fatalf("a@test.com should move to 美国 01 after cooldown, got %q", got)
	}
}

func TestReportUpstreamFailureMarksNodeAndTriggersReconcile(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, jp, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)
	markHealth(mgr, jp, NodeHealthOK, 50, 0)
	t.Setenv("DS2API_MIHOMO_FAIL_THRESHOLD", "1")

	// 单次真实请求失败即达到阈值：节点应被立即标 fail。
	proxyID := accountProxyID(t, store, "a@test.com")
	mgr.ReportUpstreamResult(proxyID, false)
	if h := mgr.NodeHealthMap()[hk]; h.Status != NodeHealthFail {
		t.Fatalf("node should be marked fail by real traffic, got %+v", h)
	}

	// watcher 消费触发信号后立即调和：账号转移到健康节点，不等下一拍巡检。
	if !mgr.reconcileBindings() {
		t.Fatal("expected immediate reconcile after real failure feedback")
	}
	if got := accountProxyID(t, store, "a@test.com"); got != config.MihomoManagedProxyID(jp) {
		t.Fatalf("account should fail over immediately, got %q", got)
	}
}

func TestReportUpstreamSuccessResetsRealFailFeedback(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	hk, _, _ := nodeKeys3()

	if err := mgr.BindAccount(context.Background(), "a@test.com", hk); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	enableAutoBridge(t, store)
	t.Setenv("DS2API_MIHOMO_FAIL_THRESHOLD", "2")

	proxyID := accountProxyID(t, store, "a@test.com")
	mgr.ReportUpstreamResult(proxyID, false) // streak=1
	mgr.ReportUpstreamResult(proxyID, true)  // 成功清零
	mgr.ReportUpstreamResult(proxyID, false) // streak=1，未达阈值

	if h := mgr.NodeHealthMap()[hk]; h.Status == NodeHealthFail {
		t.Fatalf("successful request must reset real failure feedback, got %+v", h)
	}
}

func TestReportUpstreamIgnoresNonManagedProxy(t *testing.T) {
	store := newAutoBindTestStore(t)
	mgr := NewManager(store, nil)
	t.Setenv("DS2API_MIHOMO_FAIL_THRESHOLD", "1")

	// 手动代理 ID 不属于 mihomo 托管：不得影响任何节点健康。
	mgr.ReportUpstreamResult("manual-1", false)
	for key, h := range mgr.NodeHealthMap() {
		if h.Status == NodeHealthFail {
			t.Fatalf("non-managed proxy must be ignored, node %s got %+v", key, h)
		}
	}
}

func TestSweepTargetsSweepsAllUnboundNodes(t *testing.T) {
	store := newAutoBindTestStore(t)
	snap := store.Snapshot()

	targets := sweepTargets(snap, map[string]NodeHealth{})
	if len(targets) != 3 {
		t.Fatalf("unbound nodes should all be swept, got %d: %v", len(targets), targets)
	}
}

func TestSweepTargetsCapsColdProbeSample(t *testing.T) {
	store := newAutoBindTestStore(t)
	if err := store.Update(func(c *config.Config) error {
		nodes := c.Mihomo.Subscriptions[0].Nodes
		for i := 0; i < 20; i++ {
			name := fmt.Sprintf("冷节点 %02d", i)
			nodes = append(nodes, config.MihomoNode{Name: name, Type: "ss", Raw: map[string]any{
				"name": name, "type": "ss", "server": "cold.example.com", "port": 8388,
			}})
		}
		c.Mihomo.Subscriptions[0].Nodes = nodes
		return nil
	}); err != nil {
		t.Fatalf("seed nodes failed: %v", err)
	}

	targets := sweepTargets(store.Snapshot(), map[string]NodeHealth{})
	if len(targets) != coldProbePerTick {
		t.Fatalf("cold probe should be capped at %d, got %d", coldProbePerTick, len(targets))
	}
}

func TestTestLatencyPopulatesHealthPool(t *testing.T) {
	srv := newFakeController(map[string]int64{"香港 01": 120})
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	mgr := runningMgr(newDelayTestStore(t, port))
	if _, err := mgr.TestLatency(context.Background()); err != nil {
		t.Fatalf("TestLatency failed: %v", err)
	}
	health := mgr.NodeHealthMap()
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	slow := config.MihomoNodeKey("sub-1", "超时节点")
	if h := health[hk]; h.Status != NodeHealthOK || h.LatencyMS != 120 {
		t.Errorf("healthy node not recorded: %+v", h)
	}
	if h := health[slow]; h.Status != NodeHealthFail {
		t.Errorf("failed node not recorded as fail: %+v", h)
	}
}

func TestTestLatencyPersistsHealthToConfig(t *testing.T) {
	srv := newFakeController(nil)
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	store := newDelayTestStore(t, port)
	mgr := runningMgr(store)
	if _, err := mgr.TestLatency(context.Background()); err != nil {
		t.Fatalf("TestLatency failed: %v", err)
	}
	// 延迟直接写在订阅节点字段上。
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	slow := config.MihomoNodeKey("sub-1", "超时节点")
	nodes := map[string]config.MihomoNode{}
	for _, sub := range store.Snapshot().Mihomo.Subscriptions {
		for _, node := range sub.Nodes {
			nodes[config.MihomoNodeKey(sub.ID, node.Name)] = node
		}
	}
	if n := nodes[hk]; n.Status != NodeHealthOK || n.LatencyMS != 50 {
		t.Errorf("ok node latency not persisted on node: %+v", n)
	}
	if n := nodes[slow]; n.Status != NodeHealthFail {
		t.Errorf("fail node status not persisted on node: %+v", n)
	}
}
