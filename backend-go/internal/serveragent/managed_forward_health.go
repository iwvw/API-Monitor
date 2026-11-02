package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listForwardTargets(w http.ResponseWriter, r *http.Request, db *sql.DB, forwardID string) {
	rows, err := db.QueryContext(r.Context(), `SELECT t.id,t.forward_id,t.server_id,COALESCE(a.name,''),t.priority,t.role,t.health_status,t.last_checked_at,t.last_error,t.created_at,t.updated_at FROM managed_forward_targets t LEFT JOIN server_accounts a ON a.id=t.server_id WHERE t.forward_id=? ORDER BY t.priority ASC`, forwardID)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []managedForwardTarget{}
	for rows.Next() {
		var item managedForwardTarget
		if err := rows.Scan(&item.ID, &item.ForwardID, &item.ServerID, &item.ServerName, &item.Priority, &item.Role, &item.HealthStatus, &item.LastChecked, &item.LastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		items = append(items, item)
	}
	response.OK(w, items)
}

func (s *Service) addForwardTarget(w http.ResponseWriter, r *http.Request, db *sql.DB, forwardID string) {
	var input struct {
		ServerID string `json:"server_id"`
		Priority int    `json:"priority"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid request body")
		return
	}
	if input.ServerID == "" {
		response.Error(w, 400, "server_id is required")
		return
	}
	if input.Role == "" {
		input.Role = "standby"
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM server_accounts WHERE id=?`, input.ServerID).Scan(&exists); err != nil {
		response.Error(w, 404, "server not found")
		return
	}
	id := generateTargetID()
	_, err := db.ExecContext(r.Context(), `INSERT INTO managed_forward_targets(id,forward_id,server_id,priority,role) VALUES(?,?,?,?,?)`, id, forwardID, input.ServerID, input.Priority, input.Role)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	response.JSON(w, http.StatusCreated, map[string]interface{}{"success": true, "data": map[string]string{"id": id}})
}

func (s *Service) removeForwardTarget(w http.ResponseWriter, r *http.Request, db *sql.DB, forwardID, targetID string) {
	_, err := db.ExecContext(r.Context(), `DELETE FROM managed_forward_targets WHERE id=? AND forward_id=?`, targetID, forwardID)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	response.OK(w, map[string]string{"message": "目标已删除"})
}

func (s *Service) startForwardHealthLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkForwardHealth(ctx)
		}
	}
}

// startForwardConnectorSyncLoop 周期同步 tcp_relay 活跃连接数（连接数来自源主机 Agent 的 status）。
func (s *Service) startForwardConnectorSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncForwardConnectors(ctx)
		}
	}
}

func (s *Service) syncForwardConnectors(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, server_id FROM managed_forwards WHERE transport='tcp_relay' AND desired_status='running'`)
	if err != nil {
		return
	}
	type frow struct{ id, serverID string }
	var list []frow
	for rows.Next() {
		var r frow
		if rows.Scan(&r.id, &r.serverID) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	for _, r := range list {
		n, connected := s.RunTCPForwarderStatus(r.serverID, r.id)
		if !connected {
			continue // 离线/未建立隧道：保留上次值，避免误报「无连接」
		}
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_forwards SET connector_count=? WHERE id=?`, n, r.id)
	}
}

func (s *Service) checkForwardHealth(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id,server_id,local_host,local_port,protocol,failover_current_server_id,failover_enabled,health_check_enabled,health_check_interval,health_check_timeout,health_check_unhealthy_threshold,health_check_healthy_threshold FROM managed_forwards WHERE desired_status='running' AND apply_status IN ('running','disconnected') AND health_check_enabled=1`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, serverID, localHost, protocol, failoverCurrent string
		var failoverEnabled, healthEnabled int
		var localPort, healthInterval, healthTimeout, unhealthyThr, healthyThr int
		if err := rows.Scan(&id, &serverID, &localHost, &localPort, &protocol, &failoverCurrent, &failoverEnabled, &healthEnabled, &healthInterval, &healthTimeout, &unhealthyThr, &healthyThr); err != nil {
			continue
		}
		targetServer := serverID
		if failoverCurrent != "" {
			targetServer = failoverCurrent
		}
		healthy := s.probeTargetHealth(targetServer, localHost, localPort, healthTimeout)
		s.updateForwardHealth(db, id, targetServer, healthy)
		if !healthy && failoverEnabled != 0 {
			s.executeFailover(ctx, db, id)
		}
		if healthy && failoverCurrent != "" && failoverEnabled != 0 {
			_ = s.executeFallback(ctx, db, id)
		}
	}
}

// probeTargetHealth 由目标服务器 Agent 拨号探测 local_host:local_port（不从面板本机拨号）。
func (s *Service) probeTargetHealth(serverID, host string, port, timeout int) bool {
	_, ok := s.registry.Get(serverID)
	if !ok {
		return false
	}
	return s.RunForwardHealthProbeAndWait(serverID, host, port, timeout)
}

func (s *Service) updateForwardHealth(db *sql.DB, forwardID, serverID string, healthy bool) {
	if healthy {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_forward_targets SET health_status='healthy',last_checked_at=datetime('now'),last_error='' WHERE forward_id=? AND server_id=?`, forwardID, serverID)
	} else {
		_, _ = db.ExecContext(context.Background(), `UPDATE managed_forward_targets SET health_status='unhealthy',last_checked_at=datetime('now'),last_error='TCP connection failed' WHERE forward_id=? AND server_id=?`, forwardID, serverID)
	}
}

func (s *Service) executeFailover(ctx context.Context, db *sql.DB, forwardID string) {
	if _, busy := s.taskRegistry.ActiveTask(forwardTaskResource(forwardID)); busy {
		return
	}
	var primaryID, transport string
	if err := db.QueryRowContext(ctx, `SELECT server_id,transport FROM managed_forwards WHERE id=?`, forwardID).Scan(&primaryID, &transport); err != nil {
		return
	}
	rows, err := db.QueryContext(ctx, `SELECT t.server_id FROM managed_forward_targets t WHERE t.forward_id=? AND t.health_status='healthy' ORDER BY t.priority ASC LIMIT 1`, forwardID)
	if err != nil {
		return
	}
	defer rows.Close()
	var backupID string
	if !rows.Next() {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET failover_reason='无可用备用主机',updated_at=datetime('now') WHERE id=?`, forwardID)
		return
	}
	if err := rows.Scan(&backupID); err != nil {
		return
	}
	rows.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET failover_current_server_id=?,failover_switched_at=?,failover_reason=?,updated_at=datetime('now') WHERE id=?`, backupID, now, fmt.Sprintf("主节点 %s 不可用，切换到 %s", primaryID, backupID), forwardID)
	applog.Info(ctx, "serveragent", "forward failover executed", "forward_id", forwardID, "from", primaryID, "to", backupID)
	switch transport {
	case "tcp_relay":
		// 真正转移流量：备份 agent 建立隧道，成功后摘除源主机隧道（重叠切换避免中断）。
		go func() {
			s.applyTCPRelayTarget(db, forwardID, backupID)
			if backupID != primaryID {
				s.removeTunnelFrom(db, forwardID, primaryID)
			}
		}()
	case "cloudflare_tunnel":
		// CF 隧道按主机维度承载，备用主机需自建隧道后才能承接；此处仅记录状态，
		// 实际切换需备用机具备同等 CF 隧道（Phase 规划）。
		_ = s.syncForwardIngress(ctx, db, primaryID)
	}
}

func (s *Service) executeFallback(ctx context.Context, db *sql.DB, forwardID string) error {
	var desiredStatus, failoverCurrent, primaryID, localHost, transport string
	var localPort int
	if err := db.QueryRowContext(ctx, `SELECT desired_status,COALESCE(failover_current_server_id,''),server_id,local_host,local_port,transport FROM managed_forwards WHERE id=?`, forwardID).Scan(&desiredStatus, &failoverCurrent, &primaryID, &localHost, &localPort, &transport); err != nil || failoverCurrent == "" {
		return nil
	}
	// 源主机（主节点）不落 targets 表、没有健康记录可查，恢复探测直接对源主机拨号验证
	if !s.probeTargetHealth(primaryID, localHost, localPort, 5) {
		return nil
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET failover_current_server_id='',failover_switched_at='',failover_reason='',updated_at=datetime('now') WHERE id=?`, forwardID)
	applog.Info(ctx, "serveragent", "forward fallback executed", "forward_id", forwardID, "to", failoverCurrent)
	if transport == "tcp_relay" {
		go func() {
			s.applyTCPRelayTarget(db, forwardID, primaryID)
			if primaryID != failoverCurrent {
				s.removeTunnelFrom(db, forwardID, failoverCurrent)
			}
		}()
	}
	return nil
}

// applyTCPRelayTarget 让指定服务器 Agent 建立到中继入口的隧道（切流/回流共用）。
func (s *Service) applyTCPRelayTarget(db *sql.DB, forwardID, targetServerID string) {
	var relayHost, relayServerID, localHost string
	var remotePort, localPort int
	err := db.QueryRow(`SELECT f.relay_server_id,COALESCE(a.host,''),f.local_host,f.local_port,f.remote_port FROM managed_forwards f LEFT JOIN server_accounts a ON a.id=f.relay_server_id WHERE f.id=?`, forwardID).Scan(&relayServerID, &relayHost, &localHost, &localPort, &remotePort)
	if err != nil || relayHost == "" || remotePort == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"operation": "install", "forward_id": forwardID,
		"relay_host": relayHost, "relay_port": remotePort,
		"local_host": localHost, "local_port": localPort,
	})
	if _, ok := s.registry.Get(targetServerID); !ok {
		return
	}
	_, _ = s.RunTCPForwarderTaskAndWait(targetServerID, string(payload))
}

// removeTunnelFrom 让指定服务器 Agent 拆除隧道。
func (s *Service) removeTunnelFrom(db *sql.DB, forwardID, serverID string) {
	if serverID == "" {
		return
	}
	if _, ok := s.registry.Get(serverID); !ok {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{"operation": "remove", "forward_id": forwardID})
	_, _ = s.RunTCPForwarderTaskAndWait(serverID, string(payload))
}

func (s *Service) handleAvailablePorts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	serverID := r.URL.Query().Get("server_id")
	if serverID == "" {
		response.Error(w, 400, "server_id is required")
		return
	}
	rows, err := db.QueryContext(r.Context(), `SELECT COALESCE(remote_port,0) FROM managed_forwards WHERE relay_server_id=?`, serverID)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	used := make(map[int]bool)
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err == nil && port > 0 {
			used[port] = true
		}
	}
	available := []int{}
	for port := 55655; port <= 60655; port++ {
		if !used[port] {
			available = append(available, port)
		}
	}
	response.OK(w, map[string]interface{}{
		"available": available,
		"used":      len(used),
		"total":     len(available),
		"range":     []int{55655, 60655},
	})
}

func (s *Service) handleForwardStatus(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	// 检查源主机在线状态
	_, sourceOnline := s.registry.Get(item.ServerID)
	live := map[string]interface{}{
		"source_online": sourceOnline,
	}
	if item.RelayServerID != "" {
		_, relayOnline := s.registry.Get(item.RelayServerID)
		live["relay_online"] = relayOnline
	}
	response.OK(w, map[string]interface{}{
		"id":              item.ID,
		"apply_status":    item.ApplyStatus,
		"connector_count": item.ConnectorCount,
		"last_stage":      item.LastStage,
		"last_error":      item.LastError,
		"updated_at":      item.UpdatedAt,
		"live":            live,
	})
}
