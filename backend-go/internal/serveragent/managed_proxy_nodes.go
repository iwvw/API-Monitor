package serveragent

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"golang.org/x/crypto/curve25519"
)

func isMissingTableError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

type managedProxyNode struct {
	ID                 string                    `json:"id"`
	ServerID           string                    `json:"server_id"`
	ServerName         string                    `json:"server_name"`
	Name               string                    `json:"name"`
	Protocol           string                    `json:"protocol"`
	Runtime            string                    `json:"runtime"`
	PublicHost         string                    `json:"public_host"`
	AssignedPort       int                       `json:"assigned_port"`
	StatsPort          int                       `json:"stats_port"`
	Transport          string                    `json:"transport"`
	ClientURI          string                    `json:"client_uri"`
	Revision           int64                     `json:"revision"`
	Enabled            bool                      `json:"enabled"`
	Stable             bool                      `json:"stable"`
	Publishable        bool                      `json:"publishable"`
	ApplyStatus        string                    `json:"apply_status"`
	LastError          string                    `json:"last_error"`
	ObservedStatus     string                    `json:"observed_status"`
	ObservedRevision   int64                     `json:"observed_revision"`
	ObservedPort       int                       `json:"observed_port"`
	ObservedAt         string                    `json:"observed_at"`
	HealthStatus       string                    `json:"health_status"`
	AccessMode         string                    `json:"access_mode"`
	TunnelPath         string                    `json:"tunnel_path"`
	PreferredAddressID string                    `json:"preferred_address_id"`
	ConnectAddress     string                    `json:"connect_address"`
	ConnectPort        int                       `json:"connect_port"`
	TunnelHostname     string                    `json:"tunnel_hostname"`
	Quality            []managedProxyNodeQuality `json:"quality,omitempty"`
	CreatedAt          string                    `json:"created_at"`
	UpdatedAt          string                    `json:"updated_at"`
}

type managedProxyNodeQuality struct {
	Name         string  `json:"name"`
	LatencyMS    float64 `json:"latency_ms"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	LossRate     float64 `json:"loss_rate"`
	SampledAt    string  `json:"sampled_at"`
}

func (s *Service) handleManagedProxyNodes(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	switch {
	case id == "" && r.Method == http.MethodGet:
		s.listManagedProxyNodes(w, r, db)
	case id == "" && r.Method == http.MethodPost:
		s.createManagedProxyNode(w, r, db)
	case id != "" && r.Method == http.MethodDelete:
		s.deleteManagedProxyNode(w, r, db, id)
	case id != "" && r.Method == http.MethodPut:
		s.updateManagedProxyNodeState(w, r, db, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) updateManagedProxyNodeState(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input struct {
		Enabled            *bool   `json:"enabled"`
		Stable             *bool   `json:"stable"`
		Name               string  `json:"name"`
		PreferredAddressID *string `json:"preferred_address_id"`
		ConnectAddress     *string `json:"connect_address"`
		ConnectPort        *int    `json:"connect_port"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid internal node update")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	metadataUpdated := false
	if input.Name != "" {
		var clientEncrypted string
		if err := db.QueryRowContext(r.Context(), `SELECT client_uri_encrypted FROM managed_proxy_nodes WHERE id=?`, id).Scan(&clientEncrypted); err != nil {
			if err == sql.ErrNoRows {
				response.Error(w, http.StatusNotFound, "internal node not found")
			} else {
				response.Error(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		client := secure.SecureDecrypt(clientEncrypted)
		if parsed, err := url.Parse(client); err == nil {
			parsed.Fragment = input.Name
			client = parsed.String()
		}
		updatedClient, err := secure.SecureEncrypt(client)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		result, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET name=?,client_uri_encrypted=?,updated_at=datetime('now') WHERE id=?`, input.Name, updatedClient, id)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				response.Error(w, http.StatusConflict, "this server already has a node with that name")
			} else {
				response.Error(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			response.Error(w, http.StatusNotFound, "internal node not found")
			return
		}
		metadataUpdated = true
	}
	if input.PreferredAddressID != nil || input.ConnectAddress != nil || input.ConnectPort != nil {
		var preferredID, connectAddress string
		var connectPort int
		if err := db.QueryRowContext(r.Context(), `SELECT preferred_address_id,connect_address,connect_port FROM managed_proxy_nodes WHERE id=?`, id).Scan(&preferredID, &connectAddress, &connectPort); err != nil {
			response.Error(w, http.StatusNotFound, "internal node not found")
			return
		}
		if input.PreferredAddressID != nil {
			preferredID = strings.TrimSpace(*input.PreferredAddressID)
			if preferredID != "" {
				var exists int
				if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM managed_proxy_preferences WHERE id=? AND enabled=1`, preferredID).Scan(&exists); err != nil {
					response.Error(w, http.StatusBadRequest, "preferred address is unavailable")
					return
				}
			}
		}
		if input.ConnectAddress != nil {
			connectAddress = strings.TrimSpace(*input.ConnectAddress)
			if strings.ContainsAny(connectAddress, "/?#@ ") {
				response.Error(w, http.StatusBadRequest, "connect_address must be a domain or IP address")
				return
			}
		}
		if input.ConnectPort != nil {
			connectPort = *input.ConnectPort
			if connectPort < 0 || connectPort > 65535 {
				response.Error(w, http.StatusBadRequest, "connect_port must be between 1 and 65535")
				return
			}
		}
		if _, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET preferred_address_id=?,connect_address=?,connect_port=?,updated_at=datetime('now') WHERE id=?`, preferredID, connectAddress, connectPort, id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		metadataUpdated = true
	}
	if input.Enabled == nil && input.Stable != nil {
		result, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET stable=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Stable), id)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			response.Error(w, http.StatusNotFound, "internal node not found")
			return
		}
		response.OK(w, map[string]interface{}{"id": id, "updated": true})
		return
	}
	if input.Enabled == nil {
		if metadataUpdated {
			response.OK(w, map[string]interface{}{"id": id, "updated": true})
			return
		}
		response.Error(w, http.StatusBadRequest, "name, preferred address, connect address, or enabled is required")
		return
	}
	stableArg := interface{}(nil)
	if input.Stable != nil {
		stableArg = boolToInt(*input.Stable)
	}
	var result sql.Result
	var err error
	if input.Stable != nil {
		result, err = db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET enabled=?,publishable=CASE WHEN ?=1 THEN publishable ELSE 0 END,revision=revision+1,stable=?,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), boolToInt(*input.Enabled), stableArg, id)
	} else {
		result, err = db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET enabled=?,publishable=CASE WHEN ?=1 THEN publishable ELSE 0 END,revision=revision+1,updated_at=datetime('now') WHERE id=?`, boolToInt(*input.Enabled), boolToInt(*input.Enabled), id)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		response.Error(w, http.StatusNotFound, "internal node not found")
		return
	}
	s.reconcileManagedProxyNode(w, r, db, id, false)
}

func (s *Service) listManagedProxyNodes(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT n.id,n.server_id,COALESCE(a.name,''),n.name,n.protocol,n.runtime,n.public_host,n.assigned_port,COALESCE(n.stats_port,0),n.transport,n.client_uri_encrypted,n.revision,n.enabled,COALESCE(n.stable,0),n.publishable,n.apply_status,n.last_error,n.observed_status,n.observed_revision,n.observed_port,COALESCE(n.observed_at,''),n.health_status,n.access_mode,n.tunnel_path,n.preferred_address_id,n.connect_address,n.connect_port,n.tunnel_hostname,n.created_at,n.updated_at FROM managed_proxy_nodes n LEFT JOIN server_accounts a ON a.id=n.server_id ORDER BY n.created_at DESC`)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	nodes := []managedProxyNode{}
	for rows.Next() {
		var node managedProxyNode
		var client string
		var enabled, stable, publishable int
		if err := rows.Scan(&node.ID, &node.ServerID, &node.ServerName, &node.Name, &node.Protocol, &node.Runtime, &node.PublicHost, &node.AssignedPort, &node.StatsPort, &node.Transport, &client, &node.Revision, &enabled, &stable, &publishable, &node.ApplyStatus, &node.LastError, &node.ObservedStatus, &node.ObservedRevision, &node.ObservedPort, &node.ObservedAt, &node.HealthStatus, &node.AccessMode, &node.TunnelPath, &node.PreferredAddressID, &node.ConnectAddress, &node.ConnectPort, &node.TunnelHostname, &node.CreatedAt, &node.UpdatedAt); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		node.Enabled, node.Stable, node.Publishable = enabled == 1, stable == 1, publishable == 1
		node.ClientURI = secure.SecureDecrypt(client)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		response.Error(w, 500, err.Error())
		return
	}
	if err := rows.Close(); err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	for index := range nodes {
		nodes[index].ClientURI = resolveManagedNodeClientURI(r.Context(), db, nodes[index])
	}
	qualityByServer := loadManagedProxyNodeQuality(r.Context(), db, nodes)
	for index := range nodes {
		nodes[index].Quality = qualityByServer[nodes[index].ServerID]
	}
	response.OK(w, nodes)
}

func loadManagedProxyNodeQuality(ctx context.Context, db *sql.DB, nodes []managedProxyNode) map[string][]managedProxyNodeQuality {
	result := make(map[string][]managedProxyNodeQuality)
	serverIDs := make([]string, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		serverID := strings.TrimSpace(node.ServerID)
		if serverID == "" || seen[serverID] {
			continue
		}
		seen[serverID] = true
		serverIDs = append(serverIDs, serverID)
	}
	if len(serverIDs) == 0 {
		return result
	}

	placeholders := make([]string, len(serverIDs))
	args := make([]interface{}, len(serverIDs))
	for index, serverID := range serverIDs {
		placeholders[index] = "?"
		args[index] = serverID
	}
	rows, err := db.QueryContext(ctx, `SELECT server_id,target_name,
		COALESCE(AVG(CASE WHEN success=1 THEN latency_ms END),0),
		CASE WHEN COUNT(*)=0 THEN 0 ELSE (1.0-(1.0*SUM(CASE WHEN success=1 THEN 1 ELSE 0 END)/COUNT(*)))*100 END,
		MAX(checked_at)
		FROM server_network_quality_samples
		WHERE checked_at >= datetime('now','-1 day') AND server_id IN (`+strings.Join(placeholders, ",")+`)
		GROUP BY server_id,target_name
		ORDER BY server_id,target_name`, args...)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var serverID string
		var item managedProxyNodeQuality
		if err := rows.Scan(&serverID, &item.Name, &item.AvgLatencyMS, &item.LossRate, &item.SampledAt); err != nil {
			continue
		}
		item.LatencyMS = item.AvgLatencyMS
		result[serverID] = append(result[serverID], item)
	}
	return result
}

func (s *Service) createManagedProxyNode(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input struct {
		ServerID           string `json:"server_id"`
		Name               string `json:"name"`
		Protocol           string `json:"protocol"`
		PublicHost         string `json:"public_host"`
		ServerName         string `json:"server_name"`
		CertificatePEM     string `json:"certificate_pem"`
		PrivateKeyPEM      string `json:"private_key_pem"`
		AccessMode         string `json:"access_mode"`
		PreferredAddressID string `json:"preferred_address_id"`
		ConnectAddress     string `json:"connect_address"`
		ConnectPort        int    `json:"connect_port"`
		Enabled            *bool  `json:"enabled"`
		Stable             *bool  `json:"stable"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid internal node")
		return
	}
	input.ServerID, input.Name, input.Protocol, input.PublicHost = strings.TrimSpace(input.ServerID), strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Protocol)), strings.TrimSpace(input.PublicHost)
	input.AccessMode = strings.ToLower(strings.TrimSpace(input.AccessMode))
	if input.Protocol == "vless-ws-tunnel" {
		input.Protocol, input.AccessMode = "vless-reality", "cloudflare_tunnel"
	}
	if input.AccessMode == "" {
		input.AccessMode = "direct"
	}
	if input.AccessMode != "direct" && input.AccessMode != "cloudflare_tunnel" {
		response.Error(w, 400, "access_mode must be direct or cloudflare_tunnel")
		return
	}
	if input.AccessMode == "cloudflare_tunnel" && input.Protocol != "vless-reality" {
		response.Error(w, 400, "Cloudflare Tunnel currently supports VLESS over WebSocket")
		return
	}
	if input.ServerID == "" {
		response.Error(w, 400, "server_id is required")
		return
	}
	switch input.Protocol {
	case "vless-reality", "hysteria2", "socks", "http":
	default:
		response.Error(w, 400, "protocol must be vless-reality, hysteria2, socks or http")
		return
	}
	if (input.Protocol == "socks" || input.Protocol == "http") && input.AccessMode != "direct" {
		response.Error(w, 400, "socks and http nodes must use direct access mode")
		return
	}
	var tunnelHostname, tunnelPath string
	if input.AccessMode == "cloudflare_tunnel" {
		if err := db.QueryRowContext(r.Context(), `SELECT hostname FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running'`, input.ServerID).Scan(&tunnelHostname); err != nil {
			response.Error(w, 409, "deploy and connect a Named Tunnel for this instance first")
			return
		}
		tunnelPath = "/api-monitor/" + strings.ReplaceAll(uuid.NewString(), "-", "")
		input.PublicHost = tunnelHostname
		if input.ConnectPort == 0 {
			input.ConnectPort = 443
		}
	}
	var accountHost, accountName, accountCountry, accountResolvedCountry, accountCachedInfo string
	if err := db.QueryRowContext(r.Context(), `SELECT COALESCE(host,''),COALESCE(name,''),COALESCE(country,''),COALESCE(resolved_country,''),COALESCE(cached_info,'{}') FROM server_accounts WHERE id=?`, input.ServerID).Scan(&accountHost, &accountName, &accountCountry, &accountResolvedCountry, &accountCachedInfo); err != nil {
		response.Error(w, 404, "server not found")
		return
	}
	var runtimeStatus string
	if err := db.QueryRowContext(r.Context(), `SELECT apply_status FROM managed_proxy_runtimes WHERE server_id=?`, input.ServerID).Scan(&runtimeStatus); err != nil || runtimeStatus != "running" {
		response.Error(w, http.StatusConflict, "install the sing-box proxy program for this instance first")
		return
	}
	if !s.requireAgentCapability(w, input.ServerID, "proxy_user_traffic_v1") {
		return
	}
	// Connection addressing belongs to the host instance. The caller only
	// chooses the node identity/protocol; changing a host address is done from
	// Host Instances and is picked up by the next deployment.
	if input.AccessMode == "direct" {
		input.PublicHost = strings.TrimSpace(accountHost)
	}
	if input.PublicHost == "" || input.PublicHost == "0.0.0.0" {
		response.Error(w, 400, "host instance has no deployable address")
		return
	}
	if input.Name == "" {
		cached := map[string]interface{}{}
		_ = json.Unmarshal([]byte(accountCachedInfo), &cached)
		countryCode := firstNonEmpty(cleanCountryCode(getString(cached, "country_code")), cleanCountryCode(getString(cached, "country")), cleanCountryCode(accountCountry), cleanCountryCode(accountResolvedCountry))
		flag := countryFlag(countryCode)
		input.Name = strings.TrimSpace(strings.TrimSpace(flag+" ") + accountName)
	}
	var existingID, existingProtocol string
	err := db.QueryRowContext(r.Context(), `SELECT id,protocol FROM managed_proxy_nodes WHERE server_id=? AND name=?`, input.ServerID, input.Name).Scan(&existingID, &existingProtocol)
	if err == nil {
		if existingProtocol != input.Protocol {
			response.Error(w, http.StatusConflict, "this server already has a node with that name using another protocol")
			return
		}
		s.reconcileManagedProxyNode(w, r, db, existingID, true)
		return
	}
	if err != sql.ErrNoRows {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if input.Protocol == "hysteria2" && (input.CertificatePEM == "" || input.PrivateKeyPEM == "") {
		cert, key, certErr := generateManagedTLSCertificate(input.PublicHost)
		if certErr != nil {
			response.Error(w, 500, "generate managed TLS certificate: "+certErr.Error())
			return
		}
		input.CertificatePEM, input.PrivateKeyPEM = cert, key
	}
	id := "mnode-" + uuid.NewString()
	config, clientURI, transport, err := generateManagedNode(id, input.Name, input.Protocol, input.PublicHost, input.ServerName, input.CertificatePEM, input.PrivateKeyPEM, input.AccessMode, tunnelHostname, tunnelPath, input.ConnectAddress, input.ConnectPort)
	if err != nil {
		response.Error(w, 400, err.Error())
		return
	}
	configEncrypted, err := secure.SecureEncrypt(config)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	clientEncrypted, err := secure.SecureEncrypt(clientURI)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	stable := false
	if input.Stable != nil {
		stable = *input.Stable
	}
	_, err = db.ExecContext(r.Context(), `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,revision,enabled,stable,publishable,apply_status,access_mode,tunnel_path,preferred_address_id,connect_address,connect_port,tunnel_hostname) VALUES(?,?,?,?,?,?,0,?,?,?,?,?,?,0,'pending',?,?,?,?,?,?)`, id, input.ServerID, input.Name, input.Protocol, "sing-box", input.PublicHost, transport, configEncrypted, clientEncrypted, 1, boolToInt(enabled), boolToInt(stable), input.AccessMode, tunnelPath, strings.TrimSpace(input.PreferredAddressID), strings.TrimSpace(input.ConnectAddress), input.ConnectPort, tunnelHostname)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			response.Error(w, 409, "this server already has a node with that name")
		} else {
			response.Error(w, 500, err.Error())
		}
		return
	}
	s.reconcileManagedProxyNode(w, r, db, id, false)
}

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

func (s *Service) reconcileManagedProxyNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string, advanceRevision bool) {
	var serverID string
	if err := db.QueryRowContext(r.Context(), `SELECT server_id FROM managed_proxy_nodes WHERE id=?`, id).Scan(&serverID); err != nil {
		if err == sql.ErrNoRows {
			response.Error(w, http.StatusNotFound, "internal node not found")
		} else {
			response.Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if _, online := s.registry.Get(serverID); !online {
		_, _ = db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET apply_status='failed',publishable=0,last_error='agent offline',updated_at=datetime('now') WHERE id=?`, id)
		response.Error(w, http.StatusBadGateway, "agent offline")
		return
	}
	if !s.requireAgentCapability(w, serverID, "proxy_runtime_v1") {
		return
	}
	if !s.requireAgentCapability(w, serverID, "proxy_user_traffic_v1") {
		return
	}
	task, ok := s.createExclusiveProxyTask(w, serverID, "proxy.node.reconcile", id)
	if !ok {
		return
	}
	if advanceRevision {
		result, err := db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET revision=revision+1,apply_status='pending',publishable=0,last_error='',updated_at=datetime('now') WHERE id=?`, id)
		if err != nil {
			s.taskRegistry.Fail(task.ID, err.Error())
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			s.taskRegistry.Fail(task.ID, "internal node not found")
			response.Error(w, http.StatusNotFound, "internal node not found")
			return
		}
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET apply_status='pending',publishable=0,last_error='',updated_at=datetime('now') WHERE id=?`, id)
	response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"task_id": task.ID, "status": task.Status, "node_id": id}})
	go s.applyManagedProxyNodeTask(task.ID, id)
}

func (s *Service) applyManagedProxyNodeTask(taskID, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, "open database: "+err.Error())
		return
	}
	defer db.Close()
	fail := func(stage string, cause error) {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_proxy_nodes SET apply_status='failed',publishable=0,last_error=?,updated_at=datetime('now') WHERE id=?`, cause.Error(), id)
		s.taskRegistry.UpdateProgress(taskID, 100, map[string]interface{}{"stage": stage, "message": cause.Error(), "node_id": id})
		s.taskRegistry.Fail(taskID, cause.Error())
	}
	progress := func(value int, stage, message string) {
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{"stage": stage, "message": message, "node_id": id})
	}
	progress(5, "validate", "正在校验实例与 Agent 连接")
	var serverID, runtime, protocol, configEncrypted, transport, accessMode string
	var nodeName string
	var revision int64
	var enabled int
	var requestedPort int
	err = db.QueryRowContext(ctx, `SELECT server_id,name,runtime,protocol,config_encrypted,revision,enabled,assigned_port,transport,access_mode FROM managed_proxy_nodes WHERE id=?`, id).Scan(&serverID, &nodeName, &runtime, &protocol, &configEncrypted, &revision, &enabled, &requestedPort, &transport, &accessMode)
	if err == sql.ErrNoRows {
		fail("validate", errors.New("internal node not found"))
		return
	}
	if err != nil {
		fail("validate", err)
		return
	}
	if accessMode == "direct" {
		if err := syncDirectManagedNodeAddress(ctx, db, id, serverID); err != nil {
			fail("sync_host_address", err)
			return
		}
	}
	requestedPort, excludedPorts, err := reserveManagedProxyPort(ctx, db, serverID, id, requestedPort)
	if err != nil {
		fail("reserve_port", err)
		return
	}
	release, ok := managedProxyRuntime(runtime)
	if !ok {
		fail("runtime", errors.New("managed proxy runtime is not pinned"))
		return
	}
	if enabled == 1 {
		progress(18, "runtime", "正在检查 sing-box 运行时")
	} else {
		progress(18, "runtime", "正在准备停用节点")
	}
	// Tunnel nodes use VLESS over WebSocket even though their persisted node
	// protocol remains vless-reality for compatibility with the node model.
	// Do not attach REALITY Vision flow to their subscriber users.
	bindingProtocol := protocol
	if accessMode == "cloudflare_tunnel" {
		bindingProtocol = "vless-ws-tunnel"
	}
	effectiveConfig, subscriberCount, err := bindManagedNodeSubscribers(ctx, db, id, bindingProtocol, secure.SecureDecrypt(configEncrypted))
	if err != nil {
		fail("bind_subscribers", err)
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{"node_id": id, "revision": revision, "runtime": "sing-box", "runtime_version": release.Version, "asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256, "asset_format": release.AssetFormat, "config": effectiveConfig, "enabled": enabled == 1, "requested_port": requestedPort, "excluded_ports": excludedPorts, "port_min": 45654, "port_max": 55654, "transport": transport, "subscriber_count": subscriberCount})
	if enabled == 1 {
		progress(35, "configure", "正在生成配置并分配可用端口")
	} else {
		progress(35, "configure", "正在停止节点服务并关闭端口")
	}
	result, runErr := s.RunProxyRuntimeTaskAndWait(serverID, string(payload))
	if runErr != nil {
		fail("agent_apply", runErr)
		return
	}
	if enabled == 1 {
		progress(72, "verify_agent", "代理服务已启动，正在校验 Agent 状态")
	} else {
		progress(72, "verify_agent", "节点服务已停止，正在确认状态")
	}
	var applied struct {
		AssignedPort int    `json:"assigned_port"`
		StatsPort    int    `json:"stats_port"`
		Config       string `json:"config"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result), &applied); err != nil || applied.AssignedPort < 45654 || applied.AssignedPort > 55654 || applied.StatsPort < 20000 || applied.StatsPort > 29999 {
		fail("verify_agent", errors.New("Agent returned invalid node binding"))
		return
	}
	updatedConfig, _ := secure.SecureEncrypt(applied.Config)
	var clientEncrypted string
	_ = db.QueryRowContext(ctx, `SELECT client_uri_encrypted FROM managed_proxy_nodes WHERE id=?`, id).Scan(&clientEncrypted)
	client := secure.SecureDecrypt(clientEncrypted)
	client = bindManagedNodeRuntimePort(client, applied.AssignedPort, accessMode)
	updatedClient, _ := secure.SecureEncrypt(client)
	if enabled == 0 {
		if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET assigned_port=?,stats_port=?,config_encrypted=?,client_uri_encrypted=?,apply_status='stopped',publishable=0,last_error='',observed_status='stopped',observed_revision=revision,observed_port=?,observed_at=datetime('now'),health_status='disabled',updated_at=datetime('now') WHERE id=?`, applied.AssignedPort, applied.StatsPort, updatedConfig, updatedClient, applied.AssignedPort, id); err != nil {
			fail("persist_stopped", fmt.Errorf("保存节点停用状态失败: %w", err))
			return
		}
		progress(97, "disabled", "节点已停用，正在从订阅中移除")
		s.taskRegistry.Complete(taskID, fmt.Sprintf("%s 已停用", nodeName))
		return
	}
	if accessMode == "direct" {
		progress(82, "reachability", "正在检查节点公网连通性")
		if err := verifyManagedNodeReachability(statePublicHost(ctx, db, id), applied.AssignedPort, transport); err != nil {
			message := fmt.Sprintf("节点服务已启动，但公网地址 %s:%d 无法连接，请检查主机防火墙和云平台安全组", statePublicHost(ctx, db, id), applied.AssignedPort)
			_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET assigned_port=?,apply_status='runtime_running_unreachable',publishable=0,last_error=?,observed_status=?,observed_revision=revision,observed_port=?,observed_at=datetime('now'),health_status='external_unreachable',updated_at=datetime('now') WHERE id=?`, applied.AssignedPort, message, applied.Status, applied.AssignedPort, id)
			s.taskRegistry.UpdateProgress(taskID, 100, map[string]interface{}{"stage": "reachability", "message": message, "node_id": id})
			s.taskRegistry.Fail(taskID, message)
			return
		}
	}
	healthStatus := "local_runtime_verified"
	if accessMode == "direct" && transport == "tcp" {
		healthStatus = "external_tcp_open"
	} else if accessMode == "cloudflare_tunnel" {
		healthStatus = "tunnel_pending"
	}
	if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET assigned_port=?,stats_port=?,config_encrypted=?,client_uri_encrypted=?,apply_status='running',publishable=CASE WHEN access_mode='cloudflare_tunnel' THEN 0 ELSE 1 END,last_error='',observed_status=?,observed_revision=revision,observed_port=?,observed_at=datetime('now'),health_status=?,updated_at=datetime('now') WHERE id=?`, applied.AssignedPort, applied.StatsPort, updatedConfig, updatedClient, applied.Status, applied.AssignedPort, healthStatus, id); err != nil {
		_ = s.compensateManagedProxyNode(ctx, serverID, id, revision, release)
		fail("persist_binding", fmt.Errorf("persist Agent binding: %w", err))
		return
	}
	_, _ = db.ExecContext(ctx, `INSERT INTO managed_proxy_runtimes(server_id,runtime,version,desired_status,apply_status,last_stage,last_error,installed_at,updated_at) VALUES(?,'sing-box',?,'running','running','ready','',datetime('now'),datetime('now')) ON CONFLICT(server_id) DO UPDATE SET runtime=excluded.runtime,version=excluded.version,desired_status='running',apply_status='running',last_stage='ready',last_error='',installed_at=COALESCE(managed_proxy_runtimes.installed_at,datetime('now')),updated_at=datetime('now')`, serverID, release.Version)
	if accessMode == "cloudflare_tunnel" {
		progress(90, "tunnel_ingress", "正在同步 Cloudflare Tunnel 路由")
		if err := s.syncTunnelIngress(ctx, db, serverID); err != nil {
			_ = s.compensateManagedProxyNode(ctx, serverID, id, revision, release)
			_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET assigned_port=0,observed_status='removed',observed_port=0,observed_at=datetime('now') WHERE id=?`, id)
			fail("tunnel_ingress", err)
			return
		}
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET publishable=1,health_status='tunnel_routed',updated_at=datetime('now') WHERE id=?`, id)
	}
	progress(97, "publish", "正在发布节点到订阅")
	s.taskRegistry.Complete(taskID, fmt.Sprintf("%s 已启用，端口 %d", nodeName, applied.AssignedPort))
}

func syncDirectManagedNodeAddress(ctx context.Context, db *sql.DB, nodeID, serverID string) error {
	var host, encryptedURI string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(a.host,''),n.client_uri_encrypted FROM managed_proxy_nodes n JOIN server_accounts a ON a.id=n.server_id WHERE n.id=? AND n.server_id=?`, nodeID, serverID).Scan(&host, &encryptedURI); err != nil {
		return err
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" {
		return errors.New("host instance has no deployable address")
	}
	client := secure.SecureDecrypt(encryptedURI)
	if parsed, err := url.Parse(client); err == nil {
		port := parsed.Port()
		if port == "" {
			port = "0"
		}
		parsed.Host = net.JoinHostPort(host, port)
		client = parsed.String()
	}
	updatedURI, err := secure.SecureEncrypt(client)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET public_host=?,client_uri_encrypted=?,updated_at=datetime('now') WHERE id=?`, host, updatedURI, nodeID)
	return err
}

func reserveManagedProxyPort(ctx context.Context, db *sql.DB, serverID, nodeID string, requested int) (int, []int, error) {
	rows, err := db.QueryContext(ctx, `SELECT assigned_port FROM managed_proxy_nodes WHERE server_id=? AND id<>? AND assigned_port BETWEEN 45654 AND 55654 ORDER BY assigned_port`, serverID, nodeID)
	if err != nil {
		return 0, nil, err
	}
	used := map[int]struct{}{}
	excluded := []int{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			rows.Close()
			return 0, nil, err
		}
		if _, exists := used[port]; !exists {
			used[port] = struct{}{}
			excluded = append(excluded, port)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, nil, err
	}
	if requested < 45654 || requested > 55654 {
		requested = 0
	}
	if _, conflict := used[requested]; conflict {
		requested = 0
	}
	if requested == 0 {
		for port := 45654; port <= 55654; port++ {
			if _, exists := used[port]; !exists {
				requested = port
				break
			}
		}
	}
	if requested == 0 {
		return 0, excluded, errors.New("no unreserved managed proxy port is available")
	}
	if _, err := db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET assigned_port=?,updated_at=datetime('now') WHERE id=?`, requested, nodeID); err != nil {
		return 0, excluded, err
	}
	return requested, excluded, nil
}

func (s *Service) compensateManagedProxyNode(ctx context.Context, serverID, nodeID string, revision int64, release proxyRuntimeRelease) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"node_id": nodeID, "revision": revision + 1, "runtime": release.Runtime,
		"runtime_version": release.Version, "asset_url_amd64": release.AMD64URL,
		"asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL,
		"asset_sha256_arm64": release.ARM64SHA256, "config": "{}", "remove": true,
		"asset_format": release.AssetFormat,
		"port_min":     45654, "port_max": 55654,
	})
	_, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload))
	return err
}

func (s *Service) syncTunnelIngress(ctx context.Context, db *sql.DB, serverID string) error {
	if s.cloudflare == nil {
		return errors.New("Cloudflare integration is unavailable")
	}
	var accountID, tunnelID, hostname string
	if err := db.QueryRowContext(ctx, `SELECT account_id,tunnel_id,hostname FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running'`, serverID).Scan(&accountID, &tunnelID, &hostname); err != nil {
		return errors.New("managed Named Tunnel is not connected")
	}
	ingress, err := loadTunnelIngress(ctx, db, serverID, hostname)
	if err != nil {
		return err
	}
	return s.cloudflare.ConfigureManagedTunnel(ctx, accountID, tunnelID, ingress)
}

func resolveManagedNodeClientURI(ctx context.Context, db *sql.DB, node managedProxyNode) string {
	if strings.TrimSpace(node.ClientURI) == "" {
		return node.ClientURI
	}
	address, port := strings.TrimSpace(node.ConnectAddress), node.ConnectPort
	if address == "" && node.PreferredAddressID != "" {
		_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE id=? AND enabled=1`, node.PreferredAddressID).Scan(&address, &port)
	}
	if address == "" {
		_ = db.QueryRowContext(ctx, `SELECT address,port FROM managed_proxy_preferences WHERE enabled=1 AND is_default=1 ORDER BY sort_order ASC LIMIT 1`).Scan(&address, &port)
	}
	if address == "" {
		address = node.TunnelHostname
	}
	if address == "" && node.AccessMode == "direct" {
		return node.ClientURI
	}
	if port == 0 {
		port = 443
	}
	parsed, err := url.Parse(node.ClientURI)
	if err != nil {
		return node.ClientURI
	}
	parsed.Host = net.JoinHostPort(address, strconv.Itoa(port))
	return parsed.String()
}

func statePublicHost(ctx context.Context, db *sql.DB, id string) string {
	var host string
	_ = db.QueryRowContext(ctx, `SELECT public_host FROM managed_proxy_nodes WHERE id=?`, id).Scan(&host)
	return host
}

func verifyManagedNodeReachability(host string, port int, transport string) error {
	if strings.TrimSpace(host) == "" {
		return errors.New("managed node public host is empty")
	}
	if transport == "udp" {
		return nil
	} // UDP has no transport handshake; runtime active state is the local health signal.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return fmt.Errorf("managed node is not externally reachable at %s:%d: %w", host, port, err)
	}
	_ = conn.Close()
	return nil
}

func replaceURIClientPort(raw string, port int) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Host = parsed.Hostname() + ":" + strconv.Itoa(port)
	return parsed.String()
}

// A Tunnel node has two independent endpoints: the local origin port assigned
// by the Agent and the public Cloudflare/Preferred-Address endpoint. Only a
// direct node exposes the assigned origin port to clients.
func bindManagedNodeRuntimePort(raw string, port int, accessMode string) string {
	if accessMode == "cloudflare_tunnel" {
		return raw
	}
	return replaceURIClientPort(raw, port)
}

func loadPublishableManagedNodes(ctx context.Context, db *sql.DB) ([]managedProxyNode, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,server_id,name,protocol,runtime,public_host,assigned_port,transport,client_uri_encrypted,revision,enabled,publishable,apply_status,last_error,access_mode,tunnel_path,preferred_address_id,connect_address,connect_port,tunnel_hostname,created_at,updated_at FROM managed_proxy_nodes WHERE enabled=1 AND publishable=1 AND apply_status='running' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	nodes := []managedProxyNode{}
	for rows.Next() {
		var node managedProxyNode
		var client string
		var enabled, publishable int
		if err := rows.Scan(&node.ID, &node.ServerID, &node.Name, &node.Protocol, &node.Runtime, &node.PublicHost, &node.AssignedPort, &node.Transport, &client, &node.Revision, &enabled, &publishable, &node.ApplyStatus, &node.LastError, &node.AccessMode, &node.TunnelPath, &node.PreferredAddressID, &node.ConnectAddress, &node.ConnectPort, &node.TunnelHostname, &node.CreatedAt, &node.UpdatedAt); err != nil {
			return nil, err
		}
		node.Enabled, node.Publishable = enabled == 1, publishable == 1
		node.ClientURI = secure.SecureDecrypt(client)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range nodes {
		nodes[index].ClientURI = resolveManagedNodeClientURI(ctx, db, nodes[index])
	}
	return nodes, nil
}

func (s *Service) deleteManagedProxyNode(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var serverID, runtime, applyStatus, accessMode string
	var revision int64
	var assignedPort int
	if err := db.QueryRowContext(r.Context(), `SELECT server_id,runtime,revision,assigned_port,apply_status,access_mode FROM managed_proxy_nodes WHERE id=?`, id).Scan(&serverID, &runtime, &revision, &assignedPort, &applyStatus, &accessMode); err != nil {
		if err == sql.ErrNoRows {
			response.Error(w, 404, "internal node not found")
		} else {
			response.Error(w, 500, err.Error())
		}
		return
	}
	requiresAgent := assignedPort > 0 || applyStatus == "running"
	forceDetach := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	if requiresAgent && !forceDetach {
		connection, online := s.registry.Get(serverID)
		capable := online && connection.GetCapabilities()["proxy_runtime_v1"]
		if !capable {
			reason := "Agent 离线，无法清理主机上的服务、配置和防火墙规则"
			if online {
				reason = "Agent 版本过旧，无法执行节点卸载"
			}
			response.JSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   reason,
				"data": map[string]interface{}{
					"can_force_detach": true,
					"node_id":          id,
					"server_id":        serverID,
					"agent_online":     online,
				},
			})
			return
		}
	}
	requiresAgent = requiresAgent && !forceDetach
	task, ok := s.createExclusiveProxyTask(w, serverID, "proxy.node.delete", id)
	if !ok {
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_proxy_nodes SET enabled=0,publishable=0,apply_status='removing',last_error='',updated_at=datetime('now') WHERE id=?`, id)
	response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"task_id": task.ID, "status": task.Status, "node_id": id}})
	go s.runManagedProxyNodeDelete(task.ID, id, serverID, runtime, revision, requiresAgent, forceDetach, accessMode)
}

func (s *Service) runManagedProxyNodeDelete(taskID, id, serverID, runtime string, revision int64, requiresAgent, forceDetach bool, accessMode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()
	fail := func(stage string, cause error) {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_proxy_nodes SET apply_status='remove_failed',last_error=?,updated_at=datetime('now') WHERE id=?`, cause.Error(), id)
		s.taskRegistry.UpdateProgress(taskID, 100, map[string]interface{}{"stage": stage, "message": cause.Error(), "node_id": id})
		s.taskRegistry.Fail(taskID, cause.Error())
	}
	s.taskRegistry.UpdateProgress(taskID, 10, map[string]interface{}{"stage": "unpublish", "message": "正在停止发布节点", "node_id": id})
	if requiresAgent {
		release, ok := managedProxyRuntime(runtime)
		if !ok {
			fail("runtime", errors.New("managed proxy runtime is not pinned"))
			return
		}
		s.taskRegistry.UpdateProgress(taskID, 35, map[string]interface{}{"stage": "remove_host", "message": "正在删除主机服务、配置与防火墙规则", "node_id": id})
		payload, _ := json.Marshal(map[string]interface{}{"node_id": id, "revision": revision + 1, "runtime": "sing-box", "runtime_version": release.Version, "asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256, "asset_format": release.AssetFormat, "config": "{}", "remove": true, "port_min": 45654, "port_max": 55654})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			fail("remove_host", err)
			return
		}
	}
	if accessMode == "cloudflare_tunnel" {
		s.taskRegistry.UpdateProgress(taskID, 72, map[string]interface{}{"stage": "tunnel_ingress", "message": "正在移除 Tunnel 路由", "node_id": id})
		if err := s.syncTunnelIngress(ctx, db, serverID); err != nil {
			fail("tunnel_ingress", err)
			return
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		fail("delete_record", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_plan_nodes WHERE node_id=? AND source='internal'`, id); err != nil && !isMissingTableError(err) {
		fail("delete_relations", err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_runtime_reconcile WHERE node_id=?`, id); err != nil && !isMissingTableError(err) {
		fail("delete_queue", err)
		return
	}
	for _, statement := range []string{
		`DELETE FROM subscription_usage_reports WHERE node_id=?`,
		`DELETE FROM subscription_usage_report_keys WHERE node_id=?`,
		`DELETE FROM subscription_usage_hourly WHERE node_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil && !isMissingTableError(err) {
			fail("delete_traffic_ledger", err)
			return
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_proxy_nodes WHERE id=?`, id); err != nil {
		fail("delete_record", err)
		return
	}
	if err := tx.Commit(); err != nil {
		fail("delete_record", err)
		return
	}
	message := "节点已从主机与订阅中删除"
	if forceDetach {
		message = "节点已从面板和订阅中移除；主机离线，未清理主机残留"
	}
	s.taskRegistry.Complete(taskID, message)
}
