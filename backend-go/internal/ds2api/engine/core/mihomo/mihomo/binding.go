package mihomo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// accountMatches 复刻 admin/shared 的账号匹配规则（邮箱精确匹配 /
// 手机号归一化匹配 / Identifier 匹配），保持绑定入口行为一致。
func accountMatches(acc config.Account, identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}
	if strings.TrimSpace(acc.Email) == id {
		return true
	}
	if mobileKey := config.CanonicalMobileKey(id); mobileKey != "" && mobileKey == config.CanonicalMobileKey(acc.Mobile) {
		return true
	}
	return acc.Identifier() == id
}

// validateMutation 与代理管理接口使用同一套校验，保证配置始终可加载。
func validateMutation(c *config.Config) error {
	if err := config.ValidateMihomoConfig(c.Mihomo); err != nil {
		return err
	}
	if err := config.ValidateProxyConfig(c.Proxies); err != nil {
		return err
	}
	return config.ValidateAccountProxyReferences(c.Accounts, c.Proxies)
}

// gcLocked 回收三类悬空状态（在 Store.Update 事务内运行）：
//  1. 节点已从订阅中消失的端口分配
//  2. 没有任何账号引用的托管代理（mihomo- 前缀）
//  3. 引用已删除托管代理的账号绑定，及无引用的端口分配
func gcLocked(c *config.Config) {
	for nodeKey := range c.Mihomo.PortMap {
		if _, ok := c.Mihomo.FindMihomoNode(nodeKey); !ok {
			delete(c.Mihomo.PortMap, nodeKey)
		}
	}
	used := map[string]struct{}{}
	for _, acc := range c.Accounts {
		if id := strings.TrimSpace(acc.ProxyID); config.IsMihomoManagedProxyID(id) {
			used[id] = struct{}{}
		}
	}
	kept := c.Proxies[:0]
	for _, proxy := range c.Proxies {
		id := strings.TrimSpace(proxy.ID)
		if config.IsMihomoManagedProxyID(id) {
			if _, ok := used[id]; !ok {
				continue // 无账号引用的托管代理回收
			}
		}
		kept = append(kept, proxy)
	}
	c.Proxies = kept
	existing := map[string]struct{}{}
	for _, proxy := range c.Proxies {
		existing[strings.TrimSpace(proxy.ID)] = struct{}{}
	}
	for i := range c.Accounts {
		id := strings.TrimSpace(c.Accounts[i].ProxyID)
		if config.IsMihomoManagedProxyID(id) {
			if _, ok := existing[id]; !ok {
				c.Accounts[i].ProxyID = ""
			}
		}
	}
	for nodeKey := range c.Mihomo.PortMap {
		proxyID := config.MihomoManagedProxyID(nodeKey)
		if _, ok := existing[proxyID]; !ok {
			delete(c.Mihomo.PortMap, nodeKey)
		}
	}
}

// clearManagedBindingsLocked 清除全部账号的 mihomo 托管节点绑定并回收托管代理与
// 端口分配。只清托管绑定（mihomo- 前缀），保留用户手动配置的非托管代理；
// 不置 no_proxy（与显式解绑语义分开，桥重新开启后仍可被自动调度分配）。
// 供「关闭代理桥」与「启动时发现桥被手动关闭」两处共用。
func clearManagedBindingsLocked(c *config.Config) {
	for i := range c.Accounts {
		if config.IsMihomoManagedProxyID(c.Accounts[i].ProxyID) {
			c.Accounts[i].ProxyID = ""
			c.Accounts[i].NodeCooldownUntil = 0
		}
	}
	gcLocked(c)
}

// upsertManagedProxyLocked 创建或更新节点对应的托管代理（socks5://127.0.0.1:port）。
func upsertManagedProxyLocked(c *config.Config, nodeKey, nodeName string, port int) config.Proxy {
	proxyID := config.MihomoManagedProxyID(nodeKey)
	for i, proxy := range c.Proxies {
		if strings.TrimSpace(proxy.ID) == proxyID {
			c.Proxies[i].Name = nodeName
			c.Proxies[i].Type = "socks5"
			c.Proxies[i].Host = "127.0.0.1"
			c.Proxies[i].Port = port
			return c.Proxies[i]
		}
	}
	proxy := config.Proxy{
		ID:   proxyID,
		Name: nodeName,
		Type: "socks5",
		Host: "127.0.0.1",
		Port: port,
	}
	c.Proxies = append(c.Proxies, proxy)
	return proxy
}

// BindAccount 把账号绑定到节点（nodeKey 为空字符串表示解绑）。
// 事务内完成端口分配、托管代理 upsert、账号 ProxyID 更新与 GC，
// 成功后重置账号池并重启 mihomo 使监听端口生效。
func (m *Manager) BindAccount(_ context.Context, identifier, nodeKey string) error {
	if m == nil || m.store == nil {
		return errors.New("mihomo manager 未初始化")
	}
	identifier = strings.TrimSpace(identifier)
	nodeKey = strings.TrimSpace(nodeKey)
	err := m.store.Update(func(c *config.Config) error {
		idx := -1
		for i, acc := range c.Accounts {
			if accountMatches(acc, identifier) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return errors.New("账号不存在")
		}
		if nodeKey == "" {
			c.Accounts[idx].ProxyID = ""
			c.Accounts[idx].NodeCooldownUntil = 0
			c.Accounts[idx].NoProxy = true // 显式解绑：走直连，自动调度不再分配
			gcLocked(c)
			return validateMutation(c)
		}
		node, ok := c.Mihomo.FindMihomoNode(nodeKey)
		if !ok {
			return errors.New("节点不存在（订阅可能已被删除），请先刷新节点列表")
		}
		port := c.Mihomo.AllocateMihomoPort(nodeKey)
		proxy := upsertManagedProxyLocked(c, nodeKey, node.Name, port)
		c.Accounts[idx].ProxyID = proxy.ID
		c.Accounts[idx].NodeCooldownUntil = 0 // 手动绑定视为用户显式选择，重置自动换号冷却
		c.Accounts[idx].NoProxy = false       // 显式绑定节点，允许自动调度
		gcLocked(c)
		return validateMutation(c)
	})
	if err != nil {
		return err
	}
	m.resetPool()
	m.applyBestEffort()
	return nil
}

// AssignAccounts 一键为全部账号分配节点绑定：账号按配置顺序从上到下依次
// 绑定到传入的节点列表（尽量保证每个节点只绑定一个账号，账号多于节点时
// 循环从头再来）。已存在的 mihomo 托管绑定会先全部解除再按新分配重建，
// 最终在单次事务 + 单次 Apply 内完成。返回实际绑定数。
//
// 健康池已标记为 fail 的节点会被服务端自动剔除（即使前端误传也不绑定）；
// 健康池为空（未测过延迟）时按传入列表原样分配，保持向后兼容。
func (m *Manager) AssignAccounts(_ context.Context, nodeKeys []string) (int, error) {
	if m == nil || m.store == nil {
		return 0, errors.New("mihomo manager 未初始化")
	}
	health := m.NodeHealthMap()
	clean := make([]string, 0, len(nodeKeys))
	seen := map[string]struct{}{}
	for _, key := range nodeKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if h, known := health[key]; known && h.Status == NodeHealthFail {
			continue // 测速失败的节点不参与分配
		}
		clean = append(clean, key)
	}
	nodeKeys = clean
	if len(nodeKeys) == 0 {
		return 0, errors.New("没有可用的节点：请先添加订阅；若已测延迟请确认存在测试成功的节点")
	}

	bound := 0
	err := m.store.Update(func(c *config.Config) error {
		for _, key := range nodeKeys {
			if _, ok := c.Mihomo.FindMihomoNode(key); !ok {
				return fmt.Errorf("节点不存在（订阅可能已被删除），请刷新节点列表后重试: %s", key)
			}
		}
		// 先解除全部 mihomo 托管绑定，避免残留旧绑定。
		for i := range c.Accounts {
			if config.IsMihomoManagedProxyID(strings.TrimSpace(c.Accounts[i].ProxyID)) {
				c.Accounts[i].ProxyID = ""
			}
		}
		gcLocked(c)
		// 按节点列表从上到下循环分配（跳过禁用/弹性号池休眠账号，不占用端口）。
		// 显式选择"不走代理"（NoProxy）的账号同样跳过，不参与一键分配。
		idx := 0
		for i := range c.Accounts {
			if c.Accounts[i].Disabled || c.Accounts[i].Identifier() == "" || c.Accounts[i].NoProxy {
				continue
			}
			nodeKey := nodeKeys[idx%len(nodeKeys)]
			idx++
			node, ok := c.Mihomo.FindMihomoNode(nodeKey)
			if !ok {
				continue
			}
			port := c.Mihomo.AllocateMihomoPort(nodeKey)
			proxy := upsertManagedProxyLocked(c, nodeKey, node.Name, port)
			c.Accounts[i].ProxyID = proxy.ID
			c.Accounts[i].NodeCooldownUntil = 0 // 一键分配视为用户显式重排，重置自动换号冷却
			bound++
		}
		gcLocked(c)
		return validateMutation(c)
	})
	if err != nil {
		return 0, err
	}
	m.resetPool()
	m.applyBestEffort()
	return bound, nil
}

// AddSubscription 抓取并保存一个新订阅，返回订阅 ID。
func (m *Manager) AddSubscription(ctx context.Context, name, rawURL string) (config.MihomoSubscription, error) {
	if m == nil || m.store == nil {
		return config.MihomoSubscription{}, errors.New("mihomo manager 未初始化")
	}
	name = strings.TrimSpace(name)
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return config.MihomoSubscription{}, errors.New("订阅链接不能为空")
	}
	// 网络抓取在 Store 锁外执行，避免阻塞配置读写。
	nodes, err := FetchSubscription(ctx, rawURL)
	if err != nil {
		return config.MihomoSubscription{}, err
	}
	sub := config.MihomoSubscription{
		ID:        "sub-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Name:      name,
		URL:       rawURL,
		UpdatedAt: time.Now().Unix(),
		Nodes:     nodes,
	}
	if sub.Name == "" {
		sub.Name = rawURL
	}
	if err := m.store.Update(func(c *config.Config) error {
		c.Mihomo.Subscriptions = append(c.Mihomo.Subscriptions, sub)
		return validateMutation(c)
	}); err != nil {
		return config.MihomoSubscription{}, err
	}
	m.applyBestEffort()
	return sub, nil
}

// RefreshSubscription 重新抓取订阅节点；节点集合变化后执行 GC
// （消失节点的绑定会被解除并回收端口）。
func (m *Manager) RefreshSubscription(ctx context.Context, subID string) (int, error) {
	if m == nil || m.store == nil {
		return 0, errors.New("mihomo manager 未初始化")
	}
	subID = strings.TrimSpace(subID)
	snap := m.store.Snapshot()
	var rawURL string
	for _, sub := range snap.Mihomo.Subscriptions {
		if sub.ID == subID {
			rawURL = sub.URL
			break
		}
	}
	if rawURL == "" {
		return 0, errors.New("订阅不存在")
	}
	nodes, err := FetchSubscription(ctx, rawURL)
	if err != nil {
		return 0, err
	}
	if err := m.store.Update(func(c *config.Config) error {
		for i := range c.Mihomo.Subscriptions {
			if c.Mihomo.Subscriptions[i].ID != subID {
				continue
			}
			c.Mihomo.Subscriptions[i].Nodes = nodes
			c.Mihomo.Subscriptions[i].UpdatedAt = time.Now().Unix()
			gcLocked(c)
			return validateMutation(c)
		}
		return errors.New("订阅不存在")
	}); err != nil {
		return 0, err
	}
	m.resetPool()
	m.applyBestEffort()
	return len(nodes), nil
}

// DeleteSubscription 删除订阅并解除其节点的全部绑定。
func (m *Manager) DeleteSubscription(_ context.Context, subID string) error {
	if m == nil || m.store == nil {
		return errors.New("mihomo manager 未初始化")
	}
	subID = strings.TrimSpace(subID)
	err := m.store.Update(func(c *config.Config) error {
		idx := -1
		for i, sub := range c.Mihomo.Subscriptions {
			if sub.ID == subID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return errors.New("订阅不存在")
		}
		c.Mihomo.Subscriptions = append(c.Mihomo.Subscriptions[:idx], c.Mihomo.Subscriptions[idx+1:]...)
		// 解除引用该订阅节点的账号绑定（节点已随订阅消失）。
		prefix := subID + "::"
		deadProxies := map[string]struct{}{}
		for nodeKey := range c.Mihomo.PortMap {
			if strings.HasPrefix(nodeKey, prefix) {
				deadProxies[config.MihomoManagedProxyID(nodeKey)] = struct{}{}
			}
		}
		for i := range c.Accounts {
			if _, dead := deadProxies[strings.TrimSpace(c.Accounts[i].ProxyID)]; dead {
				c.Accounts[i].ProxyID = ""
			}
		}
		gcLocked(c)
		return validateMutation(c)
	})
	if err != nil {
		return err
	}
	m.resetPool()
	m.applyBestEffort()
	return nil
}

// UpdateSettings 更新桥开关/二进制路径/端口设置/自动调度开关并立即应用。
func (m *Manager) UpdateSettings(_ context.Context, enabled bool, binaryPath string, basePort, apiPort int, autoBind bool) error {
	if m == nil || m.store == nil {
		return errors.New("mihomo manager 未初始化")
	}
	err := m.store.Update(func(c *config.Config) error {
		c.Mihomo.Enabled = enabled
		c.Mihomo.BinaryPath = strings.TrimSpace(binaryPath)
		c.Mihomo.AutoBind = autoBind
		if basePort > 0 {
			if basePort != c.Mihomo.BasePort && len(c.Mihomo.PortMap) > 0 {
				return fmt.Errorf("已存在端口分配（%d 个），更换 base_port 前请先解除所有账号绑定", len(c.Mihomo.PortMap))
			}
			c.Mihomo.BasePort = basePort
		}
		if apiPort > 0 {
			c.Mihomo.APIPort = apiPort
		}
		if !enabled {
			// 关闭代理桥：清除账号托管绑定，避免进程停止后账号仍指向已死的
			// 本地 socks5 端口导致后续请求全部失败。
			clearManagedBindingsLocked(c)
		}
		c.Mihomo = config.NormalizeMihomoConfig(c.Mihomo)
		return validateMutation(c)
	})
	if err != nil {
		return err
	}
	if !enabled {
		m.resetPool()
	}
	m.applyBestEffort()
	return nil
}

// clearStaleManagedBindings 清理残留的账号托管绑定（桥已关闭但账号 ProxyID 仍指向
// mihomo- 托管代理时）。典型场景：用户手动把配置文件里 mihomo.enabled 改为 false
// 后重启，mihomo 进程不会拉起、本地 socks5 端口不通，账号请求会全部失败。
// 仅当确实存在托管绑定时才写配置；返回是否发生了清理（供调用方决定是否重置账号池）。
func (m *Manager) clearStaleManagedBindings() bool {
	if m == nil || m.store == nil {
		return false
	}
	snap := m.store.Snapshot()
	hasManaged := false
	for i := range snap.Accounts {
		if config.IsMihomoManagedProxyID(snap.Accounts[i].ProxyID) {
			hasManaged = true
			break
		}
	}
	if !hasManaged {
		return false
	}
	cleared := false
	if err := m.store.Update(func(c *config.Config) error {
		clearManagedBindingsLocked(c)
		cleared = true
		return validateMutation(c)
	}); err != nil {
		config.Logger.Warn("[mihomo] clear stale managed bindings failed", "error", err)
		return false
	}
	return cleared
}

// ListNodes 汇总全部订阅节点及其绑定/端口状态，供管理界面展示。
func (m *Manager) ListNodes() []map[string]any {
	if m == nil || m.store == nil {
		return nil
	}
	snap := m.store.Snapshot()
	// accounts 同时携带 identifier（解绑操作的入参）与 label（展示），
	// 避免前端从展示文本里反解析账号标识。
	accountsByProxy := map[string][]map[string]string{}
	for _, acc := range snap.Accounts {
		id := strings.TrimSpace(acc.ProxyID)
		if config.IsMihomoManagedProxyID(id) {
			identifier := acc.Identifier()
			label := identifier
			if strings.TrimSpace(acc.Name) != "" {
				label = strings.TrimSpace(acc.Name) + " (" + identifier + ")"
			}
			accountsByProxy[id] = append(accountsByProxy[id], map[string]string{
				"identifier": identifier,
				"label":      label,
			})
		}
	}
	out := []map[string]any{}
	for _, sub := range snap.Mihomo.Subscriptions {
		for _, node := range sub.Nodes {
			nodeKey := config.MihomoNodeKey(sub.ID, node.Name)
			proxyID := config.MihomoManagedProxyID(nodeKey)
			port := snap.Mihomo.PortMap[nodeKey]
			server := ""
			if v, ok := node.Raw["server"].(string); ok {
				server = v
			}
			status := node.Status
			if status == "" {
				status = NodeHealthUnknown
			}
			out = append(out, map[string]any{
				"node_key":        nodeKey,
				"name":            node.Name,
				"type":            node.Type,
				"server":          server,
				"subscription":    sub.Name,
				"subscription_id": sub.ID,
				"local_port":      port,
				"proxy_id":        proxyID,
				"accounts":        accountsByProxy[proxyID],
				"health":          status,
				"latency_ms":      node.LatencyMS,
				"health_error":    node.Error,
				"tested_at":       node.TestedAt,
			})
		}
	}
	return out
}

func (m *Manager) resetPool() {
	if m == nil {
		return
	}
	m.rebuildProxyNodeIndex()
	if m.pool != nil {
		m.pool.Reset()
	}
	m.mu.Lock()
	reset := m.proxyReset
	m.mu.Unlock()
	if reset != nil {
		// 绑定/订阅变更改变了托管代理（127.0.0.1:<port>）的 host/port，
		// 丢弃 DeepSeek client 侧缓存的代理连接池，避免复用旧端口。
		reset()
	}
}

// applyBestEffort 在配置已持久化成功后尝试重新应用 mihomo 运行时配置。
// 失败只记录（Apply 内部会写 last_error，状态卡片可见），不回滚已保存的绑定——
// 否则用户看到"操作失败"但绑定其实已生效，会重复操作。
func (m *Manager) applyBestEffort() {
	if err := m.Apply(context.Background()); err != nil {
		config.Logger.Warn("[mihomo] apply after config change failed (state persisted, see status.last_error)", "error", err)
	}
}
