package serveragent

import (
	"bytes"
	"database/sql"
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
		"data":      info,
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

	client, cleanup, err := s.openSFTPClient(r, db, serverID)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	defer cleanup()
	target := pathpkg.Join(remoteDir, header.Filename)
	if relativePath != "" {
		target = pathpkg.Join(remoteDir, relativePath)
	}
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
		"success":        true,
		"updateRequired": false,
		"containers":     results,
		"data":           results,
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
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: ready\ndata: {\"success\":true}\n\n"))
		return
	}

	response.Error(w, http.StatusNotFound, "v2 tasks route not found")
}

func (s *Service) handleV2DockerRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string) {
	if len(subparts) == 1 && subparts[0] == "overview" && r.Method == http.MethodGet {
		s.handleDockerOverview(w, r, db)
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

	servers := []map[string]interface{}{}
	for rows.Next() {
		var id, name, status, cachedInfo string
		if err := rows.Scan(&id, &name, &status, &cachedInfo); err != nil {
			continue
		}
		var info map[string]interface{}
		_ = json.Unmarshal([]byte(cachedInfo), &info)
		dockerInfo, _ := info["docker"].(map[string]interface{})
		installed := false
		if v, ok := dockerInfo["installed"].(bool); ok {
			installed = v
		}
		servers = append(servers, map[string]interface{}{
			"id":     id,
			"name":   name,
			"status": status,
			"docker": map[string]interface{}{
				"installed": installed,
				"running":   getInt(dockerInfo, "running"),
				"stopped":   getInt(dockerInfo, "stopped"),
			},
			"resources": map[string]interface{}{
				"containers":      []interface{}{},
				"images":          []interface{}{},
				"networks":        []interface{}{},
				"volumes":         []interface{}{},
				"stats":           []interface{}{},
				"composeProjects": []interface{}{},
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
	taskType := req.Type
	if taskType == "" {
		taskType = strings.Trim(strings.Join([]string{req.Domain, req.Action}, "."), ".")
	}
	if taskType == "" {
		taskType = "server.task"
	}

	task := s.taskRegistry.Create(req.ServerID, taskType, req.Action)
	if conn, ok := s.registry.Get(req.ServerID); ok {
		_ = conn.SendEvent("task:create", map[string]interface{}{
			"taskId":  task.ID,
			"domain":  req.Domain,
			"action":  req.Action,
			"type":    taskType,
			"config":  req.Config,
			"payload": req.Payload,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"taskId":  task.ID,
		"data": map[string]interface{}{
			"taskId": task.ID,
			"state":  string(task.Status),
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
