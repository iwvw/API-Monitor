package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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
