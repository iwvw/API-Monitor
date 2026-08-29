package mihomo

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func testConfigWithBinding() config.Config {
	nodeKey := config.MihomoNodeKey("sub-1", "香港 01")
	proxyID := config.MihomoManagedProxyID(nodeKey)
	return config.Config{
		Accounts: []config.Account{
			{Email: "a@test.com", ProxyID: proxyID},
			{Email: "b@test.com"},
		},
		Proxies: []config.Proxy{
			{ID: proxyID, Name: "香港 01", Type: "socks5", Host: "127.0.0.1", Port: 10801},
		},
		Mihomo: config.MihomoConfig{
			Enabled:  true,
			BasePort: config.DefaultMihomoBasePort,
			APIPort:  config.DefaultMihomoAPIPort,
			Subscriptions: []config.MihomoSubscription{
				{
					ID:  "sub-1",
					URL: "https://example.com/sub",
					Nodes: []config.MihomoNode{
						{Name: "香港 01", Type: "ss", Raw: map[string]any{
							"name": "香港 01", "type": "ss", "server": "hk.example.com",
							"port": 8388, "cipher": "aes-128-gcm", "password": "pw",
						}},
						{Name: "日本 01", Type: "vmess", Raw: map[string]any{
							"name": "日本 01", "type": "vmess", "server": "jp.example.com",
							"port": 443, "uuid": "u-u-i-d",
						}},
					},
				},
			},
			PortMap: map[string]int{nodeKey: 10801},
		},
	}
}

func TestBuildRuntimeYAMLWithBinding(t *testing.T) {
	cfg := testConfigWithBinding()
	out, bindings, err := BuildRuntimeYAML(cfg, "test-controller-secret")
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Port != 10801 || bindings[0].NodeName != "香港 01" {
		t.Fatalf("unexpected bindings: %+v", bindings)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated yaml invalid: %v\n%s", err, out)
	}
	if got := doc["external-controller"]; got != "127.0.0.1:19090" {
		t.Fatalf("external-controller mismatch: %v", got)
	}
	if got := doc["secret"]; got != "test-controller-secret" {
		t.Fatalf("external-controller secret missing or wrong: %v", got)
	}
	proxies, ok := doc["proxies"].([]any)
	if !ok || len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %v", doc["proxies"])
	}
	// proxy 名应使用 "subID::nodeName" 限定名，避免跨订阅同名节点冲突。
	proxy0 := proxies[0].(map[string]any)
	if proxy0["name"] != config.MihomoNodeKey("sub-1", "香港 01") {
		t.Fatalf("proxy name must be scoped, got %v", proxy0["name"])
	}
	listeners, ok := doc["listeners"].([]any)
	if !ok || len(listeners) != 1 {
		t.Fatalf("expected 1 listener, got %v", doc["listeners"])
	}
	listener := listeners[0].(map[string]any)
	if listener["port"] != 10801 || listener["proxy"] != config.MihomoNodeKey("sub-1", "香港 01") || listener["type"] != "socks" || listener["listen"] != "127.0.0.1" {
		t.Fatalf("listener mismatch: %v", listener)
	}
}

func TestBuildRuntimeYAMLSameNodeNameAcrossSubs(t *testing.T) {
	nodeKey1 := config.MihomoNodeKey("sub-1", "香港 01")
	nodeKey2 := config.MihomoNodeKey("sub-2", "香港 01")
	cfg := config.Config{
		Accounts: []config.Account{
			{Email: "a@test.com", ProxyID: config.MihomoManagedProxyID(nodeKey1)},
			{Email: "b@test.com", ProxyID: config.MihomoManagedProxyID(nodeKey2)},
		},
		Proxies: []config.Proxy{
			{ID: config.MihomoManagedProxyID(nodeKey1), Name: "香港 01", Type: "socks5", Host: "127.0.0.1", Port: 10801},
			{ID: config.MihomoManagedProxyID(nodeKey2), Name: "香港 01", Type: "socks5", Host: "127.0.0.1", Port: 10802},
		},
		Mihomo: config.MihomoConfig{
			Enabled:  true,
			BasePort: config.DefaultMihomoBasePort,
			APIPort:  config.DefaultMihomoAPIPort,
			Subscriptions: []config.MihomoSubscription{
				{ID: "sub-1", URL: "https://a.example.com", Nodes: []config.MihomoNode{
					{Name: "香港 01", Type: "ss", Raw: map[string]any{"name": "香港 01", "type": "ss", "server": "a.example.com", "port": 8388}},
				}},
				{ID: "sub-2", URL: "https://b.example.com", Nodes: []config.MihomoNode{
					{Name: "香港 01", Type: "ss", Raw: map[string]any{"name": "香港 01", "type": "ss", "server": "b.example.com", "port": 8388}},
				}},
			},
			PortMap: map[string]int{nodeKey1: 10801, nodeKey2: 10802},
		},
	}
	out, bindings, err := BuildRuntimeYAML(cfg, "test-controller-secret")
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %+v", bindings)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated yaml invalid: %v", err)
	}
	proxies := doc["proxies"].([]any)
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies (no name dedup across subs), got %v", doc["proxies"])
	}
	proxyName := func(p any) string {
		m, _ := p.(map[string]any)
		s, _ := m["name"].(string)
		return s
	}
	if proxyName(proxies[0]) != nodeKey1 || proxyName(proxies[1]) != nodeKey2 {
		t.Fatalf("proxy names must be scoped per subscription: %s, %s", proxyName(proxies[0]), proxyName(proxies[1]))
	}
	// 两个 listener 各直出各自的限定 proxy。
	listeners := doc["listeners"].([]any)
	if len(listeners) != 2 {
		t.Fatalf("expected 2 listeners, got %v", doc["listeners"])
	}
	seen := map[any]bool{}
	for _, l := range listeners {
		lm, _ := l.(map[string]any)
		seen[lm["proxy"]] = true
	}
	if !seen[nodeKey1] || !seen[nodeKey2] {
		t.Fatalf("listeners must reference distinct scoped proxies, got %v", seen)
	}
}

func TestBuildRuntimeYAMLNoBindings(t *testing.T) {
	cfg := testConfigWithBinding()
	// 去掉账号引用后，不应再生成 listener。
	cfg.Accounts[0].ProxyID = ""
	out, bindings, err := BuildRuntimeYAML(cfg, "test-controller-secret")
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no bindings, got %+v", bindings)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated yaml invalid: %v\n%s", err, out)
	}
	if listeners, ok := doc["listeners"].([]any); !ok || len(listeners) != 0 {
		t.Fatalf("expected empty listeners, got %v", doc["listeners"])
	}
	// 节点列表仍完整输出（与绑定无关）。
	if proxies, ok := doc["proxies"].([]any); !ok || len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %v", doc["proxies"])
	}
}

func TestCollectActiveBindingsSkipsStaleState(t *testing.T) {
	cfg := testConfigWithBinding()

	// 托管代理被手工删除：不再生成 listener。
	withoutProxy := cfg.Clone()
	withoutProxy.Proxies = nil
	if bindings := collectActiveBindings(withoutProxy); len(bindings) != 0 {
		t.Fatalf("expected no bindings when managed proxy missing, got %+v", bindings)
	}

	// 节点从订阅中消失：不再生成 listener。
	withoutNode := cfg.Clone()
	withoutNode.Mihomo.Subscriptions[0].Nodes = withoutNode.Mihomo.Subscriptions[0].Nodes[1:]
	if bindings := collectActiveBindings(withoutNode); len(bindings) != 0 {
		t.Fatalf("expected no bindings when node missing, got %+v", bindings)
	}

	// sanity：原始配置仍能产出绑定。
	if bindings := collectActiveBindings(cfg); len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %+v", bindings)
	}
}

func TestAllocateMihomoPortSkipsUsed(t *testing.T) {
	m := config.MihomoConfig{BasePort: config.DefaultMihomoBasePort}
	p1 := m.AllocateMihomoPort("sub-1::A")
	p2 := m.AllocateMihomoPort("sub-1::B")
	if p1 != config.DefaultMihomoBasePort || p2 != config.DefaultMihomoBasePort+1 {
		t.Fatalf("unexpected allocation: %d, %d", p1, p2)
	}
	if again := m.AllocateMihomoPort("sub-1::A"); again != p1 {
		t.Fatalf("allocation not stable: %d vs %d", again, p1)
	}
	if !strings.HasPrefix(config.MihomoManagedProxyID("sub-1::A"), config.MihomoManagedProxyPrefix) {
		t.Fatal("managed proxy id prefix mismatch")
	}
}

// TestAllocateMihomoPortSkipsAPIPort 回归：api_port 落在节点端口区间内时，
// 后续分配必须跳过该端口，否则第 N+1 个绑定会撞上 mihomo 自身监听的
// external-controller 端口，导致下一次 Apply 因端口被占用而失败。
func TestAllocateMihomoPortSkipsAPIPort(t *testing.T) {
	m := config.MihomoConfig{BasePort: config.DefaultMihomoBasePort, APIPort: config.DefaultMihomoBasePort + 2}
	// 分配数量越过 api_port（base+2）后，后续分配不得返回 api_port。
	seen := map[int]struct{}{}
	for i := 0; i < 5; i++ {
		p := m.AllocateMihomoPort(fmt.Sprintf("sub-1::N%02d", i))
		if p == m.APIPort {
			t.Fatalf("allocation must never reuse api_port %d, got %d", m.APIPort, p)
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate port allocation %d", p)
		}
		seen[p] = struct{}{}
	}
	// 已有分配且等于 api_port 的历史脏数据（理论上被校验拒绝）不在此场景。
}

// TestSplitMihomoNodeKeyWithColonsInNodeName 回归：节点名允许包含 "::"，
// 反向拆解必须按首个 "::" 分隔并完整保留节点名，不得截断。
func TestSplitMihomoNodeKeyWithColonsInNodeName(t *testing.T) {
	nodeName := "香港::双线 01"
	key := config.MihomoNodeKey("sub-1", nodeName)
	sub, name := config.SplitMihomoNodeKey(key)
	if sub != "sub-1" || name != nodeName {
		t.Fatalf("split mismatch: sub=%q name=%q", sub, name)
	}
	if sub, name := config.SplitMihomoNodeKey("no-delimiter"); sub != "" || name != "" {
		t.Fatalf("expected empty split for malformed key, got %q %q", sub, name)
	}
	if sub, name := config.SplitMihomoNodeKey("sub-1::"); sub != "" || name != "" {
		t.Fatalf("expected empty split for empty node name, got %q %q", sub, name)
	}
}
