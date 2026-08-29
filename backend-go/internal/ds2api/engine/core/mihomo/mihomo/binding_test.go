package mihomo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// newBindingTestStore 构造一个内存态配置仓库（env-backed，不写磁盘）。
// Mihomo.Enabled 保持 false，使 Apply 走“停止进程”分支，测试不会拉起
// 真实的 mihomo 子进程。
func newBindingTestStore(t *testing.T) *config.Store {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys": ["k1"],
		"accounts": [
			{"email": "a@test.com", "password": "x"},
			{"email": "b@test.com", "password": "x"}
		],
		"mihomo": {
			"enabled": false,
			"subscriptions": [{
				"id": "sub-1",
				"name": "测试机场",
				"url": "https://example.com/sub",
				"nodes": [
					{"name": "香港 01", "type": "ss", "raw": {"name": "香港 01", "type": "ss", "server": "hk.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "pw"}},
					{"name": "日本 01", "type": "vmess", "raw": {"name": "日本 01", "type": "vmess", "server": "jp.example.com", "port": 443, "uuid": "u-u-i-d"}}
				]
			}]
		}
	}`)
	return config.LoadStore()
}

func TestBindAccountAllocatesPortAndProxy(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")

	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	snap := store.Snapshot()
	proxyID := config.MihomoManagedProxyID(nodeKey)

	var bound *config.Account
	for i := range snap.Accounts {
		if snap.Accounts[i].Email == "a@test.com" {
			bound = &snap.Accounts[i]
		}
	}
	if bound == nil || bound.ProxyID != proxyID {
		t.Fatalf("account proxy binding missing: %+v", bound)
	}
	if got := snap.Mihomo.PortMap[nodeKey]; got != config.DefaultMihomoBasePort {
		t.Fatalf("unexpected port allocation: %d", got)
	}
	var managed *config.Proxy
	for i := range snap.Proxies {
		if snap.Proxies[i].ID == proxyID {
			managed = &snap.Proxies[i]
		}
	}
	if managed == nil {
		t.Fatal("managed proxy not created")
	}
	if managed.Type != "socks5" || managed.Host != "127.0.0.1" || managed.Port != config.DefaultMihomoBasePort {
		t.Fatalf("managed proxy mismatch: %+v", managed)
	}
}

func TestBindAccountSecondNodeGetsNextPort(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	key1 := config.MihomoNodeKey("sub-1", "香港 01")
	key2 := config.MihomoNodeKey("sub-1", "日本 01")

	if err := mgr.BindAccount(context.Background(), "a@test.com", key1); err != nil {
		t.Fatalf("bind 1 failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "b@test.com", key2); err != nil {
		t.Fatalf("bind 2 failed: %v", err)
	}
	snap := store.Snapshot()
	if snap.Mihomo.PortMap[key1] != config.DefaultMihomoBasePort {
		t.Fatalf("port1 mismatch: %d", snap.Mihomo.PortMap[key1])
	}
	if snap.Mihomo.PortMap[key2] != config.DefaultMihomoBasePort+1 {
		t.Fatalf("port2 mismatch: %d", snap.Mihomo.PortMap[key2])
	}
}

func TestUnbindAccountReclaimsPortAndProxy(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")

	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "a@test.com", ""); err != nil {
		t.Fatalf("unbind failed: %v", err)
	}
	snap := store.Snapshot()
	if snap.Accounts[0].ProxyID != "" {
		t.Fatalf("proxy_id not cleared: %q", snap.Accounts[0].ProxyID)
	}
	if !snap.Accounts[0].NoProxy {
		t.Fatal("explicit unbind must set no_proxy")
	}
	if len(snap.Proxies) != 0 {
		t.Fatalf("managed proxy not reclaimed: %+v", snap.Proxies)
	}
	if len(snap.Mihomo.PortMap) != 0 {
		t.Fatalf("port allocation not reclaimed: %+v", snap.Mihomo.PortMap)
	}
}

func TestUpdateSettingsDisableClearsManagedBindings(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	// 关闭代理桥应清除账号托管绑定并回收托管代理与端口，且不得置 no_proxy
	// （与显式解绑语义不同：桥关闭后重新开启仍应允许自动调度分配）。
	if err := mgr.UpdateSettings(context.Background(), false, "", 0, 0, false); err != nil {
		t.Fatalf("disable bridge failed: %v", err)
	}
	snap := store.Snapshot()
	if snap.Accounts[0].ProxyID != "" {
		t.Fatalf("managed proxy_id not cleared after disable: %q", snap.Accounts[0].ProxyID)
	}
	if snap.Accounts[0].NoProxy {
		t.Fatal("disable bridge must not set no_proxy")
	}
	if len(snap.Proxies) != 0 {
		t.Fatalf("managed proxy not reclaimed after disable: %+v", snap.Proxies)
	}
	if len(snap.Mihomo.PortMap) != 0 {
		t.Fatalf("port allocation not reclaimed after disable: %+v", snap.Mihomo.PortMap)
	}
}

func TestUpdateSettingsDisableKeepsManualProxy(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	// 给账号 b 配一个手动（非托管）代理并注册到 proxies。
	if err := store.Update(func(c *config.Config) error {
		c.Proxies = append(c.Proxies, config.Proxy{ID: "manual-1", Name: "手动代理", Type: "socks5", Host: "10.0.0.1", Port: 9000})
		c.Accounts[1].ProxyID = "manual-1"
		return nil
	}); err != nil {
		t.Fatalf("seed manual proxy failed: %v", err)
	}
	if err := mgr.UpdateSettings(context.Background(), false, "", 0, 0, false); err != nil {
		t.Fatalf("disable bridge failed: %v", err)
	}
	snap := store.Snapshot()
	// 托管绑定被清，手动代理保留。
	if snap.Accounts[0].ProxyID != "" {
		t.Fatalf("managed proxy_id not cleared: %q", snap.Accounts[0].ProxyID)
	}
	if snap.Accounts[1].ProxyID != "manual-1" {
		t.Fatalf("manual proxy_id unexpectedly cleared: %q", snap.Accounts[1].ProxyID)
	}
	// 托管代理被回收，手动代理保留。
	foundManual := false
	for _, p := range snap.Proxies {
		if p.ID == "manual-1" {
			foundManual = true
		}
		if config.IsMihomoManagedProxyID(p.ID) {
			t.Fatalf("managed proxy not reclaimed: %+v", p)
		}
	}
	if !foundManual {
		t.Fatal("manual proxy missing after disable")
	}
	if len(snap.Mihomo.PortMap) != 0 {
		t.Fatalf("port allocation not reclaimed: %+v", snap.Mihomo.PortMap)
	}
}

func TestStartIfEnabledClearsBindingsWhenBridgeDisabled(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	defer mgr.Stop()
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	// 模拟用户手动把 mihomo.enabled 改为 false 后重启：配置里 enabled 已是
	// false，但账号仍残留托管绑定，启动时应自动清除。
	mgr.StartIfEnabled()
	snap := store.Snapshot()
	if snap.Accounts[0].ProxyID != "" {
		t.Fatalf("stale managed binding not cleared on startup: %q", snap.Accounts[0].ProxyID)
	}
	if len(snap.Proxies) != 0 || len(snap.Mihomo.PortMap) != 0 {
		t.Fatalf("stale managed state not reclaimed: proxies=%+v portmap=%+v", snap.Proxies, snap.Mihomo.PortMap)
	}
}

func TestRebindSwitchesNodeAndReclaimsOld(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	key1 := config.MihomoNodeKey("sub-1", "香港 01")
	key2 := config.MihomoNodeKey("sub-1", "日本 01")

	if err := mgr.BindAccount(context.Background(), "a@test.com", key1); err != nil {
		t.Fatalf("bind 1 failed: %v", err)
	}
	if err := mgr.BindAccount(context.Background(), "a@test.com", key2); err != nil {
		t.Fatalf("rebind failed: %v", err)
	}
	snap := store.Snapshot()
	wantID := config.MihomoManagedProxyID(key2)
	if snap.Accounts[0].ProxyID != wantID {
		t.Fatalf("proxy_id not switched: %q", snap.Accounts[0].ProxyID)
	}
	if _, ok := snap.Mihomo.PortMap[key1]; ok {
		t.Fatal("old node port not reclaimed")
	}
	if len(snap.Proxies) != 1 || snap.Proxies[0].ID != wantID {
		t.Fatalf("proxies after rebind mismatch: %+v", snap.Proxies)
	}
}

func TestBindAccountRejectsUnknownNode(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	err := mgr.BindAccount(context.Background(), "a@test.com", config.MihomoNodeKey("sub-1", "不存在"))
	if err == nil {
		t.Fatal("expected error for unknown node")
	}
	if got := store.Snapshot().Accounts[0].ProxyID; got != "" {
		t.Fatalf("failed bind must not change proxy_id, got %q", got)
	}
}

func TestDeleteSubscriptionUnbindsAccounts(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if err := mgr.DeleteSubscription(context.Background(), "sub-1"); err != nil {
		t.Fatalf("delete subscription failed: %v", err)
	}
	snap := store.Snapshot()
	if len(snap.Mihomo.Subscriptions) != 0 {
		t.Fatalf("subscription not deleted: %+v", snap.Mihomo.Subscriptions)
	}
	if snap.Accounts[0].ProxyID != "" {
		t.Fatalf("binding not cleared: %q", snap.Accounts[0].ProxyID)
	}
	if len(snap.Proxies) != 0 || len(snap.Mihomo.PortMap) != 0 {
		t.Fatalf("state not reclaimed: proxies=%+v portmap=%+v", snap.Proxies, snap.Mihomo.PortMap)
	}
}

func TestAddAndRefreshSubscriptionOverHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write([]byte("proxies:\n  - {name: \"远程 01\", type: ss, server: r.example.com, port: 8388, cipher: aes-128-gcm, password: pw}\n"))
	}))
	defer server.Close()

	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"a@test.com","password":"x"}]}`)
	store := config.LoadStore()
	mgr := NewManager(store, nil)

	sub, err := mgr.AddSubscription(context.Background(), "远程机场", server.URL)
	if err != nil {
		t.Fatalf("add subscription failed: %v", err)
	}
	if !strings.HasPrefix(sub.ID, "sub-") || len(sub.Nodes) != 1 || sub.Nodes[0].Name != "远程 01" {
		t.Fatalf("unexpected subscription: %+v", sub)
	}
	// 绑定刚抓取的节点，验证端到端可用。
	nodeKey := config.MihomoNodeKey(sub.ID, "远程 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind remote node failed: %v", err)
	}
	count, err := mgr.RefreshSubscription(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("refresh node count mismatch: %d", count)
	}
	// 刷新后节点仍在，绑定必须保留。
	snap := store.Snapshot()
	if snap.Accounts[0].ProxyID == "" {
		t.Fatal("binding lost after refresh")
	}
	if got := snap.Mihomo.PortMap[nodeKey]; got != config.DefaultMihomoBasePort {
		t.Fatalf("port not stable after refresh: %d", got)
	}
}

func TestListNodesReportsBinding(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", nodeKey); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	nodes := mgr.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	var hk map[string]any
	for _, n := range nodes {
		if n["name"] == "香港 01" {
			hk = n
		}
	}
	if hk == nil {
		t.Fatal("node 香港 01 missing from list")
	}
	if hk["local_port"] != config.DefaultMihomoBasePort {
		t.Fatalf("local_port mismatch: %v", hk["local_port"])
	}
	accounts, _ := hk["accounts"].([]map[string]string)
	if len(accounts) != 1 || accounts[0]["identifier"] != "a@test.com" || !strings.Contains(accounts[0]["label"], "a@test.com") {
		t.Fatalf("bound accounts mismatch: %v", hk["accounts"])
	}
}

func proxyIDOf(t *testing.T, store *config.Store, email string) string {
	t.Helper()
	for _, acc := range store.Snapshot().Accounts {
		if acc.Email == email {
			return acc.ProxyID
		}
	}
	t.Fatalf("account %s not found", email)
	return ""
}

func TestAssignAccountsOnePerNode(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	jp := config.MihomoNodeKey("sub-1", "日本 01")

	bound, err := mgr.AssignAccounts(context.Background(), []string{jp, hk})
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if bound != 2 {
		t.Fatalf("expected 2 bound, got %d", bound)
	}
	pa := proxyIDOf(t, store, "a@test.com")
	pb := proxyIDOf(t, store, "b@test.com")
	if pa != config.MihomoManagedProxyID(jp) || pb != config.MihomoManagedProxyID(hk) {
		t.Fatalf("unexpected assignment: a=%s b=%s", pa, pb)
	}
	snap := store.Snapshot()
	if snap.Mihomo.PortMap[jp] != config.DefaultMihomoBasePort || snap.Mihomo.PortMap[hk] != config.DefaultMihomoBasePort+1 {
		t.Fatalf("port allocation mismatch: %v", snap.Mihomo.PortMap)
	}
}

func TestAssignAccountsCyclesWhenAccountsExceedNodes(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	if err := store.Update(func(c *config.Config) error {
		c.Accounts = append(c.Accounts, config.Account{Email: "c@test.com", Password: "x"})
		return nil
	}); err != nil {
		t.Fatalf("seed account failed: %v", err)
	}
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	jp := config.MihomoNodeKey("sub-1", "日本 01")

	bound, err := mgr.AssignAccounts(context.Background(), []string{jp, hk})
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if bound != 3 {
		t.Fatalf("expected 3 bound, got %d", bound)
	}
	pa := proxyIDOf(t, store, "a@test.com")
	pb := proxyIDOf(t, store, "b@test.com")
	pc := proxyIDOf(t, store, "c@test.com")
	if pa != config.MihomoManagedProxyID(jp) || pb != config.MihomoManagedProxyID(hk) || pc != config.MihomoManagedProxyID(jp) {
		t.Fatalf("unexpected cycling assignment: a=%s b=%s c=%s", pa, pb, pc)
	}
}

func TestAssignAccountsReplacesOldBindings(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	jp := config.MihomoNodeKey("sub-1", "日本 01")
	if err := mgr.BindAccount(context.Background(), "a@test.com", jp); err != nil {
		t.Fatalf("pre-bind failed: %v", err)
	}
	hk := config.MihomoNodeKey("sub-1", "香港 01")
	if _, err := mgr.AssignAccounts(context.Background(), []string{hk, jp}); err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	pa := proxyIDOf(t, store, "a@test.com")
	pb := proxyIDOf(t, store, "b@test.com")
	if pa != config.MihomoManagedProxyID(hk) || pb != config.MihomoManagedProxyID(jp) {
		t.Fatalf("old binding not replaced: a=%s b=%s", pa, pb)
	}
}

func TestAssignAccountsRejectsEmptyNodes(t *testing.T) {
	store := newBindingTestStore(t)
	mgr := NewManager(store, nil)
	if _, err := mgr.AssignAccounts(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty node list")
	}
	if _, err := mgr.AssignAccounts(context.Background(), []string{"sub-1::不存在"}); err == nil {
		t.Fatal("expected error for unknown node key")
	}
}
