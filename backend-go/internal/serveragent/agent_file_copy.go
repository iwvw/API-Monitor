package serveragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// CopyAgentFile 由面板中继,把源主机上的文件流式复制到目标主机(主机↔主机)。
// 直接写目标路径;需要原子/备份/校验请用 CopyAgentFileAtomic。
func (s *Service) CopyAgentFile(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath bool,
	timeout time.Duration,
) (int64, error) {
	written, _, err := s.copyAgentStream(ctx, srcServerID, srcPath, srcRealPath, dstServerID, dstPath, dstRealPath, timeout)
	return written, err
}

// copyAgentStream 流式把源文件写到目标路径(目标为最终路径),返回 (字节, sha256)。
func (s *Service) copyAgentStream(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath bool,
	timeout time.Duration,
) (int64, string, error) {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	reader, _, err := s.OpenAgentFileMode(ctx, srcServerID, srcPath, srcRealPath)
	if err != nil {
		return 0, "", fmt.Errorf("打开源文件失败: %w", err)
	}
	defer reader.Close()

	hasher := sha256.New()
	buf := make([]byte, agentFileStreamChunkSize)
	var offset int64
	for {
		n, readErr := io.ReadFull(reader, buf)
		if n > 0 {
			hasher.Write(buf[:n])
			if _, werr := s.WriteAgentFileChunkMode(ctx, dstServerID, dstPath, buf[:n], offset, offset == 0, timeout, dstRealPath); werr != nil {
				return offset, "", fmt.Errorf("写入目标文件失败(已写 %d 字节): %w", offset, werr)
			}
			offset += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return offset, "", fmt.Errorf("读取源文件失败(已写 %d 字节): %w", offset, readErr)
		}
	}
	if offset == 0 {
		if _, werr := s.WriteAgentFileChunkMode(ctx, dstServerID, dstPath, nil, 0, true, timeout, dstRealPath); werr != nil {
			return 0, "", fmt.Errorf("创建目标文件失败: %w", werr)
		}
	}
	return offset, hex.EncodeToString(hasher.Sum(nil)), nil
}

// CopyAgentFileAtomic 原子复制单个文件:先写 <dst>.apitmp.<nano>,可选 sha256 校验,
// 可选把已存在的目标备份为 <dst>.bak-<unix>,最后 rename 落位。返回 (字节, sha256)。
func (s *Service) CopyAgentFileAtomic(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath, backup, verify bool,
	timeout time.Duration,
) (int64, string, error) {
	if strings.TrimSpace(srcServerID) == "" || strings.TrimSpace(dstServerID) == "" ||
		strings.TrimSpace(srcPath) == "" || strings.TrimSpace(dstPath) == "" {
		return 0, "", fmt.Errorf("源/目标 serverId 与路径均为必填")
	}
	if !s.hasAgentConnection(srcServerID) {
		return 0, "", fmt.Errorf("源主机 Agent 离线")
	}
	if !s.hasAgentConnection(dstServerID) {
		return 0, "", fmt.Errorf("目标主机 Agent 离线")
	}

	tmpPath := fmt.Sprintf("%s.apitmp.%d", strings.TrimRight(dstPath, "/"), time.Now().UnixNano())
	cleanupTmp := func() { _ = s.RemoveAgentPath(dstServerID, tmpPath, false, dstRealPath) }

	written, sum, err := s.copyAgentStream(ctx, srcServerID, srcPath, srcRealPath, dstServerID, tmpPath, dstRealPath, timeout)
	if err != nil {
		cleanupTmp()
		return written, "", err
	}
	if verify {
		got, verr := s.hashAgentFile(ctx, dstServerID, tmpPath, dstRealPath)
		if verr != nil {
			cleanupTmp()
			return written, sum, fmt.Errorf("校验临时文件失败: %w", verr)
		}
		if got != sum {
			cleanupTmp()
			return written, sum, fmt.Errorf("sha256 校验失败: 源 %s, 目标 %s", sum, got)
		}
	}
	if backup {
		if _, exists, _ := s.AgentFileExistsMode(ctx, dstServerID, dstPath, dstRealPath); exists {
			bakPath := fmt.Sprintf("%s.bak-%d", strings.TrimRight(dstPath, "/"), time.Now().Unix())
			if rerr := s.RenameAgentPath(dstServerID, dstPath, bakPath, dstRealPath); rerr != nil {
				cleanupTmp()
				return written, sum, fmt.Errorf("备份目标文件失败: %w", rerr)
			}
		}
	}
	if rerr := s.RenameAgentPath(dstServerID, tmpPath, dstPath, dstRealPath); rerr != nil {
		cleanupTmp()
		return written, sum, fmt.Errorf("临时文件落位失败: %w", rerr)
	}
	return written, sum, nil
}

// RenameAgentPath 重命名/移动主机上的路径(任务 35)。
func (s *Service) RenameAgentPath(serverID, oldPath, newPath string, realPath bool) error {
	payload := map[string]interface{}{
		"oldPath": normalizeRemotePath(oldPath),
		"newPath": normalizeRemotePath(newPath),
	}
	if realPath {
		payload["realPath"] = true
	}
	_, err := s.runAgentTransientFileTask(serverID, agentFileRenameTask, payload, agentFileTimeout)
	return err
}

// RemoveAgentPath 删除主机上的路径(任务 34);recursive 用于目录。
func (s *Service) RemoveAgentPath(serverID, path string, recursive, realPath bool) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	payload := map[string]interface{}{
		"path":      normalizeRemotePath(path),
		"recursive": recursive,
	}
	if realPath {
		payload["realPath"] = true
	}
	_, err := s.runAgentTransientFileTask(serverID, agentFileDeleteTask, payload, agentFileTimeout)
	return err
}

// hashAgentFile 读取主机文件并计算 sha256(用于原子写的校验)。
func (s *Service) hashAgentFile(ctx context.Context, serverID, path string, realPath bool) (string, error) {
	reader, _, err := s.OpenAgentFileMode(ctx, serverID, path, realPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	h := sha256.New()
	if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
