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
	Name         string `json:"name"`
	Path         string `json:"path"`
	RealPath     string `json:"realPath"`
	RelativePath string `json:"relativePath"`
	IsDirectory  bool   `json:"isDirectory"`
	IsFile       bool   `json:"isFile"`
	IsSymlink    bool   `json:"isSymlink"`
	Size         int64  `json:"size"`
}

// walkAgentPath 用 Agent 的 FILE_WALK 任务(44)一次性递归列出目录树(相对路径)。
// 需 Agent >= 0.6.57;旧版 Agent 会返回未知任务错误,调用方可回退逐层 list。
func (s *Service) walkAgentPath(ctx context.Context, serverID, path string, realPath bool) ([]agentFileEntry, error) {
	payload := map[string]interface{}{"path": normalizeRemotePath(path)}
	if realPath {
		payload["realPath"] = true
	}
	raw, err := s.runAgentTransientFileTask(serverID, agentFileWalkTask, payload, agentFileTimeout)
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

// AgentCopyOptions 控制主机↔主机复制的路径语义与原子性。
type AgentCopyOptions struct {
	SourceRealPath bool
	TargetRealPath bool
	Backup         bool
	Verify         bool
}

// CopyAgentPath 复制文件或整个目录(默认选项),等价于 CopyAgentPathEx。
func (s *Service) CopyAgentPath(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath bool,
	timeout time.Duration,
) (int64, int64, error) {
	return s.CopyAgentPathEx(ctx, srcServerID, srcPath, dstServerID, dstPath,
		AgentCopyOptions{SourceRealPath: srcRealPath, TargetRealPath: dstRealPath}, timeout)
}

// CopyAgentPathEx 复制文件或目录(目录自动递归):逐文件原子写(临时文件+rename),
// 可选备份与 sha256 校验。返回 (文件数, 总字节数)。
func (s *Service) CopyAgentPathEx(
	ctx context.Context,
	srcServerID, srcPath, dstServerID, dstPath string,
	opts AgentCopyOptions,
	timeout time.Duration,
) (int64, int64, error) {
	srcServerID = strings.TrimSpace(srcServerID)
	dstServerID = strings.TrimSpace(dstServerID)
	srcPath = strings.TrimSpace(srcPath)
	dstPath = strings.TrimSpace(dstPath)
	if srcServerID == "" || dstServerID == "" || srcPath == "" || dstPath == "" {
		return 0, 0, fmt.Errorf("源/目标 serverId 与路径均为必填")
	}
	root, err := s.statAgentPath(ctx, srcServerID, srcPath, opts.SourceRealPath)
	if err != nil {
		return 0, 0, fmt.Errorf("读取源路径失败: %w", err)
	}
	if !root.IsDirectory {
		n, _, err := s.CopyAgentFileAtomic(ctx, srcServerID, srcPath, opts.SourceRealPath, dstServerID, dstPath, opts.TargetRealPath, opts.Backup, opts.Verify, timeout)
		return 1, n, err
	}
	var files, totalBytes int64
	if err := s.makeAgentDir(ctx, dstServerID, dstPath, opts.TargetRealPath); err != nil {
		return 0, 0, fmt.Errorf("创建目标目录失败: %w", err)
	}
	entries, walkErr := s.walkAgentPath(ctx, srcServerID, srcPath, opts.SourceRealPath)
	if walkErr != nil {
		entries, walkErr = s.walkAgentPathFallback(ctx, srcServerID, srcPath, opts.SourceRealPath)
		if walkErr != nil {
			return 0, 0, walkErr
		}
	}
	for _, entry := range entries {
		if !entry.IsDirectory {
			continue
		}
		if err := s.makeAgentDir(ctx, dstServerID, joinPosixPath(dstPath, entry.RelativePath), opts.TargetRealPath); err != nil {
			return files, totalBytes, fmt.Errorf("创建目标目录 %s 失败: %w", joinPosixPath(dstPath, entry.RelativePath), err)
		}
	}
	for _, entry := range entries {
		if entry.IsDirectory || !entry.IsFile {
			continue
		}
		written, _, err := s.CopyAgentFileAtomic(ctx,
			srcServerID, joinPosixPath(srcPath, entry.RelativePath), opts.SourceRealPath,
			dstServerID, joinPosixPath(dstPath, entry.RelativePath), opts.TargetRealPath, opts.Backup, opts.Verify, timeout)
		if err != nil {
			return files, totalBytes, err
		}
		files++
		totalBytes += written
	}
	return files, totalBytes, nil
}

// walkAgentPathFallback 在 Agent 不支持 FILE_WALK 时,用逐层 list 拼出等价的递归条目。
func (s *Service) walkAgentPathFallback(ctx context.Context, serverID, path string, realPath bool) ([]agentFileEntry, error) {
	var out []agentFileEntry
	queue := []string{""}
	for len(queue) > 0 {
		rel := queue[0]
		queue = queue[1:]
		entries, err := s.listAgentDir(ctx, serverID, joinPosixPath(path, rel), realPath)
		if err != nil {
			return nil, fmt.Errorf("读取源目录 %s 失败: %w", joinPosixPath(path, rel), err)
		}
		for _, e := range entries {
			childRel := joinPosixPath(rel, e.Name)
			e.RelativePath = childRel
			out = append(out, e)
			if e.IsDirectory {
				queue = append(queue, childRel)
			}
		}
	}
	return out, nil
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
