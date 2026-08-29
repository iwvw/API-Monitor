package config

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	// MihomoManagedProxyPrefix 是代理桥自动管理的 config.Proxy ID 前缀。
	// 带此前缀的代理由 Mihomo 桥创建/回收，指向 mihomo 在本机
	// 为单个节点拉起的 SOCKS5 监听端口。
	MihomoManagedProxyPrefix = "mihomo-"
	// DefaultMihomoBasePort 是第一个节点本地监听端口的默认值。
	DefaultMihomoBasePort = 10801
	// DefaultMihomoAPIPort 是 mihomo external-controller 的默认端口。
	DefaultMihomoAPIPort = 19090
)

// MihomoConfig 描述 Mihomo 代理桥（一号一 IP）的全部持久化状态。
// 节点缓存随配置保存，保证重启后绑定关系与端口分配保持稳定。
type MihomoConfig struct {
	Enabled       bool                 `json:"enabled,omitempty"`
	BinaryPath    string               `json:"binary_path,omitempty"`
	BasePort      int                  `json:"base_port,omitempty"`
	APIPort       int                  `json:"api_port,omitempty"`
	Subscriptions []MihomoSubscription `json:"subscriptions,omitempty"`
	PortMap       map[string]int       `json:"port_map,omitempty"`
	// AutoBind 开启自动故障转移与弹性号池补位挂载：
	// 后台巡检定期测速已绑定节点，节点失效时自动把账号切到健康节点，
	// 新启用（或补位上线的弹性号池账号）且未配置代理的账号自动分配健康节点。
	AutoBind bool `json:"auto_bind,omitempty"`
}

// MihomoSubscription 是一个机场订阅及其最近一次解析出的节点缓存。
type MihomoSubscription struct {
	ID        string       `json:"id"`
	Name      string       `json:"name,omitempty"`
	URL       string       `json:"url"`
	UpdatedAt int64        `json:"updated_at,omitempty"`
	Nodes     []MihomoNode `json:"nodes,omitempty"`
}

// MihomoNode 是一个 Clash/Mihomo 节点。Raw 保存原始 proxy map，
// 生成 mihomo 运行时配置时原样写回，避免逐字段建模各协议细节。
// Status/LatencyMS/Error/TestedAt 是最近一次测速结果，直接挂在节点上，
// 随订阅一起持久化到 mihomo_subscriptions.json，重启后不丢失。
type MihomoNode struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Raw       map[string]any `json:"raw,omitempty"`
	Status    string         `json:"health,omitempty"`
	LatencyMS int            `json:"latency_ms,omitempty"`
	Error     string         `json:"health_error,omitempty"`
	TestedAt  int64          `json:"tested_at,omitempty"`
}

// MarshalJSON 序列化 Mihomo 配置时剔除订阅与端口映射：
// 这两部分状态改由 mihomo_subscriptions.json 独立持久化，
// config.json 中只保留开关/二进制路径/端口等轻量设置。
// UnmarshalJSON 仍按字段读取，保证旧版 config.json 中
// 的 subscriptions/port_map 兼容（下次保存时自动迁移）。
func (m MihomoConfig) MarshalJSON() ([]byte, error) {
	aux := struct {
		Enabled    bool   `json:"enabled,omitempty"`
		BinaryPath string `json:"binary_path,omitempty"`
		BasePort   int    `json:"base_port,omitempty"`
		APIPort    int    `json:"api_port,omitempty"`
		AutoBind   bool   `json:"auto_bind,omitempty"`
	}{
		Enabled:    m.Enabled,
		BinaryPath: m.BinaryPath,
		BasePort:   m.BasePort,
		APIPort:    m.APIPort,
		AutoBind:   m.AutoBind,
	}
	return json.Marshal(aux)
}

// NormalizeMihomoConfig 填充默认值并修剪空白。默认端口不视为“已配置”，
// 因此序列化时与零值同等处理。
func NormalizeMihomoConfig(m MihomoConfig) MihomoConfig {
	m.BinaryPath = strings.TrimSpace(m.BinaryPath)
	if m.BasePort <= 0 {
		m.BasePort = DefaultMihomoBasePort
	}
	if m.APIPort <= 0 {
		m.APIPort = DefaultMihomoAPIPort
	}
	for i := range m.Subscriptions {
		m.Subscriptions[i].ID = strings.TrimSpace(m.Subscriptions[i].ID)
		m.Subscriptions[i].Name = strings.TrimSpace(m.Subscriptions[i].Name)
		m.Subscriptions[i].URL = strings.TrimSpace(m.Subscriptions[i].URL)
		for j := range m.Subscriptions[i].Nodes {
			m.Subscriptions[i].Nodes[j].Name = strings.TrimSpace(m.Subscriptions[i].Nodes[j].Name)
			m.Subscriptions[i].Nodes[j].Type = strings.ToLower(strings.TrimSpace(m.Subscriptions[i].Nodes[j].Type))
		}
	}
	return m
}

// IsZeroMihomoConfig 判断归一化后的配置是否等价于未配置（默认值不计）。
func IsZeroMihomoConfig(m MihomoConfig) bool {
	return !m.Enabled &&
		!m.AutoBind &&
		m.BinaryPath == "" &&
		(m.BasePort == 0 || m.BasePort == DefaultMihomoBasePort) &&
		(m.APIPort == 0 || m.APIPort == DefaultMihomoAPIPort) &&
		len(m.Subscriptions) == 0 &&
		len(m.PortMap) == 0
}

// MihomoNodeKey 生成节点在 PortMap / 托管代理中的稳定键。
// 节点名只在订阅内唯一，因此键包含订阅 ID。
func MihomoNodeKey(subID, nodeName string) string {
	return strings.TrimSpace(subID) + "::" + strings.TrimSpace(nodeName)
}

// SplitMihomoNodeKey 拆解节点键，非法键返回两个空串。
// subID 是受控生成的标识（形如 sub-<hash>，不含 "::"），因此首个 "::"
// 即为分隔符；节点名可任意包含 "::"，按首处分隔即可完整保留节点名。
func SplitMihomoNodeKey(key string) (subID, nodeName string) {
	subID, nodeName, found := strings.Cut(key, "::")
	if !found || subID == "" || nodeName == "" {
		return "", ""
	}
	return subID, nodeName
}

// MihomoManagedProxyID 返回节点对应的托管代理 ID（稳定哈希）。
func MihomoManagedProxyID(nodeKey string) string {
	sum := sha1.Sum([]byte(nodeKey))
	return MihomoManagedProxyPrefix + hex.EncodeToString(sum[:8])
}

// IsMihomoManagedProxyID 判断代理 ID 是否由 Mihomo 桥托管。
func IsMihomoManagedProxyID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), MihomoManagedProxyPrefix)
}

// FindMihomoNode 在订阅缓存中按节点键查找节点。
func (m MihomoConfig) FindMihomoNode(nodeKey string) (MihomoNode, bool) {
	subID, nodeName := SplitMihomoNodeKey(nodeKey)
	if subID == "" || nodeName == "" {
		return MihomoNode{}, false
	}
	for _, sub := range m.Subscriptions {
		if sub.ID != subID {
			continue
		}
		for _, node := range sub.Nodes {
			if node.Name == nodeName {
				return node, true
			}
		}
	}
	return MihomoNode{}, false
}

// SortedPortMapKeys 以端口升序返回节点键，保证生成配置与端口分配确定性。
func (m MihomoConfig) SortedPortMapKeys() []string {
	keys := make([]string, 0, len(m.PortMap))
	for key := range m.PortMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, pj := m.PortMap[keys[i]], m.PortMap[keys[j]]
		if pi != pj {
			return pi < pj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// AllocateMihomoPort 为节点键分配一个本地端口；已分配则原样返回。
// 从 BasePort 起递增，跳过已被 PortMap 占用的端口，并始终预留
// external-controller（APIPort）。否则当 api_port 落在
// [base_port, base_port+已分配数) 区间内时，后续分配会撞上 mihomo
// 自身监听的 api_port，导致下一次 Apply 因"端口被占用"而失败。
func (m *MihomoConfig) AllocateMihomoPort(nodeKey string) int {
	if m.PortMap == nil {
		m.PortMap = map[string]int{}
	}
	if port, ok := m.PortMap[nodeKey]; ok && port > 0 {
		return port
	}
	used := make(map[int]struct{}, len(m.PortMap)+1)
	for _, port := range m.PortMap {
		used[port] = struct{}{}
	}
	if m.APIPort > 0 {
		used[m.APIPort] = struct{}{}
	}
	base := m.BasePort
	if base <= 0 {
		base = DefaultMihomoBasePort
	}
	port := base
	for {
		if _, taken := used[port]; !taken {
			break
		}
		port++
	}
	m.PortMap[nodeKey] = port
	return port
}

// ValidateMihomoConfig 校验 Mihomo 桥配置。
func ValidateMihomoConfig(m MihomoConfig) error {
	if IsZeroMihomoConfig(m) {
		return nil
	}
	if err := ValidateIntRange("mihomo.base_port", m.BasePort, 1, 65535, false); err != nil {
		return err
	}
	if err := ValidateIntRange("mihomo.api_port", m.APIPort, 1, 65535, false); err != nil {
		return err
	}
	if m.BasePort > 0 && m.APIPort > 0 && m.APIPort >= m.BasePort && m.APIPort < m.BasePort+len(m.PortMap)+16 {
		// external-controller 不能落在节点监听端口区间内。
		if _, taken := m.PortMap[nodeKeyForPort(m, m.APIPort)]; taken || portReservedByRange(m, m.APIPort) {
			return fmt.Errorf("mihomo.api_port %d conflicts with node listener ports", m.APIPort)
		}
	}
	seenSubIDs := map[string]struct{}{}
	for _, sub := range m.Subscriptions {
		if err := ValidateTrimmedString("mihomo.subscriptions.id", sub.ID, true); err != nil {
			return err
		}
		if _, dup := seenSubIDs[sub.ID]; dup {
			return fmt.Errorf("duplicate mihomo subscription id: %s", sub.ID)
		}
		seenSubIDs[sub.ID] = struct{}{}
		if err := ValidateTrimmedString("mihomo.subscriptions.url", sub.URL, true); err != nil {
			return err
		}
		seenNodes := map[string]struct{}{}
		for _, node := range sub.Nodes {
			if err := ValidateTrimmedString("mihomo.subscriptions.nodes.name", node.Name, true); err != nil {
				return err
			}
			if _, dup := seenNodes[node.Name]; dup {
				return fmt.Errorf("duplicate node name %q in subscription %s", node.Name, sub.ID)
			}
			seenNodes[node.Name] = struct{}{}
		}
	}
	usedPorts := map[int]string{}
	for key, port := range m.PortMap {
		if _, nodeOK := m.FindMihomoNode(key); !nodeOK {
			continue // 悬空分配由 GC 回收，不阻断配置保存
		}
		if err := ValidateIntRange("mihomo.port_map", port, 1, 65535, true); err != nil {
			return err
		}
		if prev, dup := usedPorts[port]; dup {
			return fmt.Errorf("mihomo.port_map port %d assigned to both %q and %q", port, prev, key)
		}
		usedPorts[port] = key
	}
	return nil
}

func nodeKeyForPort(m MihomoConfig, port int) string {
	for key, p := range m.PortMap {
		if p == port {
			return key
		}
	}
	return ""
}

func portReservedByRange(m MihomoConfig, port int) bool {
	for _, p := range m.PortMap {
		if p == port {
			return true
		}
	}
	return false
}
