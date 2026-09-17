package aiagent

// Provider 描述一种 AI 编码 Agent 的接入参数。网关、探测与实例校验全部读取本
// 注册表，不写死具体 Provider 分支；新增一种 Agent 只需要增加一个条目。
type Provider struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	DefaultPort  int      `json:"defaultPort"`
	ProcessMatch []string `json:"processMatch"`
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
		ID:           "opencode",
		Label:        "OpenCode",
		DefaultPort:  4096,
		ProcessMatch: []string{"opencode"},
		BasePath:     "",
		Streaming:    "sse",
		Verified:     true,
	},
	{
		ID:           "pi",
		Label:        "Pi",
		DefaultPort:  3000,
		ProcessMatch: []string{"pi"},
		BasePath:     "",
		Streaming:    "sse",
		Verified:     false,
		Notes:        "默认端口与进程名待实测确认",
	},
	{
		ID:           "codex",
		Label:        "Codex CLI",
		DefaultPort:  1455,
		ProcessMatch: []string{"codex"},
		BasePath:     "",
		Streaming:    "sse",
		Verified:     false,
		Notes:        "默认端口与进程名待实测确认",
	},
	{
		ID:           "claude-code",
		Label:        "Claude Code",
		DefaultPort:  4000,
		ProcessMatch: []string{"claude"},
		BasePath:     "",
		Streaming:    "sse",
		Verified:     false,
		Notes:        "默认端口与进程名待实测确认",
	},
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
