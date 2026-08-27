package serveragent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/cloudflare"
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
	WholeHost               bool   `json:"whole_host"`
	RelayServerID           string `json:"relay_server_id,omitempty"`
	RelayServerName         string `json:"relay_server_name,omitempty"`
	RelayServerHost          string `json:"relay_server_host,omitempty"`
	RemotePort               int    `json:"remote_port,omitempty"`
	AuthProxyPort            int    `json:"auth_proxy_port,omitempty"`
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

// api-monitor-relay 中继二进制资产（与 GitHub Release v0.6.1 一致）
const (
	relayLinuxAMD64URL      = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-relay-linux-amd64"
	relayLinuxAMD64SHA256   = "455051b47cb1d4da2ece0b29c2b469191e68e712fabc43f21651ce0fdf52640f"
	relayLinuxARM64URL      = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-relay-linux-arm64"
	relayLinuxARM64SHA256   = "e163a95eb5b24945a834a8ca2ffc1d2112d1449f4ff839425d9630ab6f90aa85"
	relayWindowsAMD64URL    = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-relay-windows-amd64.exe"
	relayWindowsAMD64SHA256 = "226fffc6450591b5b5bbe2c3f2c2d59dcc1594824cf593e338c0b79284f67948"
)

// relayAssetFor 按主机平台/架构返回 relay 二进制下载地址与 SHA-256。
func relayAssetFor(platform, arch string) (url, sha string, ok bool) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	arch = strings.ToLower(strings.TrimSpace(arch))
	if strings.Contains(platform, "windows") || strings.Contains(platform, "win") {
		return relayWindowsAMD64URL, relayWindowsAMD64SHA256, true
	}
	if strings.Contains(arch, "arm64") || strings.Contains(arch, "aarch64") {
		return relayLinuxARM64URL, relayLinuxARM64SHA256, true
	}
	return relayLinuxAMD64URL, relayLinuxAMD64SHA256, true
}

// api-monitor-auth-proxy 鉴权代理二进制资产（与 GitHub Release v0.6.1 一致）
const (
	authProxyLinuxAMD64URL      = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-auth-proxy-linux-amd64"
	authProxyLinuxAMD64SHA256   = "c32c9b9e738043c8b212f54785f8cafeafdb4c65b4fabaf99a514e8b20d9553a"
	authProxyLinuxARM64URL      = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-auth-proxy-linux-arm64"
	authProxyLinuxARM64SHA256   = "2d6ac72ebd9815a9019b4e0af29d272e57d68b5f533243c19476cdcf56ce0619"
	authProxyWindowsAMD64URL    = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-auth-proxy-windows-amd64.exe"
	authProxyWindowsAMD64SHA256 = "8343ea6d6c88cd319e1fd0f71803d8d159030aca0480ee0de4add745b278a381"
)

// authProxyAssetFor 按主机平台/架构返回 auth-proxy 二进制下载地址与 SHA-256。
func authProxyAssetFor(platform, arch string) (url, sha string, ok bool) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	arch = strings.ToLower(strings.TrimSpace(arch))
	if strings.Contains(platform, "windows") || strings.Contains(platform, "win") {
		return authProxyWindowsAMD64URL, authProxyWindowsAMD64SHA256, true
	}
	if strings.Contains(arch, "arm64") || strings.Contains(arch, "aarch64") {
		return authProxyLinuxARM64URL, authProxyLinuxARM64SHA256, true
	}
	return authProxyLinuxAMD64URL, authProxyLinuxAMD64SHA256, true
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
	query := `SELECT f.id,f.name,f.server_id,COALESCE(a.name,''),f.local_host,f.local_port,f.protocol,f.transport,f.tunnel_hostname,f.tunnel_path,f.whole_host,f.relay_server_id,COALESCE(ra.name,''),COALESCE(ra.host,''),f.remote_port,f.auth_proxy_port,f.access_mode,f.access_token,f.group_id,f.health_check_enabled,f.health_check_interval,f.health_check_timeout,f.health_check_unhealthy_threshold,f.health_check_healthy_threshold,f.failover_enabled,f.failover_current_server_id,f.failover_switched_at,f.failover_reason,f.desired_status,f.apply_status,f.last_stage,f.last_error,f.connector_count,f.created_at,f.updated_at FROM managed_forwards f LEFT JOIN server_accounts a ON a.id=f.server_id LEFT JOIN server_accounts ra ON ra.id=f.relay_server_id WHERE ` + whereClause + ` ORDER BY f.updated_at DESC LIMIT ? OFFSET ?`
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
		var healthEnabled, failoverEnabled, wholeHost int
		var accessToken string
		if err := rows.Scan(&item.ID, &item.Name, &item.ServerID, &item.ServerName, &item.LocalHost, &item.LocalPort, &item.Protocol, &item.Transport, &item.TunnelHostname, &item.TunnelPath, &wholeHost, &item.RelayServerID, &item.RelayServerName, &item.RelayServerHost, &item.RemotePort, &item.AuthProxyPort, &item.AccessMode, &accessToken, &item.GroupID, &healthEnabled, &item.HealthCheckInterval, &item.HealthCheckTimeout, &item.HealthCheckUnhealthyThr, &item.HealthCheckHealthyThr, &failoverEnabled, &item.FailoverCurrentServerID, &item.FailoverSwitchedAt, &item.FailoverReason, &item.DesiredStatus, &item.ApplyStatus, &item.LastStage, &item.LastError, &item.ConnectorCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			response.Error(w, 500, err.Error())
			return
		}
		item.HealthCheckEnabled = healthEnabled != 0
		item.FailoverEnabled = failoverEnabled != 0
		item.WholeHost = wholeHost != 0
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
	if input.Transport == "tcp_relay" && input.RelayServerID == "" {
		response.Error(w, 400, "relay_server_id is required for tcp_relay transport")
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
	_, err := db.ExecContext(r.Context(), `INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,relay_server_id,access_mode,access_token,group_id,whole_host,desired_status,apply_status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'running','pending')`, id, input.Name, input.ServerID, input.LocalHost, input.LocalPort, input.Protocol, input.Transport, input.RelayServerID, input.AccessMode, encryptedToken, input.GroupID, boolToInt(input.WholeHost))
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
	var healthEnabled, failoverEnabled, wholeHost int
	var accessToken string
	err := db.QueryRowContext(ctx, `SELECT f.id,f.name,f.server_id,COALESCE(a.name,''),f.local_host,f.local_port,f.protocol,f.transport,f.tunnel_hostname,f.tunnel_path,f.whole_host,f.relay_server_id,COALESCE(ra.name,''),COALESCE(ra.host,''),f.remote_port,f.auth_proxy_port,f.access_mode,f.access_token,f.group_id,f.health_check_enabled,f.health_check_interval,f.health_check_timeout,f.health_check_unhealthy_threshold,f.health_check_healthy_threshold,f.failover_enabled,f.failover_current_server_id,f.failover_switched_at,f.failover_reason,f.desired_status,f.apply_status,f.last_stage,f.last_error,f.connector_count,f.created_at,f.updated_at FROM managed_forwards f LEFT JOIN server_accounts a ON a.id=f.server_id LEFT JOIN server_accounts ra ON ra.id=f.relay_server_id WHERE f.id=?`, id).Scan(&item.ID, &item.Name, &item.ServerID, &item.ServerName, &item.LocalHost, &item.LocalPort, &item.Protocol, &item.Transport, &item.TunnelHostname, &item.TunnelPath, &wholeHost, &item.RelayServerID, &item.RelayServerName, &item.RelayServerHost, &item.RemotePort, &item.AuthProxyPort, &item.AccessMode, &accessToken, &item.GroupID, &healthEnabled, &item.HealthCheckInterval, &item.HealthCheckTimeout, &item.HealthCheckUnhealthyThr, &item.HealthCheckHealthyThr, &failoverEnabled, &item.FailoverCurrentServerID, &item.FailoverSwitchedAt, &item.FailoverReason, &item.DesiredStatus, &item.ApplyStatus, &item.LastStage, &item.LastError, &item.ConnectorCount, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil
	}
	item.HealthCheckEnabled = healthEnabled != 0
	item.FailoverEnabled = failoverEnabled != 0
	item.WholeHost = wholeHost != 0
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
		HealthCheckEnabled *bool   `json:"health_check_enabled"`
		FailoverEnabled    *bool   `json:"failover_enabled"`
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
	if input.HealthCheckEnabled != nil {
		healthCheckEnabled = *input.HealthCheckEnabled
	}
	if input.FailoverEnabled != nil {
		failoverEnabled = *input.FailoverEnabled
	}
	if input.WholeHost != nil {
		wholeHost = *input.WholeHost
	}
	var healthFlag, failoverFlag int
	if healthCheckEnabled {
		healthFlag = 1
	}
	if failoverEnabled {
		failoverFlag = 1
	}
	_, err := db.ExecContext(r.Context(), `UPDATE managed_forwards SET name=?,local_host=?,local_port=?,protocol=?,relay_server_id=?,access_mode=?,group_id=?,whole_host=?,health_check_enabled=?,failover_enabled=?,updated_at=datetime('now') WHERE id=?`, name, localHost, localPort, protocol, relayServerID, accessMode, groupID, boolToInt(wholeHost), healthFlag, failoverFlag, id)
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

func (s *Service) deployManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	item := s.loadForward(r.Context(), db, id)
	if item == nil {
		response.Error(w, 404, "forward rule not found")
		return
	}
	// token/panel 访问控制：token 由传输层强制校验；panel 复用同一 token 数据面机制，
	// 另加面板会话反向代理入口（/api/server/forward/{id}/panel/proxy/*）注入 token。
	// 仅 http/https 支持（tcp 走 tcp_relay+token）。
	switch {
	case item.AccessMode == "public":
		// 公开访问，无需校验
	case item.Transport == "tcp_relay" && needsTokenAuth(item):
		// 已落地：relay 入口强制 token 握手校验
	case item.Transport == "cloudflare_tunnel" && needsTokenAuth(item):
		if item.Protocol != "http" && item.Protocol != "https" {
			response.Error(w, 422, "CF 隧道 + token/panel 仅支持 http/https 协议（tcp 请改用 tcp_relay + token）")
			return
		}
	default:
		response.Error(w, 422, "access_mode=token/panel 仅 tcp_relay 或 CF 隧道(http/https) 已落地；其余组合部署前请改为 public")
		return
	}
	switch item.Transport {
	case "cloudflare_tunnel":
		s.deployCloudflareTunnelForward(w, r, db, item)
	case "tcp_relay":
		s.deployTCPRelayForward(w, r, db, item)
	case "p2p":
		response.Error(w, 422, "P2P 直连部署在 Phase 3 实现")
	default:
		response.Error(w, 400, "unsupported transport")
	}
}

func (s *Service) deployCloudflareTunnelForward(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	if s.cloudflare == nil {
		response.Error(w, http.StatusServiceUnavailable, "Cloudflare integration is unavailable")
		return
	}
	var tunnelExists int
	var tunnelHostname, tunnelID string
	err := db.QueryRowContext(r.Context(), `SELECT 1,tunnel_id,hostname FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running'`, item.ServerID).Scan(&tunnelExists, &tunnelID, &tunnelHostname)
	if err != nil {
		var anyTunnel int
		_ = db.QueryRowContext(r.Context(), `SELECT 1 FROM managed_proxy_tunnels WHERE server_id=?`, item.ServerID).Scan(&anyTunnel)
		if anyTunnel == 0 {
			response.Error(w, 422, "该主机尚未部署 Cloudflare Tunnel，请先部署隧道")
			return
		}
		response.Error(w, 422, "该主机的 Cloudflare Tunnel 不在运行状态，请先部署隧道")
		return
	}
	// token/panel 模式：cloudflared 本身不鉴权，需在源主机启动鉴权代理，ingress 指向代理端口
	authProxyPort := item.AuthProxyPort
	if needsTokenAuth(item) {
		srcConn, ok := s.registry.Get(item.ServerID)
		if !ok {
			response.Error(w, http.StatusBadGateway, "源主机 Agent 离线，无法启动鉴权代理")
			return
		}
		meta := srcConn.GetMetadata()
		proxyURL, proxySHA, proxyOK := authProxyAssetFor(fmt.Sprint(meta["platform"]), fmt.Sprint(meta["arch"]))
		if !proxyOK {
			response.Error(w, 422, "不支持该主机的 auth-proxy 资产")
			return
		}
		var enc string
		_ = db.QueryRowContext(r.Context(), `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
		token := secure.SecureDecrypt(enc)
		// 端口由源主机 agent 自选（避开已占用端口），agent 校验进程存活后返回实际端口
		proxyPayload, _ := json.Marshal(map[string]interface{}{
			"operation": "auth_proxy_start", "forward_id": item.ID,
			"token": token,
			"local_host": item.LocalHost, "local_port": item.LocalPort,
			"relay_asset_url": proxyURL, "relay_asset_sha256": proxySHA,
		})
		out, err := s.RunTCPForwarderTaskAndWait(item.ServerID, string(proxyPayload))
		if err != nil {
			_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_auth_proxy',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
			response.Error(w, 500, "鉴权代理启动失败: "+err.Error())
			return
		}
		var proxyResp struct {
			Port int `json:"port"`
		}
		if err := json.Unmarshal([]byte(out), &proxyResp); err != nil || proxyResp.Port < 1 || proxyResp.Port > 65535 {
			_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_auth_proxy',last_error=?,updated_at=datetime('now') WHERE id=?`, "鉴权代理未返回有效端口", item.ID)
			response.Error(w, 500, "鉴权代理未返回有效端口: "+out)
			return
		}
		authProxyPort = proxyResp.Port
		_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET auth_proxy_port=? WHERE id=?`, authProxyPort, item.ID)
	}
	path := "/fwd/" + item.ID
	if item.WholeHost {
		path = ""
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET tunnel_hostname=?,tunnel_path=?,apply_status='deploying',last_stage='deploying',updated_at=datetime('now') WHERE id=?`, tunnelHostname, path, item.ID)
	if err := s.syncForwardIngress(r.Context(), db, item.ServerID); err != nil {
		_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_ingress',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		response.Error(w, 500, "deploy failed: "+err.Error())
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, item.ID)
	updated := s.loadForward(r.Context(), db, item.ID)
	response.OK(w, updated)
}

func (s *Service) deployTCPRelayForward(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	if item.RelayServerID == "" {
		response.Error(w, 422, "中继入口主机未指定")
		return
	}
	var relayHost string
	if err := db.QueryRowContext(r.Context(), `SELECT COALESCE(host,'') FROM server_accounts WHERE id=?`, item.RelayServerID).Scan(&relayHost); err != nil || relayHost == "" {
		response.Error(w, 422, "中继入口主机未配置可连接地址（server_accounts.host）")
		return
	}
	relayConn, ok := s.registry.Get(item.RelayServerID)
	if !ok {
		response.Error(w, http.StatusBadGateway, "中继入口主机 Agent 离线")
		return
	}
	if !relayConn.GetCapabilities()["tcp_forwarder_v1"] {
		response.Error(w, http.StatusConflict, "中继入口主机 Agent 版本过旧，不支持 tcp_forwarder_v1")
		return
	}
	if issue := s.sourceClientCapabilityIssue(item.ServerID); issue != "" {
		response.Error(w, http.StatusBadGateway, issue)
		return
	}

	// 0) 默认安装中继入口：任何主机都能成为中继（agent 侧幂等，已运行即跳过）
	relayMeta := relayConn.GetMetadata()
	relayURL, relaySHA, relayOK := relayAssetFor(fmt.Sprint(relayMeta["platform"]), fmt.Sprint(relayMeta["arch"]))
	if relayOK {
		if err := s.RunTCPForwarderBootstrap(item.RelayServerID, relayURL, relaySHA); err != nil {
			_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_relay_bootstrap',last_error=?,updated_at=datetime('now') WHERE id=?`, "中继安装失败: "+err.Error(), item.ID)
			response.Error(w, 500, "中继入口安装失败: "+err.Error())
			return
		}
	}

	port := allocateRelayPort(r.Context(), db, item, item.RelayServerID)
	if port == 0 {
		response.Error(w, 422, "中继端口已满（55655-60655），请清理不需要的转发规则")
		return
	}

	// 1) 入口主机：让中继器监听公开端口并放行防火墙（token 模式下发解密凭证强制校验）
	token := ""
	if needsTokenAuth(item) {
		var enc string
		_ = db.QueryRowContext(r.Context(), `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
		token = secure.SecureDecrypt(enc)
	}
	listenPayload, _ := json.Marshal(map[string]interface{}{
		"operation": "listen", "forward_id": item.ID, "relay_port": port, "token": token,
	})
	if _, err := s.RunTCPForwarderTaskAndWait(item.RelayServerID, string(listenPayload)); err != nil {
		_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_relay',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		response.Error(w, 500, "中继入口部署失败: "+err.Error())
		return
	}
	// 2) 源主机：建立反向隧道并代理本地服务
	sourcePayload, _ := json.Marshal(map[string]interface{}{
		"operation": "install", "forward_id": item.ID,
		"relay_host": relayHost, "relay_port": port,
		"local_host": item.LocalHost, "local_port": item.LocalPort,
	})
	if _, err := s.RunTCPForwarderTaskAndWait(item.ServerID, string(sourcePayload)); err != nil {
		_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_source',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		response.Error(w, 500, "源主机隧道建立失败: "+err.Error())
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE managed_forwards SET remote_port=?,apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, port, item.ID)
	updated := s.loadForward(r.Context(), db, item.ID)
	response.OK(w, updated)
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
	forwardRows, err := db.QueryContext(ctx, `SELECT id,protocol,local_host,local_port,tunnel_hostname,tunnel_path,access_mode,auth_proxy_port FROM managed_forwards WHERE server_id=? AND transport='cloudflare_tunnel' AND desired_status='running' AND apply_status IN ('running','deploying') ORDER BY whole_host ASC, created_at ASC`, serverID)
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
		if item.AccessURL == "" {
			response.Error(w, 422, "转发尚未部署")
			return
		}
		upstream = item.AccessURL
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
	if item.Transport == "cloudflare_tunnel" && (item.TunnelPath != "" || item.WholeHost) {
		s.removeForwardIngress(context.Background(), db, item.ServerID, item.TunnelPath)
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
