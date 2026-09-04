package filebox

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/serveragent"
)

// StorageNodeInfo 别名引入 serveragent.StorageNodeInfo
type StorageNodeInfo = serveragent.StorageNodeInfo

// NodeStorageProvider 抽象外部节点服务能力（供 filebox 调用 serveragent）
type NodeStorageProvider interface {
	ListEligibleStorageNodes(ctx context.Context) ([]serveragent.StorageNodeInfo, error)
	GetEligibleStorageNode(ctx context.Context, serverID string) (*serveragent.StorageNodeInfo, error)
	GetStorageNodeAgentKey(ctx context.Context, serverID string) (string, error)
}

// StorageBackend 文件柜存储后端抽象
type StorageBackend interface {
	Type() string
	Delete(ctx context.Context, entry *Entry) error
}

// LocalBackend 本地磁盘存储实现
type LocalBackend struct {
	uploadsDir string
}

func NewLocalBackend(uploadsDir string) *LocalBackend {
	return &LocalBackend{uploadsDir: uploadsDir}
}

func (b *LocalBackend) Type() string {
	return "local"
}

func (b *LocalBackend) Delete(ctx context.Context, entry *Entry) error {
	if entry.Path != nil && *entry.Path != "" && isPathInside(b.uploadsDir, *entry.Path) {
		_ = os.Remove(*entry.Path)
	}
	return nil
}

// RemoteBackend 远端节点存储实现
type RemoteBackend struct {
	nodeProvider NodeStorageProvider
	httpClient   *http.Client
}

func NewRemoteBackend(provider NodeStorageProvider) *RemoteBackend {
	return &RemoteBackend{
		nodeProvider: provider,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (b *RemoteBackend) Type() string {
	return "remote"
}

func (b *RemoteBackend) Delete(ctx context.Context, entry *Entry) error {
	if entry.ServerID == nil || *entry.ServerID == "" || b.nodeProvider == nil {
		return nil
	}
	serverID := *entry.ServerID
	node, err := b.nodeProvider.GetEligibleStorageNode(ctx, serverID)
	if err != nil {
		// 节点可能离线，静默忽略以允许主站元数据删除
		return nil
	}
	key, err := b.nodeProvider.GetStorageNodeAgentKey(ctx, serverID)
	if err != nil {
		return nil
	}

	deleteURL, err := BuildSignedURL("DELETE", node.Host, node.StoragePort, entry.Code, entry.Filename, 0, 5*time.Minute, key)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
