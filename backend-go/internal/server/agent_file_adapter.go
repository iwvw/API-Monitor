package server

import (
	"context"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/serveragent"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// agentFileOperatorAdapter 把 serveragent 的文件能力桥接给 system 包，
// 避免 system 反向依赖 serveragent（分层方向保持 server → 两者）。
type agentFileOperatorAdapter struct {
	svc *serveragent.Service
}

func (a agentFileOperatorAdapter) WriteAgentFileChunk(
	ctx context.Context, serverID, remotePath string, data []byte, offset int64, truncate bool, timeout time.Duration,
) (systemmetrics.AgentFileWriteResult, error) {
	result, err := a.svc.WriteAgentFileChunk(ctx, serverID, remotePath, data, offset, truncate, timeout)
	if err != nil {
		return systemmetrics.AgentFileWriteResult{}, err
	}
	return toSystemFileWriteResult(result), nil
}

func (a agentFileOperatorAdapter) WriteAgentFileChunkMode(
	ctx context.Context, serverID, remotePath string, data []byte, offset int64, truncate bool, timeout time.Duration, realPath bool,
) (systemmetrics.AgentFileWriteResult, error) {
	result, err := a.svc.WriteAgentFileChunkMode(ctx, serverID, remotePath, data, offset, truncate, timeout, realPath)
	if err != nil {
		return systemmetrics.AgentFileWriteResult{}, err
	}
	return toSystemFileWriteResult(result), nil
}

func (a agentFileOperatorAdapter) WriteAgentFileBytesDetailed(
	ctx context.Context, serverID, remotePath string, data []byte, timeout time.Duration,
) (systemmetrics.AgentFileWriteResult, error) {
	result, err := a.svc.WriteAgentFileBytesDetailed(ctx, serverID, remotePath, data, timeout)
	if err != nil {
		return systemmetrics.AgentFileWriteResult{}, err
	}
	return toSystemFileWriteResult(result), nil
}

func (a agentFileOperatorAdapter) WriteAgentFileBytesDetailedMode(
	ctx context.Context, serverID, remotePath string, data []byte, timeout time.Duration, realPath bool,
) (systemmetrics.AgentFileWriteResult, error) {
	result, err := a.svc.WriteAgentFileBytesDetailedMode(ctx, serverID, remotePath, data, timeout, realPath)
	if err != nil {
		return systemmetrics.AgentFileWriteResult{}, err
	}
	return toSystemFileWriteResult(result), nil
}

func (a agentFileOperatorAdapter) AgentFileRoots(ctx context.Context, serverID string) (map[string]interface{}, error) {
	return a.svc.AgentFileRoots(ctx, serverID)
}

func (a agentFileOperatorAdapter) AgentFileStat(ctx context.Context, serverID, remotePath string) (map[string]interface{}, error) {
	return a.svc.AgentFileStat(ctx, serverID, remotePath)
}

func (a agentFileOperatorAdapter) AgentFileChmod(ctx context.Context, serverID, remotePath string, mode uint32) (string, error) {
	return a.svc.AgentFileChmod(ctx, serverID, remotePath, mode)
}

func (a agentFileOperatorAdapter) CopyAgentFile(
	ctx context.Context, srcServerID, srcPath string, srcRealPath bool, dstServerID, dstPath string, dstRealPath bool, timeout time.Duration,
) (int64, error) {
	return a.svc.CopyAgentFile(ctx, srcServerID, srcPath, srcRealPath, dstServerID, dstPath, dstRealPath, timeout)
}

func (a agentFileOperatorAdapter) CopyAgentPath(
	ctx context.Context, srcServerID, srcPath string, srcRealPath bool, dstServerID, dstPath string, dstRealPath bool, timeout time.Duration,
) (int64, int64, error) {
	return a.svc.CopyAgentPath(ctx, srcServerID, srcPath, srcRealPath, dstServerID, dstPath, dstRealPath, timeout)
}

func (a agentFileOperatorAdapter) CopyAgentPathEx(
	ctx context.Context, srcServerID, srcPath, dstServerID, dstPath string, opts systemmetrics.AgentCopyOptions, timeout time.Duration,
) (int64, int64, error) {
	return a.svc.CopyAgentPathEx(ctx, srcServerID, srcPath, dstServerID, dstPath, serveragent.AgentCopyOptions{
		SourceRealPath: opts.SourceRealPath,
		TargetRealPath: opts.TargetRealPath,
		Backup:         opts.Backup,
		Verify:         opts.Verify,
	}, timeout)
}

func toSystemFileWriteResult(result serveragent.AgentFileWriteResult) systemmetrics.AgentFileWriteResult {
	return systemmetrics.AgentFileWriteResult{
		Size:     result.Size,
		RealPath: result.RealPath,
		Message:  result.Message,
	}
}

var _ systemmetrics.AgentFileOperator = agentFileOperatorAdapter{}
