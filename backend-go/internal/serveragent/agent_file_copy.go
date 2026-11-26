package serveragent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// CopyAgentFile 由面板中继,把源主机上的文件流式复制到目标主机(主机↔主机)。
//
// 适用面最广的通道:两端只需能主动连到面板(Agent 外拨 WS 即可),
// 因此对「没有公网 IP / NAT 后」乃至 Windows↔Linux / Windows↔Windows 都成立;
// 代价是字节经面板中转(源端 1MiB 分块读出、目标端 1MiB 分块写入,内存恒定)。
// 源/目标的 realPath 分别控制是否按主机真实绝对路径读写(须在 API_MONITOR_FILE_ROOTS 白名单内)。
func (s *Service) CopyAgentFile(
	ctx context.Context,
	srcServerID, srcPath string, srcRealPath bool,
	dstServerID, dstPath string, dstRealPath bool,
	timeout time.Duration,
) (int64, error) {
	srcServerID = strings.TrimSpace(srcServerID)
	dstServerID = strings.TrimSpace(dstServerID)
	if srcServerID == "" || dstServerID == "" {
		return 0, fmt.Errorf("源/目标 serverId 均为必填")
	}
	if strings.TrimSpace(srcPath) == "" || strings.TrimSpace(dstPath) == "" {
		return 0, fmt.Errorf("源/目标路径均为必填")
	}
	if !s.hasAgentConnection(srcServerID) {
		return 0, fmt.Errorf("源主机 Agent 离线")
	}
	if !s.hasAgentConnection(dstServerID) {
		return 0, fmt.Errorf("目标主机 Agent 离线")
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	reader, _, err := s.OpenAgentFileMode(ctx, srcServerID, srcPath, srcRealPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer reader.Close()

	buf := make([]byte, agentFileStreamChunkSize)
	var offset int64
	for {
		n, readErr := io.ReadFull(reader, buf)
		if n > 0 {
			written, writeErr := s.WriteAgentFileChunkMode(ctx, dstServerID, dstPath, buf[:n], offset, offset == 0, timeout, dstRealPath)
			if writeErr != nil {
				return offset, fmt.Errorf("写入目标文件失败(已写 %d 字节): %w", offset, writeErr)
			}
			_ = written
			offset += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return offset, fmt.Errorf("读取源文件失败(已写 %d 字节): %w", offset, readErr)
		}
	}
	// 空文件:确保目标被创建/清空。
	if offset == 0 {
		if _, werr := s.WriteAgentFileChunkMode(ctx, dstServerID, dstPath, nil, 0, true, timeout, dstRealPath); werr != nil {
			return 0, fmt.Errorf("创建目标文件失败: %w", werr)
		}
	}
	return offset, nil
}
