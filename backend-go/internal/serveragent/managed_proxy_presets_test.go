package serveragent

import (
	"testing"
)

// TestManagedProxyPresetCatalogShape 保证预设目录可被前端/MCP 直接消费：
// 每个预设都有稳定的 id/名称/描述与协议适用范围，且首个为默认。
func TestManagedProxyPresetCatalogShape(t *testing.T) {
	if len(managedProxyPresetCatalog) == 0 {
		t.Fatal("preset catalog is empty")
	}
	if managedProxyPresetCatalog[0].ID != "balanced" {
		t.Fatalf("first preset must be balanced, got %q", managedProxyPresetCatalog[0].ID)
	}
	seen := map[string]bool{}
	for _, preset := range managedProxyPresetCatalog {
		if preset.ID == "" || preset.Name == "" || preset.Description == "" {
			t.Errorf("preset %#v missing id/name/description", preset)
		}
		if seen[preset.ID] {
			t.Errorf("duplicate preset id %q", preset.ID)
		}
		seen[preset.ID] = true
		if len(preset.Protocols) == 0 {
			t.Errorf("preset %q has no protocol scope", preset.ID)
		}
		if len(preset.Options) == 0 {
			t.Errorf("preset %q has no options", preset.ID)
		}
	}
}

// TestUnknownPresetFallsBackToBalanced 保证非法/空预设不会破坏节点创建。
func TestUnknownPresetFallsBackToBalanced(t *testing.T) {
	for _, id := range []string{"", "does-not-exist"} {
		preset := managedProxyPresetByID(id)
		if preset.ID != "balanced" {
			t.Errorf("managedProxyPresetByID(%q) = %q, want balanced", id, preset.ID)
		}
	}
}

// TestPresetChangesRealityClockTolerance 验证 low_latency 收紧时钟容差，
// 且默认（balanced）与历史行为一致。
func TestPresetChangesRealityClockTolerance(t *testing.T) {
	cases := []struct {
		preset string
		want   string
	}{
		{"", "1m"},
		{"balanced", "1m"},
		{"low_latency", "10s"},
	}
	for _, tc := range cases {
		raw, _, _, err := generateManagedNodeWithOptions("mnode-rs", "edge", "vless-reality", "edge.example.com", "", "", "", managedNodeOptions{Preset: tc.preset})
		if err != nil {
			t.Fatalf("preset %q: %v", tc.preset, err)
		}
		inbound := firstInbound(t, raw)
		tlsBlock, _ := inbound["tls"].(map[string]interface{})
		reality, _ := tlsBlock["reality"].(map[string]interface{})
		if got := reality["max_time_difference"]; got != tc.want {
			t.Errorf("preset %q: max_time_difference = %v, want %v", tc.preset, got, tc.want)
		}
	}
}

// TestStealthPresetOverridesRealityServerName 验证预设可替换 REALITY 落点，
// 但显式传入的 serverName 优先。
func TestStealthPresetOverridesRealityServerName(t *testing.T) {
	raw, _, _, err := generateManagedNodeWithOptions("mnode-rs", "edge", "vless-reality", "edge.example.com", "", "", "", managedNodeOptions{Preset: "stealth"})
	if err != nil {
		t.Fatal(err)
	}
	inbound := firstInbound(t, raw)
	tlsBlock, _ := inbound["tls"].(map[string]interface{})
	if got := tlsBlock["server_name"]; got != "www.microsoft.com" {
		t.Errorf("stealth server_name = %v, want www.microsoft.com", got)
	}

	explicit, _, _, err := generateManagedNodeWithOptions("mnode-rs", "edge", "vless-reality", "edge.example.com", "edge.example.com", "", "", managedNodeOptions{Preset: "stealth"})
	if err != nil {
		t.Fatal(err)
	}
	explicitTLS, _ := firstInbound(t, explicit)["tls"].(map[string]interface{})
	if got := explicitTLS["server_name"]; got != "edge.example.com" {
		t.Errorf("explicit server_name must win, got %v", got)
	}
}

// TestHysteria2PresetTuning 验证 masquerade / 带宽提示 / BBR 由预设控制。
func TestHysteria2PresetTuning(t *testing.T) {
	cert, key := testSelfSignedCertificate(t, "edge.example.com")

	balanced, _, _, err := generateManagedNodeWithOptions("mnode-hy2", "edge", "hysteria2", "edge.example.com", "edge.example.com", cert, key, managedNodeOptions{Preset: "balanced"})
	if err != nil {
		t.Fatal(err)
	}
	balancedInbound := firstInbound(t, balanced)
	if balancedInbound["masquerade"] != "https://www.bing.com" {
		t.Errorf("balanced masquerade = %v", balancedInbound["masquerade"])
	}
	if balancedInbound["ignore_client_bandwidth"] != true {
		t.Errorf("balanced must keep BBR (ignore_client_bandwidth)")
	}

	lowLatency, _, _, err := generateManagedNodeWithOptions("mnode-hy2", "edge", "hysteria2", "edge.example.com", "edge.example.com", cert, key, managedNodeOptions{Preset: "low_latency"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := firstInbound(t, lowLatency)["masquerade"]; present {
		t.Error("low_latency must drop the masquerade page")
	}

	throughput, _, _, err := generateManagedNodeWithOptions("mnode-hy2", "edge", "hysteria2", "edge.example.com", "edge.example.com", cert, key, managedNodeOptions{Preset: "throughput"})
	if err != nil {
		t.Fatal(err)
	}
	throughputInbound := firstInbound(t, throughput)
	// sing-box 语义：up_mbps/down_mbps 与 ignore_client_bandwidth 互斥，
	// 声明带宽时必须移除 BBR 开关，否则节点启动失败。
	if throughputInbound["up_mbps"] != float64(200) || throughputInbound["down_mbps"] != float64(200) {
		t.Errorf("throughput bandwidth = %v/%v, want 200/200", throughputInbound["up_mbps"], throughputInbound["down_mbps"])
	}
	if _, present := throughputInbound["ignore_client_bandwidth"]; present {
		t.Error("throughput must not set ignore_client_bandwidth alongside up_mbps/down_mbps")
	}
	if _, present := throughputInbound["bandwidth"]; present {
		t.Error("sing-box has no `bandwidth` field; use up_mbps/down_mbps")
	}
}
