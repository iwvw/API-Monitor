package serveragent

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// firstInbound 从节点配置里取出唯一的 inbound，便于断言生成结果。
func firstInbound(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var root struct {
		Inbounds []map[string]interface{} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("decode node config: %v", err)
	}
	if len(root.Inbounds) != 1 {
		t.Fatalf("inbound count = %d, want 1", len(root.Inbounds))
	}
	return root.Inbounds[0]
}

// testSelfSignedCertificate 生成 hysteria2 需要的自签证书对。
func testSelfSignedCertificate(t *testing.T, host string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return string(certPEM), string(keyPEM)
}

func TestSanitizeRealityServerName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to default", "", defaultRealityServerName},
		{"blank falls back to default", "   ", defaultRealityServerName},
		{"bare ipv4 rejected", "1.2.3.4", defaultRealityServerName},
		{"bare ipv6 rejected", "2001:db8::1", defaultRealityServerName},
		{"apple rejected by xray warning", "www.apple.com", defaultRealityServerName},
		{"apple apex rejected", "apple.com", defaultRealityServerName},
		{"icloud rejected by xray warning", "gateway.icloud.com", defaultRealityServerName},
		{"lookalike domain kept", "notapple.com", "notapple.com"},
		{"normal domain kept", "www.cloudflare.com", "www.cloudflare.com"},
		{"normalised to lowercase", "WWW.Microsoft.COM", "www.microsoft.com"},
	}
	for _, tc := range cases {
		if got := sanitizeRealityServerName(tc.in); got != tc.want {
			t.Errorf("%s: sanitizeRealityServerName(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestGenerateManagedNodeHysteria2Tuning 保证 hysteria2 节点默认带上
// BBR 强制与 masquerade 伪装，这两个参数不需要客户端改动即可生效。
func TestGenerateManagedNodeHysteria2Tuning(t *testing.T) {
	cert, key := testSelfSignedCertificate(t, "edge.example.com")
	raw, uri, transport, err := generateManagedNode("mnode-hy2", "edge", "hysteria2", "edge.example.com", "edge.example.com", cert, key)
	if err != nil {
		t.Fatalf("generateManagedNode: %v", err)
	}
	if transport != "udp" {
		t.Fatalf("transport = %q, want udp", transport)
	}
	if uri == "" {
		t.Fatal("client uri is empty")
	}
	inbound := firstInbound(t, raw)
	if inbound["ignore_client_bandwidth"] != true {
		t.Errorf("ignore_client_bandwidth = %v, want true", inbound["ignore_client_bandwidth"])
	}
	if inbound["masquerade"] != "https://www.bing.com" {
		t.Errorf("masquerade = %v, want https://www.bing.com", inbound["masquerade"])
	}
}

// TestGenerateManagedNodeRealityTuning 保证 REALITY 节点带上时钟容差，
// 并把 Xray 警告过的落点替换掉。
func TestGenerateManagedNodeRealityTuning(t *testing.T) {
	raw, uri, transport, err := generateManagedNode("mnode-rs", "edge", "vless-reality", "edge.example.com", "www.apple.com", "", "")
	if err != nil {
		t.Fatalf("generateManagedNode: %v", err)
	}
	if transport != "tcp" {
		t.Fatalf("transport = %q, want tcp", transport)
	}
	if uri == "" {
		t.Fatal("client uri is empty")
	}
	inbound := firstInbound(t, raw)
	tlsBlock, _ := inbound["tls"].(map[string]interface{})
	if tlsBlock["server_name"] != defaultRealityServerName {
		t.Errorf("server_name = %v, want %v", tlsBlock["server_name"], defaultRealityServerName)
	}
	reality, _ := tlsBlock["reality"].(map[string]interface{})
	if reality["max_time_difference"] != "1m" {
		t.Errorf("max_time_difference = %v, want 1m", reality["max_time_difference"])
	}
	handshake, _ := reality["handshake"].(map[string]interface{})
	if handshake["server"] != defaultRealityServerName {
		t.Errorf("handshake.server = %v, want %v", handshake["server"], defaultRealityServerName)
	}
}
