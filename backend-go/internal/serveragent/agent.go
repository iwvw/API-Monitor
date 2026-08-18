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
	"net/url"
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
			"command":  "curl -fsSL " + s.agentInstallURL(r.Context(), db, r, serverID) + " | bash",
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

func (s *Service) agentInstallURL(ctx context.Context, db *sql.DB, r *http.Request, serverID string) string {
	proto, host := s.resolveInstallOrigin(ctx, db, r, "")
	return fmt.Sprintf("%s://%s/api/server/agent/install-script/%s", proto, host, serverID)
}

// resolveInstallOrigin 解析 Agent 安装 / 自更新使用的面板来源地址。
// 优先级：
//  1. 显式 base_url（前端 UI 传参）；
//  2. 面板配置的公共 API 地址（user_settings.public_api_url，权威来源）——
//     面板部署在反代 / CDN / 容器平台（如 Fly.io）之后时，请求 Host 可能是内部
//     地址（如 ai.internal），Agent 无法访问，必须优先使用配置的对外地址；
//  3. 请求头推断（X-Forwarded-Host / r.Host），仅作为兜底。
func (s *Service) resolveInstallOrigin(ctx context.Context, db *sql.DB, r *http.Request, baseURL string) (string, string) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = r.URL.Query().Get("base_url")
	}
	if proto, host, ok := parseInstallBaseURL(baseURL); ok {
		return proto, host
	}

	if proto, host, ok := s.panelPublicOrigin(ctx, db); ok {
		return proto, host
	}

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

// panelPublicOrigin 读取面板配置的公共 API 地址（user_settings.public_api_url）。
func (s *Service) panelPublicOrigin(ctx context.Context, db *sql.DB) (string, string, bool) {
	var raw sql.NullString
	err := db.QueryRowContext(ctx, `SELECT public_api_url FROM user_settings WHERE id = 1`).Scan(&raw)
	if err != nil || !raw.Valid {
		return "", "", false
	}
	return parseInstallBaseURL(raw.String)
}

func parseInstallBaseURL(baseURL string) (string, string, bool) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return "", "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "", "", false
	}
	proto := strings.ToLower(parsed.Scheme)
	if proto != "http" && proto != "https" {
		return "", "", false
	}
	host := strings.TrimSpace(parsed.Host)
	if host == "" {
		return "", "", false
	}
	return proto, host, true
}

func (s *Service) resolveAgentDownloadBaseURL(ctx context.Context, db *sql.DB, serverBaseURL string) string {
	var configured sql.NullString
	err := db.QueryRowContext(ctx, `SELECT agent_download_url FROM user_settings WHERE id = 1`).Scan(&configured)
	if err == nil && configured.Valid {
		raw := strings.TrimRight(strings.TrimSpace(configured.String), "/")
		if proto, host, ok := parseInstallBaseURL(raw); ok {
			parsed, _ := url.Parse(raw)
			path := strings.TrimRight(parsed.EscapedPath(), "/")
			return proto + "://" + host + path
		}
	}
	return strings.TrimRight(strings.TrimSpace(serverBaseURL), "/") + "/agent"
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
	storedKey, err := s.getOrGenerateAgentKeyForServer(r.Context(), db, accountID)
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

	agentKey, err := s.getOrGenerateAgentKeyForServer(r.Context(), db, accountID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "Failed to get agent key: "+err.Error())
		return
	}

	proto, serverURL := s.resolveInstallOrigin(r.Context(), db, r, "")
	serverBaseURL := fmt.Sprintf("%s://%s", proto, serverURL)
	agentDownloadBaseURL := s.resolveAgentDownloadBaseURL(r.Context(), db, serverBaseURL)

	script := fmt.Sprintf(`#!/bin/bash
# API Monitor Agent install script
# Host: %s (%s:%d)
# Generated at: %s

set -e

AGENT_VERSION="latest"
INSTALL_DIR="/opt/api-monitor-agent"
SERVER_URL="%s://%s"
AGENT_DOWNLOAD_BASE_URL="%s"
SERVER_ID="%s"
AGENT_KEY="%s"
TARGET_HOST_NAME="%s"

echo "Installing API Monitor Agent..."
echo "Target host: $TARGET_HOST_NAME"
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

if [ "${API_MONITOR_AGENT_INSTALL_DETACHED:-0}" != "1" ] && command -v systemd-run >/dev/null 2>&1 && command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet api-monitor-agent.service; then
    INSTALL_SCRIPT_URL="$SERVER_URL/api/server/agent/install/linux/$SERVER_ID/$AGENT_KEY?protocol=%s&base_url=%s"
    echo "Detected running Agent service. Scheduling detached installer via systemd-run..."
    $SUDO systemd-run --unit="api-monitor-agent-install-$(date +%%s)" --collect --quiet /bin/sh -c "export API_MONITOR_AGENT_INSTALL_DETACHED=1; curl -fsSL '$INSTALL_SCRIPT_URL' | bash"
    echo "Detached installer scheduled. The current Agent terminal may disconnect; installation will continue in systemd."
    # This script is commonly invoked through a curl-to-shell pipeline. Do not close stdin
    # immediately after scheduling the detached update: doing so makes curl
    # report error 23 (write body failed) even though the systemd job exists.
    cat >/dev/null || true
    exit 0
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

if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    echo "Error: API Monitor Agent requires a Linux host running systemd"
    exit 1
fi

for REQUIRED_COMMAND in curl sha256sum tar; do
    command -v "$REQUIRED_COMMAND" >/dev/null 2>&1 || { echo "Error: $REQUIRED_COMMAND is required"; exit 1; }
done

AGENT_URL="$AGENT_DOWNLOAD_BASE_URL/agent-linux-$AGENT_ARCH"
CHECKSUM_URL="$AGENT_URL.sha256"
echo "Downloading Agent..."
TMP_AGENT="$(mktemp /tmp/api-monitor-agent.XXXXXX)"
TMP_CHECKSUM="$(mktemp /tmp/api-monitor-agent-checksum.XXXXXX)"
trap 'rm -f "$TMP_AGENT" "$TMP_CHECKSUM"' EXIT
$SUDO curl -fsSL -o "$TMP_AGENT" "$AGENT_URL" || {
    echo "Error: failed to download Agent binary"
    echo "URL: $AGENT_URL"
    exit 1
}

if $SUDO curl -fsSL -o "$TMP_CHECKSUM" "$CHECKSUM_URL"; then
    EXPECTED_SHA256="$(awk '{print $1}' "$TMP_CHECKSUM")"
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL_SHA256="$(sha256sum "$TMP_AGENT" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL_SHA256="$(shasum -a 256 "$TMP_AGENT" | awk '{print $1}')"
    else
        echo "Error: sha256sum or shasum is required"
        exit 1
    fi
    [ "$EXPECTED_SHA256" = "$ACTUAL_SHA256" ] || {
        echo "Error: Agent checksum verification failed"
        exit 1
    }
else
    echo "Error: signed release checksum is unavailable"
    exit 1
fi

$SUDO chmod +x "$TMP_AGENT"

$SUDO "$TMP_AGENT" --version || {
    echo "Error: Agent binary failed to run"
    exit 1
}

PREVIOUS_AGENT="$INSTALL_DIR/api-monitor-agent.previous"
if [ -f "$INSTALL_DIR/api-monitor-agent" ]; then
    $SUDO cp -f "$INSTALL_DIR/api-monitor-agent" "$PREVIOUS_AGENT"
fi
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
    if [ -f "$PREVIOUS_AGENT" ]; then
        echo "Rolling back Agent binary..."
        $SUDO install -m 0755 "$PREVIOUS_AGENT" "$INSTALL_DIR/api-monitor-agent"
        $SUDO systemctl restart api-monitor-agent || true
    fi
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
		agentDownloadBaseURL,
		accountID,
		agentKey,
		name,
		proto, url.QueryEscape(serverBaseURL),
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
	err := s.validateAgentKeyForServer(r.Context(), db, req.ServerID, providedKey)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "Invalid agent key")
		return
	}
	if req.Metrics != nil && len(req.Metrics) > 0 {
		s.recordAgentSignal(req.ServerID, "metrics", req.Metrics)
	} else {
		s.recordAgentSignal(req.ServerID, "heartbeat", req.Info)
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

	existingInfo := map[string]interface{}{}
	var existingCachedInfo string
	if err := db.QueryRowContext(r.Context(), "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?", req.ServerID).Scan(&existingCachedInfo); err == nil {
		_ = json.Unmarshal([]byte(existingCachedInfo), &existingInfo)
	}
	for key, value := range existingInfo {
		if current, exists := req.Info[key]; !exists || isEmptyHeartbeatInfoValue(current) {
			req.Info[key] = value
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

func isEmptyHeartbeatInfoValue(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
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
	presenceSnapshot := map[string]interface{}{}
	if s.presence != nil {
		presenceSnapshot = s.presence.snapshot(serverID)
	}
	queueDepths := map[string]interface{}{}
	droppedMessages := map[string]interface{}{}
	ptyActiveCount := 0
	if s.ptyHub != nil {
		ptyStats := s.ptyHub.Stats()
		queueDepths["pty"] = ptyStats["queue_depths"]
		droppedMessages["pty"] = ptyStats["dropped"]
		if active, ok := ptyStats["active_count"].(int); ok {
			ptyActiveCount = active
		}
	}
	if s.terminalBroker != nil {
		terminalStats := s.terminalBroker.stats()
		queueDepths["terminal_stream"] = terminalStats["queue_depths"]
		droppedMessages["terminal_stream"] = terminalStats["dropped"]
		if active, ok := terminalStats["active_count"].(int); ok {
			ptyActiveCount += active
		}
	}

	if exists {
		connectedAt := conn.AuthenticatedAt.UnixNano() / int64(time.Millisecond)
		metadata := conn.GetMetadata()
		conn.mu.RLock()
		socket := conn.Socket
		lastHeartbeat := conn.LastHeartbeat
		conn.mu.RUnlock()
		if session, ok := socket.(*EngineIOSession); ok {
			session.mu.RLock()
			queueDepths["agent_pending"] = len(session.PendingMessages)
			if _, hasTransport := presenceSnapshot["transport"]; !hasTransport || presenceSnapshot["transport"] == "" {
				presenceSnapshot["transport"] = session.Transport
			}
			session.mu.RUnlock()
		}
		resp := map[string]interface{}{
			"success":           true,
			"status":            "online",
			"connectedAt":       connectedAt,
			"version":           metadata["version"],
			"platform":          metadata["platform"],
			"last_heartbeat_at": timeToMillis(lastHeartbeat),
			"queue_depths":      queueDepths,
			"dropped_messages":  droppedMessages,
			"pty_active_count":  ptyActiveCount,
			"capabilities":      conn.GetCapabilities(),
		}
		for key, value := range presenceSnapshot {
			resp[key] = value
		}
		response.JSON(w, http.StatusOK, resp)
	} else {
		resp := map[string]interface{}{
			"success":          true,
			"status":           "offline",
			"queue_depths":     queueDepths,
			"dropped_messages": droppedMessages,
			"pty_active_count": ptyActiveCount,
		}
		for key, value := range presenceSnapshot {
			resp[key] = value
		}
		response.JSON(w, http.StatusOK, resp)
	}
}

// handleAgentAutoInstall 自动安装 Agent (通过 SSH 或升级指令)
func (s *Service) handleAgentAutoInstall(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	// 服务器不存在时直接 404，避免后续脚本/SSH 阶段以 500 报错
	var exists int
	if err := db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM server_accounts WHERE id = ?`, serverID).Scan(&exists); err != nil || exists == 0 {
		response.Error(w, http.StatusNotFound, "server not found")
		return
	}
	var req struct {
		ForceSSH bool   `json:"force_ssh"`
		BaseURL  string `json:"base_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// 1. 检测 Agent 是否在线
	proto, host := s.resolveInstallOrigin(r.Context(), db, r, req.BaseURL)
	origin := agentInstallOrigin{Proto: proto, Host: host}

	if conn, exists := s.registry.Get(serverID); exists && !req.ForceSSH {
		// 发送升级指令
		downloadURL := s.agentUpgradeDownloadURL(r.Context(), db, conn, origin)
		if s.sendUpgradeTask(conn, downloadURL) {
			response.JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "Agent 自更新指令已下发（后台执行）",
				"output":  "Agent 将在目标主机上启动独立后台 updater；控制连接和终端短暂断开不影响升级继续执行。",
			})
			return
		}
	}

	// 2. 否则通过 SSH 连接执行远程安装
	if s.presence != nil {
		s.presence.suppress(serverID, 10*time.Minute)
	}
	script, err := s.renderAgentInstallScript(r.Context(), db, serverID, origin)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "无法生成安装脚本",
			"details": err.Error(),
		})
		return
	}

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

func (s *Service) agentUpgradeDownloadURL(ctx context.Context, db *sql.DB, conn *AgentConnection, origin agentInstallOrigin) string {
	proto := origin.Proto
	if proto != "http" && proto != "https" {
		proto = "https"
	}
	host := strings.TrimSpace(origin.Host)
	serverBaseURL := strings.TrimRight(fmt.Sprintf("%s://%s", proto, host), "/")
	baseURL := s.resolveAgentDownloadBaseURL(ctx, db, serverBaseURL)

	metadata := map[string]interface{}{}
	if conn != nil {
		metadata = conn.GetMetadata()
	}
	platform := strings.ToLower(strings.TrimSpace(fmt.Sprint(metadata["platform"])))
	arch := strings.ToLower(strings.TrimSpace(fmt.Sprint(metadata["arch"])))
	filename := agentBinaryFilenameFor(platform, arch)
	return strings.TrimRight(baseURL, "/") + "/" + filename
}

func agentBinaryFilenameFor(platform, arch string) string {
	if strings.Contains(platform, "windows") || strings.Contains(platform, "win") {
		return "agent-windows-amd64.exe"
	}
	if strings.Contains(arch, "arm64") || strings.Contains(arch, "aarch64") {
		return "agent-linux-arm64"
	}
	return "agent-linux-amd64"
}

// sendUpgradeTask 发送自更新指令给 Agent
func (s *Service) sendUpgradeTask(conn *AgentConnection, downloadURL string) bool {
	return s.sendUpgradeTaskWithID(conn, downloadURL, uuid.New().String())
}

func (s *Service) sendUpgradeTaskWithID(conn *AgentConnection, downloadURL string, taskID string) bool {
	if s.presence != nil && conn != nil {
		s.presence.suppress(conn.ServerID, 10*time.Minute)
	}
	if strings.TrimSpace(taskID) == "" {
		taskID = uuid.New().String()
	}
	data, _ := json.Marshal(map[string]string{
		"download_url": downloadURL,
	})
	payload := map[string]interface{}{
		"id":      taskID,
		"type":    5, // UPGRADE
		"data":    string(data),
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
