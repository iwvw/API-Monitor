package openai

import (
	"context"
	"database/sql"
)

// subscriptionProxy 描述一个可从订阅节点导入到端点代理池的 socks/http 出口。
type subscriptionProxy struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Proxy    string `json:"proxy"`
	Location string `json:"location,omitempty"`
}

// sqlExec 抽象 ExecContext，供事务与直连共用。
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// resolveTargetEndpoint 仅在管理面板入口（/api/openai 前缀，会话鉴权）读取
// x-endpoint-id 强制指定端点，用于调试/聊天测试；外部统一出口（/v1）忽略该头，
// 保证模型池路由不外泄、外部无法锁定特定上游。
