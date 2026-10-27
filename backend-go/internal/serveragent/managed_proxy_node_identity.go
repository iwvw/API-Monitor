package serveragent

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/curve25519"
)

func countryFlag(countryCode string) string {
	code := strings.ToUpper(strings.TrimSpace(countryCode))
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{rune(0x1F1E6) + rune(code[0]-'A'), rune(0x1F1E6) + rune(code[1]-'A')})
}

// defaultRealityServerName 是 REALITY 握手落点的兜底域名：Cloudflare 全球
// 有 PoP，从任何地区握手延迟都低，且长期支持 TLS1.3 + H2。
const defaultRealityServerName = "www.cloudflare.com"

// realityForbiddenServerNames 是 Xray 明确警告过的落点：拿 apple/icloud 当
// REALITY 目标有被 GFW 封 IP 的风险，生成时直接替换掉。
var realityForbiddenServerNames = []string{"apple.com", "icloud.com"}

// sanitizeRealityServerName 保证 REALITY 落点可用：空值、裸 IP、以及
// Xray 警告过的域名一律回落到安全默认值，避免生成出高风险的节点。
func sanitizeRealityServerName(serverName string) string {
	name := strings.ToLower(strings.TrimSpace(serverName))
	if name == "" || net.ParseIP(name) != nil {
		return defaultRealityServerName
	}
	for _, forbidden := range realityForbiddenServerNames {
		if name == forbidden || strings.HasSuffix(name, "."+forbidden) {
			return defaultRealityServerName
		}
	}
	return name
}

// managedNodeOptions 承载生成节点时可选的接入方式与预设调优。零值等价于
// 历史行为（direct 接入 + balanced 预设），因此旧调用方无需感知预设。
type managedNodeOptions struct {
	AccessMode     string
	TunnelHostname string
	TunnelPath     string
	ConnectAddress string
	ConnectPort    int
	// Preset 是 managedProxyPresetCatalog 的 ID；空值按 balanced 处理。
	Preset string
}

// generateManagedNodeWithOptions 是带预设的节点配置生成入口。显式传入的
// serverName（面板表单输入）优先级最高，为空时才采用预设的默认 REALITY 落点。
func generateManagedNodeWithOptions(id, name, protocol, host, serverName, cert, key string, options managedNodeOptions) (string, string, string, error) {
	preset := managedProxyPresetByID(options.Preset)
	if strings.TrimSpace(serverName) == "" {
		if presetName, ok := presetString(preset.Options, "reality_server_name"); ok {
			serverName = presetName
		}
	}
	return generateNodeWithPreset(id, name, protocol, host, serverName, cert, key, options, preset)
}

// generateManagedNode 保留旧签名（可变参数），供既有调用方与测试使用。
func generateManagedNode(id, name, protocol, host, serverName, cert, key string, tunnelOptions ...interface{}) (string, string, string, error) {
	options := managedNodeOptions{AccessMode: "direct"}
	if len(tunnelOptions) > 0 {
		options.AccessMode, _ = tunnelOptions[0].(string)
	}
	if len(tunnelOptions) > 1 {
		options.TunnelHostname, _ = tunnelOptions[1].(string)
	}
	if len(tunnelOptions) > 2 {
		options.TunnelPath, _ = tunnelOptions[2].(string)
	}
	if len(tunnelOptions) > 3 {
		options.ConnectAddress, _ = tunnelOptions[3].(string)
	}
	if len(tunnelOptions) > 4 {
		options.ConnectPort, _ = tunnelOptions[4].(int)
	}
	return generateNodeWithPreset(id, name, protocol, host, serverName, cert, key, options, managedProxyPresetByID(""))
}

// generateNodeWithPreset 按预设渲染 sing-box inbound。预设只调整协议参数，
// 不改端口、协议与凭据。
func generateNodeWithPreset(id, name, protocol, host, serverName, cert, key string, options managedNodeOptions, preset managedProxyPreset) (string, string, string, error) {
	accessMode, tunnelHostname, tunnelPath := options.AccessMode, options.TunnelHostname, options.TunnelPath
	connectAddress, connectPort := options.ConnectAddress, options.ConnectPort
	if accessMode == "" {
		accessMode = "direct"
	}
	if accessMode == "cloudflare_tunnel" {
		if tunnelHostname == "" || tunnelPath == "" {
			return "", "", "", errors.New("Named Tunnel hostname and WebSocket path are required")
		}
		userID := uuid.NewString()
		inbound := map[string]interface{}{
			"type": "vless", "tag": id, "listen": "127.0.0.1", "listen_port": 0,
			"users":     []interface{}{map[string]interface{}{"uuid": userID}},
			"transport": map[string]interface{}{"type": "ws", "path": tunnelPath},
		}
		root := map[string]interface{}{"log": map[string]interface{}{"level": "warn"}, "inbounds": []interface{}{inbound}, "outbounds": []interface{}{map[string]interface{}{"type": "direct", "tag": "direct"}}}
		encoded, _ := json.Marshal(root)
		if strings.TrimSpace(connectAddress) == "" {
			connectAddress = tunnelHostname
		}
		if connectPort == 0 {
			connectPort = 443
		}
		q := url.Values{"encryption": {"none"}, "security": {"tls"}, "sni": {tunnelHostname}, "fp": {"chrome"}, "type": {"ws"}, "host": {tunnelHostname}, "path": {tunnelPath}}
		return string(encoded), fmt.Sprintf("vless://%s@%s:%d?%s#%s", userID, connectAddress, connectPort, q.Encode(), url.QueryEscape(name)), "tcp", nil
	}
	if protocol == "socks" || protocol == "http" {
		username, password, err := generatePlainProtocolCredential()
		if err != nil {
			return "", "", "", err
		}
		inbound := map[string]interface{}{
			"type": protocol, "tag": id, "listen": "::", "listen_port": 0,
			"users": []interface{}{map[string]interface{}{"username": username, "password": password}},
		}
		root := map[string]interface{}{"log": map[string]interface{}{"level": "warn"}, "inbounds": []interface{}{inbound}, "outbounds": []interface{}{map[string]interface{}{"type": "direct", "tag": "direct"}}}
		encoded, _ := json.Marshal(root)
		return string(encoded), fmt.Sprintf("%s://%s:%s@%s:0#%s", protocol, username, password, host, url.QueryEscape(name)), "tcp", nil
	}
	if protocol == "vless-reality" {
		serverName = sanitizeRealityServerName(serverName)
		private := make([]byte, 32)
		if _, err := rand.Read(private); err != nil {
			return "", "", "", err
		}
		private[0] &= 248
		private[31] &= 127
		private[31] |= 64
		public, err := curve25519.X25519(private, curve25519.Basepoint)
		if err != nil {
			return "", "", "", err
		}
		shortBytes := make([]byte, 4)
		_, _ = rand.Read(shortBytes)
		shortID := hex.EncodeToString(shortBytes)
		userID := uuid.NewString()
		privateKey := base64.RawURLEncoding.EncodeToString(private)
		publicKey := base64.RawURLEncoding.EncodeToString(public)
		inbound := map[string]interface{}{
			"type": "vless", "tag": id, "listen": "::", "listen_port": 0,
			"users": []interface{}{map[string]interface{}{"uuid": userID, "flow": "xtls-rprx-vision"}},
			"tls": map[string]interface{}{
				"enabled": true, "server_name": serverName,
				"reality": map[string]interface{}{
					"enabled":     true,
					"handshake":   map[string]interface{}{"server": serverName, "server_port": 443},
					"private_key": privateKey, "short_id": []string{shortID},
					// 预设决定时钟容差：balanced 1m、low_latency 10s。
					// 默认的 0 容忍会让手机/PC 校时漂移时握手直接被拒。
					"max_time_difference": presetStringOr(preset.Options, "reality_max_time_difference", "1m"),
				},
			},
		}
		root := map[string]interface{}{"log": map[string]interface{}{"level": "warn"}, "inbounds": []interface{}{inbound}, "outbounds": []interface{}{map[string]interface{}{"type": "direct", "tag": "direct"}}}
		encoded, _ := json.Marshal(root)
		q := url.Values{"encryption": {"none"}, "flow": {"xtls-rprx-vision"}, "security": {"reality"}, "sni": {serverName}, "fp": {"chrome"}, "pbk": {publicKey}, "sid": {shortID}, "type": {"tcp"}}
		return string(encoded), fmt.Sprintf("vless://%s@%s:0?%s#%s", userID, host, q.Encode(), url.QueryEscape(name)), "tcp", nil
	}
	if strings.TrimSpace(serverName) == "" {
		serverName = host
	}
	if !strings.Contains(cert, "BEGIN CERTIFICATE") || !strings.Contains(key, "BEGIN") {
		return "", "", "", errors.New("Hysteria2 TLS certificate generation failed")
	}
	passwordBytes := make([]byte, 24)
	_, _ = rand.Read(passwordBytes)
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	inbound := map[string]interface{}{
		"type": "hysteria2", "tag": id, "listen": "::", "listen_port": 0,
		"users": []interface{}{map[string]interface{}{"password": password}},
		// 忽略客户端上报的带宽，统一走 BBR。否则个别客户端声明 Brutal 高带宽后
		// 会挤占同一台主机的共享链路，其它客户端抖动明显上升。
		"ignore_client_bandwidth": presetBoolOr(preset.Options, "hysteria2_ignore_client_bandwidth", true),
		"tls":                     map[string]interface{}{"enabled": true, "server_name": serverName, "certificate": strings.Split(strings.TrimSpace(cert), "\n"), "key": strings.Split(strings.TrimSpace(key), "\n")},
	}
	// 伪装成普通站点：未通过认证的探测请求会被反代到目标站点，端口表现为
	// 一个正常网站。low_latency 预设显式置空以省掉一次反向代理跳转。
	if masquerade := presetStringOr(preset.Options, "hysteria2_masquerade", ""); masquerade != "" {
		inbound["masquerade"] = masquerade
	}
	// sing-box 用 up_mbps/down_mbps（整数 Mbps）声明服务端带宽上限，且与
	// ignore_client_bandwidth 互斥——两者同时设置会冲突导致节点启动失败。
	// 因此仅在预设显式声明带宽时写入，并同步关闭 BBR 开关。
	upMbps, hasUp := presetInt(preset.Options, "hysteria2_up_mbps")
	downMbps, hasDown := presetInt(preset.Options, "hysteria2_down_mbps")
	if hasUp && hasDown && upMbps > 0 && downMbps > 0 {
		delete(inbound, "ignore_client_bandwidth")
		inbound["up_mbps"] = upMbps
		inbound["down_mbps"] = downMbps
	}
	root := map[string]interface{}{"log": map[string]interface{}{"level": "warn"}, "inbounds": []interface{}{inbound}, "outbounds": []interface{}{map[string]interface{}{"type": "direct", "tag": "direct"}}}
	encoded, _ := json.Marshal(root)
	q := url.Values{"sni": {serverName}, "insecure": {"1"}}
	return string(encoded), fmt.Sprintf("hysteria2://%s@%s:0?%s#%s", password, host, q.Encode(), url.QueryEscape(name)), "udp", nil
}

// generatePlainProtocolCredential creates a username:password pair for a
// plaintext (socks/http) managed inbound. The bootstrap credential is replaced
// by per-subscription credentials when the node binds subscribers, matching
// the way vless/hysteria2 inbound users are bound.
func generatePlainProtocolCredential() (string, string, error) {
	usernameBytes := make([]byte, 16)
	if _, err := rand.Read(usernameBytes); err != nil {
		return "", "", err
	}
	passwordBytes := make([]byte, 18)
	if _, err := rand.Read(passwordBytes); err != nil {
		return "", "", err
	}
	username := base64.RawURLEncoding.EncodeToString(usernameBytes)
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	return username, password, nil
}

func generateManagedTLSCertificate(host string) (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"API Monitor Managed Node"}},
		NotBefore:    now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return string(certPEM), string(keyPEM), nil
}
