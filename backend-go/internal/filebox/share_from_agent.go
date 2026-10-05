package filebox

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

const storageTargetAuto = "auto"

type shareFromAgentRequest struct {
	ServerID         string `json:"serverId"`
	RemotePath       string `json:"remotePath"`
	StorageTarget    string `json:"storageTarget"`
	Expiry           string `json:"expiry"`
	BurnAfterReading any    `json:"burn_after_reading"`
	MaxDownloads     string `json:"max_downloads"`
	AccessPassword   string `json:"access_password"`
	Password         string `json:"password"`
}

// shareFromAgent 把一台 Agent 主机上的文件转成文件柜分享，返回可下载链接。
// 存储目标支持 auto（优先可用存储节点，无则本地）/ local / 指定节点 serverId。
// Phase 1：字节经面板中转（面板读 Agent 文件后写入目标存储）。
func (s *Service) shareFromAgent(w http.ResponseWriter, r *http.Request) {
	var payload shareFromAgentRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.ServerID = strings.TrimSpace(payload.ServerID)
	payload.RemotePath = strings.TrimSpace(payload.RemotePath)
	if payload.ServerID == "" || payload.RemotePath == "" {
		response.Error(w, http.StatusBadRequest, "serverId and remotePath are required")
		return
	}
	if s.agentFiles == nil {
		response.Error(w, http.StatusServiceUnavailable, "agent file source not configured")
		return
	}

	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	size, exists, err := s.agentFiles.AgentFileExists(r.Context(), payload.ServerID, payload.RemotePath)
	if err != nil {
		response.Error(w, http.StatusBadGateway, fmt.Sprintf("failed to stat agent file: %v", err))
		return
	}
	if !exists {
		response.Error(w, http.StatusNotFound, "agent file not found or unreadable")
		return
	}
	if size <= 0 {
		response.Error(w, http.StatusBadRequest, "agent file is empty or unreadable")
		return
	}
	if settings.MaxFileSize > 0 && size > settings.MaxFileSize {
		response.Error(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file too large, max %d bytes", settings.MaxFileSize))
		return
	}

	filename := sanitizeFilename(filepath.Base(payload.RemotePath))
	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !isMIMEAllowed(mimeType, settings.AllowedMIMETypes) {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("file type not allowed: %s", mimeType))
		return
	}

	code, err := s.GenerateCode(r.Context(), defaultCodeLength)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	expiryHours := parseExpiryHours(payload.Expiry, settings.DefaultExpiryHours)
	burn := parseBool(payload.BurnAfterReading)
	maxDownloads := parseNonNegativeInt64(payload.MaxDownloads)
	accessPassword := payload.AccessPassword
	if accessPassword == "" {
		accessPassword = payload.Password
	}

	target, node, err := s.resolveStorageTarget(r.Context(), payload.StorageTarget)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	var storedPath *string
	var storedServerID *string
	var storedRemotePath *string
	if target == "remote" {
		// 优先让源主机 Agent 自己 PUT 到节点（面板零字节转发）；无上传器时退回面板中转。
		if s.agentUploader != nil {
			if err := s.uploadAgentFileDirectly(r.Context(), node, code, filename, size, payload.ServerID, payload.RemotePath); err != nil {
				response.Error(w, http.StatusBadGateway, err.Error())
				return
			}
		} else {
			stream, _, err := s.agentFiles.OpenAgentFile(r.Context(), payload.ServerID, payload.RemotePath)
			if err != nil {
				response.Error(w, http.StatusBadGateway, fmt.Sprintf("failed to open agent file: %v", err))
				return
			}
			defer stream.Close()
			if err := s.putAgentFileToNode(r.Context(), node, code, filename, size, stream); err != nil {
				response.Error(w, http.StatusBadGateway, err.Error())
				return
			}
		}
		storedServerID = &node.ID
		rp := fmt.Sprintf("shares/%s/%s", code, filename)
		storedRemotePath = &rp
	} else {
		stream, _, err := s.agentFiles.OpenAgentFile(r.Context(), payload.ServerID, payload.RemotePath)
		if err != nil {
			response.Error(w, http.StatusBadGateway, fmt.Sprintf("failed to open agent file: %v", err))
			return
		}
		defer stream.Close()
		if err := s.ensureDirs(); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		saveFilename := fmt.Sprintf("%d-%s-%s", time.Now().UnixMilli(), code, filename)
		finalPath := filepath.Join(s.uploadsDir, saveFilename)
		if !isPathInside(s.uploadsDir, finalPath) {
			response.Error(w, http.StatusInternalServerError, "invalid upload path")
			return
		}
		written, err := writeStreamWithLimit(finalPath, stream, size)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to save agent file: %v", err))
			return
		}
		size = written
		storedPath = &finalPath
	}

	now := time.Now().UnixMilli()
	expiry := expiryTime(now, expiryHours)
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		if storedPath != nil {
			_ = os.Remove(*storedPath)
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()
	_, err = db.ExecContext(r.Context(), `
		INSERT INTO filebox_entries (
			code, type, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash, storage_type, server_id, remote_path
		) VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, code, filename, filename, storedPath, nullString(mimeType), size, now, expiry,
		boolInt(burn), maxDownloads, passwordHash, target, storedServerID, storedRemotePath)
	if err != nil {
		if storedPath != nil {
			_ = os.Remove(*storedPath)
		}
		response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to create filebox share: %v", err))
		return
	}

	entry, err := s.GetEntry(r.Context(), code, true)
	if err != nil || entry == nil {
		response.Error(w, http.StatusInternalServerError, "failed to load created share")
		return
	}

	base := strings.TrimRight(s.loadPublicAPIURL(r.Context()), "/")
	if base == "" {
		base = requestOrigin(r)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"code":        entry.Code,
		"data":        publicEntry(entry),
		"shareUrl":    base + "/share/" + entry.Code,
		"directUrl":   base + "/api/filebox/d/" + entry.Code,
		"storageType": entry.StorageType,
		"serverId":    entry.ServerID,
	})
}

// resolveStorageTarget 解析存储目标：auto 优先可用存储节点，无则本地。
func (s *Service) resolveStorageTarget(ctx context.Context, requested string) (string, *StorageNodeInfo, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "", storageTargetAuto:
		if s.nodeProvider != nil {
			nodes, err := s.nodeProvider.ListEligibleStorageNodes(ctx)
			if err == nil && len(nodes) > 0 {
				node := nodes[0]
				return "remote", &node, nil
			}
		}
		return "local", nil, nil
	case "local":
		return "local", nil, nil
	default:
		if s.nodeProvider == nil {
			return "", nil, fmt.Errorf("storage node provider not configured")
		}
		node, err := s.nodeProvider.GetEligibleStorageNode(ctx, requested)
		if err != nil {
			return "", nil, fmt.Errorf("storage target not eligible: %v", err)
		}
		return "remote", node, nil
	}
}

// uploadAgentFileDirectly 让源主机 Agent 自己 PUT 到节点签名 URL，面板零字节转发。
func (s *Service) uploadAgentFileDirectly(ctx context.Context, node *StorageNodeInfo, code, filename string, size int64, sourceServerID, remotePath string) error {
	if s.agentUploader == nil {
		return fmt.Errorf("agent direct uploader not configured")
	}
	key, err := s.nodeProvider.GetStorageNodeAgentKey(ctx, node.ID)
	if err != nil {
		return fmt.Errorf("failed to resolve node credential: %v", err)
	}
	putURL, err := BuildSignedURL("PUT", node.Host, node.StoragePort, code, filename, size, 15*time.Minute, key)
	if err != nil {
		return fmt.Errorf("failed to build upload URL: %v", err)
	}
	return s.agentUploader.UploadAgentFileToURL(ctx, sourceServerID, remotePath, putURL, 30*time.Minute)
}

func (s *Service) putAgentFileToNode(ctx context.Context, node *StorageNodeInfo, code, filename string, size int64, stream io.Reader) error {
	key, err := s.nodeProvider.GetStorageNodeAgentKey(ctx, node.ID)
	if err != nil {
		return fmt.Errorf("failed to resolve node credential: %v", err)
	}
	putURL, err := BuildSignedURL("PUT", node.Host, node.StoragePort, code, filename, size, 15*time.Minute, key)
	if err != nil {
		return fmt.Errorf("failed to build upload URL: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, stream)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload to storage node: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("storage node rejected upload (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// writeStreamWithLimit 把流写入目标文件，最多写入 limit 字节（超出即报错并删除）。
func writeStreamWithLimit(target string, stream io.Reader, limit int64) (int64, error) {
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(dst, io.LimitReader(stream, limit))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return 0, closeErr
	}
	if written <= 0 {
		_ = os.Remove(target)
		return 0, fmt.Errorf("empty file stream")
	}
	return written, nil
}
