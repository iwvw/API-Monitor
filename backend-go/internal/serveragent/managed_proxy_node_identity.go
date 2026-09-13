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

func generateManagedNode(id, name, protocol, host, serverName, cert, key string, tunnelOptions ...interface{}) (string, string, string, error) {
	accessMode, tunnelHostname, tunnelPath, connectAddress := "direct", "", "", ""
	connectPort := 0
	if len(tunnelOptions) > 0 {
		accessMode, _ = tunnelOptions[0].(string)
	}
	if len(tunnelOptions) > 1 {
		tunnelHostname, _ = tunnelOptions[1].(string)
	}
	if len(tunnelOptions) > 2 {
		tunnelPath, _ = tunnelOptions[2].(string)
	}
	if len(tunnelOptions) > 3 {
		connectAddress, _ = tunnelOptions[3].(string)
	}
	if len(tunnelOptions) > 4 {
		connectPort, _ = tunnelOptions[4].(int)
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
		if serverName == "" {
			serverName = "www.cloudflare.com"
		}
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
		"tls":   map[string]interface{}{"enabled": true, "server_name": serverName, "certificate": strings.Split(strings.TrimSpace(cert), "\n"), "key": strings.Split(strings.TrimSpace(key), "\n")},
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
