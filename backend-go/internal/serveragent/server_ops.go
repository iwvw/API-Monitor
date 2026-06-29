package serveragent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"github.com/pkg/sftp"
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

// handleCheckAll 批量检查所有服务器状态
func (s *Service) handleCheckAll(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, status FROM server_accounts")
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
		var status string
		if err := rows.Scan(&id, &status); err != nil {
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

// ==========================================
// SFTP OPERATIONS
// ==========================================

func (s *Service) handleSFTPRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		response.Error(w, http.StatusBadRequest, "SFTP operation not specified")
		return
	}

	operation := subparts[0]

	switch operation {
	case "list":
		s.handleSFTPList(w, r, db)
	case "read":
		s.handleSFTPRead(w, r, db)
	case "write":
		s.handleSFTPWrite(w, r, db)
	case "mkdir":
		s.handleSFTPMkdir(w, r, db)
	case "rename":
		s.handleSFTPRename(w, r, db)
	case "delete":
		s.handleSFTPDelete(w, r, db)
	case "rmdir":
		s.handleSFTPRmdir(w, r, db)
	case "chmod":
		s.handleSFTPChmod(w, r, db)
	case "upload":
		s.handleSFTPUpload(w, r, db)
	case "download":
		if len(subparts) >= 2 {
			s.handleSFTPDownload(w, r, db, subparts[1])
		} else {
			response.Error(w, http.StatusBadRequest, "server ID required for download")
		}
	default:
		response.Error(w, http.StatusNotFound, "SFTP operation not found: "+operation)
	}
}

func (s *Service) handleSFTPList(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileList(w, req.ServerID, req.Path)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()

	remotePath := normalizeRemotePath(req.Path)
	realPath, err := client.RealPath(remotePath)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	entries, err := client.ReadDir(realPath)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		files = append(files, fileInfoPayload(realPath, entry))
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    files,
		"files":   files,
		"path":    realPath,
	})
}

func (s *Service) handleSFTPRead(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileRead(w, req.ServerID, req.Path)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()

	content, err := readRemoteFile(client, normalizeRemotePath(req.Path), 1024*1024)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    content,
		"content": content,
	})
}

func (s *Service) handleSFTPWrite(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileWrite(w, req.ServerID, req.Path, req.Content)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := writeRemoteFile(client, normalizeRemotePath(req.Path), []byte(req.Content)); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "文件保存成功",
	})
}

func (s *Service) handleSFTPMkdir(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileMkdir(w, req.ServerID, req.Path)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := client.Mkdir(normalizeRemotePath(req.Path)); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "目录创建成功",
	})
}

func (s *Service) handleSFTPRename(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		OldPath  string `json:"oldPath"`
		NewPath  string `json:"newPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileRename(w, req.ServerID, req.OldPath, req.NewPath)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := client.Rename(normalizeRemotePath(req.OldPath), normalizeRemotePath(req.NewPath)); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "重命名成功",
	})
}

func (s *Service) handleSFTPDelete(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileDelete(w, req.ServerID, req.Path, false)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := client.Remove(normalizeRemotePath(req.Path)); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "文件删除成功",
	})
}

func (s *Service) handleSFTPRmdir(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID  string `json:"serverId"`
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileDelete(w, req.ServerID, req.Path, req.Recursive)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	remotePath := normalizeRemotePath(req.Path)
	if req.Recursive {
		err = removeRemoteDirRecursive(client, remotePath)
	} else {
		err = client.RemoveDirectory(remotePath)
	}
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "目录删除成功",
	})
}

func (s *Service) handleSFTPChmod(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		Path     string `json:"path"`
		Mode     string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	mode, err := strconv.ParseUint(strings.TrimPrefix(req.Mode, "0"), 8, 32)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chmod mode")
		return
	}
	if s.hasAgentConnection(req.ServerID) {
		s.handleAgentFileChmod(w, req.ServerID, req.Path, uint32(mode))
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, req.ServerID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := client.Chmod(normalizeRemotePath(req.Path), os.FileMode(mode)); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "权限修改成功",
	})
}

func (s *Service) handleSFTPUpload(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	serverID := r.FormValue("serverId")
	remoteDir := normalizeRemotePath(r.FormValue("path"))
	relativePath := strings.ReplaceAll(r.FormValue("relativePath"), "\\", "/")
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()

	target := pathpkg.Join(remoteDir, header.Filename)
	if relativePath != "" {
		target = pathpkg.Join(remoteDir, relativePath)
	}
	if s.hasAgentConnection(serverID) {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, file); err != nil {
			writeSFTPError(w, err)
			return
		}
		s.handleAgentFileWrite(w, serverID, target, buf.String())
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, serverID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	if err := client.MkdirAll(pathpkg.Dir(target)); err != nil {
		writeSFTPError(w, err)
		return
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		writeSFTPError(w, err)
		return
	}
	if err := writeRemoteFile(client, target, buf.Bytes()); err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "上传成功",
		"path":    target,
	})
}

func (s *Service) handleSFTPDownload(w http.ResponseWriter, r *http.Request, db *sql.DB, serverID string) {
	remotePath := normalizeRemotePath(r.URL.Query().Get("path"))
	if serverID == "" || remotePath == "" || remotePath == "." {
		response.Error(w, http.StatusBadRequest, "serverId and path required")
		return
	}
	if s.hasAgentConnection(serverID) {
		s.handleAgentFileDownload(w, serverID, remotePath)
		return
	}

	client, cleanup, err := s.openSFTPClient(r, db, serverID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	file, err := client.Open(remotePath)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	filename := pathpkg.Base(remotePath)
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(filename)))
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

const (
	agentFileListTask          = 30
	agentFileReadTask          = 31
	agentFileWriteTask         = 32
	agentFileMkdirTask         = 33
	agentFileDeleteTask        = 34
	agentFileRenameTask        = 35
	agentFileChmodTask         = 37
	agentFileDownloadChunkTask = 38
	agentFileTimeout           = 30 * time.Second
	agentFileChunkSize         = 1024 * 1024
)

func (s *Service) hasAgentConnection(serverID string) bool {
	_, ok := s.registry.Get(serverID)
	return ok
}

func (s *Service) runAgentFileTask(serverID string, taskType int, payload map[string]interface{}, timeout time.Duration) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	result, err := s.runAgentTaskAndWait(serverID, taskType, string(data), timeout)
	if err != nil {
		return "", fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	return result, nil
}

func (s *Service) handleAgentFileList(w http.ResponseWriter, serverID, remotePath string) {
	result, err := s.runAgentFileTask(serverID, agentFileListTask, map[string]interface{}{
		"path": normalizeRemotePath(remotePath),
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}

	var parsed struct {
		Files []map[string]interface{} `json:"files"`
		Cwd   string                   `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		writeSFTPError(w, fmt.Errorf("AGENT_FILE_ERROR: %w", err))
		return
	}
	if parsed.Files == nil {
		parsed.Files = []map[string]interface{}{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"data":      parsed.Files,
		"files":     parsed.Files,
		"path":      firstNonEmpty(parsed.Cwd, normalizeRemotePath(remotePath)),
	})
}

func (s *Service) handleAgentFileRead(w http.ResponseWriter, serverID, remotePath string) {
	content, err := s.runAgentFileTask(serverID, agentFileReadTask, map[string]interface{}{
		"path":    normalizeRemotePath(remotePath),
		"maxSize": int64(1024 * 1024),
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"data":      content,
		"content":   content,
	})
}

func (s *Service) handleAgentFileWrite(w http.ResponseWriter, serverID, remotePath, content string) {
	message, err := s.runAgentFileTask(serverID, agentFileWriteTask, map[string]interface{}{
		"path":    normalizeRemotePath(remotePath),
		"content": content,
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "file saved"),
		"path":      normalizeRemotePath(remotePath),
	})
}

func (s *Service) handleAgentFileMkdir(w http.ResponseWriter, serverID, remotePath string) {
	message, err := s.runAgentFileTask(serverID, agentFileMkdirTask, map[string]interface{}{
		"path": normalizeRemotePath(remotePath),
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "directory created"),
	})
}

func (s *Service) handleAgentFileRename(w http.ResponseWriter, serverID, oldPath, newPath string) {
	message, err := s.runAgentFileTask(serverID, agentFileRenameTask, map[string]interface{}{
		"oldPath": normalizeRemotePath(oldPath),
		"newPath": normalizeRemotePath(newPath),
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "file renamed"),
	})
}

func (s *Service) handleAgentFileDelete(w http.ResponseWriter, serverID, remotePath string, recursive bool) {
	message, err := s.runAgentFileTask(serverID, agentFileDeleteTask, map[string]interface{}{
		"path":      normalizeRemotePath(remotePath),
		"recursive": recursive,
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "file deleted"),
	})
}

func (s *Service) handleAgentFileChmod(w http.ResponseWriter, serverID, remotePath string, mode uint32) {
	message, err := s.runAgentFileTask(serverID, agentFileChmodTask, map[string]interface{}{
		"path": normalizeRemotePath(remotePath),
		"mode": mode,
	}, agentFileTimeout)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "permissions changed"),
	})
}

func (s *Service) handleAgentFileDownload(w http.ResponseWriter, serverID, remotePath string) {
	filename := pathpkg.Base(remotePath)
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(filename)))
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
	w.WriteHeader(http.StatusOK)

	for offset := int64(0); ; offset += agentFileChunkSize {
		encoded, err := s.runAgentFileTask(serverID, agentFileDownloadChunkTask, map[string]interface{}{
			"path":   remotePath,
			"offset": offset,
			"size":   agentFileChunkSize,
		}, agentFileTimeout)
		if err != nil {
			return
		}
		chunk, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(chunk) == 0 {
			return
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
		if len(chunk) < agentFileChunkSize {
			return
		}
	}
}

type sftpServerConfig struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
	Passphrase string
}

func (s *Service) openSFTPClient(r *http.Request, db *sql.DB, serverID string) (*sftp.Client, func(), error) {
	if strings.TrimSpace(serverID) == "" {
		return nil, nil, fmt.Errorf("SERVER_NOT_FOUND: 缺少服务器 ID")
	}
	cfg, err := s.getSFTPServerConfig(r, db, serverID)
	if err != nil {
		return nil, nil, err
	}
	authMethods := []ssh.AuthMethod{}
	if cfg.AuthType == "key" {
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return nil, nil, fmt.Errorf("AUTH_FAILED: SSH 私钥解析失败")
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if len(authMethods) == 0 {
		return nil, nil, fmt.Errorf("CONFIG_INCOMPLETE: SSH 凭据不完整，无法使用 SFTP")
	}
	sshConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("CONNECTION_FAILED: %w", err)
	}
	client, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, fmt.Errorf("CONNECTION_FAILED: %w", err)
	}
	cleanup := func() {
		_ = client.Close()
		_ = sshClient.Close()
	}
	return client, cleanup, nil
}

func (s *Service) getSFTPServerConfig(r *http.Request, db *sql.DB, serverID string) (sftpServerConfig, error) {
	var cfg sftpServerConfig
	var password, privateKey, passphrase sql.NullString
	err := db.QueryRowContext(r.Context(), `
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
		return cfg, fmt.Errorf("CONFIG_INCOMPLETE: SSH 主机或用户名不完整，无法使用 SFTP")
	}
	return cfg, nil
}

func normalizeRemotePath(remotePath string) string {
	text := strings.TrimSpace(remotePath)
	if text == "" {
		return "."
	}
	return pathpkg.Clean(strings.ReplaceAll(text, "\\", "/"))
}

func fileInfoPayload(parent string, info os.FileInfo) map[string]interface{} {
	mode := info.Mode()
	fullPath := pathpkg.Join(parent, info.Name())
	return map[string]interface{}{
		"name":        info.Name(),
		"path":        fullPath,
		"isDirectory": info.IsDir(),
		"isFile":      !info.IsDir(),
		"size":        info.Size(),
		"mode":        uint32(mode.Perm()),
		"mtime":       info.ModTime().UnixMilli(),
		"permissions": formatFileMode(mode),
	}
}

func formatFileMode(mode os.FileMode) string {
	perms := []string{"---", "--x", "-w-", "-wx", "r--", "r-x", "rw-", "rwx"}
	raw := uint32(mode.Perm())
	prefix := "-"
	if mode.IsDir() {
		prefix = "d"
	}
	return prefix + perms[(raw>>6)&7] + perms[(raw>>3)&7] + perms[raw&7]
}

func readRemoteFile(client *sftp.Client, remotePath string, maxSize int64) (string, error) {
	file, err := client.Open(remotePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return "", err
	}
	if stat.Size() > maxSize {
		return "", fmt.Errorf("FILE_TOO_LARGE: 文件过大，最大支持 %d 字节", maxSize)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeRemoteFile(client *sftp.Client, remotePath string, data []byte) error {
	file, err := client.Create(remotePath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func removeRemoteDirRecursive(client *sftp.Client, remotePath string) error {
	entries, err := client.ReadDir(remotePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := pathpkg.Join(remotePath, entry.Name())
		if entry.IsDir() {
			if err := removeRemoteDirRecursive(client, child); err != nil {
				return err
			}
		} else if err := client.Remove(child); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(remotePath)
}

func writeSFTPError(w http.ResponseWriter, err error) {
	msg := err.Error()
	code := "SFTP_ERROR"
	status := http.StatusInternalServerError
	if parts := strings.SplitN(msg, ":", 2); len(parts) == 2 && strings.ToUpper(parts[0]) == parts[0] {
		code = parts[0]
		msg = strings.TrimSpace(parts[1])
	}
	switch code {
	case "SERVER_NOT_FOUND":
		status = http.StatusNotFound
	case "CONFIG_INCOMPLETE":
		status = http.StatusBadRequest
	case "AUTH_FAILED":
		status = http.StatusUnauthorized
	case "PERMISSION_DENIED":
		status = http.StatusForbidden
	case "TIMEOUT":
		status = http.StatusGatewayTimeout
	case "CONNECTION_FAILED":
		status = http.StatusBadGateway
	case "FILE_TOO_LARGE":
		status = http.StatusRequestEntityTooLarge
	}
	response.JSON(w, status, map[string]interface{}{
		"success": false,
		"error":   msg,
		"code":    code,
	})
}

// ==========================================
// DOCKER OPERATIONS
// ==========================================

func (s *Service) handleDockerRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 {
		response.Error(w, http.StatusBadRequest, "Docker operation not specified")
		return
	}

	if subparts[0] == "check-update" && r.Method == http.MethodPost {
		s.handleDockerCheckUpdate(w, r, db)
		return
	}

	response.Error(w, http.StatusNotFound, "Docker operation not found")
}

func (s *Service) handleDockerCheckUpdate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID    string `json:"serverId"`
		ContainerID string `json:"containerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" {
		response.Error(w, http.StatusBadRequest, "serverId required")
		return
	}

	conn, ok := s.registry.Get(req.ServerID)
	if !ok {
		var cachedInfo string
		err := db.QueryRowContext(r.Context(), "SELECT COALESCE(cached_info, '{}') FROM server_accounts WHERE id = ?", req.ServerID).Scan(&cachedInfo)
		if err == sql.ErrNoRows {
			response.Error(w, http.StatusNotFound, "server not found")
			return
		}
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		var info map[string]interface{}
		_ = json.Unmarshal([]byte(cachedInfo), &info)
		results := dockerUpdateResultsFromCache(req.ServerID, req.ContainerID, info)
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    results,
		})
		return
	}

	mappedType := 11 // DOCKER_CHECK_UPDATE
	mappedData := map[string]interface{}{
		"container_id": req.ContainerID,
	}
	bytes, err := json.Marshal(mappedData)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to marshal task data: "+err.Error())
		return
	}
	dataStr := string(bytes)

	task := s.taskRegistry.Create(req.ServerID, "docker.checkUpdates", "container.checkUpdates")
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    mappedType,
		"data":    dataStr,
		"timeout": 180,
	}); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	eventCh := task.Subscribe()
	var finalEvent TaskEvent
	completed := false

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	for !completed {
		select {
		case event, ok := <-eventCh:
			if !ok {
				completed = true
				break
			}
			if event.Status == TaskCompleted || event.Status == TaskFailed {
				finalEvent = event
				completed = true
			}
		case <-ctx.Done():
			completed = true
		}
	}

	if finalEvent.Status != TaskCompleted {
		errMsg := "check update task failed or timed out"
		if finalEvent.Error != "" {
			errMsg = finalEvent.Error
		} else if ctx.Err() != nil {
			errMsg = "timeout waiting for agent response"
		}
		response.Error(w, http.StatusInternalServerError, errMsg)
		return
	}

	var results []map[string]interface{}
	resultStr, ok := finalEvent.Data.(string)
	if ok && resultStr != "" {
		_ = json.Unmarshal([]byte(resultStr), &results)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    results,
	})
}

func dockerUpdateResultsFromCache(serverID, containerID string, info map[string]interface{}) []map[string]interface{} {
	docker, _ := info["docker"].(map[string]interface{})
	containers := asMapList(docker["containers"])
	results := make([]map[string]interface{}, 0, len(containers))
	for _, container := range containers {
		id := stringFromAny(firstNonNil(container["id"], container["ID"], container["container_id"], container["containerId"]))
		if containerID != "" && id != "" && !strings.HasPrefix(id, containerID) && !strings.HasPrefix(containerID, id) {
			continue
		}
		image := stringFromAny(firstNonNil(container["image"], container["Image"]))
		name := stringFromAny(firstNonNil(container["name"], container["Name"], container["names"], container["Names"]))
		results = append(results, map[string]interface{}{
			"serverId":      serverID,
			"containerId":   id,
			"containerName": name,
			"image":         image,
			"has_update":    false,
			"hasUpdate":     false,
			"status":        "unknown",
			"message":       "update check requires live agent support",
		})
	}
	if results == nil {
		return []map[string]interface{}{}
	}
	return results
}

// ==========================================
// V2 TASKS
// ==========================================

func (s *Service) handleV2TasksRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 0 && r.Method == http.MethodGet {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data":    []interface{}{},
		})
		return
	}
	if len(subparts) == 0 && r.Method == http.MethodPost {
		s.handleCreateV2Task(w, r, db)
		return
	}
	if len(subparts) == 1 && subparts[0] == "stream" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		s.writeNamedSSE(w, "ready", map[string]interface{}{"success": true})
		flusher.Flush()

		eventCh, cancel := s.taskRegistry.SubscribeAll()
		defer cancel()

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case event, ok := <-eventCh:
				if !ok {
					return
				}
				payload, include := s.buildTaskUpdatePayload(event)
				if !include {
					continue
				}
				s.writeNamedSSE(w, "task.update", payload)
				flusher.Flush()
			case <-ticker.C:
				s.writeNamedSSE(w, "ping", map[string]interface{}{"ts": time.Now().UnixMilli()})
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
		return
	}

	response.Error(w, http.StatusNotFound, "v2 tasks route not found")
}

func (s *Service) buildTaskUpdatePayload(event TaskEvent) (map[string]interface{}, bool) {
	state := string(event.Status)
	if event.Status == TaskCompleted {
		state = "success"
	}
	payload := map[string]interface{}{
		"taskId":   event.TaskID,
		"state":    state,
		"progress": event.Progress,
	}
	if event.Type != "" {
		payload["event"] = event.Type
	}
	if event.Error != "" {
		payload["error"] = event.Error
		payload["message"] = event.Error
	}
	if msg, ok := taskEventMessage(event.Data); ok && payload["message"] == nil {
		payload["message"] = msg
	}

	task, exists := s.taskRegistry.Get(event.TaskID)
	if exists {
		taskType := task.Type
		action := task.Command
		payload["type"] = taskType
		payload["action"] = action
		payload["serverId"] = task.ServerID
		if strings.HasPrefix(taskType, "docker.internal.") {
			return payload, false
		}
		if strings.HasPrefix(taskType, "docker.") || strings.HasPrefix(action, "container.") ||
			strings.HasPrefix(action, "image.") || strings.HasPrefix(action, "network.") ||
			strings.HasPrefix(action, "volume.") || strings.HasPrefix(action, "compose.") {
			payload["domain"] = "docker"
			return payload, true
		}
	}

	dataMap, _ := event.Data.(map[string]interface{})
	if dataMap != nil {
		if typ, _ := dataMap["type"].(string); typ != "" {
			payload["type"] = typ
			if strings.HasPrefix(typ, "docker.") {
				payload["domain"] = "docker"
				if cmd, _ := dataMap["command"].(string); cmd != "" {
					payload["action"] = cmd
				}
				return payload, true
			}
		}
	}

	return payload, false
}

func taskEventMessage(data interface{}) (string, bool) {
	switch v := data.(type) {
	case string:
		return v, v != ""
	case map[string]interface{}:
		for _, key := range []string{"message", "detail_msg", "data"} {
			if msg, _ := v[key].(string); msg != "" {
				return msg, true
			}
		}
	}
	return "", false
}

func (s *Service) handleV2DockerRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "overview" && r.Method == http.MethodGet {
		s.handleDockerOverview(w, r, db)
		return
	}
	if len(subparts) >= 2 {
		s.handleDockerProxyRoutes(w, r, db, subparts)
		return
	}
	response.Error(w, http.StatusNotFound, "v2 docker route not found")
}

func (s *Service) handleDockerOverview(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	serverID := r.URL.Query().Get("serverId")
	query := "SELECT id, name, status, COALESCE(cached_info, '{}') FROM server_accounts"
	args := []interface{}{}
	if serverID != "" {
		query += " WHERE id = ?"
		args = append(args, serverID)
	}
	query += " ORDER BY order_index ASC, created_at DESC"

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type serverData struct {
		id         string
		name       string
		status     string
		cachedInfo string
	}

	var rawServers []serverData
	for rows.Next() {
		var d serverData
		if err := rows.Scan(&d.id, &d.name, &d.status, &d.cachedInfo); err == nil {
			rawServers = append(rawServers, d)
		}
	}

	scopeParam := r.URL.Query().Get("scope")
	scopes := make(map[string]bool)
	if scopeParam != "" {
		for _, sc := range strings.Split(scopeParam, ",") {
			scopes[strings.TrimSpace(sc)] = true
		}
	}

	servers := []map[string]interface{}{}
	for _, d := range rawServers {
		var info map[string]interface{}
		_ = json.Unmarshal([]byte(d.cachedInfo), &info)
		dockerInfo, _ := info["docker"].(map[string]interface{})
		installed := false
		if v, ok := dockerInfo["installed"].(bool); ok {
			installed = v
		}

		// Extract containers list
		containers := []interface{}{}
		if cList, ok := dockerInfo["containers"].([]interface{}); ok {
			containers = cList
		}

		// If online, dynamically fetch requested resources based on scope.
		var images interface{} = []interface{}{}
		var networks interface{} = []interface{}{}
		var volumes interface{} = []interface{}{}
		var stats interface{} = []interface{}{}
		var composeProjects interface{} = []interface{}{}

		var errOverview, errImages, errNetworks, errVolumes, errStats, errCompose string
		containersSource := "cache"

		if d.status == "online" {
			// Query in parallel
			type taskRes struct {
				key  string
				val  interface{}
				errS string
			}
			ch := make(chan taskRes, 6)
			tasksCount := 0

			runTask := func(key string, taskType int) {
				tasksCount++
				go func() {
					resStr, err := s.runAgentTaskAndWait(d.id, taskType, "", 10*time.Second)
					if err != nil {
						ch <- taskRes{key: key, val: []interface{}{}, errS: err.Error()}
						return
					}
					var parsed interface{}
					if err := json.Unmarshal([]byte(resStr), &parsed); err != nil {
						ch <- taskRes{key: key, val: []interface{}{}, errS: "agent returned invalid docker json: " + err.Error()}
						return
					}
					ch <- taskRes{key: key, val: parsed, errS: ""}
				}()
			}

			if scopes["containers"] {
				runTask("containers", 27) // DOCKER_CONTAINERS
			}
			if scopes["images"] {
				runTask("images", 13) // DOCKER_IMAGES
			}
			if scopes["networks"] {
				runTask("networks", 15) // DOCKER_NETWORKS
			}
			if scopes["volumes"] {
				runTask("volumes", 17) // DOCKER_VOLUMES
			}
			if scopes["stats"] {
				runTask("stats", 20) // DOCKER_STATS
			}
			if scopes["compose"] {
				runTask("compose", 21) // DOCKER_COMPOSE_LIST
			}

			for i := 0; i < tasksCount; i++ {
				res := <-ch
				switch res.key {
				case "containers":
					if res.errS == "" {
						containers = asInterfaceList(res.val)
						containersSource = "live"
					}
					errOverview = res.errS
				case "images":
					images = res.val
					errImages = res.errS
				case "networks":
					networks = res.val
					errNetworks = res.errS
				case "volumes":
					volumes = res.val
					errVolumes = res.errS
				case "stats":
					stats = res.val
					errStats = res.errS
				case "compose":
					composeProjects = res.val
					errCompose = res.errS
					if res.errS == "" {
						upsertDockerStackSnapshot(r.Context(), db, d.id, res.val)
					}
				}
			}

			// Installed can be inferred only from a Docker task that was actually requested and succeeded.
			dockerTaskSucceeded := containersSource == "live" ||
				(scopes["images"] && errImages == "") ||
				(scopes["networks"] && errNetworks == "") ||
				(scopes["volumes"] && errVolumes == "") ||
				(scopes["stats"] && errStats == "") ||
				(scopes["compose"] && errCompose == "")
			if dockerTaskSucceeded {
				installed = true
			}
		}

		running := getInt(dockerInfo, "running")
		stopped := getInt(dockerInfo, "stopped")

		// If running/stopped are 0, we can infer them from containers list if present
		if running == 0 && stopped == 0 && len(containers) > 0 {
			for _, c := range containers {
				if cMap, ok := c.(map[string]interface{}); ok {
					state, _ := cMap["state"].(string)
					if state == "running" {
						running++
					} else {
						stopped++
					}
				}
			}
		}

		servers = append(servers, map[string]interface{}{
			"id":     d.id,
			"name":   d.name,
			"status": d.status,
			"docker": map[string]interface{}{
				"installed":  installed,
				"running":    running,
				"stopped":    stopped,
				"containers": containers,
			},
			"resources": map[string]interface{}{
				"containers":      containers,
				"images":          images,
				"networks":        networks,
				"volumes":         volumes,
				"stats":           stats,
				"composeProjects": composeProjects,
			},
			"errors": map[string]interface{}{
				"overview":        errOverview,
				"images":          errImages,
				"networks":        errNetworks,
				"volumes":         errVolumes,
				"stats":           errStats,
				"composeProjects": errCompose,
			},
			"source": map[string]interface{}{
				"containers": containersSource,
			},
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"servers": servers,
		},
	})
}

func (s *Service) handleCreateV2Task(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req struct {
		ServerID string                 `json:"serverId"`
		Domain   string                 `json:"domain"`
		Action   string                 `json:"action"`
		Type     string                 `json:"type"`
		Config   map[string]interface{} `json:"config"`
		Payload  map[string]interface{} `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ServerID == "" {
		response.Error(w, http.StatusBadRequest, "serverId required")
		return
	}

	var mappedType int
	var mappedData interface{}
	var timeoutSec int = 60

	switch req.Action {
	case "container.start", "container.stop", "container.restart", "container.pause", "container.unpause", "container.pull", "container.delete":
		containerID, _ := req.Payload["containerId"].(string)
		if containerID == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId")
			return
		}
		image, _ := req.Payload["image"].(string)
		actionPart := strings.Split(req.Action, ".")[1]
		mappedType = 10 // DOCKER_ACTION
		mappedData = map[string]interface{}{
			"action":       actionPart,
			"container_id": containerID,
			"image":        image,
		}
		timeoutSec = 120

	case "container.update":
		containerID, _ := req.Payload["containerId"].(string)
		containerName, _ := req.Payload["containerName"].(string)
		if containerID == "" || containerName == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId or containerName")
			return
		}
		image, _ := req.Payload["image"].(string)
		mappedType = 24 // DOCKER_UPDATE_CONTAINER
		mappedData = map[string]interface{}{
			"container_id":   containerID,
			"container_name": containerName,
			"image":          image,
		}
		timeoutSec = 600

	case "container.rename":
		containerID, _ := req.Payload["containerId"].(string)
		newName, _ := req.Payload["newName"].(string)
		if containerID == "" || newName == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId or newName")
			return
		}
		mappedType = 25 // DOCKER_RENAME_CONTAINER
		mappedData = map[string]interface{}{
			"container_id": containerID,
			"new_name":     newName,
		}
		timeoutSec = 60

	case "container.logs":
		containerID, _ := req.Payload["containerId"].(string)
		if containerID == "" {
			response.Error(w, http.StatusBadRequest, "missing containerId")
			return
		}
		tail := 100
		if t, ok := req.Payload["tail"].(float64); ok {
			tail = int(t)
		}
		since, _ := req.Payload["since"].(string)
		mappedType = 19 // DOCKER_LOGS
		mappedData = map[string]interface{}{
			"container_id": containerID,
			"tail":         tail,
			"since":        since,
		}
		timeoutSec = 60

	case "container.checkUpdates":
		containerID, _ := req.Payload["containerId"].(string)
		mappedType = 11 // DOCKER_CHECK_UPDATE
		mappedData = map[string]interface{}{
			"container_id": containerID,
		}
		timeoutSec = 180

	case "container.create":
		image, _ := req.Payload["image"].(string)
		if image == "" {
			response.Error(w, http.StatusBadRequest, "missing image")
			return
		}
		if violations := validateDockerCreatePolicy(req.Payload); len(violations) > 0 {
			response.Error(w, http.StatusBadRequest, "docker create policy violation: "+strings.Join(violations, "; "))
			return
		}
		name, _ := req.Payload["name"].(string)
		ports, _ := req.Payload["ports"]
		if ports == nil {
			ports = []interface{}{}
		}
		volumes, _ := req.Payload["volumes"]
		if volumes == nil {
			volumes = []interface{}{}
		}
		env, _ := req.Payload["env"]
		if env == nil {
			env = map[string]interface{}{}
		}
		network, _ := req.Payload["network"].(string)
		restart, _ := req.Payload["restart"].(string)
		if restart == "" {
			restart = "unless-stopped"
		}
		privileged, _ := req.Payload["privileged"].(bool)
		extraArgs, _ := req.Payload["extraArgs"]
		if extraArgs == nil {
			extraArgs = []interface{}{}
		}

		mappedType = 23 // DOCKER_CREATE_CONTAINER
		mappedData = map[string]interface{}{
			"name":       name,
			"image":      image,
			"ports":      ports,
			"volumes":    volumes,
			"env":        env,
			"network":    network,
			"restart":    restart,
			"privileged": privileged,
			"extra_args": extraArgs,
		}
		timeoutSec = 300

	case "image.list":
		mappedType = 13 // DOCKER_IMAGES
		mappedData = ""
		timeoutSec = 60

	case "image.pull", "image.remove", "image.prune":
		imageRef, _ := req.Payload["image"].(string)
		if imageRef == "" {
			imageRef, _ = req.Payload["imageId"].(string)
		}
		if imageRef == "" {
			imageRef, _ = req.Payload["id"].(string)
		}
		actionPart := strings.Split(req.Action, ".")[1]
		mappedType = 14 // DOCKER_IMAGE_ACTION
		mappedData = map[string]interface{}{
			"action": actionPart,
			"image":  imageRef,
		}
		if actionPart == "pull" {
			timeoutSec = 300
		} else {
			timeoutSec = 60
		}

	case "network.list":
		mappedType = 15 // DOCKER_NETWORKS
		mappedData = ""
		timeoutSec = 60

	case "network.create", "network.remove", "network.connect", "network.disconnect", "network.prune":
		actionPart := strings.Split(req.Action, ".")[1]
		name, _ := req.Payload["name"].(string)
		driver, _ := req.Payload["driver"].(string)
		subnet, _ := req.Payload["subnet"].(string)
		gateway, _ := req.Payload["gateway"].(string)
		container, _ := req.Payload["container"].(string)
		mappedType = 16 // DOCKER_NETWORK_ACTION
		mappedData = map[string]interface{}{
			"action":    actionPart,
			"name":      name,
			"driver":    driver,
			"subnet":    subnet,
			"gateway":   gateway,
			"container": container,
		}
		timeoutSec = 60

	case "volume.list":
		mappedType = 17 // DOCKER_VOLUMES
		mappedData = ""
		timeoutSec = 60

	case "volume.create", "volume.remove", "volume.prune":
		actionPart := strings.Split(req.Action, ".")[1]
		name, _ := req.Payload["name"].(string)
		driver, _ := req.Payload["driver"].(string)
		mappedType = 18 // DOCKER_VOLUME_ACTION
		mappedData = map[string]interface{}{
			"action": actionPart,
			"name":   name,
			"driver": driver,
		}
		timeoutSec = 60

	case "stats.list":
		mappedType = 20 // DOCKER_STATS
		mappedData = ""
		timeoutSec = 60

	case "compose.list":
		mappedType = 21 // DOCKER_COMPOSE_LIST
		mappedData = ""
		timeoutSec = 60

	case "compose.up", "compose.down", "compose.start", "compose.stop", "compose.restart", "compose.pull":
		actionPart := strings.Split(req.Action, ".")[1]
		project, _ := req.Payload["project"].(string)
		if project == "" {
			project, _ = req.Payload["projectName"].(string)
		}
		if project == "" {
			project, _ = req.Payload["Name"].(string)
		}
		if project == "" {
			project, _ = req.Payload["name"].(string)
		}
		configFile := dockerComposeConfigFileFromPayload(req.Payload)
		if project == "" && configFile == "" {
			response.Error(w, http.StatusBadRequest, "missing project or configFile")
			return
		}
		mappedType = 22 // DOCKER_COMPOSE_ACTION
		mappedData = map[string]interface{}{
			"action":      actionPart,
			"project":     project,
			"config_file": configFile,
		}
		if actionPart == "pull" {
			timeoutSec = 300
		} else {
			timeoutSec = 120
		}

	default:
		response.Error(w, http.StatusBadRequest, "unsupported action: "+req.Action)
		return
	}

	var dataStr string
	if str, ok := mappedData.(string); ok {
		dataStr = str
	} else {
		bytes, err := json.Marshal(mappedData)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to marshal task data: "+err.Error())
			return
		}
		dataStr = string(bytes)
	}

	taskType := req.Type
	if taskType == "" {
		taskType = strings.Trim(strings.Join([]string{req.Domain, req.Action}, "."), ".")
	}
	if taskType == "" {
		taskType = "server.task"
	}

	conn, ok := s.registry.Get(req.ServerID)
	if !ok {
		response.Error(w, http.StatusServiceUnavailable, "agent offline")
		return
	}

	task := s.taskRegistry.Create(req.ServerID, taskType, req.Action)
	if err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    mappedType,
		"data":    dataStr,
		"timeout": timeoutSec,
	}); err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		response.Error(w, http.StatusBadGateway, "failed to send task to agent: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"taskId":  task.ID,
		"data": map[string]interface{}{
			"taskId": task.ID,
			"state":  string(task.GetStatus()),
		},
	})
}

func asMapList(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func asInterfaceList(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return []interface{}{}
	}
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringFromAny(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		if len(v) == 0 {
			return ""
		}
		return stringFromAny(v[0])
	case []string:
		if len(v) == 0 {
			return ""
		}
		return v[0]
	case fmt.Stringer:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func (s *Service) runAgentTaskAndWait(serverID string, taskType int, command string, timeout time.Duration) (string, error) {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return "", fmt.Errorf("agent offline")
	}

	task := s.taskRegistry.Create(serverID, fmt.Sprintf("docker.internal.%d", taskType), command)
	eventCh := task.Subscribe()

	err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    taskType,
		"data":    command,
		"timeout": int(timeout.Seconds()),
	})
	if err != nil {
		return "", err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return "", fmt.Errorf("task channel closed")
			}
			if event.Status == TaskCompleted {
				if str, ok := event.Data.(string); ok {
					return str, nil
				}
				return fmt.Sprintf("%v", event.Data), nil
			}
			if event.Status == TaskFailed {
				return "", fmt.Errorf("%s", event.Error)
			}
		case <-timer.C:
			return "", fmt.Errorf("task timeout")
		}
	}
}

func (s *Service) RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error) {
	return s.runAgentTaskAndWait(serverID, 1, command, timeout)
}
