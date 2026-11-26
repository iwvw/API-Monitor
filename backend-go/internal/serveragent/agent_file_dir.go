package serveragent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// agentFileEntry 对应 Agent 文件列表/stat 返回的单条目(FileEntry,序列化为 camelCase)。
type agentFileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	RealPath    string `json:"realPath"`
	IsDirectory bool   `json:"isDirectory"`
	IsFile      bool   `json:"isFile"`
	IsSymlink   bool   `json:"isSymlink"`
	Size        int64  `json:"size"`
}

func (s *Service) listAgentDir(ctx context.Context, serverID, path string, realPath bool) ([]agentFileEntry, error) {
	payload := map[string]interface{}{"path": normalizeRemotePath(path)}
	if realPath {
		payload["realPath"] = true
	}
	raw, err := s.runAgentTransientFileTask(serverID, agentFileListTask, payload, agentFileTimeout)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Files []agentFileEntry `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	return parsed.Files, nil
}

func (s *Service) statAgentPath(ctx context.Context, serverID, path string, realPath bool) (agentFileEntry, error) {
	payload := map[string]interface{}{"path": normalizeRemotePath(path)}
	if realPath {
		payload["realPath"] = true
	}
	raw, err := s.runAgentTransientFileTask(serverID, agentFileStatTask, payload, agentFileTimeout)
	if err != nil {
		return agentFileEntry{}, err
	}
	var entry agentFileEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return agentFileEntry{}, fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	return entry, nil
}

func (s *Service) makeAgentDir(ctx context.Context, serverID, path string, realPath bool) error {
	payload := map[string]interface{}{"path": normalizeRemotePath(path)}
	if realPath {
		payload["realPath"] = true
	}
	_, err := s.runAgentTransientFileTask(serverID, agentFileMkdirTask, payload, agentFileTimeout)
	return err
}

// CopyAgentPath 复制文件或整个目录(目录自动递归展开):先建目录树,再逐文件搬运。
// 单层只支持文件/目录本身;目录用 task30 列目录 + task33 建目录 + 逐文件面板中继。
// 返回 (文件数, 总字节数)。
func (s *Service) CopyAgentPath(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath bool,
	timeout time.Duration,
) (int64, int64, error) {
	srcServerID = strings.TrimSpace(srcServerID)
	dstServerID = strings.TrimSpace(dstServerID)
	srcPath = strings.TrimSpace(srcPath)
	dstPath = strings.TrimSpace(dstPath)
	if srcServerID == "" || dstServerID == "" || srcPath == "" || dstPath == "" {
		return 0, 0, fmt.Errorf("源/目标 serverId 与路径均为必填")
	}
	root, err := s.statAgentPath(ctx, srcServerID, srcPath, srcRealPath)
	if err != nil {
		return 0, 0, fmt.Errorf("读取源路径失败: %w", err)
	}
	if !root.IsDirectory {
		n, err := s.CopyAgentFile(ctx, srcServerID, srcPath, srcRealPath, dstServerID, dstPath, dstRealPath, timeout)
		return 1, n, err
	}

	// 目录:建目标根,再 BFS。paths 一律用 "/"(Agent 已把反斜杠归一化)。
	var files, totalBytes int64
	if err := s.makeAgentDir(ctx, dstServerID, dstPath, dstRealPath); err != nil {
		return 0, 0, fmt.Errorf("创建目标目录失败: %w", err)
	}
	queue := []string{""}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		entries, err := s.listAgentDir(ctx, srcServerID, joinPosixPath(srcPath, rel), srcRealPath)
		if err != nil {
			return files, totalBytes, fmt.Errorf("读取源目录 %s 失败: %w", joinPosixPath(srcPath, rel), err)
		}
		for _, entry := range entries {
			childRel := joinPosixPath(rel, entry.Name)
			if entry.IsDirectory {
				if err := s.makeAgentDir(ctx, dstServerID, joinPosixPath(dstPath, childRel), dstRealPath); err != nil {
					return files, totalBytes, fmt.Errorf("创建目标目录 %s 失败: %w", joinPosixPath(dstPath, childRel), err)
				}
				queue = append(queue, childRel)
				continue
			}
			if !entry.IsFile {
				// 跳过符号链接/特殊文件,避免复制到目标后悬空。
				continue
			}
			written, err := s.CopyAgentFile(ctx,
				srcServerID, joinPosixPath(srcPath, childRel), srcRealPath,
				dstServerID, joinPosixPath(dstPath, childRel), dstRealPath, timeout)
			if err != nil {
				return files, totalBytes, err
			}
			files++
			totalBytes += written
		}
	}
	return files, totalBytes, nil
}

// joinPosixPath 以 "/" 拼接路径(空段跳过),与 Agent 归一化后的路径风格一致。
func joinPosixPath(base, name string) string {
	base = strings.TrimRight(base, "/")
	if name == "" {
		return base
	}
	if base == "" {
		return name
	}
	return base + "/" + name
}
