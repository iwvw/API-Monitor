package serveragent

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// agentUploadMaxBytes 限制二进制直传请求体大小，避免单请求耗尽磁盘。
const agentUploadMaxBytes = 2 << 30

// SetAIAgentValidator 注入 AI 接入密钥校验器，供 Agent Key 直传端点复用。
// 由 server 装配层传入 system 服务的校验实现，避免 serveragent 反向依赖 system。
func (s *Service) SetAIAgentValidator(validator func(r *http.Request) bool) {
	s.aiAgentValidator = validator
}

func (s *Service) authorizeAIAgent(r *http.Request) bool {
	if s.aiAgentValidator == nil {
		return false
	}
	return s.aiAgentValidator(r)
}

// handleAgentFetchURL 让目标主机从 URL 拉取文件写入本地路径（字节不经面板）。
func (s *Service) handleAgentFetchURL(w http.ResponseWriter, r *http.Request, _ *sql.DB) {
	var req struct {
		ServerID string `json:"serverId"`
		URL      string `json:"url"`
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ServerID) == "" || strings.TrimSpace(req.URL) == "" || strings.TrimSpace(req.Path) == "" {
		response.Error(w, http.StatusBadRequest, "serverId, url and path are required")
		return
	}
	message, err := s.FetchURLToAgentFile(r.Context(), req.ServerID, req.URL, req.Path, 10*time.Minute)
	if err != nil {
		writeSFTPError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"message":   firstNonEmpty(message, "fetched"),
		"path":      normalizeRemotePath(req.Path),
	})
}

// handleAgentUpload 提供带 AI Agent Key 鉴权的二进制文件直传端点。
// 请求体为 multipart/form-data：字段 serverId、path 与 file（原始字节）。
// 面板读取字节后按 base64 分块下发给主机 Agent 落盘，绕开文本编码与单帧上限。
func (s *Service) handleAgentUpload(w http.ResponseWriter, r *http.Request, _ *sql.DB) {
	if !s.authorizeAIAgent(r) {
		response.Error(w, http.StatusUnauthorized, "invalid agent key")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, agentUploadMaxBytes)
	if err := r.ParseMultipartForm(agentFileChunkSize); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	serverID := strings.TrimSpace(r.FormValue("serverId"))
	remotePath := strings.TrimSpace(r.FormValue("path"))
	if serverID == "" || remotePath == "" {
		response.Error(w, http.StatusBadRequest, "serverId and path are required")
		return
	}
	if !s.hasAgentConnection(serverID) {
		writeSFTPError(w, fmt.Errorf("CONNECTION_FAILED: agent offline"))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	ctx := r.Context()
	offset := int64(0)
	buf := make([]byte, agentFileChunkSize)
	for {
		n, readErr := io.ReadFull(file, buf)
		if n > 0 {
			encoded := base64.StdEncoding.EncodeToString(buf[:n])
			truncate := offset == 0
			if _, werr := s.WriteAgentFileBase64(ctx, serverID, remotePath, encoded, offset, truncate, 5*time.Minute); werr != nil {
				writeSFTPError(w, werr)
				return
			}
			offset += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			response.Error(w, http.StatusBadRequest, "read upload stream failed")
			return
		}
	}
	// 空文件也要落盘：首块未写入时补一次截断写入，确保目标被创建/清空。
	if offset == 0 {
		if _, werr := s.WriteAgentFileBase64(ctx, serverID, remotePath, "", 0, true, agentFileTimeout); werr != nil {
			writeSFTPError(w, werr)
			return
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"transport": "agent",
		"size":      offset,
		"message":   "uploaded",
		"path":      normalizeRemotePath(remotePath),
	})
}
