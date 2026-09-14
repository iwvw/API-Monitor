package serveragent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"data":      s.buildInfoStruct(info),
		"is_cached": !req.Force,
	})
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
