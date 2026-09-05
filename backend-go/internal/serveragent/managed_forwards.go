package serveragent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

// api-monitor-stun 自建 STUN 服务器二进制资产。
// 发布：将 cmd/api-monitor-stun 交叉编译产物上传到 GitHub Release，填入下方 URL 与 SHA-256。
const (
	stunLinuxAMD64URL    = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-stun-linux-amd64"
	stunLinuxAMD64SHA256 = "2fd84c0ae0d67bba21c8a46153e493a7cad1452e881e8099c3d132665861514c"
	stunLinuxARM64URL    = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-stun-linux-arm64"
	stunLinuxARM64SHA256 = "17b5c4b59434714d765aa7e2a63d013a3b61df51b5ac07b27e4cf322d7b072d7"
	stunWindowsAMD64URL  = "https://github.com/iwvw/API-Monitor/releases/download/v0.6.1/api-monitor-stun-windows-amd64.exe"
	stunWindowsAMD64SHA  = "d53e33fde9c4b6243f2d878f9deff974a04e5fc73e9b971432956467ca0047ed"
)

// stunAssetFor 按主机平台/架构返回 api-monitor-stun 二进制下载地址与 SHA-256。
// 尚未构建/上传的架构返回 ok=false，面板据此跳过自建 STUN、回退公共 STUN。
func stunAssetFor(platform, arch string) (url, sha string, ok bool) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	arch = strings.ToLower(strings.TrimSpace(arch))
	if strings.Contains(platform, "windows") || strings.Contains(platform, "win") {
		return stunWindowsAMD64URL, stunWindowsAMD64SHA, true
	}
	if strings.Contains(arch, "arm64") || strings.Contains(arch, "aarch64") {
		return stunLinuxARM64URL, stunLinuxARM64SHA256, true
	}
	return stunLinuxAMD64URL, stunLinuxAMD64SHA256, true
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
	case item.Transport == "p2p" && needsTokenAuth(item):
		// P2P 的保底数据面是中继隧道，token 校验沿 tcp_relay 路径执行
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
		if item.WholeHost {
			// 整域转发：每条规则部署独立 Named Tunnel + 独立 cloudflared 实例
			s.deployForwardTunnel(w, r, db, item)
		} else {
			s.deployCloudflareTunnelForward(w, r, db, item)
		}
	case "tcp_relay":
		s.deployTCPRelayForward(w, r, db, item)
	case "p2p":
		s.deployP2PForward(w, r, db, item)
	default:
		response.Error(w, 400, "unsupported transport")
	}
}

func (s *Service) deployCloudflareTunnelForward(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	code, err := s.deployCloudflareTunnelCore(r.Context(), db, item)
	if err != nil {
		response.Error(w, code, err.Error())
		return
	}
	response.OK(w, s.loadForward(r.Context(), db, item.ID))
}

// deployCloudflareTunnelCore 实际部署 CF 隧道转发，返回 HTTP 状态码与错误；
// 供 HTTP 入口与「agent 上线对账重放」共用。
func (s *Service) deployCloudflareTunnelCore(ctx context.Context, db *sql.DB, item *managedForward) (int, error) {
	if s.cloudflare == nil {
		return http.StatusServiceUnavailable, errors.New("Cloudflare integration is unavailable")
	}
	var tunnelExists int
	var tunnelHostname, tunnelID string
	err := db.QueryRowContext(ctx, `SELECT 1,tunnel_id,hostname FROM managed_proxy_tunnels WHERE server_id=? AND apply_status='running'`, item.ServerID).Scan(&tunnelExists, &tunnelID, &tunnelHostname)
	if err != nil {
		var anyTunnel int
		_ = db.QueryRowContext(ctx, `SELECT 1 FROM managed_proxy_tunnels WHERE server_id=?`, item.ServerID).Scan(&anyTunnel)
		if anyTunnel == 0 {
			return 422, errors.New("该主机尚未部署 Cloudflare Tunnel，请先部署隧道")
		}
		return 422, errors.New("该主机的 Cloudflare Tunnel 不在运行状态，请先部署隧道")
	}
	// token/panel 模式：cloudflared 本身不鉴权，需在源主机启动鉴权代理，ingress 指向代理端口
	authProxyPort := item.AuthProxyPort
	if needsTokenAuth(item) {
		srcConn, ok := s.registry.Get(item.ServerID)
		if !ok {
			return http.StatusBadGateway, errors.New("源主机 Agent 离线，无法启动鉴权代理")
		}
		meta := srcConn.GetMetadata()
		proxyURL, proxySHA, proxyOK := authProxyAssetFor(fmt.Sprint(meta["platform"]), fmt.Sprint(meta["arch"]))
		if !proxyOK {
			return 422, errors.New("不支持该主机的 auth-proxy 资产")
		}
		var enc string
		_ = db.QueryRowContext(ctx, `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
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
			_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_auth_proxy',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
			return 500, errors.New("鉴权代理启动失败: "+err.Error())
		}
		var proxyResp struct {
			Port int `json:"port"`
		}
		if err := json.Unmarshal([]byte(out), &proxyResp); err != nil || proxyResp.Port < 1 || proxyResp.Port > 65535 {
			_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_auth_proxy',last_error=?,updated_at=datetime('now') WHERE id=?`, "鉴权代理未返回有效端口", item.ID)
			return 500, errors.New("鉴权代理未返回有效端口: "+out)
		}
		authProxyPort = proxyResp.Port
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET auth_proxy_port=? WHERE id=?`, authProxyPort, item.ID)
	}
	path := "/fwd/" + item.ID
	if item.WholeHost {
		path = ""
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET tunnel_hostname=?,tunnel_path=?,apply_status='deploying',last_stage='deploying',updated_at=datetime('now') WHERE id=?`, tunnelHostname, path, item.ID)
	if err := s.syncForwardIngress(ctx, db, item.ServerID); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_ingress',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 500, errors.New("deploy failed: "+err.Error())
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, item.ID)
	return 0, nil
}

func (s *Service) deployTCPRelayForward(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	code, err := s.deployTCPRelayCore(r.Context(), db, item)
	if err != nil {
		response.Error(w, code, err.Error())
		return
	}
	response.OK(w, s.loadForward(r.Context(), db, item.ID))
}

// deployTCPRelayCore 实际部署 tcp_relay 转发链路，返回 HTTP 状态码与错误；
// 供 HTTP 入口与「agent 上线对账重放」共用。
func (s *Service) deployTCPRelayCore(ctx context.Context, db *sql.DB, item *managedForward) (int, error) {
	if item.RelayServerID == "" {
		return 422, errors.New("中继入口主机未指定")
	}
	var relayHost string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(host,'') FROM server_accounts WHERE id=?`, item.RelayServerID).Scan(&relayHost); err != nil || relayHost == "" {
		return 422, errors.New("中继入口主机未配置可连接地址（server_accounts.host）")
	}
	relayConn, ok := s.registry.Get(item.RelayServerID)
	if !ok {
		return http.StatusBadGateway, errors.New("中继入口主机 Agent 离线")
	}
	if !relayConn.GetCapabilities()["tcp_forwarder_v1"] {
		return http.StatusConflict, errors.New("中继入口主机 Agent 版本过旧，不支持 tcp_forwarder_v1")
	}
	if issue := s.sourceClientCapabilityIssue(item.ServerID); issue != "" {
		return http.StatusBadGateway, errors.New(issue)
	}

	// 0) 默认安装中继入口：任何主机都能成为中继（agent 侧幂等，已运行即跳过）
	relayMeta := relayConn.GetMetadata()
	relayURL, relaySHA, relayOK := relayAssetFor(fmt.Sprint(relayMeta["platform"]), fmt.Sprint(relayMeta["arch"]))
	if relayOK {
		if err := s.RunTCPForwarderBootstrap(item.RelayServerID, relayURL, relaySHA); err != nil {
			_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_relay_bootstrap',last_error=?,updated_at=datetime('now') WHERE id=?`, "中继安装失败: "+err.Error(), item.ID)
			return 500, errors.New("中继入口安装失败: "+err.Error())
		}
	}

	port := allocateRelayPort(ctx, db, item, item.RelayServerID)
	if port == 0 {
		return 422, errors.New("中继端口已满（55655-60655），请清理不需要的转发规则")
	}

	// 1) 入口主机：让中继器监听公开端口并放行防火墙（token 模式下发解密凭证强制校验）
	token := ""
	if needsTokenAuth(item) {
		var enc string
		_ = db.QueryRowContext(ctx, `SELECT access_token FROM managed_forwards WHERE id=?`, item.ID).Scan(&enc)
		token = secure.SecureDecrypt(enc)
	}
	listenPayload, _ := json.Marshal(map[string]interface{}{
		"operation": "listen", "forward_id": item.ID, "relay_port": port, "token": token, "udp": item.UDP,
	})
	if _, err := s.RunTCPForwarderTaskAndWait(item.RelayServerID, string(listenPayload)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_relay',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 500, errors.New("中继入口部署失败: "+err.Error())
	}
	// 2) 源主机：建立反向隧道并代理本地服务
	sourcePayload, _ := json.Marshal(map[string]interface{}{
		"operation": "install", "forward_id": item.ID,
		"relay_host": relayHost, "relay_port": port,
		"local_host": item.LocalHost, "local_port": item.LocalPort,
	})
	if _, err := s.RunTCPForwarderTaskAndWait(item.ServerID, string(sourcePayload)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET apply_status='failed',last_stage='deploy_source',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 500, errors.New("源主机隧道建立失败: "+err.Error())
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET remote_port=?,apply_status='running',last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, port, item.ID)
	return 0, nil
}

// deployP2PForward 部署 P2P 直连转发。原则：先建 tcp_relay 隧道保底即通，再后台做 UDP 打洞升级为直连。
func (s *Service) deployP2PForward(w http.ResponseWriter, r *http.Request, db *sql.DB, item *managedForward) {
	code, err := s.deployP2PCore(r.Context(), db, item)
	if err != nil {
		response.Error(w, code, err.Error())
		return
	}
	response.OK(w, s.loadForward(r.Context(), db, item.ID))
}

// deployP2PCore 实际部署 P2P 链路：中继保底 + 两端候选端点收集 + 打洞协调。
func (s *Service) deployP2PCore(ctx context.Context, db *sql.DB, item *managedForward) (int, error) {
	// 1) 保底：建立 tcp_relay 隧道，保证部署完成即可用；打洞失败透明留在中继。
	if item.RelayServerID == "" {
		return 422, errors.New("P2P 转发需要配置中继入口主机（作为打洞失败时的保底数据面）")
	}
	if code, err := s.deployTCPRelayCore(ctx, db, item); err != nil {
		return code, err
	}
	if item.P2PPeerServerID == "" {
		return 0, nil
	}
	// 2) 校验对端：在线且支持 p2p_v1
	peerConn, ok := s.registry.Get(item.P2PPeerServerID)
	if !ok {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_peer_offline',last_error='P2P 对端离线，已用中继保底',updated_at=datetime('now') WHERE id=?`, item.ID)
		return 0, nil
	}
	if !peerConn.GetCapabilities()["p2p_v1"] {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_peer_old',last_error='P2P 对端 Agent 版本过低，已用中继保底',updated_at=datetime('now') WHERE id=?`, item.ID)
		return 0, nil
	}
	// 3) 收集两端候选端点。优先用自建 STUN（部署在中继入口主机），失败则公共 STUN 兜底。
	stunServers := s.selfHostedStunServers(ctx, db, item.RelayServerID)
	if len(stunServers) == 0 {
		stunServers = []string{"stun.cloudflare.com:3478", "stun.l.google.com:19302"}
	}
	collectPayload, _ := json.Marshal(map[string]interface{}{
		"operation": "collect_endpoints", "forward_id": item.ID, "stun_servers": stunServers,
	})
	sourceOut, err := s.RunP2PTaskAndWait(item.ServerID, string(collectPayload))
	if err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_collect_failed',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 0, nil
	}
	peerOut, err := s.RunP2PTaskAndWait(item.P2PPeerServerID, string(collectPayload))
	if err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_collect_failed',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 0, nil
	}
	sourceEndpoints := extractEndpoints(sourceOut)
	peerEndpoints := extractEndpoints(peerOut)
	if len(sourceEndpoints) == 0 || len(peerEndpoints) == 0 {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_no_endpoints',last_error='P2P 未收集到候选端点，已用中继保底',updated_at=datetime('now') WHERE id=?`, item.ID)
		return 0, nil
	}
	// 4) 生成共享 session_id 并两侧下发 hole_punch（互带对端候选端点）
	sessionID := randomForwardSessionID()
	sourcePunch, _ := json.Marshal(map[string]interface{}{
		"operation": "hole_punch", "forward_id": item.ID,
		"local_host": item.LocalHost, "local_port": item.LocalPort,
		"session_id": sessionID, "peer_candidates": peerEndpoints,
	})
	peerPunch, _ := json.Marshal(map[string]interface{}{
		"operation": "hole_punch", "forward_id": item.ID,
		"local_host": item.LocalHost, "local_port": item.LocalPort,
		"session_id": sessionID, "peer_candidates": sourceEndpoints,
	})
	if _, err := s.RunP2PTaskAndWait(item.ServerID, string(sourcePunch)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_hole_punch_failed',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 0, nil
	}
	if _, err := s.RunP2PTaskAndWait(item.P2PPeerServerID, string(peerPunch)); err != nil {
		_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='p2p_hole_punch_failed',last_error=?,updated_at=datetime('now') WHERE id=?`, err.Error(), item.ID)
		return 0, nil
	}
	_, _ = db.ExecContext(ctx, `UPDATE managed_forwards SET last_stage='completed',last_error='',updated_at=datetime('now') WHERE id=?`, item.ID)
	return 0, nil
}

// extractEndpoints 解析 collect_endpoints 返回的候选端点字符串数组。
func extractEndpoints(out string) []string {
	var parsed struct {
		Endpoints []struct {
			Addr string `json:"addr"`
		} `json:"endpoints"`
	}
	if json.Unmarshal([]byte(out), &parsed) != nil {
		return nil
	}
	eps := make([]string, 0, len(parsed.Endpoints))
	for _, e := range parsed.Endpoints {
		if e.Addr != "" {
			eps = append(eps, e.Addr)
		}
	}
	return eps
}

// randomForwardSessionID 生成 32 位打洞会话随机密钥（仅分发给两端 A/B）。
func randomForwardSessionID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano())
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// selfHostedStunServers 尝试在中继入口主机上托管自建 STUN 服务，返回其公网地址。
// 中继主机有公网 IP 且已装 Agent，适合充当 STUN 协调节点。任一环节失败返回空，由调用方回退公共 STUN。
func (s *Service) selfHostedStunServers(ctx context.Context, db *sql.DB, relayServerID string) []string {
	if relayServerID == "" {
		return nil
	}
	var relayHost string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(host,'') FROM server_accounts WHERE id=?`, relayServerID).Scan(&relayHost); err != nil || relayHost == "" {
		return nil
	}
	relayConn, ok := s.registry.Get(relayServerID)
	if !ok || !relayConn.GetCapabilities()["p2p_v1"] {
		return nil
	}
	meta := relayConn.GetMetadata()
	assetURL, assetSHA, assetOK := stunAssetFor(fmt.Sprint(meta["platform"]), fmt.Sprint(meta["arch"]))
	if !assetOK {
		return nil
	}
	const stunPort = 3478
	bootstrapPayload, _ := json.Marshal(map[string]interface{}{
		"operation": "bootstrap_stun", "stun_asset_url": assetURL,
		"stun_asset_sha256": assetSHA, "stun_port": stunPort,
	})
	if _, err := s.RunP2PTaskAndWait(relayServerID, string(bootstrapPayload)); err != nil {
		return nil
	}
	addr := relayHost
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	addr = net.JoinHostPort(host, fmt.Sprint(stunPort))
	return []string{addr}
}

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
