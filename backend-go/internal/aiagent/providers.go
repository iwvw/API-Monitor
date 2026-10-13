package aiagent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// gatewayAuthUser 是注入 v2 Basic 鉴权时使用的用户名（与 opencode v2 约定一致）。
const gatewayAuthUser = "opencode"

// GatewayPassword 按实例 ID 派生 v2 服务的 Basic 密码。
//
// 云端不额外存储密钥：Agent 启动 opencode-v2 时用同一规则设置
// OPENCODE_SERVER_PASSWORD，网关转发时用同一规则注入 Basic 头，两端无需协商。
// 派生值取自实例 ID 的 SHA-256，实例 ID 本身是随机不可猜的。
func GatewayPassword(instanceID string) string {
	sum := sha256.Sum256([]byte("aiagent-v2:" + instanceID))
	return hex.EncodeToString(sum[:])
}

// BasicAuthHeader 返回注入 v2 服务的 Authorization 头值。
func BasicAuthHeader(instanceID string) string {
	cred := gatewayAuthUser + ":" + GatewayPassword(instanceID)
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
}

// Provider 描述一种 AI 编码 Agent 的接入参数。网关、探测与实例校验全部读取本
// 注册表，不写死具体 Provider 分支；新增一种 Agent 只需要增加一个条目。
type Provider struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	DefaultPort  int      `json:"defaultPort"`
	ProcessMatch []string `json:"processMatch"`
	// PortRangeSize 是允许的端口区间大小：合法端口落在
	// [DefaultPort, DefaultPort+PortRangeSize]。为 0 时退化为「只允许默认端口」。
	//
	// 放开区间是为了解决「默认端口被占用即不可用」的问题（ADR-0006 第 1 条）。
	// 保留区间上界而非完全放开，是为了不把网关变成到主机任意本地服务的转发器：
	// 端口仍必须落在该 Provider 的约定区间内，且同一主机内 (server_id, port) 唯一。
	PortRangeSize int `json:"portRangeSize"`
	// BasePath 是网关转发时的目标前缀（挂在 127.0.0.1:<port> 之后）。
	BasePath  string `json:"basePath"`
	Streaming string `json:"streaming"`
	// Verified 表示该 Provider 的端口与进程规则是否经过实测确认。
	Verified bool `json:"verified"`
	Notes    string `json:"notes,omitempty"`
	// InjectGatewayAuth 为真时，网关转发前注入 Basic 鉴权头
	// （用户名 opencode，密码由实例 ID 派生）。opencode v2（2.0.x）的
	// /api/* 强制鉴权，而客户端/Agent 用同一实例 ID 派生同一密码，
	// 无需在云端额外存储密钥。
	InjectGatewayAuth bool `json:"injectGatewayAuth,omitempty"`
}

// providerRegistry 是内置 Provider 清单。第一版 opencode 已实测；其余为占位登记，
// 端口与进程名可在实测后修正，不需要改动网关与账号体系。
var providerRegistry = []Provider{
	{
		ID:            "opencode",
		Label:         "OpenCode",
		DefaultPort:   4096,
		PortRangeSize: 99,
		ProcessMatch:  []string{"opencode"},
		BasePath:      "",
		Streaming:     "sse",
		Verified:      true,
	},
	{
		// opencode 2.0.x：API 挂 /api/* 且强制 Basic 鉴权。Agent 以实例 ID
		// 派生 OPENCODE_SERVER_PASSWORD 启动，网关按同一规则注入 Basic。
		ID:                "opencode-v2",
		Label:             "OpenCode (v2)",
		DefaultPort:       4096,
		PortRangeSize:     99,
		ProcessMatch:      []string{"opencode"},
		BasePath:          "",
		Streaming:         "sse",
		Verified:          true,
		InjectGatewayAuth: true,
		Notes:             "opencode 2.x（/api/* 前缀 + Basic 鉴权）",
	},
	{
		ID:            "pi",
		Label:         "Pi",
		DefaultPort:   3000,
		PortRangeSize: 99,
		ProcessMatch:  []string{"pi"},
		BasePath:      "",
		Streaming:     "sse",
		Verified:      false,
		Notes:         "默认端口与进程名待实测确认",
	},
	{
		ID:            "codex",
		Label:         "Codex CLI",
		DefaultPort:   1455,
		PortRangeSize: 99,
		ProcessMatch:  []string{"codex"},
		BasePath:      "",
		Streaming:     "sse",
		Verified:      false,
		Notes:         "默认端口与进程名待实测确认",
	},
	{
		ID:            "claude-code",
		Label:         "Claude Code",
		DefaultPort:   4000,
		PortRangeSize: 99,
		ProcessMatch:  []string{"claude"},
		BasePath:      "",
		Streaming:     "sse",
		Verified:      false,
		Notes:         "默认端口与进程名待实测确认",
	},
}

// PortAllowed 判断端口是否落在该 Provider 的允许区间内。
func (p Provider) PortAllowed(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	if p.PortRangeSize <= 0 {
		return port == p.DefaultPort
	}
	return port >= p.DefaultPort && port <= p.DefaultPort+p.PortRangeSize
}

// Providers 返回 Provider 注册表副本。
func Providers() []Provider {
	return append([]Provider(nil), providerRegistry...)
}

// LookupProvider 按 ID 查找 Provider。ID 为空时回退到默认 Provider（opencode）。
func LookupProvider(id string) (Provider, bool) {
	if id == "" {
		id = defaultProviderID
	}
	for _, provider := range providerRegistry {
		if provider.ID == id {
			return provider, true
		}
	}
	return Provider{}, false
}

// IsKnownProvider 判断 Provider ID 是否已登记。
func IsKnownProvider(id string) bool {
	_, ok := LookupProvider(id)
	return ok
}

const defaultProviderID = "opencode"

func providerIDs() []string {
	ids := make([]string, 0, len(providerRegistry))
	for _, provider := range providerRegistry {
		ids = append(ids, provider.ID)
	}
	return ids
}
