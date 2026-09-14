package serveragent

import (
	"bytes"
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

func (s *Service) handleAgentFileList(w http.ResponseWriter, serverID, remotePath string) {
	result, err := s.runAgentTransientFileTask(serverID, agentFileListTask, map[string]interface{}{
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
	content, err := s.runAgentTransientFileTask(serverID, agentFileReadTask, map[string]interface{}{
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
		encoded, err := s.runAgentTransientFileTask(serverID, agentFileDownloadChunkTask, map[string]interface{}{
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
