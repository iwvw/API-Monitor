package mihomo

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// activeBinding 描述一个需要拉起本地监听端口的节点绑定。
type activeBinding struct {
	NodeKey  string
	NodeName string
	Port     int
}

// collectActiveBindings 汇总“已分配端口 + 节点仍存在 + 托管代理已创建 +
// 至少一个账号引用该托管代理”的节点集合，按端口升序返回。
func collectActiveBindings(cfg config.Config) []activeBinding {
	usedProxyIDs := map[string]int{}
	for _, acc := range cfg.Accounts {
		proxyID := strings.TrimSpace(acc.ProxyID)
		if config.IsMihomoManagedProxyID(proxyID) {
			usedProxyIDs[proxyID]++
		}
	}
	if len(usedProxyIDs) == 0 {
		return nil
	}
	managedProxyIDs := map[string]struct{}{}
	for _, proxy := range cfg.Proxies {
		if id := strings.TrimSpace(proxy.ID); config.IsMihomoManagedProxyID(id) {
			managedProxyIDs[id] = struct{}{}
		}
	}
	out := []activeBinding{}
	for _, nodeKey := range cfg.Mihomo.SortedPortMapKeys() {
		proxyID := config.MihomoManagedProxyID(nodeKey)
		if usedProxyIDs[proxyID] == 0 {
			continue
		}
		if _, ok := managedProxyIDs[proxyID]; !ok {
			continue
		}
		node, ok := cfg.Mihomo.FindMihomoNode(nodeKey)
		if !ok {
			continue
		}
		out = append(out, activeBinding{
			NodeKey:  nodeKey,
			NodeName: node.Name,
			Port:     cfg.Mihomo.PortMap[nodeKey],
		})
	}
	return out
}

// buildProxyList 合并所有订阅的节点，供 mihomo 运行时配置的 proxies 段使用。
// proxy 名称使用 "subID::nodeName"（与 MihomoNodeKey 一致），保证跨订阅
// 同名节点不冲突——否则两个订阅都叫 "香港 01" 时会被去重成一个 proxy，
// 两个账号的 listener 会静默走同一个出口节点，"一号一 IP"失效。
func buildProxyList(cfg config.MihomoConfig) []map[string]any {
	seen := map[string]struct{}{}
	out := []map[string]any{}
	for _, sub := range cfg.Subscriptions {
		for _, node := range sub.Nodes {
			name := config.MihomoNodeKey(sub.ID, node.Name)
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			raw := make(map[string]any, len(node.Raw)+2)
			for k, v := range node.Raw {
				raw[k] = v
			}
			raw["name"] = name
			raw["type"] = node.Type
			out = append(out, raw)
		}
	}
	return out
}

// BuildRuntimeYAML 生成 mihomo 运行时配置：
//   - proxies: 全部订阅节点（名称用 "subID::nodeName" 保证跨订阅唯一）
//   - listeners: 每个活跃绑定一个 127.0.0.1 的 SOCKS5 入站，
//     通过 mihomo listener 的 proxy 字段把该入站流量直出到对应节点，
//     从而实现"一个端口一个出口 IP"。
//
// apiSecret 写入 external-controller 的 secret，用于保护控制 API。
func BuildRuntimeYAML(cfg config.Config, apiSecret string) ([]byte, []activeBinding, error) {
	mcfg := cfg.Mihomo
	bindings := collectActiveBindings(cfg)

	listeners := make([]map[string]any, 0, len(bindings))
	for _, b := range bindings {
		listeners = append(listeners, map[string]any{
			"name":   fmt.Sprintf("ds2api-in-%d", b.Port),
			"type":   "socks",
			"listen": "127.0.0.1",
			"port":   b.Port,
			"udp":    true,
			"proxy":  b.NodeKey, // proxy 名与 buildProxyList 的限定名一致
		})
	}

	proxies := buildProxyList(mcfg)
	doc := map[string]any{
		"log-level":           "warning",
		"allow-lan":           false,
		"mode":                "global",
		"ipv6":                false,
		"external-controller": fmt.Sprintf("127.0.0.1:%d", mcfg.APIPort),
		"dns":                 map[string]any{"enable": false},
		// listeners 已带 proxy 直出；兜底规则仅用于非 listener 流量。
		"rules": []string{"MATCH,DIRECT"},
	}
	if apiSecret != "" {
		// 控制接口权限极大（切换节点、读配置、触发动作），
		// 即使只绑 127.0.0.1 也必须有 secret，防止本机其它进程越权访问。
		doc["secret"] = apiSecret
	}
	if len(proxies) == 0 {
		doc["proxies"] = []any{}
	} else {
		doc["proxies"] = proxies
	}
	if len(listeners) == 0 {
		doc["listeners"] = []any{}
	} else {
		doc["listeners"] = listeners
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("生成 mihomo 配置失败: %w", err)
	}
	return out, bindings, nil
}
