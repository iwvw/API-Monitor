package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/crypto/ssh"
)

// ==========================================
// SERVER INFO & OPERATIONS
// ==========================================

// handleServerInfo 获取服务器详细信息
func (s *Service) handleServerInfo(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Force    bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ServerID) == "" {
		response.Error(w, http.StatusBadRequest, "serverId required")
		return
	}

	var status string
	var cachedInfo string
	err := db.QueryRowContext(r.Context(), `
		SELECT status, COALESCE(cached_info, '{}')
		FROM server_accounts WHERE id = ?`, req.ServerID).Scan(&status, &cachedInfo)
	if err == sql.ErrNoRows {
		response.Error(w, http.StatusNotFound, "server not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	info := map[string]interface{}{}
	_ = json.Unmarshal([]byte(cachedInfo), &info)
	if conn, ok := s.registry.Get(req.ServerID); ok {
		for k, v := range conn.GetMetadata() {
			info[k] = v
		}
		info["agent_online"] = true
		info["status"] = "online"
	} else {
		info["agent_online"] = false
		info["status"] = status
		s.probeSSHPlatform(r.Context(), db, req.ServerID, info)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"data":      s.buildInfoStruct(info),
		"is_cached": !req.Force,
	})
}

// sshProbeInFlight 防止同一主机的系统版本探测并发重复执行。
var sshProbeInFlight sync.Map

// probeSSHPlatform 通过 SSH 探测无 Agent 主机的系统与版本并写入 cached_info，
// 结果持久化后后续请求直接命中缓存；探测失败打标记避免每次刷新都重连。
func (s *Service) probeSSHPlatform(ctx context.Context, db *sql.DB, serverID string, info map[string]interface{}) {
	platform := ""
	if v, ok := info["platform"].(string); ok {
		platform = strings.TrimSpace(v)
	}
	version := ""
	if v, ok := info["platform_version"].(string); ok {
		version = strings.TrimSpace(v)
	}
	if platform != "" && version != "" {
		return
	}
	if failed, _ := info["_ssh_probe_failed"].(bool); failed {
		return
	}
	if _, loaded := sshProbeInFlight.LoadOrStore(serverID, true); loaded {
		return
	}
	defer sshProbeInFlight.Delete(serverID)

	output, err := s.executeSSHCommand(ctx, db, serverID, "uname -s -r || ver", 8*time.Second)
	probePlatform, probeVersion := "", ""
	if err == nil {
		probePlatform, probeVersion = parseSSHPlatformProbe(output)
	}
	if probePlatform == "" {
		s.persistServerInfoPatch(ctx, db, serverID, map[string]interface{}{"_ssh_probe_failed": true})
		info["_ssh_probe_failed"] = true
		return
	}
	// 探测成功即清除历史失败标记：否则一次瞬时 SSH 故障会永久禁用探测，
	// 无 Agent 主机将再也拿不到 platform/platform_version。
	patch := map[string]interface{}{"_ssh_probe_failed": false}
	info["_ssh_probe_failed"] = false
	if platform == "" {
		patch["platform"] = probePlatform
		info["platform"] = probePlatform
	}
	if version == "" {
		patch["platform_version"] = probeVersion
		info["platform_version"] = probeVersion
	}
	s.persistServerInfoPatch(ctx, db, serverID, patch)
}

// parseSSHPlatformProbe 解析 "uname -s -r || ver" 的输出：
// POSIX 返回 "Linux 6.8.0-51-generic"；Windows cmd 返回 "Microsoft Windows [Version ...]"。
func parseSSHPlatformProbe(output string) (string, string) {
	line := ""
	for _, part := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			line = trimmed
			break
		}
	}
	if line == "" {
		return "", ""
	}
	if strings.Contains(line, "Windows") {
		if idx := strings.Index(line, "[Version"); idx >= 0 {
			ver := strings.Trim(strings.TrimPrefix(line[idx:], "[Version"), "] ")
			return "Windows", ver
		}
		return "Windows", ""
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return fields[0], ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

// persistServerInfoPatch 将字段合并写回 server_accounts.cached_info。
func (s *Service) persistServerInfoPatch(ctx context.Context, db *sql.DB, serverID string, patch map[string]interface{}) {
	// cached_info 可为 NULL（从未上报过 info 的新主机），必须 COALESCE：
	// 直接 Scan NULL 到 string 会报错并静默 return，patch 永远写不进去。
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?`, serverID).Scan(&raw); err != nil {
		return
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || stored == nil {
		stored = map[string]interface{}{}
	}
	for k, v := range patch {
		stored[k] = v
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return
	}
	_, _ = db.ExecContext(ctx, `UPDATE server_accounts SET cached_info = ? WHERE id = ?`, string(encoded), serverID)
}

// handleTestConnection 测试服务器连接
func (s *Service) handleTestConnection(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
		AuthType   string `json:"auth_type"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}

	authMethods := []ssh.AuthMethod{}
	if req.AuthType == "key" {
		var signer ssh.Signer
		var err error
		if req.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(req.PrivateKey), []byte(req.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(req.PrivateKey))
		}
		if err != nil {
			writeConnectionTestFailure(w, "SSH private key parse failed")
			return
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else if req.Password != "" {
		authMethods = append(authMethods, ssh.Password(req.Password))
	}
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.Username) == "" || len(authMethods) == 0 {
		writeConnectionTestFailure(w, "SSH connection config incomplete")
		return
	}

	start := time.Now()
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", req.Host, req.Port), &ssh.ClientConfig{
		User:            req.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":      false,
			"status":       "failed",
			"message":      err.Error(),
			"error":        err.Error(),
			"responseTime": elapsed,
		})
		return
	}
	_ = client.Close()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"status":       "success",
		"message":      "connection successful",
		"responseTime": elapsed,
	})
}

// handleServerAction 执行服务器操作（重启、关机等）
func (s *Service) handleServerAction(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Action   string `json:"action"` // reboot, shutdown, etc.
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" || req.Action == "" {
		response.Error(w, http.StatusBadRequest, "serverId and action required")
		return
	}
	allowed := map[string]bool{"reboot": true, "restart": true, "shutdown": true}
	if !allowed[req.Action] {
		response.Error(w, http.StatusBadRequest, "unsupported action")
		return
	}
	conn, ok := s.registry.Get(req.ServerID)
	if !ok {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "agent is offline",
			"message": "agent is offline",
		})
		return
	}
	if err := conn.SendEvent("server:action", map[string]interface{}{"action": req.Action}); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "action dispatched",
	})
}

// handleCheckAll 批量检查所有服务器状态（含名称，供 agent/API 消费方直接识别主机）
func (s *Service) handleCheckAll(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, COALESCE(name, ''), status FROM server_accounts")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results := []map[string]interface{}{}
	online := 0
	offline := 0
	for rows.Next() {
		var id string
		var name string
		var status string
		if err := rows.Scan(&id, &name, &status); err != nil {
			continue
		}
		agentOnline := false
		if _, ok := s.registry.Get(id); ok {
			status = "online"
			agentOnline = true
			online++
		} else {
			status = "offline"
			offline++
		}
		results = append(results, map[string]interface{}{
			"serverId":     id,
			"name":         name,
			"status":       status,
			"agent_online": agentOnline,
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"checked": len(results),
		"online":  online,
		"offline": offline,
		"data":    results,
	})
}

func writeConnectionTestFailure(w http.ResponseWriter, message string) {
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": false,
		"status":  "failed",
		"message": message,
		"error":   message,
	})
}
