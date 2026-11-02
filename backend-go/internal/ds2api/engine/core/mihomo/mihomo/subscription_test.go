package mihomo

import (
	"encoding/base64"
	"strings"
	"testing"
)

const clashSubscriptionYAML = `
mixed-port: 7890
proxies:
  - name: "香港 01"
    type: ss
    server: hk01.example.com
    port: 8388
    cipher: aes-128-gcm
    password: "pw1"
  - name: "日本 01"
    type: vmess
    server: jp01.example.com
    port: 443
    uuid: "11111111-2222-3333-4444-555555555555"
    alterId: 0
    cipher: auto
    network: ws
    tls: true
proxy-groups:
  - name: "PROXY"
    type: select
    proxies: ["香港 01", "日本 01"]
`

func TestParseSubscriptionClashYAML(t *testing.T) {
	nodes, err := ParseSubscription([]byte(clashSubscriptionYAML))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	byName := map[string]string{}
	for _, n := range nodes {
		byName[n.Name] = n.Type
		if n.Raw["server"] == nil {
			t.Fatalf("node %s missing raw server", n.Name)
		}
	}
	if byName["香港 01"] != "ss" || byName["日本 01"] != "vmess" {
		t.Fatalf("unexpected node types: %v", byName)
	}
}

func TestParseSubscriptionBase64ClashYAML(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(clashSubscriptionYAML))
	nodes, err := ParseSubscription([]byte(encoded))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestParseSubscriptionShareLinks(t *testing.T) {
	vmessJSON := `{"ps":"VM 节点","add":"vm.example.com","port":"443","id":"a-b-c-d","aid":"0","net":"ws","path":"/ws","host":"cdn.example.com","tls":"tls","sni":"sni.example.com"}`
	links := strings.Join([]string{
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON)),
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:passw0rd")) + "@ss.example.com:8388#SS 节点",
		"trojan://toppass@tj.example.com:443?sni=tj.example.com&allowInsecure=1#Trojan 节点",
		"vless://uuid-1@vless.example.com:443?security=reality&pbk=PUBKEY&sid=ab&sni=www.microsoft.com&fp=chrome&flow=xtls-rprx-vision&type=tcp#VLESS 节点",
		"hysteria2://hy2pass@hy2.example.com:8443?sni=hy2.example.com&insecure=1#HY2 节点",
	}, "\n")

	// 直接明文形式
	nodes, err := ParseSubscription([]byte(links))
	if err != nil {
		t.Fatalf("parse plain links failed: %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(nodes))
	}

	// 整体 Base64 形式（机场常见）
	encoded := base64.StdEncoding.EncodeToString([]byte(links))
	nodes2, err := ParseSubscription([]byte(encoded))
	if err != nil {
		t.Fatalf("parse base64 links failed: %v", err)
	}
	if len(nodes2) != 5 {
		t.Fatalf("expected 5 nodes from base64, got %d", len(nodes2))
	}

	byName := map[string]map[string]any{}
	for _, n := range nodes {
		byName[n.Name] = n.Raw
	}
	if got := byName["SS 节点"]["cipher"]; got != "aes-128-gcm" {
		t.Fatalf("ss cipher mismatch: %v", got)
	}
	if got := byName["SS 节点"]["password"]; got != "passw0rd" {
		t.Fatalf("ss password mismatch: %v", got)
	}
	if got := byName["Trojan 节点"]["skip-cert-verify"]; got != true {
		t.Fatalf("trojan skip-cert-verify mismatch: %v", got)
	}
	reality, ok := byName["VLESS 节点"]["reality-opts"].(map[string]any)
	if !ok || reality["public-key"] != "PUBKEY" {
		t.Fatalf("vless reality-opts mismatch: %v", byName["VLESS 节点"])
	}
	if got := byName["HY2 节点"]["type"]; got != "hysteria2" {
		t.Fatalf("hy2 type mismatch: %v", got)
	}
	if got := byName["VM 节点"]["server"]; got != "vm.example.com" {
		t.Fatalf("vmess server mismatch: %v", got)
	}
	if got := byName["VM 节点"]["port"]; got != 443 {
		t.Fatalf("vmess port mismatch: %v (%T)", got, got)
	}
}

func TestParseSubscriptionSSPlainUserInfo(t *testing.T) {
	// SIP002 明文 userinfo 形式（method:password 不编码）
	link := "ss://YWVzLTI1Ni1nY206cHc@1.2.3.4:8388#plain"
	nodes, err := ParseSubscription([]byte(link))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Raw["cipher"] != "aes-256-gcm" || nodes[0].Raw["password"] != "pw" {
		t.Fatalf("ss userinfo mismatch: %v", nodes[0].Raw)
	}
	if nodes[0].Raw["server"] != "1.2.3.4" || nodes[0].Raw["port"] != 8388 {
		t.Fatalf("ss server mismatch: %v", nodes[0].Raw)
	}
}

func TestParseSubscriptionRejectsGarbage(t *testing.T) {
	if _, err := ParseSubscription([]byte("   \n  ")); err == nil {
		t.Fatal("expected error for empty body")
	}
	if _, err := ParseSubscription([]byte("not a subscription at all {{{")); err == nil {
		t.Fatal("expected error for garbage body")
	}
}

func TestParseSubscriptionDedupesSameName(t *testing.T) {
	yamlBody := `
proxies:
  - {name: "A", type: ss, server: 1.1.1.1, port: 1000, cipher: aes-128-gcm, password: x}
  - {name: "A", type: ss, server: 2.2.2.2, port: 2000, cipher: aes-128-gcm, password: y}
`
	nodes, err := ParseSubscription([]byte(yamlBody))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected deduped 1 node, got %d", len(nodes))
	}
	if nodes[0].Raw["server"] != "1.1.1.1" {
		t.Fatalf("expected first occurrence to win, got %v", nodes[0].Raw["server"])
	}
}
