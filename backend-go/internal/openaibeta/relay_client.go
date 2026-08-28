package openaibeta

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"time"

	enginevertex "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/vertex"
)

// relayClient 封装引擎 VertexAIClient，并复用模型网关端点代理池：
// 设置里指定 ProxyEndpointID 后，每次中继按 openai_endpoints.proxy_pool 轮询
// 出口代理，且尊重 openai_proxy_state 的冷却/禁用状态（与网关共享健康数据）。
type relayClient struct {
	vc *enginevertex.VertexAIClient

	// proxyMu 保护代理池轮询游标与池内索引。
	cursor uint64
}

func newRelayClient(p settingsProvider) *relayClient {
	return &relayClient{
		vc: enginevertex.NewVertexAIClient(p),
	}
}

// pickProxy 读取指定网关端点的出口代理池，过滤冷却/禁用后按下标轮询返回。
// proxyEndpointID 为空或池为空/全禁用时返回 ""（直连）。
func (c *relayClient) pickProxy(ctx context.Context, db *sql.DB, proxyEndpointID string) (string, error) {
	if proxyEndpointID == "" {
		return "", nil
	}
	pool, err := c.loadProxyPool(ctx, db, proxyEndpointID)
	if err != nil || len(pool) == 0 {
		return "", nil
	}
	now := time.Now()
	blocked := map[string]bool{}
	rows, err := db.QueryContext(ctx, `
		SELECT proxy, kind, until FROM openai_proxy_state
		WHERE endpoint_id = ? AND until > ?`, proxyEndpointID, now.UTC().Format(time.RFC3339))
	if err != nil {
		// 读不到状态表不影响直连兜底。
		rows = nil
	}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var proxy, kind, untilRaw string
			if err := rows.Scan(&proxy, &kind, &untilRaw); err != nil {
				continue
			}
			if kind == "cool" || kind == "sunk" || kind == "429" {
				blocked[proxy] = true
			}
		}
	}
	available := make([]string, 0, len(pool))
	for _, p := range pool {
		if p != "" && !blocked[p] {
			available = append(available, p)
		}
	}
	if len(available) == 0 {
		return "", nil
	}
	idx := atomic.AddUint64(&c.cursor, 1) - 1
	return available[idx%uint64(len(available))], nil
}

// loadProxyPool 读取 openai_endpoints.proxy_pool（JSON 字符串数组）。
func (c *relayClient) loadProxyPool(ctx context.Context, db *sql.DB, endpointID string) ([]string, error) {
	var raw sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT proxy_pool FROM openai_endpoints WHERE id = ?`, endpointID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var pool []string
	if err := json.Unmarshal([]byte(raw.String), &pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// rebuildClient 重建引擎客户端（设置变更后生效）。不在锁内构造：
// 构造会回调 provider 读 Settings()，持写锁会自锁。
func (s *Service) rebuildClient() {
	c := newRelayClient(settingsProvider{s: s})
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
}

// relay 使用显式代理出口执行单候选完成（非流式）。
func (s *Service) completeChat(ctx context.Context, db *sql.DB, model string, payload map[string]any, proxyURI string) (map[string]any, error) {
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c == nil {
		return nil, enginevertex.NewInternalError("Beta 插件客户端未初始化")
	}
	if proxyURI == "" {
		proxyURI, _ = c.pickProxy(ctx, db, s.Settings().ProxyEndpointID)
	}
	return c.vc.CompleteChatViaProxy(ctx, model, payload, proxyURI)
}

// streamChat 使用显式代理出口执行单候选流式。
func (s *Service) streamChat(ctx context.Context, db *sql.DB, model string, payload map[string]any, proxyURI string, yield func(enginevertex.StreamChunk) bool) {
	s.mu.RLock()
	c := s.client
	s.mu.RUnlock()
	if c == nil {
		yield(enginevertex.StreamChunk{Err: enginevertex.NewInternalError("Beta 插件客户端未初始化")})
		return
	}
	if proxyURI == "" {
		proxyURI, _ = c.pickProxy(ctx, db, s.Settings().ProxyEndpointID)
	}
	c.vc.StreamChatViaProxy(ctx, model, payload, proxyURI, yield)
}
