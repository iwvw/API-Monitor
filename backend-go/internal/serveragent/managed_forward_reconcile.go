package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// reconcileRunningForwards 在 Agent 重连/上线后重放其负责的 running 转发：
// 源角色重建反向桥接隧道、中继角色重下发监听规则，解决 agent/relay 重启后
// 转发链路不自动恢复（relay 监听仍在但数据不通）的问题。
// 同一主机 60 秒内只执行一次，避免 agent 频繁重连触发部署风暴。
func (s *Service) reconcileRunningForwards(ctx context.Context, db *sql.DB, serverID string) {
	if serverID == "" {
		return
	}
	if _, busy := s.taskRegistry.ActiveTask("fwd-reconcile-" + serverID); busy {
		return
	}
	now := time.Now()
	s.forwardReconcileMu.Lock()
	last, ok := s.forwardReconcileAt[serverID]
	if ok && now.Sub(last) < 60*time.Second {
		s.forwardReconcileMu.Unlock()
		return
	}
	s.forwardReconcileAt[serverID] = now
	s.forwardReconcileMu.Unlock()

	rows, err := db.QueryContext(ctx, `SELECT id FROM managed_forwards WHERE apply_status='running' AND desired_status='running' AND (server_id=? OR relay_server_id=?)`, serverID, serverID)
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
		item := s.loadForward(ctx, db, id)
		if item == nil {
			continue
		}
		var code int
		var reapplyErr error
		switch item.Transport {
		case "tcp_relay":
			code, reapplyErr = s.deployTCPRelayCore(ctx, db, item)
		case "p2p":
			code, reapplyErr = s.deployP2PCore(ctx, db, item)
		case "cloudflare_tunnel":
			if item.ServerID != serverID {
				continue
			}
			if item.WholeHost {
				// 整域独立隧道：仅重放 cloudflared 实例，Named Tunnel/DNS 已存在
				reapplyErr = s.reconcileForwardTunnelInstance(ctx, db, item)
			} else {
				code, reapplyErr = s.deployCloudflareTunnelCore(ctx, db, item)
			}
		default:
			continue
		}
		if reapplyErr != nil {
			applog.Warn(ctx, "serveragent", "forward reconcile reapply failed", "forward_id", id, "status", code, "error", reapplyErr.Error())
		}
	}
}

// sourceClientCapabilityIssue 校验源主机在线且具备 tcp_forwarder_v1 能力，返回问题描述（空=可用）。
func (s *Service) sourceClientCapabilityIssue(serverID string) string {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return "源主机 Agent 离线，无法建立隧道"
	}
	if !conn.GetCapabilities()["tcp_forwarder_v1"] {
		return "源主机 Agent 未启用 TCP 转发能力（请先将 Agent 升级到支持跨平台的版本）"
	}
	return ""
}

// allocateRelayPort 在事务内分配并占用中继端口，避免并发部署撞同端口。
// 排除规则自身当前占用：重试/重启部署时沿用同一端口，避免入口地址每次漂移。
func allocateRelayPort(ctx context.Context, db *sql.DB, item *managedForward, relayServerID string) int {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0
	}
	defer tx.Rollback()
	for port := 55655; port <= 60655; port++ {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM managed_forwards WHERE relay_server_id=? AND remote_port=? AND id<>?`, relayServerID, port, item.ID).Scan(&exists); err != nil {
			// 端口空闲：更新占用并提交
			if _, err := tx.ExecContext(ctx, `UPDATE managed_forwards SET remote_port=? WHERE id=?`, port, item.ID); err == nil {
				_ = tx.Commit()
				return port
			}
			return 0
		}
	}
	return 0
}

// removeCFAuthProxy 停止并回收 CF 隧道 token/panel 转发的源主机鉴权代理（agent 离线时静默跳过）。
func (s *Service) removeCFAuthProxy(ctx context.Context, db *sql.DB, item *managedForward) {
	if item.Transport != "cloudflare_tunnel" || item.ID == "" || !needsTokenAuth(item) {
		return
	}
	if _, ok := s.registry.Get(item.ServerID); ok {
		stopPayload, _ := json.Marshal(map[string]interface{}{"operation": "auth_proxy_stop", "forward_id": item.ID})
		_, _ = s.RunTCPForwarderTaskAndWait(item.ServerID, string(stopPayload))
	}
}

// removeTCPRelayTunnels 拆除 tcp_relay 链路：源主机关隧道，入口主机关监听。
// 在 stop/delete 时调用；agent 离线时静默跳过。
func (s *Service) removeTCPRelayTunnels(ctx context.Context, db *sql.DB, item *managedForward) {
	if item.Transport != "tcp_relay" || item.ID == "" {
		return
	}
	// 入口主机：撤销监听（带端口）
	var relayPort int
	if item.RemotePort > 0 {
		relayPort = item.RemotePort
	} else {
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(remote_port,0) FROM managed_forwards WHERE id=?`, item.ID).Scan(&relayPort)
	}
	if item.RelayServerID != "" {
		unlistenPayload, _ := json.Marshal(map[string]interface{}{
			"operation": "unlisten", "forward_id": item.ID, "relay_port": relayPort,
		})
		if _, ok := s.registry.Get(item.RelayServerID); ok {
			_, _ = s.RunTCPForwarderTaskAndWait(item.RelayServerID, string(unlistenPayload))
		}
	}
	// 源主机：断隧道
	removePayload, _ := json.Marshal(map[string]interface{}{"operation": "remove", "forward_id": item.ID})
	if _, ok := s.registry.Get(item.ServerID); ok {
		_, _ = s.RunTCPForwarderTaskAndWait(item.ServerID, string(removePayload))
	}
}

func (s *Service) syncForwardIngress(ctx context.Context, db *sql.DB, serverID string) error {
	ingress, err := loadTunnelIngress(ctx, db, serverID, "")
	if err != nil {
		return fmt.Errorf("load tunnel ingress: %w", err)
	}
	var accountID, tunnelID, zoneName string
	if err := db.QueryRowContext(ctx, `SELECT account_id,tunnel_id,zone_name FROM managed_proxy_tunnels WHERE server_id=?`, serverID).Scan(&accountID, &tunnelID, &zoneName); err != nil {
		return fmt.Errorf("tunnel not found: %w", err)
	}
	forwardRows, err := db.QueryContext(ctx, `SELECT id,protocol,local_host,local_port,tunnel_hostname,tunnel_path,access_mode,auth_proxy_port FROM managed_forwards WHERE server_id=? AND transport='cloudflare_tunnel' AND whole_host=0 AND desired_status='running' AND apply_status IN ('running','deploying') ORDER BY created_at ASC`, serverID)
	if err != nil {
		return fmt.Errorf("query forwards: %w", err)
	}
	defer forwardRows.Close()
	for forwardRows.Next() {
		var fwdID, protocol, localHost, tunnelHostname, tunnelPath, accessMode string
		var localPort, authProxyPort int
		if err := forwardRows.Scan(&fwdID, &protocol, &localHost, &localPort, &tunnelHostname, &tunnelPath, &accessMode, &authProxyPort); err != nil {
			return fmt.Errorf("scan forward: %w", err)
		}
		svc := fmt.Sprintf("http://%s:%d", localHost, localPort)
		// token/panel 模式：由源主机鉴权代理把关，ingress 指向代理端口而非本地服务
		if (accessMode == "token" || accessMode == "panel") && authProxyPort > 0 {
			svc = fmt.Sprintf("http://127.0.0.1:%d", authProxyPort)
		} else if protocol == "tcp" {
			svc = fmt.Sprintf("tcp://%s:%d", localHost, localPort)
		}
		ingress = append(ingress, cloudflare.ManagedTunnelIngress{
			Hostname: tunnelHostname,
			Path:     tunnelPath,
			Service:  svc,
		})
	}
	if err := s.cloudflare.ConfigureManagedTunnel(ctx, accountID, tunnelID, ingress); err != nil {
		return fmt.Errorf("configure tunnel ingress: %w", err)
	}
	return nil
}

func (s *Service) removeForwardIngress(ctx context.Context, db *sql.DB, serverID, tunnelPath string) {
	_ = s.syncForwardIngress(ctx, db, serverID)
}

func (s *Service) handleForwardPanelProxy(w http.ResponseWriter, r *http.Request, db *sql.DB, id string, rest []string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	if item.AccessMode != "panel" {
		response.Error(w, 422, "该转发非 panel 访问模式")
		return
	}
	var upstream string
	switch item.Transport {
	case "tcp_relay":
		host := item.RelayServerHost
		if host == "" {
			response.Error(w, 422, "中继入口主机未配置可连接地址")
			return
		}
		if item.RemotePort < 1 {
			response.Error(w, 422, "转发尚未部署（无中继端口）")
			return
		}
		upstream = fmt.Sprintf("http://%s:%d", host, item.RemotePort)
	case "cloudflare_tunnel":
		if item.AuthProxyPort < 1 {
			response.Error(w, 422, "转发尚未部署（无鉴权代理端口）")
			return
		}
		// 面板经 Agent 通道直连源主机 auth-proxy（127.0.0.1:<port> + token 校验），
		// 完全绕过 Cloudflare 边缘——数据中心出口经公网域名访问 CF 隧道会被边缘
		// 1001/403 拒绝，此通道不受影响。
		var enc string
		_ = db.QueryRowContext(r.Context(), `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
		token := secure.SecureDecrypt(enc)
		if token == "" {
			response.Error(w, 500, "无法读取转发令牌")
			return
		}
		headers := map[string]string{}
		for _, k := range []string{"accept", "accept-language", "user-agent"} {
			if v := r.Header.Get(k); v != "" {
				headers[k] = v
			}
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"operation": "http_proxy", "forward_id": item.ID,
			"auth_proxy_port": item.AuthProxyPort, "token": token,
			"method": r.Method, "path": "/" + strings.Join(rest, "/"),
			"headers": headers,
		})
		out, err := s.RunTCPForwarderTaskAndWait(item.ServerID, string(payload))
		if err != nil {
			response.Error(w, http.StatusBadGateway, "经 Agent 访问源主机失败: "+err.Error())
			return
		}
		var pr struct {
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if json.Unmarshal([]byte(out), &pr) != nil {
			response.Error(w, http.StatusBadGateway, "Agent 返回异常: "+out)
			return
		}
		for k, v := range pr.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(pr.Status)
		_, _ = w.Write([]byte(pr.Body))
	default:
		response.Error(w, 422, "panel 代理仅支持 tcp_relay / cloudflare_tunnel")
		return
	}
	var enc string
	_ = db.QueryRowContext(r.Context(), `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
	token := secure.SecureDecrypt(enc)
	if token == "" {
		response.Error(w, 500, "无法读取转发令牌")
		return
	}
	target, err := url.Parse(upstream)
	if err != nil {
		response.Error(w, 500, "upstream 解析失败: "+err.Error())
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	r.URL.Path = "/" + strings.Join(rest, "/")
	// 面板会话已鉴权（前缀路由），此处注入转发 token 通过数据面校验
	r.Header.Set("Authorization", "Bearer "+token)
	proxy.ServeHTTP(w, r)
}

func (s *Service) stopManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET desired_status='stopped',apply_status='stopped',last_stage='stopped',updated_at=datetime('now') WHERE id=?`, id)
	if item.Transport == "cloudflare_tunnel" {
		if item.WholeHost {
			// 整域独立隧道：停实例，保留 Named Tunnel/DNS 便于快速恢复
			_ = s.removeForwardTunnel(context.Background(), db, item, true)
		} else if item.TunnelPath != "" {
			s.removeForwardIngress(context.Background(), db, item.ServerID, item.TunnelPath)
		}
	}
	// CF 隧道 token/panel：回收源主机鉴权代理进程
	s.removeCFAuthProxy(context.Background(), db, item)
	// tcp_relay：置 stopped 只是数据库状态，还需给 agent 发卸载指令才能真正断流
	s.removeTCPRelayTunnels(context.Background(), db, item)
	response.OK(w, map[string]string{"message": "转发规则已停止"})
}

func (s *Service) startManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET desired_status='running',apply_status='pending',last_stage='pending',last_error='',updated_at=datetime('now') WHERE id=?`, id)
	item.DesiredStatus = "running"
	item.ApplyStatus = "pending"
	s.deployManagedForward(w, r, db, id)
}

func (s *Service) preflightManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input struct {
		ForwardID     string `json:"forward_id"`
		ServerID      string `json:"server_id"`
		LocalHost     string `json:"local_host"`
		Transport     string `json:"transport"`
		LocalPort     int    `json:"local_port"`
		RelayServerID string `json:"relay_server_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid request body")
		return
	}
	checks := []map[string]interface{}{}
	allPassed := true
	if input.ServerID != "" {
		_, online := s.registry.Get(input.ServerID)
		serverOnline := online
		checks = append(checks, map[string]interface{}{
			"name": "源主机在线", "passed": serverOnline,
		})
		if !serverOnline {
			allPassed = false
		}
	}
	if input.Transport == "cloudflare_tunnel" && input.ServerID != "" {
		var tunnelOK int
		_ = db.QueryRowContext(r.Context(), `SELECT 1 FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running'`, input.ServerID).Scan(&tunnelOK)
		checks = append(checks, map[string]interface{}{
			"name": "CF Tunnel 已就绪", "passed": tunnelOK == 1,
		})
		if tunnelOK == 0 {
			allPassed = false
		}
	}
	// 同一源主机允许多条转发规则共用同一本地服务（local_host:local_port）：每条规则
	// 各有独立入口（CF 隧道路径 / 中继远程端口），端口共享为受支持语义，不做冲突拦截。
	response.OK(w, map[string]interface{}{
		"passed": allPassed,
		"checks": checks,
	})
}
