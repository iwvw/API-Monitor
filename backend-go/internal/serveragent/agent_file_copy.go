package serveragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
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

// agentFileCopyConcurrency 是主机↔主机复制时同时在途的目标写入数。
//
// 每块写入是一次跨公网的 Agent 任务往返;串行时吞吐被往返延迟限制(实测 1 MiB 块约
// 0.75s,90% 是延迟)。目标写入按显式 offset 落位,Agent 侧每次 open+seek+write 用独立
// 句柄,乱序写不同 offset 互不覆盖,因此可以并发。首块截断由调用方建文件时完成,
// worker 一律 truncate=false,避免并发 truncate 竞态。
const agentFileCopyConcurrency = 4

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

	// 先建/截断目标(offset=0 的 nil 写入),之后所有分块写入均 truncate=false。
	// 这一步同时覆盖 0 字节文件与「避免并发 worker 各自截断」的竞态。
	if _, werr := s.WriteAgentFileChunkMode(ctx, dstServerID, dstPath, nil, 0, true, timeout, dstRealPath); werr != nil {
		return 0, "", fmt.Errorf("创建目标文件失败: %w", werr)
	}

	return copyChunksConcurrently(ctx, reader, agentFileStreamChunkSize, agentFileCopyConcurrency,
		func(workerCtx context.Context, data []byte, offset int64) error {
			_, werr := s.WriteAgentFileChunkMode(workerCtx, dstServerID, dstPath, data, offset, false, timeout, dstRealPath)
			return werr
		})
}

// copyChunksConcurrently 从 r 顺序读块(顺序计算 sha256),交给并发的 write 回调按显式
// offset 落位;返回 (字节数, 源 sha256)。write 必须按 offset 无截断写入(调用方已建好文件),
// 且必须并发安全。任一 write 失败即取消其余在途任务并返回该错误。
func copyChunksConcurrently(
	ctx context.Context,
	r io.Reader,
	chunkSize, concurrency int,
	write func(ctx context.Context, data []byte, offset int64) error,
) (int64, string, error) {
	if chunkSize <= 0 {
		chunkSize = agentFileStreamChunkSize
	}
	if concurrency < 1 {
		concurrency = 1
	}

	hasher := sha256.New()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		offset int64
		data   []byte
	}
	jobs := make(chan job, concurrency)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := write(workerCtx, j.data, j.offset); err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}

	var offset int64
	var readErr error
readLoop:
	for {
		buf := make([]byte, chunkSize)
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			hasher.Write(buf[:n])
			select {
			case jobs <- job{offset: offset, data: buf[:n]}:
				offset += int64(n)
			case werr := <-errCh:
				readErr = werr
				break readLoop
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			readErr = err
			break
		}
	}
	close(jobs)
	wg.Wait()

	if readErr != nil {
		return offset, "", fmt.Errorf("读取源文件失败(已写 %d 字节): %w", offset, readErr)
	}
	select {
	case werr := <-errCh:
		return offset, "", fmt.Errorf("写入目标文件失败(已读 %d 字节): %w", offset, werr)
	default:
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
