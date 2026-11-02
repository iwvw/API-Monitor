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

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	cloudflaredVersion             = "2026.7.2"
	cloudflaredAMD64URL            = "https://github.com/cloudflare/cloudflared/releases/download/2026.7.2/cloudflared-linux-amd64"
	cloudflaredAMD64SHA256         = "ec905ea7b7e327ff8abdde8cb64697a2152de74dbcdbf6aec9db8364eb3886cd"
	cloudflaredARM64URL            = "https://github.com/cloudflare/cloudflared/releases/download/2026.7.2/cloudflared-linux-arm64"
	cloudflaredARM64SHA256         = "405df476437e027fc6d18729a5a77155c0a33a6082aeee60a799a688f3052e66"
	cloudflaredWindowsAMD64URL     = "https://github.com/cloudflare/cloudflared/releases/download/2026.7.2/cloudflared-windows-amd64.exe"
	cloudflaredWindowsAMD64SHA256  = "cdb5d4432f6ae1595654a692a51308b69d2bf7af961f5578d9391837cf072df9"
)

type managedTunnelState struct {
	ServerID      string `json:"server_id"`
	ServerName    string `json:"server_name"`
	AccountID     string `json:"account_id"`
	ZoneID        string `json:"zone_id"`
	ZoneName      string `json:"zone_name"`
	TunnelID      string `json:"tunnel_id"`
	TunnelName    string `json:"tunnel_name"`
	Hostname      string `json:"hostname"`
	DNSRecordID   string `json:"dns_record_id"`
	Revision      int64  `json:"revision"`
	DesiredStatus string `json:"desired_status"`
	ApplyStatus   string `json:"apply_status"`
	LastStage     string `json:"last_stage"`
	LastError     string `json:"last_error"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	NodeCount     int    `json:"node_count"`
}

type preferredAddress struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	Enabled       bool   `json:"enabled"`
	IsDefault     bool   `json:"is_default"`
	SortOrder     int    `json:"sort_order"`
	LastStatus    string `json:"last_status"`
	LastLatencyMS int    `json:"last_latency_ms"`
	LastError     string `json:"last_error"`
	CheckedAt     string `json:"checked_at"`
}

func (s *Service) handleManagedTunnelRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	switch {
	case len(subparts) == 0 && r.Method == http.MethodGet:
		s.listManagedTunnels(w, r, db)
	case len(subparts) == 1 && subparts[0] == "preflight" && r.Method == http.MethodPost:
		s.preflightManagedTunnel(w, r)
	case len(subparts) == 2 && subparts[1] == "deploy" && r.Method == http.MethodPost:
		s.deployManagedTunnel(w, r, db, subparts[0])
	case len(subparts) == 1 && r.Method == http.MethodDelete:
		s.uninstallManagedTunnel(w, r, db, subparts[0])
	default:
		response.Error(w, http.StatusNotFound, "managed Tunnel route not found")
	}
}

func (s *Service) handlePreferredAddressRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	switch {
	case len(subparts) == 0 && r.Method == http.MethodGet:
		s.listPreferredAddresses(w, r, db)
	case len(subparts) == 0 && r.Method == http.MethodPost:
		s.savePreferredAddress(w, r, db, "")
	case len(subparts) == 1 && r.Method == http.MethodPut:
		s.savePreferredAddress(w, r, db, subparts[0])
	case len(subparts) == 1 && r.Method == http.MethodDelete:
		s.deletePreferredAddress(w, r, db, subparts[0])
	default:
		response.Error(w, http.StatusNotFound, "preferred address route not found")
	}
}

func (s *Service) listManagedTunnels(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT t.server_id,COALESCE(a.name,''),t.account_id,t.zone_id,t.zone_name,t.tunnel_id,t.tunnel_name,t.hostname,t.dns_record_id,t.revision,t.desired_status,t.apply_status,t.last_stage,t.last_error,t.created_at,t.updated_at,(SELECT COUNT(*) FROM managed_proxy_nodes n WHERE n.server_id=t.server_id AND n.access_mode='cloudflare_tunnel') FROM managed_proxy_tunnels t LEFT JOIN server_accounts a ON a.id=t.server_id ORDER BY t.updated_at DESC`)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []managedTunnelState{}
	for rows.Next() {
		var item managedTunnelState
		if err := rows.Scan(&item.ServerID, &item.ServerName, &item.AccountID, &item.ZoneID, &item.ZoneName, &item.TunnelID, &item.TunnelName, &item.Hostname, &item.DNSRecordID, &item.Revision, &item.DesiredStatus, &item.ApplyStatus, &item.LastStage, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.NodeCount); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		items = append(items, item)
	}
	response.OK(w, items)
}

func (s *Service) preflightManagedTunnel(w http.ResponseWriter, r *http.Request) {
	if s.cloudflare == nil {
		response.Error(w, http.StatusServiceUnavailable, "Cloudflare integration is unavailable")
		return
	}
	var input struct {
		AccountID string `json:"account_id"`
		ZoneID    string `json:"zone_id"`
		Hostname  string `json:"hostname"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid Tunnel preflight request")
		return
	}
	result, err := s.cloudflare.PreflightManagedTunnel(r.Context(), input.AccountID, input.ZoneID, input.Hostname)
	if err != nil {
		response.Error(w, 422, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) deployManagedTunnel(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	if s.cloudflare == nil {
		response.Error(w, http.StatusServiceUnavailable, "Cloudflare integration is unavailable")
		return
	}
	var input struct {
		AccountID string `json:"account_id"`
		ZoneID    string `json:"zone_id"`
		Hostname  string `json:"hostname"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid Tunnel deployment request")
		return
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.ZoneID = strings.TrimSpace(input.ZoneID)
	input.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Hostname), "."))
	if input.AccountID == "" || input.ZoneID == "" || input.Hostname == "" {
		response.Error(w, 400, "account_id, zone_id and hostname are required")
		return
	}
	var serverName string
	if err := db.QueryRowContext(r.Context(), `SELECT name FROM server_accounts WHERE id=?`, serverID).Scan(&serverName); err != nil {
		response.Error(w, 404, "server not found")
		return
	}
	if !s.requireAgentCapability(w, serverID, "cloudflared_runtime_v1") {
		return
	}
	task, ok := s.createExclusiveProxyTask(w, serverID, "proxy.tunnel.deploy", input.Hostname)
	if !ok {
		return
	}
	go s.runManagedTunnelDeploy(task.ID, serverID, serverName, input.AccountID, input.ZoneID, input.Hostname)
	response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"task_id": task.ID, "status": task.Status}})
}

func (s *Service) runManagedTunnelDeploy(taskID, serverID, serverName, accountID, zoneID, hostname string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()
	fail := func(stage string, err error) {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_proxy_tunnels SET apply_status='failed',last_stage=?,last_error=?,updated_at=datetime('now') WHERE server_id=?`, stage, err.Error(), serverID)
		s.taskRegistry.Fail(taskID, err.Error())
	}
	progress := func(value int, stage, message string) {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET last_stage=?,last_error='',updated_at=datetime('now') WHERE server_id=?`, stage, serverID)
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{"stage": stage, "message": message, "server_id": serverID, "server_name": serverName})
	}

	progress(5, "preflight", "正在检查 Cloudflare Tunnel 与 DNS 权限")
	preflight, err := s.cloudflare.PreflightManagedTunnel(ctx, accountID, zoneID, hostname)
	if err != nil {
		fail("preflight", err)
		return
	}
	var state managedTunnelState
	err = db.QueryRowContext(ctx, `SELECT account_id,zone_id,zone_name,tunnel_id,tunnel_name,hostname,dns_record_id,revision,apply_status FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&state.AccountID, &state.ZoneID, &state.ZoneName, &state.TunnelID, &state.TunnelName, &state.Hostname, &state.DNSRecordID, &state.Revision, &state.ApplyStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail("load_state", err)
		return
	}
	if state.TunnelID != "" && (state.AccountID != accountID || state.ZoneID != zoneID) {
		fail("validate_state", errors.New("existing Tunnel account or zone differs; uninstall it before changing ownership"))
		return
	}
	if state.TunnelID != "" {
		// The stored tunnel may have been deleted outside the panel (or by an
		// earlier partial cleanup). Rebuilding it in place keeps the deploy
		// idempotent instead of failing with "Tunnel not found".
		progress(8, "verify_tunnel", "正在确认 Named Tunnel 仍存在")
		exists, checkErr := s.cloudflare.ManagedTunnelExists(ctx, accountID, state.TunnelID)
		if checkErr != nil {
			fail("verify_tunnel", checkErr)
			return
		}
		if !exists {
			_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET tunnel_id='',tunnel_name='',token_encrypted='',updated_at=datetime('now') WHERE server_id=?`, serverID)
			state.TunnelID, state.TunnelName = "", ""
		}
	}
	if state.TunnelID != "" && state.Hostname != "" && state.Hostname != hostname {
		// The owned record is removed only after the replacement was created.
		progress(10, "hostname_change", "检测到域名变更，将在新记录生效后清理旧记录")
	}
	if state.Revision == 0 {
		state.Revision = 1
	} else {
		state.Revision++
	}
	_, err = db.ExecContext(ctx, `INSERT INTO managed_proxy_tunnels(server_id,account_id,zone_id,zone_name,hostname,revision,desired_status,apply_status,last_stage,last_error) VALUES(?,?,?,?,?,?,'running','pending','preflight','') ON CONFLICT(server_id) DO UPDATE SET account_id=excluded.account_id,zone_id=excluded.zone_id,zone_name=excluded.zone_name,hostname=excluded.hostname,revision=excluded.revision,desired_status='running',apply_status='pending',last_stage='preflight',last_error='',updated_at=datetime('now')`, serverID, accountID, zoneID, preflight.ZoneName, hostname, state.Revision)
	if err != nil {
		fail("persist_state", err)
		return
	}

	createdTunnel := false
	oldHostname, oldRecordID := state.Hostname, state.DNSRecordID
	if state.TunnelID == "" {
		progress(18, "create_tunnel", "正在创建 Named Tunnel")
		tunnelName := "api-monitor-" + shortStableID(serverID)
		tunnel, createErr := s.cloudflare.CreateManagedTunnel(ctx, accountID, tunnelName)
		if createErr != nil {
			fail("create_tunnel", createErr)
			return
		}
		state.TunnelID, state.TunnelName, createdTunnel = tunnel.ID, tunnel.Name, true
		_, err = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET tunnel_id=?,tunnel_name=?,last_stage='create_tunnel',updated_at=datetime('now') WHERE server_id=?`, state.TunnelID, state.TunnelName, serverID)
		if err != nil {
			_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, state.TunnelID)
			fail("persist_tunnel", err)
			return
		}
	}
	rollback := func(stage string, cause error) {
		if createdTunnel {
			_ = s.cloudflare.DeleteManagedTunnelDNS(context.Background(), accountID, zoneID, state.DNSRecordID)
			_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, state.TunnelID)
			_, _ = db.ExecContext(context.Background(), `UPDATE managed_proxy_tunnels SET tunnel_id='',tunnel_name='',dns_record_id='',token_encrypted='',apply_status='failed',last_stage=?,last_error=?,updated_at=datetime('now') WHERE server_id=?`, stage, cause.Error(), serverID)
		} else if state.TunnelID != "" {
			if oldHostname != "" {
				if oldIngress, loadErr := loadTunnelIngress(context.Background(), db, serverID, oldHostname); loadErr == nil {
					_ = s.cloudflare.ConfigureManagedTunnel(context.Background(), accountID, state.TunnelID, oldIngress)
				}
			}
			if state.DNSRecordID != "" && state.DNSRecordID != oldRecordID {
				_ = s.cloudflare.DeleteManagedTunnelDNS(context.Background(), accountID, zoneID, state.DNSRecordID)
			}
			_, _ = db.ExecContext(context.Background(), `UPDATE managed_proxy_tunnels SET hostname=?,dns_record_id=?,apply_status='failed',last_stage=?,last_error=?,updated_at=datetime('now') WHERE server_id=?`, oldHostname, oldRecordID, stage, cause.Error(), serverID)
		}
		fail(stage, cause)
	}

	progress(32, "configure_ingress", "正在同步 Tunnel 路由")
	ingress, err := loadTunnelIngress(ctx, db, serverID, hostname)
	if err != nil {
		rollback("configure_ingress", err)
		return
	}
	if err := s.cloudflare.ConfigureManagedTunnel(ctx, accountID, state.TunnelID, ingress); err != nil {
		rollback("configure_ingress", err)
		return
	}

	progress(45, "configure_dns", "正在创建或更新 Tunnel DNS 记录")
	dns, err := s.cloudflare.EnsureManagedTunnelDNS(ctx, accountID, zoneID, hostname, state.TunnelID)
	if err != nil {
		rollback("configure_dns", err)
		return
	}
	state.DNSRecordID = dns.ID
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET dns_record_id=?,hostname=?,zone_name=?,updated_at=datetime('now') WHERE server_id=?`, dns.ID, hostname, preflight.ZoneName, serverID)

	progress(58, "retrieve_token", "正在获取主机专用 Tunnel 令牌")
	token, err := s.cloudflare.ManagedTunnelToken(ctx, accountID, state.TunnelID)
	if err != nil {
		rollback("retrieve_token", err)
		return
	}
	encryptedToken, err := secure.SecureEncrypt(token)
	if err != nil {
		rollback("encrypt_token", err)
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET token_encrypted=?,updated_at=datetime('now') WHERE server_id=?`, encryptedToken, serverID)

	progress(70, "install_connector", "正在主机上安装并启动 cloudflared")
	payload, _ := json.Marshal(cloudflaredTaskPayload("install", token))
	if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
		rollback("install_connector", err)
		return
	}

	progress(88, "verify_connector", "cloudflared 已启动，正在确认边缘连接")
	connected := false
	for attempt := 0; attempt < 8; attempt++ {
		count, checkErr := s.cloudflare.ManagedTunnelConnections(ctx, accountID, state.TunnelID)
		if checkErr == nil && count > 0 {
			connected = true
			break
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(3 * time.Second):
		}
	}
	if !connected {
		fail("verify_connector", errors.New("cloudflared started but Cloudflare did not report an active connector; retry after checking host egress"))
		return
	}
	if oldHostname != "" && oldHostname != hostname {
		progress(94, "migrate_nodes", "正在切换关联节点的 Tunnel 域名")
		if err := migrateManagedTunnelHostname(ctx, db, serverID, oldHostname, hostname); err != nil {
			rollback("migrate_nodes", err)
			return
		}
	}
	if oldRecordID != "" && oldRecordID != state.DNSRecordID {
		if err := s.cloudflare.DeleteManagedTunnelDNS(ctx, accountID, zoneID, oldRecordID); err != nil {
			rollback("remove_old_dns", err)
			return
		}
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE server_id=?`, serverID)
	s.taskRegistry.Complete(taskID, fmt.Sprintf("Named Tunnel %s is connected", hostname))
}

// startManagedTunnelHealthLoop periodically re-verifies the real Cloudflare
// edge connectivity of managed tunnels. apply_status='running' previously
// meant "deploy completed", which never reflected a later cloudflared
// disconnect. This loop reconciles that status against the authoritative
// Cloudflare connector count so the panel stops showing a fake "已连接".
func (s *Service) startManagedTunnelHealthLoop(ctx context.Context) {
	ticker := time.NewTicker(s.tunnelHealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.cloudflare == nil {
			continue
		}
		s.reconcileManagedTunnelHealth(ctx)
	}
}

func (s *Service) reconcileManagedTunnelHealth(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT server_id FROM managed_proxy_tunnels WHERE desired_status='running' AND apply_status IN ('running','disconnected','reconciling') ORDER BY updated_at ASC`)
	if err != nil {
		return
	}
	serverIDs := []string{}
	for rows.Next() {
		var serverID string
		if rows.Scan(&serverID) == nil {
			serverIDs = append(serverIDs, serverID)
		}
	}
	rows.Close()
	for _, serverID := range serverIDs {
		go s.reconcileManagedTunnelConnection(serverID)
	}
	// 整域转发规则的独立隧道同样纳入健康检查与自愈
	s.reconcileForwardTunnelHealth(ctx)
}

func (s *Service) reconcileManagedTunnelConnection(serverID string) {
	if s.cloudflare == nil {
		return
	}
	if _, busy := s.taskRegistry.ActiveTask(proxyTaskResource(serverID)); busy {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	var accountID, tunnelID, applyStatus, lastReconcileAt string
	if err := db.QueryRowContext(ctx, `SELECT account_id,tunnel_id,apply_status,COALESCE(last_reconcile_at,'') FROM managed_proxy_tunnels WHERE server_id=? AND desired_status='running'`, serverID).Scan(&accountID, &tunnelID, &applyStatus, &lastReconcileAt); err != nil || tunnelID == "" {
		return
	}
	if applyStatus == "reconciling" && lastReconcileAt != "" {
		if t, parseErr := time.Parse("2006-01-02 15:04:05", lastReconcileAt); parseErr == nil && time.Since(t) < 10*time.Minute {
			return
		}
	}
	connected := false
	var checkErr error
	for attempt := 0; attempt < s.tunnelHealthCheckAttempts; attempt++ {
		var count int
		count, checkErr = s.cloudflare.ManagedTunnelConnections(ctx, accountID, tunnelID)
		if checkErr == nil && count > 0 {
			connected = true
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.tunnelHealthCheckDelay):
		}
	}
	if connected {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='running',last_stage='health_check',last_error='',reconcile_attempts=0,last_reconcile_at='',updated_at=datetime('now') WHERE server_id=? AND desired_status='running'`, serverID)
		return
	}
	if checkErr != nil {
		applog.Warn(ctx, "serveragent", "managed tunnel health check failed", "server_id", serverID, "error", checkErr.Error())
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='disconnected',last_stage='health_check',last_error='Cloudflare 未检测到 cloudflared 连接',updated_at=datetime('now') WHERE server_id=? AND desired_status='running'`, serverID)
	s.attemptTunnelSelfHeal(serverID)
}

func (s *Service) attemptTunnelSelfHeal(serverID string) {
	conn, ok := s.registry.Get(serverID)
	if !ok || !conn.GetCapabilities()["cloudflared_runtime_v1"] {
		return
	}
	if _, busy := s.taskRegistry.ActiveTask(proxyTaskResource(serverID)); busy {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	var attempts int
	var tokenEncrypted, lastReconcileAt string
	if err := db.QueryRowContext(ctx, `SELECT reconcile_attempts,token_encrypted,COALESCE(last_reconcile_at,'') FROM managed_proxy_tunnels WHERE server_id=? AND desired_status='running' AND apply_status='disconnected'`, serverID).Scan(&attempts, &tokenEncrypted, &lastReconcileAt); err != nil {
		return
	}
	if attempts >= s.tunnelReconcileMaxAttempts {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET last_error='自愈重试已达上限，需人工排查',updated_at=datetime('now') WHERE server_id=?`, serverID)
		return
	}
	if lastReconcileAt != "" {
		if t, parseErr := time.Parse("2006-01-02 15:04:05", lastReconcileAt); parseErr == nil {
			backoff := time.Duration(attempts) * s.tunnelReconcileBaseInterval
			if time.Since(t) < backoff {
				return
			}
		}
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='reconciling',last_stage='self_heal',reconcile_attempts=reconcile_attempts+1,last_reconcile_at=datetime('now'),last_error='',updated_at=datetime('now') WHERE server_id=? AND desired_status='running'`, serverID)
	token := secure.SecureDecrypt(tokenEncrypted)
	if token == "" {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='disconnected',last_error='自愈解密 token 失败',updated_at=datetime('now') WHERE server_id=?`, serverID)
		return
	}
	payload, _ := json.Marshal(cloudflaredTaskPayload("install", token))
	if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='disconnected',last_error='自愈重装 cloudflared 失败: '||?,updated_at=datetime('now') WHERE server_id=?`, err.Error(), serverID)
		return
	}
	var accountID, tunnelID string
	if err := db.QueryRowContext(ctx, `SELECT account_id,tunnel_id FROM managed_proxy_tunnels WHERE server_id=? AND desired_status='running'`, serverID).Scan(&accountID, &tunnelID); err != nil || tunnelID == "" {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='disconnected',last_error='自愈后读取隧道状态失败',updated_at=datetime('now') WHERE server_id=?`, serverID)
		return
	}
	connected := false
	for attempt := 0; attempt < s.tunnelHealthCheckAttempts; attempt++ {
		count, checkErr := s.cloudflare.ManagedTunnelConnections(ctx, accountID, tunnelID)
		if checkErr == nil && count > 0 {
			connected = true
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.tunnelHealthCheckDelay):
		}
	}
	if connected {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='running',last_stage='self_heal',last_error='',reconcile_attempts=0,last_reconcile_at='',updated_at=datetime('now') WHERE server_id=? AND desired_status='running'`, serverID)
		applog.Info(ctx, "serveragent", "managed tunnel self-heal succeeded", "server_id", serverID)
	} else {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='disconnected',last_error='自愈后 Cloudflare 仍未检测到连接',updated_at=datetime('now') WHERE server_id=? AND desired_status='running'`, serverID)
	}
}

func rewriteTunnelClientURI(raw, oldHostname, newHostname string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse Tunnel client URI: %w", err)
	}
	if strings.EqualFold(parsed.Hostname(), strings.TrimSpace(oldHostname)) {
		port := parsed.Port()
		if port == "" {
			port = "443"
		}
		parsed.Host = net.JoinHostPort(newHostname, port)
	}
	query := parsed.Query()
	query.Set("sni", newHostname)
	query.Set("host", newHostname)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func migrateManagedTunnelHostname(ctx context.Context, db *sql.DB, serverID, oldHostname, newHostname string) error {
	rows, err := db.QueryContext(ctx, `SELECT id,client_uri_encrypted FROM managed_proxy_nodes WHERE server_id=? AND access_mode='cloudflare_tunnel'`, serverID)
	if err != nil {
		return err
	}
	type update struct{ id, encrypted string }
	updates := []update{}
	for rows.Next() {
		var id, encrypted string
		if err := rows.Scan(&id, &encrypted); err != nil {
			rows.Close()
			return err
		}
		rewritten, err := rewriteTunnelClientURI(secure.SecureDecrypt(encrypted), oldHostname, newHostname)
		if err != nil {
			rows.Close()
			return err
		}
		encoded, err := secure.SecureEncrypt(rewritten)
		if err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, update{id: id, encrypted: encoded})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE managed_proxy_nodes SET public_host=?,tunnel_hostname=?,client_uri_encrypted=?,updated_at=datetime('now') WHERE id=?`, newHostname, newHostname, item.encrypted, item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func cloudflaredTaskPayload(operation, token string) map[string]interface{} {
	return map[string]interface{}{
		"operation": operation, "token": token, "version": cloudflaredVersion,
		"asset_url_amd64": cloudflaredAMD64URL, "asset_sha256_amd64": cloudflaredAMD64SHA256,
		"asset_url_arm64": cloudflaredARM64URL, "asset_sha256_arm64": cloudflaredARM64SHA256,
		"asset_url_windows_amd64": cloudflaredWindowsAMD64URL, "asset_sha256_windows_amd64": cloudflaredWindowsAMD64SHA256,
	}
}

func shortStableID(value string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(value))
	clean = strings.Trim(clean, "-")
	if len(clean) > 40 {
		clean = clean[:40]
	}
	if clean == "" {
		clean = uuid.NewString()[:12]
	}
	return clean
}

func loadTunnelIngress(ctx context.Context, db *sql.DB, serverID, hostname string) ([]cloudflare.ManagedTunnelIngress, error) {
	rows, err := db.QueryContext(ctx, `SELECT tunnel_path,assigned_port FROM managed_proxy_nodes WHERE server_id=? AND access_mode='cloudflare_tunnel' AND enabled=1 AND assigned_port>0 ORDER BY created_at ASC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cloudflare.ManagedTunnelIngress{}
	for rows.Next() {
		var path string
		var port int
		if err := rows.Scan(&path, &port); err != nil {
			return nil, err
		}
		items = append(items, cloudflare.ManagedTunnelIngress{Hostname: hostname, Path: path, Service: "http://127.0.0.1:" + strconv.Itoa(port)})
	}
	return items, rows.Err()
}

func (s *Service) uninstallManagedTunnel(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, 404, "managed Tunnel not found")
		} else {
			response.Error(w, 500, err.Error())
		}
		return
	}
	var nodeCount int
	if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_proxy_nodes WHERE server_id=? AND access_mode='cloudflare_tunnel'`, serverID).Scan(&nodeCount); err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	if nodeCount > 0 && r.URL.Query().Get("cascade") != "1" {
		response.Error(w, http.StatusConflict, fmt.Sprintf("该 Tunnel 仍关联 %d 个节点；确认级联删除后请使用 cascade=1", nodeCount))
		return
	}
	if !s.requireAgentCapability(w, serverID, "cloudflared_runtime_v1") {
		return
	}
	task, ok := s.createExclusiveProxyTask(w, serverID, "proxy.tunnel.uninstall", serverID)
	if !ok {
		return
	}
	go s.runManagedTunnelUninstall(task.ID, serverID)
	response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"task_id": task.ID, "status": task.Status}})
}

func (s *Service) runManagedTunnelUninstall(taskID, serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()
	var state managedTunnelState
	if err := db.QueryRowContext(ctx, `SELECT account_id,zone_id,tunnel_id,hostname,dns_record_id FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&state.AccountID, &state.ZoneID, &state.TunnelID, &state.Hostname, &state.DNSRecordID); err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	progress := func(value int, stage, message string) {
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='removing',last_stage=?,last_error='',updated_at=datetime('now') WHERE server_id=?`, stage, serverID)
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{"stage": stage, "message": message, "server_id": serverID})
	}
	failures := []string{}
	progress(8, "unpublish", "正在停止发布 Tunnel 节点")
	_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_nodes SET publishable=0,apply_status='removing',updated_at=datetime('now') WHERE server_id=? AND access_mode='cloudflare_tunnel'`, serverID)
	progress(20, "remove_nodes", "正在卸载主机上的 Tunnel 节点")
	rows, _ := db.QueryContext(ctx, `SELECT id,runtime,revision,assigned_port FROM managed_proxy_nodes WHERE server_id=? AND access_mode='cloudflare_tunnel'`, serverID)
	type nodeState struct {
		id, runtime string
		revision    int64
		port        int
	}
	nodes := []nodeState{}
	if rows != nil {
		for rows.Next() {
			var item nodeState
			_ = rows.Scan(&item.id, &item.runtime, &item.revision, &item.port)
			nodes = append(nodes, item)
		}
		rows.Close()
	}
	for _, node := range nodes {
		release, _ := managedProxyRuntime(node.runtime)
		payload, _ := json.Marshal(map[string]interface{}{"node_id": node.id, "revision": node.revision + 1, "runtime": "sing-box", "runtime_version": release.Version, "asset_url_amd64": release.AMD64URL, "asset_sha256_amd64": release.AMD64SHA256, "asset_url_arm64": release.ARM64URL, "asset_sha256_arm64": release.ARM64SHA256, "asset_format": release.AssetFormat, "config": "{}", "remove": true, "port_min": 45654, "port_max": 55654})
		if _, err := s.RunProxyRuntimeTaskAndWait(serverID, string(payload)); err != nil {
			failures = append(failures, "remove node "+node.id+": "+err.Error())
			continue
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM subscription_runtime_reconcile WHERE node_id=?`, node.id)
		_, _ = db.ExecContext(ctx, `DELETE FROM managed_proxy_nodes WHERE id=?`, node.id)
	}
	if len(failures) > 0 {
		message := strings.Join(failures, "; ")
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='cleanup_failed',last_stage='remove_nodes',last_error=?,updated_at=datetime('now') WHERE server_id=?`, message, serverID)
		s.taskRegistry.Fail(taskID, message)
		return
	}
	progress(45, "remove_connector", "正在停止并删除主机上的 cloudflared")
	payload, _ := json.Marshal(cloudflaredTaskPayload("remove", ""))
	if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
		failures = append(failures, "remove cloudflared: "+err.Error())
	}
	progress(62, "remove_dns", "正在删除 Tunnel DNS 记录")
	if state.DNSRecordID != "" {
		if err := s.cloudflare.DeleteManagedTunnelDNS(ctx, state.AccountID, state.ZoneID, state.DNSRecordID); err != nil {
			failures = append(failures, err.Error())
		} else {
			_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET dns_record_id='' WHERE server_id=?`, serverID)
		}
	}
	progress(80, "remove_tunnel", "正在删除 Cloudflare Named Tunnel")
	if state.TunnelID != "" {
		if err := s.cloudflare.DeleteManagedTunnel(ctx, state.AccountID, state.TunnelID); err != nil {
			failures = append(failures, err.Error())
		} else {
			_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET tunnel_id='',token_encrypted='' WHERE server_id=?`, serverID)
		}
	}
	if len(failures) > 0 {
		message := strings.Join(failures, "; ")
		_, _ = db.ExecContext(ctx, `UPDATE managed_proxy_tunnels SET apply_status='cleanup_failed',last_stage='cleanup_failed',last_error=?,updated_at=datetime('now') WHERE server_id=?`, message, serverID)
		s.taskRegistry.Fail(taskID, message)
		return
	}
	_, _ = db.ExecContext(ctx, `DELETE FROM managed_proxy_tunnels WHERE server_id=?`, serverID)
	s.taskRegistry.Complete(taskID, "Tunnel、DNS、cloudflared 与本地 Tunnel 节点已全部删除")
}

func (s *Service) listPreferredAddresses(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), `SELECT id,name,address,port,enabled,is_default,sort_order,last_status,last_latency_ms,last_error,COALESCE(checked_at,'') FROM managed_proxy_preferences ORDER BY enabled DESC,is_default DESC,sort_order ASC,created_at ASC`)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []preferredAddress{}
	for rows.Next() {
		var item preferredAddress
		var enabled, isDefault int
		if err := rows.Scan(&item.ID, &item.Name, &item.Address, &item.Port, &enabled, &isDefault, &item.SortOrder, &item.LastStatus, &item.LastLatencyMS, &item.LastError, &item.CheckedAt); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		item.Enabled, item.IsDefault = enabled == 1, isDefault == 1
		items = append(items, item)
	}
	response.OK(w, items)
}

func (s *Service) savePreferredAddress(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var input struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		Port      int    `json:"port"`
		Enabled   *bool  `json:"enabled"`
		IsDefault bool   `json:"is_default"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid preferred address")
		return
	}
	input.Name, input.Address = strings.TrimSpace(input.Name), strings.TrimSpace(input.Address)
	if input.Name == "" || input.Address == "" || strings.ContainsAny(input.Address, "/?#@ ") {
		response.Error(w, 400, "name and a valid domain or IP address are required")
		return
	}
	if input.Port == 0 {
		input.Port = 443
	}
	if input.Port < 1 || input.Port > 65535 {
		response.Error(w, 400, "port must be between 1 and 65535")
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	if input.IsDefault {
		if _, err := tx.ExecContext(r.Context(), `UPDATE managed_proxy_preferences SET is_default=0,updated_at=datetime('now')`); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
	}
	if id == "" {
		id = "pref-" + uuid.NewString()
		_, err = tx.ExecContext(r.Context(), `INSERT INTO managed_proxy_preferences(id,name,address,port,enabled,is_default,sort_order) VALUES(?,?,?,?,?,?,?)`, id, input.Name, input.Address, input.Port, boolToInt(enabled), boolToInt(input.IsDefault), input.SortOrder)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(r.Context(), `UPDATE managed_proxy_preferences SET name=?,address=?,port=?,enabled=?,is_default=?,sort_order=?,updated_at=datetime('now') WHERE id=?`, input.Name, input.Address, input.Port, boolToInt(enabled), boolToInt(input.IsDefault), input.SortOrder, id)
		if err == nil {
			if affected, _ := result.RowsAffected(); affected == 0 {
				err = sql.ErrNoRows
			}
		}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, 404, "preferred address not found")
		} else {
			response.Error(w, 500, err.Error())
		}
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": id})
}

func (s *Service) deletePreferredAddress(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var used int
	_ = db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_proxy_nodes WHERE preferred_address_id=?`, id).Scan(&used)
	if used > 0 {
		response.Error(w, 409, "preferred address is in use by managed nodes")
		return
	}
	result, err := db.ExecContext(r.Context(), `DELETE FROM managed_proxy_preferences WHERE id=?`, id)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		response.Error(w, 404, "preferred address not found")
		return
	}
	response.OK(w, map[string]bool{"deleted": true})
}
