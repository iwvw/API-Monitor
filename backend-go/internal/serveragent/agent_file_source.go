package serveragent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// agentFileStreamChunkSize 是面板从 Agent 拉取文件的分块大小（与文件管理任务一致）。
const agentFileStreamChunkSize = 1024 * 1024

// AgentFileStream 把「按偏移分块读取 Agent 主机文件」适配为 io.ReadCloser，
// 供文件柜把主机文件转存到本地或远端存储节点。每次 Read 触发一次
// agentFileDownloadChunkTask（任务 38），返回的 base64 分块解码后写入调用方。
type AgentFileStream struct {
	ctx      context.Context
	service  *Service
	serverID string
	path     string
	size     int64
	offset   int64
	buffer   []byte
	closed   bool
}

// OpenAgentFile 打开 Agent 主机上的文件，返回可读流与文件大小。
func (s *Service) OpenAgentFile(ctx context.Context, serverID, remotePath string) (io.ReadCloser, int64, error) {
	size, exists, err := s.AgentFileExists(ctx, serverID, remotePath)
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, fmt.Errorf("agent file not found: %s", remotePath)
	}
	return &AgentFileStream{
		ctx:      ctx,
		service:  s,
		serverID: serverID,
		path:     normalizeRemotePath(remotePath),
		size:     size,
	}, size, nil
}

// AgentFileExists 通过文件 stat 任务确认主机文件存在并返回大小。
func (s *Service) AgentFileExists(ctx context.Context, serverID, remotePath string) (int64, bool, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return 0, false, fmt.Errorf("server id is required")
	}
	if !s.hasAgentConnection(serverID) {
		return 0, false, fmt.Errorf("agent offline")
	}
	result, err := s.runAgentTransientFileTask(serverID, agentFileStatTask, map[string]interface{}{
		"path": normalizeRemotePath(remotePath),
	}, agentFileTimeout)
	if err != nil {
		return 0, false, err
	}
	var parsed struct {
		IsFile bool  `json:"isFile"`
		IsDir  bool  `json:"isDirectory"`
		Size   int64 `json:"size"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return 0, false, fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	if parsed.IsDir || !parsed.IsFile {
		return 0, false, nil
	}
	return parsed.Size, true, nil
}

func (st *AgentFileStream) Read(p []byte) (int, error) {
	if st.closed {
		return 0, io.EOF
	}
	for len(st.buffer) == 0 {
		if st.size >= 0 && st.offset >= st.size {
			return 0, io.EOF
		}
		encoded, err := st.service.runAgentTransientFileTask(st.serverID, agentFileDownloadChunkTask, map[string]interface{}{
			"path":   st.path,
			"offset": st.offset,
			"size":   agentFileStreamChunkSize,
		}, agentFileTimeout)
		if err != nil {
			return 0, err
		}
		chunk, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return 0, fmt.Errorf("AGENT_FILE_ERROR: %w", err)
		}
		if len(chunk) == 0 {
			return 0, io.EOF
		}
		st.offset += int64(len(chunk))
		st.buffer = chunk
		if len(chunk) < agentFileStreamChunkSize {
			// 末块：下次 Read 时返回 EOF
			st.size = st.offset
		}
	}
	n := copy(p, st.buffer)
	st.buffer = st.buffer[n:]
	return n, nil
}

func (st *AgentFileStream) Close() error {
	st.closed = true
	st.buffer = nil
	return nil
}
