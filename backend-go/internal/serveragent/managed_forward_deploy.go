package serveragent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

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
