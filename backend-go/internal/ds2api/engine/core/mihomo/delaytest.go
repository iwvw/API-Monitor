package mihomo

import (
	"context"
	"errors"
	"sort"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// TestLatency 通过 mihomo external-controller 批量测试全部订阅节点的延迟
// （Clash 式：经节点隧道请求探测 URL 计时）。并发上限与自动巡检一致，
// 结果写回内存健康池（供一键分配与自动故障转移使用）。
// 返回结果：成功项按延迟升序，失败项排在最后。
func (m *Manager) TestLatency(ctx context.Context) ([]map[string]any, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("mihomo manager 未初始化")
	}
	cfg := m.store.Snapshot()
	m.mu.Lock()
	running := m.running
	apiPort := cfg.Mihomo.APIPort
	m.mu.Unlock()
	if !running || !cfg.Mihomo.Enabled {
		return nil, errors.New("代理桥未运行：请先启用代理桥并应用后再测延迟")
	}

	refs := []nodeRef{}
	seen := map[string]struct{}{}
	for _, sub := range cfg.Mihomo.Subscriptions {
		for _, node := range sub.Nodes {
			key := config.MihomoNodeKey(sub.ID, node.Name)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, nodeRef{Key: key, Name: node.Name})
		}
	}
	if len(refs) == 0 {
		return []map[string]any{}, nil
	}

	results := m.testNodeDelays(ctx, apiPort, refs)
	// 结果直接写回订阅节点字段（随 mihomo_subscriptions.json 持久化），
	// 已消失节点的旧测速随订阅更新自动清理。
	m.applyHealthResults(results)

	sort.SliceStable(results, func(i, j int) bool {
		okI := results[i].Health.Status == NodeHealthOK
		okJ := results[j].Health.Status == NodeHealthOK
		if okI != okJ {
			return okI // 成功项在前，失败项垫底
		}
		if okI {
			return results[i].Health.LatencyMS < results[j].Health.LatencyMS
		}
		return results[i].Name < results[j].Name
	})
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{"node_key": r.NodeKey, "name": r.Name}
		if r.Health.Status != NodeHealthOK {
			item["error"] = r.Health.Error
			if item["error"] == "" {
				item["error"] = "mihomo 无响应"
			}
		} else {
			item["delay_ms"] = int64(r.Health.LatencyMS)
		}
		out = append(out, item)
	}
	return out, nil
}
