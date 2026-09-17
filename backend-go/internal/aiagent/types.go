package aiagent

import "time"

// 错误码：网关与用户面接口在错误响应中使用，便于客户端区分处置方式。
const (
	CodeUnauthorized  = "AIAGENT_UNAUTHORIZED"
	CodeForbidden     = "AIAGENT_FORBIDDEN"
	CodeNotFound      = "AIAGENT_NOT_FOUND"
	CodeConflict      = "AIAGENT_CONFLICT"
	CodeInvalid       = "AIAGENT_INVALID"
	CodeHostOffline   = "AIAGENT_HOST_OFFLINE"
	CodeChannelError  = "AIAGENT_CHANNEL_ERROR"
	CodeTimeout       = "AIAGENT_TIMEOUT"
	CodeRateLimited   = "AIAGENT_RATE_LIMITED"
	CodeAgentOutdated = "AIAGENT_AGENT_OUTDATED"
)

// User 是模块自带的用户，与面板管理员账号相互独立。
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	LastLoginAt string `json:"lastLoginAt,omitempty"`
}

// Token 是长期设备令牌的展示视图，不包含明文与哈希。
type Token struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Username    string `json:"username,omitempty"`
	Prefix      string `json:"prefix"`
	DeviceLabel string `json:"deviceLabel,omitempty"`
	ExpiresAt   string `json:"expiresAt"`
	RevokedAt   string `json:"revokedAt,omitempty"`
	LastUsedAt  string `json:"lastUsedAt,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// Instance 是用户登记的一台机器上的一个 AI Agent 服务。
type Instance struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Username  string `json:"username,omitempty"`
	ServerID  string `json:"serverId"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// InstanceView 是实例列表返回的完整视图，含在线探测结果与接入信息。
type InstanceView struct {
	Instance
	ProviderLabel string        `json:"providerLabel"`
	HostName      string        `json:"hostName,omitempty"`
	HostOnline    bool          `json:"hostOnline"`
	Status        InstanceState `json:"status"`
	AccessPath    string        `json:"accessPath"`
	GatewayURL    string        `json:"gatewayUrl"`
}

// InstanceState 是实例的在线状态。
type InstanceState struct {
	// Online 为真表示 Agent 进程正在运行且端口在监听。
	Online         bool   `json:"online"`
	HostOnline     bool   `json:"hostOnline"`
	ProcessRunning bool   `json:"processRunning"`
	PortListening  bool   `json:"portListening"`
	PID            int    `json:"pid,omitempty"`
	ProbedAt       string `json:"probedAt,omitempty"`
	Error          string `json:"error,omitempty"`
}

// ProbeResult 是主机 Agent 返回的运行时探测结果。
type ProbeResult struct {
	ProcessRunning bool   `json:"processRunning"`
	PortListening  bool   `json:"portListening"`
	PID            int    `json:"pid,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

// AccessLog 是一条模块访问日志。
type AccessLog struct {
	ID         int64  `json:"id"`
	UserID     string `json:"userId,omitempty"`
	TokenID    string `json:"tokenId,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Action     string `json:"action"`
	Result     string `json:"result"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
	IP         string `json:"ip,omitempty"`
	UserAgent  string `json:"userAgent,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// 请求载荷。

type loginPayload struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceLabel string `json:"deviceLabel"`
}

type userPayload struct {
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	DisplayName *string `json:"displayName"`
	Disabled    *bool   `json:"disabled"`
}

type instancePayload struct {
	ServerID string `json:"serverId"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Port     int    `json:"port"`
	Enabled  *bool  `json:"enabled"`
	// UserID 仅管理员使用：为指定用户创建/转移实例。普通用户忽略该字段。
	UserID string `json:"userId"`
}

type metaPayload struct {
	Meta string `json:"meta"`
}

// 认证上下文：由令牌或 session 解析得到。

type authContext struct {
	UserID   string
	TokenID  string
	Username string
	IsAdmin  bool
}

// nowRFC3339 返回当前 UTC 时间的 RFC3339 字符串，统一写库与响应格式。
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
