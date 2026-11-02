package serveragent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

type managedForward struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	ServerID                string `json:"server_id"`
	ServerName              string `json:"server_name,omitempty"`
	LocalHost               string `json:"local_host"`
	LocalPort               int    `json:"local_port"`
	Protocol                string `json:"protocol"`
	Transport               string `json:"transport"`
	TunnelHostname          string `json:"tunnel_hostname,omitempty"`
	TunnelPath              string `json:"tunnel_path,omitempty"`
	TunnelID                string `json:"tunnel_id,omitempty"`
	TunnelAccountID         string `json:"tunnel_account_id,omitempty"`
	TunnelZoneID            string `json:"tunnel_zone_id,omitempty"`
	TunnelZoneName          string `json:"tunnel_zone_name,omitempty"`
	DNSRecordID             string `json:"dns_record_id,omitempty"`
	TunnelApplyStatus       string `json:"tunnel_apply_status,omitempty"`
	TunnelLastStage         string `json:"tunnel_last_stage,omitempty"`
	TunnelLastError         string `json:"tunnel_last_error,omitempty"`
	WholeHost               bool   `json:"whole_host"`
	RelayServerID           string `json:"relay_server_id,omitempty"`
	RelayServerName         string `json:"relay_server_name,omitempty"`
	RelayServerHost          string `json:"relay_server_host,omitempty"`
	RemotePort               int    `json:"remote_port,omitempty"`
	AuthProxyPort            int    `json:"auth_proxy_port,omitempty"`
	UDP                      bool   `json:"udp"`
	P2PPeerServerID          string `json:"p2p_peer_server_id,omitempty"`
	AccessMode               string `json:"access_mode"`
	AccessURL               string `json:"access_url"`
	HasToken                bool   `json:"has_token"`
	GroupID                 string `json:"group_id"`
	HealthCheckEnabled      bool   `json:"health_check_enabled"`
	HealthCheckInterval     int    `json:"health_check_interval"`
	HealthCheckTimeout      int    `json:"health_check_timeout"`
	HealthCheckUnhealthyThr int    `json:"health_check_unhealthy_threshold"`
	HealthCheckHealthyThr   int    `json:"health_check_healthy_threshold"`
	FailoverEnabled         bool   `json:"failover_enabled"`
	FailoverCurrentServerID string `json:"failover_current_server_id,omitempty"`
	FailoverSwitchedAt      string `json:"failover_switched_at,omitempty"`
	FailoverReason          string `json:"failover_reason,omitempty"`
	DesiredStatus           string `json:"desired_status"`
	ApplyStatus             string `json:"apply_status"`
	LastStage               string `json:"last_stage"`
	LastError               string `json:"last_error"`
	ConnectorCount          int    `json:"connector_count"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

type managedForwardTarget struct {
	ID           string `json:"id"`
	ForwardID    string `json:"forward_id"`
	ServerID     string `json:"server_id"`
	ServerName   string `json:"server_name,omitempty"`
	Priority     int    `json:"priority"`
	Role         string `json:"role"`
	HealthStatus string `json:"health_status"`
	LastChecked  string `json:"last_checked_at"`
	LastError    string `json:"last_error"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (s *Service) handleManagedForwardRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 && r.Method == http.MethodGet {
		s.listManagedForwards(w, r, db)
		return
	}
	if len(subparts) == 0 && r.Method == http.MethodPost {
		s.createManagedForward(w, r, db)
		return
	}
	if len(subparts) == 1 && subparts[0] == "available-ports" && r.Method == http.MethodGet {
		s.handleAvailablePorts(w, r, db)
		return
	}
	if len(subparts) == 1 && r.Method == http.MethodGet {
		s.getManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 1 && r.Method == http.MethodPut {
		s.updateManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 1 && r.Method == http.MethodDelete {
		s.deleteManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 2 && subparts[1] == "deploy" && r.Method == http.MethodPost {
		s.deployManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 2 && subparts[1] == "stop" && r.Method == http.MethodPost {
		s.stopManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 2 && subparts[1] == "start" && r.Method == http.MethodPost {
		s.startManagedForward(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 1 && subparts[0] == "preflight" && r.Method == http.MethodPost {
		s.preflightManagedForward(w, r, db)
		return
	}
	if len(subparts) == 2 && subparts[1] == "targets" && r.Method == http.MethodGet {
		s.listForwardTargets(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 3 && subparts[1] == "targets" && r.Method == http.MethodPost {
		s.addForwardTarget(w, r, db, subparts[0])
		return
	}
	if len(subparts) == 3 && subparts[1] == "targets" && r.Method == http.MethodDelete {
		s.removeForwardTarget(w, r, db, subparts[0], subparts[2])
		return
	}
	if len(subparts) == 2 && subparts[1] == "status" && r.Method == http.MethodGet {
		s.handleForwardStatus(w, r, db, subparts[0])
		return
	}
	// 面板认证代理：/{id}/panel/proxy/{rest...}，会话认证后反代到转发并注入 token
	if len(subparts) >= 3 && subparts[1] == "panel" && subparts[2] == "proxy" {
		s.handleForwardPanelProxy(w, r, db, subparts[0], subparts[3:])
		return
	}
	response.Error(w, http.StatusNotFound, "forward route not found")
}

func generateForwardID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fwd_%x", time.Now().UnixNano())
	}
	return "fwd_" + hex.EncodeToString(bytes)
}

func generateTargetID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("tgt_%x", time.Now().UnixNano())
	}
	return "tgt_" + hex.EncodeToString(bytes)
}

func forwardTaskResource(forwardID string) string { return "forward:" + forwardID }

// needsTokenAuth 该转发是否需在传输层强制 token 校验（token 模式与 panel 模式共用同一套 token 数据面机制）。
func needsTokenAuth(f *managedForward) bool {
	return f.AccessMode == "token" || f.AccessMode == "panel"
}

func buildAccessURL(fwd managedForward) string {
	switch fwd.Transport {
	case "cloudflare_tunnel":
		if fwd.Protocol == "http" || fwd.Protocol == "https" {
			scheme := "https"
			if fwd.Protocol == "http" {
				scheme = "http"
			}
			return fmt.Sprintf("%s://%s%s", scheme, fwd.TunnelHostname, fwd.TunnelPath)
		}
		return fmt.Sprintf("tcp://%s:443", fwd.TunnelHostname)
	case "tcp_relay":
		if fwd.RemotePort > 0 {
			host := fwd.RelayServerHost
			if host == "" {
				host = fwd.RelayServerID
			}
			scheme := "tcp"
			switch fwd.Protocol {
			case "http":
				scheme = "http"
			case "https":
				scheme = "https"
			}
			if fwd.UDP {
				scheme = "udp"
			}
			return fmt.Sprintf("%s://%s:%d", scheme, host, fwd.RemotePort)
		}
		return ""
	case "p2p":
		return ""
	}
	return ""
}

func (s *Service) listManagedForwards(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := []string{"1=1"}
	args := []interface{}{}
	if sid := r.URL.Query().Get("server_id"); sid != "" {
		where = append(where, "f.server_id=?")
		args = append(args, sid)
	}
	if t := r.URL.Query().Get("transport"); t != "" {
		where = append(where, "f.transport=?")
		args = append(args, t)
	}
	if st := r.URL.Query().Get("apply_status"); st != "" {
		where = append(where, "f.apply_status=?")
		args = append(args, st)
	}
	if search := r.URL.Query().Get("search"); search != "" {
		where = append(where, "f.name LIKE ?")
		args = append(args, "%"+search+"%")
	}
	whereClause := strings.Join(where, " AND ")
	var total int
	if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM managed_forwards f WHERE `+whereClause, args...).Scan(&total); err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	query := `SELECT f.id,f.name,f.server_id,COALESCE(a.name,''),f.local_host,f.local_port,f.protocol,f.transport,f.tunnel_hostname,f.tunnel_path,f.tunnel_id,f.tunnel_account_id,f.tunnel_zone_id,f.tunnel_zone_name,f.dns_record_id,f.tunnel_apply_status,f.tunnel_last_stage,f.tunnel_last_error,f.whole_host,f.udp,f.relay_server_id,COALESCE(ra.name,''),COALESCE(ra.host,''),f.remote_port,f.auth_proxy_port,f.access_mode,f.access_token,f.group_id,f.health_check_enabled,f.health_check_interval,f.health_check_timeout,f.health_check_unhealthy_threshold,f.health_check_healthy_threshold,f.failover_enabled,f.failover_current_server_id,f.failover_switched_at,f.failover_reason,f.desired_status,f.apply_status,f.last_stage,f.last_error,f.connector_count,f.created_at,f.updated_at FROM managed_forwards f LEFT JOIN server_accounts a ON a.id=f.server_id LEFT JOIN server_accounts ra ON ra.id=f.relay_server_id WHERE ` + whereClause + ` ORDER BY f.updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []managedForward{}
	for rows.Next() {
		var item managedForward
	var healthEnabled, failoverEnabled, wholeHost, udpFlag int
		var accessToken string
		if err := rows.Scan(&item.ID, &item.Name, &item.ServerID, &item.ServerName, &item.LocalHost, &item.LocalPort, &item.Protocol, &item.Transport, &item.TunnelHostname, &item.TunnelPath, &item.TunnelID, &item.TunnelAccountID, &item.TunnelZoneID, &item.TunnelZoneName, &item.DNSRecordID, &item.TunnelApplyStatus, &item.TunnelLastStage, &item.TunnelLastError, &wholeHost, &udpFlag, &item.RelayServerID, &item.RelayServerName, &item.RelayServerHost, &item.RemotePort, &item.AuthProxyPort, &item.AccessMode, &accessToken, &item.GroupID, &healthEnabled, &item.HealthCheckInterval, &item.HealthCheckTimeout, &item.HealthCheckUnhealthyThr, &item.HealthCheckHealthyThr, &failoverEnabled, &item.FailoverCurrentServerID, &item.FailoverSwitchedAt, &item.FailoverReason, &item.DesiredStatus, &item.ApplyStatus, &item.LastStage, &item.LastError, &item.ConnectorCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		item.HealthCheckEnabled = healthEnabled != 0
		item.FailoverEnabled = failoverEnabled != 0
		item.WholeHost = wholeHost != 0
		item.UDP = udpFlag != 0
		item.HasToken = accessToken != ""
		item.AccessURL = buildAccessURL(item)
		items = append(items, item)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "data": items,
		"total": total, "offset": offset, "limit": limit,
	})
}

func (s *Service) createManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var input struct {
		Name          string `json:"name"`
		ServerID      string `json:"server_id"`
		LocalHost     string `json:"local_host"`
		LocalPort     int    `json:"local_port"`
		Protocol      string `json:"protocol"`
		Transport     string `json:"transport"`
		RelayServerID string `json:"relay_server_id"`
		AccessMode    string `json:"access_mode"`
		GroupID       string `json:"group_id"`
		WholeHost     bool   `json:"whole_host"`
		UDP           bool   `json:"udp"`
		P2PPeerServerID string `json:"p2p_peer_server_id"`
		TunnelHostname string `json:"tunnel_hostname"`
		TunnelAccountID string `json:"tunnel_account_id"`
		TunnelZoneID    string `json:"tunnel_zone_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.ServerID = strings.TrimSpace(input.ServerID)
	if input.Name == "" || len(input.Name) > 64 {
		response.Error(w, 400, "name must be 1-64 characters")
		return
	}
	if input.ServerID == "" {
		response.Error(w, 400, "server_id is required")
		return
	}
	if input.LocalPort < 1 || input.LocalPort > 65535 {
		response.Error(w, 400, "local_port must be 1-65535")
		return
	}
	if input.Protocol == "" {
		input.Protocol = "tcp"
	}
	if input.Protocol != "tcp" && input.Protocol != "http" && input.Protocol != "https" {
		response.Error(w, 400, "protocol must be tcp, http, or https")
		return
	}
	if input.UDP {
		if input.Transport != "tcp_relay" && input.Transport != "p2p" {
			response.Error(w, 400, "UDP 转发当前仅支持 tcp_relay / p2p 传输方式")
			return
		}
		input.Protocol = "tcp"
	}
	if input.Transport == "" {
		response.Error(w, 400, "transport is required")
		return
	}
	if input.Transport != "cloudflare_tunnel" && input.Transport != "tcp_relay" && input.Transport != "p2p" {
		response.Error(w, 400, "transport must be cloudflare_tunnel, tcp_relay, or p2p")
		return
	}
	if input.LocalHost == "" {
		input.LocalHost = "127.0.0.1"
	}
	if input.AccessMode == "" {
		input.AccessMode = "public"
	}
	if input.AccessMode != "public" && input.AccessMode != "token" && input.AccessMode != "panel" {
		response.Error(w, 400, "access_mode must be public, token, or panel")
		return
	}
	if (input.Transport == "tcp_relay" || input.Transport == "p2p") && input.RelayServerID == "" {
		response.Error(w, 400, "relay_server_id is required for tcp_relay / p2p transport（p2p 用中继做打洞失败保底）")
		return
	}
	input.TunnelHostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.TunnelHostname), "."))
	if input.Transport == "cloudflare_tunnel" && input.WholeHost && input.TunnelHostname == "" {
		response.Error(w, 400, "整域 CF 转发需要自定义 Tunnel 域名（tunnel_hostname）")
		return
	}
	if input.TunnelHostname != "" && !validTunnelHostname(input.TunnelHostname) {
		response.Error(w, 400, "tunnel_hostname 不是合法的域名")
		return
	}
	if input.Transport == "cloudflare_tunnel" && input.WholeHost && input.TunnelHostname != "" && (input.TunnelAccountID == "" || input.TunnelZoneID == "") {
		response.Error(w, 400, "整域 CF 转发需要提供 Cloudflare 账号与 Zone（tunnel_account_id / tunnel_zone_id）")
		return
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM server_accounts WHERE id=?`, input.ServerID).Scan(&exists); err != nil {
		response.Error(w, 404, "server not found")
		return
	}
	if input.RelayServerID != "" {
		if err := db.QueryRowContext(r.Context(), `SELECT 1 FROM server_accounts WHERE id=?`, input.RelayServerID).Scan(&exists); err != nil {
			response.Error(w, 404, "relay server not found")
			return
		}
	}
	id := generateForwardID()
	// token/panel 模式：生成 32 字符访问令牌并加密存储，明文仅在 token 模式创建响应中返回一次
	encryptedToken := ""
	plainToken := ""
	if input.AccessMode == "token" || input.AccessMode == "panel" {
		plainToken = generateAccessToken()
		cipher, err := secure.SecureEncrypt(plainToken)
		if err != nil {
			response.Error(w, 500, "failed to encrypt access token: "+err.Error())
			return
		}
		encryptedToken = cipher
	}
	if input.AccessMode != "token" {
		plainToken = ""
	}
	_, err := db.ExecContext(r.Context(), `INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,tunnel_hostname,tunnel_account_id,tunnel_zone_id,relay_server_id,access_mode,access_token,group_id,whole_host,udp,p2p_peer_server_id,desired_status,apply_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'running','pending')`, id, input.Name, input.ServerID, input.LocalHost, input.LocalPort, input.Protocol, input.Transport, input.TunnelHostname, input.TunnelAccountID, input.TunnelZoneID, input.RelayServerID, input.AccessMode, encryptedToken, input.GroupID, boolToInt(input.WholeHost), boolToInt(input.UDP), input.P2PPeerServerID)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 500, "failed to load created forward")
		return
	}
	payload := map[string]interface{}{"success": true, "data": item}
	if plainToken != "" {
		payload["access_token"] = plainToken
	}
	response.JSON(w, http.StatusCreated, payload)
}

// generateAccessToken 生成 32 字符随机访问令牌（16 字节 hex）
func generateAccessToken() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func (s *Service) loadForward(ctx context.Context, db *sql.DB, id string) *managedForward {
	var item managedForward
	var healthEnabled, failoverEnabled, wholeHost, udpFlag int
	var accessToken string
	err := db.QueryRowContext(ctx, `SELECT f.id,f.name,f.server_id,COALESCE(a.name,''),f.local_host,f.local_port,f.protocol,f.transport,f.tunnel_hostname,f.tunnel_path,f.tunnel_id,f.tunnel_account_id,f.tunnel_zone_id,f.tunnel_zone_name,f.dns_record_id,f.tunnel_apply_status,f.tunnel_last_stage,f.tunnel_last_error,f.whole_host,f.udp,f.relay_server_id,COALESCE(ra.name,''),COALESCE(ra.host,''),f.remote_port,f.auth_proxy_port,f.access_mode,f.access_token,f.group_id,f.health_check_enabled,f.health_check_interval,f.health_check_timeout,f.health_check_unhealthy_threshold,f.health_check_healthy_threshold,f.failover_enabled,f.failover_current_server_id,f.failover_switched_at,f.failover_reason,f.p2p_peer_server_id,f.desired_status,f.apply_status,f.last_stage,f.last_error,f.connector_count,f.created_at,f.updated_at FROM managed_forwards f LEFT JOIN server_accounts a ON a.id=f.server_id LEFT JOIN server_accounts ra ON ra.id=f.relay_server_id WHERE f.id=?`, id).Scan(&item.ID, &item.Name, &item.ServerID, &item.ServerName, &item.LocalHost, &item.LocalPort, &item.Protocol, &item.Transport, &item.TunnelHostname, &item.TunnelPath, &item.TunnelID, &item.TunnelAccountID, &item.TunnelZoneID, &item.TunnelZoneName, &item.DNSRecordID, &item.TunnelApplyStatus, &item.TunnelLastStage, &item.TunnelLastError, &wholeHost, &udpFlag, &item.RelayServerID, &item.RelayServerName, &item.RelayServerHost, &item.RemotePort, &item.AuthProxyPort, &item.AccessMode, &accessToken, &item.GroupID, &healthEnabled, &item.HealthCheckInterval, &item.HealthCheckTimeout, &item.HealthCheckUnhealthyThr, &item.HealthCheckHealthyThr, &failoverEnabled, &item.FailoverCurrentServerID, &item.FailoverSwitchedAt, &item.FailoverReason, &item.P2PPeerServerID, &item.DesiredStatus, &item.ApplyStatus, &item.LastStage, &item.LastError, &item.ConnectorCount, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil
	}
	item.HealthCheckEnabled = healthEnabled != 0
	item.FailoverEnabled = failoverEnabled != 0
	item.WholeHost = wholeHost != 0
	item.UDP = udpFlag != 0
	item.HasToken = accessToken != ""
	item.AccessURL = buildAccessURL(item)
	return &item
}

func (s *Service) getManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	response.OK(w, item)
}

func (s *Service) updateManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	existing := s.loadForward(r.Context(), db, id)
	if existing == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	var input struct {
		Name               *string `json:"name"`
		LocalHost          *string `json:"local_host"`
		LocalPort          *int    `json:"local_port"`
		Protocol           *string `json:"protocol"`
		RelayServerID      *string `json:"relay_server_id"`
		AccessMode         *string `json:"access_mode"`
		GroupID            *string `json:"group_id"`
		WholeHost          *bool   `json:"whole_host"`
		UDP                *bool   `json:"udp"`
		TunnelHostname     *string `json:"tunnel_hostname"`
		TunnelAccountID    *string `json:"tunnel_account_id"`
		TunnelZoneID       *string `json:"tunnel_zone_id"`
		HealthCheckEnabled *bool   `json:"health_check_enabled"`
		FailoverEnabled    *bool   `json:"failover_enabled"`
		P2PPeerServerID    *string `json:"p2p_peer_server_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil {
		response.Error(w, 400, "invalid request body")
		return
	}
	name := existing.Name
	localHost := existing.LocalHost
	localPort := existing.LocalPort
	protocol := existing.Protocol
	relayServerID := existing.RelayServerID
	accessMode := existing.AccessMode
	groupID := existing.GroupID
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if name == "" || len(name) > 64 {
			response.Error(w, 400, "name must be 1-64 characters")
			return
		}
	}
	if input.LocalHost != nil {
		localHost = *input.LocalHost
	}
	if input.LocalPort != nil {
		localPort = *input.LocalPort
		if localPort < 1 || localPort > 65535 {
			response.Error(w, 400, "local_port must be 1-65535")
			return
		}
	}
	if input.Protocol != nil {
		protocol = *input.Protocol
		if protocol != "tcp" && protocol != "http" && protocol != "https" {
			response.Error(w, 400, "protocol must be tcp, http, or https")
			return
		}
	}
	if input.RelayServerID != nil {
		relayServerID = *input.RelayServerID
	}
	if input.AccessMode != nil {
		accessMode = *input.AccessMode
		if accessMode != "public" && accessMode != "token" && accessMode != "panel" {
			response.Error(w, 400, "access_mode must be public, token, or panel")
			return
		}
	}
	if input.GroupID != nil {
		groupID = *input.GroupID
	}
	healthCheckEnabled := existing.HealthCheckEnabled
	failoverEnabled := existing.FailoverEnabled
	wholeHost := existing.WholeHost
	udp := existing.UDP
	if input.HealthCheckEnabled != nil {
		healthCheckEnabled = *input.HealthCheckEnabled
	}
	if input.FailoverEnabled != nil {
		failoverEnabled = *input.FailoverEnabled
	}
	if input.WholeHost != nil {
		wholeHost = *input.WholeHost
	}
	if input.UDP != nil {
		udp = *input.UDP
		if udp && protocol != "tcp" {
			response.Error(w, 400, "UDP 转发仅支持 tcp_relay，protocol 保持 tcp")
			return
		}
	}
	tunnelHostname := existing.TunnelHostname
	if input.TunnelHostname != nil {
		tunnelHostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*input.TunnelHostname), "."))
		if existing.Transport == "cloudflare_tunnel" && wholeHost && tunnelHostname == "" {
			response.Error(w, 400, "整域 CF 转发需要自定义 Tunnel 域名（tunnel_hostname）")
			return
		}
		if tunnelHostname != "" && !validTunnelHostname(tunnelHostname) {
			response.Error(w, 400, "tunnel_hostname 不是合法的域名")
			return
		}
	}
	tunnelAccountID := existing.TunnelAccountID
	if input.TunnelAccountID != nil {
		tunnelAccountID = strings.TrimSpace(*input.TunnelAccountID)
	}
	p2pPeerServerID := existing.P2PPeerServerID
	if input.P2PPeerServerID != nil {
		p2pPeerServerID = strings.TrimSpace(*input.P2PPeerServerID)
	}
	tunnelZoneID := existing.TunnelZoneID
	if input.TunnelZoneID != nil {
		tunnelZoneID = strings.TrimSpace(*input.TunnelZoneID)
	}
	var healthFlag, failoverFlag int
	if healthCheckEnabled {
		healthFlag = 1
	}
	if failoverEnabled {
		failoverFlag = 1
	}
	_, err := db.ExecContext(r.Context(), `UPDATE managed_forwards SET name=?,local_host=?,local_port=?,protocol=?,tunnel_hostname=?,tunnel_account_id=?,tunnel_zone_id=?,relay_server_id=?,access_mode=?,group_id=?,whole_host=?,udp=?,health_check_enabled=?,failover_enabled=?,p2p_peer_server_id=?,updated_at=datetime('now') WHERE id=?`, name, localHost, localPort, protocol, tunnelHostname, tunnelAccountID, tunnelZoneID, relayServerID, accessMode, groupID, boolToInt(wholeHost), boolToInt(udp), healthFlag, failoverFlag, p2pPeerServerID, id)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 500, "failed to load updated forward")
		return
	}
	response.OK(w, item)
}

func (s *Service) deleteManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	if item.ConnectorCount > 0 && r.URL.Query().Get("cascade") != "1" {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("该转发规则仍有 %d 个活跃连接，确认删除请使用 cascade=1", item.ConnectorCount),
			"data":    map[string]interface{}{"connector_count": item.ConnectorCount},
		})
		return
	}
	_, _ = db.ExecContext(r.Context(), `DELETE FROM managed_forward_targets WHERE forward_id=?`, id)
	// 整域独立隧道：先级联卸载（停实例 + 删 Named Tunnel + 删 DNS）再删行；
	// 云端清理失败则保留规则行（cleanup_failed）供重试，避免孤儿隧道无法追踪。
	if item.Transport == "cloudflare_tunnel" && item.WholeHost && item.TunnelID != "" {
		if err := s.removeForwardTunnel(context.Background(), db, item, false); err != nil {
			response.Error(w, http.StatusInternalServerError, "级联清理未完成，已保留规则可重试："+err.Error())
			return
		}
	}
	_, err := db.ExecContext(r.Context(), `DELETE FROM managed_forwards WHERE id=?`, id)
	if err != nil {
		response.Error(w, 500, err.Error())
		return
	}
	// 先删行再同步 ingress：syncForwardIngress 从库里重建，行还在会把本规则的路径重新加回
	if item.Transport == "cloudflare_tunnel" && (item.TunnelPath != "" || item.WholeHost) && item.ApplyStatus == "running" {
		s.removeForwardIngress(context.Background(), db, item.ServerID, item.TunnelPath)
	}
	// CF 隧道 token：回收源主机鉴权代理进程
	s.removeCFAuthProxy(context.Background(), db, item)
	// tcp_relay 给源/中继 agent 发卸载指令，避免「已删除」的隧道仍在转发、端口被占用
	s.removeTCPRelayTunnels(context.Background(), db, item)
	response.OK(w, map[string]string{"message": "转发规则已删除"})
}

