package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
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

