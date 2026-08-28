package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// validTunnelHostname 校验自定义 Tunnel 域名（小写标签 + 连字符，合法主机名）。
func validTunnelHostname(hostname string) bool {
	if hostname == "" || len(hostname) > 253 || strings.ContainsAny(hostname, "/?#@ ") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, c := range label {
			if c == '-' && (i == 0 || i == len(label)-1) {
				return false
			}
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// forwardTunnelInstance 整域转发规则在源主机上的 cloudflared 实例标识（agent 侧按实例隔离 unit/pid/token）。
func forwardTunnelInstance(forwardID string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(forwardID))
	clean = strings.Trim(clean, "-")
	if clean == "" {
		clean = "fwd"
	}
	if len(clean) > 32 {
		clean = clean[:32]
	}
	return clean
}

// forwardTunnelTaskResource 整域规则独立隧道的排他任务键，与主机级隧道（proxy:<server>）隔离。
func forwardTunnelTaskResource(serverID, forwardID string) string {
	return "proxy:" + serverID + ":fwd:" + forwardID
}

func forwardCloudflaredTaskPayload(operation, token, forwardID string) map[string]interface{} {
	payload := cloudflaredTaskPayload(operation, token)
	payload["instance"] = forwardTunnelInstance(forwardID)
	return payload
}

func (s *Service) createExclusiveForwardTunnelTask(w http.ResponseWriter, item *managedForward) (*Task, bool) {
	s.taskRegistry.ReleaseStaleLeases(time.Now(), maxProxyTaskAge)
	task, err := s.taskRegistry.CreateExclusive(item.ServerID, "proxy.tunnel.forward.deploy", item.ID, forwardTunnelTaskResource(item.ServerID, item.ID))
	if err == nil {
		return task, true
	}
	if errors.Is(err, ErrTaskResourceBusy) {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "该转发规则的隧道编排任务正在执行，请等待完成后重试",
			"data":    map[string]interface{}{"server_id": item.ServerID, "forward_id": item.ID},
		})
		return nil, false
	}
	response.Error(w, http.StatusInternalServerError, err.Error())
	return nil, false
}

// deployForwardTunnel 整域 CF 转发部署独立 Named Tunnel（异步任务，hostname 取规则自定义域名）。
func (s *Service) deployForwardTunnel(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	if s.cloudflare == nil {
		response.Error(w, http.StatusServiceUnavailable, "Cloudflare integration is unavailable")
		return
	}
	if strings.TrimSpace(item.TunnelHostname) == "" {
		response.Error(w, 422, "整域转发需要自定义 Tunnel 域名（tunnel_hostname）")
		return
	}
	if !s.requireAgentCapability(w, item.ServerID, "cloudflared_runtime_v1") {
		return
	}
	task, ok := s.createExclusiveForwardTunnelTask(w, item)
	if !ok {
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET desired_status='running',apply_status='deploying',last_stage='deploy_tunnel',last_error='',updated_at=datetime('now') WHERE id=?`, item.ID)
	go s.runForwardTunnelDeploy(task.ID, item.ID)
	response.JSON(w, http.StatusAccepted, map[string]interface{}{"success": true, "data": map[string]interface{}{"task_id": task.ID, "status": task.Status}})
}

// runForwardTunnelDeploy 独立隧道实际部署：建 Named Tunnel + ingress + DNS + 安装 cloudflared 实例。
func (s *Service) runForwardTunnelDeploy(taskID, forwardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		s.taskRegistry.Fail(taskID, err.Error())
		return
	}
	defer db.Close()

	item := s.loadForward(ctx, db, forwardID)
	if item == nil {
		s.taskRegistry.Fail(taskID, "转发规则不存在")
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET desired_status='running',apply_status='deploying',last_stage='deploy_tunnel',last_error='',updated_at=datetime('now') WHERE id=?`, forwardID)
	fail := func(stage string, cause error) {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_tunnel',last_error=?,tunnel_apply_status='failed',tunnel_last_stage=?,tunnel_last_error=?,updated_at=datetime('now') WHERE id=?`, cause.Error(), stage, cause.Error(), forwardID)
		s.taskRegistry.Fail(taskID, cause.Error())
	}
	progress := func(value int, stage, message string) {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage=?,last_error='',tunnel_last_stage=?,updated_at=datetime('now') WHERE id=?`, stage, stage, forwardID)
		s.taskRegistry.UpdateProgress(taskID, value, map[string]interface{}{"stage": stage, "message": message, "forward_id": forwardID})
	}

	hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item.TunnelHostname), "."))
	if hostname == "" {
		fail("validate_hostname", errors.New("整域转发需要自定义 Tunnel 域名"))
		return
	}
	var accountID, zoneID string
	if err := db.QueryRowContext(ctx, `SELECT account_id,zone_id FROM managed_proxy_tunnels WHERE server_id=?`, item.ServerID).Scan(&accountID, &zoneID); err != nil {
		fail("load_host_tunnel", errors.New("源主机未部署主机级 Tunnel，无法确定 Cloudflare 账号与 Zone"))
		return
	}

	progress(10, "preflight", "正在校验自定义域名所属 Zone 与权限")
	preflight, err := s.cloudflare.PreflightManagedTunnel(ctx, accountID, zoneID, hostname)
	if err != nil {
		fail("preflight", err)
		return
	}

	var tunnelID, dnsRecordID string
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(tunnel_id,''),COALESCE(dns_record_id,'') FROM managed_forwards WHERE id=?`, forwardID).Scan(&tunnelID, &dnsRecordID)
	created := false
	if tunnelID == "" {
		progress(20, "create_tunnel", "正在创建独立 Named Tunnel")
		tunnelName := "fwd-" + forwardTunnelInstance(forwardID)
		t, createErr := s.cloudflare.CreateManagedTunnel(ctx, accountID, tunnelName)
		if createErr != nil {
			fail("create_tunnel", createErr)
			return
		}
		tunnelID, created = t.ID, true
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_id=?,tunnel_zone_id=?,tunnel_zone_name=?,tunnel_revision=tunnel_revision+1,updated_at=datetime('now') WHERE id=?`, tunnelID, zoneID, preflight.ZoneName, forwardID)
	}

	// token/panel：源主机启动鉴权代理，ingress 指向代理端口
	authProxyPort := item.AuthProxyPort
	if needsTokenAuth(item) {
		port, authErr := s.startForwardAuthProxy(ctx, db, item)
		if authErr != nil {
			if created {
				_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, tunnelID)
				_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_id='',tunnel_token_encrypted='',tunnel_apply_status='failed',tunnel_last_error='鉴权代理启动失败',updated_at=datetime('now') WHERE id=?`, forwardID)
			}
			fail("deploy_auth_proxy", authErr)
			return
		}
		authProxyPort = port
	}

	svc := fmt.Sprintf("http://%s:%d", item.LocalHost, item.LocalPort)
	if item.Protocol == "tcp" {
		svc = fmt.Sprintf("tcp://%s:%d", item.LocalHost, item.LocalPort)
	}
	if (item.AccessMode == "token" || item.AccessMode == "panel") && authProxyPort > 0 {
		svc = fmt.Sprintf("http://127.0.0.1:%d", authProxyPort)
	}
	progress(35, "configure_ingress", "正在配置独立 Tunnel 路由")
	if err := s.cloudflare.ConfigureManagedTunnel(ctx, accountID, tunnelID, []cloudflare.ManagedTunnelIngress{{Hostname: hostname, Path: "", Service: svc}}); err != nil {
		if created {
			_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, tunnelID)
			_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_id='',tunnel_token_encrypted='',updated_at=datetime('now') WHERE id=?`, forwardID)
		}
		fail("configure_ingress", err)
		return
	}

	progress(45, "configure_dns", "正在创建自定义域名 DNS 记录")
	dns, err := s.cloudflare.EnsureManagedTunnelDNS(ctx, accountID, zoneID, hostname, tunnelID)
	if err != nil {
		if created {
			_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, tunnelID)
			_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_id='',tunnel_token_encrypted='',updated_at=datetime('now') WHERE id=?`, forwardID)
		}
		fail("configure_dns", err)
		return
	}
	dnsRecordID = dns.ID
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET dns_record_id=?,tunnel_hostname=?,tunnel_path='',updated_at=datetime('now') WHERE id=?`, dnsRecordID, hostname, forwardID)

	progress(58, "retrieve_token", "正在获取独立 Tunnel 令牌")
	token, err := s.cloudflare.ManagedTunnelToken(ctx, accountID, tunnelID)
	if err != nil {
		fail("retrieve_token", err)
		return
	}
	encToken, err := secure.SecureEncrypt(token)
	if err != nil {
		fail("encrypt_token", err)
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_token_encrypted=?,updated_at=datetime('now') WHERE id=?`, encToken, forwardID)

	progress(70, "install_connector", "正在源主机安装独立 cloudflared 实例")
	payload, _ := json.Marshal(forwardCloudflaredTaskPayload("install", token, forwardID))
	if _, err := s.RunCloudflaredTaskAndWait(item.ServerID, string(payload)); err != nil {
		fail("install_connector", err)
		return
	}

	progress(88, "verify_connector", "cloudflared 已启动，正在确认边缘连接")
	connected := false
	for attempt := 0; attempt < 8; attempt++ {
		count, checkErr := s.cloudflare.ManagedTunnelConnections(ctx, accountID, tunnelID)
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
		fail("verify_connector", errors.New("cloudflared 已启动但 Cloudflare 未报告活跃连接；请检查主机出口"))
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='running',tunnel_last_stage='completed',tunnel_last_error='',apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, forwardID)
	s.taskRegistry.Complete(taskID, fmt.Sprintf("独立 Tunnel %s 已连接", hostname))
}

// startForwardAuthProxy 在源主机启动 token/panel 鉴权代理，返回实际代理端口。
func (s *Service) startForwardAuthProxy(ctx context.Context, db *sql.DB, item *managedForward) (int, error) {
	srcConn, ok := s.registry.Get(item.ServerID)
	if !ok {
		return 0, errors.New("源主机 Agent 离线，无法启动鉴权代理")
	}
	meta := srcConn.GetMetadata()
	proxyURL, proxySHA, proxyOK := authProxyAssetFor(fmt.Sprint(meta["platform"]), fmt.Sprint(meta["arch"]))
	if !proxyOK {
		return 0, errors.New("不支持该主机的 auth-proxy 资产")
	}
	var enc string
	_ = db.QueryRowContext(ctx, `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
	token := secure.SecureDecrypt(enc)
	proxyPayload, _ := json.Marshal(map[string]interface{}{
		"operation": "auth_proxy_start", "forward_id": item.ID,
		"token":      token,
		"local_host": item.LocalHost, "local_port": item.LocalPort,
		"relay_asset_url": proxyURL, "relay_asset_sha256": proxySHA,
	})
	out, err := s.RunTCPForwarderTaskAndWait(item.ServerID, string(proxyPayload))
	if err != nil {
		return 0, errors.New("鉴权代理启动失败: " + err.Error())
	}
	var proxyResp struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal([]byte(out), &proxyResp); err != nil || proxyResp.Port < 1 || proxyResp.Port > 65535 {
		return 0, errors.New("鉴权代理未返回有效端口: " + out)
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET auth_proxy_port=? WHERE id=?`, proxyResp.Port, item.ID)
	return proxyResp.Port, nil
}

// removeForwardTunnel 卸载整域规则独立隧道。keepTunnel=true（stop）时仅停实例保留 Named Tunnel 与 DNS。
func (s *Service) removeForwardTunnel(ctx context.Context, db *sql.DB, item *managedForward, keepTunnel bool) {
	if item.Transport != "cloudflare_tunnel" || !item.WholeHost || item.TunnelID == "" {
		return
	}
	var zoneID, tunnelID, dnsRecordID string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(tunnel_zone_id,''),COALESCE(tunnel_id,''),COALESCE(dns_record_id,'') FROM managed_forwards WHERE id=?`, item.ID).Scan(&zoneID, &tunnelID, &dnsRecordID); err != nil || tunnelID == "" {
		return
	}
	if _, ok := s.registry.Get(item.ServerID); ok {
		payload, _ := json.Marshal(forwardCloudflaredTaskPayload("remove", "", item.ID))
		_, _ = s.RunCloudflaredTaskAndWait(item.ServerID, string(payload))
	}
	if keepTunnel {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='stopped',tunnel_last_stage='stopped',updated_at=datetime('now') WHERE id=?`, item.ID)
		return
	}
	var accountID string
	_ = db.QueryRowContext(ctx, `SELECT account_id FROM managed_proxy_tunnels WHERE server_id=?`, item.ServerID).Scan(&accountID)
	if dnsRecordID != "" && accountID != "" && zoneID != "" {
		_ = s.cloudflare.DeleteManagedTunnelDNS(context.Background(), accountID, zoneID, dnsRecordID)
	}
	if accountID != "" {
		_ = s.cloudflare.DeleteManagedTunnel(context.Background(), accountID, tunnelID)
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_id='',tunnel_zone_id='',tunnel_zone_name='',dns_record_id='',tunnel_token_encrypted='',tunnel_apply_status='',tunnel_last_stage='',tunnel_last_error='',auth_proxy_port=0,updated_at=datetime('now') WHERE id=?`, item.ID)
}

// reconcileForwardTunnelHealth 周期检查整域规则独立隧道的边缘连接，断开则自愈；
// 已迁移（有 hostname）但尚未部署独立隧道的规则在此触发部署。
func (s *Service) reconcileForwardTunnelHealth(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id FROM managed_forwards WHERE transport='cloudflare_tunnel' AND whole_host=1 AND desired_status='running' AND (tunnel_id<>'' OR tunnel_hostname<>'') AND tunnel_apply_status IN ('running','disconnected','reconciling','')`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		go s.reconcileForwardTunnelConnection(id)
	}
}

func (s *Service) reconcileForwardTunnelConnection(forwardID string) {
	if s.cloudflare == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	var serverID, tunnelID, applyStatus, lastReconcileAt, hostname string
	if err := db.QueryRowContext(ctx, `SELECT server_id,COALESCE(tunnel_id,''),COALESCE(tunnel_apply_status,''),COALESCE(tunnel_last_reconcile_at,''),COALESCE(tunnel_hostname,'') FROM managed_forwards WHERE id=? AND desired_status='running' AND whole_host=1`, forwardID).Scan(&serverID, &tunnelID, &applyStatus, &lastReconcileAt, &hostname); err != nil {
		return
	}
	if tunnelID == "" {
		// 存量整域规则已分配域名但独立隧道尚未部署：触发部署
		if hostname == "" {
			return
		}
		s.triggerForwardTunnelDeploy(serverID, forwardID)
		return
	}
	if _, busy := s.taskRegistry.ActiveTask(forwardTunnelTaskResource(serverID, forwardID)); busy {
		return
	}
	if applyStatus == "reconciling" && lastReconcileAt != "" {
		if t, parseErr := time.Parse("2006-01-02 15:04:05", lastReconcileAt); parseErr == nil && time.Since(t) < 10*time.Minute {
			return
		}
	}
	var accountID string
	if err := db.QueryRowContext(ctx, `SELECT account_id FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&accountID); err != nil {
		return
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
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='running',tunnel_last_stage='health_check',tunnel_last_error='',tunnel_reconcile_attempts=0,tunnel_last_reconcile_at='',updated_at=datetime('now') WHERE id=?`, forwardID)
		return
	}
	if checkErr != nil {
		applog.Warn(ctx, "serveragent", "forward tunnel health check failed", "forward_id", forwardID, "error", checkErr.Error())
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='disconnected',tunnel_last_stage='health_check',tunnel_last_error='Cloudflare 未检测到 cloudflared 连接',updated_at=datetime('now') WHERE id=?`, forwardID)
	s.attemptForwardTunnelSelfHeal(forwardID)
}

func (s *Service) attemptForwardTunnelSelfHeal(forwardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	var serverID, tokenEncrypted, lastReconcileAt string
	var attempts int
	if err := db.QueryRowContext(ctx, `SELECT server_id,COALESCE(tunnel_token_encrypted,''),tunnel_reconcile_attempts,COALESCE(tunnel_last_reconcile_at,'') FROM managed_forwards WHERE id=? AND desired_status='running' AND whole_host=1 AND tunnel_apply_status='disconnected'`, forwardID).Scan(&serverID, &tokenEncrypted, &attempts, &lastReconcileAt); err != nil {
		return
	}
	if _, busy := s.taskRegistry.ActiveTask(forwardTunnelTaskResource(serverID, forwardID)); busy {
		return
	}
	if attempts >= s.tunnelReconcileMaxAttempts {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_last_error='自愈重试已达上限，需人工排查',updated_at=datetime('now') WHERE id=?`, forwardID)
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
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='reconciling',tunnel_last_stage='self_heal',tunnel_reconcile_attempts=tunnel_reconcile_attempts+1,tunnel_last_reconcile_at=datetime('now'),tunnel_last_error='',updated_at=datetime('now') WHERE id=?`, forwardID)
	token := secure.SecureDecrypt(tokenEncrypted)
	if token == "" {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='disconnected',tunnel_last_error='自愈解密 token 失败',updated_at=datetime('now') WHERE id=?`, forwardID)
		return
	}
	payload, _ := json.Marshal(forwardCloudflaredTaskPayload("install", token, forwardID))
	if _, err := s.RunCloudflaredTaskAndWait(serverID, string(payload)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='disconnected',tunnel_last_error='自愈重装 cloudflared 失败: '||?,updated_at=datetime('now') WHERE id=?`, err.Error(), forwardID)
		return
	}
	var accountID, tunnelID string
	_ = db.QueryRowContext(ctx, `SELECT account_id FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&accountID)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(tunnel_id,'') FROM managed_forwards WHERE id=?`, forwardID).Scan(&tunnelID)
	if accountID == "" || tunnelID == "" {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='disconnected',tunnel_last_error='自愈后读取隧道状态失败',updated_at=datetime('now') WHERE id=?`, forwardID)
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
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='running',tunnel_last_stage='self_heal',tunnel_last_error='',tunnel_reconcile_attempts=0,tunnel_last_reconcile_at='',updated_at=datetime('now') WHERE id=?`, forwardID)
		applog.Info(ctx, "serveragent", "forward tunnel self-heal succeeded", "forward_id", forwardID)
	} else {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='disconnected',tunnel_last_error='自愈后 Cloudflare 仍未检测到连接',updated_at=datetime('now') WHERE id=?`, forwardID)
	}
}

// reconcileForwardTunnelInstance agent 重连后重放整域规则的 cloudflared 实例（不重建 Named Tunnel）。
func (s *Service) reconcileForwardTunnelInstance(ctx context.Context, db *sql.DB, item *managedForward) error {
	var tokenEncrypted string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(tunnel_token_encrypted,'') FROM managed_forwards WHERE id=?`, item.ID).Scan(&tokenEncrypted); err != nil || tokenEncrypted == "" {
		return errors.New("独立隧道令牌缺失")
	}
	token := secure.SecureDecrypt(tokenEncrypted)
	if token == "" {
		return errors.New("独立隧道令牌解密失败")
	}
	payload, _ := json.Marshal(forwardCloudflaredTaskPayload("install", token, item.ID))
	if _, err := s.RunCloudflaredTaskAndWait(item.ServerID, string(payload)); err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_apply_status='running',tunnel_last_stage='reconcile',tunnel_last_error='',updated_at=datetime('now') WHERE id=?`, item.ID)
	return nil
}

// triggerForwardTunnelDeploy 非 HTTP 路径触发整域规则独立隧道部署（迁移/健康循环复用）。
func (s *Service) triggerForwardTunnelDeploy(serverID, forwardID string) {
	key := forwardTunnelTaskResource(serverID, forwardID)
	if _, busy := s.taskRegistry.ActiveTask(key); busy {
		return
	}
	task, err := s.taskRegistry.CreateExclusive(serverID, "proxy.tunnel.forward.deploy", forwardID, key)
	if err != nil {
		return
	}
	go s.runForwardTunnelDeploy(task.ID, forwardID)
}

// migrateLegacyWholeHostTunnels 为历史整域 CF 转发自动分配独立子域名（fwd-<id>.<zone>），
// 供独立隧道健康循环自动部署；已分配或非整域的规则跳过。
func migrateLegacyWholeHostTunnels(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id,server_id FROM managed_forwards WHERE transport='cloudflare_tunnel' AND whole_host=1 AND tunnel_id='' AND tunnel_hostname=''`)
	if err != nil {
		return err
	}
	type legacyItem struct{ id, serverID string }
	var list []legacyItem
	for rows.Next() {
		var it legacyItem
		if rows.Scan(&it.id, &it.serverID) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	for _, it := range list {
		var zoneName string
		if err := db.QueryRowContext(ctx, `SELECT zone_name FROM managed_proxy_tunnels WHERE server_id=?`, it.serverID).Scan(&zoneName); err != nil || zoneName == "" {
			continue
		}
		hostname := forwardTunnelInstance(it.id) + "." + zoneName
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_hostname=?,updated_at=datetime('now') WHERE id=?`, hostname, it.id)
	}
	return nil
}
