// Package mihomo 提供 Mihomo 代理桥的管理接口：状态查看、开关与端口
// 设置、机场订阅管理、节点列表，以及“账号 ↔ 节点”绑定。
package mihomo

import (
	"context"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	adminshared "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/admin/shared"
)

// Bridge 抽象 mihomo.Manager，便于测试替换。
type Bridge interface {
	Supported() bool
	Status() map[string]any
	Apply(ctx context.Context) error
	BindAccount(ctx context.Context, identifier, nodeKey string) error
	AddSubscription(ctx context.Context, name, rawURL string) (config.MihomoSubscription, error)
	RefreshSubscription(ctx context.Context, subID string) (int, error)
	DeleteSubscription(ctx context.Context, subID string) error
	UpdateSettings(ctx context.Context, enabled bool, binaryPath string, basePort, apiPort int, autoBind bool) error
	ListNodes() []map[string]any
	TestLatency(ctx context.Context) ([]map[string]any, error)
	AssignAccounts(ctx context.Context, nodeKeys []string) (int, error)
	DownloadInfo() map[string]any
	StartBinaryDownload(ctx context.Context) error
}

type Handler struct {
	Store  adminshared.ConfigStore
	Pool   adminshared.PoolController
	Bridge Bridge
}

var writeJSON = adminshared.WriteJSON

func fieldString(m map[string]any, key string) string {
	return adminshared.FieldString(m, key)
}

func fieldInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func fieldBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}
