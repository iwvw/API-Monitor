package serveragent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"golang.org/x/crypto/ssh"
)

func (s *Service) handleAgentQuickInstall(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		ID       string `json:"id"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	serverID := req.ServerID
	if serverID == "" {
		serverID = req.ID
	}
	if serverID == "" {
		createdID, created, err := s.ensureQuickInstallAgentHost(r.Context(), db, req.Name)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		serverID = createdID

		rec := httptest.NewRecorder()
		s.getAgentInstallCommand(rec, r, db, serverID)
		if rec.Code < 200 || rec.Code >= 300 {
			for k, values := range rec.Header() {
				for _, value := range values {
					w.Header().Add(k, value)
				}
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			response.Error(w, http.StatusInternalServerError, "invalid install command response")
			return
		}
		data, _ := payload["data"].(map[string]interface{})
		if data == nil {
			data = map[string]interface{}{}
		}
		data["serverId"] = serverID
		data["isNew"] = created
		payload["data"] = data
		response.JSON(w, http.StatusOK, payload)
		return
	}

	rec := httptest.NewRecorder()
	s.getAgentInstallScript(rec, r, db, serverID)
	if rec.Code < 200 || rec.Code >= 300 {
		for k, values := range rec.Header() {
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return
	}

	script := rec.Body.String()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"serverId": serverID,
			"script":   script,
			"command":  "curl -fsSL " + s.agentInstallURL(r, serverID) + " | bash",
		},
	})
}

func (s *Service) ensureQuickInstallAgentHost(ctx context.Context, db *sql.DB, name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, fmt.Errorf("server name is required")
	}

	var existingID string
	err := db.QueryRowContext(ctx, `
		SELECT id FROM server_accounts
		WHERE name = ? AND monitor_mode = 'agent'
		ORDER BY created_at DESC
		LIMIT 1`, name).Scan(&existingID)
	if err == nil {
		return existingID, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}

	var maxOrder sql.NullInt64
	_ = db.QueryRowContext(ctx, "SELECT MAX(order_index) FROM server_accounts").Scan(&maxOrder)
	orderIndex := 1
	if maxOrder.Valid {
		orderIndex = int(maxOrder.Int64) + 1
	}

	id := uuid.NewString()
	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
		INSERT INTO server_accounts (
			id, name, host, port, username, auth_type, status, monitor_mode,
			tags, description, order_index, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, "0.0.0.0", 22, "agent", "password", "unknown", "agent",
		SerializeList([]string{"agent"}), "Created from Agent quick install", orderIndex, now, now,
	)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (s *Service) agentInstallURL(r *http.Request, serverID string) string {
	proto, host := resolveInstallOrigin(r)
	return fmt.Sprintf("%s://%s/api/server/agent/install-script/%s", proto, host, serverID)
}

func resolveInstallOrigin(r *http.Request) (string, string) {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}

	proto := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("protocol")))
	if proto != "http" && proto != "https" {
		proto = strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	}
	if proto != "http" && proto != "https" {
		proto = "https"
		if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
			proto = "http"
		}
	}

	return proto, host
}

func appendInstallProtocol(rawURL, proto string) string {
	if strings.TrimSpace(rawURL) == "" || (proto != "http" && proto != "https") {
		return rawURL
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + "protocol=" + proto
}

// getAgentInstallScript 生成 Agent 安装脚本
func (s *Service) getAgentInstallScriptWithKey(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string, agentKey string) {
	storedKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}
	if agentKey != storedKey {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}
	s.getAgentInstallScript(w, r, db, accountID)
}

func (s *Service) getAgentInstallScript(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
	var name, host string
	var port int
	err := db.QueryRowContext(r.Context(), "SELECT name, host, port FROM server_accounts WHERE id = ?", accountID).Scan(&name, &host, &port)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "Account not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	agentKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	proto, serverURL := resolveInstallOrigin(r)

	script := fmt.Sprintf(`#!/bin/bash
# API Monitor Agent install script
# Host: %s (%s:%d)
# Generated at: %s

set -e

AGENT_VERSION="latest"
INSTALL_DIR="/opt/api-monitor-agent"
SERVER_URL="%s://%s"
SERVER_ID="%s"
AGENT_KEY="%s"

echo "Installing API Monitor Agent..."
echo "Target host: %s"
echo "Server: $SERVER_URL"

if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
else
    if ! command -v sudo >/dev/null 2>&1; then
        echo "Error: this installer must run as root or on a system with sudo installed"
        exit 1
    fi
    SUDO="sudo"
fi

$SUDO mkdir -p $INSTALL_DIR
cd $INSTALL_DIR

ARCH=$(uname -m)
case $ARCH in
    x86_64)
        AGENT_ARCH="amd64"
        ;;
    aarch64|arm64)
        AGENT_ARCH="arm64"
        ;;
    *)
        echo "Error: unsupported architecture $ARCH"
        exit 1
        ;;
esac

AGENT_URL="$SERVER_URL/agent/agent-linux-$AGENT_ARCH"
echo "Downloading Agent..."
TMP_AGENT="$(mktemp /tmp/api-monitor-agent.XXXXXX)"
trap 'rm -f "$TMP_AGENT"' EXIT
$SUDO curl -fsSL -o "$TMP_AGENT" "$AGENT_URL" || {
    echo "Error: failed to download Agent binary"
    echo "URL: $AGENT_URL"
    exit 1
}

$SUDO chmod +x "$TMP_AGENT"

$SUDO "$TMP_AGENT" --version || {
    echo "Error: Agent binary failed to run"
    exit 1
}

if systemctl list-unit-files api-monitor-agent.service >/dev/null 2>&1; then
    echo "Removing old Agent installation..."
    $SUDO systemctl stop api-monitor-agent 2>/dev/null || true
    $SUDO systemctl disable api-monitor-agent 2>/dev/null || true
    $SUDO rm -f /etc/systemd/system/api-monitor-agent.service
    $SUDO systemctl daemon-reload
fi

# Kill any leftover agent processes
$SUDO pkill -f api-monitor-agent 2>/dev/null || true
sleep 1

# Remove old binary
$SUDO rm -f "$INSTALL_DIR/api-monitor-agent"

$SUDO install -m 0755 "$TMP_AGENT" "$INSTALL_DIR/api-monitor-agent"

echo "Creating systemd service..."
$SUDO tee /etc/systemd/system/api-monitor-agent.service > /dev/null <<EOF
[Unit]
Description=API Monitor Agent
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/api-monitor-agent -s $SERVER_URL --id $SERVER_ID -k $AGENT_KEY
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

$SUDO systemctl daemon-reload
$SUDO systemctl enable api-monitor-agent
$SUDO systemctl restart api-monitor-agent || {
    $SUDO systemctl daemon-reload
    $SUDO systemctl enable api-monitor-agent
    $SUDO systemctl restart api-monitor-agent
}

$SUDO systemctl is-active --quiet api-monitor-agent || {
    echo "Error: api-monitor-agent failed to start"
    $SUDO systemctl status api-monitor-agent --no-pager || true
    $SUDO journalctl -u api-monitor-agent -n 50 --no-pager || true
    exit 1
}

echo ""
echo "======================================"
echo "Agent installation completed."
echo "======================================"
echo ""
echo "Service status: $(systemctl is-active api-monitor-agent)"
echo "Install dir: $INSTALL_DIR"
echo "Server ID: $SERVER_ID"
echo ""
echo "Useful commands:"
echo "  Status:  sudo systemctl status api-monitor-agent"
echo "  Start:   sudo systemctl start api-monitor-agent"
echo "  Stop:    sudo systemctl stop api-monitor-agent"
echo "  Restart: sudo systemctl restart api-monitor-agent"
echo "  Logs:    sudo journalctl -u api-monitor-agent -f"
echo ""
`,
		name, host, port,
		time.Now().Format("2006-01-02 15:04:05"),
		proto, serverURL,
		accountID,
		agentKey,
		name,
	)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%s.sh", accountID))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}

// getOrGenerateAgentKey 获取或生成全局 Agent key
func (s *Service) getOrGenerateAgentKey(ctx context.Context, db *sql.DB) (string, error) {
	// 先尝试从 system_config 读取
	var key string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'global_agent_key'").Scan(&key)
	if err == nil && key != "" {
		return key, nil
	}

	// 生成新密钥
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", err
	}
	key = hex.EncodeToString(keyBytes)

	// 保存到数据库
	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('global_agent_key', ?, datetime('now'))", key)
	if err != nil {
		return "", err
	}

	return key, nil
}

// handleAgentHeartbeat 处理 Agent 心跳
func (s *Service) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string                 `json:"server_id"`
		Status   string                 `json:"status"`
		Info     map[string]interface{} `json:"info"`
		Metrics  map[string]interface{} `json:"metrics"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ServerID == "" {
		response.Error(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if req.Info == nil {
		req.Info = map[string]interface{}{}
	}

	// 验证 Agent key
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		response.Error(w, http.StatusUnauthorized, "Missing or invalid Authorization header")
		return
	}

	providedKey := strings.TrimPrefix(authHeader, "Bearer ")
	expectedKey, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil || providedKey != expectedKey {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}

	// 更新账号状态
	now := time.Now().Format("2006-01-02 15:04:05")
	status := req.Status
	if status == "" {
		status = "online"
	}

	// 计算响应时间（从客户端时间戳）
	responseTime := 0
	if ts, ok := req.Info["timestamp"].(float64); ok {
		responseTime = int(time.Now().Unix()*1000 - int64(ts))
		if responseTime < 0 {
			responseTime = 0
		}
	}

	// 序列化 info 为 cached_info
	if firstNonEmpty(
		getString(req.Info, "resolved_country"),
		getString(req.Info, "country_code"),
		getString(req.Info, "location"),
		getString(req.Info, "region"),
	) == "" {
		if ip := getString(req.Info, "ip"); ip != "" {
			if geo, ok := s.lookupHostLocation(r.Context(), ip); ok {
				for key, value := range geo {
					req.Info[key] = value
				}
			}
		}
	}

	if req.Metrics != nil && len(req.Metrics) > 0 {
		s.markRealtimeMetricsHealthy(req.ServerID, req.Info, time.Now())
	}
	cachedInfo, _ := json.Marshal(req.Info)
	resolvedCountry := firstNonEmpty(
		getString(req.Info, "resolved_country"),
		getString(req.Info, "country_code"),
		getString(req.Info, "country"),
		getString(req.Info, "region"),
		getString(req.Info, "location"),
	)

	_, err = db.ExecContext(r.Context(), `UPDATE server_accounts
		SET status = ?,
		    last_check_time = ?,
		    last_check_status = 'success',
		    response_time = ?,
		    resolved_country = COALESCE(NULLIF(resolved_country, ''), ?),
		    cached_info = ?,
		    updated_at = ?
		WHERE id = ?`,
		status, now, responseTime, nullStr(resolvedCountry), string(cachedInfo), now, req.ServerID)

	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 持久化指标
	if req.Metrics != nil && len(req.Metrics) > 0 {
		if err := s.persistMetrics(r.Context(), db, req.ServerID, req.Metrics); err != nil {
			s.markRealtimeMetricsPersistResult(req.ServerID, false, err, time.Now())
			// 指标持久化失败不影响心跳响应
			applog.Warn(r.Context(), "serveragent", "failed to persist heartbeat metrics", "server_id", req.ServerID, "error", err.Error())
		} else {
			s.markRealtimeMetricsPersistResult(req.ServerID, true, nil, time.Now())
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Heartbeat received",
	})
}

// getAgentStatus 获取 Agent 状态
func (s *Service) getAgentStatus(w http.ResponseWriter, r *http.Request, db *sql.DB, accountID string) {
	var status, lastCheckTime, lastCheckStatus, cachedInfo string
	var responseTime int

	err := db.QueryRowContext(r.Context(), `
		SELECT status, last_check_time, last_check_status, response_time, COALESCE(cached_info, '{}')
		FROM server_accounts
		WHERE id = ?
	`, accountID).Scan(&status, &lastCheckTime, &lastCheckStatus, &responseTime, &cachedInfo)

	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "账号不存在")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	var info map[string]interface{}
	if err := json.Unmarshal([]byte(cachedInfo), &info); err != nil {
		info = make(map[string]interface{})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"status":            status,
			"last_check_time":   lastCheckTime,
			"last_check_status": lastCheckStatus,
			"response_time":     responseTime,
			"info":              info,
		},
	})
}

// generateNewAgentKey 生成新的 Agent key
func (s *Service) generateNewAgentKey(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to generate key")
		return
	}
	key := hex.EncodeToString(keyBytes)

	_, err := db.ExecContext(r.Context(), "INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('global_agent_key', ?, datetime('now'))", key)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"key": key,
		},
	})
}

// getCurrentAgentKey 获取当前 Agent key
func (s *Service) getCurrentAgentKey(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	key, err := s.getOrGenerateAgentKey(r.Context(), db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"key": key,
		},
	})
}

// handleAgentConnectionInfo 获取 Agent 连接详情 (用于精确判定上线状态)
func (s *Service) handleAgentConnectionInfo(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	conn, exists := s.registry.Get(serverID)
	if exists {
		connectedAt := conn.AuthenticatedAt.UnixNano() / int64(time.Millisecond)
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"status":      "online",
			"connectedAt": connectedAt,
		})
	} else {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  "offline",
		})
	}
}

// handleAgentAutoInstall 自动安装 Agent (通过 SSH 或升级指令)
func (s *Service) handleAgentAutoInstall(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	var req struct {
		ForceSSH bool `json:"force_ssh"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// 1. 检测 Agent 是否在线
	if conn, exists := s.registry.Get(serverID); exists && !req.ForceSSH {
		// 发送升级指令
		if s.sendUpgradeTask(conn) {
			response.JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "Agent 升级指令已下发（后台执行）",
				"output":  "正在通过现有的 Agent 连接执行版本更新...",
			})
			return
		}
	}

	// 2. 否则通过 SSH 连接执行远程安装
	rec := httptest.NewRecorder()
	s.getAgentInstallScript(rec, r, db, serverID)
	if rec.Code != http.StatusOK {
		response.JSON(w, rec.Code, map[string]interface{}{
			"success": false,
			"error":   "无法生成安装脚本",
			"details": rec.Body.String(),
		})
		return
	}
	script := rec.Body.String()

	cmd := fmt.Sprintf("cat << 'EOF' > /tmp/agent_install.sh\n%s\nEOF\nsudo bash /tmp/agent_install.sh", script)
	output, err := s.executeSSHCommand(r.Context(), db, serverID, cmd, 120*time.Second)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "安装执行失败",
			"details": err.Error(),
			"output":  output,
		})
		return
	}

	// 更新主机状态
	now := time.Now().Format("2006-01-02 15:04:05")
	_, _ = db.ExecContext(r.Context(), "UPDATE server_accounts SET status = 'online', updated_at = ? WHERE id = ?", now, serverID)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Agent 安装命令已执行",
		"output":  output,
	})
}

// sendUpgradeTask 发送升级指令给 Agent
func (s *Service) sendUpgradeTask(conn *AgentConnection) bool {
	taskID := uuid.New().String()
	payload := map[string]interface{}{
		"id":      taskID,
		"type":    5, // UPGRADE
		"data":    "",
		"timeout": 60,
	}
	err := conn.SendEvent("dashboard:task", payload)
	return err == nil
}

// executeSSHCommand 执行 SSH 远程命令
func (s *Service) executeSSHCommand(ctx context.Context, db *sql.DB, serverID string, cmd string, timeout time.Duration) (string, error) {
	cfg, err := s.getSFTPServerConfigCtx(ctx, db, serverID)
	if err != nil {
		return "", err
	}
	authMethods := []ssh.AuthMethod{}
	if cfg.AuthType == "key" {
		var signer ssh.Signer
		var err error
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return "", fmt.Errorf("AUTH_FAILED: SSH 私钥解析失败: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if len(authMethods) == 0 {
		return "", fmt.Errorf("CONFIG_INCOMPLETE: SSH 凭据不完整")
	}
	sshConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return "", fmt.Errorf("CONNECTION_FAILED: %w", err)
	}
	defer sshClient.Close()

	session, err := sshClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf strings.Builder
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-time.After(timeout):
		return "", fmt.Errorf("SSH execution timed out after %v", timeout)
	case err := <-done:
		if err != nil {
			return stdoutBuf.String(), fmt.Errorf("SSH execution failed: %v, stderr: %s", err, stderrBuf.String())
		}
		return stdoutBuf.String(), nil
	}
}

// getSFTPServerConfigCtx 获取 SSH 数据库配置的 Context 辅助函数
func (s *Service) getSFTPServerConfigCtx(ctx context.Context, db *sql.DB, serverID string) (sftpServerConfig, error) {
	var cfg sftpServerConfig
	var password, privateKey, passphrase sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT host, port, username, auth_type, password, private_key, passphrase
		FROM server_accounts WHERE id = ?`, serverID).
		Scan(&cfg.Host, &cfg.Port, &cfg.Username, &cfg.AuthType, &password, &privateKey, &passphrase)
	if err == sql.ErrNoRows {
		return cfg, fmt.Errorf("SERVER_NOT_FOUND: 服务器配置不存在")
	}
	if err != nil {
		return cfg, err
	}
	cfg.Password = secure.SecureDecrypt(password.String)
	cfg.PrivateKey = secure.SecureDecrypt(privateKey.String)
	cfg.Passphrase = secure.SecureDecrypt(passphrase.String)
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Host == "" || cfg.Username == "" {
		return cfg, fmt.Errorf("CONFIG_INCOMPLETE: SSH 主机或用户名不完整")
	}
	return cfg, nil
}
