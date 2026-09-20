package aiagent

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
	Verified bool   `json:"verified"`
	Notes    string `json:"notes,omitempty"`
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
